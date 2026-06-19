package protocol

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
	platform "github.com/dslzl/gork/app/platform"
)

var ConsoleModels = map[string]string{
	"grok-4.3-fast":                        "grok-4.3",
	"grok-4.3-console":                     "grok-4.3",
	"grok-4.3-low":                         "grok-4.3",
	"grok-4.3-medium":                      "grok-4.3",
	"grok-4.3-high":                        "grok-4.3",
	"grok-4.20-0309-reasoning-console":     "grok-4.20-0309-reasoning",
	"grok-4.20-0309-console":               "grok-4.20-0309",
	"grok-4.20-0309-non-reasoning-console": "grok-4.20-0309-non-reasoning",
	"grok-4.20-multi-agent-console":        "grok-4.20-multi-agent-0309",
	"grok-4.20-multi-agent-low":            "grok-4.20-multi-agent-0309",
	"grok-4.20-multi-agent-medium":         "grok-4.20-multi-agent-0309",
	"grok-4.20-multi-agent-high":           "grok-4.20-multi-agent-0309",
	"grok-4.20-multi-agent-xhigh":          "grok-4.20-multi-agent-0309",
	"grok-build-console":                   "grok-build-0.1",
}

var consoleModelsWithReasoning = map[string]struct{}{
	"grok-4.3":                   {},
	"grok-4.20-multi-agent-0309": {},
}

var consoleModelFixedEffort = map[string]string{
	"grok-4.3-low":                 "low",
	"grok-4.3-medium":              "medium",
	"grok-4.3-high":                "high",
	"grok-4.20-multi-agent-low":    "low",
	"grok-4.20-multi-agent-medium": "medium",
	"grok-4.20-multi-agent-high":   "high",
	"grok-4.20-multi-agent-xhigh":  "xhigh",
}

var consoleModelMaxOutputTokens = map[string]int{
	"grok-4.20-multi-agent-0309": 2000000,
	"grok-build-0.1":             256000,
}

var consoleModelsWithSearchTools = map[string]struct{}{
	"grok-4.20-multi-agent-0309":   {},
	"grok-4.20-0309":               {},
	"grok-4.20-0309-reasoning":     {},
	"grok-4.20-0309-non-reasoning": {},
	"grok-4.3":                     {},
	"grok-build-0.1":               {},
}

var consoleEffortMap = map[string]string{
	"none":    "none",
	"minimal": "low",
	"low":     "low",
	"medium":  "medium",
	"high":    "high",
	"xhigh":   "xhigh",
}

type ConsolePayloadOptions struct {
	Messages          []map[string]any
	Model             string
	Temperature       float64
	TopP              float64
	ReasoningEffort   string
	Stream            *bool
	CustomInstruction string
	Tools             []map[string]any
	ToolChoice        any
}

type ConsoleStreamAdapter struct {
	TextBuf     []string
	ThinkingBuf []string
	Usage       map[string]any
	annotations []map[string]any
	sources     []map[string]any
	seenSrcURLs map[string]struct{}
	done        bool

	// Client function-tool calling (jiujiu532 #24). Only populated when the
	// adapter is created with allowed client function tool names.
	allowedFnNames map[string]struct{}
	fnCalls        map[string]*consoleFnCall
	fnOrder        []string
	fnIgnored      map[string]struct{}
	fnKeyByOutIdx  map[string]string
}

type consoleFnCall struct {
	ID        string
	CallID    string
	Name      string
	Arguments string
	Status    string
}

