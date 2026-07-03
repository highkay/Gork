package app

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	accountdataplane "github.com/dslzl/gork/app/dataplane/account"
	platformconfig "github.com/dslzl/gork/app/platform/config"
	platformruntime "github.com/dslzl/gork/app/platform/runtime"
)

type appMainLifecycleState struct {
	runtimeStore *platformruntime.RedisRuntimeStore
	repository   accountcontrol.AccountRepository
	directory    *accountdataplane.AccountDirectory
	schedulerKey appMainSchedulerLease
	adminCleanup func()
}

type appMainSchedulerLease interface {
	Renew(context.Context) (bool, error)
	Release(context.Context) (bool, error)
}

type appMainLifecycleStep func(context.Context, *appMainLifecycleState) (Hook, error)

type appMainLifecycleBuilderOptions struct {
	ensureConfig func(context.Context) error
	setupLogging func() error
	steps        []appMainLifecycleStep
}

type appMainLifecycleBuilder struct {
	options  appMainLifecycleBuilderOptions
	state    *appMainLifecycleState
	cleanups []Hook
}

var (
	appMainRuntimeClientFactory       platformruntime.RedisRuntimeClientFactory
	appMainStartRuntimeStore          appMainLifecycleStep                = defaultAppMainStartRuntimeStore
	appMainConfigureTaskSnapshotStore appMainLifecycleStep                = defaultAppMainConfigureTaskSnapshotStore
	appMainInitializeRepository       appMainLifecycleStep                = defaultAppMainInitializeRepository
	appMainRunStartupMigrations       appMainLifecycleStep                = defaultAppMainRunStartupMigrations
	appMainReconcileLocalMediaCache   appMainLifecycleStep                = defaultAppMainReconcileLocalMediaCache
	appMainStartAccountDirectory      appMainLifecycleStep                = defaultAppMainStartAccountDirectory
	appMainStartRefreshRuntime        appMainLifecycleStep                = defaultAppMainStartRefreshRuntime
	appMainStartProxyScheduler        appMainLifecycleStep                = defaultAppMainStartProxyScheduler
	appMainAcquireSchedulerFileLock   func(context.Context) (Hook, error) = acquireAppMainSchedulerFileLock
	appMainConsoleResetInterval                                           = 30 * time.Second
)

func defaultLifecycleHooks() ([]Hook, []Hook) {
	return newAppMainLifecycleBuilder(appMainLifecycleBuilderOptions{
		ensureConfig: appMainEnsureConfig,
		setupLogging: appMainSetupLogging,
		steps: []appMainLifecycleStep{
			appMainStartRuntimeStore,
			appMainConfigureTaskSnapshotStore,
			appMainInitializeRepository,
			appMainRunStartupMigrations,
			appMainReconcileLocalMediaCache,
			appMainStartAccountDirectory,
			appMainStartRefreshRuntime,
			appMainStartProxyScheduler,
		},
	}).build()
}

func newAppMainLifecycleBuilder(options appMainLifecycleBuilderOptions) *appMainLifecycleBuilder {
	return &appMainLifecycleBuilder{
		options: options,
		state:   &appMainLifecycleState{},
	}
}

func (b *appMainLifecycleBuilder) build() ([]Hook, []Hook) {
	startupHooks := []Hook{
		func(ctx context.Context) error { return b.options.ensureConfig(ctx) },
		func(context.Context) error { return b.options.setupLogging() },
	}
	for _, step := range b.options.steps {
		startupHooks = append(startupHooks, b.stepHook(step))
	}
	return startupHooks, []Hook{b.shutdownHook()}
}

func (b *appMainLifecycleBuilder) stepHook(step appMainLifecycleStep) Hook {
	return func(ctx context.Context) error {
		cleanup, err := step(ctx, b.state)
		if err != nil {
			return err
		}
		if cleanup != nil {
			b.cleanups = append(b.cleanups, cleanup)
		}
		return nil
	}
}

func (b *appMainLifecycleBuilder) shutdownHook() Hook {
	return func(ctx context.Context) error {
		defer func() {
			b.cleanups = nil
		}()
		for i := len(b.cleanups) - 1; i >= 0; i-- {
			if err := b.cleanups[i](ctx); err != nil {
				return err
			}
		}
		return nil
	}
}

func appMainEnv() map[string]string {
	env := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}

func appMainEnvInt(key string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func appMainConfigDurationSeconds(key string, defaultSeconds int) time.Duration {
	seconds := platformconfig.GlobalConfig.GetInt(key, defaultSeconds)
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
