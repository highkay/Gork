package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterClearsExpiredFailureWindow(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := New(2, time.Second)
	limiter.now = func() time.Time { return now }

	limiter.Fail("client")
	if !limiter.Allow("client") {
		t.Fatal("one failure should not block")
	}
	limiter.Fail("client")
	if limiter.Allow("client") {
		t.Fatal("threshold failures should block")
	}

	now = now.Add(time.Second + time.Millisecond)
	if !limiter.Allow("client") {
		t.Fatal("expired cooldown should allow")
	}
	limiter.Fail("client")
	if !limiter.Allow("client") {
		t.Fatal("expired cooldown should reset failure count")
	}
}
