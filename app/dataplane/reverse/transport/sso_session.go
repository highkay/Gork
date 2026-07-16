package transport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
	platform "github.com/dslzl/gork/app/platform"
)

const defaultSSOSessionTimeout = 20 * time.Second

// SSOSessionProber probes whether an SSO cookie is still accepted by accounts.x.ai.
// Authority is the final redirect URL after setting sso= on .x.ai, not JWT alone.
type SSOSessionProber struct {
	ProxyRuntime UsageProxyRuntime
	Client       HTTPClient
	Timeout      time.Duration
	Endpoint     string
	Origin       string
	Referer      string
}

func (p SSOSessionProber) ProbeSession(ctx context.Context, token string) error {
	option := normalizeSSOSessionProber(p)
	// Clearance must target accounts.x.ai (not grok.com); CF cookies are host-scoped.
	lease, err := option.ProxyRuntime.Acquire(ctx, controlproxy.AcquireOptions{
		Scope:           controlproxy.ProxyScopeApp,
		Kind:            controlproxy.RequestKindHTTP,
		ClearanceOrigin: option.Endpoint,
	})
	if err != nil {
		errText := redactedTransportError(err)
		return platform.NewUpstreamError(fmt.Sprintf("sso_session: acquire proxy: %s", errText), 502, errText)
	}
	response, err := getSSOSession(ctx, option, token, &lease)
	if err != nil {
		return handleSSOSessionError(ctx, option.ProxyRuntime, lease, err)
	}
	finalURL := response.FinalURL
	if finalURL == "" {
		finalURL = option.Endpoint
	}
	class := protocol.ClassifySSOBrowserResponse(finalURL, response.StatusCode, string(response.Body))
	classErr := protocol.ErrorFromSSOBrowserClass(class, finalURL, response.StatusCode, string(response.Body))
	if classErr == nil {
		status := response.StatusCode
		_ = option.ProxyRuntime.Feedback(ctx, lease, controlproxy.ProxyFeedback{
			Kind:       controlproxy.ProxyFeedbackSuccess,
			StatusCode: &status,
		})
		return nil
	}
	if protocol.IsSessionInvalidError(classErr) {
		status := response.StatusCode
		if status == 0 {
			status = 401
		}
		_ = option.ProxyRuntime.Feedback(ctx, lease, controlproxy.ProxyFeedback{
			Kind:       controlproxy.ProxyFeedbackUnauthorized,
			StatusCode: &status,
		})
		return classErr
	}
	_ = option.ProxyRuntime.Feedback(ctx, lease, controlproxy.BuildFeedback(response.StatusCode, controlproxy.BuildFeedbackOptions{
		IsCloudflare: class == protocol.SSOBrowserCloudflare,
	}))
	return classErr
}

func getSSOSession(ctx context.Context, option SSOSessionProber, token string, lease *controlproxy.ProxyLease) (HTTPResponse, error) {
	httpOption := HTTPOptions{
		Client:  option.Client,
		Lease:   lease,
		Timeout: option.Timeout,
		Origin:  option.Origin,
		Referer: option.Referer,
		// Browser-like navigation so accounts.x.ai classifies as a document request.
		ContentType: "text/html",
		ExtraHeaders: map[string]string{
			"Accept":         "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
			"Sec-Fetch-Dest": "document",
			"Sec-Fetch-Mode": "navigate",
			"Sec-Fetch-Site": "none",
			"Sec-Fetch-User": "?1",
			"Upgrade-Insecure-Requests": "1",
		},
	}
	request := newHTTPRequest(option.Endpoint, token, httpOption)
	// Drop API-ish Content-Type for pure navigation.
	delete(request.Headers, "Content-Type")
	delete(request.Headers, "Origin")
	request.AllowRedirects = true
	response, err := httpOption.Client.Get(ctx, request)
	if err != nil {
		return HTTPResponse{}, httpTransportError(err)
	}
	return response, nil
}

func normalizeSSOSessionProber(options SSOSessionProber) SSOSessionProber {
	table := reverseruntime.GlobalEndpointTable()
	if options.ProxyRuntime == nil {
		options.ProxyRuntime = missingUsageProxyRuntime{}
	}
	if options.Client == nil {
		options.Client = netHTTPClient{}
	}
	if options.Timeout <= 0 {
		options.Timeout = defaultSSOSessionTimeout
	}
	if options.Endpoint == "" {
		// Prefer configured accounts base; default https://accounts.x.ai/
		base := table.Resolve("accounts_base")
		if base == "" {
			base = reverseruntime.AccountsBase
		}
		options.Endpoint = stringsTrimRightSlash(base) + "/"
	}
	// Origin/Referer for accounts session probe should stay on accounts.x.ai so
	// proxy clearance affinity and browser-like headers match the probe host.
	if options.Origin == "" {
		options.Origin = stringsTrimRightSlash(options.Endpoint)
	}
	if options.Referer == "" {
		options.Referer = options.Endpoint
	}
	return options
}

func handleSSOSessionError(ctx context.Context, runtime UsageProxyRuntime, lease controlproxy.ProxyLease, err error) error {
	if protocol.IsSessionInvalidError(err) {
		return err
	}
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) {
		_ = runtime.Feedback(ctx, lease, UpstreamFeedback(upstream))
		return err
	}
	_ = runtime.Feedback(ctx, lease, controlproxy.ProxyFeedback{Kind: controlproxy.ProxyFeedbackTransportError})
	errText := redactedTransportError(err)
	return platform.NewUpstreamError(fmt.Sprintf("sso_session: transport error: %s", errText), 502, errText)
}

func stringsTrimRightSlash(value string) string {
	return strings.TrimRight(value, "/")
}
