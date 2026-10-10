#!/usr/bin/env bash
# Compile and execute only the actual Windows boundary files, before Fyne/codecs.
set -euo pipefail
if [[ -n "${USBRIDGE_CI_ROOT:-}" ]]; then cd "$(cygpath -u "$USBRIDGE_CI_ROOT")"; fi
ROOT="$PWD"; WORK="$ROOT/.source-preview-windows-preflight"; OUT="$ROOT/artifacts/windows-preview-evidence"
[[ ! -e "$WORK" ]]
mkdir -p "$WORK" "$OUT"
export PATH="/c/ci/go/bin:/ucrt64/bin:/usr/bin:$PATH" CC=gcc CXX=g++ CGO_ENABLED=1 GOTOOLCHAIN=local
export GOPATH='C:\ci\gopath' GOCACHE='C:\ci\go-build' GOROOT='C:\ci\go'
export GOENV=off GOWORK=off GOFLAGS='-trimpath -mod=readonly' GOCACHEPROG= GOEXPERIMENT=
[[ "$(go version)" == 'go version go1.26.9 windows/amd64' ]]
[[ "$(git rev-parse HEAD)" == "${CIRCLE_SHA1:?exact source commit required}" ]]
START="$(date +%s)"
python3 .circleci/source/windows_preview_preflight.py --extract --root "$(cygpath -m "$ROOT")" --work "$(cygpath -m "$WORK")"
for unit in "$WORK"/*.c; do
 gcc -std=c11 -Werror=implicit-function-declaration -fsyntax-only -x c "$unit"
done
(
 cd client/cmd/source-preview-viewer
 # Explicit source files exclude Fyne, decoder/service and media initialization.
 files=(main_windows.go environment.go environment_windows.go environment_test.go main_windows_test.go)
 go test -json -count=5 -timeout=2m "${files[@]}" > "$WORK/console.jsonl"
 go test -json -count=5 -timeout=2m -ldflags='-H=windowsgui' "${files[@]}" > "$WORK/gui.jsonl"
)
pacman -Q mingw-w64-ucrt-x86_64-gcc mingw-w64-ucrt-x86_64-crt-git mingw-w64-ucrt-x86_64-headers-git > "$WORK/packages.txt"
python3 .circleci/source/windows_preview_preflight.py --root "$(cygpath -m "$ROOT")" --work "$(cygpath -m "$WORK")" \
 --output "$(cygpath -m "$OUT/preflight.json")" --commit "$CIRCLE_SHA1" --elapsed-seconds "$(( $(date +%s) - START ))"
echo 'Native C/UCRT syntax and actual boundary privacy tests passed before Fyne or media builds.'
