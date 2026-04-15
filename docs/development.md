## Development Guide

Complete guide for developing and contributing to the Processor platform.

## Table of Contents

- [Getting Started](#getting-started)
- [Repository Structure](#repository-structure)
- [Development Workflow](#development-workflow)
- [Local Development](#local-development)
- [Database Migrations](#database-migrations)
- [Adding Features](#adding-features)
- [Debugging](#debugging)
- [Code Style](#code-style)

## Getting Started

### Prerequisites

- **Go 1.21+**: Install from [go.dev](https://go.dev)
- **SQLite 3**: Usually pre-installed on macOS/Linux
- **Make**: Build automation
- **Docker** (optional): For containerized testing
- **Git**: Version control

**macOS**:
```bash
brew install go sqlite make
```

**Linux (Ubuntu/Debian)**:
```bash
sudo apt update
sudo apt install golang-go sqlite3 make build-essential
```

### Clone Repository

```bash
git clone git@github.com:realsystem/processor.git
cd processor
```

### Initial Setup

```bash
# Install dependencies
cd control-plane && go mod download && cd ..
cd data-plane && go mod download && cd ..

# Run database migrations
cd control-plane && make migrate

# Create admin user
go run scripts/create-admin.go

# Create API key
go run scripts/create-api-key.go -email admin@example.com -name "Dev Key"
```

## Repository Structure

```
processor/
├── control-plane/          # Public API server
│   ├── cmd/server/         # Main entry point
│   ├── internal/
│   │   ├── auth/           # Authentication & authorization
│   │   ├── jobs/           # Job queue management
│   │   ├── webhooks/       # Webhook delivery
│   │   ├── email/          # Email notifications
│   │   ├── workers/        # Worker coordination
│   │   ├── httpapi/        # HTTP handlers
│   │   ├── middleware/     # HTTP middleware
│   │   ├── config/         # Configuration
│   │   └── db/             # Database layer
│   ├── scripts/            # Utility scripts
│   ├── Dockerfile
│   ├── Makefile
│   └── .env.example
│
├── data-plane/             # Private worker
│   ├── cmd/worker/         # Main entry point
│   ├── internal/
│   │   ├── executor/       # Job executors
│   │   ├── controlclient/  # API client
│   │   └── config/         # Configuration
│   ├── Dockerfile
│   ├── Makefile
│   └── .env.example
│
├── docs/                   # Documentation
├── tests/                  # Integration tests
├── scripts/                # Repository scripts
├── docker-compose.yml      # Local Docker setup
└── Makefile                # Root build targets
```

## Development Workflow

### Standard Workflow

1. **Create feature branch**:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make changes**:
   - Write code
   - Add tests
   - Update documentation

3. **Run tests**:
   ```bash
   make test
   ```

4. **Run linters**:
   ```bash
   make lint
   ```

5. **Test locally**:
   ```bash
   make dev-up
   bash tests/smoke_test.sh
   make dev-down
   ```

6. **Commit changes**:
   ```bash
   git add .
   git commit -m "feat: add your feature description"
   ```

7. **Push and create PR**:
   ```bash
   git push origin feature/your-feature-name
   ```

### Commit Message Convention

We follow conventional commits:

- `feat:` New feature
- `fix:` Bug fix
- `docs:` Documentation change
- `test:` Adding or updating tests
- `refactor:` Code refactoring
- `perf:` Performance improvement
- `chore:` Build/tooling changes

**Examples**:
```
feat: add rate limiting middleware
fix: resolve session expiration bug
docs: update deployment guide
test: add webhook delivery tests
```

## Local Development

### Option 1: Native (Recommended for Development)

**Terminal 1 - Control Plane**:
```bash
cd control-plane
cp .env.example .env
# Edit .env if needed
make migrate
make dev
```

**Terminal 2 - Worker**:
```bash
cd data-plane
cp .env.example .env
# Edit .env if needed
make dev
```

**Terminal 3 - Testing**:
```bash
# Export API key (from create-api-key.go output)
export API_KEY="pk_your_key_here"

# Test health
curl http://localhost:8080/api/v1/health

# Create job
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"job_type":"echo_metadata","input_metadata":{"test":"value"}}'
```

### Option 2: Docker (Recommended for Testing)

```bash
# Start all services
make dev-up

# View logs
make dev-logs

# Check status
make dev-status

# Stop services
make dev-down
```

See [docker-local.md](docker-local.md) for details.

## Database Migrations

### Creating Migrations

Migrations are in `control-plane/internal/db/migrations/`.

**Create new migration**:
```sql
-- control-plane/internal/db/migrations/002_add_priority_queue.sql

-- Add priority_weight column to jobs table
ALTER TABLE jobs ADD COLUMN priority_weight INTEGER DEFAULT 10;

-- Add index for queue polling
CREATE INDEX idx_jobs_priority_poll ON jobs(status, priority_weight DESC, created_at ASC);
```

### Running Migrations

```bash
# Run migrations
cd control-plane && make migrate

# Or via Go
cd control-plane && go run ./cmd/server -migrate
```

### Migration Guidelines

1. **Forward-only**: Never edit existing migrations
2. **Idempotent**: Safe to run multiple times
3. **Fast**: Avoid blocking operations on large tables
4. **Tested**: Test migration on production-like data

## Adding Features

### Adding a New Job Executor

1. **Define executor** in `data-plane/internal/executor/executor.go`:
   ```go
   type MyExecutor struct {
       Config string
   }
   
   func (e *MyExecutor) Execute(ctx context.Context, jobType string, input map[string]interface{}) (map[string]interface{}, error) {
       // Implementation
       return map[string]interface{}{
           "status": "completed",
           "result": "...",
       }, nil
   }
   ```

2. **Register executor**:
   ```go
   func NewDefaultRegistry(config string) *Registry {
       registry := NewRegistry()
       // ...
       registry.Register("my_job_type", &MyExecutor{Config: config})
       return registry
   }
   ```

3. **Add tests**:
   ```go
   func TestMyExecutor(t *testing.T) {
       executor := &MyExecutor{}
       result, err := executor.Execute(context.Background(), "my_job_type", map[string]interface{}{})
       // assertions...
   }
   ```

4. **Update capabilities** in `.env.example`:
   ```
   WORKER_CAPABILITIES=noop,sleep,echo_metadata,my_job_type
   ```

### Adding an API Endpoint

1. **Define handler** in `control-plane/internal/httpapi/api.go`:
   ```go
   func (a *API) MyEndpoint(w http.ResponseWriter, r *http.Request) {
       // Implementation
       writeJSON(w, http.StatusOK, map[string]interface{}{
           "success": true,
           "data": result,
       })
   }
   ```

2. **Register route** in `control-plane/cmd/server/main.go`:
   ```go
   mux.Handle("/api/v1/my-endpoint", authMiddleware.APIAuth(http.HandlerFunc(api.MyEndpoint)))
   ```

3. **Add tests** in `control-plane/internal/httpapi/api_test.go` (if file exists) or create integration test

4. **Document** in `docs/api.md`

### Adding Configuration

1. **Add field** to `control-plane/internal/config/config.go`:
   ```go
   type Config struct {
       // ...
       MyFeatureEnabled bool
   }
   
   func Load() (*Config, error) {
       return &Config{
           // ...
           MyFeatureEnabled: getEnvBool("MY_FEATURE_ENABLED", false),
       }, nil
   }
   ```

2. **Update `.env.example`**:
   ```
   # My Feature
   MY_FEATURE_ENABLED=false
   ```

3. **Document** in appropriate docs

## Debugging

### Enable Debug Logging

```bash
# In .env
LOG_LEVEL=debug

# Or via environment variable
LOG_LEVEL=debug make dev
```

### Database Inspection

```bash
# Open database
sqlite3 control-plane/processor.db

# Useful queries
.tables                                 # List tables
SELECT * FROM jobs ORDER BY created_at DESC LIMIT 10;
SELECT * FROM users;
SELECT * FROM workers;

# Check queue state
SELECT status, COUNT(*) FROM jobs GROUP BY status;

# Check leases
SELECT id, job_type, status, lease_expires_at FROM jobs WHERE status = 'leased';
```

### Common Issues

**"port already in use"**:
```bash
# Find and kill process
lsof -ti:8080 | xargs kill -9
```

**"database is locked"**:
```bash
# Check for lingering connections
lsof control-plane/processor.db
```

**"Worker not receiving jobs"**:
```bash
# 1. Check worker is registered
sqlite3 control-plane/processor.db "SELECT * FROM workers;"

# 2. Register worker
cd control-plane && go run scripts/register-worker.go

# 3. Check worker capabilities match job types
# 4. Check lease expiration time
```

### Profiling

**CPU profiling**:
```go
import _ "net/http/pprof"

// In main.go
go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()
```

Then visit `http://localhost:6060/debug/pprof/`

**Memory profiling**:
```bash
go tool pprof http://localhost:6060/debug/pprof/heap
```

## Code Style

### Go Guidelines

- Follow [Effective Go](https://go.dev/doc/effective_go)
- Use `gofmt` for formatting (automatic with most editors)
- Run `golangci-lint` before committing
- Keep functions small (< 50 lines)
- Write descriptive variable names
- Add comments for exported functions
- Use table-driven tests

### Error Handling

**Good**:
```go
result, err := someOperation()
if err != nil {
    return fmt.Errorf("operation failed: %w", err)
}
```

**Avoid**:
```go
result, _ := someOperation()  // Ignoring errors
```

### Logging

**Good**:
```go
log.Printf("Processing job %s (type: %s)", jobID, jobType)
```

**Avoid**:
```go
log.Println("processing")  // Not descriptive
fmt.Println("debug")       // Use log package
```

### Testing

- Test files: `*_test.go`
- Test functions: `func TestFunctionName(t *testing.T)`
- Sub-tests: `t.Run("description", func(t *testing.T) { })`
- Helpers: Mark with `t.Helper()`

## Development Tools

### Recommended IDE Setup

**VS Code**:
```json
{
  "go.useLanguageServer": true,
  "go.lintTool": "golangci-lint",
  "go.lintOnSave": "package",
  "editor.formatOnSave": true
}
```

**GoLand**: Works out of the box

### Useful Commands

```bash
# Format code
gofmt -w .

# Check for common issues
go vet ./...

# Lint code
golangci-lint run ./...

# Update dependencies
go get -u ./...
go mod tidy

# Build binaries
make build

# Clean build artifacts
make clean

# Run specific test
go test -run TestRegister ./internal/auth/

# Benchmark
go test -bench=. ./...
```

## Next Steps

- Read [testing.md](testing.md) for testing guidelines
- Read [docker-local.md](docker-local.md) for Docker development
- Read [deployment.md](deployment.md) for production deployment
- Read [api.md](api.md) for API documentation
- Review [security.md](security.md) for security practices
