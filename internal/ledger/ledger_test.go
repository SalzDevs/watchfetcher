package ledger

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"watchledger/internal/engine"
	"watchledger/internal/store"
)

func TestVerdictRoundtrip(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := SaveRuleset(db, engine.Current); err != nil {
		t.Fatal(err)
	}

	// register the sources (rights: fixtures are synthetic test data)
	for _, src := range []string{"auction_a", "auction_b", "chrono24"} {
		if _, err := db.Exec(`INSERT OR IGNORE INTO sources (id, name, access_status, rights_basis, rights_reviewed_at, reviewer, enabled)
			VALUES (?, ?, 'approved', 'test-fixture', strftime('%s','now'), 'test', 1)`, src, src); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	// 12 comps, 3 sources, fresh → gate-clean
	for i := 0; i < 12; i++ {
		src := []string{"auction_a", "auction_b", "chrono24"}[i%3]
		_, err := AppendObservation(db, Observation{
			SourceID: src, Kind: "auction_realised",
			Brand: "Rolex", Model: "Submariner Date", Ref: "126610LN", Dial: "black", Material: "steel",
			Title: "Submariner test comp", URL: fmt.Sprintf("https://example.com/lot/%d", i),
			Price: fmt.Sprintf("%d", 13800+i*100), Currency: "USD",
			PriceUSD:   fmt.Sprintf("%d", 13800+i*100),
			ObservedAt: now.Add(-time.Duration(5+i*7) * 24 * time.Hour),
		})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	obs, err := LoadCellObservations(db, "rolex|submariner date|black|steel|")
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 12 {
		t.Fatalf("want 12 observations, got %d", len(obs))
	}
	v := engine.ComputeVerdict("Rolex", "Submariner Date", "black", "steel", "", obs, now)
	if v == nil {
		t.Fatal("expected verdict")
	}
	if err := SaveVerdictContent(db, v); err != nil {
		t.Fatal(err)
	}

	raw, err := LoadLatestVerdictContent(db, v.CellKey, engine.Current.Hash())
	if err != nil || raw == "" {
		t.Fatalf("roundtrip load failed: %v", err)
	}
	var back engine.Verdict
	if err := json.Unmarshal([]byte(raw), &back); err != nil {
		t.Fatal(err)
	}
	if back.InputsHash != v.InputsHash || back.RulesetHash != v.RulesetHash || back.Count != v.Count {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", back, v)
	}
}
