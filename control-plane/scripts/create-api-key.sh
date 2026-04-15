#!/bin/bash
# Helper script to create an API key for a user

set -e

# Check if user email is provided
if [ -z "$1" ]; then
    echo "Usage: $0 <user-email> [key-name]"
    echo "Example: $0 admin@example.com 'My API Key'"
    exit 1
fi

USER_EMAIL="$1"
KEY_NAME="${2:-Default API Key}"

# Get database path from environment or use default
DB_PATH="${DATABASE_PATH:-./processor.db}"

if [ ! -f "$DB_PATH" ]; then
    echo "Error: Database not found at $DB_PATH"
    echo "Run 'make migrate' first to create the database"
    exit 1
fi

# Get user ID
USER_ID=$(sqlite3 "$DB_PATH" "SELECT id FROM users WHERE email = '$USER_EMAIL' LIMIT 1;")

if [ -z "$USER_ID" ]; then
    echo "Error: User with email '$USER_EMAIL' not found"
    echo ""
    echo "Available users:"
    sqlite3 "$DB_PATH" "SELECT email, is_admin FROM users;"
    exit 1
fi

# Generate API key components
KEY_PREFIX="pk_$(openssl rand -hex 8)"
KEY_SECRET=$(openssl rand -hex 16)
FULL_KEY="${KEY_PREFIX}_${KEY_SECRET}"

# Hash the full key with bcrypt equivalent (using openssl for simplicity)
# Note: This is a simplified version. In production, use proper bcrypt
KEY_HASH=$(echo -n "$FULL_KEY" | openssl dgst -sha256 | cut -d' ' -f2)

# Generate unique ID
KEY_ID="key_$(openssl rand -hex 12)"

# Get current timestamp
CREATED_AT=$(date -u +"%Y-%m-%d %H:%M:%S")

# Insert into database
sqlite3 "$DB_PATH" <<EOF
INSERT INTO api_keys (id, user_id, name, key_prefix, key_hash, created_at)
VALUES ('$KEY_ID', '$USER_ID', '$KEY_NAME', '$KEY_PREFIX', '$KEY_HASH', '$CREATED_AT');
EOF

echo "✅ API Key created successfully!"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🔑 API Key: $FULL_KEY"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "⚠️  IMPORTANT: Save this key now! It won't be shown again."
echo ""
echo "Usage:"
echo "  curl -H \"Authorization: Bearer $FULL_KEY\" \\"
echo "       http://localhost:8080/api/v1/me"
echo ""
