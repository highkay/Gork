package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dslzl/gork/app/platform/config"
)

func TestValidatePublicUnauthenticatedListenRequiresExplicitOverride(t *testing.T) {
	if err := validatePublicUnauthenticatedListen(":8000", nil, false); err == nil {
		t.Fatalf("public listen without API key should fail")
	}
	if err := validatePublicUnauthenticatedListen("0.0.0.0:8000", []string{}, false); err == nil {
		t.Fatalf("wildcard listen without API key should fail")
	}
	if err := validatePublicUnauthenticatedListen("[::]:8000", nil, false); err == nil {
		t.Fatalf("IPv6 wildcard listen without API key should fail")
	}
	if err := validatePublicUnauthenticatedListen("127.0.0.1:8000", nil, false); err != nil {
		t.Fatalf("loopback listen should allow empty API key: %v", err)
	}
	if err := validatePublicUnauthenticatedListen("0.0.0.0:8000", nil, true); err != nil {
		t.Fatalf("explicit unauthenticated override rejected: %v", err)
	}
	if err := validatePublicUnauthenticatedListen("0.0.0.0:8000", []string{"secret"}, false); err != nil {
		t.Fatalf("configured API key rejected: %v", err)
	}
}

func TestNewGorkHTTPServerAppliesSecurityOptions(t *testing.T) {
	options := gorkHTTPServerOptions{
		Address:           "127.0.0.1:0",
		Handler:           http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		ReadTimeout:       2 * time.Second,
		IdleTimeout:       3 * time.Second,
		MaxHeaderBytes:    4096,
		ReadHeaderTimeout: 4 * time.Second,
	}

	server, err := newGorkHTTPServer(options)
	if err != nil {
		t.Fatalf("newGorkHTTPServer returned error: %v", err)
	}
	if server.Addr != options.Address || server.Handler == nil {
		t.Fatalf("server address/handler mismatch: %#v", server)
	}
	if server.ReadTimeout != 2*time.Second || server.IdleTimeout != 3*time.Second ||
		server.ReadHeaderTimeout != 4*time.Second || server.MaxHeaderBytes != 4096 {
		t.Fatalf("server timeouts/header bytes = %#v", server)
	}
	if server.WriteTimeout != 0 {
		t.Fatalf("stream endpoints require no global write timeout, got %s", server.WriteTimeout)
	}
}

func TestBuildGorkHTTPServerOptionsUsesEnvAndConfig(t *testing.T) {
	oldConfig := config.GlobalConfig
	config.GlobalConfig = config.NewConfigSnapshot(gorkServerEmptyConfigBackend{}, config.ConfigSnapshotOptions{Env: map[string]string{}})
	t.Cleanup(func() { config.GlobalConfig = oldConfig })

	defaults := filepath.Join(t.TempDir(), "defaults.toml")
	if err := os.WriteFile(defaults, []byte(`
[app]
api_key = "secret"

[server]
read_timeout_seconds = 2
read_header_timeout_seconds = 3
idle_timeout_seconds = 4
max_header_bytes = 8192
`), 0o600); err != nil {
		t.Fatalf("write defaults: %v", err)
	}
	if err := config.GlobalConfig.Load(context.Background(), defaults); err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("PORT", "19090")
	t.Setenv("ALLOW_UNAUTHENTICATED_API", "YES")
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	options := buildGorkHTTPServerOptions(handler)

	if options.Address != "127.0.0.1:19090" {
		t.Fatalf("Address = %q", options.Address)
	}
	if options.Handler == nil {
		t.Fatal("Handler is nil")
	}
	if len(options.APIKeys) != 1 || options.APIKeys[0] != "secret" {
		t.Fatalf("APIKeys = %#v", options.APIKeys)
	}
	if !options.AllowUnauth {
		t.Fatal("AllowUnauth = false")
	}
	if options.ReadTimeout != 2*time.Second ||
		options.ReadHeaderTimeout != 3*time.Second ||
		options.IdleTimeout != 4*time.Second ||
		options.MaxHeaderBytes != 8192 {
		t.Fatalf("timeouts/header = %#v", options)
	}
}

type gorkServerEmptyConfigBackend struct{}

func (gorkServerEmptyConfigBackend) Load(context.Context) (map[string]any, error) {
	return map[string]any{}, nil
}

func (gorkServerEmptyConfigBackend) ApplyPatch(context.Context, map[string]any) error {
	return nil
}

func (gorkServerEmptyConfigBackend) Clear(context.Context) error {
	return nil
}

func (gorkServerEmptyConfigBackend) Version(context.Context) (any, error) {
	return nil, nil
}

func (gorkServerEmptyConfigBackend) Close(context.Context) error {
	return nil
}
