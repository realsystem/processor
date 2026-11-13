## Testing Guide

This document describes the testing strategy and how to run tests for the Processor platform.

## Table of Contents

- [Testing Philosophy](#testing-philosophy)
- [Test Types](#test-types)
- [Running Tests](#running-tests)
- [Writing Tests](#writing-tests)
- [Test Coverage](#test-coverage)
- [Continuous Integration](#continuous-integration)

## Testing Philosophy

The Processor platform follows a comprehensive testing strategy:

1. **Unit Tests**: Test individual components in isolation
2. **Integration Tests**: Test interactions between components
3. **Smoke Tests**: Quick validation that the system is operational
4. **End-to-End Tests**: Validate complete workflows

Our testing goals:
- High confidence in correctness
- Fast feedback during development
- Minimal flakiness
- Easy to understand test failures

## Test Types

### Unit Tests

Unit tests verify individual functions, methods, and components in isolation.

**Location**:
- `control-plane/internal/*/` - `*_test.go` files
- `data-plane/internal/*/` - `*_test.go` files

**Coverage**:
- ✅ Auth service (registration, login, sessions, API keys)
- ✅ Jobs service (creation, polling, leasing, completion, failure)
- ✅ Executors (noop, sleep, echo, ollama stub)
- ⚠️  Webhooks service (partial)
- ⚠️  Email service (partial)

**Run unit tests**:
```bash
# Run all unit tests
make test

# Run control-plane tests only
cd control-plane && make test

# Run data-plane tests only
cd data-plane && make test

# Run with verbose output
cd control-plane && go test -v ./...

# Run with coverage
cd control-plane && go test -cover ./...
cd control-plane && go test -coverprofile=coverage.out ./...
cd control-plane && go tool cover -html=coverage.out
```

### Integration Tests

Integration tests verify end-to-end workflows across components.

**Location**: `tests/integration_test.sh`

**What it tests**:
1. Control plane health check
2. Admin user creation
3. API key creation and authentication
4. Job creation via API
5. Job retrieval and status checking
6. Job listing
7. API key listing

**Prerequisites**:
- Control plane running on `localhost:8080`
- Database initialized with migrations
- Admin user exists (auto-created in Docker)

**Run integration tests**:
```bash
# Using Docker (recommended)
make dev-up
make integration-test

# Against local services
cd control-plane && make dev  # Terminal 1
cd data-plane && make dev     # Terminal 2
bash tests/integration_test.sh # Terminal 3
```

### Smoke Tests

Smoke tests perform minimal validation to ensure the system is operational.

**Location**: `tests/smoke_test.sh`

**What it tests**:
1. Health endpoint responds with 200
2. Health endpoint returns valid JSON
3. Protected endpoints require authentication

**Run smoke tests**:
```bash
# Against Docker environment
make dev-up
bash tests/smoke_test.sh

# Against any environment
CONTROL_PLANE_URL=http://your-server:8080 bash tests/smoke_test.sh
```

### End-to-End Tests

Full workflow tests that validate:
1. Job submission
2. Worker polling and claiming
3. Job execution
4. Result storage
5. Notification delivery

**Manual E2E test**:
```bash
# 1. Start services
make dev-up

# 2. Create API key
cd control-plane
API_KEY=$(go run scripts/create-api-key.go -email admin@example.com -name "Test" | grep -oP 'pk_[a-f0-9_]+')
cd ..

# 3. Submit job
JOB_RESPONSE=$(curl -s -X POST http://localhost:8080/api/v1/jobs \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"job_type":"echo_metadata","input_metadata":{"test":"value"},"priority_weight":10}')

JOB_ID=$(echo $JOB_RESPONSE | grep -oP '"id":"job_[a-f0-9]+"' | cut -d'"' -f4)

# 4. Wait for job to complete
sleep 5

# 5. Check job result
curl -H "Authorization: Bearer $API_KEY" http://localhost:8080/api/v1/jobs/$JOB_ID
```

## Running Tests

### Quick Start

```bash
# Run all unit tests
make test

# Run all tests (unit + integration)
make test-all

# Run smoke test
make smoke-test

# Run integration test
make integration-test
```

### Docker Testing

```bash
# Start test environment
make dev-up

# View logs
make dev-logs

# Run smoke test against Docker
make smoke-test

# Run integration test against Docker
make integration-test

# Stop test environment
make dev-down
```

### Local Testing (without Docker)

```bash
# Terminal 1: Start control plane
cd control-plane
make dev

# Terminal 2: Start worker
cd data-plane
make dev

# Terminal 3: Run tests
make test
bash tests/smoke_test.sh
bash tests/integration_test.sh
```

## Writing Tests

### Unit Test Guidelines

**Good unit test characteristics**:
- Fast (< 100ms per test)
- Isolated (uses in-memory database)
- Deterministic (no flakiness)
- Clear intent (descriptive names)
- Single responsibility (one thing per test)

**Example**:
```go
func TestRegister(t *testing.T) {
    database := setupTestDB(t)
    defer database.Close()
    
    service := NewService(database)
    
    t.Run("successful registration", func(t *testing.T) {
        user, err := service.Register("test@example.com", "password123", false)
        if err != nil {
            t.Fatalf("Register failed: %v", err)
        }
        
        if user.Email != "test@example.com" {
            t.Errorf("Expected email test@example.com, got %s", user.Email)
        }
    })
    
    t.Run("duplicate email", func(t *testing.T) {
        _, err := service.Register("test@example.com", "password456", false)
        if err != ErrDuplicateEmail {
            t.Errorf("Expected ErrDuplicateEmail, got %v", err)
        }
    })
}
```

### Integration Test Guidelines

**Use integration tests for**:
- HTTP API workflows
- Database migrations
- Cross-service interactions
- Authentication flows

**Integration test template**:
```bash
#!/bin/bash
set -e

CONTROL_PLANE_URL="${CONTROL_PLANE_URL:-http://localhost:8080}"

# Test 1: Health check
RESPONSE=$(curl -s -w "\n%{http_code}" "$CONTROL_PLANE_URL/api/v1/health")
HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)

if [ "$HTTP_CODE" -eq 200 ]; then
    echo "✓ Health check passed"
else
    echo "✗ Health check failed"
    exit 1
fi
```

### Test Database Setup

All unit tests use in-memory SQLite for isolation:

```go
func setupTestDB(t *testing.T) *sql.DB {
    t.Helper()
    
    database, err := sql.Open("sqlite3", ":memory:")
    if err != nil {
        t.Fatalf("Failed to open test database: %v", err)
    }
    
    // Run migrations
    if err := db.Migrate(database); err != nil {
        t.Fatalf("Failed to run migrations: %v", err)
    }
    
    return database
}
```

## Test Coverage

### Current Coverage

**Control Plane**:
- `internal/auth`: ✅ Comprehensive (login, register, sessions, API keys)
- `internal/jobs`: ✅ Comprehensive (lifecycle, polling, leasing, retry)
- `internal/webhooks`: ⚠️ Partial (needs delivery retry tests)
- `internal/email`: ⚠️ Partial (needs SMTP provider tests)
- `internal/middleware`: ❌ None (needs auth, logging, rate limit tests)
- `internal/httpapi`: ❌ None (needs HTTP handler tests)

**Data Plane**:
- `internal/executor`: ✅ Comprehensive (all executors, context cancellation, concurrency)
- `internal/controlclient`: ❌ None (needs HTTP client tests)
- `internal/config`: ❌ None (needs config loading tests)

**Target**: 80% line coverage for all packages

### Checking Coverage

```bash
# Generate coverage report
cd control-plane
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Coverage by package
go test -cover ./internal/auth
go test -cover ./internal/jobs
go test -cover ./internal/webhooks

# Summary
go test -cover ./... | grep coverage
```

## Continuous Integration

### GitHub Actions (Recommended)

```yaml
name: Test

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Run unit tests
        run: make test
      
      - name: Start Docker environment
        run: make dev-up
      
      - name: Run smoke tests
        run: bash tests/smoke_test.sh
      
      - name: Run integration tests
        run: bash tests/integration_test.sh
```

### Pre-commit Hooks

Add to `.git/hooks/pre-commit`:
```bash
#!/bin/bash
set -e

echo "Running tests..."
make test

echo "Running linters..."
make lint
```

## Troubleshooting

### Test Failures

**"database is locked"**:
- SQLite WAL mode conflict
- Solution: Each test uses `:memory:` database

**"port already in use"**:
- Previous test left server running
- Solution: `make dev-down` or `pkill -f ./server`

**"connection refused"**:
- Service not running
- Solution: `make dev-up` and wait for health check

**Flaky tests**:
- Check for time-based assumptions
- Add proper synchronization
- Use longer timeouts for CI

### Coverage Gaps

To find untested code:
```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep -v "100.0%"
```

## Best Practices

1. **Test isolation**: Each test should be independent
2. **Fast tests**: Keep unit tests under 100ms
3. **Clear failures**: Error messages should explain what went wrong
4. **Test data**: Use realistic but minimal test data
5. **Setup/teardown**: Use `t.Helper()` for test utilities
6. **Parallel tests**: Use `t.Parallel()` where safe
7. **Table-driven tests**: Use for multiple similar cases

## Next Steps

- [ ] Add HTTP handler tests for all API endpoints
- [ ] Add middleware tests (auth, rate limit)
- [ ] Add webhook delivery retry tests
- [ ] Add email provider integration tests
- [ ] Improve test data factories
- [ ] Add performance/benchmark tests
- [ ] Set up CI/CD pipeline
