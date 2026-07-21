package build

import (
	"encoding/json"
	"strings"
	"time"
)

// toolCallAccum 聚合上游 function_call 流事件，供组装 OpenAI tool_calls chunk。
type toolCallAccum struct {
	Index     int
	ID        string
	Name      string
	Args      string
	ArgDeltas []string
}

type toolCallStreamState struct {
	byIndex map[int]*toolCallAccum
	order   []int
}

func newToolCallStreamState() *toolCallStreamState {
	return &toolCallStreamState{byIndex: map[int]*toolCallAccum{}}
}

func (s *toolCallStreamState) empty() bool {
	return s == nil || len(s.order) == 0
}

// snapshot 返回已聚合的 tool_calls（OpenAI function 形态）。
func (s *toolCallStreamState) snapshot() []map[string]any {
	if s == nil || len(s.order) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(s.order))
	for _, idx := range s.order {
		acc := s.byIndex[idx]
		if acc == nil {
			continue
		}
		args := acc.Args
		if args == "" && len(acc.ArgDeltas) > 0 {
			args = strings.Join(acc.ArgDeltas, "")
		}
		if args == "" {
			args = "{}"
		}
		id := acc.ID
		if id == "" {
			id = "call_" + firstNonEmpty(acc.Name, "tool")
		}
		out = append(out, map[string]any{
			"id":        id,
			"name":      acc.Name,
			"arguments": args,
		})
	}
	return out
}

func (s *toolCallStreamState) ensure(index int) *toolCallAccum {
	if s.byIndex == nil {
		s.byIndex = map[int]*toolCallAccum{}
	}
	if acc, ok := s.byIndex[index]; ok {
		return acc
	}
	acc := &toolCallAccum{Index: index}
	s.byIndex[index] = acc
	s.order = append(s.order, index)
	return acc
}

// applyToolStreamEvent 解析 Responses 流中的 function_call 相关事件。
// 返回 true 表示本事件属于 tool 流（调用方勿再当普通文本）。
func applyToolStreamEvent(state *toolCallStreamState, payload map[string]any) bool {
	if state == nil || payload == nil {
		return false
	}
	typ, _ := payload["type"].(string)
	switch typ {
	case "response.output_item.added", "response.output_item.done":
		return applyToolOutputItem(state, payload)
	case "response.function_call_arguments.delta":
		return applyToolArgsDelta(state, payload)
	case "response.function_call_arguments.done":
		return applyToolArgsDone(state, payload)
	default:
		return false
	}
}

func applyToolOutputItem(state *toolCallStreamState, payload map[string]any) bool {
	item, _ := payload["item"].(map[string]any)
	if item == nil {
		return false
	}
	itemType := strings.ToLower(strings.TrimSpace(stringField(item, "type")))
	if itemType != "function_call" && itemType != "custom_tool_call" {
		return false
	}
	idx := intField(payload, "output_index")
	acc := state.ensure(idx)
	if id := firstNonEmpty(stringField(item, "call_id"), stringField(item, "id")); id != "" {
		acc.ID = id
	}
	if name := strings.TrimSpace(stringField(item, "name")); name != "" {
		acc.Name = name
	}
	if args := toolArgumentsString(item); args != "" {
		acc.Args = args
	}
	if acc.ID == "" {
		acc.ID = "call_" + firstNonEmpty(acc.Name, "tool")
	}
	return true
}

func applyToolArgsDelta(state *toolCallStreamState, payload map[string]any) bool {
	idx := intField(payload, "output_index")
	acc := state.ensure(idx)
	delta := stringField(payload, "delta")
	if delta == "" {
		delta = stringField(payload, "arguments")
	}
	if delta == "" {
		return true
	}
	acc.ArgDeltas = append(acc.ArgDeltas, delta)
	acc.Args += delta
	if acc.ID == "" {
		if id := firstNonEmpty(stringField(payload, "item_id"), stringField(payload, "call_id")); id != "" {
			acc.ID = id
		}
	}
	return true
}

func applyToolArgsDone(state *toolCallStreamState, payload map[string]any) bool {
	idx := intField(payload, "output_index")
	acc := state.ensure(idx)
	if args := stringField(payload, "arguments"); args != "" {
		acc.Args = args
	}
	if acc.ID == "" {
		if id := firstNonEmpty(stringField(payload, "item_id"), stringField(payload, "call_id")); id != "" {
			acc.ID = id
		}
	}
	return true
}

