package openai

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/httpbody"
)

func handleImageEdits(w http.ResponseWriter, r *http.Request) {
	httpbody.LimitMultipart(w, r)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeRouterError(w, platform.NewValidationError("Invalid multipart body", "body", ""))
		return
	}
	modelName := r.FormValue("model")
	spec, ok := model.Get(modelName)
	if !ok || !spec.Enabled || !spec.IsImageEdit() {
		writeRouterError(w, platform.NewValidationError("Model '"+modelName+"' is not an image-edit model", "model", ""))
		return
	}
	if filesForField(r, "mask") != nil {
		writeRouterError(w, platform.NewValidationError("mask is not supported yet", "mask", ""))
		return
	}
	n := routerFormInt(r, "n", 1)
	if err := validateImageEditN(n, "n"); err != nil {
		writeRouterError(w, err)
		return
	}
	uploads := filesForField(r, "image[]")
	if len(uploads) == 0 {
		uploads = filesForField(r, "image")
	}
	if len(uploads) == 0 {
		writeRouterError(w, platform.NewValidationError("Uploaded image cannot be empty", "image", ""))
		return
	}
	content, err := imageEditMultipartContent(r.FormValue("prompt"), uploads)
	if err != nil {
		writeRouterError(w, err)
		return
	}
	result, err := routerEditImages(r.Context(), imageEditOptions{
		Model:          modelName,
		Messages:       []map[string]any{{"role": "user", "content": content}},
		N:              n,
		Size:           routerStringDefault(r.FormValue("size"), "1024x1024"),
		ResponseFormat: routerStringDefault(r.FormValue("response_format"), defaultImageResponseFormat()),
		Stream:         false,
		ChatFormat:     false,
	})
	if err != nil {
		writeRouterError(w, err)
		return
	}
	writeImageResult(w, result)
}

func imageEditMultipartContent(prompt string, uploads []*multipart.FileHeader) ([]any, error) {
	content := []any{map[string]any{"type": "text", "text": prompt}}
	for index, upload := range uploads {
		dataURI, err := uploadFileToDataURI(upload, "image."+strconv.Itoa(index))
		if err != nil {
			return nil, err
		}
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": dataURI},
		})
	}
	return content, nil
}

func handleVideosCreate(w http.ResponseWriter, r *http.Request) {
	payload, err := decodeRouterVideoCreateRequest(w, r)
	if err != nil {
		writeRouterError(w, err)
		return
	}
	if err := validateRouterVideoParams(payload.options.Size, payload.hasParam); err != nil {
		writeRouterError(w, err)
		return
	}
	result, err := CreateVideo(r.Context(), payload.options)
	if err != nil {
		writeRouterError(w, err)
		return
	}
	writeRouterJSON(w, http.StatusOK, result)
}

type routerVideoCreateRequest struct {
	options  VideoCreateOptions
	hasParam func(string) bool
}

func decodeRouterVideoCreateRequest(w http.ResponseWriter, r *http.Request) (routerVideoCreateRequest, error) {
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		httpbody.LimitMultipart(w, r)
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			return routerVideoCreateRequest{}, platform.NewValidationError("Invalid multipart body", "body", "")
		}
		return routerVideoCreateFormRequest(r)
	}
	if strings.Contains(contentType, "application/json") || strings.Contains(contentType, "+json") {
		httpbody.LimitJSON(w, r)
		return routerVideoCreateJSONRequest(r)
	}
	httpbody.LimitJSON(w, r)
	if err := r.ParseForm(); err != nil {
		return routerVideoCreateRequest{}, platform.NewValidationError("Invalid form body", "body", "")
	}
	return routerVideoCreateFormRequest(r)
}

