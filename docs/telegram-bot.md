# Telegram bot and model choice

## Model list

The control plane cannot see which models a worker has pulled, so the models it offers are
configured:

| Setting | Default | |
|---|---|---|
| `LLM_MODELS` | `llama3.2,gemma2:2b,qwen2.5:7b,llama3.1:8b,qwen2.5:14b` | Comma-separated. Pull each one on the worker (`ollama pull <name>`). |
| `LLM_DEFAULT_MODEL` | `llama3.2` | Preselected; always part of the list. |

The list is used in two places:

- **Web UI**: *Create Job* has an `LLM Chat` job type with a model dropdown, a prompt and an
  optional system prompt. The server refuses a model that is not in the list.
- **API**: `GET /api/v1/models` (API key) returns `{"data": {"models": [...], "default": "..."}}`.
  Clients such as the bot read it. Submitting a job through the API is not restricted to
  the list: put any model the worker has in `input_metadata.model`.

## Telegram bot

`telegram-bot` is a separate process that talks to the control plane only through its public
API. Run it next to the control plane, on another machine, or not at all; stopping it does
not affect the server or the web UI. Chatting with it submits `llm.chat` jobs, so a worker
must be connected.

### Setup

1. Create a bot with [@BotFather](https://t.me/BotFather) (`/newbot`) and keep the token.
2. Create an API key for the bot:
   `docker compose -f docker-compose.deploy.yml exec control-plane ./server -create-api-key you@example.com -api-key-name telegram`
3. Find your Telegram user id. Either ask [@userinfobot](https://t.me/userinfobot), or start the
   bot with a placeholder such as `TELEGRAM_ALLOWED_USERS=1` and message it: it replies with
   your id. Every message is a GPU job, so the bot refuses to start without an allowlist.
4. Put the values in `.env`:
   ```
   TELEGRAM_BOT_TOKEN=<token from BotFather>
   TELEGRAM_ALLOWED_USERS=<your id>,<another id>
   PROCESSOR_API_KEY=<key from step 2>
   ```
5. Start it: `docker compose -f docker-compose.deploy.yml --profile telegram up -d`
   (or run `./telegram-bot` from the image or `go run ./cmd/telegram-bot` with the same
   variables and `PROCESSOR_URL=https://your-control-plane`).

| Variable | Default | |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | required | |
| `TELEGRAM_ALLOWED_USERS` | required | Comma-separated Telegram user ids. |
| `PROCESSOR_API_KEY` | required | |
| `PROCESSOR_URL` | `http://localhost:8080` | |
| `BOT_DEFAULT_MODEL` | the control plane's default | Used until a chat picks a model. |
| `BOT_JOB_TIMEOUT` | `15m` | How long to wait for one answer; the job keeps running. |
| `BOT_SENSITIVE` | `true` | Marks jobs sensitive: kept out of webhooks and e-mails, erased after the retention period. |

### Using it

- Send a message: it is answered by the chat's current model.
- `/models` shows buttons to pick the model (✓ marks the current one); `/model <name>` does
  the same by name. The choice is per chat and resets when the bot restarts.
- `@qwen2.5:7b why is the sky blue?` uses another model for that one question.
- `/ask <question>` is the same as a plain message (handy in groups).

The worker runs one job at a time, so questions queue behind each other, and at most three
may be unanswered per chat. On a CPU, a 7B model answers at a few tokens per second.

### Security

- The allowlist is by Telegram **user id**; an unknown user gets a refusal that names their id and
  nothing is submitted.
- The bot never logs prompts or answers, only chat ids, job ids and models. Errors from
  Telegram are stripped of the bot token.
- Telegram sees every prompt and answer (it is not end-to-end encrypted for bots). Use the web UI
  for anything that must not leave your own infrastructure.
