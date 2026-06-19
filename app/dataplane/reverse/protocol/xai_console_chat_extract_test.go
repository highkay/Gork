package protocol

import "testing"

func feedAll(t *testing.T, a *ConsoleStreamAdapter, events [][2]string) {
	t.Helper()
	for _, ev := range events {
		if _, err := a.Feed(ev[0], ev[1]); err != nil {
			t.Fatalf("Feed(%s) error: %v", ev[0], err)
		}
	}
}

func TestConsoleAdapterCollectsReasoningSummary(t *testing.T) {
	a := NewConsoleStreamAdapter()
	feedAll(t, a, [][2]string{
		{"response.reasoning_summary_text.delta", `{"delta":"Let me "}`},
		{"response.reasoning_summary_text.delta", `{"delta":"think."}`},
		{"response.output_text.delta", `{"delta":"Answer"}`},
	})
	if got := a.ThinkingText(); got != "Let me think." {
		t.Fatalf("ThinkingText = %q, want %q", got, "Let me think.")
	}
	if got := a.FullText(); got != "Answer" {
		t.Fatalf("FullText = %q, want %q", got, "Answer")
	}
}

func TestConsoleAdapterCollectsWebSearchSources(t *testing.T) {
	a := NewConsoleStreamAdapter()
	feedAll(t, a, [][2]string{
		{"response.output_item.done", `{"item":{"type":"web_search_call","action":{"sources":[{"url":"https://a.com","title":"A"},{"url":"https://b.com","title":"B"}]}}}`},
		// duplicate url should be deduped
		{"response.output_item.done", `{"item":{"type":"web_search_call","action":{"sources":[{"url":"https://a.com","title":"A"}]}}}`},
	})
	sources := a.SearchSourcesList()
	if len(sources) != 2 {
		t.Fatalf("sources len = %d, want 2 (deduped): %v", len(sources), sources)
	}
	if sources[0]["url"] != "https://a.com" || sources[1]["url"] != "https://b.com" {
		t.Fatalf("unexpected sources: %v", sources)
	}
}

func TestConsoleAdapterAnnotationBackfillsSources(t *testing.T) {
	a := NewConsoleStreamAdapter()
	feedAll(t, a, [][2]string{
		// title == url should be cleaned to empty
		{"response.output_text.annotation.added", `{"annotation":{"type":"url_citation","url":"https://x.com","title":"https://x.com","start_index":3,"end_index":9}}`},
	})
	anns := a.AnnotationsList()
	if len(anns) != 1 {
		t.Fatalf("annotations len = %d, want 1", len(anns))
	}
	if anns[0]["title"] != "" {
		t.Fatalf("title should be cleaned when equal to url, got %q", anns[0]["title"])
	}
	if anns[0]["start_index"] != 3 || anns[0]["end_index"] != 9 {
		t.Fatalf("annotation indices wrong: %v", anns[0])
	}
	if len(a.SearchSourcesList()) != 1 {
		t.Fatal("annotation url should back-fill into search_sources")
	}
}

func TestConsoleAdapterReferencesSuffix(t *testing.T) {
	a := NewConsoleStreamAdapter()
	feedAll(t, a, [][2]string{
		{"response.output_item.done", `{"item":{"type":"web_search_call","action":{"sources":[{"url":"https://a.com","title":"Site A"}]}}}`},
	})
	// gated off → empty
	if got := a.ReferencesSuffix(false); got != "" {
		t.Fatalf("ReferencesSuffix(false) = %q, want empty", got)
	}
	got := a.ReferencesSuffix(true)
	want := "\n\n## Sources\n[gork-sources]: #\n- [Site A](https://a.com)\n"
	if got != want {
		t.Fatalf("ReferencesSuffix(true) = %q, want %q", got, want)
	}
}

func TestConsoleAdapterErrorEvent(t *testing.T) {
	a := NewConsoleStreamAdapter()
	_, err := a.Feed("error", `{"message":"boom"}`)
	if err == nil {
		t.Fatal("expected error from error event")
	}
}

func TestBuildConsolePayloadPrependsCustomInstruction(t *testing.T) {
	payload := BuildConsolePayload(ConsolePayloadOptions{
		Model:             "grok-4.3-console",
		Messages:          []map[string]any{{"role": "user", "content": "hi"}},
		CustomInstruction: "  Be concise.  ",
	})
	input, ok := payload["input"].([]map[string]any)
	if !ok || len(input) != 2 {
		t.Fatalf("input items = %v, want 2 (system + user)", payload["input"])
	}
	if input[0]["role"] != "system" {
		t.Fatalf("first item role = %v, want system", input[0]["role"])
	}
	blocks, ok := input[0]["content"].([]map[string]any)
	if !ok || len(blocks) != 1 || blocks[0]["text"] != "Be concise." {
		t.Fatalf("custom instruction not injected/trimmed correctly: %v", input[0]["content"])
	}
	if input[1]["role"] != "user" {
		t.Fatalf("second item role = %v, want user", input[1]["role"])
	}
}

func TestBuildConsolePayloadNoCustomInstruction(t *testing.T) {
	payload := BuildConsolePayload(ConsolePayloadOptions{
		Model:    "grok-4.3-console",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	input, _ := payload["input"].([]map[string]any)
	if len(input) != 1 || input[0]["role"] != "user" {
		t.Fatalf("input items = %v, want only user item", payload["input"])
	}
}

// grok-4.20-0309-reasoning must NOT receive reasoning.effort (upstream rejects).
func TestBuildConsolePayloadReasoningEffortGating(t *testing.T) {
	withEffort := BuildConsolePayload(ConsolePayloadOptions{Model: "grok-4.3-high", ReasoningEffort: "high"})
	if _, ok := withEffort["reasoning"]; !ok {
		t.Fatal("grok-4.3 should receive reasoning.effort")
	}
	noEffort := BuildConsolePayload(ConsolePayloadOptions{Model: "grok-4.20-0309-reasoning-console", ReasoningEffort: "high"})
	if _, ok := noEffort["reasoning"]; ok {
		t.Fatal("grok-4.20-0309-reasoning must NOT receive reasoning.effort (upstream rejects)")
	}
	nonReasoning := BuildConsolePayload(ConsolePayloadOptions{Model: "grok-4.20-0309-non-reasoning-console", ReasoningEffort: "high"})
	if _, ok := nonReasoning["reasoning"]; ok {
		t.Fatal("grok-4.20-0309-non-reasoning must NOT receive reasoning.effort")
	}
}
