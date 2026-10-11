#!/usr/bin/env python3
"""Real-browser bundle startup and request-policy smoke; no streaming claim."""
import hashlib
import http.server
import json
import os
import pathlib
import re
import tempfile
import threading
import urllib.parse
import zipfile
from playwright.sync_api import sync_playwright

out = pathlib.Path('artifacts/browser-lan')
out.mkdir(parents=True, exist_ok=True)
archives = list(pathlib.Path('artifacts/web-client').glob('USBridge-Web-*.zip'))
assert len(archives) == 1, 'exactly one source-built web bundle required'
report = {'fixture': 'fresh browser; strict runtime config; loopback HTTP; no agent paired',
          'requests': [], 'page_errors': [], 'console_errors': []}

def wait_for_app(page):
    # The page has a deliberately hidden AI overlay before Go creates its app
    # canvas. Waiting on the first generic canvas selects that overlay forever.
    try:
        page.wait_for_selector('canvas:not(#aiVisionCanvas)', state='visible', timeout=90000)
    except Exception:
        report['canvas_state'] = page.locator('canvas').evaluate_all('(nodes) => nodes.map(n => ({id:n.id,width:n.width,height:n.height,display:getComputedStyle(n).display}))')
        page.screenshot(path=str(out / 'startup-failure.png'), full_page=True)
        raise
