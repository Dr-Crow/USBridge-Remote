# Isolated source-owned signature process prerequisite. Raw logs never published.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
Set-StrictMode -Version Latest
$timer = [Diagnostics.Stopwatch]::StartNew()
$root = (Get-Location).Path
$work = Join-Path $root '.signature-owner-private'
$out = Join-Path $root 'signature-owner-evidence'
$stage = 'prepare'
$passed = $false
$outputOwned = $false
$commit = ''
$version = ''
$testExit = $null
$vetExit = $null
$counts = [ordered]@{}
$proofs = New-Object 'Collections.Generic.List[object]'
$failedTests = @{}
$failureCategories = @{}
$candidateCodes = @{}
$archiveSHA = 'd722201a9c0c086d1610e111c48203009af690892ed708072bd5ae20160e7a59'
$scriptSHA = 'f8af1cb208563f542d50b40d7c2b2959f5baa3ecc64518f5df3ad5b67a0805fb'
$ownerCodes = @('atomic_job_attribute_failed', 'bounded_protocol_failed', 'child_nonzero_exit', 'component_identity_changed', 'component_lock_failed', 'component_path_failed', 'extra_child_output', 'file_bounds_failed', 'file_read_failed', 'handle_list_failed', 'job_create_failed', 'job_inventory_failed', 'job_limits_failed', 'nonregular_component', 'owned_job_not_empty', 'owned_process_open_failed', 'owned_process_path_failed', 'pipe_create_failed', 'pipe_inheritance_failed', 'process_arguments_failed', 'process_environment_failed', 'protocol_ended_early', 'safety_job_closed', 'signature_budget_invalid', 'signature_cleanup_failed', 'signature_cleanup_incomplete', 'signature_context_missing', 'signature_dependency_bounds', 'signature_dependency_invalid', 'signature_duplicate_member', 'signature_final_path_failed', 'signature_host_final_path_failed', 'signature_host_hash_failed', 'signature_host_identity_failed', 'signature_host_lock_failed', 'signature_host_membership_failed', 'signature_host_not_observed_before_result', 'signature_host_pin_mismatch', 'signature_input_bounds', 'signature_input_failed', 'signature_input_invalid', 'signature_input_lines', 'signature_input_partial', 'signature_input_timeout', 'signature_inventory_failed', 'signature_job_accounting_failed', 'signature_member_inspection_failed', 'signature_member_not_owned', 'signature_output_bounds', 'signature_output_invalid', 'signature_owner_retirement_failed', 'signature_path_invalid', 'signature_pin_invalid', 'signature_pinned_final_path_failed', 'signature_pinned_hash_failed', 'signature_process_nonzero_exit', 'signature_process_wait_failed', 'signature_resource_close_uncertain', 'signature_root_hash_failed', 'signature_root_identity_failed', 'signature_root_membership_failed', 'signature_root_pin_missing', 'signature_script_location_invalid', 'signature_startup_early_exit', 'signature_startup_early_output', 'signature_startup_graph_incomplete', 'signature_startup_host_exited', 'signature_startup_inventory_failed', 'signature_startup_marker_invalid', 'signature_startup_marker_missing', 'signature_startup_timeout', 'signature_suspended_root_incomplete', 'signature_system_binary_path_invalid', 'signature_system_directory_failed', 'signature_system_directory_mismatch', 'signature_unknown_or_late_member', 'signature_work_identity_failed', 'signature_work_invalid', 'signature_work_lock_failed', 'suspended_launch_failed', 'unexpected_child_stderr', 'unretired_job_member')
$required = @('TestExtractionNativeOriginalSignature', 'TestExtractionNativeFailClosed', 'TestExtractionNativePinsBeforeLaunch')
foreach ($name in $required) { $counts[$name] = 0 }
$utf8 = New-Object Text.UTF8Encoding($false)
function Write-Receipt([string]$name, $value) {
    if ($name -cnotmatch '^(summary|positive-proof)\.json$') { throw 'receipt_name_invalid' }
    [IO.File]::WriteAllText((Join-Path $out $name), (($value | ConvertTo-Json -Depth 8) + [Environment]::NewLine), $utf8)
}
function Remaining-Millis {
    $remaining = 600000 - [int64]$timer.Elapsed.TotalMilliseconds
    if ($remaining -le 0) { throw 'aggregate_deadline_exceeded' }
    return [int][Math]::Min($remaining, [int]::MaxValue)
}
function Report-Stage([string]$value) {
    Write-Host ('signature_owner_stage={0} elapsed_seconds={1}' -f $value, [int]$timer.Elapsed.TotalSeconds)
}
function Invoke-Bounded([string]$file, [string[]]$arguments, [string]$stdout, [string]$stderr, [int]$capMilliseconds) {
    $wait = [Math]::Min((Remaining-Millis), $capMilliseconds)
    $commandTimer = [Diagnostics.Stopwatch]::StartNew()
    # Fixed CI build-tool commands only. The extracted Go owner independently
    # establishes containment for the actual signature query under test.
    $info = New-Object Diagnostics.ProcessStartInfo
    $info.FileName = $file
    $info.Arguments = $arguments -join ' '
    $info.WorkingDirectory = (Get-Location).Path
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    $process = New-Object Diagnostics.Process
    $process.StartInfo = $info
    $stdoutFile = $null
    $stderrFile = $null
    try {
        $stdoutFile = [IO.File]::Open($stdout, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
        $stderrFile = [IO.File]::Open($stderr, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
        if (-not $process.Start()) { throw 'tool_start_failed' }
        $stdoutDone = $process.StandardOutput.BaseStream.CopyToAsync($stdoutFile)
        $stderrDone = $process.StandardError.BaseStream.CopyToAsync($stderrFile)
        if (-not $process.WaitForExit($wait)) {
            try { $process.Kill() } catch { }
            $null = $process.WaitForExit(5000)
            throw 'tool_deadline_exceeded'
        }
        # Join both redirected streams before their files can be parsed. No
        # parameterless WaitForExit or fresh per-drain timeout extends the budget.
        $remaining = [Math]::Min((Remaining-Millis), ($wait - [int]$commandTimer.Elapsed.TotalMilliseconds))
        if ($remaining -le 0 -or -not [Threading.Tasks.Task]::WaitAll([Threading.Tasks.Task[]]@($stdoutDone, $stderrDone), [int]$remaining)) { throw 'tool_output_join_failed' }
        $stdoutFile.Flush(); $stderrFile.Flush()
        return [int]$process.ExitCode
    } finally {
        if ($null -ne $stdoutFile) { $stdoutFile.Dispose() }
        if ($null -ne $stderrFile) { $stderrFile.Dispose() }
        $process.Dispose()
    }
}
function Add-Owner-Candidates([string]$message, [hashtable]$candidates) {
    # Strings in positive JSON may be field names (safety_job_closed:false).
    # This collection is diagnostic-only; it cannot decide whether tests failed.
    foreach ($code in $ownerCodes) { if ($message.Contains($code)) { $candidates[$code] = $true } }
    if ($message.Contains('context deadline exceeded')) { $candidates['context_deadline_exceeded'] = $true }
    if ($message.Contains('panic: test timed out')) { $candidates['go_test_timeout'] = $true }
}
function Promote-Failed-Diagnostics([hashtable]$categories, [hashtable]$candidates, [int]$exitCode) {
    $actualFailure = $exitCode -ne 0 -or $categories.ContainsKey('fail') -or $categories.ContainsKey('skip') -or $categories.ContainsKey('build-fail')
    if ($actualFailure) { foreach ($code in $candidates.Keys) { $categories[$code] = $true } }
}
function Test-Diagnostic-Classification {
    # Deterministic, dependency-free regression runs before native tools. The
    # same diagnostic string must stay harmless for pass and informative for fail.
    $positiveCandidates = @{}
    Add-Owner-Candidates '{"safety_job_closed":false}' $positiveCandidates
    if (-not $positiveCandidates.ContainsKey('safety_job_closed')) { throw 'classification_fixture_invalid' }
    $success = @{}
    Promote-Failed-Diagnostics $success $positiveCandidates 0
    if ($success.Count -ne 0) { throw 'positive_diagnostic_became_failure' }
    # Exercise the actual closed-field proof parser, not substring inference.
    $proof = [pscustomobject][ordered]@{
        resources_released=$true; startup_handshake_verified=$true
        suspended_total_processes=1; suspended_active_processes=1; suspended_members=1; suspended_root_verified=$true
        verifier_sha256=('a' * 64); verifier_script_sha256=$scriptSHA; console_host_sha256=('b' * 64)
        console_host_identity_verified=$true; total_owned_processes=2; natural_cleanup=$true
        cleanup_joined=$true; watchdog_joined=$true; safety_job_closed=$false
        root_exit_code=0; console_host_exit_code=0; elapsed_ms=1
    }
    $null = Exact-Positive-Proof $proof
    $proof.safety_job_closed = $true
    $forcedRejected = $false
    try { $null = Exact-Positive-Proof $proof } catch { $forcedRejected = $true }
    if (-not $forcedRejected) { throw 'forced_cleanup_proof_accepted' }
    $forcedCandidates = @{}
    Add-Owner-Candidates '{"safety_job_closed":true}' $forcedCandidates
    $forcedFailure = @{fail=$true}
    Promote-Failed-Diagnostics $forcedFailure $forcedCandidates 0
    if (-not $forcedFailure.ContainsKey('safety_job_closed') -or $forcedFailure.Count -ne 2) { throw 'forced_failure_diagnostic_missing' }
    $failedCandidates = @{}
    Add-Owner-Candidates 'signature_host_identity_failed' $failedCandidates
    foreach ($action in @('fail', 'skip', 'build-fail')) {
        $failure = @{}; $failure[$action] = $true
        Promote-Failed-Diagnostics $failure $failedCandidates 0
        if (-not $failure.ContainsKey('signature_host_identity_failed') -or $failure.Count -ne 2) { throw 'failed_diagnostic_missing' }
    }
    $nonzero = @{}
    Promote-Failed-Diagnostics $nonzero $failedCandidates 1
    if (-not $nonzero.ContainsKey('signature_host_identity_failed') -or $nonzero.Count -ne 1) { throw 'nonzero_diagnostic_missing' }
}
function Exact-Positive-Proof($proof) {
    $fields = @('resources_released', 'startup_handshake_verified', 'suspended_total_processes', 'suspended_active_processes', 'suspended_members', 'suspended_root_verified', 'verifier_sha256', 'verifier_script_sha256', 'console_host_sha256', 'console_host_identity_verified', 'total_owned_processes', 'natural_cleanup', 'cleanup_joined', 'watchdog_joined', 'safety_job_closed', 'root_exit_code', 'console_host_exit_code', 'elapsed_ms')
    $actual = @($proof.PSObject.Properties.Name)
    if ($actual.Count -ne $fields.Count) { throw 'positive_proof_schema_invalid' }
    foreach ($name in $actual) { if ($name -cnotin $fields) { throw 'positive_proof_schema_invalid' } }
    foreach ($name in @('resources_released', 'startup_handshake_verified', 'suspended_root_verified', 'console_host_identity_verified', 'natural_cleanup', 'cleanup_joined', 'watchdog_joined')) {
        if ($proof.$name -isnot [bool] -or -not $proof.$name) { throw 'positive_proof_boolean_invalid' }
    }
    if ($proof.safety_job_closed -isnot [bool] -or $proof.safety_job_closed) { throw 'positive_proof_forced_cleanup' }
    foreach ($name in @('suspended_total_processes', 'suspended_active_processes', 'suspended_members', 'total_owned_processes', 'root_exit_code', 'console_host_exit_code', 'elapsed_ms')) {
        if ($proof.$name -isnot [int] -and $proof.$name -isnot [long]) { throw 'positive_proof_number_invalid' }
    }
    foreach ($name in @('suspended_total_processes', 'suspended_active_processes', 'suspended_members')) { if ($proof.$name -ne 1) { throw 'positive_proof_startup_invalid' } }
    if ($proof.total_owned_processes -ne 2 -or $proof.root_exit_code -ne 0 -or $proof.console_host_exit_code -ne 0 -or $proof.elapsed_ms -lt 0 -or $proof.elapsed_ms -gt 60000) { throw 'positive_proof_retirement_invalid' }
    foreach ($name in @('verifier_sha256', 'verifier_script_sha256', 'console_host_sha256')) { if ($proof.$name -cnotmatch '^[0-9a-f]{64}$') { throw 'positive_proof_pin_invalid' } }
    if ($proof.verifier_script_sha256 -cne $scriptSHA) { throw 'positive_proof_script_mismatch' }
    # Reconstruct only this closed source-free schema. No raw test object is
    # written, even though the test's Result already omits its Output field.
    $clean = [ordered]@{}
    foreach ($name in $fields) { $clean[$name] = $proof.$name }
    return $clean
}
try {
    if ((Test-Path -LiteralPath $work) -or (Test-Path -LiteralPath $out)) { throw 'fresh_directories_required' }
    New-Item -ItemType Directory $work, $out | Out-Null
    $outputOwned = $true
    $stage = 'classification_self_test'; Test-Diagnostic-Classification
    $observedCommit = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0 -or $observedCommit -cnotmatch '^[0-9a-f]{40}$' -or $observedCommit -cne $env:CIRCLE_SHA1) { throw 'source_commit_invalid' }
    $commit = $observedCommit
    $stage = 'download_toolchain'; Report-Stage $stage
    $archive = Join-Path $work 'go1.26.9.windows-amd64.zip'
    Invoke-WebRequest -UseBasicParsing -TimeoutSec 120 -Uri 'https://go.dev/dl/go1.26.9.windows-amd64.zip' -OutFile $archive
    if ((Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash.ToLowerInvariant() -cne $archiveSHA) { throw 'toolchain_checksum_mismatch' }
    $null = Remaining-Millis
    $stage = 'extract_toolchain'; Report-Stage $stage
    # The ZIP is the checksum-pinned official archive; use the image's existing
    # extractor into a fresh job-local directory. No machine toolchain install.
    $tar = Join-Path $env:SystemRoot 'System32\tar.exe'
    if (-not (Test-Path -LiteralPath $tar -PathType Leaf)) { throw 'native_extractor_missing' }
    $extract = Invoke-Bounded $tar @('-xf', ('"' + $archive + '"'), '-C', ('"' + $work + '"')) (Join-Path $work 'extract-out.txt') (Join-Path $work 'extract-err.txt') 120000
    if ($extract -ne 0) { throw 'toolchain_extract_failed' }
    $go = Join-Path $work 'go\bin\go.exe'
    $env:GOROOT = Join-Path $work 'go'
    $env:GOENV = 'off'; $env:GOFLAGS = ''; $env:GOCACHEPROG = ''; $env:GOEXPERIMENT = ''
    $env:GOPROXY = 'off'; $env:GOSUMDB = 'off'; $env:GOTOOLCHAIN = 'local'; $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:GOWORK = 'off'
    $env:GOCACHE = Join-Path $work 'cache'; $env:GOPATH = Join-Path $work 'gopath'
    $versionOut = Join-Path $work 'version.txt'
    if ((Invoke-Bounded $go @('version') $versionOut (Join-Path $work 'version-err.txt') 30000) -ne 0) { throw 'toolchain_version_failed' }
    $observedVersion = [IO.File]::ReadAllText($versionOut).Trim()
    if ($observedVersion -cne 'go version go1.26.9 windows/amd64') { throw 'toolchain_version_mismatch' }
    $version = 'go version go1.26.9 windows/amd64'
    $log = Join-Path $work 'tests.jsonl'
    Push-Location (Join-Path $root 'signatureowner')
    try {
        $stage = 'native_tests'; Report-Stage $stage
        $testExit = Invoke-Bounded $go @('test', '-json', '-tags', 'signatureownernative', '-timeout=5m', '-count=3', './...') $log (Join-Path $work 'test-stderr.txt') 360000
        if ((Get-Item -LiteralPath $log).Length -gt 8MB) { throw 'test_log_bounds' }
        $packagePasses = 0
        foreach ($line in Get-Content -LiteralPath $log) {
            if ($line.Length -gt 16384) { throw 'test_event_bounds' }
            try { $row = $line | ConvertFrom-Json } catch { throw 'test_event_json_invalid' }
            if ($row.Action -in @('fail', 'skip', 'build-fail')) {
                $failureCategories[$row.Action] = $true
                if ($null -ne $row.PSObject.Properties['Test'] -and $row.Test -cmatch '^Test[A-Za-z0-9_]{1,128}(/[A-Za-z0-9_]{1,128})?$') { $failedTests[$row.Test] = $true }
            }
            if ($row.Action -ceq 'pass') {
                if ($null -ne $row.PSObject.Properties['Test']) { if ($counts.Contains($row.Test)) { $counts[$row.Test]++ } }
                else { if ($row.Package -cne 'github.com/Dr-Crow/USBridge-Remote/signatureowner') { throw 'test_package_invalid' }; $packagePasses++ }
            }
            if ($null -ne $row.PSObject.Properties['Test'] -and $row.Test -ceq 'TestExtractionNativeOriginalSignature' -and $null -ne $row.PSObject.Properties['Output']) {
                $match = [regex]::Match([string]$row.Output, 'SIGNATURE_OWNER_PROOF_JSON=(\{[^\r\n]*\})')
                if ($match.Success) {
                    if ($proofs.Count -ge 3) { throw 'positive_proof_count_invalid' }
                    $proofs.Add((Exact-Positive-Proof ($match.Groups[1].Value | ConvertFrom-Json)))
                }
            }
        }
        # Classify outcomes first. Only an already failed suite can collect
        # diagnostic strings, and successful proof JSON is never scanned as an
        # error message. Prefer the exact failed test (or package-level output).
        if ($testExit -ne 0 -or $failureCategories.Count -ne 0) {
            foreach ($line in Get-Content -LiteralPath $log) {
                $row = $line | ConvertFrom-Json
                if ($null -eq $row.PSObject.Properties['Output']) { continue }
                $message = [string]$row.Output
                if ($message.Contains('SIGNATURE_OWNER_PROOF_JSON=')) { continue }
                $packageOutput = $null -eq $row.PSObject.Properties['Test']
                $failedTestOutput = -not $packageOutput -and $failedTests.ContainsKey([string]$row.Test)
                $unclassifiedNonzero = $testExit -ne 0 -and $failedTests.Count -eq 0
                if ($packageOutput -or $failedTestOutput -or $unclassifiedNonzero) { Add-Owner-Candidates $message $candidateCodes }
            }
            Promote-Failed-Diagnostics $failureCategories $candidateCodes $testExit
        }
        if ($testExit -ne 0 -or $failureCategories.Count -ne 0 -or $packagePasses -ne 1 -or $proofs.Count -ne 3) { throw 'native_suite_failed' }
        foreach ($name in $required) { if ($counts[$name] -ne 3) { throw 'native_repetition_missing' } }
        $stage = 'native_vet'; Report-Stage $stage
        $vetExit = Invoke-Bounded $go @('vet', '-tags', 'signatureownernative', './...') (Join-Path $work 'vet-stdout.txt') (Join-Path $work 'vet-stderr.txt') 120000
        if ($vetExit -ne 0) { throw 'native_vet_failed' }
    } finally { Pop-Location }
    $null = Remaining-Millis
    $stage = 'complete'
    Write-Receipt 'positive-proof.json' ([ordered]@{schema_version=1; source_commit=$commit; native_execution=$true; query='original_source_owned_system_file'; repetitions=3; proofs=@($proofs.ToArray()); source_or_binary_artifacts_published=$false})
    $passed = $true
} catch {
    # Exception text is intentionally never copied into logs or receipts.
    $passed = $false
    $failureCategories['gate_failed'] = $true
} finally {
    if ($outputOwned) {
        try { Write-Receipt 'summary.json' ([ordered]@{
            schema_version=1; source_commit=$commit; passed=$passed; stage=$stage
            elapsed_seconds=[int]$timer.Elapsed.TotalSeconds; platform='windows/amd64'; go_version=$version
            go_archive_sha256=$archiveSHA; native_test_repetitions=3; required_test_passes=$counts
            positive_proof_count=$proofs.Count; test_exit_code=$testExit; vet_exit_code=$vetExit
            failed_tests=@($failedTests.Keys | Sort-Object); failure_categories=@($failureCategories.Keys | Sort-Object)
            raw_output_published=$false; source_or_binary_artifacts_published=$false
            broker_verifier_tested=$false; media_started=$false; desktop_capture=$false; input_injected=$false
        }) } catch { $passed = $false; Write-Host 'Source-free summary could not be written.' }
    }
}
if (-not $passed) { Write-Host ('Signature-owner native gate failed at fixed stage: ' + $stage); exit 1 }
Write-Host 'Signature-owner native prerequisite passed three times; source-free evidence only.'
