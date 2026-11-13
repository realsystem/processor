#!/bin/bash
set -e

# Create API Key on VPS
# Usage: ./vps-create-api-key.sh [email] [name]

EMAIL="${1:-admin@example.com}"
NAME="${2:-Production Key}"
DB="/opt/processor/data/processor.db"

echo "Creating API key for: $EMAIL"

# Get user ID
USER_ID=$(sqlite3 "$DB" "SELECT id FROM users WHERE email='$EMAIL';" 2>/dev/null)

if [ -z "$USER_ID" ]; then
    echo "❌ User not found: $EMAIL"
    echo ""
    echo "Available users:"
    sqlite3 "$DB" "SELECT email, is_admin FROM users;"
    exit 1
fi

# Install Python bcrypt for proper hashing if needed
if ! python3 -c "import bcrypt" 2>/dev/null; then
    echo "Installing bcrypt..."
    apt-get update -qq
    apt-get install -y python3-pip -qq
    pip3 install bcrypt -q
fi

# Generate key
PREFIX=$(openssl rand -hex 8)
SECRET=$(openssl rand -hex 20)
KEY="pk_${PREFIX}_${SECRET}"

# Hash with bcrypt (proper method)
HASH=$(python3 -c "import bcrypt; print(bcrypt.hashpw(b'$KEY', bcrypt.gensalt()).decode())")

# Insert into database
KEY_ID="key_$(openssl rand -hex 12)"
sqlite3 "$DB" <<SQL
INSERT INTO api_keys (id, user_id, name, key_prefix, key_hash, created_at)
VALUES ('$KEY_ID', '$USER_ID', '$NAME', 'pk_$PREFIX', '$HASH', datetime('now'));
SQL

echo ""
echo "✅ API Key created successfully!"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🔑 API Key: $KEY"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "⚠️  IMPORTANT: Save this key now! It won't be shown again."
echo ""
echo "Test it:"
echo "  curl -H 'Authorization: Bearer $KEY' http://localhost:8080/api/v1/me"
echo "  curl -H 'Authorization: Bearer $KEY' http://<your-server-ip>:8080/api/v1/me"
echo ""