func BuildConsolePayload(options ConsolePayloadOptions) map[string]any {
	temperature := options.Temperature
	if temperature == 0 {
		temperature = 0.7
	}
	topP := options.TopP
	if topP == 0 {
		topP = 0.95
	}
	stream := true
	if options.Stream != nil {
		stream = *options.Stream
	}
	inputItems := make([]map[string]any, 0, len(options.Messages)+1)
	// features.custom_instruction (admin-configured global system prompt) is
	// prepended as a system input item so every console request mirrors the
	// grok.com path's customPersonality injection. Per-request system messages
	// follow and may refine it. Mirrors cloudriver8 build_console_payload.
	if custom := strings.TrimSpace(options.CustomInstruction); custom != "" {
		inputItems = append(inputItems, map[string]any{
			"role":    "system",
			"content": []map[string]any{{"type": "input_text", "text": custom}},
		})
	}
	for _, message := range options.Messages {
		role := stringFromAny(message["role"])
		// role=tool → function_call_output item.
		if role == "tool" {
			if out := consoleToolMessageOutput(message); out != nil {
				inputItems = append(inputItems, out)
			}
			continue
		}
		toolCalls, _ := message["tool_calls"].([]any)
		// assistant message carrying only tool_calls (no textual content): emit
		// the function_call items without an empty message block.
		if !(role == "assistant" && len(toolCalls) > 0 && isEmptyContent(message["content"])) {
			if item := consoleInputItem(message); item != nil {
				inputItems = append(inputItems, item)
			}
		}
		if role == "assistant" && len(toolCalls) > 0 {
			inputItems = append(inputItems, consoleAssistantToolCalls(toolCalls)...)
		}
	}
	effort := consoleModelFixedEffort[options.Model]
	if effort == "" {
		effort = consoleEffortMap[options.ReasoningEffort]
		if effort == "" {
			effort = "medium"
		}
	}
	consoleModel := options.Model
	if mapped := ConsoleModels[options.Model]; mapped != "" {
		consoleModel = mapped
	}
	maxTokens := consoleModelMaxOutputTokens[consoleModel]
	if maxTokens == 0 {
		maxTokens = 1000000
	}
	payload := map[string]any{
		"model":             consoleModel,
		"input":             inputItems,
		"max_output_tokens": maxTokens,
		"temperature":       temperature,
		"top_p":             topP,
		"store":             false,
		"include":           []any{"reasoning.encrypted_content"},
		"stream":            stream,
	}
	if _, ok := consoleModelsWithReasoning[consoleModel]; ok {
		payload["reasoning"] = map[string]any{"effort": effort}
	}
	defaultTools := defaultConsoleTools(consoleModel)
	userTools := toConsoleTools(options.Tools)
	mergedTools := mergeConsoleTools(defaultTools, userTools)
	if len(mergedTools) > 0 {
		payload["tools"] = mergedTools
		choice := toConsoleToolChoice(options.ToolChoice)
		if choice == nil {
			choice = "auto"
		}
		payload["tool_choice"] = choice
	}
	return payload
}

// defaultConsoleTools returns console-native tools that must remain available to
// Grok itself for models that support server-side search.
func defaultConsoleTools(consoleModel string) []map[string]any {
	if _, ok := consoleModelsWithSearchTools[consoleModel]; !ok {
		return nil
	}
	return []map[string]any{
		{"type": "web_search", "enable_image_understanding": true},
		{"type": "x_search", "enable_video_understanding": true},
	}
}

func NewConsoleStreamAdapter() *ConsoleStreamAdapter {
	return NewConsoleStreamAdapterWithTools(nil)
}

// NewConsoleStreamAdapterWithTools creates an adapter that also parses client
// function-tool calls. functionToolNames are the client-declared function tool
// names (console builtins are filtered out). Mirrors jiujiu532 #24.
func NewConsoleStreamAdapterWithTools(functionToolNames map[string]struct{}) *ConsoleStreamAdapter {
	allowed := map[string]struct{}{}
	for name := range functionToolNames {
		n := strings.TrimSpace(name)
		if n != "" && !IsConsoleBuiltinTool(n) {
			allowed[n] = struct{}{}
		}
	}
	return &ConsoleStreamAdapter{
		seenSrcURLs:    map[string]struct{}{},
		allowedFnNames: allowed,
		fnCalls:        map[string]*consoleFnCall{},
		fnIgnored:      map[string]struct{}{},
		fnKeyByOutIdx:  map[string]string{},
	}
}

