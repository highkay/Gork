package products

import (
	"errors"

	"github.com/dslzl/gork/app/platform"
)

// RetryPolicy is the shared products-layer retry decision table.
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
