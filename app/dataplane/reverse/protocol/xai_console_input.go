package protocol

import (
	"fmt"
	"slices"
	"strings"
)

func consoleInputItems(message map[string]any) []map[string]any {
	role := stringFromAny(message["role"])
	if role == "tool" && stringFromAny(message["tool_call_id"]) != "" {
		return []map[string]any{{
			"type":    "function_call_output",
			"call_id": stringFromAny(message["tool_call_id"]),
			"output":  consoleContentText(message["content"]),
		}}
	}
	apiRole := "user"
	// Console API accepts system/user/assistant; developer folds into system and unknown/tool roles into user.
	if role == "system" || role == "developer" {
		apiRole = "system"
	} else if role == "assistant" {
		apiRole = "assistant"
	}
	items := []map[string]any{}
	blocks := consoleContentBlocks(message["content"])
	if len(blocks) > 0 {
		items = append(items, map[string]any{"role": apiRole, "content": blocks})
	}
	if role == "assistant" {
		items = append(items, assistantToolCallsToConsole(message["tool_calls"])...)
	}
	return items
}

func assistantToolCallsToConsole(raw any) []map[string]any {
	calls := toolCallMaps(raw)
	items := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		function, _ := call["function"].(map[string]any)
		name := stringFromAny(function["name"])
		if name == "" {
			name = stringFromAny(call["name"])
		}
		if name == "" {
			continue
		}
		args := stringFromAny(function["arguments"])
		if args == "" {
			args = stringFromAny(call["arguments"])
		}
		if args == "" {
			args = "{}"
		}
		items = append(items, map[string]any{
			"type":      "function_call",
			"call_id":   stringFromAny(call["id"]),
			"name":      name,
			"arguments": args,
			"status":    "completed",
		})
	}
	return items
}

func toolCallMaps(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

func consoleContentText(content any) string {
	blocks := consoleContentBlocks(content)
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if text := stringFromAny(block["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func consoleContentBlocks(content any) []map[string]any {
	switch typed := content.(type) {
	case string:
		return []map[string]any{{"type": "input_text", "text": typed}}
	case []any:
		blocks := []map[string]any{}
		for _, raw := range typed {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch stringFromAny(block["type"]) {
			case "text":
				blocks = append(blocks, map[string]any{"type": "input_text", "text": stringFromAny(block["text"])})
			case "image_url":
				if imageURL, ok := block["image_url"].(map[string]any); ok {
					if url := stringFromAny(imageURL["url"]); url != "" {
						blocks = append(blocks, map[string]any{"type": "input_image", "image_url": url})
					}
				}
			default:
				text := stringFromAny(block["text"])
				if text == "" {
					text = consolePythonString(block)
				}
				blocks = append(blocks, map[string]any{"type": "input_text", "text": text})
			}
		}
		return blocks
	default:
		return []map[string]any{{"type": "input_text", "text": consolePythonString(content)}}
	}
}

func consolePythonString(value any) string {
	switch v := value.(type) {
	case nil:
		return "None"
	case string:
		return v
	case bool:
		if v {
			return "True"
		}
		return "False"
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("'%s': %s", key, consolePythonLiteral(v[key])))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprint(v)
	}
}

func consolePythonLiteral(value any) string {
	switch v := value.(type) {
	case string:
		escaped := strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `'`, `\'`)
		return "'" + escaped + "'"
	case nil:
		return "None"
	case bool:
		if v {
			return "True"
		}
		return "False"
	case map[string]any:
		return consolePythonString(v)
	default:
		return fmt.Sprint(v)
	}
}
