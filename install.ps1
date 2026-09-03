#Requires -Version 5.1
<#
.SYNOPSIS
  Install shmorby on Windows (amd64).

.DESCRIPTION
  Mirrors install.sh for Unix: checks
  prerequisites, builds shmorby.exe,
  installs to %LOCALAPPDATA%\shmorby or
  --Prefix, creates config/data dirs,
  writes a minimal config if missing,
  and pulls the embedding model.

.PARAMETER DryRun
  Print actions without making changes.

.PARAMETER DepsOnly
  Check prerequisites and exit.

.PARAMETER Uninstall
  Remove binary and directories.

.PARAMETER Prefix
  Install prefix (default:
  $env:LOCALAPPDATA\shmorby).

.PARAMETER BinDir
  Binary directory override.

.PARAMETER DataDir
  Data directory override.

.PARAMETER Quiet
  Suppress non-error output.
#>
[CmdletBinding()]
param(
    [switch]$DryRun,
    [switch]$DepsOnly,
    [switch]$Uninstall,
    [string]$Prefix = "",
    [string]$BinDir = "",
    [string]$DataDir = "",
    [switch]$Quiet
)

$BinName = "shmorby.exe"
$CfgName = "config.yaml"
$GoMinMajor = 1
$GoMinMinor = 25

function Write-Ok($msg) {
    if (-not $Quiet) {
        Write-Host "[shmorby] ok $msg" `
            -ForegroundColor Green
    }
}

function Write-Info($msg) {
    if (-not $Quiet) {
        Write-Host "[shmorby] info $msg" `
            -ForegroundColor Cyan
    }
}

function Write-Warn($msg) {
    Write-Host "[shmorby] warn $msg" `
        -ForegroundColor Yellow
}

function Die($msg) {
    Write-Host "[shmorby] error $msg" `
        -ForegroundColor Red
    exit 1
}

# -- prereqs ------------------

function Test-Go {
    try {
        $verOut = & go version 2>$null
    } catch {
        Write-Warn "Go not found"
        Write-Host ("  Install Go {0}.{1}+ from " `
            -f $GoMinMajor, $GoMinMinor) `
            -NoNewline
        Write-Host "https://go.dev/dl/"
        return $false
    }

    if (-not $verOut) {
        Write-Warn "Go not found"
        return $false
    }

    # go version go1.25.0 windows/amd64
    if ($verOut -match 'go(\d+)\.(\d+)') {
        $maj = [int]$Matches[1]
        $min = [int]$Matches[2]

        if ($maj -lt $GoMinMajor -or `
            ($maj -eq $GoMinMajor -and `
                $min -lt $GoMinMinor)) {
            Write-Warn ("Go {0}.{1}+ required, found " `
                -f $GoMinMajor, $GoMinMinor) `
                -NoNewline
            Write-Host "$maj.$min"
            return $false
        }

        Write-Ok "Checking: go -> found $maj.$min"
        return $true
    }

    Write-Ok "Checking: go -> found"
    return $true
}

function Test-Ollama {
    $cmd = Get-Command ollama `
        -ErrorAction SilentlyContinue

    if (-not $cmd) {
        Write-Warn "Checking: ollama -> NOT FOUND"
        Write-Host "  Install from https://ollama.ai/download"
        return $false
    }

    Write-Ok "Checking: ollama -> found"
    return $true
}

function Test-Git {
    $cmd = Get-Command git `
        -ErrorAction SilentlyContinue

    if (-not $cmd) {
        Write-Warn "Checking: git -> NOT FOUND"
        return $false
    }

    $v = & git --version 2>$null
    Write-Ok "Checking: git -> found $v"
    return $true
}

function Test-Prereqs {
    $allOk = $true

    if (-not (Test-Go)) { $allOk = $false }
    if (-not (Test-Ollama)) { $allOk = $false }
    if (-not (Test-Git)) { $allOk = $false }

    if (-not $allOk) {
        Write-Warn ("Some prerequisites are missing " +
            "- install them before continuing")
    }
}

function Pull-EmbeddingModel {
    $cmd = Get-Command ollama `
        -ErrorAction SilentlyContinue

    if (-not $cmd) { return }

    $model = "nomic-embed-text:latest"
    Write-Info "Pulling embedding model: $model"

    if ($DryRun) {
        Write-Info "[DRY-RUN] ollama pull $model"
        return
    }

    & ollama pull $model | Out-Null

    if ($LASTEXITCODE -ne 0) {
        Write-Warn ("Could not pull $model " +
            "- pull manually after ollama starts")
    } else {
        Write-Ok "Pulled $model"
    }
}

# -- resolve paths ------------

$ResolvedBinDir = $BinDir

if (-not $ResolvedBinDir) {
    if ($Prefix) {
        $ResolvedBinDir = Join-Path $Prefix "bin"

        if ($Prefix -like "*shmorby*") {
            $ResolvedBinDir = $Prefix
        }
    } else {
        $localApp = $env:LOCALAPPDATA

        if (-not $localApp) {
            $localApp = Join-Path $env:USERPROFILE `
                "AppData\Local"
        }

        $ResolvedBinDir = Join-Path $localApp "shmorby"
    }
}

