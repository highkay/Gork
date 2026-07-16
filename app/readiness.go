package app

import (
	"context"
	"fmt"
	"time"

	accountdataplane "github.com/dslzl/gork/app/dataplane/account"
)

// readinessComponent is one publicly-visible readiness signal.
type readinessComponent struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// readinessSnapshot is the stable /readyz response contract.
// Shape is intentionally compatible with chenyme-style readiness endpoints:
// ready/state/updatedAt/components — without exposing internal error dumps beyond detail.
type readinessSnapshot struct {
	Ready      bool                          `json:"ready"`
	State      string                        `json:"state"`
	UpdatedAt  time.Time                     `json:"updatedAt"`
	Components map[string]readinessComponent `json:"components,omitempty"`
}

// readinessAccountProbe is the account-pool surface used by readiness checks.
type readinessAccountProbe struct {
	Total     int
	Available int
}

var (
	appMainReadinessNow = func() time.Time {
		return time.Now().UTC()
	}
	// Config is re-checked explicitly so /readyz can report a structured component
	// even though request middleware already loads config for most routes.
	appMainReadinessConfig = func(ctx context.Context) error {
		return appMainLoadRequestConfig(ctx)
	}
	appMainReadinessAccounts = func(ctx context.Context) (readinessAccountProbe, error) {
		directory, err := accountdataplane.GetAccountDirectory(ctx, nil)
		if err != nil {
			return readinessAccountProbe{}, err
		}
		status := directory.SelectionStatus(int(appMainReadinessNow().Unix()))
		return readinessAccountProbe{
			Total:     status.Total,
			Available: status.Available,
		}, nil
	}
)

func evaluateAppReadiness(ctx context.Context) readinessSnapshot {
	now := appMainReadinessNow()
	components := map[string]readinessComponent{
		"process": {State: "ok"},
	}
	ready := true

	if err := appMainReadinessConfig(ctx); err != nil {
		components["config"] = readinessComponent{State: "error", Detail: err.Error()}
		ready = false
	} else {
		components["config"] = readinessComponent{State: "ok"}
	}

	probe, err := appMainReadinessAccounts(ctx)
	switch {
	case err != nil:
		components["accounts"] = readinessComponent{State: "unavailable", Detail: err.Error()}
		ready = false
	case probe.Total <= 0:
		components["accounts"] = readinessComponent{State: "empty", Detail: "account pool is empty"}
		ready = false
	default:
		components["accounts"] = readinessComponent{
			State:  "ok",
			Detail: fmt.Sprintf("total=%d available=%d", probe.Total, probe.Available),
		}
	}

	state := "ready"
	if !ready {
		state = "not_ready"
	}
	return readinessSnapshot{
		Ready:      ready,
		State:      state,
		UpdatedAt:  now,
		Components: components,
	}
}
