package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dslzl/gork/app/control/proxy"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
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

const maxByparrResponseBytes int64 = 1 << 20

type globalByparrConfig struct{}

func (globalByparrConfig) GetString(key, defaultValue string) string {
	return platformconfig.GlobalConfig.GetStr(key, defaultValue)
}

func (globalByparrConfig) GetInt(key string, defaultValue int) int {
	return platformconfig.GlobalConfig.GetInt(key, defaultValue)
}

type byparrSolveResult struct {
	Cookies       string
	UserAgent     string
	ClearanceHost string
}

func (p ByparrClearanceProvider) RefreshBundle(ctx context.Context, affinityKey, proxyURL string, targetURL ...string) (proxy.ClearanceBundle, bool, error) {
	cfg := p.Config
	if cfg == nil {
		cfg = globalByparrConfig{}
	}
	modeValue := cfg.GetString("proxy.clearance.mode", "none")

	mode, err := proxy.ParseClearanceMode(modeValue)
	if err != nil {
		return proxy.ClearanceBundle{}, false, err
	}
	if mode != proxy.ClearanceModeByparr {
		return proxy.ClearanceBundle{}, false, nil
	}

	byparrURL := cfg.GetString("proxy.clearance.byparr_url", "")
	timeoutSec := cfg.GetInt("proxy.clearance.timeout_sec", 60)
	if byparrURL == "" {
		return proxy.ClearanceBundle{}, false, nil
	}

	target := reverseruntime.GlobalEndpointTable().Resolve("base")
	if len(targetURL) > 0 {
		target = targetURL[0]
	}
	result, ok, err := p.solve(ctx, byparrURL, proxyURL, timeoutSec, target)
	if err != nil || !ok {
		return proxy.ClearanceBundle{}, false, err
	}

	host := result.ClearanceHost
	if host == "" {
		host = clearanceHost(reverseruntime.GlobalEndpointTable().Resolve("base"))
	}
	bundle := proxy.NewClearanceBundle(fmt.Sprintf("byparr:%s@%s", affinityKey, host))
	bundle.CFCookies = result.Cookies
	bundle.UserAgent = result.UserAgent
	bundle.AffinityKey = affinityKey
	bundle.ClearanceHost = host
	return bundle, true, nil
}

func (p ByparrClearanceProvider) solve(ctx context.Context, byparrURL, proxyURL string, timeoutSec int, targetURL string) (byparrSolveResult, bool, error) {
	target := strings.TrimSpace(targetURL)
	if target == "" {
		target = reverseruntime.GlobalEndpointTable().Resolve("base")
	}
	payload := map[string]any{
		"cmd":         "request.get",
		"url":         target,
		"max_timeout": timeoutSec,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return byparrSolveResult{}, false, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec+30)*time.Second)
	defer cancel()

	endpoint := strings.TrimRight(byparrURL, "/") + "/v1"
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return byparrSolveResult{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	applyByparrProxyHeaders(req, proxyURL)

	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return byparrSolveResult{}, false, nil
	}
	defer resp.Body.Close()

	raw, err := readByparrResponseBody(resp.Body)
	if err != nil {
		return byparrSolveResult{}, false, err
	}
	if resp.StatusCode >= 400 {
		return byparrSolveResult{}, false, nil
	}

	var result struct {
		Status   string `json:"status"`
		Message  string `json:"message"`
		Solution struct {
			Cookies       []flareSolverrCookie `json:"cookies"`
			UserAgent     string               `json:"userAgent"`
			UserAgentAlt  string               `json:"user_agent"`
			ClearanceHost string               `json:"clearanceHost"`
			URL           string               `json:"url"`
		} `json:"solution"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return byparrSolveResult{}, false, nil
	}
	if result.Status != "" && result.Status != "ok" {
		return byparrSolveResult{}, false, nil
	}
	if len(result.Solution.Cookies) == 0 {
		return byparrSolveResult{}, false, nil
	}

	host := clearanceHost(target)
	if result.Solution.ClearanceHost != "" {
		host = strings.ToLower(result.Solution.ClearanceHost)
	} else if result.Solution.URL != "" {
		host = clearanceHost(result.Solution.URL)
	}
	filtered := filterCookiesForHost(result.Solution.Cookies, host)
	chosen := filtered
	if len(chosen) == 0 {
		chosen = result.Solution.Cookies
	}
	if host == "" {
		host = clearanceHost(reverseruntime.GlobalEndpointTable().Resolve("base"))
	}
	userAgent := result.Solution.UserAgent
	if userAgent == "" {
		userAgent = result.Solution.UserAgentAlt
	}
	return byparrSolveResult{
		Cookies:       extractAllCookies(chosen),
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

func readByparrResponseBody(reader io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maxByparrResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxByparrResponseBytes {
		return nil, fmt.Errorf("byparr response body exceeds %d bytes", maxByparrResponseBytes)
	}
	return raw, nil
}
