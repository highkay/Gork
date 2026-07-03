package products

import (
	"errors"

	"github.com/dslzl/gork/app/platform"
)

// RetryPolicy is a shared products-layer retry decision table.
//
// It only evaluates upstream status and attempt count. Callers should use it
// only for product requests they have already classified as retry-safe.
type RetryPolicy struct {
	MaxAttempts int
	StatusCodes []int
}

func (p RetryPolicy) ShouldRetry(err error, attempt int) bool {
	if err == nil || attempt+1 >= p.MaxAttempts {
		return false
	}
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) {
		return false
	}
	for _, status := range p.StatusCodes {
		if upstream.Status == status {
			return true
		}
	}
	return false
}
