-- 0007_ebay.sql — Phase 3: eBay (official API, PLAN.md §4 Tier 2).
-- First source enabled under the G6 constraint with a real rights basis.
INSERT OR IGNORE INTO sources
  (id, name, access_status, rights_basis, rights_reviewed_at, reviewer, cadence_hours, enabled)
VALUES
  ('ebay', 'eBay Browse API', 'approved',
   'official_api_tos: eBay API License Agreement (Browse API, client-credentials)',
   strftime('%s','now'), 'founder', 24, 1);

-- item state cache for delist detection (read-model, not ledger).
-- The ledger records events; this tracks what we have seen so a vanishing
-- listing becomes an appendable delist event (PLAN.md §6 derived signals).
CREATE TABLE ebay_items (
  item_id      TEXT PRIMARY KEY,       -- numeric eBay id (from 'v1|<id>|0')
  ref          TEXT NOT NULL,
  title        TEXT NOT NULL,
  first_seen   INTEGER NOT NULL,
  last_seen    INTEGER NOT NULL,
  last_price   TEXT NOT NULL,          -- decimal string, USD
  delisted_at  INTEGER
);
CREATE INDEX idx_ebay_items_ref ON ebay_items(ref);
