# FlowLite installer for Windows — one line, no admin rights:
#   irm https://raw.githubusercontent.com/sanke08/flowlite/main/install.ps1 | iex
#
# Downloading with Invoke-WebRequest (rather than a browser) means the file
# never gets the Mark-of-the-Web zone tag, so SmartScreen doesn't block it on
# first run — the Windows equivalent of what curl does for macOS Gatekeeper.
#
# This is meant to run via `irm ... | iex`, which executes in the *caller's*
# session rather than a subprocess (unlike `curl | sh`) — so failures below
# use `throw`, never `exit`: `exit` here would close the user's whole terminal.
$ErrorActionPreference = 'Stop'
$Repo = 'sanke08/flowlite'

if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') {
    throw "FlowLite's installer currently supports 64-bit Windows (x64) only. Releases: https://github.com/$Repo/releases"
}
$Asset = 'windows-x64.zip'

Write-Host 'FlowLite: finding the latest release...'
$release = Invoke-RestMethod -UseBasicParsing "https://api.github.com/repos/$Repo/releases/latest"
$assetInfo = $release.assets | Where-Object { $_.name -like "*$Asset" } | Select-Object -First 1
if (-not $assetInfo) {
    throw "no release asset found for $Asset"
}

$TmpDir = Join-Path $env:TEMP "flowlite-install-$([guid]::NewGuid())"
New-Item -ItemType Directory -Path $TmpDir | Out-Null
$ZipPath = Join-Path $TmpDir $assetInfo.name

Write-Host "FlowLite: downloading $($assetInfo.name)..."
$ProgressPreference = 'SilentlyContinue'  # Invoke-WebRequest's live progress UI is slow; skip it.
try {
    Invoke-WebRequest -UseBasicParsing -Uri $assetInfo.browser_download_url -OutFile $ZipPath
} catch {
    Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
    throw "FlowLite: download failed ($($assetInfo.browser_download_url)): $_"
}
$ProgressPreference = 'Continue'

Write-Host 'FlowLite: extracting...'
Expand-Archive -Path $ZipPath -DestinationPath $TmpDir -Force

# The zip's one top-level folder holds flowlite.exe plus the whisper/ggml DLLs
# it needs alongside it — unlike macOS, this build is not statically linked.
$ExtractedDir = Get-ChildItem -Path $TmpDir -Directory | Select-Object -First 1
if (-not $ExtractedDir) { $ExtractedDir = Get-Item $TmpDir }

$InstallDir = Join-Path $env:LOCALAPPDATA 'FlowLite'
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null

# Note whether FlowLite is already running, and stop it so the locked .exe can
# be replaced — Windows will not let us overwrite a running binary the way
# macOS does. It is started again below.
$WasRunning = $false
$OldExe = Join-Path $InstallDir 'flowlite.exe'
if (Test-Path $OldExe) {
    # 2>&1 on a native command turns its stderr into NativeCommandError
    # records, which $ErrorActionPreference = 'Stop' would treat as fatal —
    # aborting the install after the daemon has already been stopped. Keep the
    # preference local to this block and swallow anything it throws.
    try {
        $ErrorActionPreference = 'Continue'
        $stopOutput = & $OldExe stop 2>&1 | Out-String
    } catch {
        $stopOutput = ''
    } finally {
        $ErrorActionPreference = 'Stop'
    }
    # No output at all means nothing was running; only a real stop counts.
    if ($stopOutput -and ($stopOutput -notmatch 'not running')) { $WasRunning = $true }
    # Give Windows a moment to release the file lock before overwriting it.
    for ($i = 0; $i -lt 25; $i++) {
        if (-not (Get-Process -Name 'flowlite' -ErrorAction SilentlyContinue)) { break }
        Start-Sleep -Milliseconds 200
    }
}

try {
    Copy-Item -Path (Join-Path $ExtractedDir.FullName '*') -Destination $InstallDir -Recurse -Force
} catch {
    throw "FlowLite: could not replace $InstallDir — is flowlite.exe still running? Close it and run this again. ($_)"
}
Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue

$ExePath = Join-Path $InstallDir 'flowlite.exe'
Write-Host "FlowLite: installed $ExePath"

# Add InstallDir to the user's persistent PATH (HKCU — no admin needed).
$UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($UserPath -split ';') -notcontains $InstallDir) {
    $NewUserPath = if ([string]::IsNullOrEmpty($UserPath)) { $InstallDir } else { "$UserPath;$InstallDir" }
    [Environment]::SetEnvironmentVariable('Path', $NewUserPath, 'User')
    Write-Host "FlowLite: added $InstallDir to your PATH. Open a new terminal for it to take effect."
}
$env:Path = "$env:Path;$InstallDir"  # so it also works in *this* session

Write-Host ''
if ($WasRunning) {
    # An upgrade, not a first install: put it back on the new binary.
    & $ExePath start
} elseif ($env:FLOWLITE_NO_SETUP) {
    Write-Host 'Installed. Next:  flowlite'
} else {
    & $ExePath
}
