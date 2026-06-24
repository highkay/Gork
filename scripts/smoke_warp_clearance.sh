#!/usr/bin/env sh
set -eu

COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.warp.yml}"
GORK_SERVICE="${GORK_SERVICE:-gork}"
PROXY_URL="${PROXY_URL:-http://127.0.0.1:40080}"
TARGET_URL="${TARGET_URL:-https://grok.com}"

echo "checking gork health through compose service: $GORK_SERVICE"
docker compose -f "$COMPOSE_FILE" exec -T "$GORK_SERVICE" /app/gork healthcheck

echo "checking $TARGET_URL through privoxy: $PROXY_URL"
headers="$(mktemp)"
cleanup() {
  rm -f "$headers"
}
trap cleanup EXIT

status="$(curl -fsSI -x "$PROXY_URL" "$TARGET_URL" -o "$headers" -w '%{http_code}')"
if [ "$status" -lt 200 ] || [ "$status" -ge 400 ]; then
  echo "proxy smoke failed: status=$status" >&2
  sed -n '1,20p' "$headers" >&2
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
