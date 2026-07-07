package transport

import (
	"context"
	"errors"
	"fmt"
	"time"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
	platform "github.com/dslzl/gork/app/platform"
)

const defaultUsageTimeout = 30 * time.Second

type UsageProxyRuntime interface {
	Acquire(ctx context.Context, options ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error)
	Feedback(ctx context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error
}

type UsageFetcher struct {
	ProxyRuntime UsageProxyRuntime
	Client       HTTPClient
	Timeout      time.Duration
	Endpoint     string
	Origin       string
	Referer      string
}

func (f UsageFetcher) FetchUsage(ctx context.Context, token, modeName string) (map[string]any, error) {
	option := normalizeUsageFetcher(f)
	lease, err := option.ProxyRuntime.Acquire(ctx, controlproxy.AcquireOptions{
		Scope:           controlproxy.ProxyScopeApp,
		Kind:            controlproxy.RequestKindHTTP,
		ClearanceOrigin: option.Origin,
	})
	if err != nil {
		errText := redactedTransportError(err)
		return nil, platform.NewUpstreamError(fmt.Sprintf("fetch_usage: acquire proxy: %s", errText), 502, errText)
	}
	result, err := PostJSON(ctx, option.Endpoint, token, protocol.BuildUsagePayload(modeName), HTTPOptions{
		Client:  option.Client,
		Lease:   &lease,
		Timeout: option.Timeout,
		Origin:  option.Origin,
		Referer: option.Referer,
	})
	if err != nil {
		return nil, handleUsageError(ctx, option.ProxyRuntime, lease, err)
	}
	status := 200
	_ = option.ProxyRuntime.Feedback(ctx, lease, controlproxy.ProxyFeedback{
		Kind:       controlproxy.ProxyFeedbackSuccess,
		StatusCode: &status,
	})
	return result, nil
}

func normalizeUsageFetcher(options UsageFetcher) UsageFetcher {
	table := reverseruntime.GlobalEndpointTable()
	if options.ProxyRuntime == nil {
		options.ProxyRuntime = missingUsageProxyRuntime{}
	}
	if options.Client == nil {
		options.Client = netHTTPClient{}
	}
	if options.Timeout <= 0 {
		options.Timeout = defaultUsageTimeout
	}
	if options.Endpoint == "" {
		options.Endpoint = table.Resolve("rate_limits")
	}
	if options.Origin == "" {
		options.Origin = table.Resolve("base")
	}
	if options.Referer == "" {
		options.Referer = table.Resolve("base_referer")
	}
	return options
}

func handleUsageError(ctx context.Context, runtime UsageProxyRuntime, lease controlproxy.ProxyLease, err error) error {
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) {
		_ = runtime.Feedback(ctx, lease, UpstreamFeedback(upstream))
		return err
	}
	_ = runtime.Feedback(ctx, lease, controlproxy.ProxyFeedback{Kind: controlproxy.ProxyFeedbackTransportError})
	errText := redactedTransportError(err)
	return platform.NewUpstreamError(fmt.Sprintf("fetch_usage: transport error: %s", errText), 502, errText)
}

type missingUsageProxyRuntime struct{}

func (missingUsageProxyRuntime) Acquire(context.Context, ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error) {
	return controlproxy.ProxyLease{}, fmt.Errorf("usage proxy runtime is not configured")
}

func (missingUsageProxyRuntime) Feedback(context.Context, controlproxy.ProxyLease, controlproxy.ProxyFeedback) error {
	return nil
}
