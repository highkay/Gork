package webui

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlmodel "github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/platform/auth"
	"github.com/dslzl/gork/app/products/web/ratelimit"
)

func TestWebUIModelsRequiresKeyAndMatchesPythonShape(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	webUIUnixNow = func() int64 { return 123 }
	webUIListModels = func() []controlmodel.ModelSpec {
		return []controlmodel.ModelSpec{
			{ModelName: "chat-model", PublicName: "Chat Model", Capability: controlmodel.CapabilityChat, Enabled: true},
			{ModelName: "image-model", PublicName: "Image Model", Capability: controlmodel.CapabilityImage, Enabled: true},
			{ModelName: "edit-model", PublicName: "Edit Model", Capability: controlmodel.CapabilityImage | controlmodel.CapabilityImageEdit, Enabled: true},
			{ModelName: "video-model", PublicName: "Video Model", Capability: controlmodel.CapabilityVideo, Enabled: true},
		}
	}

	rec := webUIRequest(http.MethodGet, "/webui/api/models", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing key status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = webUIRequest(http.MethodGet, "/webui/api/models", "", "Bearer web")
	body := decodeWebUIBody(t, rec)
	if body["object"] != "list" {
		t.Fatalf("object = %#v", body["object"])
	}
	data := body["data"].([]any)
	if len(data) != 4 {
		t.Fatalf("model count=%d body=%#v", len(data), body)
	}
	if data[0].(map[string]any)["created"].(float64) != 123 || data[0].(map[string]any)["owned_by"] != "xai" {
		t.Fatalf("first model = %#v", data[0])
	}
	if data[2].(map[string]any)["capability"] != "image_edit" || data[3].(map[string]any)["capability"] != "video" {
		t.Fatalf("capabilities = %#v %#v", data[2], data[3])
	}
}

func TestWebUIProtectedRateLimitsFailedAuth(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	webUIAuthRateLimiter = ratelimit.New(2, time.Minute)

	for i := 0; i < 2; i++ {
		rec := webUIRequest(http.MethodGet, "/webui/api/models", "", "Bearer wrong")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status=%d body=%s", i, rec.Code, rec.Body.String())
		}
	}
	rec := webUIRequest(http.MethodGet, "/webui/api/models", "", "Bearer wrong")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limited status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWebUIModelsIncludeDynamicRegistryModels(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	webUIUnixNow = func() int64 { return 789 }
	restore := controlmodel.SetDynamicProvider(func() []controlmodel.ModelSpec {
		return []controlmodel.ModelSpec{{
			ModelName: "grok-dynamic-webui", ModeID: controlmodel.ModeConsole, Tier: controlmodel.TierBasic,
			Capability: controlmodel.CapabilityConsoleChat, Enabled: true, PublicName: "Grok Dynamic WebUI",
		}}
	})
	t.Cleanup(restore)

	rec := webUIRequest(http.MethodGet, "/webui/api/models", "", "Bearer web")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	data := decodeWebUIBody(t, rec)["data"].([]any)
	for _, item := range data {
		model := item.(map[string]any)
		if model["id"] == "grok-dynamic-webui" {
			if model["capability"] != "chat" || model["created"].(float64) != 789 {
				t.Fatalf("dynamic webui model = %#v", model)
			}
			return
		}
	}
	t.Fatalf("dynamic model missing from webui models: %#v", data)
}

func TestWebUIChatCompletionsDelegatesToOpenAIHandler(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	var gotMethod, gotBody string
	webUIChatCompletions = func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotMethod, gotBody = r.Method, string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}

	rec := webUIRequest(http.MethodPost, "/webui/api/chat/completions", `{"model":"chat-model","messages":[]}`, "Bearer web")
	if rec.Code != http.StatusOK || rec.Body.String() != `{"ok":true}` {
		t.Fatalf("status/body=%d/%s", rec.Code, rec.Body.String())
	}
	if gotMethod != http.MethodPost || gotBody != `{"model":"chat-model","messages":[]}` {
		t.Fatalf("delegated method/body=%q/%q", gotMethod, gotBody)
	}
}

func TestWebUIChatCompletionsPassesThroughDelegateError(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	webUIChatCompletions = func(w http.ResponseWriter, r *http.Request) {
		writeWebUIJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"message": "delegate failed"}})
	}

	rec := webUIRequest(http.MethodPost, "/webui/api/chat/completions", `{"model":"chat-model","messages":[]}`, "Bearer web")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "delegate failed") {
		t.Fatalf("status/body=%d/%s", rec.Code, rec.Body.String())
	}
}

func TestWebUIChatCompletionsLimitsJSONBody(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	webUIChatCompletions = func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err == nil {
			t.Fatal("expected body limit error")
		}
		writeWebUIJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": map[string]any{"message": err.Error()}})
	}

	rec := webUIRequest(http.MethodPost, "/webui/api/chat/completions", strings.Repeat("x", 4<<20+1), "Bearer web")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status/body=%d/%s", rec.Code, rec.Body.String())
	}
}

