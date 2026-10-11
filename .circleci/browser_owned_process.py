"""Linux CI browser process-group ownership; no host profile or trust changes."""
import asyncio
import hashlib
import os
import pathlib
import re
import signal
import subprocess
import threading
import time
from browser_tls_profile import ProfileError


def group_members(group):
    members={}
    for entry in pathlib.Path('/proc').iterdir():
        if not entry.name.isdigit():continue
        try:
            raw=(entry/'stat').read_text()
        except (FileNotFoundError, ProcessLookupError):continue
        except OSError:raise ProfileError('browser_inventory_unavailable') from None
        fields=raw[raw.rfind(')')+2:].split()
        if len(fields)<20:raise ProfileError('browser_inventory_invalid')
        if int(fields[2])==group:members[int(entry.name)]=int(fields[19])
    return members


class OwnedChromium:
    def __init__(self, executable, profile, env):
        self.executable=pathlib.Path(executable).resolve(strict=True)
        self.profile=pathlib.Path(profile).resolve(strict=True)
        self.env=env
        self.process=None;self.browser=None;self.reader=None
        self.stderr_bytes=0;self.stderr_failed=False
        self.receipt={'schema':1,'natural_root_exit':False,'owned_group_empty':False,
                      'stderr_reader_joined':False,'forced_cleanup':False,
                      'sandbox_enabled':True,'devtools_loopback_only':True,'process_inventory_verified':True}

    async def start(self, playwright):
        if os.environ.get('CIRCLECI')!='true':raise ProfileError('isolated_circleci_linux_required')
        if not self.executable.is_file() or self.executable.stat().st_mode&0o022:raise ProfileError('browser_executable_identity')
        self.receipt['browser_sha256']=hashlib.sha256(self.executable.read_bytes()).hexdigest()
        args=[str(self.executable),'--headless=new','--remote-debugging-address=127.0.0.1',
              '--remote-debugging-port=0','--user-data-dir='+str(self.profile),
              '--no-first-run','--no-default-browser-check','--disable-background-networking',
              '--disable-component-update','--disable-default-apps','--disable-sync',
              '--disable-breakpad','--disable-crash-reporter','--metrics-recording-only','about:blank']
        self.process=subprocess.Popen(args,stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,
            stderr=subprocess.PIPE,env=self.env,start_new_session=True,close_fds=True)
        if os.getpgid(self.process.pid)!=self.process.pid:raise ProfileError('browser_group_identity')
        def drain():
            try:
                while True:
                    chunk=self.process.stderr.read(4096)
                    if not chunk:break
                    self.stderr_bytes=min(65537,self.stderr_bytes+len(chunk))
            except OSError:self.stderr_failed=True
        self.reader=threading.Thread(target=drain,name='owned-browser-stderr');self.reader.start()
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            if self.process.poll() is not None:raise ProfileError('browser_startup_exit')
            active=self.profile/'DevToolsActivePort'
            if active.exists():
                if active.is_symlink() or not active.is_file() or not 0<active.stat().st_size<256:raise ProfileError('browser_devtools_identity')
                lines=active.read_text().splitlines()
                if len(lines)!=2 or not lines[0].isdigit() or not 1024<=int(lines[0])<=65535 or not re.fullmatch(r'/devtools/browser/[a-f0-9-]{36}',lines[1]):raise ProfileError('browser_devtools_identity')
                endpoint='ws://127.0.0.1:'+lines[0]+lines[1]
                try:
                    self.browser=await asyncio.wait_for(playwright.chromium.connect_over_cdp(endpoint),max(.001,deadline-time.monotonic()))
                except Exception:
                    raise ProfileError('browser_cdp_attach_failed') from None
                self.receipt['initial_owned_processes']=len(group_members(self.process.pid))
                return self.browser
            await asyncio.sleep(.025)
        raise ProfileError('browser_startup_timeout')

    async def close(self, timeout=5):
        if not isinstance(timeout,(int,float)) or not 0<timeout<=5:raise ProfileError('browser_stop_budget')
        if self.process is None:return
        deadline=time.monotonic()+timeout
        def inventory():
            try:return group_members(self.process.pid)
            except (ProfileError,OSError,ValueError):
                self.receipt['process_inventory_verified']=False
                return None
        if self.browser is not None and self.process.poll() is None:
            try:
                session=await asyncio.wait_for(self.browser.new_browser_cdp_session(),max(.001,deadline-time.monotonic()))
                await asyncio.wait_for(session.send('Browser.close'),max(.001,deadline-time.monotonic()))
            except Exception:
                # Browser.close can tear down its own CDP reply. The actual OS
                # exit and group retirement below, never disconnection, decide.
                pass
        while time.monotonic()<deadline:
            code=self.process.poll()
            members=inventory()
            if code is not None and members=={}:break
            if members is None:break
            await asyncio.sleep(.025)
        if self.process.poll() is None or inventory()!={}:
            self.receipt['forced_cleanup']=True
            # This is the isolated process group created by our exact Popen;
            # forced retirement is safety cleanup and can never pass the gate.
            for sig in (signal.SIGTERM,signal.SIGKILL):
                try:os.killpg(self.process.pid,sig)
                except ProcessLookupError:pass
                until=time.monotonic()+.5
                while time.monotonic()<until and inventory()!={}:
                    self.process.poll();await asyncio.sleep(.025)
        try:self.process.wait(timeout=.5)
        except subprocess.TimeoutExpired:pass
        self.receipt['root_exit_code']=self.process.returncode
        self.receipt['natural_root_exit']=self.process.returncode==0 and not self.receipt['forced_cleanup']
        self.receipt['owned_group_empty']=inventory()=={}
        if self.reader:
            await asyncio.to_thread(self.reader.join,1)
            self.receipt['stderr_reader_joined']=not self.reader.is_alive()
        if self.receipt['stderr_reader_joined']:self.process.stderr.close()
        self.receipt['stderr_bounded']=self.stderr_bytes<=65536 and not self.stderr_failed
        if not all(self.receipt[k] for k in ('natural_root_exit','owned_group_empty','stderr_reader_joined','stderr_bounded','process_inventory_verified')):raise ProfileError('browser_retirement_unverified')
