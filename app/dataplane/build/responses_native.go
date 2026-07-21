package build

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// NormalizeResponsesJSON 将上游 /responses JSON 规范为 OpenAI Responses 对象（公开 model / id）。
func NormalizeResponsesJSON(publicModel, responseID string, raw []byte) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse build responses json: %w", err)
	}
	if responseID == "" {
		responseID = "resp_build"
	}
	if id, _ := payload["id"].(string); strings.TrimSpace(id) == "" {
		payload["id"] = responseID
	}
	payload["object"] = "response"
	if publicModel != "" {
		payload["model"] = publicModel
	}
	if _, ok := payload["created_at"]; !ok {
		if created, ok := payload["created"].(float64); ok {
			payload["created_at"] = int64(created)
		} else {
			payload["created_at"] = time.Now().Unix()
		}
	}
	if _, ok := payload["status"]; !ok {
		payload["status"] = "completed"
	}
	// 若上游无 output，用文本兜底构造 message item。
	if _, ok := payload["output"]; !ok {
		text, _ := extractOutputText(raw)
		if text != "" {
			payload["output"] = []any{
				map[string]any{
					"type":   "message",
					"id":     "msg_build",
					"status": "completed",
					"role":   "assistant",
					"content": []any{
						map[string]any{"type": "output_text", "text": text},
					},
				},
			}
		} else {
			payload["output"] = []any{}
		}
	}
	return payload, nil
}

// ResponsesStreamFramesFromSSE 将 Build SSE 转为 OpenAI Responses SSE 帧列表。
// 文本路径：response.created → output_text.delta* → completed；错误时 response.failed。
func ResponsesStreamFramesFromSSE(publicModel, responseID string, r io.Reader) ([]string, error) {
	if responseID == "" {
		responseID = "resp_build"
	}
	raw, err := io.ReadAll(io.LimitReader(r, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read build responses stream: %w", err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("empty build responses stream")
	}

	// 非 SSE 整包 JSON
	if !strings.Contains(trimmed, "data:") && strings.HasPrefix(trimmed, "{") {
		payload, err := NormalizeResponsesJSON(publicModel, responseID, raw)
		if err != nil {
			return nil, err
		}
		return []string{
			formatResponsesSSE("response.created", map[string]any{"type": "response.created", "response": inProgressResponse(publicModel, responseID)}),
			formatResponsesSSE("response.completed", map[string]any{"type": "response.completed", "response": payload}),
		}, nil
	}

	parsed, err := collectSSEStream(trimmed)
	if err != nil {
		return nil, err
	}
	if parsed.failed {
		return []string{
			formatResponsesSSE("response.failed", map[string]any{
				"type": "response.failed",
				"response": map[string]any{
					"id": responseID, "object": "response", "model": publicModel, "status": "failed",
					"error": map[string]any{"message": parsed.errMsg},
				},
			}),
		}, nil
	}

	text := strings.Join(parsed.deltas, "")
	frames := []string{
		formatResponsesSSE("response.created", map[string]any{"type": "response.created", "response": inProgressResponse(publicModel, responseID)}),
		formatResponsesSSE("response.output_item.added", map[string]any{
			"type": "response.output_item.added", "output_index": 0,
			"item": map[string]any{"type": "message", "id": "msg_build", "role": "assistant", "status": "in_progress", "content": []any{}},
		}),
		formatResponsesSSE("response.content_part.added", map[string]any{
			"type": "response.content_part.added", "output_index": 0, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": ""},
		}),
	}
	if text != "" {
		// 分片输出，避免单帧过大
		const chunk = 256
		for i := 0; i < len(text); i += chunk {
			end := i + chunk
			if end > len(text) {
				end = len(text)
			}
			frames = append(frames, formatResponsesSSE("response.output_text.delta", map[string]any{
				"type": "response.output_text.delta", "output_index": 0, "content_index": 0,
				"delta": text[i:end],
			}))
		}
	}
	// tool_calls → function_call items
	if parsed.tools != nil && !parsed.tools.empty() {
		for i, call := range parsed.tools.snapshot() {
			frames = append(frames, formatResponsesSSE("response.output_item.added", map[string]any{
				"type": "response.output_item.added", "output_index": i + 1,
				"item": map[string]any{
					"type": "function_call", "id": stringField(call, "id"), "call_id": stringField(call, "id"),
					"name": stringField(call, "name"), "arguments": stringField(call, "arguments"),
				},
			}))
		}
	}
	completed := map[string]any{
		"id": responseID, "object": "response", "model": publicModel, "status": "completed",
		"created_at": time.Now().Unix(),
		"output":     buildCompletedOutput(text, parsed.tools),
	}
	frames = append(frames, formatResponsesSSE("response.completed", map[string]any{
		"type": "response.completed", "response": completed,
	}))
	return frames, nil
}

func inProgressResponse(model, id string) map[string]any {
	return map[string]any{
		"id": id, "object": "response", "model": model, "status": "in_progress",
		"created_at": time.Now().Unix(), "output": []any{},
	}
}

func buildCompletedOutput(text string, tools *toolCallStreamState) []any {
	out := make([]any, 0, 2)
	if text != "" {
		out = append(out, map[string]any{
			"type": "message", "id": "msg_build", "status": "completed", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": text}},
		})
	}
	if tools != nil {
		for _, call := range tools.snapshot() {
			out = append(out, map[string]any{
				"type": "function_call", "id": stringField(call, "id"), "call_id": stringField(call, "id"),
				"name": stringField(call, "name"), "arguments": stringField(call, "arguments"),
			})
		}
	}
	if len(out) == 0 {
		return []any{}
	}
	return out
}

func formatResponsesSSE(event string, data any) string {
	payload, err := json.Marshal(data)
	if err != nil {
		payload = []byte("null")
	}
	return fmt.Sprintf("event: %s\ndata: %s\n\n", event, payload)
}