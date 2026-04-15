.PHONY: help build test clean dev lint control-plane data-plane \
        dev-up dev-down dev-logs dev-restart dev-status dev-clean dev-reset \
        smoke-test integration-test test-all \
        migrate seed-dev create-user

help:
	@echo "Processor Monorepo - Available Commands"
	@echo ""
	@echo "Building:"
	@echo "  make build          - Build both control-plane and data-plane binaries"
	@echo "  make control-plane  - Build control-plane only"
	@echo "  make data-plane     - Build data-plane only"
	@echo ""
	@echo "Testing:"
	@echo "  make test           - Run unit tests for both services"
	@echo "  make test-all       - Run unit tests + integration tests"
	@echo "  make integration-test - Run integration tests"
	@echo "  make smoke-test     - Run smoke tests against Docker environment"
	@echo "  make lint           - Run linters for both services"
	@echo ""
	@echo "Local Development (Docker):"
	@echo "  make dev-up         - Start all services in Docker"
	@echo "  make dev-down       - Stop and remove all Docker containers"
	@echo "  make dev-clean      - Stop and remove containers + volumes (clean slate)"
	@echo "  make dev-reset      - Full reset: clean, rebuild, and start fresh"
	@echo "  make dev-logs       - Tail logs from all services"
	@echo "  make dev-restart    - Restart all Docker services"
	@echo "  make dev-status     - Show status of Docker services"
	@echo ""
	@echo "Database:"
	@echo "  make migrate        - Run database migrations"
	@echo "  make seed-dev       - Seed database with dev data"
	@echo ""
	@echo "User Management:"
	@echo "  make create-user    - Create a new user (interactive)"
	@echo ""
	@echo "Utilities:"
	@echo "  make clean          - Clean build artifacts and Docker volumes"
	@echo "  make dev            - Start control-plane locally (use in terminal 1)"
	@echo ""

# Building
build: control-plane data-plane

control-plane:
	@echo "Building control-plane..."
	cd control-plane && make build

data-plane:
	@echo "Building data-plane..."
	cd data-plane && make build

# Testing
test:
	@echo "Running control-plane tests..."
	cd control-plane && make test
	@echo ""
	@echo "Running data-plane tests..."
	cd data-plane && make test

test-all: test integration-test

integration-test:
	@echo "Running integration tests..."
	@if [ -f "tests/integration_test.sh" ]; then \
		bash tests/integration_test.sh; \
	else \
		echo "⚠️  Integration tests not found. Run 'make dev-up' first."; \
	fi

smoke-test:
	@echo "Running smoke tests..."
	docker-compose --profile testing up smoke-test --abort-on-container-exit
	@echo "✅ Smoke tests completed"

# Linting
lint:
	@echo "Linting control-plane..."
	cd control-plane && make lint
	@echo ""
	@echo "Linting data-plane..."
	cd data-plane && make lint

# Docker Development Environment
dev-up:
	@echo "Starting Docker development environment..."
	docker-compose up -d control-plane worker-bootstrap worker
	@echo ""
	@echo "✅ Services started!"
	@echo ""
	@echo "Control Plane: http://localhost:8080"
	@echo "Health Check:  http://localhost:8080/api/v1/health"
	@echo ""
	@echo "View logs:     make dev-logs"
	@echo "Check status:  make dev-status"
	@echo "Stop services: make dev-down"

dev-down:
	@echo "Stopping Docker development environment..."
	docker-compose down
	@echo "✅ Services stopped"

dev-logs:
	@echo "Tailing logs (Ctrl+C to exit)..."
	docker-compose logs -f control-plane worker

dev-restart:
	@echo "Restarting Docker services..."
	docker-compose restart control-plane worker
	@echo "✅ Services restarted"

dev-status:
	@echo "Docker Service Status:"
	@echo ""
	docker-compose ps

dev-clean:
	@echo "Cleaning Docker environment (removing volumes)..."
	docker-compose down -v
	@echo "✅ Cleaned! Volumes removed."

dev-reset:
	@echo "Resetting Docker environment (clean slate)..."
	docker-compose down -v
	@echo "Rebuilding images..."
	docker-compose build control-plane
	@echo "Starting services..."
	docker-compose up -d control-plane worker-bootstrap worker
	@echo ""
	@echo "Waiting for services to be healthy..."
	@sleep 5
	@echo "Restarting control-plane to create admin user..."
	docker-compose restart control-plane
	@sleep 3
	@echo ""
	@echo "✅ Reset complete!"
	@echo ""
	@echo "Control Plane: http://localhost:8080"
	@echo "Login:         admin@example.com / admin123"
	@echo ""
	@echo "View logs:     make dev-logs"
	@echo "Check status:  make dev-status"

# Database operations
migrate:
	@echo "Running database migrations..."
	cd control-plane && make migrate

seed-dev:
	@echo "Seeding development database..."
	@if [ -f "scripts/seed-dev.sh" ]; then \
		bash scripts/seed-dev.sh; \
	else \
		echo "Creating admin user..."; \
		cd control-plane && go run scripts/create-admin.go; \
	fi

create-user:
	@echo "Create New User"
	@echo "==============="
	@echo ""
	@read -p "Email: " email; \
	read -sp "Password: " password; \
	echo ""; \
	read -p "Admin? (y/N): " admin; \
	is_admin=0; \
	if [ "$$admin" = "y" ] || [ "$$admin" = "Y" ]; then \
		is_admin=1; \
	fi; \
	bash scripts/create-user.sh "$$email" "$$password" "$$is_admin"

# Clean
clean:
	@echo "Cleaning build artifacts..."
	cd control-plane && make clean
	cd data-plane && make clean
	@echo ""
	@echo "⚠️  To also remove Docker volumes, run: docker-compose down -v"

# Local development (non-Docker)
dev:
	@echo "Starting local development environment..."
	@echo "This will start the control-plane in the foreground."
	@echo "Run 'cd data-plane && make dev' in another terminal to start the worker."
	cd control-plane && make dev
