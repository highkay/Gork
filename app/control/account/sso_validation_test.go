package account

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
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

type fakeSSOSessionProber struct {
	err   error
	calls []string
}

func (p *fakeSSOSessionProber) ProbeSession(_ context.Context, token string) error {
	p.calls = append(p.calls, token)
	return p.err
}

func TestValidateSSOAccountLocalJWTExpiredDeletes(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 1000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	oldClock := ssoValidationNow
	fixed := time.Unix(1_700_000_000, 0)
	ssoValidationNow = func() time.Time { return fixed }
	t.Cleanup(func() { ssoValidationNow = oldClock })

	// JWT expired > 60s ago relative to fixed clock.
	token := makeAccountTestJWT(t, fixed.Unix()-120)
	record := AccountRecord{Token: token, Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	prober := &fakeSSOSessionProber{}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{
		SSOSessionProber:         prober,
		SSOValidationMaxFailures: 1,
	})

	result, err := service.validateSSOAccount(context.Background(), record)
	if err != nil {
		t.Fatalf("validateSSOAccount returned error: %v", err)
	}
	if result.Checked != 1 || result.Expired != 1 {
		t.Fatalf("result = %#v, want expired", result)
	}
	if len(prober.calls) != 0 {
		t.Fatalf("online probe should be skipped for local expire, calls=%#v", prober.calls)
	}
	if len(repo.deletedTokens) != 1 {
		t.Fatalf("deleted = %#v", repo.deletedTokens)
	}
	validation := repo.patches[0].ExtMerge["sso_validation"].(map[string]any)
	if validation["last_fail_stage"] != "local" {
		t.Fatalf("stage = %#v", validation)
	}
}

func TestValidateSSOAccountSessionInvalidExpires(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 2000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	oldClock := ssoValidationNow
	ssoValidationNow = func() time.Time { return time.Unix(1_700_000_000, 0) }
	t.Cleanup(func() { ssoValidationNow = oldClock })

	record := AccountRecord{Token: "not-jwt-token", Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{
		SSOSessionProber: &fakeSSOSessionProber{
			err: protocol.NewSessionInvalidError("sign_in", "session"),
		},
		SSOValidationMaxFailures: 1,
	})

	result, err := service.validateSSOAccount(context.Background(), record)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Expired != 1 || len(repo.deletedTokens) != 1 {
		t.Fatalf("result=%#v deleted=%#v", result, repo.deletedTokens)
	}
	validation := repo.patches[0].ExtMerge["sso_validation"].(map[string]any)
	if validation["last_fail_stage"] != "session" {
		t.Fatalf("stage = %#v", validation)
	}
}

func TestValidateSSOAccountCloudflareIsSoftFail(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 3000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	oldClock := ssoValidationNow
	ssoValidationNow = func() time.Time { return time.Unix(1_700_000_000, 0) }
	t.Cleanup(func() { ssoValidationNow = oldClock })

	record := AccountRecord{
		Token:  "tok-cf",
		Pool:   "basic",
		Status: AccountStatusActive,
		Quota:  DefaultQuotaSet("basic").ToDict(),
		Ext:    map[string]any{"sso_validation": map[string]any{"failure_count": 5}},
	}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{
		SSOSessionProber: &fakeSSOSessionProber{
			err: platform.NewUpstreamError("sso probe cloudflare challenge", 403, "just a moment"),
		},
		SSOValidationMaxFailures: 1,
	})

	result, err := service.validateSSOAccount(context.Background(), record)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Failed != 1 || result.Expired != 0 || len(repo.deletedTokens) != 0 {
		t.Fatalf("cloudflare must soft-fail: result=%#v deleted=%#v", result, repo.deletedTokens)
	}
	validation := repo.patches[0].ExtMerge["sso_validation"].(map[string]any)
	if validation["last_fail_stage"] != "cloudflare" {
		t.Fatalf("stage = %#v", validation)
	}
}

func TestValidateSSOAccountHTTPBlockIsSoftFail(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 3100 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	oldClock := ssoValidationNow
	ssoValidationNow = func() time.Time { return time.Unix(1_700_000_000, 0) }
	t.Cleanup(func() { ssoValidationNow = oldClock })

	record := AccountRecord{Token: "tok-block", Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{
		SSOSessionProber: &fakeSSOSessionProber{
			err: platform.NewUpstreamError("sso probe http block (waf)", 403, "forbidden"),
		},
		SSOValidationMaxFailures: 1,
	})

	result, err := service.validateSSOAccount(context.Background(), record)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Failed != 1 || result.Expired != 0 {
		t.Fatalf("http block must soft-fail: %#v", result)
	}
}

