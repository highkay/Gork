package openai

import (
	"fmt"
	"strings"
	"sync"

	"github.com/dslzl/gork/app/platform"
)

const (
	videoQuality = "standard"
	videoObject  = "video"
)

var videoSizeMap = map[string][2]string{
	"720x1280":  {"9:16", "720p"},
	"1280x720":  {"16:9", "720p"},
	"1024x1024": {"1:1", "720p"},
	"1024x1792": {"9:16", "720p"},
	"1792x1024": {"16:9", "720p"},
}

var videoPresetFlags = map[string]string{
	"fun":    "--mode=extremely-crazy",
	"normal": "--mode=normal",
	"spicy":  "--mode=extremely-spicy-or-crazy",
	"custom": "--mode=custom",
}

type VideoArtifact struct {
	VideoURL             string
	VideoPostID          string
	AssetID              string
	ThumbnailURL         string
	RemixedFromVideoID   string
	LocalContentFilePath string
}

type VideoJob struct {
	mu sync.RWMutex

	ID                 string
	Model              string
	Prompt             string
	Seconds            string
	Size               string
	Quality            string
	CreatedAt          int64
	Status             string
	Progress           int
	CompletedAt        int64
	Error              map[string]any
	RemixedFromVideoID string
	VideoURL           string
	ContentPath        string
}

func (j *VideoJob) ToDict() map[string]any {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.toDictLocked()
}

func (j *VideoJob) toDictLocked() map[string]any {
	payload := map[string]any{
		"id":         j.ID,
		"object":     videoObject,
		"created_at": j.CreatedAt,
		"status":     j.Status,
		"model":      j.Model,
		"progress":   j.Progress,
		"prompt":     j.Prompt,
		"seconds":    j.Seconds,
		"size":       j.Size,
		"quality":    j.Quality,
	}
	if j.CompletedAt != 0 {
		payload["completed_at"] = j.CompletedAt
	}
	if j.Error != nil {
		payload["error"] = j.Error
	}
	if j.RemixedFromVideoID != "" {
		payload["remixed_from_video_id"] = j.RemixedFromVideoID
	}
	return payload
}

func (j *VideoJob) Snapshot() map[string]any {
	j.mu.RLock()
	defer j.mu.RUnlock()
	payload := j.toDictLocked()
	if j.VideoURL != "" {
		payload["video_url"] = j.VideoURL
	}
	if j.ContentPath != "" {
		payload["content_path"] = j.ContentPath
	}
	return payload
}

func (j *VideoJob) contentState() (string, string) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.Status, j.ContentPath
}

func (j *VideoJob) setStatus(status string, progress int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = status
	if progress >= 0 {
		if progress > 100 {
			progress = 100
		}
		j.Progress = progress
	}
}

func (j *VideoJob) fail(message string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = "failed"
	j.Error = map[string]any{"code": "video_generation_failed", "message": message}
}

func (j *VideoJob) complete(artifact VideoArtifact, completedAt int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = "completed"
	j.Progress = 100
	j.CompletedAt = completedAt
	j.VideoURL = artifact.VideoURL
	j.ContentPath = artifact.LocalContentFilePath
	j.RemixedFromVideoID = artifact.RemixedFromVideoID
}

func ValidateVideoLength(seconds int) error {
	switch seconds {
	case 6, 10, 12, 16, 20:
		return nil
	default:
		return platform.NewValidationError("seconds must be one of [6, 10, 12, 16, 20]", "seconds", "")
	}
}

func resolveVideoSize(size string) (string, string, error) {
	normalized := strings.TrimSpace(size)
	if normalized == "" {
		normalized = "720x1280"
	}
	config, ok := videoSizeMap[normalized]
	if !ok {
		return "", "", platform.NewValidationError("size must be one of [720x1280, 1280x720, 1024x1024, 1024x1792, 1792x1024]", "size", "")
	}
	return config[0], config[1], nil
}

func resolveVideoResolutionName(value, defaultValue string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		normalized = defaultValue
	}
	if normalized != "480p" && normalized != "720p" {
		return "", platform.NewValidationError("resolution_name must be one of [480p, 720p]", "resolution_name", "")
	}
	return normalized, nil
}

func resolveVideoPreset(value, defaultValue string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		normalized = defaultValue
	}
	if _, ok := videoPresetFlags[normalized]; !ok {
		return "", platform.NewValidationError("preset must be one of [custom, fun, normal, spicy]", "preset", "")
	}
	return normalized, nil
}

func buildVideoSegmentLengths(seconds int) []int {
	switch seconds {
	case 6:
		return []int{6}
	case 10:
		return []int{10}
	case 12:
		return []int{6, 6}
	case 16:
		return []int{10, 6}
	case 20:
		return []int{10, 10}
	default:
		return nil
	}
}

func videoProgressReason(progress int) string {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	return fmt.Sprintf("视频正在生成 %d%%", progress)
}
