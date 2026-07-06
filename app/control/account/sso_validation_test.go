package account

import (
	"context"
	"errors"
	"testing"

	"github.com/dslzl/gork/app/platform"
)

type fakeSSOModelVerifier struct {
	err   error
	calls []string
}

func (v *fakeSSOModelVerifier) ProbeListModels(_ context.Context, token string) error {
	v.calls = append(v.calls, token)
	return v.err
}

func TestValidateSSOAccountSkipsListModelsWhenRefreshFails(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 1000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	record := AccountRecord{Token: "tok-refresh-fail", Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	verifier := &fakeSSOModelVerifier{}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{
		Fetcher:                  &fakeUsageFetcher{err: errors.New("refresh failed")},
		SSOModelVerifier:         verifier,
		SSOValidationMaxFailures: 3,
	})

	result, err := service.validateSSOAccount(context.Background(), record)

	if err != nil {
		t.Fatalf("validateSSOAccount returned error: %v", err)
	}
	if result.Checked != 1 || result.Failed != 1 || result.Expired != 0 {
		t.Fatalf("validation result = %#v", result)
	}
	if len(verifier.calls) != 0 {
		t.Fatalf("ListModels should not be called after refresh failure, calls=%#v", verifier.calls)
	}
	if len(repo.deletedTokens) != 0 {
		t.Fatalf("deleted tokens = %#v", repo.deletedTokens)
	}
	validation := repo.patches[len(repo.patches)-1].ExtMerge["sso_validation"].(map[string]any)
	if validation["failure_count"] != 1 || validation["last_fail_stage"] != "refresh" {
		t.Fatalf("sso validation metadata = %#v", validation)
	}
}

func TestValidateSSOAccountDeletesAfterConfiguredConsecutiveFailures(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 2000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	record := AccountRecord{
		Token:  "tok-delete",
		Pool:   "basic",
		Status: AccountStatusActive,
		Quota:  DefaultQuotaSet("basic").ToDict(),
		Ext:    map[string]any{"sso_validation": map[string]any{"failure_count": 2}},
	}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{
		Fetcher:                  &fakeUsageFetcher{err: errors.New("refresh failed")},
		SSOModelVerifier:         &fakeSSOModelVerifier{},
		SSOValidationMaxFailures: 3,
	})

	result, err := service.validateSSOAccount(context.Background(), record)

	if err != nil {
		t.Fatalf("validateSSOAccount returned error: %v", err)
	}
	if result.Checked != 1 || result.Expired != 1 || result.Failed != 0 {
		t.Fatalf("validation result = %#v", result)
	}
	if len(repo.deletedTokens) != 1 || repo.deletedTokens[0] != "tok-delete" {
		t.Fatalf("deleted tokens = %#v", repo.deletedTokens)
	}
	validation := repo.patches[len(repo.patches)-1].ExtMerge["sso_validation"].(map[string]any)
	if validation["failure_count"] != 3 || validation["last_fail_stage"] != "refresh" {
		t.Fatalf("sso validation metadata = %#v", validation)
	}
}

func TestValidateSSOAccountClearsFailuresAfterRefreshAndListModelsSucceed(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 3000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	record := AccountRecord{
		Token:  "tok-ok",
		Pool:   "basic",
		Status: AccountStatusActive,
		Quota:  DefaultQuotaSet("basic").ToDict(),
		Ext:    map[string]any{"sso_validation": map[string]any{"failure_count": 2}},
	}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	verifier := &fakeSSOModelVerifier{}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{
		Fetcher:                  &fakeUsageFetcher{},
		SSOModelVerifier:         verifier,
		SSOValidationMaxFailures: 3,
	})

	result, err := service.validateSSOAccount(context.Background(), record)

	if err != nil {
		t.Fatalf("validateSSOAccount returned error: %v", err)
	}
	if result.Checked != 1 || result.Refreshed != 1 || result.Failed != 0 || result.Expired != 0 {
		t.Fatalf("validation result = %#v", result)
	}
	if len(verifier.calls) != 1 || verifier.calls[0] != "tok-ok" {
		t.Fatalf("ListModels calls = %#v", verifier.calls)
	}
	validation := repo.patches[len(repo.patches)-1].ExtMerge["sso_validation"].(map[string]any)
	if validation["failure_count"] != 0 || validation["last_ok_at"] != int64(3000) {
		t.Fatalf("sso validation metadata = %#v", validation)
	}
}

