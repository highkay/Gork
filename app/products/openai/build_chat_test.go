package openai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
)

type stubBuildDir struct {
	accounts []buildaccount.Account
}

func (s *stubBuildDir) ListActive(context.Context, time.Time) ([]buildaccount.Account, error) {
	return s.accounts, nil
}
func (s *stubBuildDir) UpdateTokens(context.Context, int64, string, string, time.Time) error {
	return nil
}
func (s *stubBuildDir) SetStatus(context.Context, int64, string, string) error { return nil }

type stubBuildHTTP struct {
	status int
	body   string
}

func (s *stubBuildHTTP) CreateResponse(context.Context, build.RequestMeta, io.Reader) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     make(http.Header),
	}, nil
}

type stubOAuth struct{}

func (stubOAuth) Refresh(context.Context, string) (build.TokenPayload, error) {
	return build.TokenPayload{}, &build.RefreshError{Code: "invalid_grant", Permanent: true}
}

func TestBuildCompletionsFeatureOffUnknownModel(t *testing.T) {
	prev := buildFeatureEnabled
	buildFeatureEnabled = func() bool { return false }
	defer func() { buildFeatureEnabled = prev }()

	_, err := Completions(context.Background(), chatCompletionOptions{
		Model:    "build/grok-4",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "Unknown model") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildCompletionsHappyPath(t *testing.T) {
	prevF := buildFeatureEnabled
	prevD := buildAccountDir
	prevC := buildAPIClient
	prevO := buildOAuthClient
	buildFeatureEnabled = func() bool { return true }
	buildAccountDir = func() buildAccountDirectory {
		return &stubBuildDir{accounts: []buildaccount.Account{{
			ID: 1, AccessToken: "at", UserID: "u1", Status: buildaccount.StatusActive,
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		}}}
	}
	buildAPIClient = func() buildHTTPClient {
		return &stubBuildHTTP{status: 200, body: `{"output_text":"pong"}`}
	}
	buildOAuthClient = func() buildTokenRefresher { return stubOAuth{} }
	defer func() {
		buildFeatureEnabled = prevF
		buildAccountDir = prevD
		buildAPIClient = prevC
		buildOAuthClient = prevO
	}()

	got, err := Completions(context.Background(), chatCompletionOptions{
		Model:    "build/grok-4",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	choices := got.Response["choices"].([]map[string]any)
	msg := choices[0]["message"].(map[string]any)
	if msg["content"] != "pong" {
		t.Fatalf("%#v", got.Response)
	}
}

func TestBuildCompletionsNoAccounts(t *testing.T) {
	prevF := buildFeatureEnabled
	prevD := buildAccountDir
	buildFeatureEnabled = func() bool { return true }
	buildAccountDir = func() buildAccountDirectory { return &stubBuildDir{} }
	defer func() {
		buildFeatureEnabled = prevF
		buildAccountDir = prevD
	}()

	_, err := Completions(context.Background(), chatCompletionOptions{
		Model:    "build/grok-4",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
