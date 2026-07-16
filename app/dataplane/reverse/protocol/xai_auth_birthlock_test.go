package protocol

import (
	"errors"
	"testing"

	platform "github.com/dslzl/gork/app/platform"
)

func TestIsBirthDateLockedError(t *testing.T) {
	locked := platform.NewUpstreamError("set_birth_date failed", 429,
		`{"error":{"code":"birth-date-change-limit-reached"}}`)
	if !isBirthDateLockedError(locked) {
		t.Fatal("locked birth-date 429 should be detected")
	}

	// True rate limit (429 but different body) must NOT be treated as locked.
	rateLimit := platform.NewUpstreamError("rate limited", 429, `{"error":{"code":"too-many-requests"}}`)
	if isBirthDateLockedError(rateLimit) {
		t.Fatal("generic 429 must not be treated as birth-date locked")
	}

	// Same marker but non-429 status must NOT match.
	wrongStatus := platform.NewUpstreamError("forbidden", 403, `birth-date-change-limit-reached`)
	if isBirthDateLockedError(wrongStatus) {
		t.Fatal("non-429 must not be treated as birth-date locked")
	}

	// Non-upstream error must NOT match.
	if isBirthDateLockedError(errors.New("transport boom")) {
		t.Fatal("plain error must not be treated as birth-date locked")
	}
}
