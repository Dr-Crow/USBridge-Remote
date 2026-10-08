# Operator-managed agent HTTPS

Local browser access requires **both** the web host and agent HTTPS endpoint to
present certificates the browser trusts. Hosting the WASM alone is insufficient.
The agent can now load an operator-managed certificate/key without contacting
USBridge's certificate infrastructure.

Configure these YAML fields before starting the agent:

```yaml
local_tls_cert_file: /absolute/path/to/agent-fullchain.pem
local_tls_key_file: /absolute/path/to/agent-key.pem
```

On Windows use absolute paths with forward slashes, for example
`C:/USBridgeTLS/agent-fullchain.pem`. The PEM certificate must contain the leaf
first, then any intermediates. Use a server-auth leaf certificate whose SANs
cover the address clients actually connect to; a private-IP URL requires that
IP in an IP SAN. Do not use a CA certificate as the server leaf.

The pair is read at startup, without being copied into bundles or uploaded.
Missing halves, parse/key mismatches, CA certificates, absent SANs, expired or
not-yet-valid certificates, and incompatible extended key usage are rejected.
An explicit invalid pair fails startup instead of quietly reverting to another
certificate. The loaded pair takes priority for all agent TLS handshakes;
nonmatching SNI and subsequent expiry fail closed. Clients still perform normal
chain/hostname verification. The status UI says **Operator certificate (verify
client trust)** rather than claiming that an unknown issuer is trusted.

Manage certificate issuance, client trust and renewal through your own local CA
or existing certificate tooling. Only the agent's OS account should read its
private key. No automatic trust-store edits, browser warning bypasses, key
creation, CA enrollment, or external renewal are performed by this feature.
Restart the agent after replacing the files. Vendor certificate renewal/retry
is disabled while an operator pair is loaded, even in legacy online mode.

Validation includes an in-process TLS handshake with an explicitly trusted test
certificate and normal client verification, SNI/validity/purpose rejection, and
failed-load preservation. This does not prove browser pairing, media capture,
USB passthrough, or WAN-blocked streaming. Native CI and a real LAN browser
session are separate acceptance checks.
