package redisrest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	URL        string
	Token      string
	HTTPClient *http.Client
}

type Client struct {
	endpoint   string
	token      string
	httpClient *http.Client
}

type commandResponse struct {
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

func New(config Config) (*Client, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(config.URL), "/")
	token := strings.TrimSpace(config.Token)
	if endpoint == "" {
		return nil, errors.New("Upstash Redis REST URL is required")
	}
	if token == "" {
		return nil, errors.New("Upstash Redis REST token is required")
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{endpoint: endpoint, token: token, httpClient: httpClient}, nil
}

func (c *Client) Do(ctx context.Context, command ...any) (any, error) {
	body, err := json.Marshal(command)
	if err != nil {
		return nil, err
	}
	raw, err := c.post(ctx, c.endpoint, body)
	if err != nil {
		return nil, err
	}
	var response commandResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	return decodeResponse(response)
}

func (c *Client) Pipeline(ctx context.Context, commands [][]any, transaction bool) ([]any, error) {
	if len(commands) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(commands)
	if err != nil {
		return nil, err
	}
	path := "/pipeline"
	if transaction {
		path = "/multi-exec"
	}
	raw, err := c.post(ctx, c.endpoint+path, body)
	if err != nil {
		return nil, err
	}
	var responses []commandResponse
	if err := json.Unmarshal(raw, &responses); err != nil {
		return nil, err
	}
	results := make([]any, 0, len(responses))
	for _, response := range responses {
		result, err := decodeResponse(response)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (c *Client) post(ctx context.Context, endpoint string, body []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if message := errorMessage(raw); message != "" {
			return nil, fmt.Errorf("Upstash Redis REST error: %s", message)
		}
		return nil, fmt.Errorf("Upstash Redis REST status %d", response.StatusCode)
	}
	return raw, nil
}

func decodeResponse(response commandResponse) (any, error) {
	if response.Error != "" {
		return nil, fmt.Errorf("Upstash Redis REST error: %s", response.Error)
	}
	if len(response.Result) == 0 || string(response.Result) == "null" {
		return nil, nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(response.Result))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func errorMessage(raw []byte) string {
	var response commandResponse
	if err := json.Unmarshal(raw, &response); err == nil && response.Error != "" {
		return response.Error
	}
	return strings.TrimSpace(string(raw))
}
