package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controlaccount "github.com/dslzl/gork/app/control/account"
)

type fakeDirectory struct {
	changed   bool
	revision  int
	syncCalls int
}

func (d *fakeDirectory) Revision() int { return d.revision }

func (d *fakeDirectory) SyncIfChanged(context.Context) (bool, error) {
	d.syncCalls++
	return d.changed, nil
}

type fakeRefreshService struct {
	refreshCalls int
	resetCalls   int
	limit        int
}

func (s *fakeRefreshService) RefreshScheduledLimit(_ context.Context, _ *string, limit int) (controlaccount.RefreshResult, error) {
	s.refreshCalls++
	s.limit = limit
	return controlaccount.RefreshResult{Checked: limit, Refreshed: 1}, nil
}

func (s *fakeRefreshService) ResetExpiredConsoleWindows(context.Context) (int, error) {
	s.resetCalls++
	return 3, nil
}

func TestRouterRejectsMissingAndInvalidCronSecret(t *testing.T) {
	handler := NewRouter(Dependencies{
		Secret: func() string { return "" },
	})
	req := httptest.NewRequest(http.MethodPost, "/internal/maintenance/account-sync", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing secret status=%d body=%s", rec.Code, rec.Body.String())
	}

	handler = NewRouter(Dependencies{
		Secret: func() string { return "secret" },
	})
	req = httptest.NewRequest(http.MethodPost, "/internal/maintenance/account-sync", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid secret status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRouterRunsSingleMaintenanceTasks(t *testing.T) {
	directory := &fakeDirectory{changed: true, revision: 8}
	refresh := &fakeRefreshService{}
	proxyCalls := 0
	handler := NewRouter(Dependencies{
		Secret:         func() string { return "secret" },
		Directory:      func() Directory { return directory },
		RefreshService: func() RefreshService { return refresh },
		ProxyRefresh: func(context.Context) error {
			proxyCalls++
			return nil
		},
	})

	tests := []struct {
		path string
		want string
	}{
		{"/internal/maintenance/account-sync", `"revision":8`},
		{"/internal/maintenance/account-refresh?limit=4", `"checked":4`},
		{"/internal/maintenance/console-quota-reset", `"reset":3`},
		{"/internal/maintenance/proxy-refresh", `"ok":true`},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodPost, tt.path, nil)
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tt.want) {
			t.Fatalf("%s status=%d body=%s, want body containing %s", tt.path, rec.Code, rec.Body.String(), tt.want)
		}
	}
	if directory.syncCalls != 1 || refresh.refreshCalls != 1 || refresh.resetCalls != 1 || proxyCalls != 1 {
		t.Fatalf("calls sync=%d refresh=%d reset=%d proxy=%d", directory.syncCalls, refresh.refreshCalls, refresh.resetCalls, proxyCalls)
	}
}

func TestDailyMaintenanceAllowsCronGetAndReturnsPartialErrors(t *testing.T) {
	refresh := &fakeRefreshService{}
	handler := NewRouter(Dependencies{
		Secret:         func() string { return "secret" },
		Directory:      func() Directory { return nil },
		RefreshService: func() RefreshService { return refresh },
		ProxyRefresh:   func(context.Context) error { return errors.New("proxy unavailable") },
	})
	req := httptest.NewRequest(http.MethodGet, "/internal/cron/daily-maintenance?limit=2", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("daily status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["ok"] != false || refresh.limit != 2 {
		t.Fatalf("daily payload=%v refresh limit=%d", payload, refresh.limit)
	}
}
