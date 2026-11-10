package yahrzeit

import (
	"encoding/base64"
	"reflect"
	"testing"
)

func TestParseRequest(t *testing.T) {
	v2 := func(qs string) string {
		return "/v2/y/" + base64.RawStdEncoding.EncodeToString([]byte(qs)) + "/yahrzeit_20200429194651.ics"
	}
	tests := []struct {
		name, uri string
		path      string
		want      map[string]string
		yahrzeit  bool
	}{
		{"v2", v2("d1=27&n1=Ploni%20Almoni&v=yahrzeit&y1=2020"), "/export/yahrzeit_20200429194651.ics",
			map[string]string{"d1": "27", "n1": "Ploni Almoni", "v": "yahrzeit", "y1": "2020"}, true},
		{"v2 ignores the real query string", v2("d1=27&v=yahrzeit") + "?v=1&d1=3", "/export/yahrzeit_20200429194651.ics",
			map[string]string{"d1": "27", "v": "yahrzeit"}, true},
		{"v2 hebrew", v2("n1=%D7%92%D7%95%D7%9C%D7%93%D7%94&v=yahrzeit"), "/export/yahrzeit_20200429194651.ics",
			map[string]string{"n1": "גולדה", "v": "yahrzeit"}, true},
		{"v2 holiday calendar", "/v2/h/" + base64.RawStdEncoding.EncodeToString([]byte("v=1&year=2026")) + "/hebcal.ics", "/export/hebcal.ics",
			map[string]string{"v": "1", "year": "2026"}, false},
		{"v2 without a file name", "/v2/y/" + base64.RawStdEncoding.EncodeToString([]byte("v=yahrzeit")), "/export/hebcal.ics",
			map[string]string{"v": "yahrzeit"}, true},
		{"v2 url-safe alphabet with padding", "/v2/y/" + base64.URLEncoding.EncodeToString([]byte("n1=a?>b&v=yahrzeit")) + "/x.ics", "/export/x.ics",
			map[string]string{"n1": "a?>b", "v": "yahrzeit"}, true},
		{"ampersands", "/export/a5/x.ics?redir=1&subscribe=1&t1=Yahrzeit&n1=Golda&v=yahrzeit", "/export/a5/x.ics",
			map[string]string{"redir": "1", "subscribe": "1", "t1": "Yahrzeit", "n1": "Golda", "v": "yahrzeit"}, true},
		{"semicolons", "/yahrzeit/yahrzeit.cgi/y.ics?dl=1;t1=Yahrzeit;n1=Grandma+Sarah;v=yahrzeit", "/yahrzeit/yahrzeit.cgi/y.ics",
			map[string]string{"dl": "1", "t1": "Yahrzeit", "n1": "Grandma Sarah", "v": "yahrzeit"}, true},
		{"double-encoded query", "/export/x.ics?subscribe=1%3Bn1%3DPloni%2520Almoni%3Bv%3Dyahrzeit", "/export/x.ics",
			map[string]string{"subscribe": "1", "n1": "Ploni Almoni", "v": "yahrzeit"}, true},
		{"query encoded into the path", "/export/74/x.ics%3Fsubscribe%3D1%3Bn1%3DPloni%2520Almoni%3Bv%3Dyahrzeit", "/export/export.ics",
			map[string]string{"subscribe": "1", "n1": "Ploni Almoni", "v": "yahrzeit"}, true},
		{"double-encoded latin-1", "/export/x.ics?dl=1%3Bn1%3DRen%E9e%3Bv%3Dyahrzeit", "/export/x.ics",
			map[string]string{"dl": "1", "n1": "Renée", "v": "yahrzeit"}, true},
		{"no v but a first date", "/export/x.ics?y1=2003&m1=12&d1=23", "/export/x.ics",
			map[string]string{"y1": "2003", "m1": "12", "d1": "23", "v": "yahrzeit"}, true},
		{"no v on the cgi path", "/yahrzeit/yahrzeit.cgi/x.ics?y1=2003&m1=12&d1=23", "/yahrzeit/yahrzeit.cgi/x.ics",
			map[string]string{"y1": "2003", "m1": "12", "d1": "23"}, false},
		{"holiday export", "/export/x.ics?v=1&year=2026&y1=1&m1=1&d1=1", "/export/x.ics",
			map[string]string{"v": "1", "year": "2026", "y1": "1", "m1": "1", "d1": "1"}, false},
		{"first value wins", "/export/x.ics?v=yahrzeit&n1=A&n1=B", "/export/x.ics",
			map[string]string{"v": "yahrzeit", "n1": "A"}, true},
		{"malformed escapes survive", "/export/x.ics?v=yahrzeit&n1=100%&n2=%zz&n3=%FF", "/export/x.ics",
			map[string]string{"v": "yahrzeit", "n1": "100%", "n2": "%zz", "n3": "\uFFFD"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, ok := ParseRequest(tt.uri)
			if !ok {
				t.Fatal("not recognized")
			}
			if req.Path != tt.path {
				t.Errorf("path %q, want %q", req.Path, tt.path)
			}
			if !reflect.DeepEqual(req.Query.vals, tt.want) {
				t.Errorf("query %v, want %v", req.Query.vals, tt.want)
			}
			if req.IsYahrzeit() != tt.yahrzeit {
				t.Errorf("IsYahrzeit = %v", req.IsYahrzeit())
			}
		})
	}
	for _, uri := range []string{"/v4/abc/x.ics", "/ical/jewish-holidays.ics", "/v2", "/", "/v3/01ABC/x.ics"} {
		if _, ok := ParseRequest(uri); ok {
			t.Errorf("%s should not be recognized", uri)
		}
	}
}

