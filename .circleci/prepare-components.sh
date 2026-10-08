#!/usr/bin/env bash
set -euo pipefail
ROOT="$1"
TARGET="$2"
OUT="$ROOT/artifacts/components"
mkdir -p "$OUT"
# Cached inputs carry no tokens. The offline path checks vendor signatures,
# archive hashes and every pinned extracted file before rebuilding metadata.
if [[ -f "$OUT/rustshine-manifest.json" && -f "$OUT/usb-broker-manifest.json" ]]; then
  go run ./cmd/component_bundle -out "$OUT" -platform "$TARGET" -from-archives
else
  go run ./cmd/component_bundle -out "$OUT" -platform "$TARGET"
fi
