package account

import (
	"context"
	"testing"

	controlaccount "github.com/dslzl/gork/app/control/account"
	appruntime "github.com/dslzl/gork/app/platform/runtime"
)

func TestAccountDirectoryBootstrapSyncAndDiagnosticsMatchPython(t *testing.T) {
	ctx := context.Background()
	repo := &syncFakeRepository{snapshot: controlaccount.RuntimeSnapshot{
		Revision: 2,
		Items: []controlaccount.AccountRecord{
			accountRecord("first", "basic", basicQuotaSet(8), []string{"blue"}),
		},
	}}
	directory := NewAccountDirectory(repo)

	changed, err := directory.SyncIfChanged(ctx)
	if err != nil {
		t.Fatalf("SyncIfChanged before bootstrap returned error: %v", err)
	}
	if changed || len(repo.scanCalls) != 0 {
		t.Fatalf("SyncIfChanged before bootstrap changed=%t scanCalls=%#v", changed, repo.scanCalls)
	}
	if directory.Size() != 0 || directory.Revision() != 0 {
		t.Fatalf("initial diagnostics = size %d revision %d", directory.Size(), directory.Revision())
	}

	if err := directory.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}
	if directory.Size() != 1 || directory.Revision() != 2 {
		t.Fatalf("post-bootstrap diagnostics = size %d revision %d", directory.Size(), directory.Revision())
	}

	repo.changes = []controlaccount.AccountChangeSet{{
		Revision: 3,
		Items: []controlaccount.AccountRecord{
			accountRecord("second", "super", superQuotaSet(11), []string{"green"}),
		},
	}}
	changed, err = directory.SyncIfChanged(ctx)
	if err != nil {
		t.Fatalf("SyncIfChanged returned error: %v", err)
	}
	if !changed || directory.Size() != 2 || directory.Revision() != 3 {
		t.Fatalf("sync result changed=%t size=%d revision=%d", changed, directory.Size(), directory.Revision())
	}
	if len(repo.scanCalls) != 1 || repo.scanCalls[0] != 2 {
		t.Fatalf("ScanChanges revisions = %#v", repo.scanCalls)
	}
}

func TestAccountDirectoryReserveReleaseMatchesPython(t *testing.T) {
	mustSetDirectoryStrategy(t, "quota")
	ctx := context.Background()
	repo := &syncFakeRepository{snapshot: controlaccount.RuntimeSnapshot{
		Revision: 1,
		Items: []controlaccount.AccountRecord{
			accountRecord("basic-blue", "basic", basicQuotaSet(7), []string{"blue"}),
			accountRecord("basic-red", "basic", basicQuotaSet(9), []string{"red"}),
			accountRecord("super-gold", "super", superQuotaSet(13), []string{"gold"}),
		},
	}}
	directory := NewAccountDirectory(repo)
	if err := directory.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}

	now := 1234
	lease, ok := directory.Reserve(
		[]int{0, 1},
		1,
		ReserveOptions{
			ExcludeTokens: []string{"basic-blue"},
			PreferTags:    []string{"red"},
			NowS:          intPtr(now),
		},
	)
	if !ok {
		t.Fatalf("Reserve returned no lease")
	}
	if lease.Token != "basic-red" || lease.PoolID != 0 || lease.ModeID != 1 || lease.SelectedAt != now {
		t.Fatalf("lease = %#v", lease)
	}
	idx := directory.table.IdxByToken["basic-red"]
	if directory.table.InflightByIdx[idx] != 1 || directory.table.LastUseAtByIdx[idx] != now {
		t.Fatalf("reserve counters inflight=%d lastUse=%d", directory.table.InflightByIdx[idx], directory.table.LastUseAtByIdx[idx])
	}

	directory.Release(lease)
	if directory.table.InflightByIdx[idx] != 0 {
		t.Fatalf("release inflight = %d", directory.table.InflightByIdx[idx])
	}

	_, ok = directory.Reserve(
		0,
		1,
		ReserveOptions{
			ExcludeTokens: []string{"basic-blue", "basic-red"},
			NowS:          intPtr(now),
		},
	)
	if ok {
		t.Fatalf("Reserve with an int pool candidate selected an excluded account")
	}
}

