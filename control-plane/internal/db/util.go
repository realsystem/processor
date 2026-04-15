package db

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateID generates a random ID with the given prefix
func GenerateID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}

// GenerateToken generates a random token
func GenerateToken(bytes int) string {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// GenerateAPIKey generates an API key with prefix and secret parts
func GenerateAPIKey() (fullKey, prefix string) {
	prefixBytes := make([]byte, 8)
	secretBytes := make([]byte, 16)

	if _, err := rand.Read(prefixBytes); err != nil {
		panic(err)
	}
	if _, err := rand.Read(secretBytes); err != nil {
		panic(err)
	}

	prefix = "pk_" + hex.EncodeToString(prefixBytes)
	fullKey = prefix + "_" + hex.EncodeToString(secretBytes)
	return fullKey, prefix
}
