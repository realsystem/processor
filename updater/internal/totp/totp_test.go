package totp

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B vectors for SHA-1, reduced to 6 digits.
func TestCodeRFC6238(t *testing.T) {
	key := []byte("12345678901234567890")
	tests := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}
	for _, tt := range tests {
		if got := Code(key, Step(time.Unix(tt.unix, 0))); got != tt.want {
			t.Errorf("Code at %d = %s, want %s", tt.unix, got, tt.want)
		}
	}
}

func TestMatchWindow(t *testing.T) {
	key := []byte("12345678901234567890")
	now := time.Unix(1234567890, 0)
	cur := Step(now)
	tests := []struct {
		name     string
		code     string
		wantOK   bool
		wantStep int64
	}{
		{"current", Code(key, cur), true, cur},
		{"previous step (clock skew)", Code(key, cur-1), true, cur - 1},
		{"next step (clock skew)", Code(key, cur+1), true, cur + 1},
		{"two steps old", Code(key, cur-2), false, 0},
		{"two steps ahead", Code(key, cur+2), false, 0},
		{"garbage", "000000x", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step, ok := Match(key, tt.code, now)
			if ok != tt.wantOK || (ok && step != tt.wantStep) {
				t.Fatalf("Match = (%d, %v), want (%d, %v)", step, ok, tt.wantStep, tt.wantOK)
			}
		})
	}
}

func TestSecretRoundTrip(t *testing.T) {
	s, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	key, err := DecodeSecret(strings.ToLower(s[:4] + " " + s[4:]))
	if err != nil || len(key) != 20 {
		t.Fatalf("DecodeSecret = %d bytes, %v", len(key), err)
	}
	if _, err := DecodeSecret("   "); err == nil {
		t.Error("empty secret accepted")
	}
	if _, err := DecodeSecret("AAAA"); err == nil {
		t.Error("short secret accepted")
	}
	if u := URI(s, "Processor updater", "owner"); !strings.HasPrefix(u, "otpauth://totp/") || !strings.Contains(u, "secret="+s) {
		t.Errorf("bad URI %q", u)
	}
}
