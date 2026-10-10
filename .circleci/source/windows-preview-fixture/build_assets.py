#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
"""CI-only synthesis and strict decode verification; never used by the fixture.

The caller must provision FFmpeg from an approved official distribution and
record its package/version provenance. No encoder is downloaded by this script.
Only synthetic lavfi inputs are used. Generated assets must not be committed.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import struct
import subprocess

BLUE = "0x164cb4"
ORANGE = "0xdc6432"
WIDTH, HEIGHT, FPS, FRAMES = 128, 72, 30, 900
AUDIO_SECONDS = "29.995"
PCM_BYTES = 1_439_760 * 4
NAMES = ("fixture-blue.h264", "fixture-orange.h264", "fixture-silence.ogg")


def checked(ffmpeg, args, *, input=None):
    # No inherited FFREPORT or FFmpeg-specific options can request file dumps.
    env = {key: value for key, value in os.environ.items()
           if key.upper() in {"SYSTEMROOT", "WINDIR", "PATH", "TEMP", "TMP", "LANG", "LC_ALL"}}
    env["AV_LOG_FORCE_NOCOLOR"] = "1"
    result = subprocess.run([str(ffmpeg), "-hide_banner", "-nostdin", "-v", "error", *args],
                            input=input, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            env=env, check=False, timeout=90)
    if result.returncode or result.stderr:
        raise RuntimeError("synthetic media generation or strict decode failed")
    return result.stdout


def decode(ffmpeg, data, media_format, output_args):
    return checked(ffmpeg, ["-xerror", "-err_detect", "explode", "-f", media_format,
                           "-i", "pipe:0", *output_args, "pipe:1"], input=data)


def ogg_pages(blob):
    pages = []
    while blob:
        if len(blob) < 28 or blob[:5] != b"OggS\x00" or blob[26] != 1:
            raise RuntimeError("unexpected Ogg layout")
        size = 28 + blob[27]
        if size > len(blob) or blob[27] == 255:
            raise RuntimeError("unexpected Ogg lacing")
        pages.append(blob[:size])
        blob = blob[size:]
    return pages


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ffmpeg", required=True, type=Path,
                        help="Absolute approved FFmpeg executable, not a PATH lookup")
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    ffmpeg = args.ffmpeg
    if not ffmpeg.is_absolute() or ffmpeg.is_symlink() or not ffmpeg.is_file():
        parser.error("--ffmpeg must be an absolute, regular, non-symlink executable")
    output = args.output
    output.mkdir(parents=True, exist_ok=True)
    if output.is_symlink() or not output.is_dir():
        parser.error("--output must be a regular directory")
    generated = {}
    decoded_colors = []
    for name, color in zip(NAMES[:2], (BLUE, ORANGE)):
        frame = checked(ffmpeg, ["-f", "lavfi", "-i", f"color=c={color}:s=128x72:r=30",
            "-frames:v", "1", "-an", "-pix_fmt", "yuv420p", "-c:v", "libx264", "-qp", "0",
            "-preset", "ultrafast", "-tune", "zerolatency", "-x264-params",
            "aud=1:repeat-headers=1:keyint=1:min-keyint=1:scenecut=0", "-threads", "1", "-f", "h264", "pipe:1"])
        if not 0 < len(frame) <= 65536:
            raise RuntimeError("synthetic H264 asset is not bounded")
        rgb = decode(ffmpeg, frame, "h264", ["-pix_fmt", "rgb24", "-f", "rawvideo"])
        if len(rgb) != WIDTH * HEIGHT * 3 or rgb != rgb[:3] * (WIDTH * HEIGHT):
            raise RuntimeError("synthetic H264 color or dimensions invalid")
        generated[name] = frame
        decoded_colors.append(list(rgb[:3]))
    # Decode the actual concatenated 900-frame, all-IDR, two-state byte stream.
    stream = b"".join(generated[NAMES[(i // FPS) % 2]] for i in range(FRAMES))
    frame_md5 = decode(ffmpeg, stream, "h264", ["-pix_fmt", "yuv420p", "-f", "framemd5"])
    rows = [line.split(b",") for line in frame_md5.splitlines() if line and not line.startswith(b"#")]
    if len(rows) != FRAMES or any(int(row[4]) != WIDTH * HEIGHT * 3 // 2 for row in rows):
        raise RuntimeError("repeated H264 stream decode frame count or dimensions invalid")
    hashes = [row[5].strip().decode("ascii") for row in rows]
    expected = [hashes[0], hashes[FPS]]
    if expected[0] == expected[1] or any(value != expected[(i // FPS) % 2] for i, value in enumerate(hashes)):
        raise RuntimeError("repeated H264 stream did not alternate its owned colors")
    silence = checked(ffmpeg, ["-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", AUDIO_SECONDS,
        "-vn", "-c:a", "libopus", "-b:a", "128k", "-application", "lowdelay", "-frame_duration", "5",
        "-vbr", "off", "-mapping_family", "0", "-f", "ogg", "-page_duration", "5000", "-flush_packets", "1", "pipe:1"])
    pages = ogg_pages(silence)
    if len(silence) > 1048576 or len(pages) != 6002 or any(len(p) != 108 for p in pages[2:]):
        raise RuntimeError("synthetic Ogg page size or count invalid")
    if any(p[28] != 0xec for p in pages[2:]) or pages[-1][5] != 4:
        raise RuntimeError("synthetic Opus packet duration or EOS invalid")
    if struct.unpack_from("<Q", pages[-1], 6)[0] != 1_439_760 + 120:
        raise RuntimeError("synthetic Opus granule invalid")
    pcm = decode(ffmpeg, silence, "ogg", ["-ac", "2", "-ar", "48000", "-f", "s16le"])
    if len(pcm) != PCM_BYTES or any(pcm):
        raise RuntimeError("synthetic Opus did not decode to bounded stereo silence")
    generated[NAMES[2]] = silence
    assets = {name: {"sha256": hashlib.sha256(data).hexdigest(), "bytes": len(data)}
              for name, data in generated.items()}
    manifest = {"schema_version": 1, "role": "synthetic-substitution-only", "desktop_capture_tested": False,
                "source_commit": "2e07af3484369bc68bc8091d5a04969867eff0f9",
                "ffmpeg_sha256": hashlib.sha256(ffmpeg.read_bytes()).hexdigest(),
                "ffmpeg_version": checked(ffmpeg, ["-version"]).decode().splitlines()[0],
                "video": {"width": WIDTH, "height": HEIGHT, "fps": FPS, "frames": FRAMES,
                          "synthetic_colors": [BLUE, ORANGE], "decoded_rgb": decoded_colors,
                          "decoded_yuv420p_md5": expected, "full_stream_strict_decode": True},
                "audio": {"input_pcm_duration_ms": 29995, "sample_rate": 48000, "channels": 2,
                          "packet_ms": 5, "data_pages": 6000, "pcm_bytes": PCM_BYTES,
                          "full_stream_strict_decode": True},
                "assets": assets}
    flags = " ".join(f"-X main.{key}={assets[name]['sha256']}" for key, name in zip(
        ("videoBlueSHA256", "videoOrangeSHA256", "audioSilenceSHA256"), NAMES))
    # Validate everything before replacing any outputs. The files stay adjacent
    # to the separately named fixture binary; no installation is performed.
    for name in (*NAMES, "fixture-assets.json", "fixture-ldflags.txt"):
        target = output / name
        if target.is_symlink() or (target.exists() and not target.is_file()):
            raise RuntimeError("refusing nonregular or symlink asset output")
    for name, data in generated.items():
        (output / name).write_bytes(data)
    (output / "fixture-assets.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    (output / "fixture-ldflags.txt").write_text(flags + "\n", encoding="ascii")
    print(json.dumps({"desktop_capture_tested": False, "manifest": str(output / "fixture-assets.json"),
                      "asset_bytes": sum(a["bytes"] for a in assets.values())}))


if __name__ == "__main__":
    main()
