# Windows Worker Setup Script (PowerShell)
# Sets up worker on Windows PC with GPU support for Ollama

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Windows Worker Setup (GPU-Enabled)" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Configuration
$VPS_IP = "66.179.241.224"
$VPS_URL = "http://${VPS_IP}:8080"
$WORKER_ID = "worker_windows_gpu"

# Step 1: Check prerequisites
Write-Host "[1/6] Checking prerequisites..." -ForegroundColor Yellow
Write-Host ""

# Check Docker
Write-Host "Checking Docker Desktop..." -ForegroundColor Gray
if (Get-Command docker -ErrorAction SilentlyContinue) {
    docker --version
    Write-Host "✓ Docker found" -ForegroundColor Green
} else {
    Write-Host "✗ Docker not found" -ForegroundColor Red
    Write-Host ""
    Write-Host "Please install Docker Desktop for Windows:" -ForegroundColor Yellow
    Write-Host "https://www.docker.com/products/docker-desktop/" -ForegroundColor Cyan
    exit 1
}

# Check Ollama
Write-Host ""
Write-Host "Checking Ollama..." -ForegroundColor Gray
if (Get-Command ollama -ErrorAction SilentlyContinue) {
    ollama --version
    Write-Host "✓ Ollama found" -ForegroundColor Green
} else {
    Write-Host "✗ Ollama not found" -ForegroundColor Red
    Write-Host ""
    Write-Host "Please install Ollama for Windows:" -ForegroundColor Yellow
    Write-Host "https://ollama.com/download/windows" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "After installation, pull a model:" -ForegroundColor Yellow
    Write-Host "  ollama pull llama3.2" -ForegroundColor Gray
    exit 1
}

# Check GPU (optional)
Write-Host ""
Write-Host "Checking NVIDIA GPU..." -ForegroundColor Gray
if (Get-Command nvidia-smi -ErrorAction SilentlyContinue) {
    nvidia-smi --query-gpu=name,driver_version,memory.total --format=csv,noheader
    Write-Host "✓ NVIDIA GPU detected - Ollama will use GPU acceleration!" -ForegroundColor Green
} else {
    Write-Host "⚠ NVIDIA GPU not detected - will use CPU (slower)" -ForegroundColor Yellow
}

# Step 2: Test Ollama
Write-Host ""
Write-Host "[2/6] Testing Ollama..." -ForegroundColor Yellow
$ollamaTest = Invoke-RestMethod -Uri "http://localhost:11434/api/tags" -ErrorAction SilentlyContinue
if ($ollamaTest) {
    Write-Host "✓ Ollama is running" -ForegroundColor Green
    Write-Host "Available models:" -ForegroundColor Gray
    $ollamaTest.models | ForEach-Object { Write-Host "  - $($_.name)" -ForegroundColor Gray }
} else {
    Write-Host "✗ Ollama is not responding on http://localhost:11434" -ForegroundColor Red
    Write-Host ""
    Write-Host "Make sure Ollama is running and try again." -ForegroundColor Yellow
    exit 1
}

# Step 3: Fetch worker token from VPS
Write-Host ""
Write-Host "[3/6] Fetching worker token from VPS..." -ForegroundColor Yellow
try {
    $response = Invoke-WebRequest -Uri "$VPS_URL/worker/config" -UseBasicParsing
    $config = $response.Content | ConvertFrom-Json
    $WORKER_TOKEN = $config.worker_token
    Write-Host "✓ Worker token retrieved" -ForegroundColor Green
} catch {
    Write-Host "✗ Failed to fetch worker token from VPS" -ForegroundColor Red
    Write-Host ""
    Write-Host "Error: $_" -ForegroundColor Red
    Write-Host ""
    Write-Host "Please check:" -ForegroundColor Yellow
    Write-Host "  1. VPS is running: $VPS_URL" -ForegroundColor Gray
    Write-Host "  2. Network connectivity to VPS" -ForegroundColor Gray
    exit 1
}

# Step 4: Create .env file
Write-Host ""
Write-Host "[4/6] Creating .env file..." -ForegroundColor Yellow
$envContent = @"
# Worker Configuration
WORKER_ID=$WORKER_ID
WORKER_TOKEN=$WORKER_TOKEN
CONTROL_PLANE_URL=$VPS_URL
POLL_INTERVAL=5s
WORKER_CAPABILITIES=noop,sleep,echo_metadata,ollama_chat

# Ollama Configuration
OLLAMA_BASE_URL=http://host.docker.internal:11434
"@

Set-Content -Path ".env.windows" -Value $envContent
Write-Host "✓ Created .env.windows" -ForegroundColor Green

# Step 5: Build worker Docker image
Write-Host ""
Write-Host "[5/6] Building worker Docker image..." -ForegroundColor Yellow
Write-Host "This may take a few minutes..." -ForegroundColor Gray
docker build -t processor-worker:latest -f data-plane/Dockerfile ./data-plane
if ($LASTEXITCODE -eq 0) {
    Write-Host "✓ Docker image built" -ForegroundColor Green
} else {
    Write-Host "✗ Failed to build Docker image" -ForegroundColor Red
    exit 1
}

# Step 6: Run worker container
Write-Host ""
Write-Host "[6/6] Starting worker container..." -ForegroundColor Yellow

# Stop existing container if running
docker stop processor-worker-windows 2>$null
docker rm processor-worker-windows 2>$null

# Run worker
docker run -d `
    --name processor-worker-windows `
    --env-file .env.windows `
    processor-worker:latest

if ($LASTEXITCODE -eq 0) {
    Write-Host "✓ Worker started" -ForegroundColor Green
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Cyan
    Write-Host "Worker is now running!" -ForegroundColor Green
    Write-Host "========================================" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "Worker ID: $WORKER_ID" -ForegroundColor Gray
    Write-Host "VPS Control Plane: $VPS_URL" -ForegroundColor Gray
    Write-Host "Ollama: http://localhost:11434 (GPU-accelerated)" -ForegroundColor Gray
    Write-Host ""
    Write-Host "View logs:" -ForegroundColor Yellow
    Write-Host "  docker logs -f processor-worker-windows" -ForegroundColor Gray
    Write-Host ""
    Write-Host "Stop worker:" -ForegroundColor Yellow
    Write-Host "  docker stop processor-worker-windows" -ForegroundColor Gray
} else {
    Write-Host "✗ Failed to start worker" -ForegroundColor Red
    exit 1
}
