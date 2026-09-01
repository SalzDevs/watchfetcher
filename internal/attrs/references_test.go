package attrs

import "testing"

func TestLookupRefExact(t *testing.T) {
	// Case-insensitive + trim: listings may carry lowercase refs.
	r, ok := LookupRef("  126710blro ")
	if !ok {
		t.Fatal("expected 126710BLRO to resolve")
	}
	if r.Brand != "Rolex" || r.Model != "GMT-Master II" || r.Dial != "pepsi" || r.Material != "steel" {
		t.Fatalf("wrong config: %+v", r)
	}
	if r.Ref != "126710BLRO" {
		t.Fatalf("ref not normalized: %q", r.Ref)
	}
}

func TestLookupRefExactOmega(t *testing.T) {
	r, ok := LookupRef("210.30.42.20.03.001")
	if !ok {
		t.Fatal("expected Omega ref to resolve")
	}
	if r.Dial != "blue" || r.Material != "steel" || r.Model != "Seamaster Diver 300M" {
		t.Fatalf("wrong config: %+v", r)
	}
}

func TestLookupRefUniquePrefix(t *testing.T) {
	// Marketplaces truncate "5167A-001" to "5167A" (dash-split) — unique base.
	r, ok := LookupRef("5167A")
	if !ok {
		t.Fatal("expected unique base 5167A to resolve")
	}
	if r.Model != "Aquanaut" || r.Dial != "black" || r.Material != "steel" {
		t.Fatalf("wrong config: %+v", r)
	}
}

func TestLookupRefAmbiguousPrefix(t *testing.T) {
	// "5711" maps to 5711/1A-010 (blue steel) AND 5711/1R-001 (brown gold):
	// ambiguous base must match nothing, never guess.
	if _, ok := LookupRef("5711"); ok {
		t.Fatal("ambiguous base 5711 must not resolve")
	}
	// But the full ref resolves.
	r, ok := LookupRef("5711/1A-010")
	if !ok || r.Dial != "blue" || r.Material != "steel" {
		t.Fatalf("full ref must resolve: %+v ok=%v", r, ok)
	}
}

func TestLookupRefUnknown(t *testing.T) {
	for _, ref := range []string{"", "   ", "999999", "126610XX", "not-a-ref"} {
		if _, ok := LookupRef(ref); ok {
			t.Fatalf("expected %q to not resolve", ref)
		}
	}
}

func TestReferencesForModel(t *testing.T) {
	refs := ReferencesForModel("rolex", "Submariner Date")
	if len(refs) != 7 {
		t.Fatalf("expected 7 Submariner Date refs, got %d", len(refs))
	}
	// Sorted by ref.
	for i := 1; i < len(refs); i++ {
		if refs[i-1].Ref >= refs[i].Ref {
			t.Fatalf("refs not sorted: %v", refs)
		}
	}
	if ReferencesForModel("Rolex", "No Such Model") != nil {
		t.Fatal("unknown model must return nil")
	}
}

func TestReferenceCount(t *testing.T) {
	if ReferenceCount() < 50 {
		t.Fatalf("expected catalogued references, got %d", ReferenceCount())
	}
}
