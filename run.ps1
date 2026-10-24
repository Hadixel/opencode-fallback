#Requires -Version 5.1
<#
.SYNOPSIS
  Install ocf (if needed) and run it.

.DESCRIPTION
  One line, interactive:
    irm https://raw.githubusercontent.com/Hadixel/opencode-fallback/main/run.ps1 | iex

  One line, with the prompt supplied:
    & ([scriptblock]::Create((irm https://raw.githubusercontent.com/Hadixel/opencode-fallback/main/run.ps1))) "explain this stack trace"

  A bare prompt is treated as `ocf run <prompt>`; explicit commands (run, check,
  version, help) pass through unchanged.

  Environment:
    OCF_BIN    install path (default: $HOME\.local\bin\ocf.exe)
    OCF_REPO   source repository
    OCF_CHAIN  model chain (same as ocf)

.NOTES
  Installs the prebuilt release binary when one exists, otherwise builds from
  source with Go and git.
#>
[CmdletBinding()]
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]] $Rest
)

$ErrorActionPreference = 'Stop'

$repo = if ($env:OCF_REPO) { $env:OCF_REPO } else { 'https://github.com/Hadixel/opencode-fallback' }
$bin = if ($env:OCF_BIN) { $env:OCF_BIN } else { Join-Path $HOME '.local\bin\ocf.exe' }

function Get-OcfAsset {
    switch ($env:PROCESSOR_ARCHITECTURE.ToUpperInvariant()) {
        'AMD64' { return 'ocf_windows_amd64.exe' }
        'ARM64' { return 'ocf_windows_arm64.exe' }
        default { throw "ocf: unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
    }
}

function Install-Ocf {
    $asset = Get-OcfAsset
    $dir = Split-Path -Parent $bin
    New-Item -ItemType Directory -Force -Path $dir | Out-Null

    try {
        Write-Host "ocf: downloading $asset"
        Invoke-WebRequest -UseBasicParsing -Uri "$repo/releases/latest/download/$asset" -OutFile $bin
        return
    }
    catch {
        Write-Host "ocf: release download failed ($($_.Exception.Message)); building from source"
    }

    foreach ($tool in 'go', 'git') {
        if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
            throw "ocf: $tool is required to build from source"
        }
    }

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ('ocf-' + [guid]::NewGuid().ToString('n'))
    try {
        git clone --depth 1 $repo $tmp | Out-Null
        Push-Location $tmp
        try {
            go build -trimpath -ldflags '-s -w' -o $bin .
        }
        finally {
            Pop-Location
        }
    }
    finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }
}

if (-not (Test-Path -LiteralPath $bin)) {
    Install-Ocf
}

if (-not $Rest -or $Rest.Count -eq 0) {
    $answer = Read-Host 'ocf prompt (blank for help)'
    if ([string]::IsNullOrWhiteSpace($answer)) {
        & $bin help
    }
    else {
        & $bin run $answer
    }
}
elseif (@('run', 'check', 'version', 'help', '-h', '--help', '-v', '--version') -contains $Rest[0]) {
    & $bin @Rest
}
else {
    & $bin run @Rest
}
