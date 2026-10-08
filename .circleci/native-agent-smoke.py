"""Windows shipped-EXE startup probe. Not a GUI, media, or WAN-isolation test."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request


def run(bundle, report_path):
    if os.name != "nt":
        raise RuntimeError("native Windows executor required")
    original = bundle / "USBridgeAgent.exe"
    digest = hashlib.sha256(original.read_bytes()).hexdigest()
    report = {"exe_sha256": digest, "headless_startup": False,
              "gui_tested": False, "media_tested": False,
              "usb_tested": False, "wan_isolation_tested": False,
              "local_runtime_preparation": "covered by separate bundle fixture"}
    start = time.monotonic()
    try:
        with tempfile.TemporaryDirectory(prefix="usbridge-native-smoke-") as tmp:
            root = Path(tmp)
            runtime = root / "runtime"
            shutil.copytree(bundle, runtime)
            profile = root / "profile"
            appdata = profile / "AppData" / "Roaming"
            config_dir = appdata / "usbridge-agent"
            config_dir.mkdir(parents=True)
            state = root / "state"
            state.mkdir()
            with socket.socket() as reserve:
                reserve.bind(("127.0.0.1", 0))
                port = reserve.getsockname()[1]
            # Explicit backend/consent keeps this process probe away from real
            # capture, driver installation, and vendor downloads. Component
            # discovery/preparation has a separate live-fixture test in this job.
            cfg = {"state_dir": str(state), "listen_host": "127.0.0.1",
                   "http_port": port, "tls_enabled": False,
                   "master_key": "ci-disposable-native-smoke",
                   "runtime_local": True, "strict_lan": True,
                   "local_runtime_enabled": False,
                   "preferred_backend": "sunshine", "streamer_consent": False,
                   "usb_broker_consent": False, "tailscale_enabled": False,
                   "streamer_auto_update": False}
            # JSON is a YAML subset. This disposable config is never packaged.
            (config_dir / "config.yaml").write_text(json.dumps(cfg), encoding="utf-8")
            env = dict(os.environ, APPDATA=str(appdata),
                       LOCALAPPDATA=str(profile / "AppData" / "Local"),
                       USERPROFILE=str(profile), USBRIDGE_STRICT_LAN="1")
            env.pop("USBRIDGE_LOCAL_RUNTIME", None)
            proc = subprocess.Popen([str(runtime / "USBridgeAgent.exe"), "--headless"],
                                    cwd=runtime, env=env, stdout=subprocess.DEVNULL,
                                    stderr=subprocess.DEVNULL)
            # Avoid inherited proxy settings for the loopback health request.
            client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            try:
                deadline = time.monotonic() + 60
                while time.monotonic() < deadline:
                    if proc.poll() is not None:
                        raise RuntimeError(f"native agent exited early: {proc.returncode}")
                    try:
                        with client.open(f"http://127.0.0.1:{port}/api/healthz", timeout=2) as response:
                            body = json.load(response)
                            if response.status == 200 and body.get("data", {}).get("status") == "ok":
                                report["headless_startup"] = True
                                break
                    except (OSError, ValueError):
                        pass
                    time.sleep(0.5)
                if not report["headless_startup"]:
                    raise RuntimeError("native agent did not answer loopback health within 60 seconds")
            finally:
                if proc.poll() is None:
                    subprocess.run(["taskkill", "/PID", str(proc.pid), "/T", "/F"],
                                   check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                proc.wait(timeout=15)
            assert hashlib.sha256(original.read_bytes()).hexdigest() == digest
    except Exception as error:
        report["error"] = str(error)
        raise
    finally:
        report["seconds"] = round(time.monotonic() - start, 2)
        report_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    run(Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve())
