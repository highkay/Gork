package main

import (
	"path/filepath"
	"testing"
)

func TestParseProfileLine(t *testing.T) {
	file, statements, count, ok := parseProfileLine("app/main.go:10.1,12.2 3 1")
	if !ok {
		t.Fatal("expected profile line to parse")
	}
	if file != "app/main.go" || statements != 3 || count != 1 {
		t.Fatalf("parseProfileLine() = (%q, %d, %d), want (app/main.go, 3, 1)", file, statements, count)
	}

	if _, _, _, ok := parseProfileLine("not enough fields"); ok {
		t.Fatal("malformed profile line parsed successfully")
	}
}

func TestPackageNameNormalizesProfilePaths(t *testing.T) {
	wd := mustGetwd()
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "relative path",
			path: "app/main.go",
			want: "./app",
		},
		{
			name: "windows separators",
			path: `app\main.go`,
			want: "./app",
		},
		{
			name: "module path",
			path: "github.com/dslzl/gork/app/main.go",
			want: "./app",
		},
		{
			name: "absolute path under repo",
			path: filepath.Join(wd, "app", "main.go"),
			want: "./app",
		},
		{
			name: "repo root file",
			path: "main.go",
			want: ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := packageName(tt.path); got != tt.want {
				t.Fatalf("packageName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
