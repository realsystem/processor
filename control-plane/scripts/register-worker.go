package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	workerID := flag.String("id", "worker_1", "Worker ID")
	workerName := flag.String("name", "local-worker", "Worker name")
	token := flag.String("token", "dev-worker-token-change-in-production", "Worker token")
	dbPath := flag.String("db", "./processor.db", "Database path")
	flag.Parse()

	// Open database
	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Check if worker already exists
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM workers WHERE id = ?", *workerID).Scan(&count)
	if err != nil {
		log.Fatalf("Database error: %v", err)
	}

	if count > 0 {
		fmt.Printf("✅ Worker already registered: %s\n", *workerID)
		return
	}

	// Hash token
	tokenHash, err := bcrypt.GenerateFromPassword([]byte(*token), 10)
	if err != nil {
		log.Fatalf("Failed to hash token: %v", err)
	}

	// Capabilities
	capabilities := []string{"noop", "sleep", "echo_metadata", "ollama_chat"}
	capJSON, _ := json.Marshal(capabilities)

	// Register worker
	_, err = db.Exec(`
		INSERT INTO workers (id, name, auth_token_hash, status, capabilities_json, version, last_seen_at, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, '1.0.0', datetime('now'), datetime('now'), datetime('now'))
	`, *workerID, *workerName, string(tokenHash), string(capJSON))

	if err != nil {
		log.Fatalf("Failed to register worker: %v", err)
	}

	fmt.Println("✅ Worker registered successfully!")
	fmt.Println()
	fmt.Printf("📋 Worker ID: %s\n", *workerID)
	fmt.Printf("📝 Name: %s\n", *workerName)
	fmt.Printf("🔧 Capabilities: %v\n", capabilities)
	fmt.Println()
	fmt.Println("The worker can now poll for jobs!")
	fmt.Println()
}
