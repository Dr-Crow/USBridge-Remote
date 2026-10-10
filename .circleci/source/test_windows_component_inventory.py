import json
import pathlib
import tempfile
import unittest
import windows_component_inventory as inventory


class InventoryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.directory = self.root / 'bin'
        self.directory.mkdir()
        self.source = self.directory / 'source-streamer.exe'
        self.source.write_bytes(b'public synthetic executable')
        self.dll = self.directory / 'runtime.dll'
        self.dll.write_bytes(b'public synthetic library')
        self.expected = {'runtime.dll': inventory.sha(self.dll)}
        self.failure = self.root / 'failure.json'

    def check(self):
        return inventory.component_files(self.source, self.expected, self.failure)

    def test_exact_inventory(self):
        result = self.check()
        self.assertEqual({v['path'] for v in result}, {'bin/source-streamer.exe', 'bin/runtime.dll'})
        self.assertFalse(self.failure.exists())

    def test_unknown_entry_fails_with_bounded_diagnostic(self):
        (self.directory / 'extra.pdb').write_bytes(b'not an authorized dependency')
        with self.assertRaises(inventory.InventoryError):
            self.check()
        result = json.loads(self.failure.read_text())
        extra = next(x for x in result['entries'] if x['name'] == 'extra.pdb')
        self.assertFalse(extra['exact_dependency_name'])
        self.assertFalse(extra['casefold_dependency_name'])
        self.assertNotIn(str(self.root), self.failure.read_text())
        self.assertNotIn('not an authorized dependency', self.failure.read_text())

    def test_case_mismatch_is_diagnosed_without_weakening_allowlist(self):
        self.expected = {'RUNTIME.DLL': inventory.sha(self.dll)}
        with self.assertRaises(inventory.InventoryError):
            self.check()
        item = next(x for x in json.loads(self.failure.read_text())['entries'] if x['name'] == 'runtime.dll')
        self.assertFalse(item['exact_dependency_name'])
        self.assertTrue(item['casefold_dependency_name'])

    def test_mutation_and_missing_dependency_fail(self):
        self.dll.write_bytes(b'changed')
        with self.assertRaises(inventory.InventoryError):
            self.check()
        self.dll.unlink()
        with self.assertRaises(inventory.InventoryError):
            self.check()

    def test_directory_and_symlink_fail(self):
        (self.directory / 'extra').mkdir()
        with self.assertRaises(inventory.InventoryError):
            self.check()
        (self.directory / 'extra').rmdir()
        self.dll.unlink()
        try:
            self.dll.symlink_to(self.source)
        except OSError:
            self.skipTest("native unprivileged symlink creation unavailable")
        with self.assertRaises(inventory.InventoryError):
            self.check()

    def test_unsafe_basename_is_redacted(self):
        (self.directory / 'secret name').write_bytes(b'x')
        with self.assertRaises(inventory.InventoryError):
            self.check()
        self.assertNotIn('secret name', self.failure.read_text())


if __name__ == '__main__':
    unittest.main()
