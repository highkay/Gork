package products

import (
	"context"

	controlmodel "github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/platform"
)

type AccountDispatchLease struct {
	Token  string
	ModeID int
}

type AccountDispatchQuery struct {
	Spec     controlmodel.ModelSpec
	Excluded []string
}

type AccountDispatchFeedback struct {
	Token  string
	Kind   string
	ModeID int
}

type AccountDispatchDirectory interface {
	ReserveDispatchAccount(context.Context, AccountDispatchQuery) (AccountDispatchLease, bool, error)
	ReleaseDispatchAccount(context.Context, AccountDispatchLease) error
	FeedbackDispatchAccount(context.Context, AccountDispatchFeedback) error
}

type AccountDispatchOptions[T any] struct {
	Directory AccountDispatchDirectory
	Spec      controlmodel.ModelSpec
	Retry     RetryPolicy
	Feedback  func(error) string
}

func RunAccountDispatch[T any](
	ctx context.Context,
	options AccountDispatchOptions[T],
	run func(context.Context, AccountDispatchLease) (T, error),
) (T, error) {
	var zero T
	excluded := []string{}
	attempts := options.Retry.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		lease, ok, err := options.Directory.ReserveDispatchAccount(ctx, AccountDispatchQuery{Spec: options.Spec, Excluded: append([]string(nil), excluded...)})
		if err != nil {
			return zero, err
		}
		if !ok {
			return zero, platform.NewRateLimitError("No available accounts")
		}
		result, runErr := run(ctx, lease)
		_ = options.Directory.ReleaseDispatchAccount(ctx, lease)
		if options.Feedback != nil {
			if kind := options.Feedback(runErr); kind != "" {
				_ = options.Directory.FeedbackDispatchAccount(ctx, AccountDispatchFeedback{Token: lease.Token, Kind: kind, ModeID: lease.ModeID})
			}
		}
		if runErr == nil {
			return result, nil
		}
		if !options.Retry.ShouldRetry(runErr, attempt) {
			return zero, runErr
		}
		excluded = append(excluded, lease.Token)
	}
	return zero, platform.NewRateLimitError("No available accounts")
}
