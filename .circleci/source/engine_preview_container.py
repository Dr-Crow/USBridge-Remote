#!/usr/bin/env python3
"""Own all generated desktops and state inside a hardened disposable container."""
import json
import os
import pathlib
import re
import secrets
import subprocess
import sys
import time
from engine_preview_contract import BASE_IMAGE_SHA256, preflight, validate_receipt

ROOT = pathlib.Path('/work')

def setup_state(base):
    for name in ('home', 'config/usbridge-agent', 'cache', 'data', 'runtime', 'state', 'logs', 'cwd', 'tmp'):
        (base / name).mkdir(mode=0o700, parents=True, exist_ok=True)
    template = pathlib.Path('/opt/gate/engine-preview-config.yaml').read_text()
    (base / 'config/usbridge-agent/config.yaml').write_text(template.replace('@STATE@', str(base / 'state')))


def main():
    os.umask(0o077)
    preflight()
    setup_state(ROOT)
    setup_state(ROOT / 'plain')
    auth = ROOT / 'xauthority'
    auth.touch(mode=0o600)
    servers = []
    try:
        for display, size in ((':96', '128x72x24'), (':97', '1280x960x24')):
            assert not pathlib.Path('/tmp/.X' + display[1:] + '-lock').exists()
            subprocess.run(['xauth', '-f', str(auth), 'add', display, '.', secrets.token_hex(16)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            log = (ROOT / ('xvfb-' + display[1:] + '.log')).open('w')
            servers.append(subprocess.Popen(['Xvfb', display, '-noreset', '-screen', '0', size, '-nolisten', 'tcp', '-auth', str(auth)], stdout=log, stderr=log))
            log.close()
        for display in (':96', ':97'):
            env = dict(os.environ, DISPLAY=display)
            for _ in range(100):
                if subprocess.run(['xdpyinfo'], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                    break
                time.sleep(.05)
            else:
                raise AssertionError('generated display not ready')
        capture_env = dict(os.environ, DISPLAY=':96')
        subprocess.run(['xsetroot', '-solid', '#164cb4'], env=capture_env, check=True)
        subprocess.run(['xdotool', 'mousemove', '0', '0'], env=capture_env, check=True)
        with (ROOT / 'driver.log').open('w') as log:
            subprocess.run(['timeout', '--kill-after=10s', '240s', 'dbus-run-session', '--', '/usr/bin/python3', '/opt/gate/preview_engine.py'], stdout=log, stderr=log, check=True)
        result = json.loads((ROOT / 'result.json').read_text())
    finally:
        for server in servers:
            if server.poll() is None:
                server.terminate()
        for server in servers:
            server.wait(timeout=10)
    result['sandbox_helpers_joined'] = True
    result['base_image_sha256'] = BASE_IMAGE_SHA256
    result['runtime_image_sha256'] = os.environ['RUNTIME_IMAGE_SHA256']
    validate_receipt(result)
    print(json.dumps(result, sort_keys=True))

if __name__ == '__main__':
    try:
        main()
    except BaseException:
        # Never print raw application logs, config, authentication or descriptors.
        stage = (ROOT / 'failure-stage').read_text() if (ROOT / 'failure-stage').is_file() else '0'
        stage = stage if re.fullmatch(r'(?:[0-9]|10)', stage) else '0'
        print('Engine preview gate failed; last completed stage ' + stage + '. Private diagnostics discarded.', file=sys.stderr)
        sys.exit(1)