with tempfile.TemporaryDirectory(prefix='usbridge-browser-') as tmp:
    root = pathlib.Path(tmp)
    with zipfile.ZipFile(archives[0]) as archive:
        provenance = json.loads(archive.read('build-provenance.json'))
        assert provenance['source_commit'] == os.environ['CIRCLE_SHA1']
        assert provenance['tracked_source_modified'] is False
        report['source_commit'] = provenance['source_commit']
        for name, expected in provenance['assets'].items():
            path = pathlib.PurePosixPath(name)
            assert not path.is_absolute() and '..' not in path.parts
            data = archive.read('web/' + name)
            assert len(data) == expected['bytes']
            assert hashlib.sha256(data).hexdigest() == expected['sha256']
            target = root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
    # Production LAN hosting also replaces this config. The downloaded bundle
    # itself remains unchanged; this is an isolated browser fixture copy.
    (root / 'runtime-config.js').write_text('globalThis.USBridgeRuntimeConfig=Object.freeze({strictLAN:true});\n')
    host_source = pathlib.Path('deploy/lan/server/main.go').read_text()
    policy = re.search(r'Set\("Content-Security-Policy",\s*("[^"\n]*")\s*\+\s*h\.connectSources\(\)\s*\+\s*("[^"\n]*")\)', host_source)
    assert policy, 'production CSP extraction must be updated'
    csp = json.loads(policy.group(1)) + json.loads(policy.group(2))
    class Handler(http.server.SimpleHTTPRequestHandler):
        def __init__(self, *args, **kwargs):
            super().__init__(*args, directory=str(root), **kwargs)
        def log_message(self, *_):
            pass
        def end_headers(self):
            self.send_header('Content-Security-Policy', csp)
            self.send_header('Cross-Origin-Opener-Policy', 'same-origin')
            self.send_header('Cross-Origin-Embedder-Policy', 'require-corp')
            super().end_headers()
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(headless=True, chromium_sandbox=True)
            page = browser.new_page(viewport={'width': 1280, 'height': 800})
            page.on('request', lambda r: report['requests'].append(r.url))
            page.on('pageerror', lambda e: report['page_errors'].append(str(e)))
            page.on('console', lambda m: report['console_errors'].append(m.text) if m.type == 'error' else None)
            page.goto('http://127.0.0.1:%d/' % server.server_port, wait_until='networkidle', timeout=90000)
            wait_for_app(page)
            page.wait_for_timeout(3000)
            report['static_transport'] = page.evaluate('''async () => {
              const api=globalThis.__usbridgeSecureWT;
              const proof=await api.probeWorker();
              return {protocol:api.protocol,frozen:Object.isFrozen(api),enabled:api.transportEnabled,proof};
            }''')
            assert report['static_transport']['frozen'] is True and report['static_transport']['enabled'] is False
            assert report['static_transport']['proof']['staticWorkerReady'] is True
            assert report['static_transport']['proof']['mediaEnabled'] is False
            report['control_owner_mock'] = page.evaluate('''async () => {
              const {negotiateControl}=await import('/transport/session.mjs');
              const {PROTOCOL,ControlParser,encodeMessage,validateHello}=await import('/transport/wire.mjs');
              const json=v=>new TextEncoder().encode(JSON.stringify(v));
              let input,ended,closes=0,helloVerified=false;
              const closed=new Promise(resolve=>{ended=resolve;});
              const welcome={protocol:PROTOCOL,session_id:'browser_fixture',codec:'h264',width:128,height:72,fps:30,audio:{codec:'opus',sample_rate:48000,channels:2},input:false,channels:[]};
              const readable=new ReadableStream({start(c){input=c;}});
              const writable=new WritableStream({write(frame){const [m]=new ControlParser([1]).feed(frame);const h=validateHello(m.payload);helloVerified=h.session_id===welcome.session_id;input.enqueue(encodeMessage(1,json(welcome)));}});
              const endpoint='https://127.0.0.1:8444/webtransport';
              class MockTransport {constructor(){this.ready=Promise.resolve();this.closed=closed;}createBidirectionalStream(){return Promise.resolve({readable,writable});}close(){closes++;ended();}}
              const signal=new AbortController();
              const owner=await negotiateControl({grant:{protocol:PROTOCOL,session_id:welcome.session_id,ticket:'A'.repeat(43),endpoint,origin:location.origin,expires_at_ms:Date.now()+10000,width:128,height:72,fps:30},authorizedEndpoint:endpoint,capabilityStatus:200,capabilityBody:json({protocol:PROTOCOL,ready:true,endpoint,tls_mode:'trusted',codec:'h264',audio:'opus',input:false,channels:[]}),signal:signal.signal},{Transport:MockTransport});
              signal.abort();const result=await owner.closed;
              return {helloVerified,parentCancelled:result.reason==='cancelled',joined:result.joined,closedOnce:closes===1,locksReleased:!readable.locked&&!writable.locked,mediaStarted:owner.mediaStarted,realQUIC:false};
            }''')
            assert all(report['control_owner_mock'][key] for key in ('helloVerified','parentCancelled','joined','closedOnce','locksReleased'))
            assert report['control_owner_mock']['mediaStarted'] is False and report['control_owner_mock']['realQUIC'] is False
            report['policy'] = page.evaluate('''async () => {
              let fetchBlocked=false,wsBlocked=false,iceBlocked=false;
              try { await fetch('https://example.com/'); } catch(e) { fetchBlocked=String(e).includes('Strict LAN'); }
              try { new WebSocket('wss://example.com/'); } catch(e) { wsBlocked=String(e).includes('Strict LAN'); }
              try { new RTCPeerConnection({iceServers:[{urls:'stun:stun.l.google.com:19302'}]}); } catch(e) { iceBlocked=String(e).includes('Strict LAN'); }
              return {fetchBlocked,wsBlocked,iceBlocked,strict:globalThis.USBridgeRuntimeConfig.strictLAN};
            }''')
            assert all(report['policy'].values()), report['policy']
            page.screenshot(path=str(out / 'desktop.png'), full_page=True)
            page.reload(wait_until='networkidle', timeout=90000)
            wait_for_app(page)
            page.set_viewport_size({'width': 390, 'height': 844})
            page.wait_for_timeout(1500)
            page.screenshot(path=str(out / 'mobile.png'), full_page=True)
            report['optional_api_cases'] = []
            # Fresh pages exercise secure-context APIs that may be absent or
            # denied. These fixtures request no permission and open no device.
            for mode in ('missing', 'denied'):
                fixture = browser.new_page(viewport={'width': 1280, 'height': 800})
                fixture.on('request', lambda r: report['requests'].append(r.url))
                fixture.on('pageerror', lambda e: report['page_errors'].append(str(e)))
                fixture.add_init_script("""(() => {
                  Object.defineProperty(navigator, 'hid', {value: undefined, configurable: true});
                  Object.defineProperty(navigator, 'getGamepads', {value: MODE === 'missing'
                    ? undefined : function() { throw new DOMException('fixture denied', 'SecurityError'); }, configurable: true});
                })();""".replace('MODE', json.dumps(mode)))
                fixture.goto('http://127.0.0.1:%d/' % server.server_port, wait_until='networkidle', timeout=90000)
                wait_for_app(fixture)
                fixture.wait_for_timeout(1500)
                observed = fixture.evaluate("""() => ({
                  hidAbsent: navigator.hid === undefined,
                  gamepadsMissing: typeof navigator.getGamepads !== 'function',
                  overlayAbsent: typeof globalThis.usbridgeSetHIDButtonRect !== 'function'
                })""")
                assert observed['hidAbsent'] and observed['overlayAbsent'], observed
                assert observed['gamepadsMissing'] == (mode == 'missing'), observed
                report['optional_api_cases'].append({'mode': mode, 'canvas_visible': True, 'observed': observed})
                fixture.close()
            assert not report['page_errors'], report['page_errors']
            external = [url for url in report['requests'] if urllib.parse.urlparse(url).hostname != '127.0.0.1']
            assert not external, external
            report['result'] = 'passed'
            browser.close()
    finally:
        server.shutdown()
        (out / 'report.json').write_text(json.dumps(report, indent=2))
print(json.dumps(report, indent=2))
