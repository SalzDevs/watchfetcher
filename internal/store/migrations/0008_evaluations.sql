-- 0008_evaluations.sql — Phase 4: the core job (PLAN.md §9).
-- Content-addressed like everything else (G4): same inputs → same listing_hash
-- → same permalink. Evaluations snapshot their evidence so the permalink
-- re-derives byte-for-byte forever (G8) even as the live ledger moves on.
CREATE TABLE evaluations (
  id           INTEGER PRIMARY KEY,
  listing_hash TEXT NOT NULL UNIQUE,   -- sha256(canonical inputs)
  inputs_hash  TEXT NOT NULL,          -- engine.InputsHash of the realised evidence set
  result       TEXT NOT NULL,          -- full evaluate.Result JSON (incl. evidence snapshot)
  ruleset_version TEXT NOT NULL,
  created_at   INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
