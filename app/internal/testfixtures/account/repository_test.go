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

func TestRepositoryReturnsDeepClonedRecords(t *testing.T) {
	page := accountcontrol.AccountPage{
		Items: []accountcontrol.AccountRecord{{
			Token: "tok-nested",
			Quota: map[string]any{
				"fast": map[string]any{"remaining": float64(3)},
				"items": []any{
					map[string]any{"value": "keep"},
				},
			},
			Ext: map[string]any{
				"nested": map[string]any{"value": "keep"},
			},
		}},
		Total: 1, Page: 1, PageSize: 50, TotalPages: 1, Revision: 9,
	}
	repo := NewRepositoryWithPage(page)

	listed, err := repo.ListAccounts(context.Background(), accountcontrol.ListAccountsQuery{})
	if err != nil {
		t.Fatalf("ListAccounts returned error: %v", err)
	}
	listed.Items[0].Quota["fast"].(map[string]any)["remaining"] = float64(0)
	listed.Items[0].Quota["items"].([]any)[0].(map[string]any)["value"] = "changed"
	listed.Items[0].Ext["nested"].(map[string]any)["value"] = "changed"

	snapshot, err := repo.RuntimeSnapshot(context.Background())
	if err != nil {
		t.Fatalf("RuntimeSnapshot returned error: %v", err)
	}
	quota := snapshot.Items[0].Quota
	ext := snapshot.Items[0].Ext
	if quota["fast"].(map[string]any)["remaining"] != float64(3) ||
		quota["items"].([]any)[0].(map[string]any)["value"] != "keep" ||
		ext["nested"].(map[string]any)["value"] != "keep" {
		t.Fatalf("repository returned shared nested state: quota=%#v ext=%#v", quota, ext)
	}
}
