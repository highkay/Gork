package backends

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	mysql "github.com/go-sql-driver/mysql"
)

func extractSQLSSLOptions(dialect SQLDialect, parsed *url.URL) (map[string]string, error) {
	query := parsed.Query()
	keys := sqlSSLQueryKeys(dialect)
	options := map[string]string{}
	for key, values := range query {
		lowerKey := strings.ToLower(key)
		if !keys[lowerKey] {
			continue
		}
		if len(values) > 0 {
			if _, exists := options[lowerKey]; !exists {
				options[lowerKey] = strings.TrimSpace(values[0])
			}
		}
		query.Del(key)
	}
	parsed.RawQuery = query.Encode()
	return options, nil
}

func sqlSSLQueryKeys(dialect SQLDialect) map[string]bool {
	keys := map[string]bool{}
	for _, key := range []string{"sslmode", "ssl-mode", "ssl"} {
		keys[key] = true
	}
	if dialect == SQLDialectPostgreSQL {
		for _, key := range []string{"sslrootcert", "sslcert", "sslkey", "sslcrl", "sslpassword", "sslnegotiation", "ssl_min_protocol_version", "ssl_max_protocol_version"} {
			keys[key] = true
		}
		return keys
	}
	for _, key := range []string{"ssl-ca", "ssl-capath", "ssl-cert", "ssl-key", "ssl-check-hostname", "ssl-cipher"} {
		keys[key] = true
	}
	return keys
}

func resolveSQLSSLMode(dialect SQLDialect, options map[string]string) (string, error) {
	for _, key := range []string{"sslmode", "ssl-mode", "ssl"} {
		if mode := strings.TrimSpace(options[key]); mode != "" {
			return normalizeSQLSSLMode(dialect, mode)
		}
	}
	if hasAnySSLOption(options) {
		if dialect == SQLDialectPostgreSQL {
			return "", errors.New("PostgreSQL SSL URL parameters require sslmode to be set explicitly")
		}
		return "", errors.New("MySQL SSL URL parameters require ssl-mode to be set explicitly")
	}
	return "", nil
}

func normalizeSQLSSLMode(dialect SQLDialect, rawMode string) (string, error) {
	mode := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(rawMode), "_", "-"))
	if mode == "" {
		return "", errors.New("SSL mode cannot be empty")
	}
	aliases := map[string]string{}
	if dialect == SQLDialectMySQL {
		aliases = map[string]string{
			"disable": "disabled", "disabled": "disabled", "false": "disabled", "0": "disabled", "no": "disabled", "off": "disabled",
			"prefer": "preferred", "preferred": "preferred", "allow": "preferred",
			"required": "required", "require": "required", "true": "required", "1": "required", "yes": "required", "on": "required",
			"verify-ca":   "verify_ca",
			"verify-full": "verify_identity", "verify-identity": "verify_identity",
		}
	} else {
		aliases = map[string]string{
			"disabled": "disable", "disable": "disable", "false": "disable", "0": "disable", "no": "disable", "off": "disable",
			"preferred": "prefer", "prefer": "prefer",
			"allow":    "allow",
			"required": "require", "require": "require", "true": "require", "1": "require", "yes": "require", "on": "require",
			"verify-ca":   "verify-ca",
			"verify-full": "verify-full", "verify-identity": "verify-full",
		}
	}
	if canonical, ok := aliases[mode]; ok {
		return canonical, nil
	}
	return "", fmt.Errorf("Unsupported SSL mode %q for SQL dialect %q", rawMode, dialect)
}

