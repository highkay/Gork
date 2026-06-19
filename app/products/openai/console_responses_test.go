package openai

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	"github.com/dslzl/gork/app/platform"
)

func TestConsoleResponsesNonStreamBuildsResponseObject(t *testing.T) {
	resetChatDepsForTest(t)
	stream := false
	emitThink := false
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok1", ModeID: model.ModeConsole}}}
	refresh := &fakeChatRefreshService{}
	chatDirectoryProvider = func() chatDirectory { return dir }
	chatRefreshService = func() chatRefreshProvider { return refresh }
	chatTimeoutSeconds = func() float64 { return 66.5 }
	consoleStreamChat = func(_ context.Context, token string, payload map[string]any, timeoutS float64) ([]protocol.ConsoleStreamEvent, error) {
		reasoning, _ := payload["reasoning"].(map[string]any)
		if token != "tok1" || payload["model"] != "grok-4.3" || payload["stream"] != true || reasoning["effort"] != "none" {
			t.Fatalf("token/payload=%q/%#v", token, payload)
		}
		if timeoutS != 66.5 {
			t.Fatalf("timeout=%v want 66.5", timeoutS)
		}
		return []protocol.ConsoleStreamEvent{
			{EventType: "response.output_text.delta", Data: `{"delta":"hello"}`},
			{EventType: "response.completed", Data: `{"response":{"usage":{"input_tokens":4,"output_tokens":5}}}`},
		}, nil
	}

	result, err := ConsoleResponses(context.Background(), consoleResponseOptions{
		Model:      "grok-4.3-console",
		Messages:   []map[string]any{{"role": "user", "content": "hi"}},
		Stream:     &stream,
		EmitThink:  &emitThink,
		ResponseID: "resp_test",
		MessageID:  "msg_test",
	})
	if err != nil {
		t.Fatalf("ConsoleResponses err=%v", err)
	}
	if result.IsStream {
		t.Fatalf("expected non-stream result: %#v", result)
	}
	if result.Response["id"] != "resp_test" || result.Response["status"] != "completed" {
		t.Fatalf("response=%#v", result.Response)
	}
	output := result.Response["output"].([]map[string]any)
	item := output[0]
	content := item["content"].([]map[string]any)
	if item["id"] != "msg_test" || item["type"] != "message" || content[0]["text"] != "hello" {
		t.Fatalf("output=%#v", output)
	}
	usage := result.Response["usage"].(map[string]any)
	if usage["input_tokens"] != 4 || usage["output_tokens"] != 5 || usage["total_tokens"] != 9 {
		t.Fatalf("usage=%#v", usage)
	}
	if dir.releases != 1 || len(dir.feedbacks) != 1 || dir.feedbacks[0].Kind != feedbackKindSuccess {
		t.Fatalf("dir=%#v releases=%d", dir.feedbacks, dir.releases)
	}
	if refresh.refreshCalls != 1 || refresh.token != "tok1" || refresh.modeID != int(model.ModeConsole) {
		t.Fatalf("refresh=%#v", refresh)
	}
}

func TestConsoleResponsesStreamFramesResponsesEvents(t *testing.T) {
	resetChatDepsForTest(t)
	stream := true
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok1", ModeID: model.ModeConsole}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	consoleStreamChat = func(context.Context, string, map[string]any, float64) ([]protocol.ConsoleStreamEvent, error) {
		return []protocol.ConsoleStreamEvent{
			{EventType: "response.output_text.delta", Data: `{"delta":"he"}`},
			{EventType: "response.output_text.delta", Data: `{"delta":"llo"}`},
			{EventType: "response.completed", Data: `{"response":{"usage":{"input_tokens":2,"output_tokens":3}}}`},
		}, nil
	}

	result, err := ConsoleResponses(context.Background(), consoleResponseOptions{
		Model:      "grok-4.3-console",
		Messages:   []map[string]any{{"role": "user", "content": "hi"}},
		Stream:     &stream,
		ResponseID: "resp_stream",
		MessageID:  "msg_stream",
	})
	if err != nil {
		t.Fatalf("ConsoleResponses stream err=%v", err)
	}
	if !result.IsStream {
		t.Fatalf("expected stream result: %#v", result)
	}
	joined := strings.Join(result.StreamFrames, "")
	for _, want := range []string{
		"event: response.created",
		"event: response.in_progress",
		"event: response.output_item.added",
		"event: response.content_part.added",
		"event: response.output_text.delta",
		`"delta":"he"`,
		`"delta":"llo"`,
		"event: response.output_text.done",
		`"text":"hello"`,
		"event: response.content_part.done",
		"event: response.output_item.done",
		"event: response.completed",
		`"input_tokens":2`,
		`"output_tokens":3`,
		"data: [DONE]",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("stream frames missing %q:\n%s", want, joined)
		}
	}
}

