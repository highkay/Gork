package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/dslzl/gork/app/control/proxy"
	platformconfig "github.com/dslzl/gork/app/platform/config"
)

type ByparrConfig interface {
	proxy.StringConfig
	GetInt(key string, defaultValue int) int
}

type ByparrClearanceProvider struct {
	Config ByparrConfig
	Client HTTPDoer
}

const maxByparrResponseBytes int64 = maxClearanceSolverResponseBytes

type globalByparrConfig struct{}

func (globalByparrConfig) GetString(key, defaultValue string) string {
	return platformconfig.GlobalConfig.GetStr(key, defaultValue)
}

func (globalByparrConfig) GetInt(key string, defaultValue int) int {
	return platformconfig.GlobalConfig.GetInt(key, defaultValue)
}

func (p ByparrClearanceProvider) RefreshBundle(ctx context.Context, affinityKey, proxyURL string, targetURL ...string) (proxy.ClearanceBundle, bool, error) {
	cfg := p.Config
	if cfg == nil {
		cfg = globalByparrConfig{}
	}
	mode, err := proxy.ParseClearanceMode(cfg.GetString("proxy.clearance.mode", "none"))
	if err != nil {
		return proxy.ClearanceBundle{}, false, err
	}
	if mode != proxy.ClearanceModeByparr {
		return proxy.ClearanceBundle{}, false, nil
	}

	byparrURL := cfg.GetString("proxy.clearance.byparr_url", "")
	if byparrURL == "" {
		return proxy.ClearanceBundle{}, false, nil
	}
	target := ""
	if len(targetURL) > 0 {
		target = targetURL[0]
	}
	result, ok, err := runClearanceSolve(ctx, clearanceSolveRequest{
		Provider:       "byparr",
		URL:            byparrURL,
		ProxyURL:       proxyURL,
		TimeoutSec:     cfg.GetInt("proxy.clearance.timeout_sec", 60),
		TargetURL:      target,
		Client:         p.Client,
		Payload:        byparrPayload,
		PrepareRequest: applyByparrProxyHeaders,
		Decode:         decodeByparrResponse,
	})
	if err != nil {
		return proxy.ClearanceBundle{}, false, err
	}
	if !ok {
		if result.LastError != "" {
			return newClearanceErrorBundle("byparr", affinityKey, result), false, nil
		}
		return proxy.ClearanceBundle{}, false, nil
	}
	return newClearanceBundle("byparr", affinityKey, result), true, nil
}

func byparrPayload(target string, _ string, timeoutSec int) ([]byte, error) {
	return json.Marshal(map[string]any{
		"cmd":         "request.get",
		"url":         target,
		"max_timeout": timeoutSec,
	})
}

func decodeByparrResponse(raw []byte, target string) (clearanceSolveResult, bool, error) {
	var result struct {
		Status   string `json:"status"`
		Message  string `json:"message"`
		Solution struct {
			Cookies       []clearanceCookie `json:"cookies"`
			UserAgent     string            `json:"userAgent"`
			UserAgentAlt  string            `json:"user_agent"`
			ClearanceHost string            `json:"clearanceHost"`
			URL           string            `json:"url"`
		} `json:"solution"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return clearanceSolveResult{}, false, nil
	}
	if (result.Status != "" && result.Status != "ok") || len(result.Solution.Cookies) == 0 {
		return clearanceSolveResult{}, false, nil
	}

	host := clearanceHost(target)
	if result.Solution.ClearanceHost != "" {
		host = strings.ToLower(result.Solution.ClearanceHost)
	} else if result.Solution.URL != "" {
		host = clearanceHost(result.Solution.URL)
	}
	if host == "" {
		host = defaultClearanceHost()
	}
	userAgent := result.Solution.UserAgent
	if userAgent == "" {
		userAgent = result.Solution.UserAgentAlt
	}
	return clearanceSolveResult{
		Cookies:       extractAllCookies(selectClearanceCookies(result.Solution.Cookies, host)),
		UserAgent:     userAgent,
		ClearanceHost: host,
	}, true, nil
}

func applyByparrProxyHeaders(req *http.Request, proxyURL string) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return
	}
	server := proxyURL
	if parsed, err := url.Parse(proxyURL); err == nil && parsed.User != nil {
		if username := parsed.User.Username(); username != "" {
			req.Header.Set("X-Proxy-Username", username)
		}
		if password, ok := parsed.User.Password(); ok {
			req.Header.Set("X-Proxy-Password", password)
		}
		parsed.User = nil
		server = parsed.String()
	}
	req.Header.Set("X-Proxy-Server", server)
}
