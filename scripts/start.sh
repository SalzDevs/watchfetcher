#!/usr/bin/env bash
# start.sh — Combined API + nightly cron for Fly single-volume deployment.
# API serves on $PORT (8080) with WAL read-only, cron runs daily in background.
# This solves Fly's single-attach volume constraint (API and cron share same /data).
set -euo pipefail

DB="${DB_PATH:-/data/pricing.sqlite}"
PORT="${PORT:-8080}"

# Start API in background
echo "[$(date -u +%FT%TZ)] start: launching API on :$PORT db=$DB"
watchapi --db "$DB" --addr ":$PORT" &
API_PID=$!
echo "[$(date -u +%FT%TZ)] start: API pid $API_PID"

# Give API a moment to start and pass health check
sleep 2
if ! kill -0 $API_PID 2>/dev/null; then
  echo "API failed to start, exiting"
  exit 1
fi

# Cron loop in background
(
  # Initial run: if DB empty or no verdicts, run soon; else small jitter
  if [ ! -f "$DB" ]; then
    echo "[$(date -u +%FT%TZ)] cron: DB missing, running initial nightly..."
    SKIP_JITTER=1 bash /app/scripts/run-nightly.sh || echo "initial nightly failed"
  else
    # Quick check if verdicts exist
    if command -v sqlite3 >/dev/null 2>&1; then
      COUNT=$(sqlite3 "$DB" "SELECT COUNT(*) FROM verdicts;" 2>/dev/null || echo 0)
    else
      # fallback via watchapi health or just check file size
      COUNT=0
      if [ -f /data/crawl_metrics.json ]; then COUNT=1; fi
    fi
    if [ "$COUNT" = "0" ] || [ "$COUNT" = "" ]; then
      echo "[$(date -u +%FT%TZ)] cron: no verdicts, running initial nightly..."
      SKIP_JITTER=1 bash /app/scripts/run-nightly.sh || echo "initial nightly failed"
    else
      echo "[$(date -u +%FT%TZ)] cron: verdicts exist ($COUNT), next nightly in ~24h"
    fi
  fi

  while true; do
    # Sleep 24h + 0-30m jitter (cron's own jitter is skipped via SKIP_JITTER=1)
    JITTER=$(( RANDOM % 1800 ))
    SLEEP=$(( 86400 + JITTER ))
    echo "[$(date -u +%FT%TZ)] cron: next nightly in $SLEEP seconds (jitter $JITTER)..."
    sleep $SLEEP || true
    echo "[$(date -u +%FT%TZ)] cron: triggering nightly..."
    SKIP_JITTER=1 bash /app/scripts/run-nightly.sh || echo "[$(date -u +%FT%TZ)] cron: nightly failed, will retry next cycle"
  done
) &
CRON_PID=$!
echo "[$(date -u +%FT%TZ)] start: cron pid $CRON_PID"

# Trap signals
trap "echo 'shutting down...'; kill $API_PID $CRON_PID 2>/dev/null; wait $API_PID 2>/dev/null; exit 0" SIGINT SIGTERM

# Wait for API (main) - Fly health checks this. If API dies, exit and Fly restarts machine.
wait $API_PID
echo "API exited, shutting down cron $CRON_PID"
kill $CRON_PID 2>/dev/null || true
wait $CRON_PID 2>/dev/null || true
exit 1
