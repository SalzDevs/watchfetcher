-- 0009_accounts.sql — Phase 5 (PLAN.md §4 Tier 3, §9, §11).
-- Magic-link only: no passwords in this DB, ever. Sessions are opaque tokens.
CREATE TABLE users (
  id         INTEGER PRIMARY KEY,
  email      TEXT UNIQUE NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE TABLE login_tokens (
  token      TEXT PRIMARY KEY,          -- sha256 of the emailed token
  email      TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  used_at    INTEGER
);

CREATE TABLE sessions (
  token      TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id),
  created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
  expires_at INTEGER NOT NULL
);

CREATE TABLE watchlist (
  user_id      INTEGER NOT NULL REFERENCES users(id),
  ref          TEXT NOT NULL,
  last_alerted INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL DEFAULT (strftime('%s','now')),
  PRIMARY KEY (user_id, ref)
);

-- user-reported paid prices — the proprietary dataset (PLAN.md §4 Tier 3).
-- Review before ledger: nothing renders until verified by a human.
CREATE TABLE price_reports (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id),
  ref        TEXT NOT NULL,
  price      TEXT NOT NULL,
  currency   TEXT NOT NULL,
  paid_at    INTEGER NOT NULL,
  status     TEXT NOT NULL DEFAULT 'submitted'
             CHECK (status IN ('submitted','verified','rejected')),
  reviewed_by TEXT,
  reviewed_at INTEGER,
  created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

-- user-reported observations carry their own source (rights: reviewed human data)
INSERT OR IGNORE INTO sources
  (id, name, access_status, rights_basis, rights_reviewed_at, reviewer, enabled)
VALUES
  ('user_reported', 'User-reported paid prices', 'approved',
   'user reported, human-reviewed before ledger entry', strftime('%s','now'), 'founder', 1);
