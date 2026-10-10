import ast
import json
import pathlib
import re
import subprocess
import types
import tempfile
import unittest
from unittest import mock
from engine_preview_contract import BASE_IMAGE_SHA256, FLAGS, FALSE_FLAGS, HASHES, PRIVATE_MOUNTS, validate_receipt, validate_private_mounts, private_mount_receipt, tcp_udp_tables
from preview_windows import parent_window
from engine_preview_diagnostics import failure, validate_failure

HERE = pathlib.Path(__file__).parent


def receipt():
    value = {key: key not in FALSE_FLAGS for key in FLAGS}
    value.update({key: 'a' * (40 if key == 'commit' else 64) for key in HASHES})
    value['base_image_sha256'] = BASE_IMAGE_SHA256
    value.update(instrumentation='source_preview_engine_acceptance/read-only-v1',
                 schema_version=1, preview_runs=4, pixel_samples=24, lifetime_seconds=83)
    value['private_mounts'] = {
        path: dict(type='tmpfs', rw=True, nosuid=True, nodev=True, noexec=noexec,
                   mode=mode, uid=owner, gid=owner, size_bytes=size)
        for path, (noexec, mode, owner, size) in PRIVATE_MOUNTS.items()
    }
    return value


class EngineReceiptTests(unittest.TestCase):
    def test_exact_schema(self):
        validate_receipt(receipt())
        for key, value in [('descriptor', {}), ('log', 'private'), ('config', 'private')]:
            data = receipt(); data[key] = value
            with self.assertRaises(AssertionError):
                validate_receipt(data)

    def test_success_requires_every_claim_and_bound(self):
        for key in FLAGS - FALSE_FLAGS - {'passed'}:
            value = receipt(); value[key] = False
            with self.assertRaises(AssertionError):
                validate_receipt(value)
        for key, invalid in [('lifetime_seconds', 75), ('pixel_samples', 19), ('preview_runs', 3), ('manifest_sha256', 'private'), ('commit', 'a'*64), ('production_parity', True), ('input_consent', True)]:
            value = receipt(); value[key] = invalid
            with self.assertRaises(AssertionError):
                validate_receipt(value)

    def test_free_text_and_bool_numbers_rejected(self):
        for key, invalid in [('stock_engine_idle_throughout', 'yes'), ('preview_runs', True), ('instrumentation', 'anything else')]:
            value = receipt(); value[key] = invalid
            with self.assertRaises(AssertionError):
                validate_receipt(value)

    def test_base_pin_is_fixed_and_distinct_from_runtime_identity(self):
        data = receipt()
        self.assertNotEqual(data['base_image_sha256'], data['runtime_image_sha256'])
        data['base_image_sha256'] = data['runtime_image_sha256']
        with self.assertRaises(AssertionError):
            validate_receipt(data)
        data = receipt()
        data['runtime_image_sha256'] = BASE_IMAGE_SHA256
        with self.assertRaises(AssertionError):
            validate_receipt(data)
        dockerfile = (HERE/'engine-preview.Dockerfile').read_text()
        self.assertIn('FROM ubuntu:22.04@sha256:' + BASE_IMAGE_SHA256 + '\nUSER root\n', dockerfile)
        self.assertIn('USER 10001:10001', dockerfile)
        self.assertNotIn('ARG RUNTIME_BASE_IMAGE', dockerfile)
        runner = (HERE.parent/'test-source-preview-engine.sh').read_text()
        self.assertNotIn('docker pull', runner)
        self.assertNotIn('.RepoDigests', runner)
        self.assertIn('docker build --platform linux/amd64', runner)

    def test_proc_parser_tracks_socket_identity(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp); (root/'net').mkdir()
            for name in ('tcp', 'tcp6', 'udp', 'udp6'):
                (root/'net'/name).write_text('header\n')
            (root/'net/tcp').write_text('header\n 0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 10001 0 12345 1\n')
            self.assertEqual(tcp_udp_tables(root), {'12345': {'proto': 'tcp', 'local': '0100007F:1F90', 'remote': '00000000:0000', 'state': '0A'}})

    def test_only_approved_nested_staging_mount_can_execute(self):
        mounts = receipt()['private_mounts']
        validate_private_mounts(mounts)
        self.assertEqual([path for path, value in mounts.items() if not value['noexec']], ['/work/state/source-preview'])
        for path in mounts:
            for key, invalid in [('noexec', not mounts[path]['noexec']), ('nosuid', False), ('nodev', False), ('rw', False), ('size_bytes', mounts[path]['size_bytes'] + 4096), ('uid', 123), ('mode', 0o777), ('type', 'overlay')]:
                changed = json.loads(json.dumps(mounts)); changed[path][key] = invalid
                with self.assertRaises(AssertionError):
                    validate_private_mounts(changed)
        mounts['/unapproved'] = mounts['/work'].copy()
        with self.assertRaises(AssertionError):
            validate_private_mounts(mounts)

    def test_mount_receipt_uses_kernel_flags_and_capacity(self):
        rows = []
        for path, (noexec, mode, owner, size) in PRIVATE_MOUNTS.items():
            flags = 'rw,nosuid,nodev' + (',noexec' if noexec else '')
            rows.append(f'1 2 0:42 / {path} {flags} - tmpfs tmpfs rw'.split())
        def info(node):
            noexec, mode, owner, size = PRIVATE_MOUNTS[str(node)]
            return types.SimpleNamespace(st_mode=mode, st_uid=owner, st_gid=owner)
        def capacity(path):
            return types.SimpleNamespace(f_frsize=4096, f_blocks=PRIVATE_MOUNTS[path][3] // 4096)
        with mock.patch('engine_preview_contract.pathlib.Path.stat', info), mock.patch('engine_preview_contract.pathlib.Path.is_symlink', return_value=False), mock.patch('engine_preview_contract.os.statvfs', side_effect=capacity):
            self.assertEqual(private_mount_receipt(rows), receipt()['private_mounts'])
            # Docker silently retaining its default noexec must fail preflight.
            rows[-1][5] += ',noexec'
            with self.assertRaises(AssertionError):
                private_mount_receipt(rows)
            rows[-1][5] = rows[-1][5].removesuffix(',noexec')
            for path in ('/work/extra', '/work/state/source-preview/extra', '/tmp/extra', '/run/extra'):
                with self.assertRaises(AssertionError):
                    private_mount_receipt(rows + [f'1 2 0:43 / {path} rw,nosuid,nodev - tmpfs tmpfs rw'.split()])

    def test_native_driver_does_not_import_offline_facade(self):
        source = (HERE/'preview_engine.py').read_text()
        ast.parse(source)
        self.assertNotIn('source-preview-ui-acceptance', source)
        self.assertNotIn('snapshot_counts', source)
        self.assertIn("[binary, '--strict-lan']", source)
        self.assertIn("tray_event('Quit')", source)
        self.assertIn('validate_capture_chain(children, arguments)', source)
        observer = (HERE.parents[1]/'agent/internal/ui/source_preview_engine_observer_linux.go').read_text()
        self.assertNotIn('fyne.io/fyne/v2/test', observer)
        self.assertNotIn('CreateRenderer(', observer)
        self.assertNotIn('WidgetRenderer(', observer)

    def test_hardened_runner_and_no_fetch_or_artifact_archive(self):
        source = (HERE.parent/'test-source-preview-engine.sh').read_text()
        for required in ('--network none', '--read-only', '--user 10001:10001', '--cap-drop ALL', '--security-opt no-new-privileges=true', '--ipc private', '--pids-limit', '--tmpfs /tmp:', '--tmpfs /run:', '--tmpfs /work:', '--tmpfs /tmp/.X11-unix:'):
            self.assertIn(required, source)
        for forbidden in ('--privileged', '--pid host', '--network host', '--device ', '--group-add', 'docker push', 'docker save', 'git clone', 'curl ', 'wget ', 'tar '):
            self.assertNotIn(forbidden, source)
        self.assertIn('--tmpfs /work:rw,nosuid,nodev,noexec,mode=0700,uid=10001,gid=10001,size=768m', source)
        self.assertIn('--tmpfs /work/state:rw,nosuid,nodev,noexec,mode=0700,uid=10001,gid=10001,size=64m', source)
        self.assertIn('--tmpfs /work/state/source-preview:rw,nosuid,nodev,exec,mode=0700,uid=10001,gid=10001,size=256m', source)
        self.assertEqual(len(re.findall(r'(?<!no)\bexec,', source)), 1)

class EngineWindowTests(unittest.TestCase):
    def setUp(self):
        # Load only pure lookup functions; never import the native driver's
        # display-opening module or execute any application/widget callback.
        tree = ast.parse((HERE/'preview_engine.py').read_text())
        functions = [node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name in {'window_id', 'mapped'}]
        self.command = mock.Mock(return_value='1234')
        self.scope = dict(command=self.command, re=re, subprocess=subprocess, parent_window=parent_window,
                          process=types.SimpleNamespace(pid=42),
                          ui=lambda: {'title': 'USBridge (Agent).+'}, known_parent_windows={})
        exec(compile(ast.Module(body=functions, type_ignores=[]), '<pure-window-functions>', 'exec'), self.scope)

    def test_filters_require_pid_literal_title_and_visibility(self):
        self.assertEqual(self.scope['window_id'](), '1234')
        self.command.assert_called_once_with('xdotool', 'search', '--all', '--onlyvisible', '--pid', '42', '--name', '^' + re.escape('USBridge (Agent).+') + '$')
        self.assertEqual(self.scope['known_parent_windows'], {42: '1234'})

    def test_ambiguity_is_never_accepted(self):
        self.command.return_value = '1234\n5678'
        for allow_missing in (False, True):
            with self.assertRaises(AssertionError):
                self.scope['window_id'](allow_missing=allow_missing)
        self.assertEqual(self.scope['known_parent_windows'], {})

    def test_missing_is_retryable_only_during_startup(self):
        self.command.side_effect = subprocess.CalledProcessError(1, 'xdotool')
        self.assertIsNone(self.scope['window_id'](allow_missing=True))
        with self.assertRaises(AssertionError):
            self.scope['window_id']()
        self.command.side_effect = subprocess.CalledProcessError(2, 'xdotool')
        with self.assertRaises(subprocess.CalledProcessError):
            self.scope['window_id'](allow_missing=True)

    def test_parent_identity_cannot_change(self):
        self.scope['window_id']()
        self.command.return_value = '5678'
        with self.assertRaises(AssertionError):
            self.scope['window_id']()
        self.assertEqual(self.scope['known_parent_windows'], {42: '1234'})

    def test_hidden_parent_uses_previously_verified_xid(self):
        self.scope['window_id']()
        self.command.reset_mock()
        self.command.return_value = 'Map State: IsUnMapped'
        self.assertFalse(self.scope['mapped']())
        self.command.assert_called_once_with('xwininfo', '-id', '1234')
        self.command.return_value = 'Map State: IsViewable'
        self.assertTrue(self.scope['mapped']())
        self.scope['process'].pid = 99
        with self.assertRaises(AssertionError):
            self.scope['mapped']()


class FailureDiagnosticTests(unittest.TestCase):
    def test_exception_values_and_arbitrary_metadata_are_never_copied(self):
        error = KeyError('secret-key-or-config')
        value = failure(error, 4, {'controls': [{'id': 'status', 'text': 'The preview failed: secret-key-or-config'}], 'key': 'secret-key-or-config'}, {'ffmpeg': 2, 'secret-key-or-config': 123})
        self.assertNotIn('secret-key-or-config', json.dumps(value))
        self.assertEqual(value['category'], 'KeyError')
        self.assertEqual(value['ui_phase'], 'failed')
        self.assertEqual(value['media_counts']['ffmpeg'], 2)
        self.assertEqual(value['locations'], [])

    def test_only_owned_code_locations_are_reported(self):
        namespace = {}
        exec(compile('def wait_for():\n    raise AssertionError("private-exception-message")\n', '/opt/gate/preview_engine.py', 'exec'), namespace)
        try:
            namespace['wait_for']()
        except AssertionError as error:
            value = failure(error, 4)
        self.assertEqual(value['locations'], [{'file': 'preview_engine.py', 'function': 'wait_for', 'line': 2}])
        self.assertNotIn('private-exception-message', json.dumps(value))

    def test_unknown_fields_and_free_text_are_rejected(self):
        for key, value in [('message', 'private'), ('category', 'arbitrary'), ('ui_phase', 'private'), ('stage', True)]:
            data = failure(AssertionError('private'), 4); data[key] = value
            with self.assertRaises(AssertionError):
                validate_failure(data)
        data = failure(AssertionError(), 4)
        data['locations'] = [{'file': '/private/path', 'function': 'wait_for', 'line': 2}]
        with self.assertRaises(AssertionError):
            validate_failure(data)


class StageTests(unittest.TestCase):
    def setUp(self):
        import prepare_engine_preview
        self.stage = prepare_engine_preview.stage

    def test_only_exact_binaries_staged_without_source_tree(self):
        import hashlib
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            ctx = root/'context'; (ctx/'gate').mkdir(parents=True); (ctx/'agent').mkdir()
            for file in ('usbridge-agent', 'usbridge-agent-observed'):
                (ctx/'agent'/file).write_bytes(b'plain fixture binary')
            prior = root/'artifacts/source-preview'
            components = prior/'package/components'; (components/'bin').mkdir(parents=True)
            (prior/'package/source').mkdir(); (prior/'package/source/private.go').write_text('must never be copied')
            commit = 'a'*40; manifest = {'schema': 1, 'components': []}; files = {}
            for name, version, profile in [('source-streamer', '2e07af3484369bc68bc8091d5a04969867eff0f9', 'source-streamer-v1'), ('source-preview-viewer', commit, 'source-preview-v1')]:
                data = name.encode(); sha = hashlib.sha256(data).hexdigest(); path = 'bin/'+name
                (components/path).write_bytes(data)
                manifest['components'].append(dict(name=name, platform='linux/amd64', version=version, profile=profile, entry=path, files=[dict(path=path, size=len(data), sha256=sha, executable=True)]))
                files['components/'+path] = sha
            raw = json.dumps(manifest).encode(); (components/'manifest.json').write_bytes(raw)
            (components/'MANIFEST.sha256').write_text(hashlib.sha256(raw).hexdigest()+'  manifest.json\n')
            files['components/manifest.json'] = hashlib.sha256(raw).hexdigest()
            (prior/'result.json').write_text(json.dumps(dict(passed=True, presented_pixels_verified=True, inherited_frame_dumps_disabled=True)))
            (prior/'package/BUILD-PROVENANCE.json').write_text(json.dumps(dict(agent_client_commit=commit, native_viewer_presented_pixels_tested=True, source_streamer_commit='2e07af3484369bc68bc8091d5a04969867eff0f9', public_client_pin='a232e27d5c423eb8e7de8da91f0eefcf9171348f', files_sha256=files)))
            self.stage(root, ctx, commit)
            self.assertEqual({p.relative_to(ctx/'components').as_posix() for p in (ctx/'components').rglob('*') if p.is_file()}, {'manifest.json', 'bin/source-streamer', 'bin/source-preview-viewer'})
            self.assertFalse((ctx/'source').exists())


if __name__ == '__main__':
    unittest.main()
