package backends

import (
	"cmp"
	"slices"

	account "github.com/dslzl/gork/app/control/account"
)

func sortRedisRecords(records []account.AccountRecord, sortBy string, desc bool) {
	slices.SortFunc(records, func(leftRecord, rightRecord account.AccountRecord) int {
		left := redisSortValue(leftRecord, sortBy)
		right := redisSortValue(rightRecord, sortBy)
		if left == right {
			if desc {
				return cmp.Compare(rightRecord.Token, leftRecord.Token)
			}
			return cmp.Compare(leftRecord.Token, rightRecord.Token)
		}
		if desc {
			return cmp.Compare(right, left)
		}
		return cmp.Compare(left, right)
	})
}

func redisSortValue(record account.AccountRecord, field string) int64 {
	switch field {
	case "created_at":
		return record.CreatedAt
	case "updated_at":
		return record.UpdatedAt
	case "last_use_at":
		return int64FromPtr(record.LastUseAt)
	case "last_fail_at":
		return int64FromPtr(record.LastFailAt)
	case "last_sync_at":
		return int64FromPtr(record.LastSyncAt)
	case "last_clear_at":
		return int64FromPtr(record.LastClearAt)
	case "usage_use_count":
		return int64(record.UsageUseCount)
	case "usage_fail_count":
		return int64(record.UsageFailCount)
	case "usage_sync_count":
		return int64(record.UsageSyncCount)
	default:
		return 0
	}
}

func int64FromPtr(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
