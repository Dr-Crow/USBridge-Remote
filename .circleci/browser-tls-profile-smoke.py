#!/usr/bin/env python3
"""Normal TLS positive/negative with owned, disposable CircleCI browsers."""
import asyncio
import http.server
import json
import os
import pathlib
import socket
import ssl
import threading
import time
import urllib.parse
from playwright.async_api import async_playwright, Error as BrowserError
from browser_tls_profile import EphemeralBrowserTLS, ProfileError, failure_code
from browser_owned_process import OwnedChromium


class LoopbackTLS:
    def __init__(self, cert, key):
        self.stop=threading.Event();self.lock=threading.Lock();self.connections=set()
        self.joined=False;self.failed=False
        tls=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);tls.minimum_version=ssl.TLSVersion.TLSv1_3
        tls.load_cert_chain(cert,key)
        owner=self
        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self,*_):pass
            def do_GET(self):
                if self.path!='/probe':self.send_error(404);return
                body=b'{"loopback_tls_probe":true}'
                self.send_response(200);self.send_header('Content-Type','application/json')
                self.send_header('Content-Length',str(len(body)))
                self.send_header('Content-Security-Policy',"default-src 'none'; frame-ancestors 'none'")
                self.end_headers();self.wfile.write(body)
        class Server(http.server.HTTPServer):
            def get_request(self):
                raw,address=self.socket.accept();raw.settimeout(1)
                with owner.lock:owner.connections.add(raw)
                try:
                    wrapped=tls.wrap_socket(raw,server_side=True,do_handshake_on_connect=False)
                    with owner.lock:owner.connections.discard(raw);owner.connections.add(wrapped)
                    wrapped.settimeout(1);wrapped.do_handshake()
                    return wrapped,address
                except BaseException:
                    raw.close()
                    if 'wrapped' in locals():wrapped.close()
                    with owner.lock:
                        owner.connections.discard(raw)
                        if 'wrapped' in locals():owner.connections.discard(wrapped)
                    raise
            def shutdown_request(self,request):
                try:super().shutdown_request(request)
                finally:
                    with owner.lock:owner.connections.discard(request)
            def handle_error(self,*_):pass
        self.server=Server(('127.0.0.1',0),Handler);self.server.timeout=.1
        def serve():
            try:
                while not self.stop.is_set():self.server.handle_request()
            except Exception:
                if not self.stop.is_set():self.failed=True
        self.thread=threading.Thread(target=serve,name='loopback-tls-probe');self.thread.start()
    @property
    def port(self):return self.server.server_port
    def close(self):
        self.stop.set()
        with self.lock:
            for connection in tuple(self.connections):
                try:connection.shutdown(socket.SHUT_RDWR)
                except OSError:pass
                connection.close()
        self.server.server_close();self.thread.join(timeout=2)
        self.joined=not self.thread.is_alive() and not self.failed
        if not self.joined:raise ProfileError('https_server_join_unverified')


async def main():
    out=pathlib.Path('artifacts/browser-lan/tls-profile.json');out.parent.mkdir(parents=True,exist_ok=True)
    report={'schema':1,'source_commit':os.environ['CIRCLE_SHA1'],'result':'failed',
            'real_quic':False,'untrusted_rejected':False,'trusted_succeeded':False,
            'browser_processes_joined':False,'https_server_joined':False,'browsers':[]}
    profile=EphemeralBrowserTLS()
    try:
        with profile:
            # Metadata is derived from the exact generated one-hour P-256 leaf,
            # for separately labelled hash-auth fixtures. Ordinary TLS stays
            # the trust route in this smoke test.
            assert profile.receipt['leaf_hash_kind'] == 'full-der-sha256'
            assert profile.receipt['leaf_key_type'] == 'P-256'
            assert profile.receipt['leaf_not_after_ms'] - profile.receipt['leaf_not_before_ms'] == 3600000
            # Native TLS regression: accept a TCP peer that never sends a
            # ClientHello, then prove shutdown interrupts/joins that exact work.
            stalled=LoopbackTLS(profile.cert_file,profile.key_file)
            peer=socket.create_connection(('127.0.0.1',stalled.port),timeout=1)
            start=time.monotonic()
            try:
                until=start+1
                while time.monotonic()<until:
                    with stalled.lock:
                        if stalled.connections:break
                    await asyncio.sleep(.01)
                with stalled.lock:accepted=bool(stalled.connections)
                if not accepted:raise ProfileError('incomplete_tls_fixture_not_accepted')
            finally:
                stalled.close();peer.close()
            report['incomplete_tls_shutdown_ms']=round((time.monotonic()-start)*1000)
            report['incomplete_tls_joined']=stalled.joined
            if report['incomplete_tls_shutdown_ms']>2100:raise ProfileError('incomplete_tls_shutdown_budget')
            server=LoopbackTLS(profile.cert_file,profile.key_file)
            try:
                async with async_playwright() as p:
                    for trusted in (False,True):
                        env=profile.browser_env if trusted else profile.untrusted_env
                        directory=profile.trusted_profile if trusted else profile.untrusted_profile
                        owner=OwnedChromium(p.chromium.executable_path,directory,env)
                        try:
                            browser=await owner.start(p)
                            context=browser.contexts[0]
                            async def route_local(route):
                                if urllib.parse.urlparse(route.request.url).hostname=='127.0.0.1':await route.continue_()
                                else:await route.abort()
                            await context.route('**/*',route_local)
                            page=await context.new_page()
                            try:response=await page.goto('https://127.0.0.1:%d/probe'%server.port,timeout=10000,wait_until='load')
                            except BrowserError as error:
                                if trusted or 'ERR_CERT_AUTHORITY_INVALID' not in str(error):raise ProfileError('browser_tls_probe_failed') from None
                                report['untrusted_rejected']=True
                            else:
                                if not trusted or response.status!=200 or await response.json()!={'loopback_tls_probe':True}:raise ProfileError('browser_tls_probe_failed')
                                details=await response.security_details()
                                if details is None or details.get('protocol')!='TLS 1.3':raise ProfileError('browser_tls_version')
                                report['trusted_succeeded']=True;report['tls_protocol']=details['protocol']
                        finally:
                            try:await owner.close()
                            finally:report['browsers'].append(owner.receipt)
                report['browser_processes_joined']=len(report['browsers'])==2 and all(x['natural_root_exit'] and x['owned_group_empty'] and x['stderr_reader_joined'] and not x['forced_cleanup'] for x in report['browsers'])
            finally:
                server.close();report['https_server_joined']=server.joined
                report['browser_processes_joined']=bool(report['browsers']) and all(x['natural_root_exit'] and x['owned_group_empty'] and x['stderr_reader_joined'] and not x['forced_cleanup'] for x in report['browsers'])
                if report['https_server_joined'] and report['browser_processes_joined']:profile.confirm_joined()
            if not report['untrusted_rejected'] or not report['trusted_succeeded']:raise ProfileError('trust_comparison_failed')
        report['result']='passed'
    except Exception as error:
        report['failure_code']=failure_code(error)
        raise ProfileError('browser_tls_fixture_failed') from None
    finally:
        report['profile']=profile.receipt;out.write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report,sort_keys=True))

if __name__=='__main__':
    try:asyncio.run(main())
    except Exception:raise SystemExit('Disposable browser TLS prerequisite failed; inspect closed receipt') from None
