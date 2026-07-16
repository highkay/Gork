package app

import (
	"context"
	"time"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	proxydataplane "github.com/dslzl/gork/app/dataplane/proxy"
	reversetransport "github.com/dslzl/gork/app/dataplane/reverse/transport"
	platformconfig "github.com/dslzl/gork/app/platform/config"
	platformruntime "github.com/dslzl/gork/app/platform/runtime"
	openaiproduct "github.com/dslzl/gork/app/products/openai"
)

func defaultAppMainStartRefreshRuntime(ctx context.Context, state *appMainLifecycleState) (Hook, error) {
	if state.repository == nil {
		return nil, nil
	}
	usageProxyRuntime, err := proxydataplane.GetTransportRuntime(ctx)
	if err != nil {
		return nil, err
	}
	service := accountcontrol.NewAccountRefreshService(state.repository, accountcontrol.AccountRefreshOptions{
		Fetcher:          reversetransport.UsageFetcher{ProxyRuntime: usageProxyRuntime},
		UsageConcurrency: platformconfig.GlobalConfig.GetInt("account.refresh.usage_concurrency", 50),
		PerTokenTimeout:  appMainConfigDurationSeconds("account.refresh.per_token_timeout_sec", 30),
		BatchTimeout:     appMainConfigDurationSeconds("account.refresh.batch_timeout_sec", 600),
		// Session probe is authoritative for SSO liveness (accounts.x.ai final URL).
		// ListModels remains available as optional secondary tooling, not the primary gate.
		SSOSessionProber: reversetransport.SSOSessionProber{ProxyRuntime: usageProxyRuntime},
		SSOModelVerifier: accountcontrol.SSOModelVerifierFunc(openaiproduct.ProbeConsoleListModels),
	})
	scheduler := accountcontrol.GetAccountRefreshScheduler(service)
	validationScheduler := accountcontrol.GetSSOValidationScheduler(service)
	leader := true
	var localLockCleanup Hook
	if state.runtimeStore != nil {
		lease, err := state.runtimeStore.AcquireLock(ctx, "scheduler-leader", platformruntime.RedisRuntimeLockOptions{
			TTLMS: appMainEnvInt("RUNTIME_REDIS_LOCK_TTL_MS", 300000),
		})
		if err != nil {
			localLockCleanup, err = appMainAcquireSchedulerFileLock(ctx)
			if err != nil {
				return nil, err
			}
			leader = localLockCleanup != nil
		} else {
			leader = lease != nil
			if leader {
				state.schedulerKey = lease
			}
		}
	} else {
		cleanup, err := appMainAcquireSchedulerFileLock(ctx)
		if err != nil {
			return nil, err
		}
		localLockCleanup = cleanup
		leader = localLockCleanup != nil
	}
	state.bindAdminRuntimeWithRefresh(service)
	consoleResetCleanup := appMainStartConsoleQuotaResetLoop(service, appMainConsoleResetInterval)
	leaseRenewalCleanup := Hook(nil)
	accountcontrol.SetRefreshService(service)
	accountcontrol.SetRefreshScheduler(scheduler)
	accountcontrol.SetSSOValidationScheduler(validationScheduler)
	accountcontrol.SetRefreshSchedulerLeader(leader)
	if leader && state.schedulerKey != nil {
		leaseRenewalCleanup = appMainStartSchedulerLeaderLeaseRenewal(
			ctx,
			state.schedulerKey,
			time.Duration(appMainEnvInt("RUNTIME_REDIS_LOCK_TTL_MS", 300000))*time.Millisecond,
		)
	}
	appMainReconcileRefreshRuntime()
	return func(ctx context.Context) error {
		if consoleResetCleanup != nil {
			consoleResetCleanup(ctx)
			consoleResetCleanup = nil
		}
		scheduler.Stop()
		validationScheduler.Stop()
		if leaseRenewalCleanup != nil {
			if err := leaseRenewalCleanup(ctx); err != nil {
				return err
			}
			leaseRenewalCleanup = nil
		}
		if state.schedulerKey != nil {
			_, _ = state.schedulerKey.Release(ctx)
			state.schedulerKey = nil
		}
		if localLockCleanup != nil {
			if err := localLockCleanup(ctx); err != nil {
				return err
			}
			localLockCleanup = nil
		}
		accountcontrol.SetRefreshScheduler(nil)
		accountcontrol.SetSSOValidationScheduler(nil)
		accountcontrol.SetRefreshSchedulerLeader(false)
		accountcontrol.SetRefreshService(nil)
		state.bindAdminRuntime()
		return nil
	}, nil
}

func appMainStartSchedulerLeaderLeaseRenewal(ctx context.Context, lease appMainSchedulerLease, ttl time.Duration) Hook {
	if lease == nil {
		return nil
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	interval := ttl / 3
	if interval <= 0 {
		interval = time.Second
	}
	renewCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-timer.C:
				ok, err := lease.Renew(renewCtx)
				if err != nil || !ok {
					accountcontrol.SetRefreshSchedulerLeader(false)
					appMainReconcileRefreshRuntime()
					return
				}
				timer.Reset(interval)
			}
		}
	}()
	return func(context.Context) error {
		cancel()
		<-done
		return nil
	}
}

func appMainStartConsoleQuotaResetLoop(service *accountcontrol.AccountRefreshService, interval time.Duration) Hook {
	if service == nil || interval <= 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				_, _ = service.ResetExpiredConsoleWindows(ctx)
				timer.Reset(interval)
			}
		}
	}()
	return func(context.Context) error {
		cancel()
		<-done
		return nil
	}
}
