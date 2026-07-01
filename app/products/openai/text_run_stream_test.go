package openai

import (
	"context"
	"testing"
)

func TestConsumeTextRunLinesCollectsTextAndThinking(t *testing.T) {
	state, events, err := consumeTextRunLines([]string{
		`data: {"result":{"response":{"token":"think ","isThinking":true}}}`,
		`data: {"result":{"response":{"token":"hello","isThinking":false,"messageTag":"final"}}}`,
		`data: [DONE]`,
	}, textRunOptions{EmitThinking: true})
	if err != nil {
		t.Fatalf("consumeTextRunLines err=%v", err)
	}
	if state.Text != "hello" || state.Thinking != "think \n" {
		t.Fatalf("state=%#v", state)
	}
	if len(events) != 2 || events[0].Kind != "thinking" || events[1].Kind != "text" {
		t.Fatalf("events=%#v", events)
	}
}

func TestConsumeTextRunLinesSuppressesDisabledTools(t *testing.T) {
	state, events, err := consumeTextRunLines([]string{
		`data: {"result":{"response":{"token":"before <tool_calls><tool_call><tool_name>search</tool_name><parameters>{\"q\":\"go\"}</parameters></tool_call></tool_calls> after","isThinking":false,"messageTag":"final"}}}`,
		`data: [DONE]`,
	}, textRunOptions{DisableTools: true, EnableToolSieve: true})
	if err != nil {
		t.Fatalf("consumeTextRunLines err=%v", err)
	}
	if state.Text != "before " || !state.ToolSyntaxSuppressed {
		t.Fatalf("state=%#v", state)
	}
	for _, event := range events {
		if event.Kind == "tool_calls" {
			t.Fatalf("disabled tools emitted tool event: %#v", events)
		}
	}
}

func TestConsumeTextRunLinesEmitsToolCalls(t *testing.T) {
	state, events, err := consumeTextRunLines([]string{
		`data: {"result":{"response":{"token":"prefix <tool_calls><tool_call><tool_name>search</tool_name><parameters>{\"q\":\"go\"}</parameters></tool_call></tool_calls>","isThinking":false,"messageTag":"final"}}}`,
		`data: [DONE]`,
	}, textRunOptions{ToolNames: []string{"search"}, EnableToolSieve: true})
	if err != nil {
		t.Fatalf("consumeTextRunLines err=%v", err)
	}
	if len(state.ToolCalls) != 1 || state.ToolCalls[0].Name != "search" {
		t.Fatalf("tool calls=%#v", state.ToolCalls)
	}
	if len(events) != 2 || events[0].Kind != "text" || events[1].Kind != "tool_calls" {
		t.Fatalf("events=%#v", events)
	}
}

func TestConsumeTextRunLinesResolvesImages(t *testing.T) {
	var gotURL, gotID, gotToken string
	state, _, err := consumeTextRunLines([]string{
		`data: {"result":{"response":{"cardAttachment":{"jsonData":"{\"id\":\"img-card\",\"image_chunk\":{\"progress\":100,\"imageUuid\":\"uuid1\",\"imageUrl\":\"generated/foo.png\",\"moderated\":false}}"}}}}`,
		`data: [DONE]`,
	}, textRunOptions{
		Context: context.Background(),
		Token:   "tok",
		ResolveImage: func(_ context.Context, token string, rawURL string, imageID string) (string, error) {
			gotToken, gotURL, gotID = token, rawURL, imageID
			return "resolved-image", nil
		},
	})
	if err != nil {
		t.Fatalf("consumeTextRunLines err=%v", err)
	}
	if gotToken != "tok" || gotURL != "https://assets.grok.com/generated/foo.png" || gotID != "uuid1" {
		t.Fatalf("resolver got token=%q url=%q id=%q", gotToken, gotURL, gotID)
	}
	if len(state.ImageTexts) != 1 || state.ImageTexts[0] != "resolved-image" {
		t.Fatalf("image texts=%#v", state.ImageTexts)
	}
}
