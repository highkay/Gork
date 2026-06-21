package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	controlaccount "github.com/dslzl/gork/app/control/account"
	proxycontrol "github.com/dslzl/gork/app/control/proxy"
	accountdataplane "github.com/dslzl/gork/app/dataplane/account"
)

const (
	defaultRefreshLimit = 5
	maxRefreshLimit     = 20
)

type Directory interface {
	Revision() int
	SyncIfChanged(context.Context) (bool, error)
}

type RefreshService interface {
	RefreshScheduledLimit(context.Context, *string, int) (controlaccount.RefreshResult, error)
	ResetExpiredConsoleWindows(context.Context) (int, error)
}

type Dependencies struct {
	Secret         func() string
	Directory      func() Directory
	RefreshService func() RefreshService
	ProxyRefresh   func(context.Context) error
}

type taskResult struct {
	OK     bool           `json:"ok"`
	Error  string         `json:"error,omitempty"`
	Values map[string]any `json:"values,omitempty"`
}

func NewRouter(options ...Dependencies) http.Handler {
	deps := normalizeDependencies(options...)
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/maintenance/account-sync", protected(deps, false, handleAccountSync(deps)))
	mux.HandleFunc("/internal/maintenance/account-refresh", protected(deps, false, handleAccountRefresh(deps)))
	mux.HandleFunc("/internal/maintenance/console-quota-reset", protected(deps, false, handleConsoleQuotaReset(deps)))
	mux.HandleFunc("/internal/maintenance/proxy-refresh", protected(deps, false, handleProxyRefresh(deps)))
	mux.HandleFunc("/internal/cron/daily-maintenance", protected(deps, true, handleDailyMaintenance(deps)))
	return mux
}

func normalizeDependencies(options ...Dependencies) Dependencies {
	deps := Dependencies{}
	if len(options) > 0 {
		deps = options[0]
	}
	if deps.Secret == nil {
		deps.Secret = func() string { return os.Getenv("CRON_SECRET") }
	}
	if deps.Directory == nil {
		deps.Directory = func() Directory { return accountdataplane.CurrentAccountDirectory() }
	}
	if deps.RefreshService == nil {
		deps.RefreshService = func() RefreshService { return controlaccount.GetMaintenanceRefreshService() }
	}
	if deps.ProxyRefresh == nil {
		deps.ProxyRefresh = defaultProxyRefresh
	}
	return deps
}

func protected(deps Dependencies, allowGet bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowGet && r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
			return
		}
		if allowGet && r.Method != http.MethodGet && r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
			return
		}
		if !authorized(r, deps.Secret()) {
			status := http.StatusUnauthorized
			errCode := "unauthorized"
			if strings.TrimSpace(deps.Secret()) == "" {
				status = http.StatusServiceUnavailable
				errCode = "cron_secret_not_configured"
			}
			writeJSON(w, status, map[string]any{"ok": false, "error": errCode})
			return
		}
		next(w, r)
	}
}

func authorized(r *http.Request, secret string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return false
	}
	return r.Header.Get("Authorization") == "Bearer "+secret
}

func handleAccountSync(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result := runAccountSync(r.Context(), deps)
		writeTask(w, result)
	}
}

func handleAccountRefresh(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result := runAccountRefresh(r.Context(), deps, refreshLimit(r))
		writeTask(w, result)
	}
}

func handleConsoleQuotaReset(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result := runConsoleQuotaReset(r.Context(), deps)
		writeTask(w, result)
	}
}

func handleProxyRefresh(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result := runProxyRefresh(r.Context(), deps)
		writeTask(w, result)
	}
}

func handleDailyMaintenance(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := refreshLimit(r)
		tasks := map[string]taskResult{
			"account_sync":        runAccountSync(r.Context(), deps),
			"console_quota_reset": runConsoleQuotaReset(r.Context(), deps),
			"account_refresh":     runAccountRefresh(r.Context(), deps, limit),
			"proxy_refresh":       runProxyRefresh(r.Context(), deps),
		}
		ok := true
		for _, result := range tasks {
			if !result.OK {
				ok = false
				break
			}
		}
		status := http.StatusOK
		if !ok {
			status = http.StatusMultiStatus
		}
		writeJSON(w, status, map[string]any{"ok": ok, "tasks": tasks})
	}
}

func runAccountSync(ctx context.Context, deps Dependencies) taskResult {
	directory := deps.Directory()
	if directory == nil {
		return taskError(errors.New("account directory is not available"))
	}
	changed, err := directory.SyncIfChanged(ctx)
	if err != nil {
		return taskError(err)
	}
	return taskOK(map[string]any{"changed": changed, "revision": directory.Revision()})
}

func runAccountRefresh(ctx context.Context, deps Dependencies, limit int) taskResult {
	service := deps.RefreshService()
	if service == nil {
		return taskError(errors.New("refresh service is not available"))
	}
	result, err := service.RefreshScheduledLimit(ctx, nil, limit)
	if err != nil {
		return taskError(err)
	}
	return taskOK(refreshResultMap(result))
}

func runConsoleQuotaReset(ctx context.Context, deps Dependencies) taskResult {
	service := deps.RefreshService()
	if service == nil {
		return taskError(errors.New("refresh service is not available"))
	}
	reset, err := service.ResetExpiredConsoleWindows(ctx)
	if err != nil {
		return taskError(err)
	}
	return taskOK(map[string]any{"reset": reset})
}

func runProxyRefresh(ctx context.Context, deps Dependencies) taskResult {
	if err := deps.ProxyRefresh(ctx); err != nil {
		return taskError(err)
	}
	return taskOK(nil)
}

func defaultProxyRefresh(ctx context.Context) error {
	directory, err := proxycontrol.GetProxyDirectory(ctx)
	if err != nil {
		return err
	}
	return directory.RefreshClearanceSafe(ctx)
}

func refreshLimit(r *http.Request) int {
	limit := defaultRefreshLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > maxRefreshLimit {
		return maxRefreshLimit
	}
	return limit
}

func taskOK(values map[string]any) taskResult {
	return taskResult{OK: true, Values: values}
}

func taskError(err error) taskResult {
	return taskResult{OK: false, Error: err.Error()}
}

func writeTask(w http.ResponseWriter, result taskResult) {
	status := http.StatusOK
	if !result.OK {
		status = http.StatusServiceUnavailable
	}
	payload := map[string]any{"ok": result.OK}
	if result.Error != "" {
		payload["error"] = result.Error
	}
	for key, value := range result.Values {
		payload[key] = value
	}
	writeJSON(w, status, payload)
}

func refreshResultMap(result controlaccount.RefreshResult) map[string]any {
	return map[string]any{
		"checked":      result.Checked,
		"refreshed":    result.Refreshed,
		"recovered":    result.Recovered,
		"expired":      result.Expired,
		"disabled":     result.Disabled,
		"rate_limited": result.RateLimited,
		"failed":       result.Failed,
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
