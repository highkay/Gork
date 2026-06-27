package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	proxycontrol "github.com/dslzl/gork/app/control/proxy"
	accountdataplane "github.com/dslzl/gork/app/dataplane/account"
	proxydataplane "github.com/dslzl/gork/app/dataplane/proxy"
	reverse "github.com/dslzl/gork/app/dataplane/reverse"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/auth"
	"github.com/dslzl/gork/app/platform/config"
	"github.com/dslzl/gork/app/platform/httpbody"
	"github.com/dslzl/gork/app/platform/logging"
	"github.com/dslzl/gork/app/platform/observability"
	appruntime "github.com/dslzl/gork/app/platform/runtime"
	"github.com/dslzl/gork/app/platform/storage"
	"github.com/dslzl/gork/app/products/openai"
	"github.com/dslzl/gork/app/products/web/ratelimit"
)

type adminConfigStore interface {
	Raw() map[string]any
	Update(context.Context, map[string]any) error
	Reset(context.Context) error
	Load(context.Context, string) error
	GetStr(string, string) string
	GetInt(string, int) int
}

type adminDirectory interface {
	Size() int
	Revision() int
	SyncIfChanged(context.Context) (bool, error)
}

type adminSelectionStatusDirectory interface {
	SelectionStatus(int) accountdataplane.SelectionStatus
}

var (
	adminStartedAt = time.Now()

	adminRouterAuthSettings = func() auth.AuthSettings {
		adminKey := config.GetConfig("app.app_key", nil)
		if adminKey == nil || adminKey == "" {
			adminKey = config.GetConfig("app.admin_key", nil)
		}
		return auth.AuthSettings{AdminKey: adminKey}
	}
	adminAuthRateLimiter   = ratelimit.New(5, time.Minute)
	adminRouterConfig      = adminConfigStore(config.GlobalConfig)
	adminReloadFileLogging = func(level string, maxFiles int) error {
		return logging.ReloadFileLogging(logging.ReloadFileLoggingOptions{FileLevel: level, MaxFiles: maxFiles})
	}
	adminReconcileLocalMediaCache = func(context.Context) error {
		return storage.ReconcileLocalMediaCache()
	}
	adminReconcileRefreshRuntime = func() string {
		strategy := accountcontrol.ReconcileRefreshRuntime()
		_ = accountdataplane.SetStrategy(strategy)
		return strategy
	}
	adminRuntimeStatus = func() map[string]any {
		return map[string]any{
			"version":         platform.GetProjectVersion(),
			"commit":          "",
			"go_version":      goruntime.Version(),
			"uptime_ms":       time.Since(adminStartedAt).Milliseconds(),
			"goroutines":      goruntime.NumGoroutine(),
			"account_storage": fmt.Sprint(config.GetConfig("account.storage", "local")),
			"runtime_store":   adminRuntimeStoreType(),
		}
	}
	adminObservabilityStatus = func() map[string]any {
		status := observability.Snapshot()
		status["metrics_enabled"] = config.GlobalConfig.GetBool("observability.metrics_enabled", false)
		status["pprof_enabled"] = config.GlobalConfig.GetBool("observability.pprof_enabled", false)
		return status
	}
	adminProxyStatus = func(ctx context.Context) map[string]any {
		directory, err := proxycontrol.GetProxyDirectory(ctx, proxydataplane.ProductionDirectoryOptions())
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		return directory.ObservabilityStatus()
	}
	adminDynamicModelStatus = func() any {
		return openai.DynamicModelStatus()
	}
	adminMediaCacheStatus = defaultAdminMediaCacheStatus
	adminAccountDirectory = func() adminDirectory { return nil }
	adminSchedulerStatus  = defaultAdminSchedulerStatus
	adminSelectionStatus  = defaultAdminSelectionStatus
	adminStorageBackend   = func() string {
		return fmt.Sprint(config.GetConfig("account.storage", "local"))
	}
	adminProtocolCheckRunner = func(ctx context.Context, targets []string) []reverse.ProtocolCheckResult {
		return reverse.RunProtocolCheck(ctx, targets, nil)
	}
	adminProtocolCheckMu     sync.Mutex
	adminProtocolCheckLatest []reverse.ProtocolCheckResult
)

