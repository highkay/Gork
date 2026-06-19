package proxy

import "math"

// backoffConfig holds the resolved circuit-breaker parameters for clearance
// refresh. Mirrors cloudriver8's ProxyDirectory._backoff_config().
type backoffConfig struct {
	baseSec     int
	maxFails    int
	multiplier  float64
	maxSec      int
	halfOpenSec int
}

// BackoffConfigReader exposes the typed config getters needed for backoff.
// ProxyDirectory's DirectoryConfig already satisfies GetInt; GetFloat is
// declared here so callers must provide a float-capable config.
type BackoffConfigReader interface {
	GetInt(key string, defaultValue int) int
	GetFloat(key string, defaultValue float64) float64
}

func (d *ProxyDirectory) backoffConfig() backoffConfig {
	cfg, ok := d.config.(BackoffConfigReader)
	if !ok {
		// Fallback to defaults when the injected config lacks GetFloat
		// (e.g. emptyDirectoryConfig in unit tests).
		return backoffConfig{baseSec: 300, maxFails: 5, multiplier: 2.0, maxSec: 3600, halfOpenSec: 30}
	}
	baseSec := maxInt(1, cfg.GetInt("proxy.clearance.failure_cooldown_sec", 300))
	maxFails := maxInt(1, cfg.GetInt("proxy.clearance.max_consecutive_failures", 5))
	multiplier := math.Max(1.0, cfg.GetFloat("proxy.clearance.backoff_multiplier", 2.0))
	maxSec := maxInt(baseSec, cfg.GetInt("proxy.clearance.max_cooldown_sec", 3600))
	halfOpenSec := maxInt(1, cfg.GetInt("proxy.clearance.half_open_probe_sec", 30))
	return backoffConfig{baseSec: baseSec, maxFails: maxFails, multiplier: multiplier, maxSec: maxSec, halfOpenSec: halfOpenSec}
}

// cooldownBundleLocked returns a still-valid fallback bundle while the key is in
// cooldown, demoting an INVALID bundle to STALE. Caller must hold d.mu.
// Mirrors _get_cooldown_bundle_locked().
func (d *ProxyDirectory) cooldownBundleLocked(key BundleKey, bundle *ClearanceBundle) *ClearanceBundle {
	until, ok := d.refreshBackoffUntil[key]
	if !ok || until == 0 {
		return nil
	}
	now := d.clock()
	if until <= now {
		delete(d.refreshBackoffUntil, key)
		return nil
	}
	if bundle == nil || bundle.CFCookies == "" || bundle.UserAgent == "" {
		return nil
	}
	if bundle.State == ClearanceBundleInvalid {
		demoted := *bundle
		demoted.State = ClearanceBundleStale
		d.bundles[key] = demoted
		return &demoted
	}
	return bundle
}

// recordRefreshFailureLocked applies exponential backoff (or half-open probe
// once max failures is reached) and returns a fallback bundle if one survives.
// Caller must hold d.mu. Mirrors _record_refresh_failure_locked().
func (d *ProxyDirectory) recordRefreshFailureLocked(key BundleKey) *ClearanceBundle {
	cfg := d.backoffConfig()
	n := d.failureCounts[key] + 1
	d.failureCounts[key] = n

	var cooldownSec float64
	if n >= cfg.maxFails {
		cooldownSec = float64(cfg.halfOpenSec)
	} else {
		cooldownSec = math.Min(float64(cfg.maxSec), float64(cfg.baseSec)*math.Pow(cfg.multiplier, float64(n-1)))
	}
	cooldownMS := int64(cooldownSec * 1000)
	if cooldownMS > 0 {
		d.refreshBackoffUntil[key] = d.clock() + cooldownMS
	}
	var bundle *ClearanceBundle
	if b, ok := d.bundles[key]; ok {
		bundle = &b
	}
	return d.cooldownBundleLocked(key, bundle)
}

// recordRefreshSuccessLocked clears failure state for a key. Caller holds d.mu.
func (d *ProxyDirectory) recordRefreshSuccessLocked(key BundleKey) {
	delete(d.failureCounts, key)
	delete(d.refreshBackoffUntil, key)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RefreshBackoffUntil returns a snapshot of per-key cooldown expiry timestamps
// (ms since epoch). Keys present are currently or recently in cooldown.
func (d *ProxyDirectory) RefreshBackoffUntil() map[BundleKey]int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[BundleKey]int64, len(d.refreshBackoffUntil))
	for key, until := range d.refreshBackoffUntil {
		out[key] = until
	}
	return out
}

// FailureCounts returns a snapshot of per-key consecutive failure counts.
func (d *ProxyDirectory) FailureCounts() map[BundleKey]int {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[BundleKey]int, len(d.failureCounts))
	for key, count := range d.failureCounts {
		out[key] = count
	}
	return out
}

func filterBackoffByAffinity(current map[BundleKey]int64, valid map[string]bool) map[BundleKey]int64 {
	next := map[BundleKey]int64{}
	for key, until := range current {
		if valid[key.Affinity] {
			next[key] = until
		}
	}
	return next
}

func filterFailureCounts(current map[BundleKey]int, valid map[string]bool) map[BundleKey]int {
	next := map[BundleKey]int{}
	for key, count := range current {
		if valid[key.Affinity] {
			next[key] = count
		}
	}
	return next
}
