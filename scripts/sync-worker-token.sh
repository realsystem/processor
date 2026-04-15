#!/bin/bash
# Sync WORKER_TOKEN between control-plane and data-plane

set -e

echo "Checking WORKER_TOKEN configuration..."
echo ""

# Check control-plane token
if [ ! -f control-plane/.env ]; then
    echo "❌ control-plane/.env not found"
    echo "   Create it from: cp control-plane/.env.example control-plane/.env"
    exit 1
fi

# Check data-plane token
if [ ! -f data-plane/.env ]; then
    echo "❌ data-plane/.env not found"
    echo "   Create it from: cp data-plane/.env.example data-plane/.env"
    exit 1
fi

# Extract tokens
CONTROL_TOKEN=$(grep "^WORKER_TOKEN=" control-plane/.env | cut -d'=' -f2- | tr -d '"' | tr -d "'")
DATA_TOKEN=$(grep "^WORKER_TOKEN=" data-plane/.env | cut -d'=' -f2- | tr -d '"' | tr -d "'")

echo "Control Plane Token: ${CONTROL_TOKEN:0:30}..."
echo "Data Plane Token:    ${DATA_TOKEN:0:30}..."
echo ""

if [ "$CONTROL_TOKEN" = "$DATA_TOKEN" ]; then
    echo "✅ WORKER_TOKENs match!"
    exit 0
else
    echo "❌ WORKER_TOKENs do NOT match!"
    echo ""
    echo "To fix this, update one of the .env files to match:"
    echo ""
    echo "Option 1: Update data-plane to match control-plane"
    echo "  sed -i.bak 's/^WORKER_TOKEN=.*/WORKER_TOKEN=$CONTROL_TOKEN/' data-plane/.env"
    echo ""
    echo "Option 2: Update control-plane to match data-plane"
    echo "  sed -i.bak 's/^WORKER_TOKEN=.*/WORKER_TOKEN=$DATA_TOKEN/' control-plane/.env"
    echo ""
    echo "Option 3: Set both to a new secure token"
    echo "  NEW_TOKEN=\$(openssl rand -hex 32)"
    echo "  sed -i.bak \"s/^WORKER_TOKEN=.*/WORKER_TOKEN=\$NEW_TOKEN/\" control-plane/.env"
    echo "  sed -i.bak \"s/^WORKER_TOKEN=.*/WORKER_TOKEN=\$NEW_TOKEN/\" data-plane/.env"
    exit 1
fi
