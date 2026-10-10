"""Keep the Linux race gate on the complete viewer package and its helpers."""
import pathlib
import re
import unittest


def validate_invocation(script):
    assert re.search(r'go test -race ./cmd/source-preview-viewer(?:\s|\))', script)
    assert not re.search(r'go test[^\n]*./cmd/source-preview-viewer/[A-Za-z_]+\.go', script)


class ViewerInvocationTest(unittest.TestCase):
    def test_complete_package_is_race_tested(self):
        script = pathlib.Path(__file__).resolve().parents[1] / 'test-source-preview.sh'
        validate_invocation(script.read_text())

    def test_omitted_helper_file_list_is_rejected(self):
        with self.assertRaises(AssertionError):
            validate_invocation('go test -race ./cmd/source-preview-viewer/events.go ./cmd/source-preview-viewer/events_test.go')
        with self.assertRaises(AssertionError):
            validate_invocation('go test ./cmd/source-preview-viewer')
