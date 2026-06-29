package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestByparrProviderReturnsFalseUnlessEnabled(t *testing.T) {
	provider := ByparrClearanceProvider{
		Config: fakeFlareSolverrConfig{
			strings: map[string]string{"proxy.clearance.mode": "flaresolverr"},
		},
	}

	if _, ok, err := provider.RefreshBundle(context.Background(), "node-1", "http://proxy:8080"); err != nil || ok {
		t.Fatalf("RefreshBundle ok=%v err=%v, want false nil", ok, err)
	}
}

func TestByparrProviderSendsProxyHeaderAndSecondsTimeout(t *testing.T) {
	var requestSeen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestSeen = true
		if r.URL.Path != "/v1" {
			t.Fatalf("path = %s, want /v1", r.URL.Path)
		}
		if got := r.Header.Get("X-Proxy-Server"); got != "http://proxy.local:8080" {
			t.Fatalf("X-Proxy-Server = %q", got)
		}
		if got := r.Header.Get("X-Proxy-Username"); got != "user" {
			t.Fatalf("X-Proxy-Username = %q", got)
		}
		if got := r.Header.Get("X-Proxy-Password"); got != "pass" {
			t.Fatalf("X-Proxy-Password = %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request payload: %v", err)
		}
		if payload["cmd"] != "request.get" || payload["url"] != "https://grok.com/test" || payload["max_timeout"] != float64(5) {
			t.Fatalf("payload = %#v", payload)
		}
		if _, ok := payload["maxTimeout"]; ok {
			t.Fatalf("payload unexpectedly included millisecond timeout alias: %#v", payload)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "ok",
			"solution": {
				"cookies": [
					{"name": "cf_clearance", "value": "ok", "domain": ".grok.com"},
					{"name": "__cf_bm", "value": "bm", "domain": ".grok.com"},
					{"name": "other", "value": "skip", "domain": ".example.com"}
				],
				"userAgent": "Byparr-UA"
			}
		}`))
	}))
	defer server.Close()

	provider := ByparrClearanceProvider{
		Config: fakeFlareSolverrConfig{
			strings: map[string]string{
				"proxy.clearance.mode":       "byparr",
				"proxy.clearance.byparr_url": server.URL,
			},
			ints: map[string]int{"proxy.clearance.timeout_sec": 5},
		},
	}

	bundle, ok, err := provider.RefreshBundle(context.Background(), "node-1", "http://user:pass@proxy.local:8080", "https://grok.com/test")
	if err != nil {
		t.Fatalf("RefreshBundle returned error: %v", err)
	}
	if !ok || !requestSeen {
		t.Fatalf("RefreshBundle ok=%v requestSeen=%v", ok, requestSeen)
	}
	if bundle.BundleID != "byparr:node-1@grok.com" || bundle.AffinityKey != "node-1" || bundle.ClearanceHost != "grok.com" {
		t.Fatalf("bundle identifiers = %#v", bundle)
	}
	if bundle.CFCookies != "cf_clearance=ok; __cf_bm=bm" {
		t.Fatalf("CFCookies = %q", bundle.CFCookies)
	}
	if strings.Contains(bundle.CFCookies, "other=skip") {
		t.Fatalf("CFCookies included unrelated cookie: %q", bundle.CFCookies)
	}
	if bundle.UserAgent != "Byparr-UA" {
		t.Fatalf("UserAgent = %q", bundle.UserAgent)
	}
}

func TestByparrProviderFailureDoesNotFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"error","message":"failed"}`))
	}))
	defer server.Close()

	provider := ByparrClearanceProvider{
		Config: fakeFlareSolverrConfig{
			strings: map[string]string{
				"proxy.clearance.mode":       "byparr",
				"proxy.clearance.byparr_url": server.URL,
			},
		},
	}

	bundle, ok, err := provider.RefreshBundle(context.Background(), "node-1", "http://proxy:8080", "https://grok.com/test")
	if err != nil || ok || bundle.BundleID != "" {
		t.Fatalf("RefreshBundle bundle=%#v ok=%v err=%v, want empty false nil", bundle, ok, err)
	}
}

func TestByparrProviderOversizedResponseReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", int(maxByparrResponseBytes)+1)))
	}))
	defer server.Close()

	provider := ByparrClearanceProvider{
		Config: fakeFlareSolverrConfig{
			strings: map[string]string{
				"proxy.clearance.mode":       "byparr",
				"proxy.clearance.byparr_url": server.URL,
			},
		},
	}

	if _, ok, err := provider.RefreshBundle(context.Background(), "node-1", "", "https://grok.com/test"); err == nil || ok {
		t.Fatalf("RefreshBundle oversized response ok=%v err=%v, want error", ok, err)
	}
}
