#!/bin/bash
set -e

# Build Linux binary using Docker (works on macOS M1/M2)

echo "🔨 Building Linux binary using Docker..."

# Use platform flag to force amd64 build
docker run --rm \
  --platform linux/amd64 \
  -v "$(pwd)/control-plane:/workspace" \
  -w /workspace \
  golang:1.21 \
  sh -c "apt-get update -qq && apt-get install -y -qq gcc && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o processor-server-linux ./cmd/server"

# Move binary to project root
mv control-plane/processor-server-linux .

echo "✅ Linux binary created: processor-server-linux"
echo ""
ls -lh processor-server-linux
