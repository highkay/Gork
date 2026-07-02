package products

import (
	"context"
	"slices"

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
	Directory         AccountDispatchDirectory
	Spec              controlmodel.ModelSpec
	Retry             RetryPolicy
	Retryable         func(error) bool
	Feedback          func(error) string
	NoAccountsMessage string
}

func RunAccountDispatch[T any](
	ctx context.Context,
	options AccountDispatchOptions[T],
	run func(context.Context, AccountDispatchLease) (T, error),
) (T, error) {
	var zero T
	excluded := []string{}
	attempts := options.maxAttempts()
	for attempt := 0; attempt < attempts; attempt++ {
		lease, ok, err := options.Directory.ReserveDispatchAccount(ctx, AccountDispatchQuery{Spec: options.Spec, Excluded: slices.Clone(excluded)})
		if err != nil {
			return zero, err
		}
		if !ok {
			return zero, noAccountsError(options.NoAccountsMessage)
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
		if !options.shouldRetry(runErr, attempt) {
			return zero, runErr
		}
		excluded = append(excluded, lease.Token)
	}
	return zero, noAccountsError(options.NoAccountsMessage)
}

func (options AccountDispatchOptions[T]) shouldRetry(err error, attempt int) bool {
	if err == nil || attempt+1 >= options.maxAttempts() {
		return false
	}
	if options.Retryable != nil {
		return options.Retryable(err)
	}
	return options.Retry.ShouldRetry(err, attempt)
}

func (options AccountDispatchOptions[T]) maxAttempts() int {
	attempts := options.Retry.MaxAttempts
	if attempts <= 0 {
		return 1
	}
	return attempts
}

func noAccountsError(message string) error {
	if message == "" {
		message = "No available accounts"
	}
	return platform.NewRateLimitError(message)
}
