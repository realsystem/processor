#!/bin/bash
set -e

# Submit test job to VPS control-plane

VPS_IP="66.179.241.224"
VPS_URL="http://$VPS_IP:8080"

# Check if API_KEY is set
if [ -z "$API_KEY" ]; then
    echo "❌ API_KEY environment variable not set"
    echo ""
    echo "Please set your API key:"
    echo "  export API_KEY='pk_your_key_here'"
    echo ""
    echo "Or load from saved file:"
    echo "  source ~/.processor-vps-api-key"
    exit 1
fi

echo "🧪 Testing VPS Job Submission"
echo "=============================="
echo ""
echo "VPS: $VPS_URL"
echo "API Key: ${API_KEY:0:20}..."
echo ""

# Parse command line arguments
JOB_TYPE="${1:-echo_metadata}"

case "$JOB_TYPE" in
    echo|echo_metadata)
        echo "Submitting echo job..."
        JOB_PAYLOAD='{
            "job_type": "echo_metadata",
            "input_metadata": {
                "message": "Test from local Mac!",
                "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
                "source": "local-test-script"
            },
            "priority_weight": 10
        }'
        ;;

    sleep)
        echo "Submitting sleep job..."
        JOB_PAYLOAD='{
            "job_type": "sleep",
            "input_metadata": {
                "duration_seconds": 5
            },
            "priority_weight": 10
        }'
        ;;

    ollama)
        echo "Submitting Ollama job (using plain text format)..."
        JOB_PAYLOAD='{
            "job_type": "ollama_chat",
            "input_metadata": {
                "text": "Write a haiku about cloud computing."
            },
            "priority_weight": 10
        }'
        ;;

    ollama-structured)
        echo "Submitting Ollama job (using structured format)..."
        JOB_PAYLOAD='{
            "job_type": "ollama_chat",
            "input_metadata": {
                "prompt": "Explain Docker in one sentence.",
                "model": "llama3.2"
            },
            "priority_weight": 10
        }'
        ;;

    noop)
        echo "Submitting noop job..."
        JOB_PAYLOAD='{
            "job_type": "noop",
            "input_metadata": {},
            "priority_weight": 10
        }'
        ;;

    *)
        echo "Unknown job type: $JOB_TYPE"
        echo ""
        echo "Usage: $0 [echo|sleep|ollama|ollama-structured|noop]"
        echo ""
        echo "Examples:"
        echo "  $0 echo               # Submit echo job"
        echo "  $0 sleep              # Submit sleep job"
        echo "  $0 ollama             # Submit Ollama LLM job (plain text)"
        echo "  $0 ollama-structured  # Submit Ollama LLM job (structured)"
        echo "  $0 noop               # Submit no-op job"
        exit 1
        ;;
esac

# Submit job
echo ""
echo "Submitting job..."
RESPONSE=$(curl -s -X POST "$VPS_URL/api/v1/jobs" \
    -H "Authorization: Bearer $API_KEY" \
    -H "Content-Type: application/json" \
    -d "$JOB_PAYLOAD")

# Check if successful
if echo "$RESPONSE" | grep -q '"success":true'; then
    JOB_ID=$(echo "$RESPONSE" | grep -o '"id":"[^"]*"' | cut -d'"' -f4)
    echo "✅ Job created: $JOB_ID"
    echo ""
    echo "$RESPONSE" | python3 -m json.tool 2>/dev/null || echo "$RESPONSE"

    # Wait and check status
    echo ""
    echo "Waiting for job to complete (max 60s)..."

    for i in {1..12}; do
        sleep 5
        STATUS_RESPONSE=$(curl -s -H "Authorization: Bearer $API_KEY" "$VPS_URL/api/v1/jobs/$JOB_ID")
        STATUS=$(echo "$STATUS_RESPONSE" | grep -o '"status":"[^"]*"' | head -1 | cut -d'"' -f4)

        echo "[$i] Status: $STATUS"

        if [ "$STATUS" = "succeeded" ] || [ "$STATUS" = "failed" ]; then
            echo ""
            echo "Final result:"
            echo "$STATUS_RESPONSE" | python3 -m json.tool 2>/dev/null || echo "$STATUS_RESPONSE"

            if [ "$STATUS" = "succeeded" ]; then
                echo ""
                echo "🎉 Job completed successfully!"
            else
                echo ""
                echo "❌ Job failed"
            fi
            exit 0
        fi
    done

    echo ""
    echo "⏱️  Job still running after 60s"
    echo "Check status: curl -H 'Authorization: Bearer \$API_KEY' $VPS_URL/api/v1/jobs/$JOB_ID"
else
    echo "❌ Failed to create job"
    echo "$RESPONSE" | python3 -m json.tool 2>/dev/null || echo "$RESPONSE"
    exit 1
fi
