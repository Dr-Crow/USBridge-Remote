#!/usr/bin/env python3
"""Real subprocess/RTSP/media acceptance for the experimental source streamer.

Requires an explicitly selected test X11 display. Does not pair a full Moonlight
client or prove native input, device audio, hardware encoding or remote transport.
Launch keys are random, stdin-only and never written to artifacts.
"""
import argparse
import base64
import ctypes
import ctypes.util
import json
import os
import re
from pathlib import Path
import selectors
import signal
import socket
import struct
import subprocess
import tempfile
import time

from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
from cryptography.hazmat.primitives.padding import PKCS7


def rtsp_packet(key, plaintext, sequence):
    nonce = struct.pack('<I', sequence) + bytes(6) + b'CR'
    sealed = AESGCM(key).encrypt(nonce, plaintext, None)
    return struct.pack('>II', 0x80000000 | len(plaintext), sequence) + sealed[-16:] + sealed[:-16]


def control_packet(key, message_type, payload, sequence):
    plain = struct.pack('<HH', message_type, len(payload)) + payload
    nonce = struct.pack('<I', sequence) + bytes(6) + b'CC'
    sealed = AESGCM(key).encrypt(nonce, plain, None)
    return struct.pack('<HHI', 1, len(plain) + 20, sequence) + sealed[-16:] + sealed[:-16]


def decode_reply(key, packet):
    if len(packet) < 24:
        raise AssertionError('short encrypted RTSP reply')
    size, sequence = struct.unpack('>II', packet[:8])
    if size != 0x80000000 | (len(packet) - 24):
        raise AssertionError('invalid encrypted reply length')
    nonce = struct.pack('<I', sequence) + bytes(6) + b'HR'
    return AESGCM(key).decrypt(nonce, packet[24:] + packet[8:24], None)


def transaction(address, packet):
    host, port = address.rsplit(':', 1)
    if host != '127.0.0.1':
        raise AssertionError('acceptance restricts RTSP to IPv4 loopback')
    with socket.create_connection((host, int(port)), timeout=3) as conn:
        conn.sendall(packet)
        chunks = []
        while True:
            block = conn.recv(65536)
            if not block:
                break
            chunks.append(block)
            if sum(map(len, chunks)) > 65536:
                raise AssertionError('oversized RTSP response')
        return b''.join(chunks)


def request(sequence, method, target, session, body=b''):
    headers = [f'{method} {target} RTSP/1.0', f'CSeq: {sequence}', f'Session: {session}']
    if body:
        headers += ['Content-Type: application/sdp', f'Content-Length: {len(body)}']
    return ('\r\n'.join(headers) + '\r\n\r\n').encode() + body


def read_ready(process, timeout=10):
    deadline = time.monotonic() + timeout
    line = bytearray()
    with selectors.DefaultSelector() as selector:
        selector.register(process.stdout, selectors.EVENT_READ)
        while not line.endswith(b'\n'):
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not selector.select(remaining):
                raise AssertionError('subprocess readiness timeout')
            block = os.read(process.stdout.fileno(), 1)
            if not block:
                raise AssertionError('subprocess closed before readiness')
            line += block
            if len(line) > 65536:
                raise AssertionError('oversized readiness event')
    if len(line) > 65536 or not line.endswith(b'\n'):
        raise AssertionError('invalid readiness framing')
    ready = json.loads(line)
    if ready.get('schema_version') != 1 or ready.get('event') != 'ready':
        raise AssertionError('unexpected readiness event')
    return ready


def receiver():
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 4 << 20)
    sock.bind(('127.0.0.1', 0))
    sock.setblocking(False)
    return sock


class OpusDecoder:
    def __init__(self):
        self.lib = ctypes.CDLL(ctypes.util.find_library('opus') or 'libopus.so.0')
        self.lib.opus_decoder_create.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.POINTER(ctypes.c_int)]
        self.lib.opus_decoder_create.restype = ctypes.c_void_p
        self.lib.opus_decode.argtypes = [ctypes.c_void_p, ctypes.c_void_p, ctypes.c_int,
                                         ctypes.POINTER(ctypes.c_int16), ctypes.c_int, ctypes.c_int]
        self.lib.opus_decode.restype = ctypes.c_int
        self.lib.opus_decoder_destroy.argtypes = [ctypes.c_void_p]
        error = ctypes.c_int()
        self.handle = self.lib.opus_decoder_create(48000, 2, ctypes.byref(error))
        if not self.handle or error.value:
            raise AssertionError('Opus decoder initialization failed')

    def decode(self, packet):
        pcm = (ctypes.c_int16 * 480)()
        data = ctypes.create_string_buffer(packet)
        samples = self.lib.opus_decode(self.handle, data, len(packet), pcm, 240, 0)
        if samples != 240:
            raise AssertionError(f'Opus packet did not decode to 5 ms: {samples}')
        return samples

    def close(self):
        if self.handle:
            self.lib.opus_decoder_destroy(self.handle)
            self.handle = None


