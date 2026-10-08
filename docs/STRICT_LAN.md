# Strict-LAN development mode

This branch is implementing strict-LAN support in separate phases. It is not
yet a validated offline deployment. Do not treat a passing source build or
unit test as proof of browser streaming, USB parity or zero child-process egress.

## Agent request boundaries

`strict_lan: true` in the agent config, or `--strict-lan` for this launch, disables
vendor entitlement, account, DNS/certificate, hosted signaling relay and public
update/download requests before constructing a request or dialing. The policy
is applied before service constructors. The engine does not start the cloud
watchdogs or embedded Tailscale; Tailscale's own startup boundary also denies it.
Cached account credentials do not override this policy. Entitlement rechecks
do not downgrade an opted-in local runtime because a vendor token has expired.
Linux online USB-driver/clipboard installers and public benchmark downloads are
also disabled. Local benchmark files and OS permission handling remain separate.

These settings take effect on engine restart, rather than a live config save.
The normal online behavior remains the default. Existing `local_runtime_enabled`
consent, component download/launch consent, pinned binary verification, pairing
keys and transport security remain distinct requirements.

## LAN mirror boundary

The dedicated mirror client accepts literal private/link-local/loopback IP
addresses. Hostnames are rejected before DNS lookup. HTTPS is required except
loopback development/testing. Environment proxies and all redirects are
disabled. A configured private CA changes only this client's trust; no system
root is installed and certificate verification remains enabled. Administrator
configuration must pin the mirror manifest's SHA256 before component use.

## Remaining acceptance gates

Verified local component resolution is implemented (see LOCAL_COMPONENTS.md).
The source browser policy, local TLS/web configuration and Docker support are
being added next. Existing proprietary streamer defaults and
cached TURN files must be addressed explicitly, including executed verification
of empty ICE syntax on supported platforms. Request guards cannot prove what a
child binary does internally. Clean-token startup, captured/blocked egress,
restart/expiry, empty-cache browser reload and physical streaming/USB acceptance
remain required before declaring complete offline support.

## Source browser and LAN hosting

See [LAN_HOSTING.md](LAN_HOSTING.md) for the source HTTPS container, browser
request/ICE policy, explicit streamer empty-ICE arguments, component mirror and
remaining runtime/physical acceptance gates. Source-browser hosting forces
strict configuration and never includes proprietary components in its image.
