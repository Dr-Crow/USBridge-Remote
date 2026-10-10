"""Closed, non-sensitive failure categories for the private native fixture."""
import pathlib

FILES = {'preview_engine.py', 'engine_preview_container.py', 'engine_preview_contract.py', 'preview_windows.py'}
FUNCTIONS = {'<module>', 'main', 'preflight', 'validate_receipt', 'wait_for', 'window_id', 'parent_window', 'parent_nonblank', 'click', 'enter', 'updated', 'idle', 'start_and_view', 'joined', 'exited', 'baseline_ready', 'assert_no_stock', 'probe', 'verify_media_shape', 'process_env', 'start_cli', 'mapped', 'wm_close', 'tray_event', 'fill_form', 'open_dialog', 'other'}
KINDS = {'AssertionError', 'CalledProcessError', 'TimeoutExpired', 'FileNotFoundError', 'PermissionError', 'RuntimeError', 'KeyError', 'OtherError'}
PHASES = {'unknown', 'idle', 'approval_required', 'verifying', 'viewer_unavailable', 'connecting', 'viewing', 'stopping', 'stopped', 'failed', 'start_failed', 'viewer_failed'}
MEDIA = {'source-streamer', 'source-preview-viewer', 'ffmpeg'}
PREFIXES = [('No capture', 'idle'), ('Approve the exact', 'approval_required'), ('Verifying components', 'verifying'), ('verified preview viewer', 'viewer_unavailable'), ('Connecting to the native', 'connecting'), ('Previewing actual frames', 'viewing'), ('Stopping the viewer', 'stopping'), ('Preview stopped', 'stopped'), ('The preview failed', 'failed'), ('source preview could not', 'start_failed'), ('preview renderer could not', 'viewer_failed')]


def validate_failure(value):
    assert type(value) is dict and set(value) == {'schema_version', 'event', 'stage', 'category', 'locations', 'ui_phase', 'media_counts'}
    assert type(value['schema_version']) is int and value['schema_version'] == 1 and value['event'] == 'engine_acceptance_failed'
    assert type(value['stage']) is int and 0 <= value['stage'] <= 10
    assert value['category'] in KINDS and value['ui_phase'] in PHASES
    assert type(value['locations']) is list and len(value['locations']) <= 4
    for frame in value['locations']:
        assert type(frame) is dict and set(frame) == {'file', 'function', 'line'}
        assert frame['file'] in FILES and frame['function'] in FUNCTIONS
        assert type(frame['line']) is int and 1 <= frame['line'] <= 2000
    assert type(value['media_counts']) is dict and set(value['media_counts']) == MEDIA
    assert all(type(n) is int and 0 <= n <= 64 for n in value['media_counts'].values())
    return value


def failure(error, stage, metadata=None, media_counts=None):
    category = type(error).__name__
    locations = []
    tb = error.__traceback__
    while tb is not None:
        file = pathlib.Path(tb.tb_frame.f_code.co_filename).name
        function = tb.tb_frame.f_code.co_name
        if file in FILES and 1 <= tb.tb_lineno <= 2000:
            locations.append({'file': file, 'function': function if function in FUNCTIONS else 'other', 'line': tb.tb_lineno})
        tb = tb.tb_next
    phase = 'unknown'
    controls = metadata.get('controls', []) if isinstance(metadata, dict) else []
    for control in controls[:64] if isinstance(controls, list) else []:
        if isinstance(control, dict) and control.get('id') == 'status':
            text = control.get('text', '')
            if isinstance(text, str):
                phase = next((name for prefix, name in PREFIXES if text.startswith(prefix)), 'unknown')
    counts = {name: 0 for name in MEDIA}
    for name, count in (media_counts if isinstance(media_counts, dict) else {}).items():
        if name in MEDIA and type(count) is int and 0 <= count <= 64:
            counts[name] = count
    # Never include exception arguments/messages, raw paths, argv, config,
    # descriptors, keys, source lines, frame locals, PIDs, logs, or pixel data.
    return validate_failure({'schema_version': 1, 'event': 'engine_acceptance_failed',
                             'stage': stage if type(stage) is int and 0 <= stage <= 10 else 0,
                             'category': category if category in KINDS else 'OtherError',
                             'locations': locations[-4:], 'ui_phase': phase, 'media_counts': counts})
