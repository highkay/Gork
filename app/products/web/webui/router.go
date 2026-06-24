package webui

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/auth"
)

func webUIProtected(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			writeWebUIJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": map[string]any{"message": "Method not allowed"}})
			return
		}
		rateLimitKey := webUIAuthRateLimitKey(r)
		if !webUIAuthRateLimiter.Allow(rateLimitKey) {
			writeWebUIJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"message": "Too many authentication attempts"}})
			return
		}
		if err := auth.VerifyWebUIKey(r.Header.Get("Authorization"), webUIAuthSettings()); err != nil {
			webUIAuthRateLimiter.Fail(rateLimitKey)
			writeWebUIError(w, err)
			return
		}
		webUIAuthRateLimiter.Success(rateLimitKey)
		handler(w, r)
	}
}

func webUIAuthRateLimitKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func writeWebUIJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeWebUIError(w http.ResponseWriter, err error) {
	var validation *platform.ValidationError
	if errors.As(err, &validation) {
		writeWebUIJSON(w, validation.Status, validation.ToDict())
		return
	}
	var rateLimit *platform.RateLimitError
	if errors.As(err, &rateLimit) {
		writeWebUIJSON(w, rateLimit.Status, rateLimit.ToDict())
		return
	}
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) {
		writeWebUIJSON(w, upstream.Status, upstream.ToDict())
		return
	}
	var appErr *platform.AppError
	if errors.As(err, &appErr) {
		writeWebUIJSON(w, appErr.Status, appErr.ToDict())
		return
	}
	writeWebUIJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"message": err.Error()}})
}
