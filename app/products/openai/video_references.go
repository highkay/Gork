package openai

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
	"github.com/dslzl/gork/app/dataplane/reverse/transport"
	"github.com/dslzl/gork/app/platform"
)

type videoReference struct {
	ContentURL string
	PostID     string
}

func prepareVideoReferences(ctx context.Context, token string, inputReferences []map[string]any) ([]videoReference, error) {
	if len(inputReferences) == 0 {
		return nil, nil
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
