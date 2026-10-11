# Read-only current-process eligibility. No media, desktop capture or identity change.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
Set-StrictMode -Version Latest
$root = (Get-Location).Path
$work = Join-Path $root '.windows-preview-eligibility'
$out = Join-Path $root 'artifacts\windows-preview-eligibility'
if ((Test-Path $work) -or (Test-Path $out)) { throw 'Fresh work and receipt directories required' }
New-Item -ItemType Directory -Force $work, $out | Out-Null
$commit = (& git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $commit -cnotmatch '^[0-9a-f]{40}$' -or $commit -cne $env:CIRCLE_SHA1) { throw 'Exact source commit required' }
$archive = Join-Path $work 'go.zip'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
Invoke-WebRequest -UseBasicParsing -TimeoutSec 120 -Uri 'https://go.dev/dl/go1.26.9.windows-amd64.zip' -OutFile $archive
if ((Get-FileHash -Algorithm SHA256 $archive).Hash.ToLowerInvariant() -cne 'd722201a9c0c086d1610e111c48203009af690892ed708072bd5ae20160e7a59') { throw 'Go archive checksum mismatch' }
$tar = Join-Path $env:SystemRoot 'System32\tar.exe'
if (!(Test-Path -LiteralPath $tar -PathType Leaf)) { throw 'Native archive extractor unavailable' }
& $tar -xf $archive -C $work
if ($LASTEXITCODE -ne 0) { throw 'Verified archive extraction failed' }
$go = Join-Path $work 'go\bin\go.exe'
$env:GOROOT = Join-Path $work 'go'
$env:GOENV = 'off'; $env:GOFLAGS = '-buildvcs=false'; $env:GOCACHEPROG = ''; $env:GOEXPERIMENT = ''
$env:GOTOOLCHAIN = 'local'; $env:GOPROXY = 'off'; $env:GOWORK = 'off'; $env:CGO_ENABLED = '0'
$env:GOOS = 'windows'; $env:GOARCH = 'amd64'
$env:GOCACHE = Join-Path $work 'cache'; $env:GOPATH = Join-Path $work 'gopath'
if ((& $go version).Trim() -cne 'go version go1.26.9 windows/amd64') { throw 'Native toolchain mismatch' }
$module = Join-Path $work 'module'; New-Item -ItemType Directory $module | Out-Null
$names = @('eligibility.go','eligibility_other.go','eligibility_windows.go','eligibility_test.go','eligibility_windows_test.go')
$hashes = [ordered]@{}
foreach ($name in $names) {
    $source = Join-Path (Join-Path $root 'agent\internal\previeweligibility') $name
    $item = Get-Item -LiteralPath $source
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $item.Length -gt 65536) { throw 'Unexpected query source file' }
    $target = Join-Path $module $name; Copy-Item -LiteralPath $source -Destination $target
    $hashes[$name] = (Get-FileHash -Algorithm SHA256 $source).Hash.ToLowerInvariant()
    if ((Get-FileHash -Algorithm SHA256 $target).Hash.ToLowerInvariant() -cne $hashes[$name]) { throw 'Query source copy mismatch' }
}
[IO.File]::WriteAllText((Join-Path $module 'go.mod'), "module usbridge_agent/internal/previeweligibility`n`ngo 1.26.9`n", (New-Object Text.UTF8Encoding($false)))
$log = Join-Path $work 'tests.jsonl'
Push-Location $module
try {
    & $go test -json -count=1 -timeout=1m ./... 1> $log 2> (Join-Path $work 'stderr.txt')
    $testExit = $LASTEXITCODE
    & $go vet ./... 1> (Join-Path $work 'vet.txt') 2> (Join-Path $work 'vet-errors.txt')
    $vetExit = $LASTEXITCODE
} finally { Pop-Location }
$observations = @(); $failed = 0; $skipped = 0; $packages = 0
$passes = @{'TestEligibilityRequiresEveryIndependentFact'=0; 'TestWindowsReadOnlyEligibilityObservation'=0}
foreach ($line in Get-Content $log) {
    $row = $line | ConvertFrom-Json
    if ($row.Action -in @('fail','build-fail')) { $failed++ }
    if ($row.Action -ceq 'skip') { $skipped++ }
    if ($row.Action -ceq 'pass') {
        if ($null -eq $row.PSObject.Properties['Test']) { $packages++ }
        elseif ($passes.ContainsKey($row.Test)) { $passes[$row.Test]++ }
        else { throw 'Unexpected query test outcome' }
    }
    if ($null -ne $row.PSObject.Properties['Output'] -and [string]$row.Output -cmatch 'PREVIEW_ELIGIBILITY (\{[^\r\n]+\})') {
        $observations += ($Matches[1] | ConvertFrom-Json)
    }
}
if ($observations.Count -ne 1) { throw 'Missing unique query observation' }
$s = $observations[0]
$keys = @('token_queried','not_elevated','interactive_session','token_closed','window_station_queried','window_station_visible','eligible')
if (@($s.PSObject.Properties).Count -ne $keys.Count) { throw 'Unexpected query fields' }
foreach ($key in $keys) { if ($null -eq $s.PSObject.Properties[$key] -or $s.$key -isnot [bool]) { throw 'Invalid query field' } }
$eligible = $s.token_queried -and $s.not_elevated -and $s.interactive_session -and $s.token_closed -and $s.window_station_queried -and $s.window_station_visible
if ($s.eligible -ne $eligible) { throw 'Query predicate mismatch' }
$passed = $testExit -eq 0 -and $vetExit -eq 0 -and $failed -eq 0 -and $skipped -eq 0 -and $packages -eq 1
foreach ($name in $passes.Keys) { $passed = $passed -and $passes[$name] -eq 1 }
$receipt = [ordered]@{ schema_version=1; commit=$commit; platform='windows/amd64'; go_version='go1.26.9';
    passed=$passed; read_only_probe_completed=$passed; native_execution=$true; observation=$s; source_files_sha256=$hashes;
    test_passes=$passes; failures=$failed; skips=$skipped; native_vet=($vetExit -eq 0);
    manager_enabled=$false; capture_started=$false; child_launches=0; security_changes=$false;
    source_or_binary_artifacts_published=$false }
[IO.File]::WriteAllText((Join-Path $out 'eligibility.json'), (($receipt | ConvertTo-Json -Depth 6) + "`n"), (New-Object Text.UTF8Encoding($false)))
if (!$passed) { throw 'Read-only eligibility observation failed' }
Write-Host 'Read-only eligibility observation completed; inspect eligible separately from probe success.'
