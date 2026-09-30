# Fast incremental rebuild — Go only, no DLL copy, no fyne package.
# Usage (from any PowerShell in the project root):
#   .\scripts\fast_rebuild.ps1
#
# ~15-30s on cached build; only changed packages recompile.
# For a full dist rebuild run .\scripts\build_windows.ps1 from PowerShell.
#
# -tags usbpass_gousb must match build_windows.sh, or this overwrites the
# real libusb/WinUSB claim path with backend_nogousb.go's disabled stub --
# USB passthrough then silently exports every device as a fake MSC
# descriptor instead of claiming it (see docs/USB_PASSTHROUGH.md).

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path $PSScriptRoot -Parent
$ExeOut   = "$RepoRoot\.cache\build\windows-amd64\release\USBridge_Client.exe"
$DistDir  = "$RepoRoot\dist\windows\bin"
$DistExe  = "$DistDir\USBridge_Client.exe"

# Pin MSYS2 UCRT64 gcc. A plain PATH hit (e.g. Strawberry Perl's MinGW) links
# a PE with an empty .idata and Windows then fails with 0xc000007b before main.
$Msys2Root = if ($env:MSYS2_ROOT) { $env:MSYS2_ROOT } else { "C:\msys64" }
$Ucrt64Bin = Join-Path $Msys2Root "ucrt64\bin"
$Gcc = Join-Path $Ucrt64Bin "gcc.exe"
$Gpp = Join-Path $Ucrt64Bin "g++.exe"
if (-not (Test-Path $Gcc)) {
    Write-Error "MSYS2 UCRT64 gcc not found at $Gcc. Install mingw-w64-ucrt-x86_64-gcc or set MSYS2_ROOT."
    exit 1
}

$GoBin = Split-Path (Get-Command go).Source -Parent
Write-Host "==> fast_rebuild: go build..."
Write-Host "==> CC=$Gcc"
$env:CGO_ENABLED  = "1"
$env:GOOS         = "windows"
$env:GOARCH       = "amd64"
$env:CC           = $Gcc
$env:CXX          = $Gpp
$env:GOCACHE      = "$RepoRoot\.cache\go-build\windows-amd64"
$env:GOMODCACHE   = "$RepoRoot\.cache\go-mod"
$env:GOMAXPROCS   = "12"
$env:GOFLAGS      = if ($env:GOFLAGS) { "$($env:GOFLAGS) -buildvcs=false" } else { "-buildvcs=false" }
# Go bin must stay ahead of ucrt64 — MSYS ships its own trimmed go.exe there.
$env:PATH         = "$GoBin;$Ucrt64Bin;$env:PATH"

# Same GOCACHE as build_windows.sh: do not inject CGO_CFLAGS=-I/ucrt64/include
# (cache miss + winsock2.h vs windows.h). Drop local moonlight openssl/opus
# .pc trees so pkg-config does not pick Android/source artifacts.
$ucrt64Pc = Join-Path $Msys2Root "ucrt64\lib\pkgconfig"
$drop = @("moonlight-common-c\openssl-3.3.2", "moonlight-common-c/openssl-3.3.2",
          "moonlight-common-c\opus-1.5.2", "moonlight-common-c/opus-1.5.2",
          "moonlight-common-c\opus-cmake-build", "moonlight-common-c/opus-cmake-build",
          "moonlight-common-c\build\android", "moonlight-common-c/build/android")
if ($env:PKG_CONFIG_PATH) {
    $kept = @()
    foreach ($p in ($env:PKG_CONFIG_PATH -split ';')) {
        $skip = $false
        foreach ($d in $drop) {
            if ($p -like "*$d*") { $skip = $true; break }
        }
        if (-not $skip -and $p) { $kept += $p }
    }
    $env:PKG_CONFIG_PATH = ($kept -join ';')
}
if (Test-Path $ucrt64Pc) {
    if (-not $env:PKG_CONFIG_PATH) {
        $env:PKG_CONFIG_PATH = $ucrt64Pc
    } elseif ($env:PKG_CONFIG_PATH -notlike "*$ucrt64Pc*") {
        $env:PKG_CONFIG_PATH = "$ucrt64Pc;$($env:PKG_CONFIG_PATH)"
    }
}
$env:PKG_CONFIG = Join-Path $Ucrt64Bin "pkg-config.exe"

New-Item -ItemType Directory -Force -Path (Split-Path $ExeOut) | Out-Null
New-Item -ItemType Directory -Force -Path (Split-Path $DistExe) | Out-Null
New-Item -ItemType Directory -Force -Path $env:GOCACHE | Out-Null
New-Item -ItemType Directory -Force -Path $env:GOMODCACHE | Out-Null


$sw = [System.Diagnostics.Stopwatch]::StartNew()

$Version = (Get-Content -Raw "$RepoRoot\VERSION").Trim()
if (-not $Version) { $Version = "1.0.0" }
Copy-Item -Force "$RepoRoot\VERSION" "$RepoRoot\cmd\VERSION"

Set-Location "$RepoRoot\cmd"
& go build -trimpath -tags usbpass_gousb "-ldflags=-H=windowsgui -X main.version=$Version -extldflags=-Wl,--stack,8388608" -o $ExeOut .
if ($LASTEXITCODE -ne 0) { Write-Error "go build failed"; exit 1 }

$sw.Stop()
Write-Host "==> Build done in $($sw.Elapsed.TotalSeconds.ToString('0.0'))s"

Write-Host "==> Copying to dist\windows\bin (next to runtime DLLs)..."
New-Item -ItemType Directory -Force -Path $DistDir | Out-Null
Copy-Item $ExeOut $DistExe -Force
if (-not (Get-ChildItem -Path $DistDir -Filter "avutil-*.dll" -ErrorAction SilentlyContinue)) {
    Write-Host "==> WARNING: no avutil-*.dll in $DistDir"
    Write-Host "    Run .\scripts\build_windows.ps1 once so FFmpeg/OpenSSL/opus DLLs are bundled."
}
Write-Host "==> Done: $DistExe"
Write-Host "    Launch dist\windows\USBridge_Client.lnk or this bin\ exe, not dist\windows\USBridge_Client.exe"
$size = (Get-Item $DistExe).Length / 1MB
Write-Host "==> ($($size.ToString('0.0')) MB)"
