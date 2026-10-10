# Synthetic process/pipe ownership only; no desktop, media, or private component.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$root = (Get-Location).Path
$work = Join-Path $root '.source-preview-windows-owner'
$out = Join-Path $root 'artifacts\windows-preview-process-evidence'
$tools = Join-Path $root '.source-preview-windows-process'
$go = Join-Path $tools 'go\bin\go.exe'
$archive = Join-Path $tools 'go1.26.9.windows-amd64.zip'
if ((Test-Path $work) -or !(Test-Path $go)) { throw 'Fresh owner work directory and preceding verified process gate required' }
if ((Get-FileHash -Algorithm SHA256 $archive).Hash.ToLowerInvariant() -cne 'd722201a9c0c086d1610e111c48203009af690892ed708072bd5ae20160e7a59') { throw 'Owner toolchain archive hash mismatch' }
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
if ((& $go version).Trim() -cne 'go version go1.26.9 windows/amd64') { throw 'Owner native toolchain mismatch' }
$commit = (& git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $commit -cnotmatch '^[0-9a-f]{40}$' -or $commit -cne $env:CIRCLE_SHA1) { throw 'Exact owner source commit required' }
$files = @('process.go', 'process_windows.go', 'process_other.go', 'process_test.go', 'process_windows_test.go', 'process_other_test.go')
$source = Join-Path $root 'agent\internal\previewprocess'
if (@(Get-ChildItem -LiteralPath $source -Filter '*.go').Count -ne $files.Count) { throw 'Unexpected owner source file inventory' }
$hashes = [ordered]@{}
foreach ($name in $files) {
    $original = Join-Path $source $name
    $target = Join-Path $work $name
    Copy-Item -LiteralPath $original -Destination $target
    $hash = (Get-FileHash -Algorithm SHA256 $original).Hash.ToLowerInvariant()
    if ((Get-FileHash -Algorithm SHA256 $target).Hash.ToLowerInvariant() -cne $hash) { throw 'Owner source copy hash mismatch' }
    $hashes[$name] = $hash
}
[IO.File]::WriteAllText((Join-Path $work 'go.mod'), "module usbridge_agent/internal/previewprocess`n`ngo 1.26.0`n", (New-Object Text.UTF8Encoding($false)))
$required = @('TestWindowsNativeLayoutsAndEmptyEnvironment', 'TestWindowsPrivatePipesExactOwnershipAndNaturalEOF',
    'TestWindowsHandleListExcludesUnknownInheritableHandle', 'TestWindowsRootCrashRetiresOutputHoldingDescendant',
    'TestWindowsZeroExitStillRecordsForcedDescendantCleanup', 'TestWindowsCancellationJoinsOwnedTree',
    'TestWindowsBlockedStdinCannotDeadlockStop', 'TestWindowsBlockedStdinCannotDeadlockCancellation',
    'TestWindowsStartupCanceledBeforeResume', 'TestWindowsOwnerExitBeforeResumeRetiresSuspendedChild',
    'TestWindowsRepeatedLifecycleReleasesHandles', 'TestWindowsBlockedOutputReadsJoinOnStop')
