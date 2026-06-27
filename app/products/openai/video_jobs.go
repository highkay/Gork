package openai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/platform"
	runtimepkg "github.com/dslzl/gork/app/platform/runtime"
)

type VideoCreateOptions struct {
	Model           string
	Prompt          string
	Seconds         int
	Size            string
	ResolutionName  string
	Preset          string
	InputReferences []map[string]any
}

type videoJobOptions struct {
	Size            string
	ResolutionName  string
	Prompt          string
	Seconds         int
	Preset          string
	InputReferences []map[string]any
}

type normalizedVideoCreate struct {
	Prompt  string
	Seconds int
	Size    string
}

const videoJobTTL = time.Hour

var (
	videoJobsMu  sync.Mutex
	videoJobs    = map[string]*VideoJob{}
	videoNowUnix = func() int64 {
		return time.Now().Unix()
	}
	videoID = func() string {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "video_" + strconv.FormatInt(time.Now().UnixNano(), 16)
		}
		return "video_" + hex.EncodeToString(raw[:])
	}
	videoStartJob = func(job *VideoJob, options videoJobOptions) {
		ctx, cancel := videoJobContext()
		go func() {
			defer cancel()
			runVideoJob(ctx, job, options)
		}()
	}
	videoJobContext = func() (context.Context, context.CancelFunc) {
		timeout := time.Duration(chatTimeoutSeconds() * float64(time.Second))
		if timeout <= 0 {
			return context.Background(), func() {}
		}
		return context.WithTimeout(context.Background(), timeout)
	}
	videoScheduleExpiration = func(videoID string) {
		go expireVideoJob(videoID, videoJobTTL)
	}
	videoGenerate = defaultVideoGenerate
)

func CreateVideo(ctx context.Context, options VideoCreateOptions) (map[string]any, error) {
	normalized, err := normalizeVideoCreateOptions(options)
	if err != nil {
		return nil, err
	}
	job := &VideoJob{
		ID:        videoID(),
		Model:     options.Model,
		Prompt:    normalized.Prompt,
		Seconds:   strconvItoa(normalized.Seconds),
		Size:      normalized.Size,
		Quality:   videoQuality,
		CreatedAt: videoNowUnix(),
		Status:    "queued",
	}
	putVideoJob(job)
	publishVideoJobSnapshot(job)
	videoScheduleExpiration(job.ID)
	videoStartJob(job, videoJobOptions{
		Size:            normalized.Size,
		ResolutionName:  options.ResolutionName,
		Prompt:          normalized.Prompt,
		Seconds:         normalized.Seconds,
		Preset:          options.Preset,
		InputReferences: options.InputReferences,
	})
	return job.ToDict(), nil
}

func normalizeVideoCreateOptions(options VideoCreateOptions) (normalizedVideoCreate, error) {
	spec, ok := model.Get(options.Model)
	if !ok || !spec.Enabled || !spec.IsVideo() {
		return normalizedVideoCreate{}, platform.NewValidationError("Model '"+options.Model+"' is not a video model", "model", "")
	}
	prompt := strings.TrimSpace(options.Prompt)
	if prompt == "" {
		return normalizedVideoCreate{}, platform.NewValidationError("prompt cannot be empty", "prompt", "")
	}
	seconds := options.Seconds
	if seconds == 0 {
		seconds = 6
	}
	if err := ValidateVideoLength(seconds); err != nil {
		return normalizedVideoCreate{}, err
	}
	size := strings.TrimSpace(options.Size)
	if size == "" {
		size = "720x1280"
	}
	_, defaultResolution, err := resolveVideoSize(size)
	if err != nil {
		return normalizedVideoCreate{}, err
	}
	if _, err := resolveVideoResolutionName(options.ResolutionName, defaultResolution); err != nil {
		return normalizedVideoCreate{}, err
	}
	if _, err := resolveVideoPreset(options.Preset, "custom"); err != nil {
		return normalizedVideoCreate{}, err
	}
	return normalizedVideoCreate{Prompt: prompt, Seconds: seconds, Size: size}, nil
}

