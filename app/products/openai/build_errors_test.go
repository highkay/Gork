package openai

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
	"github.com/dslzl/gork/app/platform"
)

type coolDir struct {
	status string
	until  time.Time
	reason string
}

func (d *coolDir) ListActive(context.Context, time.Time) ([]buildaccount.Account, error) {
	return nil, nil
}
func (d *coolDir) UpdateTokens(context.Context, int64, string, string, time.Time) error { return nil }
func (d *coolDir) SetStatus(_ context.Context, _ int64, status, reason string) error {
	d.status, d.reason = status, reason
	return nil
}
func (d *coolDir) SetStatusUntil(_ context.Context, _ int64, status, reason string, until time.Time) error {
	d.status, d.reason, d.until = status, reason, until
	return nil
}

func TestHandleBuildUpstreamFailurePaymentRequired(t *testing.T) {
	dir := &coolDir{}
	acc := buildaccount.Account{ID: 7, Billing: build.Billing{}}
	err := handleBuildUpstreamFailure(context.Background(), dir, acc, http.StatusPaymentRequired,
		`{"code":"personal-team-blocked:spending-limit","error":"You have run out of credits. Upgrade SuperGrok"}`,
		"create_response")
	var up *platform.UpstreamError
	if !errorsAs(err, &up) || up.Status != http.StatusServiceUnavailable || up.Code != "upstream_unavailable" {
		t.Fatalf("err=%v", err)
	}
	if dir.status != buildaccount.StatusCooling || dir.until.IsZero() {
		t.Fatalf("cooling not set: status=%s until=%v", dir.status, dir.until)
	}
	if up.Message != "No available Build accounts for this request" {
		t.Fatalf("leaked message: %s", up.Message)
	}
}
