package deployenv

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type RedisREST struct {
	URL   string
	Token string
}

type Redis struct {
	URL  string
	REST *RedisREST
}

type Storage struct {
	Backend       string
	MySQLURL      string
	PostgreSQLURL string
	Redis         Redis
}

type Env map[string]string

func ResolveStorage(env map[string]string) (Storage, error) {
	values := Env(env)
	manualBackend, hasManualBackend := values.lookup("ACCOUNT_STORAGE")
	if hasManualBackend {
		backend := strings.ToLower(strings.TrimSpace(manualBackend))
		if backend == "" {
			backend = "local"
		}
		storage := Storage{Backend: backend}
		fillURLs(values, &storage)
		return storage, nil
	}

	storage := Storage{Backend: "local"}
	fillURLs(values, &storage)

	hasMySQL := storage.MySQLURL != ""
	hasPostgreSQL := storage.PostgreSQLURL != ""
	hasRedis := storage.Redis.URL != "" || storage.Redis.REST != nil

	if hasMySQL && hasPostgreSQL {
		return Storage{}, fmt.Errorf("multiple SQL storage providers detected; set ACCOUNT_STORAGE explicitly")
	}
	switch {
	case hasPostgreSQL:
		storage.Backend = "postgresql"
	case hasMySQL:
		storage.Backend = "mysql"
	case hasRedis:
		storage.Backend = "redis"
	}
	return storage, nil
}

func ResolveRuntimeRedis(env map[string]string) Redis {
	values := Env(env)
	if value := values.value("RUNTIME_REDIS_URL"); value != "" {
		return Redis{URL: value}
	}
	if value := values.value("ACCOUNT_REDIS_URL"); value != "" {
		return Redis{URL: value}
	}
	return detectRedis(values)
}

func fillURLs(values Env, storage *Storage) {
	storage.MySQLURL = detectMySQL(values)
	storage.PostgreSQLURL = detectPostgreSQL(values)
	storage.Redis = detectRedis(values)
}

func detectMySQL(values Env) string {
	if value := values.first("ACCOUNT_MYSQL_URL", "MYSQL_URL"); value != "" {
		return value
	}
	if value := values.value("DATABASE_URL"); isMySQLURL(value) {
		return value
	}
	return ""
}

func detectPostgreSQL(values Env) string {
	if value := values.first(
		"ACCOUNT_POSTGRESQL_URL",
		"POSTGRES_URL",
		"POSTGRES_PRISMA_URL",
		"POSTGRES_URL_NON_POOLING",
		"DATABASE_URL_UNPOOLED",
	); value != "" {
		return value
	}
	if value := values.value("DATABASE_URL"); isPostgreSQLURL(value) {
		return value
	}
	return ""
}

func detectRedis(values Env) Redis {
	if value := values.first("ACCOUNT_REDIS_URL", "REDIS_URL", "KV_URL"); value != "" {
		return Redis{URL: value}
	}
	if rest := detectRedisREST(values); rest != nil {
		return Redis{REST: rest}
	}
	return Redis{}
}

func detectRedisREST(values Env) *RedisREST {
	pairs := [][2]string{
		{"UPSTASH_REDIS_REST_URL", "UPSTASH_REDIS_REST_TOKEN"},
		{"KV_REST_API_URL", "KV_REST_API_TOKEN"},
	}
	for _, pair := range pairs {
		rawURL := values.value(pair[0])
		token := values.value(pair[1])
		if rawURL != "" && token != "" {
			return &RedisREST{URL: rawURL, Token: token}
		}
	}
	return nil
}

func (e Env) first(keys ...string) string {
	for _, key := range keys {
		if value := e.value(key); value != "" {
			return value
		}
	}
	return ""
}

func (e Env) value(key string) string {
	value, ok := e.lookup(key)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func (e Env) lookup(key string) (string, bool) {
	if e != nil {
		value, ok := e[key]
		return value, ok
	}
	return os.LookupEnv(key)
}

func isPostgreSQLURL(raw string) bool {
	scheme := parsedScheme(raw)
	return scheme == "postgres" || scheme == "postgresql"
}

func isMySQLURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if strings.Contains(raw, "@tcp(") || strings.Contains(raw, "@unix(") {
		return true
	}
	return parsedScheme(raw) == "mysql"
}

func parsedScheme(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Scheme)
}
