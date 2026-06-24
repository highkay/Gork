package openai

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/dslzl/gork/app/control/model"
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
	spec, ok := model.Get(req.Model)
	if !ok || !spec.Enabled {
		writeRouterError(w, platform.NewValidationError("Model '"+req.Model+"' does not exist or you do not have access to it.", "model", "model_not_found"))
		return
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
	options := responseOptions{
		Model:        req.Model,
		Input:        req.Input,
		Instructions: req.Instructions,
		Stream:       isStream,
		EmitThink:    emitThink,
		Temperature:  routerFloatDefault(req.Temperature, 0.8),
		TopP:         routerFloatDefault(req.TopP, 0.95),
		Tools:        routerToolMaps(req.Tools),
		ToolChoice:   req.ToolChoice,
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
