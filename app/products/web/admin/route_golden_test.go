package admin

import (
	"os"
	"strings"
	"testing"
)

func TestAdminRouteGoldenTable(t *testing.T) {
	lines := make([]string, 0, len(adminRoutes()))
	for _, route := range adminRoutes() {
		lines = append(lines, strings.Join(route.Methods, "|")+" "+route.Path)
	}
	got := strings.Join(lines, "\n") + "\n"
	want, err := os.ReadFile("testdata/admin_routes.golden")
	if err != nil {
		t.Fatalf("read route golden: %v", err)
	}
	if got != string(want) {
		t.Fatalf("route golden mismatch\nwant:\n%s\ngot:\n%s", string(want), got)
	}
}
