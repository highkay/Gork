package build

import (
	"net/http"
	"testing"
	"time"
)

func TestIsPaymentRequiredAndSpendingBody(t *testing.T) {
	err := &UpstreamError{Status: http.StatusPaymentRequired, Body: `{"code":"personal-team-blocked:spending-limit"}`}
	if !IsPaymentRequired(err) || !IsAccountScopedFailure(err) {
		t.Fatal("payment required not detected")
	}
	if !IsSpendingLimitBody(err.Body) {
		t.Fatal("spending body")
	}
}

func TestPaymentCoolingUntilUsesPeriodEnd(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	until := PaymentCoolingUntil(Billing{BillingPeriodEnd: "2026-08-01T00:00:00Z"}, now)
	if !until.Equal(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("until=%v", until)
	}
	fallback := PaymentCoolingUntil(Billing{}, now)
	if fallback.Sub(now) != PaymentRequiredRecoveryPause {
		t.Fatalf("fallback=%v", fallback.Sub(now))
	}
}
