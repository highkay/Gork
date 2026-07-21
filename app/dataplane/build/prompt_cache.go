package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	promptCacheIdentityVersion = "v1"
	maxPromptCacheSeedBytes    = 1024
	maxCodexTurnMetadataSize   = 16 << 10
)

// ResolvePromptCacheKey 将客户端显式键或会话种子归一为上游 prompt_cache_key。
// 空种子返回空串（不注入）。与 chenyme #631 对齐：固定长度摘要，降低共享池碰撞。
func ResolvePromptCacheKey(explicitKey, sessionSeed, upstreamModel string) string {
	seed := strings.TrimSpace(explicitKey)
	if seed == "" {
		seed = strings.TrimSpace(sessionSeed)
	}
	model := strings.ToLower(strings.TrimSpace(upstreamModel))
	if seed == "" || model == "" {
		return ""
	}
	source := fmt.Sprintf("gork:prompt-cache:%s:%s:%s", promptCacheIdentityVersion, model, seed)
	digest := sha256.Sum256([]byte(source))
	hexID := hex.EncodeToString(digest[:16])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexID[0:8], hexID[8:12], hexID[12:16], hexID[16:20], hexID[20:32])
}

// PromptCacheKeyFromOverrides 从 OpenAI 请求覆盖字段提取显式缓存键。
func PromptCacheKeyFromOverrides(overrides map[string]any) string {
	if overrides == nil {
		return ""
	}
	for _, key := range []string{"prompt_cache_key", "promptCacheKey"} {
		if v, ok := overrides[key]; ok && v != nil {
			switch typed := v.(type) {
			case string:
				if s := strings.TrimSpace(typed); s != "" {
					return s
				}
			default:
				if s := strings.TrimSpace(fmt.Sprint(typed)); s != "" && s != "<nil>" {
					return s
				}
			}
		}
	}
	return ""
}

