# Processor

- Split-system job platform in Go: a public **control plane** (HTTP API + web UI, SQLite queue) and a private **data plane** worker that polls it and runs jobs (mainly `llm.chat` against Ollama/OpenAI-compatible servers). Used by the Autonomy platform (edge_server, `autonomy`) as its model/job service; must stay a general-purpose job platform.

## Commands
- Two separate Go modules (`control-plane/`, `data-plane/`), no go.work. Run Go commands inside each module; top-level `make` wraps both.
- Build: `make build` (outputs `control-plane/server`, `data-plane/worker`, both git-ignored; `make clean` removes them). Both modules build here (control plane needs the offline module cache, see Gotchas).
- Test all: `make test` (= `go test -v -race -cover ./...` per module). Single test: `cd data-plane && go test -race -run TestLLMChatInputValidation -v ./internal/executor`; same shape in `control-plane`. Verified: `go test -race -count=1 ./...` passes in both modules, `go vet` and `gofmt -l .` are clean.
- Format/vet (what CI enforces): `gofmt -l .` must print nothing; `go vet ./...`; CI also runs `go test -race -count=1 ./...`.
- Run locally: `cd control-plane && make dev` (loads `control-plane/.env` if present), then `cd data-plane && make dev` in another terminal (`make dev` itself unverified).
- Docker stack: `make dev-up` / `dev-down` / `dev-reset` (unverified: no Docker daemon here). CI uses `docker compose`; the Makefile's docker targets call legacy `docker-compose`.
- Lab (control plane + signed worker + fake model): `lab/lab.sh up | check | llm <mode> | down` (unverified, needs Docker).
- E2E (needs the compose stack up): `bash tests/smoke_test.sh`, `bash tests/e2e_job.sh` (`SIGNED=1` with `WORKER_AUTH=signature`).
- Shell scripts: `bash -n scripts/*.sh tests/*.sh lab/*.sh`; CI also runs `shellcheck --severity=warning` (not installed here, unverified). Mock model tests: `python3 lab/mock-llm/test_server.py`.
- Create admin / API key: `cd control-plane && go run scripts/create-admin.go -password <pw>`; `./server -create-api-key <email> -api-key-name <n>` (unverified).

## Architecture
- `control-plane/cmd/server/main.go`: entry point, all routes in one mux; flags `-migrate`, `-create-api-key`, `-register-worker-key <id>=<pubkey>`.
- `control-plane/internal/`: `httpapi` (JSON API, worker endpoints, `workerauth.go` signature/token auth), `web` + `templates/` (session UI), `jobs` (queue, leases, retries, options), `jobtypes` (optional `job_type@version` schemas), `workers`, `auth`, `webhooks`, `email`, `middleware`, `config`, `db` (+ `migrations/NNN_*.sql`, embedded, run on every start).
- `control-plane/cmd/telegram-bot` + `internal/telegrambot`: optional separate process (Telegram front end); uses only the public API (`/api/v1/models`, `/api/v1/jobs`), allowlist of user ids required. `LLM_MODELS`/`LLM_DEFAULT_MODEL` configure the model list (web form dropdown, `GET /api/v1/models`). See `docs/telegram-bot.md`.
- `data-plane/cmd/worker/main.go`: poll loop, heartbeats every 15 s (answer carries cancel), per-job timeout. `internal/executor` (registry: `noop`, `sleep`, `echo_metadata`, `ollama_chat`, `llm.chat`), `internal/controlclient` (HTTP + Ed25519 request signing), `internal/identity` (worker key).
- Flow: client `POST /api/v1/jobs` (API key) -> row in SQLite -> worker `POST /api/v1/worker/poll` with its capabilities, leases a matching job -> executor -> `.../complete` or `.../fail` -> webhook/email/`GET ?wait=10`. Workers connect outbound only; they self-register on first poll (token mode).
- Deployment/ops: `docs/` (see `docs/job-options.md`, `docs/job-types.md`, `docs/private-network.md`, `docs/WINDOWS_WORKER_SETUP.md`); `scripts/` = deploy/Windows helpers; `autonomy.manifest.json` is checked in CI.

