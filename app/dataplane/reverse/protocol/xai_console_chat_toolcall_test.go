package protocol

import (
	"reflect"
	"testing"
)

func TestBuildConsolePayloadForwardsClientTools(t *testing.T) {
	payload := BuildConsolePayload(ConsolePayloadOptions{
		Model: "grok-4.3-console", // maps to grok-4.3 (search tools default)
		Messages: []map[string]any{
			{"role": "user", "content": "hi"},
		},
		Tools: []map[string]any{
			{"type": "function", "function": map[string]any{"name": "get_weather", "description": "d", "parameters": map[string]any{"x": 1}}},
			{"type": "function", "function": map[string]any{"name": "web_search"}}, // builtin → dropped
		},
		ToolChoice: map[string]any{"type": "function", "function": map[string]any{"name": "get_weather"}},
	})
	tools, _ := payload["tools"].([]map[string]any)
	// default web_search + x_search, plus client get_weather (web_search dropped as builtin dup)
	var names []string
	for _, tl := range tools {
		names = append(names, stringFromAny(tl["name"]))
		if stringFromAny(tl["type"]) == "" {
			t.Fatalf("tool missing type: %#v", tl)
		}
	}
	hasGetWeather := false
	for _, n := range names {
		if n == "get_weather" {
			hasGetWeather = true
		}
	}
	if !hasGetWeather {
		t.Fatalf("client tool get_weather should be forwarded: %v", names)
	}
	choice, ok := payload["tool_choice"].(map[string]any)
	if !ok || choice["name"] != "get_weather" {
		t.Fatalf("tool_choice should map to get_weather: %#v", payload["tool_choice"])
	}
}

func TestBuildConsolePayloadToolMessageAndAssistantCalls(t *testing.T) {
	payload := BuildConsolePayload(ConsolePayloadOptions{
		Model: "grok-4.3-console",
		Messages: []map[string]any{
			{"role": "user", "content": "weather?"},
			{"role": "assistant", "content": "", "tool_calls": []any{
				map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "get_weather", "arguments": `{"city":"SF"}`}},
			}},
			{"role": "tool", "tool_call_id": "call_1", "content": "sunny"},
		},
	})
	input := payload["input"].([]map[string]any)
	// user msg, assistant function_call, tool function_call_output
	var fnCall, fnOut map[string]any
	for _, it := range input {
		switch it["type"] {
		case "function_call":
			fnCall = it
		case "function_call_output":
			fnOut = it
		}
	}
	if fnCall == nil || fnCall["call_id"] != "call_1" || fnCall["name"] != "get_weather" {
		t.Fatalf("assistant tool_call not converted: %#v", input)
	}
	if fnOut == nil || fnOut["call_id"] != "call_1" || fnOut["output"] != "sunny" {
		t.Fatalf("tool message not converted to function_call_output: %#v", input)
	}
}

func TestConsoleAdapterParsesClientFunctionCall(t *testing.T) {
	allowed := map[string]struct{}{"get_weather": {}}
	a := NewConsoleStreamAdapterWithTools(allowed)
	feedAll(t, a, [][2]string{
		{"response.output_item.added", `{"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather"}}`},
		{"response.function_call_arguments.delta", `{"item_id":"fc_1","delta":"{\"city\":"}`},
		{"response.function_call_arguments.delta", `{"item_id":"fc_1","delta":"\"SF\"}"}`},
		{"response.function_call_arguments.done", `{"item_id":"fc_1","arguments":"{\"city\":\"SF\"}"}`},
		{"response.completed", `{"response":{"usage":{"input_tokens":5,"output_tokens":3}}}`},
	})
	calls := a.ParsedToolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d: %#v", len(calls), calls)
	}
	if calls[0].CallID != "call_1" || calls[0].Name != "get_weather" || calls[0].Arguments != `{"city":"SF"}` {
		t.Fatalf("parsed tool call wrong: %#v", calls[0])
	}
}

func TestConsoleAdapterIgnoresBuiltinFunctionCall(t *testing.T) {
	allowed := map[string]struct{}{"get_weather": {}}
	a := NewConsoleStreamAdapterWithTools(allowed)
	feedAll(t, a, [][2]string{
		// console internal web_search call must NOT surface as a client tool call
		{"response.output_item.added", `{"item":{"type":"function_call","id":"fc_w","call_id":"c_w","name":"web_search"}}`},
		{"response.function_call_arguments.done", `{"item_id":"fc_w","arguments":"{}"}`},
		{"response.output_text.delta", `{"delta":"Result text"}`},
		{"response.completed", `{"response":{}}`},
	})
	if calls := a.ParsedToolCalls(); len(calls) != 0 {
		t.Fatalf("builtin web_search must not become a tool call: %#v", calls)
	}
	if a.FullText() != "Result text" {
		t.Fatalf("text should still flow when builtin tool used: %q", a.FullText())
	}
}

func TestConsoleAdapterNoToolsWhenNotDeclared(t *testing.T) {
	// adapter created without allowed names ignores all function calls
	a := NewConsoleStreamAdapter()
	feedAll(t, a, [][2]string{
		{"response.output_item.added", `{"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather"}}`},
		{"response.function_call_arguments.done", `{"item_id":"fc_1","arguments":"{}"}`},
	})
	if calls := a.ParsedToolCalls(); len(calls) != 0 {
		t.Fatalf("no client tools declared → no tool calls, got %#v", calls)
	}
}

func TestToConsoleToolChoiceBuiltinFallsBackToAuto(t *testing.T) {
	got := toConsoleToolChoice(map[string]any{"type": "function", "function": map[string]any{"name": "web_search"}})
	if got != "auto" {
		t.Fatalf("builtin tool_choice should fall back to auto, got %#v", got)
	}
	custom := toConsoleToolChoice(map[string]any{"type": "function", "function": map[string]any{"name": "my_fn"}})
	want := map[string]any{"type": "function", "name": "my_fn"}
	if !reflect.DeepEqual(custom, want) {
		t.Fatalf("custom tool_choice = %#v, want %#v", custom, want)
	}
}
