# Quick Start Guide

This guide will help you get the Processor platform running locally for development.

> **⚡ TL;DR for macOS:** `brew install go sqlite` then skip to [Option 1](#option-1-local-development)

> **⚡ First time?** If you get "command not found: go", see [Installing Prerequisites](#installing-prerequisites) below.

## Prerequisites

- Go 1.21 or later
- SQLite 3  
- (Optional) Docker and Docker Compose

## Installing Prerequisites

### macOS

#### Install Homebrew (if not already installed)
```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

#### Install Go
```bash
# Install Go 1.21+
brew install go

# Verify installation
go version
# Should output: go version go1.21.x darwin/arm64 (or amd64)
```

#### Install SQLite (usually pre-installed)
```bash
# Check if SQLite is installed
sqlite3 --version
# Should output: 3.x.x

# If not installed:
brew install sqlite
```

#### Install Docker (optional, for Docker Compose method)
```bash
# Download Docker Desktop from:
# https://www.docker.com/products/docker-desktop

# Or install via Homebrew:
brew install --cask docker

# Start Docker Desktop from Applications
```

#### Install curl (usually pre-installed)
```bash
# Verify curl is installed
curl --version

# If not installed:
brew install curl
```

### Linux (Ubuntu/Debian)

```bash
# Update package list
sudo apt update

# Install Go 1.21+
sudo apt install -y wget
wget https://go.dev/dl/go1.21.6.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.21.6.linux-amd64.tar.gz

# Add Go to PATH (add to ~/.bashrc or ~/.zshrc)
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Verify installation
go version

# Install SQLite
sudo apt install -y sqlite3

# Install Docker (optional)
sudo apt install -y docker.io docker-compose

# Install curl (usually pre-installed)
sudo apt install -y curl
```

### Verify All Prerequisites

```bash
# Check Go
go version
# Expected: go version go1.21.x ...

# Check SQLite
sqlite3 --version
# Expected: 3.x.x

# Check curl
curl --version
# Expected: curl 7.x.x or 8.x.x

# Check make (required for Makefiles)
make --version
# Expected: GNU Make 3.x or 4.x
# If not installed on Mac: xcode-select --install
# If not installed on Linux: sudo apt install build-essential
```

## Option 1: Local Development

### Step 1: Set up Control Plane

```bash
cd control-plane

# Copy example environment file
cp .env.example .env

# Edit .env if needed (defaults are fine for local dev)
# vim .env

# Download dependencies (this will populate go.sum automatically)
go mod download

# Optional: Clean up dependencies
go mod tidy

# Run database migrations (creates processor.db)
make migrate

# Start the server
make dev
```

**Note:** `go mod download` will automatically create/update `go.sum` with package checksums. This is normal and expected.

The control plane should now be running at `http://localhost:8080`.

### Step 2: Verify Control Plane

```bash
# Health check
curl http://localhost:8080/api/v1/health

# Should return:
# {"success":true,"data":{"status":"healthy","timestamp":"...","version":"1.0.0"}}

# You can also visit http://localhost:8080/api/v1/health in your browser
```

### Step 3: Create Admin User and API Key

> **Quick version:** Run these two commands
> ```bash
> go run scripts/create-admin.go
> go run scripts/create-api-key.go -email admin@example.com -name "Dev Key"
> ```

**Create the admin user:**

```bash
# Create admin user (email: admin@example.com, password: admin123)
go run scripts/create-admin.go
```

**Output:**
```
✅ Admin user created successfully!

📧 Email: admin@example.com
🔑 Password: admin123
```

**Create an API key for the admin user:**

```bash
go run scripts/create-api-key.go -email admin@example.com -name "My Test Key"
```

**Output:**
```
✅ API Key created successfully!

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔑 API Key: pk_5458996d39ae1cf8_0e0fad5d538b6f225e40c0ca50ab0b1f
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠️  IMPORTANT: Save this key now! It won't be shown again.
```

**IMPORTANT: Export the API key immediately (copy the actual key from above):**

```bash
# Export for current terminal session
export API_KEY="pk_5458996d39ae1cf8_0e0fad5d538b6f225e40c0ca50ab0b1f"

# Verify it's set
echo $API_KEY

# Test authentication
curl -H "Authorization: Bearer $API_KEY" http://localhost:8080/api/v1/me
```

**Expected response:**
```json
{
  "success": true,
  "data": {
    "email": "admin@example.com",
    "is_admin": true,
    "plan_tier": "free"
  }
}
```

**Save for future sessions (optional but recommended):**

```bash
# Save to a file you can source later
echo "export API_KEY=\"$API_KEY\"" > ~/.processor-api-key

# In future sessions, load it with:
source ~/.processor-api-key
```

### Step 4: Set up Data Plane Worker

Open a new terminal:

```bash
cd data-plane

# Copy example environment file
cp .env.example .env

# Download dependencies
go mod download
go mod tidy

# Start the worker (it will load .env automatically)
make dev
```

**Expected output:**
```
Loading environment from .env...
Starting Processor Data Plane Worker
Worker ID: worker_1
Control Plane: http://localhost:8080
Poll Interval: 5s
Capabilities: [noop sleep echo_metadata ollama_chat]
Worker started, polling for jobs...
```

The worker should now be polling the control plane for jobs!

**Note:** The default `.env.example` has `WORKER_TOKEN=dev-worker-token-change-in-production` which matches the control-plane's default. If you changed the control-plane's `WORKER_TOKEN`, update it in `data-plane/.env` to match.

### Step 4.5: Register the Worker

**IMPORTANT:** Before the worker can process jobs, you need to register it in the database.

In **Terminal 1** (control-plane directory):

```bash
cd control-plane
go run scripts/register-worker.go
```

**Output:**
```
✅ Worker registered successfully!

📋 Worker ID: worker_1
📝 Name: local-worker
🔧 Capabilities: [noop sleep echo_metadata ollama_chat]

The worker can now poll for jobs!
```

Now check **Terminal 2** (worker logs) - you should see successful polling instead of 500 errors:

```
2026/04/15 11:15:13 Worker started, polling for jobs...
# No more errors! Worker is now successfully polling.
```

### Step 5: Submit a Test Job

Make sure:
- ✅ Control plane is running (Terminal 1)
- ✅ Worker is running (Terminal 2)
- ✅ Worker is registered (Step 4.5 completed)
- ✅ API key is exported (from Step 3)

**Verify your API key is set:**

```bash
# Should show your API key
echo $API_KEY

# If not set, export it again (use the key from Step 3)
export API_KEY="pk_YOUR_ACTUAL_API_KEY_HERE"

# Test authentication
curl -H "Authorization: Bearer $API_KEY" \
     http://localhost:8080/api/v1/me
```

**Expected response:**
```json
{
  "success": true,
  "data": {
    "email": "admin@example.com",
    "is_admin": true
  }
}
```

**If you get `"error": "Authentication required"`:**
- Your `$API_KEY` is not set or is wrong
- Re-export it: `export API_KEY="pk_your_key_here"`

**Submit your first job:**

```bash
# Create an echo job
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "job_type": "echo_metadata",
    "input_metadata": {
      "message": "Hello from Processor!",
      "timestamp": "2026-04-15"
    },
    "priority_weight": 10,
    "notify_email": false
  }'
```

**Response:**
```json
{
  "success": true,
  "data": {
    "id": "job_c7145e66072171dd58945aa7",
    "job_type": "echo_metadata",
    "status": "queued",
    "priority_weight": 10,
    "created_at": "2026-04-15T11:11:49-07:00"
  }
}
```

**Watch Terminal 2 (worker logs)** - you should see it pick up and execute the job:

```
2026/04/15 11:15:18 Received job: job_c7145e66072171dd58945aa7 (type: echo_metadata)
2026/04/15 11:15:18 Executing job job_c7145e66072171dd58945aa7 of type echo_metadata
2026/04/15 11:15:18 EchoExecutor: echoing input metadata
2026/04/15 11:15:18 Job job_c7145e66072171dd58945aa7 completed successfully
```

🎉 **Success!** Your first job was processed!

### Step 6: Check Job Status

```bash
# List all your jobs
curl -H "Authorization: Bearer $API_KEY" \
     http://localhost:8080/api/v1/jobs

# Get specific job details (use the ID from Step 5 response)
curl -H "Authorization: Bearer $API_KEY" \
     http://localhost:8080/api/v1/jobs/job_abc123

# Filter by status
curl -H "Authorization: Bearer $API_KEY" \
     "http://localhost:8080/api/v1/jobs?status=succeeded"
```

**Expected response:**
```json
{
  "success": true,
  "data": {
    "id": "job_abc123...",
    "status": "succeeded",
    "result_metadata": {
      "echo": {
        "message": "Hello from Processor!",
        "timestamp": "2026-04-15"
      },
      "processed_at": "2026-04-15T10:15:05Z",
      "status": "completed"
    },
    "completed_at": "2026-04-15T10:15:05Z"
  }
}
```

## Option 2: Docker Compose (Easiest)

### Initial Setup

First, configure your environment variables:

```bash
# From the repository root
# Copy the example environment file
cp .env.example .env

# Edit .env to add your SMTP credentials (optional)
# The file is gitignored - safe to store secrets
nano .env
```

### Start Services

```bash
# Start all services
make dev-up

# Check logs
make dev-logs

# Stop services (keeps data)
make dev-down

# Reset everything (deletes data)
make dev-reset
```

Or use docker-compose directly:

```bash
docker-compose up -d       # Start
docker-compose logs -f     # View logs
docker-compose down        # Stop (keeps data)
docker-compose down -v     # Stop and delete data
```

This will start both the control plane and worker in containers.

**Default credentials:**
- Email: `admin@example.com`
- Password: `admin123`

**Note:** `.env` file is gitignored and contains secrets. Never commit it to version control.

## Next Steps

1. **Explore the API**: See [docs/api.md](docs/api.md) for complete API documentation
2. **Configure Authentication**: Set up proper session secrets and worker tokens
3. **Set up Webhooks**: Configure webhook notifications for job completion
4. **Deploy to Production**: See [docs/deployment.md](docs/deployment.md)

## Testing Different Job Types

### Noop Job (does nothing)
```json
{
  "job_type": "noop",
  "input_metadata": {}
}
```

### Sleep Job (sleeps for specified duration)
```json
{
  "job_type": "sleep",
  "input_metadata": {
    "duration_seconds": 5
  }
}
```

### Echo Job (echoes back input)
```json
{
  "job_type": "echo_metadata",
  "input_metadata": {
    "message": "Hello, world!",
    "custom_field": 123
  }
}
```

### Ollama Job (requires Ollama running locally)

Simple format:
```json
{
  "job_type": "ollama_chat",
  "input_metadata": {
    "text": "What is the meaning of life?"
  }
}
```

Structured format (with model selection):
```json
{
  "job_type": "ollama_chat",
  "input_metadata": {
    "prompt": "Explain quantum computing.",
    "model": "llama3.2"
  }
}
```

**Note:** This requires Ollama to be installed and running. See [docs/OLLAMA_SETUP.md](OLLAMA_SETUP.md) for setup instructions.

**Input formats supported:** `text`, `prompt`, `message`, or `input` - all work the same way!

## Troubleshooting

### "command not found: go"
You need to install Go. See the "Installing Prerequisites" section above.

**Quick fix for macOS:**
```bash
brew install go
```

**Quick fix for Linux:**
```bash
# Download and install Go 1.21
wget https://go.dev/dl/go1.21.6.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.6.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
go version
```

### "command not found: make"
**macOS:**
```bash
# Install Xcode Command Line Tools
xcode-select --install
```

**Linux:**
```bash
sudo apt install build-essential
```

### "command not found: sqlite3"
**macOS:**
```bash
brew install sqlite
```

**Linux:**
```bash
sudo apt install sqlite3
```

### "go mod download" fails
**Issue:** Network connectivity or proxy issues

**Solution:**
```bash
# Try with Go proxy
export GOPROXY=https://proxy.golang.org,direct
go mod download

# Or use direct mode
export GOPROXY=direct
go mod download
```

### "CGO_ENABLED" or "gcc" errors (when building control-plane)
**Issue:** SQLite requires CGO (C compiler)

**macOS:**
```bash
# Install Xcode Command Line Tools
xcode-select --install
```

**Linux:**
```bash
sudo apt install gcc
```

### Control plane won't start
- Check that port 8080 is not in use: `lsof -i :8080`
- Verify database permissions: `ls -la control-plane/processor.db`
- Check logs for errors
- Make sure you ran migrations: `cd control-plane && make migrate`

### Worker won't connect
- Verify CONTROL_PLANE_URL in .env points to the correct address
- Ensure WORKER_TOKEN matches between control plane and worker
- Check control plane is running: `curl http://localhost:8080/api/v1/health`

### Jobs aren't being processed
- Check worker logs for errors
- Verify worker capabilities match job types
- Check job status in database: `sqlite3 control-plane/processor.db "SELECT * FROM jobs;"`

### "No such file or directory: processor.db"
**Issue:** Database hasn't been created yet

**Solution:**
```bash
cd control-plane
make migrate
# This creates the database and runs migrations
```

### "WORKER_TOKEN is required"
**Issue:** Worker .env file is missing or empty

**Solution:**
```bash
cd data-plane
cp .env.example .env
# Then run: make dev
```

The Makefile will automatically load the .env file.

### Worker can't connect / "connection refused"
**Issue:** Control plane is not running

**Solution:**
```bash
# In Terminal 1: Start control plane
cd control-plane && make dev

# In Terminal 2: Start worker
cd data-plane && make dev
```

Make sure the control plane is running BEFORE starting the worker.

## Development Tips

- Use `make test` in each directory to run tests
- Use `make lint` to check code quality (requires golangci-lint)
- Database file is at `control-plane/processor.db` - you can inspect it with `sqlite3`
- Logs are printed to stdout - use systemd journal or redirect to files in production

## Example Workflow

```bash
# Terminal 1: Start control plane
cd control-plane && make dev

# Terminal 2: Start worker
cd data-plane && make dev

# Terminal 3: Submit jobs and check status
# (See API documentation for authentication)
```

For production deployment, see [docs/deployment.md](docs/deployment.md).
