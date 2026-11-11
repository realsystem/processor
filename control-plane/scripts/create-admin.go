//go:build ignore

package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	email := flag.String("email", "admin@example.com", "Admin email")
	password := flag.String("password", "admin123", "Admin password")
	dbPath := flag.String("db", "./processor.db", "Database path")
	flag.Parse()

	// Open database
	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Check if admin already exists
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", *email).Scan(&count)
	if err != nil {
		log.Fatalf("Database error: %v", err)
	}

	if count > 0 {
		fmt.Printf("✅ Admin user already exists: %s\n", *email)
		return
	}

	// Generate user ID
	idBytes := make([]byte, 12)
	rand.Read(idBytes)
	userID := "usr_" + hex.EncodeToString(idBytes)

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(*password), 10)
	if err != nil {
		log.Fatalf("Failed to hash password: %v", err)
	}

	// Create admin user
	_, err = db.Exec(`
		INSERT INTO users (id, email, password_hash, plan_tier, is_admin, created_at, updated_at)
		VALUES (?, ?, ?, 'free', 1, datetime('now'), datetime('now'))
	`, userID, *email, string(passwordHash))

	if err != nil {
		log.Fatalf("Failed to create admin user: %v", err)
	}

	fmt.Println("✅ Admin user created successfully!")
	fmt.Println()
	fmt.Printf("📧 Email: %s\n", *email)
	fmt.Printf("🔑 Password: %s\n", *password)
	fmt.Println()
	fmt.Println("⚠️  IMPORTANT: Change this password after first login!")
	fmt.Println()
	fmt.Println("Next step: Create an API key")
	fmt.Printf("  go run scripts/create-api-key.go -email %s -name 'Dev Key'\n", *email)
	fmt.Println()
}
