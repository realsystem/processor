#!/usr/bin/env bash
# Privileged half of the Telegram updater: consumes one request file dropped by the bot, switches the
# control plane to the requested release tag, checks health, rolls back if it fails, and writes a
# result file for the bot. Run by systemd (processor-update.service) as root; see docs/updater.md.
#
# The request directory is writable by the unprivileged bot, so nothing in it is trusted: the tag
# and the request id are re-validated here against strict patterns, and the file is never echoed.
set -uo pipefail
umask 022

STATE_DIR="${STATE_DIR:-/var/lib/processor-updater}"
PROCESSOR_DIR="${PROCESSOR_DIR:-/opt/processor}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.deploy.yml}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8080/api/v1/health}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-120}"
HEALTH_INTERVAL="${HEALTH_INTERVAL:-3}"
DOCKER_BIN="${DOCKER_BIN:-docker}"
CURL_BIN="${CURL_BIN:-curl}"

REQ="$STATE_DIR/requests/update.request"
RESULTS="$STATE_DIR/results"
ENV_FILE="$PROCESSOR_DIR/.env"
TAG_RE='^v[0-9]+\.[0-9]+(\.[0-9]+)?$'
CUR_RE='^[A-Za-z0-9._-]{1,64}$'
ID_RE='^[a-f0-9]{16,64}$'

REQUEST_ID=""
CUR="main"
NEW=""
FINISHED=0

log() { echo "processor-update: $*" >&2; }
clean() { printf '%s' "$1" | tr -cd 'A-Za-z0-9 ._:/,()=-' | cut -c1-300; }

# write_result STATUS FROM TO DETAIL  (atomic; the results directory is root-owned)
write_result() {
    local status="$1" from to detail tmp
    from="$(clean "$2")"
    to="$(clean "$3")"
    detail="$(clean "$4")"
    FINISHED=1
    [[ -n "$REQUEST_ID" ]] || return 0
    tmp="$(mktemp "$RESULTS/.result.XXXXXX")" || return 0
    printf '{"request_id":"%s","status":"%s","from":"%s","to":"%s","detail":"%s","finished_at":"%s"}\n' \
        "$REQUEST_ID" "$status" "$from" "$to" "$detail" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$tmp"
    chmod 644 "$tmp"
    mv -f "$tmp" "$RESULTS/update-result.json"
}

write_current() {
    local tmp
    tmp="$(mktemp "$RESULTS/.cur.XXXXXX")" || return 0
    printf '%s\n' "$1" > "$tmp"
    chmod 644 "$tmp"
    mv -f "$tmp" "$RESULTS/current-tag"
}

on_exit() {
    if [[ "$FINISHED" -eq 0 ]]; then
        write_result failed "$CUR" "$NEW" "update script ended unexpectedly; check journalctl -u processor-update"
    fi
}
trap on_exit EXIT

compose() {
    PROCESSOR_TAG="$1" "$DOCKER_BIN" compose -f "$PROCESSOR_DIR/$COMPOSE_FILE" --project-directory "$PROCESSOR_DIR" "${@:2}"
}

# Tag now in the settings file (the deployed version), default main.
read_env_tag() {
    local v=""
    [[ -f "$ENV_FILE" ]] && v="$(grep -E '^PROCESSOR_TAG=' "$ENV_FILE" | tail -n1 | cut -d= -f2- | tr -d '"'"'"' ')"
    [[ "$v" =~ $CUR_RE ]] && echo "$v" || echo "main"
}

set_env_tag() {
    local tmp
    tmp="$(mktemp "$PROCESSOR_DIR/.env.XXXXXX")" || return 1
    if [[ -f "$ENV_FILE" ]]; then
        grep -v '^PROCESSOR_TAG=' "$ENV_FILE" > "$tmp" || true
        chmod --reference="$ENV_FILE" "$tmp" 2> /dev/null || chmod 600 "$tmp"
    else
        chmod 600 "$tmp"
    fi
    printf 'PROCESSOR_TAG=%s\n' "$1" >> "$tmp"
    mv -f "$tmp" "$ENV_FILE"
}

healthy() {
    local waited=0
    while true; do
        if "$CURL_BIN" -fsS --max-time 5 "$HEALTH_URL" > /dev/null 2>&1; then
            return 0
        fi
        [[ "$waited" -ge "$HEALTH_TIMEOUT" ]] && return 1
        sleep "$HEALTH_INTERVAL"
        waited=$((waited + (HEALTH_INTERVAL > 0 ? HEALTH_INTERVAL : 1)))
    done
}

# --- read and validate the request, consume it first so it can never loop ----------------------
if [[ ! -e "$REQ" && ! -L "$REQ" ]]; then
    FINISHED=1
    exit 0
fi
if [[ -L "$REQ" || ! -f "$REQ" ]]; then
    rm -f "$REQ"
    log "request is not a regular file; dropped"
    FINISHED=1
    exit 0
fi
{
    IFS= read -r NEW_RAW
    IFS= read -r ID_RAW
} < "$REQ" || true
rm -f "$REQ"
if ! [[ "${ID_RAW:-}" =~ $ID_RE ]]; then
    # The bot always writes a request id; a request without one did not come from it.
    log "request without a valid id; dropped"
    FINISHED=1
    exit 0
fi
REQUEST_ID="$ID_RAW"
if ! [[ "${NEW_RAW:-}" =~ $TAG_RE ]]; then
    write_result refused "" "" "invalid request: not a release tag"
    log "invalid request"
    exit 0
fi
NEW="$NEW_RAW"

# --- one update at a time ------------------------------------------------------------------------
exec 9> "$STATE_DIR/update.lock"
if ! flock -n 9; then
    # Do not touch the result file: it belongs to the update that is running.
    log "another update is already in progress; request $REQUEST_ID dropped"
    FINISHED=1
    exit 0
fi

CUR="$(read_env_tag)"
log "request $REQUEST_ID: $CUR -> $NEW"

# Already there and healthy: nothing to do (idempotent). Unhealthy: fall through and repair.
if [[ "$CUR" == "$NEW" ]] && healthy; then
    write_current "$CUR"
    write_result success "$CUR" "$NEW" "already running this release and healthy"
    exit 0
fi

# --- switch -------------------------------------------------------------------------------------
write_previous() { printf '%s\n' "$CUR" > "$RESULTS/previous-tag.tmp" && mv -f "$RESULTS/previous-tag.tmp" "$RESULTS/previous-tag"; }
write_previous

if ! compose "$NEW" pull > /dev/null; then
    # Nothing was restarted, so there is nothing to roll back.
    write_result failed "$CUR" "$NEW" "could not pull the image for $NEW (is it published to the registry yet?); still on $CUR"
    exit 1
fi
set_env_tag "$NEW" || { write_result failed "$CUR" "$NEW" "could not write the settings file; still on $CUR"; exit 1; }

if compose "$NEW" up -d > /dev/null && healthy; then
    write_current "$NEW"
    write_result success "$CUR" "$NEW" "control plane healthy on $NEW"
    exit 0
fi

# --- roll back ----------------------------------------------------------------------------------
log "update to $NEW failed; rolling back to $CUR"
set_env_tag "$CUR" || true
if compose "$CUR" up -d > /dev/null && healthy; then
    write_current "$CUR"
    write_result rolled_back "$CUR" "$NEW" "back on $CUR and healthy"
    exit 1
fi
write_result failed "$CUR" "$NEW" "update and rollback both failed; recover by hand (see docs/updater.md)"
exit 1
