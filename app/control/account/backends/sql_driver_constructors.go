package backends

import (
	"database/sql"

	account "github.com/dslzl/gork/app/control/account"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func newMySQLRepository(rawURL string) (account.AccountRepository, error) {
	return getOrCreateSQLRepository(SQLDialectMySQL, rawURL, func(connectString string) (*sql.DB, error) {
		return sql.Open("mysql", connectString)
	})
}

func newPostgreSQLRepository(rawURL string) (account.AccountRepository, error) {
	return getOrCreateSQLRepository(SQLDialectPostgreSQL, rawURL, func(connectString string) (*sql.DB, error) {
		return sql.Open("pgx", connectString)
	})
}
