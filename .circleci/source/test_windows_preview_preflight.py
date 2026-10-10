import json
import pathlib
import tempfile
import unittest

import windows_preview_preflight as preflight


class PreflightTests(unittest.TestCase):
    def test_extracts_real_preamble_without_cgo_directives(self):
        self.assertEqual(preflight.extract('package main\n/*\n#cgo LDFLAGS: -luser32\n#include <stdio.h>\n*/\nimport "C"'), '\n#include <stdio.h>\n')
        with self.assertRaises(AssertionError):
            preflight.extract('/* unrelated */\npackage main')

    def events(self):
        return [dict(Action='pass', Test=name) for name in preflight.REQUIRED for _ in range(5)] + [dict(Action='pass', Package='command-line-arguments')]

    def check(self, events):
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / 'tests.jsonl'
            path.write_text('\n'.join(map(json.dumps, events)))
            return preflight.summarize(path)

    def test_requires_exact_native_counts(self):
        self.assertEqual(set(self.check(self.events()).values()), {5})
        for events in (self.events()[1:], self.events()[:-1], self.events() + self.events(),
                       self.events() + [dict(Action='skip', Test='Other')],
                       self.events() + [dict(Action='build-fail')]):
            with self.assertRaises(AssertionError):
                self.check(events)


if __name__ == '__main__':
    unittest.main()