type adminRoute struct {
	Path     string
	Methods  []string
	Handlers map[string]http.HandlerFunc
}

func adminRouteOne(method, path string, handler http.HandlerFunc) adminRoute {
	return adminRoute{Path: path, Methods: []string{method}, Handlers: map[string]http.HandlerFunc{method: handler}}
}

func defaultAdminMediaCacheStatus() any {
	status, err := storage.LocalMediaCache.Status()
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return status
}

func adminRouteMany(path string, handlers map[string]http.HandlerFunc) adminRoute {
	methods := make([]string, 0, len(handlers))
	for method := range handlers {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return adminRoute{Path: path, Methods: methods, Handlers: handlers}
}

func adminRoutes() []adminRoute {
	return []adminRoute{
		adminRouteOne(http.MethodGet, "/admin/api/verify", handleAdminVerify),
		adminRouteMany("/admin/api/config", map[string]http.HandlerFunc{
			http.MethodGet: handleAdminGetConfig, http.MethodPost: handleAdminUpdateConfig,
		}),
		adminRouteOne(http.MethodPost, "/admin/api/config/reset", handleAdminResetConfig),
		adminRouteOne(http.MethodGet, "/admin/api/storage", handleAdminStorage),
		adminRouteOne(http.MethodGet, "/admin/api/status", handleAdminStatus),
		adminRouteOne(http.MethodPost, "/admin/api/sync", handleAdminSync),
		adminRouteOne(http.MethodGet, "/admin/api/assets", handleAdminAssetsList),
		adminRouteOne(http.MethodPost, "/admin/api/assets/delete-item", handleAdminAssetDeleteItem),
		adminRouteOne(http.MethodPost, "/admin/api/assets/clear-token", handleAdminAssetClearToken),
		adminRouteOne(http.MethodPost, "/admin/api/batch/nsfw", handleAdminBatchNSFW),
		adminRouteOne(http.MethodPost, "/admin/api/batch/refresh", handleAdminBatchRefresh),
		adminRouteOne(http.MethodPost, "/admin/api/batch/cache-clear", handleAdminBatchCacheClear),
		adminRouteMany("/admin/api/batch/", map[string]http.HandlerFunc{
			http.MethodGet: handleAdminBatchStream, http.MethodPost: handleAdminBatchCancel,
		}),
		adminRouteOne(http.MethodGet, "/admin/api/cache", handleAdminCacheStats),
		adminRouteOne(http.MethodGet, "/admin/api/cache/list", handleAdminCacheList),
		adminRouteOne(http.MethodPost, "/admin/api/cache/clear", handleAdminCacheClear),
		adminRouteOne(http.MethodPost, "/admin/api/cache/item/delete", handleAdminCacheDeleteItem),
		adminRouteOne(http.MethodPost, "/admin/api/cache/items/delete", handleAdminCacheDeleteItems),
		adminRouteOne(http.MethodPost, "/admin/api/protocol-check", handleAdminProtocolCheck),
		adminRouteOne(http.MethodGet, "/admin/api/protocol-check/latest", handleAdminProtocolCheckLatest),
		adminRouteMany("/admin/api/tokens", map[string]http.HandlerFunc{
			http.MethodGet: handleAdminTokensList, http.MethodPost: handleAdminTokensSave, http.MethodDelete: handleAdminTokensDelete,
		}),
		adminRouteOne(http.MethodPost, "/admin/api/tokens/import-async", handleAdminTokensImportAsync),
		adminRouteOne(http.MethodPost, "/admin/api/tokens/add", handleAdminTokensAdd),
		adminRouteOne(http.MethodPut, "/admin/api/tokens/edit", handleAdminTokensEdit),
		adminRouteOne(http.MethodPost, "/admin/api/tokens/disabled", handleAdminTokensToggle),
		adminRouteOne(http.MethodPost, "/admin/api/tokens/disabled/batch", handleAdminTokensToggleBatch),
		adminRouteOne(http.MethodPut, "/admin/api/tokens/pool", handleAdminTokensPool),
	}
}

// NewRouter returns the /admin/api HTTP surface.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	for _, route := range adminRoutes() {
		mux.HandleFunc(route.Path, adminProtectedAny(route.Handlers))
	}
	return mux
}

