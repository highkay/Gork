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

func TestSQLSchemaStatementsIncludeAccountColumns(t *testing.T) {
	columns := strings.Split(localAccountColumns, ",")
	for _, tc := range []struct {
		name    string
		dialect SQLDialect
	}{
		{name: "mysql", dialect: SQLDialectMySQL},
		{name: "postgresql", dialect: SQLDialectPostgreSQL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := strings.Join(sqlSchemaStatements(tc.dialect), "\n")
			for _, column := range columns {
				column = strings.TrimSpace(column)
				if column == "" {
					continue
				}
				if !strings.Contains(schema, column+" ") {
					t.Fatalf("%s schema missing account column %q", tc.name, column)
				}
			}
		})
	}
}
