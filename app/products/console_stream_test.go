package products

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
)

func TestStreamConsoleChatConfiguresDefaultPoster(t *testing.T) {
	poster := &fakeConsoleStreamPoster{response: protocol.ConsoleStreamResponse{
		StatusCode: 200,
		Lines: []string{
			"event: response.output_text.delta",
			`data: {"delta":"ok"}`,
			"data: [DONE]",
		},
	}}
	oldFactory := consoleStreamPosterFactory
	consoleStreamPosterFactory = func() protocol.ConsoleStreamPoster { return poster }
	t.Cleanup(func() { consoleStreamPosterFactory = oldFactory })

	events, err := StreamConsoleChat(context.Background(), "tok", map[string]any{"model": "x"}, 42)
	if err != nil {
		t.Fatalf("StreamConsoleChat returned error: %v", err)
	}
	if poster.request.Token != "tok" || poster.request.TimeoutS != 42 || poster.request.Payload["model"] != "x" {
		t.Fatalf("poster request mismatch: %#v", poster.request)
	}
	want := []protocol.ConsoleStreamEvent{{EventType: "response.output_text.delta", Data: `{"delta":"ok"}`}}
	if !reflect.DeepEqual(want, events) {
		t.Fatalf("events mismatch: %#v", events)
	}
}

func TestConsoleHTTPOptionsIncludesClusterHeader(t *testing.T) {
	options := consoleHTTPOptions(protocol.ConsoleStreamRequest{TimeoutS: 42})
	if !options.ConsoleHeaders {
		t.Fatalf("console HTTP options should request console headers")
	}
	if options.ExtraHeaders["x-cluster"] != "https://us-east-1.api.x.ai" {
		t.Fatalf("console HTTP extra headers = %#v", options.ExtraHeaders)
	}
}

func TestConsoleHTTPEndpointUsesResponsesAPI(t *testing.T) {
	if got := consoleHTTPEndpoint(); got != reverseruntime.ConsoleResponses {
		t.Fatalf("console endpoint = %q, want %q", got, reverseruntime.ConsoleResponses)
	}
}

func TestRedactConsoleDiagnosticTextKeepsReasonAndHidesSecrets(t *testing.T) {
	raw := `token expired sso=abc123; sso-rw=rw456; cf_clearance=cf789 Authorization: Bearer bearer-token-secret abcdefghijklmnopqrstuvwxyz123456`
	got := redactConsoleDiagnosticText(raw)
	for _, secret := range []string{"abc123", "rw456", "cf789", "bearer-token-secret", "abcdefghijklmnopqrstuvwxyz123456"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted diagnostic leaked %q in %q", secret, got)
		}
	}
	for _, want := range []string{"token expired", "sso=<redacted>", "sso-rw=<redacted>", "cf_clearance=<redacted>", "Bearer <redacted>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("redacted diagnostic missing %q in %q", want, got)
		}
	}
}

type fakeConsoleStreamPoster struct {
	request  protocol.ConsoleStreamRequest
	response protocol.ConsoleStreamResponse
	err      error
}

func (f *fakeConsoleStreamPoster) PostConsoleStream(_ context.Context, request protocol.ConsoleStreamRequest) (protocol.ConsoleStreamResponse, error) {
	f.request = request
	return f.response, f.err
}
