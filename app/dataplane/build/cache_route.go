package build

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PromptCacheRoute 记录为走 cache-capable 路径而注入的内部工具。
// 响应侧需过滤这些工具产生的 internal tool_call，避免客户端重复执行。
// 对齐 chenyme/grok2api prepareBuildPromptCacheRoute（#730）。
type PromptCacheRoute struct {
	FilterXSearch       bool
	InjectedToolTypes   map[string]struct{}
	ClientDeclaredTools map[string]struct{}
}

// NeedsResponseFilter 是否需要在响应中过滤注入工具产物。
func (r PromptCacheRoute) NeedsResponseFilter() bool {
	return r.FilterXSearch || len(r.InjectedToolTypes) > 0
}

// PreparePromptCacheRoute 在有稳定 prompt_cache_key 时注入 cache 路由工具：
//   - 无 tools：注入 web_search + x_search，tool_choice=none（仅选路，不授予搜索能力）
//   - 有 client tools / web_search：补 x_search 路由
// 媒体模型与 image_generation 工具请求不注入。
func PreparePromptCacheRoute(body []byte, operation, model, promptCacheKey string, allowClientTools bool) ([]byte, PromptCacheRoute, error) {
	route := PromptCacheRoute{
		InjectedToolTypes:   make(map[string]struct{}),
		ClientDeclaredTools: make(map[string]struct{}),
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, route, fmt.Errorf("解析 Build prompt cache 请求: %w", err)
	}
	if payload == nil {
		payload = make(map[string]json.RawMessage)
	}
	tools, err := cacheRouteTools(payload)
	if err != nil {
		return nil, route, err
	}
	for _, rawTool := range tools {
		kind, name := cacheToolIdentity(rawTool)
		if kind == "function" || kind == "custom" {
			if name != "" {
				route.ClientDeclaredTools[name] = struct{}{}
			}
		}
		if kind == "x_search" {
			route.FilterXSearch = true
		}
	}

	if strings.TrimSpace(promptCacheKey) == "" ||
		!isCacheConversationOperation(operation) ||
		isCacheMediaModel(model) ||
		hasCacheToolType(tools, "image_generation") {
		return body, route, nil
	}

	if len(tools) == 0 {
		tools = append(tools,
			json.RawMessage(`{"type":"web_search"}`),
			json.RawMessage(`{"type":"x_search"}`),
		)
		payload["tool_choice"] = mustJSON("none")
		route.InjectedToolTypes["web_search"] = struct{}{}
		route.InjectedToolTypes["x_search"] = struct{}{}
		route.FilterXSearch = true
	} else if !hasCacheToolType(tools, "x_search") && (allowClientTools || hasCacheToolType(tools, "web_search")) {
		tools = append(tools, json.RawMessage(`{"type":"x_search"}`))
		route.InjectedToolTypes["x_search"] = struct{}{}
		route.FilterXSearch = true
	} else {
		// 已有 x_search 或无需注入：仅可能标记 filter。
		if hasCacheToolType(tools, "x_search") {
			route.FilterXSearch = true
		}
		return body, route, nil
	}

	payload["tools"] = mustJSON(tools)
	if _, injected := route.InjectedToolTypes["x_search"]; injected {
		updatedChoice, choiceErr := appendXSearchToAllowedTools(payload["tool_choice"])
		if choiceErr != nil {
			return nil, route, choiceErr
		}
		if len(updatedChoice) > 0 {
			payload["tool_choice"] = updatedChoice
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, route, fmt.Errorf("编码 Build prompt cache 请求: %w", err)
	}
	return encoded, route, nil
}

// AllowClientToolCacheRoute 判断客户端是否允许在已有 tools 时注入 x_search 路由。
// Claude Code / Codex 会话信号或 User-Agent 含 codex 时为 true。
func AllowClientToolCacheRoute(seed string, userAgent string) bool {
	seed = strings.TrimSpace(seed)
	if strings.HasPrefix(seed, "claude:") || strings.HasPrefix(seed, "codex:") {
		return true
	}
	return strings.Contains(strings.ToLower(userAgent), "codex")
}

func appendXSearchToAllowedTools(raw json.RawMessage) (json.RawMessage, error) {
	if isEmptyJSON(raw) {
		return nil, nil
	}
	var choice map[string]json.RawMessage
	if json.Unmarshal(raw, &choice) != nil || choice == nil {
		return nil, nil
	}
	var choiceType string
	_ = json.Unmarshal(choice["type"], &choiceType)
	if strings.TrimSpace(choiceType) != "allowed_tools" {
		return nil, nil
	}
	var allowed []json.RawMessage
	if json.Unmarshal(choice["tools"], &allowed) != nil {
		return nil, nil
	}
	for _, item := range allowed {
		kind, _ := cacheToolIdentity(item)
		if kind == "x_search" {
			return nil, nil
		}
	}
	allowed = append(allowed, json.RawMessage(`{"type":"x_search"}`))
	choice["tools"] = mustJSON(allowed)
	return json.Marshal(choice)
}

func cacheRouteTools(payload map[string]json.RawMessage) ([]json.RawMessage, error) {
	raw, exists := payload["tools"]
	if !exists || isEmptyJSON(raw) {
		return nil, nil
	}
	var tools []json.RawMessage
	if json.Unmarshal(raw, &tools) != nil {
		return nil, fmt.Errorf("tools 必须是数组")
	}
	return tools, nil
}

func cacheToolIdentity(raw json.RawMessage) (kind, name string) {
	var tool struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &tool) != nil {
		return "", ""
	}
	return strings.TrimSpace(tool.Type), strings.TrimSpace(tool.Name)
}

func hasCacheToolType(tools []json.RawMessage, kind string) bool {
	for _, rawTool := range tools {
		toolType, _ := cacheToolIdentity(rawTool)
		if toolType == kind {
			return true
		}
	}
	return false
}

func isCacheConversationOperation(operation string) bool {
	switch strings.TrimSpace(operation) {
	case "", "responses", "chat", "messages":
		return true
	default:
		return false
	}
}

func isCacheMediaModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(model, "image") || strings.Contains(model, "imagine") || strings.Contains(model, "video")
}

func isEmptyJSON(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null"
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return data
}
