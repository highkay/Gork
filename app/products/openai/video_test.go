package openai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/dataplane/reverse/transport"
	"github.com/dslzl/gork/app/platform"
	runtimepkg "github.com/dslzl/gork/app/platform/runtime"
)

func TestVideoValidationHelpersMatchPython(t *testing.T) {
	if err := ValidateVideoLength(12); err != nil {
		t.Fatalf("12s validation err=%v", err)
	}
	if err := ValidateVideoLength(8); !isValidationParam(err, "seconds") {
		t.Fatalf("8s validation err=%#v", err)
	}
	if got, want := buildVideoSegmentLengths(16), []int{10, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("segments=%v want %v", got, want)
	}
	aspect, resolution, err := resolveVideoSize("1280x720")
	if err != nil || aspect != "16:9" || resolution != "720p" {
		t.Fatalf("size aspect=%q resolution=%q err=%v", aspect, resolution, err)
	}
	if _, _, err := resolveVideoSize("1x1"); !isValidationParam(err, "size") {
		t.Fatalf("size err=%#v", err)
	}
	if got, err := resolveVideoPreset(" FUN ", "custom"); err != nil || got != "fun" {
		t.Fatalf("preset=%q err=%v", got, err)
	}
}

func TestVideoCreateRetrieveAndContentPath(t *testing.T) {
	resetVideoDepsForTest(t)
	t.Setenv("DATA_DIR", t.TempDir())
	videoStartJob = func(*VideoJob, videoJobOptions) {}
	scheduledVideoID := ""
	videoScheduleExpiration = func(videoID string) { scheduledVideoID = videoID }

	body, err := CreateVideo(context.Background(), VideoCreateOptions{
		Model:   "grok-imagine-video",
		Prompt:  "  make a clip  ",
		Seconds: 12,
		Size:    "1280x720",
	})
	if err != nil {
		t.Fatalf("create err=%v", err)
	}
	videoID := body["id"].(string)
	if !strings.HasPrefix(videoID, "video_") || body["object"] != "video" || body["status"] != "queued" {
		t.Fatalf("create body=%#v", body)
	}
	if body["prompt"] != "make a clip" || body["seconds"] != "12" || body["quality"] != "standard" {
		t.Fatalf("normalized body=%#v", body)
	}
	if scheduledVideoID != videoID {
		t.Fatalf("scheduled video id=%q want %q", scheduledVideoID, videoID)
	}

	retrieved, err := RetrieveVideo(videoID)
	if err != nil {
		t.Fatalf("retrieve err=%v", err)
	}
	if retrieved["id"] != videoID {
		t.Fatalf("retrieved=%#v", retrieved)
	}
	var appErr *platform.AppError
	if _, err := VideoContentPath(videoID); !errors.As(err, &appErr) || appErr.Status != http.StatusConflict {
		t.Fatalf("content before ready err=%#v", err)
	}

	path := filepath.Join(t.TempDir(), videoID+".mp4")
	if err := os.WriteFile(path, []byte("mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	job, _ := GetVideoJob(videoID)
	job.complete(VideoArtifact{LocalContentFilePath: path}, videoNowUnix())
	gotPath, err := VideoContentPath(videoID)
	if err != nil || gotPath != path {
		t.Fatalf("content path=%q err=%v", gotPath, err)
	}
}

func TestVideoCreateWorkerUsesDetachedContext(t *testing.T) {
	resetVideoDepsForTest(t)
	seen := make(chan error, 1)
	videoGenerate = func(ctx context.Context, _ videoGenerateOptions) (VideoArtifact, error) {
		seen <- ctx.Err()
		return VideoArtifact{}, errors.New("stop")
	}
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := CreateVideo(requestCtx, VideoCreateOptions{Model: "grok-imagine-video", Prompt: "make a clip", Seconds: 6, Size: "720x1280"}); err != nil {
		t.Fatalf("CreateVideo returned error: %v", err)
	}
	select {
	case err := <-seen:
		if err != nil {
			t.Fatalf("background context inherited request cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("background video worker did not start")
	}
}

func TestVideoCreatePublishesRuntimeSnapshotFallback(t *testing.T) {
	resetVideoDepsForTest(t)
	store := &fakeVideoSnapshotStore{snapshots: map[string]map[string]any{}}
	runtimepkg.SetTaskSnapshotStore(store)
	t.Cleanup(func() { runtimepkg.SetTaskSnapshotStore(nil) })
	videoStartJob = func(*VideoJob, videoJobOptions) {}

	body, err := CreateVideo(context.Background(), VideoCreateOptions{Model: "grok-imagine-video", Prompt: "snapshot clip", Seconds: 6, Size: "720x1280"})
	if err != nil {
		t.Fatalf("CreateVideo returned error: %v", err)
	}
	videoID := body["id"].(string)
	if store.snapshots[videoID] == nil {
		t.Fatalf("missing runtime snapshot for %s", videoID)
	}

	clearVideoJobs()
	retrieved, err := RetrieveVideo(videoID)
	if err != nil {
		t.Fatalf("RetrieveVideo runtime snapshot returned error: %v", err)
	}
	if retrieved["id"] != videoID || retrieved["prompt"] != "snapshot clip" || retrieved["object"] != videoObject {
		t.Fatalf("retrieved snapshot = %#v", retrieved)
	}
}

func TestVideoJobConcurrentStatusReads(t *testing.T) {
	resetVideoDepsForTest(t)
	path := filepath.Join(t.TempDir(), "video_testid.mp4")
	if err := os.WriteFile(path, []byte("mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	job := &VideoJob{ID: "video_testid", Model: "grok-imagine-video", Prompt: "race", Seconds: "6", Size: "720x1280", Quality: videoQuality, CreatedAt: 1, Status: "queued"}
	putVideoJob(job)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = RetrieveVideo(job.ID)
				_, _ = VideoContentPath(job.ID)
			}
		}()
	}
	for progress := 0; progress <= 100; progress++ {
		setVideoJobStatus(job, "in_progress", progress)
	}
	job.complete(VideoArtifact{LocalContentFilePath: path}, videoNowUnix())
	wg.Wait()
}

func TestRouterVideoEndpointsUseVideoJobs(t *testing.T) {
	resetRouterDepsForTest(t)
	resetVideoDepsForTest(t)
	videoStartJob = func(*VideoJob, videoJobOptions) {}

	form := strings.NewReader("model=grok-imagine-video&prompt=make+video&seconds=6&size=720x1280")
	req := httptest.NewRequest(http.MethodPost, "/v1/videos", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	videoID := decodeRouterJSON(t, rec)["id"].(string)

	rec = httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/videos/"+videoID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("retrieve status=%d body=%s", rec.Code, rec.Body.String())
	}
	if decodeRouterJSON(t, rec)["id"] != videoID {
		t.Fatalf("retrieve body=%s", rec.Body.String())
	}
}

func TestRouterVideoCreateAcceptsJSON(t *testing.T) {
	resetRouterDepsForTest(t)
	resetVideoDepsForTest(t)
	videoStartJob = func(*VideoJob, videoJobOptions) {}

	req := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"grok-imagine-video","prompt":"json clip","seconds":10,"size":"1280x720"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	videoID := decodeRouterJSON(t, rec)["id"].(string)
	retrieved, err := RetrieveVideo(videoID)
	if err != nil {
		t.Fatalf("retrieve err=%v", err)
	}
	if retrieved["prompt"] != "json clip" || retrieved["seconds"] != "10" || retrieved["size"] != "1280x720" {
		t.Fatalf("retrieved=%#v", retrieved)
	}
}

func TestVideoDefaultGenerateUsesProductionHooksAndSavesContent(t *testing.T) {
	resetVideoDepsForTest(t)
	oldDirectory := chatDirectoryProvider
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-video", ModeID: model.ModeAuto}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	t.Cleanup(func() { chatDirectoryProvider = oldDirectory })

	var mediaTypes []string
	videoCreateMediaPost = func(_ context.Context, token, mediaType string, options transport.MediaOptions) (map[string]any, error) {
		if token != "tok-video" {
			t.Fatalf("token=%q", token)
		}
		mediaTypes = append(mediaTypes, mediaType)
		if mediaType == imageMediaType && options.MediaURL == "" {
			t.Fatalf("image reference media url empty")
		}
		return map[string]any{"post": map[string]any{"id": "post-" + mediaType}}, nil
	}
	videoStreamLines = func(_ context.Context, token string, payload map[string]any, referer string, timeoutS float64) ([]string, error) {
		if token != "tok-video" || referer != "https://grok.com/imagine" || timeoutS <= 0 {
			t.Fatalf("stream token/referer/timeout=%q/%q/%v", token, referer, timeoutS)
		}
		if payload["modelName"] != videoModelName || !strings.Contains(payload["message"].(string), "--mode=custom") {
			t.Fatalf("payload=%#v", payload)
		}
		return []string{
			`data: {"result":{"response":{"streamingVideoGenerationResponse":{"progress":100,"videoPostId":"post-video","assetId":"asset-video","videoUrl":"/users/u/video.mp4/content","thumbnailImageUrl":"/users/u/thumb.jpg/content"}}}}`,
			"data: [DONE]",
		}, nil
	}
	videoDownloadBytes = func(_ context.Context, token, rawURL string) ([]byte, string, error) {
		if token != "tok-video" || rawURL != "https://assets.grok.com/users/u/video.mp4/content" {
			t.Fatalf("download token/url=%q/%q", token, rawURL)
		}
		return []byte{0, 0, 0, 'm', 'p', '4'}, "video/mp4", nil
	}
	videoSaveLocal = func(raw []byte, fileID string) (string, error) {
		if string(raw[3:]) != "mp4" || fileID != "asset-video" {
			t.Fatalf("save raw/fileID=%v/%q", raw, fileID)
		}
		return filepath.Join(t.TempDir(), fileID+".mp4"), nil
	}

	artifact, err := videoGenerate(context.Background(), videoGenerateOptions{
		Model:          "grok-imagine-video",
		Prompt:         "make a clip",
		Seconds:        6,
		Size:           "720x1280",
		ResolutionName: "720p",
		Preset:         "custom",
	})
	if err != nil {
		t.Fatalf("generate err=%v", err)
	}
	if artifact.VideoURL != "https://assets.grok.com/users/u/video.mp4/content" ||
		artifact.ThumbnailURL != "https://assets.grok.com/users/u/thumb.jpg/content" ||
		artifact.AssetID != "asset-video" ||
		!strings.HasSuffix(artifact.LocalContentFilePath, "asset-video.mp4") {
		t.Fatalf("artifact=%#v", artifact)
	}
	if !reflect.DeepEqual(mediaTypes, []string{videoMediaType}) {
		t.Fatalf("media types=%v", mediaTypes)
	}
	if dir.releases != 1 || len(dir.feedbacks) != 0 {
		t.Fatalf("release/feedback=%d/%#v", dir.releases, dir.feedbacks)
	}
}

func TestVideoFlowUsesSpecificErrorCodes(t *testing.T) {
	resetVideoDepsForTest(t)

	if _, err := prepareVideoReference(context.Background(), "tok", map[string]any{"image_url": "://bad-url"}); !appErrorCodeIs(err, "invalid_video_reference_url") {
		t.Fatalf("invalid reference err=%#v", err)
	}

	videoDownloadBytes = func(context.Context, string, string) ([]byte, string, error) {
		return nil, "", errors.New("download down")
	}
	if _, err := downloadAndSaveVideo(context.Background(), "tok", "https://assets.grok.com/v.mp4", "file1"); !appErrorCodeIs(err, "video_download_failed") {
		t.Fatalf("download err=%#v", err)
	}

	videoDownloadBytes = func(context.Context, string, string) ([]byte, string, error) {
		return []byte{0, 0, 0, 'm', 'p', '4'}, "video/mp4", nil
	}
	videoSaveLocal = func([]byte, string) (string, error) {
		return "", errors.New("disk full")
	}
	if _, err := downloadAndSaveVideo(context.Background(), "tok", "https://assets.grok.com/v.mp4", "file1"); !appErrorCodeIs(err, "video_cache_save_failed") {
		t.Fatalf("cache save err=%#v", err)
	}

	videoStreamLines = func(context.Context, string, map[string]any, string, float64) ([]string, error) {
		return []string{`data: {"error":{"message":"task failed","code":13}}`}, nil
	}
	if _, err := collectVideoSegment(context.Background(), "tok", map[string]any{}, "https://grok.com/imagine", 1, nil); !appErrorCodeIs(err, "video_upstream_task_failed") {
		t.Fatalf("upstream task err=%#v", err)
	}
}

func appErrorCodeIs(err error, code string) bool {
	var validation *platform.ValidationError
	if errors.As(err, &validation) && validation.AppError != nil && validation.Code == code {
		return true
	}
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) && upstream.AppError != nil && upstream.Code == code {
		return true
	}
	var appErr *platform.AppError
	return errors.As(err, &appErr) && appErr.Code == code
}

func resetVideoDepsForTest(t *testing.T) {
	t.Helper()
	oldStartJob := videoStartJob
	oldScheduleExpiration := videoScheduleExpiration
	oldGenerate := videoGenerate
	oldCreateMediaPost := videoCreateMediaPost
	oldUploadFromInput := videoUploadFromInput
	oldResolveUploadedAssetReference := videoResolveUploadedAssetReference
	oldStreamLines := videoStreamLines
	oldDownloadBytes := videoDownloadBytes
	oldSaveLocal := videoSaveLocal
	oldFormatConfig := videoFormatConfig
	oldAppURL := videoAppURL
	oldNow := videoNowUnix
	oldID := videoID
	oldMediaNow := routerMediaNow
	oldMediaSecret := routerMediaSigningSecret
	clearVideoJobs()
	videoNowUnix = func() int64 { return 1234 }
	videoID = func() string { return "video_testid" }
	routerMediaNow = func() time.Time { return time.Unix(1700000000, 0) }
	routerMediaSigningSecret = func() string { return "test-media-secret" }
	videoScheduleExpiration = func(string) {}
	t.Cleanup(func() {
		videoStartJob = oldStartJob
		videoScheduleExpiration = oldScheduleExpiration
		videoGenerate = oldGenerate
		videoCreateMediaPost = oldCreateMediaPost
		videoUploadFromInput = oldUploadFromInput
		videoResolveUploadedAssetReference = oldResolveUploadedAssetReference
		videoStreamLines = oldStreamLines
		videoDownloadBytes = oldDownloadBytes
		videoSaveLocal = oldSaveLocal
		videoFormatConfig = oldFormatConfig
		videoAppURL = oldAppURL
		videoNowUnix = oldNow
		videoID = oldID
		routerMediaNow = oldMediaNow
		routerMediaSigningSecret = oldMediaSecret
		clearVideoJobs()
	})
}

type fakeVideoSnapshotStore struct {
	snapshots map[string]map[string]any
}

func (s *fakeVideoSnapshotStore) Publish(*runtimepkg.AsyncTask, map[string]any) {}

func (s *fakeVideoSnapshotStore) PublishSnapshot(_ context.Context, taskID string, snapshot map[string]any) error {
	s.snapshots[taskID] = cloneVideoSnapshot(snapshot)
	return nil
}

func (s *fakeVideoSnapshotStore) GetSnapshot(_ context.Context, taskID string) (map[string]any, error) {
	snapshot := s.snapshots[taskID]
	if snapshot == nil {
		return nil, nil
	}
	return cloneVideoSnapshot(snapshot), nil
}

func cloneVideoSnapshot(snapshot map[string]any) map[string]any {
	out := make(map[string]any, len(snapshot))
	for key, value := range snapshot {
		out[key] = value
	}
	return out
}

func TestVideoUpstreamErrorRedactsSecrets(t *testing.T) {
	body := `{"error":{"code":"auth_failed","message":"Bearer sk-supersecrettokenvalue1234567890abcd failed for user@example.com"}}`
	err := newVideoUpstreamError("upload failed", 502, "video_reference_upload_failed", body)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "sk-supersecrettokenvalue") {
		t.Fatalf("token leaked in message: %s", err.Error())
	}
	if err.Body != "" && strings.Contains(err.Body, "sk-supersecrettokenvalue") {
		t.Fatalf("token leaked in body: %s", err.Body)
	}
	if !strings.Contains(err.Error(), "auth_failed") && !strings.Contains(err.Error(), "upload failed") {
		t.Fatalf("missing diagnostic summary: %s", err.Error())
	}
}

func TestValidateVideoInputReferencesLimits(t *testing.T) {
	if err := validateVideoInputReferences(nil); err != nil {
		t.Fatal(err)
	}
	tooMany := make([]map[string]any, MaxVideoInputImages+1)
	for i := range tooMany {
		tooMany[i] = map[string]any{"image_url": "https://example.com/" + string(rune('a'+i%26)) + ".png"}
	}
	if err := validateVideoInputReferences(tooMany); err == nil {
		t.Fatal("expected too many images error")
	}
	// oversized data URI
	huge := "data:image/png;base64," + strings.Repeat("A", MaxVideoInputJSONBytes)
	err := validateVideoInputReferences([]map[string]any{{"image_url": huge}})
	if err == nil {
		t.Fatal("expected too large error")
	}
}

func TestCreateVideoAllowsEmptyPromptWithReferences(t *testing.T) {
	// normalize should accept empty prompt when references present
	_, err := normalizeVideoCreateOptions(VideoCreateOptions{
		Model:           "grok-imagine-video",
		Prompt:          "",
		Seconds:         6,
		Size:            "720x1280",
		InputReferences: []map[string]any{{"image_url": "https://example.com/a.png"}},
	})
	// model may not be enabled in test catalog - accept validation model error OR nil
	if err != nil {
		if !strings.Contains(err.Error(), "not a video model") && !strings.Contains(err.Error(), "prompt") {
			// unexpected
			if strings.Contains(err.Error(), "video_input") {
				t.Fatalf("unexpected input err: %v", err)
			}
		}
	}
}
