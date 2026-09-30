package model

import (
	"testing"
)

func TestParseFastTimes(t *testing.T) {
	cases := []struct {
		name  string
		query map[string]string
		want  FastTimes
	}{
		{"none", nil, FastTimes{}},
		{"all degrees", map[string]string{"fsd": "19.8", "fed": "8.5", "tbed": "6.45"},
			FastTimes{StartDeg: 19.8, EndDeg: 8.5, TishaBavEndDeg: 6.45}},
		{"all minutes", map[string]string{"fsm": "72", "fem": "45", "tbem": "50"},
			FastTimes{StartMins: 72, EndMins: 45, TishaBavEndMins: 50}},
		{"negative is positive", map[string]string{"fsd": "-16.1", "fem": "-20"},
			FastTimes{StartDeg: 16.1, EndMins: 20}},
		{"degrees win over minutes", map[string]string{"fsd": "19.8", "fsm": "90"},
			FastTimes{StartDeg: 19.8}},
		{"invalid degrees let minutes through", map[string]string{"fsd": "90", "fsm": "90"},
			FastTimes{StartMins: 90}},
		{"out of range", map[string]string{"fsd": "0", "fed": "95", "fem": "241", "tbem": "0"},
			FastTimes{}},
		{"unparsable", map[string]string{"fsd": "abc", "fsm": "x", "tbed": ""}, FastTimes{}},
		{"parseInt prefix", map[string]string{"fem": "45min", "tbem": "7.9"},
			FastTimes{EndMins: 45, TishaBavEndMins: 7}},
		{"upper bounds", map[string]string{"fed": "89.9", "tbem": "240"},
			FastTimes{EndDeg: 89.9, TishaBavEndMins: 240}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseFastTimes(func(k string) string { return c.query[k] })
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
