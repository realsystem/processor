# Ollama Integration Setup

The data-plane worker supports executing jobs using Ollama for local LLM inference.

## Quick Start (Recommended)

The easiest way to get started:

```bash
# 1. Setup Ollama (starts container + pulls llama3.2 model)
make ollama-setup

# 2. Start the processor services
make dev-up

# 3. Test it works (requires API_KEY to be set)
export API_KEY="pk_your_key_here"
make ollama-test
```

**Check status anytime:**
```bash
make ollama-status
```

**Other commands:**
```bash
make ollama-start       # Start Ollama container
make ollama-stop        # Stop Ollama container
make ollama-pull-model  # Pull llama3.2 model
make help              # See all available commands
```

## Manual Setup

If you prefer manual control or need a different setup, choose one method below:

### Prerequisites

You need Ollama installed and running. Choose one method:

### Option 1: Docker (Recommended)

**Using make commands (easiest):**
```bash
make ollama-setup
```

**Or manually:**
```bash
# Pull and run Ollama with llama3.2 (small, fast model)
docker run -d \
  --name ollama \
  -p 11434:11434 \
  -v ollama-data:/root/.ollama \
  ollama/ollama

# Pull a model (this downloads ~2GB for llama3.2)
docker exec -it ollama ollama pull llama3.2

# Verify it's working
curl http://localhost:11434/api/generate -d '{
  "model": "llama3.2",
  "prompt": "Why is the sky blue?",
  "stream": false
}'
```

### Option 2: Native Install (macOS/Linux)

**macOS:**
```bash
# Install Ollama
brew install ollama

# Start Ollama service
ollama serve &

# Pull a model
ollama pull llama3.2
```

**Linux:**
```bash
# Install Ollama
curl -fsSL https://ollama.com/install.sh | sh

# Pull a model
ollama pull llama3.2
```

## Configuration

### Local Development (without Docker Compose)

Update `data-plane/.env`:

```bash
# Ollama Configuration
OLLAMA_BASE_URL=http://localhost:11434

# Make sure ollama_chat is in capabilities
WORKER_CAPABILITIES=noop,sleep,echo_metadata,ollama_chat
```

### Docker Compose

The docker-compose setup needs to be updated to connect the worker to Ollama.

**If Ollama is running on your host machine:**

Update `docker-compose.yml` worker service:

```yaml
worker:
  # ... existing config ...
  environment:
    - OLLAMA_BASE_URL=http://host.docker.internal:11434  # Access host's Ollama
    # ... other env vars ...
```

**Or add Ollama as a service:**

```yaml
services:
  ollama:
    image: ollama/ollama
    container_name: processor-ollama
    ports:
      - "11434:11434"
    volumes:
      - ollama-data:/root/.ollama
    networks:
      - processor-network

  worker:
    # ... existing config ...
    environment:
      - OLLAMA_BASE_URL=http://ollama:11434
    depends_on:
      - ollama

volumes:
  ollama-data:
  # ... other volumes ...
```

Then pull a model:
```bash
docker exec -it processor-ollama ollama pull llama3.2
```

## Available Models

Choose based on your hardware:

| Model | Size | RAM Required | Speed | Best For |
|-------|------|--------------|-------|----------|
| `llama3.2` | 2GB | 4GB | Fast | Testing, simple tasks |
| `llama3.2:3b` | 3GB | 6GB | Fast | Better quality |
| `llama3.1:8b` | 5GB | 8GB | Medium | Good balance |
| `llama3.1:70b` | 40GB | 64GB | Slow | Best quality |
| `mistral` | 4GB | 8GB | Medium | Good general purpose |
| `codellama` | 4GB | 8GB | Medium | Code generation |

Pull additional models:
```bash
ollama pull llama3.1:8b
ollama pull mistral
ollama pull codellama
```

## Testing

### 1. Verify Ollama is Running

```bash
# Check Ollama health
curl http://localhost:11434/api/tags

# Should return list of available models
```

