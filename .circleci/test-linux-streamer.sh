#!/usr/bin/env bash
set -euo pipefail
ROOT="${1:?repository root required}"
cd "$ROOT/agent"
# Keep the agent itself built/tested on Ubuntu 22.04. Only this stock-component
# probe needs a newer runtime. No network is available during the probe.
CGO_ENABLED=0 go test -c -o "$ROOT/artifacts/streamer-cli.test" ./internal/localruntime
docker build -f "$ROOT/.circleci/streamer-runtime.Dockerfile" -t usbridge-streamer-probe "$ROOT/.circleci"
docker image inspect usbridge-streamer-probe --format '{{.Id}}' > "$ROOT/artifacts/streamer-runtime-image.txt"
docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges usbridge-streamer-probe dpkg-query -W libc6 libstdc++6 libvulkan1 > "$ROOT/artifacts/streamer-runtime-packages.txt"
docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges \
  --user "$(id -u):$(id -g)" --tmpfs /tmp:rw,mode=1777 -e HOME=/tmp \
  -e USBRIDGE_LAB_STREAMER_BINARY=/components/rustshine/usbridge-streamer \
  -v "$ROOT/artifacts/components:/components:ro" \
  -v "$ROOT/artifacts/streamer-cli.test:/probe:ro" \
  usbridge-streamer-probe /probe -test.run '^TestPinnedStreamerEmptyICECLI$' -test.v \
  2>&1 | tee "$ROOT/artifacts/streamer-empty-ice-cli.txt"
rm "$ROOT/artifacts/streamer-cli.test"
