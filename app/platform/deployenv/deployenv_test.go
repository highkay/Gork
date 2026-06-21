package deployenv

import "testing"

func TestResolveStorageAutoDetectsVercelMarketplaceEnv(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantBackend string
		wantSQL     string
		wantRedis   string
		wantREST    bool
	}{
		{
			name:        "neon database url",
			env:         map[string]string{"DATABASE_URL": "postgresql://user:pass@host/db"},
			wantBackend: "postgresql",
			wantSQL:     "postgresql://user:pass@host/db",
		},
		{
			name:        "supabase postgres url",
			env:         map[string]string{"POSTGRES_URL": "postgres://user:pass@host/db"},
			wantBackend: "postgresql",
			wantSQL:     "postgres://user:pass@host/db",
		},
		{
			name:        "mysql url",
			env:         map[string]string{"MYSQL_URL": "mysql://user:pass@host/db"},
			wantBackend: "mysql",
			wantSQL:     "mysql://user:pass@host/db",
		},
		{
			name:        "go mysql dsn",
			env:         map[string]string{"MYSQL_URL": "user:pass@tcp(host:3306)/db"},
			wantBackend: "mysql",
			wantSQL:     "user:pass@tcp(host:3306)/db",
		},
		{
			name:        "redis tcp",
			env:         map[string]string{"REDIS_URL": "redis://host:6379/0"},
			wantBackend: "redis",
			wantRedis:   "redis://host:6379/0",
		},
		{
			name:        "vercel kv rest",
			env:         map[string]string{"KV_REST_API_URL": "https://example.upstash.io", "KV_REST_API_TOKEN": "token"},
			wantBackend: "redis",
			wantREST:    true,
		},
		{
			name:        "sql with redis prefers sql account storage",
			env:         map[string]string{"POSTGRES_URL": "postgres://db", "KV_REST_API_URL": "https://redis", "KV_REST_API_TOKEN": "token"},
			wantBackend: "postgresql",
			wantSQL:     "postgres://db",
			wantREST:    true,
		},
		{
			name:        "empty keeps local",
			env:         map[string]string{},
			wantBackend: "local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveStorage(tt.env)
			if err != nil {
				t.Fatalf("ResolveStorage returned error: %v", err)
			}
			if got.Backend != tt.wantBackend {
				t.Fatalf("Backend = %q, want %q", got.Backend, tt.wantBackend)
			}
			if tt.wantBackend == "postgresql" && got.PostgreSQLURL != tt.wantSQL {
				t.Fatalf("PostgreSQLURL = %q, want %q", got.PostgreSQLURL, tt.wantSQL)
			}
			if tt.wantBackend == "mysql" && got.MySQLURL != tt.wantSQL {
				t.Fatalf("MySQLURL = %q, want %q", got.MySQLURL, tt.wantSQL)
			}
			if got.Redis.URL != tt.wantRedis {
				t.Fatalf("Redis.URL = %q, want %q", got.Redis.URL, tt.wantRedis)
			}
			if (got.Redis.REST != nil) != tt.wantREST {
				t.Fatalf("Redis.REST present = %t, want %t", got.Redis.REST != nil, tt.wantREST)
			}
		})
	}
}

func TestResolveStorageManualBackendWinsAndAutoFillsURLs(t *testing.T) {
	got, err := ResolveStorage(map[string]string{
		"ACCOUNT_STORAGE": " redis ",
		"POSTGRES_URL":    "postgres://db",
		"REDIS_URL":       "redis://host:6379/0",
	})
	if err != nil {
		t.Fatalf("ResolveStorage returned error: %v", err)
	}
	if got.Backend != "redis" || got.Redis.URL != "redis://host:6379/0" {
		t.Fatalf("manual redis result = %#v", got)
	}

	got, err = ResolveStorage(map[string]string{
		"ACCOUNT_STORAGE": "",
		"POSTGRES_URL":    "postgres://db",
	})
	if err != nil {
		t.Fatalf("ResolveStorage empty manual returned error: %v", err)
	}
	if got.Backend != "local" {
		t.Fatalf("empty ACCOUNT_STORAGE backend = %q, want local", got.Backend)
	}
}

func TestResolveStorageAmbiguousSQL(t *testing.T) {
	_, err := ResolveStorage(map[string]string{
		"MYSQL_URL":    "mysql://db",
		"POSTGRES_URL": "postgres://db",
	})
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if want := "multiple SQL storage providers detected; set ACCOUNT_STORAGE explicitly"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestResolveRuntimeRedisPriority(t *testing.T) {
	got := ResolveRuntimeRedis(map[string]string{
		"RUNTIME_REDIS_URL":        " redis://runtime ",
		"ACCOUNT_REDIS_URL":        "redis://account",
		"UPSTASH_REDIS_REST_URL":   "https://redis",
		"UPSTASH_REDIS_REST_TOKEN": "token",
	})
	if got.URL != "redis://runtime" || got.REST != nil {
		t.Fatalf("runtime redis = %#v", got)
	}

	got = ResolveRuntimeRedis(map[string]string{
		"ACCOUNT_REDIS_URL": "",
		"KV_REST_API_URL":   "https://redis",
		"KV_REST_API_TOKEN": "token",
	})
	if got.URL != "" || got.REST == nil || got.REST.URL != "https://redis" || got.REST.Token != "token" {
		t.Fatalf("runtime rest redis = %#v", got)
	}
}
