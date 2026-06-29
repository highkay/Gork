package openai

import (
	"context"
	"strings"
)

func downloadAndSaveVideo(ctx context.Context, token, rawURL, fileID string) (string, error) {
	raw, _, err := videoDownloadBytes(ctx, token, rawURL)
	if err != nil {
		return "", newVideoUpstreamError("Video download failed: "+err.Error(), 502, "video_download_failed", "")
	}
	if len(raw) == 0 {
		return "", newVideoUpstreamError("Video download returned empty content", 502, "video_download_failed", "")
	}
	trimmed := strings.TrimLeft(string(raw[:min(len(raw), 16)]), " \t\r\n")
	if strings.HasPrefix(trimmed, "<") || strings.HasPrefix(trimmed, "{") {
		return "", newVideoUpstreamError("Video download returned non-video content", 502, "video_download_failed", "")
	}
	path, err := videoSaveLocal(raw, fileID)
	if err != nil {
		return "", newVideoUpstreamError("Video cache save failed: "+err.Error(), 502, "video_cache_save_failed", "")
	}
	return path, nil
}
