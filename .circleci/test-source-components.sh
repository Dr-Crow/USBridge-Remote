#!/usr/bin/env bash
set -euo pipefail
ROOT="$(pwd)"
OUT="$ROOT/artifacts/source-components"
WORK="$ROOT/.source-components-ci"
export PATH="$HOME/ci-toolchain/go/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=1
sudo apt-get update
sudo env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l apt-get install -y ffmpeg xvfb python3-cryptography libopus0
mkdir -p "$WORK" "$OUT/package/agent" "$OUT/package/components/bin" "$OUT/package/source"
python3 - "$ROOT" "$WORK" <<'PY'
import hashlib,json,pathlib,sys,tarfile
root,work=map(pathlib.Path,sys.argv[1:]);snap=root/'.circleci/source-snapshots';meta=json.loads((snap/'source-streamer.json').read_text());archive=snap/meta['archive']
assert meta['commit']=='39633d37bb0cfdd989960d9dc4e938011776d420'
assert hashlib.sha256(archive.read_bytes()).hexdigest()==meta['archive_sha256']
with tarfile.open(archive) as t:
    members=t.getmembers()
    assert len(members)==meta['file_count']
    assert all(m.isfile() and m.name.startswith('source/') and '..' not in pathlib.PurePosixPath(m.name).parts for m in members)
    t.extractall(work)
PY
SOURCE_COMMIT=39633d37bb0cfdd989960d9dc4e938011776d420
(cd "$WORK/source" && go test -race ./streamer ./controltransport && go build -trimpath -ldflags "-s -w -X main.version=$SOURCE_COMMIT" -o "$OUT/package/components/bin/source-streamer" ./cmd/source-streamer) 2>&1 | tee "$OUT/source-build-tests.txt"
cp "$ROOT/agent/dist/linux/usbridge-agent" "$OUT/package/agent/usbridge-agent"
bash "$WORK/source/controltransport/testdata/build_client.sh" "$WORK/enet-control-test-client"
cp "$ROOT/.circleci/source-snapshots/"* "$OUT/package/source/"
cp "$WORK/source/LICENSE" "$OUT/package/source/LICENSE"
python3 - "$OUT/package/components" "$SOURCE_COMMIT" <<'PY'
import hashlib,json,pathlib,sys
root=pathlib.Path(sys.argv[1]);data=(root/'bin/source-streamer').read_bytes()
manifest={'schema':1,'components':[{'name':'source-streamer','platform':'linux/amd64','version':sys.argv[2],'profile':'source-streamer-v1','entry':'bin/source-streamer','files':[{'path':'bin/source-streamer','size':len(data),'sha256':hashlib.sha256(data).hexdigest(),'executable':True}]}]}
(root/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
(root/'MANIFEST.sha256').write_text(hashlib.sha256((root/'manifest.json').read_bytes()).hexdigest()+'  manifest.json\n')
PY
python3 "$ROOT/.circleci/source/test_agent_source_lifecycle.py" --agent "$OUT/package/agent/usbridge-agent" --streamer "$OUT/package/components/bin/source-streamer" --output "$OUT/lifecycle"
python3 "$ROOT/.circleci/source/test_agent_source_media.py" --agent "$OUT/package/agent/usbridge-agent" --components "$OUT/package/components" --enet-helper "$WORK/enet-control-test-client" --output "$OUT/media"
cp "$ROOT/docs/SOURCE_STREAMER_PROTOCOL.md" "$OUT/package/README.md"
python3 - "$OUT" "${CIRCLE_SHA1:-unknown}" "$SOURCE_COMMIT" <<'PY'
import hashlib,json,pathlib,sys
out=pathlib.Path(sys.argv[1]);files={str(p.relative_to(out/'package')):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted((out/'package').rglob('*')) if p.is_file()}
(out/'package/BUILD-PROVENANCE.json').write_text(json.dumps({'agent_commit':sys.argv[2],'source_streamer_commit':sys.argv[3],'files_sha256':files,'hardware_tested':False,'audio_source':'synthesized silence','source_broker_included':False,'production_parity':False},indent=2)+'\n')
PY
tar -czf "$OUT/USBridgeAgent-Linux-source-experimental.tar.gz" -C "$OUT/package" .
(cd "$OUT" && sha256sum USBridgeAgent-Linux-source-experimental.tar.gz > SHA256SUMS.txt)
