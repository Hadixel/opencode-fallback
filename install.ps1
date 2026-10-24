#Requires -Version 5.1
<#
.SYNOPSIS
  Install ocf for Windows.

.DESCRIPTION
    irm https://raw.githubusercontent.com/Hadixel/opencode-fallback/main/install.ps1 | iex

  Downloads the prebuilt release binary when one exists, otherwise builds from
  source with Go and git.

.PARAMETER Bin
  Install path. Default: $env:OCF_BIN, then $HOME\.local\bin\ocf.exe

.PARAMETER Repo
  Source repository. Default: $env:OCF_REPO, then the public GitHub repo.
#>
[CmdletBinding()]
param(
    [string] $Bin = $(if ($env:OCF_BIN) { $env:OCF_BIN } else { Join-Path $HOME '.local\bin\ocf.exe' }),
    [string] $Repo = $(if ($env:OCF_REPO) { $env:OCF_REPO } else { 'https://github.com/Hadixel/opencode-fallback' })
)

$ErrorActionPreference = 'Stop'

$asset = switch ($env:PROCESSOR_ARCHITECTURE.ToUpperInvariant()) {
    'AMD64' { 'ocf_windows_amd64.exe' }
    'ARM64' { 'ocf_windows_arm64.exe' }
    default { throw "ocf: unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$dir = Split-Path -Parent $Bin
New-Item -ItemType Directory -Force -Path $dir | Out-Null

$installed = $false
try {
    Write-Host "ocf: downloading $asset"
    Invoke-WebRequest -UseBasicParsing -Uri "$Repo/releases/latest/download/$asset" -OutFile $Bin
    $installed = $true
}
catch {
    Write-Host "ocf: release download failed ($($_.Exception.Message)); building from source"
}

if (-not $installed) {
    foreach ($tool in 'go', 'git') {
        if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
            throw "ocf: $tool is required to build from source"
        }
    }
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ('ocf-' + [guid]::NewGuid().ToString('n'))
    try {
        git clone --depth 1 $Repo $tmp | Out-Null
        Push-Location $tmp
        try {
            go build -trimpath -ldflags '-s -w' -o $Bin .
        }
        finally {
            Pop-Location
        }
    }
    finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }
}

Write-Host "installed $Bin"
if (-not (($env:PATH -split ';') -contains $dir)) {
    Write-Host "note: $dir is not on PATH"
}
& $Bin version
