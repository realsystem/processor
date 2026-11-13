# Private network deployment

Processor does not need a public VPS. Workers poll the control plane outbound, so the control plane can run on any machine that the workers can reach. A private network such as Tailscale or WireGuard is enough, with no port open to the internet.

Typical layout:

| Machine | Runs | Notes |
|---|---|---|
| A small always-on server | control plane (`ghcr.io/<owner>/processor-control-plane:<tag>`) | SQLite in a volume; the port is published only on the LAN or tailnet interface |
| A machine with a GPU | worker (`ghcr.io/<owner>/processor-worker:<tag>`) + Ollama | Polls the control plane over the tailnet; Ollama stays on localhost |

## Control plane

```yaml
services:
  processor:
    image: ghcr.io/realsystem/processor-control-plane:sha-abc1234
    environment:
      ENV: production
      WORKER_AUTH: signature
      ADMIN_EMAIL: owner@example.com
      ADMIN_PASSWORD: ${PROCESSOR_ADMIN_PASSWORD}
      SESSION_SECRET: ${PROCESSOR_SESSION_SECRET}
    volumes: [processor-data:/data]
    ports: ["8090:8080"]               # reach it as http://<tailnet name>:8090
```
- Then create an API key for your client with `docker compose exec processor ./server -create-api-key owner@example.com`.
- Register each worker's key with `./server -register-worker-key gpu-1=<key> -capabilities llm.chat`.

## GPU worker

```bash
docker run -d --name processor-worker --restart unless-stopped --network host \
  -e CONTROL_PLANE_URL=http://edge-1.tailnet.ts.net:8090 \
  -e WORKER_ID=gpu-1 -e WORKER_KEY_FILE=/keys/worker.pem \
  -e WORKER_CAPABILITIES=llm.chat,ollama_chat \
  -e LLM_BASE_URL=http://127.0.0.1:11434 -e LLM_DEFAULT_MODEL=qwen2.5:14b \
  -e JOB_TIMEOUT=10m \
  -v processor-worker-keys:/keys \
  ghcr.io/realsystem/processor-worker:sha-abc1234
docker logs processor-worker | grep "public key"   # register this on the control plane
```

The worker keeps nothing but its key, and it picks jobs up again when the machine wakes. Jobs submitted while it was asleep wait in the queue.

## Data location

Job inputs and results are stored on the control plane until they are purged. Choose the control plane's machine with that in mind, and submit private work with `"sensitive": true` (purged after 24 h by default) or an explicit `retention_seconds`.
