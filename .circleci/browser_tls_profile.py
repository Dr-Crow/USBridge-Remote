"""One-hour loopback CA scoped to a disposable CircleCI Chromium home.

This test helper never changes an existing HOME or system trust store. The
caller must join its browser/server before closing this context. It must not
cache or upload the temporary root. No insecure browser options are supplied.
"""
from __future__ import annotations
import datetime
import hashlib
import ipaddress
import os
import pathlib
import shutil
import stat
import subprocess
import sys
import tempfile


class ProfileError(RuntimeError):
    pass


def browser_environment(root: pathlib.Path) -> dict[str, str]:
    root = root.resolve(strict=True)
    return {'PATH': '/usr/bin:/bin', 'HOME': str(root / 'home'),
            'XDG_CONFIG_HOME': str(root / 'home/.config'),
            'XDG_DATA_HOME': str(root / 'home/.local/share'),
            'XDG_CACHE_HOME': str(root / 'home/.cache'),
            'TMPDIR': str(root / 'tmp'), 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8'}


def _write_private(path: pathlib.Path, data: bytes) -> None:
    with path.open('xb') as out:
        os.chmod(path, 0o600)
        out.write(data)


def _certificates(root: pathlib.Path) -> dict:
    from cryptography import x509
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import ec
    from cryptography.x509.oid import NameOID, ExtendedKeyUsageOID
    start = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0)
    end = start + datetime.timedelta(hours=1)
    ca_key = ec.generate_private_key(ec.SECP256R1())
    leaf_key = ec.generate_private_key(ec.SECP256R1())
    ca_name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'USBridge disposable loopback test CA')])
    ca = (x509.CertificateBuilder().subject_name(ca_name).issuer_name(ca_name)
          .public_key(ca_key.public_key()).serial_number(x509.random_serial_number())
          .not_valid_before(start).not_valid_after(end)
          .add_extension(x509.BasicConstraints(ca=True, path_length=0), critical=True)
          .add_extension(x509.KeyUsage(False, False, False, False, False, True, True, False, False), critical=True)
          .add_extension(x509.NameConstraints([x509.IPAddress(ipaddress.ip_network('127.0.0.1/32'))], None), critical=True)
          .sign(ca_key, hashes.SHA256()))
    leaf = (x509.CertificateBuilder()
            .subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, '127.0.0.1')]))
            .issuer_name(ca_name).public_key(leaf_key.public_key())
            .serial_number(x509.random_serial_number()).not_valid_before(start).not_valid_after(end)
            .add_extension(x509.BasicConstraints(ca=False, path_length=None), critical=True)
            .add_extension(x509.SubjectAlternativeName([x509.IPAddress(ipaddress.ip_address('127.0.0.1'))]), critical=False)
            .add_extension(x509.ExtendedKeyUsage([ExtendedKeyUsageOID.SERVER_AUTH]), critical=False)
            .add_extension(x509.KeyUsage(True, False, False, False, False, False, False, False, False), critical=True)
            .sign(ca_key, hashes.SHA256()))
    _write_private(root / 'ca.pem', ca.public_bytes(serialization.Encoding.PEM))
    _write_private(root / 'server.pem', leaf.public_bytes(serialization.Encoding.PEM))
    _write_private(root / 'server-key.pem', leaf_key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))
    # The CA private key is never serialized. Only the leaf key is handed to the
    # loopback fixture. Both key objects become unreachable on return.
    return {'ca_sha256': ca.fingerprint(hashes.SHA256()).hex(),
            'leaf_sha256': leaf.fingerprint(hashes.SHA256()).hex(),
            'validity_seconds': 3600, 'permitted_ip': '127.0.0.1',
            'ca_private_key_written': False}


class EphemeralBrowserTLS:
    def __init__(self, certutil: str = '/usr/bin/certutil'):
        self.certutil = pathlib.Path(certutil)
        self.root = None
        self.joined = False
        self.receipt = {'schema': 1, 'result': 'not_started', 'profile_removed': False,
                        'browser_server_joined': False, 'host_trust_modified': False,
                        'certificate_errors_ignored': False}

    def _tool_identity(self):
        if self.certutil != pathlib.Path('/usr/bin/certutil'):
            raise ProfileError('official_certutil_path_required')
        tool = self.certutil.stat()
        if not stat.S_ISREG(tool.st_mode) or tool.st_uid != 0 or tool.st_mode & 0o022 or not 0 < tool.st_size < 20 * 1024 * 1024:
            raise ProfileError('certutil_identity')

    def __enter__(self):
        if sys.platform != 'linux' or os.environ.get('CIRCLECI') != 'true':
            raise ProfileError('isolated_circleci_linux_required')
        self._tool_identity()
        self.root = pathlib.Path(tempfile.mkdtemp(prefix='usbridge-browser-tls-')).resolve()
        os.chmod(self.root, 0o700)
        try:
            for p in ('home/.pki/nssdb', 'home/.config', 'home/.local/share', 'home/.cache', 'tmp', 'trusted-profile', 'untrusted/home/.pki/nssdb', 'untrusted/tmp', 'untrusted/profile'):
                (self.root / p).mkdir(parents=True, mode=0o700, exist_ok=True)
            self.receipt.update(_certificates(self.root))
            self.browser_env = browser_environment(self.root)
            self.untrusted_env = browser_environment(self.root / 'untrusted')
            self.cert_file = self.root / 'server.pem'
            self.key_file = self.root / 'server-key.pem'
            self.trusted_profile = self.root / 'trusted-profile'
            self.untrusted_profile = self.root / 'untrusted/profile'
            db = 'sql:' + str(self.root / 'home/.pki/nssdb')
            self._run(['-N', '--empty-password', '-d', db])
            self._run(['-A', '-d', db, '-n', 'usbridge-one-hour-loopback', '-t', 'C,,', '-i', str(self.root / 'ca.pem')])
            self.receipt['certutil_sha256'] = hashlib.sha256(self.certutil.read_bytes()).hexdigest()
            self.receipt['result'] = 'prepared'
            return self
        except BaseException:
            shutil.rmtree(self.root)
            self.receipt['profile_removed'] = not self.root.exists()
            raise

    def _run(self, args):
        try:
            result = subprocess.run([str(self.certutil), *args], stdin=subprocess.DEVNULL,
                                    stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
                                    env=self.browser_env, timeout=15, check=False)
        except (OSError, subprocess.TimeoutExpired) as error:
            raise ProfileError('nss_command_failed') from None
        if result.returncode != 0 or result.stderr:
            raise ProfileError('nss_command_failed')

    def confirm_joined(self):
        self.joined = True
        self.receipt['browser_server_joined'] = True

    def __exit__(self, kind, value, tb):
        # Erase ephemeral credentials even on a failed fixture. A missing join
        # remains a fatal uncertainty, never a successful cleanup receipt.
        try:
            shutil.rmtree(self.root)
            self.receipt['profile_removed'] = not self.root.exists()
        except OSError:
            self.receipt['result'] = 'cleanup_failed'
            raise ProfileError('profile_cleanup_failed') from None
        if not self.joined:
            self.receipt['result'] = 'join_unverified'
            raise ProfileError('browser_server_join_unverified')
        self.receipt['result'] = 'passed' if kind is None else 'fixture_failed'
        return False