$selector = '^(' + ($required -join '|') + ')$'
$counts = [ordered]@{}
foreach ($name in $required) { $counts[$name] = 0 }
$log = Join-Path $work 'native.jsonl'
$stage = 'native_tests'
function Write-OwnerFailure {
    $names = @{}
    $locations = @{}
    if ((Test-Path $log) -and (Get-Item $log).Length -le 8MB) {
        foreach ($line in Get-Content $log) {
            try { $row = $line | ConvertFrom-Json } catch { continue }
            if ($row.Action -ceq 'fail' -and $null -ne $row.PSObject.Properties['Test'] -and $row.Test -cmatch '^Test[A-Za-z0-9_]{1,128}$') { $names[$row.Test] = $true }
            if ($null -ne $row.PSObject.Properties['Output']) {
                foreach ($match in [regex]::Matches([string]$row.Output, '(?m)^\s*(process_test\.go|process_windows_test\.go):([1-9][0-9]{0,4}):')) {
                    $locations[($match.Groups[1].Value + ':' + $match.Groups[2].Value)] = $true
                }
            }
        }
    }
    $receipt = [ordered]@{ schema_version = 1; passed = $false; source_commit = $commit; stage = $stage;
        test_names = @($names.Keys | Sort-Object); test_locations = @($locations.Keys | Sort-Object);
        raw_output_published = $false; production_integration_enabled = $false;
        window_or_media_started = $false; source_or_binary_artifacts_published = $false }
    [IO.File]::WriteAllText((Join-Path $out 'owner-failure.json'), (($receipt | ConvertTo-Json -Depth 6) + "`n"), (New-Object Text.UTF8Encoding($false)))
}
Push-Location $work
try {
    & $go test -json -count=20 -timeout=10m "-run=$selector" ./... 1> $log 2> (Join-Path $work 'native-stderr.txt')
    if ($LASTEXITCODE -ne 0) { Write-OwnerFailure; throw 'Native owner assertions failed; see bounded failure receipt' }
    $packages = 0
    foreach ($line in Get-Content $log) {
        $row = $line | ConvertFrom-Json
        if ($row.Action -in @('fail', 'skip', 'build-fail')) { throw 'Owner native failure or skip' }
        if ($row.Action -ceq 'pass') {
            if ($null -ne $row.PSObject.Properties['Test']) {
                if (!$counts.Contains($row.Test)) { throw 'Unexpected native owner assertion' }
                $counts[$row.Test]++
            } else { $packages++ }
        }
    }
    if ($packages -ne 1) { throw 'Missing owner package pass' }
    foreach ($name in $required) { if ($counts[$name] -ne 20) { throw 'Missing exact native owner repetitions' } }
    $stage = 'portable_and_native_contracts'
    $log = Join-Path $work 'contracts.jsonl'
    & $go test -json -count=1 -timeout=2m ./... 1> $log 2> (Join-Path $work 'contracts-stderr.txt')
    if ($LASTEXITCODE -ne 0) { Write-OwnerFailure; throw 'Owner contract checks failed' }
    $contractPasses = 0
    $contractPackages = 0
    foreach ($line in Get-Content $log) {
        $row = $line | ConvertFrom-Json
        if ($row.Action -in @('fail', 'skip', 'build-fail')) { Write-OwnerFailure; throw 'Owner contract failure or skip' }
        if ($row.Action -ceq 'pass') {
            if ($null -ne $row.PSObject.Properties['Test']) { $contractPasses++ } else { $contractPackages++ }
        }
    }
    if ($contractPackages -ne 1 -or $contractPasses -ne 29) { throw 'Missing owner contract assertions' }
    $stage = 'vet'
    & $go vet ./... 1> (Join-Path $work 'vet-stdout.txt') 2> (Join-Path $work 'vet-stderr.txt')
    if ($LASTEXITCODE -ne 0) { Write-OwnerFailure; throw 'Owner native vet failed' }
} catch { Write-OwnerFailure; throw } finally { Pop-Location }
$receipt = [ordered]@{ schema_version = 1; passed = $true; source_commit = $commit;
    platform = 'windows/amd64'; go_version = 'go1.26.9'; source_files_sha256 = $hashes;
    native_execution = $true; native_assertions = $counts; required_native_passes = 240;
    failures = 0; skips = 0; native_vet = $true; contract_test_passes = $contractPasses; production_integration_enabled = $false;
    window_or_media_started = $false; source_or_binary_artifacts_published = $false }
[IO.File]::WriteAllText((Join-Path $out 'owner.json'), (($receipt | ConvertTo-Json -Depth 6) + "`n"), (New-Object Text.UTF8Encoding($false)))
Write-Host 'Native pipe owner: all 12 assertions passed 20 times, plus contract tests and vet.'
