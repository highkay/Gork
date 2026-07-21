package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
	"github.com/dslzl/gork/app/dataplane/reverse/transport"
	"github.com/dslzl/gork/app/platform"
)

// 对齐 chenyme media.MaxInputJSONBytes / MaxInputImages（#723 扩展至 32MiB）。
const (
	MaxVideoInputJSONBytes = 32 << 20
	MaxVideoInputImages    = 8
)

type videoReference struct {
	ContentURL string
	PostID     string
}

// validateVideoInputReferences 限制参考图数量与 data URI 总体积（创建阶段 fail-fast）。
func validateVideoInputReferences(inputReferences []map[string]any) error {
	if len(inputReferences) == 0 {
		return nil
	}
	if len(inputReferences) > MaxVideoInputImages {
		return platform.NewValidationError(
			fmt.Sprintf("input_reference accepts at most %d images", MaxVideoInputImages),
			"input_reference",
			"video_input_too_many_images",
		)
	}
	encoded, err := json.Marshal(map[string]any{"image_urls": videoReferenceURLSamples(inputReferences)})
	if err != nil {
		return platform.NewValidationError("failed to encode video input references", "input_reference", "")
	}
	if len(encoded) > MaxVideoInputJSONBytes {
		return platform.NewValidationError(
			fmt.Sprintf("video input references exceed %d bytes", MaxVideoInputJSONBytes),
			"input_reference",
			"video_input_too_large",
		)
	}
	// data URI 原始长度再做一道保险（base64 膨胀前）。
	var rawBytes int
	for _, ref := range inputReferences {
		url := strings.TrimSpace(stringValue(ref["image_url"], ""))
		if strings.HasPrefix(url, "data:") {
			rawBytes += len(url)
		}
	}
	if rawBytes > MaxVideoInputJSONBytes {
		return platform.NewValidationError(
			fmt.Sprintf("video input data URIs exceed %d bytes", MaxVideoInputJSONBytes),
			"input_reference",
			"video_input_too_large",
		)
	}
	return nil
}

func videoReferenceURLSamples(inputReferences []map[string]any) []string {
	out := make([]string, 0, len(inputReferences))
	for _, ref := range inputReferences {
		if url := strings.TrimSpace(stringValue(ref["image_url"], "")); url != "" {
			out = append(out, url)
			continue
		}
		if id := strings.TrimSpace(stringValue(ref["file_id"], "")); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func prepareVideoReferences(ctx context.Context, token string, inputReferences []map[string]any) ([]videoReference, error) {
	if len(inputReferences) == 0 {
		return nil, nil
	}
	if err := validateVideoInputReferences(inputReferences); err != nil {
		return nil, err
	}
	references := make([]videoReference, 0, len(inputReferences))
	for index, ref := range inputReferences {
		reference, err := prepareVideoReference(ctx, token, ref)
		if err != nil {
			return nil, wrapVideoReferenceError(index, err)
		}
		references = append(references, reference)
	}
	return references, nil
}

func prepareVideoReference(ctx context.Context, token string, inputReference map[string]any) (videoReference, error) {
	fileID := strings.TrimSpace(stringValue(inputReference["file_id"], ""))
	imageInput := strings.TrimSpace(stringValue(inputReference["image_url"], ""))
	if fileID != "" && imageInput != "" {
		return videoReference{}, platform.NewValidationError("input_reference accepts only one of file_id or image_url", "input_reference", "")
	}
	if fileID != "" {
		return videoReference{}, platform.NewValidationError("input_reference.file_id is not supported yet", "input_reference.file_id", "")
	}
	if imageInput == "" {
		return videoReference{}, platform.NewValidationError("input_reference.image_url is required", "input_reference.image_url", "")
	}
	if !strings.HasPrefix(imageInput, "data:") {
		if _, err := url.ParseRequestURI(imageInput); err != nil {
			return videoReference{}, platform.NewValidationError("input_reference.image_url is not a valid URL", "input_reference.image_url", "invalid_video_reference_url")
		}
	}
	contentURL := imageInput
	if !isUpstreamAssetContentURL(imageInput) {
		upload, err := videoUploadFromInput(ctx, token, imageInput)
		if err != nil {
			return videoReference{}, newVideoUpstreamError("Video input reference upload failed: "+err.Error(), 502, "video_reference_upload_failed", "")
		}
		resolved, err := videoResolveUploadedAssetReference(token, upload.FileID, upload.FileURI)
		if err != nil {
			return videoReference{}, err
		}
		contentURL = resolved
	}
	post, err := videoCreateMediaPost(ctx, token, imageMediaType, transport.MediaOptions{
		MediaURL: contentURL,
		Prompt:   "",
		Referer:  reverseruntime.GlobalEndpointTable().Resolve("imagine_referer"),
	})
	if err != nil {
		return videoReference{}, err
	}
	postID := nestedString(post, "post", "id")
	if postID == "" {
		return videoReference{}, platform.NewUpstreamError("Video image reference create-post returned no post id", 502, "")
	}
	return videoReference{ContentURL: contentURL, PostID: postID}, nil
}

func wrapVideoReferenceError(index int, err error) error {
	message := fmt.Sprintf("Video input reference %d failed: %v", index+1, err)
	var appErr *platform.AppError
	if errors.As(err, &appErr) && appErr.Kind == platform.ErrorKindValidation {
		return platform.NewValidationError(message, "input_reference", "")
	}
	return platform.NewUpstreamError(message, 502, "")
}

func isUpstreamAssetContentURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	assets, err := url.Parse(reverseruntime.GlobalEndpointTable().Resolve("assets_download"))
	if err != nil {
		return false
	}
	return parsed.Scheme == assets.Scheme && parsed.Host == assets.Host && strings.HasSuffix(parsed.Path, "/content")
}

func referenceContentURLs(references []videoReference) []string {
	if len(references) == 0 {
		return nil
	}
	result := make([]string, 0, len(references))
	for _, reference := range references {
		result = append(result, reference.ContentURL)
	}
	return result
}
