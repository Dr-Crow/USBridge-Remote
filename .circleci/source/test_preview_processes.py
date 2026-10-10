import unittest
from preview_processes import validate_capture_chain


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
