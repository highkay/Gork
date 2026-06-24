package admin

import (
	accountcontrol "github.com/dslzl/gork/app/control/account"
	accountdataplane "github.com/dslzl/gork/app/dataplane/account"
)

// AccountRuntimeBinding wires control-plane runtime services into admin handlers.
type AccountRuntimeBinding interface {
	Bind() func()
}

type accountRuntimeBinding struct {
	repo           accountcontrol.AccountRepository
	directory      *accountdataplane.AccountDirectory
	refreshService *accountcontrol.AccountRefreshService
	providers      adminRuntimeProviderBinder
}

type adminRuntimeProviderBinder interface {
	bindAssetsRepo(func() adminAssetsRepository) func()
	bindDirectory(func() adminDirectory) func()
	bindBatchRefresh(func() adminBatchRefreshService) func()
	bindTokensRefresh(func() adminTokensRefreshService) func()
}

type adminRuntimeGlobalProviders struct{}

// NewAccountRuntimeBinding adapts account runtime services to admin providers.
func NewAccountRuntimeBinding(
	repo accountcontrol.AccountRepository,
	directory *accountdataplane.AccountDirectory,
	refreshService *accountcontrol.AccountRefreshService,
) AccountRuntimeBinding {
	return accountRuntimeBinding{
		repo:           repo,
		directory:      directory,
		refreshService: refreshService,
		providers:      adminRuntimeGlobalProviders{},
	}
}

func (b accountRuntimeBinding) Bind() func() {
	cleanups := []func(){}
	if b.repo != nil {
		adapter := accountRuntimeRepository{repo: b.repo}
		cleanups = append(cleanups, b.providers.bindAssetsRepo(func() adminAssetsRepository {
			return adapter
		}))
	}
	if b.directory != nil {
		cleanups = append(cleanups, b.providers.bindDirectory(func() adminDirectory {
			return b.directory
		}))
	}
	if b.refreshService != nil {
		adapter := accountRuntimeRefreshService{service: b.refreshService}
		cleanups = append(cleanups, b.providers.bindBatchRefresh(func() adminBatchRefreshService {
			return adapter
		}))
		cleanups = append(cleanups, b.providers.bindTokensRefresh(func() adminTokensRefreshService {
			return adapter
		}))
	}
	return func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
}

func (adminRuntimeGlobalProviders) bindAssetsRepo(provider func() adminAssetsRepository) func() {
	previous := adminAssetsRepoProvider
	adminAssetsRepoProvider = provider
	return func() {
		adminAssetsRepoProvider = previous
	}
}

func (adminRuntimeGlobalProviders) bindDirectory(provider func() adminDirectory) func() {
	previous := adminAccountDirectory
	adminAccountDirectory = provider
	return func() {
		adminAccountDirectory = previous
	}
}

func (adminRuntimeGlobalProviders) bindBatchRefresh(provider func() adminBatchRefreshService) func() {
	previous := adminBatchRefreshServiceProvider
	adminBatchRefreshServiceProvider = provider
	return func() {
		adminBatchRefreshServiceProvider = previous
	}
}

func (adminRuntimeGlobalProviders) bindTokensRefresh(provider func() adminTokensRefreshService) func() {
	previous := adminTokensRefreshServiceProvider
	adminTokensRefreshServiceProvider = provider
	return func() {
		adminTokensRefreshServiceProvider = previous
	}
}
