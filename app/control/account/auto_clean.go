package account

import (
	"context"
	"log/slog"
	"sync"
	"time"

	platformconfig "github.com/dslzl/gork/app/platform/config"
)

// 对齐 chenyme/grok2api reauth auto-clean（默认关闭）：
// 周期性硬删已标记 expired 且超过 min_age 的账号。
// 冷却中、仍 active 的账号永不选中。

const (
	autoCleanDefaultIntervalSec = 3600
	autoCleanDefaultMinAgeSec   = 86400
	autoCleanDefaultMaxDeletes  = 50
	autoCleanMaxPageSize        = 100
	autoCleanMaxPages           = 20
	autoCleanRunTimeout         = 4 * time.Minute
)

// AutoCleanConfig 过期账号自动清理策略。
type AutoCleanConfig struct {
	Enabled         bool
	Interval        time.Duration
	MinAge          time.Duration
	IncludeDisabled bool
	MaxDeletes      int
}

func autoCleanEnabled() bool {
	return platformconfig.GlobalConfig.GetBool("account.auto_clean.enabled", false)
}

func loadAutoCleanConfig() AutoCleanConfig {
	intervalSec := platformconfig.GlobalConfig.GetInt("account.auto_clean.interval_sec", autoCleanDefaultIntervalSec)
	minAgeSec := platformconfig.GlobalConfig.GetInt("account.auto_clean.min_age_sec", autoCleanDefaultMinAgeSec)
	maxDeletes := platformconfig.GlobalConfig.GetInt("account.auto_clean.max_deletes_per_tick", autoCleanDefaultMaxDeletes)
	cfg := AutoCleanConfig{
		Enabled:         autoCleanEnabled(),
		Interval:        time.Duration(intervalSec) * time.Second,
		MinAge:          time.Duration(minAgeSec) * time.Second,
		IncludeDisabled: platformconfig.GlobalConfig.GetBool("account.auto_clean.include_disabled", false),
		MaxDeletes:      maxDeletes,
	}
	return normalizeAutoCleanConfig(cfg)
}

func normalizeAutoCleanConfig(value AutoCleanConfig) AutoCleanConfig {
	if value.Interval < time.Minute {
		value.Interval = time.Minute
	}
	if value.Interval > 24*time.Hour {
		value.Interval = 24 * time.Hour
	}
	if value.MinAge < time.Minute {
		value.MinAge = time.Minute
	}
	if value.MinAge > 30*24*time.Hour {
		value.MinAge = 30 * 24 * time.Hour
	}
	if value.MaxDeletes < 1 {
		value.MaxDeletes = 1
	}
	if value.MaxDeletes > 500 {
		value.MaxDeletes = 500
	}
	return value
}

// AutoCleanResult 单次清理摘要。
type AutoCleanResult struct {
	Scanned int
	Deleted int
	Skipped int
}

// RunExpiredAccountAutoClean 删除 status=expired（可选 disabled）且 expired_at 超过 min_age 的账号。
// 仅当 cfg.Enabled 时执行；调用方负责调度与 leader 门禁。
func RunExpiredAccountAutoClean(ctx context.Context, repo AccountRepository, cfg AutoCleanConfig) (AutoCleanResult, error) {
	cfg = normalizeAutoCleanConfig(cfg)
	var result AutoCleanResult
	if !cfg.Enabled || repo == nil {
		return result, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, autoCleanRunTimeout)
	defer cancel()

	now := time.Now().UTC()
	// AccountRecord 时间戳与 Ext.expired_at 使用 Unix 毫秒（与 invalid_credentials 一致）。
	cutoff := now.Add(-cfg.MinAge).UnixMilli()
	statuses := []AccountStatus{AccountStatusExpired}
	if cfg.IncludeDisabled {
		statuses = append(statuses, AccountStatusDisabled)
	}

	for _, status := range statuses {
		if result.Deleted >= cfg.MaxDeletes {
			break
		}
		statusCopy := status
		page := 1
		for page <= autoCleanMaxPages && result.Deleted < cfg.MaxDeletes {
			query := ListAccountsQuery{
				Page:     page,
				PageSize: autoCleanMaxPageSize,
				Status:   &statusCopy,
				SortBy:   "updated_at",
				SortDesc: false,
			}
			query.Normalize()
			accountPage, err := repo.ListAccounts(runCtx, query)
			if err != nil {
				return result, err
			}
			if len(accountPage.Items) == 0 {
				break
			}
			var tokens []string
			for _, record := range accountPage.Items {
				result.Scanned++
				if !isAutoCleanCandidate(record, cutoff) {
					result.Skipped++
					continue
				}
				tokens = append(tokens, record.Token)
				if result.Deleted+len(tokens) >= cfg.MaxDeletes {
					break
				}
			}
			if len(tokens) > 0 {
				mutation, err := repo.DeleteAccounts(runCtx, tokens)
				if err != nil {
					return result, err
				}
				// mutation.Deleted 可能不存在；以请求长度保守计数
				deleted := len(tokens)
				if mutation.Deleted > 0 {
					deleted = mutation.Deleted
				}
				result.Deleted += deleted
				slog.Info("account_auto_clean",
					"status", string(status),
					"batch_deleted", deleted,
					"total_deleted", result.Deleted,
				)
			}
			if len(accountPage.Items) < autoCleanMaxPageSize {
				break
			}
			// 删除后同页会收缩；不自增 page 可扫完，但为避免死循环仍前进。
			page++
		}
	}
	return result, nil
}

