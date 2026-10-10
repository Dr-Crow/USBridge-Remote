#!/usr/bin/env python3
"""Copy only already-built public component binaries, never a source package."""
import argparse
import hashlib
import json
import pathlib
import re
import shutil


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def stage(root, context, commit):
    assert re.fullmatch('[0-9a-f]{40}', commit)
    prior = root / 'artifacts/source-preview'
    proof = json.loads((prior / 'result.json').read_text())
    provenance = json.loads((prior / 'package/BUILD-PROVENANCE.json').read_text())
    assert proof['passed'] and proof['presented_pixels_verified'] and proof['inherited_frame_dumps_disabled']
    assert provenance['agent_client_commit'] == commit and provenance['native_viewer_presented_pixels_tested']
    assert provenance['source_streamer_commit'] == '2e07af3484369bc68bc8091d5a04969867eff0f9'
    assert provenance['public_client_pin'] == 'a232e27d5c423eb8e7de8da91f0eefcf9171348f'
    source = prior / 'package/components'
    assert source.is_dir() and not source.is_symlink()
    manifest = source / 'manifest.json'
    assert manifest.is_file() and not manifest.is_symlink() and manifest.stat().st_size <= 1 << 20
    assert not (source / 'MANIFEST.sha256').is_symlink()
    pin = (source / 'MANIFEST.sha256').read_text().split()[0]
    assert re.fullmatch('[0-9a-f]{64}', pin) and digest(manifest) == pin
    assert provenance['files_sha256']['components/manifest.json'] == pin
    data = json.loads(manifest.read_text())
    destination = context / 'components'
    destination.mkdir()
    (destination / 'bin').mkdir()
    shutil.copyfile(manifest, destination / 'manifest.json')
    hashes = {'commit': commit, 'manifest_sha256': pin}
    for name, field, profile in [('source-streamer', 'source_binary_sha256', 'source-streamer-v1'), ('source-preview-viewer', 'viewer_binary_sha256', 'source-preview-v1')]:
        matches = [c for c in data['components'] if c['name'] == name and c['platform'] == 'linux/amd64']
        assert len(matches) == 1
        component = matches[0]
        assert component['entry'] == 'bin/' + name and component['profile'] == profile
        assert component['version'] == ('2e07af3484369bc68bc8091d5a04969867eff0f9' if name == 'source-streamer' else commit)
        assert len(component['files']) == 1
        entry = component['files'][0]
        assert entry['path'] == component['entry'] and entry['executable'] is True
        src = source / entry['path']
        assert src.is_file() and not src.is_symlink() and not src.parent.is_symlink()
        assert src.stat().st_size == entry['size'] and digest(src) == entry['sha256']
        # Independently bind source and viewer to the previous gate's provenance.
        assert provenance['files_sha256']['components/' + entry['path']] == entry['sha256']
        dst = destination / entry['path']
        shutil.copyfile(src, dst)
        dst.chmod(0o755)
        hashes[field] = entry['sha256']
    for name, field in [('usbridge-agent', 'plain_binary_sha256'), ('usbridge-agent-observed', 'observed_binary_sha256')]:
        hashes[field] = digest(context / 'agent' / name)
    (context / 'gate/input-hashes.json').write_text(json.dumps(hashes, sort_keys=True) + '\n')

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('root', type=pathlib.Path)
    parser.add_argument('context', type=pathlib.Path)
    parser.add_argument('commit')
    args = parser.parse_args()
    stage(args.root, args.context, args.commit)
