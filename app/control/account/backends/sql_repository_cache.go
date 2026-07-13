package backends

import (
	"database/sql"
	"sync"
)

var (
	sqlRepositoryCacheMu sync.Mutex
	sqlRepositoryCache   = map[string]*SQLAccountRepository{}
)

func getOrCreateSQLRepository(dialect SQLDialect, rawURL string, open func(string) (*sql.DB, error)) (*SQLAccountRepository, error) {
	connectString, err := prepareSQLConnectString(dialect, rawURL)
	if err != nil {
		return nil, err
	}
	cacheKey := string(dialect) + "\x00" + connectString

	sqlRepositoryCacheMu.Lock()
	if repo := sqlRepositoryCache[cacheKey]; repo != nil {
		sqlRepositoryCacheMu.Unlock()
		return repo, nil
	}
	sqlRepositoryCacheMu.Unlock()

	db, err := open(connectString)
	if err != nil {
		return nil, err
	}
	configureSQLPool(db)
	repo := NewSQLAccountRepository(db, dialect, true)
	repo.cacheKey = cacheKey

	sqlRepositoryCacheMu.Lock()
	if cached := sqlRepositoryCache[cacheKey]; cached != nil {
		sqlRepositoryCacheMu.Unlock()
		_ = db.Close()
		return cached, nil
	}
	sqlRepositoryCache[cacheKey] = repo
	sqlRepositoryCacheMu.Unlock()
	return repo, nil
}

func evictSQLRepositoryCache(cacheKey string, repo *SQLAccountRepository) {
	sqlRepositoryCacheMu.Lock()
	defer sqlRepositoryCacheMu.Unlock()
	if sqlRepositoryCache[cacheKey] == repo {
		delete(sqlRepositoryCache, cacheKey)
	}
}
