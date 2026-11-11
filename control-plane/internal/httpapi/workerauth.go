package httpapi

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/processor/control-plane/internal/auth"
)

// Worker requests are authenticated in one of two ways (WORKER_AUTH):
//
//   - token: the shared X-Worker-Token header (the original scheme);
//   - signature: per-worker Ed25519 keys. The worker sends X-Worker-Id,
//     X-Worker-Timestamp (unix seconds) and X-Worker-Signature, the base64url
//     signature of SignaturePayload. The public key is registered beforehand with
//     `server -register-worker-key`. A signed worker can only act as itself: the
//     worker_id in the body must equal X-Worker-Id.
//
// "any" accepts both, which lets a fleet move from the token to keys one worker at a time.

// SignatureWindow is how far a signed request's timestamp may be from the server clock.
const SignatureWindow = 2 * time.Minute

const maxWorkerBody = 10 << 20

// SignaturePayload is what a worker signs: method, path, timestamp and the SHA-256 of
// the body, separated by newlines.
func SignaturePayload(method, path, timestamp string, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(method + "\n" + path + "\n" + timestamp + "\n" + hex.EncodeToString(sum[:]))
}

// SetWorkerAuth selects the worker authentication mode: token, signature or any.
func (api *API) SetWorkerAuth(mode string) { api.workerAuth = mode }

// authWorker authenticates a worker request and returns its body. signedID is the
// worker id proven by a signature ("" for token authentication). On failure it has
// already written the error response.
func (api *API) authWorker(w http.ResponseWriter, r *http.Request) (body []byte, signedID string, ok bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWorkerBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Cannot read body")
		return nil, "", false
	}
	mode := api.workerAuth
	if mode == "" {
		mode = "token"
	}
	if sig := r.Header.Get("X-Worker-Signature"); sig != "" && mode != "token" {
		id, err := api.verifyWorkerSignature(r, body, sig)
		if err != "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", err)
			return nil, "", false
		}
		return body, id, true
	}
	if mode != "signature" && api.workerToken != "" && auth.SecureCompare(r.Header.Get("X-Worker-Token"), api.workerToken) {
		return body, "", true
	}
	writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid worker credentials")
	return nil, "", false
}

func (api *API) verifyWorkerSignature(r *http.Request, body []byte, sig string) (string, string) {
	id := r.Header.Get("X-Worker-Id")
	ts := r.Header.Get("X-Worker-Timestamp")
	if id == "" || ts == "" {
		return "", "X-Worker-Id and X-Worker-Timestamp are required with a signature"
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return "", "bad X-Worker-Timestamp"
	}
	if d := time.Since(time.Unix(sec, 0)); d > SignatureWindow || d < -SignatureWindow {
		return "", "X-Worker-Timestamp outside the allowed window"
	}
	keyB64, err := api.workerService.PublicKey(id)
	if err != nil || keyB64 == "" {
		return "", "no public key registered for this worker"
	}
	key, err := decodeB64(keyB64)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return "", "registered public key is malformed"
	}
	raw, err := decodeB64(sig)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(key), SignaturePayload(r.Method, r.URL.Path, ts, body), raw) {
		return "", "bad signature"
	}
	return id, ""
}

// sameWorker rejects a signed request that names another worker in its body.
func sameWorker(w http.ResponseWriter, signedID, bodyID string) bool {
	if signedID != "" && signedID != bodyID {
		writeError(w, http.StatusForbidden, "forbidden", "worker_id does not match the signing worker")
		return false
	}
	return true
}

func decodeB64(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.StdEncoding, base64.RawStdEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, base64.CorruptInputError(0)
}
