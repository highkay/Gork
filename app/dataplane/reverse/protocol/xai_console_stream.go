package protocol

import (
	"encoding/json"
	"fmt"
	"strings"

	platform "github.com/dslzl/gork/app/platform"
)

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
