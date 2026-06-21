package redisrest

import (
	"encoding/json"
	"fmt"
	"strconv"
)

func Int(value any) (int, error) {
	switch typed := value.(type) {
	case nil:
		return 0, nil
	case json.Number:
		parsed, err := typed.Int64()
		return int(parsed), err
	case float64:
		return int(typed), nil
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case string:
		parsed, err := strconv.Atoi(typed)
		return parsed, err
	default:
		return 0, fmt.Errorf("unexpected Redis integer result: %T", value)
	}
}

func String(value any) (string, bool, error) {
	switch typed := value.(type) {
	case nil:
		return "", false, nil
	case string:
		return typed, true, nil
	case json.Number:
		return typed.String(), true, nil
	default:
		return fmt.Sprint(typed), true, nil
	}
}

func StringSlice(value any) ([]string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			value, ok, err := String(item)
			if err != nil {
				return nil, err
			}
			if ok {
				result = append(result, value)
			}
		}
		return result, nil
	case []string:
		return typed, nil
	default:
		return nil, fmt.Errorf("unexpected Redis string slice result: %T", value)
	}
}

func StringMap(value any) (map[string]string, error) {
	result := map[string]string{}
	switch typed := value.(type) {
	case nil:
		return result, nil
	case map[string]any:
		for key, item := range typed {
			value, ok, err := String(item)
			if err != nil {
				return nil, err
			}
			if ok {
				result[key] = value
			}
		}
		return result, nil
	case []any:
		if len(typed)%2 != 0 {
			return nil, fmt.Errorf("unexpected Redis hash pair count: %d", len(typed))
		}
		for i := 0; i < len(typed); i += 2 {
			key, ok, err := String(typed[i])
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			value, ok, err := String(typed[i+1])
			if err != nil {
				return nil, err
			}
			if ok {
				result[key] = value
			}
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unexpected Redis hash result: %T", value)
	}
}
