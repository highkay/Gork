package backends

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLBackendResponsibilitiesStaySplit(t *testing.T) {
	cases := []struct {
		file string
		want []string
	}{
		{"sql_driver_constructors.go", []string{"newMySQLRepository", "newPostgreSQLRepository"}},
		{"sql_dsn.go", []string{"prepareSQLConnectString", "SQLDialectMySQL", "SQLDialectPostgreSQL"}},
		{"sql_tls.go", []string{"extractSQLSSLOptions", "buildMySQLTLSConfig"}},
		{"sql_repository_cache.go", []string{"getOrCreateSQLRepository", "sqlRepositoryCache"}},
		{"sql_pool.go", []string{"configureSQLPool"}},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Clean(tc.file))
			if err != nil {
				t.Fatal(err)
			}
			text := string(raw)
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Fatalf("%s missing %q", tc.file, want)
				}
			}
		})
	}
}
