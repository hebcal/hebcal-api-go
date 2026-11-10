package yahrzeit

import (
	"os"
	"strings"
	"testing"
)

// BenchmarkICS renders whole feeds, parsing included: the shapes production
// serves most (a few English names, twenty years), and a Hebrew one, whose
// every line is multi-octet and so takes the slow path through line folding.
func BenchmarkICS(b *testing.B) {
	raw, err := os.ReadFile("testdata/cases.tsv")
	if err != nil {
		b.Fatal(err)
	}
	urls := map[string]string{}
	for _, ln := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		f := strings.Split(ln, "\t")
		urls[f[0]] = f[2]
	}
	for _, name := range []string{"v2-three-yahrzeits", "v2-person1", "hebrew-months", "export-ampersands"} {
		url := urls[name]
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				req, _ := ParseRequest(url)
				cal, err := New(req, today2026)
				if err != nil {
					b.Fatal(err)
				}
				res, err := cal.ICS("", nil, goldenStamp)
				if err != nil {
					b.Fatal(err)
				}
				b.SetBytes(int64(len(res.Body)))
			}
		})
	}
}
