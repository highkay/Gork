package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDefaultsCoverRuntimeConfigKeys(t *testing.T) {
	root := findConfigRepoRootForTest(t)
	defaults, err := LoadTOML(filepath.Join(root, "config.defaults.toml"))
	if err != nil {
		t.Fatalf("LoadTOML defaults: %v", err)
	}
	defaultKeys := map[string]bool{}
	for key := range FlattenConfig(defaults, "") {
		defaultKeys[key] = true
	}

	readKeys := runtimeConfigReadKeys(t, root)
	missing := []string{}
	for key := range readKeys {
		if !defaultKeys[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("runtime config keys missing defaults: %s", strings.Join(missing, ", "))
	}
}

func TestRuntimeConfigSchemaMetadata(t *testing.T) {
	defaults, err := LoadTOML(filepath.Join(findConfigRepoRootForTest(t), "config.defaults.toml"))
	if err != nil {
		t.Fatalf("LoadTOML defaults: %v", err)
	}
	entries := map[string]ConfigSchemaEntry{}
	for _, entry := range DefaultSchema(defaults) {
		entries[entry.Key] = entry
	}
	if entry := entries["security.media.signed_url_ttl_seconds"]; !entry.HotReload {
		t.Fatalf("security.media.signed_url_ttl_seconds HotReload = false, want true")
	}
	if entry := entries["proxy.clearance.flaresolverr_url"]; entry.HotReload {
		t.Fatalf("proxy.clearance.flaresolverr_url HotReload = true, want false")
	}
	if entry := entries["proxy.clearance.mode"]; entry.Sensitive {
		t.Fatalf("proxy.clearance.mode Sensitive = true, want false")
	}
	if entry := entries["proxy.clearance.cf_cookies"]; !entry.Sensitive {
		t.Fatalf("proxy.clearance.cf_cookies Sensitive = false, want true")
	}
}

func findConfigRepoRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "config.defaults.toml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("config.defaults.toml not found")
		}
		dir = parent
	}
}

func runtimeConfigReadKeys(t *testing.T, root string) map[string]bool {
	t.Helper()
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`GlobalConfig\.Get(?:Str|Int|Float|Bool|List)?\("([^"]+)"`),
		regexp.MustCompile(`GetConfig\("([^"]+)"`),
		regexp.MustCompile(`appBoolConfig\("([^"]+)"`),
	}
	keys := map[string]bool{}
	for _, base := range []string{"app", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, base), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(raw)
			for _, pattern := range patterns {
				for _, match := range pattern.FindAllStringSubmatch(text, -1) {
					keys[match[1]] = true
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}
	return keys
}
