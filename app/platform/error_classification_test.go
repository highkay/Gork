package platform

import (
	"errors"
	"testing"
)

func TestClassifyErrorMapsKnownApplicationErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrorClass
	}{
		{"validation", NewValidationError("bad", "field", ""), ErrorClassValidation},
		{"auth", NewAuthError("bad key"), ErrorClassAuth},
		{"rate limit", NewRateLimitError("slow down"), ErrorClassRateLimit},
		{"upstream", NewUpstreamError("upstream failed", 502, "body"), ErrorClassUpstream},
		{"storage", NewStorageError("write failed"), ErrorClassStorage},
		{"config", NewConfigError("missing config"), ErrorClassConfig},
		{"transport", NewTransportError("dial failed"), ErrorClassTransport},
		{"internal", errors.New("boom"), ErrorClassInternal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyError(tc.err); got != tc.want {
				t.Fatalf("ClassifyError() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestErrorClassRouteNamesAreStable(t *testing.T) {
	got := ErrorClasses()
	want := []ErrorClass{
		ErrorClassValidation,
		ErrorClassAuth,
		ErrorClassRateLimit,
		ErrorClassUpstream,
		ErrorClassTransport,
		ErrorClassStorage,
		ErrorClassConfig,
		ErrorClassInternal,
	}
	if len(got) != len(want) {
		t.Fatalf("ErrorClasses length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ErrorClasses()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
