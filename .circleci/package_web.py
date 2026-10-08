#!/usr/bin/env python3
"""Fail closed on incomplete static web assets; package only declared inputs."""

import hashlib
import json
import re
import shutil
import subprocess
import sys
import zipfile
from datetime import datetime, timezone
from pathlib import Path

MODELS = ("icon_detect", "dbnet", "svtr")
ORT = (
    "ort.webgpu.min.mjs",
    "ort-wasm-simd-threaded.mjs",
    "ort-wasm-simd-threaded.wasm",
    "ort-wasm-simd-threaded.jsep.mjs",
    "ort-wasm-simd-threaded.jsep.wasm",
    "ort-wasm-simd-threaded.asyncify.mjs",
    "ort-wasm-simd-threaded.asyncify.wasm",
)
WASM_HEADER = b"\x00asm\x01\x00\x00\x00"


def digest(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def validate(root, goroot):
    web = root / "client/web"
    names = ["index.html", "gui.html", "app.wasm", "wasm_exec.js", "ai_vision.js"]
    names += ["vendor/ort/" + name for name in ORT]
    names += ["models/" + name + ".onnx" for name in MODELS]
    names += ["vendor/ort/README.md"]
    for name in names:
        p = web / name
        if p.is_symlink() or not p.is_file() or p.stat().st_size == 0:
            raise ValueError("Missing, empty or symlinked required web asset: " + name)
        if name.endswith(".wasm"):
            with p.open("rb") as f:
                if f.read(8) != WASM_HEADER:
                    raise ValueError("Invalid WebAssembly header: " + name)
    shim = goroot / "lib/wasm/wasm_exec.js"
    if digest(web / "wasm_exec.js") != digest(shim):
        raise ValueError("wasm_exec.js does not match the build toolchain")
    if (web / "gui.html").read_bytes() != (web / "index.html").read_bytes():
        raise ValueError("gui.html differs from the built entry point")
    source = (root / "client/internal/localui/download.go").read_text()
    pins = {
        name: (sha, int(size))
        for name, sha, size in re.findall(
            r'Filename:\s*"([^"]+)",\s*SHA256:\s*"([a-f0-9]{64})",\s*Size:\s*(\d+)',
            source,
        )
    }
    for name in MODELS:
        p = web / ("models/" + name + ".onnx")
        if p.name not in pins or (digest(p), p.stat().st_size) != pins[p.name]:
            raise ValueError("Model differs from the source's pinned hash/size: " + p.name)
    return {name: web / name for name in names}


def command(*args, cwd=None):
    return subprocess.check_output(args, cwd=cwd, text=True).strip()


def package(root, output):
    goroot = Path(command("go", "env", "GOROOT", cwd=root / "client"))
    assets = validate(root, goroot)
    asset_count = len(assets)
    revision = command("git", "rev-parse", "HEAD", cwd=root)
    modified = bool(command("git", "status", "--porcelain", "--untracked-files=no", cwd=root))
    version = (root / "client/VERSION").read_text().strip()
    epoch = int(command("git", "show", "-s", "--format=%ct", "HEAD", cwd=root))
    output.mkdir(parents=True, exist_ok=True)
    metadata = output / "build-provenance.json"
    metadata.write_text(json.dumps({
        "source_repository": "https://github.com/Dr-Crow/USBridge-Remote",
        "source_commit": revision,
        "tracked_source_modified": modified,
        "client_version": version,
        "go": command("go", "version"),
        "node_test_runner": command("node", "--version"),
        "target": "js/wasm",
        "cgo_enabled": False,
        "entry_point": "client/cmd/wasm",
        "build_script": "client/scripts/build_web.sh",
        "wasm_exec_sha256": digest(assets["wasm_exec.js"]),
        "network_policy": "Existing public STUN and hosted relay fallback remain; not LAN-only.",
        "validation": "Source build, Node mock tests, vet and asset verification; no browser/agent streaming acceptance.",
        "assets": {name: {"bytes": p.stat().st_size, "sha256": digest(p)}
                   for name, p in sorted(assets.items())},
    }, indent=2) + "\n")
    readme = output / "README.md"
    shutil.copyfile(root / "docs/WEB_CLIENT_BUILDS.md", readme)
    assets = {"web/" + name: p for name, p in assets.items()}
    assets.update({
        "README.md": readme,
        "build-provenance.json": metadata,
        "LICENSE": root / "client/LICENSE",
        "GO-LICENSE": goroot / "LICENSE",
        "MODELS-PROVENANCE.md": root / "client/internal/localui/models/README.md",
    })
    # Preserve dependency/test reports without capturing environment or credentials.
    for name in ("modules.txt", "module-verification.txt",
                 "tests-native.txt", "tests-wasm.txt", "vet-wasm.txt",
                 "tests-packaging.txt", "wasm-compile.txt", "go-toolchain-sha256.txt", "node-toolchain-sha256.txt"):
        p = output / name
        if p.is_file():
            assets["build/" + name] = p
    manifest = output / "BUNDLE-SHA256SUMS.txt"
    manifest.write_text("".join(digest(p) + "  " + name + "\n" for name, p in sorted(assets.items())))
    assets["SHA256SUMS.txt"] = manifest
    archive = output / ("USBridge-Web-" + version + ".zip")
    timestamp = datetime.fromtimestamp(max(epoch, 315532800), timezone.utc).timetuple()[:6]
    with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as z:
        for name, p in sorted(assets.items()):
            info = zipfile.ZipInfo(name, timestamp)
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o100644 << 16
            with p.open("rb") as src, z.open(info, "w") as dest:
                shutil.copyfileobj(src, dest)
    checksum = output / "SHA256SUMS.txt"
    checksum.write_text(digest(archive) + "  " + archive.name + "\n")
    print(json.dumps({"archive": archive.name, "bytes": archive.stat().st_size,
                      "sha256": digest(archive), "source_commit": revision,
                      "static_assets": asset_count}))


if __name__ == "__main__":
    try:
        package(Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve())
    except (ValueError, OSError, subprocess.CalledProcessError) as e:
        raise SystemExit("Web bundle verification failed: " + str(e))
