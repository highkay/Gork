package build

import (
	"strings"
	"testing"
)

func TestChatStreamFramesFunctionCallSSE(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_weather","name":"get_weather","arguments":""}}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"city\":"}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"\"SF\"}"}`,
		``,
		`data: {"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"city\":\"SF\"}"}`,
		``,
		`data: {"type":"response.completed"}`,
		``,
	}, "\n")
	frames, err := ChatStreamFramesFromResponsesSSE("build/grok-4", "cmpl-tools", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(frames, "")
	if !strings.Contains(joined, `"tool_calls"`) {
		t.Fatalf("missing tool_calls: %s", joined)
	}
	if !strings.Contains(joined, "get_weather") {
		t.Fatalf("missing name: %s", joined)
	}
	if !strings.Contains(joined, "call_weather") {
		t.Fatalf("missing id: %s", joined)
	}
	if !strings.Contains(joined, `"finish_reason":"tool_calls"`) {
		t.Fatalf("finish_reason: %s", joined)
	}
	if frames[len(frames)-1] != "data: [DONE]\n\n" {
		t.Fatalf("last=%q", frames[len(frames)-1])
	}
	// 应有 arguments 增量
	if !strings.Contains(joined, `"arguments":"{\"city\":"`) && !strings.Contains(joined, `\"city\"`) {
		t.Fatalf("missing arg deltas: %s", joined)
	}
}

func TestChatStreamFramesPlainJSONToolCalls(t *testing.T) {
	raw := `{"id":"r1","output":[{"type":"function_call","call_id":"c1","name":"lookup","arguments":"{\"q\":1}"}]}`
	frames, err := ChatStreamFramesFromResponsesSSE("build/x", "id-json", strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(frames, "")
	if !strings.Contains(joined, "lookup") || !strings.Contains(joined, "tool_calls") {
		t.Fatalf("%s", joined)
	}
	if !strings.Contains(joined, `"finish_reason":"tool_calls"`) {
		t.Fatalf("finish: %s", joined)
	}
}

func TestChatStreamFramesTextStillStop(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hi"}`,
		``,
		`data: {"type":"response.completed"}`,
		``,
	}, "\n")
	frames, err := ChatStreamFramesFromResponsesSSE("build/x", "id-t", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(frames, "")
	if !strings.Contains(joined, `"finish_reason":"stop"`) {
		t.Fatalf("%s", joined)
	}
	if strings.Contains(joined, "tool_calls") {
		t.Fatalf("unexpected tools: %s", joined)
	}
}
