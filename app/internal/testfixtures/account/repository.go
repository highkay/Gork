package accountfixtures

import (
	"context"
	"maps"
	"slices"

	accountcontrol "github.com/dslzl/gork/app/control/account"
)

var _ accountcontrol.AccountRepository = (*Repository)(nil)

// Repository is a small in-memory account repository for package tests.
type Repository struct {
	Page    accountcontrol.AccountPage
	Queries []accountcontrol.ListAccountsQuery
}

// NewRepositoryWithPage returns a repository that serves the provided page.
func NewRepositoryWithPage(page accountcontrol.AccountPage) *Repository {
	page.Items = cloneRecords(page.Items)
	return &Repository{Page: page}
}

func (r *Repository) Initialize(context.Context) error {
	return nil
}

func (r *Repository) GetRevision(context.Context) (int, error) {
	return r.Page.Revision, nil
}

func (r *Repository) RuntimeSnapshot(context.Context) (accountcontrol.RuntimeSnapshot, error) {
	return accountcontrol.RuntimeSnapshot{Revision: r.Page.Revision, Items: cloneRecords(r.Page.Items)}, nil
}

func (r *Repository) ScanChanges(context.Context, int, int) (accountcontrol.AccountChangeSet, error) {
	return accountcontrol.AccountChangeSet{Revision: r.Page.Revision, Items: cloneRecords(r.Page.Items), DeletedTokens: []string{}}, nil
}

func (r *Repository) UpsertAccounts(_ context.Context, upserts []accountcontrol.AccountUpsert) (accountcontrol.AccountMutationResult, error) {
	return accountcontrol.AccountMutationResult{Upserted: len(upserts), Revision: r.Page.Revision}, nil
}

func (r *Repository) PatchAccounts(_ context.Context, patches []accountcontrol.AccountPatch) (accountcontrol.AccountMutationResult, error) {
	return accountcontrol.AccountMutationResult{Patched: len(patches), Revision: r.Page.Revision}, nil
}

func (r *Repository) DeleteAccounts(_ context.Context, tokens []string) (accountcontrol.AccountMutationResult, error) {
	return accountcontrol.AccountMutationResult{Deleted: len(tokens), Revision: r.Page.Revision}, nil
}

func (r *Repository) GetAccounts(_ context.Context, tokens []string) ([]accountcontrol.AccountRecord, error) {
	if len(tokens) == 0 {
		return []accountcontrol.AccountRecord{}, nil
	}
	wanted := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		wanted[token] = struct{}{}
	}
	out := []accountcontrol.AccountRecord{}
	for _, record := range r.Page.Items {
		if _, ok := wanted[record.Token]; ok {
			out = append(out, record)
		}
	}
	return cloneRecords(out), nil
}

func (r *Repository) ListAccounts(_ context.Context, query accountcontrol.ListAccountsQuery) (accountcontrol.AccountPage, error) {
	r.Queries = append(r.Queries, query)
	page := r.Page
	page.Items = cloneRecords(page.Items)
	return page, nil
}

func (r *Repository) ReplacePool(_ context.Context, command accountcontrol.BulkReplacePoolCommand) (accountcontrol.AccountMutationResult, error) {
	return accountcontrol.AccountMutationResult{Upserted: len(command.Upserts), Revision: r.Page.Revision}, nil
}

func (r *Repository) Close(context.Context) error {
	return nil
}

func cloneRecords(records []accountcontrol.AccountRecord) []accountcontrol.AccountRecord {
	out := make([]accountcontrol.AccountRecord, 0, len(records))
	for _, record := range records {
		record.Tags = slices.Clone(record.Tags)
		record.Quota = cloneMap(record.Quota)
		record.Ext = cloneMap(record.Ext)
		out = append(out, record)
	}
	return out
}

func cloneMap(input map[string]any) map[string]any {
	return maps.Clone(input)
}
