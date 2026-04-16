#!/bin/bash
set -e

# Run local worker (Docker) connected to VPS control-plane

VPS_IP="66.179.241.224"
VPS_USER="root"
CONTROL_PLANE_URL="http://$VPS_IP:8080"

echo "🔧 Local Worker → VPS Control Plane"
echo "===================================="
echo ""
echo "This will run a local worker in Docker that connects to:"
echo "  Control Plane: $CONTROL_PLANE_URL"
echo ""

# ============================================================================
# STEP 1: Get worker token from VPS
# ============================================================================
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "1️⃣  Fetching worker token from VPS..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Password: xOk0n76EY2EmEe"

WORKER_TOKEN=$(ssh "$VPS_USER@$VPS_IP" "cat /opt/processor/WORKER_TOKEN.txt 2>/dev/null" || echo "")

if [ -z "$WORKER_TOKEN" ]; then
    echo "❌ Could not fetch worker token from VPS"
    echo ""
    echo "Please run this manually to get the token:"
    echo "  ssh $VPS_USER@$VPS_IP 'cat /opt/processor/WORKER_TOKEN.txt'"
    echo ""
    read -p "Enter worker token manually: " WORKER_TOKEN

    if [ -z "$WORKER_TOKEN" ]; then
        echo "❌ Worker token is required"
        exit 1
    fi
fi

echo "✅ Worker token retrieved"
echo "   Token: $WORKER_TOKEN"

# ============================================================================
# STEP 2: Check if Ollama is running locally
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "2️⃣  Checking Ollama..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if docker ps --format '{{.Names}}' | grep -q '^ollama$'; then
    echo "✅ Ollama container is running"
    OLLAMA_URL="http://host.docker.internal:11434"
elif curl -s http://localhost:11434/api/tags > /dev/null 2>&1; then
    echo "✅ Ollama is running locally"
    OLLAMA_URL="http://host.docker.internal:11434"
else
    echo "⚠️  Ollama not detected"
    echo ""
    echo "To enable Ollama support, run:"
    echo "  make ollama-setup"
    echo ""
    OLLAMA_URL="http://host.docker.internal:11434"
    read -p "Continue without Ollama? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# ============================================================================
# STEP 3: Stop existing worker if running
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "3️⃣  Stopping existing worker..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if docker ps --format '{{.Names}}' | grep -q '^processor-worker-local$'; then
    echo "Stopping existing worker..."
    docker stop processor-worker-local
    docker rm processor-worker-local
    echo "✅ Existing worker stopped"
else
    echo "No existing worker found"
fi

# ============================================================================
# STEP 4: Build worker image
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "4️⃣  Building worker image..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

docker build -t processor-worker-local ./data-plane

echo "✅ Worker image built"

# ============================================================================
# STEP 5: Run worker container
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "5️⃣  Starting worker container..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

docker run -d \
  --name processor-worker-local \
  --restart unless-stopped \
  -e CONTROL_PLANE_URL="$CONTROL_PLANE_URL" \
  -e WORKER_TOKEN="$WORKER_TOKEN" \
  -e WORKER_ID="worker_local_$(hostname -s)" \
  -e WORKER_NAME="local-mac-worker" \
  -e POLL_INTERVAL="5s" \
  -e WORKER_CAPABILITIES="noop,sleep,echo_metadata,ollama_chat" \
  -e OLLAMA_BASE_URL="$OLLAMA_URL" \
  -e LOG_LEVEL="info" \
  processor-worker-local

echo "✅ Worker container started"

# ============================================================================
# STEP 6: Wait and check logs
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "6️⃣  Checking worker status..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

sleep 3

echo "Recent logs:"
docker logs processor-worker-local --tail 20

echo ""
echo "Checking worker registration..."
sleep 2

# ============================================================================
# FINAL SUMMARY
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎉 WORKER STARTED!"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Worker Details:"
echo "  Name: local-mac-worker"
echo "  ID: worker_local_$(hostname -s)"
echo "  Control Plane: $CONTROL_PLANE_URL"
echo "  Ollama: $OLLAMA_URL"
echo ""
echo "Useful Commands:"
echo "  View logs:       docker logs processor-worker-local -f"
echo "  Stop worker:     docker stop processor-worker-local"
echo "  Restart worker:  docker restart processor-worker-local"
echo "  Remove worker:   docker stop processor-worker-local && docker rm processor-worker-local"
echo ""
echo "Verify worker is registered:"
echo "  curl http://$VPS_IP:8080/api/v1/admin/workers -H 'Authorization: Bearer \$API_KEY'"
echo ""
echo "Next steps:"
echo "  1. Submit a test job to the VPS"
echo "  2. Watch worker logs: docker logs processor-worker-local -f"
echo "  3. Check job execution"
echo ""