func TestQueryKeepsOrder(t *testing.T) {
	q := ParseQuery("n2=B&v=yahrzeit&n1=A&n2=C&&=x&novalue")
	if got, want := q.Keys(), []string{"n2", "v", "n1", "", "novalue"}; !reflect.DeepEqual(got, want) {
		t.Errorf("keys %q, want %q", got, want)
	}
}

func TestCalendarTitle(t *testing.T) {
	tests := []struct{ qs, want string }{
		{"", "Yahrzeit"},
		{"n1=Ploni+Almoni", "Yahrzeit: Ploni Almoni"},
		{"n2=B&n1=A", "Yahrzeit: B, A"},
		{"n1=A&t1=Birthday", "Hebrew Birthday: A"},
		{"n1=A&t1=Anniversary", "Hebrew Anniversary: A"},
		{"n1=A&t1=Other", "Hebrew Anniversary: A"},
		{"n1=A&t1=Birthday&n2=B&t2=Yahrzeit", "Hebrew Anniversaries: A, B"},
		{"n1=A&t1=Birthday&n2=B&t2=Birthday", "Hebrew Birthday: A, B"},
		{"n1=" + "abcdefghij" + "abcdefghij" + "abcdefghij" + "abcdefghij" + "abcdefghij" + "abcdefghij",
			"Yahrzeit: abcdefghijabcdefghijabcdefghijabcdefghijabcdefghija..."},
		// The limit is counted in UTF-16 units: Hebrew letters are one each
		// though two octets, so this 60-unit title is left whole.
		{"n1=%D7%90%D7%91%D7%92%D7%93%D7%94%D7%95%D7%96%D7%97%D7%98%D7%99%D7%90%D7%91%D7%92%D7%93%D7%94%D7%95%D7%96%D7%97%D7%98%D7%99%D7%90%D7%91%D7%92%D7%93%D7%94%D7%95%D7%96%D7%97%D7%98%D7%99%D7%90%D7%91%D7%92%D7%93%D7%94%D7%95%D7%96%D7%97%D7%98%D7%99%D7%90%D7%91%D7%92%D7%93%D7%94%D7%95%D7%96%D7%97%D7%98%D7%99",
			"Yahrzeit: אבגדהוזחטיאבגדהוזחטיאבגדהוזחטיאבגדהוזחטיאבגדהוזחטי"},
	}
	for _, tt := range tests {
		if got := calendarTitle(ParseQuery(tt.qs), 64); got != tt.want {
			t.Errorf("%s:\n got %q\nwant %q", tt.qs, got, tt.want)
		}
	}
}
