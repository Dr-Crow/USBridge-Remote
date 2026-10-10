#!/usr/bin/env bash
# Additional opt-in native parent/dialog gate. Run AFTER test-source-preview.sh;
# its manager/crypto/pixel/audio gates and provenance remain independent.
set -euo pipefail
# A caller's opt-in must never launch a prior fixture before we own displays.
unset SOURCE_PREVIEW_NATIVE_ACCEPTANCE SOURCE_PREVIEW_DIALOG_ACCEPTANCE
ROOT="$(pwd)"
OUT="$ROOT/artifacts/source-preview-dialog"
export PATH="$HOME/ci-toolchain/go/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=1
[[ "$(go version)" == 'go version go1.26.9 linux/amd64' ]]
[[ "$(id -u)" != 0 ]]
[[ -f "$ROOT/artifacts/source-preview/package/components/MANIFEST.sha256" ]]
mkdir -p "$OUT"
/usr/bin/python3 -m unittest discover -s "$ROOT/.circleci/source" -p test_preview_processes.py -v
printf '{"passed":false,"agent_parent_window_interaction_tested":false,"state":"not-run"}\n' > "$OUT/result.json"
COMMIT="$(git rev-parse HEAD)"
[[ -z "${CIRCLE_SHA1:-}" || "$COMMIT" == "$CIRCLE_SHA1" ]]
/usr/bin/python3 - "$ROOT/artifacts/source-preview" "$COMMIT" <<'PYPRIOR'
import json,pathlib,sys
out=pathlib.Path(sys.argv[1])
r=json.loads((out/'result.json').read_text())
p=json.loads((out/'package/BUILD-PROVENANCE.json').read_text())
assert r['passed'] and r['presented_pixels_verified'] and r['inherited_frame_dumps_disabled']
assert p['agent_client_commit']==sys.argv[2] and p['native_viewer_presented_pixels_tested']
PYPRIOR
# Runtime packages only. The prior gate supplies the native compiler/link deps.
sudo apt-get update
sudo env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l apt-get install -y xvfb xauth xdotool x11-utils x11-xserver-utils dbus-daemon python3-dbus iproute2 util-linux
WORK="$(mktemp -d "$ROOT/.source-preview-dialog.XXXXXX")"
AUTH="$WORK/xauthority"
CAPTURE_PID= VIEWER_PID=
cleanup(){
 [[ -z "$CAPTURE_PID" ]] || kill "$CAPTURE_PID" 2>/dev/null || true
 [[ -z "$VIEWER_PID" ]] || kill "$VIEWER_PID" 2>/dev/null || true
 [[ -z "$CAPTURE_PID" ]] || wait "$CAPTURE_PID" 2>/dev/null || true
 [[ -z "$VIEWER_PID" ]] || wait "$VIEWER_PID" 2>/dev/null || true
 rm -rf "$WORK"
}
trap cleanup EXIT
# Do not use -tags ci: that selects Fyne's software test driver, not native GUI.
(cd agent && go build -tags source_preview_acceptance -o "$WORK/source-preview-ui-acceptance" ./cmd/source-preview-ui-acceptance) 2>&1 | tee "$OUT/parent-build.txt"
(cd agent && go test -race -tags ci,source_preview_acceptance -timeout 3m ./internal/sourcepreview) 2>&1 | tee "$OUT/observer-unit-tests.txt"
(cd agent && go test -race -tags ci,source_preview_acceptance -timeout 1m -run '^TestSourcePreviewAcceptanceDialogLocator$' ./internal/ui) 2>&1 | tee "$OUT/dialog-locator-unit.txt"
for DIR in home config cache data state runtime; do mkdir -m 700 "$WORK/$DIR"; done
touch "$AUTH"; chmod 600 "$AUTH"
for D in :96 :97; do
 [[ ! -e "/tmp/.X11-unix/X${D#:}" && ! -e "/tmp/.X${D#:}-lock" ]] || { echo 'Refusing an existing display' >&2; exit 1; }
 xauth -f "$AUTH" add "$D" . "$(openssl rand -hex 16)"
done
export XAUTHORITY="$AUTH"
Xvfb :96 -noreset -screen 0 128x72x24 -nolisten tcp -auth "$AUTH" > "$OUT/capture-xvfb.log" 2>&1 & CAPTURE_PID=$!
Xvfb :97 -noreset -screen 0 1280x960x24 -nolisten tcp -auth "$AUTH" > "$OUT/parent-viewer-xvfb.log" 2>&1 & VIEWER_PID=$!
for D in :96 :97; do
 ready=0
 for _ in $(seq 1 100); do if DISPLAY="$D" xdpyinfo >/dev/null 2>&1; then ready=1; break; fi; sleep .05; done
 [[ "$ready" == 1 ]]
done
DISPLAY=:96 xsetroot -solid '#164cb4'
DISPLAY=:96 xdotool mousemove 0 0
PIN="$(cut -d' ' -f1 "$ROOT/artifacts/source-preview/package/components/MANIFEST.sha256")"
[[ "$PIN" =~ ^[a-f0-9]{64}$ ]]
# Only this new network namespace gains loopback configuration. Nothing changes
# host networking, security, capture permissions, system services, or accounts.
# Drop root before launching DBus, Python, the parent, source, or viewer.
sudo unshare --net /bin/bash -eu -c '
 /usr/sbin/ip link set lo up
 exec /usr/bin/setpriv --reuid="$1" --regid="$2" --init-groups --no-new-privs --bounding-set=-all /usr/bin/env -i \
  PATH=/usr/bin:/bin LANG=C.UTF-8 HOME="$3/home" XDG_CONFIG_HOME="$3/config" \
  XDG_CACHE_HOME="$3/cache" XDG_DATA_HOME="$3/data" XDG_RUNTIME_DIR="$3/runtime" \
  DISPLAY=:97 XAUTHORITY="$3/xauthority" FYNE_SCALE=1 LIBGL_ALWAYS_SOFTWARE=1 \
  SOURCE_PREVIEW_DIALOG_ACCEPTANCE=1 SOURCE_PREVIEW_DIALOG_WORK="$3" \
  SOURCE_PREVIEW_DIALOG_OUTPUT="$4" SOURCE_PREVIEW_REPO="$5" \
  SOURCE_PREVIEW_COMPONENTS="$5/artifacts/source-preview/package/components" \
  SOURCE_PREVIEW_MANIFEST_SHA256="$6" SOURCE_PREVIEW_OUTER_NETNS="$7" SOURCE_PREVIEW_COMMIT="$8" \
  /usr/bin/timeout --kill-after=10s 150s /usr/bin/dbus-run-session -- \
  /usr/bin/python3 "$5/.circleci/source/preview_dialog.py"
' dialog-gate "$(id -u)" "$(id -g)" "$WORK" "$OUT" "$ROOT" "$PIN" "$(readlink /proc/self/ns/net)" "$COMMIT" 2>&1 | tee "$OUT/native-dialog.txt"
/usr/bin/python3 - "$OUT/result.json" <<'PY'
import json,sys
r=json.load(open(sys.argv[1]))
assert r['passed'] and r['agent_parent_window_interaction_tested']
assert r['launches']['stream_starts']==r['launches']['streams_joined']==3
assert r['launches']['viewer_starts']==r['launches']['viewers_joined']==3
assert r['launches']['fresh_keys'] and r['launches']['fresh_key_ids'] and r['launches']['fresh_ids']
assert r['owned_processes_and_transport_sockets_gone']
PY
