package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEvaluateAppReadiness_ReadyWhenConfigAndAccountsOK(t *testing.T) {
	stubAppMainRequestMiddleware(t)
	fixed := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	appMainReadinessNow = func() time.Time { return fixed }
	appMainReadinessConfig = func(context.Context) error { return nil }
	appMainReadinessAccounts = func(context.Context) (readinessAccountProbe, error) {
		return readinessAccountProbe{Total: 3, Available: 2}, nil
	}

	snapshot := evaluateAppReadiness(context.Background())
	if !snapshot.Ready || snapshot.State != "ready" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	if snapshot.UpdatedAt != fixed {
		t.Fatalf("updatedAt=%v want %v", snapshot.UpdatedAt, fixed)
	}
	if snapshot.Components["process"].State != "ok" {
		t.Fatalf("process=%#v", snapshot.Components["process"])
	}
	if snapshot.Components["config"].State != "ok" {
		t.Fatalf("config=%#v", snapshot.Components["config"])
	}
	if snapshot.Components["accounts"].State != "ok" {
		t.Fatalf("accounts=%#v", snapshot.Components["accounts"])
	}
	if snapshot.Components["accounts"].Detail != "total=3 available=2" {
		t.Fatalf("accounts detail=%q", snapshot.Components["accounts"].Detail)
	}
}

func TestEvaluateAppReadiness_NotReadyWhenAccountsEmpty(t *testing.T) {
	stubAppMainRequestMiddleware(t)
	appMainReadinessConfig = func(context.Context) error { return nil }
	appMainReadinessAccounts = func(context.Context) (readinessAccountProbe, error) {
		return readinessAccountProbe{Total: 0, Available: 0}, nil
	}

	snapshot := evaluateAppReadiness(context.Background())
	if snapshot.Ready || snapshot.State != "not_ready" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	if snapshot.Components["accounts"].State != "empty" {
		t.Fatalf("accounts=%#v", snapshot.Components["accounts"])
	}
}

func TestEvaluateAppReadiness_NotReadyWhenAccountsUnavailable(t *testing.T) {
	stubAppMainRequestMiddleware(t)
	appMainReadinessConfig = func(context.Context) error { return nil }
	appMainReadinessAccounts = func(context.Context) (readinessAccountProbe, error) {
		return readinessAccountProbe{}, errors.New("directory not bootstrapped")
	}

	snapshot := evaluateAppReadiness(context.Background())
	if snapshot.Ready || snapshot.State != "not_ready" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	if snapshot.Components["accounts"].State != "unavailable" {
		t.Fatalf("accounts=%#v", snapshot.Components["accounts"])
	}
}

func TestEvaluateAppReadiness_NotReadyWhenConfigFails(t *testing.T) {
	stubAppMainRequestMiddleware(t)
	appMainReadinessConfig = func(context.Context) error { return errors.New("config load failed") }
	appMainReadinessAccounts = func(context.Context) (readinessAccountProbe, error) {
		return readinessAccountProbe{Total: 1, Available: 1}, nil
	}

	snapshot := evaluateAppReadiness(context.Background())
	if snapshot.Ready || snapshot.State != "not_ready" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	if snapshot.Components["config"].State != "error" {
		t.Fatalf("config=%#v", snapshot.Components["config"])
	}
	// Accounts may still be reported even when config failed.
	if snapshot.Components["accounts"].State != "ok" {
		t.Fatalf("accounts=%#v", snapshot.Components["accounts"])
	}
}

func TestReadyzRoute_Returns503WhenNotReady(t *testing.T) {
	stubAppMainRequestMiddleware(t)
	appMainReadinessConfig = func(context.Context) error { return nil }
	appMainReadinessAccounts = func(context.Context) (readinessAccountProbe, error) {
		return readinessAccountProbe{Total: 0, Available: 0}, nil
	}

	app := NewApp(AppOptions{WebRouter: textHandler("web")})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload readinessSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v body=%s", err, rec.Body.String())
	}
	if payload.Ready || payload.State != "not_ready" {
		t.Fatalf("payload=%#v", payload)
	}
}

func TestReadyzRoute_Returns200WhenReady(t *testing.T) {
	stubAppMainRequestMiddleware(t)
	appMainReadinessConfig = func(context.Context) error { return nil }
	appMainReadinessAccounts = func(context.Context) (readinessAccountProbe, error) {
		return readinessAccountProbe{Total: 2, Available: 1}, nil
	}

	app := NewApp(AppOptions{WebRouter: textHandler("web")})
	assertAppResponse(t, app.Handler(), http.MethodGet, "/readyz", "", http.StatusOK, `"ready":true`)
	assertAppResponse(t, app.Handler(), http.MethodGet, "/readyz", "", http.StatusOK, `"state":"ready"`)
	// /health remains a liveness-compatible always-ok surface.
	assertAppResponse(t, app.Handler(), http.MethodGet, "/health", "", http.StatusOK, `"status":"ok"`)
}
