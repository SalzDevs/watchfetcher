-- 0005_seed_corridors.sql — launch corridors (PLAN.md §11: one canonical page
-- per corridor). Rates are seed values with sources; verified_at starts now —
-- the 180-day staleness wall applies from here.
-- Duty 4.5% = EU TARIC HS 9102.21 (wrist-watches); UK/US rates per national
-- tariff pages. Each rule names its source. Verify before relying on a quote.
INSERT OR IGNORE INTO tax_rules
  (from_country, to_country, hs_code, duty_rate, vat_rate, vat_basis, insurance_pct, basis, source_url, verified_at, verified_by, ruleset_version)
VALUES
  ('JP','PT','9102.21','4.5','23','cif_plus_duty','0.5','EU TARIC import duty 4.5% (HS 9102.21) + Portugal import VAT 23% on CIF+duty','https://ec.europa.eu/taxation_customs/dds2/taric/', strftime('%s','now'), 'seed', 'v1.0.0'),
  ('JP','DE','9102.21','4.5','19','cif_plus_duty','0.5','EU TARIC import duty 4.5% (HS 9102.21) + Germany import VAT 19% on CIF+duty','https://ec.europa.eu/taxation_customs/dds2/taric/', strftime('%s','now'), 'seed', 'v1.0.0'),
  ('JP','UK','9102.21','0','20','cif_plus_duty','0.5','UK import duty 0% for used wrist-watches under preference rules (verify origin) + UK import VAT 20% on CIF','https://www.gov.uk/guidance/check-uk-import-duty-rates', strftime('%s','now'), 'seed', 'v1.0.0'),
  ('US','UK','9102.21','0','20','cif_plus_duty','0.5','UK import duty 0% (verify origin) + UK import VAT 20% on CIF','https://www.gov.uk/guidance/check-uk-import-duty-rates', strftime('%s','now'), 'seed', 'v1.0.0'),
  ('CH','DE','9102.21','4.5','19','cif_plus_duty','0.5','EU TARIC import duty 4.5% (HS 9102.21) + Germany import VAT 19% on CIF+duty','https://ec.europa.eu/taxation_customs/dds2/taric/', strftime('%s','now'), 'seed', 'v1.0.0'),
  ('CH','FR','9102.21','4.5','20','cif_plus_duty','0.5','EU TARIC import duty 4.5% (HS 9102.21) + France import VAT 20% on CIF+duty','https://ec.europa.eu/taxation_customs/dds2/taric/', strftime('%s','now'), 'seed', 'v1.0.0'),
  ('HK','DE','9102.21','4.5','19','cif_plus_duty','0.5','EU TARIC import duty 4.5% (HS 9102.21) + Germany import VAT 19% on CIF+duty','https://ec.europa.eu/taxation_customs/dds2/taric/', strftime('%s','now'), 'seed', 'v1.0.0'),
  ('US','PT','9102.21','4.5','23','cif_plus_duty','0.5','EU TARIC import duty 4.5% (HS 9102.21) + Portugal import VAT 23% on CIF+duty','https://ec.europa.eu/taxation_customs/dds2/taric/', strftime('%s','now'), 'seed', 'v1.0.0'),
  ('JP','US','9102.21','5','0','none','0.5','US HTS 9102.21 duty 5%; no import VAT (state sales tax is out of scope — see warning)','https://hts.usitc.gov', strftime('%s','now'), 'seed', 'v1.0.0'),
  ('CH','IT','9102.21','4.5','22','cif_plus_duty','0.5','EU TARIC import duty 4.5% (HS 9102.21) + Italy import VAT 22% on CIF+duty','https://ec.europa.eu/taxation_customs/dds2/taric/', strftime('%s','now'), 'seed', 'v1.0.0');

-- FX reference rates, units per EUR (ECB-style reference; replace with the
-- live ECB fetcher in a later commit — the engine pins fx_date either way).
INSERT OR IGNORE INTO fx_rates (date, base, rates)
VALUES ('2026-09-01', 'EUR', '{"USD":1.09,"GBP":0.85,"JPY":157.0,"CHF":0.94,"HKD":8.51,"EUR":1.0}');
