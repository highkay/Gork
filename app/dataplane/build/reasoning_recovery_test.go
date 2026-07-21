package build

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStripReasoningEncryptedContent(t *testing.T) {
	body := []byte(`{
		"model":"grok-4",
		"input":[
			{"type":"reasoning","encrypted_content":"opaque","summary":[{"type":"summary_text","text":"think"}]},
			{"type":"message","role":"user","content":"continue"}
		],
		"prompt_cache_key":"sess"
	}`)
	out, changed := stripReasoningEncryptedContent(body)
	if !changed {
		t.Fatal("expected change")
	}
	if strings.Contains(string(out), "encrypted_content") {
		t.Fatalf("encrypted leaked: %s", out)
	}
	if !strings.Contains(string(out), `"text":"think"`) {
		t.Fatalf("summary lost: %s", out)
	}
}

func TestStripReasoningEncryptedContentDropsOpaqueOnly(t *testing.T) {
	body := []byte(`{"input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"message","role":"user","content":"hi"}]}`)
	out, changed := stripReasoningEncryptedContent(body)
	if !changed {
		t.Fatal("expected change")
	}
	if strings.Contains(string(out), `"type":"reasoning"`) {
		t.Fatalf("opaque-only reasoning should drop: %s", out)
	}
}

func TestIsReasoningDecodeFailure(t *testing.T) {
	if !isReasoningDecodeFailure([]byte(`{"error":{"message":"Could not decode the compaction blob. Ensure it is unmodified."}}`)) {
		t.Fatal("expected compaction marker")
	}
	if !isReasoningDecodeFailure([]byte(`could not decrypt the provided encrypted_content`)) {
		t.Fatal("expected decrypt marker")
	}
	if isReasoningDecodeFailure([]byte(`{"error":"unrelated"}`)) {
		t.Fatal("false positive")
	}
}

func TestCreateResponseRecoveringStripsAndSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		data, _ := io.ReadAll(r.Body)
		switch n {
		case 1:
			if !strings.Contains(string(data), `"encrypted_content":"opaque"`) {
				t.Fatalf("first body=%s", data)
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Could not decode the compaction blob."}}`))
		case 2:
			if strings.Contains(string(data), "encrypted_content") {
				t.Fatalf("second still has encrypted: %s", data)
			}
			if r.Header.Get("x-grok-session-id") == "" {
				t.Fatal("session should remain for strip retry")
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"ok","output":[]}`))
		default:
			t.Fatalf("unexpected call %d", n)
		}
	}))
	defer srv.Close()

	client := NewAPIClient(srv.Client(), ClientConfig{BaseURL: srv.URL + "/v1"}.Normalize())
	body := []byte(`{"model":"grok-4","input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"message","role":"user","content":"hi"}],"prompt_cache_key":"k1"}`)
	resp, outcome, err := client.CreateResponseRecovering(context.Background(), RequestMeta{
		AccessToken: "t", Model: "grok-4", PromptCacheKey: "k1",
	}, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if !outcome.EncryptedContentDowngraded || outcome.SessionReset || outcome.Failed {
		t.Fatalf("outcome=%+v", outcome)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestCreateResponseRecoveringPreservesRateLimitAfterSessionReset(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		data, _ := io.ReadAll(r.Body)
		switch n {
		case 1:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Could not decrypt the provided encrypted_content"}}`))
		case 2:
			// strip retry still decode fails
			if strings.Contains(string(data), "encrypted_content") {
				t.Fatalf("strip body still encrypted: %s", data)
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Could not decode the compaction blob."}}`))
		case 3:
			// session reset: no prompt_cache_key, no session header
			if strings.Contains(string(data), "prompt_cache_key") {
				t.Fatalf("session reset body still has cache key: %s", data)
			}
			if r.Header.Get("x-grok-session-id") != "" {
				t.Fatalf("session should be cleared, got %q", r.Header.Get("x-grok-session-id"))
			}
			w.Header().Set("Retry-After", "17")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited after session reset"}}`))
		default:
			t.Fatalf("unexpected call %d", n)
		}
	}))
	defer srv.Close()

	client := NewAPIClient(srv.Client(), ClientConfig{BaseURL: srv.URL + "/v1"}.Normalize())
	body := []byte(`{"model":"grok-4","input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"message","role":"user","content":"hi"}],"prompt_cache_key":"session-1"}`)
	resp, outcome, err := client.CreateResponseRecovering(context.Background(), RequestMeta{
		AccessToken: "t", Model: "grok-4", PromptCacheKey: "session-1",
	}, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if calls.Load() != 3 || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("calls=%d status=%d body=%s", calls.Load(), resp.StatusCode, raw)
	}
	if resp.Header.Get("Retry-After") != "17" {
		t.Fatalf("Retry-After=%q", resp.Header.Get("Retry-After"))
	}
	if !outcome.EncryptedContentDowngraded || !outcome.SessionReset || outcome.Failed {
		t.Fatalf("outcome=%+v", outcome)
	}
	if !strings.Contains(string(raw), "rate limited after session reset") {
		t.Fatalf("body=%s", raw)
	}
}