// ExtractPromptCacheSeed 提取客户端会话标识（Claude Code / Codex / OpenAI 兼容信号）。
// 对齐 chenyme/grok2api prompt_cache 亲和：优先 Header，再 body 字段。
// title 生成等 Claude Code 辅助请求返回空，避免与主会话共享 reasoning replay。
func ExtractPromptCacheSeed(headers http.Header, body []byte) string {
	if isClaudeCodeTitleRequest(headers, body) {
		return ""
	}
	if headers != nil {
		if seed := normalizePromptCacheSeed(headers.Get("X-Claude-Code-Session-Id")); seed != "" {
			return claudeCodePromptCacheSeed(seed, headers)
		}
		if seed := codexPromptCacheSeedFromHeaders(headers); seed != "" {
			return seed
		}
		for _, name := range []string{
			"X-Session-Id", "Session-Id", "Session_id",
			"X-Conversation-Id", "Conversation-Id", "Conversation_id",
			"X-Client-Session-Id", "X-Grok-Conv-Id",
		} {
			if seed := normalizePromptCacheSeed(headers.Get(name)); seed != "" {
				return seed
			}
		}
	}
	var payload struct {
		PromptCacheKey      string `json:"prompt_cache_key"`
		ConversationID      string `json:"conversation_id"`
		ConversationIDCamel string `json:"conversationId"`
		SessionID           string `json:"session_id"`
		SessionIDCamel      string `json:"sessionId"`
		Metadata            struct {
			SessionID      string `json:"session_id"`
			SessionIDCamel string `json:"sessionId"`
			UserID         string `json:"user_id"`
		} `json:"metadata"`
		ClientMetadata map[string]json.RawMessage `json:"client_metadata"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	if seed := normalizePromptCacheSeed(payload.PromptCacheKey); seed != "" {
		return seed
	}
	if seed := normalizePromptCacheSeed(payload.Metadata.SessionID); seed != "" {
		return seed
	}
	if seed := normalizePromptCacheSeed(payload.Metadata.SessionIDCamel); seed != "" {
		return seed
	}
	if seed := promptCacheSeedFromUserID(payload.Metadata.UserID); seed != "" {
		return claudeCodePromptCacheSeed(seed, headers)
	}
	if seed := codexPromptCacheSeedFromRawTurnMetadata(payload.ClientMetadata["x-codex-turn-metadata"]); seed != "" {
		return seed
	}
	if seed := normalizeRawPromptCacheSeed(payload.ClientMetadata["x-codex-window-id"]); seed != "" {
		return "codex:window:" + seed
	}
	if seed := normalizePromptCacheSeed(payload.SessionID); seed != "" {
		return seed
	}
	if seed := normalizePromptCacheSeed(payload.SessionIDCamel); seed != "" {
		return seed
	}
	if seed := normalizePromptCacheSeed(payload.ConversationID); seed != "" {
		return seed
	}
	return normalizePromptCacheSeed(payload.ConversationIDCamel)
}

// ExtractGrokTurnIndex 从请求头读取官方客户端轮次（仅透传，不推断）。
func ExtractGrokTurnIndex(headers http.Header) string {
	if headers == nil {
		return ""
	}
	return normalizeGrokTurnIndex(headers.Get("x-grok-turn-idx"))
}

func claudeCodePromptCacheSeed(sessionID string, headers http.Header) string {
	sessionID = normalizePromptCacheSeed(sessionID)
	if sessionID == "" {
		return ""
	}
	agentID := "main"
	if headers != nil {
		if value := normalizePromptCacheSeed(headers.Get("X-Claude-Code-Agent-Id")); value != "" {
			agentID = value
		}
	}
	return "claude:" + sessionID + ":agent:" + agentID
}

func codexPromptCacheSeedFromHeaders(headers http.Header) string {
	if headers == nil {
		return ""
	}
	if seed := codexPromptCacheSeedFromTurnMetadata(headers.Get("X-Codex-Turn-Metadata")); seed != "" {
		return seed
	}
	if seed := normalizePromptCacheSeed(headers.Get("X-Codex-Window-Id")); seed != "" {
		return "codex:window:" + seed
	}
	return ""
}

func codexPromptCacheSeedFromTurnMetadata(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxCodexTurnMetadataSize {
		return ""
	}
	var metadata struct {
		PromptCacheKey string `json:"prompt_cache_key"`
		WindowID       string `json:"window_id"`
	}
	if json.Unmarshal([]byte(value), &metadata) != nil {
		return ""
	}
	if seed := normalizePromptCacheSeed(metadata.PromptCacheKey); seed != "" {
		return seed
	}
	if seed := normalizePromptCacheSeed(metadata.WindowID); seed != "" {
		return "codex:window:" + seed
	}
	return ""
}

func codexPromptCacheSeedFromRawTurnMetadata(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return codexPromptCacheSeedFromTurnMetadata(value)
	}
	return codexPromptCacheSeedFromTurnMetadata(string(raw))
}

func normalizeRawPromptCacheSeed(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return normalizePromptCacheSeed(value)
}

func isClaudeCodeTitleRequest(headers http.Header, body []byte) bool {
	if headers == nil || normalizePromptCacheSeed(headers.Get("X-Claude-Code-Session-Id")) == "" || len(body) == 0 {
		return false
	}
	var payload struct {
		System json.RawMessage `json:"system"`
	}
	if json.Unmarshal(body, &payload) != nil || len(payload.System) == 0 {
		return false
	}
	var texts []string
	var text string
	if json.Unmarshal(payload.System, &text) == nil {
		texts = append(texts, text)
	} else {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(payload.System, &blocks) != nil {
			return false
		}
		for _, block := range blocks {
			if block.Type == "text" {
				texts = append(texts, block.Text)
			}
		}
	}
	for _, value := range texts {
		value = strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(value, "generate a concise") && strings.Contains(value, "title") && strings.Contains(value, "coding session") {
			return true
		}
	}
	return false
}

func promptCacheSeedFromUserID(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	var embedded struct {
		SessionID      string `json:"session_id"`
		SessionIDCamel string `json:"sessionId"`
	}
	if json.Unmarshal([]byte(userID), &embedded) == nil {
		if seed := normalizePromptCacheSeed(embedded.SessionID); seed != "" {
			return seed
		}
		if seed := normalizePromptCacheSeed(embedded.SessionIDCamel); seed != "" {
			return seed
		}
	}
	const marker = "_session_"
	if index := strings.LastIndex(userID, marker); index >= 0 {
		return normalizePromptCacheSeed(userID[index+len(marker):])
	}
	return ""
}

func normalizePromptCacheSeed(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxPromptCacheSeedBytes {
		return ""
	}
	return value
}
