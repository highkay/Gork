package openai

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/dslzl/gork/app/control/model"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
	"github.com/dslzl/gork/app/dataplane/reverse/transport"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/config"
	"github.com/dslzl/gork/app/platform/storage"
)

const (
	imageMediaType        = "MEDIA_POST_TYPE_IMAGE"
	videoMediaType        = "MEDIA_POST_TYPE_VIDEO"
	videoModelName        = "imagine-video-gen"
	videoExtensionRefType = "ORIGINAL_REF_TYPE_VIDEO_EXTENSION"
)

var (
	videoCreateMediaPost = func(ctx context.Context, token, mediaType string, options transport.MediaOptions) (map[string]any, error) {
		return transport.CreateMediaPost(ctx, token, mediaType, options)
	}
	videoUploadFromInput = func(ctx context.Context, token, fileInput string) (transport.AssetUploadResult, error) {
		return transport.UploadFromInput(ctx, token, fileInput)
	}
	videoResolveUploadedAssetReference = func(token, fileID, fileURI string) (string, error) {
		return transport.ResolveUploadedAssetReference(token, fileID, fileURI)
	}
	videoStreamLines   = defaultVideoStreamLines
	videoDownloadBytes = func(ctx context.Context, token, rawURL string) ([]byte, string, error) {
		result, err := transport.DownloadAsset(ctx, token, rawURL)
		if err != nil {
			return nil, "", err
		}
		defer result.Stream.Close()
		raw, err := io.ReadAll(result.Stream)
		if err != nil {
			return nil, "", err
		}
		mime := "video/mp4"
		if result.ContentType != nil && *result.ContentType != "" {
			mime = *result.ContentType
		}
		return raw, mime, nil
	}
	videoSaveLocal = func(raw []byte, fileID string) (string, error) {
		return storage.SaveLocalVideo(raw, fileID)
	}
	videoFormatConfig = func() string {
		return fmt.Sprint(config.GetConfig("features.video_format", "grok_url"))
	}
	videoAppURL = func() string {
		return strings.TrimRight(fmt.Sprint(config.GetConfig("app.app_url", "")), "/")
	}
)

func defaultVideoGenerate(ctx context.Context, options videoGenerateOptions) (VideoArtifact, error) {
	spec, err := model.Resolve(options.Model)
	if err != nil {
		return VideoArtifact{}, err
	}
	if !spec.IsVideo() {
		return VideoArtifact{}, platform.NewValidationError("Model '"+options.Model+"' is not a video model", "model", "")
	}
	aspectRatio, defaultResolution, err := resolveVideoSize(options.Size)
	if err != nil {
		return VideoArtifact{}, err
	}
	resolution, err := resolveVideoResolutionName(options.ResolutionName, defaultResolution)
	if err != nil {
		return VideoArtifact{}, err
	}
	preset, err := resolveVideoPreset(options.Preset, "custom")
	if err != nil {
		return VideoArtifact{}, err
	}
	directory := chatDirectoryProvider()
	if directory == nil {
		return VideoArtifact{}, platform.NewRateLimitError("Account directory not initialised")
	}
	account, ok, err := directory.ReserveChatAccount(ctx, spec, nil)
	if err != nil {
		return VideoArtifact{}, err
	}
	if !ok {
		return VideoArtifact{}, platform.NewRateLimitError("No available accounts for video generation")
	}
	success := false
	var failErr error
	defer func() {
		_ = directory.ReleaseChatAccount(ctx, account)
		if success || failErr == nil {
			return
		}
		kind := feedbackKind(failErr)
		if kind == feedbackKindUnauthorized || kind == feedbackKindForbidden {
			_ = directory.FeedbackChatAccount(ctx, chatFeedback{Token: account.Token, Kind: kind, ModeID: account.ModeID})
		}
	}()
	artifact, err := generateVideoWithToken(ctx, account.Token, videoTokenOptions{
		Prompt:          options.Prompt,
		AspectRatio:     aspectRatio,
		ResolutionName:  resolution,
		Seconds:         options.Seconds,
		Preset:          preset,
		InputReferences: options.InputReferences,
		ProgressCB:      options.ProgressCB,
		TimeoutSeconds:  chatTimeoutSeconds(),
	})
	if err != nil {
		failErr = err
		return VideoArtifact{}, err
	}
	if artifact.VideoURL != "" {
		fileID := firstNonEmpty(artifact.AssetID, artifact.VideoPostID, MakeResponseID())
		localPath, err := downloadAndSaveVideo(ctx, account.Token, artifact.VideoURL, fileID)
		if err != nil {
			failErr = err
			return VideoArtifact{}, err
		}
		artifact.LocalContentFilePath = localPath
	}
	success = true
	return artifact, nil
}

type videoTokenOptions struct {
	Prompt          string
	AspectRatio     string
	ResolutionName  string
	Seconds         int
	Preset          string
	InputReferences []map[string]any
	ProgressCB      func(int)
	TimeoutSeconds  float64
}

func generateVideoWithToken(ctx context.Context, token string, options videoTokenOptions) (VideoArtifact, error) {
	references, err := prepareVideoReferences(ctx, token, options.InputReferences)
	if err != nil {
		return VideoArtifact{}, err
	}
	parentPostID := ""
	if len(references) > 0 {
		parentPostID = references[0].PostID
	} else {
		post, err := videoCreateMediaPost(ctx, token, videoMediaType, transport.MediaOptions{
			Prompt:  options.Prompt,
			Referer: reverseruntime.GlobalEndpointTable().Resolve("imagine_referer"),
		})
		if err != nil {
			return VideoArtifact{}, err
		}
		parentPostID = nestedString(post, "post", "id")
		if parentPostID == "" {
			return VideoArtifact{}, platform.NewUpstreamError("Video create-post returned no post id", 502, "")
		}
	}
	segments := buildVideoSegmentLengths(options.Seconds)
	var artifact VideoArtifact
	extendPostID := parentPostID
	elapsedSeconds := 0
	for index, segmentLength := range segments {
		var payload map[string]any
		referer := reverseruntime.GlobalEndpointTable().Resolve("imagine_referer")
		if index == 0 {
			payload = videoCreatePayload(options.Prompt, parentPostID, options.AspectRatio, options.ResolutionName, segmentLength, options.Preset, referenceContentURLs(references))
		} else {
			payload = videoExtendPayload(options.Prompt, parentPostID, extendPostID, options.AspectRatio, options.ResolutionName, segmentLength, options.Preset, videoExtendStartTime(elapsedSeconds))
			referer = reverseruntime.GlobalEndpointTable().Resolve("imagine_referer") + "/post/" + parentPostID
		}
		progressCB := func(progress int) {
			if options.ProgressCB == nil {
				return
			}
			scaled := int(((float64(index) + (float64(clampInt(progress, 0, 100)) / 100.0)) / float64(len(segments))) * 100)
			options.ProgressCB(scaled)
		}
		artifact, err = collectVideoSegment(ctx, token, payload, referer, options.TimeoutSeconds, progressCB)
		if err != nil {
			return VideoArtifact{}, err
		}
		if index == 0 && len(segments) > 1 {
			if artifact.VideoPostID != "" {
				artifact.RemixedFromVideoID = artifact.VideoPostID
			} else {
				artifact.RemixedFromVideoID = parentPostID
			}
		}
		extendPostID = firstNonEmpty(artifact.VideoPostID, artifact.AssetID, parentPostID)
		elapsedSeconds += segmentLength
	}
	if artifact.VideoURL == "" {
		return VideoArtifact{}, platform.NewUpstreamError("Video generation returned no artifact", 502, "")
	}
	return artifact, nil
}
