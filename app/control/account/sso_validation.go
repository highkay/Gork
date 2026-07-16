package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	platformconfig "github.com/dslzl/gork/app/platform/config"
)

const ssoValidationExtKey = "sso_validation"

var (
	ssoValidationMaxFailures = func() int {
		return platformconfig.GlobalConfig.GetInt("account.sso_validation.max_failures", 3)
	}
	ssoValidationConcurrency = func() int {
		return platformconfig.GlobalConfig.GetInt("account.sso_validation.concurrency", 10)
	}
	ssoValidationBatchSize = func() int {
		return platformconfig.GlobalConfig.GetInt("account.sso_validation.batch_size", 100)
	}
	// ssoValidationNow allows tests to freeze local JWT expiry checks.
	ssoValidationNow = func() time.Time { return time.Now() }
)

type SSOValidationResult struct {
	Checked   int
	Refreshed int
	Failed    int
	Expired   int
	NextPage  int
}

func (r *SSOValidationResult) mergeRefreshResult(other RefreshResult) {
	r.Checked += other.Checked
	r.Refreshed += other.Refreshed
	r.Failed += other.Failed
	r.Expired += other.Expired
}

func (s *AccountRefreshService) ValidateSSOBatch(ctx context.Context, page int, pageSize int) (SSOValidationResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = ssoValidationBatchSize()
	}
	query := ListAccountsQuery{
		Page:           page,
		PageSize:       pageSize,
		IncludeDeleted: false,
		SortBy:         "token",
		SortDesc:       false,
	}
	query.Normalize()
	accountPage, err := s.repo.ListAccounts(ctx, query)
	if err != nil {
		return SSOValidationResult{}, err
	}
	nextPage := query.Page + 1
	if len(accountPage.Items) == 0 || (accountPage.TotalPages > 0 && query.Page >= accountPage.TotalPages) {
		nextPage = 1
	}
	result := SSOValidationResult{NextPage: nextPage}
	records := filterRefreshManageable(accountPage.Items)
	if len(records) == 0 {
		return result, nil
	}
	results, err := s.runSSOValidationRecords(ctx, records)
	if err != nil {
		return SSOValidationResult{}, err
	}
	for _, item := range results {
		result.mergeRefreshResult(item)
	}
	return result, nil
}

func (s *AccountRefreshService) runSSOValidationRecords(ctx context.Context, records []AccountRecord) ([]RefreshResult, error) {
	concurrency := max(s.validationConcurrency(), 1)
	if concurrency > len(records) {
		concurrency = len(records)
	}
	results := make([]RefreshResult, len(records))
	errs := make(chan error, len(records))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				result, err := s.validateSSOAccount(ctx, records[index])
				results[index] = result
				if err != nil {
					errs <- err
				}
			}
		}()
	}
	for index := range records {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, ctx.Err()
		case jobs <- index:
		}
	}
	close(jobs)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

// validateSSOAccount follows sso2gropcpa two-layer logic:
//  1. local precheck (empty / JWT expired) — no HTTP
//  2. online accounts.x.ai session probe (final redirect URL is authority)
//  3. if session probe is inconclusive (CF / WAF / network), fall back to
//     console ListModels so dead SSO can still be expired when console works
//
// Terminal session death expires immediately (subject to max_failures).
// Cloudflare / rate limit / WAF / network alone do not delete.
func (s *AccountRefreshService) validateSSOAccount(ctx context.Context, record AccountRecord) (RefreshResult, error) {
	if record.IsDeleted() {
		return RefreshResult{}, nil
	}
	if reason := protocol.SSOLocalInvalidReason(record.Token, ssoValidationNow()); reason != "" {
		return s.recordSSOValidationFailure(ctx, record, "local", protocol.NewSessionInvalidError(reason, "local"))
	}
	if err := s.probeSSOSession(ctx, record.Token); err != nil {
		if protocol.IsTerminalSSOFailure(err) {
			return s.recordSSOValidationFailure(ctx, record, ssoFailureStage(err), err)
		}
		// Inconclusive browser probe → console ListModels (same path runtime uses).
		if fallbackErr := s.probeSSOListModelsFallback(ctx, record.Token); fallbackErr != nil {
			return s.recordSSOValidationFailure(ctx, record, "list_models", fallbackErr)
		}
		// ListModels OK despite CF/WAF on accounts page → treat as usable for now.
		if err := s.recordSSOValidationSuccess(ctx, record); err != nil {
			return RefreshResult{}, err
		}
		return RefreshResult{Checked: 1, Refreshed: 1}, nil
	}
	if err := s.recordSSOValidationSuccess(ctx, record); err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{Checked: 1, Refreshed: 1}, nil
}

