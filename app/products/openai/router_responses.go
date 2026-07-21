package openai

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/dataplane/build"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/httpbody"
)

func handleResponses(w http.ResponseWriter, r *http.Request) {
	httpbody.LimitJSON(w, r)
	var req ResponsesCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRouterError(w, platform.NewValidationError("Invalid JSON body", "body", ""))
		return
	}
	// Build 模型在动态/静态表中可能尚未登记，仍允许走 Build 原生路径。
	if !model.IsBuildModelName(req.Model) {
		spec, ok := model.Get(req.Model)
		if !ok || !spec.Enabled {
			writeRouterError(w, platform.NewValidationError("Model '"+req.Model+"' does not exist or you do not have access to it.", "model", "model_not_found"))
			return
		}
	}
	if routerInputEmpty(req.Input) {
		writeRouterError(w, platform.NewValidationError("input cannot be empty", "input", ""))
		return
	}
	isStream := routerBoolConfig("features.stream", true)
	if req.Stream != nil {
		isStream = *req.Stream
	}
	emitThink := routerBoolConfig("features.thinking", true)
	if req.Reasoning != nil {
		emitThink = req.Reasoning["effort"] != "none"
	}
	var seed, turnIdx string
	var overrides map[string]any
	if r != nil {
		seed = build.ExtractPromptCacheSeed(r.Header, nil)
		turnIdx = build.ExtractGrokTurnIndex(r.Header)
	}
	if key := strings.TrimSpace(req.PromptCacheKey); key != "" {
		overrides = map[string]any{"prompt_cache_key": key}
	}
	options := responseOptions{
		Model:            req.Model,
		Input:            req.Input,
		Instructions:     req.Instructions,
		Stream:           isStream,
		EmitThink:        emitThink,
		Temperature:      routerFloatDefault(req.Temperature, 0.8),
		TopP:             routerFloatDefault(req.TopP, 0.95),
		Tools:            routerToolMaps(req.Tools),
		ToolChoice:       req.ToolChoice,
		PromptCacheSeed:  seed,
		GrokTurnIndex:    turnIdx,
		RequestOverrides: overrides,
	}
	if routerRequestExplicitStream(req.Stream) {
		streamResponsesResult(w, r, options)
		return
	}
	result, err := routerResponses(r.Context(), options)
	if err != nil {
		writeRouterError(w, err)
		return
	}
	writeChatResult(w, result)
}

func routerRequestExplicitStream(stream *bool) bool {
	// Responses starts early SSE only when the request explicitly asks for it.
	// The configured stream default still controls downstream product behavior
	// for compatibility with existing non-stream route responses.
	return stream != nil && *stream
}

func streamResponsesResult(w http.ResponseWriter, r *http.Request, options responseOptions) {
	startRouterStream(w)
	writeRouterStreamHeartbeat(w, options.Model, MakeResponseID())
	done := make(chan chatMediaStreamOutcome, 1)
	go func() {
		result, err := routerResponses(r.Context(), options)
		done <- chatMediaStreamOutcome{result: result, err: err}
	}()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case outcome := <-done:
			if outcome.err != nil {
				logChatStreamError(options.Model, outcome.err)
				writeRouterStreamError(w, outcome.err)
				return
			}
			writeRouterStreamFrames(w, outcome.result.StreamFrames)
			return
		case <-ticker.C:
			writeRouterStreamHeartbeat(w, options.Model, MakeResponseID())
		case <-r.Context().Done():
			return
		}
	}
}
