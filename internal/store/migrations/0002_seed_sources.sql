-- 0002_seed_sources.sql — register the retired marketplace sources.
-- Historical asks data exists in the legacy snapshot (data/archive/).
-- They stay unverified + disabled forever unless real rights evidence lands.
INSERT OR IGNORE INTO sources (id, name, access_status, rights_basis, reviewer, enabled)
VALUES
  ('chrono24',       'Chrono24',            'unverified', NULL, NULL, 0),
  ('bobswatches',    "Bob's Watches",       'unverified', NULL, NULL, 0),
  ('the1916company', 'The 1916 Company',    'unverified', NULL, NULL, 0),
  ('watchfinder',    'Watchfinder',         'unverified', NULL, NULL, 0);
