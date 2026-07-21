package build

import (
	"log/slog"
	"sync"
	"time"

	"github.com/dslzl/gork/app/dataplane/build/reasoningreplay"
	platformconfig "github.com/dslzl/gork/app/platform/config"
	platformruntime "github.com/dslzl/gork/app/platform/runtime"
)

// 默认推理回放：优先 Redis（RUNTIME_REDIS_URL），否则进程内有界内存。
// 对齐 chenyme reasoningreplay multi-instance backend。

var (
	replayMu      sync.Mutex
	defaultReplay *reasoningreplay.ReasoningReplay
	replayBackend string // "redis" | "memory"
)

// DefaultReasoningReplay 返回进程级回放门面（懒初始化）。
func DefaultReasoningReplay() *reasoningreplay.ReasoningReplay {
	replayMu.Lock()
	defer replayMu.Unlock()
	if defaultReplay == nil {
		defaultReplay, replayBackend = openDefaultReasoningReplay()
	}
	return defaultReplay
}

// ReasoningReplayBackend 返回当前后端名称（redis/memory），供 Admin 状态展示。
func ReasoningReplayBackend() string {
	_ = DefaultReasoningReplay()
	replayMu.Lock()
	defer replayMu.Unlock()
	if replayBackend == "" {
		return "memory"
	}
	return replayBackend
}

// SetReasoningReplay 测试/启动注入；传 nil 下次 Default 会重建。
func SetReasoningReplay(replay *reasoningreplay.ReasoningReplay) {
	replayMu.Lock()
	defer replayMu.Unlock()
	defaultReplay = replay
	if replay == nil {
		replayBackend = ""
	} else {
		replayBackend = "injected"
	}
}

// ReloadReasoningReplayConfig 热更新开关与 TTL（store 保留）。
func ReloadReasoningReplayConfig() {
	r := DefaultReasoningReplay()
	if r == nil {
		return
	}
	r.UpdateConfig(loadReasoningReplayConfig())
}

func loadReasoningReplayConfig() reasoningreplay.Config {
	ttlSec := platformconfig.GlobalConfig.GetInt("provider.build.reasoning_replay_ttl_sec", 3600)
	if ttlSec <= 0 {
		ttlSec = 3600
	}
	return reasoningreplay.Config{
		Enabled: platformconfig.GlobalConfig.GetBool("provider.build.reasoning_replay_enabled", true),
		TTL:     time.Duration(ttlSec) * time.Second,
	}
}

func openDefaultReasoningReplay() (*reasoningreplay.ReasoningReplay, string) {
	cfg := loadReasoningReplayConfig()
	maxEntries := platformconfig.GlobalConfig.GetInt("provider.build.reasoning_replay_max_entries", 10240)

	// 优先 Redis：与 runtime 协调共用 RUNTIME_REDIS_URL。
	if rawURL := platformruntime.RuntimeRedisURL(); rawURL != "" {
		store, err := reasoningreplay.OpenRedisStore(rawURL, "gork:reasoning-replay:")
		if err == nil {
			slog.Info("reasoning_replay_backend", "backend", "redis")
			return reasoningreplay.New(store, cfg, nil), "redis"
		}
		slog.Warn("reasoning_replay_redis_unavailable", "error", err.Error())
	}

	store := reasoningreplay.NewMemoryStore(maxEntries)
	slog.Info("reasoning_replay_backend", "backend", "memory")
	return reasoningreplay.New(store, cfg, nil), "memory"
}
