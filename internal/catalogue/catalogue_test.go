package catalogue

import (
	"path/filepath"
	"testing"

	"watchledger/internal/store"
)

func TestResolverCascade(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Rung 2: exact ref
	r, err := Lookup(db, " 126710blro ")
	if err != nil || !ok(r) {
		t.Fatalf("exact lookup failed: %+v err=%v", r, err)
	}
	if r.Ref != "126710BLRO" || r.Brand != "Rolex" || r.Dial != "pepsi" || r.Material != "steel" {
		t.Fatalf("wrong resolution: %+v", r)
	}
	if r.Rung != RungExact || r.Confidence != ConfExact {
		t.Fatalf("wrong rung/confidence: %+v", r)
	}

	// Rung 2 prefix, unique base: '5164A' → '5164A-001'
	r, err = Lookup(db, "5164A")
	if err != nil || !ok(r) || r.Ref != "5164A-001" {
		t.Fatalf("unique prefix failed: %+v err=%v", r, err)
	}

	// Ambiguous prefix: '5711' maps to blue steel AND brown gold → refuse
	r, err = Lookup(db, "5711")
	if err != nil || ok(r) {
		t.Fatalf("ambiguous base must not resolve: %+v err=%v", r, err)
	}

	// Rung 3: alias
	r, err = Lookup(db, "batman")
	if err != nil || !ok(r) || r.Ref != "126710BLNR" || r.Rung != RungAlias || r.Confidence != ConfAlias {
		t.Fatalf("alias failed: %+v err=%v", r, err)
	}

	// Miss → review queue, never a guess
	r, err = Lookup(db, "999999XYZ")
	if err != nil || !r.NeedsReview {
		t.Fatalf("miss must need review: %+v err=%v", r, err)
	}
}

func ok(r Resolution) bool { return !r.NeedsReview && r.Ref != "" }
