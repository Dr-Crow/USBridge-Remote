$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
New-Item -ItemType Directory -Path 'C:\ci' -Force | Out-Null

function Get-VerifiedFile($Url, $Path, $ExpectedHash) {
    Invoke-WebRequest -UseBasicParsing -Uri $Url -OutFile $Path
    if ((Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash -ine $ExpectedHash) {
        throw "SHA256 mismatch: $Path"
    }
}

# Pinned upstream archives. Pacman's rolling package versions are captured
# in build-info.txt so a CI result records its actual resolved dependencies.
Get-VerifiedFile 'https://go.dev/dl/go1.26.6.windows-amd64.zip' 'C:\ci\go.zip' '5b6c5b556525810463b5c897b50dc7a82d6a3dc0bfaf55d990a7e9f31d6b2318'
Expand-Archive -LiteralPath 'C:\ci\go.zip' -DestinationPath 'C:\ci' -Force
Get-VerifiedFile 'https://github.com/msys2/msys2-installer/releases/download/2026-09-27/msys2-base-x86_64-20260927.sfx.exe' 'C:\ci\msys2.exe' 'ad336cccfda47758b5e15cda993fbba421115cb0b126697daef1ee4dfe37209f'
$extract = Start-Process -FilePath 'C:\ci\msys2.exe' -ArgumentList '-y', '-oC:\ci' -Wait -PassThru
if ($extract.ExitCode -ne 0) { throw 'MSYS2 extraction failed' }
$env:CHERE_INVOKING = 'yes'
$env:MSYSTEM = 'UCRT64'
$bash = 'C:\ci\msys64\usr\bin\bash.exe'
& $bash -lc 'true'
if ($LASTEXITCODE -ne 0) { throw 'MSYS2 initialization failed' }
& $bash -lc 'pacman --noconfirm -Syuu'
if ($LASTEXITCODE -notin @(0, 1)) { throw 'MSYS2 core update failed' }
& $bash -lc 'pacman --noconfirm -Syuu'
if ($LASTEXITCODE -ne 0) { throw 'MSYS2 package update failed' }
& $bash -lc 'pacman --noconfirm -S --needed mingw-w64-ucrt-x86_64-gcc mingw-w64-ucrt-x86_64-binutils mingw-w64-ucrt-x86_64-python git zip diffutils'
if ($LASTEXITCODE -ne 0) { throw 'MSYS2 build dependencies failed' }
