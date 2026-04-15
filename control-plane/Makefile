.PHONY: help build test clean dev migrate lint run stop

help:
	@echo "Available targets:"
	@echo "  make build    - Build the server binary"
	@echo "  make test     - Run tests"
	@echo "  make clean    - Clean build artifacts"
	@echo "  make dev      - Run in development mode"
	@echo "  make migrate  - Run database migrations"
	@echo "  make lint     - Run linters"
	@echo "  make run      - Run the server"
	@echo "  make stop     - Stop the server (kill port 8080)"

build:
	@echo "Building control-plane server..."
	go build -o server ./cmd/server

test:
	@echo "Running tests..."
	go test -v -race -cover ./...

lint:
	@echo "Running linters..."
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed, skipping"; exit 0; }
	golangci-lint run ./...

clean:
	@echo "Cleaning..."
	rm -f server
	rm -f *.db *.db-shm *.db-wal

migrate:
	@echo "Running migrations..."
	@if [ -f .env ]; then \
		export $$(cat .env | grep -v '^#' | xargs) && go run ./cmd/server -migrate; \
	else \
		go run ./cmd/server -migrate; \
	fi

dev: build
	@echo "Starting server in development mode..."
	@if [ -f .env ]; then \
		echo "Loading environment from .env..."; \
		export $$(cat .env | grep -v '^#' | xargs) && ./server; \
	else \
		echo "⚠️  Warning: .env file not found. Using system environment variables."; \
		./server; \
	fi

run: build
	@if [ -f .env ]; then \
		export $$(cat .env | grep -v '^#' | xargs) && ./server; \
	else \
		./server; \
	fi

stop:
	@echo "Stopping control-plane server..."
	@lsof -ti:8080 | xargs kill -9 2>/dev/null && echo "✅ Server stopped" || echo "No server running on port 8080"
