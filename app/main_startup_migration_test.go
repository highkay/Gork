package app

import (
	"testing"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	platformstartup "github.com/dslzl/gork/app/platform/startup"
)

func TestAppMainStartupRecordsPreserveRuntimeFields(t *testing.T) {
	lastUse := int64(100)
	lastFail := int64(200)
	lastSync := int64(300)
	lastClear := int64(400)
	deleted := int64(500)
	reason := "cooldown"
	source := []accountcontrol.AccountRecord{{
		Token:          "tok",
		Pool:           "pool",
		Status:         accountcontrol.AccountStatusActive,
		Tags:           []string{"tag"},
		Ext:            map[string]any{"k": "v"},
		UsageUseCount:  1,
		UsageFailCount: 2,
		UsageSyncCount: 3,
		LastUseAt:      &lastUse,
		LastFailAt:     &lastFail,
		LastFailReason: &reason,
		LastSyncAt:     &lastSync,
		LastClearAt:    &lastClear,
		StateReason:    &reason,
		DeletedAt:      &deleted,
	}}
	records := appMainStartupRecords(source)

	if len(records) != 1 {
		t.Fatalf("records = %#v", records)
	}
	got := records[0]
	if got.Status != "active" ||
		got.LastUseAt != lastUse ||
		got.LastFailAt != lastFail ||
		got.LastFailReason != reason ||
		got.LastSyncAt != lastSync ||
		got.LastClearAt != lastClear ||
		got.StateReason != reason ||
		got.DeletedAt != deleted ||
		got.UsageUseCount != 1 ||
		got.UsageFailCount != 2 ||
		got.UsageSyncCount != 3 {
		t.Fatalf("startup record lost runtime fields: %#v", got)
	}
	got.Tags[0] = "changed"
	got.Ext["k"] = "changed"
	if source[0].Tags[0] != "tag" || source[0].Ext["k"] != "v" {
		t.Fatalf("conversion leaked mutable state to source: %#v", source[0])
	}
}

func TestAppMainControlPatchesPreserveStartupFields(t *testing.T) {
	useDelta := 7
	status := "disabled"
	source := []platformstartup.AccountPatch{{
		Token:          "tok",
		Status:         status,
		QuotaAuto:      map[string]any{"remaining": 1},
		UsageUseDelta:  &useDelta,
		LastUseAt:      "123",
		LastFailReason: "bad",
		ExtMerge:       map[string]any{"x": "y"},
	}}
	patches := appMainControlPatches(source)

	if len(patches) != 1 {
		t.Fatalf("patches = %#v", patches)
	}
	got := patches[0]
	if got.Status == nil || *got.Status != accountcontrol.AccountStatusDisabled {
		t.Fatalf("status = %#v", got.Status)
	}
	if got.UsageUseDelta == nil || *got.UsageUseDelta != useDelta {
		t.Fatalf("usage delta = %#v", got.UsageUseDelta)
	}
	if got.LastUseAt == nil || *got.LastUseAt != 123 {
		t.Fatalf("last use = %#v", got.LastUseAt)
	}
	if got.LastFailReason == nil || *got.LastFailReason != "bad" {
		t.Fatalf("last fail reason = %#v", got.LastFailReason)
	}
	got.ExtMerge["x"] = "changed"
	if source[0].ExtMerge["x"] != "y" {
		t.Fatalf("conversion leaked mutable patch state to source: %#v", source[0])
	}
}
