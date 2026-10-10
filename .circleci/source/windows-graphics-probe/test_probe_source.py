"""Architectural checks only; native compilation and WGL execution run in CI."""
import pathlib
import re
import unittest


class ProbeSourceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source = pathlib.Path(__file__).with_name('probe.c').read_text()
        cls.code = re.sub(r'/\*.*?\*/|//[^\n]*', '', cls.source, flags=re.S)

    def test_no_capture_input_network_or_child_execution(self):
        forbidden = ('GetDesktopWindow', 'GetWindowDC', 'BitBlt', 'StretchBlt',
                     'PrintWindow', 'glReadPixels', 'SendInput', 'mouse_event',
                     'keybd_event', 'CreateProcessA', 'CreateProcessW',
                     'ShellExecuteA', 'ShellExecuteW', 'WinExec', 'system',
                     'popen', '_popen', 'socket', 'connect', 'URLDownloadToFile')
        for name in forbidden:
            self.assertIsNone(re.search(r'\b' + name + r'\s*\(', self.code), name)
        self.assertEqual(re.findall(r'\bGetDC\(([^)]*)\)', self.code), ['window'])
        self.assertNotIn('ShowWindow(', self.code)

    def test_fixed_selection_and_loader(self):
        self.assertEqual(re.findall(r'SetEnvironmentVariableW\(L"([^"]+)", L"([^"]+)"\)', self.code),
                         [('GALLIUM_DRIVER', 'llvmpipe'), ('LIBGL_ALWAYS_SOFTWARE', 'true')])
        self.assertIn('LoadLibraryExW(path, NULL, LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR | LOAD_LIBRARY_SEARCH_SYSTEM32)', self.code)
        self.assertIn('local_dll_path(path, 32768, L"opengl32.dll")', self.code)
        self.assertIn('FILE_ATTRIBUTE_REPARSE_POINT', self.code)
        self.assertIn('if (argc != 1) return 2;', self.code)

    def test_current_context_precedes_strings(self):
        current = self.code.index('current_context() != context || current_dc() != dc')
        strings = self.code.index('copy_gl_string(vendor, get_string(0x1F00))')
        self.assertLess(current, strings)
        self.assertIn('if (c < 32 || c > 126) return 0;', self.code)
        self.assertIn('i < 256', self.code)
        self.assertIn('strncmp(renderer, "llvmpipe (", 10)', self.code)

    def test_one_private_json_then_eof(self):
        self.assertEqual(len(re.findall(r'\bWriteFile\(', self.code)), 1)
        self.assertEqual(len(re.findall(r'\bReadFile\(', self.code)), 1)
        self.assertIn('owned-wgl-probe-only', self.code)
        self.assertIn('ERROR_BROKEN_PIPE', self.code)
        self.assertIn('if (!make_current(NULL, NULL) || !delete_context(context)) result = 2;', self.code)
        self.assertIn('if (window && !DestroyWindow(window)) result = 2;', self.code)


if __name__ == '__main__':
    unittest.main()
