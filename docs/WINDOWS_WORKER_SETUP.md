# Windows Worker Setup (GPU-Enabled)

This guide helps you set up a worker on Windows with GPU support for fast Ollama inference.

## Prerequisites

1. **NVIDIA GPU** with recent drivers (for GPU acceleration)
2. **Docker Desktop for Windows** - [Download](https://www.docker.com/products/docker-desktop/)
3. **Ollama for Windows** - [Download](https://ollama.com/download/windows)

## Quick Start (Automated)

1. **Clone the repository** (or copy files to Windows PC)

2. **Run the setup script** (PowerShell as Administrator):
   ```powershell
   cd \path\to\processor
   .\scripts\setup-windows-worker.ps1
   ```

The script will:
- Check prerequisites (Docker, Ollama, NVIDIA GPU)
- Test Ollama connectivity
- Fetch worker token from VPS
- Build worker Docker image
- Start worker container

## Manual Setup

### Step 1: Install Ollama

Download and install Ollama for Windows:
https://ollama.com/download/windows

After installation, pull a model:
```powershell
ollama pull llama3.2
```

Verify GPU is detected:
```powershell
ollama run llama3.2 "test"
# Check Task Manager > Performance > GPU to see GPU usage
```

### Step 2: Install Docker Desktop

Download and install Docker Desktop:
https://www.docker.com/products/docker-desktop/

Make sure WSL2 backend is enabled (default).

### Step 3: Verify NVIDIA GPU (Optional)

Check GPU is detected:
```powershell
nvidia-smi
```

Expected output:
```
+-----------------------------------------------------------------------------+
| NVIDIA-SMI 535.xx       Driver Version: 535.xx       CUDA Version: 12.x   |
|-------------------------------+----------------------+----------------------+
| GPU  Name            TCC/WDDM | Bus-Id        Disp.A | Volatile Uncorr. ECC |
| Fan  Temp  Perf  Pwr:Usage/Cap|         Memory-Usage | GPU-Util  Compute M. |
|===============================+======================+======================|
|   0  NVIDIA GeForce RTX...    |   00000000:01:00.0 On |                  N/A |
```

### Step 4: Get Worker Token

Fetch worker token from VPS:
```powershell
$VPS_URL = "http://<your-server-ip>:8080"
Invoke-RestMethod -Uri "$VPS_URL/worker/config" | ConvertTo-Json
```

Or manually retrieve from VPS:
```bash
ssh root@<your-server-ip>
cat /opt/processor/.env | grep WORKER_TOKEN
```

### Step 5: Create Configuration

Create `.env.windows` in the repository root:

```bash
# Worker Configuration
WORKER_ID=worker_windows_gpu
WORKER_TOKEN=dev-worker-token-change-in-production  # Use actual token from VPS
CONTROL_PLANE_URL=http://<your-server-ip>:8080
POLL_INTERVAL=5s
WORKER_CAPABILITIES=noop,sleep,echo_metadata,ollama_chat

# Ollama Configuration (Windows host)
OLLAMA_BASE_URL=http://host.docker.internal:11434
```

**Note:** `host.docker.internal` allows Docker containers to access Windows host services.

### Step 6: Build Worker Image

```powershell
cd \path\to\processor
docker build -t processor-worker:latest -f data-plane/Dockerfile ./data-plane
```

### Step 7: Register Worker on VPS

SSH to VPS and register the new worker:

```bash
ssh root@<your-server-ip>
cd /opt/processor
sqlite3 data/processor.db "INSERT INTO workers (id, name, capabilities, status) VALUES ('worker_windows_gpu', 'windows-gpu-worker', 'noop,sleep,echo_metadata,ollama_chat', 'active');"
```

Or use the registration script:
```bash
# Edit scripts/register-worker.go to set WORKER_ID=worker_windows_gpu
go run scripts/register-worker.go
```

### Step 8: Run Worker

```powershell
docker run -d `
  --name processor-worker-windows `
  --env-file .env.windows `
  processor-worker:latest
```

### Step 9: Verify

Check worker logs:
```powershell
docker logs -f processor-worker-windows
```

Expected output:
```
Loading environment from .env...
Starting Processor Data Plane Worker
Worker ID: worker_windows_gpu
Control Plane: http://<your-server-ip>:8080
Poll Interval: 5s
Capabilities: [noop sleep echo_metadata ollama_chat]
Worker started, polling for jobs...
```

## Testing

Submit a test job to verify GPU acceleration:

```powershell
$API_KEY = "pk_your_api_key_here"
$VPS_URL = "http://<your-server-ip>:8080"

Invoke-RestMethod -Method Post -Uri "$VPS_URL/api/v1/jobs" `
  -Headers @{"Authorization"="Bearer $API_KEY"; "Content-Type"="application/json"} `
  -Body (@{
    job_type = "ollama_chat"
    input_metadata = @{
      text = "Write a haiku about GPUs"
      model = "llama3.2"
    }
    priority_weight = 10
  } | ConvertTo-Json)
```

**With GPU:** Response in ~2-5 seconds  
**Without GPU (CPU):** Response in ~15-30 seconds

Monitor GPU usage in Task Manager > Performance > GPU while job runs.

## Performance Tips

1. **Use larger models with GPU:**
   ```powershell
   ollama pull llama3.1:8b   # Better quality, still fast on GPU
   ollama pull mistral       # Good general purpose
   ```

2. **Keep models loaded:**
   - First request loads model into VRAM (~5-10 seconds)
   - Subsequent requests are instant (model stays in memory)

3. **Monitor VRAM usage:**
   - llama3.2: ~2GB VRAM
   - llama3.1:8b: ~5GB VRAM
   - llama3.1:70b: ~40GB VRAM (requires high-end GPU)

4. **Check GPU utilization:**
   ```powershell
   # While job is running, check Task Manager or:
   nvidia-smi -l 1  # Updates every second
   ```

## Troubleshooting

### "Ollama not responding"

Check Ollama service:
```powershell
# Restart Ollama
taskkill /F /IM ollama.exe
ollama serve
```

### "Worker can't connect to VPS"

Check firewall:
- Windows Firewall may block outbound connections
- VPS firewall must allow port 8080

Test connectivity:
```powershell
Test-NetConnection -ComputerName <your-server-ip> -Port 8080
```

### "No GPU acceleration"

Verify:
1. NVIDIA drivers installed: `nvidia-smi`
2. GPU is not in TCC mode (for consumer GPUs)
3. Ollama shows GPU usage in Task Manager while running

### "Docker container can't reach host Ollama"

Make sure Docker Desktop is using WSL2 backend:
- Docker Desktop > Settings > General > Use WSL2 based engine (checked)

Test from container:
```powershell
docker run --rm curlimages/curl:latest curl http://host.docker.internal:11434/api/tags
```

## Stopping Worker

```powershell
docker stop processor-worker-windows
docker rm processor-worker-windows
```

## Updates

To update worker with new code:

```powershell
# Stop and remove old container
docker stop processor-worker-windows
docker rm processor-worker-windows

# Remove old image
docker rmi processor-worker:latest

# Rebuild and restart
docker build -t processor-worker:latest -f data-plane/Dockerfile ./data-plane
docker run -d --name processor-worker-windows --env-file .env.windows processor-worker:latest
```

## Multiple Workers

You can run multiple workers with different capabilities:

```powershell
# GPU worker for Ollama jobs
WORKER_ID=worker_windows_gpu_1
WORKER_CAPABILITIES=ollama_chat

# CPU worker for other jobs
WORKER_ID=worker_windows_cpu_1
WORKER_CAPABILITIES=noop,sleep,echo_metadata
```

Each needs to be registered separately on the VPS.
