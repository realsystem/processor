#!/usr/bin/env bash
# Installs the host side of the Telegram updater (run as root from a checkout of this repository):
# the state directories, the root-owned update script, and the systemd path unit that runs it when
# the bot drops a request. It does not start the bot and never touches the control plane.
# See docs/updater.md for the whole procedure.
set -euo pipefail

STATE_DIR="${STATE_DIR:-/var/lib/processor-updater}"
CONF_DIR="${CONF_DIR:-/etc/processor-updater}"
LIB_DIR="${LIB_DIR:-/usr/local/lib/processor-updater}"
SYSTEMD_DIR="${SYSTEMD_DIR:-/etc/systemd/system}"
BOT_UID="${BOT_UID:-10001}"
PROCESSOR_DIR="${PROCESSOR_DIR:-/opt/processor}"

if [[ "$EUID" -ne 0 ]]; then
    echo "run as root (sudo $0)" >&2
    exit 1
fi
for cmd in docker flock systemctl install sed; do
    command -v "$cmd" > /dev/null || {
        echo "missing required command: $cmd" >&2
        exit 1
    }
done
repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[[ -f "$PROCESSOR_DIR/docker-compose.deploy.yml" ]] || echo "note: $PROCESSOR_DIR/docker-compose.deploy.yml not found yet; set PROCESSOR_DIR in $CONF_DIR/update.env" >&2

# State: the bot may write requests/ and bot/; results/ is written by root only (the bot reads it).
install -d -m 0755 -o root -g root "$STATE_DIR" "$STATE_DIR/results"
install -d -m 0750 -o "$BOT_UID" -g "$BOT_UID" "$STATE_DIR/bot" "$STATE_DIR/requests"
install -d -m 0750 -o root -g root "$CONF_DIR"

# The privileged script is owned by root and not writable by anyone else.
install -d -m 0755 -o root -g root "$LIB_DIR"
install -m 0755 -o root -g root "$repo/scripts/processor-update.sh" "$LIB_DIR/processor-update.sh"

for unit in processor-update.path processor-update.service; do
    sed "s|@STATE_DIR@|$STATE_DIR|g" "$repo/deploy/systemd/$unit" > "$SYSTEMD_DIR/$unit"
    chmod 644 "$SYSTEMD_DIR/$unit"
done

if [[ ! -f "$CONF_DIR/update.env" ]]; then
    printf 'PROCESSOR_DIR=%s\n# HEALTH_URL=http://127.0.0.1:8080/api/v1/health\n# HEALTH_TIMEOUT=120\n' "$PROCESSOR_DIR" > "$CONF_DIR/update.env"
    chmod 600 "$CONF_DIR/update.env"
fi
if [[ ! -f "$CONF_DIR/updater.env" ]]; then
    install -m 0600 -o root -g root "$repo/deploy/updater.env.example" "$CONF_DIR/updater.env"
fi

systemctl daemon-reload
systemctl enable --now processor-update.path

cat << EOF2
Installed the host side of the updater.
  state:   $STATE_DIR   (requests/ and bot/ writable by uid $BOT_UID, results/ by root only)
  script:  $LIB_DIR/processor-update.sh
  units:   processor-update.path (active), processor-update.service
  config:  $CONF_DIR/update.env (host script), $CONF_DIR/updater.env (bot)

Next (details in docs/updater.md):
  1. Save the bot token (from @BotFather) as the only content of $CONF_DIR/telegram_token, then:
       chown $BOT_UID:$BOT_UID $CONF_DIR/telegram_token && chmod 0400 $CONF_DIR/telegram_token
  2. Start the bot:   docker compose -f docker-compose.updater.yml up -d --build
  3. Send /id to the bot, put your id in UPDATER_ALLOWED_IDS in $CONF_DIR/updater.env, restart the bot
  4. TOTP:            docker compose -f docker-compose.updater.yml run --rm updater-bot --totp-setup
  5. Cut a release tag (see docs/updater.md), wait for green CI, then /update <code>
EOF2
