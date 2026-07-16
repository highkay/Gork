package build

import (
	"encoding/json"
	"testing"
)

func TestBuildResponsesInputStructuredMultiTurn(t *testing.T) {
	instructions, input, err := BuildResponsesInput([]ChatMessage{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "weather?"},
		{
			Role: "assistant",
			ToolCalls: []map[string]any{
				{
					"id":   "call_1",
					"type": "function",
					"function": map[string]any{
						"name":      "get_weather",
						"arguments": `{"city":"NYC"}`,
					},
				},
			},
		},
		{Role: "tool", ToolCallID: "call_1", Content: "sunny"},
		{Role: "user", Content: "thanks"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if instructions != "be brief" {
		t.Fatalf("instructions=%q", instructions)
	}
	items, ok := input.([]any)
	if !ok || len(items) != 4 {
		t.Fatalf("input=%#v", input)
	}
	user0 := items[0].(map[string]any)
	if user0["type"] != "message" || user0["role"] != "user" || user0["content"] != "weather?" {
		t.Fatalf("user0=%#v", user0)
	}
	call := items[1].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "call_1" || call["name"] != "get_weather" {
		t.Fatalf("call=%#v", call)
	}
	if call["arguments"] != `{"city":"NYC"}` {
		t.Fatalf("args=%#v", call["arguments"])
	}
	out := items[2].(map[string]any)
	if out["type"] != "function_call_output" || out["call_id"] != "call_1" || out["output"] != "sunny" {
		t.Fatalf("out=%#v", out)
	}
	user1 := items[3].(map[string]any)
	if user1["content"] != "thanks" {
		t.Fatalf("user1=%#v", user1)
	}
}

func TestBuildResponsesBodyUsesStructuredInput(t *testing.T) {
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
	if payload["instructions"] != "be brief" {
		t.Fatalf("instructions=%v", payload["instructions"])
	}
	items, ok := payload["input"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("input=%#v", payload["input"])
	}
	msg := items[0].(map[string]any)
	if msg["type"] != "message" || msg["role"] != "user" || msg["content"] != "hi" {
		t.Fatalf("msg=%#v", msg)
	}
}

func TestBuildResponsesInputToolMissingCallID(t *testing.T) {
	_, _, err := BuildResponsesInput([]ChatMessage{
		{Role: "user", Content: "hi"},
		{Role: "tool", Content: "x"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExtractChatMessagesPreservesToolHistory(t *testing.T) {
	msgs := ExtractChatMessages([]map[string]any{
		{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": "c1", "function": map[string]any{"name": "f", "arguments": "{}"}},
		}},
		{"role": "tool", "tool_call_id": "c1", "content": "ok"},
	})
	if len(msgs) != 2 {
		t.Fatalf("%#v", msgs)
	}
	if len(msgs[0].ToolCalls) != 1 || msgs[1].ToolCallID != "c1" {
		t.Fatalf("%#v", msgs)
	}
}
