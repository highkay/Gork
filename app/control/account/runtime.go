package account

import (
	"sync"

	"github.com/dslzl/gork/app/platform/config"
)

var refreshRuntimeState = struct {
	sync.Mutex
	service                accountScheduledRefresher
	maintenanceService     accountMaintenanceRefresher
	scheduler              *AccountRefreshScheduler
	ssoValidationScheduler *SSOValidationScheduler
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
	if maintenance, ok := service.(accountMaintenanceRefresher); ok {
		refreshRuntimeState.maintenanceService = maintenance
	} else {
		refreshRuntimeState.maintenanceService = nil
	}
}

func GetRefreshService() accountScheduledRefresher {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.service
}

func GetMaintenanceRefreshService() accountMaintenanceRefresher {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.maintenanceService
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
	scheduler, validationScheduler, leader := runtimeSchedulerState()
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
	return targetStrategy
}

func runtimeSchedulerState() (*AccountRefreshScheduler, *SSOValidationScheduler, bool) {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.scheduler, refreshRuntimeState.ssoValidationScheduler, refreshRuntimeState.schedulerLeader
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
