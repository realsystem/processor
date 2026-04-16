#!/bin/bash
set -e

# Create API key in Docker environment
# Usage: ./scripts/create-api-key-docker.sh [email] [name]

EMAIL="${1:-admin@example.com}"
NAME="${2:-Docker API Key}"

# Check if container is running
if ! docker ps --format '{{.Names}}' | grep -q 'processor-control-plane'; then
    echo "❌ Docker services not running. Start with: make dev-up"
    exit 1
fi

# Check if user exists
USER_ID=$(docker exec processor-control-plane \
    sqlite3 /data/processor.db \
    "SELECT id FROM users WHERE email='$EMAIL';" 2>&1)

if [ -z "$USER_ID" ]; then
    echo "❌ User $EMAIL not found in Docker database."
    echo ""
    echo "Available users:"
    docker exec processor-control-plane \
        sqlite3 /data/processor.db \
        "SELECT email FROM users;"
    exit 1
fi

# Generate random key (24 bytes = 48 hex chars)
# Format: pk_{8 hex}_{40 hex}
PREFIX=$(openssl rand -hex 8)
SECRET=$(openssl rand -hex 20)
FULL_KEY="pk_${PREFIX}_${SECRET}"

# Hash the full key for storage (bcrypt equivalent using sha256)
# Note: This is simplified - bcrypt would be better but requires more tools
KEY_HASH=$(echo -n "$FULL_KEY" | openssl dgst -sha256 | awk '{print $2}')

# Create API key in database
docker exec processor-control-plane sqlite3 /data/processor.db <<SQL
INSERT INTO api_keys (user_id, name, key_hash, created_at)
VALUES ('$USER_ID', '$NAME', '$KEY_HASH', datetime('now'));
SQL

echo "✅ API Key created successfully!"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🔑 API Key: $FULL_KEY"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "⚠️  IMPORTANT: Save this key now! It won't be shown again."
echo ""
echo "To use it:"
echo "  export API_KEY=\"$FULL_KEY\""
echo "  curl -H \"Authorization: Bearer \$API_KEY\" http://localhost:8080/api/v1/me"
echo ""
