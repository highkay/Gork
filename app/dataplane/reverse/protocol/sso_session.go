package protocol

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/dslzl/gork/app/platform"
)

// SSO browser probe classification (accounts.x.ai session cookie probe).
// Order matches sso2gropcpa.classify_browser_response: CF → rate limit →
// session-invalid URL → business OK → non-CF 401/403 as WAF block.
type SSOBrowserClass string

const (
	SSOBrowserOK              SSOBrowserClass = "ok"
	SSOBrowserCloudflare      SSOBrowserClass = "cloudflare"
	SSOBrowserRateLimited     SSOBrowserClass = "rate_limited"
	SSOBrowserSessionInvalid  SSOBrowserClass = "session_invalid"
	SSOBrowserHTTPBlock       SSOBrowserClass = "http_block"
	SSOBrowserUnknown         SSOBrowserClass = "unknown"
)

// SessionInvalidError means the SSO cookie/session is dead (not CF/WAF/network).
type SessionInvalidError struct {
	Reason string
	Stage  string
}

func (e *SessionInvalidError) Error() string {
	if e == nil {
		return "sso session invalid"
	}
	if e.Reason == "" {
		return "sso session invalid"
	}
	return "sso session invalid: " + e.Reason
}

func NewSessionInvalidError(reason, stage string) *SessionInvalidError {
	if reason == "" {
		reason = "session_invalid"
	}
	return &SessionInvalidError{Reason: reason, Stage: stage}
}

func IsSessionInvalidError(err error) bool {
	var target *SessionInvalidError
	return errors.As(err, &target)
}

// IsTerminalSSOFailure is true for permanent SSO death (delete/expire candidates).
func IsTerminalSSOFailure(err error) bool {
	return IsSessionInvalidError(err) || IsInvalidCredentialsError(err)
}

// ExtractSSOTokenValue returns the raw SSO JWT/cookie value from a stored token.
func ExtractSSOTokenValue(token string) string {
	raw := strings.TrimSpace(token)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "sso=") || strings.Contains(lower, "sso-rw=") {
		for _, part := range strings.Split(raw, ";") {
			name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "sso", "sso-rw":
				value = strings.TrimSpace(value)
				if value != "" {
					return value
				}
			}
		}
		return ""
	}
	return raw
}

// SSOLocalInvalidReason returns a terminal reason without HTTP, or "" to continue online.
//
//	empty cookie        → "empty"
//	non-JWT (< 2 dots)  → "" (probe online)
//	JWT exp < now-60s   → "jwt_expired"
func SSOLocalInvalidReason(token string, now time.Time) string {
	value := ExtractSSOTokenValue(token)
	if value == "" {
		return "empty"
	}
	parts := strings.Split(value, ".")
	if len(parts) < 3 {
		return ""
	}
	payload, err := decodeJWTPayload(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp <= 0 {
		return ""
	}
	if claims.Exp < now.Unix()-60 {
		return "jwt_expired"
	}
	return ""
}

func decodeJWTPayload(segment string) ([]byte, error) {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return nil, fmt.Errorf("empty jwt payload")
	}
	if raw, err := base64.RawURLEncoding.DecodeString(segment); err == nil {
		return raw, nil
	}
	return base64.URLEncoding.DecodeString(segment)
}

// SessionInvalidReasonFromURL extracts a stable reason from a login/error URL.
func SessionInvalidReasonFromURL(rawURL string) string {
	lower := strings.ToLower(rawURL)
	switch {
	case strings.Contains(lower, "auth-error"), strings.Contains(lower, "auth_error"):
		return "auth_error"
	case strings.Contains(lower, "sign-in"), strings.Contains(lower, "signin"):
		return "sign_in"
	case strings.Contains(lower, "sign-up"), strings.Contains(lower, "signup"):
		return "sign_up"
	default:
		return "session_invalid"
	}
}

