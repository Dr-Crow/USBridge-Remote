import json
import pathlib
import tempfile
import unittest

from check_windows_viewer_startup import checked_command
from windows_preview_build_receipt import sha


class StartupCommandTests(unittest.TestCase):
    def test_exact_binary_and_dll_identity(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp).resolve()
            (root/'bin').mkdir()
            viewer, dll, runner = root/'bin/source-preview-viewer.exe', root/'bin/runtime.dll', root/'runner.exe'
            for p in (viewer, dll, runner): p.write_bytes(p.name.encode())
            receipt = dict(passed=True, viewer_compiled=True, commit='a'*40,
                           viewer_sha256=sha(viewer), runtime_dlls_sha256={'runtime.dll': sha(dll)})
            (root/'build.json').write_text(json.dumps(receipt))
            command = checked_command(root, root, runner, 'a'*40)
            self.assertIn('--window-startup', command)
            self.assertNotIn('--agent', command)
            with self.assertRaises(AssertionError): checked_command(root, root, runner, 'b'*40)
            dll.write_bytes(b'changed')
            with self.assertRaises(AssertionError): checked_command(root, root, runner, 'a'*40)
            dll.write_bytes(dll.name.encode())
            viewer.write_bytes(b'changed')
            with self.assertRaises(AssertionError): checked_command(root, root, runner, 'a'*40)
