package account

import (
	"context"
	"testing"
	"time"
)

type fakeSSOValidationRunner struct {
	calls []struct {
		page     int
		pageSize int
	}
	results []SSOValidationResult
}

type blockingSSOValidationRunner struct {
	started chan struct{}
	done    chan struct{}
}

func (r *fakeSSOValidationRunner) ValidateSSOBatch(_ context.Context, page int, pageSize int) (SSOValidationResult, error) {
	r.calls = append(r.calls, struct {
		page     int
		pageSize int
	}{page: page, pageSize: pageSize})
	if len(r.results) == 0 {
		return SSOValidationResult{NextPage: page + 1}, nil
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result, nil
}

func (r *blockingSSOValidationRunner) ValidateSSOBatch(ctx context.Context, _ int, _ int) (SSOValidationResult, error) {
	r.started <- struct{}{}
	<-ctx.Done()
	r.done <- struct{}{}
	return SSOValidationResult{}, ctx.Err()
}

func TestSSOValidationSchedulerRunOnceAdvancesCursor(t *testing.T) {
	runner := &fakeSSOValidationRunner{results: []SSOValidationResult{{Checked: 2, NextPage: 3}}}
	scheduler := NewSSOValidationScheduler(runner, SSOValidationSchedulerOptions{
		Interval:  time.Hour,
		BatchSize: 25,
	})

	result, err := scheduler.runOnce(context.Background())

	if err != nil {
		t.Fatalf("runOnce returned error: %v", err)
	}
	if result.Checked != 2 || scheduler.nextPage != 3 {
		t.Fatalf("result/cursor = %#v/%d", result, scheduler.nextPage)
	}
	if len(runner.calls) != 1 || runner.calls[0].page != 1 || runner.calls[0].pageSize != 25 {
		t.Fatalf("runner calls = %#v", runner.calls)
	}

	_, err = scheduler.runOnce(context.Background())
	if err != nil {
		t.Fatalf("second runOnce returned error: %v", err)
	}
	if len(runner.calls) != 2 || runner.calls[1].page != 3 {
		t.Fatalf("second runner calls = %#v", runner.calls)
	}
}

func TestSSOValidationSchedulerStopWaitsForInFlightRun(t *testing.T) {
	runner := &blockingSSOValidationRunner{
		started: make(chan struct{}, 1),
		done:    make(chan struct{}, 1),
	}
	scheduler := NewSSOValidationScheduler(runner, SSOValidationSchedulerOptions{
		Interval:  time.Millisecond,
		BatchSize: 25,
	})
	scheduler.Start()
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("SSO validation did not start")
	}

	scheduler.Stop()

	select {
	case <-runner.done:
	default:
		t.Fatal("Stop returned before in-flight SSO validation finished")
	}
}
