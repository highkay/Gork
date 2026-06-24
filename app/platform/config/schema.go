package config

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type ConfigKind string

const (
	ConfigKindBool       ConfigKind = "bool"
	ConfigKindInt        ConfigKind = "int"
	ConfigKindFloat      ConfigKind = "float"
	ConfigKindString     ConfigKind = "string"
	ConfigKindStringList ConfigKind = "string_list"
)

type ConfigSchemaEntry struct {
	Key       string     `json:"key"`
	Kind      ConfigKind `json:"kind"`
	Default   any        `json:"default"`
	Min       *float64   `json:"min,omitempty"`
	Max       *float64   `json:"max,omitempty"`
	Sensitive bool       `json:"sensitive"`
	HotReload bool       `json:"hot_reload"`
	Env       string     `json:"env"`
	Desc      string     `json:"desc"`
}

type ConfigValidationIssue struct {
	Key     string `json:"key"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ConfigValidationError struct {
	Issues []ConfigValidationIssue `json:"issues"`
}

func (e *ConfigValidationError) Error() string {
	if e == nil || len(e.Issues) == 0 {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Issues[0].Key, e.Issues[0].Message)
}

func DefaultSchema(defaults map[string]any) []ConfigSchemaEntry {
	flat := FlattenConfig(defaults, "")
	keys := make([]string, 0, len(flat))
	for key := range flat {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]ConfigSchemaEntry, 0, len(keys))
	for _, key := range keys {
		entry := ConfigSchemaEntry{
			Key:       key,
			Kind:      inferConfigKind(flat[key]),
			Default:   flat[key],
			Sensitive: sensitiveConfigKey(key),
			HotReload: hotReloadConfigKey(key),
			Env:       EnvNameForKey("GROK_", key),
			Desc:      configDescription(key),
		}
		if min, ok := configMin(key); ok {
			entry.Min = &min
		}
		if max, ok := configMax(key); ok {
			entry.Max = &max
		}
		entries = append(entries, entry)
	}
	return entries
}

func EnvNameForKey(prefix, key string) string {
	if prefix == "" {
		prefix = "GROK_"
	}
	return prefix + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

func ValidateConfigData(defaults map[string]any, data map[string]any) *ConfigValidationError {
	schema := DefaultSchema(defaults)
	flat := FlattenConfig(data, "")
	issues := []ConfigValidationIssue{}
	known := map[string]ConfigSchemaEntry{}
	for _, entry := range schema {
		known[entry.Key] = entry
	}
	for key, value := range flat {
		entry, ok := known[key]
		if !ok {
			issues = append(issues, ConfigValidationIssue{Key: key, Code: "unknown_key", Message: "unknown config key"})
			continue
		}
		issues = append(issues, validateConfigValue(entry, value)...)
	}
	if len(issues) == 0 {
		return nil
	}
	return &ConfigValidationError{Issues: issues}
}

func ValidateConfigPatch(defaults map[string]any, patch map[string]any) *ConfigValidationError {
	return ValidateConfigData(defaults, patch)
}

func RenderSchemaMarkdown(entries []ConfigSchemaEntry) string {
	var b strings.Builder
	b.WriteString("| Key | Type | Default | Env | Hot reload | Sensitive | Description |\n")
	b.WriteString("| :-- | :-- | :-- | :-- | :-- | :-- | :-- |\n")
	for _, entry := range entries {
		b.WriteString(fmt.Sprintf("| `%s` | `%s` | `%v` | `%s` | `%t` | `%t` | %s |\n",
			entry.Key, entry.Kind, entry.Default, entry.Env, entry.HotReload, entry.Sensitive, entry.Desc))
	}
	return b.String()
}

func validateConfigValue(entry ConfigSchemaEntry, value any) []ConfigValidationIssue {
	issues := []ConfigValidationIssue{}
	if !configValueMatchesKind(entry.Kind, value) {
		return append(issues, ConfigValidationIssue{
			Key:     entry.Key,
			Code:    "invalid_type",
			Message: fmt.Sprintf("expected %s", entry.Kind),
		})
	}
	if number, ok := numericConfigValue(value); ok {
		if entry.Min != nil && number < *entry.Min {
			issues = append(issues, ConfigValidationIssue{Key: entry.Key, Code: "too_small", Message: fmt.Sprintf("must be >= %v", *entry.Min)})
		}
		if entry.Max != nil && number > *entry.Max {
			issues = append(issues, ConfigValidationIssue{Key: entry.Key, Code: "too_large", Message: fmt.Sprintf("must be <= %v", *entry.Max)})
		}
	}
	if configLooksLikeDSN(entry.Key) {
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" && !validConfigDSN(s) {
			issues = append(issues, ConfigValidationIssue{Key: entry.Key, Code: "invalid_dsn", Message: "invalid DSN"})
		}
	}
	if strings.Contains(entry.Key, "proxy") && strings.Contains(entry.Key, "url") {
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" && !validConfigURL(s) {
			issues = append(issues, ConfigValidationIssue{Key: entry.Key, Code: "invalid_url", Message: "invalid URL"})
		}
	}
	if strings.HasSuffix(entry.Key, "_format") {
		if s, ok := value.(string); ok && !validMediaFormat(s) {
			issues = append(issues, ConfigValidationIssue{Key: entry.Key, Code: "invalid_format", Message: "invalid media format"})
		}
	}
	return issues
}

func inferConfigKind(value any) ConfigKind {
	switch value.(type) {
	case bool:
		return ConfigKindBool
	case int, int64, int32:
		return ConfigKindInt
	case float32, float64:
		return ConfigKindFloat
	case []any, []string:
		return ConfigKindStringList
	default:
		return ConfigKindString
	}
}

func configValueMatchesKind(kind ConfigKind, value any) bool {
	switch kind {
	case ConfigKindBool:
		_, ok := value.(bool)
		return ok || parseBoolString(value)
	case ConfigKindInt:
		_, ok := numericConfigValue(value)
		return ok
	case ConfigKindFloat:
		_, ok := numericConfigValue(value)
		return ok
	case ConfigKindString:
		_, ok := value.(string)
		return ok
	case ConfigKindStringList:
		switch value.(type) {
		case []any, []string:
			return true
		default:
			return false
		}
	default:
		return true
	}
}

func numericConfigValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	case fmt.Stringer:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed.String()), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func parseBoolString(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on", "0", "false", "no", "off":
		return true
	default:
		return false
	}
}

func configMin(key string) (float64, bool) {
	switch {
	case strings.Contains(key, "concurrency"),
		strings.Contains(key, "batch_size"),
		strings.Contains(key, "timeout"),
		strings.Contains(key, "interval"),
		strings.Contains(key, "max_failures"),
		strings.Contains(key, "max_files"):
		return 1, true
	case strings.HasSuffix(key, "_max_mb"):
		return 0, true
	default:
		return 0, false
	}
}

func configMax(key string) (float64, bool) {
	switch {
	case strings.Contains(key, "concurrency"):
		return 10000, true
	case strings.Contains(key, "batch_size"):
		return 10000, true
	case strings.HasSuffix(key, "_max_mb"):
		return 1048576, true
	default:
		return 0, false
	}
}

func sensitiveConfigKey(key string) bool {
	terms := []string{"key", "token", "secret", "password", "passwd", "dsn", "sso", "clearance", "cookie"}
	normalized := strings.ToLower(key)
	for _, term := range terms {
		if strings.Contains(normalized, term) {
			return true
		}
	}
	return false
}

func hotReloadConfigKey(key string) bool {
	return !strings.HasPrefix(key, "account.storage") &&
		!strings.Contains(key, "_url") &&
		!strings.Contains(key, "dsn")
}

func configDescription(key string) string {
	switch key {
	case "cache.local.image_max_mb", "cache.local.video_max_mb":
		return "0 means save files without size limit, index, reconcile, or eviction; values > 0 enable LRU eviction."
	case "startup.migration.account_batch_size":
		return "Account startup migration batch size."
	default:
		return strings.ReplaceAll(key, ".", " ")
	}
}

func configLooksLikeDSN(key string) bool {
	return strings.Contains(key, "_url") || strings.Contains(key, "dsn")
}

func validConfigDSN(value string) bool {
	if strings.Contains(value, "://") {
		return validConfigURL(value)
	}
	return strings.Contains(value, "@") || strings.Contains(value, "/") || strings.Contains(value, ":")
}

func validConfigURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != ""
}

func validMediaFormat(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "url", "base64", "openai", "xai",
		"grok_url", "local_url", "grok_md", "local_md",
		"grok_html", "local_html":
		return true
	default:
		return false
	}
}
