#!/bin/bash
# End-to-end: create an API key, submit a job, let the worker run it, read the result.
# Needs the docker compose stack (control-plane + worker) running.
set -euo pipefail

URL="${CONTROL_PLANE_URL:-http://localhost:8080}"
EMAIL="${ADMIN_EMAIL:-admin@example.com}"

if [[ "${SIGNED:-0}" == "1" ]]; then
    # the worker signs its requests: register its public key with the control plane
    pub=$(docker compose exec -T worker ./worker -print-public-key | tail -n 1)
    docker compose exec -T control-plane ./server -register-worker-key "worker_docker=$pub" -capabilities echo_metadata
fi

KEY=$(docker compose exec -T control-plane ./server -create-api-key "$EMAIL" -api-key-name e2e | tail -n 1)
[[ "$KEY" == pk_* ]] || { echo "no API key: $KEY"; exit 1; }

job=$(curl -fsS -X POST "$URL/api/v1/jobs" -H "Authorization: Bearer $KEY" \
    -H "Content-Type: application/json" -H "Idempotency-Key: e2e-$$-${SIGNED:-0}" \
    -d '{"job_type":"echo_metadata","input_metadata":{"hello":"world"}}')
id=$(echo "$job" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["id"])')
echo "submitted $id"

for _ in $(seq 1 12); do
    status=$(curl -fsS "$URL/api/v1/jobs/$id?wait=10" -H "Authorization: Bearer $KEY" \
        | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["status"])')
    [[ "$status" == "succeeded" ]] && { echo "job $id succeeded"; exit 0; }
    [[ "$status" == "failed" || "$status" == "dead_letter" ]] && { echo "job $id $status"; exit 1; }
done
echo "job $id did not finish (last status: $status)"
exit 1
