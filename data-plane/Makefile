.PHONY: help build test clean dev lint run stop

help:
	@echo "Available targets:"
	@echo "  make build  - Build the worker binary"
	@echo "  make test   - Run tests"
	@echo "  make clean  - Clean build artifacts"
	@echo "  make dev    - Run in development mode"
	@echo "  make lint   - Run linters"
	@echo "  make run    - Run the worker"
	@echo "  make stop   - Stop the worker"

build:
	@echo "Building data-plane worker..."
	go build -o worker ./cmd/worker

test:
	@echo "Running tests..."
	go test -v -race -cover ./...

lint:
	@echo "Running linters..."
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed, skipping"; exit 0; }
	golangci-lint run ./...

clean:
	@echo "Cleaning..."
	rm -f worker

dev: build
	@echo "Starting worker in development mode..."
	@if [ -f .env ]; then \
		echo "Loading environment from .env..."; \
		export $$(cat .env | grep -v '^#' | xargs) && ./worker; \
	else \
		echo "⚠️  Warning: .env file not found. Using system environment variables."; \
		./worker; \
	fi

run: build
	@if [ -f .env ]; then \
		export $$(cat .env | grep -v '^#' | xargs) && ./worker; \
	else \
		./worker; \
	fi

stop:
	@echo "Stopping worker..."
	@pkill -f "./worker" 2>/dev/null && echo "✅ Worker stopped" || echo "No worker running"
