package account

import (
	"context"
	"sync"
	"time"

	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	appruntime "github.com/dslzl/gork/app/platform/runtime"
)

var refreshNowMS = appruntime.NowMS

var allRefreshModeIDs = []int{0, 1, 2, 3, 4, 5}

const (
	defaultUsageConcurrency = 50
	maxUsageConcurrency     = 100
)

type RefreshResult struct {
	Checked     int
	Refreshed   int
	Recovered   int
	Expired     int
	Disabled    int
	RateLimited int
	Failed      int
}

func (r *RefreshResult) Merge(other RefreshResult) {
	r.Checked += other.Checked
	r.Refreshed += other.Refreshed
	r.Recovered += other.Recovered
	r.Expired += other.Expired
	r.Disabled += other.Disabled
	r.RateLimited += other.RateLimited
	r.Failed += other.Failed
}

type AccountRefreshRepository interface {
	GetAccounts(context.Context, []string) ([]AccountRecord, error)
	ListAccounts(context.Context, ListAccountsQuery) (AccountPage, error)
	PatchAccounts(context.Context, []AccountPatch) (AccountMutationResult, error)
	DeleteAccounts(context.Context, []string) (AccountMutationResult, error)
	RuntimeSnapshot(context.Context) (RuntimeSnapshot, error)
}

type SSOModelVerifier interface {
	ProbeListModels(context.Context, string) error
}

type SSOModelVerifierFunc func(context.Context, string) error

func (f SSOModelVerifierFunc) ProbeListModels(ctx context.Context, token string) error {
	return f(ctx, token)
}

type AccountRefreshOptions struct {
	Fetcher                  protocol.UsageFetcher
	UsageConcurrency         int
	PerTokenTimeout          time.Duration
	BatchTimeout             time.Duration
	OnDemandMinInterval      time.Duration
	SSOModelVerifier         SSOModelVerifier
	SSOValidationConcurrency int
	SSOValidationMaxFailures int
}

type AccountRefreshService struct {
	repo                     AccountRefreshRepository
	fetcher                  protocol.UsageFetcher
	usageConcurrency         int
	perTokenTimeout          time.Duration
	batchTimeout             time.Duration
	onDemandMinInterval      time.Duration
	ssoModelVerifier         SSOModelVerifier
	ssoValidationConcurrency int
	ssoValidationMaxFailures int
	mu                       sync.Mutex
	onDemandRunning          bool
	onDemandLast             time.Time
}

func NewAccountRefreshService(repo AccountRefreshRepository, options AccountRefreshOptions) *AccountRefreshService {
	concurrency := clampRefreshConcurrency(options.UsageConcurrency, defaultUsageConcurrency, maxUsageConcurrency)
	minInterval := options.OnDemandMinInterval
	if minInterval <= 0 {
		minInterval = 300 * time.Second
	}
	return &AccountRefreshService{
		repo:                     repo,
		fetcher:                  options.Fetcher,
		usageConcurrency:         concurrency,
		perTokenTimeout:          options.PerTokenTimeout,
		batchTimeout:             options.BatchTimeout,
		onDemandMinInterval:      minInterval,
		ssoModelVerifier:         options.SSOModelVerifier,
		ssoValidationConcurrency: options.SSOValidationConcurrency,
		ssoValidationMaxFailures: options.SSOValidationMaxFailures,
	}
}

func clampRefreshConcurrency(value int, fallback int, limit int) int {
	if value <= 0 {
		value = fallback
	}
	if value < 1 {
		return 1
	}
	if limit > 0 && value > limit {
		return limit
	}
	return value
}

func isRefreshManageable(record AccountRecord, now int64) bool {
	if record.IsDeleted() {
		return false
	}
	status := deriveRefreshStatus(record, now)
	return status == AccountStatusActive || status == AccountStatusCooling
}

func deriveRefreshStatus(record AccountRecord, now int64) AccountStatus {
	if record.Status != AccountStatusCooling {
		return record.Status
	}
	value, ok := record.Ext["cooldown_until"]
	if !ok || value == nil {
		return AccountStatusCooling
	}
	cooldownUntil, err := int64FromAny(value)
	if err != nil || now < cooldownUntil {
		return AccountStatusCooling
	}
	return AccountStatusActive
}
