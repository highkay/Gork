package accountfixtures

import (
	"context"
	"testing"

	accountcontrol "github.com/dslzl/gork/app/control/account"
)

func TestRepositoryCapturesListQueriesAndRuntimeSnapshot(t *testing.T) {
	page := accountcontrol.AccountPage{
		Items: []accountcontrol.AccountRecord{{
			Token:  "tok-fixture",
			Status: accountcontrol.AccountStatusActive,
		}},
		Total: 1, Page: 1, PageSize: 50, TotalPages: 1, Revision: 9,
	}
	repo := NewRepositoryWithPage(page)

	result, err := repo.ListAccounts(context.Background(), accountcontrol.ListAccountsQuery{Page: 2, PageSize: 25, IncludeDeleted: true})
	if err != nil {
		t.Fatalf("ListAccounts returned error: %v", err)
	}
	if result.Revision != 9 || len(result.Items) != 1 || result.Items[0].Token != "tok-fixture" {
		t.Fatalf("ListAccounts result = %#v", result)
	}
	if len(repo.Queries) != 1 || !repo.Queries[0].IncludeDeleted || repo.Queries[0].Page != 2 {
		t.Fatalf("captured queries = %#v", repo.Queries)
	}

	snapshot, err := repo.RuntimeSnapshot(context.Background())
	if err != nil {
		t.Fatalf("RuntimeSnapshot returned error: %v", err)
	}
	if snapshot.Revision != 9 || len(snapshot.Items) != 1 || snapshot.Items[0].Token != "tok-fixture" {
		t.Fatalf("RuntimeSnapshot = %#v", snapshot)
	}
}