func TestValidateSSOAccountSuccessClearsStaleFailReason(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 4000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	oldClock := ssoValidationNow
	ssoValidationNow = func() time.Time { return time.Unix(1_700_000_000, 0) }
	t.Cleanup(func() { ssoValidationNow = oldClock })

	stale := "sso_validation_refresh"
	record := AccountRecord{
		Token:          "tok-ok",
		Pool:           "basic",
		Status:         AccountStatusActive,
		Quota:          DefaultQuotaSet("basic").ToDict(),
		LastFailReason: &stale,
		Ext:            map[string]any{"sso_validation": map[string]any{"failure_count": 2}},
	}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	prober := &fakeSSOSessionProber{}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{
		SSOSessionProber:         prober,
		SSOValidationMaxFailures: 3,
	})

	result, err := service.validateSSOAccount(context.Background(), record)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Refreshed != 1 || result.Failed != 0 {
		t.Fatalf("result = %#v", result)
	}
	if len(prober.calls) != 1 || prober.calls[0] != "tok-ok" {
		t.Fatalf("prober calls = %#v", prober.calls)
	}
	patch := repo.patches[0]
	validation := patch.ExtMerge["sso_validation"].(map[string]any)
	if validation["failure_count"] != 0 || validation["last_ok_at"] != int64(4000) {
		t.Fatalf("validation meta = %#v", validation)
	}
	if patch.LastFailReason == nil || *patch.LastFailReason != "" {
		t.Fatalf("stale fail reason should clear, got %#v", patch.LastFailReason)
	}
}

func TestValidateSSOAccountFallsBackToListModelsWhenSessionProberMissing(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 5000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	oldClock := ssoValidationNow
	ssoValidationNow = func() time.Time { return time.Unix(1_700_000_000, 0) }
	t.Cleanup(func() { ssoValidationNow = oldClock })

	record := AccountRecord{Token: "tok-fallback", Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	verifier := &fakeSSOModelVerifier{}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{
		SSOModelVerifier:         verifier,
		SSOValidationMaxFailures: 3,
	})

	result, err := service.validateSSOAccount(context.Background(), record)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Refreshed != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(verifier.calls) != 1 {
		t.Fatalf("list models fallback calls = %#v", verifier.calls)
	}
}

func TestValidateSSOBatchPagesStableTokenOrderAndSkipsUnmanageableAccounts(t *testing.T) {
	oldClock := ssoValidationNow
	ssoValidationNow = func() time.Time { return time.Unix(1_700_000_000, 0) }
	t.Cleanup(func() { ssoValidationNow = oldClock })

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
		SSOSessionProber:         &fakeSSOSessionProber{},
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

func TestRecordFailureAsyncInvalidCredentialsDeletesAtInvalidCredentialThreshold(t *testing.T) {
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
	if len(repo.deletedTokens) != 1 || repo.deletedTokens[0] != "tok-chat" {
		t.Fatalf("deleted tokens = %#v, want tok-chat", repo.deletedTokens)
	}
}

func TestValidateSSOAccountDoesNotDeleteOnTransientBelowThreshold(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 6000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	oldClock := ssoValidationNow
	ssoValidationNow = func() time.Time { return time.Unix(1_700_000_000, 0) }
	t.Cleanup(func() { ssoValidationNow = oldClock })

	record := AccountRecord{
		Token:  "tok-retry",
		Pool:   "basic",
		Status: AccountStatusActive,
		Quota:  DefaultQuotaSet("basic").ToDict(),
		Ext:    map[string]any{"sso_validation": map[string]any{"failure_count": 0}},
	}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{
		SSOSessionProber: &fakeSSOSessionProber{
			err: protocol.NewSessionInvalidError("sign_in", "session"),
		},
		SSOValidationMaxFailures: 3,
	})

	result, err := service.validateSSOAccount(context.Background(), record)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Failed != 1 || result.Expired != 0 || len(repo.deletedTokens) != 0 {
		t.Fatalf("below threshold must soft mark: %#v deleted=%#v", result, repo.deletedTokens)
	}
}

func makeAccountTestJWT(t *testing.T, exp int64) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, err := json.Marshal(map[string]any{"exp": exp})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + body + ".sig"
}
