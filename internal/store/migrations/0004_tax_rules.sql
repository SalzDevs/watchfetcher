-- 0004_tax_rules.sql — corridor duty/VAT rules (PLAN.md §8: no line item
-- without a stated basis; every rule cites source + verification date).
CREATE TABLE tax_rules (
  id             INTEGER PRIMARY KEY,
  from_country   TEXT NOT NULL,        -- ISO-3166 alpha-2
  to_country     TEXT NOT NULL,
  hs_code        TEXT NOT NULL DEFAULT '9102.21',
  duty_rate      TEXT NOT NULL,        -- decimal string, percent (G7)
  vat_rate       TEXT NOT NULL,        -- decimal string, percent ('0' = none)
  vat_basis      TEXT NOT NULL DEFAULT 'cif_plus_duty',
  insurance_pct  TEXT NOT NULL DEFAULT '0.5',
  basis          TEXT NOT NULL,
  source_url     TEXT NOT NULL,
  verified_at    INTEGER NOT NULL,
  verified_by    TEXT NOT NULL,
  ruleset_version TEXT NOT NULL,
  UNIQUE (from_country, to_country, hs_code, ruleset_version)
);

CREATE TABLE fx_rates (
  date  TEXT PRIMARY KEY,              -- 'YYYY-MM-DD' (ECB reference date)
  base  TEXT NOT NULL DEFAULT 'EUR',
  rates TEXT NOT NULL                  -- jsonb: {"USD":1.09,...} units per EUR
);