def run(args):
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    key, key_id = os.urandom(16), 1000
    session = 'acceptance-' + os.urandom(8).hex()
    video, audio, decoder = receiver(), receiver(), OpusDecoder()
    config = dict(schema_version=1, owner='acceptance-operator', session_id=session,
        key_b64=base64.b64encode(key).decode(), key_id=key_id, peer_ip='127.0.0.1',
        video_port=video.getsockname()[1], audio_port=audio.getsockname()[1],
        display=args.display, capture_consent=True, ffmpeg=str(args.ffmpeg.resolve()),
        width=128, height=72, fps=30, pixel_format='yuv420p', packet_size=1024,
        audio_mode='silence', max_seconds=20)
    command = args.command or [str(args.streamer.resolve()), '--launch-stdin']
    started = time.monotonic()
    control_process = None
    control_records = None
    with tempfile.TemporaryFile() as stderr:
        process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                   stderr=stderr, start_new_session=True)
        try:
            process.stdin.write(json.dumps(config).encode() + b'\n')
            process.stdin.flush()
            ready = read_ready(process)
            if ready.get('session_id') != session:
                raise AssertionError('readiness session mismatch')
            address = ready['rtsp_address']
            first = rtsp_packet(key, request(1, 'OPTIONS', '*', session), 1)
            reply = decode_reply(key, transaction(address, first))
            if not reply.startswith(b'RTSP/1.0 200 '):
                raise AssertionError('OPTIONS failed')
            if transaction(address, first):
                raise AssertionError('replayed request accepted')
            bad = bytearray(rtsp_packet(key, request(2, 'DESCRIBE', '*', session), 2))
            bad[8] ^= 1
            if transaction(address, bytes(bad)):
                raise AssertionError('corrupted authentication tag accepted')
            body = (b'v=0\r\na=x-nv-video[0].clientViewportWd:128 \r\n'
                    b'a=x-nv-video[0].clientViewportHt:72 \r\na=x-nv-video[0].maxFPS:30 \r\n'
                    b'a=x-nv-video[0].packetSize:1024 \r\na=x-ss-general.encryptionEnabled:7 \r\n')
            flow = [('DESCRIBE', '*', b''), ('SETUP', 'streamid=audio/0/0', b''),
                    ('SETUP', 'streamid=video/0/0', b''), ('SETUP', 'streamid=control/13/0', b''),
                    ('ANNOUNCE', 'streamid=control/13/0', body), ('PLAY', '/', b'')]
            for sequence, (method, target, payload) in enumerate(flow, 3):
                reply = decode_reply(key, transaction(address, rtsp_packet(key,
                    request(sequence, method, target, session, payload), sequence)))
                if not reply.startswith(b'RTSP/1.0 200 '):
                    raise AssertionError(f'{method} failed: {reply[:100]!r}')
                if method == 'SETUP' and target.startswith(('streamid=audio', 'streamid=video')):
                    match = re.search(rb'server_port=(\d+)', reply)
                    if not match:
                        raise AssertionError('SETUP omitted server media port')
                    port = int(match.group(1))
                    if not 1 <= port <= 65535:
                        raise AssertionError('invalid server media port')
                    (audio if target.startswith('streamid=audio') else video).sendto(b'PING', ('127.0.0.1', port))
            if args.enet_helper:
                host, port = ready['control_address'].rsplit(':', 1)
                if host != '127.0.0.1':
                    raise AssertionError('control helper requires IPv4 loopback')
                control_records = tempfile.NamedTemporaryFile()
                for sequence, (kind, payload) in enumerate([(0x0302, b'\0\0'), (0x0307, b'\0')]):
                    packet = control_packet(key, kind, payload, sequence)
                    control_records.write(struct.pack('>BBI', 0, 1, len(packet)) + packet)
                control_records.flush()
                control_process = subprocess.Popen([str(args.enet_helper.resolve()), '127.0.0.1',
                    host, port, control_records.name, '10000'], stdout=subprocess.PIPE, stderr=stderr)
                # The helper acknowledges ENet delivery, not application authentication.
                # Subsequent media/lifecycle checks are still mandatory.
                with selectors.DefaultSelector() as selector:
                    selector.register(control_process.stdout, selectors.EVENT_READ)
                    observed = bytearray()
                    deadline = time.monotonic() + 5
                    while b'ACKED\n' not in observed:
                        left = deadline - time.monotonic()
                        if left <= 0 or not selector.select(left):
                            raise AssertionError('ENet connection/acknowledgment timeout')
                        block = os.read(control_process.stdout.fileno(), 1024)
                        if not block or len(observed) + len(block) > 4096:
                            raise AssertionError('ENet helper failed before acknowledgment')
                        observed += block
            counts = {'video': 0, 'audio': 0, 'audio_fec': 0}
            video_frames = set()
            seen_nonces = set()
            pending_frame = []
            audio_samples = 0
            with selectors.DefaultSelector() as selector, (output / 'video-plain.packets').open('wb') as records:
                selector.register(video, selectors.EVENT_READ, 'video')
                selector.register(audio, selectors.EVENT_READ, 'audio')
                deadline = time.monotonic() + 3
                while time.monotonic() < deadline:
                    for event, _ in selector.select(0.1):
                        wire, peer = event.fileobj.recvfrom(65536)
                        if peer[0] != '127.0.0.1':
                            raise AssertionError('non-loopback media peer')
                        kind = event.data
                        if kind == 'video':
                            nonce = wire[:12]
                            if nonce in seen_nonces:
                                raise AssertionError('video nonce repeated')
                            seen_nonces.add(nonce)
                            plain = AESGCM(key).decrypt(nonce, wire[32:] + wire[16:32], None)
                            if plain[:2] != b'\x90\x60':
                                raise AssertionError('invalid decrypted video RTP header')
                            frame = struct.unpack('<I', wire[12:16])[0]
                            pending_frame.append(plain)
                            if plain[24] & 2:
                                if frame != len(video_frames) + 1:
                                    raise AssertionError('video frame gap or reordering')
                                video_frames.add(frame)
                                for packet in pending_frame:
                                    records.write(struct.pack('>I', len(packet)) + packet)
                                pending_frame.clear()
                        else:
                            if len(wire) < 12:
                                raise AssertionError('short audio RTP packet')
                            if (wire[1] & 127) != 97:
                                counts['audio_fec'] += 1
                                continue
                            sequence = struct.unpack('>H', wire[2:4])[0]
                            iv = struct.pack('>I', key_id + sequence) + bytes(12)
                            decryptor = Cipher(algorithms.AES(key), modes.CBC(iv)).decryptor()
                            padded = decryptor.update(wire[12:]) + decryptor.finalize()
                            unpadder = PKCS7(128).unpadder()
                            opus = unpadder.update(padded) + unpadder.finalize()
                            if len(opus) < 2:
                                raise AssertionError('empty decoded Opus packet')
                            audio_samples += decoder.decode(opus)
                        counts[kind] += 1
            if counts['video'] < 30 or len(video_frames) < 15 or counts['audio'] < 100:
                raise AssertionError(f'insufficient live media: {counts}, frames={len(video_frames)}')
            teardown = rtsp_packet(key, request(9, 'TEARDOWN', '/', session), 9)
            response = decode_reply(key, transaction(address, teardown))
            if not response.startswith(b'RTSP/1.0 200 '):
                raise AssertionError('TEARDOWN failed')
            process.stdin.close()
            if process.wait(timeout=5) != 0:
                raise AssertionError('subprocess failed after teardown')
            stopped = process.stdout.read(65537)
            if len(stopped) > 65536:
                raise AssertionError('oversized lifecycle output')
            final = json.loads(stopped)
            if final.get('event') != 'stopped' or final.get('reason') != 'completed':
                raise AssertionError('missing successful stop event')
            public_decode = False
            if args.video_fixture:
                subprocess.run([str(args.video_fixture.resolve()), str(output / 'video-plain.packets'),
                    str(output / 'capture.h264'), str(len(video_frames)), '1024'], check=True,
                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15)
                subprocess.run([str(args.ffmpeg.resolve()), '-hide_banner', '-nostdin', '-v', 'error',
                    '-i', str(output / 'capture.h264'), '-f', 'null', '-'], check=True,
                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15)
                public_decode = True
            result = dict(passed=True, process_launch=True, encrypted_rtsp=True,
                replay_rejected=True, corrupt_tag_rejected=True, media_packets=counts,
                video_frames=len(video_frames), teardown_joined=True,
                full_public_client=False, enet_control_tested=bool(args.enet_helper),
                native_input_tested=False, audio_source='silence',
                opus_decoded_samples_per_channel=audio_samples,
                public_video_depacketizer_and_ffmpeg_passed=public_decode,
                elapsed_seconds=round(time.monotonic()-started, 3))
            (output / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
            print(json.dumps(result, sort_keys=True))
        finally:
            video.close()
            audio.close()
            decoder.close()
            if control_process is not None:
                if control_process.poll() is None:
                    control_process.terminate()
                control_process.wait(timeout=5)
                control_process.stdout.close()
            if control_records is not None:
                control_records.close()
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=5)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--streamer', type=Path)
    parser.add_argument('--display', required=True)
    parser.add_argument('--ffmpeg', type=Path, default=Path('/usr/bin/ffmpeg'))
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--video-fixture', type=Path, help='compiled pinned public-client fixture')
    parser.add_argument('--enet-helper', type=Path, help='compiled pinned ENet control test driver')
    parser.add_argument('--command', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if not args.streamer and not args.command:
        parser.error('--streamer or --command is required')
    run(args)


if __name__ == '__main__':
    main()
