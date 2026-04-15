# User Management Guide

## Creating New Users

There are **three ways** to create new users:

---

## Option 1: Web UI Registration (Recommended)

**Easiest method for end users:**

1. Navigate to: **http://localhost:8080/register**

2. Fill in the registration form:
   - **Email**: user@example.com
   - **Password**: yourpassword
   - **Confirm Password**: yourpassword

3. Click **"Create Account"**

4. You'll be automatically logged in!

**Notes:**
- Users created via web UI are **regular users** (not admins)
- To make a user an admin, see "Managing Users" below

---

## Option 2: Using Make Command (Interactive)

**Best for creating users via CLI:**

```bash
make create-user
```

You'll be prompted for:
- Email address
- Password (hidden input)
- Admin status (y/N)

**Example:**
```bash
$ make create-user
Create New User
===============

Email: developer@example.com
Password: [hidden]
Admin? (y/N): n
✅ User created successfully!
   ID:    usr_1234567890
   Email: developer@example.com
   Role:  regular user
```

---

## Option 3: Using Script Directly

**For scripting and automation:**

```bash
./scripts/create-user.sh <email> <password> [is_admin]
```

**Examples:**

Create a regular user:
```bash
./scripts/create-user.sh user@example.com mypassword 0
```

Create an admin user:
```bash
./scripts/create-user.sh admin@example.com adminpass 1
```

---

## Managing Users

### View All Users

```bash
docker-compose exec control-plane sqlite3 /data/processor.db \
  "SELECT id, email, is_admin, created_at FROM users;"
```

### Make a User Admin

```bash
docker-compose exec control-plane sqlite3 /data/processor.db \
  "UPDATE users SET is_admin = 1 WHERE email = 'user@example.com';"
```

### Remove Admin Privileges

```bash
docker-compose exec control-plane sqlite3 /data/processor.db \
  "UPDATE users SET is_admin = 0 WHERE email = 'user@example.com';"
```

### Disable a User

```bash
docker-compose exec control-plane sqlite3 /data/processor.db \
  "UPDATE users SET disabled_at = datetime('now') WHERE email = 'user@example.com';"
```

### Enable a Disabled User

```bash
docker-compose exec control-plane sqlite3 /data/processor.db \
  "UPDATE users SET disabled_at = NULL WHERE email = 'user@example.com';"
```

### Delete a User

```bash
docker-compose exec control-plane sqlite3 /data/processor.db \
  "DELETE FROM users WHERE email = 'user@example.com';"
```

**Note:** This will also delete all associated jobs, API keys, and webhooks due to foreign key constraints.

---

## User Roles

### Regular User
- Can create and manage their own jobs
- Can create API keys for automation
- Can configure webhooks for notifications
- Cannot see other users' data
- Cannot access admin panel

### Admin User
- All regular user permissions
- Can view all jobs from all users
- Can monitor all workers
- Can see system-wide statistics
- Access to admin panel at `/admin`

---

## Admin Panel Access

Admin users can access the admin panel at:
- **URL**: http://localhost:8080/admin

**Admin panel features:**
- System-wide job statistics
- All users' jobs
- Worker monitoring
- User management

---

## Password Management

Users can change their own passwords through:

1. **Profile page**: http://localhost:8080/profile (future feature)

For now, to reset a user's password via CLI:

```bash
# Generate new password hash
docker-compose exec control-plane sh -c '
  cd /tmp
  cat > hash.go << "EOF"
package main
import (
    "fmt"
    "os"
    "golang.org/x/crypto/bcrypt"
)
func main() {
    hash, _ := bcrypt.GenerateFromPassword([]byte(os.Args[1]), 10)
    fmt.Println(string(hash))
}
EOF
  go run hash.go "newpassword"
  rm hash.go
'

# Copy the hash output, then:
docker-compose exec control-plane sqlite3 /data/processor.db \
  "UPDATE users SET password_hash = '<paste-hash-here>' WHERE email = 'user@example.com';"
```

---

## Common Tasks

### Create Multiple Users (Bulk)

Create a file `users.txt`:
```
user1@example.com,password1,0
user2@example.com,password2,0
admin2@example.com,adminpass,1
```

Then run:
```bash
while IFS=, read -r email password admin; do
  ./scripts/create-user.sh "$email" "$password" "$admin"
done < users.txt
```

### List Active Sessions

```bash
docker-compose exec control-plane sqlite3 /data/processor.db \
  "SELECT u.email, s.created_at, s.last_seen_at 
   FROM sessions s 
   JOIN users u ON s.user_id = u.id 
   WHERE s.expires_at > datetime('now');"
```

### Count Users

```bash
docker-compose exec control-plane sqlite3 /data/processor.db \
  "SELECT 
     COUNT(*) as total,
     SUM(is_admin) as admins,
     COUNT(*) - SUM(is_admin) as regular
   FROM users 
   WHERE disabled_at IS NULL;"
```

---

## Troubleshooting

**"Email already exists" error:**
- The email is already registered
- Check with: `docker-compose exec control-plane sqlite3 /data/processor.db "SELECT email FROM users;"`

**Can't login after creating user:**
- Verify user exists in database
- Check if user is disabled: `SELECT email, disabled_at FROM users WHERE email = 'user@example.com';`
- Try resetting password

**Docker not running:**
- Start services with: `make dev-up`
- Or reset everything: `make dev-reset`