func adminProtectedAny(handlers map[string]http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler, ok := handlers[r.Method]
		if !ok {
			writeAdminJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": map[string]any{"message": "Method not allowed"}})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			httpbody.Limit(w, r, httpbody.DefaultMultipartLimitBytes)
		}
		queryAppKey := strings.TrimSpace(r.URL.Query().Get("app_key"))
		rateLimitKey := adminAuthRateLimitKey(r)
		if queryAppKey != "" && adminBatchStreamQueryAuth(r) {
			writeAdminError(w, platform.NewAppError("Admin batch streams require Authorization header", platform.ErrorKindAuthentication, "invalid_api_key", http.StatusUnauthorized, nil))
			return
		}
		if !adminAuthRateLimiter.Allow(rateLimitKey) {
			writeAdminJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"message": "Too many authentication attempts"}})
			return
		}
		if err := auth.VerifyAdminKey(r.Header.Get("Authorization"), queryAppKey, adminRouterAuthSettings()); err != nil {
			adminAuthRateLimiter.Fail(rateLimitKey)
			writeAdminError(w, err)
			return
		}
		adminAuthRateLimiter.Success(rateLimitKey)
		if queryAppKey != "" && strings.TrimSpace(r.Header.Get("Authorization")) == "" {
			writeAdminQueryAuthDeprecationHeaders(w)
		}
		handler(w, r)
	}
}

func adminBatchStreamQueryAuth(r *http.Request) bool {
	return r.Method == http.MethodGet &&
		strings.HasPrefix(r.URL.Path, "/admin/api/batch/") &&
		strings.HasSuffix(r.URL.Path, "/stream")
}

func writeAdminQueryAuthDeprecationHeaders(w http.ResponseWriter) {
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Warning", `299 - "app_key query authentication is deprecated; use Authorization: Bearer"`)
}

func adminAuthRateLimitKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func handleAdminVerify(w http.ResponseWriter, _ *http.Request) {
	writeAdminJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func handleAdminProtocolCheck(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Targets []string `json:"targets"`
	}
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		_ = decoder.Decode(&payload)
	}
	results := adminProtocolCheckRunner(r.Context(), payload.Targets)
	adminProtocolCheckMu.Lock()
	adminProtocolCheckLatest = append([]reverse.ProtocolCheckResult(nil), results...)
	adminProtocolCheckMu.Unlock()
	writeAdminJSON(w, http.StatusOK, results)
}

func handleAdminProtocolCheckLatest(w http.ResponseWriter, _ *http.Request) {
	adminProtocolCheckMu.Lock()
	results := append([]reverse.ProtocolCheckResult(nil), adminProtocolCheckLatest...)
	adminProtocolCheckMu.Unlock()
	writeAdminJSON(w, http.StatusOK, results)
}

func handleAdminGetConfig(w http.ResponseWriter, r *http.Request) {
	raw := adminRouterConfig.Raw()
	if adminRevealSensitiveConfirmed(r) {
		w.Header().Set("Cache-Control", "no-store")
		writeAdminJSON(w, http.StatusOK, raw)
		return
	}
	writeAdminJSON(w, http.StatusOK, redactAdminConfig(raw))
}

func adminRevealSensitiveConfirmed(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Admin-Reveal-Sensitive")), "confirm")
}

func handleAdminUpdateConfig(w http.ResponseWriter, r *http.Request) {
	patch, err := decodeAdminPatch(r)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	result, err := updateAdminConfig(r.Context(), patch)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, result)
}

