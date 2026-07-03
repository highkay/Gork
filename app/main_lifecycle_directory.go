package app

import (
	"context"
	"time"

	accountdataplane "github.com/dslzl/gork/app/dataplane/account"
)

func defaultAppMainStartAccountDirectory(ctx context.Context, state *appMainLifecycleState) (Hook, error) {
	if state.repository == nil {
		return nil, nil
	}
	directory := accountdataplane.NewAccountDirectory(state.repository)
	state.directory = directory
	state.bindAdminRuntime()
	restoreDirectory := accountdataplane.RegisterAccountDirectory(directory)

	syncCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = directory.Bootstrap(syncCtx)
		appMainAccountDirectorySyncLoop(syncCtx, directory)
	}()
	return func(context.Context) error {
		cancel()
		<-done
		restoreDirectory()
		return nil
	}, nil
}

func appMainAccountDirectorySyncLoop(ctx context.Context, directory *accountdataplane.AccountDirectory) {
	idleInterval := appMainEnvInt("ACCOUNT_SYNC_INTERVAL", 30)
	activeInterval := appMainEnvInt("ACCOUNT_SYNC_ACTIVE_INTERVAL", 3)
	const idleAfter = 5
	idleStreak := 0
	for {
		interval := idleInterval
		if idleStreak < idleAfter {
			interval = activeInterval
		}
		timer := time.NewTimer(time.Duration(interval) * time.Second)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}
		changed, err := directory.SyncIfChanged(ctx)
		if err != nil {
			idleStreak = idleAfter
			continue
		}
		if changed {
			idleStreak = 0
		} else if idleStreak < idleAfter {
			idleStreak++
		}
	}
}
