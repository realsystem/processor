package identity

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "worker.pem")
	k1, created, err := LoadOrCreate(path)
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	k2, created, err := LoadOrCreate(path)
	if err != nil || created || !k1.Equal(k2) {
		t.Fatalf("reload: %v %v", created, err)
	}
	if len(PublicKeyB64(k1)) != 43 || !ed25519.Verify(k1.Public().(ed25519.PublicKey), []byte("x"), ed25519.Sign(k2, []byte("x"))) {
		t.Fatal("public key form")
	}
	os.WriteFile(path, []byte("junk"), 0o600)
	if _, _, err := LoadOrCreate(path); err == nil {
		t.Fatal("junk must not load")
	}
}
