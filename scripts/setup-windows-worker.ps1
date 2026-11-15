# Windows worker setup (PowerShell)
# Runs the Processor worker on a Windows PC next to Ollama (GPU-accelerated if NVIDIA).
#
#   $env:CONTROL_PLANE_URL = "https://your-control-plane"
#   $env:WORKER_TOKEN      = "<WORKER_TOKEN from the server's .env>"   # or you are prompted
#   .\scripts\setup-windows-worker.ps1                 # native worker.exe (needs Go)
#   .\scripts\setup-windows-worker.ps1 -Mode docker    # Docker Desktop instead
#   .\scripts\setup-windows-worker.ps1 -InstallTask    # native: also start at logon
#
# The worker registers itself on its first poll; nothing to do on the server.
param(
    [ValidateSet("native", "docker")][string]$Mode = "native",
    [switch]$InstallTask
)
$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$ControlPlane = $env:CONTROL_PLANE_URL
if (-not $ControlPlane) { throw 'Set $env:CONTROL_PLANE_URL (e.g. https://processor.example.com)' }
$ControlPlane = $ControlPlane.TrimEnd("/")
if ($ControlPlane -match "^http://" -and $ControlPlane -notmatch "^http://(localhost|127\.|100\.|10\.|192\.168\.)") {
    Write-Host "WARNING: $ControlPlane is plain HTTP over a public address; the token and all prompts/answers are unencrypted. Use HTTPS or a private network (docs/private-network.md)." -ForegroundColor Yellow
}

$WorkerId = if ($env:WORKER_ID) { $env:WORKER_ID } else { "worker_windows_gpu" }
$Capabilities = if ($env:WORKER_CAPABILITIES) { $env:WORKER_CAPABILITIES } else { "noop,sleep,echo_metadata,ollama_chat,llm.chat" }
$Model = if ($env:LLM_DEFAULT_MODEL) { $env:LLM_DEFAULT_MODEL } else { "llama3.2" }
$Token = $env:WORKER_TOKEN
if (-not $Token) {
    $secure = Read-Host "WORKER_TOKEN (from the server's .env)" -AsSecureString
    $Token = [System.Net.NetworkCredential]::new("", $secure).Password
}
if (-not $Token) { throw "WORKER_TOKEN is required" }

Write-Host "[1/4] Checking Ollama..." -ForegroundColor Yellow
if (-not (Get-Command ollama -ErrorAction SilentlyContinue)) {
    throw "Ollama not found. Install it from https://ollama.com/download/windows, then: ollama pull $Model"
}
try {
    $tags = Invoke-RestMethod -Uri "http://localhost:11434/api/tags" -TimeoutSec 5
} catch {
    throw "Ollama is not responding on http://localhost:11434 - start it (ollama serve) and retry. $_"
}
$names = @($tags.models | ForEach-Object { $_.name })
Write-Host ("  models: " + ($(if ($names) { $names -join ", " } else { "(none)" })))
if (-not ($names | Where-Object { $_ -eq $Model -or $_ -like "${Model}:*" })) {
    Write-Host "  WARNING: model '$Model' is not pulled; run: ollama pull $Model" -ForegroundColor Yellow
}
if (Get-Command nvidia-smi -ErrorAction SilentlyContinue) {
    nvidia-smi --query-gpu=name,driver_version,memory.total --format=csv,noheader
} else {
    Write-Host "  no NVIDIA GPU detected - Ollama will use the CPU (slower)" -ForegroundColor Yellow
}

Write-Host "[2/4] Checking the control plane..." -ForegroundColor Yellow
try {
    Invoke-RestMethod -Uri "$ControlPlane/api/v1/health" -TimeoutSec 10 | Out-Null
    Write-Host "  reachable: $ControlPlane"
} catch {
    throw "Cannot reach $ControlPlane/api/v1/health : $_"
}

if ($Mode -eq "docker") {
    Write-Host "[3/4] Building the worker image..." -ForegroundColor Yellow
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw "Docker not found (https://www.docker.com/products/docker-desktop/), or use -Mode native" }
    docker build -t processor-worker:latest -f "$Root\data-plane\Dockerfile" "$Root\data-plane"
    if ($LASTEXITCODE -ne 0) { throw "docker build failed" }

    Write-Host "[4/4] Starting the worker container..." -ForegroundColor Yellow
    $envFile = Join-Path $Root ".env.windows"   # holds the token; git-ignored
    @"
WORKER_ID=$WorkerId
WORKER_TOKEN=$Token
CONTROL_PLANE_URL=$ControlPlane
POLL_INTERVAL=5s
JOB_TIMEOUT=10m
WORKER_CAPABILITIES=$Capabilities
OLLAMA_BASE_URL=http://host.docker.internal:11434
LLM_DEFAULT_MODEL=$Model
"@ | Set-Content -Path $envFile -Encoding ascii
    docker rm -f processor-worker-windows 2>$null | Out-Null
    docker run -d --restart unless-stopped --name processor-worker-windows --env-file $envFile processor-worker:latest
    if ($LASTEXITCODE -ne 0) { throw "docker run failed" }
    Write-Host "Logs: docker logs -f processor-worker-windows" -ForegroundColor Gray
    return
}

Write-Host "[3/4] Building worker.exe..." -ForegroundColor Yellow
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw "Go not found (https://go.dev/dl/), or use -Mode docker" }
$exe = Join-Path $Root "data-plane\worker.exe"
Push-Location (Join-Path $Root "data-plane")
try { go build -o $exe ./cmd/worker; if ($LASTEXITCODE -ne 0) { throw "go build failed" } } finally { Pop-Location }

$env:WORKER_ID = $WorkerId; $env:WORKER_TOKEN = $Token; $env:CONTROL_PLANE_URL = $ControlPlane
$env:WORKER_CAPABILITIES = $Capabilities; $env:LLM_DEFAULT_MODEL = $Model
if (-not $env:JOB_TIMEOUT) { $env:JOB_TIMEOUT = "10m" }

if ($InstallTask) {
    # Scheduled tasks don't inherit this shell's variables, so wrap the launch in a small
    # script kept in the user's profile (the token stays in your own account's files).
    $dir = Join-Path $env:LOCALAPPDATA "processor-worker"
    New-Item -ItemType Directory -Force $dir | Out-Null
    $launch = Join-Path $dir "run.ps1"
    @"
`$env:WORKER_ID='$WorkerId'; `$env:WORKER_TOKEN='$Token'; `$env:CONTROL_PLANE_URL='$ControlPlane'
`$env:WORKER_CAPABILITIES='$Capabilities'; `$env:LLM_DEFAULT_MODEL='$Model'; `$env:JOB_TIMEOUT='$($env:JOB_TIMEOUT)'
& '$exe' *>> '$dir\worker.log'
"@ | Set-Content -Path $launch -Encoding ascii
    icacls $launch /inheritance:r /grant:r "${env:USERNAME}:(R,W)" | Out-Null
    $action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$launch`""
    $trigger = New-ScheduledTaskTrigger -AtLogOn
    $settings = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero)
    Register-ScheduledTask -TaskName "ProcessorWorker" -Action $action -Trigger $trigger -Settings $settings -Force | Out-Null
    Start-ScheduledTask -TaskName "ProcessorWorker"
    Write-Host "[4/4] Installed scheduled task 'ProcessorWorker' (starts at logon). Log: $dir\worker.log" -ForegroundColor Green
    return
}

Write-Host "[4/4] Starting worker.exe (Ctrl+C to stop)..." -ForegroundColor Yellow
& $exe
