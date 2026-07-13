package reverse

import (
	"context"
	"testing"
	"time"

	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
)

func TestRunProtocolCheckFailsClosedWithoutChecker(t *testing.T) {
	results := RunProtocolCheck(context.Background(), []string{"chat"}, nil)
	if len(results) != 1 {
		t.Fatalf("results=%#v", results)
	}
	if results[0].Status != "error" || results[0].ErrorType != "operational_check_unavailable" {
		t.Fatalf("result=%#v", results[0])
	}
	if results[0].RequestID == "" || results[0].CheckedAt == "" {
		t.Fatalf("metadata missing: %#v", results[0])
	}
}

func TestRunProtocolCheckSkipsBlankTargets(t *testing.T) {
	results := RunProtocolCheck(context.Background(), []string{" ", ""}, nil)
	if len(results) != 0 {
		t.Fatalf("results=%#v", results)
	}
}

func TestRunProtocolCheckPassesContextAndTargetsToChecker(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "marker")
	checker := &recordingProtocolChecker{}

	results := RunProtocolCheck(ctx, []string{" chat ", "image"}, checker)

	if len(results) != 2 {
		t.Fatalf("results=%#v", results)
	}
	if checker.ctx != ctx {
		t.Fatalf("checker context = %#v, want original context", checker.ctx)
	}
	if got := checker.targets; len(got) != 2 || got[0] != "chat" || got[1] != "image" {
		t.Fatalf("checker targets = %#v", got)
	}
}

func TestEndpointProtocolCheckerOnlyValidatesEndpointConfig(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	checker := EndpointProtocolChecker{
		Endpoints: reverseruntime.DefaultEndpointTable(),
		Now:       func() time.Time { return now },
	}

	ok := checker.CheckProtocolTarget(context.Background(), "chat")
	if ok.Status != "ok" || ok.ErrorType != "" {
		t.Fatalf("chat endpoint result = %#v", ok)
	}
	if ok.RequestID == "" || ok.CheckedAt != now.Format(time.RFC3339) {
		t.Fatalf("chat metadata = %#v", ok)
	}

	missing := checker.CheckProtocolTarget(context.Background(), "unknown")
	if missing.Status != "error" || missing.ErrorType != "missing_endpoint" {
		t.Fatalf("missing endpoint result = %#v", missing)
	}
}

type recordingProtocolChecker struct {
	ctx     context.Context
	targets []string
}

func (c *recordingProtocolChecker) CheckProtocolTarget(ctx context.Context, target string) ProtocolCheckResult {
	c.ctx = ctx
	c.targets = append(c.targets, target)
	return ProtocolCheckResult{Target: target, Status: "ok"}
}
