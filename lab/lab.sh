#!/usr/bin/env bash
# Processor's part of the lab (see autonomy/design/lab.md). Builds from THIS checkout,
# including uncommitted changes. Needs only bash, docker and docker compose.
#
#   lab/lab.sh up | down | status | logs [service] | reset | llm <mode> | key
#
# Environment (all optional): LAB_DIR (~/.autonomy-lab), LAB_NETWORK (autonomy-lab),
# LAB_PROJECT (alab), LAB_PROCESSOR_PORT (18080).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export LAB_DIR="${LAB_DIR:-$HOME/.autonomy-lab}"
export LAB_NETWORK="${LAB_NETWORK:-autonomy-lab}"
export LAB_PROJECT="${LAB_PROJECT:-alab}"
export LAB_PROCESSOR_PORT="${LAB_PROCESSOR_PORT:-18080}"
export LAB_STATE="$LAB_DIR/processor"
ENV_OUT="$LAB_DIR/processor.env"
cd "$HERE"

compose() { docker compose -f "$HERE/compose.yml" "$@"; }
die() { echo "lab: $*" >&2; exit 1; }
say() { echo "lab/processor: $*"; }

need_docker() {
  command -v docker >/dev/null || die "docker is not installed"
  docker info >/dev/null 2>&1 || die "docker is not running"
  docker compose version >/dev/null 2>&1 || die "docker compose is not available"
}

mock_mode() { # mode [delay]
  local body="{\"mode\":\"$1\"${2:+,\"delay\":$2}}"
  docker exec -i "${LAB_PROJECT}-mock-llm" python - "$body" <<'PY'
import sys, urllib.request
r = urllib.request.Request("http://127.0.0.1:11434/_lab/mode", sys.argv[1].encode(),
                           {"Content-Type": "application/json"})
print(urllib.request.urlopen(r, timeout=5).read().decode())
PY
}

cmd_up() {
  need_docker
  umask 077
  mkdir -p "$LAB_STATE/mock-llm" "$LAB_STATE/worker-key" "$LAB_STATE/secrets"
  chmod 777 "$LAB_STATE/mock-llm" "$LAB_STATE/worker-key"   # container users differ; lab data only
  docker network inspect "$LAB_NETWORK" >/dev/null 2>&1 \
    || docker network create --label autonomy.lab=true "$LAB_NETWORK" >/dev/null
  say "building from $(git -C "$HERE/.." rev-parse --short HEAD 2>/dev/null || echo ?) (working copy)"
  compose up -d --build --wait control-plane mock-llm

  # the worker signs its requests: create its key, register the public key, then start it
  pub="$(compose run --rm --no-deps -T worker ./worker -print-public-key | tail -n 1)"
  [ -n "$pub" ] || die "could not read the worker's public key"
  compose exec -T control-plane ./server -register-worker-key "worker_lab=$pub" \
    -capabilities noop,echo_metadata,llm.chat >/dev/null
  compose up -d --wait worker

  # an API key for the bot, made once and kept
  keyfile="$LAB_STATE/secrets/bot-api-key"
  if [ ! -s "$keyfile" ]; then
    k="$(compose exec -T control-plane ./server -create-api-key lab@example.com -api-key-name lab-bot | tail -n 1)"
    case "$k" in pk_*) ;; *) die "no API key: $k" ;; esac
    printf '%s\n' "$k" > "$keyfile"
  fi
  chmod 644 "$keyfile"   # the bot container reads it as another user; a lab key for a lab service

  cat > "$ENV_OUT" <<ENV
PROCESSOR_URL=http://${LAB_PROJECT}-processor:8080
PROCESSOR_HOST_URL=http://127.0.0.1:${LAB_PROCESSOR_PORT}
PROCESSOR_API_KEY_FILE=$keyfile
MOCK_LLM_URL=http://${LAB_PROJECT}-mock-llm:11434
MOCK_LLM_LOG=$LAB_STATE/mock-llm/mock-llm.jsonl
ENV
  say "up. $ENV_OUT written"
  cmd_check
}

# one job through the whole path: API -> control plane -> worker -> mock-llm
cmd_check() {
  [ -s "$ENV_OUT" ] || die "not up (no $ENV_OUT); run: lab/lab.sh up"
  key="$(cat "$LAB_STATE/secrets/bot-api-key")"
  out="$(docker run -i --rm --network "$LAB_NETWORK" -e KEY="$key" -e URL="http://${LAB_PROJECT}-processor:8080" \
    python:3.12-alpine python - 2>&1 <<'PY'
import json, os, sys, urllib.request
URL, KEY = os.environ["URL"], os.environ["KEY"]
def call(method, path, body=None):
    r = urllib.request.Request(URL + path, json.dumps(body).encode() if body else None,
        {"Authorization": "Bearer " + KEY, "Content-Type": "application/json"}, method=method)
    return json.load(urllib.request.urlopen(r, timeout=40))
job = call("POST", "/api/v1/jobs", {"job_type": "llm.chat", "input_metadata": {
    "messages": [{"role": "user", "content": "hello"}]}})["data"]
for _ in range(8):
    job = call("GET", "/api/v1/jobs/%s?wait=5" % job["id"])["data"]
    if job["status"] in ("succeeded", "failed", "dead_letter"):
        break
res = job.get("result_metadata") or {}
content = (res.get("content") or "")
print(job["status"], content[:60])
if job["status"] != "succeeded" or not content:
    print(json.dumps(job)[:600], file=sys.stderr)
    sys.exit(1)
PY
  )" || die "smoke job failed: $out"
  say "smoke job ok: $out"
}

cmd_down()   { need_docker; compose down --remove-orphans; rm -f "$ENV_OUT"; say "down (data kept)"; }
cmd_reset()  { need_docker; compose down -v --remove-orphans; rm -rf "$LAB_STATE" "$ENV_OUT"; say "reset"; }
cmd_status() { need_docker; compose ps; }
cmd_logs()   { need_docker; compose logs --no-color --tail 100 "$@"; }

cmd_llm() {
  need_docker
  case "${1:-}" in
    down)   docker stop "${LAB_PROJECT}-mock-llm" >/dev/null; say "mock-llm stopped" ;;
    normal) docker start "${LAB_PROJECT}-mock-llm" >/dev/null 2>&1 || true
            for _ in 1 2 3 4 5 6 7 8 9 10; do mock_mode normal >/dev/null 2>&1 && break; sleep 1; done
            mock_mode normal ;;
    slow)   mock_mode slow "${2:-10}" ;;
    garbage|echo|error) mock_mode "$1" ;;
    *) die "usage: lab.sh llm normal|down|slow [seconds]|garbage|echo|error" ;;
  esac
}

case "${1:-}" in
  up) cmd_up ;;
  down) cmd_down ;;
  status) cmd_status ;;
  logs) shift; cmd_logs "$@" ;;
  reset) cmd_reset ;;
  check) cmd_check ;;
  llm) shift; cmd_llm "$@" ;;
  key) cat "$LAB_STATE/secrets/bot-api-key" ;;
  *) echo "usage: lab/lab.sh up|down|status|logs [service]|reset|check|llm <mode>|key" >&2; exit 2 ;;
esac