func TestAccountDirectoryReserveDetailedReportsFailureReason(t *testing.T) {
	mustSetDirectoryStrategy(t, "quota")
	now := 100
	tests := []struct {
		name   string
		table  *AccountRuntimeTable
		reason ReserveFailureReason
	}{
		{name: "empty", table: MakeEmptyTable(), reason: ReserveFailureNoAvailable},
		{name: "rate limited", table: reserveFailureTable(StatusActive, 0, 0, 0), reason: ReserveFailureRateLimited},
		{name: "invalid credentials", table: reserveFailureTable(StatusExpired, 1, 0, 0), reason: ReserveFailureInvalidCredentials},
		{name: "disabled", table: reserveFailureTable(StatusDisabled, 1, 0, 0), reason: ReserveFailureDisabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := accountDirectoryWithTable(tt.table)
			lease, reason, ok := directory.ReserveDetailed(0, 1, ReserveOptions{NowS: intPtr(now)})
			if ok || lease.Token != "" {
				t.Fatalf("ReserveDetailed selected lease=%#v ok=%t", lease, ok)
			}
			if reason != tt.reason {
				t.Fatalf("reason = %q, want %q", reason, tt.reason)
			}
		})
	}
}

func TestAccountDirectoryReserveUsesConfiguredMaxInflight(t *testing.T) {
	mustSetDirectoryStrategy(t, "random")
	oldConfig := directoryConfigSource
	directoryConfigSource = fakeDirectoryConfig{"account.selection.max_inflight": 1}
	t.Cleanup(func() { directoryConfigSource = oldConfig })

	directory := accountDirectoryWithTable(reserveFailureTable(StatusActive, 1, 1, 0))
	lease, reason, ok := directory.ReserveDetailed(0, 1, ReserveOptions{NowS: intPtr(10)})
	if ok || lease.Token != "" {
		t.Fatalf("ReserveDetailed selected lease=%#v ok=%t", lease, ok)
	}
	if reason != ReserveFailureRateLimited {
		t.Fatalf("reason = %q, want %q", reason, ReserveFailureRateLimited)
	}
}

func TestAccountDirectorySelectionStatusCountsAvailability(t *testing.T) {
	mustSetDirectoryStrategy(t, "random")
	oldConfig := directoryConfigSource
	directoryConfigSource = fakeDirectoryConfig{"account.selection.max_inflight": 2}
	t.Cleanup(func() { directoryConfigSource = oldConfig })

	table := MakeEmptyTable()
	table.AppendSlot(AccountSlot{Token: "available", PoolID: 0, StatusID: StatusActive, QuotaFast: 1, TotalFast: 10, WindowFast: 60, Health: 1})
	table.AppendSlot(AccountSlot{Token: "quota", PoolID: 0, StatusID: StatusActive, QuotaFast: 0, TotalFast: 10, WindowFast: 60, Health: 1})
	idx := table.AppendSlot(AccountSlot{Token: "cooling", PoolID: 0, StatusID: StatusActive, QuotaFast: 1, TotalFast: 10, WindowFast: 60, Health: 1})
	table.CoolingUntilSByIdx[idx] = 20
	table.AppendSlot(AccountSlot{Token: "invalid", PoolID: 0, StatusID: StatusExpired, QuotaFast: 1, TotalFast: 10, WindowFast: 60, Health: 1})
	table.AppendSlot(AccountSlot{Token: "disabled", PoolID: 0, StatusID: StatusDisabled, QuotaFast: 1, TotalFast: 10, WindowFast: 60, Health: 1})

	status := accountDirectoryWithTable(table).SelectionStatus(10)
	if status.Strategy != "random" || status.MaxInflight != 2 {
		t.Fatalf("strategy/max_inflight = %s/%d", status.Strategy, status.MaxInflight)
	}
	if status.Total != 5 || status.Available != 1 || status.Cooling != 2 || status.InvalidCredentials != 1 || status.Disabled != 1 {
		t.Fatalf("selection status = %#v", status)
	}
}

