package openai

import (
	"context"
	"sync"

	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/redact"
)

const chatRefreshQueueCapacity = 16

var chatRefreshEnqueue = newChatRefreshQueue(chatRefreshQueueCapacity).enqueue

type chatRefreshQueue struct {
	once sync.Once
	jobs chan func()
}

func newChatRefreshQueue(capacity int) *chatRefreshQueue {
	if capacity <= 0 {
		capacity = 1
	}
	return &chatRefreshQueue{jobs: make(chan func(), capacity)}
}

func (q *chatRefreshQueue) enqueue(job func()) bool {
	if q == nil || job == nil {
		return false
	}
	q.once.Do(func() {
		go q.run()
	})
	select {
	case q.jobs <- job:
		return true
	default:
		// ponytail: one bounded worker; drop excess refresh jobs if quota traffic spikes.
		return false
	}
}

func (q *chatRefreshQueue) run() {
	for job := range q.jobs {
		job()
	}
}

func enqueueChatRefresh(ctx context.Context, job func(context.Context)) {
	if job == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	jobCtx := context.WithoutCancel(ctx)
	_ = chatRefreshEnqueue(func() {
		job(jobCtx)
	})
}

func upstreamBodyExcerpt(err *platform.UpstreamError, limit int) string {
	if err == nil {
		return "-"
	}
	if limit <= 0 {
		limit = 240
	}
	return redact.Excerpt(err.Body, limit)
}

func upstreamBodyHash(err *platform.UpstreamError) string {
	if err == nil || err.Body == "" {
		return ""
	}
	return redact.SHA256Hex(err.Body)
}

func transportUpstreamError(err error, context string) *platform.UpstreamError {
	if err == nil {
		return nil
	}
	var upstream *platform.UpstreamError
	if errorsAs(err, &upstream) {
		return upstream
	}
	body := redact.Excerpt(err.Error(), 400)
	return platform.NewUpstreamError(context+": "+body, 502, body)
}

func logTaskException(err error) {
	_ = err
}

func quotaSync(ctx context.Context, token string, modeID int) {
	if currentAccountStrategy() != "quota" {
		return
	}
	service := chatRefreshService()
	if service == nil {
		return
	}
	enqueueChatRefresh(ctx, func(ctx context.Context) {
		_ = service.RefreshCall(ctx, token, modeID)
	})
}

func failSync(ctx context.Context, token string, modeID int, err error) {
	service := chatRefreshService()
	if service == nil {
		return
	}
	_ = service.RecordFailure(ctx, token, modeID, err)
	if currentAccountStrategy() == "quota" && upstreamStatus(err) == 429 {
		enqueueChatRefresh(ctx, func(ctx context.Context) {
			_, _ = service.RefreshOnDemand(ctx)
		})
	}
}

func shouldRetryUpstream(err error, retryCodes map[int]struct{}) bool {
	if _, ok := retryCodes[upstreamStatus(err)]; ok {
		return true
	}
	return isInvalidCredentials(err)
}

func feedbackKind(err error) accountFeedbackKind {
	if err == nil {
		return feedbackKindServerError
	}
	if isInvalidCredentials(err) {
		return feedbackKindUnauthorized
	}
	switch upstreamStatus(err) {
	case 429:
		return feedbackKindRateLimited
	case 401:
		return feedbackKindUnauthorized
	case 403:
		return feedbackKindForbidden
	default:
		return feedbackKindServerError
	}
}
