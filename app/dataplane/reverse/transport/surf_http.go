package transport

import (
	"context"
	"net/http"
	"strings"
	"sync"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
	proxyadapters "github.com/dslzl/gork/app/dataplane/proxy/adapters"
	platformconfig "github.com/dslzl/gork/app/platform/config"
	"github.com/enetx/g"
	"github.com/enetx/surf"
)

type httpTransportProfile struct {
	ProxyURL  string
	Browser   string
	UserAgent string
}

type httpTransportProfileKey struct{}

func withHTTPTransportProfile(ctx context.Context, lease *controlproxy.ProxyLease) context.Context {
	profile := proxyadapters.ResolveProxyProfile(lease)
	proxyURL := ""
	if lease != nil && lease.ProxyURL != nil {
		proxyURL = proxyadapters.NormalizeProxyURL(*lease.ProxyURL)
	}
	return context.WithValue(ctx, httpTransportProfileKey{}, httpTransportProfile{
		ProxyURL:  proxyURL,
		Browser:   profile.Browser,
		UserAgent: profile.UserAgent,
	})
}

func httpTransportProfileFromRequest(request *http.Request) httpTransportProfile {
	if profile, ok := request.Context().Value(httpTransportProfileKey{}).(httpTransportProfile); ok {
		return profile
	}
	profile := proxyadapters.ResolveProxyProfile(nil)
	return httpTransportProfile{
		Browser:   profile.Browser,
		UserAgent: profile.UserAgent,
	}
}

type surfConfig interface {
	GetBool(key string, defaultValue bool) bool
}

type globalSurfConfig struct{}

func (globalSurfConfig) GetBool(key string, defaultValue bool) bool {
	if platformconfig.GlobalConfig == nil {
		return defaultValue
	}
	return platformconfig.GlobalConfig.GetBool(key, defaultValue)
}

type surfTransportKey struct {
	ProxyURL      string
	BrowserFamily string
	OS            string
	HTTP3Disabled bool
}

type surfHTTPDoer struct {
	fallback HTTPDoer
	config   surfConfig
	mu       sync.Mutex
	clients  map[surfTransportKey]HTTPDoer
}

func newSurfHTTPDoer(fallback HTTPDoer) *surfHTTPDoer {
	return &surfHTTPDoer{
		fallback: fallback,
		config:   globalSurfConfig{},
		clients:  map[surfTransportKey]HTTPDoer{},
	}
}

func (d *surfHTTPDoer) Do(request *http.Request) (*http.Response, error) {
	if !d.config.GetBool("proxy.egress.surf_enabled", false) {
		return d.fallback.Do(request)
	}
	client, err := d.client(surfKeyFromProfile(httpTransportProfileFromRequest(request)))
	if err != nil {
		return nil, err
	}
	return client.Do(request)
}

func (d *surfHTTPDoer) client(key surfTransportKey) (HTTPDoer, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if client, ok := d.clients[key]; ok {
		return client, nil
	}
	client, err := buildSurfHTTPClient(key)
	if err != nil {
		return nil, err
	}
	d.clients[key] = client
	return client, nil
}

func buildSurfHTTPClient(key surfTransportKey) (HTTPDoer, error) {
	builder := surf.NewClient().Builder()
	if key.BrowserFamily == "firefox" {
		builder = builder.JA().Firefox148()
	} else {
		builder = builder.JA().Chrome145()
	}
	if key.ProxyURL != "" {
		builder = builder.Proxy(g.String(key.ProxyURL))
	}
	result := builder.DisableCompression().Build()
	if result.IsErr() {
		return nil, result.Err()
	}
	return result.Ok().Std(), nil
}

func surfKeyFromProfile(profile httpTransportProfile) surfTransportKey {
	return surfTransportKey{
		ProxyURL:      profile.ProxyURL,
		BrowserFamily: surfBrowserFamily(profile.Browser, profile.UserAgent),
		OS:            surfOS(profile.UserAgent),
		HTTP3Disabled: true,
	}
}

func surfBrowserFamily(browser, userAgent string) string {
	value := strings.ToLower(browser + " " + userAgent)
	if strings.Contains(value, "firefox") || strings.Contains(value, "fxios") {
		return "firefox"
	}
	return "chrome"
}

func surfOS(userAgent string) string {
	lower := strings.ToLower(userAgent)
	switch {
	case strings.Contains(lower, "android"):
		return "android"
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad") || strings.Contains(lower, "ios"):
		return "ios"
	case strings.Contains(lower, "windows"):
		return "windows"
	case strings.Contains(lower, "linux"):
		return "linux"
	case strings.Contains(lower, "mac os x") || strings.Contains(lower, "macintosh"):
		return "macos"
	default:
		return ""
	}
}
