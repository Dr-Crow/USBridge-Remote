# Local component sources

Strict mode resolves components in this order: a verified installation in
`state_dir/local-components/<name>`; an explicitly selected directory; an
explicitly selected ZIP; an explicitly selected LAN mirror. If a selected source
is corrupt or unavailable the operation returns an error, with no public fallback.
No component is bundled, downloaded from a vendor or released by this mechanism.
Administrators supply lawful original files and a manifest for their own hosts.

Configuration:

```yaml
strict_lan: true
local_component_directory: /path/to/component-directory
# Alternatively: local_component_bundle: /path/to/offline-bundle.zip
# Alternatively: local_component_mirror: https://192.168.1.2:8443/components/
# Required for a mirror, recommended for local sources and explicit upgrades:
local_component_manifest_sha256: REPLACE_WITH_ACTUAL_MANIFEST_SHA256
# Optional private CA, scoped only to the mirror client:
local_component_ca_file: /path/to/local-ca.pem
```

The directory, ZIP root or mirror base contains `manifest.json` and the named
files at the same relative paths. A schema-1 manifest contains `components` with
`name`, `platform`, `version`, `profile`, `entry` and `files`. `files` contain
`path`, `size`, `sha256` and an optional `executable` boolean. `entry` must be
one of those files. Each file has a positive declared size and exact SHA256.
Platforms use Go's `windows/amd64`, `linux/amd64`, `darwin/arm64`, etc.; a matching
component is mandatory. Component names used by the agent are `sunshine`,
`punktfunk`, `rustshine` and `broker`. A profile describes compatibility; it is
not a license grant or proof of offline behavior. All dependencies/assets must
be declared, including the pinned Windows codec DLL alongside the RustShine
entry when using the existing local-runtime mechanism.

Paths must be relative, normalized, without traversal, backslashes, drive names
or percent-encoded ambiguity. Case-insensitive file collisions, DOS device names
and trailing-dot/space aliases are also rejected for portable Windows safety. Symlinks, duplicate ZIP entries, duplicate file
names, absent entries, wrong architecture, oversize data and mismatched hashes
are rejected. The manifest limit is 1 MiB; each declared file is limited to
512 MiB. The original directory/archive is never edited. Selected data is copied
to a fresh directory and verified before activation. The prior complete tree is
kept as `<name>.previous`; a failed activation restores it. Windows file locks
can prevent an update, which fails without overwriting the active installation.
This is process-level activation/rollback, not a proved power-loss guarantee.

Installed reuse is intentional. To select a new version, change the configured
manifest pin to the new manifest's hash and select its source. Launch rehashes
the installed manifest/files; UI/status reads use process-only readiness state
without hashing files or contacting a mirror. Every restart must revalidate the
installation; legacy copies, PATH and environment overrides do not bypass strict
resolution.

RustShine and the USB broker still require their separate component consent and
explicit `local_runtime_enabled` research consent. Their existing pinned binary
and codec checks are unchanged. The resolver itself never launches or modifies a
binary, issues entitlement, logs a pairing key or installs a driver. Mac Intel
has no existing pinned RustShine/broker pair; a matching manifest cannot create
support where the pinned runtime has none.

These checks do not establish proprietary source recovery or offline streaming.
Public-free ICE/TURN handling, supplied local HTTPS, browser policy, Docker tests
and captured/blocked egress plus physical acceptance remain separate milestones.