func (s *AccountRefreshService) probeSSOSession(ctx context.Context, token string) error {
	prober := s.ssoSessionProber
	if prober == nil {
		// Backward-compatible fallback: ListModels only if session prober not wired.
		if s.ssoModelVerifier == nil {
			return fmt.Errorf("sso session prober is not configured")
		}
		return s.ssoModelVerifier.ProbeListModels(ctx, token)
	}
	return prober.ProbeSession(ctx, token)
}

func (s *AccountRefreshService) probeSSOListModelsFallback(ctx context.Context, token string) error {
	if s.ssoModelVerifier == nil {
		// No fallback verifier: keep the original non-terminal probe error path
		// by returning a soft transport-style error without credentials death.
		return fmt.Errorf("sso list_models fallback is not configured")
	}
	return s.ssoModelVerifier.ProbeListModels(ctx, token)
}

func ssoFailureStage(err error) string {
	var sessionErr *protocol.SessionInvalidError
	if errors.As(err, &sessionErr) && sessionErr != nil {
		if sessionErr.Stage != "" {
			return sessionErr.Stage
		}
		return "session"
	}
	// Non-terminal classes from ErrorFromSSOBrowserClass messages.
	msg := strings.ToLower(fmt.Sprint(err))
	switch {
	case strings.Contains(msg, "cloudflare"):
		return "cloudflare"
	case strings.Contains(msg, "rate limited"):
		return "rate_limited"
	case strings.Contains(msg, "http block"):
		return "http_block"
	default:
		return "session"
	}
}

func (s *AccountRefreshService) recordSSOValidationFailure(
	ctx context.Context,
	record AccountRecord,
	stage string,
	cause error,
) (RefreshResult, error) {
	count := ssoValidationFailureCount(record) + 1
	now := refreshNowMS()
	reason := "sso_validation_" + stage
	patch := AccountPatch{
		Token:          record.Token,
		LastFailAt:     &now,
		LastFailReason: &reason,
		StateReason:    &reason,
		ExtMerge: map[string]any{
			ssoValidationExtKey: map[string]any{
				"failure_count":    count,
				"last_fail_at":     now,
				"last_fail_stage":  stage,
				"last_fail_reason": fmt.Sprint(cause),
			},
		},
	}
	if _, err := s.repo.PatchAccounts(ctx, []AccountPatch{patch}); err != nil {
		return RefreshResult{}, err
	}
	// Only permanent SSO death (session invalid / invalid credentials) can expire.
	// CF / rate limit / WAF / network stay soft-fail.
	if !protocol.IsTerminalSSOFailure(cause) || count < s.validationMaxFailures() {
		return RefreshResult{Checked: 1, Failed: 1}, nil
	}
	if _, err := s.repo.DeleteAccounts(ctx, []string{record.Token}); err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{Checked: 1, Expired: 1}, nil
}

func (s *AccountRefreshService) recordSSOValidationSuccess(ctx context.Context, record AccountRecord) error {
	now := refreshNowMS()
	patch := AccountPatch{
		Token: record.Token,
		ExtMerge: map[string]any{
			ssoValidationExtKey: map[string]any{
				"failure_count": 0,
				"last_ok_at":    now,
			},
		},
	}
	// Clear stale sso_validation_* fail markers so health views stay accurate.
	if record.LastFailReason != nil && strings.HasPrefix(*record.LastFailReason, "sso_validation_") {
		empty := ""
		patch.LastFailReason = &empty
		patch.StateReason = &empty
	}
	_, err := s.repo.PatchAccounts(ctx, []AccountPatch{patch})
	return err
}

func (s *AccountRefreshService) validationMaxFailures() int {
	if s.ssoValidationMaxFailures > 0 {
		return s.ssoValidationMaxFailures
	}
	value := ssoValidationMaxFailures()
	if value <= 0 {
		return 3
	}
	return value
}

func (s *AccountRefreshService) validationConcurrency() int {
	if s.ssoValidationConcurrency > 0 {
		return s.ssoValidationConcurrency
	}
	value := ssoValidationConcurrency()
	if value <= 0 {
		return 10
	}
	return value
}

func ssoValidationFailureCount(record AccountRecord) int {
	raw, ok := record.Ext[ssoValidationExtKey]
	if !ok {
		return 0
	}
	data, ok := raw.(map[string]any)
	if !ok {
		return 0
	}
	count, err := intFromAny(data["failure_count"], 0)
	if err != nil || count < 0 {
		return 0
	}
	return count
}
