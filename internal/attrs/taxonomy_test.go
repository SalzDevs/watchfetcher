package attrs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestTaxonomyDrift(t *testing.T) {
	// Find config/models.json relative to this file (internal/attrs -> ../../config/models.json)
	candidates := []string{
		"../../config/models.json",
		"config/models.json",
		filepath.Join(findRepoRoot(t), "config/models.json"),
	}
	var data []byte
	var err error
	for _, p := range candidates {
		data, err = os.ReadFile(p)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("cannot read config/models.json: %v (tried %v)", err, candidates)
	}
	var cfg struct {
		Brands map[string][]string `json:"brands"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal models.json: %v", err)
	}
	// Compare brand sets
	if len(cfg.Brands) != len(brandModels) {
		t.Fatalf("brand count drift: config %d vs taxonomy %d", len(cfg.Brands), len(brandModels))
	}
	for brand, models := range cfg.Brands {
		taxModels, ok := brandModels[brand]
		if !ok {
			t.Fatalf("brand %q in config but not in taxonomy.go", brand)
		}
		// Sort and compare
		a := sortedCopy(models)
		b := sortedCopy(taxModels)
		if len(a) != len(b) {
			t.Fatalf("brand %q model count drift: config %d vs taxonomy %d\nconfig: %v\ntaxonomy: %v", brand, len(a), len(b), a, b)
		}
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("brand %q model drift at %d: config %q vs taxonomy %q", brand, i, a[i], b[i])
			}
		}
	}
	for brand := range brandModels {
		if _, ok := cfg.Brands[brand]; !ok {
			t.Fatalf("brand %q in taxonomy but not in config", brand)
		}
	}
}

func sortedCopy(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
}

func findRepoRoot(t *testing.T) string {
	// Walk up from attrs dir looking for go.mod
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found")
		}
		dir = parent
		if strings.Count(dir, string(filepath.Separator)) < 2 {
			break
		}
	}
	return "."
}

// Ensure new materials are present
func TestMaterialsIncludeTitaniumCeramic(t *testing.T) {
	mats := Materials()
	need := map[string]bool{"titanium": false, "ceramic": false}
	for _, m := range mats {
		if _, ok := need[m]; ok {
			need[m] = true
		}
	}
	for k, ok := range need {
		if !ok {
			t.Fatalf("Materials() missing %q, got %v", k, mats)
		}
	}
}

func TestDetectMaterialTitaniumCeramic(t *testing.T) {
	if got := DetectMaterial("Yacht-Master 42 RLX Titanium"); got != "titanium" {
		t.Fatalf("RLX Titanium should be titanium, got %q", got)
	}
	if got := DetectMaterial("Cerachrom bezel"); got != "ceramic" {
		t.Fatalf("Cerachrom should be ceramic, got %q", got)
	}
	if got := DetectMaterial("Deepsea Challenge RLX Titanium"); got != "titanium" {
		t.Fatalf("Deepsea titanium got %q", got)
	}
}

func TestDialsCount(t *testing.T) {
	dials := Dials()
	if len(dials) != 17 {
		t.Fatalf("Dials() should be 17, got %d: %v", len(dials), dials)
	}
}
