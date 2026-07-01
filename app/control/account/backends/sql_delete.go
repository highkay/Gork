package backends

import (
	"context"

	account "github.com/dslzl/gork/app/control/account"
	platformruntime "github.com/dslzl/gork/app/platform/runtime"
)

func (r *SQLAccountRepository) deleteAccountsInTx(
	ctx context.Context,
	tokens []string,
) (account.AccountMutationResult, error) {
	tx, err := r.beginSQLMutation(ctx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	defer tx.Rollback()
	revision, err := bumpSQLRevision(ctx, tx, r.dialect)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	count, err := deleteSQLAccounts(ctx, tx, r.dialect, tokens, revision)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return account.AccountMutationResult{}, err
	}
	return account.AccountMutationResult{Deleted: count, Revision: revision}, nil
}

func deleteSQLAccounts(
	ctx context.Context,
	tx localSQLRunner,
	dialect SQLDialect,
	tokens []string,
	revision int,
) (int, error) {
	ts := platformruntime.NowMS()
	args := []any{ts, ts, revision}
	for _, token := range tokens {
		args = append(args, token)
	}
	query := "UPDATE accounts SET deleted_at = " + sqlBind(dialect, 1) +
		", updated_at = " + sqlBind(dialect, 2) +
		", revision = " + sqlBind(dialect, 3) +
		" WHERE token IN (" + sqlPlaceholders(dialect, 4, len(tokens)) + ") AND deleted_at IS NULL"
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return affectedRows(result)
}

func deleteSQLPool(
	ctx context.Context,
	tx localSQLRunner,
	dialect SQLDialect,
	pool string,
	revision int,
) (int, error) {
	ts := platformruntime.NowMS()
	query := "UPDATE accounts SET deleted_at = " + sqlBind(dialect, 1) +
		", updated_at = " + sqlBind(dialect, 2) +
		", revision = " + sqlBind(dialect, 3) +
		" WHERE pool = " + sqlBind(dialect, 4) + " AND deleted_at IS NULL"
	result, err := tx.ExecContext(ctx, query, ts, ts, revision, pool)
	if err != nil {
		return 0, err
	}
	return affectedRows(result)
}
