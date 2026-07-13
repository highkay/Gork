package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

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
	if body[0]["status"] != "ok" || body[0]["error_type"] != nil {
		t.Fatalf("protocol check should use endpoint checker: %#v", body[0])
	}
	if body[0]["request_id"] == "" || body[0]["checked_at"] == "" {
		t.Fatalf("protocol check metadata missing: %#v", body[0])
	}
}

func TestRunGorkCommandProtocolCheckAcceptsEqualsTargetFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	handled, code, err := runGorkCommand(context.Background(), []string{"protocol-check", "--target=chat,voice", "--json"}, &stdout, &stderr)

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
}

func TestRunGorkCommandProtocolCheckFailsWhenNoTargetsChecked(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	handled, code, err := runGorkCommand(context.Background(), []string{"protocol-check", "--target", ",", "--json"}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("runGorkCommand returned error: %v", err)
	}
	if !handled || code == 0 {
		t.Fatalf("handled/code = %t/%d stderr=%s", handled, code, stderr.String())
	}
	var body []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatalf("decode stdout: %v; %s", err, stdout.String())
	}
	if len(body) != 0 {
		t.Fatalf("protocol check body = %#v", body)
	}
}