func hasAnySSLOption(options map[string]string) bool {
	for _, value := range options {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func hasSSLOption(options map[string]string, keys ...string) bool {
	for _, key := range keys {
		if strings.TrimSpace(options[key]) != "" {
			return true
		}
	}
	return false
}

func applyPostgreSQLSSLOptions(parsed *url.URL, options map[string]string) error {
	mode, err := resolveSQLSSLMode(SQLDialectPostgreSQL, options)
	if err != nil {
		return err
	}
	if err := validatePostgreSQLSSLOptions(mode, options); err != nil {
		return err
	}
	if mode == "" {
		return nil
	}
	if err := validatePostgreSQLSSLCertFiles(options); err != nil {
		return err
	}
	query := parsed.Query()
	query.Set("sslmode", mode)
	for _, key := range []string{"sslrootcert", "sslcert", "sslkey"} {
		if value := strings.TrimSpace(options[key]); value != "" {
			query.Set(key, value)
		}
	}
	parsed.RawQuery = query.Encode()
	return nil
}

func validatePostgreSQLSSLOptions(mode string, options map[string]string) error {
	var unsupported []string
	for _, key := range []string{"sslcrl", "sslpassword", "sslnegotiation", "ssl_min_protocol_version", "ssl_max_protocol_version"} {
		if strings.TrimSpace(options[key]) != "" {
			unsupported = append(unsupported, key)
		}
	}
	if len(unsupported) > 0 {
		sort.Strings(unsupported)
		return fmt.Errorf("Unsupported PostgreSQL SSL URL parameter(s): %s", strings.Join(unsupported, ", "))
	}
	if mode == "disable" && hasSSLOption(options, "sslrootcert", "sslcert", "sslkey") {
		return errors.New("PostgreSQL SSL certificate parameters cannot be used with sslmode=disable")
	}
	if (mode == "allow" || mode == "prefer") && hasSSLOption(options, "sslrootcert", "sslcert", "sslkey") {
		return errors.New("PostgreSQL sslmode=allow/prefer is not supported with certificate URL parameters")
	}
	if strings.TrimSpace(options["sslkey"]) != "" && strings.TrimSpace(options["sslcert"]) == "" {
		return errors.New("PostgreSQL sslkey requires sslcert")
	}
	return nil
}

func validatePostgreSQLSSLCertFiles(options map[string]string) error {
	for _, key := range []string{"sslrootcert", "sslcert"} {
		if path := strings.TrimSpace(options[key]); path != "" {
			if _, err := os.Stat(path); err != nil {
				return err
			}
		}
	}
	if keyPath := strings.TrimSpace(options["sslkey"]); keyPath != "" {
		if _, err := os.Stat(keyPath); err != nil {
			return err
		}
	}
	return nil
}

func buildMySQLTLSConfigName(options map[string]string, serverName string) (string, error) {
	mode, err := resolveSQLSSLMode(SQLDialectMySQL, options)
	if err != nil {
		return "", err
	}
	if mode == "" || mode == "disabled" {
		if mode == "disabled" && hasSSLOption(options, "ssl-ca", "ssl-capath", "ssl-cert", "ssl-key") {
			return "", errors.New("MySQL SSL certificate parameters cannot be used with ssl-mode=disabled")
		}
		return "", nil
	}
	if mode == "preferred" {
		return "", errors.New("MySQL ssl-mode=allow/prefer is not supported by aiomysql")
	}
	verifyHostname, err := parseSQLSSLBool("ssl-check-hostname", options["ssl-check-hostname"])
	if err != nil {
		return "", err
	}
	if mode == "required" && verifyHostname != nil && *verifyHostname {
		return "", errors.New("MySQL ssl-check-hostname=true requires ssl-mode=verify_identity")
	}
	if strings.TrimSpace(options["ssl-key"]) != "" && strings.TrimSpace(options["ssl-cert"]) == "" {
		return "", errors.New("MySQL ssl-key requires ssl-cert")
	}
	config, err := buildMySQLTLSConfig(mode, options, serverName, verifyHostname)
	if err != nil {
		return "", err
	}
	name := sqlTLSConfigName(options, serverName, mode)
	if err := mysql.RegisterTLSConfig(name, config); err != nil {
		return "", err
	}
	return name, nil
}

func parseSQLSSLBool(name, rawValue string) (*bool, error) {
	value := strings.ToLower(strings.TrimSpace(rawValue))
	if value == "" {
		return nil, nil
	}
	switch value {
	case "1", "true", "yes", "on":
		result := true
		return &result, nil
	case "0", "false", "no", "off":
		result := false
		return &result, nil
	default:
		return nil, fmt.Errorf("Unsupported boolean value %q for SQL SSL option %q", rawValue, name)
	}
}

func buildMySQLTLSConfig(mode string, options map[string]string, serverName string, verifyHostname *bool) (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if serverName != "" {
		config.ServerName = serverName
	}
	switch mode {
	case "required":
		config.InsecureSkipVerify = true
	case "verify_ca":
		config.InsecureSkipVerify = true
		config.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verifySQLCertificateChain(rawCerts, config.RootCAs)
		}
	case "verify_identity":
		config.InsecureSkipVerify = false
	}
	if verifyHostname != nil {
		config.InsecureSkipVerify = !*verifyHostname
		if *verifyHostname {
			config.VerifyPeerCertificate = nil
		} else if mode == "verify_ca" || mode == "verify_identity" {
			config.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				return verifySQLCertificateChain(rawCerts, config.RootCAs)
			}
		} else {
			config.VerifyPeerCertificate = nil
		}
	}
	if err := applyMySQLTLSCertFiles(config, options); err != nil {
		return nil, err
	}
	if err := applyMySQLTLSCipher(config, options["ssl-cipher"]); err != nil {
		return nil, err
	}
	return config, nil
}

