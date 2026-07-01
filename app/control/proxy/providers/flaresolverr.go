package providers

import (
	"context"
	"encoding/json"

	"github.com/dslzl/gork/app/control/proxy"
	platformconfig "github.com/dslzl/gork/app/platform/config"
)

type FlareSolverrConfig interface {
	proxy.StringConfig
	GetInt(key string, defaultValue int) int
}

type FlareSolverrClearanceProvider struct {
	Config FlareSolverrConfig
	Client HTTPDoer
}

const maxFlareSolverrResponseBytes int64 = maxClearanceSolverResponseBytes

type globalFlareSolverrConfig struct{}

func (globalFlareSolverrConfig) GetString(key, defaultValue string) string {
	return platformconfig.GlobalConfig.GetStr(key, defaultValue)
}

func (globalFlareSolverrConfig) GetInt(key string, defaultValue int) int {
	return platformconfig.GlobalConfig.GetInt(key, defaultValue)
}

func (p FlareSolverrClearanceProvider) RefreshBundle(ctx context.Context, affinityKey, proxyURL string, targetURL ...string) (proxy.ClearanceBundle, bool, error) {
	cfg := p.Config
	if cfg == nil {
		cfg = globalFlareSolverrConfig{}
	}
	mode, err := proxy.ParseClearanceMode(cfg.GetString("proxy.clearance.mode", "none"))
	if err != nil {
		return proxy.ClearanceBundle{}, false, err
	}
	if mode != proxy.ClearanceModeFlareSolverr {
		return proxy.ClearanceBundle{}, false, nil
	}

	fsURL := cfg.GetString("proxy.clearance.flaresolverr_url", "")
	if fsURL == "" {
		return proxy.ClearanceBundle{}, false, nil
	}
	target := ""
	if len(targetURL) > 0 {
		target = targetURL[0]
	}
	result, ok, err := runClearanceSolve(ctx, clearanceSolveRequest{
		Provider:   "flaresolverr",
		URL:        fsURL,
		ProxyURL:   proxyURL,
		TimeoutSec: cfg.GetInt("proxy.clearance.timeout_sec", 60),
		TargetURL:  target,
		Client:     p.Client,
		Payload:    flareSolverrPayload,
		Decode:     decodeFlareSolverrResponse,
	})
	if err != nil || !ok {
		return proxy.ClearanceBundle{}, false, err
	}
	return newClearanceBundle("flaresolverr", affinityKey, result), true, nil
}

func flareSolverrPayload(target string, proxyURL string, timeoutSec int) ([]byte, error) {
	payload := map[string]any{
		"cmd":        "request.get",
		"url":        target,
		"maxTimeout": timeoutSec * 1000,
	}
	if proxyURL != "" {
		payload["proxy"] = map[string]string{"url": proxyURL}
	}
	return json.Marshal(payload)
}

func decodeFlareSolverrResponse(raw []byte, target string) (clearanceSolveResult, bool, error) {
	var result struct {
		Status   string `json:"status"`
		Message  string `json:"message"`
		Solution struct {
			Cookies   []clearanceCookie `json:"cookies"`
			UserAgent string            `json:"userAgent"`
		} `json:"solution"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return clearanceSolveResult{}, false, nil
	}
	if result.Status != "ok" || len(result.Solution.Cookies) == 0 {
		return clearanceSolveResult{}, false, nil
	}

	host := clearanceHost(target)
	if host == "" {
		host = defaultClearanceHost()
	}
	return clearanceSolveResult{
		Cookies:       extractAllCookies(selectClearanceCookies(result.Solution.Cookies, host)),
		UserAgent:     result.Solution.UserAgent,
		ClearanceHost: host,
	}, true, nil
}
