package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/platform"
	platformconfig "github.com/dslzl/gork/app/platform/config"
	"github.com/dslzl/gork/app/platform/security"
	openaiproduct "github.com/dslzl/gork/app/products/openai"
	adminproduct "github.com/dslzl/gork/app/products/web/admin"
)

// buildAccountsDBFile 与 data 目录并列的独立 SQLite（不进 SSO accounts）。
const buildAccountsDBFile = "build_accounts.db"

// defaultAppMainInitializeBuildAccountStore 打开 Build 账号库并注入 openai 产品选号目录。
// 关 features.build_provider 时仍挂载空池：请求路径被门闸拦住，B-c admin 可先导入。
// 若 data 目录不可写且功能关闭，则降级为不挂载，避免阻塞主流程启动。
func defaultAppMainInitializeBuildAccountStore(ctx context.Context, state *appMainLifecycleState) (Hook, error) {
	cipher, err := security.OpenCipher(platformconfig.GlobalConfig.GetStr("security.credential_encryption_key", ""))
	if err != nil {
		return nil, fmt.Errorf("build account cipher: %w", err)
	}
	path := platform.DataPath(buildAccountsDBFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		if !platformconfig.GlobalConfig.GetBool("features.build_provider", false) {
			slog.Warn("build account store: data dir unavailable, skipping", "path", path, "err", err)
			return nil, nil
		}
		return nil, fmt.Errorf("build account store mkdir: %w", err)
	}
	store := buildaccount.NewSQLiteStore(path, cipher)
	if err := store.Initialize(ctx); err != nil {
		_ = store.Close()
		if !platformconfig.GlobalConfig.GetBool("features.build_provider", false) {
			slog.Warn("build account store: initialize failed, skipping", "path", path, "err", err)
			return nil, nil
		}
		return nil, fmt.Errorf("build account store: %w", err)
	}
	state.buildAccountStore = store
	openaiproduct.SetBuildAccountDirectory(store)
	restoreAdmin := adminproduct.SetBuildAccountStore(store)
	// 过期 Build 账号纳入 auto_clean（与 SSO 共用开关，默认关闭）。
	accountcontrol.SetBuildAutoCleanStore(buildaccount.AutoCleanAdapter{Store: store})
	return func(context.Context) error {
		accountcontrol.SetBuildAutoCleanStore(nil)
		restoreAdmin()
		openaiproduct.SetBuildAccountDirectory(nil)
		state.buildAccountStore = nil
		return store.Close()
	}, nil
}
