#!/bin/bash
set -e

# Deploy by building on VPS (avoids cross-compilation issues)

VPS_IP="${1:-${VPS_IP:-}}"
[ -n "$VPS_IP" ] || { echo "usage: $0 <server-ip>   (or VPS_IP=<server-ip>)" >&2; exit 2; }
VPS_USER="root"

echo "🚀 Deploying Processor Control Plane to VPS"
echo "Building directly on the server to avoid cross-compilation issues"
echo ""
echo "VPS: $VPS_USER@$VPS_IP"
echo ""
read -p "Press Enter to continue..."

# Step 1: Upload source code
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "1️⃣ Creating source package..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Create tarball of control-plane source
tar czf /tmp/control-plane-source.tar.gz \
    --exclude='processor.db' \
    --exclude='*.log' \
    --exclude='.git' \
    control-plane/

echo "✅ Source package created"

# Step 2: Upload
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "2️⃣ Uploading to VPS..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
scp /tmp/control-plane-source.tar.gz "$VPS_USER@$VPS_IP:/tmp/"
echo "✅ Upload complete"

# Step 3: Build and install on VPS
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "3️⃣ Building and installing on VPS..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

ssh "$VPS_USER@$VPS_IP" bash <<'REMOTE_SCRIPT'
set -e

echo "Installing dependencies..."
apt-get update -qq
apt-get install -y golang-1.21 sqlite3 ca-certificates build-essential

# Setup Go path
export PATH=/usr/lib/go-1.21/bin:$PATH
export GOPATH=/root/go

echo "Extracting source..."
cd /tmp
tar xzf control-plane-source.tar.gz

echo "Building binary..."
cd control-plane
go mod download
CGO_ENABLED=1 go build -o processor-server ./cmd/server

echo "Installing..."
mkdir -p /opt/processor/{bin,data,migrations,templates,static}
cp processor-server /opt/processor/bin/server
chmod +x /opt/processor/bin/server
cp -r migrations/* /opt/processor/migrations/ 2>/dev/null || true
cp -r internal/server/templates/* /opt/processor/templates/ 2>/dev/null || true
cp -r internal/server/static/* /opt/processor/static/ 2>/dev/null || true

# Generate secrets
SESSION_SECRET=$(openssl rand -hex 32)
WORKER_TOKEN=$(openssl rand -hex 32)
ADMIN_PASSWORD=$(openssl rand -hex 12)

echo "Creating configuration..."
cat > /opt/processor/.env <<EOF
DATABASE_PATH=/opt/processor/data/processor.db
SESSION_SECRET=$SESSION_SECRET
WORKER_TOKEN=$WORKER_TOKEN
HTTP_PORT=8080
ENV=production
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=$ADMIN_PASSWORD
LOG_LEVEL=info
LEASE_DURATION=5m
EOF

# Save worker token
echo "$WORKER_TOKEN" > /opt/processor/WORKER_TOKEN.txt
( umask 077; echo "$ADMIN_PASSWORD" > /opt/processor/ADMIN_PASSWORD.txt )
echo "✅ Worker token saved to /opt/processor/WORKER_TOKEN.txt"

echo "Running database migrations..."
cd /opt/processor
./bin/server -migrate

echo "Creating systemd service..."
cat > /etc/systemd/system/processor-control-plane.service <<'SERVICEEOF'
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
SERVICEEOF

echo "Configuring firewall..."
ufw allow 8080/tcp 2>/dev/null || echo "UFW not available"

echo "Starting service..."
systemctl daemon-reload
systemctl enable processor-control-plane
systemctl restart processor-control-plane

sleep 3

echo "Checking service status..."
systemctl status processor-control-plane --no-pager || true

echo ""
echo "Testing endpoint..."
curl -s http://localhost:8080/api/v1/health || echo "Service not ready yet"

echo ""
echo "✅ Deployment complete!"

# Cleanup
rm -rf /tmp/control-plane /tmp/control-plane-source.tar.gz

REMOTE_SCRIPT

# Step 4: Test from outside
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "4️⃣ Testing deployment..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
sleep 2
curl -s "http://$VPS_IP:8080/api/v1/health" | python3 -m json.tool || echo "⚠️  Waiting for service..."

# Cleanup local files
rm -f /tmp/control-plane-source.tar.gz

# Final summary
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎉 DEPLOYMENT COMPLETE!"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Control Plane: http://$VPS_IP:8080"
echo "Web UI: http://$VPS_IP:8080"
echo ""
echo "Default Login:"
echo "  Email: admin@example.com"
echo "  Password: ssh $VPS_USER@$VPS_IP 'cat /opt/processor/ADMIN_PASSWORD.txt'"
echo ""
echo "Get Worker Token:"
echo "  ssh $VPS_USER@$VPS_IP 'cat /opt/processor/WORKER_TOKEN.txt'"
echo ""
echo "View Logs:"
echo "  ssh $VPS_USER@$VPS_IP 'journalctl -u processor-control-plane -f'"
echo ""
echo "⚠️  IMPORTANT: Change admin password after first login!"
echo ""
