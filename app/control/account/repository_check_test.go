package account

import (
	"context"
	"testing"
)

func TestCheckAccountRepositoryConsistencyReportsOK(t *testing.T) {
	record := checkRecord("tok-ok")
	repo := &checkAccountRepository{
		revision: 3,
		snapshot: RuntimeSnapshot{Revision: 3, Items: []AccountRecord{
			record,
		}},
		page: AccountPage{Revision: 3, Items: []AccountRecord{record}, TotalPages: 1},
		getRecords: map[string]AccountRecord{
			record.Token: record,
		},
	}

	report, err := CheckAccountRepositoryConsistency(context.Background(), repo)

	if err != nil {
		t.Fatalf("CheckAccountRepositoryConsistency returned error: %v", err)
	}
	if !report.OK || len(report.Issues) != 0 || report.SnapshotCount != 1 || report.ListCount != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestCheckAccountRepositoryConsistencyReportsMismatches(t *testing.T) {
	snapshotRecord := checkRecord("tok-snapshot")
	listRecord := checkRecord("tok-list")
	listRecord.Ext = nil
	repo := &checkAccountRepository{
		revision: 5,
		snapshot: RuntimeSnapshot{Revision: 4, Items: []AccountRecord{
			snapshotRecord,
			snapshotRecord,
		}},
		page:       AccountPage{Revision: 6, Items: []AccountRecord{listRecord}, TotalPages: 1},
		getRecords: map[string]AccountRecord{},
	}

	report, err := CheckAccountRepositoryConsistency(context.Background(), repo)

	if err != nil {
		t.Fatalf("CheckAccountRepositoryConsistency returned error: %v", err)
	}
	if report.OK {
		t.Fatalf("report should not be OK: %#v", report)
	}
	for _, code := range []string{
		"snapshot_revision_mismatch",
		"list_revision_mismatch",
		"snapshot_duplicate_token",
		"list_nil_ext",
		"list_missing_snapshot_token",
		"snapshot_missing_list_token",
		"get_accounts_missing_token",
	} {
		if !checkReportHasIssue(report, code) {
			t.Fatalf("report missing %s: %#v", code, report.Issues)
		}
	}
}

type checkAccountRepository struct {
	revision   int
	snapshot   RuntimeSnapshot
	page       AccountPage
	getRecords map[string]AccountRecord
}

func (r *checkAccountRepository) Initialize(context.Context) error { return nil }
func (r *checkAccountRepository) Close(context.Context) error      { return nil }
func (r *checkAccountRepository) GetRevision(context.Context) (int, error) {
	return r.revision, nil
}
func (r *checkAccountRepository) RuntimeSnapshot(context.Context) (RuntimeSnapshot, error) {
	return r.snapshot, nil
}
func (r *checkAccountRepository) ScanChanges(context.Context, int, int) (AccountChangeSet, error) {
	return AccountChangeSet{}, nil
}
func (r *checkAccountRepository) UpsertAccounts(context.Context, []AccountUpsert) (AccountMutationResult, error) {
	return AccountMutationResult{}, nil
}
func (r *checkAccountRepository) PatchAccounts(context.Context, []AccountPatch) (AccountMutationResult, error) {
	return AccountMutationResult{}, nil
}
func (r *checkAccountRepository) DeleteAccounts(context.Context, []string) (AccountMutationResult, error) {
	return AccountMutationResult{}, nil
}
func (r *checkAccountRepository) GetAccounts(_ context.Context, tokens []string) ([]AccountRecord, error) {
	out := []AccountRecord{}
	for _, token := range tokens {
		if record, ok := r.getRecords[token]; ok {
			out = append(out, record)
		}
	}
	return out, nil
}
func (r *checkAccountRepository) ListAccounts(context.Context, ListAccountsQuery) (AccountPage, error) {
	return r.page, nil
}
func (r *checkAccountRepository) ReplacePool(context.Context, BulkReplacePoolCommand) (AccountMutationResult, error) {
	return AccountMutationResult{}, nil
}

func checkRecord(token string) AccountRecord {
	return AccountRecord{
		Token:  token,
		Pool:   "basic",
		Status: AccountStatusActive,
		Quota:  DefaultQuotaSet("basic").ToDict(),
		Tags:   []string{},
		Ext:    map[string]any{},
	}
}

func checkReportHasIssue(report AccountConsistencyReport, code string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
