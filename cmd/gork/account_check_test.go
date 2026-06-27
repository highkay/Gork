package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestRunGorkCommandConfigValidateAndDocs(t *testing.T) {
	dir := t.TempDir()
	defaults := filepath.Join(dir, "defaults.toml")
	user := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(defaults, []byte("[server]\nport = 8000\n[proxy]\nurl = \"http://127.0.0.1:8080\"\n"), 0o600); err != nil {
		t.Fatalf("write defaults: %v", err)
	}
	if err := os.WriteFile(user, []byte("[server]\nport = 9000\n"), 0o600); err != nil {
		t.Fatalf("write user: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	handled, code, err := runGorkCommand(context.Background(), []string{"config", "validate", "--defaults", defaults, "--config", user}, &stdout, &stderr)
	if err != nil || !handled || code != 0 || strings.TrimSpace(stdout.String()) != "ok" {
		t.Fatalf("validate handled/code/err/stdout/stderr = %t/%d/%v/%q/%q", handled, code, err, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	handled, code, err = runGorkCommand(context.Background(), []string{"config", "docs", "--defaults", defaults}, &stdout, &stderr)
	if err != nil || !handled || code != 0 || !strings.Contains(stdout.String(), "GROK_SERVER_PORT") {
		t.Fatalf("docs handled/code/err/stdout/stderr = %t/%d/%v/%q/%q", handled, code, err, stdout.String(), stderr.String())
	}
}

func TestRunGorkCommandConfigValidateReportsFieldError(t *testing.T) {
	dir := t.TempDir()
	defaults := filepath.Join(dir, "defaults.toml")
	user := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(defaults, []byte("[server]\nport = 8000\n"), 0o600); err != nil {
		t.Fatalf("write defaults: %v", err)
	}
	if err := os.WriteFile(user, []byte("[server]\nport = \"bad\"\n"), 0o600); err != nil {
		t.Fatalf("write user: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	handled, code, err := runGorkCommand(context.Background(), []string{"config", "validate", "--defaults", defaults, "--config", user}, &stdout, &stderr)
	if err != nil || !handled || code != 1 || !strings.Contains(stderr.String(), "server.port") {
		t.Fatalf("validate handled/code/err/stdout/stderr = %t/%d/%v/%q/%q", handled, code, err, stdout.String(), stderr.String())
	}
}

func TestValidateConfigEnvAcceptsStringListOverrides(t *testing.T) {
	defaults := map[string]any{
		"proxy": map[string]any{
			"egress": map[string]any{
				"proxy_pool": []any{},
			},
		},
		"retry": map[string]any{
			"reset_session_status_codes": []any{403},
		},
	}
	issues := validateConfigEnv(defaults, "GROK_", map[string]string{
		"GROK_PROXY_EGRESS_PROXY_POOL":          "http://proxy-a:8080,http://proxy-b:8080",
		"GROK_RETRY_RESET_SESSION_STATUS_CODES": "403,429",
	})
	if len(issues) != 0 {
		t.Fatalf("validateConfigEnv issues = %#v", issues)
	}
}
