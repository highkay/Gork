package products

import (
	"context"
	"errors"
	"strings"
	"testing"

	controlmodel "github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/platform"
)

func TestRetryPolicyRetriesConfiguredUpstreamStatusUntilLimit(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 2, StatusCodes: []int{502, 503}}
	err := platform.NewUpstreamError("bad gateway", 502, "")

	if !policy.ShouldRetry(err, 0) {
		t.Fatalf("first 502 should retry")
	}
	if policy.ShouldRetry(err, 1) {
		t.Fatalf("last configured attempt should not retry")
	}
	if policy.ShouldRetry(platform.NewValidationError("bad", "body", ""), 0) {
		t.Fatalf("validation errors should not retry")
	}
}

func TestRunAccountDispatchRetriesWithExcludedTokenAndFeedback(t *testing.T) {
	directory := &fakeDispatchDirectory{
		leases: []AccountDispatchLease{
			{Token: "one", ModeID: 1},
			{Token: "two", ModeID: 1},
		},
	}
	attempts := 0
	result, err := RunAccountDispatch(context.Background(), AccountDispatchOptions[string]{
		Directory: directory,
		Spec:      controlmodel.ModelSpec{ModelName: "grok", Capability: controlmodel.CapabilityChat},
		Retry:     RetryPolicy{MaxAttempts: 2, StatusCodes: []int{502}},
		Feedback: func(err error) string {
			if err == nil {
				return "ok"
			}
			return "transport_error"
		},
	}, func(_ context.Context, lease AccountDispatchLease) (string, error) {
		attempts++
		if lease.Token == "one" {
			return "", platform.NewUpstreamError("temporary", 502, "")
		}
		return "done:" + lease.Token, nil
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if result != "done:two" || attempts != 2 {
		t.Fatalf("result=%q attempts=%d", result, attempts)
	}
	if got := directory.queries[1].Excluded[0]; got != "one" {
		t.Fatalf("second attempt excluded token = %q", got)
	}
	if got := directory.feedbacks[0].Kind; got != "transport_error" {
		t.Fatalf("feedback kind = %q", got)
	}
	if len(directory.released) != 2 || directory.released[0] != "one" || directory.released[1] != "two" {
		t.Fatalf("released tokens = %#v", directory.released)
	}
}

func TestRunAccountDispatchSkipsEmptyFeedback(t *testing.T) {
	directory := &fakeDispatchDirectory{
		leases: []AccountDispatchLease{{Token: "one", ModeID: 1}},
	}
	result, err := RunAccountDispatch(context.Background(), AccountDispatchOptions[string]{
		Directory: directory,
		Spec:      controlmodel.ModelSpec{ModelName: "grok", Capability: controlmodel.CapabilityChat},
		Feedback:  func(error) string { return "" },
	}, func(_ context.Context, lease AccountDispatchLease) (string, error) {
		return "done:" + lease.Token, nil
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if result != "done:one" {
		t.Fatalf("result = %q", result)
	}
	if len(directory.feedbacks) != 0 {
		t.Fatalf("feedbacks = %#v, want none", directory.feedbacks)
	}
}

func TestRunAccountDispatchReturnsReserveError(t *testing.T) {
	directory := &fakeDispatchDirectory{err: errors.New("reserve failed")}
	_, err := RunAccountDispatch(context.Background(), AccountDispatchOptions[string]{
		Directory: directory,
		Spec:      controlmodel.ModelSpec{ModelName: "grok"},
	}, func(context.Context, AccountDispatchLease) (string, error) {
		return "", nil
	})
	if err == nil || err.Error() != "reserve failed" {
		t.Fatalf("reserve error = %v", err)
	}
}

func TestRunAccountDispatchUsesCustomRetryable(t *testing.T) {
	retryErr := errors.New("retry me")
	directory := &fakeDispatchDirectory{
		leases: []AccountDispatchLease{
			{Token: "one", ModeID: 1},
			{Token: "two", ModeID: 1},
		},
	}
	result, err := RunAccountDispatch(context.Background(), AccountDispatchOptions[string]{
		Directory: directory,
		Spec:      controlmodel.ModelSpec{ModelName: "grok"},
		Retry:     RetryPolicy{MaxAttempts: 2},
		Retryable: func(err error) bool { return errors.Is(err, retryErr) },
	}, func(_ context.Context, lease AccountDispatchLease) (string, error) {
		if lease.Token == "one" {
			return "", retryErr
		}
		return lease.Token, nil
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if result != "two" {
		t.Fatalf("result = %q", result)
	}
	if got := directory.queries[1].Excluded[0]; got != "one" {
		t.Fatalf("second attempt excluded token = %q", got)
	}
}

func TestRunAccountDispatchUsesNoAccountsMessage(t *testing.T) {
	_, err := RunAccountDispatch(context.Background(), AccountDispatchOptions[string]{
		Directory:         &fakeDispatchDirectory{},
		Spec:              controlmodel.ModelSpec{ModelName: "grok"},
		NoAccountsMessage: "No available accounts for this model tier",
	}, func(context.Context, AccountDispatchLease) (string, error) {
		return "", nil
	})
	if err == nil || !strings.Contains(err.Error(), "No available accounts for this model tier") {
		t.Fatalf("no-account error = %v", err)
	}
}

type fakeDispatchDirectory struct {
	leases    []AccountDispatchLease
	err       error
	queries   []AccountDispatchQuery
	released  []string
	feedbacks []AccountDispatchFeedback
}

func (d *fakeDispatchDirectory) ReserveDispatchAccount(_ context.Context, query AccountDispatchQuery) (AccountDispatchLease, bool, error) {
	d.queries = append(d.queries, query)
	if d.err != nil {
		return AccountDispatchLease{}, false, d.err
	}
	if len(d.leases) == 0 {
		return AccountDispatchLease{}, false, nil
	}
	lease := d.leases[0]
	d.leases = d.leases[1:]
	return lease, true, nil
}

func (d *fakeDispatchDirectory) ReleaseDispatchAccount(_ context.Context, lease AccountDispatchLease) error {
	d.released = append(d.released, lease.Token)
	return nil
}

func (d *fakeDispatchDirectory) FeedbackDispatchAccount(_ context.Context, feedback AccountDispatchFeedback) error {
	d.feedbacks = append(d.feedbacks, feedback)
	return nil
}
