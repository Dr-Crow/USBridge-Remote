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
python3 -c "import compression.zstd, tarfile"
PYTHONPATH=.circleci/source python3 - "$OUT/manager-staging-tests.json" "$CIRCLE_SHA1" <<'PYTEST'
import io,json,pathlib,sys,unittest
import test_windows_manager_components
suite=unittest.defaultTestLoader.loadTestsFromModule(test_windows_manager_components)
result=unittest.TextTestRunner(stream=io.StringIO()).run(suite)
allowed={
 'test_case_collision_on_disk_is_rejected':'case-insensitive filesystem cannot represent both basenames',
 'test_destination_file_and_dangling_symlink_are_rejected':'native unprivileged symlink creation unavailable',
 'test_symlink_executable_dependency_and_parent_are_rejected':'native unprivileged symlink creation unavailable',
 'test_destination_parent_symlink_is_rejected':'native unprivileged symlink creation unavailable',
}
skips=[{'test':test.id().rsplit('.',1)[-1],'reason':reason} for test,reason in result.skipped]
valid_skips=all(allowed.get(x['test'])==x['reason'] for x in skips)
passed=result.wasSuccessful() and result.testsRun==36 and valid_skips
receipt={'schema_version':1,'commit':sys.argv[2],'passed':passed,'tests_run':result.testsRun,
         'tests_passed':result.testsRun-len(result.failures)-len(result.errors)-len(skips),
         'failures':len(result.failures),'errors':len(result.errors),'skips':skips,
         'synthetic_files_only':True,'component_execution_performed':False,'source_or_binary_artifacts_published':False}
pathlib.Path(sys.argv[1]).write_text(json.dumps(receipt,indent=2)+'\n')
if not passed:raise SystemExit('Manager staging checks failed; see source-free test receipt')
PYTEST
python3 .circleci/source/windows-graphics-probe/test_probe_source.py
gcc -std=c11 -Wall -Wextra -Werror -fsyntax-only .circleci/source/windows-graphics-probe/probe.c
python3 .circleci/source/windows_preview_preflight.py --extract --root "$(cygpath -m "$ROOT")" --work "$(cygpath -m "$WORK")"
for unit in "$WORK"/*.c; do
 gcc -std=c11 -Werror=implicit-function-declaration -fsyntax-only -x c "$unit"
done
(
 cd client/cmd/source-preview-viewer
 # Explicit source files exclude Fyne, decoder/service and media initialization.
 files=(main_windows.go environment.go environment_windows.go environment_test.go main_windows_test.go graphics_environment.go graphics_environment_windows.go graphics_environment_test.go graphics_environment_windows_test.go)
 go test -json -count=5 -timeout=2m "${files[@]}" > "$WORK/console.jsonl"
 go test -json -count=5 -timeout=2m -ldflags='-H=windowsgui' "${files[@]}" > "$WORK/gui.jsonl"
)
pacman -Q mingw-w64-ucrt-x86_64-gcc mingw-w64-ucrt-x86_64-crt-git mingw-w64-ucrt-x86_64-headers-git > "$WORK/packages.txt"
python3 .circleci/source/windows_preview_preflight.py --root "$(cygpath -m "$ROOT")" --work "$(cygpath -m "$WORK")" \
 --output "$(cygpath -m "$OUT/preflight.json")" --commit "$CIRCLE_SHA1" --elapsed-seconds "$(( $(date +%s) - START ))"
echo 'Native C/UCRT syntax and actual boundary privacy tests passed before Fyne or media builds.'
