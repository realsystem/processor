// Package identity keeps a worker's Ed25519 key for signed requests to the control plane.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LoadOrCreate reads the PEM (PKCS#8) Ed25519 key at path, creating it (mode 0600) if the
// file does not exist.
func LoadOrCreate(path string) (ed25519.PrivateKey, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, false, err
		}
		der, err := x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			return nil, false, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, false, err
		}
		pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
		if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
			return nil, false, err
		}
		return priv, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, false, fmt.Errorf("%s: no PEM block", path)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, false, fmt.Errorf("%s: not an Ed25519 key", path)
	}
	return priv, false, nil
}

// PublicKeyB64 is the form the control plane registers (-register-worker-key).
func PublicKeyB64(priv ed25519.PrivateKey) string {
	return base64.RawURLEncoding.EncodeToString(priv.Public().(ed25519.PublicKey))
}
