#!/usr/bin/env bash
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
OUT="$ROOT/artifacts/lan-host"
mkdir -p "$OUT"
IMAGE=usbridge-lan-ci
BUILD=usbridge-lan-builder-ci
NET=usbridge-lan-ci-net
VOL=usbridge-lan-ci-fixtures
TMP="$(mktemp -d)"
cleanup() {
  docker rm -f usbridge-lan-ci-host usbridge-lan-ci-fixtures >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
  docker volume rm "$VOL" >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT
# No image push, deployment, host trust changes or privileged container.
docker build --target build -t "$BUILD" -f "$ROOT/deploy/lan/Dockerfile" "$ROOT" 2>&1 | tee "$OUT/build.txt"
docker build -t "$IMAGE" -f "$ROOT/deploy/lan/Dockerfile" "$ROOT" 2>&1 | tee -a "$OUT/build.txt"
docker image inspect "$IMAGE" --format '{{.Id}} {{.Architecture}} {{.Config.User}}' > "$OUT/image.txt"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=lan-host \
  -addext subjectAltName=DNS:lan-host,IP:127.0.0.1 \
  -keyout "$TMP/server.key" -out "$TMP/server.crt" >/dev/null 2>&1
chmod 644 "$TMP/server.key" # Disposable fixture only; never an operator key.
mkdir "$TMP/components"
printf 'verified' > "$TMP/components/fixture.bin"
HASH="$(sha256sum "$TMP/components/fixture.bin" | cut -d' ' -f1)"
printf '{"schema":1,"components":[{"name":"fixture","platform":"linux/amd64","version":"test","profile":"fixture","entry":"fixture.bin","files":[{"path":"fixture.bin","size":8,"sha256":"%s"}]}]}' "$HASH" > "$TMP/components/manifest.json"
printf 'must not serve' > "$TMP/components/private.key"
docker volume create "$VOL" >/dev/null
docker create --name usbridge-lan-ci-fixtures -v "$VOL:/fixtures" "$BUILD" true >/dev/null
docker cp "$TMP/." usbridge-lan-ci-fixtures:/fixtures
docker rm usbridge-lan-ci-fixtures >/dev/null
docker network create --internal "$NET" >/dev/null
docker run -d --name usbridge-lan-ci-host --network "$NET" --network-alias lan-host \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  -v "$VOL:/fixtures:ro" "$IMAGE" \
  --cert=/fixtures/server.crt --key=/fixtures/server.key --components=/fixtures/components \
  --agent-origins=https://192.168.1.8:8443 >/dev/null
docker run --rm --network "$NET" -v "$VOL:/fixtures:ro" "$BUILD" bash -euo pipefail -c '
  curl --retry 10 --retry-connrefused --retry-delay 1 --cacert /fixtures/server.crt -fsS https://lan-host:8443/healthz
  curl --cacert /fixtures/server.crt -fsS https://lan-host:8443/runtime-config.js | grep -q '\''"strictLAN":true'\''
  curl --cacert /fixtures/server.crt -fsSI https://lan-host:8443/app.wasm | grep -qi "content-type: application/wasm"
  curl --cacert /fixtures/server.crt -fsS https://lan-host:8443/bootstrap.js >/dev/null
  curl --cacert /fixtures/server.crt -fsS https://lan-host:8443/models/icon_detect.onnx >/dev/null
  curl --cacert /fixtures/server.crt -fsS https://lan-host:8443/components/fixture.bin | grep -q verified
  test "$(curl --cacert /fixtures/server.crt -sS -o /dev/null -w "%{http_code}" https://lan-host:8443/components/private.key)" = 404
  test "$(curl --cacert /fixtures/server.crt -sS -o /dev/null -w "%{http_code}" -X POST https://lan-host:8443/healthz)" = 405
  if curl --connect-timeout 3 --max-time 4 -fsS https://1.1.1.1 >/dev/null 2>&1; then echo "internal test network allowed WAN" >&2; exit 1; fi
  echo "TLS/assets/strict config/declared mirror/internal network checks passed"
' 2>&1 | tee "$OUT/tests.txt"
if docker run --rm "$IMAGE" > "$OUT/missing-tls.txt" 2>&1; then
  echo 'Server accepted missing TLS credentials' >&2; exit 1
fi
docker logs usbridge-lan-ci-host > "$OUT/server.txt" 2>&1
printf 'Commit: %s\n' "${CIRCLE_SHA1:-$(git rev-parse HEAD)}" > "$OUT/provenance.txt"
