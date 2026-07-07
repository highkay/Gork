package openai

import (
	"context"
	"errors"
	"time"

	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	"github.com/dslzl/gork/app/platform"
	platformconfig "github.com/dslzl/gork/app/platform/config"
)

const consoleMultiAgentModel = "grok-4.20-multi-agent-0309"

var errConsoleModelRateLimitQueue = errors.New("console model local rate limit queue is busy")

var (
	consoleModelPacer           = newConsoleModelPacer()
	consoleRateLimitConfigFloat = func(key string, defaultValue float64) float64 {
		return platformconfig.GlobalConfig.GetFloat(key, defaultValue)
	}
)

type consoleModelPacerState struct {
	mu   chan struct{}
	next time.Time
}

type consoleModelPacerQueue struct {
	mu     chan struct{}
	states map[string]*consoleModelPacerState
	now    func() time.Time
	sleep  func(context.Context, time.Duration) error
}

func newConsoleModelPacer() *consoleModelPacerQueue {
	return &consoleModelPacerQueue{
		mu:     newConsoleRateLimitLock(),
		states: map[string]*consoleModelPacerState{},
		now:    time.Now,
		sleep:  consoleRateLimitSleep,
	}
}

func (p *consoleModelPacerQueue) Wait(ctx context.Context, model string, interval, maxWait time.Duration) error {
	if p == nil || interval <= 0 {
		return nil
	}
	state, err := p.state(ctx, model)
	if err != nil {
		return err
	}
	start := p.now()
	if err := acquireConsoleRateLimitLock(ctx, state.mu, maxWait); err != nil {
		return err
	}
	defer releaseConsoleRateLimitLock(state.mu)

	now := p.now()
	if delay := state.next.Sub(now); delay > 0 {
		if maxWait > 0 && delay > maxWait-now.Sub(start) {
			return errConsoleModelRateLimitQueue
		}
		if err := p.sleep(ctx, delay); err != nil {
			return err
		}
		now = p.now()
	}
	state.next = now.Add(interval)
	return nil
}

func (p *consoleModelPacerQueue) Delay(ctx context.Context, model string, delay time.Duration) error {
	if p == nil || delay <= 0 {
		return nil
	}
	state, err := p.state(ctx, model)
	if err != nil {
		return err
	}
	if err := acquireConsoleRateLimitLock(ctx, state.mu, 0); err != nil {
		return err
	}
	defer releaseConsoleRateLimitLock(state.mu)

	next := p.now().Add(delay)
	if next.After(state.next) {
		state.next = next
	}
	return nil
}

func (p *consoleModelPacerQueue) state(ctx context.Context, model string) (*consoleModelPacerState, error) {
	if err := acquireConsoleRateLimitLock(ctx, p.mu, 0); err != nil {
		return nil, err
	}
	defer releaseConsoleRateLimitLock(p.mu)
	if state := p.states[model]; state != nil {
		return state, nil
	}
	state := &consoleModelPacerState{mu: newConsoleRateLimitLock()}
	p.states[model] = state
	return state, nil
}

func waitConsoleModelRateLimit(ctx context.Context, model string) error {
	resolved := protocol.ResolveConsoleModel(model)
	interval := consoleModelThrottleInterval(resolved)
	if interval <= 0 {
		return nil
	}
	maxWait := consoleModelMaxQueueWait(resolved)
	if err := consoleModelPacer.Wait(ctx, resolved, interval, maxWait); err != nil {
		if errors.Is(err, errConsoleModelRateLimitQueue) {
			return platform.NewRateLimitError("Console model local rate limit queue is busy")
		}
		return platform.NewRateLimitError("Console model local rate limit queue canceled")
	}
	return nil
}

func cooldownConsoleModelRateLimit(ctx context.Context, model string, err error) {
	if upstreamStatus(err) != 429 {
		return
	}
	resolved := protocol.ResolveConsoleModel(model)
	interval := consoleModelCooldownInterval(resolved)
	if interval <= 0 {
		return
	}
	_ = consoleModelPacer.Delay(ctx, resolved, interval)
}

func consoleModelThrottleInterval(model string) time.Duration {
	key := "chat.console_min_interval_ms"
	fallback := 0.0
	if model == consoleMultiAgentModel {
		key = "chat.console_multi_agent_min_interval_ms"
		fallback = 3000
	}
	ms := consoleRateLimitConfigFloat(key, fallback)
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms * float64(time.Millisecond))
}

func consoleModelCooldownInterval(model string) time.Duration {
	key := "chat.console_cooldown_ms"
	fallback := 0.0
	if model == consoleMultiAgentModel {
		key = "chat.console_multi_agent_cooldown_ms"
		fallback = 60000
	}
	ms := consoleRateLimitConfigFloat(key, fallback)
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms * float64(time.Millisecond))
}

func consoleModelMaxQueueWait(model string) time.Duration {
	key := "chat.console_max_queue_wait_ms"
	fallback := 15000.0
	if model == consoleMultiAgentModel {
		key = "chat.console_multi_agent_max_queue_wait_ms"
		fallback = 15000
	}
	ms := consoleRateLimitConfigFloat(key, fallback)
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms * float64(time.Millisecond))
}

func newConsoleRateLimitLock() chan struct{} {
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	return ch
}

func acquireConsoleRateLimitLock(ctx context.Context, ch chan struct{}, timeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errConsoleModelRateLimitQueue
		case <-ch:
			return nil
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}

func releaseConsoleRateLimitLock(ch chan struct{}) {
	ch <- struct{}{}
}

func consoleRateLimitSleep(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
