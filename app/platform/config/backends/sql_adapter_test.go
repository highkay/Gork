package backends

import (
	"database/sql"
	"testing"
)

func TestPrepareConfigSQLConnectStringNormalizesSupportedURLs(t *testing.T) {
	mysqlDSN, err := prepareConfigSQLConnectString("mysql", " mysql://user:pass@localhost:3306/gork?parseTime=true ")
	if err != nil {
		t.Fatalf("mysql connect string error = %v", err)
	}
	if want := "user:pass@tcp(localhost:3306)/gork?parseTime=true"; mysqlDSN != want {
		t.Fatalf("mysql connect string = %q, want %q", mysqlDSN, want)
	}

	mysqlDSN, err = prepareConfigSQLConnectString("mysql", "user:pass@tcp(localhost:3306)/gork")
	if err != nil {
		t.Fatalf("mysql raw DSN error = %v", err)
	}
	if want := "user:pass@tcp(localhost:3306)/gork"; mysqlDSN != want {
		t.Fatalf("mysql raw DSN = %q, want %q", mysqlDSN, want)
	}

	postgresURL, err := prepareConfigSQLConnectString("postgresql", "postgresql+asyncpg://user:pass@localhost/gork")
	if err != nil {
		t.Fatalf("postgres connect string error = %v", err)
	}
	if want := "postgres://user:pass@localhost/gork"; postgresURL != want {
		t.Fatalf("postgres URL = %q, want %q", postgresURL, want)
	}
}

func TestPrepareConfigSQLConnectStringRejectsUnsupportedDialect(t *testing.T) {
	if _, err := prepareConfigSQLConnectString("sqlite", "sqlite://config.db"); err == nil {
		t.Fatalf("unsupported dialect error = nil")
	}
	if _, err := prepareConfigSQLConnectString("mysql", " "); err == nil {
		t.Fatalf("empty mysql URL error = nil")
	}
}

func TestConfigSQLDriverAndPoolDefaults(t *testing.T) {
	driver, err := configSQLDriverName("mysql")
	if err != nil || driver != "mysql" {
		t.Fatalf("mysql driver = %q, err = %v", driver, err)
	}
	driver, err = configSQLDriverName("postgresql")
	if err != nil || driver != "pgx" {
		t.Fatalf("postgres driver = %q, err = %v", driver, err)
	}

	db, err := sql.Open("mysql", "user:pass@tcp(localhost:3306)/gork")
	if err != nil {
		t.Fatalf("open db handle: %v", err)
	}
	defer db.Close()

	configureConfigSQLPool(db)
	if got := db.Stats().MaxOpenConnections; got != configSQLMaxOpenConns {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, configSQLMaxOpenConns)
	}
}
