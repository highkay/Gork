package build

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// RequestMeta 是单次 Build 上游调用的鉴权与路由头字段。
type RequestMeta struct {
	AccessToken    string
	UserID         string
	Model          string
	AgentID        string
	SessionID      string
	ConversationID string
	RequestID      string
	Stream         bool
}

// APIClient 出站 cli-chat-proxy（标准 HTTP/2，无 browser reverse）。
type APIClient struct {
	http   *http.Client
	config ClientConfig
}

// NewAPIClient 创建上游客户端；httpClient 为空则用默认超时客户端。
func NewAPIClient(httpClient *http.Client, cfg ClientConfig) *APIClient {
	cfg = cfg.Normalize()
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.Timeout}
	}
	return &APIClient{http: httpClient, config: cfg}
}

// ListModels GET /models，返回 data[].id。
func (c *APIClient) ListModels(ctx context.Context, accessToken string) ([]string, error) {
	resp, err := c.do(ctx, http.MethodGet, "/models", nil, RequestMeta{AccessToken: accessToken}, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read build models body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &UpstreamError{Status: resp.StatusCode, Body: truncateBody(body), Op: "list_models"}
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse build models: %w", err)
	}
	out := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}

// CreateResponse POST /responses；调用方负责关闭 resp.Body。
func (c *APIClient) CreateResponse(ctx context.Context, meta RequestMeta, body io.Reader) (*http.Response, error) {
	return c.do(ctx, http.MethodPost, "/responses", body, meta, true)
}

func (c *APIClient) do(
	ctx context.Context,
	method, path string,
	body io.Reader,
	meta RequestMeta,
	jsonContentType bool,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.joinURL(path), body)
	if err != nil {
		return nil, err
	}
	if jsonContentType && body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := c.applyHeaders(req, meta); err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("build upstream %s %s: %w", method, path, err)
	}
	if err := normalizeGzipResponse(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return resp, nil
}

func (c *APIClient) joinURL(path string) string {
	base := strings.TrimRight(c.config.BaseURL, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func (c *APIClient) applyHeaders(req *http.Request, meta RequestMeta) error {
	token := strings.TrimSpace(meta.AccessToken)
	if token == "" {
		return fmt.Errorf("build access_token 为空")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-XAI-Token-Auth", c.config.TokenAuth)
	req.Header.Set("x-grok-client-version", c.config.ClientVersion)
	req.Header.Set("x-grok-client-identifier", c.config.ClientIdentifier)
	req.Header.Set("x-grok-client-surface", "tui")
	req.Header.Set("x-grok-client-name", "grok")
	req.Header.Set("User-Agent", c.config.UserAgent)
	req.Header.Set("Accept-Encoding", "gzip")

	agentID := firstNonEmpty(meta.AgentID, newOpaqueID("agent"))
	sessionID := firstNonEmpty(meta.SessionID, newOpaqueID("session"))
	reqID := firstNonEmpty(meta.RequestID, newOpaqueID("req"))
	req.Header.Set("x-grok-agent-id", agentID)
	req.Header.Set("x-grok-session-id", sessionID)
	req.Header.Set("x-grok-req-id", reqID)
	req.Header.Set("traceparent", "00-"+strings.ReplaceAll(reqID, "-", "")+"-"+hex8()+"-01")

	if conv := strings.TrimSpace(meta.ConversationID); conv != "" {
		req.Header.Set("x-grok-conv-id", conv)
		req.Header.Set("x-grok-conversation-id", conv)
	}
	if uid := strings.TrimSpace(meta.UserID); uid != "" {
		req.Header.Set("x-userid", uid)
	}
	if model := strings.TrimSpace(meta.Model); model != "" {
		req.Header.Set("x-grok-model-override", model)
	}
	if meta.Stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	return nil
}

func normalizeGzipResponse(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	if !strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		return nil
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("gzip build response: %w", err)
	}
	resp.Body = struct {
		io.Reader
		io.Closer
	}{Reader: gz, Closer: multiCloser{gz, resp.Body}}
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	resp.Uncompressed = true
	return nil
}

type multiCloser struct {
	a, b io.Closer
}

func (m multiCloser) Close() error {
	err1 := m.a.Close()
	err2 := m.b.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func newOpaqueID(prefix string) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return prefix + "-" + hex.EncodeToString(b[:])
}

func hex8() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func truncateBody(body []byte) string {
	const max = 512
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max]) + "…"
}
