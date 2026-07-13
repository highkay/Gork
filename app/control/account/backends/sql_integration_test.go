package backends

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	account "github.com/dslzl/gork/app/control/account"
)

func TestSQLAccountRepositoryContainerIntegration(t *testing.T) {
	tests := []struct {
		name    string
		envName string
		driver  string
		dialect SQLDialect
	}{
		{name: "mysql", envName: "ACCOUNT_MYSQL_TEST_URL", driver: "mysql", dialect: SQLDialectMySQL},
		{name: "postgresql", envName: "ACCOUNT_POSTGRESQL_TEST_URL", driver: "pgx", dialect: SQLDialectPostgreSQL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawURL := strings.TrimSpace(os.Getenv(tt.envName))
			if rawURL == "" {
				t.Skipf("set %s to a container DSN to run %s integration test", tt.envName, tt.name)
			}

			ctx := context.Background()
			connectString, err := prepareSQLConnectString(tt.dialect, rawURL)
			if err != nil {
				t.Fatalf("prepareSQLConnectString returned error: %v", err)
			}
			db, err := sql.Open(tt.driver, connectString)
			if err != nil {
				t.Fatalf("sql.Open returned error: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			configureSQLPool(db)
			if got := db.Stats().MaxOpenConnections; got <= 0 {
				t.Fatalf("MaxOpenConnections = %d, want configured positive pool limit", got)
			}

			repo := NewSQLAccountRepository(db, tt.dialect, false)
			if err := repo.Initialize(ctx); err != nil {
				t.Fatalf("Initialize returned error: %v", err)
			}

			tag := fmt.Sprintf("integration-%s-%d", tt.name, time.Now().UnixNano())
			upserts := make([]account.AccountUpsert, 0, 120)
			tokens := make([]string, 0, 120)
			for i := 0; i < 120; i++ {
				token := fmt.Sprintf("%s-token-%03d", tag, i)
				tokens = append(tokens, token)
				upserts = append(upserts, account.AccountUpsert{
					Token: token,
					Pool:  "basic",
					Tags:  []string{tag},
					Ext:   map[string]any{"ordinal": i},
				})
			}
			t.Cleanup(func() { _, _ = repo.DeleteAccounts(context.Background(), tokens) })

			const workers = 6
			errCh := make(chan error, workers)
			var wg sync.WaitGroup
			for worker := 0; worker < workers; worker++ {
				start := worker * len(upserts) / workers
				end := (worker + 1) * len(upserts) / workers
				wg.Add(1)
				go func(batch []account.AccountUpsert) {
					defer wg.Done()
					_, err := repo.UpsertAccounts(ctx, batch)
					errCh <- err
				}(upserts[start:end])
			}
			wg.Wait()
			close(errCh)
			for err := range errCh {
				if err != nil {
					t.Fatalf("concurrent UpsertAccounts returned error: %v", err)
				}
			}

			page, err := repo.ListAccounts(ctx, account.ListAccountsQuery{
				Page:     2,
				PageSize: 25,
				Tags:     []string{tag},
				SortBy:   "token",
			})
			if err != nil {
				t.Fatalf("ListAccounts returned error: %v", err)
			}
			if page.Total != len(upserts) || len(page.Items) != 25 || page.Items[0].Token != tokens[25] {
				t.Fatalf("filtered page total/items/first = %d/%d/%q", page.Total, len(page.Items), firstAccountToken(page.Items))
			}
		})
	}
}

func firstAccountToken(items []account.AccountRecord) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].Token
}
