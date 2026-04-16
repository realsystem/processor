#!/bin/bash
set -e

# Complete VPS Deployment Script
# This script does everything: build, upload, install, configure, and start

VPS_IP="66.179.241.224"
VPS_USER="root"

echo "🚀 Complete VPS Deployment"
echo "=========================="
echo ""
echo "This will:"
echo "  1. Build Linux binary using Docker"
echo "  2. Upload to VPS"
echo "  3. Install dependencies"
echo "  4. Run migrations"
echo "  5. Create systemd service"
echo "  6. Start the service"
echo "  7. Create API key"
echo ""
echo "VPS: $VPS_USER@$VPS_IP"
echo ""
read -p "Press Enter to continue..."

# ============================================================================
# STEP 1: Build Linux binary
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "1️⃣  Building Linux binary using Docker..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

./scripts/build-linux-binary.sh

echo "✅ Binary built successfully"

# ============================================================================
# STEP 2: Create deployment package
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "2️⃣  Creating deployment package..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Create temp directory
mkdir -p /tmp/processor-vps-deploy/migrations
cp processor-server-linux /tmp/processor-vps-deploy/server
cp -r control-plane/internal/db/migrations/* /tmp/processor-vps-deploy/migrations/
cp -r control-plane/templates /tmp/processor-vps-deploy/
cp -r control-plane/static /tmp/processor-vps-deploy/
cp scripts/vps-create-api-key.sh /tmp/processor-vps-deploy/create-key.sh

# Verify migrations were copied
echo "Verifying migrations..."
ls -la /tmp/processor-vps-deploy/migrations/

# Create installation script
cat > /tmp/processor-vps-deploy/install.sh <<'INSTALL'
#!/bin/bash
set -e

echo "Installing Processor Control Plane on VPS..."

# Stop service if running
if systemctl is-active --quiet processor-control-plane 2>/dev/null; then
    echo "🛑 Stopping existing service..."
    systemctl stop processor-control-plane
fi

# Install dependencies
echo "📦 Installing dependencies..."
apt-get update -qq
apt-get install -y sqlite3 ca-certificates python3-pip -qq
pip3 install bcrypt -q 2>/dev/null || true

# Create directory structure
echo "📁 Creating directories..."
mkdir -p /opt/processor/{bin,data,migrations,templates,static}

# Install files
echo "📝 Installing files..."
cp server /opt/processor/bin/server
chmod +x /opt/processor/bin/server

# Copy migrations, templates, static
cp -r migrations/* /opt/processor/migrations/ 2>/dev/null || echo "No migrations to copy"
cp -r templates/* /opt/processor/templates/ 2>/dev/null || echo "No templates to copy"
cp -r static/* /opt/processor/static/ 2>/dev/null || echo "No static files to copy"

# Install API key creation script
cp create-key.sh /opt/processor/
chmod +x /opt/processor/create-key.sh

# Generate secrets (only if .env doesn't exist - preserve existing config)
if [ ! -f /opt/processor/.env ]; then
    echo "🔐 Generating secrets..."
    SESSION_SECRET=$(openssl rand -hex 32)
    WORKER_TOKEN=$(openssl rand -hex 32)

    echo "⚙️  Creating configuration..."
    cat > /opt/processor/.env <<EOF
DATABASE_PATH=/opt/processor/data/processor.db
SESSION_SECRET=$SESSION_SECRET
WORKER_TOKEN=$WORKER_TOKEN
HTTP_PORT=8080
ENV=production
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=admin123
LOG_LEVEL=info
LEASE_DURATION=5m
EOF

    # Save worker token for reference
    echo "$WORKER_TOKEN" > /opt/processor/WORKER_TOKEN.txt
    chmod 600 /opt/processor/WORKER_TOKEN.txt
    echo "✅ Configuration created"
else
    echo "ℹ️  Using existing configuration"
fi

# CRITICAL: Ensure migrations directory exists and has files
echo "🔍 Checking migrations..."
if [ ! -d "/opt/processor/migrations" ] || [ -z "$(ls -A /opt/processor/migrations)" ]; then
    echo "❌ ERROR: No migration files found!"
    ls -la /opt/processor/migrations/
    exit 1
fi

echo "   Found migrations:"
ls -1 /opt/processor/migrations/

# Delete existing database if it has no tables (corrupted state)
if [ -f "/opt/processor/data/processor.db" ]; then
    TABLE_COUNT=$(sqlite3 /opt/processor/data/processor.db "SELECT COUNT(*) FROM sqlite_master WHERE type='table';" 2>/dev/null || echo "0")
    if [ "$TABLE_COUNT" = "0" ]; then
        echo "⚠️  Removing empty database..."
        rm -f /opt/processor/data/processor.db*
    fi
fi

# Run migrations with environment variables
echo "🗄️  Running database migrations..."
cd /opt/processor

# Load environment and run migrations
set -a
source /opt/processor/.env
set +a

./bin/server -migrate 2>&1 | tee /tmp/migration.log

# Check if migrations actually ran
if grep -qi "error\|failed" /tmp/migration.log; then
    echo "❌ ERROR: Migrations failed!"
    cat /tmp/migration.log
    exit 1
fi

echo "✅ Migrations completed"

# Verify tables were created
echo "🔍 Verifying database..."
TABLE_COUNT=$(sqlite3 /opt/processor/data/processor.db "SELECT COUNT(*) FROM sqlite_master WHERE type='table';" 2>/dev/null || echo "0")
echo "   Found $TABLE_COUNT tables in database"

if [ "$TABLE_COUNT" -lt "5" ]; then
    echo "❌ ERROR: Not enough tables created!"
    echo "   Tables found:"
    sqlite3 /opt/processor/data/processor.db ".tables"
    exit 1
fi

echo "   Tables:"
sqlite3 /opt/processor/data/processor.db ".tables"

# Create systemd service
echo "🔧 Creating systemd service..."
cat > /etc/systemd/system/processor-control-plane.service <<'SERVICE'
[Unit]
Description=Processor Control Plane
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/processor
EnvironmentFile=/opt/processor/.env
ExecStart=/opt/processor/bin/server
Restart=always
RestartSec=10
NoNewPrivileges=true
PrivateTmp=true
StandardOutput=journal
StandardError=journal
SyslogIdentifier=processor

[Install]
WantedBy=multi-user.target
SERVICE

# Configure firewall
echo "🔥 Configuring firewall..."
ufw allow 8080/tcp 2>/dev/null || echo "UFW not available, skipping firewall config"

# Enable and start service
echo "🚀 Starting service..."
systemctl daemon-reload
systemctl enable processor-control-plane
systemctl start processor-control-plane

# Wait for service to start and create admin user
sleep 5

# Check status
echo ""
echo "📊 Service Status:"
systemctl status processor-control-plane --no-pager -l | head -20

echo ""
echo "🧪 Testing endpoint..."
sleep 2
curl -s http://localhost:8080/api/v1/health | python3 -m json.tool 2>/dev/null || echo "⚠️  Service not responding yet"

# Check if admin user was created
echo ""
echo "👤 Checking admin user..."
USER_COUNT=$(sqlite3 /opt/processor/data/processor.db "SELECT COUNT(*) FROM users WHERE email='admin@example.com';" 2>/dev/null || echo "0")
if [ "$USER_COUNT" = "1" ]; then
    echo "✅ Admin user exists"
else
    echo "⚠️  Admin user not found (check logs)"
fi

echo ""
echo "✅ Installation complete!"
INSTALL

chmod +x /tmp/processor-vps-deploy/install.sh

# Create tarball
cd /tmp/processor-vps-deploy
tar czf ../processor-vps-deploy.tar.gz .
cd -

echo "✅ Deployment package created"

# ============================================================================
# STEP 3: Upload to VPS
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "3️⃣  Uploading to VPS..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

scp /tmp/processor-vps-deploy.tar.gz "$VPS_USER@$VPS_IP:/tmp/"

echo "✅ Upload complete"

# ============================================================================
# STEP 4: Install on VPS
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "4️⃣  Installing on VPS..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

ssh "$VPS_USER@$VPS_IP" "cd /tmp && tar xzf processor-vps-deploy.tar.gz && bash install.sh"

# ============================================================================
# STEP 5: Create API Key
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "5️⃣  Creating API Key..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

ssh "$VPS_USER@$VPS_IP" "/opt/processor/create-key.sh admin@example.com 'Production Key'" | tee /tmp/api-key-output.txt

# Extract API key from output
API_KEY=$(grep "🔑.*pk_" /tmp/api-key-output.txt | grep -o "pk_[a-f0-9_]*" || echo "")

# ============================================================================
# STEP 6: Test deployment
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "6️⃣  Testing deployment..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

sleep 3

echo "Testing health endpoint..."
curl -s "http://$VPS_IP:8080/api/v1/health" | python3 -m json.tool 2>/dev/null || echo "⚠️  Endpoint not accessible yet"

if [ -n "$API_KEY" ]; then
    echo ""
    echo "Testing authenticated endpoint..."
    curl -s -H "Authorization: Bearer $API_KEY" "http://$VPS_IP:8080/api/v1/me" | python3 -m json.tool 2>/dev/null || echo "⚠️  Auth endpoint not ready yet"
fi

# ============================================================================
# STEP 7: Cleanup
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🧹 Cleaning up..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

rm -f processor-server-linux
rm -rf /tmp/processor-vps-deploy
rm -f /tmp/processor-vps-deploy.tar.gz
rm -f /tmp/api-key-output.txt

echo "✅ Cleanup complete"

# ============================================================================
# FINAL SUMMARY
# ============================================================================
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎉 DEPLOYMENT SUCCESSFUL!"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Control Plane URL: http://$VPS_IP:8080"
echo ""
echo "Web UI Login:"
echo "  URL: http://$VPS_IP:8080"
echo "  Email: admin@example.com"
echo "  Password: admin123"
echo ""
if [ -n "$API_KEY" ]; then
    echo "API Key (SAVE THIS!):"
    echo "  $API_KEY"
    echo ""
    echo "Save to file:"
    echo "  echo 'export API_KEY=\"$API_KEY\"' > ~/.processor-vps-api-key"
    echo "  source ~/.processor-vps-api-key"
    echo ""
fi
echo "Get Worker Token:"
echo "  ssh $VPS_USER@$VPS_IP 'cat /opt/processor/WORKER_TOKEN.txt'"
echo ""
echo "Useful Commands:"
echo "  View logs:    ssh $VPS_USER@$VPS_IP 'journalctl -u processor-control-plane -f'"
echo "  Restart:      ssh $VPS_USER@$VPS_IP 'systemctl restart processor-control-plane'"
echo "  Status:       ssh $VPS_USER@$VPS_IP 'systemctl status processor-control-plane'"
echo "  Create key:   ssh $VPS_USER@$VPS_IP '/opt/processor/create-key.sh admin@example.com KeyName'"
echo ""
echo "⚠️  IMPORTANT:"
echo "  1. Change admin password after first login!"
echo "  2. Save the API key above - it won't be shown again"
echo "  3. Save the worker token for data-plane workers"
echo ""
echo "Next steps:"
echo "  1. Test: curl http://$VPS_IP:8080/api/v1/health"
echo "  2. Login to web UI: http://$VPS_IP:8080"
echo "  3. Deploy data-plane worker (see docs)"
echo "  4. Set up HTTPS with nginx (see docs/PRODUCTION_READINESS.md)"
echo ""
