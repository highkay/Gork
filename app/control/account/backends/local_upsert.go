package backends

import (
	"context"
	"database/sql"

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
	quotaCache := map[string]quotaJSONSet{}
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

type quotaJSONSet struct {
	Auto    string
	Fast    string
	Expert  string
	Heavy   string
	Grok43  string
	Console string
}

func cachedLocalQuotaJSON(cache map[string]quotaJSONSet, pool string) (quotaJSONSet, error) {
	if quota, ok := cache[pool]; ok {
		return quota, nil
	}
	quota, err := quotaJSONFromSet(account.DefaultQuotaSet(pool))
	if err != nil {
		return quotaJSONSet{}, err
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
	quota quotaJSONSet,
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

func quotaJSONFromSet(quotaSet account.AccountQuotaSet) (quotaJSONSet, error) {
	auto, err := quotaJSON(quotaSet.Auto)
	if err != nil {
		return quotaJSONSet{}, err
	}
	fast, err := quotaJSON(quotaSet.Fast)
	if err != nil {
		return quotaJSONSet{}, err
	}
	expert, err := quotaJSON(quotaSet.Expert)
	if err != nil {
		return quotaJSONSet{}, err
	}
	heavy, err := optionalQuotaJSON(quotaSet.Heavy)
	if err != nil {
		return quotaJSONSet{}, err
	}
	grok43, err := optionalQuotaJSON(quotaSet.Grok43)
	if err != nil {
		return quotaJSONSet{}, err
	}
	console, err := optionalQuotaJSON(quotaSet.Console)
	if err != nil {
		return quotaJSONSet{}, err
	}
	return quotaJSONSet{Auto: auto, Fast: fast, Expert: expert, Heavy: heavy, Grok43: grok43, Console: console}, nil
}

func quotaJSON(window account.QuotaWindow) (string, error) {
	raw, err := jsonString(window.ToDict())
	if err != nil {
		return "", err
	}
	return raw, nil
}

func optionalQuotaJSON(window *account.QuotaWindow) (string, error) {
	if window == nil {
		return "{}", nil
	}
	return quotaJSON(*window)
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
	quota_auto     = excluded.quota_auto,
	quota_fast     = excluded.quota_fast,
	quota_expert   = excluded.quota_expert,
	quota_heavy    = excluded.quota_heavy,
	quota_grok_4_3 = excluded.quota_grok_4_3,
	quota_console  = excluded.quota_console,
	ext            = excluded.ext,
	revision       = excluded.revision
`
