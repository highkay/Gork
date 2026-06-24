package openai

import (
	"os"
	"strings"
	"testing"
)

func TestOpenAIRouteGoldenTable(t *testing.T) {
	lines := make([]string, 0, len(openAIRoutes()))
	for _, route := range openAIRoutes() {
		auth := "public"
		if route.Protected {
			auth = "auth"
		}
		lines = append(lines, route.Method+" "+route.Path+" "+auth)
	}
	assertRouteGolden(t, "testdata/openai_routes.golden", lines)
}

func assertRouteGolden(t *testing.T, path string, lines []string) {
	t.Helper()
	got := strings.Join(lines, "\n") + "\n"
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read route golden: %v", err)
	}
	if got != string(want) {
		t.Fatalf("route golden mismatch\nwant:\n%s\ngot:\n%s", string(want), got)
	}
}