func (a *ConsoleStreamAdapter) Feed(eventType, data string) ([]string, error) {
	if a.done {
		return nil, nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		return nil, nil
	}
	switch eventType {
	case "response.output_text.delta":
		delta := stringFromAny(obj["delta"])
		if delta != "" {
			a.TextBuf = append(a.TextBuf, delta)
			return []string{delta}, nil
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_summary.delta":
		delta := stringFromAny(obj["delta"])
		if delta != "" {
			a.ThinkingBuf = append(a.ThinkingBuf, delta)
		}
	case "response.output_item.done":
		a.collectWebSearchSources(obj)
		if item, ok := obj["item"].(map[string]any); ok && stringFromAny(item["type"]) == "function_call" {
			a.upsertFunctionCall(item, obj, true)
		}
	case "response.output_item.added":
		if item, ok := obj["item"].(map[string]any); ok && stringFromAny(item["type"]) == "function_call" {
			a.upsertFunctionCall(item, obj, false)
		}
	case "response.function_call_arguments.delta":
		key := a.functionKey(obj)
		if a.shouldIgnoreFunctionEvent(key, obj) {
			return nil, nil
		}
		if delta := stringFromAny(obj["delta"]); key != "" && delta != "" {
			info := a.ensureFunctionCall(key, obj)
			info.Arguments += delta
		}
	case "response.function_call_arguments.done":
		key := a.functionKey(obj)
		if a.shouldIgnoreFunctionEvent(key, obj) {
			return nil, nil
		}
		if args, ok := obj["arguments"].(string); ok && key != "" {
			info := a.ensureFunctionCall(key, obj)
			info.Arguments = args
		}
	case "response.output_text.annotation.added":
		a.collectAnnotation(obj)
	case "response.completed":
		if resp, ok := obj["response"].(map[string]any); ok {
			if usage, ok := resp["usage"].(map[string]any); ok {
				a.Usage = usage
			}
			if output, ok := resp["output"].([]any); ok {
				for _, raw := range output {
					if item, ok := raw.(map[string]any); ok && stringFromAny(item["type"]) == "function_call" {
						a.upsertFunctionCall(item, map[string]any{}, true)
					}
				}
			}
		}
		a.done = true
	case "error":
		message := stringFromAny(obj["message"])
		if message == "" {
			message = fmt.Sprint(obj)
		}
		return nil, platform.NewUpstreamError("Console API error: "+message, 502, "")
	}
	return nil, nil
}

// collectWebSearchSources harvests sources from a completed web_search_call
// item. Mirrors cloudriver8 ConsoleStreamAdapter.feed_data output_item.done.
func (a *ConsoleStreamAdapter) collectWebSearchSources(obj map[string]any) {
	item, ok := obj["item"].(map[string]any)
	if !ok || stringFromAny(item["type"]) != "web_search_call" {
		return
	}
	action, ok := item["action"].(map[string]any)
	if !ok {
		return
	}
	if rawSources, ok := action["sources"].([]any); ok {
		for _, raw := range rawSources {
			src, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			a.addSource(stringFromAny(src["url"]), stringFromAny(src["title"]))
		}
	}
	if stringFromAny(action["type"]) == "open_page" {
		a.addSource(stringFromAny(action["url"]), "")
	}
}

// collectAnnotation records a url_citation annotation and back-fills it into
// search_sources (multi-agent models only emit URLs as annotations).
func (a *ConsoleStreamAdapter) collectAnnotation(obj map[string]any) {
	ann, ok := obj["annotation"].(map[string]any)
	if !ok {
		return
	}
	if t := stringFromAny(ann["type"]); t != "" && t != "url_citation" {
		return
	}
	url := stringFromAny(ann["url"])
	if url == "" {
		return
	}
	title := stringFromAny(ann["title"])
	if title == url {
		title = "" // multi-agent duplicates URL into title; clean it
	}
	a.annotations = append(a.annotations, map[string]any{
		"url":         url,
		"title":       title,
		"start_index": consoleIntFromAny(ann["start_index"]),
		"end_index":   consoleIntFromAny(ann["end_index"]),
	})
	a.addSource(url, title)
}

func consoleIntFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func (a *ConsoleStreamAdapter) addSource(url, title string) {
	if url == "" {
		return
	}
	if _, seen := a.seenSrcURLs[url]; seen {
		return
	}
	a.seenSrcURLs[url] = struct{}{}
	a.sources = append(a.sources, map[string]any{"url": url, "title": title})
}

func (a *ConsoleStreamAdapter) FullText() string {
	return strings.Join(a.TextBuf, "")
}

// ThinkingText returns the accumulated reasoning summary text.
func (a *ConsoleStreamAdapter) ThinkingText() string {
	return strings.Join(a.ThinkingBuf, "")
}

// AnnotationsList returns a copy of the collected URL citation annotations.
func (a *ConsoleStreamAdapter) AnnotationsList() []map[string]any {
	if len(a.annotations) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(a.annotations))
	for _, item := range a.annotations {
		clone := make(map[string]any, len(item))
		for k, v := range item {
			clone[k] = v
		}
		out = append(out, clone)
	}
	return out
}

