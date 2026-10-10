import unittest
from windows_viewer_cache_key import AUXILIARY, PUBLIC_CLIENT, inventory, key


def entries(extra=()):
    names = ('client/go.mod', 'client/go.sum', 'client/main.go', *AUXILIARY[2:], *extra)
    raw = ''.join('100644 blob ' + 'a' * 40 + '\t' + n + '\0' for n in names)
    return (raw + '160000 commit ' + PUBLIC_CLIENT + '\tclient/moonlight-common-c\0').encode()


class PublicCacheKeyTest(unittest.TestCase):
    def test_exact_inputs_are_deterministic(self):
        args = (entries(), b'gcc 1.2.3\n', 'go version go1.26.9 windows/amd64', b'gcc 1.2.3')
        self.assertEqual(key(*args), key(*args))
        for i, changed in ((0, entries(('client/changed.go',))), (1, b'gcc 1.2.4\n'), (3, b'gcc 1.2.4')):
            new = list(args); new[i] = changed
            self.assertNotEqual(key(*args)[0], key(*new)[0])

    def test_excludes_other_roots_and_aliases(self):
        for name in ('agent/internal/sourcepreview/manager.go', '.circleci/source-snapshots/x.tar.gz',
                     'client/../private.go', '/client/absolute.go', 'client/go.mod'):
            with self.assertRaises(AssertionError): inventory(entries((name,)))

    def test_rejects_other_gitlinks_and_symlinks(self):
        for bad in (entries().replace(PUBLIC_CLIENT.encode(), b'b' * 40),
                    entries() + ('160000 commit ' + 'a' * 40 + '\tclient/unknown\0').encode(),
                    entries().replace(b'100644 blob', b'120000 blob', 1)):
            with self.assertRaises(AssertionError): inventory(bad)

    def test_rejects_missing_inventory_and_wrong_toolchain(self):
        with self.assertRaises(AssertionError): inventory(b'')
        with self.assertRaises(AssertionError): key(entries(), b'gcc 1.2.3\n', 'go1.26.6', b'gcc')
        with self.assertRaises(AssertionError): key(entries(), b'credential=value\n', 'go version go1.26.9 windows/amd64', b'gcc')


if __name__ == '__main__': unittest.main()
