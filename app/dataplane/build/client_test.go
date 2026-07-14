package build

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientListModels(t *testing.T) {
	// Given: 上游返回 data[].id
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		assertBuildAuthHeaders(t, r, "tok-1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "grok-4"}, {"id": "grok-3"}},
		})
	}))
	defer srv.Close()

	client := NewAPIClient(srv.Client(), ClientConfig{BaseURL: srv.URL + "/v1"}.Normalize())
	// When
	models, err := client.ListModels(context.Background(), "tok-1")
	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != "grok-4" || models[1] != "grok-3" {
		t.Fatalf("models=%v", models)
	}
}

func TestClientCreateResponseJSON(t *testing.T) {
	// Given
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		assertBuildAuthHeaders(t, r, "access")
		if r.Header.Get("Accept") != "application/json" {
			t.Fatalf("Accept=%q", r.Header.Get("Accept"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type=%q", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("x-grok-model-override") != "grok-4" {
			t.Fatalf("model-override=%q", r.Header.Get("x-grok-model-override"))
		}
		if r.Header.Get("x-userid") != "user-9" {
			t.Fatalf("x-userid=%q", r.Header.Get("x-userid"))
		}
		if r.Header.Get("x-grok-agent-id") == "" || r.Header.Get("x-grok-session-id") == "" {
			t.Fatal("missing agent/session ids")
		}
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[]}`))
	}))
	defer srv.Close()

	client := NewAPIClient(srv.Client(), ClientConfig{BaseURL: srv.URL + "/v1"}.Normalize())
	payload := []byte(`{"model":"grok-4","input":"hi"}`)
	// When
	resp, err := client.CreateResponse(context.Background(), RequestMeta{
		AccessToken: "access",
		UserID:      "user-9",
		Model:       "grok-4",
		Stream:      false,
	}, bytes.NewReader(payload))
	// Then
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	data, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(data, []byte(`"resp_1"`)) {
		t.Fatalf("body=%s", data)
	}
	if !bytes.Equal(gotBody, payload) {
		t.Fatalf("upstream body=%s", gotBody)
	}
}

func TestClientCreateResponseStreamAccept(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("Accept=%q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {}\n\n"))
	}))
	defer srv.Close()

	client := NewAPIClient(srv.Client(), ClientConfig{BaseURL: srv.URL + "/v1"}.Normalize())
	resp, err := client.CreateResponse(context.Background(), RequestMeta{
		AccessToken: "t",
		Stream:      true,
	}, strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
}

func TestClientGunzipsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write([]byte(`{"data":[{"id":"m1"}]}`))
		_ = gz.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	client := NewAPIClient(srv.Client(), ClientConfig{BaseURL: srv.URL + "/v1"}.Normalize())
	models, err := client.ListModels(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0] != "m1" {
		t.Fatalf("models=%v", models)
	}
}

func TestClientListModelsNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	defer srv.Close()

	client := NewAPIClient(srv.Client(), ClientConfig{BaseURL: srv.URL + "/v1"}.Normalize())
	_, err := client.ListModels(context.Background(), "bad")
	if err == nil {
		t.Fatal("expected error")
	}
	var ue *UpstreamError
	if !asUpstream(err, &ue) || ue.Status != http.StatusUnauthorized {
		t.Fatalf("err=%v", err)
	}
}

func assertBuildAuthHeaders(t *testing.T, r *http.Request, token string) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+token {
		t.Fatalf("Authorization=%q", got)
	}
	if r.Header.Get("X-XAI-Token-Auth") != DefaultTokenAuth {
		t.Fatalf("TokenAuth=%q", r.Header.Get("X-XAI-Token-Auth"))
	}
	if r.Header.Get("x-grok-client-version") != DefaultClientVersion {
		t.Fatalf("version=%q", r.Header.Get("x-grok-client-version"))
	}
	if r.Header.Get("x-grok-client-identifier") != DefaultClientIDName {
		t.Fatalf("identifier=%q", r.Header.Get("x-grok-client-identifier"))
	}
	if r.Header.Get("x-grok-client-surface") != "tui" {
		t.Fatalf("surface=%q", r.Header.Get("x-grok-client-surface"))
	}
	if r.Header.Get("User-Agent") == "" {
		t.Fatal("missing User-Agent")
	}
}

func asUpstream(err error, target **UpstreamError) bool {
	if err == nil {
		return false
	}
	var ue *UpstreamError
	if !errorsAs(err, &ue) {
		return false
	}
	*target = ue
	return true
}
