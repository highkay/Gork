package protocol

import (
	"encoding/json"
	"strings"
)

// --- payload-side helpers: client tools → console Responses shape ----------

func isEmptyContent(content any) bool {
	switch v := content.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case []any:
		return len(v) == 0
	default:
		return false
	}
}

// consoleToolMessageOutput converts an OpenAI role=tool message into a console
// function_call_output input item. Mirrors _tool_message_to_console_output.
func consoleToolMessageOutput(message map[string]any) map[string]any {
	callID := strings.TrimSpace(stringFromAny(message["tool_call_id"]))
	if callID == "" {
		callID = strings.TrimSpace(stringFromAny(message["call_id"]))
	}
	if callID == "" {
		return nil
	}
	return map[string]any{
		"type":    "function_call_output",
		"call_id": callID,
		"output":  consoleContentToText(message["content"]),
	}
}

// consoleAssistantToolCalls converts an assistant message's tool_calls into
// console function_call input items. Mirrors _assistant_tool_calls_to_console.
func consoleAssistantToolCalls(toolCalls []any) []map[string]any {
	items := make([]map[string]any, 0, len(toolCalls))
	for _, raw := range toolCalls {
		tc, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if t := stringFromAny(tc["type"]); t != "" && t != "function" {
			continue
		}
		fn, ok := tc["function"].(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(stringFromAny(fn["name"]))
		if name == "" {
			continue
		}
		callID := strings.TrimSpace(stringFromAny(tc["id"]))
		if callID == "" {
			callID = strings.TrimSpace(stringFromAny(tc["call_id"]))
		}
		if callID == "" {
			continue
		}
		arguments := consoleArgumentsToString(fn["arguments"])
		items = append(items, map[string]any{
			"type":      "function_call",
			"call_id":   callID,
			"name":      name,
			"arguments": arguments,
			"status":    "completed",
		})
	}
	return items
}

func consoleArgumentsToString(arguments any) string {
	switch v := arguments.(type) {
	case nil:
		return "{}"
	case string:
		if v == "" {
			return "{}"
		}
		return v
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return "{}"
	}
}

func consoleContentToText(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, raw := range v {
			if block, ok := raw.(map[string]any); ok {
				text := stringFromAny(block["text"])
				if text == "" {
					text = stringFromAny(block["content"])
				}
				if text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return stringFromAny(v)
	}
}

// toConsoleTools converts OpenAI function tools to console Responses shape,
// dropping any tool whose name is a console internal/builtin tool.
func toConsoleTools(tools []map[string]any) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if stringFromAny(tool["type"]) != "function" {
			out = append(out, cloneAnyMap(tool))
			continue
		}
		src := tool
		if fn, ok := tool["function"].(map[string]any); ok {
			src = fn
		}
		name := strings.TrimSpace(stringFromAny(src["name"]))
		if name == "" || IsConsoleBuiltinTool(name) {
			continue
		}
		item := map[string]any{"type": "function", "name": name}
		if desc, ok := src["description"]; ok && desc != nil {
			item["description"] = desc
		}
		if params, ok := src["parameters"]; ok && params != nil {
			item["parameters"] = params
		}
		if strict, ok := src["strict"]; ok {
			item["strict"] = strict
		} else if strict, ok := tool["strict"]; ok {
			item["strict"] = strict
		}
		out = append(out, item)
	}
	return out
}

// mergeConsoleTools merges default console tools with user tools; an explicit
// client tool with the same identity overrides the default.
func mergeConsoleTools(defaultTools, userTools []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(defaultTools)+len(userTools))
	positions := map[[2]string]int{}
	add := func(tool map[string]any) {
		ident := consoleToolIdentity(tool)
		if pos, ok := positions[ident]; ok {
			result[pos] = tool
			return
		}
		positions[ident] = len(result)
		result = append(result, tool)
	}
	for _, tool := range defaultTools {
		add(tool)
	}
	for _, tool := range userTools {
		add(tool)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func consoleToolIdentity(tool map[string]any) [2]string {
	t := strings.TrimSpace(stringFromAny(tool["type"]))
	if t == "function" {
		return [2]string{t, strings.TrimSpace(stringFromAny(tool["name"]))}
	}
	return [2]string{t, ""}
}

// toConsoleToolChoice maps an OpenAI tool_choice to console Responses shape.
func toConsoleToolChoice(toolChoice any) any {
	switch v := toolChoice.(type) {
	case nil:
		return nil
	case string:
		return v
	case map[string]any:
		if stringFromAny(v["type"]) != "function" {
			return cloneAnyMap(v)
		}
		name := strings.TrimSpace(stringFromAny(v["name"]))
		if fn, ok := v["function"].(map[string]any); ok {
			name = strings.TrimSpace(stringFromAny(fn["name"]))
		}
		if name == "" {
			return cloneAnyMap(v)
		}
		if IsConsoleBuiltinTool(name) {
			return "auto"
		}
		return map[string]any{"type": "function", "name": name}
	default:
		return v
	}
}

// --- adapter-side helpers: parse streamed function_call events --------------

func (a *ConsoleStreamAdapter) functionKey(obj map[string]any) string {
	if raw := stringFromAny(obj["item_id"]); raw != "" {
		return raw
	}
	idx := obj["output_index"]
	if idx == nil {
		return ""
	}
	idxKey := stringFromAny(idx)
	if mapped, ok := a.fnKeyByOutIdx[idxKey]; ok {
		return mapped
	}
	return "output:" + idxKey
}

func (a *ConsoleStreamAdapter) allowsFunctionName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	_, ok := a.allowedFnNames[name]
	return ok
}

