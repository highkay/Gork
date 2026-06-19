package protocol

import "testing"

func TestIsConsoleBuiltinTool(t *testing.T) {
	builtins := []string{
		"web_search", "x_search", "browse_page", "code_execution", "code_interpreter",
		"file_search", "web_search_with_snippets", "open_page", "open_page_with_find",
		"view_image", "x_user_search", "x_keyword_search", "x_semantic_search",
		"x_thread_fetch", "view_x_video", "chatroom_send", "collections_search",
		"search_images", "image_search",
	}
	for _, name := range builtins {
		if !IsConsoleBuiltinTool(name) {
			t.Errorf("%q should be a console builtin", name)
		}
	}
	for _, name := range []string{"get_weather", "my_custom_fn", ""} {
		if IsConsoleBuiltinTool(name) {
			t.Errorf("%q should NOT be a console builtin", name)
		}
	}
}

func TestFilterClientConsoleTools(t *testing.T) {
	tools := []map[string]any{
		{"type": "function", "function": map[string]any{"name": "get_weather"}}, // keep
		{"type": "function", "function": map[string]any{"name": "web_search"}},  // drop (builtin)
		{"type": "function", "name": "browse_page"},                             // drop (flattened builtin)
		{"type": "function", "name": "lookup_order"},                            // keep (flattened custom)
	}
	out := FilterClientConsoleTools(tools)
	if len(out) != 2 {
		t.Fatalf("filtered tools = %d, want 2: %v", len(out), out)
	}
	if consoleToolName(out[0]) != "get_weather" || consoleToolName(out[1]) != "lookup_order" {
		t.Fatalf("unexpected surviving tools: %v", out)
	}
}

func TestFilterClientConsoleToolsEmpty(t *testing.T) {
	if FilterClientConsoleTools(nil) != nil {
		t.Fatal("nil input should return nil")
	}
	allBuiltin := []map[string]any{
		{"type": "function", "function": map[string]any{"name": "x_search"}},
	}
	if FilterClientConsoleTools(allBuiltin) != nil {
		t.Fatal("all-builtin input should return nil")
	}
}

func TestClientConsoleToolNames(t *testing.T) {
	tools := []map[string]any{
		{"function": map[string]any{"name": "get_weather"}},
		{"function": map[string]any{"name": "web_search"}}, // builtin, excluded
		{"name": "lookup_order"},
	}
	names := ClientConsoleToolNames(tools)
	if len(names) != 2 {
		t.Fatalf("names = %v, want 2 entries", names)
	}
	if _, ok := names["get_weather"]; !ok {
		t.Error("get_weather should be present")
	}
	if _, ok := names["web_search"]; ok {
		t.Error("web_search (builtin) should be excluded")
	}
}
