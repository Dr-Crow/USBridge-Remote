#!/usr/bin/env python3
"""Full public-client RTSP/ENet/media acceptance; requires isolated X11 display."""
import argparse,base64,json,os,struct,subprocess,tempfile,sys,time,selectors,hashlib,shutil
from pathlib import Path
from test_source_streamer import read_ready

def read_observer(process, timeout=5):
    deadline=time.monotonic()+timeout
    data=bytearray()
    with selectors.DefaultSelector() as selector:
        selector.register(process.stdout,selectors.EVENT_READ)
        while not data.endswith(b'\n'):
            remaining=deadline-time.monotonic()
            if remaining<=0 or not selector.select(remaining):raise AssertionError('input observer timeout')
            byte=os.read(process.stdout.fileno(),1)
            if not byte:raise AssertionError('input observer exited early')
            data+=byte
            if len(data)>65536:raise AssertionError('input observer result too large')
    return json.loads(data)

def run(a):
    a.output.mkdir(parents=True,exist_ok=True)
    key=os.urandom(16); kid=1000
    config=dict(schema_version=1,owner='acceptance-operator',session_id='full-public-client',key_b64=base64.b64encode(key).decode(),key_id=kid,peer_ip='127.0.0.1',video_port=0,audio_port=0,display=a.display,capture_consent=True,ffmpeg=str(a.ffmpeg.resolve()),width=128,height=72,fps=30,pixel_format=a.pixel_format,packet_size=1024,audio_mode='silence',max_seconds=45)
    observer=None
    with tempfile.TemporaryDirectory(prefix='full-client-agent-') as directory, tempfile.TemporaryFile() as errors:
        command=[str(a.streamer.resolve()),'--launch-stdin']
        if a.agent:
            root=Path(directory); components=root/'components';components.mkdir()
            child=components/'source-streamer';shutil.copyfile(a.streamer,child);child.chmod(0o700)
            data=child.read_bytes()
            manifest={'schema':1,'components':[{'name':'source-streamer','platform':'linux/amd64','version':'acceptance','profile':'source-streamer-v1','entry':child.name,'files':[{'path':child.name,'sha256':hashlib.sha256(data).hexdigest(),'size':len(data),'executable':True}]}]}
            raw=json.dumps(manifest).encode();(components/'manifest.json').write_bytes(raw)
            command=[str(a.agent.resolve()),'--source-streamer-mode','--source-component-directory',str(components),'--source-manifest-sha256',hashlib.sha256(raw).hexdigest(),'--source-state-dir',str(root/'state')]
        p=subprocess.Popen(command,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=errors)
        try:
            if a.input_consent:
                config['input_consent']=True
                probe=Path(__file__).with_name('x11_probe.py')
                observer=subprocess.Popen([sys.executable,str(probe),a.display],stdin=subprocess.PIPE,stdout=subprocess.PIPE)
                if read_observer(observer).get('ready') is not True:raise AssertionError('isolated input observer failed')
            p.stdin.write(json.dumps(config).encode()+b'\n');p.stdin.flush()
            ready=read_ready(p)
            command=[str(a.client.resolve()),'rtspenc://'+ready['rtsp_address'],str(a.output/'video.h264')]
            if a.pixel_format=='yuv444p':command.append('444')
            if a.input_consent:
                if 'input-x11-keyboard-mouse' not in ready['capabilities']:raise AssertionError('input consent was not acknowledged')
                command.append('input')
            r=subprocess.run(command,input=key+struct.pack('!I',kid),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=40)
            (a.output/'client-stages.log').write_bytes(r.stderr[:65536].replace(base64.b64encode(key),b'[redacted]').replace(key.hex().encode(),b'[redacted]'))
            result=json.loads(r.stdout)
            (a.output/'result.json').write_text(json.dumps(result,indent=2)+'\n')
            if r.returncode:raise AssertionError(f'public client failed: {result}; see client-stages.log')
            p.stdin.close()
            if p.wait(timeout=5) != 0:raise AssertionError('source runtime failed after public client shutdown')
            terminal_raw=p.stdout.read(65537)
            (a.output/'source-final-stdout.log').write_bytes(terminal_raw)
            terminal=json.loads(terminal_raw)
            if terminal.get('event')!='stopped' or terminal.get('reason')!='completed':raise AssertionError('source runtime did not join cleanly')
            result['source_shutdown_joined']=True
            if observer:
                observer.stdin.write(b'snapshot\n');observer.stdin.flush()
                snapshot=read_observer(observer)
                (a.output/'input-observation.json').write_text(json.dumps(snapshot,indent=2)+'\n')
                events=snapshot['events']; code=snapshot['a_code']
                if not any(e[0]==2 and e[1]==code for e in events) or not any(e[0]==3 and e[1]==code for e in events):raise AssertionError('public-client keyboard did not reach X11 observer')
                if not any(e[0]==4 and e[1]==1 for e in events) or not any(e[0]==5 and e[1]==1 for e in events):raise AssertionError('public-client button did not reach X11 observer')
                if snapshot['a_held'] or snapshot['ctrl_held'] or snapshot['mask'] & (0x1f << 8):raise AssertionError('disconnect left input held')
                result['actual_x11_input_and_disconnect_release_passed']=True
            decode=subprocess.run([str(a.ffmpeg.resolve()),'-v','error','-i',str(a.output/'video.h264'),'-f','null','-'],capture_output=True,timeout=15)
            (a.output/'video-decode.log').write_bytes(decode.stderr)
            if decode.returncode:raise AssertionError('received H264 did not decode')
            result['h264_decode_passed']=True
            probe=subprocess.run(['/usr/bin/ffprobe','-v','error','-select_streams','v:0','-show_entries','stream=pix_fmt,profile','-of','json',str(a.output/'video.h264')],capture_output=True,check=True,timeout=10)
            actual_format=json.loads(probe.stdout)['streams'][0]
            if actual_format['pix_fmt']!=a.pixel_format:raise AssertionError('decoded chroma did not match negotiated format')
            result['decoded_video_format']=actual_format
            result['agent_supervised']=bool(a.agent)
            result['streamer_sha256']=hashlib.sha256(a.streamer.read_bytes()).hexdigest()
            if a.agent:result['agent_sha256']=hashlib.sha256(a.agent.read_bytes()).hexdigest()
            (a.output/'result.json').write_text(json.dumps(result,indent=2)+'\n')
            print(json.dumps(result))
        finally:
            if p.stdin and not p.stdin.closed:
                try:p.stdin.close()
                except (BrokenPipeError,OSError):pass
            try:p.wait(timeout=5)
            except subprocess.TimeoutExpired:
                p.kill();p.wait(timeout=5)
            remaining=p.stdout.read(65537)
            if remaining:(a.output/'source-final-stdout.log').write_bytes(remaining)
            (a.output/'source-exit.json').write_text(json.dumps({'exit_code':p.returncode})+'\n')
            errors.seek(0)
            # Runtime logs must not include private launch JSON/session keys.
            raw=errors.read(65536).replace(base64.b64encode(key),b'[redacted]').replace(key.hex().encode(),b'[redacted]')
            (a.output/'source-stderr.log').write_bytes(raw)
            if observer:
                try:observer.stdin.close()
                except (BrokenPipeError,OSError):pass
                try:observer.wait(timeout=3)
                except subprocess.TimeoutExpired:observer.kill();observer.wait(timeout=3)
if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--pixel-format',choices=['yuv420p','yuv444p'],default='yuv420p')
    p.add_argument('--agent',type=Path)
    p.add_argument('--input-consent',action='store_true',help='inject only into the explicitly selected isolated test display');p.add_argument('--streamer',type=Path,required=True);p.add_argument('--client',type=Path,required=True)
    p.add_argument('--display',required=True);p.add_argument('--ffmpeg',type=Path,default=Path('/usr/bin/ffmpeg'));p.add_argument('--output',type=Path,required=True)
    run(p.parse_args())
