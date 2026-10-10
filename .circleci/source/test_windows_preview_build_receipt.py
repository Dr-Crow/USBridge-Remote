import json
import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock

import windows_preview_build_receipt as receipt


class ReceiptTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)

    def events(self, values):
        path = self.root / 'test.jsonl'
        path.write_text('\n'.join(json.dumps(v) for v in values) + '\n')
        return path

    def test_completed_native_test(self):
        path = self.events([{'Action': 'pass', 'Test': 'Required'}, {'Action': 'pass'}])
        self.assertEqual(receipt.summarize_tests(path, {'Required'}),
                         {'passed': 1, 'failed': 0, 'skipped': 0, 'required_passed': ['Required']})

    def test_reject_failure_skip_incomplete_and_missing(self):
        for values in [
            [{'Action': 'pass', 'Test': 'Required'}],
            [{'Action': 'pass', 'Test': 'Other'}, {'Action': 'pass'}],
            [{'Action': 'pass', 'Test': 'Required'}, {'Action': 'pass'}, {'Action': 'fail'}],
            [{'Action': 'pass', 'Test': 'Required'}, {'Action': 'pass'}, {'Action': 'skip', 'Test': 'Optional'}],
        ]:
            with self.subTest(values=values), self.assertRaises(AssertionError):
                receipt.summarize_tests(self.events(values), {'Required'})

    def tree(self):
        package, ucrt, system = (self.root / name for name in ('package', 'ucrt', 'system'))
        for item in package, ucrt, system:
            item.mkdir()
        binary = package / 'viewer.exe'
        binary.write_bytes(b'viewer')
        (ucrt / 'a.dll').write_bytes(b'a')
        (ucrt / 'b.dll').write_bytes(b'b')
        (system / 'kernel32.dll').write_bytes(b'system')
        return binary, ucrt, system

    def imports(self, mapping):
        def run(args, **kwargs):
            value = mapping[pathlib.Path(args[-1]).name]
            self.assertEqual(args[1], '-p')
            self.assertTrue(kwargs['check'])
            self.assertEqual(kwargs['timeout'], 30)
            return subprocess.CompletedProcess(args, 0, '\n'.join('DLL Name: ' + n for n in value), '')
        return run

    def test_recursive_closure_and_system_imports(self):
        binary, ucrt, system = self.tree()
        imports = {'viewer.exe': ['a.dll', 'kernel32.dll'], 'a.dll': ['b.dll'],
                   'b.dll': ['a.dll', 'api-ms-win-core-file-l1-1-0.dll']}
        with mock.patch.object(receipt.subprocess, 'run', side_effect=self.imports(imports)):
            copied, systems = receipt.stage_dependencies(binary, ucrt, system)
        self.assertEqual(copied, {name: receipt.sha(ucrt / name) for name in ('a.dll', 'b.dll')})
        self.assertEqual(systems, ['api-ms-win-core-file-l1-1-0.dll', 'kernel32.dll'])

    def test_reject_unresolved_or_unsafe_import(self):
        binary, ucrt, system = self.tree()
        for name in ('unknown.dll', '../a.dll', 'C:\\a.dll', 'a.dll extra'):
            with self.subTest(name=name), mock.patch.object(receipt.subprocess, 'run',
                    side_effect=self.imports({'viewer.exe': [name]})), self.assertRaises(AssertionError):
                receipt.stage_dependencies(binary, ucrt, system)

    def test_reject_conflicting_target_and_symlink(self):
        binary, ucrt, system = self.tree()
        target = binary.parent / 'a.dll'
        target.write_bytes(b'bad')
        with mock.patch.object(receipt.subprocess, 'run', side_effect=self.imports({'viewer.exe': ['a.dll']})):
            with self.assertRaises(AssertionError):
                receipt.stage_dependencies(binary, ucrt, system)
            target.unlink()
            target.symlink_to(ucrt / 'a.dll')
            with self.assertRaises(AssertionError):
                receipt.stage_dependencies(binary, ucrt, system)


if __name__ == '__main__':
    unittest.main()