func RetrieveVideo(videoID string) (map[string]any, error) {
	job, ok := GetVideoJob(videoID)
	if !ok {
		snapshot, found, err := runtimepkg.GetTaskSnapshot(context.Background(), videoID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, platform.NewValidationError("Video '"+videoID+"' not found", "video_id", "")
		}
		payload, ok := videoPayloadFromSnapshot(snapshot)
		if !ok {
			return nil, platform.NewValidationError("Video '"+videoID+"' not found", "video_id", "")
		}
		return payload, nil
	}
	return job.ToDict(), nil
}

func VideoContentPath(videoID string) (string, error) {
	job, ok := GetVideoJob(videoID)
	if !ok {
		snapshot, found, err := runtimepkg.GetTaskSnapshot(context.Background(), videoID)
		if err != nil {
			return "", err
		}
		if !found || snapshot["object"] != videoObject {
			return "", platform.NewValidationError("Video '"+videoID+"' not found", "video_id", "")
		}
		if snapshot["status"] != "completed" || strings.TrimSpace(asString(snapshot["content_path"])) == "" {
			return "", platform.NewAppError("Video content is not ready yet", platform.ErrorKindValidation, "video_not_ready", 409, nil)
		}
		path := strings.TrimSpace(asString(snapshot["content_path"]))
		if _, err := os.Stat(path); err != nil {
			return "", platform.NewValidationError("Video content for '"+videoID+"' not found", "video_id", "")
		}
		return path, nil
	}
	status, path := job.contentState()
	if status != "completed" || path == "" {
		return "", platform.NewAppError("Video content is not ready yet", platform.ErrorKindValidation, "video_not_ready", 409, nil)
	}
	if _, err := os.Stat(path); err != nil {
		return "", platform.NewValidationError("Video content for '"+videoID+"' not found", "video_id", "")
	}
	return path, nil
}

func GetVideoJob(videoID string) (*VideoJob, bool) {
	videoJobsMu.Lock()
	defer videoJobsMu.Unlock()
	job, ok := videoJobs[videoID]
	return job, ok
}

func putVideoJob(job *VideoJob) {
	videoJobsMu.Lock()
	defer videoJobsMu.Unlock()
	videoJobs[job.ID] = job
}

func clearVideoJobs() {
	videoJobsMu.Lock()
	defer videoJobsMu.Unlock()
	videoJobs = map[string]*VideoJob{}
}

func expireVideoJob(videoID string, ttl time.Duration) {
	timer := time.NewTimer(ttl)
	defer timer.Stop()
	<-timer.C
	videoJobsMu.Lock()
	defer videoJobsMu.Unlock()
	delete(videoJobs, videoID)
}

func setVideoJobStatus(job *VideoJob, status string, progress int) {
	job.setStatus(status, progress)
	publishVideoJobSnapshot(job)
}

func runVideoJob(ctx context.Context, job *VideoJob, options videoJobOptions) {
	setVideoJobStatus(job, "in_progress", 1)
	artifact, err := videoGenerate(ctx, videoGenerateOptions{
		Model:           job.Model,
		Prompt:          options.Prompt,
		Seconds:         options.Seconds,
		Size:            options.Size,
		ResolutionName:  options.ResolutionName,
		Preset:          options.Preset,
		InputReferences: options.InputReferences,
	})
	if err != nil {
		job.fail(err.Error())
		publishVideoJobSnapshot(job)
		return
	}
	job.complete(artifact, videoNowUnix())
	publishVideoJobSnapshot(job)
}

func strconvItoa(value int) string {
	return strconv.FormatInt(int64(value), 10)
}

func publishVideoJobSnapshot(job *VideoJob) {
	_ = runtimepkg.PublishTaskSnapshot(context.Background(), job.ID, job.Snapshot())
}

func videoPayloadFromSnapshot(snapshot map[string]any) (map[string]any, bool) {
	if snapshot["object"] != videoObject {
		return nil, false
	}
	payload := make(map[string]any, len(snapshot))
	for key, value := range snapshot {
		if key == "content_path" || key == "video_url" {
			continue
		}
		payload[key] = value
	}
	return payload, true
}
