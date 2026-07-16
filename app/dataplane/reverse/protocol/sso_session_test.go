package protocol

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestExtractSSOTokenValue(t *testing.T) {
	if got := ExtractSSOTokenValue("  raw-token  "); got != "raw-token" {
		t.Fatalf("raw token = %q", got)
	}
	if got := ExtractSSOTokenValue("sso=abc.def.ghi; sso-rw=abc.def.ghi; cf_clearance=x"); got != "abc.def.ghi" {
		t.Fatalf("cookie token = %q", got)
	}
	if got := ExtractSSOTokenValue(""); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestSSOLocalInvalidReason(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if got := SSOLocalInvalidReason("", now); got != "empty" {
		t.Fatalf("empty reason = %q", got)
	}
	if got := SSOLocalInvalidReason("not-a-jwt", now); got != "" {
		t.Fatalf("non-jwt should continue online, got %q", got)
	}
	expired := makeTestJWT(t, now.Unix()-120)
	if got := SSOLocalInvalidReason(expired, now); got != "jwt_expired" {
		t.Fatalf("expired jwt = %q", got)
	}
	alive := makeTestJWT(t, now.Unix()+3600)
	if got := SSOLocalInvalidReason(alive, now); got != "" {
		t.Fatalf("alive jwt = %q", got)
	}
	// within 60s skew still accepted
	skew := makeTestJWT(t, now.Unix()-30)
	if got := SSOLocalInvalidReason(skew, now); got != "" {
		t.Fatalf("skew jwt = %q", got)
	}
}

func TestClassifySSOBrowserResponse(t *testing.T) {
	cases := []struct {
		name   string
		url    string
		status int
		body   string
		want   SSOBrowserClass
	}{
		{name: "cloudflare body", url: "https://accounts.x.ai/", status: 403, body: "Just a Moment... cloudflare challenge", want: SSOBrowserCloudflare},
		{name: "rate limit", url: "https://accounts.x.ai/", status: 429, body: "Too Many Requests", want: SSOBrowserRateLimited},
		{name: "sign-in", url: "https://accounts.x.ai/sign-in?next=/", status: 200, body: "login", want: SSOBrowserSessionInvalid},
		{name: "auth-error", url: "https://accounts.x.ai/auth-error", status: 200, body: "", want: SSOBrowserSessionInvalid},
		{name: "consent ok", url: "https://accounts.x.ai/oauth2/consent", status: 200, body: "", want: SSOBrowserOK},
		{name: "accounts home ok", url: "https://accounts.x.ai/", status: 200, body: "<html>ok</html>", want: SSOBrowserOK},
		{name: "http block", url: "https://accounts.x.ai/", status: 403, body: "forbidden by waf", want: SSOBrowserHTTPBlock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifySSOBrowserResponse(tc.url, tc.status, tc.body)
			if got != tc.want {
				t.Fatalf("class = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSessionInvalidReasonFromURL(t *testing.T) {
	if got := SessionInvalidReasonFromURL("https://accounts.x.ai/sign-in"); got != "sign_in" {
		t.Fatalf("sign_in = %q", got)
	}
	if got := SessionInvalidReasonFromURL("https://accounts.x.ai/auth-error"); got != "auth_error" {
		t.Fatalf("auth_error = %q", got)
	}
	if got := SessionInvalidReasonFromURL("https://accounts.x.ai/sign-up"); got != "sign_up" {
		t.Fatalf("sign_up = %q", got)
	}
}

func TestErrorFromSSOBrowserClassTerminality(t *testing.T) {
	if err := ErrorFromSSOBrowserClass(SSOBrowserOK, "https://accounts.x.ai/", 200, ""); err != nil {
		t.Fatalf("ok should be nil: %v", err)
	}
	err := ErrorFromSSOBrowserClass(SSOBrowserSessionInvalid, "https://accounts.x.ai/sign-in", 200, "")
	if !IsSessionInvalidError(err) || !IsTerminalSSOFailure(err) {
		t.Fatalf("session invalid should be terminal: %v", err)
	}
	cf := ErrorFromSSOBrowserClass(SSOBrowserCloudflare, "https://accounts.x.ai/", 403, "just a moment")
	if IsTerminalSSOFailure(cf) {
		t.Fatalf("cloudflare must not be terminal: %v", cf)
	}
	block := ErrorFromSSOBrowserClass(SSOBrowserHTTPBlock, "https://accounts.x.ai/", 403, "waf")
	if IsTerminalSSOFailure(block) {
		t.Fatalf("http block must not be terminal: %v", block)
	}
}

func makeTestJWT(t *testing.T, exp int64) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, err := json.Marshal(map[string]any{"exp": exp})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + body + ".sig"
}
