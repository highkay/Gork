package build

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPreparePromptCacheRouteToolFreeInjectsSearchNone(t *testing.T) {
	body, route, err := PreparePromptCacheRoute(
		[]byte(`{"model":"grok-4","input":"hello"}`),
		"chat", "grok-4", "cache-key", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["tool_choice"] != "none" {
		t.Fatalf("tool_choice=%v", payload["tool_choice"])
	}
	tools, _ := payload["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools=%#v", tools)
	}
	if !route.FilterXSearch || len(route.InjectedToolTypes) != 2 {
		t.Fatalf("route=%+v", route)
	}
}

func TestPreparePromptCacheRouteClientToolsInjectsXSearch(t *testing.T) {
	body, route, err := PreparePromptCacheRoute([]byte(`{
		"model":"grok-4",
		"tools":[{"type":"function","name":"Read","parameters":{"type":"object"}}]
	}`), "chat", "grok-4", "cache-key", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"x_search"`) {
		t.Fatalf("missing x_search: %s", body)
	}
	if _, ok := route.ClientDeclaredTools["Read"]; !ok || len(route.InjectedToolTypes) != 1 {
		t.Fatalf("route=%+v", route)
	}
}

func TestPreparePromptCacheRouteSkipsWithoutCacheKey(t *testing.T) {
	body, route, err := PreparePromptCacheRoute(
		[]byte(`{"model":"grok-4","input":"hello"}`),
		"chat", "grok-4", "", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "x_search") || route.FilterXSearch {
		t.Fatalf("should skip without cache key: body=%s route=%+v", body, route)
	}
}

func TestPreparePromptCacheRouteSkipsMediaModel(t *testing.T) {
	body, route, err := PreparePromptCacheRoute(
		[]byte(`{"model":"grok-imagine","input":"hello"}`),
		"chat", "grok-imagine-video", "cache-key", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "x_search") || route.NeedsResponseFilter() {
		t.Fatalf("media model should skip: %s route=%+v", body, route)
	}
}

func TestAllowClientToolCacheRoute(t *testing.T) {
	if !AllowClientToolCacheRoute("claude:s:agent:main", "") {
		t.Fatal("claude seed")
	}
	if !AllowClientToolCacheRoute("codex:window:w", "") {
		t.Fatal("codex seed")
	}
	if !AllowClientToolCacheRoute("", "codex-cli/1.0") {
		t.Fatal("codex UA")
	}
	if AllowClientToolCacheRoute("plain-seed", "curl/8") {
		t.Fatal("plain should be false")
	}
}

func TestFilterPromptCacheResponseDropsInternalXSearch(t *testing.T) {
	raw := []byte(`{
		"id":"resp",
		"output":[
			{"type":"custom_tool_call","call_id":"xs_call_1","name":"x_keyword_search"},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}
		],
		"tools":[{"type":"x_search"},{"type":"function","name":"Read"}]
	}`)
	resp := &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(raw)),
	}
	route := PromptCacheRoute{
		FilterXSearch:     true,
		InjectedToolTypes: map[string]struct{}{"x_search": {}},
		ClientDeclaredTools: map[string]struct{}{
			"Read": {},
		},
	}
	if err := FilterPromptCacheResponse(resp, false, route); err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(data), "xs_call") || strings.Contains(string(data), `"type":"x_search"`) {
		t.Fatalf("internal call/tool leaked: %s", data)
	}
	if !strings.Contains(string(data), `"name":"Read"`) {
		t.Fatalf("client tool dropped: %s", data)
	}
	if !strings.Contains(string(data), `"text":"hi"`) {
		t.Fatalf("message lost: %s", data)
	}
}
