package platform

// String reads a string from a JSON-like object.
func String(values map[string]any, key string) string {
	value, ok := values[key].(string)
	if !ok {
		return ""
	}
	return value
}

// Bool reads a bool from a JSON-like object.
func Bool(values map[string]any, key string) bool {
	value, ok := values[key].(bool)
	return ok && value
}

// Number reads a number from a JSON-like object.
func Number(values map[string]any, key string) float64 {
	switch value := values[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

// Object reads a nested object from a JSON-like object.
func Object(values map[string]any, key string) map[string]any {
	value, ok := values[key].(map[string]any)
	if !ok {
		return nil
	}
	return value
}

// Array reads an array from a JSON-like object.
func Array(values map[string]any, key string) []any {
	value, ok := values[key].([]any)
	if !ok {
		return nil
	}
	return value
}
