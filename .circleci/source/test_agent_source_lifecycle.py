#!/usr/bin/env python3
"""Actual agent/source-binary lifecycle; deliberately does not start capture."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

from test_source_streamer import read_ready, rtsp_packet, request, transaction, decode_reply


def run(agent, streamer, output):
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='agent-source-acceptance-') as directory:
        root = Path(directory)
        components = root / 'components'
        components.mkdir()
        child = components / 'source-streamer'
        shutil.copyfile(streamer, child)
        child.chmod(0o700)
        payload = child.read_bytes()
        manifest = {'schema': 1, 'components': [{'name': 'source-streamer', 'platform': 'linux/amd64',
            'version': 'local-acceptance', 'profile': 'source-streamer-v1', 'entry': child.name,
            'files': [{'path': child.name, 'sha256': hashlib.sha256(payload).hexdigest(),
                       'size': len(payload), 'executable': True}]}]}
        raw = json.dumps(manifest).encode()
        (components / 'manifest.json').write_bytes(raw)
        pin = hashlib.sha256(raw).hexdigest()
        command = [str(agent.resolve()), '--source-streamer-mode', '--source-component-directory',
            str(components), '--source-manifest-sha256', pin, '--source-state-dir', str(root / 'state')]
        key = os.urandom(16)
        config = dict(schema_version=1, owner='acceptance-operator', session_id='real-lifecycle',
            key_b64=base64.b64encode(key).decode(), key_id=1000, peer_ip='127.0.0.1',
            video_port=51001, audio_port=51002, display=':99', capture_consent=True,
            ffmpeg='/usr/bin/ffmpeg', width=128, height=72, fps=30, pixel_format='yuv420p',
            packet_size=1024, audio_mode='silence', max_seconds=20)
        with tempfile.TemporaryFile() as stderr:
            process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=stderr)
            try:
                process.stdin.write(json.dumps(config).encode() + b'\n')
                process.stdin.flush()
                ready = read_ready(process)
                address = ready['rtsp_address']
                if ready.get('session_id') != config['session_id']:
                    raise AssertionError('session identity mismatch')
                for seq, method in [(1, 'OPTIONS'), (2, 'TEARDOWN')]:
                    packet = rtsp_packet(key, request(seq, method, '*', config['session_id']), seq)
                    reply = decode_reply(key, transaction(address, packet))
                    if not reply.startswith(b'RTSP/1.0 200 '):
                        raise AssertionError(f'{method} rejected')
                process.stdin.close()
                if process.wait(timeout=5) != 0:
                    raise AssertionError('agent exited with failure')
                tail = process.stdout.read(65537)
                if len(tail) > 65536 or json.loads(tail).get('event') != 'stopped':
                    raise AssertionError('agent did not emit bounded stop status')
                result = {'passed': True, 'real_agent_subprocess': True, 'real_streamer_subprocess': True,
                    'manifest_sha256': pin, 'streamer_sha256': hashlib.sha256(payload).hexdigest(),
                    'agent_sha256': hashlib.sha256(agent.read_bytes()).hexdigest(),
                    'encrypted_options': True, 'encrypted_teardown': True, 'capture_started': False,
                    'media_tested': False, 'hardware_tested': False}
                (output / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
                print(json.dumps(result, sort_keys=True))
            finally:
                if process.poll() is None:
                    process.terminate()
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait(timeout=5)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--agent', type=Path, required=True)
    parser.add_argument('--streamer', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    run(args.agent, args.streamer, args.output)
