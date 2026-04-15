.PHONY: help build test clean dev lint control-plane data-plane

help:
	@echo "Available targets:"
	@echo "  make build         - Build both control-plane and data-plane"
	@echo "  make test          - Run all tests"
	@echo "  make lint          - Run linters"
	@echo "  make clean         - Clean build artifacts"
	@echo "  make dev           - Start local development environment"
	@echo "  make control-plane - Build control-plane only"
	@echo "  make data-plane    - Build data-plane only"

build: control-plane data-plane

control-plane:
	@echo "Building control-plane..."
	cd control-plane && make build

data-plane:
	@echo "Building data-plane..."
	cd data-plane && make build

test:
	@echo "Running control-plane tests..."
	cd control-plane && make test
	@echo "Running data-plane tests..."
	cd data-plane && make test

lint:
	@echo "Linting control-plane..."
	cd control-plane && make lint
	@echo "Linting data-plane..."
	cd data-plane && make lint

clean:
	@echo "Cleaning control-plane..."
	cd control-plane && make clean
	@echo "Cleaning data-plane..."
	cd data-plane && make clean

dev:
	@echo "Starting local development environment..."
	@echo "This will start the control-plane in the foreground."
	@echo "Run 'cd data-plane && make dev' in another terminal to start the worker."
	cd control-plane && make dev
