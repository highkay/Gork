package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	"github.com/dslzl/gork/app/platform/config"
	"github.com/dslzl/gork/app/platform/logging"
	"github.com/dslzl/gork/app/platform/observability"
	"github.com/dslzl/gork/app/products/anthropic"
	"github.com/dslzl/gork/app/products/openai"
	"github.com/dslzl/gork/app/products/web"
)

var (
	appStartedAt = time.Now()

	appMainEnsureConfig = func(ctx context.Context) error {
		return config.GlobalConfig.EnsureLoaded(ctx, "")
	}
	appMainLoadRequestConfig = func(ctx context.Context) error {
		return config.GlobalConfig.Load(ctx, "")
	}
	appMainReconcileRefreshRuntime = func() string {
		return accountcontrol.ReconcileRefreshRuntime()
	}
	appMainSetupLogging = func() error {
		return logging.SetupLogging(logging.LoggingOptions{})
	}
	appMainObservabilityConfig = func() appMainObservabilitySettings {
		return appMainObservabilitySettings{
			MetricsEnabled: config.GlobalConfig.GetBool("observability.metrics_enabled", false),
			PprofEnabled:   config.GlobalConfig.GetBool("observability.pprof_enabled", false),
		}
	}
)

type Hook func(context.Context) error

type appMainObservabilitySettings struct {
	MetricsEnabled bool
	PprofEnabled   bool
}

type AppOptions struct {
	StaticsRoot     string
	WebRouter       http.Handler
	OpenAIRouter    http.Handler
	AnthropicRouter http.Handler
	StartupHooks    []Hook
	ShutdownHooks   []Hook
}

type App struct {
	handler       http.Handler
	startupHooks  []Hook
	shutdownHooks []Hook
}

func NewApp(options AppOptions) *App {
	if options.StartupHooks == nil && options.ShutdownHooks == nil {
		options.StartupHooks, options.ShutdownHooks = defaultLifecycleHooks()
	} else if options.StartupHooks == nil {
		options.StartupHooks = defaultStartupHooks()
	}
	return &App{
		handler:       observability.Middleware(withAppMiddleware(newAppRouter(normalizeAppOptions(options)))),
		startupHooks:  append([]Hook(nil), options.StartupHooks...),
		shutdownHooks: append([]Hook(nil), options.ShutdownHooks...),
	}
}

func defaultStartupHooks() []Hook {
	startupHooks, _ := defaultLifecycleHooks()
	return startupHooks
}

func (app *App) Handler() http.Handler {
	return app.handler
}

func (app *App) Start(ctx context.Context) error {
	for _, hook := range app.startupHooks {
		if err := hook(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (app *App) Shutdown(ctx context.Context) error {
	for _, hook := range app.shutdownHooks {
		if err := hook(ctx); err != nil {
			return err
		}
	}
	return nil
}

func normalizeAppOptions(options AppOptions) AppOptions {
	if options.StaticsRoot == "" {
		options.StaticsRoot = filepath.Join("app", "statics")
	}
	if options.WebRouter == nil {
		options.WebRouter = web.NewRouter()
	}
	if options.OpenAIRouter == nil {
		options.OpenAIRouter = openai.NewRouter()
	}
	if options.AnthropicRouter == nil {
		options.AnthropicRouter = anthropic.NewRouter()
	}
	return options
}

func newAppRouter(options AppOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/metrics":
			if !appMainObservabilityConfig().MetricsEnabled {
				writeAppJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
				return
			}
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			_, _ = w.Write([]byte(observability.MetricsText()))
		case strings.HasPrefix(r.URL.Path, "/debug/pprof"):
			if !appMainObservabilityConfig().PprofEnabled {
				writeAppJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
				return
			}
			serveAppPprof(w, r)
		case r.URL.Path == "/health":
			writeAppJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		case r.URL.Path == "/favicon.ico":
			serveAppFavicon(w, r, options.StaticsRoot)
		case strings.HasPrefix(r.URL.Path, "/static/"):
			http.StripPrefix("/static/", http.FileServer(http.Dir(options.StaticsRoot))).ServeHTTP(w, r)
		case r.URL.Path == "/v1/messages":
			options.AnthropicRouter.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/v1/"):
			options.OpenAIRouter.ServeHTTP(w, r)
		default:
			options.WebRouter.ServeHTTP(w, r)
		}
	})
}

func appRuntimeSummary(selectionStrategy string) map[string]any {
	return map[string]any{
		"selection_strategy": selectionStrategy,
		"uptime_ms":          time.Since(appStartedAt).Milliseconds(),
		"goroutines":         runtime.NumGoroutine(),
	}
}

func serveAppPprof(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/debug/pprof/", "/debug/pprof":
		pprof.Index(w, r)
	case "/debug/pprof/cmdline":
		pprof.Cmdline(w, r)
	case "/debug/pprof/profile":
		pprof.Profile(w, r)
	case "/debug/pprof/symbol":
		pprof.Symbol(w, r)
	case "/debug/pprof/trace":
		pprof.Trace(w, r)
	default:
		name := strings.TrimPrefix(r.URL.Path, "/debug/pprof/")
		pprof.Handler(name).ServeHTTP(w, r)
	}
}

func serveAppFavicon(w http.ResponseWriter, r *http.Request, staticsRoot string) {
	path := filepath.Join(staticsRoot, "favicon.ico")
	if _, err := os.Stat(path); err != nil {
		writeAppJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	http.ServeFile(w, r, path)
}

func withAppMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAppCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		defer recoverAppPanic(w)
		if err := appMainLoadRequestConfig(r.Context()); err != nil {
			writeAppJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]any{"message": "Internal server error", "type": "server_error"},
			})
			return
		}
		appMainReconcileRefreshRuntime()
		next.ServeHTTP(w, r)
	})
}

func writeAppCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Methods", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
}

func recoverAppPanic(w http.ResponseWriter) {
	if recovered := recover(); recovered != nil {
		writeAppJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]any{"message": "Internal server error", "type": "server_error"},
		})
	}
}

func writeAppJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
