package account

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeScheduledRefresher struct {
	calls chan string
	err   error
}

func resetAccountRefreshSchedulerSingletonForTest(t *testing.T) {
	t.Helper()
	previous := accountRefreshSchedulerSingleton
	if accountRefreshSchedulerSingleton != nil {
		accountRefreshSchedulerSingleton.Stop()
	}
	accountRefreshSchedulerSingleton = nil
	t.Cleanup(func() {
		if accountRefreshSchedulerSingleton != nil {
			accountRefreshSchedulerSingleton.Stop()
		}
		accountRefreshSchedulerSingleton = previous
	})
}

func (f *fakeScheduledRefresher) RefreshScheduled(_ context.Context, pool *string) (RefreshResult, error) {
	if pool != nil {
		select {
		case f.calls <- *pool:
		default:
		}
	}
	return RefreshResult{Checked: 1, Failed: 2}, f.err
}

func TestAccountRefreshIntervalDefaults(t *testing.T) {
	if got := accountRefreshInterval("basic", nil); got != 24*time.Hour {
		t.Fatalf("basic interval = %s, want 24h", got)
	}
	if got := accountRefreshInterval("super", nil); got != 2*time.Hour {
		t.Fatalf("super interval = %s, want 2h", got)
	}
	if got := accountRefreshInterval("heavy", nil); got != 2*time.Hour {
		t.Fatalf("heavy interval = %s, want 2h", got)
	}
}

func TestAccountRefreshIntervalReadsConfigAndHonorsOverrides(t *testing.T) {
	if accountRefreshIntervalConfigKeys["basic"] != "account.refresh.basic_interval_sec" ||
		accountRefreshIntervalConfigKeys["super"] != "account.refresh.super_interval_sec" ||
		accountRefreshIntervalConfigKeys["heavy"] != "account.refresh.heavy_interval_sec" {
		t.Fatalf("interval config keys = %#v", accountRefreshIntervalConfigKeys)
	}
	oldReader := accountRefreshIntervalSeconds
	accountRefreshIntervalSeconds = func(pool string, defaultSeconds int) int {
		if pool != "super" {
			return defaultSeconds
		}
		return 5
	}
	t.Cleanup(func() { accountRefreshIntervalSeconds = oldReader })

	if got := accountRefreshInterval("super", nil); got != 5*time.Second {
		t.Fatalf("configured super interval = %s, want 5s", got)
	}
	overrides := map[string]time.Duration{"super": time.Millisecond}
	if got := accountRefreshInterval("super", overrides); got != time.Millisecond {
		t.Fatalf("override super interval = %s, want 1ms", got)
	}
}

func TestAccountRefreshSchedulerLifecycleAndSingleton(t *testing.T) {
	oldRunOnStart := accountRefreshRunOnStart
	accountRefreshRunOnStart = func() bool { return false }
	t.Cleanup(func() { accountRefreshRunOnStart = oldRunOnStart })

	service1 := &fakeScheduledRefresher{calls: make(chan string, 10)}
	service2 := &fakeScheduledRefresher{calls: make(chan string, 10)}
	scheduler := NewAccountRefreshScheduler(service1)
	if scheduler.IsRunning() {
		t.Fatal("new scheduler should not be running")
	}
	scheduler.Start()
	if !scheduler.IsRunning() {
		t.Fatal("scheduler should be running after Start")
	}
	scheduler.BindService(service2)
	if scheduler.service != service2 {
		t.Fatal("BindService should update scheduler service")
	}
	scheduler.BindService(service1)
	scheduler.Start()
	if len(scheduler.cancelByPool) != 3 {
		t.Fatalf("duplicate Start changed loop count to %d, want 3", len(scheduler.cancelByPool))
	}
	scheduler.Stop()
	if scheduler.IsRunning() {
		t.Fatal("scheduler should stop after Stop")
	}

	resetAccountRefreshSchedulerSingletonForTest(t)
	first := GetAccountRefreshScheduler(service1)
	second := GetAccountRefreshScheduler(service2)
	if first != second || second.service != service2 {
		t.Fatal("singleton should be reused and rebound to latest service")
	}
	second.Stop()
}

