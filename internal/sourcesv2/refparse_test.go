package sourcesv2

import "testing"

func TestExtractRefCandidates(t *testing.T) {
	text := "Rolex Submariner Date Ref. 126610LN, alongside a Tudor Black Bay 58 79030N and an Omega Speedmaster 310.30.42.50.01.001"
	got := ExtractRefCandidates(text)
	want := []string{"310.30.42.50.01.001", "126610LN", "79030N"}
	if len(got) != len(want) {
		t.Fatalf("want %v got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate %d: want %s got %s", i, want[i], got[i])
		}
	}
	// Patek form
	got = ExtractRefCandidates("Patek Philippe Nautilus 5711/1A-010")
	if len(got) != 1 || got[0] != "5711/1A-010" {
		t.Fatalf("patek: got %v", got)
	}
	// none
	if got := ExtractRefCandidates("A very nice painting"); len(got) != 0 {
		t.Fatalf("painting produced refs: %v", got)
	}
}
