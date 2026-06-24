package admin

import (
	"context"
	"testing"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	accountdataplane "github.com/dslzl/gork/app/dataplane/account"
	accountfixtures "github.com/dslzl/gork/app/internal/testfixtures/account"
)

func TestBindAccountRuntimeWiresDefaultAdminProviders(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := accountfixtures.NewRepositoryWithPage(accountcontrol.AccountPage{
		Items: []accountcontrol.AccountRecord{{
			Token:  "tok-live",
			Pool:   "basic",
			Status: accountcontrol.AccountStatusActive,
			Tags:   []string{"nsfw"},
		}},
		Total: 1, Page: 1, PageSize: 50, TotalPages: 1, Revision: 3,
	})
	directory := accountdataplane.NewAccountDirectory(repo)
	cleanup := BindAccountRuntime(repo, directory, nil)
	t.Cleanup(cleanup)

	if adminAccountDirectory() != directory {
		t.Fatalf("admin directory provider was not wired")
	}
	provided := defaultAdminTokensRepoProvider()
	if provided == nil {
		t.Fatalf("default admin tokens repo provider returned nil")
	}
	result, err := provided.ListAccounts(context.Background(), adminAssetsListQuery{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("ListAccounts returned error: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Token != "tok-live" || result.Revision != 3 {
		t.Fatalf("admin list result = %#v", result)
	}
	if repo.Queries[0].IncludeDeleted {
		t.Fatalf("admin runtime list should not include soft-deleted tokens: %#v", repo.Queries[0])
	}

	cleanup()
	if adminAccountDirectory() != nil {
		t.Fatalf("cleanup did not restore directory provider")
	}
}

func TestAccountRuntimeBindingInterfaceWiresDefaultAdminProviders(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := accountfixtures.NewRepositoryWithPage(accountcontrol.AccountPage{
		Items: []accountcontrol.AccountRecord{{Token: "tok-bound", Status: accountcontrol.AccountStatusActive}},
		Total: 1, Page: 1, PageSize: 50, TotalPages: 1, Revision: 7,
	})
	directory := accountdataplane.NewAccountDirectory(repo)
	var binding AccountRuntimeBinding = NewAccountRuntimeBinding(repo, directory, nil)

	cleanup := binding.Bind()
	t.Cleanup(cleanup)

	if adminAccountDirectory() != directory {
		t.Fatalf("admin directory provider was not wired")
	}
	result, err := defaultAdminTokensRepoProvider().ListAccounts(context.Background(), adminAssetsListQuery{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("ListAccounts returned error: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Token != "tok-bound" || result.Revision != 7 {
		t.Fatalf("admin list result = %#v", result)
	}

	cleanup()
	if adminAccountDirectory() != nil {
		t.Fatalf("cleanup did not restore directory provider")
	}
}
