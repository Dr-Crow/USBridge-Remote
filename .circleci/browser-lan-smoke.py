#!/usr/bin/env python3
"""Real-browser bundle startup and request-policy smoke; no streaming claim."""
import hashlib
import http.server
import json
import os
import pathlib
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
    class Handler(http.server.SimpleHTTPRequestHandler):
        def __init__(self, *args, **kwargs):
            super().__init__(*args, directory=str(root), **kwargs)
        def log_message(self, *_):
            pass
        def end_headers(self):
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
            page.wait_for_selector('canvas', timeout=90000)
            page.wait_for_timeout(3000)
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
            page.wait_for_selector('canvas', timeout=90000)
            page.set_viewport_size({'width': 390, 'height': 844})
            page.wait_for_timeout(1500)
            page.screenshot(path=str(out / 'mobile.png'), full_page=True)
            assert not report['page_errors'], report['page_errors']
            external = [url for url in report['requests'] if urllib.parse.urlparse(url).hostname != '127.0.0.1']
            assert not external, external
            report['result'] = 'passed'
            browser.close()
    finally:
        server.shutdown()
        (out / 'report.json').write_text(json.dumps(report, indent=2))
print(json.dumps(report, indent=2))
