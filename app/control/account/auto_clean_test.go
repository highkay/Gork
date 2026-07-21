package account

import (
	"context"
	"testing"
	"time"
)

type fakeAutoCleanRepo struct {
	pages   map[AccountStatus][]AccountRecord
	deleted []string
}

func (r *fakeAutoCleanRepo) Initialize(context.Context) error { return nil }
func (r *fakeAutoCleanRepo) GetRevision(context.Context) (int, error) {
	return 0, nil
}
func (r *fakeAutoCleanRepo) RuntimeSnapshot(context.Context) (RuntimeSnapshot, error) {
	return RuntimeSnapshot{}, nil
}
func (r *fakeAutoCleanRepo) ScanChanges(context.Context, int, int) (AccountChangeSet, error) {
	return AccountChangeSet{}, nil
}
func (r *fakeAutoCleanRepo) UpsertAccounts(context.Context, []AccountUpsert) (AccountMutationResult, error) {
	return AccountMutationResult{}, nil
}
func (r *fakeAutoCleanRepo) PatchAccounts(context.Context, []AccountPatch) (AccountMutationResult, error) {
	return AccountMutationResult{}, nil
}
func (r *fakeAutoCleanRepo) DeleteAccounts(_ context.Context, tokens []string) (AccountMutationResult, error) {
	r.deleted = append(r.deleted, tokens...)
	return AccountMutationResult{Deleted: len(tokens)}, nil
}
func (r *fakeAutoCleanRepo) GetAccounts(context.Context, []string) ([]AccountRecord, error) {
	return nil, nil
}
func (r *fakeAutoCleanRepo) ListAccounts(_ context.Context, query ListAccountsQuery) (AccountPage, error) {
	if query.Status == nil {
		return AccountPage{}, nil
	}
	items := r.pages[*query.Status]
	return AccountPage{Items: items, Page: 1, PageSize: query.PageSize, Total: len(items)}, nil
}
func (r *fakeAutoCleanRepo) ReplacePool(context.Context, BulkReplacePoolCommand) (AccountMutationResult, error) {
	return AccountMutationResult{}, nil
}
func (r *fakeAutoCleanRepo) Close(context.Context) error { return nil }

func TestRunExpiredAccountAutoCleanDeletesOldExpired(t *testing.T) {
	now := time.Now().UTC()
	oldMS := now.Add(-48 * time.Hour).UnixMilli()
	freshMS := now.Add(-1 * time.Hour).UnixMilli()
	repo := &fakeAutoCleanRepo{
		pages: map[AccountStatus][]AccountRecord{
			AccountStatusExpired: {
				{Token: "old-exp", Status: AccountStatusExpired, Ext: map[string]any{"expired_at": oldMS}, UpdatedAt: oldMS},
				{Token: "fresh-exp", Status: AccountStatusExpired, Ext: map[string]any{"expired_at": freshMS}, UpdatedAt: freshMS},
				{Token: "no-mark", Status: AccountStatusExpired, UpdatedAt: oldMS}, // fallback updated_at
			},
		},
	}
	result, err := RunExpiredAccountAutoClean(context.Background(), repo, AutoCleanConfig{
		Enabled:    true,
		Interval:   time.Hour,
		MinAge:     24 * time.Hour,
		MaxDeletes: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 2 {
		t.Fatalf("deleted=%d scanned=%d skipped=%d tokens=%v", result.Deleted, result.Scanned, result.Skipped, repo.deleted)
	}
	want := map[string]bool{"old-exp": true, "no-mark": true}
	for _, tok := range repo.deleted {
		if !want[tok] {
			t.Fatalf("unexpected delete %q", tok)
		}
	}
	for tok := range want {
		found := false
		for _, d := range repo.deleted {
			if d == tok {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing delete %q in %v", tok, repo.deleted)
		}
	}
}

func TestRunExpiredAccountAutoCleanDisabledNoop(t *testing.T) {
	repo := &fakeAutoCleanRepo{
		pages: map[AccountStatus][]AccountRecord{
			AccountStatusExpired: {{Token: "x", Status: AccountStatusExpired, Ext: map[string]any{"expired_at": int64(1)}}},
		},
	}
	result, err := RunExpiredAccountAutoClean(context.Background(), repo, AutoCleanConfig{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 0 || len(repo.deleted) != 0 {
		t.Fatalf("disabled should noop: %+v deleted=%v", result, repo.deleted)
	}
}

func TestRunExpiredAccountAutoCleanRespectsBudget(t *testing.T) {
	oldMS := time.Now().Add(-48 * time.Hour).UnixMilli()
	items := make([]AccountRecord, 0, 5)
	for i := 0; i < 5; i++ {
		items = append(items, AccountRecord{
			Token: "t" + string(rune('a'+i)), Status: AccountStatusExpired,
			Ext: map[string]any{"expired_at": oldMS}, UpdatedAt: oldMS,
		})
	}
	repo := &fakeAutoCleanRepo{pages: map[AccountStatus][]AccountRecord{AccountStatusExpired: items}}
	result, err := RunExpiredAccountAutoClean(context.Background(), repo, AutoCleanConfig{
		Enabled: true, MinAge: time.Hour, MaxDeletes: 2, Interval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 2 || len(repo.deleted) != 2 {
		t.Fatalf("budget: deleted=%d tokens=%v", result.Deleted, repo.deleted)
	}
}

func TestNormalizeAutoCleanConfigBounds(t *testing.T) {
	cfg := normalizeAutoCleanConfig(AutoCleanConfig{
		Interval: time.Second, MinAge: time.Second, MaxDeletes: 0,
	})
	if cfg.Interval < time.Minute || cfg.MinAge < time.Minute || cfg.MaxDeletes < 1 {
		t.Fatalf("bounds not applied: %+v", cfg)
	}
}

type fakeBuildCleanStore struct {
	items   []BuildAutoCleanAccount
	deleted []int64
}

func (f *fakeBuildCleanStore) List(context.Context) ([]BuildAutoCleanAccount, error) {
	return f.items, nil
}
func (f *fakeBuildCleanStore) Delete(_ context.Context, id int64) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func TestRunBuildExpiredAccountAutoClean(t *testing.T) {
	old := time.Now().UTC().Add(-48 * time.Hour)
	fresh := time.Now().UTC().Add(-1 * time.Hour)
	store := &fakeBuildCleanStore{items: []BuildAutoCleanAccount{
		{ID: 1, Status: "expired", UpdatedAt: old},
		{ID: 2, Status: "expired", UpdatedAt: fresh},
		{ID: 3, Status: "active", UpdatedAt: old},
	}}
	result, err := RunBuildExpiredAccountAutoClean(context.Background(), store, AutoCleanConfig{
		Enabled: true, MinAge: 24 * time.Hour, MaxDeletes: 10, Interval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 1 || len(store.deleted) != 1 || store.deleted[0] != 1 {
		t.Fatalf("result=%+v deleted=%v", result, store.deleted)
	}
}
