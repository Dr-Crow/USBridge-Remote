# Source-built web client artifacts

CircleCI's web-client job builds the existing Go browser client from
client/cmd/wasm using client/scripts/build_web.sh. This produces our own
app.wasm from the public Go source. It does not reconstruct or compile the
proprietary Rust streamer or USB broker, and it does not make the client LAN-only.
Public Google STUN and the hosted signaling fallback remain in this stage.

## Bundle contents and verification

USBridge-Web-<client-version>.zip includes a web/ directory containing index.html,
its gui.html alias, app.wasm, the matching Go wasm_exec.js, ai_vision.js,
all three committed ONNX models, and all seven vendored ONNX Runtime JS/WASM
assets. Model bytes must match the sizes and SHA256 pins in
client/internal/localui/download.go. Missing assets, LFS pointers, invalid WASM
headers and a mismatched Go loader stop packaging rather than producing an
incomplete application.

The ZIP also includes the client and Go licenses, existing model/runtime
provenance, build-provenance.json, dependency and test reports, and a per-file
SHA256SUMS.txt. CircleCI separately publishes the ZIP's SHA256SUMS.txt and the
build provenance so downloads can be checked before extraction. Provenance
records the exact source commit, client version, toolchain versions, build
target, asset hashes and whether tracked source was modified.

The developer gamepad-test.html harness is deliberately omitted: it requires a
separately built gamepad-test.wasm and is not the main app. The optional stdio MCP
bridge is not a browser runtime dependency; its existing configuration still
fetches the bridge from GitHub. It is not included or described as an offline
feature in this bundle.

Go 1.26.6 is pinned; CI verifies its official Linux amd64 archive SHA256.
The image's Node version is recorded for Go's WASM mock tests. If no compatible
Node >=18 exists, CI uses SHA256-pinned Node 22.15.0. wasm_exec.js comes directly
from the same Go toolchain that compiled app.wasm. Node tests exercise mock
PeerConnection/DataChannel lifecycles; they are not browser or video-stream tests.

## Serving the bundle

Extract the ZIP, then serve its web/ directory with a static HTTPS server.
Serve .wasm as application/wasm and .mjs as JavaScript. Use trusted certificates
for the selected origin and preserve the agent's pairing/authentication.
Browser security requirements and optional AI acceleration depend on the actual
browser and deployment headers; they require separate acceptance tests.
No server, certificate enrollment, trust-root installation or network-policy
change is performed by the CI build.

Hard-reload or disable caching when testing another app.wasm build. A successful
source build and complete static bundle do not establish a working LAN stream,
physical tablet input, or absence of outbound network attempts. Follow
LAN_ONLY_PLAN.md for the separate strict-LAN deployment milestone.

## Local checks

Use Go 1.26.6, Node >=18 and Python 3. Run the following from the repository root,
with USBRIDGE_WEB_GO pointing to an existing verified Go executable:

    USBRIDGE_WEB_GO=/path/to/go/bin/go bash .circleci/build-web.sh

Reports and ZIPs are written beneath artifacts/web-client. CI artifacts are test
deliverables, not a permanent release/update channel. This job publishes no
release and uses no vendor account credentials.
