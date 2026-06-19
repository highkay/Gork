package proxy

import (
	"context"
	"sync"
	"time"

	platformconfig "github.com/dslzl/gork/app/platform/config"
	platformruntime "github.com/dslzl/gork/app/platform/runtime"
)

type ProxyClearanceDirectory interface {
	Load(ctx context.Context) error
	WarmUp(ctx context.Context) error
	RefreshClearanceSafe(ctx context.Context) error
	RefreshClearanceFiltered(ctx context.Context, skip map[BundleKey]bool) error
	RefreshBackoffUntil() map[BundleKey]int64
	FailureCounts() map[BundleKey]int
	Bundles() map[BundleKey]ClearanceBundle
}

type SchedulerConfig interface {
	GetInt(key string, defaultValue int) int
	GetFloat(key string, defaultValue float64) float64
	GetBool(key string, defaultValue bool) bool
}

type SchedulerOptions struct {
	Config SchedulerConfig
	Clock  func() int64
}

type globalSchedulerConfig struct{}

func (globalSchedulerConfig) GetInt(key string, defaultValue int) int {
	return platformconfig.GlobalConfig.GetInt(key, defaultValue)
}

func (globalSchedulerConfig) GetFloat(key string, defaultValue float64) float64 {
	return platformconfig.GlobalConfig.GetFloat(key, defaultValue)
}

func (globalSchedulerConfig) GetBool(key string, defaultValue bool) bool {
	return platformconfig.GlobalConfig.GetBool(key, defaultValue)
}

type ProxyClearanceScheduler struct {
	directory ProxyClearanceDirectory
	config    SchedulerConfig
	clock     func() int64

	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
}

func NewProxyClearanceScheduler(directory ProxyClearanceDirectory, options ...SchedulerOptions) *ProxyClearanceScheduler {
	scheduler := &ProxyClearanceScheduler{directory: directory}
	if len(options) > 0 {
		scheduler.config = options[0].Config
		scheduler.clock = options[0].Clock
	}
	if scheduler.clock == nil {
		scheduler.clock = platformruntime.NowMS
	}
	return scheduler
}

func (s *ProxyClearanceScheduler) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.running = true
	go s.loop(loopCtx)
}

func (s *ProxyClearanceScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func (s *ProxyClearanceScheduler) loop(ctx context.Context) {
	if s.startupWarmUpEnabled() {
		s.warmUp(ctx)
	}
	for s.isRunning() {
		timer := time.NewTimer(time.Duration(s.adaptiveInterval()) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if !s.isRunning() {
			return
		}
		s.refresh(ctx)
	}
}

func (s *ProxyClearanceScheduler) warmUp(ctx context.Context) {
	if err := s.directory.Load(ctx); err != nil {
		return
	}
	_ = s.directory.WarmUp(ctx)
}

func (s *ProxyClearanceScheduler) refresh(ctx context.Context) {
	if err := s.directory.Load(ctx); err != nil {
		return
	}
	skip := s.cooldownKeys()
	_ = s.directory.RefreshClearanceFiltered(ctx, skip)
}

// cooldownKeys returns keys whose cooldown has not yet expired; the scheduler
// skips them so it never wastes a FlareSolverr call on a key it knows is cold.
func (s *ProxyClearanceScheduler) cooldownKeys() map[BundleKey]bool {
	now := s.clock()
	out := map[BundleKey]bool{}
	for key, until := range s.directory.RefreshBackoffUntil() {
		if until > now {
			out[key] = true
		}
	}
	return out
}

func (s *ProxyClearanceScheduler) halfOpenKeys() map[BundleKey]bool {
	maxFails := maxInt(1, s.cfg().GetInt("proxy.clearance.max_consecutive_failures", 5))
	out := map[BundleKey]bool{}
	for key, count := range s.directory.FailureCounts() {
		if count >= maxFails {
			out[key] = true
		}
	}
	return out
}

// adaptiveInterval shortens the refresh cadence when keys are in half-open
// probe state, lengthens it when many keys are cooling down, otherwise uses the
// configured base. Mirrors cloudriver8 _get_adaptive_interval().
func (s *ProxyClearanceScheduler) adaptiveInterval() int {
	base := s.interval()
	now := s.clock()

	cooldownCount := 0
	for _, until := range s.directory.RefreshBackoffUntil() {
		if until > now {
			cooldownCount++
		}
	}
	halfOpen := s.halfOpenKeys()
	total := len(s.directory.Bundles())
	if total == 0 {
		total = 1
	}

	if len(halfOpen) > 0 {
		return maxInt(30, base/4)
	}
	if cooldownCount > total/2 {
		return maxInt(60, base/2)
	}
	return base
}

func (s *ProxyClearanceScheduler) interval() int {
	return s.cfg().GetInt("proxy.clearance.refresh_interval", 600)
}

func (s *ProxyClearanceScheduler) startupWarmUpEnabled() bool {
	return s.cfg().GetBool("proxy.clearance.startup_warmup", true)
}

func (s *ProxyClearanceScheduler) cfg() SchedulerConfig {
	if s.config != nil {
		return s.config
	}
	return globalSchedulerConfig{}
}

func (s *ProxyClearanceScheduler) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
