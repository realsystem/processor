# Processor - Split-System Batch Job Platform

[![CI](https://github.com/realsystem/processor/actions/workflows/ci.yml/badge.svg)](https://github.com/realsystem/processor/actions/workflows/ci.yml)

A production-ready split-system application for asynchronous job processing with a public control plane and private data plane worker.

## Architecture Overview

**Control Plane** (Ubuntu VPS, 1GB RAM)
- Public-facing web application and JSON API
- User authentication and authorization
- Job queue management with SQLite
- Webhook and email notifications
- Worker coordination
- Admin interface

**Data Plane** (Local PC with GPU/Ollama)
- Polling worker daemon
- Job execution engine
- Ollama integration adapter
- Heartbeat and status reporting
- No inbound network exposure required

## Key Features

- ✅ User accounts with email/password authentication
- ✅ API key support for programmatic access
- ✅ Durable DB-backed job queue with priority and retry support
- ✅ Webhook notifications with signing and retry
- ✅ Email notifications with retry
- ✅ Worker lease-based coordination
- ✅ Admin interface for operations
- ✅ Audit logging
- ✅ Production-ready security
- ✅ Generic executor interface; `llm.chat` for Ollama and OpenAI-compatible servers
- ✅ Optional per job: idempotency keys, trace context, cancellation, waiting for the result, retention and sensitive data handling ([job options](docs/job-options.md))
- ✅ Optional per-worker Ed25519 identities instead of the shared worker token

## Quick Start

### Local Development

```bash
# Build everything
make build

# Run control plane locally
cd control-plane
make dev

# Run data plane worker locally (in another terminal)
cd data-plane
make dev
```

### Production Deployment

See [docs/deployment.md](docs/deployment.md) for complete production deployment instructions.

## Documentation

- [Architecture](docs/architecture.md) - System design and components
- [API Reference](docs/api.md) - HTTP API documentation
- [Deployment](docs/deployment.md) - Production deployment guide
- [Job options and signed workers](docs/job-options.md) - idempotency, tracing, cancel, retention, `llm.chat`, worker keys
- [Private network deployment](docs/private-network.md) - control plane on any host in a Tailscale/WireGuard network, GPU worker at home
- [Security](docs/security.md) - Security model and considerations
- [Operations](docs/operations.md) - Operating and troubleshooting

## Project Structure

```
/
├── control-plane/     # Public web/API service
├── data-plane/        # Private worker daemon
├── docs/             # Documentation
└── Makefile          # Top-level build targets
```

## Technology Stack

- **Language**: Go 1.21+
- **Database**: SQLite with WAL mode
- **Web Server**: nginx (reverse proxy)
- **Templates**: Go html/template
- **Authentication**: Session cookies + API keys
- **Deployment**: systemd services

## License

Proprietary - Internal use only
