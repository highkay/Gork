package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
	"github.com/dslzl/gork/app/dataplane/reverse/transport"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/redact"
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

const (
	videoDiagnosticBodyLimit    = 64 << 10
	videoDiagnosticSummaryLimit = 256
)

// newVideoUpstreamError 构造视频上游错误；Body 经脱敏与长度截断，避免密钥/Cookie 落入日志与任务记录。
// 对齐 chenyme/grok2api video upload diagnostics（#727/#723）。
func newVideoUpstreamError(message string, status int, code string, body string) *platform.UpstreamError {
	summary := summarizeVideoUpstreamError(message, status, body)
	safeBody := redact.Excerpt(body, videoDiagnosticBodyLimit)
	if safeBody == "-" {
		safeBody = ""
	}
	err := platform.NewUpstreamError(summary, status, safeBody)
	err.Code = code
	return err
}

func summarizeVideoUpstreamError(message string, status int, body string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		message = fmt.Sprintf("Grok video upstream returned %d", status)
	}
	// 从 JSON body 提取 code/message 作为补充诊断（已脱敏）。
	if code, detail, ok := extractVideoUpstreamErrorFields(body); ok {
		parts := []string{message}
		if code != "" {
			parts = append(parts, code)
		}
		if detail != "" && !strings.Contains(message, detail) {
			parts = append(parts, detail)
		}
		message = strings.Join(parts, ": ")
	}
	return redact.Excerpt(message, videoDiagnosticSummaryLimit)
}

func extractVideoUpstreamErrorFields(body string) (code, message string, ok bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", "", false
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return "", "", false
	}
	if errorObject, mapped := root["error"].(map[string]any); mapped {
		code = firstNonEmpty(stringValue(errorObject["code"], ""), stringValue(errorObject["type"], ""))
		message = firstNonEmpty(stringValue(errorObject["message"], ""), stringValue(errorObject["detail"], ""))
	} else if errorText, isString := root["error"].(string); isString {
		message = errorText
	}
	if code == "" {
		code = firstNonEmpty(stringValue(root["code"], ""), stringValue(root["error_code"], ""))
	}
	if message == "" {
		message = firstNonEmpty(stringValue(root["message"], ""), stringValue(root["error_message"], ""), stringValue(root["detail"], ""))
	}
	code = redact.Excerpt(code, 64)
	if code == "-" {
		code = ""
	}
	message = redact.Excerpt(message, 160)
	if message == "-" {
		message = ""
	}
	return code, message, code != "" || message != ""
}
