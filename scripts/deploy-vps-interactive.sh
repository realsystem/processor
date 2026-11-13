#!/bin/bash
set -e

# Interactive VPS Deployment
# This script will prompt for password when needed

VPS_IP="${1:-${VPS_IP:-}}"
[ -n "$VPS_IP" ] || { echo "usage: $0 <server-ip>   (or VPS_IP=<server-ip>)" >&2; exit 2; }
VPS_USER="root"

echo "🚀 Deploying Processor Control Plane to VPS"
echo "VPS: $VPS_USER@$VPS_IP"
echo ""
echo "⚠️  You will be prompted for the VPS password multiple times"
echo ""
read -p "Press Enter to continue..."

# Step 1: Build binary
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "1️⃣ Building Linux binary using Docker..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
./scripts/build-linux-binary.sh
echo "✅ Binary built: processor-server-linux"

# Step 2: Create deployment package
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "2️⃣ Creating deployment package..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Create temporary directory
mkdir -p /tmp/processor-deploy
cp processor-server-linux /tmp/processor-deploy/server
cp -r control-plane/migrations /tmp/processor-deploy/ 2>/dev/null || true
cp -r control-plane/internal/server/templates /tmp/processor-deploy/ 2>/dev/null || true
cp -r control-plane/internal/server/static /tmp/processor-deploy/ 2>/dev/null || true

# Create setup script
cat > /tmp/processor-deploy/setup.sh <<'SETUPSCRIPT'
#!/bin/bash
set -e

echo "Installing on VPS..."

# Install dependencies
echo "Installing dependencies..."
apt-get update -qq
apt-get install -y sqlite3 ca-certificates

# Create directory structure
echo "Creating directories..."
mkdir -p /opt/processor/{bin,data,migrations,templates,static}

# Move files
echo "Installing files..."
cp server /opt/processor/bin/
chmod +x /opt/processor/bin/server
cp -r migrations/* /opt/processor/migrations/ 2>/dev/null || true
cp -r templates/* /opt/processor/templates/ 2>/dev/null || true
cp -r static/* /opt/processor/static/ 2>/dev/null || true

# Generate secrets
SESSION_SECRET=$(openssl rand -hex 32)
WORKER_TOKEN=$(openssl rand -hex 32)
ADMIN_PASSWORD=$(openssl rand -hex 12)

# Create environment file
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

echo "Worker Token: $WORKER_TOKEN" > /opt/processor/WORKER_TOKEN.txt

( umask 077; echo "$ADMIN_PASSWORD" > /opt/processor/ADMIN_PASSWORD.txt )
echo "⚠️  SAVE THIS: Worker token written to /opt/processor/WORKER_TOKEN.txt"

# Run migrations
echo "Running database migrations..."
cd /opt/processor
./bin/server -migrate

# Create systemd service
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

# Configure firewall
echo "Configuring firewall..."
ufw allow 8080/tcp 2>/dev/null || echo "UFW not available, skip firewall config"

# Start service
echo "Starting service..."
systemctl daemon-reload
systemctl enable processor-control-plane
systemctl restart processor-control-plane

sleep 2

# Check status
systemctl status processor-control-plane --no-pager || true

echo ""
echo "✅ Installation complete!"
echo ""
echo "Service status:"
systemctl is-active processor-control-plane && echo "✅ Running" || echo "❌ Not running"

echo ""
echo "Testing endpoint..."
curl -s http://localhost:8080/api/v1/health | head -20 || echo "⚠️  Service not responding yet"

SETUPSCRIPT

chmod +x /tmp/processor-deploy/setup.sh

# Create tarball
cd /tmp/processor-deploy
tar czf ../processor-deploy.tar.gz .
cd -

echo "✅ Package created: /tmp/processor-deploy.tar.gz"

# Step 3: Upload
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "3️⃣ Uploading to VPS..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
scp /tmp/processor-deploy.tar.gz "$VPS_USER@$VPS_IP:/tmp/"
echo "✅ Upload complete"

# Step 4: Install
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "4️⃣ Installing on VPS..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
ssh "$VPS_USER@$VPS_IP" "cd /tmp && tar xzf processor-deploy.tar.gz && bash setup.sh"

# Step 5: Test
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "5️⃣ Testing deployment..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
sleep 3
curl -s "http://$VPS_IP:8080/api/v1/health" | python3 -m json.tool || echo "⚠️  Endpoint not accessible yet"

# Cleanup
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🧹 Cleaning up..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
rm -f processor-server-linux
rm -rf /tmp/processor-deploy
rm -f /tmp/processor-deploy.tar.gz

# Final summary
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎉 DEPLOYMENT COMPLETE!"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Control Plane URL: http://$VPS_IP:8080"
echo ""
echo "Next steps:"
echo ""
echo "1. Test the endpoint:"
echo "   curl http://$VPS_IP:8080/api/v1/health"
echo ""
echo "2. Get worker token:"
echo "   ssh $VPS_USER@$VPS_IP 'cat /opt/processor/WORKER_TOKEN.txt'"
echo ""
echo "3. View logs:"
echo "   ssh $VPS_USER@$VPS_IP 'journalctl -u processor-control-plane -f'"
echo ""
echo "4. Create API key (see docs/VPS_DEPLOYMENT.md)"
echo ""
echo "5. Login to web UI:"
echo "   http://$VPS_IP:8080"
echo "   Email: admin@example.com"
echo "   Password: ssh $VPS_USER@$VPS_IP 'cat /opt/processor/ADMIN_PASSWORD.txt'"
echo ""
echo "⚠️  IMPORTANT: Change admin password after first login!"
echo ""
