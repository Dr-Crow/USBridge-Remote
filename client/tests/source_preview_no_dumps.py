#!/usr/bin/env python3
"""Native fixture: actual viewer must decode while ignoring inherited dump flags.

Only run against an explicitly approved synthetic Xvfb source. A test supervisor
must start that source with receiver ports zero, fresh keys, and the default
128x72/30fps profile, then pipe its one-line source-preview-v1 descriptor to this
script's stdin. Credentials are forwarded only through the viewer's private
stdin pipe, never arguments/files/output. The supervisor still owns source stop
and join in its finally block. DISPLAY/XAUTHORITY select the synthetic display.

Usage: source_preview_no_dumps.py /absolute/path/to/source-preview-viewer

A pass proves frame submission and no inherited diagnostic file capture. It
cannot prove pixels were presented: inspect the actual window separately.
"""
import json
import os
from pathlib import Path
import selectors
import socket
import threading
import subprocess
import sys
import tempfile
import time


def fail():
    # Never stringify exceptions: JSON parse errors and subprocess details can
    # include input data. The parent owns useful, sanitized failure reporting.
    print(json.dumps({"passed": False, "check": "viewer_inherited_diagnostics"}))
    return 1


def main():
    if len(sys.argv) != 2 or not Path(sys.argv[1]).is_absolute():
        return fail()
    line = sys.stdin.buffer.readline(4097)
    if len(line) > 4096 or not line.endswith(b"\n"):
        return fail()
    try:
        descriptor = json.loads(line)
        session_id = descriptor["session_id"]
        if descriptor["width"] != 128 or descriptor["height"] != 72:
            return fail()
    except (ValueError, KeyError, TypeError):
        return fail()

    child = None
    with tempfile.TemporaryDirectory(prefix="source-preview-no-dumps-") as root:
        root = Path(root)
        dump_dir = root / "frames"
        dump_dir.mkdir()
        wave_dump = root / "pyrowave.pgm"
        pulse_trap = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        pulse_trap.bind(("127.0.0.1", 0))
        pulse_trap.listen(4)
        pulse_trap.settimeout(0.1)
        pulse_attempts = []
        pulse_stop = threading.Event()
        def watch_pulse():
            while not pulse_stop.is_set():
                try:
                    connection, _ = pulse_trap.accept()
                    pulse_attempts.append(True)
                    connection.close()  # Never receive or retain audio bytes.
                except socket.timeout:
                    pass
                except OSError:
                    return
        pulse_thread = threading.Thread(target=watch_pulse, daemon=True)
        pulse_thread.start()
        def finish_pulse_trap():
            pulse_stop.set()
            pulse_thread.join(timeout=1)
            if pulse_thread.is_alive():
                raise ValueError("audio trap did not stop")
            if pulse_trap.fileno() >= 0:
                pulse_trap.setblocking(False)
                try:
                    while True:
                        connection, _ = pulse_trap.accept()
                        pulse_attempts.append(True)
                        connection.close()
                except BlockingIOError:
                    pass
                finally:
                    pulse_trap.close()
        env = os.environ.copy()
        env.update({
            "PULSE_SERVER": "tcp:127.0.0.1:%d" % pulse_trap.getsockname()[1],
            "USBRIDGE_FRAME_DUMP_DIR": str(dump_dir),
            "USBRIDGE_FRAME_DUMP_EVERY_N": "1",
            "USBRIDGE_PYROWAVE_DUMP": str(wave_dump),
            "USBRIDGE_SKIP_DECODE": "1",
            "USBRIDGE_HWDEC": "diagnostic-value-must-be-ignored",
            "USBRIDGE_LOG_FRAME_JITTER": "1",
            "USBRIDGE_LOG_RTP_STATS": "1",
            "USBRIDGE_FUTURE_DIAGNOSTIC": "1",
        })
        ready = first_frame = stopped = False
        try:
            child = subprocess.Popen(
                [sys.argv[1], "--source-preview-stdin"],
                stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL, env=env, bufsize=0,
            )
            child.stdin.write(line)
            child.stdin.flush()
            # Drop Python references promptly; this does not claim reliable
            # whole-process memory zeroization.
            line = None
            descriptor = None
            selector = selectors.DefaultSelector()
            selector.register(child.stdout, selectors.EVENT_READ)
            data = bytearray()
            deadline = time.monotonic() + 12
            stop_at = None
            stop_deadline = None
            while time.monotonic() < deadline:
                now = time.monotonic()
                if stop_at is not None and now >= stop_at and not child.stdin.closed:
                    child.stdin.close()  # private lease EOF exercises real teardown
                    stop_deadline = now + 4
                if stop_deadline is not None and now >= stop_deadline:
                    raise ValueError("bounded cleanup failed")
                events = selector.select(0.1)
                if not events:
                    if child.poll() is not None:
                        break
                    continue
                chunk = os.read(child.stdout.fileno(), 4096)
                if not chunk:
                    break
                data.extend(chunk)
                if len(data) > 65536:
                    raise ValueError("bounded output failed")
                while b"\n" in data:
                    raw, _, rest = data.partition(b"\n")
                    data = bytearray(rest)
                    event = json.loads(raw)
                    if stopped or event.get("schema_version") != 1 or event.get("session_id") != session_id:
                        raise ValueError("invalid lifecycle")
                    kind = event.get("event")
                    if kind == "ready" and not ready and set(event) == {"schema_version", "event", "session_id"}:
                        ready = True
                    elif kind == "first_frame" and ready and not first_frame and set(event) == {"schema_version", "event", "session_id"}:
                        first_frame = True
                        # Multiple real frames must traverse the decoder. With
                        # FRAME_DUMP_EVERY_N=1, even one would reveal a leak.
                        stop_at = time.monotonic() + 1
                    elif kind == "stopped" and ready and set(event) == {"schema_version", "event", "session_id", "reason"} and event["reason"] == "completed":
                        stopped = True
                    else:
                        raise ValueError("invalid event")
            selector.close()
            if not child.stdin.closed:
                child.stdin.close()
            result = child.wait(timeout=4)
            if result != 0 or data or not (ready and first_frame and stopped):
                raise ValueError("incomplete lifecycle")
            finish_pulse_trap()
            if pulse_attempts:
                raise ValueError("silent preview opened an audio output backend")
            if list(dump_dir.iterdir()) or wave_dump.exists() or set(root.iterdir()) != {dump_dir}:
                raise ValueError("diagnostic capture leaked")
            print(json.dumps({"passed": True, "check": "viewer_inherited_diagnostics", "ready": True,
                              "first_frame_submitted": True, "diagnostic_files": 0, "audio_output_connections": 0,
                              "presented_pixels_verified": False}))
            return 0
        except Exception:
            return fail()
        finally:
            finish_pulse_trap()
            if child is not None:
                if child.stdin is not None and not child.stdin.closed:
                    child.stdin.close()
                if child.poll() is None:
                    child.kill()
                child.wait(timeout=4)


if __name__ == "__main__":
    sys.exit(main())