func TestAccountDirectoryReserveAnyMatchesPythonNoModeQuota(t *testing.T) {
	mustSetDirectoryStrategy(t, "quota")
	table := MakeEmptyTable()
	idx := table.AppendSlot(AccountSlot{
		Token:      "ws-token",
		PoolID:     0,
		StatusID:   StatusActive,
		QuotaAuto:  1,
		WindowAuto: 60,
		QuotaFast:  0,
		Health:     0.8,
		Tags:       []string{"ws"},
	})
	directory := accountDirectoryWithTable(table)

	now := 2468
	lease, ok := directory.ReserveAny(
		[]int{0},
		ReserveOptions{PreferTags: []string{"ws"}, NowS: intPtr(now)},
	)
	if !ok {
		t.Fatalf("ReserveAny returned no lease")
	}
	if lease.Token != "ws-token" || lease.ModeID != -1 || lease.SelectedAt != now {
		t.Fatalf("reserve_any lease = %#v", lease)
	}
	if table.InflightByIdx[idx] != 1 || table.LastUseAtByIdx[idx] != now {
		t.Fatalf("reserve_any counters inflight=%d lastUse=%d", table.InflightByIdx[idx], table.LastUseAtByIdx[idx])
	}
}

func TestAccountDirectoryFeedbackDispatchMatchesPython(t *testing.T) {
	mustSetDirectoryStrategy(t, "quota")
	table, idx := feedbackTable()
	directory := accountDirectoryWithTable(table)
	remaining, resetAtMS, now := 6, 123456, 777
	directory.Feedback("tok", controlaccount.FeedbackKindSuccess, 1, FeedbackOptions{
		Remaining: intPtr(remaining),
		ResetAtMS: intPtr(resetAtMS),
		NowS:      intPtr(now),
	})
	if table.QuotaFastByIdx[idx] != remaining || table.ResetFastAtByIdx[idx] != 123 {
		t.Fatalf("success quota/header update quota=%d reset=%d", table.QuotaFastByIdx[idx], table.ResetFastAtByIdx[idx])
	}

	table, idx = feedbackTable()
	directory = accountDirectoryWithTable(table)
	directory.Feedback("tok", controlaccount.FeedbackKindRateLimited, 1, FeedbackOptions{NowS: intPtr(now)})
	if table.QuotaFastByIdx[idx] != 0 || table.LastFailAtByIdx[idx] != now {
		t.Fatalf("quota rate-limit quota=%d lastFail=%d", table.QuotaFastByIdx[idx], table.LastFailAtByIdx[idx])
	}

	table, idx = feedbackTable()
	directory = accountDirectoryWithTable(table)
	directory.Feedback("tok", controlaccount.FeedbackKindUnauthorized, 1, FeedbackOptions{NowS: intPtr(now)})
	if table.StatusByIdx[idx] != StatusExpired || table.LastFailAtByIdx[idx] != now {
		t.Fatalf("unauthorized status=%d lastFail=%d", table.StatusByIdx[idx], table.LastFailAtByIdx[idx])
	}

	for _, kind := range []controlaccount.FeedbackKind{
		controlaccount.FeedbackKindForbidden,
		controlaccount.FeedbackKindServerError,
	} {
		table, idx = feedbackTable()
		directory = accountDirectoryWithTable(table)
		directory.Feedback("tok", kind, 1, FeedbackOptions{NowS: intPtr(now)})
		if table.LastFailAtByIdx[idx] != now {
			t.Fatalf("%s lastFail=%d", kind, table.LastFailAtByIdx[idx])
		}
	}

	for _, kind := range []controlaccount.FeedbackKind{
		controlaccount.FeedbackKindDisable,
		controlaccount.FeedbackKindDelete,
		controlaccount.FeedbackKindRestore,
	} {
		table, idx = feedbackTable()
		directory = accountDirectoryWithTable(table)
		directory.Feedback("tok", kind, 1, FeedbackOptions{NowS: intPtr(now)})
		if table.StatusByIdx[idx] != StatusActive || table.LastFailAtByIdx[idx] != 0 {
			t.Fatalf("%s should be a directory feedback no-op: status=%d lastFail=%d", kind, table.StatusByIdx[idx], table.LastFailAtByIdx[idx])
		}
	}

	mustSetDirectoryStrategy(t, "random")
	oldConfig := directoryConfigSource
	directoryConfigSource = fakeDirectoryConfig{"account.refresh.basic_interval_sec": 17}
	t.Cleanup(func() { directoryConfigSource = oldConfig })
	table, idx = feedbackTable()
	directory = accountDirectoryWithTable(table)
	before := int(appruntime.NowS())
	directory.Feedback("tok", controlaccount.FeedbackKindRateLimited, 1, FeedbackOptions{NowS: intPtr(now)})
	if table.CoolingUntilSByIdx[idx] != 0 || table.QuotaFastByIdx[idx] != 0 || table.ResetFastAtByIdx[idx] < before+17 || table.QuotaConsoleByIdx[idx] != 1 || table.LastFailAtByIdx[idx] != now {
		t.Fatalf("random rate-limit state cooling=%d fast=%d reset=%d console=%d lastFail=%d",
			table.CoolingUntilSByIdx[idx],
			table.QuotaFastByIdx[idx],
			table.ResetFastAtByIdx[idx],
			table.QuotaConsoleByIdx[idx],
			table.LastFailAtByIdx[idx],
		)
	}
	if _, ok := directory.Reserve(0, 1, ReserveOptions{NowS: intPtr(now + 1)}); ok {
		t.Fatalf("rate-limited fast mode was still selectable")
	}
	if lease, ok := directory.Reserve(0, 5, ReserveOptions{NowS: intPtr(now + 1)}); !ok || lease.Token != "tok" {
		t.Fatalf("console mode should remain selectable after fast rate limit: lease=%#v ok=%t", lease, ok)
	}
}

