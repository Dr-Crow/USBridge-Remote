# Owned WGL software capability probe

This CI-only helper creates one hidden owned window and WGL context. It does not
capture pixels, inject input, connect to the network or create child processes.
Its result is separate from the real viewer's HWND and changing-pixel gates.

Build in the native MSYS2 UCRT64 job, beside the staged viewer and DLL closure:

```sh
gcc -std=c11 -O2 -Wall -Wextra -Werror \
  .circleci/source/windows-graphics-probe/probe.c \
  -o "$WORK/bin/windows-graphics-probe.exe" -luser32 -lgdi32
```

Do not link `-lopengl32`: the helper explicitly loads the verified app-local
`opengl32.dll` with dependency search restricted to its directory and System32.
It sets literal `GALLIUM_DRIVER=llvmpipe` and `LIBGL_ALWAYS_SOFTWARE=true` before
loading Mesa. Only bounded printable ASCII GL vendor, renderer and version
strings are returned. The renderer must be LLVMpipe. Native diagnostics are
discarded, and the private output pipe receives one strict JSON object.

The tagged native test requires all five environment values:

- `WINDOWS_GRAPHICS_PROBE`: absolute path, basename `windows-graphics-probe.exe`
- `WINDOWS_GRAPHICS_PROBE_SHA256`: probe SHA-256
- `WINDOWS_GRAPHICS_PROBE_STAGING`: absolute path to the minimal staging JSON
- `WINDOWS_GRAPHICS_PROBE_STAGING_SHA256`: staging JSON SHA-256
- `WINDOWS_GRAPHICS_PROBE_RECEIPT`: new absolute output JSON path

The staging JSON has exactly `schema_version: 1` and `runtime_dlls_sha256`, the
full verified viewer DLL basename-to-SHA-256 map. Both `opengl32.dll` and
`libgallium_wgl.dll` must be included. Duplicate/case-aliased names, traversal,
nonregular files, symlinks/reparse points and changed hashes fail closed. The
harness holds read-only replacement-denying handles for the complete run.

Run from `.circleci/source/windows-preview-acceptance`:

```sh
go test -tags graphicsprobe -count=1 -run '^TestWindowsOwnedSoftwareGraphicsProbe$'
```

Parser regression tests are available with `-run '^TestGraphicsProbeStrictParsers$'`.
The helper is started by the existing atomic Job/private-pipe launcher with a
constructed environment. While the helper holds its context, the harness checks
only that PID's loaded modules. Mesa and every other loaded declared DLL must
come from its app-local directory; other modules must be the exact probe or
System32 modules. Full paths are never written to the receipt.

Closing stdin must naturally retire the process and empty the Job before any
safety close. The receipt includes GL fields, loaded module basenames/hashes,
and cleanup proof, with `actual_viewer_tested: false`. Passing this test does not
replace the real viewer's private-stdin startup or changing-owned-pixel tests.
