# Native agent launch adapters with owned synthetic children only.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$root = (Get-Location).Path
$work = Join-Path $root '.source-preview-windows-wiring'
$out = Join-Path $root 'artifacts\windows-preview-process-evidence'
$tools = Join-Path $root '.source-preview-windows-process'
$go = Join-Path $tools 'go\bin\go.exe'
if ((Test-Path $work) -or !(Test-Path $go)) { throw 'Fresh work directory and verified process gate required' }
if ((Get-FileHash -Algorithm SHA256 (Join-Path $tools 'go1.26.9.windows-amd64.zip')).Hash.ToLowerInvariant() -cne 'd722201a9c0c086d1610e111c48203009af690892ed708072bd5ae20160e7a59') { throw 'Wiring toolchain checksum mismatch' }
$commit = (& git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $commit -cnotmatch '^[0-9a-f]{40}$' -or $commit -cne $env:CIRCLE_SHA1) { throw 'Exact source commit required' }
$owner = Get-Content -Raw (Join-Path $out 'owner.json') | ConvertFrom-Json
if ($owner.passed -ne $true -or $owner.source_commit -cne $commit -or $owner.required_native_passes -ne 240 -or $owner.failures -ne 0 -or $owner.skips -ne 0 -or $owner.native_execution -ne $true -or $owner.platform -cne 'windows/amd64' -or $owner.go_version -cne 'go1.26.9') { throw 'Same-commit native owner prerequisite missing' }
New-Item -ItemType Directory -Force $work, $out | Out-Null
$env:GOROOT = Join-Path $tools 'go'
$env:GOTOOLCHAIN = 'local'
$env:GOENV = 'off'
$env:GOFLAGS = '-buildvcs=false'
$env:GOCACHEPROG = ''
$env:GOEXPERIMENT = ''
$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:GOPROXY = 'off'
$env:GOWORK = 'off'
$env:GOCACHE = Join-Path $tools 'cache'
$env:GOPATH = Join-Path $tools 'gopath'
if ((& $go version).Trim() -cne 'go version go1.26.9 windows/amd64') { throw 'Native toolchain mismatch' }
$files = @(
    'internal/componentjson/decode.go',
    'internal/componentjson/decode_test.go',
    'internal/localcomponents/resolver.go',
    'internal/localcomponents/resolver_test.go',
    'internal/localcomponents/staging_contract_test.go',
    'internal/netpolicy/local_http.go',
    'internal/netpolicy/local_http_test.go',
    'internal/netpolicy/policy.go',
    'internal/netpolicy/policy_test.go',
    'internal/netpolicy/provisioning_test.go',
    'internal/previewchild/child.go',
    'internal/previewchild/child_other.go',
    'internal/previewchild/child_other_test.go',
    'internal/previewchild/child_test.go',
    'internal/previewchild/child_windows.go',
    'internal/previewchild/child_windows_test.go',
    'internal/previewprocess/process.go',
    'internal/previewprocess/process_other.go',
    'internal/previewprocess/process_other_test.go',
    'internal/previewprocess/process_test.go',
    'internal/previewprocess/process_windows.go',
    'internal/previewprocess/process_windows_test.go',
    'internal/sourcepreview/acceptance_observer.go',
    'internal/sourcepreview/acceptance_observer_test.go',
    'internal/sourcepreview/manager.go',
    'internal/sourcepreview/manager_test.go',
    'internal/sourcepreview/native_acceptance_test.go',
    'internal/sourcepreview/viewer.go',
    'internal/sourcepreview/viewer_lifecycle_test.go',
    'internal/sourcepreview/viewer_other_test.go',
    'internal/sourcepreview/viewer_test.go',
    'internal/sourcepreview/viewer_windows_test.go',
    'internal/sourcestreamer/canonical_launch_test.go',
    'internal/sourcestreamer/lifecycle_test.go',
    'internal/sourcestreamer/process_other_test.go',
    'internal/sourcestreamer/process_windows_test.go',
    'internal/sourcestreamer/stdio.go',
    'internal/sourcestreamer/supervisor.go',
    'internal/sourcestreamer/supervisor_test.go',
    'internal/sourcestreamer/terminal_test.go'
)
if (@($owner.source_files_sha256.PSObject.Properties).Count -ne 6) { throw 'Unexpected owner prerequisite inventory' }
$hashes = [ordered]@{}
foreach ($name in $files) {
    $original = Join-Path (Join-Path $root 'agent') $name
    $item = Get-Item -LiteralPath $original
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $item.Length -gt 1MB) { throw 'Unexpected agent source member' }
    $target = Join-Path $work $name
    New-Item -ItemType Directory -Force (Split-Path -Parent $target) | Out-Null
    Copy-Item -LiteralPath $original -Destination $target
    $hash = (Get-FileHash -Algorithm SHA256 $original).Hash.ToLowerInvariant()
    if ((Get-FileHash -Algorithm SHA256 $target).Hash.ToLowerInvariant() -cne $hash) { throw 'Agent source copy mismatch' }
    if ($name.StartsWith('internal/previewprocess/', [StringComparison]::Ordinal)) {
        $property = $owner.source_files_sha256.PSObject.Properties[(Split-Path -Leaf $name)]
        if ($null -eq $property -or $property.Value -cne $hash) { throw 'Owner package changed after native prerequisite' }
    }
    $hashes[$name] = $hash
}
[IO.File]::WriteAllText((Join-Path $work 'go.mod'), "module usbridge_agent`n`ngo 1.26.9`n", (New-Object Text.UTF8Encoding($false)))
$required = @('TestWindowsSourceCooperativeEOFIsNatural', 'TestWindowsSourceCrashRetiresInheritedOutput',
    'TestWindowsSourceZeroExitStillRecordsForcedCleanup', 'TestWindowsSourceBlockedStdinStopAndCancel',
    'TestWindowsSourceStartupCancellationJoinsBlockedWrite', 'TestWindowsSourceTypedFrameFailure',
    'TestWindowsViewerCooperativeEOFIsNatural', 'TestWindowsViewerCrashRetiresInheritedOutput',
    'TestWindowsViewerZeroExitStillRecordsForcedCleanup', 'TestWindowsViewerBlockedStdinStopAndCancel',
    'TestWindowsViewerStartupCancellationJoinsBlockedWrite', 'TestWindowsManagerRemainsDisabled',
    'TestWindowsExplicitEnvironmentDropsOnlyDrivePseudoVariables')
