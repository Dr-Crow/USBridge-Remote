import json
import pathlib
import tempfile
import unittest
import prepare_windows_preview_media as media


class FixtureSummaryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = pathlib.Path(self.temp.name) / 'fixture.jsonl'

    def summary(self, extra=(), omit=None):
        events = [{'Action': 'pass', 'Test': name} for name in media.FIXTURE_REQUIRED if name != omit]
        if not any(e.get('Action') == 'skip' for e in extra):
            events.append({'Action': 'pass', 'Test': 'TestAssetHashSizeTypeAndSymlinkRejection/symlink'})
        events.extend(extra)
        events.append({'Action': 'pass'})
        self.path.write_text('\n'.join(json.dumps(e) for e in events))
        return media.summarize_fixture(self.path)

    def test_complete(self):
        result = self.summary()
        self.assertEqual(result['required_passed'], sorted(media.FIXTURE_REQUIRED))
        self.assertTrue(result['symlink_rejection_native_tested'])
        self.assertIsNone(result['skip_reason'])

    def test_one_explicit_native_fixture_limit(self):
        result = self.summary([{'Action': 'skip', 'Test': 'TestAssetHashSizeTypeAndSymlinkRejection/symlink'}])
        self.assertFalse(result['symlink_rejection_native_tested'])
        self.assertEqual(len(result['skipped']), 1)

    def test_unexpected_skip_failure_or_missing_required_is_fatal(self):
        for extra in ([{'Action': 'skip', 'Test': 'Unexpected'}], [{'Action': 'fail'}]):
            with self.subTest(extra=extra), self.assertRaises(AssertionError):
                self.summary(extra)
        with self.assertRaises(AssertionError):
            self.summary(omit='TestChildBrokenPipe')


if __name__ == '__main__':
    unittest.main()
