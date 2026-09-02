-- 0006_review_queue.sql — resolver rung-4/uncatalogued candidates land here.
-- Every human resolution writes an alias (PLAN.md §7.1: precision compounds).
CREATE TABLE review_queue (
  id             INTEGER PRIMARY KEY,
  source_id      TEXT NOT NULL REFERENCES sources(id),
  raw_doc_id     INTEGER REFERENCES raw_documents(id),
  candidate      TEXT NOT NULL,        -- jsonb: the lot text / parsed fields
  resolver_output TEXT,                -- jsonb: top candidates considered
  status         TEXT NOT NULL DEFAULT 'open'
                 CHECK (status IN ('open','resolved','rejected')),
  resolved_ref   TEXT,
  resolved_by    TEXT,
  resolved_at    INTEGER,
  created_at     INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

-- 0007: register Bonhams — published auction results are a public record
-- (PLAN.md §4 Tier 1). GBP sales assumed; sampled during Phase 2 hardening.
INSERT OR IGNORE INTO sources
  (id, name, access_status, rights_basis, rights_reviewed_at, reviewer, cadence_hours, enabled)
VALUES
  ('bonhams', 'Bonhams', 'approved',
   'public_record: published auction results pages', strftime('%s','now'), 'founder', 24, 1);
