package openai

import (
	"context"
	"math/rand"
	"time"
)

var chatRetryDelay = func(attempt int) time.Duration {
	bases := []time.Duration{
		100 * time.Millisecond,
		300 * time.Millisecond,
		800 * time.Millisecond,
		1500 * time.Millisecond,
	}
	idx := attempt
	if idx < 0 {
		idx = 0
	}
	if idx >= len(bases) {
		idx = len(bases) - 1
	}
	base := bases[idx]
	return base + time.Duration(rand.Int63n(int64(base)))
}

func waitBeforeChatRetry(ctx context.Context, attempt int) error {
	delay := chatRetryDelay(attempt)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
