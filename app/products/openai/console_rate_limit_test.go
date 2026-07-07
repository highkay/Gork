package openai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dslzl/gork/app/platform"
)

func TestConsoleModelThrottleIntervalDefaultsMultiAgentOnly(t *testing.T) {
	oldConfig := consoleRateLimitConfigFloat
	t.Cleanup(func() { consoleRateLimitConfigFloat = oldConfig })
	consoleRateLimitConfigFloat = func(_ string, fallback float64) float64 { return fallback }

	if got := consoleModelThrottleInterval("grok-4.20-multi-agent-0309"); got != 3*time.Second {
		t.Fatalf("multi-agent interval = %s, want 3s", got)
	}
	if got := consoleModelThrottleInterval("grok-4.3"); got != 0 {
		t.Fatalf("generic console interval = %s, want 0", got)
	}
	if got := consoleModelCooldownInterval("grok-4.20-multi-agent-0309"); got != time.Minute {
		t.Fatalf("multi-agent cooldown = %s, want 1m", got)
	}
	if got := consoleModelMaxQueueWait("grok-4.20-multi-agent-0309"); got != 15*time.Second {
		t.Fatalf("multi-agent max queue wait = %s, want 15s", got)
	}
}

func TestConsoleModelRateLimitPacesResolvedMultiAgentAliases(t *testing.T) {
	oldPacer := consoleModelPacer
	oldConfig := consoleRateLimitConfigFloat
	t.Cleanup(func() {
		consoleModelPacer = oldPacer
		consoleRateLimitConfigFloat = oldConfig
	})

	now := time.Unix(100, 0)
	sleeps := []time.Duration{}
	pacer := newConsoleModelPacer()
	pacer.now = func() time.Time { return now }
	pacer.sleep = func(_ context.Context, delay time.Duration) error {
		sleeps = append(sleeps, delay)
		now = now.Add(delay)
		return nil
	}
	consoleModelPacer = pacer
	consoleRateLimitConfigFloat = func(key string, fallback float64) float64 {
		if key == "chat.console_multi_agent_max_queue_wait_ms" {
			return 0
		}
		return fallback
	}

	if err := waitConsoleModelRateLimit(context.Background(), "grok-4.20-multi-agent-low"); err != nil {
		t.Fatalf("first wait returned error: %v", err)
	}
	if err := waitConsoleModelRateLimit(context.Background(), "grok-4.20-multi-agent-high"); err != nil {
		t.Fatalf("second wait returned error: %v", err)
	}
	if len(sleeps) != 1 || sleeps[0] != 3*time.Second {
		t.Fatalf("sleeps = %#v, want one 3s delay", sleeps)
	}
}

func TestConsoleModelRateLimitCooldownExtendsNextStart(t *testing.T) {
	oldPacer := consoleModelPacer
	oldConfig := consoleRateLimitConfigFloat
	t.Cleanup(func() {
		consoleModelPacer = oldPacer
		consoleRateLimitConfigFloat = oldConfig
	})

	now := time.Unix(200, 0)
	sleeps := []time.Duration{}
	pacer := newConsoleModelPacer()
	pacer.now = func() time.Time { return now }
	pacer.sleep = func(_ context.Context, delay time.Duration) error {
		sleeps = append(sleeps, delay)
		now = now.Add(delay)
		return nil
	}
	consoleModelPacer = pacer
	consoleRateLimitConfigFloat = func(key string, fallback float64) float64 {
		if key == "chat.console_multi_agent_max_queue_wait_ms" {
			return 0
		}
		return fallback
	}

	cooldownConsoleModelRateLimit(context.Background(), "grok-4.20-multi-agent-console", platform.NewUpstreamError("rate", 429, ""))
	if err := waitConsoleModelRateLimit(context.Background(), "grok-4.20-multi-agent-high"); err != nil {
		t.Fatalf("wait returned error: %v", err)
	}
	if len(sleeps) != 1 || sleeps[0] != time.Minute {
		t.Fatalf("sleeps = %#v, want one 1m cooldown", sleeps)
	}
}

func TestConsoleModelRateLimitFailsFastWhenQueueWaitExceedsCap(t *testing.T) {
	oldPacer := consoleModelPacer
	oldConfig := consoleRateLimitConfigFloat
	t.Cleanup(func() {
		consoleModelPacer = oldPacer
		consoleRateLimitConfigFloat = oldConfig
	})

	now := time.Unix(300, 0)
	pacer := newConsoleModelPacer()
	pacer.now = func() time.Time { return now }
	pacer.sleep = func(context.Context, time.Duration) error {
		t.Fatal("queue should fail before sleeping")
		return nil
	}
	consoleModelPacer = pacer
	consoleRateLimitConfigFloat = func(key string, fallback float64) float64 {
		if key == "chat.console_multi_agent_cooldown_ms" {
			return 60000
		}
		if key == "chat.console_multi_agent_max_queue_wait_ms" {
			return 15000
		}
		return fallback
	}

	cooldownConsoleModelRateLimit(context.Background(), "grok-4.20-multi-agent-console", platform.NewUpstreamError("rate", 429, ""))
	err := waitConsoleModelRateLimit(context.Background(), "grok-4.20-multi-agent-high")
	var rateLimit *platform.RateLimitError
	if !errors.As(err, &rateLimit) {
		t.Fatalf("wait err = %T %v, want rate limit", err, err)
	}
}
