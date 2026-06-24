package web

import (
	"os"
	"strings"
	"testing"
)

func TestWebRouteGoldenTable(t *testing.T) {
	lines := make([]string, 0, len(webRoutes()))
	for _, route := range webRoutes() {
		method := route.Method
		if method == "" {
			method = "MOUNT"
		}
		lines = append(lines, method+" "+route.Path)
	}
	got := strings.Join(lines, "\n") + "\n"
	want, err := os.ReadFile("testdata/web_routes.golden")
	if err != nil {
		t.Fatalf("read route golden: %v", err)
	}
	if got != string(want) {
		t.Fatalf("route golden mismatch\nwant:\n%s\ngot:\n%s", string(want), got)
	}
}