func verifySQLCertificateChain(rawCerts [][]byte, roots *x509.CertPool) error {
	if len(rawCerts) == 0 {
		return errors.New("SQL TLS peer did not provide certificates")
	}
	certs := make([]*x509.Certificate, 0, len(rawCerts))
	for _, raw := range rawCerts {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			return err
		}
		certs = append(certs, cert)
	}
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}
	_, err := certs[0].Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
	})
	return err
}

func applyMySQLTLSCipher(config *tls.Config, rawCipher string) error {
	rawCipher = strings.TrimSpace(rawCipher)
	if rawCipher == "" {
		return nil
	}
	cipherSuites := strings.Split(rawCipher, ":")
	for _, item := range cipherSuites {
		cipher, ok := mysqlTLSCipherSuite(strings.TrimSpace(item))
		if !ok {
			return fmt.Errorf("Unsupported MySQL ssl-cipher %q", item)
		}
		config.CipherSuites = append(config.CipherSuites, cipher)
	}
	return nil
}

func mysqlTLSCipherSuite(name string) (uint16, bool) {
	switch strings.ToUpper(strings.ReplaceAll(name, "-", "_")) {
	case "ECDHE_RSA_AES128_GCM_SHA256":
		return tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, true
	case "ECDHE_RSA_AES256_GCM_SHA384":
		return tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, true
	case "ECDHE_ECDSA_AES128_GCM_SHA256":
		return tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, true
	case "ECDHE_ECDSA_AES256_GCM_SHA384":
		return tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384, true
	case "ECDHE_RSA_CHACHA20_POLY1305_SHA256":
		return tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256, true
	case "ECDHE_ECDSA_CHACHA20_POLY1305_SHA256":
		return tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256, true
	case "ECDHE_RSA_AES128_SHA", "ECDHE_RSA_AES_128_CBC_SHA":
		return tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA, true
	case "ECDHE_RSA_AES256_SHA", "ECDHE_RSA_AES_256_CBC_SHA":
		return tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA, true
	case "RSA_AES128_GCM_SHA256":
		return tls.TLS_RSA_WITH_AES_128_GCM_SHA256, true
	case "RSA_AES256_GCM_SHA384":
		return tls.TLS_RSA_WITH_AES_256_GCM_SHA384, true
	case "AES128_SHA", "RSA_AES_128_CBC_SHA":
		return tls.TLS_RSA_WITH_AES_128_CBC_SHA, true
	case "AES256_SHA", "RSA_AES_256_CBC_SHA":
		return tls.TLS_RSA_WITH_AES_256_CBC_SHA, true
	default:
		return 0, false
	}
}

func applyMySQLTLSCertFiles(config *tls.Config, options map[string]string) error {
	if caPath := strings.TrimSpace(options["ssl-ca"]); caPath != "" {
		pool := x509.NewCertPool()
		pem, err := os.ReadFile(caPath)
		if err != nil {
			return err
		}
		if ok := pool.AppendCertsFromPEM(pem); !ok {
			return fmt.Errorf("failed to append MySQL SSL CA certificate %q", caPath)
		}
		config.RootCAs = pool
	}
	if capath := strings.TrimSpace(options["ssl-capath"]); capath != "" {
		pool := config.RootCAs
		if pool == nil {
			pool = x509.NewCertPool()
		}
		entries, err := os.ReadDir(capath)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			pem, err := os.ReadFile(capath + string(os.PathSeparator) + entry.Name())
			if err != nil {
				return err
			}
			pool.AppendCertsFromPEM(pem)
		}
		config.RootCAs = pool
	}
	certPath := strings.TrimSpace(options["ssl-cert"])
	keyPath := strings.TrimSpace(options["ssl-key"])
	if certPath != "" {
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return err
		}
		config.Certificates = []tls.Certificate{cert}
	}
	return nil
}

func sqlTLSConfigName(options map[string]string, serverName, mode string) string {
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	builder.WriteString(serverName)
	builder.WriteByte('\n')
	builder.WriteString(mode)
	for _, key := range keys {
		builder.WriteByte('\n')
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(options[key])
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return "gork_" + hex.EncodeToString(sum[:8])
}
