import hashlib
import json
import os
import pathlib
import tempfile
import types
import unittest
from unittest import mock

import windows_seed_public_cache as seed
from windows_viewer_cache_key import AUXILIARY, PUBLIC_CLIENT, key

STAGE = pathlib.Path(__file__).resolve().parent
KEY = 'c' * 64


def put_data(cache, data=b'public compilation unit'):
    digest = hashlib.sha256(data).hexdigest()
    path = cache / digest[:2] / (digest + '-d')
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data)
    return path


def put_action(cache, output, action='a' * 64, timestamp=123):
    path = cache / action[:2] / (action + '-a')
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(f'v1 {action} {output.name[:64]} {output.stat().st_size:20d} {timestamp:20d}\n'.encode())
    return path


def inventory():
    names = ('client/go.mod', 'client/go.sum', *AUXILIARY[2:])
    return (''.join('100644 blob ' + 'a' * 40 + '\t' + n + '\0' for n in names) +
            '160000 commit ' + PUBLIC_CLIENT + '\tclient/moonlight-common-c\0').encode()


class CacheSeedTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir=STAGE)
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.source = self.root / 'public'
        self.target = self.root / 'private'
        self.source.mkdir()
        self.data = put_data(self.source)
        self.action = put_action(self.source, self.data)

    def run_seed(self):
        return seed.seed_cache(self.source, self.target, KEY)

    def reject(self):
        with self.assertRaises((seed.SeedError, OSError)):
            self.run_seed()

    def test_seed_is_one_way_and_receipt_is_closed(self):
        (self.source / 'README').write_text('public cache metadata')
        (self.source / 'trim.txt').write_text('123')
        before = seed.scan(self.source)
        self.target.mkdir()
        private = put_data(self.target, b'private unrelated unit')
        receipt = self.run_seed()
        self.assertEqual(seed.scan(self.source), before)
        self.assertEqual(private.read_bytes(), b'private unrelated unit')
        self.assertFalse((self.source / private.relative_to(self.target)).exists())
        self.assertFalse((self.target / 'README').exists())
        self.assertFalse((self.target / 'trim.txt').exists())
        self.assertEqual(receipt['copied_files'], 2)
        self.assertEqual(receipt['skipped_metadata_files'], 2)
        self.assertEqual(receipt['public_cache_sha256'], receipt['source_after_sha256'])
        self.assertTrue(receipt['source_unchanged'])
        self.assertEqual(set(receipt), {'schema', 'passed', 'public_cache_key', 'public_cache_sha256',
                                       'source_unchanged', 'source_after_sha256', 'public_files',
                                       'public_bytes', 'copied_files', 'copied_bytes',
                                       'retained_files', 'skipped_metadata_files'})
        text = json.dumps(receipt)
        for forbidden in (str(self.root), self.data.name, 'public compilation unit', 'private unrelated unit'):
            self.assertNotIn(forbidden, text)

    def test_identical_data_reused_existing_action_retained(self):
        data = put_data(self.target)
        action = put_action(self.target, data, timestamp=456)
        before = action.read_bytes()
        receipt = self.run_seed()
        self.assertEqual(receipt['retained_files'], 2)
        self.assertEqual(receipt['copied_files'], 0)
        self.assertEqual(action.read_bytes(), before)

    def test_bad_key_fails_before_destination_creation(self):
        for value in ('', 'C' * 64, 'c' * 63, 'c' * 65, KEY + '\n', '../' + KEY):
            with self.subTest(value=value), self.assertRaises(seed.SeedError):
                seed.seed_cache(self.source, self.target, value)
        self.assertFalse(self.target.exists())

    def test_bad_filename_and_shard(self):
        for name in ('bad-d', 'A' * 64 + '-d', 'a' * 64 + '-x', 'b' * 64 + '-d'):
            with self.subTest(name=name):
                path = self.source / 'aa' / name
                path.write_bytes(b'no')
                self.reject()
                path.unlink()
        self.assertFalse(self.target.exists())

    def test_unknown_root_and_nested_directories_rejected(self):
        wrong = self.source / 'unrelated'
        wrong.mkdir()
        self.reject()
        wrong.rmdir()
        self.data.unlink()
        self.data.mkdir()
        self.reject()
        self.assertFalse(self.target.exists())

    def test_hash_mismatch_rejected(self):
        self.data.write_bytes(b'wrong bytes')
        self.reject()
        self.assertFalse(self.target.exists())

    def test_malformed_action_rejected(self):
        good = self.action.read_bytes()
        for data in (b'not Go metadata', good.replace(b'v1 ', b'v2 ', 1),
                     good.replace(b'a' * 64, b'b' * 64, 1), good + b'x',
                     good[:-2] + b'-\n'):
            with self.subTest(data=data[:3]):
                self.action.write_bytes(data)
                self.reject()
        self.assertFalse(self.target.exists())

    def test_source_destination_overlap_rejected(self):
        for target in (self.source, self.source / 'nested', self.root):
            with self.subTest(target=target), self.assertRaises(seed.SeedError):
                seed.seed_cache(self.source, target, KEY)

    def test_source_symlink_rejected(self):
        other = self.root / 'other'
        other.write_bytes(self.data.read_bytes())
        self.data.unlink()
        self.data.symlink_to(other)
        self.reject()
        self.assertFalse(self.target.exists())

    def test_metadata_symlink_rejected_even_though_not_copied(self):
        (self.source / 'README').symlink_to(self.data)
        self.reject()
        self.assertFalse(self.target.exists())

    def test_destination_alias_symlink_rejected(self):
        self.target.symlink_to(self.source, target_is_directory=True)
        self.reject()

    def test_ancestor_symlink_rejected(self):
        alias = self.root / 'alias'
        alias.symlink_to(self.root, target_is_directory=True)
        with self.assertRaises(seed.SeedError):
            seed.seed_cache(alias / 'public', self.target, KEY)

    def test_target_file_symlink_rejected(self):
        target = put_data(self.target)
        target.unlink()
        target.symlink_to(self.data)
        self.reject()

    def test_reparse_points_rejected_without_windows(self):
        original = pathlib.Path.lstat
        wanted = original(self.data)
        injected = []
        def reparse(path, *args, **kwargs):
            info = original(path, *args, **kwargs)
            # Directory enumeration and Path.absolute can spell the same
            # Windows file differently. Bind this fixture to the real file
            # identity, without changing the production path/type checks.
            if os.path.samestat(info, wanted):
                injected.append(True)
                return types.SimpleNamespace(st_mode=info.st_mode, st_file_attributes=0x400)
            return info
        with mock.patch.object(pathlib.Path, 'lstat', reparse):
            with self.assertRaisesRegex(seed.SeedError, 'links and reparse points are forbidden'):
                self.run_seed()
        self.assertTrue(injected, 'reparse metadata fixture was not observed')
        self.assertFalse(self.target.exists())

    def test_hardlink_rejected(self):
        self.target.mkdir()
        os.link(self.data, self.target / 'linked')
        self.reject()

    def test_existing_conflicting_data_fails_without_writes(self):
        target = put_data(self.target)
        target.write_bytes(b'corrupt existing output')
        before = {str(p.relative_to(self.target)): p.read_bytes()
                  for p in self.target.rglob('*') if p.is_file()}
        self.reject()
        self.assertEqual(before, {str(p.relative_to(self.target)): p.read_bytes()
                                 for p in self.target.rglob('*') if p.is_file()})

    def test_count_and_byte_limits(self):
        for name, value in (('MAX_FILES', 1), ('MAX_BYTES', 1), ('MAX_FILE_BYTES', 1)):
            with self.subTest(name=name), mock.patch.object(seed, name, value):
                self.reject()
        self.assertFalse(self.target.exists())

    def test_source_change_after_copy_rejected(self):
        original = seed.scan
        count = 0
        def mutate(cache):
            nonlocal count
            if cache == self.source:
                count += 1
                if count == 2:
                    (self.source / 'README').write_text('changed while copying')
            return original(cache)
        with mock.patch.object(seed, 'scan', side_effect=mutate):
            self.reject()

    def test_copy_corruption_rejected(self):
        original = seed.inspect_file
        def corrupt(path, cache_name=None, sink=None):
            entry = original(path, cache_name, sink)
            if sink:
                sink.write(b'corruption')
            return entry
        with mock.patch.object(seed, 'inspect_file', side_effect=corrupt):
            self.reject()
        self.assertEqual(self.data.read_bytes(), b'public compilation unit')

    def test_native_entry_point_has_only_fixed_cache_paths(self):
        with mock.patch.object(seed.os, 'name', 'nt'), \
             mock.patch.dict(os.environ, {'GOCACHE': seed.DESTINATION}, clear=True), \
             mock.patch.object(seed.pathlib, 'Path', side_effect=lambda value: value):
            self.assertEqual(seed.native_paths(), (seed.SOURCE, seed.DESTINATION))

    def test_native_receipt_binds_validated_source_commit(self):
        output = self.root / 'artifacts/windows-preview-evidence'
        output.mkdir(parents=True)
        commit = 'b' * 40
        with mock.patch('sys.argv', ['seed', '--root', str(self.root)]), \
             mock.patch.object(seed, 'native_paths', return_value=(self.source, self.target)), \
             mock.patch.dict(os.environ, {'CIRCLE_SHA1': commit}), \
             mock.patch.object(seed.subprocess, 'check_output', return_value=commit), \
             mock.patch.object(seed, 'verify_key', return_value=KEY), \
             mock.patch('builtins.print'):
            seed.main()
        receipt = json.loads((output / 'public-cache-seed.json').read_text())
        self.assertEqual(receipt['source_commit'], commit)
        self.assertEqual(receipt['public_cache_key'], KEY)
        self.assertTrue(receipt['source_unchanged'])

    def test_native_main_refuses_stale_facts_before_copy(self):
        output = self.root / 'artifacts/windows-preview-evidence'
        output.mkdir(parents=True)
        with mock.patch('sys.argv', ['seed', '--root', str(self.root)]), \
             mock.patch.object(seed, 'native_paths', return_value=(self.source, self.target)), \
             mock.patch.dict(os.environ, {'CIRCLE_SHA1': 'a' * 40}), \
             mock.patch.object(seed.subprocess, 'check_output', return_value='a' * 40), \
             mock.patch.object(seed, 'verify_key', side_effect=seed.SeedError('stale')), \
             mock.patch.object(seed, 'seed_cache') as copy:
            with self.assertRaises(SystemExit):
                seed.main()
            copy.assert_not_called()
        self.assertFalse(self.target.exists())
        self.assertFalse((output / 'public-cache-seed.json').exists())

    def test_native_paths_reject_non_windows(self):
        with mock.patch.object(seed.os, 'name', 'posix'), self.assertRaises(seed.SeedError):
            seed.native_paths()

    def test_native_paths_reject_wrong_destination_or_public_modules(self):
        for env in ({'GOCACHE': seed.SOURCE}, {'GOCACHE': seed.DESTINATION,
                    'GOMODCACHE': r'C:\ci\public-viewer-modules'}):
            with mock.patch.object(seed.os, 'name', 'nt'), mock.patch.dict(os.environ, env, clear=True):
                with self.assertRaises(seed.SeedError):
                    seed.native_paths()


class FreshKeyTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir=STAGE)
        self.addCleanup(self.temp.cleanup)
        self.work = pathlib.Path(self.temp.name)
        self.raw = inventory()
        self.packages = b'gcc 1.2.3\npython 3.12.0\n'
        self.version = 'go version go1.26.9 windows/amd64'
        self.compiler = b'gcc version 1.2.3'
        self.digest, self.facts = key(self.raw, self.packages, self.version, self.compiler)
        (self.work / 'public-cache-key.txt').write_text(self.digest + '\n')
        (self.work / 'public-cache-inputs.json').write_text(json.dumps(self.facts))
        (self.work / 'packages.txt').write_bytes(self.packages)

    def verify(self, values=None):
        values = values or [self.raw, self.packages, self.version, self.compiler]
        with mock.patch.object(seed.subprocess, 'check_output', side_effect=values) as run:
            result = seed.verify_key(self.work, self.work)
            self.assertEqual([call.args[0][:2] for call in run.call_args_list],
                             [['git', 'ls-tree'], ['pacman', '-Q'], ['go', 'version'], ['gcc', '-v']])
            return result

    def test_fresh_fact_match(self):
        self.assertEqual(self.verify(), self.digest)

    def test_changed_fresh_facts_rejected(self):
        cases = [self.raw.replace(b'a' * 40, b'b' * 40, 1),
                 b'gcc 1.2.4\npython 3.12.0\n', 'go version go1.26.8 windows/amd64', b'gcc version 1.2.4']
        for i, changed in enumerate(cases):
            values = [self.raw, self.packages, self.version, self.compiler]
            values[i] = changed
            with self.subTest(i=i), self.assertRaises((seed.SeedError, AssertionError)):
                self.verify(values)

    def test_saved_key_facts_and_packages_rejected(self):
        for name, bad in [('public-cache-key.txt', b'../malformed\n'),
                          ('public-cache-inputs.json', b'{"schema": 1}'),
                          ('packages.txt', b'gcc 1.0\n')]:
            path = self.work / name
            good = path.read_bytes()
            path.write_bytes(bad)
            with self.subTest(name=name), self.assertRaises(seed.SeedError):
                self.verify()
            path.write_bytes(good)


if __name__ == '__main__':
    unittest.main()
