# Docker Local Development Guide

Complete guide for running and testing Processor locally using Docker.

## Table of Contents

- [Quick Start](#quick-start)
- [Service Architecture](#service-architecture)
- [Docker Compose Setup](#docker-compose-setup)
- [Volume Management](#volume-management)
- [Networking](#networking)
- [Development Workflow](#development-workflow)
- [Debugging](#debugging)
- [Common Issues](#common-issues)
- [Testing](#testing)

## Quick Start

### Prerequisites

- Docker 20.10+
- Docker Compose 2.0+

**Verify installation**:
```bash
docker --version
docker-compose --version
```

### Start All Services

```bash
# From project root
make dev-up
```

This will:
1. Build Docker images for control-plane and data-plane
2. Start control-plane service
3. Run worker-bootstrap to register the worker
4. Start worker service
5. Create persistent volume for database

**Access the services**:
- Control Plane API: http://localhost:8080
- Health Check: http://localhost:8080/api/v1/health

### Stop All Services

```bash
make dev-down
```

## Service Architecture

The Docker environment consists of three main services:

### 1. Control Plane (`control-plane`)

**Purpose**: Public-facing API server and job queue manager

**Built from**: `control-plane/Dockerfile`

**Key features**:
- Multi-stage build (builder + runtime)
- Runs as non-root user (UID 1000)
- Health check endpoint
- Persistent SQLite database

**Environment variables**:
```yaml
DATABASE_PATH: /data/processor.db
SESSION_SECRET: docker-dev-session-secret-change-in-production
WORKER_TOKEN: docker-dev-worker-token
HTTP_PORT: 8080
ENV: development
ADMIN_EMAIL: admin@example.com
ADMIN_PASSWORD: admin123
LOG_LEVEL: info
LEASE_DURATION: 5m
```

**Ports**:
- `8080:8080` - HTTP API

**Healthcheck**:
```bash
wget --quiet --tries=1 --spider http://localhost:8080/api/v1/health
# Interval: 30s, Timeout: 10s, Retries: 3, Start period: 10s
```

### 2. Worker Bootstrap (`worker-bootstrap`)

**Purpose**: One-time initialization to register worker in database

**Built from**: `control-plane/Dockerfile` (reuses same image)

**What it does**:
1. Waits for control-plane to be healthy
2. Runs database migrations
3. Inserts worker registration record into SQLite
4. Exits after completion

**Worker registration**:
```sql
INSERT OR IGNORE INTO workers (
  id, name, auth_token_hash, status, 
  capabilities_json, version, 
  last_seen_at, created_at, updated_at
) VALUES (
  'worker_docker',
  'docker-worker',
  '$2a$10$dummyhash',
  'active',
  '["noop","sleep","echo_metadata","ollama_chat"]',
  '1.0.0',
  datetime('now'),
  datetime('now'),
  datetime('now')
);
```

**Restart policy**: `no` (runs once)

### 3. Worker (`worker`)

**Purpose**: Polls control-plane for jobs and executes them

**Built from**: `data-plane/Dockerfile`

**Key features**:
- Multi-stage build with optimizations
- Statically linked binary
- Runs as non-root user (UID 1000)
- Graceful shutdown support

**Environment variables**:
```yaml
CONTROL_PLANE_URL: http://control-plane:8080
WORKER_TOKEN: docker-dev-worker-token
WORKER_ID: worker_docker
WORKER_NAME: docker-worker
POLL_INTERVAL: 5s
WORKER_CAPABILITIES: noop,sleep,echo_metadata,ollama_chat
LOG_LEVEL: info
```

**Dependencies**:
- Waits for control-plane health check
- Waits for worker-bootstrap completion

### 4. Smoke Test (Optional)

**Purpose**: Verify system is working end-to-end

**Profile**: `testing` (not started by default)

**Run smoke tests**:
```bash
make smoke-test
```

## Docker Compose Setup

### Full Configuration

```yaml
version: '3.8'

networks:
  processor-network:
    driver: bridge

volumes:
  processor-data:

services:
  control-plane:
    # See docker-compose.yml for full config
  
  worker-bootstrap:
    # See docker-compose.yml for full config
  
  worker:
    # See docker-compose.yml for full config
    depends_on:
      control-plane:
        condition: service_healthy
      worker-bootstrap:
        condition: service_completed_successfully
```

### Key Design Decisions

**Why multi-stage builds?**
- Smaller runtime images (Alpine ~20MB vs builder ~500MB)
- Improved security (no build tools in runtime)
- Faster pulls and deployments

**Why non-root users?**
- Security best practice
- Prevents privilege escalation
- Required for production deployments

**Why bootstrap service?**
- SQLite doesn't support concurrent writes during startup
- Ensures worker exists before worker service starts polling
- Idempotent (safe to run multiple times)

**Why bridge network?**
- Isolated from host network
- DNS-based service discovery
- Services can communicate by name

## Volume Management

### Persistent Data Volume

```bash
# Volume name
processor-data

# Mounted at
/data (in containers)

# Contains
- processor.db (SQLite database)
- processor.db-wal (Write-Ahead Log)
- processor.db-shm (Shared memory file)
```

### View Volume

```bash
# List all volumes
docker volume ls | grep processor

# Inspect volume
docker volume inspect processor_processor-data

# Location on host (macOS/Linux may differ)
docker volume inspect processor_processor-data | grep Mountpoint
```

### Backup Database

```bash
# Copy database from volume
docker run --rm \
  -v processor_processor-data:/data \
  -v $(pwd):/backup \
  alpine cp /data/processor.db /backup/processor-backup.db

# Or use docker-compose
docker-compose run --rm control-plane \
  sh -c "cp /data/processor.db /backup/processor-backup.db"
```

### Reset Database

```bash
# Stop services
make dev-down

# Remove volume
docker volume rm processor_processor-data

# Restart (will create fresh database)
make dev-up
```

### Restore Database

```bash
# Stop services
make dev-down

# Copy backup to volume
docker run --rm \
  -v processor_processor-data:/data \
  -v $(pwd):/backup \
  alpine cp /backup/processor-backup.db /data/processor.db

# Restart services
make dev-up
```

## Networking

### Service Discovery

Services communicate using Docker DNS:

```bash
# From worker container
curl http://control-plane:8080/api/v1/health

# From control-plane container
# (no outbound calls in this architecture)
```

### Port Mapping

Only control-plane is exposed to host:

```
Host:8080 -> control-plane:8080
```

Worker and bootstrap are internal-only.

### Network Isolation

```bash
# View network
docker network inspect processor_processor-network

# List connected containers
docker network inspect processor_processor-network | grep Name
```

## Development Workflow

### Standard Development Loop

```bash
# 1. Start services
make dev-up

# 2. View logs
make dev-logs

# 3. Make code changes in control-plane/ or data-plane/

# 4. Rebuild and restart
make dev-down
make dev-up

# 5. Test changes
curl http://localhost:8080/api/v1/health
```

### Faster Iteration

```bash
# Restart services without rebuilding
make dev-restart

# Rebuild specific service
docker-compose build control-plane
docker-compose up -d control-plane

# Rebuild everything
docker-compose build
```

### View Service Status

```bash
make dev-status

# Or directly
docker-compose ps
```

### View Logs

```bash
# All services
make dev-logs

# Specific service
docker-compose logs -f control-plane
docker-compose logs -f worker

# Last N lines
docker-compose logs --tail=50 control-plane

# Since timestamp
docker-compose logs --since 2024-04-15T10:00:00 worker
```

### Execute Commands in Containers

```bash
# Open shell in control-plane
docker-compose exec control-plane sh

# Run migration
docker-compose exec control-plane ./server -migrate

# Query database
docker-compose exec control-plane \
  sqlite3 /data/processor.db "SELECT * FROM jobs LIMIT 10;"

# Check worker status
docker-compose exec worker ps aux
```

### Rebuild After Code Changes

```bash
# Clean rebuild
docker-compose down
docker-compose build --no-cache
docker-compose up -d

# Or use Makefile
make clean
make dev-up
```

## Debugging

### View Container Logs

```bash
# Real-time logs
docker-compose logs -f control-plane worker

# Errors only
docker-compose logs control-plane | grep -i error

# Warnings and errors
docker-compose logs control-plane | grep -iE "warn|error"
```

### Inspect Container Health

```bash
# Check health status
docker-compose ps

# View health check logs
docker inspect processor-control-plane | grep -A 10 Health

# Manual health check
curl http://localhost:8080/api/v1/health
```

### Database Debugging

```bash
# Connect to SQLite
docker-compose exec control-plane sqlite3 /data/processor.db

# Inside SQLite shell
.tables
.schema jobs
SELECT * FROM jobs ORDER BY created_at DESC LIMIT 5;
SELECT * FROM workers;
.quit
```

### Network Debugging

```bash
# Test connectivity from worker to control-plane
docker-compose exec worker wget -O- http://control-plane:8080/api/v1/health

# View network configuration
docker-compose exec control-plane ip addr
docker-compose exec worker ip addr

# DNS resolution
docker-compose exec worker nslookup control-plane
```

### Resource Usage

```bash
# View resource consumption
docker stats processor-control-plane processor-worker

# Disk usage
docker system df
```

### Attach to Running Container

```bash
# Attach to control-plane (see stdout)
docker attach processor-control-plane

# Detach: Ctrl+P, Ctrl+Q
```

## Common Issues

### Issue: "port 8080 already in use"

**Cause**: Another service is using port 8080

**Solution**:
```bash
# Find process using port
lsof -i :8080

# Kill process or stop conflicting service
kill <PID>

# Or change port in docker-compose.yml
ports:
  - "8081:8080"  # Map to different host port
```

### Issue: "worker registration complete" but worker not polling

**Cause**: Worker token mismatch or network issue

**Solution**:
```bash
# 1. Check worker logs
docker-compose logs worker

# 2. Verify token match
docker-compose exec control-plane env | grep WORKER_TOKEN
docker-compose exec worker env | grep WORKER_TOKEN

# 3. Test connectivity
docker-compose exec worker wget -O- http://control-plane:8080/api/v1/health
```

### Issue: "database is locked"

**Cause**: SQLite WAL mode issue or multiple writers

**Solution**:
```bash
# 1. Stop all services
make dev-down

# 2. Remove lock files
docker run --rm -v processor_processor-data:/data alpine \
  sh -c "rm -f /data/processor.db-wal /data/processor.db-shm"

# 3. Restart
make dev-up
```

### Issue: Services start but immediately exit

**Cause**: Binary compilation error or missing dependencies

**Solution**:
```bash
# 1. Check logs for error
docker-compose logs control-plane

# 2. Rebuild with no cache
docker-compose build --no-cache control-plane

# 3. Test binary locally
cd control-plane && make build && ./bin/server
```

### Issue: Health check failing

**Cause**: Service not ready or crashed

**Solution**:
```bash
# 1. Check control-plane logs
docker-compose logs control-plane

# 2. Exec into container
docker-compose exec control-plane sh

# 3. Test health endpoint internally
wget -O- http://localhost:8080/api/v1/health

# 4. Check process
ps aux | grep server
```

### Issue: "standard_init_linux.go: exec user process caused: no such file or directory"

**Cause**: Binary built for wrong architecture (e.g., macOS binary in Linux container)

**Solution**:
```bash
# Ensure GOOS=linux in Dockerfile
# Check Dockerfile build command:
RUN CGO_ENABLED=0 GOOS=linux go build -o worker ./cmd/worker
```

## Testing

### Manual Testing

```bash
# 1. Start services
make dev-up

# 2. Wait for services to be ready
sleep 10

# 3. Test health endpoint
curl http://localhost:8080/api/v1/health

# 4. Create admin API key (if needed)
docker-compose exec control-plane sh -c "cd /app && ./server -create-api-key"

# 5. Submit test job
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "job_type": "echo_metadata",
    "input_metadata": {"message": "Docker test"},
    "priority_weight": 10
  }'

# 6. Check worker logs
docker-compose logs -f worker
```

### Smoke Tests

```bash
# Run automated smoke tests
make smoke-test

# This runs the smoke-test service which:
# - Waits for control-plane to be healthy
# - Runs basic API tests
# - Exits with status code
```

### Integration Tests

```bash
# Run full integration test suite
make integration-test

# This requires services to be running
# Tests full job lifecycle
```

### Load Testing

```bash
# Simple load test with curl
for i in {1..100}; do
  curl -X POST http://localhost:8080/api/v1/jobs \
    -H "Authorization: Bearer YOUR_API_KEY" \
    -H "Content-Type: application/json" \
    -d '{"job_type":"noop"}' &
done
wait

# Monitor resource usage
docker stats
```

### Cleanup After Testing

```bash
# Stop and remove containers
make dev-down

# Remove volumes (reset database)
docker volume rm processor_processor-data

# Remove images
docker-compose down --rmi all

# Full cleanup
docker-compose down -v --rmi all
```

## Advanced Topics

### Multi-Worker Setup

```bash
# Edit docker-compose.yml to add more workers
worker-2:
  build:
    context: ./data-plane
  environment:
    - WORKER_ID=worker_docker_2
    - WORKER_NAME=docker-worker-2
    # ... other env vars

# Remember to register each worker in bootstrap
```

### Production-Like Environment

```bash
# Use production secrets
export SESSION_SECRET=$(openssl rand -hex 32)
export WORKER_TOKEN=$(openssl rand -hex 32)

# Update docker-compose.yml with env vars
environment:
  - SESSION_SECRET=${SESSION_SECRET}
  - WORKER_TOKEN=${WORKER_TOKEN}
```

### Docker Compose Profiles

```bash
# Start with testing profile
docker-compose --profile testing up

# This includes smoke-test service
```

### Remote Debugging

```bash
# Expose delve debugger port (add to docker-compose.yml)
ports:
  - "8080:8080"
  - "2345:2345"  # Delve debugger

# Build with debug symbols
RUN go build -gcflags="all=-N -l" -o server ./cmd/server
```

## Summary

Docker Compose provides a complete local development environment for Processor:

- **Isolated**: Services run in containers with controlled networking
- **Reproducible**: Same environment across all development machines
- **Fast**: Quick start with `make dev-up`
- **Debuggable**: Easy access to logs, database, and container shells
- **Testable**: Built-in smoke tests and integration test support

For production deployment, see `docs/deployment.md`.
