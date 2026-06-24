#!/usr/bin/env sh
set -eu

export HOST="${HOST:-${SERVER_HOST:-0.0.0.0}}"
export PORT="${PORT:-${SERVER_PORT:-8000}}"

if [ "$#" -eq 0 ]; then
  set -- /app/gork
fi

/app/scripts/init_storage.sh

if [ "$(id -u)" = "0" ]; then
  runtime_user="${GORK_USER:-gork}"
  runtime_group="${GORK_GROUP:-gork}"
  chown -R "$runtime_user:$runtime_group" "${DATA_DIR:-/app/data}" "${LOG_DIR:-/app/logs}" "${TMPDIR:-${DATA_DIR:-/app/data}/tmp}"
  exec su-exec "$runtime_user:$runtime_group" "$@"
fi

exec "$@"
