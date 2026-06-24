package openai

import "strings"

func routerStringDefault(value string, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

func routerIntDefault(value int, defaultValue int) int {
	if value == 0 {
		return defaultValue
	}
	return value
}

func lastUserText(messages []MessageItem) string {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Role != "user" {
			continue
		}
		if text, ok := message.Content.(string); ok && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

func routerToolMaps(values []any) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if item, ok := value.(map[string]any); ok {
			result = append(result, item)
		}
	}
	return result
}

func routerInputEmpty(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return typed == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}
