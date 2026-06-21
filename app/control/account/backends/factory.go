package backends

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	account "github.com/dslzl/gork/app/control/account"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/deployenv"
)

var supportedBackends = map[string]struct{}{
	"local":      {},
	"redis":      {},
	"mysql":      {},
	"postgresql": {},
}

type RepositoryConstructor func(string) (account.AccountRepository, error)
type RepositoryRESTConstructor func(deployenv.RedisREST) (account.AccountRepository, error)

type RepositoryConstructors struct {
	Local      RepositoryConstructor
	Redis      RepositoryConstructor
	RedisREST  RepositoryRESTConstructor
	MySQL      RepositoryConstructor
	PostgreSQL RepositoryConstructor
}

func CreateRepository(
	env map[string]string,
	constructors RepositoryConstructors,
) (account.AccountRepository, error) {
	constructors = constructors.WithDefaults()
	backend, err := GetRepositoryBackend(env)
	if err != nil {
		return nil, err
	}
	return createRepositoryForBackend(env, constructors, backend)
}

func DescribeRepositoryTarget(env map[string]string) (string, string, error) {
	storage, err := deployenv.ResolveStorage(env)
	if err != nil {
		return "", "", err
	}
	switch storage.Backend {
	case "local":
		return "local", ResolveLocalDBPath(env), nil
	case "redis":
		if storage.Redis.URL != "" {
			return "redis", RedactRepositoryURL(storage.Redis.URL), nil
		}
		if storage.Redis.REST != nil {
			return "redis", RedactRepositoryURL(storage.Redis.REST.URL), nil
		}
		return "", "", fmt.Errorf("Redis account storage requires ACCOUNT_REDIS_URL or Upstash REST env")
	case "mysql":
		return "mysql", RedactRepositoryURL(storage.MySQLURL), nil
	case "postgresql":
		return "postgresql", RedactRepositoryURL(storage.PostgreSQLURL), nil
	default:
		return storage.Backend, "<unknown>", nil
	}
}

func GetRepositoryBackend(env map[string]string) (string, error) {
	storage, err := deployenv.ResolveStorage(env)
	if err != nil {
		return "", err
	}
	if _, ok := supportedBackends[storage.Backend]; !ok {
		return "", unknownBackendError(storage.Backend)
	}
	return storage.Backend, nil
}

func ResolveLocalDBPath(env map[string]string) string {
	raw := envValue(env, "ACCOUNT_LOCAL_PATH", platform.DataPath("accounts.db"))
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	return filepath.Join(projectRoot(), raw)
}

func RedactRepositoryURL(rawURL string) string {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return "<empty>"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return raw
	}
	return redactedParsedURL(parsed, raw)
}

func (constructors RepositoryConstructors) WithDefaults() RepositoryConstructors {
	if constructors.Local == nil {
		constructors.Local = newLocalRepository
	}
	if constructors.Redis == nil {
		constructors.Redis = newRedisRepository
	}
	if constructors.RedisREST == nil {
		constructors.RedisREST = newRedisRESTRepository
	}
	if constructors.MySQL == nil {
		constructors.MySQL = newMySQLRepository
	}
	if constructors.PostgreSQL == nil {
		constructors.PostgreSQL = newPostgreSQLRepository
	}
	return constructors
}

func createRepositoryForBackend(
	env map[string]string,
	constructors RepositoryConstructors,
	backend string,
) (account.AccountRepository, error) {
	storage, err := deployenv.ResolveStorage(env)
	if err != nil {
		return nil, err
	}
	switch backend {
	case "local":
		return constructors.Local(ResolveLocalDBPath(env))
	case "redis":
		if storage.Redis.URL != "" {
			return constructors.Redis(storage.Redis.URL)
		}
		if storage.Redis.REST != nil {
			return constructors.RedisREST(*storage.Redis.REST)
		}
		return nil, fmt.Errorf("Redis account storage requires ACCOUNT_REDIS_URL or Upstash REST env")
	case "mysql":
		return constructors.MySQL(storage.MySQLURL)
	case "postgresql":
		return constructors.PostgreSQL(storage.PostgreSQLURL)
	default:
		return nil, unknownBackendError(backend)
	}
}

func redactedParsedURL(parsed *url.URL, raw string) string {
	host := parsed.Hostname()
	if port := parsed.Port(); port != "" {
		host = host + ":" + port
	}
	auth := redactedAuth(parsed.User)
	if host == "" {
		return raw
	}
	result := parsed.Scheme + "://" + auth + host + parsed.EscapedPath()
	if parsed.RawQuery != "" {
		result += "?" + parsed.RawQuery
	}
	if fragment := parsed.EscapedFragment(); fragment != "" {
		result += "#" + fragment
	}
	return result
}

func redactedAuth(user *url.Userinfo) string {
	if user == nil {
		return ""
	}
	username := user.Username()
	password, hasPassword := user.Password()
	if username != "" {
		return username + ":***@"
	}
	if hasPassword && password != "" {
		return "***@"
	}
	return ""
}

func envValue(env map[string]string, name, defaultValue string) string {
	var raw string
	var ok bool
	if env == nil {
		raw, ok = os.LookupEnv(name)
	} else {
		raw, ok = env[name]
	}
	if !ok {
		return defaultValue
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue
	}
	return raw
}

func requiredEnv(env map[string]string, name string) (string, error) {
	value := envValue(env, name, "")
	if value == "" {
		return "", fmt.Errorf("Missing required env: %s", name)
	}
	return value, nil
}

func projectRoot() string {
	_, file, _, ok := goruntime.Caller(0)
	if ok && filepath.IsAbs(file) {
		root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
		return filepath.Clean(root)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return filepath.Clean(wd)
}

func notMigratedConstructor(name string) RepositoryConstructor {
	return func(string) (account.AccountRepository, error) {
		return nil, fmt.Errorf("%s account repository backend is not migrated to Go yet", name)
	}
}

func unknownBackendError(backend string) error {
	return fmt.Errorf("Unknown account storage backend: '%s'", backend)
}
