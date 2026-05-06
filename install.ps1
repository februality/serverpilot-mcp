# serverpilot-mcp installer for Windows (PowerShell 5.1+).
#
# Usage:
#   irm https://raw.githubusercontent.com/februality/serverpilot-mcp/main/install.ps1 | iex
#
# Parameters (set as env vars before running):
#   $env:SP_VERSION      — pin a specific release tag
#   $env:SP_INSTALL_DIR  — override install directory
#   $env:SP_NO_WIZARD=1  — skip the post-install setup wizard
#   $env:CI=1            — auto-skips wizard

[CmdletBinding()]
param(
    [switch]$Unattended,
    [switch]$Force,
    [string]$Version,
    [string]$InstallDir
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$PSDefaultParameterValues['*:ErrorAction'] = 'Stop'

$repo     = 'februality/serverpilot-mcp'
$binName  = 'serverpilot-mcp'
$exeName  = "$binName.exe"

if (-not $InstallDir) {
    $InstallDir = if ($env:SP_INSTALL_DIR) { $env:SP_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\$binName" }
}
if (-not $Version) { $Version = $env:SP_VERSION }
$skipWizard = $Unattended -or $env:SP_NO_WIZARD -or $env:CI

function Info($msg)  { Write-Host "==> $msg" }
function Fail($msg)  { Write-Error $msg; exit 1 }

# Detect arch.
$arch = if ([Environment]::Is64BitOperatingSystem) {
    if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { 'arm64' } else { 'amd64' }
} else {
    Fail '32-bit Windows is not supported.'
}

# Resolve version.
if (-not $Version) {
    Info 'Resolving latest release...'
    try {
        $latest = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest"
        $Version = $latest.tag_name
    } catch {
        Fail "could not query GitHub API: $_. Set -Version to a release tag explicitly."
    }
}
Info "Installing $binName $Version for windows/$arch"

# Skip if up-to-date.
$existing = Get-Command $binName -ErrorAction SilentlyContinue
if (-not $Force -and $existing) {
    try {
        $current = & $existing.Path version --short 2>$null
        if ($current -eq $Version -or $current -eq $Version.TrimStart('v')) {
            Info "$binName $current is already installed. Use -Force to reinstall."
            return
        }
    } catch { }
}

# Download.
$archive = "${binName}_windows_${arch}.zip"
$urlBase = "https://github.com/$repo/releases/download/$Version"
$tmp     = Join-Path $env:TEMP "$binName-install-$([System.Guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
    Info "Downloading $archive..."
    Invoke-WebRequest "$urlBase/$archive"        -OutFile (Join-Path $tmp $archive)
    Invoke-WebRequest "$urlBase/checksums.txt"   -OutFile (Join-Path $tmp 'checksums.txt')

    Info 'Verifying checksum...'
    $hash    = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $tmp $archive)).Hash.ToLower()
    $line    = (Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match " $([regex]::Escape($archive))$" })
    if (-not $line) { Fail "no checksum entry for $archive" }
    $expected = ($line -split '\s+')[0].ToLower()
    if ($hash -ne $expected) { Fail "checksum mismatch (expected $expected, got $hash)" }

    Info 'Extracting...'
    Expand-Archive -Path (Join-Path $tmp $archive) -DestinationPath $tmp -Force
    $sourceExe = Join-Path $tmp $exeName
    if (-not (Test-Path $sourceExe)) { Fail "binary $exeName not found in archive" }

    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }
    $targetExe = Join-Path $InstallDir $exeName
    Move-Item -Path $sourceExe -Destination $targetExe -Force
    Info "Installed: $targetExe"

    # Add to user PATH if not present.
    $userPath = [Environment]::GetEnvironmentVariable('PATH', 'User')
    if (-not $userPath) { $userPath = '' }
    $pathEntries = $userPath -split ';' | Where-Object { $_ -ne '' }
    if ($pathEntries -notcontains $InstallDir) {
        $newPath = if ($userPath) { "$userPath;$InstallDir" } else { $InstallDir }
        [Environment]::SetEnvironmentVariable('PATH', $newPath, 'User')
        Write-Host ''
        Write-Host "NOTE: $InstallDir was added to your user PATH."
        Write-Host '      Open a new terminal for this to take effect.'
        Write-Host ''
    }

    if (-not $skipWizard -and [Environment]::UserInteractive) {
        Info 'Launching setup wizard...'
        & $targetExe setup
    } else {
        Write-Host ''
        Write-Host 'Skipping interactive setup. Finish configuring with:'
        Write-Host "    $targetExe setup"
        Write-Host ''
    }
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
