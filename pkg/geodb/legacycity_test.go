package geodb

import (
	"encoding/json"
	"testing"
)

// The bare names and aliases below appear as city= in crawled legacy URLs;
// each must resolve to a GeoNames id, not fall through to a 404.
func TestLegacyCityAliases(t *testing.T) {
	db, err := New(testZips, testGeonames)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for name, want := range map[string]int{
		"NYC":           5128581,
		"marseille":     2995469,
		"Rehovot":       293725,
		"Herzliyya":     294778,
		"Nahariyya":     294117,
		"IL-Beersheba":  295530,
		"Tel-Aviv":      293397,
		"Bat-Yam":       db.legacyCities[munge("IL-Bat Yam")],
		"Petach-Tikva":  293918,
		"Rishon-LeZion": db.legacyCities[munge("IL-Rishon LeZion")],
		"England":       2643743,
		"dc":            4140963,
		"Zurich":        2657896,
		"Kolkata":       1275004,
		"Malta":         2562305,
		"Puerto Rico":   4568127,
		"Brooklyn":      5110302,
		"Cochin":        1273874,
	} {
		if got := db.legacyCities[munge(name)]; got != want || got == 0 {
			t.Errorf("legacy city %q = %d, want %d", name, got, want)
		}
	}
}

// Two keys that munge alike but name different places would make the lookup
// depend on map order.
func TestLegacyCityKeysUnique(t *testing.T) {
	var raw map[string]int
	if err := json.Unmarshal(city2geonameidJSON, &raw); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]string, len(raw))
	for k, id := range raw {
		m := munge(k)
		if prev, ok := seen[m]; ok && raw[prev] != id {
			t.Errorf("%q (%d) and %q (%d) both munge to %q", prev, raw[prev], k, id, m)
		}
		seen[m] = k
	}
}
