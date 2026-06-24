package openai

import (
	"context"
	"html"
	"strings"

	"github.com/dslzl/gork/app/platform"
)

func downloadAndSaveVideo(ctx context.Context, token, rawURL, fileID string) (string, error) {
	raw, _, err := videoDownloadBytes(ctx, token, rawURL)
	if err != nil {
		return "", newVideoUpstreamError("Video download failed: "+err.Error(), 502, "video_download_failed", "")
	}
	if len(raw) == 0 {
		return "", newVideoUpstreamError("Video download returned empty content", 502, "video_download_failed", "")
	}
	trimmed := strings.TrimLeft(string(raw[:minInt(len(raw), 16)]), " \t\r\n")
	if strings.HasPrefix(trimmed, "<") || strings.HasPrefix(trimmed, "{") {
		return "", newVideoUpstreamError("Video download returned non-video content", 502, "video_download_failed", "")
	}
	path, err := videoSaveLocal(raw, fileID)
	if err != nil {
		return "", newVideoUpstreamError("Video cache save failed: "+err.Error(), 502, "video_cache_save_failed", "")
	}
	return path, nil
}

func renderVideoHTML(rawURL string) string {
	return `<video controls src="` + html.EscapeString(rawURL) + `"></video>`
}

func localVideoURL(fileID string) string {
	appURL := videoAppURL()
	if appURL == "" {
		return signedRouterFileURL("/v1/files/video", fileID)
	}
	return appURL + signedRouterFileURL("/v1/files/video", fileID)
}

func normalizeVideoFormat(value string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(value))
	if format == "" {
		format = "grok_url"
	}
	switch format {
	case "grok_url", "local_url", "grok_html", "local_html":
		return format, nil
	default:
		return "", platform.NewValidationError("video_format must be one of [grok_url, local_url, grok_html, local_html]", "features.video_format", "")
	}
}

func resolveVideoOutput(ctx context.Context, token, rawURL, fileID string) (string, string) {
	format, err := normalizeVideoFormat(videoFormatConfig())
	if err != nil || format == "grok_url" {
		return rawURL, ""
	}
	if format == "grok_html" {
		return renderVideoHTML(rawURL), ""
	}
	path, err := downloadAndSaveVideo(ctx, token, rawURL, fileID)
	if err != nil {
		if format == "local_html" {
			return renderVideoHTML(rawURL), ""
		}
		return rawURL, ""
	}
	localURL := localVideoURL(fileID)
	if format == "local_html" {
		return renderVideoHTML(localURL), path
	}
	return localURL, path
}
