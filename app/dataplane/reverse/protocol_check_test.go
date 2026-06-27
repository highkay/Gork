package reverse

import (
	"context"
	"testing"
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
