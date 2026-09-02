-- 0001_foundation.sql — WatchLedger core schema (PLAN.md §5).
-- Append-only everywhere. No UPDATE paths on ledger/raw/verdict tables.

CREATE TABLE schema_info (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- ---- Sources: rights registry. G6 enforced structurally (PLAN §13). ----
CREATE TABLE sources (
  id                 TEXT PRIMARY KEY,
  name               TEXT NOT NULL,
  access_status      TEXT NOT NULL DEFAULT 'unverified'
                     CHECK (access_status IN ('approved','pending','unverified','revoked')),
  rights_basis       TEXT,
  rights_reviewed_at INTEGER,
  reviewer           TEXT,
  cadence_hours      INTEGER,
  enabled            INTEGER NOT NULL DEFAULT 0,
  CHECK (enabled = 0 OR (access_status = 'approved'
         AND rights_basis IS NOT NULL AND rights_reviewed_at IS NOT NULL))
);

-- ---- Catalogue: curated, versioned (PLAN §5 four stores). ----
CREATE TABLE catalogue_references (
  ref        TEXT PRIMARY KEY,
  brand      TEXT NOT NULL,
  family     TEXT NOT NULL,
  dial       TEXT NOT NULL DEFAULT '',
  material   TEXT NOT NULL DEFAULT '',
  version    INTEGER NOT NULL DEFAULT 1,
  updated_at INTEGER NOT NULL
);

CREATE TABLE catalogue_aliases (
  alias      TEXT NOT NULL,
  ref        TEXT NOT NULL REFERENCES catalogue_references(ref),
  kind       TEXT NOT NULL,          -- nickname | typo | market_name
  added_by   TEXT NOT NULL DEFAULT 'seed',
  created_at INTEGER NOT NULL,
  PRIMARY KEY (alias, ref)
);

-- ---- Raw documents: immutable original bytes (G3). ----
CREATE TABLE raw_documents (
  id           INTEGER PRIMARY KEY,
  source_id    TEXT NOT NULL REFERENCES sources(id),
  url          TEXT NOT NULL,
  fetched_at   INTEGER NOT NULL,
  content_hash TEXT NOT NULL UNIQUE,   -- sha256(body); dedup = change detection
  content_type TEXT NOT NULL,
  body         BLOB NOT NULL
);

-- ---- Observation ledger: append-only price events (G2). ----
CREATE TABLE observations (
  id            INTEGER PRIMARY KEY,
  source_id     TEXT NOT NULL REFERENCES sources(id),
  kind          TEXT NOT NULL DEFAULT 'ask'
                CHECK (kind IN ('ask','sold','auction_realised','user_reported','delist')),
  brand         TEXT NOT NULL,
  model         TEXT NOT NULL,
  dial          TEXT NOT NULL DEFAULT '',
  material      TEXT NOT NULL DEFAULT '',
  scope         TEXT NOT NULL DEFAULT '',
  ref           TEXT NOT NULL,
  resolution_confidence REAL NOT NULL,  -- resolver cascade output
  resolution_rung       INTEGER NOT NULL,
  title         TEXT NOT NULL DEFAULT '',
  url           TEXT NOT NULL DEFAULT '',
  raw_doc_id    INTEGER REFERENCES raw_documents(id),  -- provenance chain (G3)
  price         TEXT NOT NULL,          -- decimal string, source currency (G7)
  currency      TEXT NOT NULL,
  price_usd     TEXT NOT NULL,          -- decimal string
  observed_at   INTEGER NOT NULL,
  content_hash  TEXT NOT NULL UNIQUE,   -- identity of the event
  created_at    INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
CREATE INDEX idx_obs_cell     ON observations(brand, model, dial, material, scope);
CREATE INDEX idx_obs_ref      ON observations(ref);
CREATE INDEX idx_obs_observed ON observations(observed_at);

-- ---- Rulesets: versioned statistical configuration (G4). ----
CREATE TABLE rulesets (
  id         INTEGER PRIMARY KEY,
  version    TEXT UNIQUE NOT NULL,
  hash       TEXT NOT NULL,
  definition TEXT NOT NULL,            -- engine.Ruleset JSON
  created_at INTEGER NOT NULL
);

-- ---- Verdicts: content-addressed results (G4, G8). ----
CREATE TABLE verdict_content (
  inputs_hash  TEXT NOT NULL,
  ruleset_hash TEXT NOT NULL,
  cell_key     TEXT NOT NULL,
  result       TEXT NOT NULL,          -- engine.Verdict JSON
  created_at   INTEGER NOT NULL,
  PRIMARY KEY (inputs_hash, ruleset_hash)
);
CREATE INDEX idx_vc_cell ON verdict_content(cell_key);
