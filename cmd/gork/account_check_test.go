package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGorkCommandAccountCheckJSON(t *testing.T) {
	t.Setenv("ACCOUNT_STORAGE", "local")
	t.Setenv("ACCOUNT_LOCAL_PATH", filepath.Join(t.TempDir(), "accounts.db"))
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	handled, code, err := runGorkCommand(context.Background(), []string{"account", "check", "--json"}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("runGorkCommand returned error: %v", err)
	}
	if !handled || code != 0 {
		t.Fatalf("handled/code = %t/%d stderr=%s", handled, code, stderr.String())
	}
	var body map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatalf("decode stdout: %v; %s", err, stdout.String())
	}
	if body["ok"] != true || body["issues"] == nil {
		t.Fatalf("body = %#v", body)
	}
}

func TestRunGorkCommandRejectsUnknownAccountCheckFlag(t *testing.T) {
	handled, code, err := runGorkCommand(context.Background(), []string{"account", "check", "--bad"}, &bytes.Buffer{}, &bytes.Buffer{})
	if !handled || code != 2 || err == nil {
		t.Fatalf("handled/code/err = %t/%d/%v", handled, code, err)
	}
}

func TestRunGorkCommandProtocolCheckJSON(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	handled, code, err := runGorkCommand(context.Background(), []string{"protocol-check", "--target", "chat,voice", "--json"}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("runGorkCommand returned error: %v", err)
	}
	if !handled || code != 0 {
		t.Fatalf("handled/code = %t/%d stderr=%s", handled, code, stderr.String())
	}
	var body []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatalf("decode stdout: %v; %s", err, stdout.String())
	}
	if len(body) != 2 || body[0]["target"] != "chat" || body[1]["target"] != "voice" {
		t.Fatalf("protocol check body = %#v", body)
	}
	if body[0]["request_id"] == "" || body[0]["checked_at"] == "" {
		t.Fatalf("protocol check metadata missing: %#v", body[0])
	}
}

func TestRunGorkCommandHealthcheckOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	handled, code, err := runGorkCommand(context.Background(), []string{"healthcheck", "--url", server.URL, "--timeout", "1s"}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("runGorkCommand returned error: %v", err)
	}
	if !handled || code != 0 || strings.TrimSpace(stdout.String()) != "ok" {
		t.Fatalf("handled/code/stdout/stderr = %t/%d/%q/%q", handled, code, stdout.String(), stderr.String())
	}
}

func TestRunGorkCommandHealthcheckFailsOnBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	handled, code, err := runGorkCommand(context.Background(), []string{"healthcheck", "--url", server.URL}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("runGorkCommand returned error: %v", err)
	}
	if !handled || code != 1 || !strings.Contains(stderr.String(), "status=503") {
		t.Fatalf("handled/code/stdout/stderr = %t/%d/%q/%q", handled, code, stdout.String(), stderr.String())
	}
}

func TestRunGorkCommandHealthcheckDefaultURLUsesEnvironment(t *testing.T) {
	t.Setenv("GORK_HEALTHCHECK_URL", "")
	t.Setenv("PORT", "19090")
	t.Setenv("SERVER_PORT", "18080")
	if got := defaultHealthcheckURL(); got != "http://127.0.0.1:19090/health" {
		t.Fatalf("defaultHealthcheckURL with PORT = %q", got)
	}
	t.Setenv("PORT", "")
	if got := defaultHealthcheckURL(); got != "http://127.0.0.1:18080/health" {
		t.Fatalf("defaultHealthcheckURL with SERVER_PORT = %q", got)
	}
	t.Setenv("GORK_HEALTHCHECK_URL", "http://example.test/healthz")
	if got := defaultHealthcheckURL(); got != "http://example.test/healthz" {
		t.Fatalf("defaultHealthcheckURL override = %q", got)
	}
}

func TestRunGorkCommandHealthcheckRejectsUnknownFlag(t *testing.T) {
	handled, code, err := runGorkCommand(context.Background(), []string{"healthcheck", "--bad"}, &bytes.Buffer{}, &bytes.Buffer{})
	if !handled || code != 2 || err == nil {
		t.Fatalf("handled/code/err = %t/%d/%v", handled, code, err)
	}
}
