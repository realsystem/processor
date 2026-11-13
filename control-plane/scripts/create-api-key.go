//go:build ignore

package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	email := flag.String("email", "", "User email (required)")
	name := flag.String("name", "CLI Generated Key", "API key name")
	dbPath := flag.String("db", "./processor.db", "Database path")
	flag.Parse()

	if *email == "" {
		fmt.Println("Usage: go run scripts/create-api-key.go -email admin@example.com [-name 'My Key'] [-db ./processor.db]")
		os.Exit(1)
	}

	// Open database
	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Get user ID
	var userID string
	err = db.QueryRow("SELECT id FROM users WHERE email = ?", *email).Scan(&userID)
	if err == sql.ErrNoRows {
		fmt.Printf("Error: User with email '%s' not found\n\n", *email)
		fmt.Println("Available users:")

		rows, _ := db.Query("SELECT email, is_admin FROM users")
		defer rows.Close()

		for rows.Next() {
			var email string
			var isAdmin bool
			rows.Scan(&email, &isAdmin)
			adminStr := ""
			if isAdmin {
				adminStr = " (admin)"
			}
			fmt.Printf("  - %s%s\n", email, adminStr)
		}
		os.Exit(1)
	} else if err != nil {
		log.Fatalf("Database error: %v", err)
	}

	// Generate API key
	fullKey, prefix, hash, keyID := generateAPIKey()

	// Insert into database
	_, err = db.Exec(`
		INSERT INTO api_keys (id, user_id, name, key_prefix, key_hash, created_at)
		VALUES (?, ?, ?, ?, ?, datetime('now'))
	`, keyID, userID, *name, prefix, hash)

	if err != nil {
		log.Fatalf("Failed to create API key: %v", err)
	}

	// Success!
	fmt.Println("✅ API Key created successfully!")
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("🔑 API Key: %s\n", fullKey)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("⚠️  IMPORTANT: Save this key now! It won't be shown again.")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Printf("  curl -H \"Authorization: Bearer %s\" \\\n", fullKey)
	fmt.Println("       http://localhost:8080/api/v1/me")
	fmt.Println()
}

func generateAPIKey() (fullKey, prefix, hash, keyID string) {
	// Generate random bytes
	prefixBytes := make([]byte, 8)
	secretBytes := make([]byte, 16)
	idBytes := make([]byte, 12)

	// Use crypto/rand
	rand.Read(prefixBytes)
	rand.Read(secretBytes)
	rand.Read(idBytes)

	prefix = "pk_" + hex.EncodeToString(prefixBytes)
	secret := hex.EncodeToString(secretBytes)
	fullKey = prefix + "_" + secret
	keyID = "key_" + hex.EncodeToString(idBytes)

	// Hash the full key
	hashBytes, _ := bcrypt.GenerateFromPassword([]byte(fullKey), 10)
	hash = string(hashBytes)

	return
}
