package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "time/tzdata"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hebcal/hebcal-api-go/internal/repository/readings/readingstest"
)

// tt binds *testing.T so its text method can take a tool handler's three
// return values directly: text(tl.someTool(...)).
type tt struct{ t *testing.T }

// text extracts the single text block a hebcal tool returns, failing the test
// on an error or any other shape. The middle (structured-output) value is
// always nil for these text-only tools.
func (x tt) text(res *mcpsdk.CallToolResult, _ any, err error) string {
	x.t.Helper()
	if err != nil {
		x.t.Fatalf("handler returned error: %v", err)
	}
	if len(res.Content) == 0 {
		x.t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		x.t.Fatalf("content[0] is %T, want *TextContent", res.Content[0])
	}
	return tc.Text
}

func TestConvertGregorianToHebrew(t *testing.T) {
	tl := &tools{}
	got := tt{t}.text(tl.convertGregorianToHebrew(context.Background(), nil,
		convertGregorianToHebrewArgs{Date: "2024-01-01"}))
	for _, want := range []string{
		"Hebrew year: 5784",
		"Hebrew month: Tevet",
		"Day of Hebrew month: 20",
		"Date in Hebrew letters: ",
		"Is leap year: true",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, got)
		}
	}
}

func TestConvertGregorianToHebrewBadDate(t *testing.T) {
	tl := &tools{}
	got := tt{t}.text(tl.convertGregorianToHebrew(context.Background(), nil,
		convertGregorianToHebrewArgs{Date: "2024/01/01"}))
	if !strings.Contains(got, "Error parsing date") {
		t.Errorf("want parse error, got %q", got)
	}
}

func TestConvertHebrewToGregorian(t *testing.T) {
	tl := &tools{}
	got := tt{t}.text(tl.convertHebrewToGregorian(context.Background(), nil,
		convertHebrewToGregorianArgs{Day: 15, Month: "Shevat", Year: 5784}))
	if got != "2024-01-25" {
		t.Errorf("got %q, want 2024-01-25", got)
	}
}

func TestConvertHebrewToGregorianBadMonth(t *testing.T) {
	tl := &tools{}
	got := tt{t}.text(tl.convertHebrewToGregorian(context.Background(), nil,
		convertHebrewToGregorianArgs{Day: 15, Month: "XYZ123", Year: 5784}))
	if !strings.Contains(got, "Cannot interpret") {
		t.Errorf("want interpret error, got %q", got)
	}
}

func TestYahrzeit(t *testing.T) {
	tl := &tools{}
	got := tt{t}.text(tl.yahrzeit(context.Background(), nil,
		yahrzeitArgs{Date: "2020-03-15", AfterSunset: false}))
	rows := strings.Split(got, "\n")
	if len(rows) < 3 {
		t.Fatalf("want header plus data rows, got:\n%s", got)
	}
	if !strings.Contains(rows[0], "Anniversary number") {
		t.Errorf("missing table header: %q", rows[0])
	}
	if !strings.Contains(rows[1], "----") {
		t.Errorf("missing table separator: %q", rows[1])
	}
	// Every data row has the five-column markdown shape.
	for _, r := range rows[2:] {
		if strings.Count(r, "|") != 6 {
			t.Errorf("row is not 5 columns: %q", r)
		}
	}
}

func TestTorahPortion(t *testing.T) {
	// A fake sidecar supplies the reading name and summary.
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":{"en":"Shemot"},"summary":"Exodus 1:1-6:1"}`))
	})
	tl := &tools{rd: readingstest.Serve(t, h)}
	got := tt{t}.text(tl.torahPortion(context.Background(), nil,
		torahPortionArgs{Date: "2024-01-06", IL: false}))
	for _, want := range []string{
		"Torah portion: Parashat Shemot",
		"Name in Hebrew: ",
		"Reading: Exodus 1:1-6:1",
		"Date read: 2024-01-06",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, got)
		}
	}
}

// TestTorahPortionChag uses the holiday reading name the sidecar returns when a
// chag displaces the parsha (divergence #4): "Pesach Shabbat Chol ha-Moed",
// not hebcal-go's coarser "Pesach V (CH”M)". The chag branch prints only the
// portion and the date read.
func TestTorahPortionChag(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":{"en":"Pesach Shabbat Chol ha-Moed"},"summary":"Exodus 33:12-34:26; Numbers 28:19-25; Song of Songs 1:1-8:14"}`))
	})
	tl := &tools{rd: readingstest.Serve(t, h)}
	got := tt{t}.text(tl.torahPortion(context.Background(), nil,
		torahPortionArgs{Date: "2024-04-27", IL: false}))
	if !strings.Contains(got, "Torah portion: Pesach Shabbat Chol ha-Moed") {
		t.Errorf("chag portion should use the sidecar's name:\n%s", got)
	}
	if strings.Contains(got, "Name in Hebrew:") || strings.Contains(got, "Reading:") {
		t.Errorf("chag output should be portion + date read only:\n%s", got)
	}
	if !strings.Contains(got, "Date read: 2024-04-27") {
		t.Errorf("missing date read:\n%s", got)
	}
}

