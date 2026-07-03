package app

import (
	"context"

	platformruntime "github.com/dslzl/gork/app/platform/runtime"
)

func defaultAppMainStartRuntimeStore(ctx context.Context, state *appMainLifecycleState) (Hook, error) {
	store, err := platformruntime.CreateRuntimeStoreFromEnv(appMainRuntimeClientFactory)
	if err != nil {
		return nil, err
	}
	state.runtimeStore = store
	if store == nil {
		return nil, nil
	}
	return func(ctx context.Context) error {
		return store.Close(ctx)
	}, nil
}

func defaultAppMainConfigureTaskSnapshotStore(_ context.Context, state *appMainLifecycleState) (Hook, error) {
	if state.runtimeStore == nil {
		platformruntime.SetTaskSnapshotStore(nil)
		return nil, nil
	}
	taskClient, ok := state.runtimeStore.Redis.(platformruntime.RedisTaskClient)
	if !ok {
		platformruntime.SetTaskSnapshotStore(nil)
		return nil, nil
	}
	ttlS := appMainEnvInt("RUNTIME_TASK_TTL_S", 300)
	if ttlS < 60 {
		ttlS = 60
	}
	platformruntime.SetTaskSnapshotStore(platformruntime.NewRedisTaskSnapshotStore(
		taskClient,
		platformruntime.RedisTaskSnapshotStoreOptions{TTLS: ttlS},
	))
	return func(context.Context) error {
		platformruntime.SetTaskSnapshotStore(nil)
		return nil
	}, nil
}