func (a *ConsoleStreamAdapter) forgetOutIdxForKey(key string) {
	for idx, mapped := range a.fnKeyByOutIdx {
		if mapped == key {
			delete(a.fnKeyByOutIdx, idx)
		}
	}
}

func (a *ConsoleStreamAdapter) ignoreFunctionKey(key string) {
	if key == "" {
		return
	}
	a.fnIgnored[key] = struct{}{}
	delete(a.fnCalls, key)
	a.forgetOutIdxForKey(key)
	if len(a.fnOrder) > 0 {
		next := a.fnOrder[:0]
		for _, k := range a.fnOrder {
			if k != key {
				next = append(next, k)
			}
		}
		a.fnOrder = next
	}
}

func (a *ConsoleStreamAdapter) shouldIgnoreFunctionEvent(key string, obj map[string]any) bool {
	if len(a.allowedFnNames) == 0 {
		a.ignoreFunctionKey(key)
		return true
	}
	if key != "" {
		if _, ok := a.fnIgnored[key]; ok {
			return true
		}
	}
	name := strings.TrimSpace(stringFromAny(obj["name"]))
	if name != "" && !a.allowsFunctionName(name) {
		a.ignoreFunctionKey(key)
		return true
	}
	return false
}

func (a *ConsoleStreamAdapter) ensureFunctionCall(key string, obj map[string]any) *consoleFnCall {
	info := a.fnCalls[key]
	if info == nil {
		info = &consoleFnCall{ID: key, Status: "in_progress"}
		a.fnCalls[key] = info
		a.fnOrder = append(a.fnOrder, key)
	}
	if idx := obj["output_index"]; idx != nil {
		a.fnKeyByOutIdx[stringFromAny(idx)] = key
	}
	return info
}

func (a *ConsoleStreamAdapter) upsertFunctionCall(item, eventObj map[string]any, completed bool) {
	eventKey := a.functionKey(eventObj)
	itemKey := stringFromAny(item["id"])
	if itemKey == "" {
		itemKey = stringFromAny(item["call_id"])
	}
	key := itemKey
	if key == "" {
		key = eventKey
	}
	if key == "" {
		return
	}
	if _, ok := a.fnIgnored[key]; ok {
		return
	}
	name := strings.TrimSpace(stringFromAny(item["name"]))
	if name != "" && !a.allowsFunctionName(name) {
		a.ignoreFunctionKey(key)
		return
	}
	info := a.ensureFunctionCall(key, eventObj)
	if v := stringFromAny(item["id"]); v != "" {
		info.ID = v
	}
	if v := stringFromAny(item["call_id"]); v != "" {
		info.CallID = v
	}
	if name != "" {
		info.Name = name
	}
	if args, ok := item["arguments"].(string); ok && (args != "" || info.Arguments == "") {
		info.Arguments = args
	}
	if completed || stringFromAny(item["status"]) == "completed" {
		info.Status = "completed"
	}
}

// ParsedToolCalls returns the collected client function tool calls, in arrival
// order, filtered to allowed client function names.
func (a *ConsoleStreamAdapter) ParsedToolCalls() []ParsedToolCall {
	calls := make([]ParsedToolCall, 0, len(a.fnOrder))
	for _, key := range a.fnOrder {
		info := a.fnCalls[key]
		if info == nil || !a.allowsFunctionName(info.Name) {
			continue
		}
		callID := info.CallID
		if callID == "" {
			callID = info.ID
		}
		args := info.Arguments
		if args == "" {
			args = "{}"
		}
		calls = append(calls, ParsedToolCall{CallID: callID, Name: info.Name, Arguments: args})
	}
	return calls
}
