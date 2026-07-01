package transport

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
)

type fakeSurfConfig struct {
	enabled bool
}

func (f fakeSurfConfig) GetBool(key string, defaultValue bool) bool {
	if key == "proxy.egress.surf_enabled" {
		return f.enabled
	}
	return defaultValue
}

type fakeHTTPDoer struct {
	called bool
}

func (f *fakeHTTPDoer) Do(*http.Request) (*http.Response, error) {
	f.called = true
	return &http.Response{
		StatusCode: http.StatusTeapot,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

func TestSurfHTTPDoerDisabledDelegates(t *testing.T) {
	fallback := &fakeHTTPDoer{}
	doer := newSurfHTTPDoer(fallback)
	doer.config = fakeSurfConfig{enabled: false}

	response, err := doer.Do(httptest.NewRequest(http.MethodGet, "https://grok.test", nil))
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	if !fallback.called || response.StatusCode != http.StatusTeapot {
		t.Fatalf("fallback called=%v status=%d", fallback.called, response.StatusCode)
	}
}

func TestHTTPTransportProfileUsesLeaseProxyAndBrowserFamily(t *testing.T) {
	proxyURL := "socks5://127.0.0.1:1080"
	lease := controlproxy.NewProxyLease("lease-1")
	lease.ProxyURL = &proxyURL
	lease.UserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:148.0) Gecko/20100101 Firefox/148.0"

	request := httptest.NewRequest(http.MethodGet, "https://grok.test", nil)
	request = request.WithContext(withHTTPTransportProfile(request.Context(), &lease))
	key := surfKeyFromProfile(httpTransportProfileFromRequest(request))

	if key.ProxyURL != "socks5h://127.0.0.1:1080" {
		t.Fatalf("proxy URL = %q, want socks5h://127.0.0.1:1080", key.ProxyURL)
	}
	if key.BrowserFamily != "firefox" || key.OS != "linux" || !key.HTTP3Disabled {
		t.Fatalf("key = %#v, want firefox/linux with HTTP3 disabled", key)
	}
}

func TestSurfHTTPDoerCacheSeparatesBrowserFamilies(t *testing.T) {
	doer := newSurfHTTPDoer(http.DefaultClient)
	chromeKey := surfTransportKey{BrowserFamily: "chrome", OS: "macos", HTTP3Disabled: true}
	firefoxKey := surfTransportKey{BrowserFamily: "firefox", OS: "macos", HTTP3Disabled: true}

	chromeA, err := doer.client(chromeKey)
	if err != nil {
		t.Fatalf("chrome client: %v", err)
	}
	chromeB, err := doer.client(chromeKey)
	if err != nil {
		t.Fatalf("cached chrome client: %v", err)
	}
	firefox, err := doer.client(firefoxKey)
	if err != nil {
		t.Fatalf("firefox client: %v", err)
	}

	if chromeA != chromeB {
		t.Fatalf("same key did not reuse client")
	}
	if chromeA == firefox {
		t.Fatalf("different browser family reused client")
	}
}

func TestSurfHTTPDoerPreservesRequestHeaders(t *testing.T) {
	seen := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	doer := newSurfHTTPDoer(http.DefaultClient)
	doer.config = fakeSurfConfig{enabled: true}
	request, err := http.NewRequest(http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	request.Header.Set("Cookie", "sso=token; cf_clearance=clear")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept-Encoding", "gzip, deflate")
	request.Header.Set("User-Agent", "Project-UA")
	request = request.WithContext(withHTTPTransportProfile(request.Context(), nil))

	response, err := doer.Do(request)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	_ = response.Body.Close()

	headers := <-seen
	for key, want := range map[string]string{
		"Cookie":          "sso=token; cf_clearance=clear",
		"Content-Type":    "application/json",
		"Accept-Encoding": "gzip, deflate",
		"User-Agent":      "Project-UA",
	} {
		if got := headers.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}