func TestValidateSSOAccountRecordsListModelsFailureAfterRefreshSuccess(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 4000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	record := AccountRecord{Token: "tok-list-fail", Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{
		Fetcher:                  &fakeUsageFetcher{},
		SSOModelVerifier:         &fakeSSOModelVerifier{err: errors.New("list models failed")},
		SSOValidationMaxFailures: 3,
	})

	result, err := service.validateSSOAccount(context.Background(), record)

	if err != nil {
		t.Fatalf("validateSSOAccount returned error: %v", err)
	}
	if result.Checked != 1 || result.Failed != 1 || result.Expired != 0 {
		t.Fatalf("validation result = %#v", result)
	}
	if len(repo.deletedTokens) != 0 {
		t.Fatalf("deleted tokens = %#v", repo.deletedTokens)
	}
	validation := repo.patches[len(repo.patches)-1].ExtMerge["sso_validation"].(map[string]any)
	if validation["failure_count"] != 1 || validation["last_fail_stage"] != "list_models" {
		t.Fatalf("sso validation metadata = %#v", validation)
	}
}

func TestRecordFailureAsyncInvalidCredentialsUsesInvalidCredentialThreshold(t *testing.T) {
	oldNow := invalidCredentialsNowMS
	invalidCredentialsNowMS = func() int64 { return 9000 }
	t.Cleanup(func() { invalidCredentialsNowMS = oldNow })
	oldMaxFailures := invalidCredentialsMaxFailures
	invalidCredentialsMaxFailures = func() int { return 1 }
	t.Cleanup(func() { invalidCredentialsMaxFailures = oldMaxFailures })
	record := AccountRecord{
		Token:  "tok-chat",
		Pool:   "basic",
		Status: AccountStatusActive,
		Quota:  DefaultQuotaSet("basic").ToDict(),
	}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{})

	err := service.RecordFailureAsync(context.Background(), "tok-chat", 1, platform.NewUpstreamError("bad", 403, "token expired"))

	if err != nil {
		t.Fatalf("RecordFailureAsync returned error: %v", err)
	}
	if len(repo.deletedTokens) != 0 {
		t.Fatalf("deleted tokens = %#v, want none", repo.deletedTokens)
	}
	if len(repo.patches) != 1 {
		t.Fatalf("patches = %#v, want one", repo.patches)
	}
	patch := repo.patches[0]
	if patch.Status == nil || *patch.Status != AccountStatusExpired {
		t.Fatalf("status = %#v, want expired", patch.Status)
	}
	if patch.LastFailAt == nil || *patch.LastFailAt != 9000 {
		t.Fatalf("last fail at = %#v, want 9000", patch.LastFailAt)
	}
	if patch.LastFailReason == nil || *patch.LastFailReason != "invalid_credentials" {
		t.Fatalf("last fail reason = %#v, want invalid_credentials", patch.LastFailReason)
	}
	invalid := patch.ExtMerge["invalid_credentials"].(map[string]any)
	if invalid["failure_count"] != 1 || invalid["last_fail_source"] != "runtime" {
		t.Fatalf("invalid credentials metadata = %#v", invalid)
	}
}

func TestValidateSSOBatchPagesStableTokenOrderAndSkipsUnmanageableAccounts(t *testing.T) {
	active := AccountRecord{Token: "tok-active", Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()}
	expired := AccountRecord{Token: "tok-expired", Pool: "basic", Status: AccountStatusExpired, Quota: DefaultQuotaSet("basic").ToDict()}
	repo := &fakeRefreshRepo{
		listPage: AccountPage{
			Items:      []AccountRecord{active, expired},
			Page:       2,
			PageSize:   2,
			TotalPages: 3,
		},
	}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{
		Fetcher:                  &fakeUsageFetcher{},
		SSOModelVerifier:         &fakeSSOModelVerifier{},
		SSOValidationMaxFailures: 3,
	})

	result, err := service.ValidateSSOBatch(context.Background(), 2, 2)

	if err != nil {
		t.Fatalf("ValidateSSOBatch returned error: %v", err)
	}
	if len(repo.listQueries) != 1 {
		t.Fatalf("list queries = %#v", repo.listQueries)
	}
	query := repo.listQueries[0]
	if query.Page != 2 || query.PageSize != 2 || query.IncludeDeleted || query.SortBy != "token" || query.SortDesc {
		t.Fatalf("list query = %#v", query)
	}
	if result.NextPage != 3 || result.Checked != 1 || result.Refreshed != 1 {
		t.Fatalf("batch result = %#v", result)
	}
}
