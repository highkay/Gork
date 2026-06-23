package backends

import (
	"database/sql"
	"os"
	"strconv"
	"strings"
	"time"
)

func configureSQLPool(db *sql.DB) {
	if db == nil {
		return
	}
	poolSize, maxOverflow, recycleSeconds := sqlPoolSettingsFromEnv()
	db.SetMaxIdleConns(poolSize)
	db.SetMaxOpenConns(poolSize + maxOverflow)
	if recycleSeconds > 0 {
		db.SetConnMaxLifetime(time.Duration(recycleSeconds) * time.Second)
	}
}

func sqlPoolSettingsFromEnv() (int, int, int) {
	serverless := isServerlessSQLRuntime()
	defaultPoolSize := 5
	defaultMaxOverflow := 10
	if serverless {
		defaultPoolSize = 1
		defaultMaxOverflow = 2
	}
	return envInt("ACCOUNT_SQL_POOL_SIZE", defaultPoolSize, 1),
		envInt("ACCOUNT_SQL_MAX_OVERFLOW", defaultMaxOverflow, 0),
		envInt("ACCOUNT_SQL_POOL_RECYCLE", 1800, 0)
}

func isServerlessSQLRuntime() bool {
	return os.Getenv("VERCEL") != "" ||
		os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" ||
		os.Getenv("FUNCTIONS_WORKER_RUNTIME") != ""
}

func envInt(name string, defaultValue, minimum int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum {
		return defaultValue
	}
	return value
}
