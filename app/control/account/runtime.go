package account

import (
	"sync"

	"github.com/dslzl/gork/app/platform/config"
)

var refreshRuntimeState = struct {
	sync.Mutex
	service                accountScheduledRefresher
	scheduler              *AccountRefreshScheduler
	ssoValidationScheduler *SSOValidationScheduler
	autoCleanScheduler     *AutoCleanScheduler
	schedulerLeader        bool
	strategy               string
}{
	strategy: "random",
}

var runtimeRefreshEnabled = func() bool {
	return config.GlobalConfig.GetBool("account.refresh.enabled", false)
}

func SetRefreshService(service accountScheduledRefresher) {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	refreshRuntimeState.service = service
}

func GetRefreshService() accountScheduledRefresher {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.service
}

func SetRefreshScheduler(scheduler *AccountRefreshScheduler) {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	refreshRuntimeState.scheduler = scheduler
}

func GetRefreshScheduler() *AccountRefreshScheduler {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.scheduler
}

func SetSSOValidationScheduler(scheduler *SSOValidationScheduler) {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	refreshRuntimeState.ssoValidationScheduler = scheduler
}

func GetSSOValidationSchedulerRuntime() *SSOValidationScheduler {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.ssoValidationScheduler
}

func SetAutoCleanScheduler(scheduler *AutoCleanScheduler) {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	refreshRuntimeState.autoCleanScheduler = scheduler
}

func GetAutoCleanSchedulerRuntime() *AutoCleanScheduler {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.autoCleanScheduler
}

func SetRefreshSchedulerLeader(isLeader bool) {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	refreshRuntimeState.schedulerLeader = isLeader
}

func IsRefreshSchedulerLeader() bool {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.schedulerLeader
}

func SetAccountSelectionStrategy(strategy string) {
	if strategy != "quota" && strategy != "random" {
		return
	}
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	refreshRuntimeState.strategy = strategy
}

func CurrentAccountSelectionStrategy() string {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.strategy
}

func ReconcileRefreshRuntime(enabled ...bool) string {
	refreshEnabled := runtimeRefreshEnabled()
	if len(enabled) > 0 {
		refreshEnabled = enabled[0]
	}
	targetStrategy := "random"
	if refreshEnabled {
		targetStrategy = "quota"
	}
	scheduler, validationScheduler, autoClean, leader := runtimeSchedulerState()
	SetAccountSelectionStrategy(targetStrategy)
	if scheduler != nil && leader {
		if refreshEnabled && !scheduler.IsRunning() {
			scheduler.Start()
		}
		if !refreshEnabled && scheduler.IsRunning() {
			scheduler.Stop()
		}
	}
	reconcileSSOValidationScheduler(validationScheduler, leader)
	reconcileAutoCleanScheduler(autoClean, leader)
	return targetStrategy
}

func runtimeSchedulerState() (*AccountRefreshScheduler, *SSOValidationScheduler, *AutoCleanScheduler, bool) {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.scheduler, refreshRuntimeState.ssoValidationScheduler, refreshRuntimeState.autoCleanScheduler, refreshRuntimeState.schedulerLeader
}

func reconcileSSOValidationScheduler(scheduler *SSOValidationScheduler, leader bool) {
	if scheduler == nil || !leader {
		return
	}
	enabled := ssoValidationEnabled()
	if enabled && !scheduler.IsRunning() {
		scheduler.Start()
	}
	if !enabled && scheduler.IsRunning() {
		scheduler.Stop()
	}
}

func reconcileAutoCleanScheduler(scheduler *AutoCleanScheduler, leader bool) {
	if scheduler == nil || !leader {
		return
	}
	enabled := autoCleanEnabled()
	if enabled && !scheduler.IsRunning() {
		scheduler.Start()
	}
	if !enabled && scheduler.IsRunning() {
		scheduler.Stop()
	}
}
