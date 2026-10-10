#!/usr/bin/env python3
"""Full public-client acceptance supervised by packaged agent on isolated Xvfb."""
import argparse,os,select,subprocess,sys
from pathlib import Path
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--agent',type=Path,required=True);p.add_argument('--streamer',type=Path,required=True);p.add_argument('--client',type=Path,required=True);p.add_argument('--output',type=Path,required=True)
p.add_argument('--input-consent',action='store_true')
p.add_argument('--pixel-format',choices=['yuv420p','yuv444p'],default='yuv420p')
a=p.parse_args()
r,w=os.pipe()
server=subprocess.Popen(['Xvfb','-displayfd',str(w),'-screen','0','128x72x24','-nolisten','tcp','-noreset'],pass_fds=(w,),stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
os.close(w)
try:
    if not select.select([r],[],[],10)[0]:raise RuntimeError('isolated Xvfb startup failed')
    display=os.read(r,32).decode().strip()
    if not display.isdecimal():raise RuntimeError('isolated Xvfb display unavailable')
    subprocess.run([sys.executable,str(Path(__file__).with_name('test_full_client.py')),'--display',':'+display,'--pixel-format',a.pixel_format,'--agent',str(a.agent.resolve()),'--streamer',str(a.streamer.resolve()),'--client',str(a.client.resolve()),'--output',str(a.output.resolve())]+(['--input-consent'] if a.input_consent else []),check=True,timeout=60)
finally:
    os.close(r);server.terminate()
    try:server.wait(timeout=5)
    except subprocess.TimeoutExpired:server.kill();server.wait()
