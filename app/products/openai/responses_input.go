package openai

import "strings"

func responseMessages(instructions string, input any) []map[string]any {
	messages := []map[string]any{}
	if strings.TrimSpace(instructions) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": instructions})
	}
	return append(messages, parseResponseInput(input)...)
}

func parseResponseInput(input any) []map[string]any {
	if text, ok := input.(string); ok {
		return []map[string]any{{"role": "user", "content": text}}
	}
	items := responseInputItems(input)
	messages := []map[string]any{}
	for _, item := range items {
		itemType := stringValue(item["type"], "")
		if itemType == "" {
			if _, ok := item["role"]; ok {
				itemType = "message"
			}
		}
		switch itemType {
		case "function_call":
			callID := stringValue(item["call_id"], "")
			messages = append(messages, map[string]any{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []any{map[string]any{
					"id":   callID,
					"type": "function",
					"function": map[string]any{
						"name":      stringValue(item["name"], ""),
						"arguments": stringValue(item["arguments"], "{}"),
					},
				}},
			})
		case "function_call_output":
			// 保留多模态 tool output（文本 + 图片 parts），对齐 chenyme #713。
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": stringValue(item["call_id"], ""),
				"content":      normalizeFunctionCallOutput(item["output"]),
			})
		case "message":
			messages = append(messages, map[string]any{
				"role":    stringValue(item["role"], "user"),
				"content": normalizeResponseContent(item["content"]),
			})
		}
	}
	return messages
}

// normalizeFunctionCallOutput 保留字符串输出；数组/多模态 content 走 normalizeResponseContent。
func normalizeFunctionCallOutput(output any) any {
	if output == nil {
		return ""
	}
	switch typed := output.(type) {
	case string:
		return typed
	case []any, []map[string]any:
		return normalizeResponseContent(typed)
	default:
		// 非字符串标量：尽量转文本；结构化对象保留 normalize 路径。
		if text := stringValue(output, ""); text != "" {
			return text
		}
		return normalizeResponseContent(output)
	}
}

func responseInputItems(input any) []map[string]any {
	switch typed := input.(type) {
	case []map[string]any:
		return typed
	case []any:
		items := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				items = append(items, mapped)
			}
		}
		return items
	default:
		return nil
	}
}

func normalizeResponseContent(content any) any {
	parts, ok := content.([]any)
	if !ok {
		return valueOrDefaultAny(content, "")
	}
	normalized := []any{}
	for _, part := range parts {
		mapped, ok := part.(map[string]any)
		if !ok {
			continue
		}
		switch stringValue(mapped["type"], "") {
		case "input_text", "output_text":
			normalized = append(normalized, map[string]any{"type": "text", "text": stringValue(mapped["text"], "")})
		case "image", "input_image":
			if url := responseImageURL(mapped); url != "" {
				normalized = append(normalized, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
			}
		default:
			normalized = append(normalized, mapped)
		}
	}
	return normalized
}

func responseImageURL(part map[string]any) string {
	source := valueOrDefaultAny(part["image_url"], part["source"])
	if mapped, ok := source.(map[string]any); ok {
		return stringValue(mapped["url"], "")
	}
	return stringValue(source, "")
}

func toResponseChatTools(tools []map[string]any) []map[string]any {
	normalised := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if stringValue(tool["type"], "") == "function" && tool["function"] == nil && stringValue(tool["name"], "") != "" {
			normalised = append(normalised, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        stringValue(tool["name"], ""),
					"description": stringValue(tool["description"], ""),
					"parameters":  tool["parameters"],
				},
			})
			continue
		}
		normalised = append(normalised, tool)
	}
	return normalised
}
