package protocol

import "strings"

func ClientFunctionToolNames(tools []map[string]any) []string {
	names := []string{}
	for _, tool := range tools {
		if stringFromAny(tool["type"]) != "function" {
			continue
		}
		src := tool
		if fn, ok := tool["function"].(map[string]any); ok {
			src = fn
		}
		name := strings.TrimSpace(stringFromAny(src["name"]))
		if name != "" && !isConsoleInternalToolName(name) {
			names = append(names, name)
		}
	}
	return names
}

var consoleInternalToolNames = map[string]struct{}{
	"web_search":                  {},
	"web_search_with_snippets":    {},
	"browse_page":                 {},
	"open_page":                   {},
	"open_page_with_find":         {},
	"x_search":                    {},
	"x_keyword_search":            {},
	"x_semantic_search":           {},
	"x_user_search":               {},
	"x_thread_fetch":              {},
	"image_search":                {},
	"search_images":               {},
	"view_image":                  {},
	"view_x_video":                {},
	"code_execution":              {},
	"file_search":                 {},
	"chatroom_send":               {},
	"generate_image":              {},
	"create_image":                {},
	"edit_image":                  {},
	"computer_use_preview":        {},
	"server_side_tool_web_search": {},
}

func isConsoleInternalToolName(name string) bool {
	_, ok := consoleInternalToolNames[strings.TrimSpace(name)]
	return ok
}

func consoleToolPayloads(tools []map[string]any, allowFunctionTools bool) []map[string]any {
	payloads := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if stringFromAny(tool["type"]) != "function" {
			payloads = append(payloads, cloneAnyMap(tool))
			continue
		}
		if !allowFunctionTools {
			continue
		}
		src := tool
		if fn, ok := tool["function"].(map[string]any); ok {
			src = fn
		}
		name := strings.TrimSpace(stringFromAny(src["name"]))
		if name == "" {
			continue
		}
		payload := map[string]any{"type": "function", "name": name}
		if description := stringFromAny(src["description"]); description != "" {
			payload["description"] = description
		}
		if parameters, ok := src["parameters"]; ok {
			payload["parameters"] = parameters
		}
		payloads = append(payloads, payload)
	}
	return payloads
}

func mergeConsoleTools(defaults []map[string]any, userTools []map[string]any) []map[string]any {
	merged := make([]map[string]any, 0, len(defaults)+len(userTools))
	overrides := map[string]struct{}{}
	for _, tool := range userTools {
		if key := consoleToolKey(tool); key != "" {
			overrides[key] = struct{}{}
		}
	}
	for _, tool := range defaults {
		if _, ok := overrides[consoleToolKey(tool)]; !ok {
			merged = append(merged, tool)
		}
	}
	return append(merged, userTools...)
}

func consoleToolKey(tool map[string]any) string {
	toolType := stringFromAny(tool["type"])
	if toolType == "function" {
		return "function:" + stringFromAny(tool["name"])
	}
	return toolType
}

func consoleToolChoice(choice any) any {
	if choice == nil {
		return "auto"
	}
	return choice
}
