#!/bin/bash
set -e

# Register Windows GPU worker on VPS

VPS_IP="66.179.241.224"
WORKER_ID="worker_windows_gpu"
WORKER_NAME="windows-gpu-worker"
CAPABILITIES="noop,sleep,echo_metadata,ollama_chat"

echo "🔧 Registering Windows GPU Worker on VPS"
echo "=========================================="
echo ""
echo "VPS: $VPS_IP"
echo "Worker ID: $WORKER_ID"
echo "Capabilities: $CAPABILITIES"
echo ""

# SSH to VPS and register worker
ssh root@$VPS_IP << EOF
cd /opt/processor

# Check if worker already exists
if sqlite3 data/processor.db "SELECT id FROM workers WHERE id='$WORKER_ID';" | grep -q "$WORKER_ID"; then
    echo "⚠️  Worker already exists, updating..."
    sqlite3 data/processor.db "UPDATE workers SET name='$WORKER_NAME', capabilities='$CAPABILITIES', status='active', updated_at=CURRENT_TIMESTAMP WHERE id='$WORKER_ID';"
else
    echo "➕ Creating new worker..."
    sqlite3 data/processor.db "INSERT INTO workers (id, name, capabilities, status, created_at, updated_at) VALUES ('$WORKER_ID', '$WORKER_NAME', '$CAPABILITIES', 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);"
fi

# Verify registration
echo ""
echo "✅ Worker registered successfully!"
echo ""
sqlite3 data/processor.db "SELECT id, name, capabilities, status FROM workers WHERE id='$WORKER_ID';"
EOF

echo ""
echo "🎉 Windows worker is ready!"
echo ""
echo "On your Windows PC, run:"
echo "  .\\scripts\\setup-windows-worker.ps1"
echo ""
echo "Or manually start with:"
echo "  docker run -d --name processor-worker-windows --env-file .env.windows processor-worker:latest"
