package openai

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/platform"
)

func TestResponsesParseInputMatchesPythonShapes(t *testing.T) {
	messages := parseResponseInput([]any{
		map[string]any{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": `{"q":"x"}`},
		map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "done"},
		map[string]any{"type": "message", "role": "user", "content": []any{
			map[string]any{"type": "input_text", "text": "hello"},
			map[string]any{"type": "input_image", "image_url": map[string]any{"url": "https://img/1.png"}},
		}},
		map[string]any{"type": "reasoning", "summary": []any{}},
	})

	want := []map[string]any{
		{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": `{"q":"x"}`}}}},
		{"role": "tool", "tool_call_id": "call_1", "content": "done"},
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "hello"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://img/1.png"}},
		}},
	}
	if !reflect.DeepEqual(messages, want) {
		t.Fatalf("messages mismatch\nwant: %#v\n got: %#v", want, messages)
	}
}

func TestResponsesToolHelpersMatchResponsesAPIShape(t *testing.T) {
	tools := toResponseChatTools([]map[string]any{
		{"type": "function", "name": "lookup", "description": "Lookup", "parameters": map[string]any{"type": "object"}},
		{"type": "function", "function": map[string]any{"name": "wrapped"}},
	})
	if tools[0]["function"].(map[string]any)["name"] != "lookup" {
		t.Fatalf("flat tool was not wrapped: %#v", tools[0])
	}
	if tools[1]["function"].(map[string]any)["name"] != "wrapped" {
		t.Fatalf("wrapped tool changed: %#v", tools[1])
	}
}

func TestResponsesTokenEstimateCrossModelLanguageGolden(t *testing.T) {
	for _, tt := range []struct {
		name  string
		model string
		state chatCompletionState
		want  int
	}{
		{name: "english fast", model: "grok-4.20-fast", state: chatCompletionState{Text: "hello world"}, want: 2},
		{name: "chinese auto", model: "grok-4.20-auto", state: chatCompletionState{Text: "你好，世界"}, want: 3},
		{name: "mixed console", model: "grok-4.3-console", state: chatCompletionState{Text: "hello 世界"}, want: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output := []map[string]any{responseMessageItem("msg", tt.state)}
			if got := estimateResponseOutputTokens(output, tt.state); got != tt.want {
				t.Fatalf("%s output tokens=%d want %d", tt.model, got, tt.want)
			}
		})
	}

	toolOutput := []map[string]any{{
		"type":      "function_call",
		"name":      "lookup",
		"arguments": `{"q":"你好"}`,
	}}
	wantTool := platform.EstimateTokens("lookup") + platform.EstimateTokens(`{"q":"你好"}`)
	if got := estimateResponseOutputTokens(toolOutput, chatCompletionState{}); got != wantTool {
		t.Fatalf("tool output tokens=%d want %d", got, wantTool)
	}
}

func TestResponsesNonStreamBuildsTextReasoningAndFeedback(t *testing.T) {
	resetChatDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-resp", ModeID: model.ModeFast}}}
	refresh := &fakeChatRefreshService{}
	chatDirectoryProvider = func() chatDirectory { return dir }
	chatRefreshService = func() chatRefreshProvider { return refresh }
	streamPost = func(_ context.Context, req chatStreamRequest) (*chatStreamResponse, error) {
		if req.Token != "tok-resp" {
			t.Fatalf("token=%q", req.Token)
		}
		var payload map[string]any
		if err := json.Unmarshal(req.PayloadBytes, &payload); err != nil {
			t.Fatalf("payload json: %v", err)
		}
		if !strings.Contains(payload["message"].(string), "[system]: sys") || !strings.Contains(payload["message"].(string), "[user]: hello") {
			t.Fatalf("payload message=%q", payload["message"])
		}
		return &chatStreamResponse{StatusCode: 200, Lines: []string{
			`data: {"result":{"response":{"token":"think ","isThinking":true}}}`,
			`data: {"result":{"response":{"token":"hello","isThinking":false,"messageTag":"final"}}}`,
			`data: [DONE]`,
		}}, nil
	}

	result, err := Responses(context.Background(), responseOptions{
		Model:        "grok-4.20-fast",
		Input:        "hello",
		Instructions: "sys",
		EmitThink:    true,
	})
	if err != nil {
		t.Fatalf("Responses err=%v", err)
	}
	output := result.Response["output"].([]map[string]any)
	if output[0]["type"] != "reasoning" || output[1]["type"] != "message" {
		t.Fatalf("output=%#v", output)
	}
	content := output[1]["content"].([]map[string]any)[0]
	if content["text"] != "hello" {
		t.Fatalf("content=%#v", content)
	}
	if dir.releases != 1 || len(dir.feedbacks) != 1 || dir.feedbacks[0].Kind != feedbackKindSuccess {
		t.Fatalf("dir releases=%d feedbacks=%#v", dir.releases, dir.feedbacks)
	}
	if refresh.refreshCalls != 1 || refresh.token != "tok-resp" {
		t.Fatalf("refresh=%#v", refresh)
	}
}

