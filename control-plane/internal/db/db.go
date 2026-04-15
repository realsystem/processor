package db

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens a database connection with proper SQLite configuration
func Open(dbPath string) (*sql.DB, error) {
	// Configure SQLite with WAL mode, foreign keys, and busy timeout
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&_synchronous=NORMAL", dbPath)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Connection pool settings
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	// Verify connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// Migrate runs all pending migrations
func Migrate(db *sql.DB) error {
	log.Println("Running database migrations...")

	// Read migration files
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// Sort migrations by filename
	var migrationFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			migrationFiles = append(migrationFiles, entry.Name())
		}
	}
	sort.Strings(migrationFiles)

	// Run each migration
	for _, filename := range migrationFiles {
		log.Printf("Applying migration: %s", filename)

		content, err := migrationsFS.ReadFile(filepath.Join("migrations", filename))
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", filename, err)
		}

		// Execute migration - ignore "already exists" errors since migrations should be idempotent
		if _, err := db.Exec(string(content)); err != nil {
			// Check if error is about something already existing (idempotent migrations)
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "already exists") {
				log.Printf("Migration %s: skipping (already applied)", filename)
				continue
			}
			return fmt.Errorf("failed to apply migration %s: %w", filename, err)
		}
	}

	log.Println("Migrations completed successfully")
	return nil
}

// MigrateFromFile runs migrations from a file path (used when not embedding)
func MigrateFromFile(db *sql.DB, migrationPath string) error {
	log.Printf("Running migration from file: %s", migrationPath)

	content, err := os.ReadFile(migrationPath)
	if err != nil {
		return fmt.Errorf("failed to read migration file: %w", err)
	}

	if _, err := db.Exec(string(content)); err != nil {
		return fmt.Errorf("failed to apply migration: %w", err)
	}

	log.Println("Migration completed successfully")
	return nil
}
