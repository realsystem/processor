#!/bin/bash
set -e

# Test Ollama Integration
# This script tests that Ollama is working and submits a test job

echo "🧪 Testing Ollama Integration"
echo ""

# Check if Ollama is running
echo "1️⃣ Checking if Ollama is running..."
if ! curl -s http://localhost:11434/api/tags > /dev/null 2>&1; then
    echo "❌ Ollama is not running on http://localhost:11434"
    echo ""
    echo "Please start Ollama first:"
    echo "  Option 1 (Docker): docker run -d -p 11434:11434 ollama/ollama"
    echo "  Option 2 (Native): ollama serve"
    echo ""
    echo "Then pull a model:"
    echo "  ollama pull llama3.2"
    exit 1
fi

echo "✅ Ollama is running"
echo ""

# Check available models
echo "2️⃣ Checking available models..."
MODELS=$(curl -s http://localhost:11434/api/tags | grep -o '"name":"[^"]*"' | cut -d'"' -f4 | head -5)
if [ -z "$MODELS" ]; then
    echo "❌ No models found. Please pull a model:"
    echo "  ollama pull llama3.2"
    exit 1
fi

echo "✅ Available models:"
echo "$MODELS" | while read model; do
    echo "   - $model"
done
echo ""

# Check if API key is set
if [ -z "$API_KEY" ]; then
    echo "❌ API_KEY not set"
    echo "Please export your API key:"
    echo "  export API_KEY='pk_your_key_here'"
    exit 1
fi

echo "3️⃣ API key is set"
echo ""

# Check control plane is running
echo "4️⃣ Checking control plane..."
if ! curl -s http://localhost:8080/api/v1/health > /dev/null 2>&1; then
    echo "❌ Control plane is not running"
    echo "Please start it: cd control-plane && make dev"
    exit 1
fi

echo "✅ Control plane is running"
echo ""

# Submit test job
echo "5️⃣ Submitting Ollama test job..."
RESPONSE=$(curl -s -X POST http://localhost:8080/api/v1/jobs \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "job_type": "ollama_chat",
    "input_metadata": {
      "model": "llama3.2",
      "prompt": "Say hello in exactly 5 words."
    },
    "priority_weight": 10
  }')

JOB_ID=$(echo "$RESPONSE" | grep -o '"id":"[^"]*"' | cut -d'"' -f4)

if [ -z "$JOB_ID" ]; then
    echo "❌ Failed to create job"
    echo "$RESPONSE"
    exit 1
fi

echo "✅ Job created: $JOB_ID"
echo ""

# Wait for job to complete
echo "6️⃣ Waiting for job to complete (this may take 30-60 seconds for first run)..."
MAX_WAIT=120
ELAPSED=0
STATUS=""

while [ $ELAPSED -lt $MAX_WAIT ]; do
    sleep 3
    ELAPSED=$((ELAPSED + 3))

    JOB_STATUS=$(curl -s -H "Authorization: Bearer $API_KEY" \
        "http://localhost:8080/api/v1/jobs/$JOB_ID")

    STATUS=$(echo "$JOB_STATUS" | grep -o '"status":"[^"]*"' | cut -d'"' -f4 | head -1)

    if [ "$STATUS" = "succeeded" ]; then
        echo "✅ Job completed in ${ELAPSED}s"
        echo ""
        echo "7️⃣ Job Result:"
        echo "$JOB_STATUS" | python3 -m json.tool 2>/dev/null || echo "$JOB_STATUS"
        echo ""
        echo "🎉 Ollama integration is working!"
        exit 0
    elif [ "$STATUS" = "failed" ]; then
        echo "❌ Job failed"
        echo "$JOB_STATUS" | python3 -m json.tool 2>/dev/null || echo "$JOB_STATUS"
        exit 1
    fi

    echo "   Status: $STATUS (waited ${ELAPSED}s)"
done

echo "❌ Job timeout after ${MAX_WAIT}s"
echo "Last status: $STATUS"
exit 1
