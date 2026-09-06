#!/usr/bin/env bash
# prod-start.sh — web server + nightly loop in one container (one volume, one DB).
# Nightly: engine recompute → alerts → (eBay ingest when creds present).
# Failures logged, never fatal — stale-but-honest beats fresh-but-wrong (PLAN §14).
set -uo pipefail

DB="${DB_PATH:-/data/watchledger.sqlite}"
PORT="${PORT:-8080}"
BASE="${BASE_URL:-https://watchfairvalue.com}"

echo "[prod-start] web on :$PORT db=$DB"
/usr/local/bin/watchledger-web --db "$DB" --addr ":$PORT" &
WEB_PID=$!

nightly() {
  echo "[nightly] $(date -u +%FT%TZ) engine start"
  /usr/local/bin/watchledger-engine --db "$DB" || echo "[nightly] engine failed (kept previous verdicts)"
  if [ -n "${EBAY_CLIENT_ID:-}" ] && [ -n "${EBAY_CLIENT_SECRET:-}" ]; then
    echo "[nightly] ebay ingest start"
    /usr/local/bin/watchledger-ingest --source ebay --db "$DB" || echo "[nightly] ebay ingest failed"
  fi
  if [ -n "${BONHAMS_SALES:-}" ]; then
    echo "[nightly] bonhams ingest start"
    ARGS=""
    for sale in $BONHAMS_SALES; do ARGS="$ARGS --auction $sale"; done
    /usr/local/bin/watchledger-ingest --source bonhams --fetch-lots --lot-delay-ms 300 $ARGS --db "$DB" || echo "[nightly] bonhams ingest failed"
  fi
  echo "[nightly] alerts start"
  /usr/local/bin/watchledger-alerts --db "$DB" --base-url "$BASE" || echo "[nightly] alerts failed"
  echo "[nightly] $(date -u +%FT%TZ) done"
}

(
  sleep 90 # let boot settle; first run shortly after deploy
  while true; do
    nightly
    sleep 86400 # 24h
  done
) &
CRON_PID=$!

trap 'echo "[prod-start] shutting down"; kill "$WEB_PID" "$CRON_PID" 2>/dev/null; exit 0' INT TERM
wait "$WEB_PID"
kill "$CRON_PID" 2>/dev/null || true
exit 1
