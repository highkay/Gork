package web

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/auth"
	"github.com/dslzl/gork/app/platform/config"
	adminproduct "github.com/dslzl/gork/app/products/web/admin"
	webuiapi "github.com/dslzl/gork/app/products/web/webui"
)

var (
	webRouterAuthSettings = func() auth.AuthSettings {
		return auth.AuthSettings{
			WebUIKey:     config.GetConfig("app.webui_key", ""),
			WebUIEnabled: config.GetConfig("app.webui_enabled", false),
		}
	}
	webRouterProjectVersion = platform.GetProjectVersion
	webRouterLatestRelease  = platform.GetLatestReleaseInfo
	webRouterStaticsRoot    = func() string { return filepath.Join("app", "statics") }
	webRouterServeHTML      = serveWebRouterHTML
)

type webRoute struct {
	Method  string
	Path    string
	Handler http.Handler
}

func webMount(path string, handler http.Handler) webRoute {
	return webRoute{Path: path, Handler: handler}
}

func webGet(path string, handler http.HandlerFunc) webRoute {
	return webRoute{Method: http.MethodGet, Path: path, Handler: handler}
}

func webRoutes() []webRoute {
	return []webRoute{
		webMount("/admin/api/", adminproduct.NewRouter()),
		webMount("/webui/api/", webuiapi.NewRouter()),
		webGet("/", handleWebRoot),
		webGet("/admin", redirectWeb("/admin/login")),
		webGet("/admin/login", serveWebPage("admin/login.html")),
		webGet("/admin/account", serveWebPage("admin/account.html")),
		webGet("/admin/config", serveWebPage("admin/config.html")),
		webGet("/admin/cache", serveWebPage("admin/cache.html")),
	webGet("/admin/build", serveWebPage("admin/build.html")),
		webGet("/webui", redirectWeb("/webui/login")),
		webGet("/webui/login", handleWebUILogin),
		webGet("/webui/chat", serveWebUIPage("webui/chat.html")),
		webGet("/webui/chatkit", serveWebUIPage("webui/chatkit.html")),
		webGet("/webui/masonry", serveWebUIPage("webui/masonry.html")),
		webGet("/webui/api/verify", webuiapi.VerifyHandler(webRouterAuthSettings)),
		webGet("/meta", handleWebMeta),
		webGet("/meta/update", handleWebUpdateMeta),
	}
}

// NewRouter returns the unified frontend HTTP surface.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	for _, route := range webRoutes() {
		if route.Method == "" {
			mux.Handle(route.Path, route.Handler)
			continue
		}
		mux.Handle(route.Path, webMethod(route.Method, route.Handler.ServeHTTP))
	}
	return mux
}

func webMethod(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handler(w, r)
	}
}

func handleWebRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusTemporaryRedirect)
}

func redirectWeb(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	}
}

func serveWebPage(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		webRouterServeHTML(w, r, path)
	}
}

func handleWebUILogin(w http.ResponseWriter, r *http.Request) {
	if !auth.IsWebUIEnabled(webRouterAuthSettings()) {
		http.NotFound(w, r)
		return
	}
	webRouterServeHTML(w, r, "webui/login.html")
}

func serveWebUIPage(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.IsWebUIEnabled(webRouterAuthSettings()) {
			http.NotFound(w, r)
			return
		}
		webRouterServeHTML(w, r, path)
	}
}

func handleWebMeta(w http.ResponseWriter, r *http.Request) {
	version := webRouterProjectVersion()
	writeWebJSON(w, http.StatusOK, map[string]any{"version": version, "asset_version": version})
}

func handleWebUpdateMeta(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "true"
	writeWebJSON(w, http.StatusOK, webRouterLatestRelease(context.Background(), force))
}

func writeWebJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeWebError(w http.ResponseWriter, err error) {
	if appErr, ok := err.(*platform.AppError); ok {
		writeWebJSON(w, appErr.Status, appErr.ToDict())
		return
	}
	writeWebJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"message": err.Error()}})
}