func handleAdminResetConfig(w http.ResponseWriter, r *http.Request) {
	result, err := resetAdminConfig(r.Context())
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, result)
}

func handleAdminStorage(w http.ResponseWriter, _ *http.Request) {
	writeAdminJSON(w, http.StatusOK, map[string]any{"type": adminStorageBackend()})
}

func handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	directory := adminAccountDirectory()
	if directory == nil {
		writeAdminError(w, adminDirectoryError())
		return
	}
	selectionStrategy := adminReconcileRefreshRuntime()
	runtimeSummary := adminRuntimeStatus()
	if runtimeSummary == nil {
		runtimeSummary = map[string]any{}
	}
	runtimeSummary["selection_strategy"] = selectionStrategy
	payload := map[string]any{
		"status":             "ok",
		"size":               directory.Size(),
		"revision":           directory.Revision(),
		"selection_strategy": selectionStrategy,
		"runtime":            runtimeSummary,
	}
	if observability := adminObservabilityStatus(); observability != nil {
		payload["observability"] = observability
	}
	if proxy := adminProxyStatus(r.Context()); proxy != nil {
		payload["proxy"] = proxy
	}
	if dynamic := adminDynamicModelStatus(); dynamic != nil {
		payload["dynamic_models"] = dynamic
	}
	if mediaCache := adminMediaCacheStatus(); mediaCache != nil {
		payload["media_cache"] = mediaCache
	}
	if scheduler := adminSchedulerStatus(); scheduler != nil {
		payload["scheduler"] = scheduler
	}
	if selection := adminSelectionStatus(); selection != nil {
		payload["selection"] = selection
	}
	writeAdminJSON(w, http.StatusOK, payload)
	_ = r
}

func adminRuntimeStoreType() string {
	if strings.TrimSpace(os.Getenv("RUNTIME_REDIS_URL")) != "" {
		return "redis"
	}
	return "local"
}

func defaultAdminSchedulerStatus() map[string]any {
	out := map[string]any{"leader": accountcontrol.IsRefreshSchedulerLeader()}
	if scheduler := accountcontrol.GetRefreshScheduler(); scheduler != nil {
		status := scheduler.Status()
		refresh := map[string]any{"running": status.Running, "pools": map[string]any{}}
		for pool, poolStatus := range status.Pools {
			refresh["pools"].(map[string]any)[pool] = map[string]any{
				"last_error":            poolStatus.LastError,
				"failure_count":         poolStatus.ConsecutiveFailures,
				"last_result":           poolStatus.LastResult,
				"next_run_after_ms":     durationMillis(poolStatus.NextRunAfter),
				"last_started_at_unix":  unixTime(poolStatus.LastStartedAt),
				"last_finished_at_unix": unixTime(poolStatus.LastFinishedAt),
				"next_run_at_unix":      unixTime(poolStatus.NextRunAt),
			}
		}
		out["refresh"] = refresh
	}
	return out
}

func defaultAdminSelectionStatus() map[string]any {
	if directory, ok := adminAccountDirectory().(adminSelectionStatusDirectory); ok {
		status := directory.SelectionStatus(int(appruntime.NowS()))
		return map[string]any{
			"strategy":     status.Strategy,
			"max_inflight": status.MaxInflight,
			"total":        status.Total,
			"selectable":   status.Available,
			"cooling":      status.Cooling,
			"rate_limited": status.Cooling,
			"invalid":      status.InvalidCredentials,
			"disabled":     status.Disabled,
			"inflight":     status.Inflight,
		}
	}
	return map[string]any{
		"strategy":     accountcontrol.CurrentAccountSelectionStrategy(),
		"max_inflight": config.GlobalConfig.GetInt("account.selection.max_inflight", 8),
	}
}

func durationMillis(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	return int64(value / time.Millisecond)
}

func unixTime(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.Unix()
}

