package proxy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	platformconfig "github.com/dslzl/gork/app/platform/config"
)

type fakeSchedulerDirectory struct {
	loadCalls    int
	warmUpCalls  int
	refreshCalls int
	loadErr      error
	warmUpErr    error
	refreshErr   error
	warmUpCh     chan struct{}
	skipSeen     map[BundleKey]bool
	backoffUntil map[BundleKey]int64
	failures     map[BundleKey]int
	bundles      map[BundleKey]ClearanceBundle
}

func (f *fakeSchedulerDirectory) Load(context.Context) error {
	f.loadCalls++
	return f.loadErr
}

func (f *fakeSchedulerDirectory) WarmUp(context.Context) error {
	f.warmUpCalls++
	if f.warmUpCh != nil {
		select {
		case f.warmUpCh <- struct{}{}:
		default:
		}
	}
	return f.warmUpErr
}

func (f *fakeSchedulerDirectory) RefreshClearanceSafe(context.Context) error {
	f.refreshCalls++
	return f.refreshErr
}

func (f *fakeSchedulerDirectory) RefreshClearanceFiltered(_ context.Context, skip map[BundleKey]bool) error {
	f.refreshCalls++
	f.skipSeen = skip
	return f.refreshErr
}

func (f *fakeSchedulerDirectory) RefreshBackoffUntil() map[BundleKey]int64 {
	return f.backoffUntil
}

func (f *fakeSchedulerDirectory) FailureCounts() map[BundleKey]int {
	return f.failures
}

func (f *fakeSchedulerDirectory) Bundles() map[BundleKey]ClearanceBundle {
	return f.bundles
}

type fakeSchedulerConfig struct {
	interval int
}

func (f fakeSchedulerConfig) GetInt(key string, defaultValue int) int {
	if key == "proxy.clearance.refresh_interval" && f.interval != 0 {
		return f.interval
	}
	return defaultValue
}

func (f fakeSchedulerConfig) GetFloat(_ string, defaultValue float64) float64 {
	return defaultValue
}

func (f fakeSchedulerConfig) GetBool(_ string, defaultValue bool) bool {
	return defaultValue
}

func TestProxyClearanceSchedulerStartStopMatchesPython(t *testing.T) {
	directory := &fakeSchedulerDirectory{}
	scheduler := NewProxyClearanceScheduler(directory, SchedulerOptions{
		Config: fakeSchedulerConfig{interval: 3600},
	})

	scheduler.Start(context.Background())
	scheduler.Start(context.Background())
	if !scheduler.running {
		t.Fatalf("scheduler should be running after Start")
	}
	scheduler.Stop()
	if scheduler.running {
		t.Fatalf("scheduler should not be running after Stop")
	}
}

func TestProxyClearanceSchedulerStartRunsWarmUpImmediately(t *testing.T) {
	directory := &fakeSchedulerDirectory{warmUpCh: make(chan struct{}, 1)}
	scheduler := NewProxyClearanceScheduler(directory, SchedulerOptions{
		Config: fakeSchedulerConfig{interval: 3600},
	})

	scheduler.Start(context.Background())
	t.Cleanup(scheduler.Stop)
	select {
	case <-directory.warmUpCh:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not warm up immediately after Start")
	}
	scheduler.Stop()
}

func TestProxyClearanceSchedulerWarmUpMatchesPython(t *testing.T) {
	directory := &fakeSchedulerDirectory{}
	scheduler := NewProxyClearanceScheduler(directory)

	scheduler.warmUp(context.Background())
	if directory.loadCalls != 1 || directory.warmUpCalls != 1 || directory.refreshCalls != 0 {
		t.Fatalf("warmUp calls load=%d warmUp=%d refresh=%d", directory.loadCalls, directory.warmUpCalls, directory.refreshCalls)
	}
}

func TestProxyClearanceSchedulerWarmUpSwallowsErrors(t *testing.T) {
	directory := &fakeSchedulerDirectory{loadErr: errors.New("load failed")}
	scheduler := NewProxyClearanceScheduler(directory)

	scheduler.warmUp(context.Background())
	if directory.loadCalls != 1 || directory.warmUpCalls != 0 {
		t.Fatalf("warmUp with load error calls load=%d warmUp=%d", directory.loadCalls, directory.warmUpCalls)
	}
}

func TestProxyClearanceSchedulerRefreshMatchesPython(t *testing.T) {
	directory := &fakeSchedulerDirectory{}
	scheduler := NewProxyClearanceScheduler(directory)

	scheduler.refresh(context.Background())
	if directory.loadCalls != 1 || directory.refreshCalls != 1 || directory.warmUpCalls != 0 {
		t.Fatalf("refresh calls load=%d refresh=%d warmUp=%d", directory.loadCalls, directory.refreshCalls, directory.warmUpCalls)
	}
}

