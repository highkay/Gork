package openai

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/httpbody"
)

func handleImageGenerations(w http.ResponseWriter, r *http.Request) {
	httpbody.LimitJSON(w, r)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeRouterError(w, platform.NewValidationError("Invalid JSON body", "body", ""))
		return
	}
	var req ImageGenerationRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeRouterError(w, platform.NewValidationError("Invalid JSON body", "body", ""))
		return
	}
	rawFields := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &rawFields)
	if err := validateRouterImageParams(req.Size, req.ResponseFormat, func(param string) bool {
		_, ok := rawFields[param]
		return ok
	}); err != nil {
		writeRouterError(w, err)
		return
	}
	spec, ok := model.Get(req.Model)
	if !ok || !spec.Enabled || !spec.IsImage() {
		writeRouterError(w, platform.NewValidationError("Model '"+req.Model+"' is not an image model", "model", ""))
		return
	}
	n := routerIntDefault(req.N, 1)
	if err := validateImageN(req.Model, n, "n"); err != nil {
		writeRouterError(w, err)
		return
	}
	result, err := routerGenerateImages(r.Context(), imageGenerationOptions{
		Model:          req.Model,
		Prompt:         req.Prompt,
		N:              n,
		Size:           routerStringDefault(req.Size, "1024x1024"),
		ResponseFormat: routerStringDefault(req.ResponseFormat, "url"),
		Stream:         false,
		ChatFormat:     false,
	})
	if err != nil {
		writeRouterError(w, err)
		return
	}
	writeImageResult(w, result)
}

func chatFromImageResult(result imageResult) chatCompletionResult {
	return chatCompletionResult{
		IsStream:     result.IsStream,
		StreamFrames: result.StreamFrames,
		Response:     result.Response,
	}
}
