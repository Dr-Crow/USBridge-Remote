import hashlib
import json
import pathlib
import tempfile
import unittest
import windows_manager_components as manager
from prepare_windows_preview_manager import validate_media, read_receipt, ComponentStagingError, CASES, SOURCE, retire_verified_graphics_probe


class ManagerPrerequisiteTests(unittest.TestCase):
    def setUp(self):
        self.commit = '1' * 40
        self.media = {'schema_version': 1, 'commit': self.commit, 'passed': True, 'platform': 'windows/amd64',
                      'source_streamer_commit': SOURCE}
        for key in ('actual_agent_cli_supervision', 'actual_media_tested', 'actual_owned_window_pixels_tested',
                    'job_containment', 'fresh_restart_verified'):
            self.media[key] = True
        for key in ('actual_agent_app_new', 'physical_display_presentation_tested', 'desktop_capture_tested',
                    'input_injection_tested', 'windows_agent_preview_manager_enabled', 'source_snapshot_changed',
                    'source_or_binary_artifacts_published', 'pixels_saved'):
            self.media[key] = False
        self.inputs = {'schema_version': 1, 'commit': self.commit, 'source_streamer_commit': SOURCE,
                       'files_sha256': {role: digit * 64 for role, digit in [('viewer', '2'), ('source', '3'), ('fixture', '4')]}}
        for role, pin in self.inputs['files_sha256'].items():
            self.media[role + '_sha256'] = pin
        self.build = {'schema_version': 1, 'commit': self.commit, 'passed': True, 'viewer_compiled': True,
                      'platform': 'windows/amd64', 'go_version': 'go1.26.9', 'viewer_sha256': '2' * 64}
        self.media['cases'] = []
        for name in sorted(CASES):
            case = {'name': name, 'safety_job_kill_used': False,
                    'owned_window_color_transitions': 2, 'owned_window_samples': 10}
            for key in ('passed', 'typed_first_frame', 'owned_window_blue', 'owned_window_orange',
                        'natural_cleanup', 'job_empty_before_safety_close', 'owned_window_removed', 'advertised_listeners_closed'):
                case[key] = True
            self.media['cases'].append(case)

    def check(self):
        return validate_media(self.media, self.inputs, self.build, self.commit)

    def test_exact_media_prerequisite(self):
        self.assertEqual(self.check(), self.inputs['files_sha256'])

    def test_every_boolean_remains_strict(self):
        for group in (self.media, self.build, *self.media['cases']):
            for key, original in list(group.items()):
                if type(original) is not bool:
                    continue
                for mutation in (not original, None, int(original), str(original)):
                    with self.subTest(key=key, mutation=mutation):
                        group[key] = mutation
                        with self.assertRaises(ComponentStagingError): self.check()
                group[key] = original

    def test_pin_commit_and_case_mutations(self):
        for group, key in [(self.media, 'commit'), (self.inputs, 'commit'), (self.build, 'commit'),
                           (self.media, 'source_streamer_commit'), (self.inputs, 'source_streamer_commit'),
                           (self.build, 'viewer_sha256'), (self.media, 'source_sha256')]:
            original = group[key]
            group[key] = 'a' * len(original)
            with self.subTest(key=key), self.assertRaises(ComponentStagingError): self.check()
            group[key] = original
        self.media['cases'][1]['name'] = self.media['cases'][0]['name']
        with self.assertRaises(ComponentStagingError): self.check()

    def test_bounded_actual_pixel_counts(self):
        for key in ('owned_window_color_transitions', 'owned_window_samples'):
            original = self.media['cases'][0][key]
            for value in (None, True, 0, 1, 1501, 2.0, '2'):
                self.media['cases'][0][key] = value
                with self.subTest(key=key, value=value), self.assertRaises(ComponentStagingError): self.check()
            self.media['cases'][0][key] = original

    def test_receipt_duplicate_and_size_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            file = pathlib.Path(directory) / 'receipt.json'
            for raw in (b'{"passed":false,"passed":true}', b'x' * 65537, b'\xff', b'{', b'[]'):
                file.write_bytes(raw)
                with self.assertRaises(ComponentStagingError): read_receipt(file)
            file.write_text(json.dumps(self.media))
            self.assertEqual(read_receipt(file), self.media)


class RetiredGraphicsProbeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.work = pathlib.Path(self.temp.name).resolve()
        (self.work / 'bin').mkdir()
        self.probe = self.work / 'bin/windows-graphics-probe.exe'
        self.probe.write_bytes(b'nonexecutable source-owned test fixture')
        self.proof = {'schema_version': 1, 'role': 'owned-wgl-probe-only',
            'probe_sha256': hashlib.sha256(self.probe.read_bytes()).hexdigest(),
            'passed': True, 'fixed_software': True, 'fixed_driver': 'llvmpipe',
            'natural_cleanup': True, 'job_empty_before_safety_close': True,
            'safety_job_kill_used': False, 'actual_viewer_tested': False,
            'actual_window_pixels_tested': False, 'desktop_capture_tested': False,
            'input_injection_tested': False}

    def run_retirement(self):
        (self.work / 'software-graphics-probe.json').write_text(json.dumps(self.proof))
        return retire_verified_graphics_probe(self.work, self.work)

    def test_exact_retired_probe_moves_without_deletion_or_ignoring_unknown_files(self):
        unknown = self.work / 'bin/unapproved.exe'
        unknown.write_bytes(b'unknown')
        identity = self.probe.stat().st_ino
        receipt = self.run_retirement()
        target = self.work / 'retired-graphics-probe/windows-graphics-probe.exe'
        self.assertEqual(target.stat().st_ino, identity)
        self.assertFalse(self.probe.exists())
        self.assertTrue(unknown.exists())
        self.assertTrue(receipt['identity_preserved'])
        self.assertFalse(receipt['deleted'])
        viewer = self.work / 'bin/source-preview-viewer.exe'
        viewer.write_bytes(b'fixture viewer')
        with self.assertRaises(ComponentStagingError) as caught:
            manager._inventory(viewer, {viewer.name: hashlib.sha256(viewer.read_bytes()).hexdigest()}, manager.MAX_VIEWER_EXECUTABLE_BYTES)
        self.assertEqual(caught.exception.receipt['failure_code'], 'unexpected_component_member')

    def test_changed_hash_missing_or_forced_cleanup_fail_closed(self):
        for key, value in [('probe_sha256', 'a' * 64), ('natural_cleanup', False),
                ('job_empty_before_safety_close', False), ('safety_job_kill_used', True),
                ('role', 'other'), ('passed', 1), ('desktop_capture_tested', True)]:
            original = self.proof[key]
            self.proof[key] = value
            with self.subTest(key=key), self.assertRaises(ComponentStagingError): self.run_retirement()
            self.assertTrue(self.probe.exists())
            self.proof[key] = original

    def test_existing_destination_is_not_replaced(self):
        (self.work / 'retired-graphics-probe').mkdir()
        with self.assertRaises(ComponentStagingError): self.run_retirement()
        self.assertTrue(self.probe.exists())

    def test_symlinked_probe_is_rejected(self):
        other = self.work / 'other.exe'
        self.probe.rename(other)
        try: self.probe.symlink_to(other)
        except (OSError, NotImplementedError): self.skipTest('native symlink creation unavailable')
        if not self.probe.is_symlink(): self.skipTest('filesystem emulates symlinks as copies')
        with self.assertRaises(ComponentStagingError): self.run_retirement()



if __name__ == '__main__':
    unittest.main()