func handleAdminSync(w http.ResponseWriter, r *http.Request) {
	directory := adminAccountDirectory()
	if directory == nil {
		writeAdminError(w, adminDirectoryError())
		return
	}
	changed, err := directory.SyncIfChanged(r.Context())
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"changed": changed, "revision": directory.Revision()})
}

func decodeAdminPatch(r *http.Request) (map[string]any, error) {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	var patch map[string]any
	if err := decoder.Decode(&patch); err != nil {
		return nil, platform.NewValidationError("Invalid JSON body", "body", "invalid_json")
	}
	if patch == nil {
		patch = map[string]any{}
	}
	return patch, nil
}

func updateAdminConfig(ctx context.Context, patch map[string]any) (map[string]any, error) {
	patch = sanitizeProxyConfig(patch)
	if err := ensureRuntimePatchAllowed(patch); err != nil {
		return nil, err
	}
	if err := ensureConfigPatchValid(patch); err != nil {
		return nil, err
	}
	cacheLocalChanged := patchTouchesPrefix(patch, "cache.local")
	if err := updateConfigWithSource(ctx, adminRouterConfig, patch, "admin"); err != nil {
		return nil, err
	}
	if err := adminRouterConfig.Load(ctx, ""); err != nil {
		return nil, err
	}
	if err := adminReloadFileLogging(adminRouterConfig.GetStr("logging.file_level", ""), adminRouterConfig.GetInt("logging.max_files", 7)); err != nil {
		return nil, err
	}
	if cacheLocalChanged {
		if err := adminReconcileLocalMediaCache(ctx); err != nil {
			return nil, err
		}
	}
	return map[string]any{"status": "success", "message": "配置已更新", "selection_strategy": adminReconcileRefreshRuntime()}, nil
}

func resetAdminConfig(ctx context.Context) (map[string]any, error) {
	if err := adminRouterConfig.Reset(ctx); err != nil {
		return nil, err
	}
	if err := adminRouterConfig.Load(ctx, ""); err != nil {
		return nil, err
	}
	if err := adminReloadFileLogging(adminRouterConfig.GetStr("logging.file_level", ""), adminRouterConfig.GetInt("logging.max_files", 7)); err != nil {
		return nil, err
	}
	if err := adminReconcileLocalMediaCache(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "message": "配置已还原为默认值", "selection_strategy": adminReconcileRefreshRuntime()}, nil
}

func adminDirectoryError() error {
	return platform.NewAppError("Account directory not initialised", platform.ErrorKindServer, "directory_not_initialised", 503, nil)
}

func redactAdminConfig(value any) any {
	return redactAdminConfigValue("", value)
}

func redactAdminConfigValue(path string, value any) any {
	switch typed := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, item := range typed {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			if adminConfigKeySensitive(childPath) {
				redacted[key] = "<redacted>"
				continue
			}
			redacted[key] = redactAdminConfigValue(childPath, item)
		}
		return redacted
	case []any:
		redacted := make([]any, len(typed))
		for i, item := range typed {
			redacted[i] = redactAdminConfigValue(path, item)
		}
		return redacted
	case string:
		if adminConfigStringSensitive(typed) {
			return "<redacted>"
		}
		return typed
	default:
		return typed
	}
}

func adminConfigKeySensitive(key string) bool {
	return config.SensitiveConfigKey(key)
}

func adminConfigStringSensitive(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.Contains(trimmed, "://") && strings.Contains(trimmed, "@")
}

func writeAdminJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAdminError(w http.ResponseWriter, err error) {
	var validation *platform.ValidationError
	if errors.As(err, &validation) && validation.AppError != nil {
		writeAdminJSON(w, validation.Status, validation.ToDict())
		return
	}
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) && upstream.AppError != nil {
		writeAdminJSON(w, upstream.Status, upstream.ToDict())
		return
	}
	var appErr *platform.AppError
	if errors.As(err, &appErr) && appErr != nil {
		writeAdminJSON(w, appErr.Status, appErr.ToDict())
		return
	}
	writeAdminJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"message": err.Error()}})
}
