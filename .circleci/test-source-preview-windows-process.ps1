# Device-free native containment gate. Only its closed JSON receipt is published.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$timer = [Diagnostics.Stopwatch]::StartNew()
function Report-Stage([string] $name) { Write-Host ("process_gate_stage={0} elapsed_seconds={1}" -f $name, [int]$timer.Elapsed.TotalSeconds) }
Set-StrictMode -Version Latest
$root = (Get-Location).Path
$work = Join-Path $root '.source-preview-windows-process'
$out = Join-Path $root 'artifacts\windows-preview-process-evidence'
if ((Test-Path $work) -or (Test-Path $out)) { throw 'Process gate requires fresh work and receipt directories' }
New-Item -ItemType Directory -Force $work, $out | Out-Null
$commit = (& git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $commit -cnotmatch '^[0-9a-f]{40}$' -or $commit -cne $env:CIRCLE_SHA1) { throw 'Exact source commit required' }
$archiveSHA = 'd722201a9c0c086d1610e111c48203009af690892ed708072bd5ae20160e7a59'
$archive = Join-Path $work 'go1.26.9.windows-amd64.zip'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
Report-Stage 'download_toolchain'
Invoke-WebRequest -UseBasicParsing -TimeoutSec 120 -Uri 'https://go.dev/dl/go1.26.9.windows-amd64.zip' -OutFile $archive
if ((Get-FileHash -Algorithm SHA256 $archive).Hash.ToLowerInvariant() -cne $archiveSHA) { throw 'Go archive checksum mismatch' }
Report-Stage 'extract_toolchain'
Expand-Archive -LiteralPath $archive -DestinationPath $work
$go = Join-Path $work 'go\bin\go.exe'
$env:GOROOT = Join-Path $work 'go'
$env:GOENV = 'off'
$env:GOFLAGS = ''
$env:GOCACHEPROG = ''
$env:GOEXPERIMENT = ''
$env:GOPROXY = 'off'
$env:GOTOOLCHAIN = 'local'
$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:GOCACHE = Join-Path $work 'cache'
$env:GOPATH = Join-Path $work 'gopath'
$env:GOWORK = 'off'
$version = (& $go version).Trim()
if ($LASTEXITCODE -ne 0 -or $version -cne 'go version go1.26.9 windows/amd64') { throw 'Unexpected native Go toolchain' }
$log = Join-Path $work 'tests.jsonl'
$stderr = Join-Path $work 'test-stderr.txt'
function Report-TestFailure {
    # Raw test output can contain private paths or helper data. Publish only
    # Go identifiers, exact local test locations and enumerated fixed categories.
    $tests = @{}
    $locations = @{}
    $categories = @{}
    $snapshots = @{}
    $comparisons = 0
    $codes = @('job_create_failed', 'job_limits_failed', 'job_inventory_failed',
        'pipe_create_failed', 'pipe_inheritance_failed', 'handle_list_failed',
        'atomic_job_attribute_failed', 'suspended_launch_failed', 'atomic_job_membership_failed',
        'process_arguments_failed', 'process_environment_failed', 'process_handle_failed',
        'process_wait_handle_failed', 'child_resume_failed', 'child_nonzero_exit', 'safety_job_closed',
        'natural_exit_timeout', 'protocol_ended_early', 'protocol_timeout',
        'extra_child_output', 'protocol_eof_timeout', 'stderr_eof_timeout',
        'bounded_protocol_failed', 'unexpected_child_stderr')
    if ((Test-Path $log) -and (Get-Item $log).Length -le 8MB) {
        foreach ($line in Get-Content $log) {
            try { $row = $line | ConvertFrom-Json } catch { $categories['invalid_test_json'] = $true; continue }
            if ($row.Action -ceq 'pass' -and $null -ne $row.PSObject.Properties['Test'] -and $row.Test -ceq 'TestWindowsPipeOnlyLaunchDoesNotAllocateConsoleHost') { $comparisons++ }
            if ($row.Action -ceq 'build-fail') { $categories['build_failed'] = $true }
            if ($row.Action -ceq 'fail' -and $null -ne $row.PSObject.Properties['Test'] -and $row.Test -cmatch '^Test[A-Za-z0-9_]{1,128}$') { $tests[$row.Test] = $true }
            if ($null -ne $row.PSObject.Properties['Output']) {
                $message = [string]$row.Output
                foreach ($code in $codes) { if ($message.Contains($code)) { $categories[$code] = $true } }
                if ($message.Contains('panic: test timed out')) { $categories['test_timeout'] = $true }
                foreach ($match in [regex]::Matches($message, 'inventory_snapshot failed=([01]) assigned=([0-9]{1,10}) count=([0-9]{1,10}) zero=([0-9]{1,3}) wide=([0-9]{1,3}) current=([0-9]{1,3}) expected=([0-9]{1,3}) console_hosts=([0-9]{1,3}) other=([0-9]{1,3}) unavailable=([0-9]{1,3})')) {
                    $snapshots[$match.Value] = [ordered]@{ failed = [int]$match.Groups[1].Value; assigned = [uint32]$match.Groups[2].Value; count = [uint32]$match.Groups[3].Value; zero = [int]$match.Groups[4].Value; oversized = [int]$match.Groups[5].Value; current_process = [int]$match.Groups[6].Value; expected_processes = [int]$match.Groups[7].Value; system_console_hosts = [int]$match.Groups[8].Value; other = [int]$match.Groups[9].Value; unavailable = [int]$match.Groups[10].Value }
                }
                foreach ($match in [regex]::Matches($message, '(?m)^\s*(main_test\.go|safety_test\.go|winapi_windows_test\.go):([1-9][0-9]{0,4}):')) {
                    $locations[($match.Groups[1].Value + ':' + $match.Groups[2].Value)] = $true
                }
            }
        }
    } else { $categories['test_log_missing_or_oversize'] = $true }
    $failure = [ordered]@{
        schema_version = 1
        source_commit = $commit
        passed = $false
        stage = 'native_process_tests'
        test_names = @($tests.Keys | Sort-Object)
        test_locations = @($locations.Keys | Sort-Object)
        failure_categories = @($categories.Keys | Sort-Object)
        detached_comparison_passes = $comparisons
        inventory_snapshots = @($snapshots.Keys | Sort-Object | ForEach-Object { $snapshots[$_] })
        raw_output_published = $false
        media_session_started = $false
        source_or_binary_artifacts_published = $false
    }
    $json = $failure | ConvertTo-Json -Depth 6
    [IO.File]::WriteAllText((Join-Path $out 'process-failure.json'), ($json + [Environment]::NewLine), (New-Object Text.UTF8Encoding($false)))
    Write-Host $json
}
Push-Location (Join-Path $root '.circleci\source\windows-preview-acceptance')
try {
    Report-Stage 'native_tests'
    & $go test -json -count=20 -timeout=5m ./... 1> $log 2> $stderr
    if ($LASTEXITCODE -ne 0) { Report-TestFailure; throw 'Native process containment tests failed; raw helper diagnostics remain private' }
    Report-Stage 'native_vet'
    & $go vet ./... 1> (Join-Path $work 'vet-stdout.txt') 2> (Join-Path $work 'vet-stderr.txt')
    if ($LASTEXITCODE -ne 0) { throw 'Native process containment vet failed' }
} finally { Pop-Location }
$required = @(
    'TestWindowsAPILayouts',
    'TestWindowsPipeEndpointsBeginNoninheritable',
    'TestWindowsPipeOnlyLaunchDoesNotAllocateConsoleHost',
    'TestWindowsSuspendedLaunchPrivatePipesNaturalEOF',
    'TestWindowsJobCloseKillsInheritedDescendant',
    'TestWindowsParentCrashRetiresSuspendedChild'
)
$counts = [ordered]@{}
foreach ($name in $required) { $counts[$name] = 0 }
$packagePass = 0
$totalPass = 0
foreach ($line in Get-Content $log) {
    $row = $line | ConvertFrom-Json
    if ($row.Action -in @('fail', 'skip', 'build-fail')) { throw 'Unexpected failure or skip in native process tests' }
    if ($row.Action -ceq 'pass') {
        if ($null -ne $row.PSObject.Properties['Test']) {
            $totalPass++
            if ($counts.Contains($row.Test)) { $counts[$row.Test]++ }
        } else {
            if ($row.Package -cne 'usbridge.test/windows-preview-acceptance') { throw 'Unexpected test package' }
            $packagePass++
        }
    }
}
if ($packagePass -ne 1) { throw 'Missing unique package pass' }
foreach ($name in $required) { if ($counts[$name] -ne 20) { throw "Missing native test repetitions: $name" } }
$receipt = [ordered]@{
    schema_version = 1
    elapsed_seconds = [int]$timer.Elapsed.TotalSeconds
    source_commit = $commit
    platform = 'windows/amd64'
    go_version = $version
    go_archive_sha256 = $archiveSHA
    native_execution = $true
    tests = $counts
    total_test_passes = $totalPass
    failures = 0
    skips = 0
    atomic_job_membership = $true
    private_pipes = $true
    natural_eof_cleanup = $true
    descendant_job_close_cleanup = $true
    pre_resume_parent_crash_cleanup = $true
    production_manager_enabled = $false
    media_session_started = $false
    window_or_desktop_opened = $false
    input_injected = $false
    source_or_binary_artifacts_published = $false
}
[IO.File]::WriteAllText((Join-Path $out 'process.json'), (($receipt | ConvertTo-Json -Depth 6) + [Environment]::NewLine), (New-Object Text.UTF8Encoding($false)))
Write-Host 'Native atomic Job Object, private pipe, EOF, descendant and parent-crash checks passed 20 times.'
