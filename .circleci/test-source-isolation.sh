#!/usr/bin/env bash
# Run only after the source package's ordinary acceptance is green.
set -euo pipefail
ROOT="$(pwd)"; OUT="$ROOT/artifacts/source-components"; WORK="$ROOT/.source-components-ci"
/usr/bin/python3 "$ROOT/.circleci/source/test_isolation_commands.py"
NET="usbridge-source-proof-${CIRCLE_SHA1:0:12}"
IMAGE=usbridge-source-acceptance
mkdir -p "$OUT/internal-network" "$WORK/isolation/lib"
cleanup(){ docker network rm "$NET" >/dev/null 2>&1 || true; };trap cleanup EXIT
export PATH="$HOME/ci-toolchain/go/bin:$PATH" GOTOOLCHAIN=local
(cd "$WORK/broker/source" && CGO_ENABLED=0 go test -c -o "$WORK/isolation/broker-session-tests" ./brokersession)
cp "$ROOT/.circleci/source/"*.py "$WORK/isolation/"
cp "$ROOT/.circleci/source-isolation/run.sh" "$WORK/isolation/"
cp "$WORK/enet-control-test-client" "$WORK/isolation/"
sudo apt-get update
sudo env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l apt-get install -y cmake libssl-dev
CLIENT_PIN=e913d516fab11d8fc84a35c7818cea1e455436fa
CLIENT_SRC="$WORK/public-client"
git init -q "$CLIENT_SRC"
git -C "$CLIENT_SRC" remote add origin https://github.com/itsme228/moonlight-common-c.git
git -C "$CLIENT_SRC" fetch --depth 1 origin "$CLIENT_PIN"
git -C "$CLIENT_SRC" checkout --detach FETCH_HEAD
git -C "$CLIENT_SRC" submodule update --init --recursive
[[ "$(git -C "$CLIENT_SRC" rev-parse HEAD)" == "$CLIENT_PIN" ]]
[[ "$(git -C "$CLIENT_SRC/enet" rev-parse HEAD)" == aca87840b57f045a1f7f9299e4b1b9b8e2a5e2f1 ]]
[[ "$(git -C "$CLIENT_SRC/nanors" rev-parse HEAD)" == b1e3c22ca0cdc0bb83e3cd6ed1a2fc77869ed99a ]]
cmake -S "$CLIENT_SRC" -B "$WORK/public-client-build" -DCMAKE_BUILD_TYPE=Debug -DBUILD_SHARED_LIBS=ON > "$OUT/internal-network/public-client-build.txt" 2>&1
cmake --build "$WORK/public-client-build" -j2 >> "$OUT/internal-network/public-client-build.txt" 2>&1
cp "$WORK/public-client-build/"libmoonlight-common-c.so* "$WORK/isolation/lib/"
cc -std=gnu11 -I"$CLIENT_SRC/src" "$ROOT/.circleci/source-isolation/full_client.c" -L"$WORK/isolation/lib" -Wl,-rpath,'$ORIGIN/lib' -lmoonlight-common-c -lpthread -l:libopus.so.0 -o "$WORK/isolation/full-client"
printf '%s\n' "$CLIENT_PIN" > "$OUT/internal-network/public-client-commit.txt"
[[ -z "$(git -C "$CLIENT_SRC" status --porcelain --untracked-files=all)" ]]
docker build -t "$IMAGE" "$ROOT/.circleci/source-isolation" 2>&1 | tee "$OUT/internal-network/image-build.txt"
docker image inspect "$IMAGE" --format '{{.Id}} {{.Architecture}}' > "$OUT/internal-network/image.txt"
docker network create --internal "$NET" >/dev/null
test "$(docker network inspect "$NET" --format '{{.Internal}}')" = true
docker run --rm --network "$NET" --dns 127.0.0.1 --read-only --cap-drop ALL --security-opt no-new-privileges \
  --pids-limit 128 --memory 2g --cpus 2 --user "$(id -u):$(id -g)" \
  --tmpfs /tmp:rw,exec,nosuid,nodev,size=768m,mode=1777 \
  -v "$OUT/package:/package:ro" -v "$WORK/isolation:/acceptance:ro" -v "$OUT/internal-network:/results:rw" \
  "$IMAGE" /bin/bash /acceptance/run.sh 2>&1 | tee "$OUT/internal-network/tests.txt"
printf 'Agent commit: %s\n' "$CIRCLE_SHA1" > "$OUT/internal-network/provenance.txt"
