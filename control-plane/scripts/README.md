# Helper Scripts

## create-admin.go

Creates an admin user in the database. Use this for initial setup or if the automatic admin creation from environment variables doesn't work.

### Usage

```bash
go run scripts/create-admin.go -password <password> [-email <email>] [-db <db-path>]
```

### Examples

**Create a local development admin** (the password is required; there is no default):
```bash
go run scripts/create-admin.go -password admin123
# Creates: admin@example.com / admin123 (development only; production refuses this password)
```

**Create admin with custom credentials:**
```bash
go run scripts/create-admin.go -email admin@mydomain.com -password SecurePass123
```

**Use a different database path:**
```bash
go run scripts/create-admin.go -db /var/lib/processor/processor.db
```

### Default Values

- Email: `admin@example.com`
- Password: `admin123`
- Database: `./processor.db`

### Output

```
✅ Admin user created successfully!

📧 Email: admin@example.com
🔑 Password: admin123

⚠️  IMPORTANT: Change this password after first login!

Next step: Create an API key
  go run scripts/create-api-key.go -email admin@example.com -name 'Dev Key'
```

### Notes

- If the admin user already exists, the script will exit without error
- The password is hashed with bcrypt before storage
- The user is created with `is_admin = true`

---

## create-api-key.go

Creates an API key for a user without requiring direct database access or authentication.

### Usage

```bash
go run scripts/create-api-key.go -email <user-email> [-name <key-name>] [-db <db-path>]
```

### Examples

**Create a key for the admin user:**
```bash
go run scripts/create-api-key.go -email admin@example.com -name "Development Key"
```

**Create a key for another user:**
```bash
go run scripts/create-api-key.go -email user@example.com -name "Production API Key"
```

**Use a different database path:**
```bash
go run scripts/create-api-key.go -email admin@example.com -db /var/lib/processor/processor.db
```

### Requirements

- The database must exist (run `make migrate` first)
- The user must exist in the database
- The server must have been started at least once to create the admin user

### Output

The script will output the full API key. **Save it immediately** - it won't be shown again!

```
✅ API Key created successfully!

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔑 API Key: pk_1a2b3c4d5e6f7890_9876543210fedcba9876543210fedcba
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠️  IMPORTANT: Save this key now! It won't be shown again.

Usage:
  curl -H "Authorization: Bearer pk_1a2b3c4d5e6f7890_..." \
       http://localhost:8080/api/v1/me
```

### Security Notes

- API keys are hashed with bcrypt (cost 10) before storage
- Only the key prefix (first 24 chars) is stored in plaintext
- Keys cannot be retrieved after creation - only revoked
- This script should only be used in development or for administrative purposes
- In production, users should create API keys through the web UI (once implemented)
