import unittest
import pathlib
import tempfile
from unittest import mock
from preview_processes import namespace_media_processes, validate_capture_chain


class NamespaceMediaTests(unittest.TestCase):
    def test_reparented_and_deleted_media_are_included(self):
        with tempfile.TemporaryDirectory() as tmp:
            proc = pathlib.Path(tmp)
            for pid, executable in {1: '/sbin/tini', 42: '/opt/agent/usbridge-agent',
                                    80: '/work/state/source-preview/local-components/streamer/bin/source-streamer',
                                    81: '/usr/bin/ffmpeg (deleted)',
                                    82: '/work/source-preview-viewer', 83: '/usr/bin/Xvfb'}.items():
                entry = proc / str(pid); entry.mkdir()
                (entry / 'exe').symlink_to(executable)
                # Every media process is deliberately reparented to init. The
                # namespace census must not rely on a live agent ancestry chain.
                (entry / 'stat').write_text(f'{pid} (fixture) S 1 0 0\n')
            (proc / '84').mkdir()  # process exited during the scan
            (proc / '85-invalid').mkdir()
            self.assertEqual(namespace_media_processes(proc),
                             {80: 'source-streamer', 81: 'ffmpeg', 82: 'source-preview-viewer'})

    def test_incomplete_process_visibility_fails_closed(self):
        with tempfile.TemporaryDirectory() as tmp:
            proc = pathlib.Path(tmp); (proc / '42').mkdir()
            with mock.patch('preview_processes.os.readlink', side_effect=PermissionError):
                with self.assertRaises(PermissionError):
                    namespace_media_processes(proc)

    def test_engine_uses_namespace_census_and_retains_ownership_checks(self):
        source = pathlib.Path(__file__).with_name('preview_engine.py').read_text()
        self.assertIn('return namespace_media_processes()', source)
        self.assertIn('assert set(children) <= descendants(process.pid)', source)
        self.assertIn("assert not media_processes(), 'media remained in the private PID namespace'", source)


class ProcessShapeTests(unittest.TestCase):
    def setUp(self):
        self.processes = {1: 'source-streamer', 2: 'source-preview-viewer', 3: 'ffmpeg', 4: 'ffmpeg'}
        self.args = {3: ['ffmpeg', '-f', 'x11grab', '-video_size', '128x72', '-framerate', '30', '-i', ':96', '-an', '-c:v', 'libx264', '-f', 'h264', 'pipe:1'],
                     4: ['ffmpeg', '-f', 's16le', '-ar', '48000', '-ac', '2', '-i', 'pipe:0', '-vn', '-c:a', 'libopus', '-f', 'ogg', 'pipe:1']}

    def test_both_codecs_required(self):
        self.assertEqual(validate_capture_chain(self.processes, self.args.__getitem__), {'generated_video': 1, 'private_pcm_audio': 1})
        del self.processes[4]
        with self.assertRaises(AssertionError):
            validate_capture_chain(self.processes, self.args.__getitem__)

    def test_hardware_or_wrong_display_rejected(self):
        for pid, source in [(4, 'default'), (3, ':0')]:
            original = self.args[pid][:]
            self.args[pid][self.args[pid].index('-i') + 1] = source
            with self.assertRaises(AssertionError):
                validate_capture_chain(self.processes, self.args.__getitem__)
            self.args[pid] = original

    def test_duplicate_video_rejected(self):
        self.args[4] = self.args[3]
        with self.assertRaises(AssertionError):
            validate_capture_chain(self.processes, self.args.__getitem__)


if __name__ == '__main__':
    unittest.main()
