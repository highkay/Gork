package build

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ChatStreamFramesFromResponsesSSE 将 Build /responses SSE（或整包 JSON）转为
// OpenAI chat.completion.chunk 的 data 帧列表（含末尾 data: [DONE]）。
// 对齐 chenyme #626：上游 error/failed 之后的事件忽略，不发成功 stop。
func ChatStreamFramesFromResponsesSSE(model, responseID string, r io.Reader) ([]string, error) {
	if responseID == "" {
		responseID = "chatcmpl-build"
	}
	raw, err := io.ReadAll(io.LimitReader(r, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read build stream: %w", err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("empty build stream body")
	}
	// 非 SSE：整包 JSON 一次性输出（文本和/或 tool_calls）
	if !strings.Contains(trimmed, "data:") && strings.HasPrefix(trimmed, "{") {
		return framesFromPlainResponsesJSON(model, responseID, raw)
	}
	parsed, err := collectSSEStream(trimmed)
	if err != nil {
		return nil, err
	}
	if parsed.failed {
		return framesFromStreamError(model, responseID, parsed.errMsg), nil
	}
	if !parsed.tools.empty() || len(parsed.deltas) > 0 {
		return framesFromTextAndTools(model, responseID, parsed.deltas, parsed.tools), nil
	}
	if text, ok := extractTextFromSSEJSONBlobs(trimmed); ok {
		return framesFromTextAndTools(model, responseID, []string{text}, nil), nil
	}
	if calls := extractToolCallsFromSSEBlobs(trimmed); len(calls) > 0 {
		return framesFromCompletedToolCalls(model, responseID, calls), nil
	}
	return nil, fmt.Errorf("build stream 无可用文本或 tool_calls")
}

type sseStreamParse struct {
	deltas []string
	tools  *toolCallStreamState
	failed bool
	errMsg string
}

func collectSSEStream(payload string) (sseStreamParse, error) {
	out := sseStreamParse{tools: newToolCallStreamState()}
	scanner := bufio.NewScanner(strings.NewReader(payload))
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2<<20)
	var dataLines []string
	finished := false
	flush := func() {
		if finished || len(dataLines) == 0 {
			dataLines = nil
			return
		}
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = nil
		if data == "" || data == "[DONE]" {
			return
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(data), &payload); err == nil {
			if applyToolStreamEvent(out.tools, payload) {
				return
			}
		}
		kind, pieces, msg := classifyEventData(data)
		switch kind {
		case eventKindError:
			out.failed = true
			out.errMsg = msg
			finished = true
		case eventKindDelta:
			out.deltas = append(out.deltas, pieces...)
		case eventKindTerminal:
			finished = true
		}
	}
	for scanner.Scan() {
		if finished {
			// 错误/终态后忽略后续行（#626）
			continue
		}
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if !finished {
		flush()
	}
	if err := scanner.Err(); err != nil {
		return out, err
	}
	return out, nil
}

const (
	eventKindIgnore   = 0
	eventKindDelta    = 1
	eventKindError    = 2
	eventKindTerminal = 3
)

func classifyEventData(data string) (kind int, deltas []string, errMsg string) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return eventKindIgnore, nil, ""
	}
	typ, _ := payload["type"].(string)
	switch typ {
	case "response.failed", "error", "response.error":
		msg := extractStreamErrorMessage(payload)
		if msg == "" {
			msg = "upstream stream failed"
		}
		return eventKindError, nil, msg
	case "response.output_text.delta", "response.text.delta", "response.content_part.delta":
		if s := stringField(payload, "delta"); s != "" {
			return eventKindDelta, []string{s}, ""
		}
		if s := stringField(payload, "text"); s != "" {
			return eventKindDelta, []string{s}, ""
		}
		return eventKindIgnore, nil, ""
	case "response.completed", "response.done", "response.output_text.done":
		return eventKindTerminal, nil, ""
	}
	if s := stringField(payload, "delta"); s != "" {
		return eventKindDelta, []string{s}, ""
	}
	if s, err := extractOutputText([]byte(data)); err == nil && s != "" {
		return eventKindDelta, []string{s}, ""
	}
	return eventKindIgnore, nil, ""
}

func extractStreamErrorMessage(payload map[string]any) string {
	if s := stringField(payload, "message"); s != "" {
		return s
	}
	if errObj, ok := payload["error"].(map[string]any); ok {
		if s := stringField(errObj, "message"); s != "" {
			return s
		}
	}
	if resp, ok := payload["response"].(map[string]any); ok {
		if errObj, ok := resp["error"].(map[string]any); ok {
			if s := stringField(errObj, "message"); s != "" {
				return s
			}
		}
	}
	return ""
}

func extractTextFromSSEJSONBlobs(payload string) (string, bool) {
	var parts []string
	for _, line := range strings.Split(payload, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		if s, err := extractOutputText([]byte(data)); err == nil && s != "" {
			parts = append(parts, s)
		}
	}
	joined := strings.TrimSpace(strings.Join(parts, ""))
	return joined, joined != ""
}

func framesFromPlainResponsesJSON(model, responseID string, raw []byte) ([]string, error) {
	calls := ExtractToolCallsFromResponses(raw)
	text, textErr := extractOutputText(raw)
	if len(calls) > 0 {
		var deltas []string
		if textErr == nil && strings.TrimSpace(text) != "" {
			deltas = []string{text}
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
		return framesFromTextAndTools(model, responseID, deltas, state), nil
	}
	if textErr != nil {
		return nil, textErr
	}
	return framesFromTextAndTools(model, responseID, []string{text}, nil), nil
}

func extractToolCallsFromSSEBlobs(payload string) []map[string]any {
	var all []map[string]any
	for _, line := range strings.Split(payload, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		if calls := ExtractToolCallsFromResponses([]byte(data)); len(calls) > 0 {
			all = append(all, calls...)
		}
	}
	return all
}

func framesFromStreamError(model, responseID, msg string) []string {
	now := time.Now().Unix()
	chunk := map[string]any{
		"id":      responseID,
		"object":  "chat.completion.chunk",
		"created": now,
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": "error",
		}},
		"error": map[string]any{"message": msg},
	}
	return []string{mustDataFrame(chunk), "data: [DONE]\n\n"}
}

func mustDataFrame(payload any) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return "data: {}\n\n"
	}
	return "data: " + string(data) + "\n\n"
}

func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return typed
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
