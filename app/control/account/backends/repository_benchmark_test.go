package backends

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	account "github.com/dslzl/gork/app/control/account"
	_ "modernc.org/sqlite"
)

func BenchmarkAccountRepositoryListAccountsPagination(b *testing.B) {
	ctx := context.Background()
	items := benchmarkAccountUpserts(5000)
	query := account.ListAccountsQuery{
		Page:     25,
		PageSize: 100,
		SortBy:   "token",
		SortDesc: false,
	}

	benchmarks := []struct {
		name string
		repo func(*testing.B) account.AccountRepository
	}{
		{name: "local", repo: func(b *testing.B) account.AccountRepository {
			return benchmarkLocalRepository(b, ctx, items)
		}},
		{name: "sql_sqlite", repo: func(b *testing.B) account.AccountRepository {
			return benchmarkSQLRepository(b, ctx, items)
		}},
		{name: "redis_fake", repo: func(b *testing.B) account.AccountRepository {
			return benchmarkRedisRepository(b, ctx, items)
		}},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			repo := bm.repo(b)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				page, err := repo.ListAccounts(ctx, query)
				if err != nil {
					b.Fatalf("ListAccounts returned error: %v", err)
				}
				if page.Total != len(items) || len(page.Items) != query.PageSize {
					b.Fatalf("page total/items = %d/%d", page.Total, len(page.Items))
				}
			}
		})
	}
}

func benchmarkLocalRepository(b *testing.B, ctx context.Context, items []account.AccountUpsert) account.AccountRepository {
	b.Helper()
	repo := NewLocalAccountRepository(filepath.Join(b.TempDir(), "accounts.db"))
	benchmarkSeedRepository(b, ctx, repo, items)
	return repo
}

func benchmarkSQLRepository(b *testing.B, ctx context.Context, items []account.AccountUpsert) account.AccountRepository {
	b.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		b.Fatalf("sql.Open returned error: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	repo := NewSQLAccountRepository(db, SQLDialectSQLite, false)
	benchmarkSeedRepository(b, ctx, repo, items)
	return repo
}

func benchmarkRedisRepository(b *testing.B, ctx context.Context, items []account.AccountUpsert) account.AccountRepository {
	b.Helper()
	repo := NewRedisAccountRepository(newFakeRedisAccountStore())
	benchmarkSeedRepository(b, ctx, repo, items)
	return repo
}

func benchmarkSeedRepository(b *testing.B, ctx context.Context, repo account.AccountRepository, items []account.AccountUpsert) {
	b.Helper()
	if err := repo.Initialize(ctx); err != nil {
		b.Fatalf("Initialize returned error: %v", err)
	}
	if _, err := repo.UpsertAccounts(ctx, items); err != nil {
		b.Fatalf("UpsertAccounts returned error: %v", err)
	}
}

func benchmarkAccountUpserts(count int) []account.AccountUpsert {
	items := make([]account.AccountUpsert, 0, count)
	for i := 0; i < count; i++ {
		pool := "basic"
		if i%3 == 1 {
			pool = "super"
		} else if i%3 == 2 {
			pool = "heavy"
		}
		items = append(items, account.AccountUpsert{
			Token: fmt.Sprintf("tok-%05d", i),
			Pool:  pool,
			Tags:  []string{fmt.Sprintf("bucket-%02d", i%16)},
			Ext:   map[string]any{"seed": i},
		})
	}
	return items
}
