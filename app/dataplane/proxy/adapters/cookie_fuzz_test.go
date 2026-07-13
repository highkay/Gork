package adapters

import (
	"strings"
	"testing"
)

func FuzzBuildSSOCookie(f *testing.F) {
	for _, seed := range []string{
		"token",
		"sso=abc; sso-rw=abc",
		"sso=abc; cf_clearance=old",
		"a=b; c=d",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		cookie := BuildSSOCookie(raw)
		if strings.ContainsAny(cookie, "\r\n") {
			t.Fatalf("cookie contains line break: %q", cookie)
		}
		if !hasCookiePair(cookie, "sso") {
			t.Fatalf("cookie missing sso pair: %q", cookie)
		}
		if !hasCookiePair(cookie, "sso-rw") {
			t.Fatalf("cookie missing sso-rw pair: %q", cookie)
		}
	})
}
