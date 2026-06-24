package tomlutil

import (
	"bufio"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

func Parse(r io.Reader) (map[string]any, error) {
	data := map[string]any{}
	current := data
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := stripComment(strings.TrimSpace(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = EnsureSection(data, strings.TrimSpace(line[1:len(line)-1]))
			continue
		}
		key, rawValue, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		current[strings.TrimSpace(key)] = ParseValue(strings.TrimSpace(rawValue))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func EnsureSection(root map[string]any, dotted string) map[string]any {
	current := root
	for _, part := range strings.Split(dotted, ".") {
		part = strings.TrimSpace(part)
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	return current
}

func ParseValue(raw string) any {
	if len(raw) >= 2 && raw[0] == '[' && raw[len(raw)-1] == ']' {
		return parseArray(raw[1 : len(raw)-1])
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		if unquoted, err := strconv.Unquote(raw); err == nil {
			return unquoted
		}
		return raw[1 : len(raw)-1]
	}
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return raw[1 : len(raw)-1]
	}
	switch strings.ToLower(raw) {
	case "true":
		return true
	case "false":
		return false
	}
	if strings.ContainsAny(raw, ".eE") {
		if value, err := strconv.ParseFloat(raw, 64); err == nil {
			return value
		}
	}
	if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return value
	}
	return raw
}

func Write(w io.Writer, data map[string]any) error {
	return writeMap(w, "", data)
}

func FormatValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strconv.Quote(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'g', -1, 32)
	default:
		if formatted, ok := formatArray(typed); ok {
			return formatted
		}
		return strconv.Quote(fmt.Sprint(typed))
	}
}

func stripComment(line string) string {
	quote := rune(0)
	for index, char := range line {
		if quote != 0 {
			if char == quote && (quote != '"' || index == 0 || line[index-1] != '\\') {
				quote = 0
			}
			continue
		}
		if char == '"' || char == '\'' {
			quote = char
			continue
		}
		if char == '#' {
			return strings.TrimSpace(line[:index])
		}
	}
	return line
}

func parseArray(raw string) []any {
	values := []any{}
	for _, item := range splitArrayItems(raw) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		values = append(values, ParseValue(item))
	}
	return values
}

func splitArrayItems(raw string) []string {
	items := []string{}
	start := 0
	depth := 0
	quote := byte(0)
	for index := 0; index < len(raw); index++ {
		char := raw[index]
		if quote != 0 {
			if char == quote && (quote != '"' || index == 0 || raw[index-1] != '\\') {
				quote = 0
			}
			continue
		}
		switch char {
		case '"', '\'':
			quote = char
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				items = append(items, raw[start:index])
				start = index + 1
			}
		}
	}
	items = append(items, raw[start:])
	return items
}

func writeMap(w io.Writer, prefix string, data map[string]any) error {
	scalars, sections := splitKeys(data)
	if prefix != "" {
		if _, err := fmt.Fprintf(w, "[%s]\n", prefix); err != nil {
			return err
		}
	}
	for _, key := range scalars {
		if _, err := fmt.Fprintf(w, "%s = %s\n", key, FormatValue(data[key])); err != nil {
			return err
		}
	}
	for _, key := range sections {
		if prefix != "" || len(scalars) > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		nextPrefix := key
		if prefix != "" {
			nextPrefix = prefix + "." + key
		}
		if err := writeMap(w, nextPrefix, data[key].(map[string]any)); err != nil {
			return err
		}
	}
	return nil
}

func splitKeys(data map[string]any) ([]string, []string) {
	scalars := []string{}
	sections := []string{}
	for key, value := range data {
		if _, ok := value.(map[string]any); ok {
			sections = append(sections, key)
			continue
		}
		scalars = append(scalars, key)
	}
	sort.Strings(scalars)
	sort.Strings(sections)
	return scalars, sections
}

func formatArray(value any) (string, bool) {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return "", false
	}
	if reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array {
		return "", false
	}
	parts := make([]string, 0, reflected.Len())
	for index := 0; index < reflected.Len(); index++ {
		parts = append(parts, FormatValue(reflected.Index(index).Interface()))
	}
	return "[" + strings.Join(parts, ", ") + "]", true
}