func TestConsoleResponsesNonStreamReturnsFunctionCallItems(t *testing.T) {
	resetChatDepsForTest(t)
	stream := false
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok1", ModeID: model.ModeConsole}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	consoleStreamChat = func(_ context.Context, _ string, payload map[string]any, _ float64) ([]protocol.ConsoleStreamEvent, error) {
		tools := payload["tools"].([]map[string]any)
		found := false
		for _, tool := range tools {
			if tool["type"] == "function" && tool["name"] == "lookup_order" {
				found = true
			}
			if tool["name"] == "web_search" {
				t.Fatalf("builtin web_search must not be forwarded as a client function: %#v", tools)
			}
		}
		if !found {
			t.Fatalf("client function tool missing from payload: %#v", payload)
		}
		choice := payload["tool_choice"].(map[string]any)
		if choice["name"] != "lookup_order" {
			t.Fatalf("tool_choice=%#v", choice)
		}
		return []protocol.ConsoleStreamEvent{
			{EventType: "response.output_item.added", Data: `{"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup_order"}}`},
			{EventType: "response.function_call_arguments.done", Data: `{"item_id":"fc_1","arguments":"{\"order_id\":\"A1\"}"}`},
			{EventType: "response.completed", Data: `{"response":{"usage":{"input_tokens":8,"output_tokens":4}}}`},
		}, nil
	}

	result, err := ConsoleResponses(context.Background(), consoleResponseOptions{
		Model:    "grok-4.3-console",
		Messages: []map[string]any{{"role": "user", "content": "lookup"}},
		Stream:   &stream,
		Tools: []map[string]any{
			{"type": "function", "function": map[string]any{"name": "lookup_order", "parameters": map[string]any{"type": "object"}}},
			{"type": "function", "function": map[string]any{"name": "web_search"}},
		},
		ToolChoice: map[string]any{"type": "function", "function": map[string]any{"name": "lookup_order"}},
		ResponseID: "resp_tool",
		MessageID:  "msg_tool",
	})
	if err != nil {
		t.Fatalf("ConsoleResponses tool err=%v", err)
	}
	output := result.Response["output"].([]map[string]any)
	if len(output) != 1 || output[0]["type"] != "function_call" || output[0]["call_id"] != "call_1" || output[0]["name"] != "lookup_order" {
		t.Fatalf("function_call output=%#v", output)
	}
	if output[0]["arguments"] != `{"order_id":"A1"}` {
		t.Fatalf("function_call arguments=%#v", output[0]["arguments"])
	}
}

func TestConsoleResponsesStreamReturnsFunctionCallEvents(t *testing.T) {
	resetChatDepsForTest(t)
	stream := true
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok1", ModeID: model.ModeConsole}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	consoleStreamChat = func(context.Context, string, map[string]any, float64) ([]protocol.ConsoleStreamEvent, error) {
		return []protocol.ConsoleStreamEvent{
			{EventType: "response.output_item.added", Data: `{"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup_order"}}`},
			{EventType: "response.function_call_arguments.delta", Data: `{"item_id":"fc_1","delta":"{\"order_id\":"}`},
			{EventType: "response.function_call_arguments.delta", Data: `{"item_id":"fc_1","delta":"\"A1\"}"}`},
			{EventType: "response.completed", Data: `{"response":{"usage":{"input_tokens":8,"output_tokens":4}}}`},
		}, nil
	}

	result, err := ConsoleResponses(context.Background(), consoleResponseOptions{
		Model:    "grok-4.3-console",
		Messages: []map[string]any{{"role": "user", "content": "lookup"}},
		Stream:   &stream,
		Tools:    []map[string]any{{"type": "function", "function": map[string]any{"name": "lookup_order"}}},
	})
	if err != nil {
		t.Fatalf("ConsoleResponses stream tool err=%v", err)
	}
	joined := strings.Join(result.StreamFrames, "")
	for _, want := range []string{
		"event: response.function_call_arguments.delta",
		"event: response.function_call_arguments.done",
		`"type":"function_call"`,
		`"call_id":"call_1"`,
		`"name":"lookup_order"`,
		`"arguments":"{\"order_id\":\"A1\"}"`,
		"event: response.completed",
		"data: [DONE]",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("stream tool frames missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "response.output_text.delta") {
		t.Fatalf("tool-call response must not emit buffered text deltas:\n%s", joined)
	}
}

func TestConsoleResponsesRetriesRetryableUpstreamStatus(t *testing.T) {
	resetChatDepsForTest(t)
	stream := false
	dir := &fakeChatDirectory{accounts: []chatAccount{
		{Token: "tokA", ModeID: model.ModeConsole},
		{Token: "tokB", ModeID: model.ModeConsole},
	}}
	chatSelectionMaxRetries = func() int { return 1 }
	chatRetryConfig = func() map[string]any { return map[string]any{"retry.on_codes": "429"} }
	chatDirectoryProvider = func() chatDirectory { return dir }
	calls := []string{}
	consoleStreamChat = func(_ context.Context, token string, _ map[string]any, _ float64) ([]protocol.ConsoleStreamEvent, error) {
		calls = append(calls, token)
		if token == "tokA" {
			return nil, platform.NewUpstreamError("rate limited", 429, "")
		}
		return []protocol.ConsoleStreamEvent{
			{EventType: "response.output_text.delta", Data: `{"delta":"ok"}`},
			{EventType: "response.completed", Data: `{"response":{"usage":{"input_tokens":1,"output_tokens":1}}}`},
		}, nil
	}

	result, err := ConsoleResponses(context.Background(), consoleResponseOptions{
		Model:      "grok-4.3-console",
		Messages:   []map[string]any{{"role": "user", "content": "hi"}},
		Stream:     &stream,
		ResponseID: "resp_retry",
		MessageID:  "msg_retry",
	})
	if err != nil {
		t.Fatalf("ConsoleResponses retry err=%v", err)
	}
	if result.Response["status"] != "completed" {
		t.Fatalf("response=%#v", result.Response)
	}
	if !reflect.DeepEqual(calls, []string{"tokA", "tokB"}) {
		t.Fatalf("calls=%#v", calls)
	}
	if !reflect.DeepEqual(dir.excludes, [][]string{{}, {"tokA"}}) {
		t.Fatalf("excludes=%#v", dir.excludes)
	}
	if len(dir.feedbacks) != 2 || dir.feedbacks[0].Kind != feedbackKindRateLimited || dir.feedbacks[1].Kind != feedbackKindSuccess {
		t.Fatalf("feedbacks=%#v", dir.feedbacks)
	}
	if dir.releases != 2 {
		t.Fatalf("releases=%d", dir.releases)
	}
}