// ClassifySSOBrowserResponse classifies accounts.x.ai probe by final URL + status + body.
func ClassifySSOBrowserResponse(finalURL string, status int, body string) SSOBrowserClass {
	bodyLower := strings.ToLower(body)
	urlLower := strings.ToLower(finalURL)

	// 1) Cloudflare is not SSO death (fingerprint/egress).
	if isCloudflareBrowserResponse(status, bodyLower, urlLower) {
		return SSOBrowserCloudflare
	}
	// 2) Rate limit — retryable.
	if status == 429 || strings.Contains(bodyLower, "too many requests") || strings.Contains(bodyLower, "rate limit") {
		return SSOBrowserRateLimited
	}
	// 3) Redirected to login / auth-error → session invalid.
	if isSessionInvalidURL(urlLower) {
		return SSOBrowserSessionInvalid
	}
	// 4) consent / done / 2xx-3xx business page → session accepted.
	if strings.Contains(urlLower, "consent") || strings.Contains(urlLower, "/done") {
		return SSOBrowserOK
	}
	if status >= 200 && status < 400 {
		return SSOBrowserOK
	}
	// 5) Non-CF 401/403 → WAF/http block, not permanent SSO death.
	if status == 401 || status == 403 {
		return SSOBrowserHTTPBlock
	}
	return SSOBrowserUnknown
}

func isSessionInvalidURL(urlLower string) bool {
	return strings.Contains(urlLower, "auth-error") ||
		strings.Contains(urlLower, "auth_error") ||
		strings.Contains(urlLower, "sign-in") ||
		strings.Contains(urlLower, "signin") ||
		strings.Contains(urlLower, "sign-up") ||
		strings.Contains(urlLower, "signup") ||
		strings.Contains(urlLower, "/login") ||
		strings.Contains(urlLower, "login?")
}

func isCloudflareBrowserResponse(status int, bodyLower, urlLower string) bool {
	// Strong signals only — avoid treating normal pages that mention "cloudflare"
	// in analytics/scripts as challenges (that previously caused false CF soft-fails).
	if strings.Contains(urlLower, "cdn-cgi") || strings.Contains(urlLower, "challenges.cloudflare") {
		return true
	}
	if strings.Contains(bodyLower, "cf-challenge") ||
		strings.Contains(bodyLower, "cf-browser-verification") ||
		strings.Contains(bodyLower, "just a moment") ||
		strings.Contains(bodyLower, "attention required") ||
		strings.Contains(bodyLower, "enable javascript and cookies to continue") ||
		strings.Contains(bodyLower, "checking your browser before accessing") {
		return true
	}
	if (status == 403 || status == 503) && strings.Contains(bodyLower, "cloudflare") {
		return true
	}
	return false
}

// ErrorFromSSOBrowserClass maps a browser class to an error for SSO validation.
// ok → nil; session_invalid → SessionInvalidError; others → UpstreamError (non-terminal).
func ErrorFromSSOBrowserClass(class SSOBrowserClass, finalURL string, status int, body string) error {
	switch class {
	case SSOBrowserOK:
		return nil
	case SSOBrowserSessionInvalid:
		return NewSessionInvalidError(SessionInvalidReasonFromURL(finalURL), "session")
	case SSOBrowserCloudflare:
		return platform.NewUpstreamError("sso probe cloudflare challenge", statusOr(status, 403), truncateProbeBody(body))
	case SSOBrowserRateLimited:
		return platform.NewUpstreamError("sso probe rate limited", statusOr(status, 429), truncateProbeBody(body))
	case SSOBrowserHTTPBlock:
		return platform.NewUpstreamError("sso probe http block (waf)", statusOr(status, 403), truncateProbeBody(body))
	default:
		msg := fmt.Sprintf("sso probe unknown response status=%d url=%s", status, sanitizeProbeURL(finalURL))
		return platform.NewUpstreamError(msg, statusOr(status, 502), truncateProbeBody(body))
	}
}

func statusOr(status, fallback int) int {
	if status > 0 {
		return status
	}
	return fallback
}

func truncateProbeBody(body string) string {
	const limit = 240
	body = strings.TrimSpace(body)
	if len(body) <= limit {
		return body
	}
	return body[:limit]
}

func sanitizeProbeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		if len(raw) > 120 {
			return raw[:120]
		}
		return raw
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
