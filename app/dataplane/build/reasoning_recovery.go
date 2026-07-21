package build

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// 对齐 chenyme/grok2api responses_reasoning_recovery：
// 上游对 opaque reasoning / compaction blob 解码失败时，同账号内降级重试；
// 恢复路径上的 429 必须原样返回，以便网关冷却/换号。

var reasoningDecodeFailureMarkers = [][]byte{
	[]byte("could not decode the compaction blob"),
	[]byte("could not decrypt the provided encrypted_content"),
}

// ReasoningRecoveryOutcome 记录兼容性降级步骤（可写入响应头供诊断）。
type ReasoningRecoveryOutcome struct {
	EncryptedContentDowngraded bool
	SessionReset               bool
	Failed                     bool
}

func (o ReasoningRecoveryOutcome) Merge(other ReasoningRecoveryOutcome) ReasoningRecoveryOutcome {
	return ReasoningRecoveryOutcome{
		EncryptedContentDowngraded: o.EncryptedContentDowngraded || other.EncryptedContentDowngraded,
		SessionReset:               o.SessionReset || other.SessionReset,
		Failed:                     o.Failed || other.Failed,
	}
}

// CompatibilityWarnings 返回逗号分隔的兼容性警告列表。
func (o ReasoningRecoveryOutcome) CompatibilityWarnings() string {
	var parts []string
	if o.EncryptedContentDowngraded {
		parts = append(parts, "reasoning_encrypted_content_downgraded")
	}
	if o.SessionReset {
		parts = append(parts, "reasoning_session_reset")
	}
	if o.Failed {
		parts = append(parts, "reasoning_recovery_failed")
	}
	return strings.Join(parts, ",")
}

const recoveryDiagnosticBodyLimit = 64 << 10

// CreateResponseRecovering 在 CreateResponse 之后，对明确的 reasoning 解码 400 做同账号恢复。
// body 必须是可重复的完整请求体（非流式读一次的 []byte）。
func (c *APIClient) CreateResponseRecovering(ctx context.Context, meta RequestMeta, body []byte) (*http.Response, ReasoningRecoveryOutcome, error) {
	resp, err := c.CreateResponse(ctx, meta, bytes.NewReader(body))
	if err != nil {
		return nil, ReasoningRecoveryOutcome{}, err
	}
	return c.recoverReasoningDecodeFailure(ctx, meta, body, resp)
}

func (c *APIClient) recoverReasoningDecodeFailure(
	ctx context.Context,
	meta RequestMeta,
	body []byte,
	response *http.Response,
) (*http.Response, ReasoningRecoveryOutcome, error) {
	if response == nil || response.StatusCode != http.StatusBadRequest {
		return response, ReasoningRecoveryOutcome{}, nil
	}
	errorBody, truncated, err := readDiagnosticBody(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return cloneBufferedResponse(response, errorBody), ReasoningRecoveryOutcome{}, nil
	}
	original := cloneBufferedResponse(response, errorBody)
	if truncated || !isReasoningDecodeFailure(errorBody) {
		return original, ReasoningRecoveryOutcome{}, nil
	}
	// 上游明确拒绝 opaque reasoning 时清理服务端回放，避免下一轮继续注入失效密文。
	if replay := DefaultReasoningReplay(); replay != nil && replay.Enabled() {
		sessionKey := strings.TrimSpace(meta.PromptCacheKey)
		if sessionKey == "" {
			sessionKey = strings.TrimSpace(meta.SessionID)
		}
		if sessionKey != "" && strings.TrimSpace(meta.Model) != "" {
			replay.Clear(ctx, meta.Model, sessionKey)
		}
	}

	portableBody, encryptedChanged := stripReasoningEncryptedContent(body)
	if encryptedChanged {
		retry, retryErr := c.CreateResponse(ctx, meta, bytes.NewReader(portableBody))
		if retryErr != nil {
			return original, ReasoningRecoveryOutcome{Failed: true}, nil
		}
		if isHTTPSuccess(retry.StatusCode) {
			_ = original.Body.Close()
			return retry, ReasoningRecoveryOutcome{EncryptedContentDowngraded: true}, nil
		}
		if retry.StatusCode == http.StatusTooManyRequests {
			// 去密文后的 429 是真实限流，不得回退成无效 400。
			_ = original.Body.Close()
			return retry, ReasoningRecoveryOutcome{EncryptedContentDowngraded: true}, nil
		}
		sameDecodeFailure, inspectErr := responseHasReasoningDecodeFailure(retry)
		if inspectErr != nil || !sameDecodeFailure {
			// 非同类解码失败：保留原始 400，避免掩盖首错。
			_ = retry.Body.Close()
			return original, ReasoningRecoveryOutcome{Failed: true}, nil
		}
		// 同类解码失败继续 session reset 路径；retry body 已在 inspect 中关闭。
	}

	if !canResetReasoningSession(meta, portableBody) {
		return original, ReasoningRecoveryOutcome{Failed: true}, nil
	}
	statelessBody := removePromptCacheKey(portableBody)
	resetMeta := meta
	resetMeta.PromptCacheKey = ""
	resetMeta.SessionID = ""
	resetMeta.TurnIndex = ""
	retry, retryErr := c.CreateResponse(ctx, resetMeta, bytes.NewReader(statelessBody))
	if retryErr != nil {
		return original, ReasoningRecoveryOutcome{Failed: true}, nil
	}
	if retry.StatusCode == http.StatusTooManyRequests {
		_ = original.Body.Close()
		return retry, ReasoningRecoveryOutcome{
			EncryptedContentDowngraded: encryptedChanged,
			SessionReset:               true,
		}, nil
	}
	if !isHTTPSuccess(retry.StatusCode) {
		_ = retry.Body.Close()
		return original, ReasoningRecoveryOutcome{Failed: true}, nil
	}
	_ = original.Body.Close()
	return retry, ReasoningRecoveryOutcome{
		EncryptedContentDowngraded: encryptedChanged,
		SessionReset:               true,
	}, nil
}

