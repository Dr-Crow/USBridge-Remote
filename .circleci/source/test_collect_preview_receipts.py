import importlib.util
import json
import pathlib
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('collector', pathlib.Path(__file__).with_name('collect_preview_receipts.py'))
collector = importlib.util.module_from_spec(spec)
spec.loader.exec_module(collector)


class ReceiptTests(unittest.TestCase):
    def test_only_explicit_receipts(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            source = root / 'source-preview-dialog'
            source.mkdir()
            (source / 'result.json').write_text('{"passed":true}')
            (source / 'session.json').write_text('{"key_b64":"private"}')
            (source / 'source.tar.gz').write_bytes(b'source')
            (source / 'package').mkdir()
            (source / 'package' / 'source.go').write_text('source')
            result = collector.collect(root)
            self.assertEqual(set(result['files_sha256']), {'source-preview-dialog/result.json'})
            self.assertFalse(result['source_or_binary_artifacts_published'])

    def test_secret_field_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            source = root / 'source-preview'
            source.mkdir()
            (source / 'result.json').write_text('{"nested":{"key_b64":"private"}}')
            with self.assertRaises(AssertionError):
                collector.collect(root)

    def test_symlink_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            source = root / 'source-preview'
            source.mkdir()
            (root / 'outside').write_text('{"passed":true}')
            (source / 'result.json').symlink_to(root / 'outside')
            with self.assertRaises(AssertionError):
                collector.collect(root)


if __name__ == '__main__':
    unittest.main()
