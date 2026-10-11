import asyncio
import importlib.util
import os
import pathlib
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock
from browser_owned_process import OwnedChromium, group_members
from browser_tls_profile import ProfileError

class OwnerTests(unittest.IsolatedAsyncioTestCase):
    def make_owner(self,code):
        tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup)
        owner=OwnedChromium(sys.executable,tmp.name,{})
        owner.process=subprocess.Popen([sys.executable,'-c',code],stdin=subprocess.PIPE,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE,start_new_session=True)
        def drain():
            while owner.process.stderr.read(4096):pass
        owner.reader=threading.Thread(target=drain);owner.reader.start()
        return owner
    async def test_real_owned_group_retires_on_fixed_close(self):
        owner=self.make_owner('import sys;sys.stdin.buffer.read()')
        self.assertIn(owner.process.pid,group_members(owner.process.pid))
        class Close:
            async def new_browser_cdp_session(self):return self
            async def send(self,_):owner.process.stdin.close()
        owner.browser=Close();await owner.close()
        self.assertTrue(owner.receipt['natural_root_exit']);self.assertTrue(owner.receipt['owned_group_empty']);self.assertTrue(owner.receipt['stderr_reader_joined']);self.assertFalse(owner.receipt['forced_cleanup'])
    async def test_forced_root_cleanup_is_failure_even_when_group_retires(self):
        owner=self.make_owner('import time;time.sleep(60)')
        try:
            with self.assertRaisesRegex(ProfileError,'browser_retirement_unverified'):await owner.close(timeout=.025)
            self.assertTrue(owner.receipt['forced_cleanup']);self.assertFalse(owner.receipt['natural_root_exit']);self.assertTrue(owner.receipt['owned_group_empty'])
        finally:owner.process.stdin.close()
    async def test_failed_exit_cannot_pass_natural_retirement(self):
        owner=self.make_owner('raise SystemExit(3)')
        try:
            with self.assertRaisesRegex(ProfileError,'browser_retirement_unverified'):await owner.close(timeout=.5)
            self.assertEqual(owner.receipt['root_exit_code'],3);self.assertFalse(owner.receipt['forced_cleanup'])
        finally:owner.process.stdin.close()

class ServerTests(unittest.TestCase):
    def test_incomplete_local_connection_does_not_block_join(self):
        spec=importlib.util.spec_from_file_location('tls_smoke',pathlib.Path(__file__).with_name('browser-tls-profile-smoke.py'))
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        class Wrapped:
            def __init__(self,sock):self.sock=sock
            def __getattr__(self,name):return getattr(self.sock,name)
            def do_handshake(self):pass
        class FakeContext:
            def __init__(self,*_):pass
            def load_cert_chain(self,*_):pass
            def wrap_socket(self,raw,**_):return Wrapped(raw)
        with mock.patch.object(module.ssl,'SSLContext',FakeContext):
            server=module.LoopbackTLS('unused','unused')
        client=socket.create_connection(('127.0.0.1',server.port),timeout=1)
        try:
            time.sleep(.025);start=time.monotonic();server.close();self.assertTrue(server.joined);self.assertLess(time.monotonic()-start,2)
        finally:client.close()

if __name__=='__main__':unittest.main()
