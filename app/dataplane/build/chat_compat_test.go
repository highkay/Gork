package build

import (
	"encoding/json"
	"testing"
)

func TestBuildResponsesBodyMinimal(t *testing.T) {
	body, err := BuildResponsesBody("grok-4", []ChatMessage{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "hi"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "grok-4" {
		t.Fatalf("model=%v", payload["model"])
	}
	if payload["instructions"] != "be brief" {
		t.Fatalf("instructions=%v", payload["instructions"])
	}
	if payload["input"] != "User: hi" {
		t.Fatalf("input=%v", payload["input"])
	}
	if payload["stream"] != false {
		t.Fatalf("stream=%v", payload["stream"])
	}
}

func TestChatCompletionFromResponsesJSON(t *testing.T) {
	raw := []byte(`{"output_text":"hello world","output":[]}`)
	got, err := ChatCompletionFromResponsesJSON("build/grok-4", "id-1", raw)
	if err != nil {
		t.Fatal(err)
	}
	choices := got["choices"].([]map[string]any)
	msg := choices[0]["message"].(map[string]any)
	if msg["content"] != "hello world" {
		t.Fatalf("content=%v", msg["content"])
	}
}

func TestExtractChatMessagesContentParts(t *testing.T) {
	msgs := ExtractChatMessages([]map[string]any{
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "part-a"},
			map[string]any{"type": "input_text", "text": "part-b"},
		}},
	})
	if len(msgs) != 1 || msgs[0].Content != "part-a\npart-b" {
		t.Fatalf("%#v", msgs)
	}
}

func TestBuildResponsesBodyRejectsEmpty(t *testing.T) {
	_, err := BuildResponsesBody("m", nil, false)
	if err == nil {
		t.Fatal("expected error")
	}
}
