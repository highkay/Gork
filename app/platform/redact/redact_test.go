package redact

import (
	"strings"
	"testing"
)

func TestSensitiveTextRedactsCommonSecretForms(t *testing.T) {
	raw := `Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456
Cookie: sso=secret; sso-rw=write; cf_clearance=cf-secret-value
dsn=https://user:pass@example.com/path
api_key="sk-abcdefghijklmnopqrstuvwxyz1234567890"`

	got := SensitiveText(raw)
	for _, secret := range []string{"abcdefghijklmnopqrstuvwxyz123456", "secret", "write", "cf-secret-value", "user:pass", "sk-"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted text still contains %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "Bearer <redacted>") ||
		!strings.Contains(got, "sso=<redacted>") ||
		!strings.Contains(got, "cf_clearance=<redacted>") ||
		!strings.Contains(got, "https://user:<redacted>@example.com/path") ||
		!strings.Contains(got, "api_key=<redacted>") {
		t.Fatalf("redacted text missing expected markers: %s", got)
	}
}

func TestExcerptRedactsBeforeTruncating(t *testing.T) {
	got := Excerpt("prefix password=very-secret-token suffix", 20)
	if strings.Contains(got, "very-secret-token") {
		t.Fatalf("excerpt leaked secret: %s", got)
	}
	if len(got) > 20 {
		t.Fatalf("excerpt length=%d want <=20", len(got))
	}
}

func TestSHA256HexIsStable(t *testing.T) {
	got := SHA256Hex("hello")
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Fatalf("SHA256Hex=%s want %s", got, want)
	}
}
