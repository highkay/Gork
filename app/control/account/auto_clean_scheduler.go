package account

import (
	"context"
	"sync"
	"time"
)

// AutoCleanScheduler 周期性执行过期账号硬删；默认关闭，启用后首 tick 等待一个 interval。
type AutoCleanScheduler struct {
	repo AccountRepository

	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
	wg      sync.WaitGroup
}

var autoCleanSchedulerSingleton *AutoCleanScheduler

// NewAutoCleanScheduler 创建调度器（未 Start）。
func NewAutoCleanScheduler(repo AccountRepository) *AutoCleanScheduler {
	return &AutoCleanScheduler{repo: repo}
}

// BindRepository 热替换仓储。
func (s *AutoCleanScheduler) BindRepository(repo AccountRepository) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repo = repo
}

// IsRunning 是否在跑。
func (s *AutoCleanScheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Start 启动循环；启用后先 sleep 一个 interval，再执行（与上游“enable 不立刻删”一致）。
func (s *AutoCleanScheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.loop(ctx)
	}()
}

// Stop 停止并等待 in-flight。
func (s *AutoCleanScheduler) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.running = false
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

func (s *AutoCleanScheduler) loop(ctx context.Context) {
	for {
		cfg := loadAutoCleanConfig()
		if !cfg.Enabled {
			// 配置关闭时慢轮询，避免热开后长时间不响应。
			timer := time.NewTimer(time.Minute)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				continue
			}
		}
		// 首 tick / 每 tick：先等 interval 再删（enable 后不立即 hard-delete）。
		timer := time.NewTimer(cfg.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.mu.Lock()
			repo := s.repo
			s.mu.Unlock()
			cfg := loadAutoCleanConfig()
			_, _ = RunExpiredAccountAutoClean(ctx, repo, cfg)
			// Build 过期账号：与 SSO 池共用开关与年龄策略。
			if buildStore := getBuildAutoCleanStore(); buildStore != nil {
				_, _ = RunBuildExpiredAccountAutoClean(ctx, buildStore, cfg)
			}
		}
	}
}

// GetAutoCleanScheduler 单例；首次调用绑定 repo。
func GetAutoCleanScheduler(repo AccountRepository) *AutoCleanScheduler {
	if autoCleanSchedulerSingleton == nil {
		autoCleanSchedulerSingleton = NewAutoCleanScheduler(repo)
	} else if repo != nil {
		autoCleanSchedulerSingleton.BindRepository(repo)
	}
	return autoCleanSchedulerSingleton
}
