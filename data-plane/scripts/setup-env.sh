#!/bin/bash
# Helper script to set up data-plane .env file

set -e

echo "Setting up data-plane worker environment..."
echo ""

# Check if .env already exists
if [ -f .env ]; then
    echo "⚠️  .env file already exists"
    read -p "Do you want to overwrite it? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Aborted"
        exit 0
    fi
fi

# Default values
CONTROL_PLANE_URL="${CONTROL_PLANE_URL:-http://localhost:8080}"
WORKER_TOKEN="${WORKER_TOKEN:-dev-worker-token-change-in-production}"
WORKER_ID="${WORKER_ID:-worker_1}"
WORKER_NAME="${WORKER_NAME:-local-worker}"
POLL_INTERVAL="${POLL_INTERVAL:-5s}"
WORKER_CAPABILITIES="${WORKER_CAPABILITIES:-noop,sleep,echo_metadata,ollama_chat}"
OLLAMA_BASE_URL="${OLLAMA_BASE_URL:-http://localhost:11434}"
LOG_LEVEL="${LOG_LEVEL:-info}"

# Create .env file
cat > .env <<EOF
# Control Plane URL
CONTROL_PLANE_URL=$CONTROL_PLANE_URL

# Worker Token (must match control plane WORKER_TOKEN)
WORKER_TOKEN=$WORKER_TOKEN

# Worker Configuration
WORKER_ID=$WORKER_ID
WORKER_NAME=$WORKER_NAME
POLL_INTERVAL=$POLL_INTERVAL

# Capabilities (comma-separated)
WORKER_CAPABILITIES=$WORKER_CAPABILITIES

# Ollama Configuration (optional)
OLLAMA_BASE_URL=$OLLAMA_BASE_URL

# Logging
LOG_LEVEL=$LOG_LEVEL
EOF

echo "✅ .env file created successfully!"
echo ""
echo "Current configuration:"
echo "  Control Plane URL: $CONTROL_PLANE_URL"
echo "  Worker ID: $WORKER_ID"
echo "  Worker Token: ${WORKER_TOKEN:0:20}..."
echo ""
echo "⚠️  IMPORTANT: Make sure WORKER_TOKEN matches the control-plane's WORKER_TOKEN"
echo ""
echo "You can now start the worker with:"
echo "  make dev"
echo ""
