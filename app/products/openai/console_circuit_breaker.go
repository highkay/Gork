package openai

import (
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dslzl/gork/app/platform"
)

// consoleTeamCircuitBreaker tracks 429 state for logging/monitoring only.
// Since the rate limit is team-level (60 req/min shared), we do NOT block requests.
// Instead, we retry with random delays to compete for the shared quota.
type consoleTeamCircuitBreaker struct {
	mu          sync.Mutex
	last429Time time.Time
	cooldownSec int
}

func newConsoleTeamCircuitBreaker(cooldownSec int) *consoleTeamCircuitBreaker {
	if cooldownSec <= 0 {
		cooldownSec = 60
	}
	return &consoleTeamCircuitBreaker{
		cooldownSec: cooldownSec,
	}
}

// blocked always returns false - we never block, just log and retry.
func (cb *consoleTeamCircuitBreaker) blocked() bool {
	return false
}

// remainingCooldown always returns 0 - we never block.
func (cb *consoleTeamCircuitBreaker) remainingCooldown() time.Duration {
	return 0
}

// trip records the 429 event for logging, but does NOT block future requests.
func (cb *consoleTeamCircuitBreaker) trip() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.last429Time = time.Now()
	slog.Warn("console circuit breaker: 429 detected, will retry with random delay")
}

// tripFromRetryAfter records 429 with retry-after info for logging.
func (cb *consoleTeamCircuitBreaker) tripFromRetryAfter(retryAfterSec int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.last429Time = time.Now()
	slog.Warn("console circuit breaker: 429 detected", "retry_after_sec", retryAfterSec)
}

// isConsoleRateLimitError checks if the error is a 429 rate limit error.
func isConsoleRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "429") || strings.Contains(msg, "rate_limit") || strings.Contains(msg, "resource-exhausted")
}

// extract429Body extracts the response body from an UpstreamError for 429 parsing.
func extract429Body(err error) string {
	if upstreamErr, ok := err.(*platform.UpstreamError); ok {
		return upstreamErr.Body
	}
	return err.Error()
}

// Console429Info holds parsed information from a 429 response body.
type Console429Info struct {
	PerSecondActual int
	PerSecondLimit  int
	PerMinuteActual int
	PerMinuteLimit  int
	IsPerSecondHit  bool // per-second actual >= limit
	IsPerMinuteHit  bool // per-minute actual >= limit
}

// Regex patterns for parsing 429 response body
var (
	perSecondPattern = regexp.MustCompile(`Requests per Second \(actual/limit\): (\d+)/(\d+)`)
	perMinutePattern = regexp.MustCompile(`Requests per Minute \(actual/limit\): (\d+)/(\d+)`)
)

// parseConsole429Info parses the 429 response body to determine which rate limit was hit.
func parseConsole429Info(body string) Console429Info {
	var info Console429Info

	// Parse per-second
	if matches := perSecondPattern.FindStringSubmatch(body); len(matches) == 3 {
		info.PerSecondActual, _ = strconv.Atoi(matches[1])
		info.PerSecondLimit, _ = strconv.Atoi(matches[2])
		if info.PerSecondLimit > 0 {
			info.IsPerSecondHit = info.PerSecondActual >= info.PerSecondLimit
		}
	}

	// Parse per-minute
	if matches := perMinutePattern.FindStringSubmatch(body); len(matches) == 3 {
		info.PerMinuteActual, _ = strconv.Atoi(matches[1])
		info.PerMinuteLimit, _ = strconv.Atoi(matches[2])
		if info.PerMinuteLimit > 0 {
			info.IsPerMinuteHit = info.PerMinuteActual >= info.PerMinuteLimit
		}
	}

	return info
}

// Global console circuit breaker - tracking only, no blocking
var consoleCircuitBreaker = newConsoleTeamCircuitBreaker(60)
