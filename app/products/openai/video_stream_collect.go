package openai

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
	"github.com/dslzl/gork/app/dataplane/reverse/transport"
	"github.com/dslzl/gork/app/platform"
)

func defaultVideoStreamLines(ctx context.Context, token string, payload map[string]any, referer string, timeoutS float64) ([]string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	table := reverseruntime.GlobalEndpointTable()
	stream, err := transport.PostStream(ctx, table.Resolve("chat"), token, raw, transport.HTTPOptions{
		Timeout:     secondsDuration(timeoutS, 120*time.Second),
		ContentType: "application/json",
		Origin:      table.Resolve("base"),
		Referer:     referer,
	})
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	lines := []string{}
	for {
		line, ok, err := stream.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func collectVideoSegment(ctx context.Context, token string, payload map[string]any, referer string, timeoutS float64, progressCB func(int)) (VideoArtifact, error) {
	lines, err := videoStreamLines(ctx, token, payload, referer, timeoutS)
	if err != nil {
		return VideoArtifact{}, err
	}
	finalURL := ""
	finalAssetID := ""
	finalThumbnail := ""
	videoPostID := ""
	streamData := []string{}
	for _, line := range lines {
		eventType, data := protocol.ClassifyLine(line)
		if eventType == "done" {
			break
		}
		if eventType != "data" || data == "" {
			continue
		}
		streamData = append(streamData, data)
		obj := map[string]any{}
		if err := json.Unmarshal([]byte(data), &obj); err != nil {
			continue
		}
		if err := protocol.StreamErrorFromPayload(obj); err != nil {
			return VideoArtifact{}, newVideoUpstreamError(err.Message, err.Status, "video_upstream_task_failed", err.Body)
		}
		if stream := nestedMap(obj, "result", "response", "streamingVideoGenerationResponse"); stream != nil {
			progress := intFromAny(stream["progress"])
			if progressCB != nil {
				progressCB(progress)
			}
			videoPostID = firstNonEmpty(stringValue(stream["videoPostId"], ""), stringValue(stream["videoId"], ""), videoPostID)
			if progress >= 100 && stream["moderated"] != true {
				if rawURL := stringValue(stream["videoUrl"], ""); rawURL != "" {
					finalURL = absolutizeVideoURL(rawURL)
				}
				if assetID := stringValue(stream["assetId"], ""); assetID != "" {
					finalAssetID = assetID
				}
				if thumbnail := stringValue(stream["thumbnailImageUrl"], ""); thumbnail != "" {
					finalThumbnail = absolutizeVideoURL(thumbnail)
				}
			}
		}
		if attachments := extractVideoFileAttachments(obj); len(attachments) > 0 && finalAssetID == "" {
			finalAssetID = attachments[0]
		}
	}
	if finalURL == "" && finalAssetID != "" {
		if resolved := protocol.ResolveAssetReference(finalAssetID, "", ""); resolved != nil {
			finalURL = *resolved
		}
	}
	if finalURL == "" && finalAssetID != "" {
		return VideoArtifact{}, newVideoUpstreamError("Video segment returned only assetId without a resolvable URL", 502, "video_upstream_task_failed", strings.Join(streamData, "\n"))
	}
	if finalURL == "" {
		return VideoArtifact{}, newVideoUpstreamError("Video generation returned no final video URL", 502, "video_upstream_task_failed", strings.Join(streamData, "\n"))
	}
	return VideoArtifact{VideoURL: finalURL, VideoPostID: firstNonEmpty(videoPostID, finalAssetID), AssetID: finalAssetID, ThumbnailURL: finalThumbnail}, nil
}

func absolutizeVideoURL(rawURL string) string {
	fullURL, _, _ := protocol.ResolveDownloadURL(rawURL)
	return fullURL
}

func extractVideoFileAttachments(obj map[string]any) []string {
	modelResponse := nestedMap(obj, "result", "response", "modelResponse")
	if modelResponse == nil {
		return nil
	}
	attachments, _ := modelResponse["fileAttachments"].([]any)
	result := []string{}
	for _, item := range attachments {
		if text := stringValue(item, ""); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func newVideoUpstreamError(message string, status int, code string, body string) *platform.UpstreamError {
	err := platform.NewUpstreamError(message, status, body)
	err.Code = code
	return err
}