func isAutoCleanCandidate(record AccountRecord, cutoffUnix int64) bool {
	if record.Status != AccountStatusExpired && record.Status != AccountStatusDisabled {
		return false
	}
	// 冷却中的账号跳过（不应出现在 expired 列表，但防御性检查）。
	if record.Status == AccountStatusCooling {
		return false
	}
	markedAt := expiredMarkedAtUnix(record)
	if markedAt <= 0 {
		// 无 expired_at 时回退 updated_at / last_fail_at，避免永久堆积。
		if record.UpdatedAt > 0 {
			markedAt = record.UpdatedAt
		} else if record.LastFailAt != nil {
			markedAt = *record.LastFailAt
		} else {
			return false
		}
	}
	return markedAt <= cutoffUnix
}

func expiredMarkedAtUnix(record AccountRecord) int64 {
	if record.Ext == nil {
		return 0
	}
	raw, ok := record.Ext[expiredAtKey]
	if !ok || raw == nil {
		// disabled 也可使用 disabled_at
		if record.Status == AccountStatusDisabled {
			raw, ok = record.Ext[disabledAtKey]
			if !ok || raw == nil {
				return 0
			}
		} else {
			return 0
		}
	}
	switch v := raw.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case jsonNumber:
		i, _ := v.Int64()
		return i
	default:
		return 0
	}
}

// jsonNumber 兼容 encoding/json.Number 而不强制 import 到签名。
type jsonNumber interface {
	Int64() (int64, error)
}

// BuildAccountAutoCleanStore Build 过期账号清理所需的最小仓储面。
type BuildAccountAutoCleanStore interface {
	List(ctx context.Context) ([]BuildAutoCleanAccount, error)
	Delete(ctx context.Context, id int64) error
}

// BuildAutoCleanAccount Build 池条目视图。
type BuildAutoCleanAccount struct {
	ID        int64
	Status    string
	UpdatedAt time.Time
}

var (
	buildAutoCleanMu    sync.Mutex
	buildAutoCleanStore BuildAccountAutoCleanStore
)

// SetBuildAutoCleanStore 注入 Build 账号库；传 nil 禁用 Build 侧清理。
func SetBuildAutoCleanStore(store BuildAccountAutoCleanStore) {
	buildAutoCleanMu.Lock()
	defer buildAutoCleanMu.Unlock()
	buildAutoCleanStore = store
}

// BuildAutoCleanStoreBound 是否已挂载 Build 清理仓储（Admin 状态展示）。
func BuildAutoCleanStoreBound() bool {
	return getBuildAutoCleanStore() != nil
}

func getBuildAutoCleanStore() BuildAccountAutoCleanStore {
	buildAutoCleanMu.Lock()
	defer buildAutoCleanMu.Unlock()
	return buildAutoCleanStore
}

// RunBuildExpiredAccountAutoClean 软删 Build 池中 status=expired 且 UpdatedAt 超过 min_age 的账号。
func RunBuildExpiredAccountAutoClean(ctx context.Context, store BuildAccountAutoCleanStore, cfg AutoCleanConfig) (AutoCleanResult, error) {
	cfg = normalizeAutoCleanConfig(cfg)
	var result AutoCleanResult
	if !cfg.Enabled || store == nil {
		return result, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, autoCleanRunTimeout)
	defer cancel()
	accounts, err := store.List(runCtx)
	if err != nil {
		return result, err
	}
	cutoff := time.Now().UTC().Add(-cfg.MinAge)
	var deleted int
	for _, acc := range accounts {
		result.Scanned++
		if acc.Status != "expired" && !(cfg.IncludeDisabled && acc.Status == "disabled") {
			result.Skipped++
			continue
		}
		if acc.UpdatedAt.IsZero() || acc.UpdatedAt.After(cutoff) {
			result.Skipped++
			continue
		}
		if deleted >= cfg.MaxDeletes {
			break
		}
		if err := store.Delete(runCtx, acc.ID); err != nil {
			return result, err
		}
		deleted++
		result.Deleted++
	}
	if result.Deleted > 0 {
		slog.Info("build_account_auto_clean", "deleted", result.Deleted, "scanned", result.Scanned)
	}
	return result, nil
}