// TestTorahPortionChagNoSidecar falls back to hebcal-go's coarser chag label
// when no sidecar is wired.
func TestTorahPortionChagNoSidecar(t *testing.T) {
	tl := &tools{rd: nil}
	got := tt{t}.text(tl.torahPortion(context.Background(), nil,
		torahPortionArgs{Date: "2024-04-27", IL: false}))
	if !strings.Contains(got, "Torah portion: ") {
		t.Errorf("expected a fallback portion name:\n%s", got)
	}
	if !strings.Contains(got, "Date read: 2024-04-27") {
		t.Errorf("missing date read:\n%s", got)
	}
}

// TestTorahPortionNoSidecar omits the Reading line rather than failing when no
// readings client is wired.
func TestTorahPortionNoSidecar(t *testing.T) {
	tl := &tools{rd: nil}
	got := tt{t}.text(tl.torahPortion(context.Background(), nil,
		torahPortionArgs{Date: "2024-01-06", IL: false}))
	if strings.Contains(got, "Reading:") {
		t.Errorf("expected no Reading line without a sidecar, got:\n%s", got)
	}
	if !strings.Contains(got, "Torah portion: Parashat Shemot") {
		t.Errorf("output missing the portion line:\n%s", got)
	}
}

func TestJewishHolidaysYear(t *testing.T) {
	tl := &tools{}
	got := tt{t}.text(tl.jewishHolidaysYear(context.Background(), nil,
		jewishHolidaysYearArgs{Year: 2024}))
	for _, want := range []string{"Gregorian date", "Hebrew date", "Holiday", "Categories"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing header %q", want)
		}
	}
	// renderEn applies the smart apostrophe and the one-m Tamuz spelling.
	if !strings.Contains(got, "Ta’anit Esther") {
		t.Errorf("holiday names should carry the smart apostrophe:\n%s", got)
	}
	if !strings.Contains(got, "Rosh Chodesh Tamuz") {
		t.Errorf("Rosh Chodesh Tamuz should have one m:\n%s", got)
	}
	// ...but "Tzom Tammuz" keeps two, exactly as @hebcal/core (and hebcal-mcp)
	// render it -- FixMonthSpelling leaves it alone.
	if !strings.Contains(got, "Tzom Tammuz") {
		t.Errorf("Tzom Tammuz should keep two m's to match production:\n%s", got)
	}
}

func TestDafYomi(t *testing.T) {
	tl := &tools{}
	got := tt{t}.text(tl.dafYomi(context.Background(), nil, dafYomiArgs{Date: "2024-01-01"}))
	for _, want := range []string{
		"Daf Yomi (English): Baba Kamma 60",
		"Daf Yomi (Hebrew): ",
		"Hebrew date: 20 Tevet 5784",
		"https://www.sefaria.org/",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, got)
		}
	}
}

func TestShabbatTimes(t *testing.T) {
	tl := &tools{}
	got := tt{t}.text(tl.shabbatTimes(context.Background(), nil, shabbatTimesArgs{
		Latitude: 41.85003, Longitude: -87.65005, Tzid: "America/Chicago",
		StartDate: "2024-01-05", EndDate: "2024-01-13",
	}))
	if !strings.Contains(got, "Candle lighting") || !strings.Contains(got, "Havdalah") {
		t.Errorf("missing candle/havdalah rows:\n%s", got)
	}
	// The first candle-lighting row is pinned by the Node test.
	if !strings.Contains(got, "| 2024-01-05 | 16:15 | Candle lighting | Parashat Shemot |") {
		t.Errorf("first candle row does not match production:\n%s", got)
	}
}

func TestShabbatTimesOutOfRange(t *testing.T) {
	tl := &tools{}
	got := tt{t}.text(tl.shabbatTimes(context.Background(), nil, shabbatTimesArgs{
		Latitude: 200, Longitude: 0, Tzid: "America/Chicago",
		StartDate: "2024-01-05", EndDate: "2024-01-06",
	}))
	if !strings.Contains(got, "out of range") {
		t.Errorf("want a range error rather than a panic, got %q", got)
	}
}

// TestStatelessHandler exercises the real streamable-HTTP transport: tools/list
// over POST returns all seven tools, and a GET is refused.
func TestStatelessHandler(t *testing.T) {
	srv := httptest.NewServer(Handler(nil))
	defer srv.Close()

	body := `{"jsonrpc":"2.0","method":"tools/list","id":1}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	names := toolNames(t, resp.Body)
	want := []string{
		"convert-gregorian-to-hebrew", "convert-hebrew-to-gregorian", "yahrzeit",
		"torah-portion", "jewish-holidays-year", "daf-yomi", "shabbat-times",
	}
	if len(names) != len(want) {
		t.Fatalf("got %d tools %v, want %d", len(names), names, len(want))
	}
	for _, w := range want {
		if !names[w] {
			t.Errorf("tools/list missing %q", w)
		}
	}

	// Stateless mode answers a GET with 405.
	getResp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", getResp.StatusCode)
	}
}

// toolNames parses the SSE response of a tools/list call into a set of names.
func toolNames(t *testing.T, r io.Reader) map[string]bool {
	t.Helper()
	raw, _ := io.ReadAll(r)
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var msg struct {
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(line[6:]), &msg); err != nil {
			continue
		}
		out := make(map[string]bool)
		for _, tool := range msg.Result.Tools {
			out[tool.Name] = true
		}
		return out
	}
	t.Fatalf("no data: line in SSE response:\n%s", raw)
	return nil
}
