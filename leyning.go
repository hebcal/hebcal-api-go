package main

// Torah readings for the /shabbat API.
//
// hebcal-go has no leyning data of its own, so readings come from the
// hebcal-web /leyning endpoint (src/leyning.js), which wraps @hebcal/leyning
// and @hebcal/triennial. This file is the HTTP client for that endpoint plus
// the formatting that turns a reading into the classic-API "leyning" object,
// a port of formatLeyningResult() in @hebcal/rest-api and of the triennial
// block in hebcal-web src/myEventsToClassicApi.js.
//
// Readings are requested with &events=on, which asks /leyning to label each
// holiday reading with the descriptions of the events that produce it and to
// include the holiday readings it otherwise suppresses on a Shabbat that also
// has a parsha (Rosh Chodesh, Shabbat Shekalim and friends). Without that,
// this service could not reproduce eventToClassicApiObject(), which looks up
// a reading per event rather than per date.
//
// Results are cached per (date, Israel) because leyning depends on nothing
// else: every city in Israel reads the same portion on a given day, as does
// every city in the Diaspora. A week's worth of dates is therefore shared by
// all the requests for that week, whatever the location.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"
)

const defaultLeyningURL = "http://localhost:8080/leyning"

// leyningCacheSize holds roughly a year of dates for both Israel and the
// Diaspora, so the working set of "this week" and "next week" always fits.
const leyningCacheSize = 400

// leyningTimeout bounds a single /leyning request. The endpoint is a local
// service and answers in single-digit milliseconds; this is a backstop.
const leyningTimeout = 3 * time.Second

// leyningAliyah is one passage: book, begin and end chapter:verse.
type leyningAliyah struct {
	K string `json:"k"`
	B string `json:"b"`
	E string `json:"e"`
}

// withBook formats a passage in full, e.g. "Numbers 28:9-28:15". Ported from
// formatAliyahWithBook() in @hebcal/leyning.
func (a leyningAliyah) withBook() string {
	return a.K + " " + a.B + "-" + a.E
}

// short formats a passage compactly, dropping a repeated chapter number from
// the end verse ("Numbers 28:9-15"). Ported from formatAliyahShort().
func (a leyningAliyah) short(showBook bool) string {
	prefix := ""
	if showBook {
		prefix = a.K + " "
	}
	if a.B == a.E {
		return prefix + a.B
	}
	end := a.E
	if c1, _, ok1 := strings.Cut(a.B, ":"); ok1 {
		if c2, v2, ok2 := strings.Cut(a.E, ":"); ok2 && c1 == c2 {
			end = v2
		}
	}
	return prefix + a.B + "-" + end
}

// haftaraParts is a Haftarah: /leyning serializes it as a single object when
// it is one passage and as an array when it spans several.
type haftaraParts []leyningAliyah

func (h *haftaraParts) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimLeft(string(b), " \t\r\n")
	if strings.HasPrefix(trimmed, "[") {
		var arr []leyningAliyah
		if err := json.Unmarshal(b, &arr); err != nil {
			return err
		}
		*h = arr
		return nil
	}
	var one leyningAliyah
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	*h = haftaraParts{one}
	return nil
}

// summary joins passages into a citation: same-book ranges with commas,
// different books with semicolons. Ported from makeSummaryFromParts().
func (h haftaraParts) summary() string {
	if len(h) == 0 {
		return ""
	}
	prev := h[0]
	s := prev.short(true)
	for _, part := range h[1:] {
		if part.K == prev.K {
			s += ", "
		} else {
			s += "; " + part.K + " "
		}
		s += part.short(false)
		prev = part
	}
	return s
}

// leyningReading is one item of the /leyning?cfg=json response. Only the
// fields the classic API renders are decoded.
type leyningReading struct {
	Date string `json:"date"`
	Name struct {
		En string `json:"en"`
	} `json:"name"`
	// Type is "shabbat" for a parsha ha-shavua reading, "holiday" for a
	// festival reading, and "weekday" for the Monday/Thursday reading.
	Type string `json:"type"`
	// Desc lists the holiday events this reading belongs to (&events=on).
	// Parsha, weekday, and Mincha readings have none.
	Desc       []string                 `json:"desc"`
	Summary    string                   `json:"summary"`
	Fullkriyah map[string]leyningAliyah `json:"fullkriyah"`
	Haftara    string                   `json:"haftara"`
	Sephardic  string                   `json:"sephardic"`
	Chabad     haftaraParts             `json:"chabad"`
	Reason     map[string]string        `json:"reason"`
	Triennial  map[string]leyningAliyah `json:"triennial"`
}