func TestAccountRefreshSchedulerRunsPoolLoop(t *testing.T) {
	oldRunOnStart := accountRefreshRunOnStart
	accountRefreshRunOnStart = func() bool { return false }
	t.Cleanup(func() { accountRefreshRunOnStart = oldRunOnStart })

	service := &fakeScheduledRefresher{calls: make(chan string, 10)}
	scheduler := NewAccountRefreshScheduler(service)
	scheduler.refreshPool(context.Background(), "heavy")
	if pool := <-service.calls; pool != "heavy" {
		t.Fatalf("direct refreshPool = %q, want heavy", pool)
	}
	scheduler.intervals = map[string]time.Duration{
		"basic": time.Millisecond,
		"super": time.Hour,
		"heavy": time.Hour,
	}
	scheduler.Start()
	defer scheduler.Stop()

	select {
	case pool := <-service.calls:
		if pool != "basic" {
			t.Fatalf("scheduled pool = %q, want basic", pool)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("scheduler did not run basic pool loop")
	}
}

func TestAccountRefreshSchedulerRecordsPoolStatusAndErrors(t *testing.T) {
	service := &fakeScheduledRefresher{calls: make(chan string, 10), err: errors.New("quota failed")}
	scheduler := NewAccountRefreshScheduler(service)

	scheduler.refreshPool(context.Background(), "super")

	status := scheduler.Status()
	poolStatus := status.Pools["super"]
	if poolStatus.LastError != "quota failed" {
		t.Fatalf("last error = %q, want quota failed", poolStatus.LastError)
	}
	if poolStatus.ConsecutiveFailures != 1 {
		t.Fatalf("consecutive failures = %d, want 1", poolStatus.ConsecutiveFailures)
	}
	if poolStatus.LastResult.Checked != 1 || poolStatus.LastResult.Failed != 2 {
		t.Fatalf("last result = %#v, want checked=1 failed=2", poolStatus.LastResult)
	}

	service.err = nil
	scheduler.refreshPool(context.Background(), "super")

	status = scheduler.Status()
	poolStatus = status.Pools["super"]
	if poolStatus.LastError != "" || poolStatus.ConsecutiveFailures != 0 {
		t.Fatalf("status after success error=%q failures=%d, want cleared", poolStatus.LastError, poolStatus.ConsecutiveFailures)
	}
}

func TestAccountRefreshSchedulerRunOnStartAndJitterAreConfigurable(t *testing.T) {
	oldRunOnStart := accountRefreshRunOnStart
	oldJitter := accountRefreshJitterDuration
	accountRefreshRunOnStart = func() bool { return true }
	accountRefreshJitterDuration = func(pool string, interval time.Duration) time.Duration {
		if pool == "basic" {
			return 3 * time.Millisecond
		}
		return 0
	}
	t.Cleanup(func() {
		accountRefreshRunOnStart = oldRunOnStart
		accountRefreshJitterDuration = oldJitter
	})

	service := &fakeScheduledRefresher{calls: make(chan string, 10)}
	scheduler := NewAccountRefreshScheduler(service)
	scheduler.intervals = map[string]time.Duration{
		"basic": 20 * time.Millisecond,
		"super": time.Hour,
		"heavy": time.Hour,
	}

	scheduler.Start()
	defer scheduler.Stop()

	select {
	case <-service.calls:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("scheduler did not run immediately on start")
	}

	status := scheduler.Status()
	if status.Pools["basic"].NextRunAfter != 23*time.Millisecond {
		t.Fatalf("basic next run after = %s, want 23ms", status.Pools["basic"].NextRunAfter)
	}
}
