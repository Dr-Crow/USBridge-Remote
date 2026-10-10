#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""CI-only end-to-end fixture check. Requires the generated adjacent assets.

Runs the actual fixture with both frozen argv vectors concurrently for its
complete bounded lease, then strictly decodes the exact output with explicitly
selected trusted FFmpeg. No capture input or network connection is created.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
import platform
from pathlib import Path
import subprocess
import sys
import threading
import time

from build_assets import decode, ogg_pages, NAMES, PCM_BYTES

VIDEO_ARGS = ["-hide_banner", "-nostdin", "-v", "error", "-f", "gdigrab", "-draw_mouse", "0", "-offset_x", "0", "-offset_y", "0", "-video_size", "128x72", "-framerate", "30", "-i", "desktop", "-an", "-pix_fmt", "yuv420p", "-c:v", "libx264", "-qp", "0", "-preset", "ultrafast", "-tune", "zerolatency", "-x264-params", "aud=1:repeat-headers=1:keyint=1:min-keyint=1:scenecut=0", "-threads", "1", "-f", "h264", "pipe:1"]
AUDIO_ARGS = ["-hide_banner", "-nostdin", "-v", "error", "-f", "s16le", "-ar", "48000", "-ac", "2", "-i", "pipe:0", "-vn", "-c:a", "libopus", "-b:a", "128k", "-application", "lowdelay", "-frame_duration", "5", "-vbr", "off", "-mapping_family", "0", "-f", "ogg", "-page_duration", "5000", "-flush_packets", "1", "pipe:1"]


def read_exact(stream, size):
    chunks = []
    remaining = size
    while remaining:
        chunk = stream.read(remaining)
        if not chunk:
            raise RuntimeError("fixture ended before its complete stream")
        chunks.append(chunk)
        remaining -= len(chunk)
    return b"".join(chunks)


def exercise(exe, audio, assets):
    expected = ogg_pages(assets[NAMES[2]]) if audio else [assets[NAMES[(i // 30) % 2]] for i in range(900)]
    env = {key: value for key, value in os.environ.items()
           if key.upper() in {"SYSTEMROOT", "WINDIR", "PATH", "TEMP", "TMP"}}
    # These intentionally irrelevant inherited settings must never select a
    # runtime program/path, forwarding mode, or report file in the fixture.
    env.update({"FFREPORT": "file=forbidden-report.txt", "FFMPEG_BINARY": "forbidden.exe"})
    proc = subprocess.Popen([str(exe), *(AUDIO_ARGS if audio else VIDEO_ARGS)],
                            stdin=subprocess.PIPE if audio else subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env)
    timer = threading.Timer(35, proc.kill)
    timer.start()
    feeder = None
    if audio:
        def feed():
            try:
                chunk = bytes(960)
                for _ in range(PCM_BYTES // len(chunk)):
                    proc.stdin.write(chunk)
                proc.stdin.close()
            except (BrokenPipeError, OSError):
                pass
        feeder = threading.Thread(target=feed, daemon=True)
        feeder.start()
    try:
        times, chunks = [], []
        for block in expected:
            actual = read_exact(proc.stdout, len(block))
            if actual != block:
                raise RuntimeError("runtime bytes differ from pinned synthetic assets")
            times.append(time.monotonic())
            chunks.append(actual)
        if proc.stdout.read(1):
            raise RuntimeError("fixture emitted beyond its bounded stream")
        returncode = proc.wait(timeout=2)
        stderr = proc.stderr.read(1024)
        if returncode or stderr:
            raise RuntimeError("fixture returned an error or leaked a diagnostic")
        media_times = times[2:] if audio else times
        expected_span = 29.995 if audio else 899 / 30
        span = media_times[-1] - media_times[0]
        if span < expected_span - 0.15 or span > 30.5:
            raise RuntimeError("fixture runtime output was unpaced or exceeded its lease")
        # A second boundary is useful for detecting an initial unpaced burst,
        # without making loaded CI fail on scheduler/pipe read jitter.
        boundary = 200 if audio else 30
        if media_times[boundary] - media_times[0] < 0.85:
            raise RuntimeError("fixture emitted an initial media burst")
        return b"".join(chunks), {"blocks": len(expected), "elapsed_seconds": round(span, 4)}
    finally:
        timer.cancel()
        if proc.poll() is None:
            proc.kill()
        proc.wait()
        if feeder:
            feeder.join(timeout=2)
        proc.stdout.close()
        proc.stderr.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ffmpeg", required=True, type=Path)
    parser.add_argument("--fixture", required=True, type=Path)
    args = parser.parse_args()
    for path in (args.ffmpeg, args.fixture):
        if not path.is_absolute() or path.is_symlink() or not path.is_file():
            parser.error("executables must be absolute regular non-symlink files")
    directory = args.fixture.parent
    manifest = json.loads((directory / "fixture-assets.json").read_text(encoding="utf-8"))
    assets = {name: (directory / name).read_bytes() for name in NAMES}
    for name, blob in assets.items():
        if hashlib.sha256(blob).hexdigest() != manifest["assets"][name]["sha256"]:
            raise RuntimeError("asset manifest digest mismatch")
    # Unsupported invocations must fail without a single media or secret byte.
    invalid = subprocess.run([str(args.fixture), "-version"], stdout=subprocess.PIPE,
                             stderr=subprocess.PIPE, timeout=2, check=False)
    if invalid.returncode != 2 or invalid.stdout or invalid.stderr != b"synthetic fixture failed\n":
        raise RuntimeError("fixture did not reject an unsupported argument vector")
    with ThreadPoolExecutor(max_workers=2) as pool:
        video_job = pool.submit(exercise, args.fixture, False, assets)
        audio_job = pool.submit(exercise, args.fixture, True, assets)
        video, video_result = video_job.result()
        audio, audio_result = audio_job.result()
    frames = decode(args.ffmpeg, video, "h264", ["-pix_fmt", "yuv420p", "-f", "framemd5"])
    rows = [row.split(b",") for row in frames.splitlines() if row and not row.startswith(b"#")]
    expected_hashes = manifest["video"]["decoded_yuv420p_md5"]
    if len(rows) != 900 or any(int(row[4]) != 128 * 72 * 3 // 2 or
       row[5].strip().decode() != expected_hashes[(i // 30) % 2] for i, row in enumerate(rows)):
        raise RuntimeError("actual runtime video strict decode failed")
    pcm = decode(args.ffmpeg, audio, "ogg", ["-ac", "2", "-ar", "48000", "-f", "s16le"])
    if len(pcm) != PCM_BYTES or any(pcm):
        raise RuntimeError("actual runtime audio strict decode failed")
    if (Path.cwd() / "forbidden-report.txt").exists():
        raise RuntimeError("fixture honored an inherited report setting")
    print(json.dumps({"schema_version": 1, "role": "synthetic-substitution-only",
        "desktop_capture_tested": False, "fixture_runtime_exercised": True,
        "runtime_platform": sys.platform, "runtime_machine": platform.machine(),
        "native_windows_fixture_exercised": sys.platform == "win32",
        "fixture_sha256": hashlib.sha256(args.fixture.read_bytes()).hexdigest(),
        "video": {**video_result, "strict_decode": True, "frames": 900},
        "audio": {**audio_result, "strict_decode": True, "pcm_bytes": len(pcm)}}))


if __name__ == "__main__":
    main()
