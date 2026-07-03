package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	grokapp "github.com/dslzl/gork/app"
	"github.com/dslzl/gork/app/platform/auth"
	"github.com/dslzl/gork/app/platform/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if handled, code, err := runGorkCommand(ctx, os.Args[1:], os.Stdout, os.Stderr); handled {
		if err != nil {
			log.Printf("command failed: %v", err)
		}
		os.Exit(code)
	}

	application := grokapp.NewApp(grokapp.AppOptions{})
	if err := application.Start(ctx); err != nil {
		log.Fatalf("startup failed: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := application.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown failed: %v", err)
		}
	}()

	server, err := newGorkHTTPServer(buildGorkHTTPServerOptions(application.Handler()))
	if err != nil {
		log.Fatalf("server configuration failed: %v", err)
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("gork listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
}

func buildGorkHTTPServerOptions(handler http.Handler) gorkHTTPServerOptions {
	return gorkHTTPServerOptions{
		Address:           listenAddress(),
		Handler:           handler,
		APIKeys:           auth.GetAPIKeys(auth.AuthSettings{APIKey: config.GlobalConfig.Get("app.api_key", "")}),
		AllowUnauth:       allowUnauthenticatedAPI(),
		ReadTimeout:       configSeconds("server.read_timeout_seconds", 0),
		ReadHeaderTimeout: configSeconds("server.read_header_timeout_seconds", 15),
		IdleTimeout:       configSeconds("server.idle_timeout_seconds", 0),
		MaxHeaderBytes:    config.GlobalConfig.GetInt("server.max_header_bytes", 0),
	}
}

func listenAddress() string {
	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	return host + ":" + port
}

func allowUnauthenticatedAPI() bool {
	switch os.Getenv("ALLOW_UNAUTHENTICATED_API") {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	default:
		return false
	}
}

func configSeconds(key string, fallback int) time.Duration {
	seconds := config.GlobalConfig.GetInt(key, fallback)
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
