package build

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseBillingNestedConfigAndAliases(t *testing.T) {
	raw := []byte(`{
		"config": {
			"plan": {"code":"pro","name":"Pro"},
			"monthlyLimit": 100,
			"used": 25,
			"onDemandCap": 50,
			"onDemandUsed": 10,
			"currentPeriod": {"type":"week","start":"s","end":"e"}
		}
	}`)
	got, err := ParseBilling(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanCode != "pro" || got.PlanName != "Pro" {
		t.Fatalf("plan=%#v", got)
	}
	if got.MonthlyLimit != 100 || got.Used != 25 {
		t.Fatalf("quota=%#v", got)
	}
	if got.CreditUsagePercent != 20 { // 10/50*100
		t.Fatalf("percent=%v", got.CreditUsagePercent)
	}
	if got.UsagePeriodType != "week" {
		t.Fatalf("period=%#v", got)
	}
}

func TestMergeBillingSnapshotsPrefersCreditsPeriod(t *testing.T) {
	monthly := Billing{PlanCode: "m", MonthlyLimit: 100, Used: 10}
	credits := Billing{
		PlanName: "Credits", OnDemandCap: 40, OnDemandUsed: 8,
		CreditUsagePercent: 20, UsagePeriodType: "week",
		UsagePeriodStart: "a", UsagePeriodEnd: "b",
	}
	got := MergeBillingSnapshots(monthly, credits)
	if got.PlanCode != "m" || got.PlanName != "Credits" {
		t.Fatalf("plan=%#v", got)
	}
	if got.OnDemandCap != 40 || got.UsagePeriodType != "week" {
		t.Fatalf("merged=%#v", got)
	}
	if got.MonthlyLimit != 100 {
		t.Fatalf("monthly should keep limit=%v", got.MonthlyLimit)
	}
}

func TestAPIClientGetBillingMergesCredits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.RawQuery == "format=credits" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"onDemandCap": 30, "onDemandUsed": 6, "creditUsagePercent": 20,
				"currentPeriod": map[string]any{"type": "week", "start": "s", "end": "e"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"planCode": "basic", "planName": "Basic", "monthlyLimit": 100, "used": 5,
		})
	}))
	defer srv.Close()

	client := NewAPIClient(srv.Client(), ClientConfig{BaseURL: srv.URL})
	got, err := client.GetBilling(t.Context(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanCode != "basic" || got.OnDemandCap != 30 || got.UsagePeriodType != "week" {
		t.Fatalf("%#v", got)
	}
	if got.SyncedAt.IsZero() {
		t.Fatal("expected SyncedAt")
	}
}
