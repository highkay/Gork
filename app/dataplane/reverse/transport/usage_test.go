package transport

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
	platform "github.com/dslzl/gork/app/platform"
)

func TestUsageFetcherPostsRateLimitPayloadAndRecordsSuccess(t *testing.T) {
	lease := controlproxy.NewProxyLease("usage-lease")
	runtime := &fakeUsageProxyRuntime{lease: lease}
	client := &fakeHTTPClient{responses: []HTTPResponse{{
		StatusCode: 200,
		Body:       []byte(`{"remainingQueries":7,"totalQueries":10}`),
	}}}

	result, err := UsageFetcher{
		ProxyRuntime: runtime,
		Client:       client,
		Timeout:      5 * time.Second,
	}.FetchUsage(context.Background(), "sso-token", "fast")
	if err != nil {
		t.Fatalf("FetchUsage returned error: %v", err)
	}
	if result["remainingQueries"] != float64(7) || result["totalQueries"] != float64(10) {
		t.Fatalf("usage result = %#v", result)
	}
	if len(runtime.acquireOptions) != 1 {
		t.Fatalf("acquire calls = %d, want 1", len(runtime.acquireOptions))
	}
	option := runtime.acquireOptions[0]
	if option.Scope != controlproxy.ProxyScopeApp ||
		option.Kind != controlproxy.RequestKindHTTP ||
		option.ClearanceOrigin != "https://grok.com" {
		t.Fatalf("acquire option = %#v", option)
	}
	if len(client.posts) != 1 {
		t.Fatalf("post calls = %d, want 1", len(client.posts))
	}
	request := client.posts[0]
	if request.URL != "https://grok.com/rest/rate-limits" ||
		string(request.Payload) != `{"modelName":"fast"}` ||
		request.Timeout != 5*time.Second ||
		request.Origin != "https://grok.com" ||
		request.Referer != "https://grok.com/" {
		t.Fatalf("usage request = %#v", request)
	}
	if request.Lease == nil || request.Lease.LeaseID != lease.LeaseID {
		t.Fatalf("request lease = %#v", request.Lease)
	}
	if !reflect.DeepEqual(runtime.feedback, []controlproxy.ProxyFeedback{{
		Kind:       controlproxy.ProxyFeedbackSuccess,
		StatusCode: usageIntPtr(200),
	}}) {
		t.Fatalf("feedback = %#v", runtime.feedback)
	}
}

func TestUsageFetcherRecordsUpstreamFeedback(t *testing.T) {
	runtime := &fakeUsageProxyRuntime{lease: controlproxy.NewProxyLease("usage-lease")}
	client := &fakeHTTPClient{responses: []HTTPResponse{{
		StatusCode: 429,
		Body:       []byte(`{"error":"resource-exhausted"}`),
	}}}

	_, err := UsageFetcher{ProxyRuntime: runtime, Client: client}.FetchUsage(context.Background(), "sso-token", "expert")
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("error = %T %v, want UpstreamError", err, err)
	}
	if upstream.Status != 429 {
		t.Fatalf("upstream status = %d, want 429", upstream.Status)
	}
	if len(runtime.feedback) != 1 ||
		runtime.feedback[0].Kind != controlproxy.ProxyFeedbackRateLimited ||
		runtime.feedback[0].StatusCode == nil ||
		*runtime.feedback[0].StatusCode != 429 {
		t.Fatalf("feedback = %#v", runtime.feedback)
	}
}

func TestUsageFetcherMissingProxyRuntimeReturnsUpstreamError(t *testing.T) {
	_, err := UsageFetcher{Client: &fakeHTTPClient{}}.FetchUsage(context.Background(), "sso-token", "fast")
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("error = %T %v, want UpstreamError", err, err)
	}
	if upstream.Status != 502 || upstream.Body != "usage proxy runtime is not configured" {
		t.Fatalf("upstream error = %#v", upstream)
	}
}

type fakeUsageProxyRuntime struct {
	lease          controlproxy.ProxyLease
	err            error
	acquireOptions []controlproxy.AcquireOptions
	feedback       []controlproxy.ProxyFeedback
}

func (r *fakeUsageProxyRuntime) Acquire(_ context.Context, options ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error) {
	if len(options) > 0 {
		r.acquireOptions = append(r.acquireOptions, options[0])
	}
	if r.err != nil {
		return controlproxy.ProxyLease{}, r.err
	}
	return r.lease, nil
}

func (r *fakeUsageProxyRuntime) Feedback(_ context.Context, _ controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error {
	r.feedback = append(r.feedback, feedback)
	return nil
}

func usageIntPtr(value int) *int {
	return &value
}
