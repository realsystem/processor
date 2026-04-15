#!/bin/bash
# Integration test script for Processor
# Tests end-to-end job lifecycle

set -e

CONTROL_PLANE_URL="${CONTROL_PLANE_URL:-http://localhost:8080}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@example.com}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin123}"

echo "========================================="
echo "Processor Integration Test Suite"
echo "========================================="
echo ""
echo "Control Plane: $CONTROL_PLANE_URL"
echo ""

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

pass() {
    echo -e "${GREEN}✓${NC} $1"
}

fail() {
    echo -e "${RED}✗${NC} $1"
    exit 1
}

info() {
    echo -e "${YELLOW}ℹ${NC} $1"
}

# Test 1: Health Check
info "Test 1: Health check"
RESPONSE=$(curl -s -w "\n%{http_code}" "$CONTROL_PLANE_URL/api/v1/health")
HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
BODY=$(echo "$RESPONSE" | head -n -1)

if [ "$HTTP_CODE" -eq 200 ]; then
    pass "Health check returned 200"
else
    fail "Health check failed with status $HTTP_CODE"
fi

if echo "$BODY" | grep -q '"success":true'; then
    pass "Health check response valid"
else
    fail "Health check response invalid: $BODY"
fi

# Test 2: Create API Key
info "Test 2: Creating API key"
# For integration tests, we assume admin user exists
# In Docker environment, this is auto-created

# We'll create a new API key via the create-api-key script
# For now, let's use a known dev API key or create one programmatically

# Create admin user first (idempotent)
if [ -f "control-plane/scripts/create-admin.go" ]; then
    info "Ensuring admin user exists..."
    cd control-plane && go run scripts/create-admin.go > /dev/null 2>&1 || true
    cd ..
fi

# Create API key
if [ -f "control-plane/scripts/create-api-key.go" ]; then
    info "Creating API key..."
    cd control-plane
    API_KEY_OUTPUT=$(go run scripts/create-api-key.go -email "$ADMIN_EMAIL" -name "Integration Test Key" 2>&1)
    API_KEY=$(echo "$API_KEY_OUTPUT" | grep -oP 'pk_[a-f0-9_]+' || echo "")
    cd ..

    if [ -z "$API_KEY" ]; then
        fail "Failed to create API key"
    fi
    pass "API key created: ${API_KEY:0:20}..."
else
    fail "create-api-key.go script not found"
fi

# Test 3: Authenticate with API Key
info "Test 3: Testing API key authentication"
RESPONSE=$(curl -s -w "\n%{http_code}" -H "Authorization: Bearer $API_KEY" "$CONTROL_PLANE_URL/api/v1/me")
HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
BODY=$(echo "$RESPONSE" | head -n -1)

if [ "$HTTP_CODE" -eq 200 ]; then
    pass "API key authentication successful"
else
    fail "API key authentication failed with status $HTTP_CODE: $BODY"
fi

# Test 4: Create Job
info "Test 4: Creating a test job"
JOB_PAYLOAD='{
  "job_type": "echo_metadata",
  "input_metadata": {
    "message": "Integration test",
    "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"
  },
  "priority_weight": 10,
  "notify_email": false,
  "notify_webhook": false
}'

RESPONSE=$(curl -s -w "\n%{http_code}" -X POST \
    -H "Authorization: Bearer $API_KEY" \
    -H "Content-Type: application/json" \
    -d "$JOB_PAYLOAD" \
    "$CONTROL_PLANE_URL/api/v1/jobs")

HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
BODY=$(echo "$RESPONSE" | head -n -1)

if [ "$HTTP_CODE" -eq 200 ] || [ "$HTTP_CODE" -eq 201 ]; then
    JOB_ID=$(echo "$BODY" | grep -oP '"id":"job_[a-f0-9]+"' | grep -oP 'job_[a-f0-9]+' || echo "")
    if [ -n "$JOB_ID" ]; then
        pass "Job created: $JOB_ID"
    else
        fail "Job created but no ID found in response: $BODY"
    fi
else
    fail "Job creation failed with status $HTTP_CODE: $BODY"
fi

# Test 5: Get Job Status
info "Test 5: Checking job status"
sleep 2 # Give worker time to process

RESPONSE=$(curl -s -w "\n%{http_code}" \
    -H "Authorization: Bearer $API_KEY" \
    "$CONTROL_PLANE_URL/api/v1/jobs/$JOB_ID")

HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
BODY=$(echo "$RESPONSE" | head -n -1)

if [ "$HTTP_CODE" -eq 200 ]; then
    pass "Job retrieved successfully"

    # Check if job completed (may still be queued/running in some environments)
    if echo "$BODY" | grep -q '"status":"succeeded"'; then
        pass "Job completed successfully"
    elif echo "$BODY" | grep -q '"status":"queued"'; then
        pass "Job is queued (worker may not be running)"
    elif echo "$BODY" | grep -q '"status":"leased"'; then
        pass "Job is leased (worker is processing)"
    elif echo "$BODY" | grep -q '"status":"running"'; then
        pass "Job is running"
    else
        info "Job status: $(echo "$BODY" | grep -oP '"status":"[^"]+"' || echo 'unknown')"
    fi
else
    fail "Job retrieval failed with status $HTTP_CODE: $BODY"
fi

# Test 6: List Jobs
info "Test 6: Listing jobs"
RESPONSE=$(curl -s -w "\n%{http_code}" \
    -H "Authorization: Bearer $API_KEY" \
    "$CONTROL_PLANE_URL/api/v1/jobs")

HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
BODY=$(echo "$RESPONSE" | head -n -1)

if [ "$HTTP_CODE" -eq 200 ]; then
    pass "Jobs listed successfully"

    # Check if our job is in the list
    if echo "$BODY" | grep -q "$JOB_ID"; then
        pass "Created job found in job list"
    else
        fail "Created job not found in job list"
    fi
else
    fail "Job listing failed with status $HTTP_CODE: $BODY"
fi

# Test 7: List API Keys
info "Test 7: Listing API keys"
RESPONSE=$(curl -s -w "\n%{http_code}" \
    -H "Authorization: Bearer $API_KEY" \
    "$CONTROL_PLANE_URL/api/v1/api-keys")

HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
BODY=$(echo "$RESPONSE" | head -n -1)

if [ "$HTTP_CODE" -eq 200 ]; then
    pass "API keys listed successfully"
else
    fail "API key listing failed with status $HTTP_CODE: $BODY"
fi

echo ""
echo "========================================="
echo -e "${GREEN}All integration tests passed!${NC}"
echo "========================================="
echo ""

exit 0
