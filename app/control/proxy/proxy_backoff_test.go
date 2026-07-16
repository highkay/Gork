package proxy

import "testing"

// fakeBackoffConfig satisfies BackoffConfigReader + DirectoryConfig with
// tunable, fast backoff parameters for deterministic tests.
type fakeBackoffConfig struct {
	ints   map[string]int
	floats map[string]float64
}

func (f fakeBackoffConfig) GetString(_ string, dv string) string { return dv }
func (f fakeBackoffConfig) GetList(_ string, dv []string) []string {
	return append([]string(nil), dv...)
}
func (f fakeBackoffConfig) GetInt(key string, dv int) int {
	if v, ok := f.ints[key]; ok {
		return v
	}
	return dv
}
func (f fakeBackoffConfig) GetFloat(key string, dv float64) float64 {
	if v, ok := f.floats[key]; ok {
		return v
	}
	return dv
}

func newBackoffTestDirectory(now *int64) *ProxyDirectory {
	cfg := fakeBackoffConfig{
		ints: map[string]int{
			"proxy.clearance.failure_cooldown_sec":     10,
			"proxy.clearance.max_consecutive_failures": 3,
			"proxy.clearance.max_cooldown_sec":         100,
			"proxy.clearance.half_open_probe_sec":      5,
		},
		floats: map[string]float64{"proxy.clearance.backoff_multiplier": 2.0},
	}
	d := NewProxyDirectory(DirectoryOptions{Config: cfg, Clock: func() int64 { return *now }})
	return d
}

func TestRecordRefreshFailureExponentialBackoff(t *testing.T) {
	var now int64 = 1_000_000
	d := newBackoffTestDirectory(&now)
	key := BundleKey{Affinity: "direct", ClearanceHost: "grok.com"}

	// 1st failure: 10s. 2nd: 20s (10*2^1). Both below max_fails=3 → exponential.
	d.mu.Lock()
	d.recordRefreshFailureLocked(key)
	d.mu.Unlock()
	if got := d.RefreshBackoffUntil()[key]; got != now+10_000 {
		t.Fatalf("1st failure cooldown until=%d, want %d", got, now+10_000)
	}
	if got := d.FailureCounts()[key]; got != 1 {
		t.Fatalf("failure count = %d, want 1", got)
	}

	d.mu.Lock()
	d.recordRefreshFailureLocked(key)
	d.mu.Unlock()
	if got := d.RefreshBackoffUntil()[key]; got != now+20_000 {
		t.Fatalf("2nd failure cooldown until=%d, want %d", got, now+20_000)
	}
}

func TestRecordRefreshFailureHalfOpenProbe(t *testing.T) {
	var now int64 = 1_000_000
	d := newBackoffTestDirectory(&now)
	key := BundleKey{Affinity: "direct", ClearanceHost: "grok.com"}

	// Drive to max_fails=3 → 3rd failure switches to half_open_probe_sec=5s.
	for i := 0; i < 3; i++ {
		d.mu.Lock()
		d.recordRefreshFailureLocked(key)
		d.mu.Unlock()
	}
	if got := d.FailureCounts()[key]; got != 3 {
		t.Fatalf("failure count = %d, want 3", got)
	}
	if got := d.RefreshBackoffUntil()[key]; got != now+5_000 {
		t.Fatalf("half-open cooldown until=%d, want %d (5s probe)", got, now+5_000)
	}
}

func TestRecordRefreshFailureCappedByMaxCooldown(t *testing.T) {
	var now int64 = 1_000_000
	d := newBackoffTestDirectory(&now)
	d.config = fakeBackoffConfig{
		ints: map[string]int{
			"proxy.clearance.failure_cooldown_sec":     10,
			"proxy.clearance.max_consecutive_failures": 100, // keep exponential path
			"proxy.clearance.max_cooldown_sec":         25,  // cap below 10*2^2=40
		},
		floats: map[string]float64{"proxy.clearance.backoff_multiplier": 2.0},
	}
	key := BundleKey{Affinity: "direct", ClearanceHost: "grok.com"}
	for i := 0; i < 3; i++ { // 3rd would be 40s, capped to 25s
		d.mu.Lock()
		d.recordRefreshFailureLocked(key)
		d.mu.Unlock()
	}
	if got := d.RefreshBackoffUntil()[key]; got != now+25_000 {
		t.Fatalf("capped cooldown until=%d, want %d", got, now+25_000)
	}
}

func TestRecordRefreshSuccessResetsState(t *testing.T) {
	var now int64 = 1_000_000
	d := newBackoffTestDirectory(&now)
	key := BundleKey{Affinity: "direct", ClearanceHost: "grok.com"}

	d.mu.Lock()
	d.recordRefreshFailureLocked(key)
	d.recordRefreshSuccessLocked(key)
	d.mu.Unlock()

	if len(d.FailureCounts()) != 0 {
		t.Fatalf("failure counts not cleared: %v", d.FailureCounts())
	}
	if len(d.RefreshBackoffUntil()) != 0 {
		t.Fatalf("backoff not cleared: %v", d.RefreshBackoffUntil())
	}
}

func TestCooldownBundleDemotesInvalidToStale(t *testing.T) {
	var now int64 = 1_000_000
	d := newBackoffTestDirectory(&now)
	key := BundleKey{Affinity: "direct", ClearanceHost: "grok.com"}

	// Seed an INVALID-but-usable bundle and an active cooldown window.
	d.mu.Lock()
	d.bundles[key] = ClearanceBundle{CFCookies: "cf=1", UserAgent: "UA", State: ClearanceBundleInvalid}
	d.refreshBackoffUntil[key] = now + 5_000
	bundle := d.bundles[key]
	fallback := d.cooldownBundleLocked(key, &bundle)
	d.mu.Unlock()

	if fallback == nil {
		t.Fatal("expected a fallback bundle while in cooldown")
	}
	if fallback.State != ClearanceBundleStale {
		t.Fatalf("fallback state = %v, want Stale", fallback.State)
	}
}

func TestCooldownBundleExpiresAndClears(t *testing.T) {
	var now int64 = 1_000_000
	d := newBackoffTestDirectory(&now)
	key := BundleKey{Affinity: "direct", ClearanceHost: "grok.com"}

	d.mu.Lock()
	d.refreshBackoffUntil[key] = now - 1 // already expired
	bundle := ClearanceBundle{CFCookies: "cf=1", UserAgent: "UA", State: ClearanceBundleValid}
	fallback := d.cooldownBundleLocked(key, &bundle)
	d.mu.Unlock()

	if fallback != nil {
		t.Fatal("expired cooldown should return no fallback")
	}
	if len(d.RefreshBackoffUntil()) != 0 {
		t.Fatal("expired cooldown key should be deleted")
	}
}
