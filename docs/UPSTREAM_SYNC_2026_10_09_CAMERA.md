# Follow-on upstream integration: camera and isochronous USB

This follows the eight-commit integration at fork `92281bbe29af49177529cfc1329a3a4dbc926471`.
The additional upstream range is `b78722269924eceec23d575445926bf33879b1b9`
through `e509a87a1096e6b1ce275ed06d277103f5d93257`:

- `e675488`: isochronous USB endpoints and alternate settings.
- `6659600`: client camera forwarding to a remote virtual USB camera.
- `35f8693`: source version bump to 3.0.105.
- `e509a87`: camera software-encoding limitation documentation.

The range changes 29 text files and updates Moonlight to
`a232e27d5c423eb8e7de8da91f0eefcf9171348f`. All fetched upstream text files
matched their declared Git blob hashes. Three-way merges against the published
fork completed without conflicts. One upstream Go file received gofmt cleanup.
The only new agent backend change adds `camera_sink` to the existing default USB
sink list; explicitly configured values remain preserved. Existing local component
resolution, strict-LAN policy, entitlement handling and protected-device consent
are not intentionally changed by this integration.

## Limits and local checks

- Changed Go sources parse with gofmt and are formatted.
- A new regression test checks that unsupported-platform camera stubs advertise
  no support/devices and return an error without invoking the input callback.
- The stub test passed ten repetitions on Linux with CGO disabled. The isolated
  stub test binary cross-compiled for Linux, Windows and macOS, amd64/arm64.
- These are isolated stub tests, not full client/agent builds or camera capture.
- Native Linux camera compilation needs libavcodec/libavutil/libswscale development
  packages, absent in the current cloud executor. Physical V4L2 camera, virtual
  USB camera and isochronous audio/video hardware tests have not run.
- Upstream currently implements camera capture on Linux; encoding is software.
  Neither Windows/macOS camera support nor GPU camera encoding is inferred from
  successful cross-compilation of unsupported-platform stubs.
- The existing audited proprietary component baseline remains v0.3.131. Source
  declarations do not establish that those binaries support the new camera/ISO
  features. A fresh authorized manifest and component compatibility test are
  required before advertising those features in an accepted runtime bundle.
- The latest public release endpoint still reported release-v3.0.104 when checked
  on October 9 at 22:24 UTC, despite the source version increment.

This checkpoint belongs on the integration branch pending the complete hosted
matrix and local/hardware acceptance. Do not promote a working release based on
these focused checks alone. CircleCI was deliberately pausing new starts during
publication; pending/no-workflow records are not passes.
