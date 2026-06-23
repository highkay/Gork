package products

import (
	"context"

	controlaccount "github.com/dslzl/gork/app/control/account"
	"github.com/dslzl/gork/app/control/model"
	dataaccount "github.com/dslzl/gork/app/dataplane/account"
	"github.com/dslzl/gork/app/platform"
	platformconfig "github.com/dslzl/gork/app/platform/config"
)

const randomMaxRetries = 5

// ReserveAccountQuery mirrors the Python directory.reserve(...) parameters.
type ReserveAccountQuery struct {
	PoolCandidates []int
	ModeID         model.ModeID
	NowSOverride   *int64
	ExcludeTokens  []string
}

type ReserveAccountOptions struct {
	ExcludeTokens []string
	NowSOverride  *int64
}

type AccountDirectory interface {
	Reserve(context.Context, ReserveAccountQuery) (any, error)
}

type DetailedAccountDirectory interface {
	AccountDirectory
	ReserveDetailed(context.Context, ReserveAccountQuery) (any, AccountSelectionFailureReason, error)
}

type AccountRefreshService interface {
	RefreshOnDemand(context.Context) error
}

type refreshOnDemandRuntimeService interface {
	RefreshOnDemand(context.Context) (controlaccount.RefreshResult, error)
}

var (
	accountSelectionStrategy = defaultAccountSelectionStrategy
	accountSelectionGetInt   = defaultAccountSelectionGetInt
	accountSelectionGetBool  = defaultAccountSelectionGetBool
	accountRefreshService    = defaultAccountRefreshService
)

type controlRefreshServiceAdapter struct {
	service refreshOnDemandRuntimeService
}

type AccountSelectionFailureReason string

const (
	AccountSelectionFailureNone               AccountSelectionFailureReason = ""
	AccountSelectionFailureNoAvailable        AccountSelectionFailureReason = "no_available_account"
	AccountSelectionFailureRateLimited        AccountSelectionFailureReason = "all_rate_limited"
	AccountSelectionFailureInvalidCredentials AccountSelectionFailureReason = "all_invalid_credentials"
	AccountSelectionFailureDisabled           AccountSelectionFailureReason = "all_disabled"
)

type AccountSelectionResult struct {
	SelectedMode model.ModeID
	OK           bool
	Reason       AccountSelectionFailureReason
}

func (r AccountSelectionResult) ErrorCode() string {
	switch r.Reason {
	case AccountSelectionFailureRateLimited:
		return "account_pool_rate_limited"
	case AccountSelectionFailureInvalidCredentials:
		return "account_pool_invalid_credentials"
	case AccountSelectionFailureDisabled:
		return "account_pool_disabled"
	default:
		return "no_available_account"
	}
}

func AccountSelectionError(result AccountSelectionResult) error {
	message := "No available accounts"
	switch result.Reason {
	case AccountSelectionFailureRateLimited:
		message = "All candidate accounts are rate limited or at capacity"
	case AccountSelectionFailureInvalidCredentials:
		message = "All candidate accounts have invalid credentials"
	case AccountSelectionFailureDisabled:
		message = "All candidate accounts are disabled"
	}
	return platform.NewAppError(message, platform.ErrorKindRateLimit, result.ErrorCode(), 429, map[string]any{
		"selected_mode":    int(result.SelectedMode),
		"selection_reason": string(result.Reason),
	})
}

func AccountSelectionFailureFromData(reason dataaccount.ReserveFailureReason) AccountSelectionFailureReason {
	switch reason {
	case dataaccount.ReserveFailureRateLimited:
		return AccountSelectionFailureRateLimited
	case dataaccount.ReserveFailureInvalidCredentials:
		return AccountSelectionFailureInvalidCredentials
	case dataaccount.ReserveFailureDisabled:
		return AccountSelectionFailureDisabled
	case dataaccount.ReserveFailureNoAvailable:
		return AccountSelectionFailureNoAvailable
	default:
		return AccountSelectionFailureNone
	}
}

