package geodb

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
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
		"NYC":             5128581,
		"marseille":       2995469,
		"Rehovot":         293725,
		"Herzliyya":       294778,
		"Nahariyya":       294117,
		"IL-Beersheba":    295530,
		"Tel-Aviv":        293397,
		"Bat-Yam":         db.geo.Load().legacyCityID("IL-Bat Yam"),
		"Petach-Tikva":    293918,
		"Rishon-LeZion":   db.geo.Load().legacyCityID("IL-Rishon LeZion"),
		"England":         2643743,
		"dc":              4140963,
		"Zurich":          2657896,
		"Kolkata":         1275004,
		"Malta":           2562305,
		"Puerto Rico":     4568127,
		"Brooklyn":        5110302,
		"Cochin":          1273874,
		"Efrat":           283991,
		"Hebron":          285066,
		"Kiryat Shmona":   293825,
		"Tzfat":           293100,
		"IL-Tzfat":        293100,
		"Safed":           293100,
		"IL-Kiryat Gat":   293842,
		"IL-Netivot":      294068,
		"IL-Ramle":        293768,
		"IL-Rosh HaAyin":  293690,
		"IL-Binyamina":    295410,
		"IL-Beit_Shemesh": 295432,
		// bare names and CC-prefixed aliases once resolved only through
		// @hebcal/cities' Location.lookup() registrations
		"Athens":            264371,
		"San Diego":         5391811,
		"Tokyo":             1850147,
		"Ramat Gan":         293788,
		"Teaneck":           5105262,
		"Marseilles":        2995469,
		"US-Boston":         4930956,
		"US-Hawaii":         5856195,
		"US-New Jersey":     5101798,
		"GB-United Kingdom": 2643743,
	} {
		if got := db.geo.Load().legacyCityID(name); got != want || got == 0 {
			t.Errorf("legacy city %q = %d, want %d", name, got, want)
		}
	}
}

// A geonames.sqlite3 built before legacy_city existed is refused at startup,
// and on a reload the working generation keeps serving.
func TestLegacyCityTableRequired(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.sqlite3")
	copyFile(t, testGeonames, old)
	w, err := sql.Open("sqlite3", old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Exec("DROP TABLE legacy_city"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	if db, err := New(testZips, old); err == nil {
		db.Close()
		t.Fatal("New accepted a geonames database without legacy_city")
	} else if !strings.Contains(err.Error(), "legacy_city") {
		t.Errorf("New error %q does not name legacy_city", err)
	}

	path := filepath.Join(dir, "geonames.sqlite3")
	copyFile(t, testGeonames, path)
	db, err := New(testZips, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := os.Rename(old, path); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ReloadGeonames(); ok || err == nil {
		t.Fatalf("ReloadGeonames() = %v, %v; want false and an error", ok, err)
	}
	if loc := db.LookupLegacyCity("Tel Aviv"); loc == nil || loc.GeonameID != 293397 {
		t.Errorf("after refused reload: LookupLegacyCity(Tel Aviv) = %+v", loc)
	}
}
