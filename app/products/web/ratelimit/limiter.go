package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	mu        sync.Mutex
	failures  map[string]failure
	threshold int
	cooldown  time.Duration
	now       func() time.Time
}

type failure struct {
	count        int
	blockedUntil time.Time
}

func New(threshold int, cooldown time.Duration) *Limiter {
	if threshold <= 0 {
		threshold = 5
	}
	if cooldown <= 0 {
		cooldown = time.Minute
	}
	return &Limiter{
		failures:  map[string]failure{},
		threshold: threshold,
		cooldown:  cooldown,
		now:       time.Now,
	}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	item := l.failures[key]
	if item.blockedUntil.IsZero() || !l.now().Before(item.blockedUntil) {
		if !item.blockedUntil.IsZero() {
			delete(l.failures, key)
		}
		return true
	}
	return false
}

func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	item := l.failures[key]
	item.count++
	if item.count >= l.threshold {
		item.blockedUntil = l.now().Add(l.cooldown)
	}
	l.failures[key] = item
}

func (l *Limiter) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}