// SearchSourcesList returns a copy of the collected search sources.
func (a *ConsoleStreamAdapter) SearchSourcesList() []map[string]any {
	if len(a.sources) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(a.sources))
	for _, item := range a.sources {
		clone := make(map[string]any, len(item))
		for k, v := range item {
			clone[k] = v
		}
		out = append(out, clone)
	}
	return out
}

// ReferencesSuffix renders the collected sources as a "## Sources" markdown
// block, gated by features.show_search_sources. Mirrors the grok.com app-chat
// StreamAdapter.ReferencesSuffix so text-only clients see identical formatting.
func (a *ConsoleStreamAdapter) ReferencesSuffix(showSearchSources bool) string {
	if len(a.sources) == 0 || !showSearchSources {
		return ""
	}
	lines := []string{"\n\n## Sources", "[gork-sources]: #"}
	for _, item := range a.sources {
		url := stringFromAny(item["url"])
		if url == "" {
			continue
		}
		title := stringFromAny(item["title"])
		if title == "" {
			title = url
		}
		title = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(title, `\`, `\\`), "[", `\[`), "]", `\]`)
		lines = append(lines, fmt.Sprintf("- [%s](%s)", title, url))
	}
	if len(lines) == 2 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func ClassifyConsoleLine(line string) (string, string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "skip", ""
	}
	if strings.HasPrefix(line, "event:") {
		return "event", strings.TrimSpace(line[6:])
	}
	if strings.HasPrefix(line, "data:") {
		data := strings.TrimSpace(line[5:])
		if data == "[DONE]" {
			return "done", ""
		}
		return "data", data
	}
	return "skip", ""
}

func ConsoleSuccessFeedback() controlproxy.ProxyFeedback {
	feedback := controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackSuccess)
	status := 200
	feedback.StatusCode = &status
	return feedback
}

func ConsoleTransportErrorFeedback() controlproxy.ProxyFeedback {
	return controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackTransportError)
}

func ConsoleStatusFeedback(status int) controlproxy.ProxyFeedback {
	kind := controlproxy.ProxyFeedbackForbidden
	if status == 403 {
		kind = controlproxy.ProxyFeedbackChallenge
	} else if status == 429 {
		kind = controlproxy.ProxyFeedbackRateLimited
	} else if status >= 500 {
		kind = controlproxy.ProxyFeedbackUpstream5xx
	}
	feedback := controlproxy.NewProxyFeedback(kind)
	feedback.StatusCode = &status
	return feedback
}

func consoleInputItem(message map[string]any) map[string]any {
	role := stringFromAny(message["role"])
	apiRole := "user"
	if role == "system" || role == "developer" {
		apiRole = "system"
	} else if role == "assistant" {
		apiRole = "assistant"
	}
	blocks := consoleContentBlocks(message["content"])
	if len(blocks) == 0 {
		return nil
	}
	return map[string]any{"role": apiRole, "content": blocks}
}

func consoleContentBlocks(content any) []map[string]any {
	switch typed := content.(type) {
	case string:
		return []map[string]any{{"type": "input_text", "text": typed}}
	case []any:
		blocks := []map[string]any{}
		for _, raw := range typed {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch stringFromAny(block["type"]) {
			case "text":
				blocks = append(blocks, map[string]any{"type": "input_text", "text": stringFromAny(block["text"])})
			case "image_url":
				if imageURL, ok := block["image_url"].(map[string]any); ok {
					if url := stringFromAny(imageURL["url"]); url != "" {
						blocks = append(blocks, map[string]any{"type": "input_image", "image_url": url})
					}
				}
			default:
				text := stringFromAny(block["text"])
				if text == "" {
					text = consolePythonString(block)
				}
				blocks = append(blocks, map[string]any{"type": "input_text", "text": text})
			}
		}
		return blocks
	default:
		return []map[string]any{{"type": "input_text", "text": consolePythonString(content)}}
	}
}

func consolePythonString(value any) string {
	switch v := value.(type) {
	case nil:
		return "None"
	case string:
		return v
	case bool:
		if v {
			return "True"
		}
		return "False"
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("'%s': %s", key, consolePythonLiteral(v[key])))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprint(v)
	}
}

func consolePythonLiteral(value any) string {
	switch v := value.(type) {
	case string:
		escaped := strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `'`, `\'`)
		return "'" + escaped + "'"
	case nil:
		return "None"
	case bool:
		if v {
			return "True"
		}
		return "False"
	case map[string]any:
		return consolePythonString(v)
	default:
		return fmt.Sprint(v)
	}
}
