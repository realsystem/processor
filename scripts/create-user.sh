#!/bin/bash
# Create a new user directly in the database

EMAIL="$1"
PASSWORD="$2"
IS_ADMIN="${3:-0}"  # Optional: 1 for admin, 0 for regular user (default)

if [ -z "$EMAIL" ] || [ -z "$PASSWORD" ]; then
    echo "Usage: $0 <email> <password> [is_admin]"
    echo ""
    echo "Arguments:"
    echo "  email      - User email address"
    echo "  password   - User password"
    echo "  is_admin   - Optional: 1 for admin, 0 for regular user (default: 0)"
    echo ""
    echo "Examples:"
    echo "  $0 user@example.com mypassword123"
    echo "  $0 admin@example.com adminpass 1"
    exit 1
fi

# Check if running in Docker
if docker-compose ps | grep -q "processor-control-plane"; then
    echo "Creating user in Docker environment..."

    # Use the control-plane container to run a Go script that creates the user
    docker-compose exec -T control-plane sh -c "cat > /tmp/create-user.go << 'EOFGO'
package main

import (
    \"database/sql\"
    \"fmt\"
    \"os\"
    \"time\"

    _ \"github.com/mattn/go-sqlite3\"
    \"golang.org/x/crypto/bcrypt\"
)

func generateID(prefix string) string {
    return fmt.Sprintf(\"%s_%d\", prefix, time.Now().UnixNano())
}

func main() {
    if len(os.Args) < 3 {
        fmt.Println(\"Usage: create-user <email> <password> [is_admin]\")
        os.Exit(1)
    }

    email := os.Args[1]
    password := os.Args[2]
    isAdmin := \"0\"
    if len(os.Args) > 3 {
        isAdmin = os.Args[3]
    }

    db, err := sql.Open(\"sqlite3\", \"/data/processor.db\")
    if err != nil {
        fmt.Printf(\"Failed to open database: %v\n\", err)
        os.Exit(1)
    }
    defer db.Close()

    // Hash password
    hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
    if err != nil {
        fmt.Printf(\"Failed to hash password: %v\n\", err)
        os.Exit(1)
    }

    // Create user
    userID := generateID(\"usr\")
    query := \`
        INSERT INTO users (id, email, password_hash, plan_tier, is_admin, created_at, updated_at)
        VALUES (?, ?, ?, 'free', ?, datetime('now'), datetime('now'))
    \`

    _, err = db.Exec(query, userID, email, string(hash), isAdmin)
    if err != nil {
        fmt.Printf(\"Failed to create user: %v\n\", err)
        os.Exit(1)
    }

    role := \"regular user\"
    if isAdmin == \"1\" {
        role = \"admin\"
    }

    fmt.Printf(\"✅ User created successfully!\n\")
    fmt.Printf(\"   ID:    %s\n\", userID)
    fmt.Printf(\"   Email: %s\n\", email)
    fmt.Printf(\"   Role:  %s\n\", role)
}
EOFGO

cd /tmp && go run create-user.go '$EMAIL' '$PASSWORD' '$IS_ADMIN'
rm /tmp/create-user.go
"
else
    echo "Error: Docker containers not running. Start with 'make dev-up' first."
    exit 1
fi
