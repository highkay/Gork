package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateConfigInputsReportsUnknownEnv(t *testing.T) {
	defaults := filepath.Join(t.TempDir(), "defaults.toml")
	if err := os.WriteFile(defaults, []byte("[server]\nport = 8000\n"), 0o600); err != nil {
		t.Fatalf("write defaults: %v", err)
	}

	issues, err := validateConfigInputs(defaults, "", "GROK_", map[string]string{
		"GROK_UNKNOWN_VALUE": "1",
	})
	if err != nil {
		t.Fatalf("validateConfigInputs returned error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues = %#v", issues)
	}
	if issues[0].Key != "GROK_UNKNOWN_VALUE" || issues[0].Code != "unknown_env" {
		t.Fatalf("issue = %#v", issues[0])
	}
}
