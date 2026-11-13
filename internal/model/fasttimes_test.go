package model

import (
	"reflect"
	"testing"

	"github.com/hebcal/hebcal-go/hebcal"
)

func TestSetFastTimes(t *testing.T) {
	cases := []struct {
		name  string
		query map[string]string
		want  hebcal.Options
	}{
		{"none", nil, hebcal.Options{}},
		{"all degrees", map[string]string{"fsd": "19.8", "fed": "8.5", "tbed": "6.45"},
			hebcal.Options{FastStartDeg: 19.8, FastEndDeg: 8.5, TishaBavEndDeg: 6.45}},
		{"all minutes", map[string]string{"fsm": "72", "fem": "45", "tbem": "50"},
			hebcal.Options{FastStartMins: 72, FastEndMins: 45, TishaBavEndMins: 50}},
		{"negative is positive", map[string]string{"fsd": "-16.1", "fem": "-20"},
			hebcal.Options{FastStartDeg: 16.1, FastEndMins: 20}},
		{"degrees win over minutes", map[string]string{"fsd": "19.8", "fsm": "90"},
			hebcal.Options{FastStartDeg: 19.8}},
		{"invalid degrees let minutes through", map[string]string{"fsd": "90", "fsm": "90"},
			hebcal.Options{FastStartMins: 90}},
		{"out of range", map[string]string{"fsd": "0", "fed": "95", "fem": "241", "tbem": "0"},
			hebcal.Options{}},
		{"unparsable", map[string]string{"fsd": "abc", "fsm": "x", "tbed": ""}, hebcal.Options{}},
		{"parseInt prefix", map[string]string{"fem": "45min", "tbem": "7.9"},
			hebcal.Options{FastEndMins: 45, TishaBavEndMins: 7}},
		{"upper bounds", map[string]string{"fed": "89.9", "tbem": "240"},
			hebcal.Options{FastEndDeg: 89.9, TishaBavEndMins: 240}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got hebcal.Options
			SetFastTimes(&got, func(k string) string { return c.query[k] })
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
