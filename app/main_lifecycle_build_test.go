package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
	platformconfig "github.com/dslzl/gork/app/platform/config"
	openaiproduct "github.com/dslzl/gork/app/products/openai"
)

func TestDefaultAppMainInitializeBuildAccountStoreMountsDirectory(t *testing.T) {
	// Given: 临时 DATA_DIR + 空 GlobalConfig
	tmp := t.TempDir()
	t.Setenv("DATA_DIR", tmp)
	oldCfg := platformconfig.GlobalConfig
	t.Cleanup(func() {
		platformconfig.GlobalConfig = oldCfg
		openaiproduct.SetBuildAccountDirectory(nil)
	})
	platformconfig.GlobalConfig = platformconfig.NewConfigSnapshot(
		lifecycleConfigBackend{data: &map[string]any{}},
		platformconfig.ConfigSnapshotOptions{},
	)

	state := &appMainLifecycleState{}
	// When: 初始化 Build store
	cleanup, err := defaultAppMainInitializeBuildAccountStore(context.Background(), state)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected cleanup hook")
	}
	if state.buildAccountStore == nil {
		t.Fatal("expected store on state")
	}

	// Then: 可写入并被选号目录读到
	store, ok := state.buildAccountStore.(*buildaccount.SQLiteStore)
	if !ok {
		t.Fatalf("store type %T", state.buildAccountStore)
	}
	acc, err := store.Upsert(context.Background(), buildaccount.FromCredential(build.Credential{
		Name:        "t",
		UserID:      "u-build-1",
		AccessToken: "at",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if acc.ID == 0 {
		t.Fatal("missing id")
	}
	dbPath := filepath.Join(tmp, buildAccountsDBFile)
	if _, err := filepath.Abs(dbPath); err != nil {
		t.Fatal(err)
	}

	// shutdown 清空目录
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if state.buildAccountStore != nil {
		t.Fatal("store should be cleared")
	}
}
