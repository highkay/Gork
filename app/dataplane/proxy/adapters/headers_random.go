package adapters

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	mathrand "math/rand"
	"net/url"
	"strings"

	platformconfig "github.com/dslzl/gork/app/platform/config"
)

func statsigID() string {
	if platformconfig.GlobalConfig != nil && !platformconfig.GlobalConfig.GetBool("features.dynamic_statsig", true) {
		return uuidString()
	}
	// This mirrors client-side diagnostic noise and is not a security token.
	if mathrand.Intn(2) == 0 {
		msg := fmt.Sprintf("x1:TypeError: Cannot read properties of null (reading 'children[\\'%s\\']')", randomString("abcdefghijklmnopqrstuvwxyz0123456789", 5))
		return base64.StdEncoding.EncodeToString([]byte(msg))
	}
	msg := fmt.Sprintf("x1:TypeError: Cannot read properties of undefined (reading '%s')", randomString("abcdefghijklmnopqrstuvwxyz", 10))
	return base64.StdEncoding.EncodeToString([]byte(msg))
}

func originHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func randomString(alphabet string, length int) string {
	if length <= 0 {
		return ""
	}
	var builder strings.Builder
	for i := 0; i < length; i++ {
		builder.WriteByte(alphabet[mathrand.Intn(len(alphabet))])
	}
	return builder.String()
}

func uuidString() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fallbackUUID()
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4],
		b[4:6],
		b[6:8],
		b[8:10],
		b[10:16],
	)
}

func fallbackUUID() string {
	parts := []int{8, 4, 4, 4, 12}
	out := make([]string, 0, len(parts))
	for _, length := range parts {
		out = append(out, randomString("0123456789abcdef", length))
	}
	return strings.Join(out, "-")
}