func TestGetAccountDirectorySingletonMatchesPython(t *testing.T) {
	ctx := context.Background()
	oldDirectory := accountDirectorySingleton
	accountDirectorySingleton = nil
	t.Cleanup(func() { accountDirectorySingleton = oldDirectory })

	if _, err := GetAccountDirectory(ctx, nil); err == nil {
		t.Fatalf("first GetAccountDirectory without repository returned nil error")
	}

	repo := &syncFakeRepository{snapshot: controlaccount.RuntimeSnapshot{
		Revision: 4,
		Items: []controlaccount.AccountRecord{
			accountRecord("singleton", "basic", basicQuotaSet(5), nil),
		},
	}}
	first, err := GetAccountDirectory(ctx, repo)
	if err != nil {
		t.Fatalf("GetAccountDirectory returned error: %v", err)
	}
	if first.Size() != 1 || first.Revision() != 4 {
		t.Fatalf("singleton diagnostics = size %d revision %d", first.Size(), first.Revision())
	}
	second, err := GetAccountDirectory(ctx, nil)
	if err != nil {
		t.Fatalf("second GetAccountDirectory returned error: %v", err)
	}
	if first != second {
		t.Fatalf("singleton was not reused")
	}
}

type fakeDirectoryConfig map[string]int

func (f fakeDirectoryConfig) GetInt(key string, defaultValue int) int {
	if value, ok := f[key]; ok {
		return value
	}
	return defaultValue
}

func accountDirectoryWithTable(table *AccountRuntimeTable) *AccountDirectory {
	return &AccountDirectory{table: table}
}

func reserveFailureTable(statusID int, quota int, inflight int, coolingUntilS int) *AccountRuntimeTable {
	table := MakeEmptyTable()
	idx := table.AppendSlot(AccountSlot{
		Token:      "tok",
		PoolID:     0,
		StatusID:   statusID,
		QuotaFast:  quota,
		TotalFast:  10,
		WindowFast: 60,
		Health:     1,
	})
	table.InflightByIdx[idx] = inflight
	table.CoolingUntilSByIdx[idx] = coolingUntilS
	return table
}

func mustSetDirectoryStrategy(t *testing.T, strategy string) {
	t.Helper()
	previous := CurrentStrategy()
	if err := SetStrategy(strategy); err != nil {
		t.Fatalf("SetStrategy(%q) returned error: %v", strategy, err)
	}
	t.Cleanup(func() {
		if err := SetStrategy(previous); err != nil {
			t.Fatalf("restore strategy %q returned error: %v", previous, err)
		}
	})
}

func intPtr(value int) *int {
	return &value
}
