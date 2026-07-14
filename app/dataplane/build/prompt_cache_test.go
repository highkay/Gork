package build

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResolvePromptCacheKeyStable(t *testing.T) {
	a := ResolvePromptCacheKey("sess-1", "", "grok-4")
	b := ResolvePromptCacheKey("sess-1", "other", "grok-4")
	if a == "" || a != b {
		t.Fatalf("explicit key should win and be stable: %q vs %q", a, b)
	}
	c := ResolvePromptCacheKey("", "seed-x", "grok-4")
	if c == "" || c == a {
		t.Fatalf("session seed should produce different key: %q", c)
	}
	if ResolvePromptCacheKey("", "", "grok-4") != "" {
		t.Fatal("empty seed must return empty")
	}
}

func TestBuildResponsesBodyInjectsPromptCacheKey(t *testing.T) {
	body, err := BuildResponsesBodyOpts(ResponsesBodyOptions{
		Model:          "grok-4",
		Messages:       []ChatMessage{{Role: "user", Content: "hi"}},
		PromptCacheKey: "aabb-ccdd-eeff-0011-223344556677",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["prompt_cache_key"] != "aabb-ccdd-eeff-0011-223344556677" {
		t.Fatalf("prompt_cache_key=%v", payload["prompt_cache_key"])
	}
}

func TestPromptCacheKeyFromOverrides(t *testing.T) {
	if got := PromptCacheKeyFromOverrides(map[string]any{"prompt_cache_key": " k "}); got != "k" {
		t.Fatalf("got %q", got)
	}
	if got := PromptCacheKeyFromOverrides(map[string]any{"promptCacheKey": "x"}); got != "x" {
		t.Fatalf("got %q", got)
	}
}

func TestChatStreamIgnoresEventsAfterError(t *testing.T) {
	sse := strings.Join([]string{
		`event: response.created`,
		`data: {"type":"response.created"}`,
		``,
		`event: response.failed`,
		`data: {"type":"response.failed","response":{"error":{"message":"upstream failed"}}}`,
		``,
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"late delta"}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed"}`,
		``,
	}, "\n")
	frames, err := ChatStreamFramesFromResponsesSSE("m", "id", strings.NewReader(sse))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(frames, "")
	if strings.Contains(joined, "late delta") {
		t.Fatalf("late delta leaked: %s", joined)
	}
	if strings.Contains(joined, `"finish_reason":"stop"`) {
		t.Fatalf("error stream must not stop successfully: %s", joined)
	}
	if !strings.Contains(joined, "upstream failed") {
		t.Fatalf("missing error message: %s", joined)
	}
	if strings.Count(joined, "data: [DONE]") != 1 {
		t.Fatalf("expected one DONE: %s", joined)
	}
}