func routerVideoCreateFormRequest(r *http.Request) (routerVideoCreateRequest, error) {
	inputReferences := []map[string]any(nil)
	for _, upload := range filesForField(r, "input_reference[]") {
		dataURI, err := uploadFileToDataURI(upload, "input_reference")
		if err != nil {
			return routerVideoCreateRequest{}, err
		}
		inputReferences = append(inputReferences, map[string]any{"image_url": dataURI})
		if len(inputReferences) >= 7 {
			break
		}
	}
	return routerVideoCreateRequest{
		options: VideoCreateOptions{
			Model:           routerStringDefault(r.FormValue("model"), "grok-video"),
			Prompt:          r.FormValue("prompt"),
			Seconds:         routerFormInt(r, "seconds", 6),
			Size:            routerStringDefault(r.FormValue("size"), "720x1280"),
			ResolutionName:  r.FormValue("resolution_name"),
			Preset:          r.FormValue("preset"),
			InputReferences: inputReferences,
		},
		hasParam: func(param string) bool {
			return strings.TrimSpace(r.FormValue(param)) != ""
		},
	}, nil
}

func routerVideoCreateJSONRequest(r *http.Request) (routerVideoCreateRequest, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return routerVideoCreateRequest{}, platform.NewValidationError("Invalid JSON body", "body", "")
	}
	return routerVideoCreateRequest{
		options: VideoCreateOptions{
			Model:           routerStringDefault(routerVideoJSONString(raw, "model"), "grok-video"),
			Prompt:          routerVideoJSONString(raw, "prompt"),
			Seconds:         routerVideoJSONInt(raw, "seconds", 6),
			Size:            routerStringDefault(routerVideoJSONString(raw, "size"), "720x1280"),
			ResolutionName:  routerVideoJSONString(raw, "resolution_name"),
			Preset:          routerVideoJSONString(raw, "preset"),
			InputReferences: routerVideoJSONInputReferences(raw),
		},
		hasParam: func(param string) bool {
			return routerVideoJSONHasParam(raw, param)
		},
	}, nil
}

func routerVideoJSONString(raw map[string]json.RawMessage, key string) string {
	value, ok := raw[key]
	if !ok {
		return ""
	}
	var out string
	if err := json.Unmarshal(value, &out); err != nil {
		return ""
	}
	return out
}

func routerVideoJSONInt(raw map[string]json.RawMessage, key string, fallback int) int {
	value, ok := raw[key]
	if !ok {
		return fallback
	}
	var number int
	if err := json.Unmarshal(value, &number); err == nil {
		return number
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		if parsed, err := strconv.Atoi(strings.TrimSpace(text)); err == nil {
			return parsed
		}
	}
	return fallback
}

func routerVideoJSONInputReferences(raw map[string]json.RawMessage) []map[string]any {
	for _, key := range []string{"input_reference", "input_references", "input_reference[]"} {
		value, ok := raw[key]
		if !ok {
			continue
		}
		var references []map[string]any
		if err := json.Unmarshal(value, &references); err != nil {
			continue
		}
		if len(references) > 7 {
			return references[:7]
		}
		return references
	}
	return nil
}

func routerVideoJSONHasParam(raw map[string]json.RawMessage, key string) bool {
	value, ok := raw[key]
	if !ok {
		return false
	}
	trimmed := strings.TrimSpace(string(value))
	return trimmed != "" && trimmed != "null" && trimmed != `""`
}

func handleVideosRead(w http.ResponseWriter, r *http.Request) {
	videoID := strings.TrimPrefix(r.URL.Path, "/v1/videos/")
	if strings.HasSuffix(videoID, "/content") {
		videoID = strings.TrimSuffix(videoID, "/content")
		path, err := VideoContentPath(videoID)
		if err != nil {
			writeRouterError(w, err)
			return
		}
		if !serveRouterFile(w, r, path, "video/mp4") {
			writeRouterError(w, platform.NewValidationError("Video content for '"+videoID+"' not found", "video_id", ""))
		}
		return
	}
	result, err := RetrieveVideo(videoID)
	if err != nil {
		writeRouterError(w, err)
		return
	}
	writeRouterJSON(w, http.StatusOK, result)
}

func routerFormInt(r *http.Request, key string, defaultValue int) int {
	raw := strings.TrimSpace(r.FormValue(key))
	if raw == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func filesForField(r *http.Request, key string) []*multipart.FileHeader {
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		return nil
	}
	return r.MultipartForm.File[key]
}
