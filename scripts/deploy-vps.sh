#!/bin/bash
set -e

# Deploy control-plane to VPS
# Usage: ./scripts/deploy-vps.sh <vps-ip> <vps-user>

VPS_IP="${1:?usage: $0 <server-ip> [user]}"
VPS_USER="${2:-root}"
SERVICE_NAME="processor-control-plane"

echo "🚀 Deploying Processor Control Plane to VPS"
echo "VPS: $VPS_USER@$VPS_IP"
echo ""

# Check SSH connection
echo "1️⃣ Testing SSH connection..."
if ! ssh -o ConnectTimeout=5 "$VPS_USER@$VPS_IP" "echo 'SSH connection successful'"; then
    echo "❌ Cannot connect to VPS. Check credentials and network."
    exit 1
fi
echo "✅ SSH connection successful"
echo ""

# Build the binary locally
echo "2️⃣ Building control-plane binary..."
cd control-plane
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o ../processor-control-plane-linux ./cmd/server
cd ..
echo "✅ Binary built: processor-control-plane-linux"
echo ""

# Create deployment package
echo "3️⃣ Creating deployment package..."
tar czf processor-deploy.tar.gz \
    processor-control-plane-linux \
    control-plane/migrations \
    control-plane/internal/server/templates \
    control-plane/internal/server/static
echo "✅ Package created: processor-deploy.tar.gz"
echo ""

# Upload to VPS
echo "4️⃣ Uploading to VPS..."
scp processor-deploy.tar.gz "$VPS_USER@$VPS_IP:/tmp/"
echo "✅ Files uploaded"
echo ""

# Install and configure on VPS
echo "5️⃣ Installing on VPS..."
ssh "$VPS_USER@$VPS_IP" bash <<'REMOTE_SCRIPT'
set -e

APP_DIR="/opt/processor"
SERVICE_NAME="processor-control-plane"

echo "Installing dependencies..."
apt-get update -qq
apt-get install -y sqlite3 ca-certificates

echo "Creating application directory..."
mkdir -p $APP_DIR/{bin,data,migrations,templates,static}

echo "Extracting files..."
cd /tmp
tar xzf processor-deploy.tar.gz
mv processor-control-plane-linux $APP_DIR/bin/server
mv control-plane/migrations/* $APP_DIR/migrations/ 2>/dev/null || true
mv control-plane/internal/server/templates/* $APP_DIR/templates/ 2>/dev/null || true
mv control-plane/internal/server/static/* $APP_DIR/static/ 2>/dev/null || true
chmod +x $APP_DIR/bin/server
rm -f processor-deploy.tar.gz

echo "Creating environment file..."
cat > $APP_DIR/.env <<'EOF'
# Processor Control Plane Configuration
DATABASE_PATH=/opt/processor/data/processor.db
SESSION_SECRET=$(openssl rand -hex 32)
WORKER_TOKEN=$(openssl rand -hex 32)
HTTP_PORT=8080
ENV=production
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=change-me-on-first-login
LOG_LEVEL=info
LEASE_DURATION=5m

# SMTP Configuration (optional - configure for email notifications)
# SMTP_HOST=smtp.gmail.com
# SMTP_PORT=587
# SMTP_USER=your-email@gmail.com
# SMTP_PASS=your-app-password
# SMTP_FROM=your-email@gmail.com
EOF

echo "Running database migrations..."
cd $APP_DIR
./bin/server -migrate

echo "Creating systemd service..."
cat > /etc/systemd/system/$SERVICE_NAME.service <<'SERVICEEOF'
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

# Security
NoNewPrivileges=true
PrivateTmp=true

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=processor-control-plane

[Install]
WantedBy=multi-user.target
SERVICEEOF

echo "Configuring firewall..."
ufw allow 8080/tcp 2>/dev/null || true

echo "Starting service..."
systemctl daemon-reload
systemctl enable $SERVICE_NAME
systemctl restart $SERVICE_NAME

echo "Waiting for service to start..."
sleep 3

echo "Checking service status..."
systemctl status $SERVICE_NAME --no-pager

REMOTE_SCRIPT

echo ""
echo "✅ Deployment complete!"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎉 Control Plane deployed to: http://$VPS_IP:8080"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Next steps:"
echo "1. Test the deployment:"
echo "   curl http://$VPS_IP:8080/api/v1/health"
echo ""
echo "2. View logs:"
echo "   ssh $VPS_USER@$VPS_IP 'journalctl -u $SERVICE_NAME -f'"
echo ""
echo "3. Create an API key:"
echo "   ssh $VPS_USER@$VPS_IP 'cd /opt/processor && ./bin/server'"
echo "   # Then use scripts/create-api-key.go on the VPS"
echo ""
echo "4. Configure SMTP (optional):"
echo "   ssh $VPS_USER@$VPS_IP 'nano /opt/processor/.env'"
echo "   # Edit SMTP settings, then: systemctl restart $SERVICE_NAME"
echo ""
echo "⚠️  IMPORTANT: Change the admin password on first login!"
echo "   Login: admin@example.com / change-me-on-first-login"
echo ""

# Cleanup
rm -f processor-control-plane-linux processor-deploy.tar.gz

echo "🧹 Cleaned up local files"