func TestWebUIChatRouteGoldenStatusHeadersAndShapes(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	webUIUnixNow = func() int64 { return 456 }
	webUIListModels = func() []controlmodel.ModelSpec {
		return []controlmodel.ModelSpec{
			{ModelName: "chat-model", PublicName: "Chat Model", Capability: controlmodel.CapabilityChat, Enabled: true},
			{ModelName: "image-model", PublicName: "Image Model", Capability: controlmodel.CapabilityImage, Enabled: true},
		}
	}
	webUIChatCompletions = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-golden","object":"chat.completion"}`))
	}

	models := webUIRequest(http.MethodGet, "/webui/api/models", "", "Bearer web")
	assertWebUIGoldenJSON(t, models, http.StatusOK, map[string]any{"object": "list"})
	modelsBody := decodeWebUIBody(t, models)
	data := modelsBody["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("model count=%d body=%#v", len(data), modelsBody)
	}
	first := data[0].(map[string]any)
	second := data[1].(map[string]any)
	if first["id"] != "chat-model" || first["object"] != "model" || first["created"].(float64) != 456 || first["owned_by"] != "xai" || first["capability"] != "chat" {
		t.Fatalf("first model shape = %#v", first)
	}
	if second["id"] != "image-model" || second["capability"] != "image" {
		t.Fatalf("second model shape = %#v", second)
	}

	chat := webUIRequest(http.MethodPost, "/webui/api/chat/completions", `{"model":"chat-model","messages":[]}`, "Bearer web")
	assertWebUIGoldenJSON(t, chat, http.StatusOK, map[string]any{"id": "chatcmpl-golden", "object": "chat.completion"})

	methodGuard := webUIRequest(http.MethodDelete, "/webui/api/models", "", "Bearer web")
	assertWebUIGoldenJSON(t, methodGuard, http.StatusMethodNotAllowed, map[string]any{"error.message": "Method not allowed"})

	authError := webUIRequest(http.MethodGet, "/webui/api/models", "", "")
	assertWebUIGoldenJSON(t, authError, http.StatusUnauthorized, map[string]any{"error.code": "authentication_error"})

	matrix := []struct {
		planPath string
		covered  bool
	}{
		{planPath: "GET /webui/api/models", covered: true},
		{planPath: "POST /webui/api/chat/completions", covered: true},
	}
	gaps := 0
	for _, row := range matrix {
		if !row.covered {
			t.Errorf("missing golden coverage for %s", row.planPath)
			gaps++
		}
	}
	t.Logf("webui_chat_route_golden_matrix rows=%d gaps=%d", len(matrix), gaps)
}

func webUIRequest(method, target, body, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, req)
	return rec
}

func assertWebUIGoldenJSON(t *testing.T, rec *httptest.ResponseRecorder, status int, want map[string]any) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, status, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content-type=%q", got)
	}
	body := decodeWebUIBody(t, rec)
	for key, wantValue := range want {
		gotValue, ok := webUIGoldenJSONValue(body, key)
		if !ok {
			t.Fatalf("json missing %q in %#v", key, body)
		}
		if gotValue != wantValue {
			t.Fatalf("json[%s]=%#v want %#v body=%s", key, gotValue, wantValue, rec.Body.String())
		}
	}
}

func webUIGoldenJSONValue(body map[string]any, dotted string) (any, bool) {
	current := any(body)
	for _, part := range strings.Split(dotted, ".") {
		item, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = item[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func decodeWebUIBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func resetWebUITestDeps(t *testing.T) {
	t.Helper()
	oldAuth := webUIAuthSettings
	oldAuthLimiter := webUIAuthRateLimiter
	oldModels := webUIListModels
	oldNow := webUIUnixNow
	oldChat := webUIChatCompletions
	oldImagine := webUIImagineEvents
	oldImagineRunID := webUIImagineRunID
	oldImagineNSFW := webUIImagineDefaultNSFW
	oldImagineNow := webUIImagineNow
	oldImagineTickets := webUIImagineTickets
	oldImagineStopTimeout := webUIImagineStopTimeout
	oldWebSocketOptions := webUIWebSocketOptionsProvider
	oldWebSocketLimiter := webUIWebSocketLimiter
	oldVoiceDirectory := webUIVoiceDirectory
	oldVoiceFetch := webUIVoiceFetchToken
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{} }
	webUIAuthRateLimiter = ratelimit.New(5, time.Minute)
	webUIListModels = controlmodel.ListEnabled
	webUIUnixNow = defaultWebUIUnixNow
	webUIChatCompletions = defaultWebUIChatCompletions
	webUIImagineEvents = defaultWebUIImagineEvents
	webUIImagineRunID = defaultWebUIImagineRunID
	webUIImagineDefaultNSFW = defaultWebUIImagineDefaultNSFW
	webUIImagineNow = time.Now
	webUIImagineTickets = newWebUIImagineTicketStore()
	webUIImagineStopTimeout = time.Second
	webUIWebSocketOptionsProvider = defaultWebUIWebSocketOptions
	webUIWebSocketLimiter = newWebUIWebSocketConnectionLimiter()
	webUIVoiceDirectory = defaultWebUIVoiceDirectory
	webUIVoiceFetchToken = defaultWebUIVoiceFetchToken
	t.Cleanup(func() {
		webUIAuthSettings = oldAuth
		webUIAuthRateLimiter = oldAuthLimiter
		webUIListModels = oldModels
		webUIUnixNow = oldNow
		webUIChatCompletions = oldChat
		webUIImagineEvents = oldImagine
		webUIImagineRunID = oldImagineRunID
		webUIImagineDefaultNSFW = oldImagineNSFW
		webUIImagineNow = oldImagineNow
		webUIImagineTickets = oldImagineTickets
		webUIImagineStopTimeout = oldImagineStopTimeout
		webUIWebSocketOptionsProvider = oldWebSocketOptions
		webUIWebSocketLimiter = oldWebSocketLimiter
		webUIVoiceDirectory = oldVoiceDirectory
		webUIVoiceFetchToken = oldVoiceFetch
	})
}