func (a controlRefreshServiceAdapter) RefreshOnDemand(ctx context.Context) error {
	_, err := a.service.RefreshOnDemand(ctx)
	return err
}

func defaultAccountSelectionStrategy() string {
	return dataaccount.CurrentStrategy()
}

func defaultAccountSelectionGetInt(key string, defaultValue int) int {
	return platformconfig.GlobalConfig.GetInt(key, defaultValue)
}

func defaultAccountSelectionGetBool(key string, defaultValue bool) bool {
	return platformconfig.GlobalConfig.GetBool(key, defaultValue)
}

func defaultAccountRefreshService() AccountRefreshService {
	service := controlaccount.GetRefreshService()
	if service == nil {
		return nil
	}
	onDemand, ok := any(service).(refreshOnDemandRuntimeService)
	if !ok {
		return nil
	}
	return controlRefreshServiceAdapter{service: onDemand}
}

func SelectionMaxRetries() int {
	if accountSelectionStrategy() == "random" {
		return randomMaxRetries
	}
	return accountSelectionGetInt("retry.max_retries", 1)
}

func ModeCandidates(spec model.ModelSpec) []model.ModeID {
	primary := spec.ModeID
	if spec.IsChat() && spec.ModeID == model.ModeAuto && accountSelectionGetBool("features.auto_chat_mode_fallback", true) {
		return []model.ModeID{primary, model.ModeFast, model.ModeExpert}
	}
	return []model.ModeID{primary}
}

func ReserveAccount(ctx context.Context, directory AccountDirectory, spec model.ModelSpec, options ReserveAccountOptions) (any, model.ModeID, bool, error) {
	lease, result, err := ReserveAccountDetailed(ctx, directory, spec, options)
	return lease, result.SelectedMode, result.OK, err
}

func ReserveAccountDetailed(ctx context.Context, directory AccountDirectory, spec model.ModelSpec, options ReserveAccountOptions) (any, AccountSelectionResult, error) {
	lease, result, err := reserveFromCandidates(ctx, directory, spec, options)
	if err != nil || result.OK {
		return lease, result, err
	}
	if accountSelectionStrategy() == "random" {
		return nil, result, nil
	}

	refresh := accountRefreshService()
	if refresh == nil {
		return nil, result, nil
	}
	if err := refresh.RefreshOnDemand(ctx); err != nil {
		return nil, result, err
	}

	return reserveFromCandidates(ctx, directory, spec, options)
}

func reserveFromCandidates(ctx context.Context, directory AccountDirectory, spec model.ModelSpec, options ReserveAccountOptions) (any, AccountSelectionResult, error) {
	result := AccountSelectionResult{SelectedMode: spec.ModeID, Reason: AccountSelectionFailureNoAvailable}
	for _, candidate := range ModeCandidates(spec) {
		query := ReserveAccountQuery{
			PoolCandidates: spec.PoolCandidates(),
			ModeID:         candidate,
			NowSOverride:   options.NowSOverride,
			ExcludeTokens:  options.ExcludeTokens,
		}
		lease, reason, err := reserveAccountCandidate(ctx, directory, query)
		if err != nil {
			return nil, result, err
		}
		if lease != nil {
			return lease, AccountSelectionResult{SelectedMode: candidate, OK: true}, nil
		}
		if reason != AccountSelectionFailureNone && result.Reason == AccountSelectionFailureNoAvailable {
			result.Reason = reason
		}
	}
	return nil, result, nil
}

func reserveAccountCandidate(ctx context.Context, directory AccountDirectory, query ReserveAccountQuery) (any, AccountSelectionFailureReason, error) {
	if detailed, ok := directory.(DetailedAccountDirectory); ok {
		lease, reason, err := detailed.ReserveDetailed(ctx, query)
		if lease == nil && reason == AccountSelectionFailureNone {
			reason = AccountSelectionFailureNoAvailable
		}
		return lease, reason, err
	}
	lease, err := directory.Reserve(ctx, query)
	if lease != nil || err != nil {
		return lease, AccountSelectionFailureNone, err
	}
	return nil, AccountSelectionFailureNoAvailable, nil
}