$subtests = @('TestWindowsSourceBlockedStdinStopAndCancel/false', 'TestWindowsSourceBlockedStdinStopAndCancel/true',
    'TestWindowsViewerBlockedStdinStopAndCancel/false', 'TestWindowsViewerBlockedStdinStopAndCancel/true')
$portable = @('TestJoinSharesOneDeadlineAndRetainsTimeout', 'TestLifecyclePreservesTypedOwnerFailures', 'TestViewerLifecyclePreservesTypedOwnerFailures')
$packages = @('./internal/previewchild', './internal/sourcestreamer', './internal/sourcepreview')
$log = Join-Path $work 'native.jsonl'
$stage = 'native_launch_adapters'
function Write-WiringFailure {
    $names = @{}
    $locations = @{}
    if ((Test-Path $log) -and (Get-Item $log).Length -le 8MB) {
        foreach ($line in Get-Content $log) {
            try { $row = $line | ConvertFrom-Json } catch { continue }
            if ($row.Action -ceq 'fail' -and $null -ne $row.PSObject.Properties['Test'] -and $row.Test -cmatch '^Test[A-Za-z0-9_]{1,128}(/(true|false))?$') { $names[$row.Test] = $true }
            if ($null -ne $row.PSObject.Properties['Output']) {
                foreach ($match in [regex]::Matches([string]$row.Output, '(?m)^\s*(child_test\.go|child_windows_test\.go|process_windows_test\.go|viewer_windows_test\.go|viewer_lifecycle_test\.go|lifecycle_test\.go):([1-9][0-9]{0,4}):')) {
                    $locations[($match.Groups[1].Value + ':' + $match.Groups[2].Value)] = $true
                }
            }
        }
    }
    $failure = [ordered]@{ schema_version = 1; passed = $false; source_commit = $commit; stage = $stage;
        test_names = @($names.Keys | Sort-Object); test_locations = @($locations.Keys | Sort-Object);
        raw_output_published = $false; production_manager_enabled = $false;
        window_or_media_started = $false; source_or_binary_artifacts_published = $false }
    [IO.File]::WriteAllText((Join-Path $out 'wiring-failure.json'), (($failure | ConvertTo-Json -Depth 6) + "`n"), (New-Object Text.UTF8Encoding($false)))
}
function Read-ExactPasses($expected) {
    $counts = [ordered]@{}
    foreach ($name in $expected) { $counts[$name] = 0 }
    $packageNames = @{}
    foreach ($line in Get-Content $log) {
        $row = $line | ConvertFrom-Json
        if ($row.Action -in @('fail', 'skip', 'build-fail')) { throw 'Native wiring failure or skip' }
        if ($row.Action -ceq 'pass') {
            if ($null -ne $row.PSObject.Properties['Test']) {
                if (!$counts.Contains($row.Test)) { throw 'Unexpected wiring assertion' }
                $counts[$row.Test]++
            } else {
                if ($row.Package -cnotmatch '^usbridge_agent/internal/(previewchild|sourcestreamer|sourcepreview)$' -or $packageNames.ContainsKey($row.Package)) { throw 'Unexpected wiring package' }
                $packageNames[$row.Package] = $true
            }
        }
    }
    if ($packageNames.Count -ne 3) { throw 'Missing wiring package passes' }
    foreach ($name in $expected) { if ($counts[$name] -ne 20) { throw 'Missing exact wiring repetitions' } }
    return ,$counts
}
Push-Location $work
try {
    $selector = '^(' + ($required -join '|') + ')$'
    & $go test -json -count=20 -timeout=10m "-run=$selector" @packages 1> $log 2> (Join-Path $work 'native-stderr.txt')
    if ($LASTEXITCODE -ne 0) { throw 'Native production adapter tests failed' }
    $nativeCounts = Read-ExactPasses ($required + $subtests)
    $stage = 'portable_cleanup_contracts'
    $log = Join-Path $work 'contracts.jsonl'
    $selector = '^(' + ($portable -join '|') + ')$'
    & $go test -json -count=20 -timeout=2m "-run=$selector" @packages 1> $log 2> (Join-Path $work 'contracts-stderr.txt')
    if ($LASTEXITCODE -ne 0) { throw 'Portable lifecycle contracts failed' }
    $contractCounts = Read-ExactPasses $portable
    $stage = 'vet'
    & $go vet ./... 1> (Join-Path $work 'vet-stdout.txt') 2> (Join-Path $work 'vet-stderr.txt')
    if ($LASTEXITCODE -ne 0) { throw 'Native production adapter vet failed' }
} catch { Write-WiringFailure; throw } finally { Pop-Location }
$receipt = [ordered]@{ schema_version = 1; passed = $true; source_commit = $commit;
    platform = 'windows/amd64'; go_version = 'go1.26.9'; source_files_sha256 = $hashes;
    native_execution = $true; native_assertions = $nativeCounts; portable_contracts = $contractCounts;
    required_native_top_level_passes = 260; required_native_leaf_cases = 300; portable_contract_passes = 60;
    failures = 0; skips = 0; native_vet = $true; production_launch_adapters_exercised = $true;
    production_manager_enabled = $false; window_or_media_started = $false; source_or_binary_artifacts_published = $false }
[IO.File]::WriteAllText((Join-Path $out 'wiring.json'), (($receipt | ConvertTo-Json -Depth 6) + "`n"), (New-Object Text.UTF8Encoding($false)))
Write-Host 'Native production launch adapters: 13 assertions and their subcases passed 20 times, plus contracts and vet.'
