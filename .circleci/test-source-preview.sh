#!/usr/bin/env bash
# Separate opt-in GUI gate. Captures only a generated display owned by this job.
set -euo pipefail
ROOT="$PWD"; OUT="$ROOT/artifacts/source-preview"; WORK="$ROOT/.source-preview-ci"
export PATH="$HOME/ci-toolchain/go/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=1
[[ "$(go version)" == 'go version go1.26.9 linux/amd64' ]]
mkdir -p "$OUT" "$WORK"
sudo apt-get update
sudo env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l apt-get install -y build-essential cmake pkg-config binutils libopus-dev libssl-dev libavcodec-dev libavutil-dev libswscale-dev libpulse-dev libva-dev libvulkan-dev libgl1-mesa-dev xorg-dev libxtst6 xauth x11-xserver-utils xdotool
# The inherited repository gitlink, not a moving upstream branch.
git submodule update --init --recursive client/moonlight-common-c
[[ "$(git -C client/moonlight-common-c rev-parse HEAD)" == a232e27d5c423eb8e7de8da91f0eefcf9171348f ]]
[[ -z "$(git -C client/moonlight-common-c status --porcelain)" ]]
(cd client && bash scripts/build_source_preview_viewer.sh) 2>&1 | tee "$OUT/viewer-build.txt"
git -C client/moonlight-common-c diff > "$OUT/moonlight-build-patch.diff"
(cd client && go test -race -timeout 5m ./internal/sourcepreview ./internal/api/moonlight && go test -race -timeout 5m -run '^Test(SourcePreview|Disconnect)' ./internal/service && go test -race ./cmd/source-preview-viewer/environment.go ./cmd/source-preview-viewer/environment_test.go ./cmd/source-preview-viewer/events.go ./cmd/source-preview-viewer/events_test.go) 2>&1 | tee "$OUT/viewer-tests.txt"
cp -a "$ROOT/artifacts/source-components/package" "$OUT/package"
cp "$ROOT/client/dist/source-preview-viewer" "$OUT/package/components/bin/source-preview-viewer"
cp "$ROOT/client/LICENSE" "$OUT/package/source/VIEWER-LICENSE"
cp "$ROOT/client/docs/SOURCE_PREVIEW_VIEWER.md" "$OUT/package/"
/usr/bin/python3 - "$OUT/package/components" "${CIRCLE_SHA1:?}" <<'PY'
import hashlib,json,pathlib,sys
root=pathlib.Path(sys.argv[1]);p=root/'bin/source-preview-viewer';data=p.read_bytes();m=json.loads((root/'manifest.json').read_text())
m['components'].append({'name':'source-preview-viewer','platform':'linux/amd64','version':sys.argv[2],'profile':'source-preview-v1','entry':'bin/source-preview-viewer','files':[{'path':'bin/source-preview-viewer','sha256':hashlib.sha256(data).hexdigest(),'size':len(data),'executable':True}]})
raw=(json.dumps(m,indent=2)+'\n').encode();(root/'manifest.json').write_bytes(raw);(root/'MANIFEST.sha256').write_text(hashlib.sha256(raw).hexdigest()+'  manifest.json\n')
PY
AUTH="$WORK/xauthority"; touch "$AUTH"; chmod 600 "$AUTH"
for D in :96 :97; do [[ ! -e "/tmp/.X11-unix/X${D#:}" ]] || { echo 'Refusing an existing display' >&2; exit 1; }; xauth -f "$AUTH" add "$D" . "$(openssl rand -hex 16)"; done
export XAUTHORITY="$AUTH"
Xvfb :96 -noreset -screen 0 128x72x24 -nolisten tcp -auth "$AUTH" > "$OUT/capture-xvfb.log" 2>&1 & CAPTURE_PID=$!
Xvfb :97 -noreset -screen 0 1024x768x24 -nolisten tcp -auth "$AUTH" > "$OUT/viewer-xvfb.log" 2>&1 & VIEWER_PID=$!
cleanup(){ kill "$CAPTURE_PID" "$VIEWER_PID" 2>/dev/null || true; wait "$CAPTURE_PID" "$VIEWER_PID" 2>/dev/null || true; rm -f "$AUTH"; }
trap cleanup EXIT
for D in :96 :97; do ready=0; for _ in $(seq 1 100); do if DISPLAY="$D" xdpyinfo >/dev/null 2>&1; then ready=1; break; fi; sleep .05; done; [[ "$ready" == 1 ]]; done
DISPLAY=:96 xsetroot -solid '#164cb4'
DISPLAY=:96 xdotool mousemove 0 0
export DISPLAY=:97 SOURCE_PREVIEW_CAPTURE_DISPLAY=:96 SOURCE_PREVIEW_NATIVE_ACCEPTANCE=1
export SOURCE_PREVIEW_COMPONENTS="$OUT/package/components" SOURCE_PREVIEW_MANIFEST_SHA256="$(cut -d' ' -f1 "$OUT/package/components/MANIFEST.sha256")"
export SOURCE_PREVIEW_PIXEL_PROBE="$ROOT/.circleci/source/preview_pixels.py" SOURCE_PREVIEW_OUTPUT="$OUT"
(cd agent && go test -race -tags ci -timeout 2m -run '^TestNativeSourcePreviewRenderer$' -v ./internal/sourcepreview) 2>&1 | tee "$OUT/native-preview.txt"
/usr/bin/python3 - "$OUT" "${CIRCLE_SHA1:?}" <<'PY'
import hashlib,json,pathlib,sys,tarfile
out=pathlib.Path(sys.argv[1]);pkg=out/'package';r=json.loads((out/'result.json').read_text());assert r['passed'] and r['presented_pixels_verified'] and r['inherited_frame_dumps_disabled']
files={p.relative_to(pkg).as_posix():hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(pkg.rglob('*')) if p.is_file() and p.name!='BUILD-PROVENANCE.json'}
(pkg/'BUILD-PROVENANCE.json').write_text(json.dumps({'agent_client_commit':sys.argv[2],'source_streamer_commit':'2e07af3484369bc68bc8091d5a04969867eff0f9','source_broker_commit':'69b4722ca94aa766875c968beb9cc19a669c4199','public_client_pin':'a232e27d5c423eb8e7de8da91f0eefcf9171348f','files_sha256':files,'native_viewer_presented_pixels_tested':True,'agent_manager_test_binary':True,'agent_parent_window_interaction_tested':False,'user_desktop_captured':False,'width':128,'height':72,'audio':'synthetic silence','input':False,'ordinary_moonlight_pairing':False,'production_parity':False},indent=2)+'\n')
archive=out/'USBridge-Linux-source-preview-experimental.tar.gz'
with tarfile.open(archive,'w:gz') as t:
 for p in sorted(pkg.rglob('*')):
  if p.is_file():t.add(p,arcname=p.relative_to(pkg),recursive=False)
(out/'SHA256SUMS.txt').write_text(hashlib.sha256(archive.read_bytes()).hexdigest()+'  '+archive.name+'\n')
PY
