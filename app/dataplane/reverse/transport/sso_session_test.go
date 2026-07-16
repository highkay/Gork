package transport

import (
	"context"
	"testing"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
)

type fakeSSOSessionProxyRuntime struct {
	feedbacks []controlproxy.ProxyFeedback
}

func (f *fakeSSOSessionProxyRuntime) Acquire(context.Context, ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error) {
	return controlproxy.ProxyLease{}, nil
}

func (f *fakeSSOSessionProxyRuntime) Feedback(_ context.Context, _ controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error {
	f.feedbacks = append(f.feedbacks, feedback)
	return nil
}

func TestSSOSessionProberClassifiesSignInAsSessionInvalid(t *testing.T) {
	client := &fakeHTTPClient{responses: []HTTPResponse{{
		StatusCode: 200,
		Body:       []byte("login page"),
		FinalURL:   "https://accounts.x.ai/sign-in?next=/",
	}}}
	runtime := &fakeSSOSessionProxyRuntime{}
	prober := SSOSessionProber{ProxyRuntime: runtime, Client: client, Endpoint: "https://accounts.x.ai/"}

	err := prober.ProbeSession(context.Background(), "sso-token")
	if !protocol.IsSessionInvalidError(err) {
		t.Fatalf("err = %v, want session invalid", err)
	}
	if len(client.gets) != 1 {
		t.Fatalf("get requests = %#v", client.gets)
	}
	req := client.gets[0]
	if req.URL != "https://accounts.x.ai/" {
		t.Fatalf("url = %q", req.URL)
	}
	if req.Headers["Sec-Fetch-Mode"] != "navigate" {
		t.Fatalf("headers = %#v", req.Headers)
	}
}

func TestSSOSessionProberOK(t *testing.T) {
	client := &fakeHTTPClient{responses: []HTTPResponse{{
		StatusCode: 200,
		Body:       []byte("ok"),
		FinalURL:   "https://accounts.x.ai/",
	}}}
	runtime := &fakeSSOSessionProxyRuntime{}
	prober := SSOSessionProber{ProxyRuntime: runtime, Client: client, Endpoint: "https://accounts.x.ai/"}

	if err := prober.ProbeSession(context.Background(), "sso-token"); err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}
	if len(runtime.feedbacks) != 1 || runtime.feedbacks[0].Kind != controlproxy.ProxyFeedbackSuccess {
		t.Fatalf("feedbacks = %#v", runtime.feedbacks)
	}
}

func TestSSOSessionProberCloudflareNotTerminal(t *testing.T) {
	client := &fakeHTTPClient{responses: []HTTPResponse{{
		StatusCode: 403,
		Body:       []byte("Just a Moment cloudflare"),
		FinalURL:   "https://accounts.x.ai/",
	}}}
	prober := SSOSessionProber{
		ProxyRuntime: &fakeSSOSessionProxyRuntime{},
		Client:       client,
		Endpoint:     "https://accounts.x.ai/",
	}

	err := prober.ProbeSession(context.Background(), "sso-token")
	if err == nil || protocol.IsTerminalSSOFailure(err) {
		t.Fatalf("cloudflare should be non-terminal error, got %v", err)
	}
}
