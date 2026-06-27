#!/usr/bin/env sh
set -eu

COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.warp.yml}"
GORK_SERVICE="${GORK_SERVICE:-gork}"
PROXY_URL="${PROXY_URL:-http://127.0.0.1:40080}"
TARGET_URL="${TARGET_URL:-https://grok.com}"
DOCKER_BIN="${DOCKER_BIN:-docker}"
CURL_BIN="${CURL_BIN:-curl}"

redact_url() {
  printf '%s\n' "$1" | sed -E 's#^([A-Za-z][A-Za-z0-9+.-]*://)[^/@]*@#\1***@#'
}

is_http_status() {
  case "$1" in
    [0-9][0-9][0-9]) return 0 ;;
    *) return 1 ;;
  esac
}

echo "checking gork health through compose service: $GORK_SERVICE"
"$DOCKER_BIN" compose -f "$COMPOSE_FILE" exec -T "$GORK_SERVICE" /app/gork healthcheck

echo "checking $TARGET_URL through privoxy: $(redact_url "$PROXY_URL")"
headers="$(mktemp)"
curl_err="$(mktemp)"
cleanup() {
  rm -f "$headers" "$curl_err"
}
trap cleanup EXIT

curl_exit=0
status="$("$CURL_BIN" -sSI -x "$PROXY_URL" "$TARGET_URL" -o "$headers" -w '%{http_code}' 2>"$curl_err")" || curl_exit=$?
if [ "$curl_exit" -ne 0 ] || ! is_http_status "$status" || [ "$status" -lt 200 ] || [ "$status" -ge 400 ]; then
  echo "proxy smoke failed: curl_exit=$curl_exit status=${status:-unavailable}" >&2
  if [ -s "$curl_err" ]; then
    sed -n '1,20p' "$curl_err" >&2
  fi
  if [ -s "$headers" ]; then
    sed -n '1,20p' "$headers" >&2
  fi
  exit 1
fi

echo "proxy smoke ok: status=$status"
grep -iE '^(server|cf-ray|cf-cache-status|cf-mitigated):' "$headers" || true
if grep -iq '^set-cookie:.*cf_clearance=' "$headers"; then
  echo "clearance status: cf_clearance cookie observed"
elif grep -iq '^cf-mitigated:' "$headers"; then
  echo "clearance status: cloudflare mitigation header observed"
else
  echo "clearance status: no explicit cf_clearance challenge marker observed"
fi
