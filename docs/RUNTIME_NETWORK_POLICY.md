# Runtime-local versus strict-offline provisioning

The fork defaults `runtime_local: true`. This suppresses the agent's hosted
account, DNS/certificate, entitlement-refresh, signaling and TURN services and
embedded Tailscale startup. Agent/background component update polling is disabled.
RustShine launch arguments omit cached vendor TURN credentials and configure an
empty ICE list. This is application policy, not an OS firewall or proof that every
closed component is free of network traffic.

`strict_lan: true` is the stronger provisioning restriction: no public component
fetches, including explicit setup, are permitted. Use the existing validated disk,
ZIP or LAN-mirror sources in that mode. It overrides the provisioning exception.

When strict mode is off, ordinary first setup may fetch the selected Sunshine or
Punktfunk package. Explicit RustShine/USB setup can acquire a genuine vendor free
download entitlement and its signed component assets. A request-scoped context
permits only the known provisioning routes; it cannot enable account, billing,
TURN, DNS or certificate APIs and never temporarily disables a global policy.
Vendor tokens remain separate from local runtime claims.

A local retry loop checks whether a previously selected/consented component is
missing. Only while missing may it retry provisioning; once staged it makes no
periodic vendor renewal or update request. USB consent and pairing remain separate.
No shared GitHub credential is embedded. Stock acquisition still depends on the
vendor's provisioning service; bundles and a LAN mirror avoid that dependency.

`runtime_local: false` explicitly restores the old runtime-service behavior unless
strict mode is enabled. This is an advanced compatibility setting, not required
for ordinary local operation. Existing configs omitting the setting inherit the
new local-runtime policy; explicit false is serialized so it survives reload.

## Verification boundary

Unit tests check policy separation, context isolation, exact permitted API routes
and strict-mode precedence. Configuration and affected application tests must pass
on the committed source. Live per-process WAN-denial/packet capture, physical
streaming and all supported platforms remain acceptance gates. A passing parser
probe or Docker internal-network fixture does not replace those tests.
