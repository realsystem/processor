// Package totp implements RFC 6238 time-based one-time passwords (SHA-1, 6 digits, 30 s),
// the format every authenticator app understands.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 default; authenticator apps expect it
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// StepSeconds is the length of one code's validity window.
	StepSeconds = 30
	digits      = 6
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret returns a new random 160-bit secret, base32 encoded.
func GenerateSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate totp secret: %w", err)
	}
	return b32.EncodeToString(buf), nil
}

// DecodeSecret parses a base32 secret, tolerating spaces, dashes, padding and lower case.
func DecodeSecret(s string) ([]byte, error) {
	s = strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "=", "", "\n", "", "\r", "", "\t", "").Replace(s))
	if s == "" {
		return nil, fmt.Errorf("empty totp secret")
	}
	key, err := b32.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode totp secret: %w", err)
	}
	if len(key) < 10 {
		return nil, fmt.Errorf("totp secret too short")
	}
	return key, nil
}

// Step returns the 30-second counter for t.
func Step(t time.Time) int64 { return t.Unix() / StepSeconds }

// Code returns the 6-digit code for a counter value.
func Code(key []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	return fmt.Sprintf("%0*d", digits, bin%1000000)
}

// Match checks code against the steps just before, at and just after now (clock skew of one
// step) and returns the step it matches. All three are compared so timing does not reveal
// which one was close.
func Match(key []byte, code string, now time.Time) (int64, bool) {
	cur := Step(now)
	var matched int64
	found := 0
	for _, s := range []int64{cur - 1, cur, cur + 1} {
		if subtle.ConstantTimeCompare([]byte(Code(key, s)), []byte(code)) == 1 {
			matched = s
			found = 1
		}
	}
	return matched, found == 1
}

// URI returns the otpauth:// URI an authenticator app can import.
func URI(secretB32, issuer, account string) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	q := url.Values{}
	q.Set("secret", secretB32)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return "otpauth://totp/" + label + "?" + q.Encode()
}
