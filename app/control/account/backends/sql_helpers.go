package backends

import (
	"fmt"
	"strings"
)

func sqlPlaceholders(dialect SQLDialect, start int, count int) string {
	if count <= 0 {
		return ""
	}
	items := make([]string, count)
	for i := range items {
		items[i] = sqlBind(dialect, start+i)
	}
	return strings.Join(items, ",")
}

func sqlScanRevisionChangesQuery(dialect SQLDialect) string {
	return "SELECT " + localAccountColumns + " FROM accounts WHERE revision = " +
		sqlBind(dialect, 1) + " ORDER BY token"
}

func safeSQLSort(sortBy string) string {
	switch sortBy {
	case "token", "pool", "status", "created_at", "updated_at", "tags",
		"usage_use_count", "usage_fail_count", "usage_sync_count",
		"last_use_at", "last_fail_at", "last_sync_at", "last_clear_at",
		"state_reason", "deleted_at", "revision":
		return sortBy
	default:
		return "updated_at"
	}
}

func safeAccountSQLColumn(column string) (string, error) {
	switch column {
	case "token", "pool", "status", "created_at", "updated_at", "tags",
		"quota_auto", "quota_fast", "quota_expert", "quota_heavy", "quota_grok_4_3", "quota_console",
		"usage_use_count", "usage_fail_count", "usage_sync_count",
		"last_use_at", "last_fail_at", "last_fail_reason", "last_sync_at", "last_clear_at",
		"state_reason", "deleted_at", "ext", "revision":
		return column, nil
	default:
		return "", fmt.Errorf("invalid account SQL column: %s", column)
	}
}

func sqlAssignments(dialect SQLDialect, sets []localPatchSet) (string, []any) {
	assignments := make([]string, 0, len(sets))
	values := make([]any, 0, len(sets))
	for _, set := range dedupeSQLPatchSets(sets) {
		column, err := safeAccountSQLColumn(set.column)
		if err != nil {
			continue
		}
		values = append(values, set.value)
		assignments = append(assignments, column+" = "+sqlBind(dialect, len(values)))
	}
	return strings.Join(assignments, ", "), values
}

func dedupeSQLPatchSets(sets []localPatchSet) []localPatchSet {
	seen := map[string]bool{}
	reversed := make([]localPatchSet, 0, len(sets))
	for i := len(sets) - 1; i >= 0; i-- {
		if seen[sets[i].column] {
			continue
		}
		seen[sets[i].column] = true
		reversed = append(reversed, sets[i])
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed
}
