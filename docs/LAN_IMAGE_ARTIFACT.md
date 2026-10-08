# Offline-loadable LAN host image

The CircleCI `lan-host` job exports `usbridge-lan-linux-amd64.tar.gz` only after
its Docker TLS, asset, manifest-serving and internal-network checks pass.
The image includes our source-built Go web client and HTTPS asset server.
It does not include vendor streamers, operator secrets, test TLS keys, a license
server, TURN relay or a connection-code directory. It is Linux amd64 only.

Download the image archive, `image-tag.txt`, `compose.yaml`, and `SHA256SUMS.txt`
from the same job. Also retain `image.txt`, `provenance.txt`, and this README for
the recorded source commit and image ID. Check the archive against its SHA-256
before loading; checksum files from the same download are corruption checks,
not an independent publisher signature.

```sh
sha256sum -c SHA256SUMS.txt
gunzip -c usbridge-lan-linux-amd64.tar.gz | docker load
export USBRIDGE_LAN_IMAGE="$(cat image-tag.txt)"
export USBRIDGE_TLS_DIR=/absolute/path/to/tls
export USBRIDGE_COMPONENT_DIR=/absolute/path/to/components
export USBRIDGE_AGENT_ORIGINS=https://192.168.1.8:8443
# Use the host's LAN address when making the server available to other clients.
export USBRIDGE_LAN_BIND=192.168.1.10
docker compose -f compose.yaml up -d --no-build --pull never
```

Supply your own browser-trusted certificate and key as `server.crt` and
`server.key`. The service runs as uid/gid 65532, so grant only that service the
necessary read access to the key. Do not make a private key world-readable.
The components directory needs a validated `manifest.json` and the files it
lists, readable by the service. Do not place credentials in that directory.
No directory is created silently if a bind source is missing.

Compose uses an internal Docker network, read-only mounts/filesystem, dropped
capabilities, and no-new-privileges. Host firewall and published-port behavior
still need validation on your Docker host. The CI test verifies this network
is marked internal and rejects a public TCP connection from its test client;
it is not an audit of the agent, browser, streamer or USB broker processes.

This image provides web assets and a local component mirror. A trusted agent
HTTPS endpoint and direct LAN connectivity are still required for browser
pairing/streaming. End-to-end media/device acceptance is separate and has not
been established just by successfully building or loading this image.
