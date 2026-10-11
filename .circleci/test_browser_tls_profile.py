import os
import pathlib
import tempfile
import unittest
from unittest import mock
from browser_tls_profile import EphemeralBrowserTLS, ProfileError, browser_environment, _write_private, failure_code

class ProfileTests(unittest.TestCase):
    def test_refuses_non_ci_before_generating_or_trusting(self):
        with mock.patch.dict(os.environ, {}, clear=True), mock.patch('browser_tls_profile._certificates') as generate:
            with self.assertRaisesRegex(ProfileError, 'isolated_circleci_linux_required'):
                EphemeralBrowserTLS().__enter__()
            generate.assert_not_called()
    def test_preparation_targets_only_fresh_private_home_and_exact_nss_commands(self):
        commands=[]
        with mock.patch.dict(os.environ, {'CIRCLECI':'true'}), mock.patch.object(EphemeralBrowserTLS,'_tool_identity'), mock.patch('browser_tls_profile._certificates', return_value={'validity_seconds':3600}), mock.patch.object(EphemeralBrowserTLS,'_run',lambda _,args:commands.append(args)):
            with EphemeralBrowserTLS() as fixture:
                root=fixture.root
                self.assertEqual(root.stat().st_mode&0o777,0o700)
                self.assertNotEqual(fixture.browser_env['HOME'],os.environ.get('HOME'))
                db='sql:'+str(root/'home/.pki/nssdb')
                self.assertEqual(commands,[['-N','--empty-password','-d',db],['-A','-d',db,'-n','usbridge-one-hour-loopback','-t','C,,','-i',str(root/'ca.pem')]])
                fixture.confirm_joined()
            self.assertFalse(root.exists())
    def test_failed_preparation_removes_temporary_material(self):
        roots=[]
        def fail(root):
            roots.append(root);(root/'fixture-key').write_bytes(b'fixture');raise ProfileError('fixture failure')
        with mock.patch.dict(os.environ, {'CIRCLECI':'true'}), mock.patch.object(EphemeralBrowserTLS,'_tool_identity'), mock.patch('browser_tls_profile._certificates', side_effect=fail):
            with self.assertRaisesRegex(ProfileError,'fixture failure'):EphemeralBrowserTLS().__enter__()
        self.assertEqual(len(roots),1);self.assertFalse(roots[0].exists())
    def test_failure_category_does_not_publish_native_errors_or_unknown_codes(self):
        self.assertEqual(failure_code(ProfileError('browser_cdp_attach_failed')),'browser_cdp_attach_failed')
        for error in (OSError('ws://127.0.0.1/private'),ProfileError('privateword'),RuntimeError('private')):
            self.assertEqual(failure_code(error),'unclassified_fixture_failure')

    def test_environment_contains_no_parent_secrets_or_trust_override(self):
        with tempfile.TemporaryDirectory() as p:
            root=pathlib.Path(p)
            with mock.patch.dict(os.environ, {'SECRET':'x','SSL_CERT_FILE':'bad','HTTPS_PROXY':'bad'}):
                env=browser_environment(root)
            self.assertNotIn('SECRET',env); self.assertNotIn('SSL_CERT_FILE',env); self.assertNotIn('HTTPS_PROXY',env)
            self.assertEqual(env['HOME'],str(root/'home'));self.assertEqual(env['XDG_DATA_HOME'],str(root/'home/.local/share'))
    def test_private_write_is_exclusive_and_mode_bounded(self):
        with tempfile.TemporaryDirectory() as p:
            path=pathlib.Path(p)/'key';_write_private(path,b'fixture')
            self.assertEqual(path.stat().st_mode&0o777,0o600)
            with self.assertRaises(FileExistsError):_write_private(path,b'replacement')
    def test_unverified_join_deletes_secrets_but_cannot_pass(self):
        root=pathlib.Path(tempfile.mkdtemp());(root/'secret').write_bytes(b'fixture')
        fixture=EphemeralBrowserTLS();fixture.root=root
        with self.assertRaisesRegex(ProfileError,'browser_server_join_unverified'):fixture.__exit__(None,None,None)
        self.assertFalse(root.exists());self.assertFalse(fixture.receipt['browser_server_joined']);self.assertEqual(fixture.receipt['result'],'join_unverified')
    def test_verified_join_and_cleanup_produce_closed_receipt(self):
        root=pathlib.Path(tempfile.mkdtemp());fixture=EphemeralBrowserTLS();fixture.root=root;fixture.confirm_joined();fixture.__exit__(None,None,None)
        self.assertFalse(root.exists());self.assertTrue(fixture.receipt['profile_removed']);self.assertEqual(fixture.receipt['result'],'passed')
    def test_cleanup_failure_never_produces_pass(self):
        with tempfile.TemporaryDirectory() as p:
            fixture=EphemeralBrowserTLS();fixture.root=pathlib.Path(p);fixture.confirm_joined()
            with mock.patch('browser_tls_profile.shutil.rmtree',side_effect=OSError()):
                with self.assertRaisesRegex(ProfileError,'profile_cleanup_failed'):fixture.__exit__(None,None,None)
            self.assertEqual(fixture.receipt['result'],'cleanup_failed')

if __name__=='__main__':unittest.main()
