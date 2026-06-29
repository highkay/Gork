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

func redisSortValue(record account.AccountRecord, field string) int {
	switch field {
	case "created_at":
		return int(record.CreatedAt)
	case "updated_at":
		return int(record.UpdatedAt)
	case "last_use_at":
		return intFromPtr(record.LastUseAt)
	case "last_fail_at":
		return intFromPtr(record.LastFailAt)
	case "last_sync_at":
		return intFromPtr(record.LastSyncAt)
	case "last_clear_at":
		return intFromPtr(record.LastClearAt)
	case "usage_use_count":
		return record.UsageUseCount
	case "usage_fail_count":
		return record.UsageFailCount
	case "usage_sync_count":
		return record.UsageSyncCount
	default:
		return 0
	}
}

func intFromPtr(value *int64) int {
	if value == nil {
		return 0
	}
	return int(*value)
}
