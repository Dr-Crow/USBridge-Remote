#!/usr/bin/env bash
# Job-local, official-registry, software-only WGL fallback. No OS/GPU driver,
# machine environment, registry or persistent graphics setting is changed.
set -euo pipefail
ROOT="$PWD"; WORK="$ROOT/.source-preview-windows-build"; OUT="$ROOT/artifacts/windows-preview-evidence"
[[ "${GOCACHE:?}" == 'C:\ci\go-build' ]]
[[ -s "$OUT/public-cache-seed.json" ]]
pacman --noconfirm -S --needed mingw-w64-ucrt-x86_64-llvm-libs \
 mingw-w64-ucrt-x86_64-spirv-tools mingw-w64-ucrt-x86_64-libsystre
PYTHONPATH=.circleci/source python3 -m unittest discover -s .circleci/source -p 'test_windows_software_gl.py'
python3 .circleci/source/windows_software_gl.py --work "$(cygpath -m "$WORK")" \
 --output "$(cygpath -m "$OUT")" --ucrt-bin "$(cygpath -m /ucrt64/bin)" --commit "$CIRCLE_SHA1"
gcc -O2 -Wall -Wextra -Werror .circleci/source/windows-graphics-probe/probe.c \
 -o "$WORK/bin/windows-graphics-probe.exe" -luser32 -lgdi32
export WINDOWS_GRAPHICS_PROBE="$(cygpath -m "$WORK/bin/windows-graphics-probe.exe")"
export WINDOWS_GRAPHICS_PROBE_SHA256="$(sha256sum "$WORK/bin/windows-graphics-probe.exe" | cut -d' ' -f1)"
export WINDOWS_GRAPHICS_PROBE_STAGING="$(cygpath -m "$WORK/probe-staging.json")"
export WINDOWS_GRAPHICS_PROBE_STAGING_SHA256="$(sha256sum "$WORK/probe-staging.json" | cut -d' ' -f1)"
export WINDOWS_GRAPHICS_PROBE_RECEIPT="$(cygpath -m "$OUT/software-graphics-probe.json")"
(
 cd .circleci/source/windows-preview-acceptance
 CGO_ENABLED=0 go test -tags graphicsprobe -run '^(TestWindowsOwnedSoftwareGraphicsProbe|TestGraphicsProbeStrictParsers)$' -count=1 -timeout=45s ./...
)
python3 - "$OUT/software-graphics-probe.json" <<'PY'
import json,pathlib,sys
p=pathlib.Path(sys.argv[1]);assert p.is_file() and not p.is_symlink() and p.stat().st_size<65536
r=json.loads(p.read_text());assert r['passed'] is True
PY
unset WINDOWS_GRAPHICS_PROBE WINDOWS_GRAPHICS_PROBE_SHA256 WINDOWS_GRAPHICS_PROBE_STAGING WINDOWS_GRAPHICS_PROBE_STAGING_SHA256 WINDOWS_GRAPHICS_PROBE_RECEIPT
