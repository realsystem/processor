# Processor - Split-System Batch Job Platform

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
- ✅ Generic executor interface ready for Ollama and other batch tasks

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
