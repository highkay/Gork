package providers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dslzl/gork/app/control/proxy"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
)

const maxClearanceSolverResponseBytes int64 = 1 << 20

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type clearanceCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
}

type clearanceSolveRequest struct {
	Provider       string
	URL            string
	ProxyURL       string
	TimeoutSec     int
	TargetURL      string
	Client         HTTPDoer
	Payload        func(target string, proxyURL string, timeoutSec int) ([]byte, error)
	PrepareRequest func(*http.Request, string)
	Decode         func([]byte, string) (clearanceSolveResult, bool, error)
}

type clearanceSolveResult struct {
	Cookies       string
	UserAgent     string
	ClearanceHost string
	LastError     string
}

func runClearanceSolve(ctx context.Context, request clearanceSolveRequest) (clearanceSolveResult, bool, error) {
	target := strings.TrimSpace(request.TargetURL)
	if target == "" {
		target = reverseruntime.GlobalEndpointTable().Resolve("base")
	}
	body, err := request.Payload(target, request.ProxyURL, request.TimeoutSec)
	if err != nil {
		return clearanceSolveResult{}, false, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(request.TimeoutSec+30)*time.Second)
	defer cancel()

	endpoint := strings.TrimRight(request.URL, "/") + "/v1"
	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return clearanceSolveResult{}, false, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if request.PrepareRequest != nil {
		request.PrepareRequest(httpReq, request.ProxyURL)
	}

	client := request.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return clearanceSolveResult{
			ClearanceHost: clearanceHost(target),
			LastError:     fmt.Sprintf("%s request failed: %v", request.Provider, err),
		}, false, nil
	}
	defer resp.Body.Close()

	raw, err := readClearanceSolverResponseBody(resp.Body, request.Provider)
	if err != nil {
		return clearanceSolveResult{}, false, err
	}
	if resp.StatusCode >= 400 {
		return clearanceSolveResult{}, false, nil
	}
	return request.Decode(raw, target)
}

func newClearanceBundle(provider string, affinityKey string, result clearanceSolveResult) proxy.ClearanceBundle {
	host := result.ClearanceHost
	if host == "" {
		host = clearanceHost(reverseruntime.GlobalEndpointTable().Resolve("base"))
	}
	bundle := proxy.NewClearanceBundle(fmt.Sprintf("%s:%s@%s", provider, affinityKey, host))
	bundle.CFCookies = result.Cookies
	bundle.UserAgent = result.UserAgent
	bundle.AffinityKey = affinityKey
	bundle.ClearanceHost = host
	return bundle
}

func newClearanceErrorBundle(provider string, affinityKey string, result clearanceSolveResult) proxy.ClearanceBundle {
	bundle := newClearanceBundle(provider, affinityKey, result)
	bundle.State = proxy.ClearanceBundleInvalid
	bundle.LastRefreshError = result.LastError
	return bundle
}

func readClearanceSolverResponseBody(reader io.Reader, provider string) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maxClearanceSolverResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxClearanceSolverResponseBytes {
		return nil, fmt.Errorf("%s response body exceeds %d bytes", provider, maxClearanceSolverResponseBytes)
	}
	return raw, nil
}

func clearanceHost(target string) string {
	parsed, err := url.Parse(target)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func defaultClearanceHost() string {
	return clearanceHost(reverseruntime.GlobalEndpointTable().Resolve("base"))
}

func selectClearanceCookies(cookies []clearanceCookie, host string) []clearanceCookie {
	filtered := filterCookiesForHost(cookies, host)
	if len(filtered) == 0 {
		return cookies
	}
	return filtered
}

func filterCookiesForHost(cookies []clearanceCookie, host string) []clearanceCookie {
	filtered := make([]clearanceCookie, 0, len(cookies))
	for _, cookie := range cookies {
		domain := strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
		if host == "" || cookie.Domain == "" || strings.HasSuffix(host, domain) {
			filtered = append(filtered, cookie)
		}
	}
	return filtered
}

func extractAllCookies(cookies []clearanceCookie) string {
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		parts = append(parts, fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
	}
	return strings.Join(parts, "; ")
}
