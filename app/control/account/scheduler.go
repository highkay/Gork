package account

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/dslzl/gork/app/platform/config"
	"github.com/dslzl/gork/app/platform/logging"
)

var accountRefreshPoolOrder = []string{"basic", "super", "heavy"}

var accountRefreshDefaultIntervals = map[string]time.Duration{
	"basic": 24 * time.Hour,
	"super": 2 * time.Hour,
	"heavy": 2 * time.Hour,
}

var accountRefreshIntervalConfigKeys = map[string]string{
	"basic": "account.refresh.basic_interval_sec",
	"super": "account.refresh.super_interval_sec",
	"heavy": "account.refresh.heavy_interval_sec",
}

var accountRefreshIntervalSeconds = func(pool string, defaultSeconds int) int {
	if key, ok := accountRefreshIntervalConfigKeys[pool]; ok {
		return config.GlobalConfig.GetInt(key, defaultSeconds)
	}
	return defaultSeconds
}

var accountRefreshRunOnStart = func() bool {
	return config.GlobalConfig.GetBool("account.refresh.run_on_start", true)
}

var accountRefreshJitterDuration = func(_ string, interval time.Duration) time.Duration {
	ratio := config.GlobalConfig.GetFloat("account.refresh.jitter_ratio", 0.1)
	if ratio <= 0 || interval <= 0 {
		return 0
	}
	maxJitter := time.Duration(float64(interval) * ratio)
	if maxJitter <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(maxJitter) + 1))
}

var accountRefreshNow = time.Now

type accountScheduledRefresher interface {
	RefreshScheduled(context.Context, *string) (RefreshResult, error)
}

type AccountRefreshPoolStatus struct {
	Pool                string
	LastStartedAt       time.Time
	LastFinishedAt      time.Time
	LastError           string
	LastResult          RefreshResult
	ConsecutiveFailures int
	NextRunAfter        time.Duration
	NextRunAt           time.Time
}

type AccountRefreshSchedulerStatus struct {
	Running bool
	Pools   map[string]AccountRefreshPoolStatus
}

type AccountRefreshScheduler struct {
	service      accountScheduledRefresher
	intervals    map[string]time.Duration
	cancelByPool map[string]context.CancelFunc
	statusByPool map[string]AccountRefreshPoolStatus
	mu           sync.Mutex
}

var accountRefreshSchedulerSingleton *AccountRefreshScheduler

func NewAccountRefreshScheduler(refreshService accountScheduledRefresher) *AccountRefreshScheduler {
	return &AccountRefreshScheduler{
		service:      refreshService,
		intervals:    map[string]time.Duration{},
		cancelByPool: map[string]context.CancelFunc{},
		statusByPool: map[string]AccountRefreshPoolStatus{},
	}
}

func (s *AccountRefreshScheduler) BindService(refreshService accountScheduledRefresher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.service = refreshService
}

func (s *AccountRefreshScheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cancelByPool) > 0
}

func (s *AccountRefreshScheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cancelByPool) > 0 {
		return
	}
	runOnStart := accountRefreshRunOnStart()
	for _, pool := range accountRefreshPoolOrder {
		ctx, cancel := context.WithCancel(context.Background())
		s.cancelByPool[pool] = cancel
		s.recordNextRunLocked(pool, s.nextDelayLocked(pool))
		go s.loop(ctx, pool, runOnStart)
	}
}

func (s *AccountRefreshScheduler) Stop() {
	s.mu.Lock()
	cancels := s.cancelByPool
	s.cancelByPool = map[string]context.CancelFunc{}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (s *AccountRefreshScheduler) loop(ctx context.Context, pool string, runOnStart bool) {
	if runOnStart {
		s.refreshPool(ctx, pool)
	}
	for {
		delay := s.nextDelay(pool)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.refreshPool(ctx, pool)
		}
	}
}

func (s *AccountRefreshScheduler) refreshPool(ctx context.Context, pool string) {
	s.mu.Lock()
	service := s.service
	s.recordStartedLocked(pool)
	s.mu.Unlock()
	if service == nil {
		s.recordFinished(pool, RefreshResult{}, nil)
		return
	}
	result, err := service.RefreshScheduled(ctx, &pool)
	if err != nil {
		logging.Logger.Warn("account scheduled refresh failed", "pool", pool, "error", err)
	}
	s.recordFinished(pool, result, err)
}

func (s *AccountRefreshScheduler) Status() AccountRefreshSchedulerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := AccountRefreshSchedulerStatus{
		Running: len(s.cancelByPool) > 0,
		Pools:   map[string]AccountRefreshPoolStatus{},
	}
	for _, pool := range accountRefreshPoolOrder {
		status.Pools[pool] = s.statusLocked(pool)
	}
	for pool, poolStatus := range s.statusByPool {
		if _, ok := status.Pools[pool]; !ok {
			status.Pools[pool] = poolStatus
		}
	}
	return status
}

func (s *AccountRefreshScheduler) nextDelay(pool string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	delay := s.nextDelayLocked(pool)
	s.recordNextRunLocked(pool, delay)
	return delay
}

func (s *AccountRefreshScheduler) nextDelayLocked(pool string) time.Duration {
	interval := accountRefreshInterval(pool, s.intervals)
	return interval + accountRefreshJitterDuration(pool, interval)
}

func (s *AccountRefreshScheduler) recordNextRunLocked(pool string, delay time.Duration) {
	status := s.statusLocked(pool)
	status.NextRunAfter = delay
	status.NextRunAt = accountRefreshNow().Add(delay)
	s.statusByPool[pool] = status
}

func (s *AccountRefreshScheduler) recordStartedLocked(pool string) {
	status := s.statusLocked(pool)
	status.Pool = pool
	status.LastStartedAt = accountRefreshNow()
	s.statusByPool[pool] = status
}

func (s *AccountRefreshScheduler) recordFinished(pool string, result RefreshResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.statusLocked(pool)
	status.LastFinishedAt = accountRefreshNow()
	status.LastResult = result
	if err != nil {
		status.LastError = err.Error()
		status.ConsecutiveFailures++
	} else {
		status.LastError = ""
		status.ConsecutiveFailures = 0
	}
	s.statusByPool[pool] = status
}

func (s *AccountRefreshScheduler) statusLocked(pool string) AccountRefreshPoolStatus {
	if status, ok := s.statusByPool[pool]; ok {
		status.Pool = pool
		return status
	}
	return AccountRefreshPoolStatus{Pool: pool}
}

func GetAccountRefreshScheduler(refreshService accountScheduledRefresher) *AccountRefreshScheduler {
	if accountRefreshSchedulerSingleton == nil {
		accountRefreshSchedulerSingleton = NewAccountRefreshScheduler(refreshService)
	} else {
		accountRefreshSchedulerSingleton.BindService(refreshService)
	}
	return accountRefreshSchedulerSingleton
}

func accountRefreshInterval(pool string, overrides map[string]time.Duration) time.Duration {
	if overrides != nil {
		if interval, ok := overrides[pool]; ok {
			return interval
		}
	}
	if interval, ok := accountRefreshDefaultIntervals[pool]; ok {
		return time.Duration(accountRefreshIntervalSeconds(pool, int(interval/time.Second))) * time.Second
	}
	fallback := accountRefreshDefaultIntervals["basic"]
	return time.Duration(accountRefreshIntervalSeconds("basic", int(fallback/time.Second))) * time.Second
}
