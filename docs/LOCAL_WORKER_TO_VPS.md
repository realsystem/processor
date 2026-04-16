# Run Local Worker Connected to VPS

This guide shows how to run a worker on your local Mac (in Docker) that connects to the VPS control-plane.

## Architecture

```
┌─────────────────┐
│  VPS (Remote)   │
│                 │
│  Control Plane  │ ← Jobs submitted here
│  :8080          │
└────────┬────────┘
         │
         │ HTTP Polling
         │
┌────────▼────────┐
│  Local Mac      │
│                 │
│  Worker         │ ← Processes jobs
│  (Docker)       │
│                 │
│  ↓              │
│  Ollama         │ ← LLM execution
│  :11434         │
└─────────────────┘
```

## Quick Start

### 1. Setup Ollama (Optional but recommended)

```bash
make ollama-setup
```

### 2. Run Local Worker

```bash
./scripts/run-local-worker-to-vps.sh
```

This will:
- Fetch worker token from VPS
- Check if Ollama is running
- Build worker Docker image
- Start worker container connected to VPS
- Show worker logs

Password: `xOk0n76EY2EmEe`

### 3. Get API Key

If you don't have an API key yet:

```bash
# Create on VPS
ssh root@66.179.241.224 '/opt/processor/create-key.sh admin@example.com "My Key"'

# Or use web UI
open http://66.179.241.224:8080
# Login → Profile → API Keys → Create
```

Save the API key:
```bash
export API_KEY="pk_your_key_here"

# Save for future sessions
echo 'export API_KEY="pk_your_key_here"' > ~/.processor-vps-api-key
source ~/.processor-vps-api-key
```

### 4. Submit Test Jobs

```bash
# Echo job
./scripts/test-vps-job.sh echo

# Sleep job
./scripts/test-vps-job.sh sleep

# Ollama LLM job (requires Ollama)
./scripts/test-vps-job.sh ollama

# No-op job
./scripts/test-vps-job.sh noop
```

## Manual Job Submission

```bash
# Submit an echo job
curl -X POST http://66.179.241.224:8080/api/v1/jobs \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "job_type": "echo_metadata",
    "input_metadata": {
      "message": "Hello from API!"
    }
  }'

# Submit an Ollama job
curl -X POST http://66.179.241.224:8080/api/v1/jobs \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "job_type": "ollama_chat",
    "input_metadata": {
      "model": "llama3.2",
      "prompt": "Explain Docker in one sentence."
    }
  }'
```

## Worker Management

### View Logs

```bash
docker logs processor-worker-local -f
```

### Stop Worker

```bash
docker stop processor-worker-local
```

### Restart Worker

```bash
docker restart processor-worker-local
```

### Remove Worker

```bash
docker stop processor-worker-local
docker rm processor-worker-local
```

### Rebuild and Restart

```bash
# After making code changes
docker stop processor-worker-local
docker rm processor-worker-local
docker build -t processor-worker-local ./data-plane
docker run -d \
  --name processor-worker-local \
  --restart unless-stopped \
  -e CONTROL_PLANE_URL="http://66.179.241.224:8080" \
  -e WORKER_TOKEN="$(ssh root@66.179.241.224 'cat /opt/processor/WORKER_TOKEN.txt')" \
  -e WORKER_ID="worker_local_$(hostname -s)" \
  -e WORKER_NAME="local-mac-worker" \
  -e POLL_INTERVAL="5s" \
  -e WORKER_CAPABILITIES="noop,sleep,echo_metadata,ollama_chat" \
  -e OLLAMA_BASE_URL="http://host.docker.internal:11434" \
  -e LOG_LEVEL="info" \
  processor-worker-local
```

## Troubleshooting

### Worker can't connect to VPS

```bash
# Test VPS is accessible
curl http://66.179.241.224:8080/api/v1/health

# Check worker logs
docker logs processor-worker-local --tail 50

# Verify worker token
ssh root@66.179.241.224 'cat /opt/processor/WORKER_TOKEN.txt'
```

### Worker can't access Ollama

```bash
# Check Ollama is running
docker ps | grep ollama
# or
curl http://localhost:11434/api/tags

# Start Ollama if needed
make ollama-start

# Test from inside worker container
docker exec processor-worker-local curl http://host.docker.internal:11434/api/tags
```

### Jobs not being picked up

```bash
# Check worker capabilities match job type
docker logs processor-worker-local | grep Capabilities

# Verify worker is registered in VPS
curl http://66.179.241.224:8080/api/v1/admin/workers \
  -H "Authorization: Bearer $API_KEY"

# Check job queue
curl http://66.179.241.224:8080/api/v1/jobs?status=queued \
  -H "Authorization: Bearer $API_KEY"
```

### Authentication errors

```bash
# Verify API key works
curl -H "Authorization: Bearer $API_KEY" \
  http://66.179.241.224:8080/api/v1/me

# Check worker token is correct
docker exec processor-worker-local env | grep WORKER_TOKEN
```

## Configuration

### Environment Variables

The worker supports these environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `CONTROL_PLANE_URL` | - | VPS control-plane URL (required) |
| `WORKER_TOKEN` | - | Authentication token (required) |
| `WORKER_ID` | `worker_local_hostname` | Unique worker identifier |
| `WORKER_NAME` | `local-mac-worker` | Human-readable name |
| `POLL_INTERVAL` | `5s` | How often to poll for jobs |
| `WORKER_CAPABILITIES` | `noop,sleep,echo_metadata,ollama_chat` | Supported job types |
| `OLLAMA_BASE_URL` | `http://host.docker.internal:11434` | Ollama API endpoint |
| `LOG_LEVEL` | `info` | Logging level (debug, info, warn, error) |

### Custom Configuration

Edit the run command in `scripts/run-local-worker-to-vps.sh` to customize.

## Use Cases

### Development

Run worker locally while developing:
- Test changes without deploying
- Debug with full logs
- Use local Ollama models

### Hybrid Setup

- Control plane on VPS (always available)
- Worker on Mac (more powerful, has GPU)
- Ollama models on Mac (faster, more memory)

### Multiple Workers

Run multiple workers for parallel processing:

```bash
# Worker 1
./scripts/run-local-worker-to-vps.sh

# Worker 2 (rename container)
docker run -d \
  --name processor-worker-local-2 \
  ... # same config
```

## Next Steps

1. Monitor jobs in web UI: http://66.179.241.224:8080/jobs
2. Set up webhooks for job notifications
3. Deploy additional workers on other machines
4. Configure HTTPS for production (see PRODUCTION_READINESS.md)
