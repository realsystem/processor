#!/usr/bin/env bash
# Deploy the control plane on THIS machine (e.g. a fresh GCP VM) from the prebuilt GHCR image
# (built by CI on push to main; nothing is compiled here).
#   ./scripts/deploy-local.sh [admin-email]
#   PROCESSOR_TAG=sha-abc1234 ./scripts/deploy-local.sh   # pin a version (default: main)
# Re-running keeps the existing .env (and your data) and just pulls and restarts.
set -euo pipefail
cd "$(dirname "$0")/.."

ADMIN_EMAIL="${1:-${ADMIN_EMAIL:-admin@example.com}}"

if ! command -v docker >/dev/null; then
  echo "Installing Docker..."
  curl -fsSL https://get.docker.com | sudo sh
  sudo usermod -aG docker "$USER"
fi
DOCKER="docker"; docker info >/dev/null 2>&1 || DOCKER="sudo docker"

if [ ! -f .env ]; then
  ADMIN_PASSWORD="$(openssl rand -base64 18)"
  cat > .env <<ENV
ENV=production
WORKER_AUTH=signature
ADMIN_EMAIL=$ADMIN_EMAIL
ADMIN_PASSWORD=$ADMIN_PASSWORD
SESSION_SECRET=$(openssl rand -hex 32)
ENV
  chmod 600 .env
  echo "Created .env  (admin: $ADMIN_EMAIL / $ADMIN_PASSWORD)  <- save this"
fi

COMPOSE="$DOCKER compose -f docker-compose.deploy.yml"
$COMPOSE pull
$COMPOSE up -d

echo "Waiting for health..."
for _ in $(seq 1 30); do
  curl -fs http://localhost:8080/api/v1/health >/dev/null && { echo "Control plane is up on port 8080."; break; }
  sleep 2
done

cat <<MSG

Next:
  - Reach it from your PC via Tailscale (http://<vm-tailnet-name>:8080) or a TLS proxy.
    Port 8080 is published on all interfaces: keep the GCP firewall closed to it.
  - Register a worker's public key (printed by the Windows setup script):
      $COMPOSE exec control-plane ./server -register-worker-key gpu-1=<pubkey> -capabilities llm.chat
MSG
