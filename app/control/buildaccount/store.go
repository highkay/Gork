package buildaccount

import (
	"context"
	"time"

	"github.com/dslzl/gork/app/dataplane/build"
)

// Store 是 Build 账号仓储接口（B-a 仅 SQLite 实现）。
type Store interface {
	Initialize(ctx context.Context) error
	Upsert(ctx context.Context, account Account) (Account, error)
	Get(ctx context.Context, id int64) (Account, error)
	GetByUserID(ctx context.Context, userID string) (Account, error)
	List(ctx context.Context) ([]Account, error)
	ListActive(ctx context.Context, now time.Time) ([]Account, error)
	UpdateTokens(ctx context.Context, id int64, access, refresh string, expiresAt time.Time) error
	UpdateBilling(ctx context.Context, id int64, billing build.Billing) error
	SetStatus(ctx context.Context, id int64, status string, reason string) error
	// SetStatusUntil 在 status=cooling 时写入 cooling_until；until 为零则默认 2h。
	// 可选：未实现时调用方回退 SetStatus。
	SetStatusUntil(ctx context.Context, id int64, status string, reason string, until time.Time) error
	Delete(ctx context.Context, id int64) error
	Close() error
}
