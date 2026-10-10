#!/usr/bin/env python3
"""Real agent/source media on a newly isolated Xvfb, never the user's display."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import select
import subprocess
import sys
import tempfile

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--agent',type=Path,required=True)
parser.add_argument('--components',type=Path,required=True)
parser.add_argument('--enet-helper',type=Path,required=True)
parser.add_argument('--output',type=Path,required=True)
a=parser.parse_args()
pin=hashlib.sha256((a.components/'manifest.json').read_bytes()).hexdigest()
r,w=os.pipe()
server=subprocess.Popen(['Xvfb','-displayfd',str(w),'-screen','0','128x72x24','-nolisten','tcp','-noreset'],pass_fds=(w,),stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
os.close(w)
try:
    if not select.select([r],[],[],10)[0]:raise RuntimeError('isolated Xvfb startup failed')
    display=os.read(r,32).decode().strip()
    if not display.isdecimal():raise RuntimeError('isolated Xvfb display unavailable')
    with tempfile.TemporaryDirectory(prefix='source-agent-media-') as state:
        command=[sys.executable,str(Path(__file__).with_name('test_source_streamer.py')),'--display',':'+display,'--output',str(a.output.resolve()),'--enet-helper',str(a.enet_helper.resolve()),'--command',str(a.agent.resolve()),'--source-streamer-mode','--source-component-directory',str(a.components.resolve()),'--source-manifest-sha256',pin,'--source-state-dir',state]
        subprocess.run(command,check=True,timeout=40)
finally:
    os.close(r)
    server.terminate()
    try:server.wait(timeout=5)
    except subprocess.TimeoutExpired:server.kill();server.wait()
