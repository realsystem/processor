# Configuration Guide

All configuration is managed through environment variables stored in a `.env` file.

## Quick Start

1. **Copy the example file:**
   ```bash
   cp .env.example .env
   ```

2. **Edit the file with your settings:**
   ```bash
   nano .env  # or use your preferred editor
   ```

3. **Restart services:**
   ```bash
   make dev-down
   make dev-up
   ```

## Security

- ✅ `.env` is gitignored - safe to store secrets
- ✅ Never commit `.env` to version control
- ✅ Share `.env.example` (without secrets) instead
- ✅ Each developer has their own `.env` file

## Configuration Variables

### Email / SMTP Settings

Configure email notifications:

```bash
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your-email@gmail.com
SMTP_PASS=your-app-password
SMTP_FROM=your-email@gmail.com
```

**Optional:** If not configured, emails will be logged to console instead.

See [EMAIL_SETUP.md](EMAIL_SETUP.md) for provider-specific instructions.

### Security Settings

```bash
# Session secret for cookie encryption (change in production!)
SESSION_SECRET=change-this-to-a-random-string-in-production

# Worker authentication token (change in production!)
WORKER_TOKEN=change-this-to-a-random-string
```

**Important:** Generate random secrets for production:
```bash
# Generate a random secret
openssl rand -base64 32
```

### Admin User

Default admin account created on first startup:

```bash
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=admin123
```

**Important:** Change these before deploying to production!

### Application Settings

```bash
# Environment: development, staging, production
ENV=development

# HTTP server port
HTTP_PORT=8080

# Log level: debug, info, warn, error
LOG_LEVEL=info

# How long jobs can be leased before being reclaimed
LEASE_DURATION=5m

# SQLite database file path (inside container)
DATABASE_PATH=/data/processor.db
```

## Environment-Specific Configuration

### Development

Use the default `.env` file:

```bash
ENV=development
LOG_LEVEL=debug
SESSION_SECRET=dev-secret-ok-to-use
```

### Production

Create a production-specific `.env.production`:

```bash
ENV=production
LOG_LEVEL=info
SESSION_SECRET=<generate-with-openssl-rand>
WORKER_TOKEN=<generate-with-openssl-rand>
SMTP_HOST=smtp.your-provider.com
# ... etc
```

Then use it:
```bash
cp .env.production .env
docker-compose up -d
```

## Checking Configuration

View loaded environment variables:

```bash
# Check SMTP settings
docker-compose exec control-plane printenv | grep SMTP

# Check all settings
docker-compose exec control-plane printenv
```

## Troubleshooting

### Changes not taking effect

Restart the services after editing `.env`:

```bash
make dev-down
make dev-up
```

### Missing .env file

Copy from example:

```bash
cp .env.example .env
```

### Accidentally committed .env

1. Remove it from git:
   ```bash
   git rm --cached .env
   git commit -m "Remove .env from git"
   ```

2. Ensure `.env` is in `.gitignore`:
   ```bash
   echo ".env" >> .gitignore
   git add .gitignore
   git commit -m "Add .env to gitignore"
   ```

3. Rotate all secrets (they're now public!)

## Best Practices

1. **Never commit `.env`** - it contains secrets
2. **Keep `.env.example` updated** - document all variables
3. **Use different secrets per environment** - dev, staging, prod
4. **Rotate secrets regularly** - especially if exposed
5. **Use environment-specific files** - `.env.production`, `.env.staging`
6. **Document defaults** - in `.env.example`
7. **Use strong random secrets** - `openssl rand -base64 32`

## See Also

- [EMAIL_SETUP.md](EMAIL_SETUP.md) - SMTP provider configuration
- [USER_MANAGEMENT.md](USER_MANAGEMENT.md) - Managing users
- [QUICKSTART.md](QUICKSTART.md) - Getting started guide
