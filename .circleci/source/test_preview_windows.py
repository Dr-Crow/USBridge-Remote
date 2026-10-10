import subprocess
import unittest
from preview_windows import parent_window


class WindowSelectionTests(unittest.TestCase):
    def test_intersects_pid_exact_title_and_visibility(self):
        calls = []
        def command(*args):
            calls.append(args)
            return '12345'
        self.assertEqual(parent_window(command, 17, 'Agent (local)'), '12345')
        self.assertEqual(calls, [('xdotool', 'search', '--all', '--onlyvisible', '--pid', '17', '--name', r'^Agent\ \(local\)$')])

    def test_missing_can_retry_only_when_requested(self):
        def missing(*args):
            raise subprocess.CalledProcessError(1, args, output='')
        self.assertIsNone(parent_window(missing, 17, 'Agent', required=False))
        with self.assertRaises(AssertionError):
            parent_window(missing, 17, 'Agent')

    def test_ambiguity_never_selects_first_result(self):
        for result in ('1\n2', 'abc', '0'):
            with self.assertRaises(AssertionError):
                parent_window(lambda *args: result, 17, 'Agent', required=False)

    def test_other_command_failures_are_fatal(self):
        def failed(*args):
            raise subprocess.CalledProcessError(2, args, output='')
        with self.assertRaises(subprocess.CalledProcessError):
            parent_window(failed, 17, 'Agent', required=False)


if __name__ == '__main__':
    unittest.main()