### 2. Start the Worker

```bash
cd data-plane
make dev

# You should see in logs:
# Worker ID: worker_1
# Capabilities: [noop sleep echo_metadata ollama_chat]
```

### 3. Submit a Test Job

Make sure your API key is set:
```bash
export API_KEY="pk_your_api_key_here"
```

Submit an Ollama job:
```bash
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "job_type": "ollama_chat",
    "input_metadata": {
      "model": "llama3.2",
      "prompt": "Explain quantum computing in one sentence."
    },
    "priority_weight": 10
  }'
```

**Response:**
```json
{
  "success": true,
  "data": {
    "id": "job_abc123...",
    "job_type": "ollama_chat",
    "status": "queued"
  }
}
```

### 4. Check Job Results

Wait a few seconds, then check the job:

```bash
curl -H "Authorization: Bearer $API_KEY" \
     http://localhost:8080/api/v1/jobs/job_abc123...
```

**Expected response:**
```json
{
  "success": true,
  "data": {
    "id": "job_abc123...",
    "status": "succeeded",
    "result_metadata": {
      "status": "completed",
      "model": "llama3.2",
      "prompt": "Explain quantum computing in one sentence.",
      "response": "Quantum computing uses quantum mechanics principles...",
      "duration_seconds": 2.5,
      "response_length": 156
    }
  }
}
```

## Example Jobs

The executor supports multiple input formats for convenience:

### Structured Format (Recommended)
```json
{
  "job_type": "ollama_chat",
  "input_metadata": {
    "prompt": "What is the capital of France?",
    "model": "llama3.2"
  }
}
```

### Plain Text Format (Simple)
```json
{
  "job_type": "ollama_chat",
  "input_metadata": {
    "text": "What is the capital of France?"
  }
}
```

### Alternative Formats
Any of these work:
- `{"prompt": "..."}` - Structured (recommended)
- `{"text": "..."}` - Plain text
- `{"message": "..."}` - Message format
- `{"input": "..."}` - Input format

### Code Generation
```json
{
  "job_type": "ollama_chat",
  "input_metadata": {
    "text": "Write a Python function to calculate fibonacci numbers.",
    "model": "codellama"
  }
}
```

### Complex Analysis
```json
{
  "job_type": "ollama_chat",
  "input_metadata": {
    "message": "Analyze the pros and cons of microservices architecture.",
    "model": "llama3.1:8b"
  }
}
```

## Troubleshooting

### "failed to call Ollama API: connection refused"

**Issue:** Ollama is not running or worker can't reach it.

**Solutions:**
```bash
# Check if Ollama is running
curl http://localhost:11434/api/tags

# If using Docker Compose, check network
docker network inspect processor-network

# If worker is in Docker, use host.docker.internal or service name
```

### "model not found"

**Issue:** Model hasn't been pulled yet.

**Solution:**
```bash
# List available models
ollama list

# Pull the model
ollama pull llama3.2
```

### Job times out or takes too long

**Issue:** Large models can be slow, especially on first run.

**Solutions:**
- Use smaller models (llama3.2, mistral)
- First request downloads and loads model (can take 1-2 minutes)
- Subsequent requests are faster
- Increase job timeout if needed

### Out of memory

**Issue:** Model is too large for available RAM.

**Solutions:**
- Use smaller models
- Close other applications
- Check model requirements vs. available RAM

## Performance Notes

- **First request**: Slow (model loading + inference)
- **Subsequent requests**: Fast (model stays in memory)
- **Memory**: Models stay loaded in RAM for ~5 minutes after last use
- **Concurrency**: Ollama handles one request at a time per model
- **GPU**: If you have NVIDIA GPU, Ollama will use it automatically (much faster)

## Security Notes

- Ollama runs locally - no data sent to external APIs
- Models are stored locally in `~/.ollama` or Docker volume
- Consider prompt injection risks when using user input
- Rate limit or validate prompts in production
