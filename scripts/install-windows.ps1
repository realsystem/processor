# One-shot Windows installer for a Processor GPU worker.
# Installs what the worker needs with winget, pulls a model, then (optionally) runs
# setup-windows-worker.ps1 to build and start the worker.
#
#   powershell -ExecutionPolicy Bypass -File .\scripts\install-windows.ps1
#   ... -Model llama3.1:8b          # model to pull (default llama3.2)
#   ... -Docker                     # also install Docker Desktop (only for -Mode docker)
#   ... -Tailscale                  # also install Tailscale (private link to your server)
#   ... -Setup                      # afterwards run setup-windows-worker.ps1 -InstallTask
#                                   #   (needs $env:CONTROL_PLANE_URL; asks for WORKER_TOKEN)
#
# Not installed by this script: the NVIDIA driver. Get it from https://www.nvidia.com/drivers
# (or GeForce Experience); `nvidia-smi` must work for GPU acceleration.
param(
    [string]$Model = "llama3.2",
    [switch]$Docker,
    [switch]$Tailscale,
    [switch]$Setup
)
$ErrorActionPreference = "Stop"

if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
    throw "winget not found. Install 'App Installer' from the Microsoft Store (Windows 10 1809+/11), or install manually: Ollama https://ollama.com/download/windows, Go https://go.dev/dl/, Git https://git-scm.com/download/win"
}

function Refresh-Path {
    $env:Path = [Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [Environment]::GetEnvironmentVariable("Path", "User")
}

function Ensure-Package($Id, $Command, $Label) {
    Refresh-Path
    if (Get-Command $Command -ErrorAction SilentlyContinue) { Write-Host "[ok]      $Label already installed"; return }
    Write-Host "[install] $Label ($Id)..." -ForegroundColor Yellow
    winget install --id $Id -e --silent --accept-package-agreements --accept-source-agreements
    # -1978335189 = "already installed / no applicable update"; treat as success
    if ($LASTEXITCODE -ne 0 -and $LASTEXITCODE -ne -1978335189) { throw "winget failed for $Id (exit $LASTEXITCODE)" }
    Refresh-Path
}

Write-Host "== Processor worker prerequisites ==" -ForegroundColor Cyan
Ensure-Package "Git.Git"        "git"    "Git"
Ensure-Package "GoLang.Go"      "go"     "Go"
Ensure-Package "Ollama.Ollama"  "ollama" "Ollama"
if ($Docker)    { Ensure-Package "Docker.DockerDesktop"  "docker"    "Docker Desktop (a reboot / WSL2 may be required)" }
if ($Tailscale) { Ensure-Package "tailscale.tailscale"   "tailscale" "Tailscale (then sign in from the tray icon)" }

Write-Host "== GPU ==" -ForegroundColor Cyan
if (Get-Command nvidia-smi -ErrorAction SilentlyContinue) {
    nvidia-smi --query-gpu=name,driver_version,memory.total --format=csv,noheader
} else {
    Write-Host "No NVIDIA driver found (nvidia-smi missing). Ollama will run on the CPU. Install the driver from https://www.nvidia.com/drivers for GPU use." -ForegroundColor Yellow
}

Write-Host "== Ollama ==" -ForegroundColor Cyan
function Test-Ollama { try { Invoke-RestMethod "http://localhost:11434/api/tags" -TimeoutSec 3 | Out-Null; $true } catch { $false } }
if (-not (Test-Ollama)) {
    Write-Host "Starting Ollama..."
    Start-Process ollama -ArgumentList "serve" -WindowStyle Hidden
    foreach ($i in 1..20) { if (Test-Ollama) { break }; Start-Sleep -Seconds 1 }
}
if (-not (Test-Ollama)) { throw "Ollama is not responding on http://localhost:11434" }
Write-Host "Pulling model $Model (a few GB, can take a while)..." -ForegroundColor Yellow
ollama pull $Model
if ($LASTEXITCODE -ne 0) { throw "ollama pull $Model failed" }

Write-Host ""
Write-Host "Installed: git $(git --version) | $(go version) | ollama $(ollama --version 2>&1 | Select-Object -First 1)" -ForegroundColor Green

if ($Setup) {
    $env:LLM_DEFAULT_MODEL = $Model
    & (Join-Path $PSScriptRoot "setup-windows-worker.ps1") -InstallTask
} else {
    Write-Host @"

Next:
  `$env:CONTROL_PLANE_URL = "https://your-control-plane"
  .\scripts\setup-windows-worker.ps1 -InstallTask     # asks for WORKER_TOKEN, builds and starts the worker
(Open a NEW PowerShell window first if 'go' or 'ollama' is not recognized.)
"@
}
