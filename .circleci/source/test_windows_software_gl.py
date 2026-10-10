import copy
import json
import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock

import windows_software_gl as gl
import windows_preview_build_receipt as build


class SoftwareGraphicsTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.viewer = self.root / 'viewer.exe'
        self.viewer.write_bytes(b'public viewer')
        self.runtime = self.root / 'runtime.dll'
        self.runtime.write_bytes(b'public runtime')
        self.base = {'commit': 'a'*40, 'viewer_sha256': gl.sha(self.viewer),
                     'runtime_dlls_sha256': {'runtime.dll': gl.sha(self.runtime)}}

    def test_baseline_remains_hash_checked(self):
        self.assertEqual(gl.verified_viewer_dependencies(self.base, self.root, self.viewer), self.base['runtime_dlls_sha256'])
        self.runtime.write_bytes(b'changed')
        with self.assertRaises(AssertionError): gl.verified_viewer_dependencies(self.base, self.root, self.viewer)

    def test_software_receipt_binds_package_commit_and_all_files(self):
        fake_pins = {}
        for name in gl.DLLS:
            p = self.root/name; p.write_bytes(name.encode()); fake_pins[name] = gl.sha(p)
        value = {'schema_version': 1, 'passed': True, 'commit': self.base['commit'],
                 'viewer_sha256': self.base['viewer_sha256'], 'mesa_package': gl.PACKAGE,
                 'mesa_version': gl.VERSION, 'mesa_archive_sha256': gl.ARCHIVE_SHA,
                 'fixed_driver': 'llvmpipe', 'fixed_software': True,
                 'runtime_dlls_sha256': self.base['runtime_dlls_sha256'] | fake_pins}
        receipt = self.root/'software-graphics-inputs.json'
        def check(v):
            receipt.write_text(json.dumps(v))
            return gl.verified_viewer_dependencies(self.base, self.root, self.viewer)
        with mock.patch.object(gl, 'DLLS', fake_pins):
            self.assertEqual(check(value), value['runtime_dlls_sha256'])
            for key, bad in [('commit','b'*40), ('viewer_sha256','b'*64), ('mesa_version','other'),
                             ('mesa_archive_sha256','b'*64), ('fixed_driver','zink'), ('fixed_software',False)]:
                wrong = copy.deepcopy(value); wrong[key] = bad
                with self.subTest(key=key), self.assertRaises(AssertionError): check(wrong)
            wrong = copy.deepcopy(value); del wrong['runtime_dlls_sha256']['runtime.dll']
            with self.assertRaises(AssertionError): check(wrong)
            wrong = copy.deepcopy(value); wrong['runtime_dlls_sha256']['opengl32.dll']='b'*64
            with self.assertRaises(AssertionError): check(wrong)

    def test_unsafe_missing_symlink_and_size_fail(self):
        for files in [{'../runtime.dll':'a'*64}, {'runtime.dll':'bad'}, {'missing.dll':'a'*64}]:
            with self.subTest(files=files), self.assertRaises((AssertionError,FileNotFoundError)):
                gl.checked_files(self.root,files)
        link=self.root/'linked.dll';link.symlink_to(self.runtime)
        with self.assertRaises(AssertionError):gl.checked_files(self.root,{'linked.dll':gl.sha(self.runtime)})
        with mock.patch.object(pathlib.Path, 'lstat', return_value=type('Info',(),{'st_file_attributes':0x400, 'st_mode':0o100644, 'st_size':14})()):
            with self.assertRaises(AssertionError):gl.checked_files(self.root,self.base['runtime_dlls_sha256'])

    def test_existing_packages_cannot_change(self):
        self.assertEqual(gl.unchanged_existing_packages('gcc 1\npython 2\n','gcc 1\npython 2\nllvm 3\n'), [{'name':'llvm','version':'3'}])
        for after in ['gcc 2\npython 2\n', 'gcc 1\n', 'gcc 1\ngcc 1\n', 'gcc 1\npython 2\nbad /path\n']:
            with self.assertRaises(AssertionError):gl.unchanged_existing_packages('gcc 1\npython 2\n',after)

    def test_extract_rejects_unpinned_archive_before_tar(self):
        p=self.root/'archive.zst';p.write_bytes(b'wrong')
        with mock.patch.object(gl.subprocess,'run') as call, self.assertRaises(AssertionError):gl.extract_pinned(p,self.root)
        call.assert_not_called()

    def test_dynamic_root_uses_only_explicit_extra_source(self):
        ucrt=self.root/'ucrt';system=self.root/'system';ucrt.mkdir();system.mkdir()
        mesa=self.root/'opengl32.dll';gallium=self.root/'libgallium_wgl.dll'
        mesa.write_bytes(b'mesa');gallium.write_bytes(b'gallium');(system/'KERNEL32.dll').write_bytes(b'system')
        imports={'opengl32.dll':['libgallium_wgl.dll'], 'libgallium_wgl.dll':['KERNEL32.dll']}
        def run(args,**kwargs):
            return subprocess.CompletedProcess(args,0,'\n'.join('DLL Name: '+name for name in imports[pathlib.Path(args[-1]).name]),'')
        with mock.patch.object(build.subprocess,'run',side_effect=run):
            copied, systems = build.stage_dependencies(mesa,ucrt,system,{'libgallium_wgl.dll':gallium})
        self.assertEqual(copied,{'libgallium_wgl.dll':gl.sha(gallium)})
        self.assertEqual(systems,['KERNEL32.dll'])


if __name__ == '__main__':unittest.main()
