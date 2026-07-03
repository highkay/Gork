package app

import (
	"os/exec"
	"strings"
	"testing"
)

func TestInternalPackageDependencyBoundaries(t *testing.T) {
	rules := []struct {
		root      string
		forbidden string
	}{
		{root: "./app/platform/...", forbidden: "github.com/dslzl/gork/app/products"},
		{root: "./app/control/...", forbidden: "github.com/dslzl/gork/app/products"},
		{root: "./app/dataplane/...", forbidden: "github.com/dslzl/gork/app/products/web"},
		{root: "./app/products/openai/...", forbidden: "github.com/dslzl/gork/app/products/web"},
		{root: "./app/products/anthropic/...", forbidden: "github.com/dslzl/gork/app/products/web"},
	}

	for _, rule := range rules {
		t.Run(rule.root+" excludes "+rule.forbidden, func(t *testing.T) {
			cmd := exec.Command("go", "list", "-deps", rule.root)
			cmd.Dir = ".."
			raw, err := cmd.Output()
			if err != nil {
				t.Fatalf("go list %s: %v", rule.root, err)
			}
			for _, dep := range strings.Fields(string(raw)) {
				if dep == rule.forbidden || strings.HasPrefix(dep, rule.forbidden+"/") {
					t.Fatalf("%s depends on forbidden package %s", rule.root, dep)
				}
			}
		})
	}
}
