#!/bin/bash
# Smoke test script for quick validation
# Minimal tests to verify system is operational

set -e

CONTROL_PLANE_URL="${CONTROL_PLANE_URL:-http://localhost:8080}"

echo "Running smoke tests against $CONTROL_PLANE_URL"
echo ""

# Test 1: Health endpoint responds
echo -n "Testing health endpoint... "
RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" "$CONTROL_PLANE_URL/api/v1/health")

if [ "$RESPONSE" -eq 200 ]; then
    echo "✓ PASS (HTTP $RESPONSE)"
else
    echo "✗ FAIL (HTTP $RESPONSE)"
    exit 1
fi

# Test 2: Health endpoint returns valid JSON
echo -n "Testing health response format... "
BODY=$(curl -s "$CONTROL_PLANE_URL/api/v1/health")

if echo "$BODY" | grep -q '"success":true'; then
    echo "✓ PASS"
else
    echo "✗ FAIL"
    echo "Response: $BODY"
    exit 1
fi

# Test 3: Authentication is required for protected endpoints
echo -n "Testing authentication requirement... "
RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" "$CONTROL_PLANE_URL/api/v1/me")

if [ "$RESPONSE" -eq 401 ]; then
    echo "✓ PASS (HTTP $RESPONSE - Unauthorized)"
else
    echo "✗ FAIL (Expected 401, got HTTP $RESPONSE)"
    exit 1
fi

echo ""
echo "✅ All smoke tests passed!"
exit 0
