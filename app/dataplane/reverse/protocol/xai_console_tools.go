package protocol

// ConsoleBuiltinTools is the set of server-side / internal tool names that the
// console.x.ai upstream handles itself. These must never be surfaced to clients
// as function calls, and client-supplied tools matching these names must never
// be forwarded upstream.
//
// The set is the union of cloudriver8's CONSOLE_BUILTIN_TOOLS (conservative, 10
// names) and jiujiu532's _CONSOLE_INTERNAL_TOOL_NAMES (broader, includes
// aliases and observed variants), so it captures the widest set of internal
// tool names seen across both upstreams.
var ConsoleBuiltinTools = map[string]struct{}{
	// Public tool types / aliases.
	"web_search":         {},
	"x_search":           {},
	"code_interpreter":   {}, // cloudriver8
	"code_execution":     {},
	"file_search":        {}, // jiujiu532
	"chatroom_send":      {},
	"collections_search": {}, // jiujiu532
	// Web-search server-side function names.
	"web_search_with_snippets": {}, // jiujiu532
	"browse_page":              {},
	"open_page":                {}, // jiujiu532
	"open_page_with_find":      {}, // jiujiu532
	// Image-search server-side function names / observed aliases.
	"search_images": {},
	"image_search":  {},
	"view_image":    {}, // jiujiu532
	// X-search server-side function names.
	"x_user_search":     {}, // jiujiu532
	"x_keyword_search":  {},
	"x_semantic_search": {},
	"x_thread_fetch":    {}, // jiujiu532
	"view_x_video":      {}, // jiujiu532
}

// IsConsoleBuiltinTool reports whether name is a console-managed internal tool
// that must not be exposed to or accepted from clients.
func IsConsoleBuiltinTool(name string) bool {
	_, ok := ConsoleBuiltinTools[name]
	return ok
}

// FilterClientConsoleTools returns only the client-supplied function tools that
// are NOT console built-ins, i.e. genuine user-defined function tools safe to
// forward upstream. Each tool is expected in OpenAI shape:
//
//	{"type":"function","function":{"name":"..."}}
//
// or the flattened console shape {"type":"function","name":"..."}.
func FilterClientConsoleTools(tools []map[string]any) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if IsConsoleBuiltinTool(consoleToolName(tool)) {
			continue
		}
		out = append(out, tool)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ClientConsoleToolNames returns the set of non-builtin function tool names from
// a client tools list, used to decide which streamed tool calls are genuine
// client function calls (vs. console internal tool usage to be hidden).
func ClientConsoleToolNames(tools []map[string]any) map[string]struct{} {
	names := map[string]struct{}{}
	for _, tool := range tools {
		name := consoleToolName(tool)
		if name == "" || IsConsoleBuiltinTool(name) {
			continue
		}
		names[name] = struct{}{}
	}
	return names
}

func consoleToolName(tool map[string]any) string {
	if fn, ok := tool["function"].(map[string]any); ok {
		if name := stringFromAny(fn["name"]); name != "" {
			return name
		}
	}
	return stringFromAny(tool["name"])
}