func TestResponsesNonStreamBuildsFunctionCallOutputWhenParsed(t *testing.T) {
	resetChatDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-resp", ModeID: model.ModeFast}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	streamPost = func(_ context.Context, req chatStreamRequest) (*chatStreamResponse, error) {
		var payload map[string]any
		if err := json.Unmarshal(req.PayloadBytes, &payload); err != nil {
			t.Fatalf("payload json: %v", err)
		}
		if !strings.Contains(payload["message"].(string), "lookup") {
			t.Fatalf("payload message missing tool prompt=%q", payload["message"])
		}
		return &chatStreamResponse{StatusCode: 200, Lines: []string{
			`data: {"result":{"response":{"token":"<tool_calls><tool_call><tool_name>lookup</tool_name><parameters>{\"q\":\"x\"}</parameters></tool_call></tool_calls>","isThinking":false,"messageTag":"final"}}}`,
			`data: [DONE]`,
		}}, nil
	}

	result, err := Responses(context.Background(), responseOptions{
		Model: "grok-4.20-fast",
		Input: "hello",
		Tools: []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        "lookup",
				"description": "search",
				"parameters":  map[string]any{"type": "object"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Responses err=%v", err)
	}
	output := result.Response["output"].([]map[string]any)
	if len(output) != 1 {
		t.Fatalf("output=%#v", output)
	}
	call := output[0]
	if call["type"] != "function_call" || call["name"] != "lookup" || call["arguments"] != `{"q":"x"}` {
		t.Fatalf("function_call=%#v", call)
	}
	body, err := json.Marshal(result.Response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if strings.Contains(string(body), "<tool_calls>") || strings.Contains(string(body), "<tool_name>") {
		t.Fatalf("raw XML leaked in response=%s", body)
	}
}

func TestResponsesToolChoiceNoneSuppressesFunctionCallOutput(t *testing.T) {
	resetChatDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-resp", ModeID: model.ModeFast}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	streamPost = func(_ context.Context, req chatStreamRequest) (*chatStreamResponse, error) {
		var payload map[string]any
		if err := json.Unmarshal(req.PayloadBytes, &payload); err != nil {
			t.Fatalf("payload json: %v", err)
		}
		if !strings.Contains(payload["message"].(string), "Do NOT call any tools") {
			t.Fatalf("payload message missing none instruction=%q", payload["message"])
		}
		return &chatStreamResponse{StatusCode: 200, Lines: []string{
			`data: {"result":{"response":{"token":"safe <tool_calls><tool_call><tool_name>lookup</tool_name><parameters>{\"q\":\"x\"}</parameters></tool_call></tool_calls> ignored","isThinking":false,"messageTag":"final"}}}`,
			`data: [DONE]`,
		}}, nil
	}

	result, err := Responses(context.Background(), responseOptions{
		Model:      "grok-4.20-fast",
		Input:      "hello",
		ToolChoice: "none",
		Tools: []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        "lookup",
				"description": "search",
				"parameters":  map[string]any{"type": "object"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Responses err=%v", err)
	}
	body, err := json.Marshal(result.Response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if strings.Contains(string(body), "<tool_calls>") || strings.Contains(string(body), `"function_call"`) {
		t.Fatalf("tool output leaked in response=%s", body)
	}
	output := result.Response["output"].([]map[string]any)
	if len(output) != 1 || output[0]["type"] != "message" {
		t.Fatalf("output=%#v", output)
	}
	content := output[0]["content"].([]map[string]any)
	if content[0]["text"] != "safe " {
		t.Fatalf("content=%#v", content)
	}
}

func TestResponsesStreamEmitsResponsesEvents(t *testing.T) {
	resetChatDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-resp", ModeID: model.ModeFast}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	streamPost = func(context.Context, chatStreamRequest) (*chatStreamResponse, error) {
		return &chatStreamResponse{StatusCode: 200, Lines: []string{
			`data: {"result":{"response":{"token":"hello","isThinking":false,"messageTag":"final"}}}`,
			`data: [DONE]`,
		}}, nil
	}

	result, err := Responses(context.Background(), responseOptions{
		Model:  "grok-4.20-fast",
		Input:  "hello",
		Stream: true,
	})
	if err != nil {
		t.Fatalf("Responses stream err=%v", err)
	}
	if !result.IsStream {
		t.Fatalf("expected stream result: %#v", result)
	}
	joined := strings.Join(result.StreamFrames, "")
	for _, needle := range []string{
		"event: response.created",
		"event: response.output_item.added",
		"event: response.output_text.delta",
		`"delta":"hello"`,
		"event: response.completed",
		"data: [DONE]",
	} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("missing %q in frames=%s", needle, joined)
		}
	}
}

func TestResponsesStreamEventOrderGoldenCoversReasoningTextToolAndDone(t *testing.T) {
	resetChatDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-resp", ModeID: model.ModeFast}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	streamPost = func(context.Context, chatStreamRequest) (*chatStreamResponse, error) {
		return &chatStreamResponse{StatusCode: 200, Lines: []string{
			`data: {"result":{"response":{"token":"think ","isThinking":true}}}`,
			`data: {"result":{"response":{"token":"hello ","isThinking":false,"messageTag":"final"}}}`,
			`data: {"result":{"response":{"token":"<tool_calls><tool_call><tool_name>lookup</tool_name><parameters>{\"q\":\"x\"}</parameters></tool_call></tool_calls>","isThinking":false,"messageTag":"final"}}}`,
			`data: [DONE]`,
		}}, nil
	}

	result, err := Responses(context.Background(), responseOptions{
		Model:     "grok-4.20-fast",
		Input:     "look up x",
		Stream:    true,
		EmitThink: true,
		Tools: []map[string]any{{
			"type":        "function",
			"name":        "lookup",
			"description": "Lookup",
			"parameters":  map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("Responses stream golden err=%v", err)
	}

	got := responseStreamEventNames(strings.Join(result.StreamFrames, ""))
	want := []string{
		"response.created",
		"response.output_item.added",
		"response.reasoning_summary_part.added",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done",
		"response.reasoning_summary_part.done",
		"response.output_item.done",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
		"response.output_item.added",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.output_item.done",
		"response.completed",
		"[DONE]",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event order = %#v, want %#v\nframes=%s", got, want, strings.Join(result.StreamFrames, ""))
	}
}

func TestResponsesStreamEmitsFunctionCallEventsAndCompletedOutput(t *testing.T) {
	resetChatDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-resp", ModeID: model.ModeFast}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	streamPost = func(context.Context, chatStreamRequest) (*chatStreamResponse, error) {
		return &chatStreamResponse{StatusCode: 200, Lines: []string{
			`data: {"result":{"response":{"token":"<tool_calls><tool_call><tool_name>lookup</tool_name><parameters>{\"q\":\"x\"}</parameters></tool_call></tool_calls>","isThinking":false,"messageTag":"final"}}}`,
			`data: [DONE]`,
		}}, nil
	}

	result, err := Responses(context.Background(), responseOptions{
		Model:  "grok-4.20-fast",
		Input:  "look up x",
		Stream: true,
		Tools: []map[string]any{{
			"type":        "function",
			"name":        "lookup",
			"description": "Lookup",
			"parameters":  map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("Responses stream tool err=%v", err)
	}
	joined := strings.Join(result.StreamFrames, "")
	for _, needle := range []string{
		"event: response.function_call_arguments.delta",
		`"type":"function_call"`,
		`"name":"lookup"`,
		"event: response.completed",
	} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("missing %q in frames=%s", needle, joined)
		}
	}
	completedIndex := strings.LastIndex(joined, "event: response.completed")
	if completedIndex < 0 || !strings.Contains(joined[completedIndex:], `"type":"function_call"`) {
		t.Fatalf("completed response missing function_call: %s", joined)
	}
	if strings.Contains(joined[completedIndex:], `"output_tokens":0`) {
		t.Fatalf("completed response should estimate function_call output tokens: %s", joined[completedIndex:])
	}
}

func responseStreamEventNames(stream string) []string {
	names := []string{}
	for _, line := range strings.Split(stream, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "event: ") {
			names = append(names, strings.TrimPrefix(line, "event: "))
		}
		if line == "data: [DONE]" {
			names = append(names, "[DONE]")
		}
	}
	return names
}

func TestResponsesStreamToolSieveHandlesSplitXML(t *testing.T) {
	resetChatDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-resp", ModeID: model.ModeFast}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	streamPost = func(context.Context, chatStreamRequest) (*chatStreamResponse, error) {
		return &chatStreamResponse{StatusCode: 200, Lines: []string{
			`data: {"result":{"response":{"token":"<tool_calls><tool_call><tool_name>look","isThinking":false,"messageTag":"final"}}}`,
			`data: {"result":{"response":{"token":"up</tool_name><parameters>{\"q\":\"x\"}</parameters></tool_call></tool_calls>","isThinking":false,"messageTag":"final"}}}`,
			`data: [DONE]`,
		}}, nil
	}

	result, err := Responses(context.Background(), responseOptions{
		Model:  "grok-4.20-fast",
		Input:  "look up x",
		Stream: true,
		Tools: []map[string]any{{
			"type":        "function",
			"name":        "lookup",
			"description": "Lookup",
			"parameters":  map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("Responses stream tool split err=%v", err)
	}
	joined := strings.Join(result.StreamFrames, "")
	if strings.Contains(joined, "<tool_calls>") || strings.Contains(joined, "<tool_name>") {
		t.Fatalf("tool XML leaked into stream: %s", joined)
	}
	completedIndex := strings.LastIndex(joined, "event: response.completed")
	if completedIndex < 0 || !strings.Contains(joined[completedIndex:], `"type":"function_call"`) {
		t.Fatalf("completed response missing function_call: %s", joined)
	}
}

func TestResponsesStreamEmitsAnnotationAdded(t *testing.T) {
	resetChatDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-resp", ModeID: model.ModeFast}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	streamPost = func(context.Context, chatStreamRequest) (*chatStreamResponse, error) {
		return &chatStreamResponse{StatusCode: 200, Lines: []string{
			`data: {"result":{"response":{"cardAttachment":{"jsonData":"{\"id\":\"cite1\",\"url\":\"https://example.com/a\"}"},"webSearchResults":{"results":[{"url":"https://example.com/a","title":"Example A"}]}}}}`,
			`data: {"result":{"response":{"token":"See<grok:render card_id=\"cite1\" card_type=\"citation\" type=\"render_inline_citation\"></grok:render> now.","isThinking":false,"messageTag":"final"}}}`,
			`data: [DONE]`,
		}}, nil
	}

	result, err := Responses(context.Background(), responseOptions{
		Model:  "grok-4.20-fast",
		Input:  "cite it",
		Stream: true,
	})
	if err != nil {
		t.Fatalf("Responses stream annotation err=%v", err)
	}
	joined := strings.Join(result.StreamFrames, "")
	if !strings.Contains(joined, "event: response.output_text.annotation.added") || !strings.Contains(joined, `"url":"https://example.com/a"`) {
		t.Fatalf("annotation frame missing: %s", joined)
	}
}

func TestResponsesStreamEmitsImageDeltaBeforeCompleted(t *testing.T) {
	resetChatDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-resp", ModeID: model.ModeFast}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	streamPost = func(context.Context, chatStreamRequest) (*chatStreamResponse, error) {
		return &chatStreamResponse{StatusCode: 200, Lines: []string{
			`data: {"result":{"response":{"token":"hello","isThinking":false,"messageTag":"final"}}}`,
			`data: {"result":{"response":{"cardAttachment":{"jsonData":"{\"id\":\"img-card\",\"image_chunk\":{\"progress\":100,\"imageUuid\":\"uuid1\",\"imageUrl\":\"generated/foo.png\",\"moderated\":false}}"}}}}`,
			`data: [DONE]`,
		}}, nil
	}

	result, err := Responses(context.Background(), responseOptions{
		Model:  "grok-4.20-fast",
		Input:  "draw",
		Stream: true,
	})
	if err != nil {
		t.Fatalf("Responses stream image err=%v", err)
	}
	joined := strings.Join(result.StreamFrames, "")
	completedIndex := strings.LastIndex(joined, "event: response.completed")
	imageDeltaIndex := strings.Index(joined, `"delta":"https://assets.grok.com/generated/foo.png`)
	if imageDeltaIndex < 0 || completedIndex < 0 || imageDeltaIndex > completedIndex {
		t.Fatalf("image delta missing before completed: %s", joined)
	}
}

func TestResponsesRetriesRetryableUpstreamStatusAndExcludesToken(t *testing.T) {
	resetChatDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{
		{Token: "tok-first", ModeID: model.ModeFast},
		{Token: "tok-second", ModeID: model.ModeFast},
	}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	chatSelectionMaxRetries = func() int { return 1 }
	chatRetryConfig = func() map[string]any { return map[string]any{"retry.retry_status_codes": "503"} }

	calls := 0
	streamPost = func(_ context.Context, req chatStreamRequest) (*chatStreamResponse, error) {
		calls++
		switch calls {
		case 1:
			if req.Token != "tok-first" {
				t.Fatalf("first token=%q", req.Token)
			}
			return &chatStreamResponse{StatusCode: 503, Body: "upstream busy"}, nil
		case 2:
			if req.Token != "tok-second" {
				t.Fatalf("second token=%q", req.Token)
			}
			return &chatStreamResponse{StatusCode: 200, Lines: []string{
				`data: {"result":{"response":{"token":"ok","isThinking":false,"messageTag":"final"}}}`,
				`data: [DONE]`,
			}}, nil
		default:
			t.Fatalf("unexpected stream call %d", calls)
			return nil, nil
		}
	}

	result, err := Responses(context.Background(), responseOptions{
		Model: "grok-4.20-fast",
		Input: "hello",
	})
	if err != nil {
		t.Fatalf("Responses retry err=%v", err)
	}
	if result.Response["status"] != "completed" {
		t.Fatalf("response=%#v", result.Response)
	}
	if calls != 2 {
		t.Fatalf("stream calls=%d", calls)
	}
	if !reflect.DeepEqual(dir.excludes, [][]string{{}, {"tok-first"}}) {
		t.Fatalf("excludes=%#v", dir.excludes)
	}
	if dir.releases != 2 || len(dir.feedbacks) != 2 {
		t.Fatalf("releases=%d feedbacks=%#v", dir.releases, dir.feedbacks)
	}
	if dir.feedbacks[0].Kind != feedbackKindServerError || dir.feedbacks[1].Kind != feedbackKindSuccess {
		t.Fatalf("feedbacks=%#v", dir.feedbacks)
	}
}

func TestResponsesUsesConfiguredTimeout(t *testing.T) {
	resetChatDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-resp", ModeID: model.ModeFast}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	chatTimeoutSeconds = func() float64 { return 42.5 }
	var timeout float64
	streamPost = func(_ context.Context, req chatStreamRequest) (*chatStreamResponse, error) {
		timeout = req.TimeoutSeconds
		return &chatStreamResponse{StatusCode: 200, Lines: []string{
			`data: {"result":{"response":{"token":"ok","isThinking":false,"messageTag":"final"}}}`,
			`data: [DONE]`,
		}}, nil
	}

	_, err := Responses(context.Background(), responseOptions{
		Model: "grok-4.20-fast",
		Input: "hello",
	})
	if err != nil {
		t.Fatalf("Responses timeout err=%v", err)
	}
	if timeout != 42.5 {
		t.Fatalf("timeout=%v", timeout)
	}
}
