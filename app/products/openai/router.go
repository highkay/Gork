package openai

import (
	"encoding/json"
	"net/http"

	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/auth"
	"github.com/dslzl/gork/app/platform/config"
)

var (
	routerAvailablePools = func(*http.Request) map[string]struct{} {
		return map[string]struct{}{}
	}
	routerCompletions    = Completions
	routerResponses      = Responses
	routerGenerateImages = GenerateImages
	routerEditImages     = EditImages
	routerAuthSettings   = func() auth.AuthSettings {
		return auth.AuthSettings{APIKey: config.GetConfig("app.api_key", "")}
	}
)

type openAIRoute struct {
	Method    string
	Path      string
	Handler   http.HandlerFunc
	Protected bool
}

func openAIRoutes() []openAIRoute {
	return []openAIRoute{
		{Method: http.MethodGet, Path: "/v1/models", Handler: handleListModels, Protected: true},
		{Method: http.MethodGet, Path: "/v1/models/", Handler: handleGetModel, Protected: true},
		{Method: http.MethodPost, Path: "/v1/chat/completions", Handler: handleChatCompletions, Protected: true},
		{Method: http.MethodPost, Path: "/v1/responses", Handler: handleResponses, Protected: true},
		{Method: http.MethodPost, Path: "/v1/images/generations", Handler: handleImageGenerations, Protected: true},
		{Method: http.MethodPost, Path: "/v1/images/edits", Handler: handleImageEdits, Protected: true},
		{Method: http.MethodPost, Path: "/v1/videos", Handler: handleVideosCreate, Protected: true},
		{Method: http.MethodGet, Path: "/v1/videos/", Handler: handleVideosRead, Protected: true},
		{Method: http.MethodGet, Path: "/v1/files/video", Handler: handleServeVideo},
		{Method: http.MethodGet, Path: "/v1/files/image", Handler: handleServeImage},
	}
}

// NewRouter returns the OpenAI-compatible /v1 HTTP surface.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	for _, route := range openAIRoutes() {
		handler := routerMethod(route.Method, route.Handler)
		if route.Protected {
			handler = routerProtected(route.Method, route.Handler)
		}
		mux.HandleFunc(route.Path, handler)
	}
	return mux
}

func routerMethod(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Allow", method)
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", method)
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, x-api-key")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeRouterJSON(w, http.StatusMethodNotAllowed, map[string]any{
				"error": map[string]any{
					"message": "Method not allowed",
					"type":    "invalid_request_error",
				},
			})
			return
		}
		handler(w, r)
	}
}

func routerProtected(method string, handler http.HandlerFunc) http.HandlerFunc {
	return routerMethod(method, func(w http.ResponseWriter, r *http.Request) {
		err := auth.VerifyAPIKey(
			r.Header.Get("Authorization"),
			r.Header.Get("x-api-key"),
			routerAuthSettings(),
		)
		if err != nil {
			writeRouterError(w, err)
			return
		}
		handler(w, r)
	})
}

func writeRouterJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		return
	}
}

func writeRouterError(w http.ResponseWriter, err error) {
	adapted := platform.AdaptErrorResponse(err)
	for key, value := range adapted.Headers {
		w.Header().Set(key, value)
	}
	writeRouterJSON(w, adapted.Status, adapted.Payload)
}

func routerErrorResponse(err error) (int, map[string]any) {
	adapted := platform.AdaptErrorResponse(err)
	return adapted.Status, adapted.Payload
}

func routerBoolConfig(key string, defaultValue bool) bool {
	value := config.GetConfig(key, defaultValue)
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "1" || typed == "true" || typed == "yes" || typed == "on"
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case nil:
		return defaultValue
	default:
		return true
	}
}

func routerFloatDefault(value *float64, defaultValue float64) float64 {
	if value == nil {
		return defaultValue
	}
	return *value
}

func writeRouterStream(w http.ResponseWriter, frames []string) {
	startRouterStream(w)
	writeRouterStreamFrames(w, frames)
}

func startRouterStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
}

func writeRouterStreamHeartbeat(w http.ResponseWriter, modelName string, responseID string) {
	_, _ = w.Write([]byte(": heartbeat\n\n"))
	flushRouterStream(w)
}

func writeRouterStreamFrames(w http.ResponseWriter, frames []string) {
	for _, frame := range frames {
		_, _ = w.Write([]byte(frame))
	}
	flushRouterStream(w)
}

func writeRouterStreamError(w http.ResponseWriter, err error) {
	_, payload := routerErrorResponse(err)
	raw, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		raw = []byte(`{"error":{"message":"stream error","type":"server_error","code":"internal_error"}}`)
	}
	_, _ = w.Write([]byte("event: error\n"))
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(raw)
	_, _ = w.Write([]byte("\n\ndata: [DONE]\n\n"))
	flushRouterStream(w)
}

func flushRouterStream(w http.ResponseWriter) {
	flusher, ok := w.(http.Flusher)
	if ok {
		flusher.Flush()
	}
}

func writeChatResult(w http.ResponseWriter, result chatCompletionResult) {
	if result.IsStream {
		writeRouterStream(w, result.StreamFrames)
		return
	}
	writeRouterJSON(w, http.StatusOK, result.Response)
}

func writeImageResult(w http.ResponseWriter, result imageResult) {
	if result.IsStream {
		writeRouterStream(w, result.StreamFrames)
		return
	}
	writeRouterJSON(w, http.StatusOK, result.Response)
}
