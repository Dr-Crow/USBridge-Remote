"""Portable adversarial fixtures; no PE binaries or private components are used."""
import hashlib
import json
import os
import pathlib
import tempfile
import types
import unittest
from unittest import mock

import windows_manager_components as manager


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


class ManagerComponentTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name).resolve()
        source_dir, viewer_dir = self.root / 'source-input', self.root / 'viewer-input'
        source_dir.mkdir()
        viewer_dir.mkdir()
        self.source = source_dir / 'source-streamer.exe'
        self.viewer = viewer_dir / 'source-preview-viewer.exe'
        self.source.write_bytes(b'public synthetic source; not executable')
        self.viewer.write_bytes(b'public synthetic viewer; not executable')
        (source_dir / 'runtime.dll').write_bytes(b'source synthetic library')
        (viewer_dir / 'runtime.dll').write_bytes(b'viewer synthetic library')
        (viewer_dir / 'opengl32.dll').write_bytes(b'synthetic app-local software graphics')
        (viewer_dir / 'libgallium_wgl.dll').write_bytes(b'synthetic Mesa closure')
        (viewer_dir / 'libLLVM-22.dll').write_bytes(b'synthetic LLVM closure')
        self.output = self.root / 'manager-components'
        self.arguments = {
            'source': self.source, 'source_sha256': sha(self.source),
            'source_dependencies': {'runtime.dll': sha(source_dir / 'runtime.dll')},
            'source_version': '2' * 40,
            'viewer': self.viewer, 'viewer_sha256': sha(self.viewer),
            'viewer_dependencies': {p.name: sha(p) for p in viewer_dir.glob('*.dll')},
            'viewer_version': 'v1.2.3-test', 'commit': 'a' * 40, 'output': self.output,
        }

    def stage(self, **changes):
        return manager.stage_manager_components(**(self.arguments | changes))

    def rejected(self, code=None, **changes):
        with self.assertRaises(manager.ComponentStagingError) as caught:
            self.stage(**changes)
        receipt = caught.exception.receipt
        self.assertIs(receipt['passed'], False)
        self.assertIs(receipt['component_execution_performed'], False)
        self.assertIs(receipt['source_or_binary_artifacts_published'], False)
        self.assertNotIn(str(self.root), json.dumps(receipt))
        self.assertNotIn(str(self.root), str(caught.exception))
        self.assertNotIn('public synthetic', json.dumps(receipt))
        if code is not None:
            self.assertEqual(receipt['failure_code'], code)
        self.assertEqual(list(self.root.glob('.manager-components-*')), [])
        return caught.exception

    def test_exact_two_components_with_app_local_dlls_and_hashed_manifest(self):
        receipt = self.stage()
        manifest_raw = (self.output / 'manifest.json').read_bytes()
        manifest = json.loads(manifest_raw)
        self.assertEqual(receipt['manifest_sha256'], hashlib.sha256(manifest_raw).hexdigest())
        self.assertEqual(receipt['manifest_size'], len(manifest_raw))
        self.assertEqual(manifest, {'schema': 1, 'components': receipt['components']})
        self.assertEqual({p.name for p in self.output.iterdir()}, {'manifest.json', 'source', 'viewer'})
        source, viewer = manifest['components']
        self.assertEqual((source['name'], source['profile'], source['entry']),
                         ('source-streamer', 'source-streamer-v1', 'source/source-streamer.exe'))
        self.assertEqual((viewer['name'], viewer['profile'], viewer['entry']),
                         ('source-preview-viewer', 'source-preview-v1', 'viewer/source-preview-viewer.exe'))
        self.assertEqual(source['version'], '2' * 40)
        self.assertEqual(viewer['version'], 'v1.2.3-test')
        total = 0
        for component in manifest['components']:
            self.assertEqual(component['platform'], 'windows/amd64')
            self.assertEqual(sum(f['executable'] for f in component['files']), 1)
            prefix = component['entry'].split('/')[0]
            files = component['files']
            self.assertEqual({f['path'].split('/')[1] for f in files},
                             {p.name for p in (self.output / prefix).iterdir()})
            for entry in files:
                path = self.output / entry['path']
                self.assertEqual(entry['sha256'], sha(path))
                self.assertEqual(entry['size'], path.stat().st_size)
                self.assertEqual(entry['executable'], entry['path'] == component['entry'])
                total += entry['size']
        self.assertEqual(total, receipt['total_file_bytes'])
        self.assertEqual((self.output / 'viewer/opengl32.dll').read_bytes(), b'synthetic app-local software graphics')
        self.assertNotEqual(sha(self.output / 'source/runtime.dll'), sha(self.output / 'viewer/runtime.dll'))
        self.assertNotIn(str(self.root), json.dumps(receipt))
        self.assertNotIn('public synthetic', json.dumps(receipt))

    def test_source_alias_accepts_samefile_without_lexical_equality(self):
        alias = self.source.parent / '..' / self.source.parent.name / self.source.name
        self.assertNotEqual(alias, self.source)
        self.assertTrue(alias.samefile(self.source))
        receipt = self.stage(source=alias)
        self.assertTrue(receipt['passed'])

    def test_viewer_alias_accepts_samefile_without_lexical_equality(self):
        alias = self.viewer.parent / '..' / self.viewer.parent.name / self.viewer.name
        self.assertTrue(self.stage(viewer=alias)['passed'])

    def test_separate_cross_role_identical_executables_are_rejected(self):
        self.viewer.write_bytes(self.source.read_bytes())
        self.rejected('cross_role_executable_hash', viewer_sha256=sha(self.viewer))
        self.assertFalse(self.output.exists())

    def test_cross_role_hardlink_is_rejected(self):
        self.viewer.unlink()
        os.link(self.source, self.viewer)
        self.rejected('cross_role_executable_alias', viewer_sha256=sha(self.viewer))
        self.assertFalse(self.output.exists())

    def test_altered_executable_hash_is_rejected(self):
        for role in ('source', 'viewer'):
            with self.subTest(role=role):
                self.rejected('file_hash_mismatch', **{role + '_sha256': '0' * 64})
                self.assertFalse(self.output.exists())

    def test_altered_dependency_hash_or_bytes_is_rejected(self):
        pins = self.arguments['viewer_dependencies'] | {'opengl32.dll': '0' * 64}
        self.rejected('file_hash_mismatch', viewer_dependencies=pins)
        (self.source.parent / 'runtime.dll').write_bytes(b'changed source bytes')
        self.rejected('file_hash_mismatch')
        self.assertFalse(self.output.exists())

    def test_unknown_dll_is_rejected_in_both_components(self):
        for directory in (self.source.parent, self.viewer.parent):
            with self.subTest(directory=directory.name):
                extra = directory / 'unknown.dll'
                extra.write_bytes(b'not in the verified closure')
                self.rejected('unexpected_component_member')
                extra.unlink()
        self.assertFalse(self.output.exists())

    def test_unknown_non_dll_or_subdirectory_is_rejected(self):
        path = self.viewer.parent / 'unknown.pdb'
        path.write_bytes(b'not authorized')
        self.rejected('unexpected_component_member')
        path.unlink()
        path.mkdir()
        self.rejected('unexpected_component_member')

    def test_missing_dependency_is_rejected(self):
        (self.viewer.parent / 'libLLVM-22.dll').unlink()
        self.rejected('missing_component_member')

    def test_case_collision_in_allowlist_is_rejected(self):
        pins = self.arguments['viewer_dependencies'] | {'RUNTIME.DLL': self.arguments['viewer_dependencies']['runtime.dll']}
        self.rejected('basename_case_collision', viewer_dependencies=pins)

    def test_case_only_mismatch_does_not_weaken_allowlist(self):
        pins = dict(self.arguments['source_dependencies'])
        pins['RUNTIME.DLL'] = pins.pop('runtime.dll')
        self.rejected('unexpected_component_member', source_dependencies=pins)

    def test_case_collision_on_disk_is_rejected(self):
        path = self.viewer.parent / 'RUNTIME.DLL'
        if path.exists():
            self.skipTest('case-insensitive filesystem cannot represent both basenames')
        path.write_bytes(b'collision')
        self.rejected()

    def test_untrusted_dependency_names_are_rejected(self):
        for name in ('../escape.dll', '/escape.dll', r'C:\escape.dll', r'sub\escape.dll',
                     'a.dll:stream', 'CON.dll', 'COM1.dll', 'LPT9.dll', 'a.dll ',
                     '.hidden.dll', 'bad%name.dll', 'bad\nname.dll', 'a.exe'):
            with self.subTest(name=name):
                self.rejected('invalid_dependency_pin', source_dependencies={name: 'b' * 64})
        self.assertFalse((self.root / 'escape.dll').exists())

    def test_invalid_hashes_versions_and_commit_are_rejected(self):
        for digest in ('A' * 64, 'g' * 64, '', '0' * 63, None, 123):
            with self.subTest(digest=digest):
                self.rejected('invalid_executable_pin', source_sha256=digest)
                self.rejected('invalid_dependency_pin', source_dependencies={'runtime.dll': digest})
        for field in ('source_version', 'viewer_version', 'commit'):
            with self.subTest(field=field):
                self.rejected(**{field: str(self.root)})

    def test_dependency_count_is_bounded(self):
        pins = {f'dep{i}.dll': 'b' * 64 for i in range(manager.MAX_DEPENDENCIES + 1)}
        self.rejected('invalid_dependency_inventory', viewer_dependencies=pins)

    def test_empty_and_huge_files_are_rejected_before_hashing(self):
        dll = self.source.parent / 'runtime.dll'
        for size in (0, manager.MAX_DLL_BYTES + 1):
            with self.subTest(size=size):
                with dll.open('wb') as f:
                    f.truncate(size)
                with mock.patch.object(manager, '_hash_file', side_effect=AssertionError('oversize file must not be hashed')):
                    self.rejected('file_size_limit')

    def test_executable_size_is_bounded(self):
        with self.source.open('wb') as f:
            f.truncate(manager.MAX_EXECUTABLE_BYTES + 1)
        self.rejected('file_size_limit')

    def test_combined_component_aggregate_is_bounded(self):
        total = sum(p.stat().st_size for directory in (self.source.parent, self.viewer.parent) for p in directory.iterdir())
        with mock.patch.object(manager, 'MAX_TOTAL_BYTES', total - 1):
            self.rejected('aggregate_size_limit')
        self.assertFalse(self.output.exists())

    def test_empty_dependency_closure_is_valid(self):
        (self.source.parent / 'runtime.dll').unlink()
        receipt = self.stage(source_dependencies={})
        self.assertEqual(len(receipt['components'][0]['files']), 1)

    def test_destination_reuse_preserves_existing_content(self):
        self.output.mkdir()
        self.rejected('destination_exists')
        sentinel = self.output / 'sentinel'
        sentinel.write_bytes(b'preserve existing destination')
        self.rejected('destination_exists')
        self.assertEqual(sentinel.read_bytes(), b'preserve existing destination')

    def test_successful_destination_cannot_be_reused(self):
        self.stage()
        before = (self.output / 'manifest.json').read_bytes()
        self.rejected('destination_exists')
        self.assertEqual((self.output / 'manifest.json').read_bytes(), before)

    def test_destination_file_and_dangling_symlink_are_rejected(self):
        self.output.write_bytes(b'leave me intact')
        self.rejected('destination_exists')
        self.assertEqual(self.output.read_bytes(), b'leave me intact')
        self.output.unlink()
        self.make_symlink(self.output, self.root / 'absent')
        self.rejected('destination_exists')
        self.assertTrue(self.output.is_symlink())

    def test_source_destination_aliasing_is_rejected(self):
        self.rejected('source_destination_alias', output=self.source.parent / 'new-output')
        alias = self.source.parent / '..' / self.source.parent.name / 'new-output'
        self.rejected('source_destination_alias', output=alias)
        self.rejected('destination_exists', output=self.viewer.parent)
        self.rejected('cross_role_executable_alias', viewer=self.source,
                      viewer_sha256=sha(self.source), viewer_dependencies=self.arguments['source_dependencies'])

    def test_relative_paths_are_rejected(self):
        self.rejected(source=pathlib.Path('source-streamer.exe'))
        self.rejected(output=pathlib.Path('output'))

    def test_invalid_destination_basenames_are_rejected(self):
        for name in ('NUL', 'CON.output', 'LPT1', 'output.', 'output:stream', 'output%name', 'output name'):
            with self.subTest(name=name):
                self.rejected('invalid_destination', output=self.root / name)

    def make_symlink(self, link, target, directory=False):
        try:
            link.symlink_to(target, target_is_directory=directory)
        except OSError:
            self.skipTest('native unprivileged symlink creation unavailable')

    def test_symlink_executable_dependency_and_parent_are_rejected(self):
        original = self.source.read_bytes()
        self.source.unlink()
        self.make_symlink(self.source, self.viewer)
        self.rejected('unsafe_path')
        self.source.unlink()
        self.source.write_bytes(original)
        dll = self.source.parent / 'runtime.dll'
        original_dll = dll.read_bytes()
        dll.unlink()
        self.make_symlink(dll, self.viewer.parent / 'runtime.dll')
        self.rejected('unsafe_path')
        dll.unlink()
        dll.write_bytes(original_dll)
        link = self.root / 'input-link'
        self.make_symlink(link, self.source.parent, directory=True)
        self.rejected('unsafe_path', source=link / self.source.name)
        # Normalizing .. before validating ancestors would conceal this link.
        self.rejected('unsafe_path', source=link / '..' / self.source.parent.name / self.source.name)

    def test_destination_parent_symlink_is_rejected(self):
        link = self.root / 'output-link'
        self.make_symlink(link, self.root, directory=True)
        self.rejected('unsafe_path', output=link / self.output.name)

    def test_reparse_file_and_ancestor_are_rejected_portably(self):
        original_lstat = pathlib.Path.lstat
        for rejected_path in (self.source, self.viewer.parent, self.root):
            def lstat(path, *args, **kwargs):
                info = original_lstat(path, *args, **kwargs)
                if path == rejected_path:
                    return types.SimpleNamespace(st_mode=info.st_mode, st_file_attributes=manager.REPARSE_POINT)
                return info
            with self.subTest(path=rejected_path.name), mock.patch.object(pathlib.Path, 'lstat', lstat):
                self.rejected('unsafe_path')

    def test_changed_input_during_copy_is_rejected_and_temporary_tree_removed(self):
        original_hash = manager._hash_file
        def changing(path, digest, limit, snapshot=None, target=None):
            if target is not None:
                path.write_bytes(b'changed during copy')
            return original_hash(path, digest, limit, snapshot=snapshot, target=target)
        with mock.patch.object(manager, '_hash_file', changing):
            self.rejected('input_changed')
        self.assertFalse(self.output.exists())

    def test_new_input_member_during_copy_is_rejected(self):
        original_hash = manager._hash_file
        def changing(path, digest, limit, snapshot=None, target=None):
            result = original_hash(path, digest, limit, snapshot=snapshot, target=target)
            if target is not None and path == self.viewer:
                (self.viewer.parent / 'injected.dll').write_bytes(b'unlisted')
            return result
        with mock.patch.object(manager, '_hash_file', changing):
            self.rejected('unexpected_component_member')
        self.assertFalse(self.output.exists())

    def test_corrupted_copied_file_is_rejected(self):
        original_hash = manager._hash_file
        def corrupt(path, digest, limit, snapshot=None, target=None):
            result = original_hash(path, digest, limit, snapshot=snapshot, target=target)
            if target is not None:
                target.write_bytes(b'corrupted copy')
            return result
        with mock.patch.object(manager, '_hash_file', corrupt):
            self.rejected('file_hash_mismatch')
        self.assertFalse(self.output.exists())

    def test_manifest_is_rehashed_before_publication(self):
        original_hash = manager._hash_file
        def corrupt(path, digest, limit, snapshot=None, target=None):
            if path.name == 'manifest.json':
                path.write_bytes(b'{"corrupted":true}')
            return original_hash(path, digest, limit, snapshot=snapshot, target=target)
        with mock.patch.object(manager, '_hash_file', corrupt):
            self.rejected('file_hash_mismatch')
        self.assertFalse(self.output.exists())

    def test_publish_race_never_replaces_existing_empty_destination(self):
        original_publish = manager._publish_directory
        def publish(staged, output):
            output.mkdir()
            original_publish(staged, output)
        with mock.patch.object(manager, '_publish_directory', publish):
            self.rejected()
        self.assertTrue(self.output.is_dir())
        self.assertEqual(list(self.output.iterdir()), [])

    def test_no_destination_visible_until_complete_atomic_publication(self):
        original_publish = manager._publish_directory
        called = []
        def publish(staged, output):
            self.assertFalse(output.exists())
            self.assertEqual({p.name for p in staged.iterdir()}, {'source', 'viewer', 'manifest.json'})
            self.assertTrue((staged / 'viewer/opengl32.dll').is_file())
            called.append(True)
            original_publish(staged, output)
        with mock.patch.object(manager, '_publish_directory', publish):
            self.stage()
        self.assertEqual(called, [True])

    def test_os_error_is_sanitized_without_exception_chaining(self):
        with mock.patch.object(manager, '_publish_directory', side_effect=OSError('private path: ' + str(self.root))):
            error = self.rejected('staging_io_or_input_error')
        self.assertTrue(error.__suppress_context__)
        self.assertFalse(self.output.exists())


if __name__ == '__main__':
    unittest.main()
