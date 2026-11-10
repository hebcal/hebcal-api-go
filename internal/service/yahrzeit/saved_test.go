package yahrzeit

import (
	"strings"
	"testing"
	"time"
)

// A row as MySQL returns it: the JSON type sorts an object's keys by length,
// then bytewise. Public figures only.
const herzlRow = `{"i": "off", "em": "nobody@example.org", "n1": "Theodor Herzl", "n2": "Ze'ev Jabotinsky", "n3": "Levi Eshkol", "n4": "David Ben-Gurion", "n5": "Golda Meir", "n6": "Yitzhak Rabin", "s1": 0, "s2": 1, "s3": 0, "s4": 0, "s5": 0, "s6": 1, "t1": "b", "t2": "y", "t3": "y", "t4": "y", "t5": "y", "t6": "y", "x1": "1860-05-02", "x2": "1940-08-03", "x3": "1969-02-26", "x4": "1973-12-01", "x5": "1978-12-08", "x6": "1995-11-04", "seq": 1, "tzo": "480", "years": 20, "yizkor": "on", "hebdate": "on"}`

const herzlID = "01m73gh503ng2gfkfbyt3t0a38"

func TestNewSavedRequest(t *testing.T) {
	updated := time.Date(2026, 11, 10, 19, 56, 32, 0, time.UTC)
	req, err := NewSavedRequest("/v3/"+herzlID+"/x.ics", "yrem=0&years=5", herzlID, []byte(herzlRow), updated)
	if err != nil {
		t.Fatal(err)
	}
	q := req.Query
	for key, want := range map[string]string{
		"y1": "1860", "m1": "5", "d1": "2", "x1": "",
		"y6": "1995", "m6": "11", "d6": "4",
		"t1": "Birthday", "t2": "Yahrzeit",
		"s1": "off", "s2": "on",
		"seq": "1", "tzo": "480",
		"years": "5", // the URL overrides the row
		"yrem":  "0",
		"v":     "yahrzeit",
		"ulid":  herzlID,
	} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	var names []string
	for _, k := range q.Keys() {
		if isNumKey(k, 'n') {
			names = append(names, q.Get(k))
		}
	}
	if got := strings.Join(names, ","); got != "Theodor Herzl,Ze'ev Jabotinsky,Levi Eshkol,David Ben-Gurion,Golda Meir,Yitzhak Rabin" {
		t.Errorf("name order %q", got)
	}
	if got := calendarTitle(q, 64); got != "Hebrew Anniversaries: Theodor Herzl, Ze'ev Jabotinsky, Levi E..." {
		t.Errorf("title %q", got)
	}
	if req.ID != herzlID || req.Version == "" {
		t.Errorf("ID %q Version %q", req.ID, req.Version)
	}
	// The URL cannot rename the calendar.
	req, _ = NewSavedRequest("/v3/"+herzlID+"/x.ics", "ulid=other", herzlID, []byte(herzlRow), updated)
	if req.Query.Get("ulid") != herzlID {
		t.Error("URL overrode ulid")
	}
}

func TestNewSavedRequestVersion(t *testing.T) {
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	a, _ := NewSavedRequest("/v3/x", "", herzlID, []byte(herzlRow), t1)
	b, _ := NewSavedRequest("/v3/x", "", herzlID, []byte(herzlRow), t1.Add(time.Second))
	c, _ := NewSavedRequest("/v3/x", "", herzlID, []byte(strings.Replace(herzlRow, "Golda", "Goldie", 1)), t1)
	if a.Version == b.Version || a.Version == c.Version {
		t.Errorf("versions should differ: %q %q %q", a.Version, b.Version, c.Version)
	}
}

// Ten or more entries: MySQL puts n10 after n9, which is the order the title
// must list them in. Gaps in the numbering still get a type.
func TestQueryFromJSONOrderAndGaps(t *testing.T) {
	row := `{"n1": "A", "n2": "B", "n3": "C", "n4": "D", "n5": "E", "n6": "F", "n7": "G", "n8": "H", "n9": "I", "n10": "J", "n12": "L", "x1": "2000-01-01", "x2": "2000-01-01", "x3": "2000-01-01", "x4": "2000-01-01", "x5": "2000-01-01", "x6": "2000-01-01", "x7": "2000-01-01", "x8": "2000-01-01", "x9": "2000-01-01", "t12": "a", "x10": "2000-01-01", "x12": "2000-01-01", "an": "off", "years": 20}`
	req, err := NewSavedRequest("/v3/x", "", herzlID, []byte(row), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	q := req.Query
	if got := calendarTitle(q, 1000); got != "Hebrew Anniversaries: A, B, C, D, E, F, G, H, I, J, L" {
		t.Errorf("title %q", got)
	}
	if q.Get("t11") != "Yahrzeit" || q.Get("t12") != "Anniversary" {
		t.Errorf("t11 %q t12 %q", q.Get("t11"), q.Get("t12"))
	}
	if ids := entryIDs(q); len(ids) != 11 {
		t.Errorf("entryIDs %v", ids)
	}
}

func TestQueryFromJSONValues(t *testing.T) {
	q, err := queryFromJSON([]byte(`{"a": null, "b": {"c": 1}, "d": ["x", "y"], "e": true, "f": 1.50, "s1": true, "s2": false}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(q.Keys(), ","); got != "d,e,f,s1,s2" {
		t.Errorf("keys %q", got)
	}
	for k, want := range map[string]string{"d": "x", "e": "true", "f": "1.5", "s1": "on", "s2": "off"} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
	for _, bad := range []string{``, `[]`, `"x"`, `{"a":`} {
		if _, err := queryFromJSON([]byte(bad)); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestSavedID(t *testing.T) {
	for path, want := range map[string]string{
		"/v3/" + herzlID + "/x.ics":            herzlID,
		"/v3/" + herzlID + ".ics":              herzlID,
		"/v3/01M73GH503NG2GFKFBYT3T0A38/x.ics": "01M73GH503NG2GFKFBYT3T0A38",
		"/v3/short/x.ics":                      "",
		"/v3/01m73gh503ng2gfkfbyt3t0a3%/x.ics": "",
		"/v4/" + herzlID + "/x.ics":            "",
	} {
		id, ok := SavedID(path)
		if id != want || ok != (want != "") {
			t.Errorf("SavedID(%q) = %q, %v", path, id, ok)
		}
	}
}
