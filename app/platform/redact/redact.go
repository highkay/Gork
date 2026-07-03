package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var sensitivePatterns = []struct {
	pattern *regexp.Regexp
	repl    string
}{
	{regexp.MustCompile(`(?i)\b(sso|sso-rw|cf_clearance)=([^;\s"'\\]+)`), `${1}=<redacted>`},
	{regexp.MustCompile(`(?i)(bearer\s+)([A-Za-z0-9._~+/=-]{8,})`), `${1}<redacted>`},
	{regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|secret|password)["']?\s*[:=]\s*["']?([^"',\s}]+)`), `${1}=<redacted>`},
	{regexp.MustCompile(`(?i)(https?://[^/\s:@]+):([^@\s/]+)@`), `${1}:<redacted>@`},
	{regexp.MustCompile(`\b[A-Za-z0-9_-]{32,}\b`), `<redacted>`},
}

func SensitiveText(value string) string {
	out := strings.ReplaceAll(value, "\n", `\n`)
	for _, replacement := range sensitivePatterns {
		out = replacement.pattern.ReplaceAllString(out, replacement.repl)
	}
	return out
}

func Excerpt(value string, limit int) string {
	if limit <= 0 {
		limit = 240
	}
	out := SensitiveText(value)
	if len(out) > limit {
		out = out[:limit]
	}
	if out == "" {
		return "-"
	}
	return out
}

func SHA256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