## Conventions
- Errors wrapped with `%w`; use `log` not `fmt.Println`; never log job input/results (sensitive jobs: ids and types only).
- Add a DB change as a new numbered migration, never edit an applied one.
- New worker job type: register it in `executor.NewDefaultRegistry` and add the name to the default `WORKER_CAPABILITIES` (config.go, docker-compose.yml, scripts); jobs only reach workers whose capabilities list the type.
- Tests: Go table-driven tests next to the code; keep `-race` clean.
- Branch + PR to `main`; CI must be green; squash-merge. Recent history uses plain imperative titles; `docs/development.md` asks for conventional commits (`feat:`/`fix:`): follow it unless told otherwise.
- In Claude Code web sessions `gh pr create` fails (GraphQL blocked): `gh api repos/realsystem/processor/pulls -f title=... -f head=BRANCH -f base=main -F body=@file`; merge with `gh api -X PUT repos/realsystem/processor/pulls/N/merge -f merge_method=squash`.

## Gotchas
- Control plane needs CGO (go-sqlite3): gcc required, and `-race` needs it in both modules. Module downloads fail in sandboxed sessions without network; the control plane then builds only with a pre-seeded copy of its go.mod/go.sum, e.g. `GOWORK=off GOPROXY=direct GOSUMDB=off GOFLAGS=-modfile=/tmp/gomods/control-plane.go.mod go test ./...` (needs that cache to exist).
- Go toolchain: `go.mod` says 1.21, Dockerfiles use `golang:1.25`, Dependabot ignores `x/crypto`/`x/time` bumps that need newer Go. Don't raise `go` in go.mod casually.
- `make lint` fails today when golangci-lint is installed (9 pre-existing errcheck/staticcheck findings in data-plane); CI does not run it. Don't treat as a regression.
- Env: see `.env.example` (copy to `.env`, git-ignored; `*.env` and `.env.windows` too). `ENV=production` refuses short/default `SESSION_SECRET`/`WORKER_TOKEN` (>=32 chars, no `docker-dev-*`) and `ADMIN_PASSWORD=admin123`. Worker needs `CONTROL_PLANE_URL` plus `WORKER_TOKEN` or `WORKER_KEY_FILE`; `WORKER_AUTH` on the server is `token|signature|any`.
- The control plane never serves its worker token; don't add a route that does. Windows scripts must not assume one (the old `/worker/config` did not exist).
- `lab/mock-llm/__pycache__/*.pyc` is tracked; running the mock-llm tests modifies it. `git checkout --` it, don't commit.
- Secret scan (gitleaks, `.gitleaks.toml`) allowlists `docs/`, `.env.example`, `_test.go`; use placeholders like `pk_XXX` elsewhere.
- Docs partly stale: `docs/architecture.md` and the README describe a public VPS; the owner's setup is the control plane on the edge server over Tailscale, GPU worker polling it (`docs/private-network.md`). Windows PC worker: `scripts/install-windows.ps1`, then `scripts/setup-windows-worker.ps1` (never run on Windows).

## Definition of done
- `gofmt -l .` empty, `go vet ./...` and `go test -race ./...` pass in each module you touched; `bash -n`/shellcheck for changed shell scripts.
- Anything touching Docker, compose, lab or the worker protocol: CI's docker smoke/e2e job (token and signed worker) is green, since it cannot run locally here.
- Docs updated when behavior or env vars change (`docs/`, `.env.example`, compose); no secrets committed.

## GitHub Free-plan limits
This repo is public, so GitHub Actions minutes are free here. The account is on the GitHub Free plan and these limits still apply.

- **Workflows:** keep jobs lean. Use `ubuntu-latest`, add `concurrency` with `cancel-in-progress: true` and `timeout-minutes` to new jobs, and set `retention-days` (3-7) on uploaded artifacts. The account-wide cap is 20 concurrent jobs (5 macOS); extra jobs queue.
- **Files:** never commit a file over 50 MiB (GitHub warns) or 100 MiB (the push is blocked). Keep datasets, video, model weights and build output out of git; use Git LFS only if truly needed.
- **GitHub API:** 5,000 requests/hour authenticated (60 unauthenticated), search 30/min, plus secondary limits. Scripts or agents that call the API must paginate, use conditional requests, and not fan out across repos in parallel.
