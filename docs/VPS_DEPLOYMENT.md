# VPS Deployment Guide

Deploy the Processor control-plane to a VPS server.

## Quick Deploy (Automated)

```bash
# From project root
./scripts/deploy-vps.sh 66.179.241.224 root
```

This will:
1. Build the Linux binary
2. Upload to VPS
3. Install dependencies
4. Configure systemd service
5. Start the service

## Manual Deployment

If you prefer step-by-step manual deployment:

### Step 1: Build Binary

```bash
# Build for Linux
cd control-plane
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o processor-control-plane-linux ./cmd/server
```

### Step 2: Upload to VPS

```bash
# Upload binary and files
scp processor-control-plane-linux root@66.179.241.224:/tmp/
scp -r migrations root@66.179.241.224:/tmp/
scp -r internal/server/templates root@66.179.241.224:/tmp/
scp -r internal/server/static root@66.179.241.224:/tmp/
```

### Step 3: SSH into VPS

```bash
ssh root@66.179.241.224
```

### Step 4: Install on VPS

```bash
# Install dependencies
apt-get update
apt-get install -y sqlite3 ca-certificates

# Create application directory
mkdir -p /opt/processor/{bin,data,migrations,templates,static}

# Move files
mv /tmp/processor-control-plane-linux /opt/processor/bin/server
mv /tmp/migrations/* /opt/processor/migrations/
mv /tmp/templates/* /opt/processor/templates/
mv /tmp/static/* /opt/processor/static/
chmod +x /opt/processor/bin/server

# Create environment file
cat > /opt/processor/.env <<'EOF'
DATABASE_PATH=/opt/processor/data/processor.db
SESSION_SECRET=$(openssl rand -hex 32)
WORKER_TOKEN=$(openssl rand -hex 32)
HTTP_PORT=8080
ENV=production
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=change-me-on-first-login
LOG_LEVEL=info
LEASE_DURATION=5m
EOF

# Run migrations
cd /opt/processor
./bin/server -migrate
```

### Step 5: Create Systemd Service

```bash
cat > /etc/systemd/system/processor-control-plane.service <<'EOF'
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
EOF

# Enable and start
systemctl daemon-reload
systemctl enable processor-control-plane
systemctl start processor-control-plane

# Check status
systemctl status processor-control-plane
```

### Step 6: Configure Firewall

```bash
# Allow HTTP traffic
ufw allow 8080/tcp
ufw status
```

### Step 7: Test Deployment

```bash
# From VPS
curl http://localhost:8080/api/v1/health

# From your machine
curl http://66.179.241.224:8080/api/v1/health
```

Expected response:
```json
{
  "success": true,
  "data": {
    "status": "healthy",
    "timestamp": "...",
    "version": "1.0.0"
  }
}
```

## Post-Deployment

### Create API Key

SSH into VPS and create an API key:

```bash
ssh root@66.179.241.224

# On VPS
cd /opt/processor

# Create a helper script
cat > create-api-key.sh <<'EOF'
#!/bin/bash
EMAIL="${1:-admin@example.com}"
NAME="${2:-Production Key}"
DB_PATH="${3:-/opt/processor/data/processor.db}"

# Generate random key
PREFIX=$(openssl rand -hex 8)
SECRET=$(openssl rand -hex 20)
FULL_KEY="pk_${PREFIX}_${SECRET}"

# Hash for storage (simplified - bcrypt would be better)
KEY_HASH=$(echo -n "$FULL_KEY" | openssl dgst -sha256 | awk '{print $2}')

# Get user ID
USER_ID=$(sqlite3 "$DB_PATH" "SELECT id FROM users WHERE email='$EMAIL';")

if [ -z "$USER_ID" ]; then
    echo "Error: User $EMAIL not found"
    exit 1
fi

# Insert API key
sqlite3 "$DB_PATH" <<SQL
INSERT INTO api_keys (id, user_id, name, key_prefix, key_hash, created_at)
VALUES ('key_$(openssl rand -hex 12)', '$USER_ID', '$NAME', 'pk_$PREFIX', '$KEY_HASH', datetime('now'));
SQL

echo "✅ API Key created!"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🔑 API Key: $FULL_KEY"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Save this key - it won't be shown again!"
EOF

chmod +x create-api-key.sh

# Create API key
./create-api-key.sh admin@example.com "Production Key"
```

### Configure SMTP (Optional)

```bash
ssh root@66.179.241.224

# Edit environment
nano /opt/processor/.env

# Add/update SMTP settings:
# SMTP_HOST=smtp.gmail.com
# SMTP_PORT=587
# SMTP_USER=your-email@gmail.com
# SMTP_PASS=your-app-password
# SMTP_FROM=your-email@gmail.com

# Restart service
systemctl restart processor-control-plane
```

## Management Commands

### View Logs

```bash
# Real-time logs
ssh root@66.179.241.224 'journalctl -u processor-control-plane -f'

# Last 100 lines
ssh root@66.179.241.224 'journalctl -u processor-control-plane -n 100'
```

### Restart Service

```bash
ssh root@66.179.241.224 'systemctl restart processor-control-plane'
```

### Check Status

```bash
ssh root@66.179.241.224 'systemctl status processor-control-plane'
```

### Update Deployment

```bash
# Build new binary
cd control-plane
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o processor-control-plane-linux ./cmd/server

# Upload and restart
scp processor-control-plane-linux root@66.179.241.224:/opt/processor/bin/server
ssh root@66.179.241.224 'systemctl restart processor-control-plane'
```

### Backup Database

```bash
# Download database backup
scp root@66.179.241.224:/opt/processor/data/processor.db ./processor-backup-$(date +%Y%m%d).db
```

## Troubleshooting

### Service won't start

```bash
# Check logs
journalctl -u processor-control-plane -n 50

# Check if port is in use
netstat -tlnp | grep 8080

# Test binary directly
cd /opt/processor
./bin/server
```

### Cannot connect from outside

```bash
# Check firewall
ufw status

# Check if service is listening
netstat -tlnp | grep 8080

# Test locally first
curl http://localhost:8080/api/v1/health
```

### Database errors

```bash
# Check database file
ls -la /opt/processor/data/processor.db

# Run migrations again
cd /opt/processor
./bin/server -migrate

# Check database integrity
sqlite3 /opt/processor/data/processor.db "PRAGMA integrity_check;"
```

## Security Recommendations

1. **Change default admin password** immediately after deployment
2. **Use strong SESSION_SECRET and WORKER_TOKEN** (auto-generated in .env)
3. **Configure firewall** properly (ufw)
4. **Enable HTTPS** with nginx reverse proxy + Let's Encrypt
5. **Regular backups** of the database
6. **Monitor logs** for suspicious activity
7. **Keep system updated**: `apt-get update && apt-get upgrade`

## Production Setup with NGINX + SSL

See [docs/PRODUCTION_READINESS.md](PRODUCTION_READINESS.md) for:
- NGINX reverse proxy setup
- SSL/TLS with Let's Encrypt
- Domain configuration
- Rate limiting
- Security hardening
