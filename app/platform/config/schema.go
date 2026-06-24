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
	if desc, ok := configDescriptions[key]; ok {
		return desc
	}
	return strings.ReplaceAll(key, ".", " ")
}

var configDescriptions = map[string]string{
	"app.api_key":       "API bearer token for /v1/* routes; empty disables API authentication.",
	"app.app_key":       "Admin console password; generated at first startup when empty.",
	"app.app_url":       "Public base URL used to build local media links.",
	"app.webui_enabled": "Enables the built-in WebUI pages.",
	"app.webui_key":     "Optional WebUI password; empty allows WebUI access once enabled.",
	"account.invalid_credentials.max_failures":   "Consecutive invalid-credential failures before an account is expired.",
	"account.refresh.basic_interval_sec":         "Basic pool quota refresh interval in seconds.",
	"account.refresh.batch_timeout_sec":          "Total timeout in seconds for a quota refresh batch.",
	"account.refresh.enabled":                    "Enables quota refresh mode; false uses random selection with retry feedback.",
	"account.refresh.heavy_interval_sec":         "Heavy pool quota refresh interval in seconds.",
	"account.refresh.on_demand_min_interval_sec": "Minimum interval before an on-demand quota refresh can repeat.",
	"account.refresh.per_token_timeout_sec":      "Timeout in seconds for refreshing one token.",
	"account.refresh.super_interval_sec":         "Super pool quota refresh interval in seconds.",
	"account.refresh.usage_concurrency":          "Concurrency for background usage refresh workers.",
	"account.selection.max_inflight":             "Maximum concurrent requests leased to one account.",
	"account.sso_validation.batch_size":          "Number of SSO accounts validated per scheduled batch.",
	"account.sso_validation.concurrency":         "Concurrency for scheduled SSO validation.",
	"account.sso_validation.enabled":             "Enables scheduled validation for console.x.ai SSO accounts.",
	"account.sso_validation.interval_sec":        "Scheduled SSO validation interval in seconds.",
	"account.sso_validation.max_failures":        "Consecutive SSO validation failures before an account is marked invalid.",
	"asset.delete_timeout":                       "Timeout in seconds for upstream asset delete operations.",
	"asset.download_timeout":                     "Timeout in seconds for upstream asset download operations.",
	"asset.list_timeout":                         "Timeout in seconds for upstream asset list operations.",
	"asset.upload_timeout":                       "Timeout in seconds for upstream asset upload operations.",
	"batch.asset_delete_concurrency":             "Global asset delete concurrency, also used by admin batch cleanup defaults.",
	"batch.asset_list_concurrency":               "Global asset list concurrency shared by concurrent requests.",
	"batch.asset_upload_concurrency":             "Global asset upload concurrency shared by attachment requests.",
	"batch.nsfw_concurrency":                     "Per-token concurrency for admin NSFW enablement jobs.",
	"batch.refresh_concurrency":                  "Per-token concurrency for admin usage refresh jobs.",
	"cache.local.image_max_mb":                   "0 stores images without indexing or eviction; values > 0 enable indexed LRU eviction.",
	"cache.local.video_max_mb":                   "0 stores videos without indexing or eviction; values > 0 enable indexed LRU eviction.",
	"chat.timeout":                               "Timeout in seconds for chat and responses requests.",
	"features.auto_chat_mode_fallback":           "Falls back from auto quota to fast/expert chat modes when possible.",
	"features.custom_instruction":                "Global instruction appended to chat requests.",
	"features.dynamic_statsig":                   "Generates dynamic Statsig identifiers for Grok web compatibility.",
	"features.enable_nsfw":                       "Allows NSFW image generation paths.",
	"features.image_format":                      "Image response format: grok_url, local_url, markdown, HTML-compatible, or base64.",
	"features.imagine_public_image_proxy":        "Downloads imagine-public images locally before returning URLs.",
	"features.memory":                            "Enables conversation memory when supported by the upstream flow.",
	"features.show_search_sources":               "Appends a plaintext Sources section in addition to structured search_sources.",
	"features.stream":                            "Enables streaming responses where the requested endpoint supports them.",
	"features.temporary":                         "Uses temporary conversations where supported.",
	"features.thinking":                          "Includes thinking or reasoning output when available.",
	"features.thinking_summary":                  "Returns a compact reasoning summary instead of full raw thinking text.",
	"features.video_format":                      "Video response format: grok_url, local_url, grok_html, or local_html.",
	"image.stream_timeout":                       "Timeout in seconds for streaming image generation.",
	"image.timeout":                              "Timeout in seconds for image generation and edit requests.",
	"logging.file_level":                         "Minimum level written to rotating local log files.",
	"logging.max_files":                          "Maximum number of daily log files retained.",
	"nsfw.timeout":                               "Timeout in seconds for NSFW enablement requests.",
	"observability.metrics_enabled":              "Exposes Prometheus metrics at /metrics.",
	"observability.pprof_enabled":                "Exposes Go pprof endpoints under /debug/pprof.",
	"proxy.clearance.browser":                    "curl_cffi browser fingerprint used for manual Cloudflare clearance.",
	"proxy.clearance.cf_cookies":                 "Manual Cloudflare Cookie header value.",
	"proxy.clearance.flaresolverr_url":           "FlareSolverr service URL used to refresh Cloudflare clearance.",
	"proxy.clearance.mode":                       "Cloudflare clearance mode: none, manual, or flaresolverr.",
	"proxy.clearance.refresh_interval":           "Cloudflare clearance refresh interval in seconds.",
	"proxy.clearance.timeout_sec":                "Cloudflare challenge wait timeout in seconds.",
	"proxy.clearance.user_agent":                 "User-Agent that must match the clearance cookie source browser.",
	"proxy.egress.mode":                          "Outbound proxy mode: direct, single_proxy, or proxy_pool.",
	"proxy.egress.proxy_pool":                    "Proxy pool for API traffic when proxy_pool mode is enabled.",
	"proxy.egress.proxy_url":                     "Single proxy URL for API traffic.",
	"proxy.egress.resource_proxy_pool":           "Proxy pool for image/video downloads; falls back to proxy_pool.",
	"proxy.egress.resource_proxy_url":            "Proxy URL for image/video downloads; falls back to proxy_url.",
	"proxy.egress.skip_ssl_verify":               "Skips proxy TLS certificate validation for self-signed proxy endpoints.",
	"retry.max_retries":                          "Maximum application-level account-switch retries; 0 disables retries.",
	"retry.on_codes":                             "Comma-separated HTTP status codes that trigger account-switch retries.",
	"retry.reset_session_status_codes":           "HTTP status codes that rebuild transport proxy sessions.",
	"reverse.endpoints.accounts_base":            "Base URL for x.ai account and SSO endpoints.",
	"reverse.endpoints.assets_cdn":               "Base URL for Grok asset CDN requests.",
	"reverse.endpoints.base":                     "Base URL for Grok web API requests.",
	"reverse.endpoints.console_base":             "Base URL for console.x.ai free-account flows.",
	"reverse.endpoints.console_cluster":          "Console API cluster URL used by free-account model calls.",
	"reverse.endpoints.ws_livekit":               "LiveKit WebSocket URL used by realtime and voice flows.",
	"startup.migration.account_batch_size":       "Batch size for startup account storage migrations.",
	"video.timeout":                              "Timeout in seconds for video generation and polling.",
	"voice.timeout":                              "Timeout in seconds for voice and realtime requests.",
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