// hasDesc reports whether this reading was produced by the given event.
func (r *leyningReading) hasDesc(desc string) bool {
	for _, d := range r.Desc {
		if d == desc {
			return true
		}
	}
	return false
}

type leyningResponse struct {
	Items []*leyningReading `json:"items"`
}

// leyningKey identifies a cached day of readings.
type leyningKey struct {
	date string // YYYY-MM-DD
	il   bool
}

// leyningClient fetches and caches readings from the /leyning endpoint.
type leyningClient struct {
	url    string
	client *http.Client
	cache  *lru.Cache[leyningKey, []*leyningReading]
	group  singleflight.Group
}

func newLeyningClient(baseURL string) *leyningClient {
	// lru.New only errors on a non-positive size, a programmer error here.
	cache, err := lru.New[leyningKey, []*leyningReading](leyningCacheSize)
	if err != nil {
		panic(err)
	}
	return &leyningClient{
		url:    baseURL,
		client: &http.Client{Timeout: leyningTimeout},
		cache:  cache,
	}
}

// readings returns the readings for every Gregorian date in [start, end],
// keyed by YYYY-MM-DD. Cached days are served from memory; the rest are
// fetched in a single request spanning the missing days.
func (c *leyningClient) readings(ctx context.Context, start, end gregDate, il bool) (map[string][]*leyningReading, error) {
	dates := isoDateRange(start, end)
	out := make(map[string][]*leyningReading, len(dates))
	var missing []string
	for _, d := range dates {
		if v, ok := c.cache.Get(leyningKey{d, il}); ok {
			out[d] = v
		} else {
			missing = append(missing, d)
		}
	}
	if len(missing) == 0 {
		return out, nil
	}
	// One request covering the missing span: the /shabbat window is at most a
	// week, so fetching the gap whole beats a request per day.
	from, to := missing[0], missing[len(missing)-1]
	sfKey := from + ".." + to
	if il {
		sfKey += "i"
	}
	v, err, _ := c.group.Do(sfKey, func() (any, error) {
		return c.fetch(ctx, from, to, il)
	})
	if err != nil {
		return nil, err
	}
	for d, readings := range v.(map[string][]*leyningReading) {
		out[d] = readings
	}
	return out, nil
}

// fetch performs one GET against /leyning and returns the readings for every
// date in [from, to], adding each day (including days with no reading) to the
// cache so a repeat request for the same week is a pure cache hit.
func (c *leyningClient) fetch(ctx context.Context, from, to string, il bool) (map[string][]*leyningReading, error) {
	q := url.Values{}
	q.Set("cfg", "json")
	q.Set("start", from)
	q.Set("end", to)
	q.Set("events", "on")
	if il {
		q.Set("i", "on")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("leyning: %s returned %s", c.url, resp.Status)
	}
	var body leyningResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("leyning: %v", err)
	}
	byDate := make(map[string][]*leyningReading)
	for _, item := range body.Items {
		byDate[item.Date] = append(byDate[item.Date], item)
	}
	fromD, err1 := isoDateStringToDate(from)
	toD, err2 := isoDateStringToDate(to)
	if err1 != nil || err2 != nil {
		return byDate, nil
	}
	for _, d := range isoDateRange(fromD, toD) {
		c.cache.Add(leyningKey{d, il}, byDate[d])
	}
	return byDate, nil
}

// isoDateRange lists every YYYY-MM-DD date from start through end inclusive.
func isoDateRange(start, end gregDate) []string {
	first := time.Date(start.Year, start.Month, start.Day, 0, 0, 0, 0, time.UTC)
	last := time.Date(end.Year, end.Month, end.Day, 0, 0, 0, 0, time.UTC)
	var out []string
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format("2006-01-02"))
	}
	return out
}

// findParshaReading returns the parsha ha-shavua reading for a date, i.e. what
// getLeyningForParshaHaShavua() would return for the ParshaEvent.
func findParshaReading(readings []*leyningReading) *leyningReading {
	for _, r := range readings {
		if r.Type == "shabbat" {
			return r
		}
	}
	return nil
}

