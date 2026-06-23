package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
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
