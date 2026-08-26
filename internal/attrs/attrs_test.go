package attrs

import "testing"

func TestDetectDial(t *testing.T) {
	cases := map[string]string{
		"Rolex Submariner Black Dial":     "black",
		"Omega Seamaster Blue Dial":       "blue",
		"GMT Master II Batman Bezel":      "batman",
		"Pepsi GMT":                       "pepsi",
		"Datejust Green Diamond Dial":     "green",
		"Rolex Yellow Gold Case":          "",
		"Black Ceramic Bezel Submariner":  "black",
		"":                                "",
	}
	for text, want := range cases {
		if got := DetectDial(text); got != want {
			t.Errorf("DetectDial(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestDetectMaterial(t *testing.T) {
	cases := map[string]string{
		"Stainless Steel Submariner": "steel",
		"18k Yellow Gold Day-Date":   "gold",
		"Two Tone Rolesor Datejust":  "two_tone",
		"Platinum 950 Daytona":       "platinum",
		"Patek 5711A Nautilus":       "steel",
	}
	for text, want := range cases {
		if got := DetectMaterial(text); got != want {
			t.Errorf("DetectMaterial(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestDetectScopeNegation(t *testing.T) {
	cases := map[string]string{
		"Original box, original papers":    "full_set",
		"Original box, no original papers": "box_only",
		"No box, no papers watch only":     "naked",
	}
	for text, want := range cases {
		if got := DetectScope(text); got != want {
			t.Errorf("DetectScope(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestExtractRef(t *testing.T) {
	cases := map[string]string{
		"rolex/submariner-date-126610ln-black": "126610LN",
		"omega/seamaster-210.30.42.20.03.001":  "210.30.42.20.03.001",
		"tudor/black-bay-M79230-0002":          "M79230",
	}
	for text, want := range cases {
		if got := ExtractRef(text); got != want {
			t.Errorf("ExtractRef(%q) = %q, want %q", text, got, want)
		}
	}
}
