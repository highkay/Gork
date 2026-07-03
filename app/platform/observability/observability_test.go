package observability

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewarePropagatesRequestIDAndRecordsMetrics(t *testing.T) {
	ResetForTest()
	var accessLog bytes.Buffer
	SetAccessLogWriterForTest(&accessLog)

	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestID(r.Context()); got != "req-test" {
			t.Fatalf("request id in context = %q", got)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?api_key=secret", strings.NewReader(""))
	req.Header.Set("X-Request-ID", "req-test")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != "req-test" {
		t.Fatalf("response request id = %q", rec.Header().Get("X-Request-ID"))
	}
	metrics := MetricsText()
	if !strings.Contains(metrics, `gork_http_requests_total{method="POST",path="/v1/chat/completions",status="201"} 1`) {
		t.Fatalf("metrics missing request count:\n%s", metrics)
	}
	logged := accessLog.String()
	if !strings.Contains(logged, `"request_id":"req-test"`) || strings.Contains(logged, "secret") {
		t.Fatalf("access log not structured/sanitized: %s", logged)
	}
}

func TestMiddlewareGeneratesRequestIDWhenMissing(t *testing.T) {
	ResetForTest()
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) == "" {
			t.Fatal("request id was not injected")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing response request id")
	}
}

func TestMiddlewarePreservesFlusher(t *testing.T) {
	ResetForTest()
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("response writer does not expose http.Flusher")
		}
		flusher.Flush()
		_, _ = w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))
	if !rec.Flushed {
		t.Fatalf("underlying recorder was not flushed")
	}
}

func TestUpstreamErrorRingAndSnapshot(t *testing.T) {
	ResetForTest()
	for i := 0; i < 12; i++ {
		RecordUpstreamError(UpstreamError{
			Product:    "openai",
			Model:      "model-a",
			StatusCode: 500 + i,
			Message:    "upstream failed",
		})
	}

	errors := RecentUpstreamErrors(20)
	if len(errors) != 10 {
		t.Fatalf("recent error count = %d, want ring limit 10", len(errors))
	}
	if errors[0].StatusCode != 502 || errors[len(errors)-1].StatusCode != 511 {
		t.Fatalf("ring order = %#v", errors)
	}
	snapshot := Snapshot()
	if snapshot["upstream_error_count"] != 12 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if !strings.Contains(MetricsText(), `gork_upstream_errors_total{product="openai",status="500"} 1`) {
		t.Fatalf("metrics missing upstream error count:\n%s", MetricsText())
	}
}
