import hashlib
import tempfile
import unittest
from pathlib import Path

from package_web import MODELS, ORT, WASM_HEADER, validate


class BundleValidationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.web = self.root / "client/web"
        self.goroot = self.root / "go"
        names = ["index.html", "gui.html", "ai_vision.js", "wasm_exec.js", "app.wasm"]
        names += ["vendor/ort/" + name for name in ORT]
        names += ["vendor/ort/README.md"]
        for name in names:
            p = self.web / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_bytes(WASM_HEADER + b"fixture" if name.endswith(".wasm") else b"fixture")
        shim = self.goroot / "lib/wasm/wasm_exec.js"
        shim.parent.mkdir(parents=True)
        shim.write_bytes(b"fixture")
        pins = []
        for name in MODELS:
            data = name.encode()
            p = self.web / ("models/" + name + ".onnx")
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_bytes(data)
            pins.append('{Filename: "%s", SHA256: "%s", Size: %d},' %
                        (p.name, hashlib.sha256(data).hexdigest(), len(data)))
        source = self.root / "client/internal/localui/download.go"
        source.parent.mkdir(parents=True)
        source.write_text("\n".join(pins))

    def test_complete_bundle(self):
        assets = validate(self.root, self.goroot)
        self.assertIn("models/svtr.onnx", assets)
        self.assertIn("vendor/ort/ort-wasm-simd-threaded.jsep.wasm", assets)

    def test_missing_runtime_fails(self):
        (self.web / "vendor/ort/ort-wasm-simd-threaded.wasm").unlink()
        with self.assertRaisesRegex(ValueError, "Missing"):
            validate(self.root, self.goroot)

    def test_corrupt_or_lfs_pointer_model_fails(self):
        (self.web / "models/icon_detect.onnx").write_bytes(b"version https://git-lfs.github.com/spec/v1")
        with self.assertRaisesRegex(ValueError, "pinned hash/size"):
            validate(self.root, self.goroot)

    def test_mismatched_go_loader_fails(self):
        (self.web / "wasm_exec.js").write_bytes(b"other toolchain")
        with self.assertRaisesRegex(ValueError, "build toolchain"):
            validate(self.root, self.goroot)

    def test_invalid_wasm_fails(self):
        (self.web / "app.wasm").write_bytes(b"not wasm")
        with self.assertRaisesRegex(ValueError, "WebAssembly"):
            validate(self.root, self.goroot)

    def test_symlinked_asset_fails(self):
        p = self.web / "ai_vision.js"
        p.unlink()
        p.symlink_to(self.web / "index.html")
        with self.assertRaisesRegex(ValueError, "symlinked"):
            validate(self.root, self.goroot)

    def test_stale_gui_entry_fails(self):
        (self.web / "gui.html").write_bytes(b"stale entry")
        with self.assertRaisesRegex(ValueError, "entry point"):
            validate(self.root, self.goroot)
