#!/usr/bin/env bash
# run-nightly.sh — Fly.io nightly crawl (polite, idempotent, monitored).
# Fetcher → ingest (append-only) → engine (cumulative). If Chrono24 is banned,
# we keep previous verdicts and surface ALERT in logs + metrics JSON.
#
# Usage locally: ./scripts/run-nightly.sh
# On Fly: fly machine run --schedule "0 3 * * *" --volume data:/data
# Env overrides: DB_PATH, NDJSON_PATH, METRICS_PATH, GBPUSD/EURUSD/CHFUSD
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DB="${DB_PATH:-${ROOT}/data/pricing.sqlite}"
# NDJSON defaults to dated file alongside DB (so Fly /data volume is used when DB is /data/...)
if [ -n "${NDJSON_PATH:-}" ]; then NDJSON="$NDJSON_PATH"
else NDJSON="$(dirname "$DB")/nightly.$(date -u +%Y-%m-%d).ndjson"
fi
if [ -n "${METRICS_PATH:-}" ]; then METRICS="$METRICS_PATH"
else METRICS="$(dirname "$DB")/crawl_metrics.json"
fi
CONFIG="${CONFIG_PATH:-${ROOT}/config/models.json}"
LOCK="${LOCK_PATH:-/tmp/watchfetcher.lock}"

# flock prevents overlapping runs (Fly schedule may overlap if previous slow)
if command -v flock >/dev/null 2>&1; then
  exec 9>"$LOCK"
  if ! flock -n 9; then
    echo "[$(date -u +%FT%TZ)] nightly: already running, skipping" >&2
    exit 0
  fi
else
  echo "[$(date -u +%FT%TZ)] nightly: flock not found, running without lock (local dev)" >&2
fi

mkdir -p "$(dirname "$DB")" "$(dirname "$NDJSON")" "$(dirname "$METRICS")"
if [ "${SKIP_JITTER:-0}" != "1" ]; then
  RAND_JITTER=$(( RANDOM % 1800 )) # 0-30 min random delay so Fly fleet doesn't thunder-herd
  echo "[$(date -u +%FT%TZ)] nightly: sleeping ${RAND_JITTER}s jitter before crawl" >&2
  sleep "$RAND_JITTER" || true
fi

echo "[$(date -u +%FT%TZ)] nightly: fetching (80/model, polite)" >&2
# Use compiled binaries in Docker, else go run for local dev
if command -v watchfetcher >/dev/null 2>&1; then FETCH_BIN="watchfetcher"
elif [ -x "/usr/local/bin/watchfetcher" ]; then FETCH_BIN="/usr/local/bin/watchfetcher"
elif [ -x "${ROOT}/watchfetcher" ]; then FETCH_BIN="${ROOT}/watchfetcher"
else FETCH_BIN="go run ${ROOT}/cmd/fetcher"; fi
if command -v watchingest >/dev/null 2>&1; then INGEST_BIN="watchingest"
elif [ -x "/usr/local/bin/watchingest" ]; then INGEST_BIN="/usr/local/bin/watchingest"
else INGEST_BIN="go run ${ROOT}/cmd/ingest"; fi
if command -v watchengine >/dev/null 2>&1; then ENGINE_BIN="watchengine"
elif [ -x "/usr/local/bin/watchengine" ]; then ENGINE_BIN="/usr/local/bin/watchengine"
else ENGINE_BIN="go run ${ROOT}/cmd/engine"; fi

set -x
# shellcheck disable=SC2086
$FETCH_BIN \
  --config "$CONFIG" \
  --max-per-model 80 \
  --concurrency 3 \
  --out "$NDJSON" \
  --metrics "$METRICS" || FETCH_RC=$?

set +x
FETCH_RC=${FETCH_RC:-0}

if [ ! -s "$NDJSON" ] && [ "$FETCH_RC" -ne 0 ]; then
  echo "[$(date -u +%FT%TZ)] nightly: fetcher failed (rc=$FETCH_RC) and NDJSON empty — keeping previous DB" >&2
else
  LINES=$(wc -l < "$NDJSON" 2>/dev/null || echo 0)
  echo "[$(date -u +%FT%TZ)] nightly: fetched $LINES listings -> $NDJSON" >&2
  if [ "$LINES" -gt 0 ]; then
    echo "[$(date -u +%FT%TZ)] nightly: ingesting -> $DB" >&2
    # shellcheck disable=SC2086
    $INGEST_BIN --in "$NDJSON" --db "$DB"
  else
    echo "[$(date -u +%FT%TZ)] nightly: no listings to ingest (all sources failed?)" >&2
    cat "$METRICS" 2>/dev/null || true
  fi
fi

echo "[$(date -u +%FT%TZ)] nightly: recomputing verdicts (cumulative)" >&2
# shellcheck disable=SC2086
$ENGINE_BIN --db "$DB" || ENGINE_RC=$?
ENGINE_RC=${ENGINE_RC:-0}

echo "[$(date -u +%FT%TZ)] nightly: done (fetch=$FETCH_RC engine=$ENGINE_RC)" >&2
if [ -f "$METRICS" ]; then
  echo "--- crawl_metrics.json ---" >&2
  cat "$METRICS" >&2
  # Chrono24-specific alert for Fly logs (grep ALERT)
  if grep -q '"banned": true' "$METRICS" 2>/dev/null; then
    echo "ALERT: one or more sources flagged as banned — check $METRICS, will auto-retry next run" >&2
  fi
  if grep -q '"source": "chrono24"' "$METRICS" 2>/dev/null && grep -q '"jobs_ok": 0' "$METRICS" 2>/dev/null; then
    echo "ALERT: chrono24 completely failed — likely WAF block, previous verdicts preserved" >&2
  fi
fi

# Keep last 7 NDJSON files, rotate (alongside DB)
ls -t "$(dirname "$DB")"/nightly.*.ndjson 2>/dev/null | tail -n +8 | xargs rm -f 2>/dev/null || true

# Exit 0 even if fetch partially failed — engine preserved old verdicts (idempotent)
# Only fail if engine failed and DB missing
if [ "$ENGINE_RC" -ne 0 ] && [ ! -f "$DB" ]; then
  exit "$ENGINE_RC"
fi
exit 0
