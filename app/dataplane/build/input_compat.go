package build

import (
	"fmt"
	"strings"
)

// BuildResponsesInput 将 chat messages 转为 Build /responses 的 instructions + input。
// system/developer → instructions；其余为 structured 数组：
// message / function_call / function_call_output（对齐 OpenAI Responses 多轮工具历史）。
func BuildResponsesInput(messages []ChatMessage) (instructions string, input any, err error) {
	var systemParts []string
	items := make([]any, 0, len(messages))
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		text := strings.TrimSpace(msg.Content)
		switch role {
		case "system", "developer":
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case "assistant":
			items = appendAssistantInput(items, msg)
		case "tool":
			item, itemErr := functionCallOutputItem(msg)
			if itemErr != nil {
				return "", nil, itemErr
			}
			items = append(items, item)
		default:
			// user / 未知角色按 user message
			if text == "" && len(msg.ToolCalls) == 0 {
				continue
			}
			items = append(items, messageInputItem("user", text))
		}
	}
	if len(items) == 0 {
		return "", nil, fmt.Errorf("empty message after chat→responses conversion")
	}
	return strings.Join(systemParts, "\n\n"), items, nil
}

func appendAssistantInput(items []any, msg ChatMessage) []any {
	text := strings.TrimSpace(msg.Content)
	if text != "" {
		items = append(items, messageInputItem("assistant", text))
	}
	for _, call := range msg.ToolCalls {
		if item, ok := functionCallItem(call); ok {
			items = append(items, item)
		}
	}
	return items
}

func messageInputItem(role, text string) map[string]any {
	return map[string]any{
		"type":    "message",
		"role":    role,
		"content": text,
	}
}

func functionCallItem(call map[string]any) (map[string]any, bool) {
	if call == nil {
		return nil, false
	}
	callID := firstNonEmpty(
		stringField(call, "id"),
		stringField(call, "call_id"),
	)
	name := stringField(call, "name")
	args := stringField(call, "arguments")
	if fn, ok := call["function"].(map[string]any); ok {
		name = firstNonEmpty(name, stringField(fn, "name"))
		args = firstNonEmpty(args, stringField(fn, "arguments"))
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false
	}
	if callID == "" {
		callID = "call_" + name
	}
	if args == "" {
		args = "{}"
	}
	return map[string]any{
		"type":      "function_call",
		"call_id":   callID,
		"name":      name,
		"arguments": args,
	}, true
}

func functionCallOutputItem(msg ChatMessage) (map[string]any, error) {
	callID := strings.TrimSpace(msg.ToolCallID)
	if callID == "" {
		return nil, fmt.Errorf("tool message missing tool_call_id")
	}
	output := strings.TrimSpace(msg.Content)
	return map[string]any{
		"type":    "function_call_output",
		"call_id": callID,
		"output":  output,
	}, nil
}

