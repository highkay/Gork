package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/auth"
	"github.com/dslzl/gork/app/platform/config"
)

var (
	routerAvailablePools = func(*http.Request) map[string]struct{} {
		return map[string]struct{}{}
	}
	routerAccountPools = func(context.Context) map[string]int {
		return nil
	}
	routerCompletions    = Completions
	routerResponses      = Responses
	routerGenerateImages = GenerateImages
	routerEditImages     = EditImages
	routerAuthSettings   = func() auth.AuthSettings {
		return auth.AuthSettings{APIKey: config.GetConfig("app.api_key", "")}
	}
)

// NewRouter returns the OpenAI-compatible /v1 HTTP surface.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", routerProtected(http.MethodGet, handleListModels))
	mux.HandleFunc("/v1/models/", routerProtected(http.MethodGet, handleGetModel))
	mux.HandleFunc("/v1/chat/completions", routerProtected(http.MethodPost, handleChatCompletions))
	mux.HandleFunc("/v1/responses", routerProtected(http.MethodPost, handleResponses))
	mux.HandleFunc("/v1/images/generations", routerProtected(http.MethodPost, handleImageGenerations))
	mux.HandleFunc("/v1/images/edits", routerProtected(http.MethodPost, handleImageEdits))
	mux.HandleFunc("/v1/videos", routerProtected(http.MethodPost, handleVideosCreate))
	mux.HandleFunc("/v1/videos/", routerProtected(http.MethodGet, handleVideosRead))
	mux.HandleFunc("/v1/files/video", routerMethod(http.MethodGet, handleServeVideo))
	mux.HandleFunc("/v1/files/image", routerMethod(http.MethodGet, handleServeImage))
	return mux
}

// SetRouterAccountPools sets the function used to query account pool counts.
// Pass nil to disable account pool filtering on /v1/models.
func SetRouterAccountPools(provider func(context.Context) map[string]int) {
	routerAccountPools = provider
}

func routerMethod(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
	status, payload := routerErrorResponse(err)
	writeRouterJSON(w, status, payload)
}

func routerErrorResponse(err error) (int, map[string]any) {
	var validation *platform.ValidationError
	if errors.As(err, &validation) && validation.AppError != nil {
		return validation.Status, validation.ToDict()
	}
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) && upstream.AppError != nil {
		return upstream.Status, upstream.ToDict()
	}
	var appErr *platform.AppError
	if errors.As(err, &appErr) && appErr != nil {
		return appErr.Status, appErr.ToDict()
	}
	return http.StatusInternalServerError, map[string]any{
		"error": map[string]any{
			"message": err.Error(),
			"type":    "server_error",
			"code":    "internal_error",
		},
	}
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
	frame := formatChatDataFrame(map[string]any{
		"id":      responseID,
		"object":  "chat.completion.chunk",
		"created": formatNowUnix(),
		"model":   modelName,
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{},
		}},
	})
	_, _ = w.Write([]byte(frame))
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
	_, _ = w.Write([]byte("\n\n"))
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
