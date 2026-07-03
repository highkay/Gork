package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
