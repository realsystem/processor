# Job options, `llm.chat` and signed workers

Everything here is optional. A client that sends none of it gets the original behaviour.

## Submitting jobs: `POST /api/v1/jobs`

New fields in the body; two of them can also be set by standard headers, which win over the body:

| Field / header | Effect |
|---|---|
| `idempotency_key` / `Idempotency-Key` | If you submit again with the same key (per user), you get the first job back with `"idempotent_replay": true`. If the job type differs: `409 idempotency_conflict`. Safe for retries after timeouts. |
| `traceparent` header | W3C trace context. It is stored with the job and handed to the worker, so the job joins the caller's trace. Malformed values are dropped. |
| `correlation_id` / `X-Correlation-Id` | Stored and handed to the worker; free-form, up to 200 characters. |
| `sensitive` | Input, result and error text are never put in webhooks or e-mails, and the worker logs only the job id and type. The data is erased after `retention_seconds` (default 24 h). |
| `retention_seconds` | Once the job is finished, input, result and error text are erased after this many seconds (`purged_at` is set; status and timings stay). |

```bash
curl -X POST "$URL/api/v1/jobs" -H "Authorization: Bearer $KEY" \
  -H "Idempotency-Key: assess-2026-11-11" \
  -H "traceparent: 00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01" \
  -d '{"job_type": "llm.chat", "sensitive": true,
       "input_metadata": {"messages": [{"role": "user", "content": "Summarise …"}]}}'
```

## Waiting, cancelling, looking around

| Endpoint | |
|---|---|
| `GET /api/v1/jobs/{id}?wait=10` | Waits up to 10 s (the maximum) for the job to finish before answering. |
| `POST /api/v1/jobs/{id}/cancel` | A queued job is canceled at once. A running job is canceled at its worker's next heartbeat (within about 15 s), and the worker stops it and drops its result. A finished job gives `409 not_cancelable`. |
| `GET /api/v1/stats` | Jobs by status, how long the oldest queued job has waited, and the online workers with their capabilities. |
| `GET /api/v1/job-types` | Job types some online worker (seen in the last 2 minutes) can run. |

A worker that stops sending heartbeats in the middle of a job no longer leaves the job `running` forever. When its lease expires, the job counts as a failed attempt and is requeued, or dead-lettered when it has no attempts left.

## `llm.chat`

A chat completion on the worker's model server, with structured output and token usage.

| Worker setting | Default | |
|---|---|---|
| `LLM_BACKEND` | `ollama` | `ollama` uses `/api/chat`. `openai` uses `/v1/chat/completions` on any OpenAI-compatible server (llama.cpp, vLLM, LM Studio, hosted APIs). |
| `LLM_BASE_URL` | `OLLAMA_BASE_URL` | |
| `LLM_API_KEY` / `LLM_API_KEY_FILE` | — | Sent as a bearer token (for the `openai` backend). |
| `LLM_DEFAULT_MODEL` | `llama3.2` | Used when the job names no model. |
| `JOB_TIMEOUT` | `5m` | Applies to every job type. |

Input:
```json
{"model": "qwen2.5:14b",
 "system": "You are …",
 "messages": [{"role": "user", "content": "…"}],
 "format": {"type": "object", "properties": {"verdict": {"type": "string"}}},
 "options": {"temperature": 0.2, "max_tokens": 512},
 "keep_alive": "10m"}
```
- `messages` can be replaced by `"prompt": "…"`.
- `format` is `"json"` or a JSON schema.
- `keep_alive` is used by Ollama only.

Result:
```json
{"status": "completed", "backend": "ollama", "model": "qwen2.5:14b",
 "message": {"role": "assistant", "content": "…"}, "content": "…",
 "done_reason": "stop", "usage": {"prompt_tokens": 120, "completion_tokens": 40},
 "duration_seconds": 3.1}
```
The worker never logs prompts or answers. Error messages carry the model server's status and at most 300 bytes of its reply, never the request. The older `ollama_chat` job type is unchanged, except that it no longer logs the prompt.

## Signed workers

By default, workers authenticate with the shared `WORKER_TOKEN`. With per-worker keys, each worker has its own Ed25519 key, can act only as itself, and can be revoked on its own.

1. On the worker, set `WORKER_KEY_FILE=/var/lib/processor/worker.pem`. The key is created at first start, and the worker logs its public key. You can also print it with `worker -print-public-key`.
2. On the control plane, run `server -register-worker-key <WORKER_ID>=<public key> -capabilities llm.chat`.
3. Set `WORKER_AUTH` on the control plane:
   - `signature`: keys only;
   - `any`: tokens or keys, useful while you move workers over one at a time;
   - `token`: the default.

A signed request carries the headers `X-Worker-Id`, `X-Worker-Timestamp` (unix seconds, within ±2 minutes) and `X-Worker-Signature`. The signature is base64url Ed25519 over `METHOD\nPATH\nTIMESTAMP\nhex(sha256(body))`. The `worker_id` in the body must match `X-Worker-Id`.

With no `WORKER_TOKEN` configured, token authentication now refuses every request. Before this change, a request without a token was accepted when no token was configured.

## Admin commands

```bash
server -migrate                                  # migrations (they also run on every start)
server -create-api-key owner@example.com -api-key-name my-app   # prints pk_…
server -register-worker-key gpu-1=<public key> -capabilities llm.chat,ollama_chat
```
