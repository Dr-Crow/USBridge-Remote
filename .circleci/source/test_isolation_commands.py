#!/usr/bin/env python3
"""Preflight the three isolated public-client cases without starting capture."""
import argparse
from pathlib import Path
import shlex
import sys

root = Path(__file__).resolve().parents[1]
script = root / 'source-isolation/run.sh'
parser = argparse.ArgumentParser()
for field in ('agent', 'streamer', 'client', 'output'):
    parser.add_argument('--' + field, required=True)
parser.add_argument('--pixel-format', choices=('yuv420p', 'yuv444p'), default='yuv420p')
parser.add_argument('--input-consent', action='store_true')
cases = []
for line in script.read_text().splitlines():
    if '/acceptance/test_agent_source_full_client.py ' not in line:
        continue
    args = parser.parse_args(shlex.split(line)[2:])
    cases.append((args.output, args.pixel_format, args.input_consent))
expected = [('/results/full-public-client', 'yuv420p', False),
            ('/results/full-public-client-444', 'yuv444p', False),
            ('/results/full-public-client-input', 'yuv420p', True)]
if cases != expected:
    raise SystemExit(f'isolation case matrix mismatch: {cases}')
print('isolated public-client case matrix passed')
