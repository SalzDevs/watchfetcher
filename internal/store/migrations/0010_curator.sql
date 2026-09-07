-- 0010_curator.sql — curator staging (PLAN.md §17: the founder IS the curator).
-- Paste raw auction-result lines → parsed → human-verified → ledger.
CREATE TABLE curator_results (
  id          INTEGER PRIMARY KEY,
  raw_text    TEXT NOT NULL,
  house       TEXT,                        -- 'phillips' | 'christies' | ...
  ref         TEXT,                        -- resolved ref ('' = unresolved)
  price       TEXT,                        -- total paid (hammer + premium), decimal string
  currency    TEXT,
  sale_date   TEXT,                        -- YYYY-MM-DD
  lot_url     TEXT,
  status      TEXT NOT NULL DEFAULT 'parsed'
              CHECK (status IN ('parsed','ledged','discarded','unresolved')),
  parsed      TEXT,                        -- full parse JSON (provenance)
  observation_id INTEGER,
  created_at  INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

-- curated sources: manually harvested public auction records (G6-approved)
INSERT OR IGNORE INTO sources (id, name, access_status, rights_basis, rights_reviewed_at, reviewer, enabled)
VALUES
  ('curator',       'Curated auction results', 'approved', 'manually harvested public auction records, reviewed by founder', strftime('%s','now'), 'founder', 0),
  ('phillips',      'Phillips',                'approved', 'public_record: published auction results', strftime('%s','now'), 'founder', 0),
  ('christies',     'Christie''s',             'approved', 'public_record: published auction results', strftime('%s','now'), 'founder', 0),
  ('sothebys',      'Sotheby''s',              'approved', 'public_record: published auction results', strftime('%s','now'), 'founder', 0),
  ('antiquorum',    'Antiquorum',              'approved', 'public_record: published auction results', strftime('%s','now'), 'founder', 0);