func toolArgumentsString(item map[string]any) string {
	if s := strings.TrimSpace(stringField(item, "arguments")); s != "" {
		return s
	}
	if m, ok := item["arguments"].(map[string]any); ok {
		b, err := json.Marshal(m)
		if err == nil {
			return string(b)
		}
	}
	return ""
}

func intField(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch typed := v.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	default:
		return 0
	}
}

// framesFromTextAndTools 组装文本 delta 与 tool_calls 的 OpenAI chat 流帧。
// 有 tool 时 finish_reason=tool_calls；仅文本时 finish_reason=stop。
func framesFromTextAndTools(model, responseID string, textDeltas []string, tools *toolCallStreamState) []string {
	now := time.Now().Unix()
	frames := make([]string, 0, len(textDeltas)+8)
	first := true
	for _, delta := range textDeltas {
		if delta == "" {
			continue
		}
		deltaMap := map[string]any{"content": delta}
		if first {
			deltaMap["role"] = "assistant"
			first = false
		}
		frames = append(frames, mustDataFrame(map[string]any{
			"id": responseID, "object": "chat.completion.chunk", "created": now, "model": model,
			"choices": []any{map[string]any{"index": 0, "delta": deltaMap}},
		}))
	}
	if tools != nil && !tools.empty() {
		for _, idx := range tools.order {
			acc := tools.byIndex[idx]
			if acc == nil {
				continue
			}
			id := firstNonEmpty(acc.ID, "call_"+firstNonEmpty(acc.Name, "tool"))
			name := acc.Name
			// 首帧：id + name + 空 arguments（OpenAI 流式惯例）
			openDelta := map[string]any{
				"tool_calls": []any{map[string]any{
					"index": acc.Index,
					"id":    id,
					"type":  "function",
					"function": map[string]any{
						"name":      name,
						"arguments": "",
					},
				}},
			}
			if first {
				openDelta["role"] = "assistant"
				first = false
			}
			frames = append(frames, mustDataFrame(map[string]any{
				"id": responseID, "object": "chat.completion.chunk", "created": now, "model": model,
				"choices": []any{map[string]any{"index": 0, "delta": openDelta}},
			}))
			if len(acc.ArgDeltas) > 0 {
				for _, part := range acc.ArgDeltas {
					if part == "" {
						continue
					}
					frames = append(frames, mustDataFrame(map[string]any{
						"id": responseID, "object": "chat.completion.chunk", "created": now, "model": model,
						"choices": []any{map[string]any{
							"index": 0,
							"delta": map[string]any{
								"tool_calls": []any{map[string]any{
									"index": acc.Index,
									"function": map[string]any{"arguments": part},
								}},
							},
						}},
					}))
				}
			} else if acc.Args != "" {
				frames = append(frames, mustDataFrame(map[string]any{
					"id": responseID, "object": "chat.completion.chunk", "created": now, "model": model,
					"choices": []any{map[string]any{
						"index": 0,
						"delta": map[string]any{
							"tool_calls": []any{map[string]any{
								"index": acc.Index,
								"function": map[string]any{"arguments": acc.Args},
							}},
						},
					}},
				}))
			}
		}
		frames = append(frames, mustDataFrame(map[string]any{
			"id": responseID, "object": "chat.completion.chunk", "created": now, "model": model,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls",
			}},
		}), "data: [DONE]\n\n")
		return frames
	}
	frames = append(frames, mustDataFrame(map[string]any{
		"id": responseID, "object": "chat.completion.chunk", "created": now, "model": model,
		"choices": []any{map[string]any{
			"index": 0, "delta": map[string]any{}, "finish_reason": "stop",
		}},
	}), "data: [DONE]\n\n")
	return frames
}

// framesFromCompletedToolCalls 将整包 JSON 中的 function_call 输出为流式 tool_calls 帧。
func framesFromCompletedToolCalls(model, responseID string, calls []map[string]any) []string {
	if len(calls) == 0 {
		return nil
	}
	state := newToolCallStreamState()
	for i, call := range calls {
		acc := state.ensure(i)
		acc.ID = stringField(call, "id")
		if fn, ok := call["function"].(map[string]any); ok {
			acc.Name = stringField(fn, "name")
			acc.Args = stringField(fn, "arguments")
		}
	}
	return framesFromTextAndTools(model, responseID, nil, state)
}