func TestProxyClearanceSchedulerRefreshSwallowsErrors(t *testing.T) {
	directory := &fakeSchedulerDirectory{refreshErr: errors.New("refresh failed")}
	scheduler := NewProxyClearanceScheduler(directory)

	scheduler.refresh(context.Background())
	if directory.loadCalls != 1 || directory.refreshCalls != 1 {
		t.Fatalf("refresh with error calls load=%d refresh=%d", directory.loadCalls, directory.refreshCalls)
	}
}

func TestProxyClearanceSchedulerIntervalMatchesPython(t *testing.T) {
	if got := NewProxyClearanceScheduler(&fakeSchedulerDirectory{}).interval(); got != 600 {
		t.Fatalf("default interval = %d, want 600", got)
	}
	scheduler := NewProxyClearanceScheduler(&fakeSchedulerDirectory{}, SchedulerOptions{
		Config: fakeSchedulerConfig{interval: 45},
	})
	if got := scheduler.interval(); got != 45 {
		t.Fatalf("configured interval = %d, want 45", got)
	}
}

func TestProxyClearanceSchedulerNilConfigUsesGlobalConfig(t *testing.T) {
	defaultsPath := filepath.Join(t.TempDir(), "config.defaults.toml")
	if err := os.WriteFile(defaultsPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write defaults: %v", err)
	}
	previous := platformconfig.GlobalConfig
	t.Cleanup(func() {
		platformconfig.GlobalConfig = previous
	})
	platformconfig.GlobalConfig = platformconfig.NewConfigSnapshot(fakeGlobalConfigBackend{
		data: map[string]any{
			"proxy": map[string]any{
				"clearance": map[string]any{
					"refresh_interval": 123,
				},
			},
		},
	}, platformconfig.ConfigSnapshotOptions{})
	if err := platformconfig.GlobalConfig.Load(context.Background(), defaultsPath); err != nil {
		t.Fatalf("load global config: %v", err)
	}

	scheduler := NewProxyClearanceScheduler(&fakeSchedulerDirectory{})
	if got := scheduler.interval(); got != 123 {
		t.Fatalf("global interval = %d, want 123", got)
	}
}

func TestSchedulerRefreshSkipsCooldownKeys(t *testing.T) {
	var now int64 = 1_000_000
	cold := BundleKey{Affinity: "direct", ClearanceHost: "grok.com"}
	warm := BundleKey{Affinity: "p1", ClearanceHost: "grok.com"}
	directory := &fakeSchedulerDirectory{
		backoffUntil: map[BundleKey]int64{cold: now + 5_000, warm: now - 1},
	}
	scheduler := NewProxyClearanceScheduler(directory, SchedulerOptions{
		Config: fakeSchedulerConfig{interval: 600},
		Clock:  func() int64 { return now },
	})

	scheduler.refresh(context.Background())
	if directory.skipSeen == nil {
		t.Fatal("refresh did not pass a skip set")
	}
	if !directory.skipSeen[cold] {
		t.Fatal("active-cooldown key should be skipped")
	}
	if directory.skipSeen[warm] {
		t.Fatal("expired-cooldown key should not be skipped")
	}
}

func TestSchedulerAdaptiveInterval(t *testing.T) {
	var now int64 = 1_000_000
	mk := func(n int) BundleKey { return BundleKey{Affinity: string(rune('a' + n)), ClearanceHost: "grok.com"} }

	// half-open present → base/4 (min 30).
	halfOpen := &fakeSchedulerDirectory{
		failures: map[BundleKey]int{mk(0): 5},
		bundles:  map[BundleKey]ClearanceBundle{mk(0): {}},
	}
	s1 := NewProxyClearanceScheduler(halfOpen, SchedulerOptions{
		Config: fakeSchedulerConfig{interval: 600}, Clock: func() int64 { return now },
	})
	if got := s1.adaptiveInterval(); got != 150 {
		t.Fatalf("half-open interval = %d, want 150 (600/4)", got)
	}

	// >half the bundles cooling down → base/2 (min 60).
	manyCold := &fakeSchedulerDirectory{
		backoffUntil: map[BundleKey]int64{mk(0): now + 5_000, mk(1): now + 5_000},
		bundles:      map[BundleKey]ClearanceBundle{mk(0): {}, mk(1): {}, mk(2): {}},
	}
	s2 := NewProxyClearanceScheduler(manyCold, SchedulerOptions{
		Config: fakeSchedulerConfig{interval: 600}, Clock: func() int64 { return now },
	})
	if got := s2.adaptiveInterval(); got != 300 {
		t.Fatalf("many-cold interval = %d, want 300 (600/2)", got)
	}

	// Healthy → base.
	healthy := &fakeSchedulerDirectory{bundles: map[BundleKey]ClearanceBundle{mk(0): {}}}
	s3 := NewProxyClearanceScheduler(healthy, SchedulerOptions{
		Config: fakeSchedulerConfig{interval: 600}, Clock: func() int64 { return now },
	})
	if got := s3.adaptiveInterval(); got != 600 {
		t.Fatalf("healthy interval = %d, want 600", got)
	}
}