// findHolidayReading returns the reading for a holiday event, i.e. what
// getLeyningForHoliday() would return for it. Weekday and Mincha readings
// belong to no event and so are never returned.
func findHolidayReading(readings []*leyningReading, desc string) *leyningReading {
	for _, r := range readings {
		if r.hasDesc(desc) {
			return r
		}
	}
	return nil
}

// formatLeyning renders a reading as the classic-API "leyning" object. Ported
// from formatLeyningResult() in @hebcal/rest-api; the key order reproduces
// JavaScript object semantics, where integer-like keys ("1".."8") are
// enumerated first in ascending order and the rest follow insertion order.
func formatLeyning(r *leyningReading, triennial bool) orderedObj {
	nums, hasMaftir, others := splitAliyotKeys(r.Fullkriyah)
	obj := make(orderedObj, 0, len(nums)+6)
	for _, k := range nums {
		obj = append(obj, jsonKV{k, r.Fullkriyah[k].withBook()})
	}
	if r.Summary != "" {
		obj = append(obj, jsonKV{"torah", r.Summary})
	}
	if r.Haftara != "" {
		obj = append(obj, jsonKV{"haftarah", r.Haftara})
	}
	if r.Sephardic != "" {
		obj = append(obj, jsonKV{"haftarah_sephardic", r.Sephardic})
	}
	if len(r.Chabad) != 0 {
		obj = append(obj, jsonKV{"haftarah_chabad", r.Chabad.summary()})
	}
	for _, k := range others {
		obj = append(obj, jsonKV{k, r.Fullkriyah[k].withBook()})
	}
	if hasMaftir {
		obj = append(obj, jsonKV{"maftir", r.Fullkriyah["M"].withBook()})
	}
	appendLeyningReasons(obj, r.Reason)
	if triennial && len(r.Triennial) != 0 {
		obj = append(obj, jsonKV{"triennial", formatTriennial(r.Triennial)})
	}
	return obj
}

// appendLeyningReasons suffixes " | <reason>" onto the aliyot and Haftarot
// that a special Shabbat or Rosh Chodesh replaced. Ported from
// formatReasons() in @hebcal/rest-api; it edits values in place, so the key
// order set up by formatLeyning is untouched.
func appendLeyningReasons(obj orderedObj, reason map[string]string) {
	if len(reason) == 0 {
		return
	}
	suffix := func(key, why string) {
		if why == "" {
			return
		}
		for i, kv := range obj {
			if kv.Key == key {
				obj[i].Val = kv.Val.(string) + " | " + why
				return
			}
		}
	}
	for _, num := range []string{"7", "8", "M"} {
		key := num
		if num == "M" {
			key = "maftir"
		}
		suffix(key, reason[num])
	}
	suffix("haftarah", reason["haftara"])
	suffix("haftarah_sephardic", reason["sephardic"])
	suffix("haftarah_chabad", reason["chabad"])
}

// formatTriennial renders the triennial aliyot, matching the block in
// hebcal-web src/myEventsToClassicApi.js.
func formatTriennial(aliyot map[string]leyningAliyah) orderedObj {
	nums, hasMaftir, others := splitAliyotKeys(aliyot)
	obj := make(orderedObj, 0, len(nums)+1)
	for _, k := range nums {
		obj = append(obj, jsonKV{k, aliyot[k].withBook()})
	}
	for _, k := range others {
		obj = append(obj, jsonKV{k, aliyot[k].withBook()})
	}
	if hasMaftir {
		obj = append(obj, jsonKV{"maftir", aliyot["M"].withBook()})
	}
	return obj
}

// splitAliyotKeys separates an aliyot map into the integer-like keys (sorted
// ascending, the order JavaScript enumerates them in), whether a maftir is
// present, and any remaining keys (sorted, for a stable response).
func splitAliyotKeys(aliyot map[string]leyningAliyah) (nums []string, hasMaftir bool, others []string) {
	for k := range aliyot {
		switch {
		case k == "M":
			hasMaftir = true
		case isDigits(k):
			nums = append(nums, k)
		default:
			others = append(others, k)
		}
	}
	sort.Slice(nums, func(i, j int) bool {
		a, _ := strconv.Atoi(nums[i])
		b, _ := strconv.Atoi(nums[j])
		return a < b
	})
	sort.Strings(others)
	return nums, hasMaftir, others
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
