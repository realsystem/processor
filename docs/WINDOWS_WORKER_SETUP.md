# Windows Worker Setup (GPU-Enabled)

Run the data-plane worker on a Windows PC so it serves `llm.chat` / `ollama_chat` jobs from your
control plane using the PC's GPU. The worker only makes outbound requests to the control plane;
no inbound port is needed on the PC. It registers itself on its first poll, so nothing has to be
created on the server.

## Prerequisites

1. [Ollama for Windows](https://ollama.com/download/windows), with a model pulled: `ollama pull llama3.2`
2. NVIDIA GPU with current drivers (optional; without one Ollama runs on the CPU, much slower)
3. One of: [Go](https://go.dev/dl/) (native `worker.exe`, no Docker) or Docker Desktop with WSL2
4. The control plane reachable from the PC, ideally over **HTTPS or a private network**
   ([private-network.md](private-network.md)). Over plain HTTP on a public address the worker
   token and every prompt and answer are unencrypted.
5. The `WORKER_TOKEN` from the server's `.env` (copy it by hand; the control plane never serves it)

## One-shot install (recommended)

From PowerShell (admin not required for most packages; accept the winget prompts):

```powershell
git clone https://github.com/realsystem/processor; cd processor
$env:CONTROL_PLANE_URL = "https://your-control-plane"
powershell -ExecutionPolicy Bypass -File .\scripts\install-windows.ps1 -Setup
```

It installs Git, Go and Ollama with winget, pulls the model (`-Model llama3.1:8b` to change),
and with `-Setup` builds the worker and installs it as a start-at-logon task. Add `-Tailscale`
for a private link to your server or `-Docker` for Docker Desktop. It does not install the
NVIDIA driver (https://www.nvidia.com/drivers). Equivalent manual commands:

```powershell
winget install -e --id Git.Git
winget install -e --id GoLang.Go
winget install -e --id Ollama.Ollama
winget install -e --id tailscale.tailscale        # optional
winget install -e --id Docker.DockerDesktop       # optional, docker mode only
ollama pull llama3.2
```

## Quick start (script)

PowerShell, from the repository root:

```powershell
$env:CONTROL_PLANE_URL = "https://your-control-plane"
$env:WORKER_TOKEN      = "<WORKER_TOKEN from the server>"   # omit to be prompted
.\scripts\setup-windows-worker.ps1                 # native worker.exe
.\scripts\setup-windows-worker.ps1 -InstallTask    # native, and start at every logon
.\scripts\setup-windows-worker.ps1 -Mode docker    # Docker Desktop instead
```

Optional environment variables: `WORKER_ID` (default `worker_windows_gpu`), `WORKER_CAPABILITIES`
(default `noop,sleep,echo_metadata,ollama_chat,llm.chat`), `LLM_DEFAULT_MODEL` (default `llama3.2`).
The script checks Ollama and the control plane's `/api/v1/health`, then builds and starts the worker.
If PowerShell blocks the script: `powershell -ExecutionPolicy Bypass -File .\scripts\setup-windows-worker.ps1`.

## Manual native run

```powershell
cd data-plane
go build -o worker.exe .\cmd\worker
$env:CONTROL_PLANE_URL    = "https://your-control-plane"
$env:WORKER_TOKEN         = "<WORKER_TOKEN>"
$env:WORKER_ID            = "worker_windows_gpu"
$env:WORKER_CAPABILITIES  = "llm.chat,ollama_chat,noop"
$env:LLM_DEFAULT_MODEL    = "llama3.2"
$env:JOB_TIMEOUT          = "10m"      # default 5m; large models may need more
.\worker.exe
```

Ollama is found at `http://localhost:11434` by default. To use LM Studio, llama.cpp or vLLM
instead, set `LLM_BACKEND=openai` and `LLM_BASE_URL=http://localhost:1234` (see
[job-options.md](job-options.md)).

Instead of the shared token you can use a per-worker Ed25519 key: set `WORKER_KEY_FILE`, run
`worker.exe -print-public-key`, and register it on the server with
`-register-worker-key <worker_id>=<key>` (see [job-options.md](job-options.md)).

## Verify

The worker log should show `Worker started, polling for jobs...`. The worker then appears under
Admin > Workers. Submit a job (needs an API key):

```powershell
$h = @{ Authorization = "Bearer $env:API_KEY" }
$body = @{ job_type = "llm.chat"; input_metadata = @{ prompt = "Write a haiku about GPUs" } } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri "$env:CONTROL_PLANE_URL/api/v1/jobs" -Headers $h -ContentType "application/json" -Body $body
```

Watch Task Manager > Performance > GPU, or `nvidia-smi -l 1`, while it runs. The first request
loads the model into VRAM and is slower; set `"keep_alive": "30m"` in the job input to keep it loaded.

## Running in the background

- `-InstallTask` registers a scheduled task `ProcessorWorker` that starts at logon and restarts on
  failure. Log: `%LOCALAPPDATA%\processor-worker\worker.log`. Remove it with
  `Unregister-ScheduledTask ProcessorWorker -Confirm:$false`. Its launcher file holds the token
  and is restricted to your user.
- Docker mode uses `--restart unless-stopped`; stop it with `docker rm -f processor-worker-windows`.

## Troubleshooting

- **Cannot reach the control plane**: `Test-NetConnection <host> -Port 443` (or 8080); check the
  server firewall.
- **401/403 from the control plane**: wrong `WORKER_TOKEN`.
- **Jobs of a type are never picked up**: that type is missing from `WORKER_CAPABILITIES`.
- **Ollama not responding**: start it with `ollama serve`; confirm `curl http://localhost:11434/api/tags`.
- **No GPU use**: check `nvidia-smi`, update drivers, and make sure the model fits in VRAM
  (about 2 GB for llama3.2, 5 GB for llama3.1:8b).
- **Docker mode cannot reach Ollama**: use the WSL2 backend; test with
  `docker run --rm curlimages/curl curl http://host.docker.internal:11434/api/tags`.

## Multiple workers

Run each with its own `WORKER_ID`; they register themselves. Give a GPU worker `llm.chat,ollama_chat`
and CPU-only workers the lightweight types.
