package account

type selectionCandidateCounts struct {
	total              int
	available          int
	cooling            int
	invalidCredentials int
	disabled           int
	inflight           int
}

func selectionMaxInflight() int {
	value := directoryConfigSource.GetInt("account.selection.max_inflight", defaultMaxInflight)
	if value <= 0 {
		return defaultMaxInflight
	}
	return value
}

func classifyReserveFailure(table *AccountRuntimeTable, pools []int, modeID int, options SelectOptions, anyMode bool) ReserveFailureReason {
	counts := countSelectionCandidates(table, pools, modeID, options, anyMode)
	if counts.total == 0 {
		return ReserveFailureNoAvailable
	}
	if counts.available > 0 {
		return ReserveFailureNoAvailable
	}
	if counts.disabled == counts.total {
		return ReserveFailureDisabled
	}
	if counts.invalidCredentials == counts.total {
		return ReserveFailureInvalidCredentials
	}
	if counts.cooling > 0 {
		return ReserveFailureRateLimited
	}
	return ReserveFailureNoAvailable
}

func countSelectionCandidates(table *AccountRuntimeTable, pools []int, modeID int, options SelectOptions, anyMode bool) selectionCandidateCounts {
	counts := selectionCandidateCounts{}
	if table == nil {
		return counts
	}
	poolSet := map[int]bool{}
	for _, poolID := range pools {
		poolSet[poolID] = true
	}
	maxInflight := options.MaxInflight
	if maxInflight <= 0 {
		maxInflight = defaultMaxInflight
	}
	for idx := range table.TokenByIdx {
		if table.StatusByIdx[idx] == StatusDeleted {
			continue
		}
		if len(poolSet) > 0 && !poolSet[table.PoolByIdx[idx]] {
			continue
		}
		if options.ExcludeIdxs[idx] {
			continue
		}
		if !slotSupportsMode(table, idx, modeID, anyMode) {
			continue
		}
		counts.total++
		counts.inflight += table.InflightByIdx[idx]
		switch table.StatusByIdx[idx] {
		case StatusDisabled:
			counts.disabled++
		case StatusExpired:
			counts.invalidCredentials++
		case StatusCooling:
			counts.cooling++
		case StatusActive:
			if table.CoolingUntilSByIdx[idx] > options.NowS {
				counts.cooling++
				continue
			}
			if table.InflightByIdx[idx] >= maxInflight {
				counts.cooling++
				continue
			}
			if !slotHasSelectableQuota(table, idx, modeID, anyMode) {
				counts.cooling++
				continue
			}
			counts.available++
		default:
			counts.cooling++
		}
	}
	return counts
}

func slotSupportsMode(table *AccountRuntimeTable, idx int, modeID int, anyMode bool) bool {
	if anyMode {
		for _, candidate := range allModeIDs {
			if table.WindowFor(idx, candidate) > 0 {
				return true
			}
		}
		return false
	}
	return isKnownModeID(modeID) && table.WindowFor(idx, modeID) > 0
}

func slotHasSelectableQuota(table *AccountRuntimeTable, idx int, modeID int, anyMode bool) bool {
	if anyMode {
		for _, candidate := range allModeIDs {
			if table.WindowFor(idx, candidate) > 0 && table.QuotaFor(idx, candidate) > 0 {
				return true
			}
		}
		return false
	}
	return table.QuotaFor(idx, modeID) > 0
}
