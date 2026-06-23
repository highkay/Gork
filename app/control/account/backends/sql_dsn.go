package backends

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
)

func prepareSQLConnectString(dialect SQLDialect, rawURL string) (string, error) {
	switch dialect {
	case SQLDialectMySQL:
		return prepareMySQLDSN(rawURL)
	case SQLDialectPostgreSQL:
		return preparePostgreSQLURL(rawURL)
	default:
		raw := strings.TrimSpace(rawURL)
		if raw == "" {
			return "", fmt.Errorf("%s account repository URL is required", dialect)
		}
		return raw, nil
	}
}

func normalizeMySQLDSN(rawURL string) (string, error) {
	return prepareMySQLDSN(rawURL)
}

func prepareMySQLDSN(rawURL string) (string, error) {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return "", errors.New("mysql account repository URL is required")
	}
	if !strings.Contains(raw, "://") {
		return raw, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "mysql", "mariadb", "mariadb+aiomysql", "mysql+aiomysql":
	default:
		return raw, nil
	}
	sslOptions, err := extractSQLSSLOptions(SQLDialectMySQL, parsed)
	if err != nil {
		return "", err
	}
	tlsName, err := buildMySQLTLSConfigName(sslOptions, parsed.Hostname())
	if err != nil {
		return "", err
	}
	user := parsed.User.Username()
	if password, ok := parsed.User.Password(); ok {
		user += ":" + password
	}
	host := parsed.Host
	if !strings.Contains(host, ":") {
		host = net.JoinHostPort(host, "3306")
	}
	database := strings.TrimPrefix(parsed.EscapedPath(), "/")
	queryValues := parsed.Query()
	if tlsName != "" {
		queryValues.Set("tls", tlsName)
	}
	query := normalizedQuery(queryValues)
	return fmt.Sprintf("%s@tcp(%s)/%s%s", user, host, database, query), nil
}

func normalizePostgreSQLURL(rawURL string) string {
	prepared, err := preparePostgreSQLURL(rawURL)
	if err != nil {
		return normalizePostgreSQLScheme(rawURL)
	}
	return prepared
}

func preparePostgreSQLURL(rawURL string) (string, error) {
	normalized := normalizePostgreSQLScheme(rawURL)
	if strings.TrimSpace(normalized) == "" {
		return "", errors.New("postgresql account repository URL is required")
	}
	if !strings.Contains(normalized, "://") {
		return normalized, nil
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "postgres" {
		return normalized, nil
	}
	sslOptions, err := extractSQLSSLOptions(SQLDialectPostgreSQL, parsed)
	if err != nil {
		return "", err
	}
	if err := applyPostgreSQLSSLOptions(parsed, sslOptions); err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func normalizePostgreSQLScheme(rawURL string) string {
	raw := strings.TrimSpace(rawURL)
	for _, item := range []struct{ from, to string }{
		{"postgresql+asyncpg://", "postgres://"},
		{"postgres+asyncpg://", "postgres://"},
		{"postgresql://", "postgres://"},
		{"pgsql://", "postgres://"},
	} {
		if strings.HasPrefix(raw, item.from) {
			return item.to + raw[len(item.from):]
		}
	}
	return raw
}

func normalizedQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make(url.Values, len(values))
	for _, key := range keys {
		pairs[key] = values[key]
	}
	return "?" + pairs.Encode()
}
