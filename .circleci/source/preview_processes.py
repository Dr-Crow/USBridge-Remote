"""Validate codec roles without retaining process arguments or media."""
from collections import Counter


def validate_capture_chain(processes, arguments):
    assert Counter(processes.values()) == {'source-streamer': 1, 'source-preview-viewer': 1, 'ffmpeg': 2}, 'expected one source, one viewer and two codecs'
    roles = Counter()
    for pid, name in processes.items():
        if name != 'ffmpeg':
            continue
        args = arguments(pid)
        assert 1 < len(args) <= 128 and args[-1] == 'pipe:1'
        values = lambda flag: [args[i + 1] for i in range(len(args) - 1) if args[i] == flag]
        inputs = values('-i')
        if inputs == [':96'] and 'x11grab' in values('-f'):
            assert values('-video_size') == ['128x72'] and values('-framerate') == ['30']
            assert values('-c:v') == ['libx264'] and '-an' in args
            roles['generated_video'] += 1
        elif inputs == ['pipe:0'] and 's16le' in values('-f'):
            assert values('-ar') == ['48000'] and values('-ac') == ['2']
            assert values('-c:a') == ['libopus'] and '-vn' in args
            roles['private_pcm_audio'] += 1
        else:
            raise AssertionError('unexpected codec input role')
    assert roles == {'generated_video': 1, 'private_pcm_audio': 1}
    return dict(roles)
