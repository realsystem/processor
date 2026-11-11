package httpapi

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

// The same vector is in control-plane/internal/httpapi and data-plane/internal/controlclient:
// worker and control plane must agree on what is signed.
func TestSignaturePayloadVector(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	payload := SignaturePayload("POST", "/api/v1/worker/poll", "1700000000", []byte(`{"worker_id":"wrk_1"}`))
	if got := base64.RawURLEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)); got != "A6EHv_POEL4dcN0Y50vAmWfk1jCbpQ1fHdyGZBJVMbg" {
		t.Fatalf("public key %s", got)
	}
	want := "K3tuG1XEW_WWyXfUr8_4Us_rI51UdumDNco75XHOqgoHdBcDw99xqIOm5I-lZA6YgJH6daYBDpOVr6F-JIaTBg"
	if got := base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, payload)); got != want {
		t.Fatalf("signature %s", got)
	}
}
