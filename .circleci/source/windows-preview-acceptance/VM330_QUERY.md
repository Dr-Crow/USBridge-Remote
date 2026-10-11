# VM330 read-only GDI+ signature prerequisite

This test-only profile is compiled with `signatureprobe,vm330_signature_probe`.
It queries three times one of the two exact installed WinSxS assemblies recorded
in the test and requires SHA-256
`50453fb310b9863066b40a0cda1204e86b6b86f261aa4f806e7cb89aa51215ee`.
The fixed PowerShell script must return the existing Windows catalog policy,
OS-binary status, Microsoft Windows publisher and pinned signer. File final path
and retained read-only lock are checked. The same absolute 12-second invocation
budget, exact root/conhost ownership, natural exit and all joins remain required.

These files end in `_test.go`: normal helper builds and production/default CI
module admission keep the original `6aa65963...` hash. This prerequisite never
loads GDI+, captures a screen, starts a viewer or admits an OS module. Successful
query evidence is required before reviewing a separate VM viewer fixture change.

Build with official Go 1.26.9, CGO disabled:
`go test -c -tags=signatureprobe,vm330_signature_probe -o vm330-signature-query.exe .`

In the already approved ordinary desktop PowerShell session, set `CIRCLE_SHA1`
to this exact source commit and `WINDOWS_SIGNATURE_GDIPLUS_RECEIPT` to a fresh
absolute JSON path in an existing writable directory. Then run:
`./vm330-signature-query.exe -test.run=^TestWindowsSignatureVerifierExactGDIPlus$ -test.v -test.timeout=60s`

The receipt must say passed=true, viewer_module_accepted=false and have three
strict inspections with zero exits, natural cleanup and no safety closure.
No account, token, certificate, execution-policy or machine setting is changed.

## Closed startup diagnostic variant

The VM tag also retains only a bounded stderr byte count and fixed pattern
booleans after the reader joins. The normal build still discards stderr. Both
variants keep any stderr fatal. A missing marker remains the primary failure;
classification never admits a module or changes policy. Incomplete, invalid
encoding, read-failed or mixed known errors are not classified as policy proof.
Raw stderr is cleared and is never written into a receipt.
