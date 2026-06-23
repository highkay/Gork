package proxy

import (
	"context"
	"sync"
	"time"

	platformconfig "github.com/dslzl/gork/app/platform/config"
	"github.com/dslzl/gork/app/platform/logging"
)

type ProxyClearanceDirectory interface {
	Load(ctx context.Context) error
	WarmUp(ctx context.Context) error
	RefreshClearanceSafe(ctx context.Context) error
}

type SchedulerConfig interface {
	GetInt(key string, defaultValue int) int
}

type SchedulerOptions struct {
	Config SchedulerConfig
}

type globalSchedulerConfig struct{}

func (globalSchedulerConfig) GetInt(key string, defaultValue int) int {
	return platformconfig.GlobalConfig.GetInt(key, defaultValue)
}

type ProxyClearanceScheduler struct {
	directory ProxyClearanceDirectory
	config    SchedulerConfig

	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
	status  ProxyClearanceSchedulerStatus
}

type ProxyClearanceSchedulerStatus struct {
	Running             bool
	LastOperation       string
	LastStartedAt       time.Time
	LastFinishedAt      time.Time
	LastError           string
	ConsecutiveFailures int
	NextRunAfter        time.Duration
	NextRunAt           time.Time
}

func NewProxyClearanceScheduler(directory ProxyClearanceDirectory, options ...SchedulerOptions) *ProxyClearanceScheduler {
	scheduler := &ProxyClearanceScheduler{directory: directory}
	if len(options) > 0 {
		scheduler.config = options[0].Config
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
	s.status.Running = true
	go s.loop(loopCtx)
}

func (s *ProxyClearanceScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.status.Running = false
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func (s *ProxyClearanceScheduler) loop(ctx context.Context) {
	s.warmUp(ctx)
	for s.isRunning() {
		delay := time.Duration(s.interval()) * time.Second
		s.recordNextRun(delay)
		timer := time.NewTimer(delay)
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
	s.recordStarted("warm_up")
	if err := s.directory.Load(ctx); err != nil {
		s.recordFinished("warm_up", err)
		return
	}
	s.recordFinished("warm_up", s.directory.WarmUp(ctx))
}

func (s *ProxyClearanceScheduler) refresh(ctx context.Context) {
	s.recordStarted("refresh")
	if err := s.directory.Load(ctx); err != nil {
		s.recordFinished("refresh", err)
		return
	}
	s.recordFinished("refresh", s.directory.RefreshClearanceSafe(ctx))
}

func (s *ProxyClearanceScheduler) interval() int {
	cfg := s.config
	if cfg == nil {
		cfg = globalSchedulerConfig{}
	}
	return cfg.GetInt("proxy.clearance.refresh_interval", 600)
}

func (s *ProxyClearanceScheduler) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *ProxyClearanceScheduler) Status() ProxyClearanceSchedulerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	status.Running = s.running
	return status
}

func (s *ProxyClearanceScheduler) recordStarted(operation string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.LastOperation = operation
	s.status.LastStartedAt = time.Now()
}

func (s *ProxyClearanceScheduler) recordFinished(operation string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.LastOperation = operation
	s.status.LastFinishedAt = time.Now()
	if err != nil {
		s.status.LastError = err.Error()
		s.status.ConsecutiveFailures++
		logging.Logger.Warn("proxy clearance scheduler failed", "operation", operation, "error", err)
		return
	}
	s.status.LastError = ""
	s.status.ConsecutiveFailures = 0
}

func (s *ProxyClearanceScheduler) recordNextRun(delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.NextRunAfter = delay
	s.status.NextRunAt = time.Now().Add(delay)
}
