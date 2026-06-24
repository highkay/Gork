#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
DATA_DIR="${DATA_DIR:-$ROOT_DIR/data}"
LOG_DIR="${LOG_DIR:-$ROOT_DIR/logs}"
TMP_DIR="${TMPDIR:-$DATA_DIR/tmp}"
DEFAULT_CONFIG="$ROOT_DIR/config.defaults.toml"

mkdir -p "$DATA_DIR" "$LOG_DIR" "$TMP_DIR"

if [ ! -f "$DATA_DIR/config.toml" ]; then
  cp "$DEFAULT_CONFIG" "$DATA_DIR/config.toml"
fi

chmod 600 "$DATA_DIR/config.toml" || true

if [ ! -w "$DATA_DIR" ]; then
  echo "DATA_DIR is not writable: $DATA_DIR" >&2
  exit 1
fi

if [ ! -w "$LOG_DIR" ]; then
  echo "LOG_DIR is not writable: $LOG_DIR" >&2
  exit 1
fi

if [ ! -w "$TMP_DIR" ]; then
  echo "TMPDIR is not writable: $TMP_DIR" >&2
  exit 1
fi
