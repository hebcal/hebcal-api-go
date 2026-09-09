package geodb

import "testing"

func TestAutocompleteSortNearMountainView(t *testing.T) {
	items := []Item{
		{Value: "Santiago, Chile", Population: 4837295, Latitude: -33.45694, Longitude: -70.64827},
		{Value: "San Jose, CA", Population: 945942, Latitude: 37.33939, Longitude: -121.89496},
		{Value: "San Francisco, CA", Population: 873965, Latitude: 37.77493, Longitude: -122.41942},
		{Value: "Santa Clara, CA", Population: 127647, Latitude: 37.35411, Longitude: -121.95524},
	}
	SortAutocomplete(items, &Point{Latitude: 37.3861, Longitude: -122.0839})
	nearby := map[string]bool{"San Jose, CA": true, "San Francisco, CA": true, "Santa Clara, CA": true}
	for i := 0; i < 3; i++ {
		if !nearby[items[i].Value] {
			t.Fatalf("rank %d = %q, want a Bay Area city; all=%v", i, items[i].Value, []string{items[0].Value, items[1].Value, items[2].Value, items[3].Value})
		}
	}
	if items[3].Value != "Santiago, Chile" {
		t.Fatalf("last = %q, want distant Santiago below Bay Area matches", items[3].Value)
	}
}

func TestAutocompleteSortFallsBackToPopulation(t *testing.T) {
	items := []Item{
		{Value: "San Jose, CA", Population: 945942, Latitude: 37.33939, Longitude: -121.89496},
		{Value: "Santiago, Chile", Population: 4837295, Latitude: -33.45694, Longitude: -70.64827},
	}
	SortAutocomplete(items, nil)
	if items[0].Value != "Santiago, Chile" {
		t.Fatalf("first = %q, want Santiago population fallback", items[0].Value)
	}
}

// TestZipShortName pins that zipShortName -- shared by every ZIP autocomplete
// path (exact 5-digit match, ZIP-text search, and numeric ZIP prefix) --
// keeps the ", DC" suffix for Washington, D.C., the one case where the short
// name is not simply the text before the first comma. Before zipPrefixComplete
// was changed to call this helper, the numeric-prefix path read the raw city
// column directly and returned "Washington" instead of "Washington, DC" for
// the same ZIP code that the other two paths resolve correctly.
func TestZipShortName(t *testing.T) {
	tests := []struct{ value, want string }{
		{"Washington, DC 20001", "Washington, DC"},
		{"Beverly Hills, CA 90210", "Beverly Hills"},
	}
	for _, tc := range tests {
		if got := zipShortName(tc.value); got != tc.want {
			t.Errorf("zipShortName(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}
}
