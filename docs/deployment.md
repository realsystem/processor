# Deployment Guide

## Prerequisites

### Control Plane (Ubuntu VPS)

- Ubuntu 22.04 LTS or later
- 1GB RAM minimum
- 20GB disk space
- Public IP address
- Domain name (for HTTPS)
- Root or sudo access

### Data Plane (Local PC)

- Linux, macOS, or Windows
- 4GB+ RAM
- Network access to control plane
- (Optional) GPU for future Ollama tasks

## Control Plane Deployment

### 1. Initial Server Setup

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Create processor user
sudo useradd -r -s /bin/bash -m -d /var/lib/processor processor

# Create directories
sudo mkdir -p /var/lib/processor
sudo mkdir -p /var/log/processor
sudo mkdir -p /etc/processor
sudo mkdir -p /opt/processor/control-plane

# Set ownership
sudo chown -R processor:processor /var/lib/processor
sudo chown -R processor:processor /var/log/processor
```

### 2. Install Dependencies

```bash
# Install nginx
sudo apt install -y nginx

# Install SQLite (should be pre-installed)
sudo apt install -y sqlite3

# Install certbot for Let's Encrypt (HTTPS)
sudo apt install -y certbot python3-certbot-nginx
```

### 3. Build Control Plane

**On your local machine** (requires Go 1.21+):

```bash
cd control-plane

# Build for Linux AMD64
GOOS=linux GOARCH=amd64 go build -o server ./cmd/server

# Verify binary
file server
# Output: server: ELF 64-bit LSB executable, x86-64...

# Create tarball
tar czf control-plane.tar.gz server migrations/ static/ templates/
```

### 4. Deploy to VPS

```bash
# Copy to VPS
scp control-plane.tar.gz user@your-vps.com:/tmp/

# On VPS
ssh user@your-vps.com

# Extract
cd /opt/processor/control-plane
sudo tar xzf /tmp/control-plane.tar.gz
sudo chown -R processor:processor /opt/processor/control-plane
```

### 5. Configure Environment

```bash
# Create environment file
sudo vim /etc/processor/.env
```

```bash
# /etc/processor/.env

# Database
DATABASE_PATH=/var/lib/processor/processor.db

# Secrets (generate with: openssl rand -hex 32)
SESSION_SECRET=<generate-random-32-byte-hex>
WORKER_TOKEN=<generate-random-32-byte-hex>

# Application
BASE_URL=https://processor.example.com
HTTP_PORT=8080
ENV=production

# Admin Bootstrap (first run only)
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=ChangeMe123

# SMTP (optional, for email notifications)
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USER=notifications@example.com
SMTP_PASS=<smtp-password>
SMTP_FROM=noreply@processor.example.com

# Logging
LOG_LEVEL=info
```

```bash
# Set permissions
sudo chmod 600 /etc/processor/.env
sudo chown root:root /etc/processor/.env
```

### 6. Run Database Migration

```bash
# Switch to processor user
sudo -u processor bash

# Load environment
set -a; source /etc/processor/.env; set +a

# Run migration
cd /opt/processor/control-plane
./server -migrate

# Verify
sqlite3 $DATABASE_PATH "SELECT name FROM sqlite_master WHERE type='table';"
# Should list: users, sessions, api_keys, jobs, job_attempts, webhooks, webhook_deliveries, email_notifications, workers, audit_log

# Exit processor user
exit
```

### 7. Create systemd Service

```bash
sudo vim /etc/systemd/system/processor-control-plane.service
```

```ini
[Unit]
Description=Processor Control Plane
After=network.target

[Service]
Type=simple
User=processor
Group=processor
WorkingDirectory=/opt/processor/control-plane
EnvironmentFile=/etc/processor/.env
ExecStart=/opt/processor/control-plane/server
Restart=always
RestartSec=10

# Security
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/processor /var/log/processor
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictRealtime=true
RestrictNamespaces=true

# Resource limits
LimitNOFILE=65536
MemoryMax=800M

[Install]
WantedBy=multi-user.target
```

```bash
# Reload systemd
sudo systemctl daemon-reload

# Enable and start service
sudo systemctl enable processor-control-plane
sudo systemctl start processor-control-plane

# Check status
sudo systemctl status processor-control-plane

# Check logs
sudo journalctl -u processor-control-plane -f
```

### 8. Configure nginx

```bash
sudo vim /etc/nginx/sites-available/processor
```

```nginx
# Rate limiting zones
limit_req_zone $binary_remote_addr zone=auth_limit:10m rate=5r/m;
limit_req_zone $binary_remote_addr zone=api_limit:10m rate=100r/m;