# Allow --DataDir override
$ResolvedDataDir = $DataDir

if (-not $ResolvedDataDir) {
    $localApp = $env:LOCALAPPDATA

    if (-not $localApp) {
        $localApp = Join-Path $env:USERPROFILE "AppData\Local"
    }

    $ResolvedDataDir = Join-Path $localApp "shmorby"
}

$appData = $env:APPDATA

if (-not $appData) {
    $appData = Join-Path $env:USERPROFILE "AppData\Roaming"
}

$CfgDir = Join-Path $appData "shmorby"

# -- uninstall ----------------

function Invoke-Uninstall {
    $removed = 0
    $binPath = Join-Path $ResolvedBinDir $BinName

    if (Test-Path $binPath) {
        if ($DryRun) {
            Write-Ok "[DRY-RUN] Removed $binPath"
        } else {
            Remove-Item -Force $binPath `
                -ErrorAction SilentlyContinue
            Write-Ok "Removed $binPath"
        }

        $removed++
    }

    foreach ($d in @($CfgDir, $ResolvedDataDir)) {
        if (Test-Path $d) {
            if ($DryRun) {
                Write-Ok "[DRY-RUN] Removed $d"
            } else {
                Remove-Item -Recurse -Force $d `
                    -ErrorAction SilentlyContinue
                Write-Ok "Removed $d"
            }

            $removed++
        }
    }

    if ($removed -eq 0) {
        Write-Info "Nothing to uninstall"
    }
}

if ($Uninstall) {
    Invoke-Uninstall
    exit 0
}

if ($DepsOnly) {
    Test-Prereqs
    exit 0
}

Test-Prereqs

# -- build --------------------

$src = $PSScriptRoot

if (-not $src) { $src = Get-Location }

$dst = Join-Path $src "bin\$BinName"

Write-Info "Building: go build -o $dst ./cmd/shmorby"

if ($DryRun) {
    Write-Info "[DRY-RUN] Build: go build ..."
} else {
    $oldLoc = Get-Location
    $oldCgo = $env:CGO_ENABLED

    try {
        Set-Location $src
        $env:CGO_ENABLED = "1"
        & go build -o $dst ./cmd/shmorby

        if ($LASTEXITCODE -ne 0) {
            Die "go build failed"
        }

        Write-Ok "Build complete"
    } finally {
        if ($null -eq $oldCgo) {
            Remove-Item Env:\CGO_ENABLED `
                -ErrorAction SilentlyContinue
        } else {
            $env:CGO_ENABLED = $oldCgo
        }

        Set-Location $oldLoc
    }
}

# Install binary
if ($DryRun) {
    Write-Ok "[DRY-RUN] Installing: $ResolvedBinDir\$BinName"
} else {
    New-Item -ItemType Directory -Force `
        -Path $ResolvedBinDir | Out-Null
    Copy-Item -Force $dst `
        (Join-Path $ResolvedBinDir $BinName)
    Write-Ok "Installing: $ResolvedBinDir\$BinName"
}

# Create directories
foreach ($d in @($CfgDir, $ResolvedDataDir)) {
    if ($DryRun) {
        Write-Ok "[DRY-RUN] Mkdir: $d"
    } else {
        New-Item -ItemType Directory -Force `
            -Path $d | Out-Null
        Write-Ok "Creating: $d"
    }
}

# Write minimal default config
$cfgPath = Join-Path $CfgDir $CfgName

if ($DryRun) {
    Write-Ok "[DRY-RUN] Write: $cfgPath"
} elseif (Test-Path $cfgPath) {
    Write-Info "Config exists: $cfgPath"
} else {
    "provider: ollama`n" |
        Set-Content -Path $cfgPath -Encoding utf8
    Write-Ok "Writing: $cfgPath"
}

Pull-EmbeddingModel

Write-Host ""
Write-Ok "shmorby installed to $ResolvedBinDir\$BinName"
Write-Host ""
Write-Host "  Add to PATH:  `$env:PATH += `";$ResolvedBinDir`""
Write-Host "  Run:          shmorby.exe"
Write-Host "  Config:       $cfgPath"
Write-Host ""
Write-Host ("  CGO note: requires mingw " +
    "(choco install mingw or scoop install mingw)")
Write-Host ("  Validate:     .\shmorby.exe --validate " +
    "--config examples\shmorby.windows.yaml")
