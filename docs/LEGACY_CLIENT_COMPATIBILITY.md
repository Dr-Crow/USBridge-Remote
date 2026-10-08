# Legacy client protocol projection

The original native client understands `agent_protocol` values such as `free`,
`pro` and `opensource`. It does not understand our optional `agent_runtime`
metadata. Reporting only the raw vendor tariff therefore leaves a prepared local
runtime looking like Free to that client.

The agent's status and device-info responses now project `pro` into that legacy
field only when RustShine is the selected backend and its local runtime copy has
been prepared. Vendor Tier and signed credentials are unchanged and are never
replaced with a forged vendor token. Source clients still display `local` using
the separate runtime metadata.

This is protocol compatibility for the configured local Pro profile, not proof
of active streaming, a paid subscription, a specific codec or a working device.
Hardware and driver capability responses remain authoritative. An unprepared or
inactive local runtime is not promoted. Sunshine retains `opensource`; it must
not be mislabeled as RustShine because clients use that field for input mapping.

A stock native-client session and physical USB/tablet validation are still needed.
This projection does not disable a separate client's own cloud/network behavior;
our self-hosted web client has its own local network policy. It also does not
claim Enterprise-only capabilities absent from the tested local Pro profile.
