package build

import (
	"encoding/json"
	"net/http"
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

func TestExtractPromptCacheSeedFromClaudeCodeHeader(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Claude-Code-Session-Id", "sess-abc")
	headers.Set("X-Claude-Code-Agent-Id", "worker-1")
	got := ExtractPromptCacheSeed(headers, nil)
	if got != "claude:sess-abc:agent:worker-1" {
		t.Fatalf("seed=%q", got)
	}
}

func TestExtractPromptCacheSeedFromCodexWindow(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Codex-Window-Id", "win-9")
	got := ExtractPromptCacheSeed(headers, nil)
	if got != "codex:window:win-9" {
		t.Fatalf("seed=%q", got)
	}
}

func TestExtractPromptCacheSeedFromCodexTurnMetadata(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Codex-Turn-Metadata", `{"prompt_cache_key":"codex-key-1","window_id":"w"}`)
	got := ExtractPromptCacheSeed(headers, nil)
	if got != "codex-key-1" {
		t.Fatalf("seed=%q", got)
	}
}

func TestExtractPromptCacheSeedSkipsClaudeTitleRequest(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Claude-Code-Session-Id", "sess-title")
	body := []byte(`{"system":"Generate a concise title for this coding session"}`)
	if got := ExtractPromptCacheSeed(headers, body); got != "" {
		t.Fatalf("title request seed=%q, want empty", got)
	}
}

func TestExtractGrokTurnIndex(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-grok-turn-idx", "42")
	if got := ExtractGrokTurnIndex(headers); got != "42" {
		t.Fatalf("turn=%q", got)
	}
}
