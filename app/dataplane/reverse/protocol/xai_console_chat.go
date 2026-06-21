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
	Messages        []map[string]any
	Model           string
	Temperature     float64
	TopP            float64
	ReasoningEffort string
	Stream          *bool
	Tools           []map[string]any
	ToolChoice      any
}

type ConsoleStreamAdapter struct {
	TextBuf           []string
	Usage             map[string]any
	FunctionToolNames map[string]struct{}
	FunctionCalls     []ParsedToolCall
	functionByKey     map[string]*ParsedToolCall
	functionOrder     []string
	done              bool
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
	inputItems := make([]map[string]any, 0, len(options.Messages))
	for _, message := range options.Messages {
		inputItems = append(inputItems, consoleInputItems(message)...)
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
	if _, ok := consoleModelsWithSearchTools[consoleModel]; ok {
		payload["tools"] = []map[string]any{
			{"type": "web_search", "enable_image_understanding": true},
			{"type": "x_search", "enable_video_understanding": true},
		}
		payload["tool_choice"] = "auto"
	}
	if len(options.Tools) > 0 {
		payload["tools"] = mergeConsoleTools(asConsoleTools(payload["tools"]), consoleToolPayloads(options.Tools))
		payload["tool_choice"] = consoleToolChoice(options.ToolChoice)
	}
	return payload
}

func NewConsoleStreamAdapter(functionToolNames ...[]string) *ConsoleStreamAdapter {
	names := map[string]struct{}{}
	if len(functionToolNames) > 0 {
		for _, name := range functionToolNames[0] {
			name = strings.TrimSpace(name)
			if name != "" {
				names[name] = struct{}{}
			}
		}
	}
	return &ConsoleStreamAdapter{FunctionToolNames: names, functionByKey: map[string]*ParsedToolCall{}}
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
	case "response.output_item.added", "response.output_item.done":
		a.feedFunctionItem(obj)
	case "response.function_call_arguments.delta", "response.function_call_arguments.done":
		a.feedFunctionArguments(obj)
	case "response.completed":
		if resp, ok := obj["response"].(map[string]any); ok {
			if usage, ok := resp["usage"].(map[string]any); ok {
				a.Usage = usage
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

func (a *ConsoleStreamAdapter) FullText() string {
	return strings.Join(a.TextBuf, "")
}

func (a *ConsoleStreamAdapter) HasFunctionTools() bool {
	return len(a.FunctionToolNames) > 0
}

func (a *ConsoleStreamAdapter) feedFunctionItem(obj map[string]any) {
	if !a.HasFunctionTools() {
		return
	}
	item, ok := obj["item"].(map[string]any)
	if !ok || stringFromAny(item["type"]) != "function_call" {
		return
	}
	name := strings.TrimSpace(stringFromAny(item["name"]))
	if !a.allowsFunctionName(name) {
		return
	}
	key := a.functionKey(obj, item)
	if key == "" {
		return
	}
	call := a.ensureFunctionCall(key)
	call.Name = name
	if call.CallID == "" {
		call.CallID = stringFromAny(item["call_id"])
	}
	if call.CallID == "" {
		call.CallID = key
	}
	if args := stringFromAny(item["arguments"]); args != "" {
		call.Arguments = args
	}
	if call.Arguments == "" {
		call.Arguments = "{}"
	}
	a.refreshFunctionCalls()
}

func (a *ConsoleStreamAdapter) feedFunctionArguments(obj map[string]any) {
	if !a.HasFunctionTools() {
		return
	}
	key := a.functionKey(obj, nil)
	if key == "" {
		return
	}
	call := a.ensureFunctionCall(key)
	if delta := stringFromAny(obj["delta"]); delta != "" {
		if call.Arguments == "{}" {
			call.Arguments = ""
		}
		call.Arguments += delta
	}
	if args := stringFromAny(obj["arguments"]); args != "" {
		call.Arguments = args
	}
	if call.CallID == "" {
		call.CallID = key
	}
	if call.Arguments == "" {
		call.Arguments = "{}"
	}
	a.refreshFunctionCalls()
}

func (a *ConsoleStreamAdapter) functionKey(obj map[string]any, item map[string]any) string {
	if item != nil {
		for _, key := range []string{"call_id", "id"} {
			if value := stringFromAny(item[key]); value != "" {
				return value
			}
		}
	}
	for _, key := range []string{"call_id", "item_id"} {
		if value := stringFromAny(obj[key]); value != "" {
			return value
		}
	}
	if index := stringFromAny(obj["output_index"]); index != "" {
		return "output:" + index
	}
	return ""
}

func (a *ConsoleStreamAdapter) ensureFunctionCall(key string) *ParsedToolCall {
	if call, ok := a.functionByKey[key]; ok {
		return call
	}
	call := &ParsedToolCall{CallID: key, Arguments: "{}"}
	a.functionByKey[key] = call
	a.functionOrder = append(a.functionOrder, key)
	return call
}

func (a *ConsoleStreamAdapter) allowsFunctionName(name string) bool {
	if name == "" || isConsoleInternalToolName(name) {
		return false
	}
	_, ok := a.FunctionToolNames[name]
	return ok
}

func (a *ConsoleStreamAdapter) refreshFunctionCalls() {
	calls := make([]ParsedToolCall, 0, len(a.functionOrder))
	for _, key := range a.functionOrder {
		call := a.functionByKey[key]
		if call == nil || call.Name == "" {
			continue
		}
		calls = append(calls, *call)
	}
	a.FunctionCalls = calls
}

func ClientFunctionToolNames(tools []map[string]any) []string {
	names := []string{}
	for _, tool := range tools {
		if stringFromAny(tool["type"]) != "function" {
			continue
		}
		src := tool
		if fn, ok := tool["function"].(map[string]any); ok {
			src = fn
		}
		name := strings.TrimSpace(stringFromAny(src["name"]))
		if name != "" && !isConsoleInternalToolName(name) {
			names = append(names, name)
		}
	}
	return names
}

var consoleInternalToolNames = map[string]struct{}{
	"web_search":                  {},
	"web_search_with_snippets":    {},
	"browse_page":                 {},
	"open_page":                   {},
	"open_page_with_find":         {},
	"x_search":                    {},
	"x_keyword_search":            {},
	"x_semantic_search":           {},
	"x_user_search":               {},
	"x_thread_fetch":              {},
	"image_search":                {},
	"search_images":               {},
	"view_image":                  {},
	"view_x_video":                {},
	"code_execution":              {},
	"file_search":                 {},
	"chatroom_send":               {},
	"generate_image":              {},
	"create_image":                {},
	"edit_image":                  {},
	"computer_use_preview":        {},
	"server_side_tool_web_search": {},
}

func isConsoleInternalToolName(name string) bool {
	_, ok := consoleInternalToolNames[strings.TrimSpace(name)]
	return ok
}

func consoleToolPayloads(tools []map[string]any) []map[string]any {
	payloads := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if stringFromAny(tool["type"]) != "function" {
			payloads = append(payloads, cloneAnyMap(tool))
			continue
		}
		src := tool
		if fn, ok := tool["function"].(map[string]any); ok {
			src = fn
		}
		name := strings.TrimSpace(stringFromAny(src["name"]))
		if name == "" {
			continue
		}
		payload := map[string]any{"type": "function", "name": name}
		if description := stringFromAny(src["description"]); description != "" {
			payload["description"] = description
		}
		if parameters, ok := src["parameters"]; ok {
			payload["parameters"] = parameters
		}
		payloads = append(payloads, payload)
	}
	return payloads
}

func asConsoleTools(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

func mergeConsoleTools(defaults []map[string]any, userTools []map[string]any) []map[string]any {
	merged := make([]map[string]any, 0, len(defaults)+len(userTools))
	overrides := map[string]struct{}{}
	for _, tool := range userTools {
		if key := consoleToolKey(tool); key != "" {
			overrides[key] = struct{}{}
		}
	}
	for _, tool := range defaults {
		if _, ok := overrides[consoleToolKey(tool)]; !ok {
			merged = append(merged, tool)
		}
	}
	return append(merged, userTools...)
}

func consoleToolKey(tool map[string]any) string {
	toolType := stringFromAny(tool["type"])
	if toolType == "function" {
		return "function:" + stringFromAny(tool["name"])
	}
	return toolType
}

func consoleToolChoice(choice any) any {
	if choice == nil {
		return "auto"
	}
	return choice
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

func consoleInputItems(message map[string]any) []map[string]any {
	role := stringFromAny(message["role"])
	if role == "tool" && stringFromAny(message["tool_call_id"]) != "" {
		return []map[string]any{{
			"type":    "function_call_output",
			"call_id": stringFromAny(message["tool_call_id"]),
			"output":  consoleContentText(message["content"]),
		}}
	}
	apiRole := "user"
	if role == "system" || role == "developer" {
		apiRole = "system"
	} else if role == "assistant" {
		apiRole = "assistant"
	}
	items := []map[string]any{}
	blocks := consoleContentBlocks(message["content"])
	if len(blocks) > 0 {
		items = append(items, map[string]any{"role": apiRole, "content": blocks})
	}
	if role == "assistant" {
		items = append(items, assistantToolCallsToConsole(message["tool_calls"])...)
	}
	return items
}

func consoleInputItem(message map[string]any) map[string]any {
	items := consoleInputItems(message)
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

func assistantToolCallsToConsole(raw any) []map[string]any {
	calls := toolCallMaps(raw)
	items := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		function, _ := call["function"].(map[string]any)
		name := stringFromAny(function["name"])
		if name == "" {
			name = stringFromAny(call["name"])
		}
		if name == "" {
			continue
		}
		args := stringFromAny(function["arguments"])
		if args == "" {
			args = stringFromAny(call["arguments"])
		}
		if args == "" {
			args = "{}"
		}
		items = append(items, map[string]any{
			"type":      "function_call",
			"call_id":   stringFromAny(call["id"]),
			"name":      name,
			"arguments": args,
			"status":    "completed",
		})
	}
	return items
}

func toolCallMaps(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

func consoleContentText(content any) string {
	blocks := consoleContentBlocks(content)
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if text := stringFromAny(block["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
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
