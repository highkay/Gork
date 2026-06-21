package backends

import (
	"context"
	"database/sql"
	"fmt"

	account "github.com/dslzl/gork/app/control/account"
	platformruntime "github.com/dslzl/gork/app/platform/runtime"
)

func upsertLocalAccounts(
	ctx context.Context,
	tx *sql.Tx,
	items []account.AccountUpsert,
	revision int,
) (int, error) {
	ts := platformruntime.NowMS()
	count := 0
	stmt, err := tx.PrepareContext(ctx, localUpsertSQL)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	quotaCache := map[string]localQuotaJSON{}
	for _, item := range items {
		token, pool, ok := normalizeLocalUpsert(item)
		if !ok {
			continue
		}
		quota, err := cachedLocalQuotaJSON(quotaCache, pool)
		if err != nil {
			return 0, err
		}
		affected, err := upsertLocalAccount(ctx, stmt, item, token, pool, ts, revision, quota)
		if err != nil {
			return 0, err
		}
		count += affected
	}
	return count, nil
}

func normalizeLocalUpsert(item account.AccountUpsert) (string, string, bool) {
	item.Normalize()
	record, err := account.NewAccountRecord(account.AccountRecord{Token: item.Token, Pool: item.Pool})
	if err != nil {
		return "", "", false
	}
	pool := "basic"
	if item.Pool == "basic" || item.Pool == "super" || item.Pool == "heavy" {
		pool = item.Pool
	}
	return record.Token, pool, true
}

type localQuotaJSON struct {
	Auto    string
	Fast    string
	Expert  string
	Heavy   string
	Grok43  string
	Console string
}

func cachedLocalQuotaJSON(cache map[string]localQuotaJSON, pool string) (localQuotaJSON, error) {
	if quota, ok := cache[pool]; ok {
		return quota, nil
	}
	quotaSet := account.DefaultQuotaSet(pool)
	quota := localQuotaJSON{
		Auto:    mustQuotaJSON(quotaSet.Auto),
		Fast:    mustQuotaJSON(quotaSet.Fast),
		Expert:  mustQuotaJSON(quotaSet.Expert),
		Heavy:   optionalQuotaJSON(quotaSet.Heavy),
		Grok43:  optionalQuotaJSON(quotaSet.Grok43),
		Console: optionalQuotaJSON(quotaSet.Console),
	}
	cache[pool] = quota
	return quota, nil
}

func upsertLocalAccount(
	ctx context.Context,
	stmt *sql.Stmt,
	item account.AccountUpsert,
	token string,
	pool string,
	ts int64,
	revision int,
	quota localQuotaJSON,
) (int, error) {
	item.Normalize()
	tags, err := jsonString(item.Tags)
	if err != nil {
		return 0, err
	}
	ext, err := jsonString(item.Ext)
	if err != nil {
		return 0, err
	}
	result, err := stmt.ExecContext(ctx, token, pool, ts, ts, tags,
		quota.Auto, quota.Fast, quota.Expert, quota.Heavy, quota.Grok43, quota.Console, ext, revision)
	if err != nil {
		return 0, err
	}
	return affectedRows(result)
}

func mustQuotaJSON(window account.QuotaWindow) string {
	raw, err := jsonString(window.ToDict())
	if err != nil {
		panic(fmt.Errorf("quota json: %w", err))
	}
	return raw
}

func optionalQuotaJSON(window *account.QuotaWindow) string {
	if window == nil {
		return "{}"
	}
	return mustQuotaJSON(*window)
}

const localUpsertSQL = `
INSERT INTO accounts (
	token, pool, status, created_at, updated_at,
	tags, quota_auto, quota_fast, quota_expert, quota_heavy, quota_grok_4_3, quota_console,
	usage_use_count, usage_fail_count, usage_sync_count,
	ext, revision
) VALUES (
	?, ?, 'active', ?, ?,
	?, ?, ?, ?, ?, ?, ?,
	0, 0, 0, ?, ?
)
ON CONFLICT(token) DO UPDATE SET
	pool           = excluded.pool,
	status         = 'active',
	deleted_at     = NULL,
	updated_at     = excluded.updated_at,
	tags           = excluded.tags,
	quota_console  = excluded.quota_console,
	ext            = excluded.ext,
	revision       = excluded.revision
`