func responseHasReasoningDecodeFailure(response *http.Response) (bool, error) {
	if response == nil {
		return false, nil
	}
	if response.StatusCode != http.StatusBadRequest {
		_ = response.Body.Close()
		return false, nil
	}
	body, truncated, err := readDiagnosticBody(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return false, err
	}
	return !truncated && isReasoningDecodeFailure(body), nil
}

func canResetReasoningSession(meta RequestMeta, body []byte) bool {
	if strings.TrimSpace(meta.PromptCacheKey) == "" && strings.TrimSpace(meta.SessionID) == "" {
		return false
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	previousResponseID, _ := payload["previous_response_id"].(string)
	return strings.TrimSpace(previousResponseID) == ""
}

func removePromptCacheKey(body []byte) []byte {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return body
	}
	delete(payload, "prompt_cache_key")
	encoded, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return encoded
}

func isReasoningDecodeFailure(body []byte) bool {
	lower := bytes.ToLower(body)
	for _, marker := range reasoningDecodeFailureMarkers {
		if bytes.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// stripReasoningEncryptedContent 去掉 opaque reasoning 密文，保留可读 summary/content。
// 仅含密文的 reasoning 项会被整段删除。
func stripReasoningEncryptedContent(body []byte) ([]byte, bool) {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return body, false
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) == 0 {
		return body, false
	}
	changed := false
	rebuilt := make([]any, 0, len(input))
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok || stringField(item, "type") != "reasoning" {
			rebuilt = append(rebuilt, raw)
			continue
		}
		encrypted, ok := item["encrypted_content"].(string)
		if !ok || strings.TrimSpace(encrypted) == "" {
			rebuilt = append(rebuilt, raw)
			continue
		}
		cleaned := cloneJSONObject(item)
		delete(cleaned, "encrypted_content")
		delete(cleaned, "id")
		delete(cleaned, "status")
		changed = true
		if hasReadableReasoningContent(cleaned) {
			rebuilt = append(rebuilt, cleaned)
		}
	}
	if !changed {
		return body, false
	}
	payload["input"] = rebuilt
	encoded, err := json.Marshal(payload)
	if err != nil {
		return body, false
	}
	return encoded, true
}

func hasReadableReasoningContent(item map[string]any) bool {
	for _, field := range []string{"summary", "content"} {
		parts, _ := item[field].([]any)
		for _, raw := range parts {
			part, _ := raw.(map[string]any)
			if strings.TrimSpace(stringField(part, "text")) != "" {
				return true
			}
		}
	}
	return false
}

func cloneJSONObject(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for k, v := range value {
		out[k] = v
	}
	return out
}

func readDiagnosticBody(body io.ReadCloser) ([]byte, bool, error) {
	if body == nil {
		return nil, false, nil
	}
	raw, err := io.ReadAll(io.LimitReader(body, recoveryDiagnosticBodyLimit+1))
	if err != nil {
		return raw, false, err
	}
	truncated := len(raw) > recoveryDiagnosticBodyLimit
	if truncated {
		raw = raw[:recoveryDiagnosticBodyLimit]
	}
	return raw, truncated, nil
}

func cloneBufferedResponse(response *http.Response, body []byte) *http.Response {
	if response == nil {
		return nil
	}
	clone := *response
	clone.Body = io.NopCloser(bytes.NewReader(body))
	clone.ContentLength = int64(len(body))
	if clone.Header == nil {
		clone.Header = make(http.Header)
	} else {
		clone.Header = response.Header.Clone()
	}
	return &clone
}

func isHTTPSuccess(status int) bool {
	return status >= 200 && status < 300
}

// AppendCompatibilityWarning 将警告追加到响应头 X-Gork-Compatibility-Warnings。
func AppendCompatibilityWarning(header http.Header, warning string) {
	if header == nil || strings.TrimSpace(warning) == "" {
		return
	}
	const key = "X-Gork-Compatibility-Warnings"
	existing := strings.TrimSpace(header.Get(key))
	if existing == "" {
		header.Set(key, warning)
		return
	}
	for _, value := range strings.Split(existing, ",") {
		if strings.TrimSpace(value) == warning {
			return
		}
	}
	header.Set(key, existing+","+warning)
}

// ApplyReasoningRecoveryWarnings 将 recovery outcome 写入响应头。
func ApplyReasoningRecoveryWarnings(header http.Header, outcome ReasoningRecoveryOutcome) {
	if header == nil {
		return
	}
	if outcome.EncryptedContentDowngraded {
		AppendCompatibilityWarning(header, "reasoning_encrypted_content_downgraded")
	}
	if outcome.SessionReset {
		AppendCompatibilityWarning(header, "reasoning_session_reset")
	}
	if outcome.Failed {
		AppendCompatibilityWarning(header, "reasoning_recovery_failed")
	}
}