# Upstream
upstream processor_backend {
    server 127.0.0.1:8080 fail_timeout=5s max_fails=3;
}

# HTTP -> HTTPS redirect
server {
    listen 80;
    listen [::]:80;
    server_name processor.example.com;

    location /.well-known/acme-challenge/ {
        root /var/www/html;
    }

    location / {
        return 301 https://$server_name$request_uri;
    }
}

# HTTPS
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name processor.example.com;

    # SSL certificates (Let's Encrypt)
    ssl_certificate /etc/letsencrypt/live/processor.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/processor.example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;

    # Security headers
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-Frame-Options "DENY" always;
    add_header X-XSS-Protection "1; mode=block" always;

    # Logging
    access_log /var/log/nginx/processor-access.log;
    error_log /var/log/nginx/processor-error.log;

    # Rate limiting for auth endpoints
    location ~ ^/(register|login) {
        limit_req zone=auth_limit burst=3 nodelay;
        proxy_pass http://processor_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Rate limiting for API
    location /api/ {
        limit_req zone=api_limit burst=20 nodelay;
        proxy_pass http://processor_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Static files
    location /static/ {
        alias /opt/processor/control-plane/static/;
        expires 1y;
        add_header Cache-Control "public, immutable";
    }

    # All other requests
    location / {
        proxy_pass http://processor_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Timeouts
        proxy_connect_timeout 5s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;

        # Buffering
        proxy_buffering on;
        proxy_buffer_size 4k;
        proxy_buffers 8 4k;
        proxy_busy_buffers_size 8k;
    }

    # Health check (no rate limit)
    location /api/v1/health {
        proxy_pass http://processor_backend;
        access_log off;
    }
}
```

```bash
# Enable site
sudo ln -s /etc/nginx/sites-available/processor /etc/nginx/sites-enabled/

# Test configuration
sudo nginx -t

# Reload nginx
sudo systemctl reload nginx
```

### 9. Obtain SSL Certificate

```bash
# Using Let's Encrypt
sudo certbot --nginx -d processor.example.com

# Follow prompts
# Choose: Redirect HTTP to HTTPS

# Auto-renewal is configured via systemd timer
sudo systemctl status certbot.timer
```

### 10. Configure Firewall

```bash
# Allow SSH, HTTP, HTTPS
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp

# Enable firewall
sudo ufw enable

# Verify
sudo ufw status
```

### 11. Verify Deployment

```bash
# Health check
curl https://processor.example.com/api/v1/health

# Should return:
# {"success":true,"data":{"status":"healthy","timestamp":"...","version":"1.0.0"}}

# Access web UI
# Open https://processor.example.com in browser

# Log in with admin credentials
# Email: admin@example.com
# Password: ChangeMe123

# IMPORTANT: Change admin password immediately after first login
```

## Data Plane Deployment

### 1. Build Worker

**On your local machine**:

```bash
cd data-plane

# Build for your platform
# Linux:
GOOS=linux GOARCH=amd64 go build -o worker ./cmd/worker

# macOS:
GOOS=darwin GOARCH=amd64 go build -o worker ./cmd/worker

# Windows:
GOOS=windows GOARCH=amd64 go build -o worker.exe ./cmd/worker
```

### 2. Configure Worker

```bash
# Create directory
mkdir -p ~/processor-worker
cd ~/processor-worker

# Copy binary
cp /path/to/data-plane/worker .

# Create environment file
vim .env
```

```bash
# .env

# Control plane URL
CONTROL_PLANE_URL=https://processor.example.com

# Worker token (must match control plane WORKER_TOKEN)
WORKER_TOKEN=<same-as-control-plane-worker-token>

# Worker configuration
WORKER_ID=worker_1
WORKER_NAME=main-worker
POLL_INTERVAL=5s

# Capabilities
WORKER_CAPABILITIES=noop,sleep,echo_metadata,ollama_chat

# Ollama configuration (optional)
OLLAMA_BASE_URL=http://localhost:11434

# Logging
LOG_LEVEL=info
```

### 3. Linux Deployment (systemd)

```bash
# Copy binary to system location
sudo cp worker /usr/local/bin/processor-worker
sudo chmod +x /usr/local/bin/processor-worker

# Create systemd service
sudo vim /etc/systemd/system/processor-worker.service
```

```ini
[Unit]
Description=Processor Data Plane Worker
After=network.target

[Service]
Type=simple
User=<your-user>
WorkingDirectory=/home/<your-user>/processor-worker
EnvironmentFile=/home/<your-user>/processor-worker/.env
ExecStart=/usr/local/bin/processor-worker
Restart=always
RestartSec=10

# Resource limits
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

```bash
# Enable and start
sudo systemctl daemon-reload
sudo systemctl enable processor-worker
sudo systemctl start processor-worker

# Check status
sudo systemctl status processor-worker

# View logs
sudo journalctl -u processor-worker -f
```

### 4. macOS Deployment (launchd)

```bash
# Create LaunchAgent plist
vim ~/Library/LaunchAgents/com.processor.worker.plist
```

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.processor.worker</string>
    
    <key>ProgramArguments</key>
    <array>
        <string>/Users/<your-user>/processor-worker/worker</string>
    </array>
    
    <key>WorkingDirectory</key>
    <string>/Users/<your-user>/processor-worker</string>
    
    <key>EnvironmentVariables</key>
    <dict>
        <key>CONTROL_PLANE_URL</key>
        <string>https://processor.example.com</string>
        <key>WORKER_TOKEN</key>
        <string>your-worker-token</string>
        <key>WORKER_ID</key>
        <string>worker_1</string>
    </dict>
    
    <key>RunAtLoad</key>
    <true/>
    
    <key>KeepAlive</key>
    <true/>
    
    <key>StandardOutPath</key>
    <string>/Users/<your-user>/processor-worker/worker.log</string>
    
    <key>StandardErrorPath</key>
    <string>/Users/<your-user>/processor-worker/worker.err.log</string>
</dict>
</plist>
```

```bash
# Load service
launchctl load ~/Library/LaunchAgents/com.processor.worker.plist

# Check status
launchctl list | grep processor

# View logs
tail -f ~/processor-worker/worker.log
```

### 5. Windows Deployment (NSSM)

```powershell
# Download NSSM (Non-Sucking Service Manager)
# https://nssm.cc/download

# Install as service
nssm install ProcessorWorker C:\processor-worker\worker.exe

# Configure environment
nssm set ProcessorWorker AppEnvironmentExtra CONTROL_PLANE_URL=https://processor.example.com
nssm set ProcessorWorker AppEnvironmentExtra WORKER_TOKEN=your-worker-token
nssm set ProcessorWorker AppDirectory C:\processor-worker

# Start service
nssm start ProcessorWorker

# Check status
nssm status ProcessorWorker
```

## Local Development Setup

### Control Plane

```bash
cd control-plane

# Install dependencies
go mod download

# Create .env for local development
cp .env.example .env
vim .env
```

```bash
# .env (local)
DATABASE_PATH=./processor.db
SESSION_SECRET=dev-session-secret-change-in-production
WORKER_TOKEN=dev-worker-token-change-in-production
HTTP_PORT=8080
ENV=development
LOG_LEVEL=debug
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=admin123
```

```bash
# Run migrations
go run ./cmd/server -migrate

# Start server
make dev
# or: go run ./cmd/server

# Server runs at http://localhost:8080
```

### Data Plane

```bash
cd data-plane

# Install dependencies
go mod download

# Create .env
cp .env.example .env
vim .env
```

```bash
# .env (local)
CONTROL_PLANE_URL=http://localhost:8080
WORKER_TOKEN=dev-worker-token-change-in-production
WORKER_ID=worker_local
POLL_INTERVAL=5s
LOG_LEVEL=debug
```

```bash
# Start worker
make dev
# or: go run ./cmd/worker

# Worker starts polling http://localhost:8080
```

## Docker Deployment (Local Testing)

### Dockerfiles Provided

- `control-plane/Dockerfile`
- `data-plane/Dockerfile`

### Docker Compose

```bash
# Create docker-compose.yml in repo root
vim docker-compose.yml
```

```yaml
version: '3.8'

services:
  control-plane:
    build:
      context: ./control-plane
      dockerfile: Dockerfile
    ports:
      - "8080:8080"
    environment:
      - DATABASE_PATH=/data/processor.db
      - SESSION_SECRET=docker-test-secret
      - WORKER_TOKEN=docker-test-worker-token
      - HTTP_PORT=8080
      - ENV=development
      - ADMIN_EMAIL=admin@example.com
      - ADMIN_PASSWORD=admin123
    volumes:
      - processor-data:/data
    restart: unless-stopped

  worker:
    build:
      context: ./data-plane
      dockerfile: Dockerfile
    environment:
      - CONTROL_PLANE_URL=http://control-plane:8080
      - WORKER_TOKEN=docker-test-worker-token
      - WORKER_ID=worker_docker
      - POLL_INTERVAL=5s
    depends_on:
      - control-plane
    restart: unless-stopped

volumes:
  processor-data:
```

```bash
# Start services
docker-compose up -d

# View logs
docker-compose logs -f

# Stop services
docker-compose down
```

## Updating / Redeployment

### Control Plane Update

```bash
# On local machine: build new binary
cd control-plane
GOOS=linux GOARCH=amd64 go build -o server ./cmd/server
tar czf control-plane.tar.gz server migrations/ static/ templates/

# Copy to VPS
scp control-plane.tar.gz user@your-vps.com:/tmp/

# On VPS
ssh user@your-vps.com
sudo systemctl stop processor-control-plane
cd /opt/processor/control-plane
sudo tar xzf /tmp/control-plane.tar.gz
sudo chown -R processor:processor .

# Run migrations (if schema changed)
sudo -u processor bash -c 'set -a; source /etc/processor/.env; set +a; ./server -migrate'

# Restart service
sudo systemctl start processor-control-plane
sudo systemctl status processor-control-plane

# Verify
curl https://processor.example.com/api/v1/health
```

### Worker Update

```bash
# Build new binary
cd data-plane
go build -o worker ./cmd/worker

# Copy to worker machine
scp worker user@worker-machine:~/processor-worker/

# On worker machine
sudo systemctl stop processor-worker
cp ~/processor-worker/worker /usr/local/bin/processor-worker
sudo systemctl start processor-worker
sudo systemctl status processor-worker
```

## Monitoring Setup

### Uptime Monitoring

Use external service (e.g., UptimeRobot, Pingdom):
- Monitor: `https://processor.example.com/api/v1/health`
- Interval: 5 minutes
- Alert: Email/SMS if down for >5 minutes

### Log Aggregation (Optional)

**Simple: View with journalctl**
```bash
# Tail logs
sudo journalctl -u processor-control-plane -f

# Search logs
sudo journalctl -u processor-control-plane | grep ERROR
```

**Advanced: Centralized logging**
- Install: Loki, Grafana, Promtail
- Or: Use cloud service (Papertrail, Loggly)

## Backup Automation

```bash
# Create backup script
sudo vim /usr/local/bin/backup-processor.sh
```

```bash
#!/bin/bash
set -e

BACKUP_DIR="/var/backups/processor"
RETENTION_DAYS=30

mkdir -p "$BACKUP_DIR"

# Backup database
sqlite3 /var/lib/processor/processor.db ".backup ${BACKUP_DIR}/processor-$(date +%Y%m%d-%H%M%S).db"

# Clean old backups
find "$BACKUP_DIR" -name "processor-*.db" -mtime +$RETENTION_DAYS -delete

echo "Backup completed: $(date)"
```

```bash
# Make executable
sudo chmod +x /usr/local/bin/backup-processor.sh

# Create cron job
sudo crontab -e
```

```
# Backup daily at 2 AM
0 2 * * * /usr/local/bin/backup-processor.sh >> /var/log/processor/backup.log 2>&1
```

## Security Hardening Checklist

- [ ] Change default admin password
- [ ] Use strong random secrets (SESSION_SECRET, WORKER_TOKEN)
- [ ] Enable HTTPS with valid certificate
- [ ] Configure firewall (ufw)
- [ ] Set proper file permissions (600 for .env, database)
- [ ] Enable automatic security updates
- [ ] Configure fail2ban (optional, for SSH protection)
- [ ] Regular backups automated
- [ ] Monitor logs for suspicious activity
- [ ] Keep Go and system packages updated

```bash
# Enable automatic security updates (Ubuntu)
sudo apt install -y unattended-upgrades
sudo dpkg-reconfigure -plow unattended-upgrades
```

## Troubleshooting Deployment

### Cannot connect to database

```bash
# Check file exists
ls -la /var/lib/processor/processor.db

# Check permissions
sudo -u processor test -r /var/lib/processor/processor.db && echo "OK" || echo "FAIL"

# Check SELinux (if applicable)
sestatus
```

### Service won't start

```bash
# Check logs
sudo journalctl -u processor-control-plane -n 100 --no-pager

# Check environment variables loaded
sudo systemctl show processor-control-plane | grep Environment

# Test binary manually
sudo -u processor bash
cd /opt/processor/control-plane
./server
```

### Worker can't connect to control plane

```bash
# Test connectivity
curl https://processor.example.com/api/v1/health

# Test worker auth
curl -X POST https://processor.example.com/api/v1/worker/poll \
  -H "X-Worker-Token: your-worker-token" \
  -H "Content-Type: application/json" \
  -d '{"worker_id":"test","capabilities":["noop"]}'

# Check DNS resolution
nslookup processor.example.com

# Check firewall
ping processor.example.com
```
