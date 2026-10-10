"""Exact native-window selection for isolated acceptance displays."""
import re
import subprocess


def parent_window(command, pid, title, required=True):
    if not isinstance(pid, int) or pid <= 0 or not isinstance(title, str) or not title or '\n' in title:
        raise AssertionError('invalid native parent identity')
    # xdotool search defaults to --any (OR). --all binds both the PID and
    # literal title; visibility excludes GLFW's hidden auxiliary windows.
    args = ('xdotool', 'search', '--all', '--onlyvisible', '--pid', str(pid), '--name', '^' + re.escape(title) + '$')
    try:
        ids = command(*args).splitlines()
    except subprocess.CalledProcessError as exc:
        if exc.returncode != 1 or exc.output:
            raise
        ids = []
    if not ids and not required:
        return None
    if len(ids) != 1 or not ids[0].isdigit() or int(ids[0]) <= 0:
        raise AssertionError('native parent window missing or ambiguous')
    return ids[0]
