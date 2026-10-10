"""Exact component inventory with bounded, source-free failure diagnostics."""
import argparse
import hashlib
import json
import os
import pathlib
import re

from windows_preview_build_receipt import sha, stage_dependencies


class InventoryError(Exception):
    pass


def component_files(source, dependencies, failure_path):
    expected = {source.name: sha(source), **dependencies}
    entries = sorted(source.parent.iterdir())
    facts = []
    files = []
    invalid = len(entries) > 128
    for path in entries[:128]:
        exact = path == source or path.name in dependencies
        regular = path.is_file() and not path.is_symlink()
        bounded = regular and path.stat().st_size <= 64 << 20
        digest = sha(path) if bounded else None
        expected_digest = expected.get(source.name if path == source else path.name)
        valid = exact and bounded and digest == expected_digest
        invalid |= not valid
        safe_name = path.name if re.fullmatch(r'[A-Za-z0-9_.+-]{1,100}', path.name) else None
        facts.append({'name': safe_name, 'name_sha256': hashlib.sha256(path.name.encode()).hexdigest(),
                      'source_path_equal': path == source, 'exact_dependency_name': path.name in dependencies,
                      'casefold_dependency_name': any(path.name.casefold() == n.casefold() for n in dependencies),
                      'regular_nonsymlink': regular, 'bounded_size': bounded,
                      'expected_hash_matches': digest is not None and digest == expected_digest,
                      'sha256': digest})
        if valid:
            files.append({'path': 'bin/' + path.name, 'sha256': digest,
                          'size': path.stat().st_size, 'executable': path == source})
    if len(files) != len(expected):
        invalid = True
    if invalid:
        receipt = {'schema_version': 1, 'passed': False, 'stage': 'source_component_inventory',
                   'entry_count': len(entries), 'expected_count': len(expected), 'entries': facts,
                   'expected_dll_names': sorted(dependencies), 'raw_paths_or_contents_published': False,
                   'media_session_started': False, 'source_or_binary_artifacts_published': False}
        failure_path.write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
        raise InventoryError('source component inventory does not match the exact dependency closure')
    return files


def main():
    parser = argparse.ArgumentParser()
    for name in ('source', 'ucrt-bin', 'output'):
        parser.add_argument('--' + name, type=pathlib.Path, required=True)
    parser.add_argument('--commit', required=True)
    args = parser.parse_args()
    assert os.name == 'nt' and re.fullmatch('[0-9a-f]{40}', args.commit)
    assert args.source.is_absolute() and args.source.is_file() and not args.source.is_symlink()
    args.output.mkdir(parents=True, exist_ok=True)
    dependencies, systems = stage_dependencies(args.source, args.ucrt_bin,
                                               pathlib.Path(os.environ['SystemRoot']) / 'System32')
    files = component_files(args.source, dependencies, args.output / 'staging-failure.json')
    result = {'schema_version': 1, 'passed': True, 'stage': 'source_component_inventory',
              'commit': args.commit, 'files': files, 'system_dll_imports': systems,
              'native_windows_staging': True, 'media_session_started': False,
              'source_or_binary_artifacts_published': False}
    (args.output / 'staging.json').write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')


if __name__ == '__main__':
    main()
