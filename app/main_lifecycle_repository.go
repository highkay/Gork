package app

import (
	"context"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	accountbackends "github.com/dslzl/gork/app/control/account/backends"
	platformstorage "github.com/dslzl/gork/app/platform/storage"
	adminproduct "github.com/dslzl/gork/app/products/web/admin"
)

func defaultAppMainInitializeRepository(ctx context.Context, state *appMainLifecycleState) (Hook, error) {
	repo, err := accountbackends.CreateRepository(appMainEnv(), accountbackends.RepositoryConstructors{})
	if err != nil {
		return nil, err
	}
	if err := repo.Initialize(ctx); err != nil {
		_ = repo.Close(ctx)
		return nil, err
	}
	state.repository = repo
	state.bindAdminRuntime()
	return func(ctx context.Context) error {
		state.clearAdminRuntime()
		return repo.Close(ctx)
	}, nil
}

func defaultAppMainRunStartupMigrations(ctx context.Context, state *appMainLifecycleState) (Hook, error) {
	cleanup, err := runAppMainStartupMigrations(ctx, state)
	if err != nil {
		return nil, err
	}
	if err := appMainLoadRequestConfig(ctx); err != nil {
		return nil, err
	}
	return cleanup, nil
}

func defaultAppMainReconcileLocalMediaCache(context.Context, *appMainLifecycleState) (Hook, error) {
	return nil, platformstorage.ReconcileLocalMediaCache()
}

func (state *appMainLifecycleState) bindAdminRuntime() {
	state.bindAdminRuntimeWithRefresh(nil)
}

func (state *appMainLifecycleState) bindAdminRuntimeWithRefresh(service *accountcontrol.AccountRefreshService) {
	state.clearAdminRuntime()
	state.adminCleanup = adminproduct.BindAccountRuntime(state.repository, state.directory, service)
}

func (state *appMainLifecycleState) clearAdminRuntime() {
	if state.adminCleanup == nil {
		return
	}
	state.adminCleanup()
	state.adminCleanup = nil
}
