# CLAUDE.md

Guidance for Claude Code (claude.ai/code) working in this repository.

## What this is

`hebcal-api` is one Go binary serving what used to be several services:

- the Hebcal.com **REST APIs** — `/converter`, `/zmanim`, `/shabbat`, `/geo`,
  `/complete` — ported from [hebcal-web](https://github.com/hebcal/hebcal-web)'s
  `src/converter.js`, `src/zmanim.js` and `src/shabbat.js`;
- the **Yahrzeit iCalendar feeds** — `download.hebcal.com/v2/y/…ics` and its
  two older spellings, and the saved `/v3/<ulid>/…ics` calendars read from
  MySQL — ported from `@hebcal/icalendar` (as `pkg/icalendar`) and
  `src/yahrzeitDownload.js`; and the **zmanim feeds** -- `/zmanim`,
  `/zmanim2` and `/sunrs` -- ported from `src/zmanimDownload.js`; and the
  **holiday feeds** -- `download.hebcal.com/v4/…ics` and its two legacy
  spellings -- ported from `src/hebcal-download.js` and `src/icalCommon.js`;
- the **PDF calendars** — `download.hebcal.com/v4/…pdf` (and its legacy
  `/v2/h/…pdf` spelling) and `www.hebcal.com/holidays/hebcal-<year>.pdf`. Much of this
  file is that service's engineering notes.

See [README.md](README.md) for the endpoints, the package layout and how to run
it.

**Provenance of the PDF calendars.** The renderer began as a port of the Node.js
service in hebcal-web (`src/pdf.js`, `src/holidayPdf.js`). That JavaScript
implementation has been retired: there is one implementation, nothing to compare against, and
this code is the definition of what a calendar looks like. A change to
rendering is verified by the tests in `internal/service/pdf` and by inspecting
the output with tools that understand PDFs (`pdftotext -bbox`, `pdffonts`,
`pdfinfo`) -- see "Testing strategy" below.

## Comments and port lineage

Almost every file here is a port of something in `hebcal-web`, `@hebcal/*`,
`hebcal-mcp`, and it is likely more will be ported over time.
Keep that lineage **out of the `*.go` doc comments** and **in this file**:

- A Go doc comment describes what the function *does* and why, in Go terms, and
  reads standalone. It does not say "port of `foo()` in `src/bar.js`", "matches
  `@hebcal/core`", or "the JS version does X differently".
- Behavioural facts that look like bugs stay in the comment — "Purim carries
  `MINOR_HOLIDAY` but a major-only calendar keeps it", "`euro=0` still sets
  euro" — just phrased as domain rules, not as "because `@hebcal/rest-api`
  does X".
- The Go↔JS mapping (function, variable, whole file, or a 1:1 concept) goes in
  **Port provenance** at the end of this file. When you port more code, add its
  rows there.
- References to the JS source are fine in `README.md` and `CLAUDE.md`.

## Architecture of the PDF half

Everything up to the event list is shared with the CSV calendars and lives in
`internal/service/download`: decoding the request (the `/v4/` protobuf and its
two legacy spellings), resolving it into `download.Params`, generating the
events, and fetching the learning series no Go schedule covers. Neither
format package depends on the other.

The service follows the repository's usual direction — `handler` → `service` →
`model`/`pkg`, and nothing in a service package writes to an
`http.ResponseWriter`.

| file | role |
|---|---|
| `internal/handler/download.go` | transport shared with the CSV route: the `/v4/` dispatch by extension, methods, ETag/304, the status each error maps to |
| `internal/handler/pdf.go` | both PDF routes' headers and the holiday route's error mapping |
| `internal/service/download/request.go` | `Decode`: any of the three download URL shapes → the `Download` protobuf; the 501/503 learning error |
| `internal/service/download/params.go` | protobuf → `hebcal.CalOptions`; port of `deserializeDownload.js` |
| `internal/service/download/v2.go` | the legacy `/v2/h/` URL: query string → protobuf; port of `downloadHref2()` |
| `internal/service/download/cgi.go` | the classic `/hebcal/index.cgi/…pdf?query` URL: its two query encodings → `v2Query`, then `DecodeV2` |
| `internal/service/download/events.go` | `GenerateCalEvents`: event generation via hebcal-go and the category filters |
| `internal/service/download/learning.go` | the readings-svc `/learning` fetch for unsupported learning series |
| `internal/service/pdf/pdf.go` | `Service.Prepare`/`Render`, the document title |
| `internal/service/pdf/events.go` | flattening events into cells, ordering within a day |
| `internal/service/pdf/hebmonth.go` | Hebrew-month pagination (`mm=1`, `mm=2`) |
| `internal/service/pdf/render.go` | page layout |
| `internal/service/pdf/text.go` | bidi and shaping |
| `internal/service/pdf/fonts.go` | font loading, metrics, per-document embedding |
| `internal/service/download/links.go` | URL shortening and tracking, shared with the iCalendar feeds (`pdf/links.go` sets the PDF's utm_medium) |
| `internal/service/pdf/fallback.go` | merging the fetched learning rows into the cells |
| `internal/service/holidaypdf/` | `/holidays/hebcal-<year>.pdf` URL parsing |
| `internal/model/calendarnames.go` | month and weekday names, generated |
| `pkg/downloadpb/` | the `Download` protobuf a `/v4/` URL carries |

The transport pieces are shared with the JSON routes: geo lookup is
`pkg/geodb`, the weak ETag and the response middleware are `internal/httpx`,
the pino access log is `internal/logger`, and `/ping` + `/metrics` are
`internal/handler/server.go`. Anything the PDF routes need from the transport
layer belongs in `httpx`, not in a private copy.

One consequence worth knowing: the PDF routes go through the same
`httpx.Middleware` as the JSON ones, so they get `X-Response-Time`, the
`http_requests_total` counter and the access log for free (`application/pdf` is
not in `compressibleType`, so nothing is compressed twice). The handler writes
the whole document and the middleware sets `Content-Length` and drops the body
for a `HEAD`.

Events come from [hebcal-go](https://github.com/hebcal/hebcal-go) **in-process**,
not over HTTP from a classic-API endpoint. Such an endpoint reports a
category string per event, but the renderer needs the numeric flag bitmask —
colour and font weight both switch on it, and the category is a lossy projection
of it. (The learning fallback in `fallback.go` is the one exception, and it is
for series hebcal-go cannot generate at all.)

Text is handled by three libraries rather than one:

- [go-text/typesetting](https://github.com/go-text/typesetting) shapes, a pure
  Go HarfBuzz — real Hebrew GSUB/GPOS: nikud positioning, presentation forms.
- `golang.org/x/text/unicode/bidi` orders, implementing UAX #9.
- [seehuhn.de/go/pdf](https://seehuhn.de/go/pdf) writes the PDF, embedding both
  TrueType and CFF outlines with real subsetting.

With the real bidi algorithm, mixed Hebrew and Latin strings come out right
without per-string special-casing of parentheses or trailing commas.

## Things that cost real time to work out

These are not obvious from the code, and several look like bugs when they are
not. Read this section before changing layout, shaping or URLs.

### Layout coordinates run top-down

`render.go` measures from the top-left, the way a page is read; PDF measures
from the bottom-left. All layout is written top-down and converts in exactly
two helpers, `baseline()` and `yLine()`. Keep it that way — mixing the two
systems is what originally put the day numbers in the wrong corner.

The constant names mislead: the grid rectangle is
`rect(LMARGIN, BMARGIN, W-L-R, H-T-B)`, so `BMARGIN` is its **top** edge and
`H-TMARGIN` its bottom.

### Baselines come from hhea, not OS/2

A baseline sits one ascender below the top of the text box, and that ascender
is the font's **hhea** value. `sfnt.Font.Ascent` exposes the OS/2 typographic
ascender instead. For Source Sans Pro they disagree sharply — 984 against 750 —
which sat every day number about 3.3pt high. Adobe Hebrew happens to agree at
727, so only the Latin faces looked wrong. `fonts.go` reads hhea directly.

### Shape at a reference size, never at the drawing size

HarfBuzz quantises advances to the pixel grid at whatever ppem it is given. At
8.5pt a space came out 1.797pt instead of 1.700 — about 5.8% wide, compounding
across a string. Widths must scale linearly from font units, so `text.go`
shapes every run at 1000pt and scales.

This is not only about widths: the time string's width positions the event
subject after it, and the same measurement drives centring, right-alignment and
the font-shrinking loop.

### The extra Hebrew word space belongs at the leading edge

A right-to-left run carries one extra space width, and it sits at the
**leading edge**: the visible result is one space between the words and the
second before them. `הדלקת נרות` draws 46.92pt of ink starting 2.83pt in,
inside a 49.75pt advance.

Widening each space in place gives the same total advance — lines still
right-align correctly — but visibly too much air between words and too little
before the time. `text.go` adds the extra width to the run's `Skip`.

### The two-line break point is not the middle

When a subject is still too wide after four half-point reductions,
`splitInTwo` breaks it in two. The subject is split into words *and* the
separators between them, and the break falls at the element just past the
array's midpoint, or at the space after it when that element is a word. For an
even number of words that leaves one more word on the first line than halving
does: "Yom HaAliyah School" / "Observance". A right-to-left subject breaks as a
plain halving, so `splitInTwo` takes an `rtl` flag. With fewer than three words
a left-to-right subject has no space past its midpoint and nothing is broken at
all, yet `numLines` is still incremented, so the row advances (and its link box
is sized) as if it had wrapped.

### Every drawn glyph needs a ToUnicode destination

A mark glyph left with an empty destination writes `<>` into the CMap. That is
meaningless, extractors disagree about it, and some repeat the base letter —
which broke copy/paste and search on Hebrew calendars. A cluster's runes are
divided among its glyphs: base runes to the advancing glyph, combining marks to
the zero-advance ones. `render_pdf_test.go` asserts no CMap contains `<>`.

### Marks carry a horizontal offset too

HarfBuzz positions a combining mark with an `(XOffset, YOffset)`. The vertical
part maps onto `Rise`; a PDF glyph sequence has no horizontal equivalent, so the
shift is expressed through the advances around it — pen forward before the mark,
back after. Dropping `XOffset` leaves every niqud to the left of its letter.

Because that edits an already-emitted glyph, `run.Width` is summed **after** the
loop, not accumulated during it.

### Bidi runs come back in logical order

`x/text/bidi`'s `Order()` yields runs logically. The renderer draws left to
right, so a right-to-left paragraph needs them reversed: `ינואר 2027` is stored
month-first but displays with the year on the left.

### Fonts are embedded as Type0 composites

A simple font addresses at most 256 glyphs through a custom encoding, and
viewers disagree about one whose encoding is not a standard base — Chrome
rendered the Hebrew heavier than macOS Preview did from the same file.
Preview, poppler and ink measurement never showed a difference, so the
composite choice is a precaution rather than a confirmed fix.

### Holiday filtering goes by category, not by flag

Purim, Erev Purim and Chanukah carry `MINOR_HOLIDAY`, exactly like Tu BiShvat
and Rosh Hashana LaBehemot. A calendar asking only for major holidays keeps the
first three and drops the last two, which no reading of the flags explains.

The category scheme files those three as `["holiday", "major"]`, and the filter
tests the category rather than the flag. `eventCategories()` and `keepEvent()`
in `events.go` implement both halves; filtering on `MINOR_HOLIDAY` directly
loses Purim and Chanukah from a major-only calendar.

### A calendar with no events is a 400

A PDF request that produces no events is answered
`400 Please select at least one event option`, not an empty document. The
check has to come after the daily-learning fetch, since those rows can be the
whole calendar.

Months before the first event and after the last are omitted; gaps in between
are kept and drawn empty.

### Events within a day come from hebcal-go's order

hebcal-go sorts each day's events by `hebcal.TimeOfDay()`: timed events
before dawn ("Fast begins" for a minor fast, an early Latest Kiddush Levana),
then the parsha, special Shabbatot and minor fasts, the Molad and Shabbat
Mevarchim, other holidays and the Omer, the Erev Pesach chametz deadlines,
daily learning, evening holidays (Erev holidays, Chanukah candles), and finally
the evening's timed events -- candle lighting, Havdalah, "Fast ends", Erev
Tish'a B'Av's "Fast begins", a late Kiddush Levana -- by time. So 14 Nisan on a
weekday reads "Fast begins", "Ta'anit Bechorot", the two chametz deadlines,
the learning rows, "Erev Pesach", "Candle lighting".

The PDF keeps that order rather than re-deriving it. `Event.rank` carries
`TimeOfDay()` from `Generate`, and `sortDay` is a stable sort on it and then
on `Event.series`, so it changes nothing hebcal-go produced. What it is *for*
is the learning rows `fallback.go` fetches from readings-svc: `mergeLearning`
appends them at the end of the day, and with `rank` `TimeOfDayLearning` they
move up into the learning slot, before the evening holiday, and into their
places among the series hebcal-go generated (see "Daily-learning order").

**Latest Kiddush Levana is a timed event too, but not a `hebcal.TimedEvent`.**
`hebcal.KiddushLevanaEvent` embeds one, so a plain `ev.(hebcal.TimedEvent)`
misses it; go through `hebcal.AsTimedEvent`. Without that, `timeStringOf` returns
nothing for it, and the row draws hebcal-go's `Render()` text -- "Latest Kiddush
Levana: 9:44", time baked into the subject with no a/p suffix -- where every
other timed row draws a bold "9:44p".

### The legacy /v2/ URL is a query string, not a protobuf

`/v2/h/<base64>/<name>.pdf` is the download URL hebcal.com emitted before the
protobuf one, and crawlers still ask for it daily. Its base64 decodes to a
plain query string (`v=1&geonameid=3584003&m=50&year=2021&c=1&…`), not to a
`Download` message.

`v2.go` converts that query string into the `Download` message and hands it to
`ParamsFromMessage` — so from `download.Decode` onward a `/v2/` request *is* a
`/v4/` request, there is no second decoding path to keep in step, and the
answer is a 200 rendered in one hop. The sample URLs in `v2_test.go` pin the
payloads.

**Two location forms are resolved that a plain conversion would drop**: a
legacy `city=` identifier ("GB-London") and the degrees/minutes
`ladeg`/`lamin`/`ladir` form. Without them the calendar would carry no
location -- and, since a location implies candle-lighting, come out titled
"Hebcal Diaspora" with no times. `applyV2Location` handles both. The legacy
city name rides in the protobuf's `cityName` (otherwise unused for a legacy
URL) and is resolved by the `LookupLegacyCity` branch `applyLocation` already
had; the degrees/minutes form goes through `location.FromLegacyLatLong`, which
covers the range checks, the legacy numeric-timezone mapping and the
"40°42′N 74°0′W America/New_York" city name.

Two details of that: the legacy city name is a lookup key rather than a label,
so `applyLocation` clears `Params.CityName` and lets the resolved location name
the calendar ("Hebcal Melbourne 2026", not "Hebcal AU-Melbourne 2026"); and the
coordinates in the rendered name are one arc-minute below the input
(`31°46′` in, `31°45′` drawn), which is `makeGeoCityName`'s own floating-point
truncation -- `60 * (31.766666666666666 - 31)` is `45.99999999999994` in any
IEEE-754 double.

**Two very old parameters are read the way the feeds read them.** A
`city-typeahead` that is a ZIP code -- five digits, or a label ending in a
space and five digits when `zip` is empty -- is the location unless a
geonameid is given, whatever `geo` says: the form submitted such URLs with
`geo=none` or `geo=geoname&geonameid=`, and `typeaheadZip` turns them into
`geo=zip` before `normalize` so the ZIP and its `b`/`m` survive. And `a=on`
(or `a=1`) with no `lg` is `lg=a`, Ashkenazi transliterations. Both are
`getLocationFromQuery` / `makeHebcalOptions` behaviour that `downloadHref2`
never needed, since the website's form never produced them; about 200 of the
legacy feed URLs crawled from production carry one or the other.

Three things about the rest of the conversion look wrong and are not:

- **`euro` and `subscribe` are bare truthiness tests, `h12` an off-test.**
  Every non-empty string counts as set, so `euro=0` sets euro while `h12=0`
  does not set 12-hour. The three cannot share one helper; `v2Query` has `on`,
  `off` and `truthy` for that reason.
- **A URL that names no Havdalah rule means tzeit.** An absent `m` reads as
  null, which together with `M=on`, a `td=`, and a bare `M=off` all land on
  `havdalahTzeit`.
- **Sedrot has no line of its own.** It arrives through the daily-learning
  mapping, whose last entry maps the `s` parameter to the protobuf's `sedrot`.
  That mapping is one more copy of the one named under "Daily learning" below.

**Location precedence is spelled out twice.** `location.FromQuery` (the JSON
routes) and `applyV2Location` (every download URL, which produces a protobuf
rather than a `geodb.Location`) both pick geonameid, ZIP, `city`, decimal
lat/long, then degrees/minutes, in that order; they must agree. In particular
`geo=pos` says nothing by itself -- an old form's degrees/minutes URL carries
it too -- so the decimal branch needs `latitude` *and* `longitude`, as
`FromQuery` does. The degrees/minutes arithmetic and the name it gives live
once, in `location.FromLegacyLatLong`.

Two encodings deliberately differ from what hebcal.com itself writes, neither
observable in the rendered calendar: protobuf field order (Go writes `oneof`
fields last), and `start`/`end`, which travel as the ISO string the message can
also carry rather than as epoch seconds — the same date, without a trip through
the server's local timezone.

Only `/v2/h/` and only `.pdf`, `.csv` and `.ics` are ours on this path (an
`.ics` is "The holiday feeds"), and a URL with no `v=` is 404. A URL whose
`v=` names a Yahrzeit calendar never reaches this code: it is served by
`yahrzeitDownload` (see "The iCalendar feeds"), which answers a `.pdf` 404 and
serves a `.csv` as the Yahrzeit CSV export.

### The classic /hebcal/index.cgi/ URL is a query string in two encodings

`/hebcal/index.cgi/<name>.pdf?<query>` is the download URL older than both the
`/v2/` base64 and the `/v4/` protobuf, and crawlers still ask for it. It
carries the same query string a `/v2/` URL hides in base64, so `cgi.go` decodes
a CGI request by building the same `v2Query` and running it through
`DecodeV2`, after two preprocessing steps `/v2/` has no counterpart for
(`applyCGILegacyParams`): the very old `nh=on` (which expands to `maj`/`min`/
`mod`/`mf`/`ss`, every negative option but Rosh Chodesh, and overrides an
explicit one beside it), and lowercase `m=on` as the old spelling of `M=on`.
`tag` and `set` are ancient and ignored. From `download.Decode` onward a CGI
request *is* a `/v4/` request, the same collapse `/v2/` makes.

**The query arrives in two encodings, both handled by `normalizeCGIQuery`.**
The old CGI accepted `;` as a parameter separator, so a query can be
semicolon-separated (`dl=1;v=1;...`), which is turned into `&`. And the whole
query can be percent-encoded a second time, its `;` as `%3B` and usually its
`=` as `%3D`; that is spotted by the `dl=1%3B` (or `subscribe=1%3B`) prefix and
`unescape`d -- a **Latin-1** decode that leaves `+` alone, not
`decodeURIComponent` -- before splitting (`unescapeLatin1`).

The status codes: only `.pdf`, `.csv` and `.ics` are ours, a
URL naming no `v=` is 404 (which is why `hebcal_2028_may.pdf` with no query
string is 404, not a calendar), and a `v=` naming a Yahrzeit calendar goes to
`yahrzeitDownload`. The finite `redirectDownload.json` map of specific no-query
legacy URLs is not reproduced -- those 404 here, which is acceptable for a
handful of curated redirects.

### The /holidays/ calendars

`internal/service/holidaypdf` serves `www.hebcal.com`'s
`/holidays/hebcal-<year>.pdf`. It is a small request -- a year and `i=on` --
feeding the same generator and renderer as `/v4/`, so the whole package is URL
parsing: `Parse` hands back a `download.Params` and `pdfHoliday` does the
rest. Four things about it are not obvious:

- **The year string keeps its extension.** The year is parsed from
  `"2026.pdf"`, and `leadingInt` stops at the dot. The hyphen test that makes
  `hebcal-2026-2027.pdf` a *Hebrew* year (5787, the year beginning in the first
  of the two Gregorian years, which is how the year-index pages link it) runs
  on that same string.
- **There is no locale support, deliberately.** Nothing on www.hebcal.com ever
  links a localized holiday calendar: the holiday pages and the year index
  emit `hebcal-<year>.pdf` with at most `?i=on`. These are always English, so
  `parseHolidayPDF` hard-codes `Locale: "en"` and ignores `lg` rather than
  carrying a locale through the renderer for URLs nobody requests.
- **Every link is tagged with its own event's Hebrew year.** The document sets
  no campaign, so each link falls back to `'pdf-' + the event's Hebrew year`. A
  2026 calendar therefore carries both `uc=pdf-5786` and `uc=pdf-5787`
  (`Params.PerEventCampaign`).
- **60 days of `Cache-Control`, and none on a refusal.** The header is set only
  once the URL is accepted, so the 404, 400 and 410 go out uncached; and a
  holiday PDF carries `nosniff` but no `Access-Control-Allow-Origin`. That is
  why the two routes have separate error mappings (`writeDownloadError` and
  `writeHolidayError`): the download path's 410 is cacheable and names the
  year, the holiday path's is neither.

### The alternate date is hebcal-go's brief Hebrew date

`download.HebrewDateBrief` draws the day line as
`event.RenderBrief(event.NewHebrewDateEvent(hd), locale)`: the month from the
locale catalog with a typographic apostrophe ("Sh’vat") and the locale's
ordinal ("12. Tewet" where `lg=de` wants it), without the year.

### Response headers and caching

A rendered PDF carries `Cache-Control: public, max-age=1209600, s-maxage=1209600`
(14 days), `Access-Control-Allow-Origin: *`, `X-Content-Type-Options: nosniff`,
and a weak `ETag`. The response codes divide by whether the Cache-Control
survives: 200 and the out-of-range 410 keep it (the 410 because that year never
comes into range); the no-events 400 drops it, and so does the unknown-location
404, because a missing location may be added later and pinning the 404 in
Varnish for two weeks is worse than a cache miss. `pdfDownload` and
`writeDownloadError` implement that. `X-Content-Type-Options` comes free from
Go's `http.Error` on the error paths but is set explicitly on the 200, which
uses `w.Write`.

**The shared error helpers strip caching headers.** `httpx.WritePlainError`
and `httpx.WriteJSONError` delete `Cache-Control`, `Expires`, `Last-Modified`
and `ETag` before writing, whatever the status, so an error found after a
handler set the 200's headers (a PDF render failure, the `/shabbat` readings-svc
503, a `/zmanim?im=1` 400) is never cached. The two errors that are *meant* to
be cached -- the download 410 and `/complete`'s no-match 404 -- are written
with `http.Error` / `writeNotFoundJSON` so they keep theirs; keep it that way.

**A dateless `/shabbat` or `/zmanim` answer is keyed by the clock, not the
URL.** Read `time.Now()` once per request for the date choice (`WeekRange` /
`StartAndEnd`), and derive the expiry from the result: `/shabbat`'s
`ExpiresAfterWeek` takes the window's start date, not the clock, so the
header cannot describe a different week from the body (`/zmanim`'s
`ExpiresTomorrow` takes the same `now`). Two clock readings that straddle
Saturday midnight would pair last week's body with next week's Expires, and a
popular URL's Varnish object is refetched at exactly that instant. For the same
reason the dateless `/shabbat` ETag mixes in the week's start date: with a
URL-only tag, Varnish revalidating last week's copy (`beresp.keep` +
`If-None-Match`) gets a 304 and keeps it indefinitely.

**CORS differs by format.** The PDF 200 sends `Access-Control-Allow-Origin: *`
because www.hebcal.com's `print-iframe.ejs` loads the PDF cross-origin from
download.hebcal.com. The `.csv` 200 sends none, and nothing needs a page to
`fetch()` a CSV. (`http.Error`
does not set ACAO on either format's error paths.)

The `ETag` (`httpx.MakeETag`) is a weak FNV-1a hash of the
request path, query, encoding class and the API version — not a hash of the
rendered bytes. Because it depends only on the
request and the build, the conditional check can answer `304` *before* the
render runs: a client only holds the tag if it received a 200 for the same URL,
and the calendar is a deterministic function of that URL. A new release of this
service changes every tag, since `config.APIVersion` is folded into the hash.
(A library upgrade alone does not, unless it ships in a new release.)

### Fast start and end times

hebcal-web accepts six parameters that move the start and end of fast days --
`fsd`/`fsm` (minor fasts begin, degrees or minutes before sunrise),
`fed`/`fem` (minor fasts end) and `tbed`/`tbem` (Tish'a B'Av ends) -- and the
`/v4/` token carries them as `fastStartDeg` … `tishaBavEndMins` (fields
70-75). The options and their defaults -- Tish'a B'Av ends at 6.45°
(Tucazinsky), and in Israel a minor fast ends 15 minutes after sunset (Rabbi
Deblitzky's practice) -- live in hebcal-go (`CalOptions.FastStartDeg` …
`TishaBavEndMins`, `makeFastStartEnd`).

This side only parses, straight into `hebcal.CalOptions`: `model.SetFastTimes`
is `getFastTimeOpts()` -- degrees in (0, 90), minutes 1..240, `Math.abs` on
both, and degrees win over minutes within a pair, so hebcal-go's
mutual-exclusion error cannot be reached from a URL. `/shabbat` calls it from
`shabbat.CalOptions`, the PDF path from `setFastTimes`. The `/v4/` token
goes through the same function after rendering each field back to its
query-string form, which is `deserializeDownload`'s path: a `float` field is
32-bit, and `toPrecision(6)` there (`'g', 6, 32` here) is what turns
19.799999237060547 back into 19.8. `geo=none` drops all six on a legacy URL,
as it drops `b`, `m` and `td`.

## Things that look like bugs and are not

Do not "fix" these without understanding why they are there.

- **`Achrei Mot-Kedoshim` links use the long URL form.** Keying on the text
  before the first hyphen finds `achrei`, which is not a portion. The quirk is
  deliberate and kept.
- **`sedra.Parshiot()` has 53 entries, not 54.** V'Zot HaBerachah is read on
  Simchat Torah rather than on a Shabbat.
- **Tamuz is spelled with one m.** `hdate` uses two; the website uses one. All
  fourteen other month names already agree, so `hebMonthNameOverrides` is a
  single entry. The proper fix belongs in `hdate`.
- **Chanukah candle times can differ by about a minute.** Of 143 timed events in
  2028, three differ. That is a zmanim question for hebcal-go and noaa-go, not
  a rendering one.
- **The `ft` ligature is absent from the ToUnicode map**, so `Shoftim` extracts
  oddly. A pdftotext artifact, not a regression.

## The CSV calendars (`/v4/…csv`)

`internal/service/csv` serves `download.hebcal.com/v4/<data>/<name>.csv` (and
the `/v2/h/` and `/hebcal/index.cgi/` spellings of it), the
Outlook-compatible export of the same `/v4/` calendar the PDF draws. It is a
port of `@hebcal/rest-api`'s `src/csv.ts` (`eventToCsv()`) and of hebcal-web's
`eventsWithParshaToCsv()` / `getCsvParshaMemo()` (`src/parshaCommon.js`), and
it shares everything up to the event list with the PDF through
`internal/service/download`: `Decode(ctx, u, ".csv")`, `ParamsFromMessage`,
and `GenerateCalEvents` (the raw, category-filtered hebcal-go events that
`pdf.Generate` flattens into cells). `handler.download`, mounted on all three
download URL shapes, sends `.csv` to `csvDownload` and everything else to
`pdfDownload`; both go through the one download concurrency limiter
(`-download-max-concurrency`), and the CSV path needs no fonts.

The bar is the same bytes production serves, and it was measured that way: a scratch harness ran hebcal-web's own
`deserializeDownload` → `makeHebcalOptions` → `makeHebrewCalendar` →
`eventsWithParshaToCsv` against the handler test databases and diffed the
result row by row with this service's output. Across about 40 URLs (the samples
from the access log, every locale family, euro dates, `h12`, Israel, ZIP and
lat/long locations, alternate dates in both month modes, omer, Yom Kippur
Katan, Shabbat Mevarchim, fast-time options, yom-tov-only, no-minor) every row
matched except the cases listed below. It is worth re-running that harness
before changing anything here.

Things that are not obvious:

- **Rows are written with `encoding/csv`** (`UseCRLF`). A field is quoted only
  when it needs to be (a comma, a quote, a leading space), `"` is doubled, and
  commas are no longer deleted from the subject or turned into `;` in the
  description -- a deliberate divergence from production's always-quoted,
  comma-rewritten rows. Newlines in a description are still flattened to
  `" / "`, which a CSV writer would otherwise have to quote.
- **Every document starts with a UTF-8 BOM** -- a deliberate divergence.
  hebcal-web writes one only for a non-English locale, but an English
  calendar is not ASCII either ("Sukkot III (CH’’M)", "Sh’vat"), and without
  the BOM Excel reads it as Windows-1252. Lines end in CRLF, the last one
  included.
- **A CSV spans several years, a PDF one.** hebcal-web applies
  `getNumYears()` to `.ics`/`.csv` only: 7 by default, capped by candle-lighting
  (4), sedrot or omer (5), omer+sedrot (4), alternate dates (2–4) and any daily
  learning (2), and 1 with two or more daily series. `numYears` ports it.
- **The description's priority** is: the special-Shabbat / Rosh Chodesh names
  for a weekly portion (`" + "`-joined, or "Machar Chodesh" before a new month);
  then the event's own memo (hebcal-go's `event.Memo`) -- the molad for
  Shabbat Mevarchim (via `shabbat.MevarchimMoladMemo`, for this service's
  clock rules), "Minor Day of Atonement on the day preceeding Rosh Chodesh
  <month>" (sic, untranslated) for Yom Kippur Katan, the coming portion or
  festival reading for erev-Shabbat candle-lighting; then the first sentence of the
  `MEMO:` catalog entry (Hebrew for Hebrew locales, English otherwise); then,
  for a timed event, its linked event rendered in full ("Fast begins" →
  "Tzom Gedaliah", erev-Yom-Tov candles → "Erev Sukkot").
- **Erev-Shabbat candle-lighting in a festival week names the festival
  reading** ("Sukkot Shabbat Chol ha-Moed", "Pesach I" -- but plain "Pesach" in
  Israel), which hebcal-go's `sedra` reports as the chag week's name.
- **Yizkor's description is the holiday it is recited on** ("Yom Kippur"), as
  for a timed event's linked event.
- **Kiddush Levana carries no description**, deliberately: hebcal-web's memo
  for it is going away, so the CSV starts without it.
- **Brief vs full subjects** follow `shouldRenderBrief()`: timed events,
  Hebrew dates other than the 1st, daily learning, Shabbat Mevarchim and Yom
  Kippur Katan are brief, through hebcal-go's `event.RenderBrief` and the
  learning events' own `RenderBrief`; descriptions come from their `Memo`. The
  appended Hebrew (`lg=ah`/`sh`) is always the brief form.
- **Rosh Hashana's year is gematriya in Hebrew** ("רֹאשׁ הַשָּׁנָה תשפ״ז") and a
  number elsewhere, as hebcal-go renders it -- deliberately unlike
  `@hebcal/core`, which writes a number in Hebrew too. This applies to every
  output (CSV, PDF, `/shabbat`, `/converter`).
- **Daily-learning subjects keep typewriter apostrophes** ("Fourth Year's
  Fruit"); every other subject is smartened.
- **A Hebrew date leads its day**, and the readings-svc learning rows sit in
  the learning slot, interleaved with the series hebcal-go generated in
  `download.learningOrder` (see "Daily-learning order"): the download form's,
  with **929 first** -- hebcal-web keeps the selected series in a JavaScript
  object, whose integer-like key `929` iterates before every other. That order
  fixed the same cells in the PDF.

Known differences from production (every other row of the sweep is
byte-identical):

1. Zmanim about a minute off on some times (noaa-go vs @hebcal/core), as in
   the PDFs.
2. Hebrew Nach Yomi spells Amos "עָמוּס" where production writes "עָמוֹס":
   `@hebcal/learning` and `@hebcal/leyning` translate it differently, the
   website loads leyning's catalog last, and the generated
   github.com/hebcal/locales kept learning's. A catalog-precedence question for
   that generator.
3. `lg=ah` Shabbat Mevarchim: production appends "מְבָרְכִים חוֹדֶשׁ Teves",
   because `MevarchimChodeshEvent` translates the month into the calendar's
   locale in its constructor; this service writes "טֵבֵת". Not reproduced.
4. Hebrew-month (`mm=1`/`mm=2`) is ignored, as in the iCalendar feeds (see
   "The holiday feeds"): production appends the next Tishrei from `year+1`
   and adds Gregorian-date rows; here the request is read as a Gregorian-month
   one.

## The iCalendar feeds (`pkg/icalendar`, Yahrzeit first)

Every Yahrzeit + Anniversary feed is served here, the saved `/v3/` calendars
included, and so is every holiday feed ("The holiday feeds" below). The static
`/ical/` calendars are generated here too, though not yet in production (see
"The static calendars"). [iCalendar-port.md](iCalendar-port.md) has what is
left, and how to stand up the reference server.

`pkg/icalendar` is a port of `@hebcal/icalendar` (hebcal-icalendar 7.0.6), and
`internal/service/yahrzeit` of hebcal-web's `yahrzeitDownload.js`,
`yahrzeitEvents.js` and the parts of `yahrzeitCommon.js` and `app-download.js`
they need. Served as `.ics` and as `.csv` (see "The Yahrzeit `.csv`"
below):

- `/v2/y/<base64 query string>/<name>.ics`
- `/yahrzeit/yahrzeit.cgi/<name>.ics?<query>`
- `/export/[<dir>/]<name>.ics?<query>` and `/hebcal/index.cgi/<name>.ics?<query>`
  when the query names a personal calendar
- `/v3/<ulid>/<name>.ics`, a saved calendar whose query lives in MySQL

`handler.download` (mounted on `/v2/`, `/export/`, `/hebcal/index.cgi/` and
`/yahrzeit/yahrzeit.cgi/`) asks `yahrzeit.ParseRequest` first, and hands
the request to `yahrzeitDownload` when the decoded query's `v` starts with
`y`; everything else falls through to the PDF/CSV path unchanged.

**The bar is the same bytes production serves, UIDs included.**
A subscriber's client matches events by UID, so a changed UID duplicates every
event in their calendar. `internal/service/yahrzeit/testdata/golden/` holds 36
feeds captured from a local hebcal-web (`tools/yahrzeit-goldens.sh`
regenerates them; `testdata/TODAY` pins the day they depend on), and
`TestGolden` requires byte equality. Run the script again -- and read the diff
-- before changing anything in either package.

### pkg/icalendar is a serializer, not a port of the IcalEvent class

`IcalEvent`'s constructor mixes two jobs: deciding what an event says (brief
or full render, emoji placement, category from flags, the default alarm, the
UID) and writing it down. Only the second lives in `pkg/icalendar`, which
takes a resolved `icalendar.Event` and knows nothing about hebcal-go. The
first belongs to the service that owns the events -- `yahrzeit.ICS` today, a
holiday-feed service later -- because everything it needs
(`shouldRenderBrief`, `renderSubject`, the category table) already lives under
`internal/service/` and `pkg/` deliberately imports nothing from `internal/`.
`AlarmUserEvent`, `AlarmOmer`, `AlarmCandleLighting`, `AbsoluteAlarm` and
`DefaultUID` are exported for those services; `internal/service/ical` (the
static feeds) uses `AlarmOmer` and `DefaultUID`, and Kindness a Day the
`RRule` field.

### Line folding

RFC 5545 limits a physical line to 75 octets. Content is cut at **74**, as
`foldLine` does, because a continuation line spends one octet on its leading
space. The cut never lands inside a UTF-8 sequence or a grapheme cluster
(nikud stays with its letter, a ZWJ family or a flag stays whole, CR stays
with LF). `github.com/rivo/uniseg` does the segmenting, and only at a
candidate break whose neighbours are not both ASCII -- between two ASCII
characters there is always a boundary, which settles most breaks for free.

Two deliberate differences from `foldLine`, neither reachable from a real feed:

- **A cluster too wide for the next line is not carried over.** `foldLine`
  backs a break up to the start of the cluster it would split even when that
  cluster is 73+ octets, and the carried-over part then overflows the next
  line (found by `FuzzFoldLine`: two ASCII characters, 37 combining marks, one
  more). Here such a cluster is cut where it crosses the limit.
- **The all-ASCII fast path keeps CRLF together**; `foldLine`'s cuts at fixed
  offsets.

A 100 KB, three-person, twenty-year feed takes ~0.9 ms end to end
(`BenchmarkICS`), folding about 13% of it; there is nothing in it worth
optimizing.

### Things that look wrong and are not

- **MurmurHash3 is the x86 32-bit variant** (`murmur3.StringSum32` from
  github.com/twmb/murmur3, seed 0), which is `murmur32HexSync`.
  `TestMurmur32Hex` pins eight values taken from `@hebcal/murmurhash3`. The
  one hash that does **not** match is the empty calendar's placeholder UID:
  hebcal-web uses the x86 **128**-bit variant, twmb has only the x64 one, and
  that UID embeds today's date so it changes daily anyway.
- **Reminders are on by default.** `yrem` is tested as "not `0` and not
  `off`", so every legacy URL -- all older than the option -- gets a timed
  "Yahrzeit reminder" event on the eve of each Yahrzeit (4:30pm; 2:30pm on a
  Friday, 8pm on a Saturday) with a `TRIGGER:P0DT0H0M0S` alarm. The Yahrzeit
  itself has no alarm; birthdays, anniversaries and Yizkor get the user-event
  default, noon the day before.
- **The reminder is a floating time** (`DTSTART:20260224T163000`, no TZID),
  except in two cases: `i=on` pins it to Asia/Jerusalem, and the exact
  User-Agent `Google-Calendar-Importer` gets a zone derived from `tzo` (the
  browser's `getTimezoneOffset()` when the calendar was made), because Google
  does not understand floating times. Hence `Vary: User-Agent`, and the
  User-Agent in the ETag. Both cases also write `GEO:` -- `0;0` for Google's
  copy, since that location is a zone and nothing else.
- **That hack needs a `t<N>` parameter.** It is gated on the calendar's set of
  anniversary types containing Yahrzeit, and the set is built from the `t`
  keys alone, so an entry with no `t1` is rendered as a Yahrzeit (the default)
  yet gets floating reminders even for Google.
- **A half-hour `tzo` produces a zone that does not exist** (`tzo=-330` →
  `Etc/GMT+5.5`), and a fractional offset inside the US or European range
  produces none at all. Both are what production emits.
- **The title's 64-character limit is in UTF-16 code units**, so
  `calendarTitle` encodes to UTF-16 to find the cut.
- **Parameter order names the calendar.** The title lists names in the order
  their `n` keys appear in the URL, and a repeated key keeps its *first*
  value -- the opposite of the `v2Query` the PDF path uses, which is why
  `yahrzeit.Query` is its own type.
- **A subscription forgets the past; a download does not.** Without `dl=1`,
  events older than twelve weeks before this week's Sunday are dropped (and
  the feed capped at 1200 events). The ETag therefore includes the week.
- **No Cache-Control on a feed.** hebcal-web sets none for Yahrzeit calendars,
  and Varnish's `vcl_deliver` marks them `private`. The empty calendar's
  one-event placeholder is the exception: `max-age=86400`, no ETag.
- **An entry number is a key, not a count.** hebcal-web loops from 1 to the
  highest entry number; `entryIDs` visits only the numbers present, so
  `n999999999=` costs nothing. Same output.

### Deliberate divergences

- **Line breaks in a value are escaped.** hebcal-web writes a name or `title=`
  containing `%0A` raw, which lets a URL inject properties or whole components
  into the feed. `icalendar.line` writes any CR/LF in content as `\n`.
- **`start=` outside Hebrew years 1-9999 is a 400** rather than whatever the
  date arithmetic makes of it.
- **A Yahrzeit `.csv` is written like the holiday CSVs**, not like
  hebcal-web's -- see the next section.
- **`/yahrzeit/yahrzeit.cgi/` with `v=1`** is a holiday feed on hebcal-web and
  a 404 here; Varnish routes only `v=y...` to this service.

### The Yahrzeit `.csv`

The same four URL shapes with a `.csv` extension get `Calendar.CSV`: the
events `makeEvents` generates for the feed, as rows of the CSV service's nine
columns, written through `csv.Document` -- so the BOM, `encoding/csv` quoting
and CRLF of the holiday CSVs, not `eventsToCsv`'s always-quoted rows. Three
things that differ from the `.ics` and are what hebcal-web does too:

- **No reminders, and the whole range.** `yrem` is ignored (reminders are
  forced off), the subscription's twelve-week look-back and 1200-event cap do
  not apply, and the subject has no emoji. A calendar with no events is a
  header row, not the placeholder event.
- **`euro` is a bare truthiness test** (`Boolean(query.euro)`), so `euro=0`
  writes day/month/year.
- **No edit-page memo, on any route.** A CSV is a one-time import, not a
  feed, so the "To edit this Hebcal Yahrzeit + Anniversary Calendar" memo
  (and the per-entry edit link) is never written, `dl=1` or not -- `/v3/`,
  `/v2/y/`, `/export/` alike. `makeEvents` takes a `feed` flag for it.
- **Headers:** no Cache-Control and `Content-Disposition` only on `dl=1`, like
  the feed; but no `Vary: User-Agent`, and the ETag variant drops the week and
  the Google distinction, since neither changes the file. Content-Type is
  `text/x-csv; charset=utf-8`.

Measured against hebcal-web's own `makeYahrzeitEvents` + `eventsToCsv` over
the 35 golden cases (533 rows): identical in every field once production's
format is undone -- commas deleted from the subject, commas turned into `;`
and `"` into `''` in either column, and the memo's paragraph breaks, which
hebcal-web builds as a literal two-character `\n` (`String.raw`), that
`eventsToCsv`'s newline flattening therefore never sees. Here they are real
newlines in the memo, flattened to `" / "` like every other CSV description.
That format difference is the deliberate divergence, for the reasons the CSV
section gives (Excel, and keeping punctuation people typed into a name).

### Saved calendars: `/v3/` and MySQL

`/v3/<ulid>/<name>.ics` (and `.csv`) is every calendar saved from the
Yahrzeit form. `handler.yahrzeitSaved` reads the row through
`internal/repository/yahrzeitdb`, `yahrzeit.NewSavedRequest` turns it into the
same `Request` a legacy URL produces, and from there it is
`yahrzeitDownload` unchanged -- so `/v3/` needed no new rendering code, only a
lookup in front.

**Parity is measured, not assumed.** `testdata/saved/` holds two real rows
(public figures only: `01m73gh503ng2gfkfbyt3t0a38`, the Israeli prime
ministers, and `01jmt1hef19p1hw6qn48k8p4z9`, the Chabad calendar of 23
entries) exactly as `SELECT contents` returned them, and `cases.tsv` renders
seven `/v3/` URLs from them (Google's copy, `dl=1`, `yrem=0&i=on`,
`an=off&years=3`) against goldens. The reference server for those goldens is
hebcal-web's own download app with `app.context.mysql` replaced by an object
whose `query2` returns `JSON.parse(<file>)` -- mysql2 parses the JSON column
the same way, so key order is what production sees. The same 43 goldens were
then re-checked over a real MariaDB with the binary's own driver: identical.

Things that are not obvious:

- **The JSON key order is MySQL's, and it names the calendar.** MySQL's JSON
  type normalizes an object to keys sorted by length, then bytewise -- `n1 …
  n9, n10 … n23` -- and mysql2 hands that text to `JSON.parse`, which keeps
  it (no key here is integer-like). `queryFromJSON` therefore decodes with
  `json.Decoder.Token()`, never into a map. (MariaDB stores JSON as text and
  keeps insertion order; rows written by MySQL already arrive sorted, so the
  test database gives the same result.)
- **The row is expanded the way `getYahrzeitDetailsFromDb` does it**:
  `x<N>` "1978-12-08" becomes `y`/`m`/`d` without leading zeros, a numeric
  `s<N>` becomes `on`/`off`, and `t<N>` is spelled out for **every** number
  from 1 to the highest entry -- gaps included, which then count as Yahrzeits
  in the calendar's set of types (and so in its title and the Google zone
  hack). `years` is a JSON number. Then `v=yahrzeit`, then the URL's own
  query overrides any key, then `ulid` is set last, so a URL cannot rename
  the calendar.
- **`em` is the owner's email.** It rides in the query like any other key;
  nothing renders it, and the access log logs the URL, not the merged query.
  `TestYahrzeitSavedRealRows` asserts it never reaches the feed.
- **The id is used as the URL spells it**, not lowercased: it goes into
  UIDs, `X-WR-RELCALID` and the edit links, and hebcal-web takes
  `path.substring(4, 30)` verbatim. The collation makes the lookup
  case-insensitive anyway. A non-alphanumeric id is 404 without a query.
- **The ETag covers the row** (`Request.Version`: `updated` plus a hash of
  `contents`), so an edit changes it. The 304 check still needs the SELECT;
  that is one primary-key read.
- **Two writes follow every successful lookup, 304s included**: `UPDATE
  yahrzeit SET downloaded = 1, updated = NOW()` the first time, and `REPLACE
  INTO yahrzeit_atime` always. The first is load-bearing --
  hebcal-shabbat-email's retention job prunes calendars never downloaded --
  and both are kept for compatibility (the database handles the load). They
  run on one background worker behind a 256-deep queue, 3 s each; a full queue drops the write with a warning rather than
  delay a response, and the next poll writes it again. Note the first write
  bumps `updated`, so a calendar's first fetch changes its ETag once.
- **Failures:** unknown id → 404 with no caching headers; no `-ini` file, no
  `hebcal.mysql.user`/`dbname`, or a database that does not answer within
  7 s → 503 + `Retry-After: 30`, so Varnish's 24 h grace serves the stale
  copy. The cause goes to the access log's `msg`, not the body.
- **Connection:** `-ini` (default `/etc/hebcal-dot-com.ini`) is read once at
  startup by `config.ReadINI`, a flat key=value reader -- only the five
  `hebcal.mysql.*` keys are used, so no INI dependency. The pool is
  `MaxOpenConns(8)`, five-minute lifetime, one-minute idle time (the managed
  database drops idle connections), and dials lazily, so the service starts
  whether or not the database is up. TLS is `preferred` (used when the
  server offers it, unverified, since the connection stays on the VPC);
  `hebcal.mysql.tls` overrides it. hebcal-web's mysql2 pool uses no TLS at
  all. The password never appears in a flag, the unit file or the log
  (`yahrzeitdb.Config.String()` omits it).

### VTIMEZONE

`icalendar.ZoneInfo` reads `<dir>/<tzid>.ics` (`-zoneinfo`, default
`/var/www/zoneinfo`), the tree vzic generates; hebcal-devops'
`cloud-config/web-api.yaml` builds it on the api hosts the way
`dl-nodejs.yaml` does on the Node ones. The cache is unbounded on purpose --
there are ~344 files, only successful reads are stored, and the identifier is
validated before it becomes a path, so a request cannot grow it. A missing
directory or zone is not an error: the feed goes out with `X-WR-TIMEZONE` and
no definition, as hebcal-web's does.

### The zmanim feeds (`/zmanim`, `/zmanim2`, `/sunrs`)

`internal/service/zmanim/feed.go` serves download.hebcal.com's three
halachic-times subscriptions, a port of `src/zmanimDownload.js`: `/zmanim`
(every zman a timed event, yesterday to +60 days), `/zmanim2` (one all-day
event per day titled with the Hebrew date, its times in the description, +90
days) and `/sunrs` (sunrise and sunset, +365). It reuses the `/zmanim` API's
tables through `computeTimes`, writes through `pkg/icalendar`, and needs no
sidecar. `/zmanim` is shared with the JSON API: `cfg=json` is the API, any
other GET/HEAD is the feed (subscription URLs carry no `cfg`), and a CORS
preflight stays the API's.

Measured (November 2026) against a local hebcal-web download server over 26
URLs -- geonameid, ZIP, legacy `city=`, lat/long with and without `tzid`,
Sydney, Tromsø, Svalbard, the North Pole, `lg` = he, h, a, fr,
he-x-NoNikud, and the error cases: every line identical except `DTSTART`/`DTEND`
(and the `/zmanim2` description times) a minute apart on some days -- the
known noaa-go vs `@hebcal/core` difference, which moves an event without
changing its UID -- and, at Tromsø on the first day of polar night, a few
sunrise-derived times one library computes and the other does not.
`internal/handler/zmanim_feed_test.go` pins lines captured from that server.

Things that are not obvious:

- **The zman titles and memos come from hebcal-web's catalogs, via
  `locales`.** `TITLE:<zman>` / `MEMO:<zman>` are keys in hebcal-web's
  `po/en.po`, and the Hebrew titles live in its `po/he.po` (plus Kiddush
  Levana overrides in `he-x-NoNikud.po` and `ashkenazi.po`), which hebcal-web
  loads *over* `@hebcal/core`'s and which spell several titles differently
  ("חֲצוֹת הַלַיְלָה" where core has "חֲצוֹת הַלַּיְלָה"). github.com/hebcal/locales
  carries all of it, with that precedence: `zmanTitle` and
  `zmanMemo` look up the keys in `en`, and the title is translated with
  `model.Gettext` like everything else. A zman title or memo edited in hebcal-web
  reaches this service only through a new `locales` release.
- **Kiddush Levana times are moved to the night** in the feed (to tzeit 8.5°
  that evening, or back to alot hashachar that morning), and dropped on a day
  with no alot or tzeit; the JSON API reports them unadjusted. `klAtNight`.
- **An event's date is the date its zman was computed for**, not the date it
  falls on, so a Chatzot HaLailah after midnight is stamped on the evening
  before. UIDs depend on it; do not "fix" it.
- **UIDs** are `hebcal-<yyyymmdd>-<murmur32(English title)>-<place>`, place
  being the geonameid, the ZIP, or `makeAnchor` of a lat/long location's name;
  `/zmanim2`'s are `hebcal-zmanim2-<yyyymmdd>-<place>`.
- **Timed zman events are private** (no `CLASS:PUBLIC`, they are user events
  in `@hebcal/core`) and carry no alarm; `/zmanim2`'s are public.
- **Same-minute ties keep table order** (stable sort on the instant, events
  tabulated name-major), in both the timed feed and a `/zmanim2` description.
- **The 12-hour clock follows the country** (`event.ReformatTimeStr` with the
  location's CC; a lat/long location has none and is US, 12-hour), and a
  `/zmanim2` line pads a one-digit hour with a space so the colons line up.
- **The PRODID language is the raw `lg`** when it is two letters, so `lg=xx`
  writes `//XX` while the titles fall back to English, as hebcal-web does.
- **"Today" is the location's date** (`Server.Clock` read in its zone), and
  the ETag covers it, so a same-day refresh is a 304 before anything is
  computed; that is safe because the feed is a function of URL and date.
  Cache-Control is 7 days (`publishedTTL` P7D); errors carry none.
- **At a pole every sun-derived zman is the zero time**, which the JSON API
  reports as `null`. `TestZmanimJSONPolarNull` and the
  North Pole 400 in `TestZmanimFeedErrors` guard it.

### The static calendars (`/ical/`)

`cmd/hebcal-static-ical` writes download.hebcal.com's 36 static feeds plus
Kindness a Day -- a port of hebcal-web's `src/makeStaticCalendars.js`,
`src/staticCalendars.js`, `src/staticCalendars.json` and `src/kindness.js` --
and `handler.staticICal` serves them from the `ical/` subdirectory of
`-document-root` (`DOCUMENT_ROOT`, default `/var/www/html`, hebcal-web's own
`DOCUMENT_ROOT`). `staticICal` only validates the file name; the serving --
precompressed copies, `Last-Modified`, the 304 -- is `serveStatic(w, r, rel,
cacheControl)`, which takes any path under the document root, so another
static file (`/favicon.ico` with a year's immutable Cache-Control, as
hebcal-web serves it) is a route that validates its path and calls it. `gen-static-ical.sh`
(copied from hebcal-web) runs the command, then fetches the Chabad
special-dates calendar from this service's own `/v3/` on port 8082.
hebcal-devops' `etc/cron.d/hebcal-api-ical` is meant to run it weekly on the
api hosts and rsync `ical/` into `/var/www/html/ical`; production has not cut
over yet, and the Node dl hosts still build the feeds.

The pieces: `internal/service/ical` is the **event adapter** the holiday
feeds share ("The holiday feeds"); `internal/service/staticcal`
holds the feed table (`Feeds`, from both JSON configs), `Window`, `Render` and
Kindness; `readings.Client.TorahMemos` is readings-svc's `/torahMemo`. The
`.csv` side goes through `csv.Render`, so the static CSVs share the `/v4/`
CSVs' deliberate format (BOM, minimal quoting, commas kept).

**Measured (November 2026):** rendered for 2026-11-11, 2027-03-25 (Adar II)
and 2029-09-03 against a local hebcal-web with the same `today`, every one of
the 36 holiday, Torah, Hebrew-date and learning `.ics` files is
byte-identical, `DTSTAMP` and folding included; every `.csv` agrees field for
field once production's comma rewriting is undone. `staticcal.TestGolden`
pins 14 of them (gzipped goldens and captured readings-svc answers in
`testdata/`, regenerated by `tools/static-ical-goldens.mjs`).

Things that are not obvious:

- **UIDs hash `@hebcal/core`'s `getDesc()`**, which is hebcal-go's
  `event.Desc` with one-m Tamuz ("Pesach II", "Omer 7", "Yizkor", "Parashat
  Re'eh" -- never a rendered title, whose apostrophes are typographic), except
  as `ical.uidDesc` spells out: a Hebrew date as "30 Av 5786"
  (`model.HDateString`); a learning reading as
  hebcal-go's `Basename()` ("Bava Batra 21", without the "Yerushalmi"
  `Render` adds), minus the verse range for Tanakh Yomi ("Daniel Seder 1");
  a readings-svc row as `title_orig` or `title` (`Item.Desc()`). Chofetz
  Chaim's combined event has its own, `hebcal-<date>-chofetz-chaim`.
- **The Torah memo is matched by Gregorian date and that same description.**
  readings-svc's `/torahMemo` runs `makeTorahMemoText()` over `calendar({sedrot,
  il})`; one call per feed covers its window. A row dated outside the range
  asked for is an error.
- **Parsha summaries are `MEMO:<parsha>` in github.com/hebcal/locales**,
  English or Hebrew, the halves of a doubled portion joined with
  `"\n "` -- and only the seven real pairs: "Lech-Lecha" is one portion
  (`download.ParshaSummary`). Because the same `MEMO:` namespace holds the
  holiday descriptions, `download.HolidayDescription` returns nothing for a
  parsha, as `@hebcal/rest-api` 7.3.3 does; without that guard the summary's
  first sentence would land in every `/v4/` CSV parsha row.
- **Chofetz Chaim's title is the full render**, which for the preface adds
  the Hebrew words a reading begins and ends with. readings-svc gives only the
  brief title, so `chofetzChaimTitle` takes them from the end of the memo,
  where they follow the Latin-script name and pages.
- **A Hebrew date links nowhere** (the converter URL hebcal-go has is not
  used), and the description of every other event ends with its tracked link
  -- `uc=ical-<file>` on hebcal.com pages, `utm_*` on Sefaria
  (`download.EventLink` with medium `icalendar`, shared with the PDF).
- **Window and stamps:** 90 days back to `round(365.25 * years) - 60` days
  ahead of the local date; every file's `DTSTAMP` and mtime is the minute the
  run began. The server's `Last-Modified` is that mtime -- of the `.br`/`.gz`
  copy when one is sent, which hebcal-web's koa-send also used, and which the
  generator stamps identically.
- **Precompressed copies win over on-the-fly encoding**, as with koa-send: a
  client offering `zstd, br` gets the `.br` file. A client offering neither
  `br` nor `gzip` gets the file through the middleware like any other body.
  The 304 is `If-Modified-Since` only (no ETag, so any `If-None-Match` or
  request `no-cache` gets a 200), and a 404 carries no Cache-Control.

Deliberate divergences:

- **A failing feed does not stop the run.** hebcal-web's script dies on the
  first error (a learning cycle that ends inside a window -- Dirshu Daf
  HaYomi B'Halacha in 2029 -- throws "Events can not be empty"); here it is
  logged, that feed keeps its last files, the rest are written, and the exit
  status is 1. A readings-svc series with no rows is an error for the same
  reason: an older sidecar ignores a series it does not know.
- **Files are written through a temporary name and renamed**, and compressed
  in-process (brotli quality 11, gzip -9) rather than by spawning the CLIs, so
  the server never reads half a file. `-nocompress` also deletes stale `.br`
  and `.gz` copies, which hebcal-web left to be served in place of the new
  file. gen-static-ical.sh still uses the `brotli`/`gzip` CLIs for the Chabad
  calendar (hence `brotli` in web-api.yaml), and its `curl` has `--fail`, so
  an error page is never published as a calendar.
- **Kindness a Day** is the same events and UIDs, but written by
  `pkg/icalendar`: its preamble gains `X-LOTUS-CHARSET` and
  `REFRESH-INTERVAL`, and `CLASS:PUBLIC` precedes `DESCRIPTION` rather than
  following it.

## The holiday feeds (`/v4/…ics`)

`internal/service/ical` and `internal/handler/ics.go` serve
`download.hebcal.com/v4/<data>/<name>.ics` and its two legacy spellings, `/v2/h/`
and the classic `/hebcal/index.cgi/` and `/export/` query strings -- the
holiday calendar subscribed to or saved from the website. It is a port of
`hebcalDownload()` (`src/hebcal-download.js`), `createMemo()` and
`makeIcalEvents()` (`src/icalCommon.js`), `makeKiddushLevanaMemo()` and
`makeIcalOpts()`, and it shares everything up to the event list with the PDF and
CSV: `download.Decode`, `ParamsFromMessage`, `NumYears` and
`GenerateCalEvents`. Like them it goes through the download concurrency
limiter, holds a 14-day `Cache-Control`, and answers `304` from its ETag before
generating anything.

**Measured, not assumed.** `tools/ics-sweep.sh tools/ics-cases.txt` renders
about 50 URLs -- every locale family, Israel, lat/long locations (Sydney, and
Tromsø inside the Arctic Circle), Hebrew years and months,
daily learning, Omer with a location, titles and colours, tracking parameters --
in all three URL shapes against a local hebcal-web, and a second sweep over
nineteen years from 1760 to 2450 on both schedules diffed every UID. Everything
is byte-identical except the cases below. `internal/service/ical`'s `TestGolden`
pins twelve of them (`tools/ics-goldens.sh` regenerates the goldens); run the
sweep again before changing anything in the package. The Torah text comes from
`tools/readings-stub.mjs`, which runs hebcal-web's own `makeTorahMemoText()`
behind a unix socket, so the comparison needs no readings-svc.

Things that are not obvious:

- **A /v4/ URL's own query string is laid over what its payload says**, and a
  legacy URL's query string *is* the request. The options the `Download`
  message has no room for -- `title`, `caldesc`, `color`, `relcalid`,
  `publishedTTL`, `emoji`, `utm_*` -- come from that effective query
  (`download.DecodeICS` returns it as `ICSQuery`). Whether the feed is a file to
  save is the same query's `subscribe`: any value makes it inline, but only `1`
  lets it name itself.
- **Titles follow `getCalendarTitle()`, which is not the PDF's.** A multi-year
  feed is "Hebcal Diaspora 2026-2032" (the year and `ny`), a Hebrew year or an
  empty calendar the year alone, anything else the span of its events; a
  subscription has no dates. The location is named by its *short* name, taken
  from the location rather than from the typeahead text (`Sydney, Australia` is
  "Sydney"). A subscription with no location and no `title=` names itself from
  what it subscribes to -- "Hebcal Israel - Torah Readings" -- and describes
  itself in `X-WR-CALDESC` when it leaves out the major holidays; the list
  order, and an empty entry for Shemirat HaLashon, are the website's
  (`download.ICSQuery.OptionNames`).
- **The tracking campaign is `ical-` and the title's slug**, built from the
  plain-ASCII geonames name as the PDF's is. A `utm_campaign` that does not start
  with `ical-` or `pdf-` makes hebcal.com links carry `us=ical&um=icalendar` as
  well as `uc=` (`download.EventLinkFrom`).
- **Timed events** (candle lighting, Havdalah, fast start and end, Chanukah
  candles, Kiddush Levana) start in the location's zone (`DTSTART;TZID=`), carry
  its short name as `LOCATION` and its position as `GEO`, and take the location
  into their UID: `hebcal-<date>-<murmur32 of the untranslated description>-` plus
  the geonameid, the ZIP code, or the anchor of the *full* location name
  (`Params.Place`; the short name would give another UID). `GEO` is the
  coordinates as the request spelled them -- a /v4/ URL stores them as a 32-bit
  float, so `40.7` reads back as `40.70000076293945` there and nowhere else.
  Candle lighting alone has an alarm, ten minutes before.
- **The Omer's alarm is at nightfall of the evening it begins** when the
  calendar has a location: half an hour before sunset on a Friday, 8.5° on a
  Saturday, dusk otherwise, as an absolute `TRIGGER;VALUE=DATE-TIME`. The event
  is then named for the location -- `hebcal-omer-<Hebrew year>-day<n>-<geoid>`,
  the Hebrew year despite what the Gregorian date of the evening says, and only
  for a geonames or ZIP location -- and gains its `LOCATION`. **On a night
  with no such time** (a high-latitude summer, where the sun never sinks
  8.5° or to civil dusk: Stockholm and Tallinn in June) a calendar with
  candle-lighting falls back to tzeit at 7.083° without elevation, still
  absolute, but the event keeps its plain UID and has no `LOCATION`; only
  where that fails too (a polar summer) is the alarm the generic
  `-P0DT3H30M0S`. `TestOmerAlarmFallsBackToTzeit`.
- **A candle-lighting or Havdalah memo is the event's own and nothing else**,
  with no link. **Kiddush Levana's** is four labelled lines in the calendar's
  language -- the dates by the Intl rules of each locale, which Go has no CLDR
  for, so `tools/dump-kl-dates.mjs` writes them out (`kldates.go`; it uses the
  plain space `format()` writes where `formatToParts()` has a narrow no-break
  one) -- and, on the Maharil line, "(adjusted to 4:22pm Candle lighting)" when
  hebcal-go moved the event (`KiddushLevanaEvent.AdjustedTo`, hebcal-go 0.27.1+),
  so that line can differ from hebcal-web's when the two zmanim libraries put
  alot hashachar a few seconds apart.
- **The Hebrew half of an `lg=ah`/`sh` readings-svc row has nikud.** An
  item's `hebrew` field is the classic API's, rendered without it; the title
  the feed appends is the brief Hebrew one ("חֲזָרָה מִשְׁנָה בְּרוּרָה"). So for
  those two locales `FetchItems` asks for the same rows in `he` as well and
  takes their titles -- one more sidecar request, only for those calendars.
  The truncation notice, which has no Hebrew name, repeats its English one
  after the " / ".
- **A calendar of only readings-svc rows names its campaign from their dates**
  (`ical-diaspora-2026-2027`), as its title does: `campaignName` spans the
  fetched rows as well as hebcal-go's events, of which there are none.
- **A holiday with no Hebrew description gets its English one** in a Hebrew
  feed, as in the website's `localizedHolidayDescription()`; the static feeds'
  guard against it was wrong, and removing it changed nothing they publish.
- **The Torah text is fetched per (Hebrew year, schedule)**, once however many
  feeds ask: `Service.torahMemos` finds the years that hold an event that can
  have a reading (not a timed one, not the Omer, Mevarchim, Hebrew dates, or
  learning), asks `/torahMemo` for each through an LRU with `singleflight`, and
  fetches nothing for a feed with no such event. A readings-svc that does not
  answer is a 503 with `Retry-After` and none configured a 501, never a feed
  with its readings missing (`ical.ReadingsError`). **Deploy readings-svc first**:
  it answers an unknown route 404, which the client rightly takes for an error.
- **A subscription is cut at 2399 events.** Past that, what is older than twelve
  weeks goes; if that is not enough it is truncated and ends with a "Hebcal
  calendar feed truncated" event the day after the last. Only a cut feed depends
  on the week, so only then does the ETag gain the Sunday (a second check
  after generating, like the original's); a file to save is never cut.
  `Service.Limit` counts every event, the readings-svc rows included.
- **An empty calendar is a placeholder, not an error** (a 400 for the PDF, a
  header row for the CSV): one private "Calendar contains no events" event
  dated today, a UID hashing the request's own URL (x64 128-bit murmur, where
  production uses the x86 variant -- it changes daily anyway), no
  `REFRESH-INTERVAL`, `max-age=86400` and no ETag.
- **Errors carry no `Cache-Control`**, except the 410 for a year outside the
  narrow range. hebcal-web's dispatcher sets the 14 days before the handler has
  decided whether the request is any good, and some of its errors keep it; the
  PDF and CSV drop it, and so does this.
- **There is no `Access-Control-Allow-Origin`**: nothing fetches an `.ics`
  cross-origin, and hebcal-web sends none.
- **`min` means "not off" on a legacy feed URL.** For a calendar of major
  holidays the minor ones come along unless `min` says `off` or `0`, where the
  PDF and CSV want it to say `on`: the website's own form placeholder
  (`min=...`) is still in subscribers' URLs, and without this their calendars
  lose Tu BiShvat and the other minor holidays. `download.DecodeICS` sets the
  message's `minor` flag for it, on the legacy shapes only.
- **Yom Kippur Katan has no emoji**, deliberately: hebcal-go's event carries
  none and this service adds none.

Known differences from production (every other line of the sweeps is
identical):

1. Minutes of zmanim, as everywhere (noaa-go against `@hebcal/core`): a
   `DTSTART`, the Omer alarm, a candle lighting inside a Kiddush Levana
   description. The UID does not change, so the event moves a minute.
2. Rosh Hashana's year is gematriya in Hebrew ("תשפ״ז"), a number in
   hebcal-web -- deliberate, as in the CSV and the PDF.
3. A legacy URL that names both a `year` and a `start`/`end` renders the year
   (the form its `/v4/` equivalent takes); hebcal-web renders the range. The
   website sends one or the other.
4. Hebrew-month mode (`mm=1`/`mm=2`) is ignored on purpose
   (`download.IgnoreMonthMode`, also applied to the CSV): it lays out a printed
   page, and a list of events has no months to count. hebcal-web honours it,
   appending the next Tishrei and a "Nov 10"-style Gregorian-date event on
   each day when `d=on`/`D=on`; here the request is the same feed as one with
   `mm=0`, so those events cannot appear, even from a `/v4/` URL that has the
   setting baked into its protobuf.
5. `nh=on` is understood only in the classic `/hebcal/index.cgi/` form, and
   `v=now` is served here though hebcal-web answers it 404.
6. Yom Kippur Katan's memo names the month with a typographic apostrophe
   ("Rosh Chodesh Sh’vat"), as hebcal-go 0.27.1 intends; hebcal-web writes a
   straight one. It applies to the CSV and the static `/ical/` feeds too, whose
   `yom-kippur-katan` goldens were edited by hand to match (the folded lines
   re-wrapped by the renderer) rather than regenerated from hebcal-web.
7. `lg=ah` and `lg=sh` Mevarchim summaries: hebcal-web appends "מְבָרְכִים חוֹדֶשׁ
   Teves" (the month left in English, a side effect of when the event
   translates it); this service writes the month in Hebrew ("טֵבֵת"), as for the
   CSV. Only the title differs; the UID does not.
8. Yom Kippur Katan has no emoji here (intentional); hebcal-web writes ✡️.
9. In a pre-1900 year, a zone's local mean time is not a whole number of
   minutes, so a Kiddush Levana time can be a minute off in Jerusalem.
10. `lg=ah` and `lg=sh` titles containing a comma ("Hakdamah 28, 33, 34",
   "Day 202: P107, P113"): hebcal-web escapes the transliteration but appends
   the Hebrew after escaping, so its commas go out bare; here the whole
   `SUMMARY` is escaped, as RFC 5545 requires. Deliberate.

## The readings-svc sidecar

What this service cannot do in Go comes from
[readings-svc](https://github.com/hebcal/readings-svc), a small Node.js process
in a sibling checkout (`../readings-svc`) that wraps the mature `@hebcal`
packages and answers HTTP **over a unix domain socket**
(`-readings-socket`, default `/run/hebcal/readings-svc.sock`):

- `/leyning` — Torah readings, including triennial, for `/shabbat`;
- `/learning` — the eight daily-learning series with no Go schedule, for the PDF
  calendars and the static `/ical/` feeds;
- `/torahMemo` — the finished Torah/Haftarah text of every reading in a date
  range, for the static feeds' descriptions (see "The static calendars");
- `/shabbatTorahReading` — one Shabbat's reading summary, for the MCP
  `torah-portion` tool (see "The MCP server").

`internal/repository/readings` is the whole client: the socket transport, the
`Item` type, the LRU, and the two request builders. Both callers -- `/shabbat`
and `internal/service/download/learning.go` -- share one `*readings.Client`, so
there is one socket, one connection pool and one place that knows the
transport. Keep it that way, the same rule the `httpx` note above states.

`/leyning` and `/learning` answer in `@hebcal/rest-api`'s classic-API item
shape: `eventToClassicApiObject()` has already run, so each item's `leyning`
is exactly the object the `/shabbat` response wants, and it is passed through
as `json.RawMessage` with its key order intact; a learning item's `category`
names its series.

Three things about the client are load-bearing:

- **`/leyning` takes no locale at all**, the same choice `holidayPdf.js`
  makes and for the same reason. Readings are locale-invariant -- book names,
  verse references and the `| Shabbat Shekalim` reasons come out of
  @hebcal/leyning in English whatever the locale (measured: `lg=he`, `a`, `es`
  produce byte-identical `leyning` objects) -- so a locale would only change
  each item's `title`, which nothing reads. hebcal-go's events are matched to
  the sidecar's items by the untranslated event description instead:
  `Item.Desc()` is `title_orig`, falling back to `title` when the classic API
  omitted it, which it does exactly when the two are equal. That is why the
  match would survive a localized sidecar even so -- but the locale is gone
  from `leyning.js` rather than left as something to reason about. A localized
  `/shabbat` request therefore gets English readings, which is what hebcal-web
  does too. The parsha is matched differently -- it is the one item on the day
  whose category is `parashat` -- because `eventToClassicApiObject` tests the
  flag mask for equality, and so does `itemLeyning`. `/learning` does still
  take `lg`, because there the titles *are* the content.
- **The LRU is keyed by `(date, il)` and nothing else.** Every city in Israel
  reads the same portion on a given day, as does every city in the Diaspora, so
  a week's worth of dates is shared by all the requests for that week whatever
  the location. `singleflight` collapses the concurrent misses.
- **A macOS socket path must stay under ~104 bytes.** `sun_path` is that long,
  and `t.TempDir()` returns a `/var/folders/...` path that overflows it, so
  `internal/repository/readings/readingstest` anchors its socket under `/tmp`
  (as `pkg/geoip`'s test already did). readings-svc itself takes `--socket` for
  the same reason: Debian puts the socket in `/run`, which macOS has no
  equivalent of.

`readingstest.Serve` starts an `httptest.Server` on a unix socket, so the PDF
and `/shabbat` tests exercise the real transport rather than a TCP substitute.
`internal/handler/testdata/leyning/readings.json` holds one day of captured
classic-API items per `YYYY-MM-DD|<0 or 1 for Israel>` key; regenerate it from a
running sidecar when a golden needs a new date.

## The MCP server (`/mcp`)

`www.hebcal.com/mcp` is a Model Context Protocol server, ported from the
Node.js `../hebcal-mcp` (`@hebcal/mcp`). It exposes **seven tools**, all pure
calendar computations, over a
**stateless streamable-HTTP** transport (POST `/mcp`; GET/DELETE → 405). No
stdio CLI is ported: the deployed remote endpoint is the only target.

Library: the **official `github.com/modelcontextprotocol/go-sdk`** (the Go
counterpart of the `@modelcontextprotocol/sdk` the Node version uses, so the
port is 1:1 — `McpServer`/`registerTool` → `mcp.NewServer` + generic
`mcp.AddTool`, tool input schemas generated from Go structs via `json` +
`jsonschema:"…description…"` tags).

| file | role |
|---|---|
| `internal/service/mcp/` | the seven tools; `Handler()` builds the `*mcpsdk.Server` and wraps it stateless. Package `mcp`; the SDK is aliased `mcpsdk` |
| `internal/handler/mcp.go` | mounts `s.MCP` at `/mcp` through the shared middleware (access log + metrics) |

**Almost everything is computed in-process** with the same libraries the JSON
APIs use — `hdate` (conversions, `GetYahrzeit`, leap year), `internal/model`
(`GematriyaDate`, `HDateString`, `HDMonthNameEn`), `hebcal-go` `sedra` /
`event` / `hebcal` (parsha, holidays-for-year, candle-lighting + `TimedEvent`
`LinkedEvent`), and `learning/dafyomi` (Daf Yomi via `dafyomi.New` +
`NewDafYomiEvent`, whose `Render("en")` is already the brief form the Node
`renderBrief` produced). hebcal-go's `dafYomiEvent.Render` has **no** "Daf
Yomi:" prefix, unlike @hebcal/core's `render()`.

**The one thing hebcal-go cannot do is the `torah-portion` reading summary**
(`getLeyningForParshaHaShavua().summary`, e.g. the merged special-Shabbat form
`"Leviticus 1:1-5:26; Deuteronomy 25:17-19"`). It is **not** in the classic-API
`leyning` object and not reconstructable without re-porting
`makeSummaryFromParts`. So readings-svc has a dedicated **`/shabbatTorahReading`**
route (`shabbatTorahReading(url)` in `leyning.js`): takes `date` + `i`, and returns
the **whole `getLeyningForParshaHaShavua()` object verbatim** (`name`,
`summary`, `fullkriyah`, `haftara`, …). When a chag displaces the weekly
parsha, there is no ParshaEvent, so it returns the **holiday's** reading
instead, via `getLeyningForHoliday(getHolidaysOnDate(date)[0])` — the same
@hebcal/leyning shape, whose `name.en` is the chag reading label ("Shmini
Atzeret (on Shabbat)"). It is separate from `/leyning`, whose classic-API shape
(built for `/shabbat`) omits `summary` and carries the triennial cycle this
does not. The Go side,
`readings.Client.ShabbatTorahReading(ctx, dateISO, il)`, decodes `name.en`,
`name.he` and `summary` out of that object (keyed on the Shabbat date, so the
chag lookup lands on the Saturday); it is its own request, **not** the
`(date,il)`-keyed `Leyning()` LRU, which `/shabbat` needs summary-free (adding
`summary` there would diverge `/shabbat` from production, which carries no such
key). `/shabbat` output is therefore untouched. torah-portion soft-depends on
the sidecar the way PDF daily-learning does: without it (nil client, or an
error) the chag branch falls back to the name hebcal-go's `sedra` gives the
holiday reading (`Parsha.Name` with `Chag` set, e.g. "Pesach Shabbat Chol
ha-Moed"), with no Hebrew-name or `Reading:` line, rather than the tool
failing.

**On a chag the Go tool deliberately says more than the original hebcal-mcp.**
`hebcal-mcp`'s `torahPortion` guards `Name in Hebrew:` and `Reading:` behind
`if (!parsha.chag)` -- it has a `ParshaEvent` only for a weekly parsha, so on a
chag it prints just the portion and the date read. The Go tool has the same
gap in hebcal-go, but the sidecar's `getLeyningForHoliday()` object already
carries `name.he` and `summary`, so the chag branch emits `Name in Hebrew:`
(from `name.he`) and `Reading:` (from `summary`) whenever the sidecar supplied
them. This is an intentional improvement, not a parity target.

The seven tools' output strings (markdown tables / labelled lines) match
hebcal-mcp's line for line, except the chag `torah-portion` lines above and
two accepted divergences: the gematriya ב-prefix on "Date in Hebrew letters"
(below), and a ~2-minute candle-lighting difference in `shabbat-times` for
Jerusalem, the noaa-go vs @hebcal/core sunset divergence. Go tests in
`internal/service/mcp` pin the tool outputs and exercise the real stateless
streamable-HTTP transport (`tools/list` returns 7; a GET is 405).

Wiring: `main.go` sets `app.MCP = mcp.Handler(app.Readings)`; `server.go`'s
`Routes()` mounts `mux.HandleFunc("/mcp", mw.Serve(s.mcp))`. Tool names:
`convert-gregorian-to-hebrew`, `convert-hebrew-to-gregorian`, `yahrzeit`,
`torah-portion`, `jewish-holidays-year`, `daf-yomi`, `shabbat-times`. Parse and
range errors are returned as normal (non-`IsError`) text content, matching the
Node `errorCard`, so the model sees them. `shabbat-times` validates
lat/long range before `zmanim.NewLocation`, which **panics** out of range.

**Accepted divergence — the "Date in Hebrew letters" gematriya prefix.**
hebcal-mcp uses `@hebcal/hdate`'s `renderGematriya()`, which writes the month
with no ב preposition (`"י״ד תַּמּוּז תשפ״ד"`). `model.GematriyaDate` (shared with
`/converter`, which matches hebcal-web) writes the prefixed form
(`"י״ד בְּתַמּוּז תשפ״ד"`). We reuse `model.GematriyaDate` and accept the one-word
difference rather than carry a second gematriya formatter. Month **transliteration**
matches, because `@hebcal/hdate` and `model` both spell Tamuz with one m and
`Sh'vat` with a typewriter apostrophe (`render()`'s smartened `Sh’vat` is
reproduced with `jsutil.SmartApostrophe` in the yahrzeit day-and-month column).

## Daily learning

Schedules are not selected through the four dedicated `CalOptions` booleans but
through its generic `DailyLearning []string`, resolved against hebcal-go's
`dailylearning` registry. The registry is populated by importing
`github.com/hebcal/learning` **for its side effects** — each schedule registers
itself in an `init()`. `internal/service/download/params.go` carries that blank import, next to the list it
feeds; dropping it silently reduces the service to the four hard-wired series.

`learningSchedules` in `params.go` maps each protobuf field to a registry name.
`unsupportedSeries` lists the eight with no schedule at all, and `fallback.go`
fetches those from readings-svc's `/learning` and merges them.
`applyV2DailyLearning` in `v2.go` maps the legacy URLs' query codes onto the
same protobuf fields, mirroring hebcal-web's `dailyLearningConfig.json`. Those
four lists move together, and a fifth is in a different repository:
`queryToDailyLearningName` in readings-svc's `learning.js`, which resolves the
same query codes (`dcc`, `dshl`, `dsm`, `dksa`, `ayd`, `ddh`, `ahsy`, `dtn`) to
@hebcal/learning schedule names.

**readings-svc silently ignores a query code it does not know**, so a series
added here must be deployed there first; against an older sidecar the series
renders without a single row and with no error.

The fetch goes over the readings-svc unix domain socket (`-readings-socket`,
default `/run/hebcal/readings-svc.sock`) -- see "The readings-svc sidecar"
below.

The two failure modes are deliberately different codes, carried out of the
service as one `UnsupportedSeriesError` and split by `writeDownloadError`:
**501** when no socket is configured, because retrying cannot help, and
**503** with `Retry-After` when a configured readings-svc does not answer. In the
default configuration 501 is unreachable. Keep the two
together: anything the learning package gains moves from the second to the
first. `learning_test.go` asserts every name is registered and produces events.

Note that Pirkei Avot is read only between Pesach and Rosh Hashana, so a test
sampling a winter month finds nothing for it.

## Upstream

Fixes that belong in a library go upstream rather than being worked around
here; `git log` and the libraries' release notes have the history. What is
still useful to know:

- **github.com/hebcal/locales** carries hebcal-web's own `po/` catalogs (the
  zman `TITLE:`/`MEMO:` keys, the Hebrew zman titles) over `@hebcal/core`'s,
  and the Sefaria parsha summaries as `MEMO:<parsha>`. A string edited in
  hebcal-locales reaches this service only through a new release of the Go
  module.
- **github.com/hebcal/learning** gives each schedule event `URL()`,
  `RenderBrief` and `Memo`. The empty URLs for Schottenstein Yerushalmi and
  multi-reading Rambam 3-chapter days are deliberate and match @hebcal/core.
- **`CalOptions.SuppressHavdalah`** exists because hebcal-go reads
  `HavdalahMins == 0` as "unset, use the default tzeit", where @hebcal/core's
  nullable `havdalahMins === 0` means "no Havdalah". `params.go` sets it
  whenever `M=off` and `m=0`.
- **`CalOptions.CandleLightingAtSunset`** is `b=0`, for the same reason: a
  zero `CandleLightingMins` means "use the default".
- `tools/dump-urls.mjs` dumps @hebcal/core's event URLs for a range of years,
  the reference half of a URL comparison against hebcal-go. A single year
  does not exercise everything -- Yom Kippur Katan, a common year's single
  Adar and the Israel schedule each need a different part of 1990-2089.

## Option derivations in `params.go`

`params.go` turns the protobuf into `hebcal.CalOptions`, and a dozen
derivations happen on the way that no field-by-field reading of the message
reveals. Each has a regression test in `calendar_port_test.go`.

- **A location implies candle-lighting**, regardless of `c`.
  `setLocation` / the geoPos branch in `applyLocation`.
- **An Israel location forces `il` and its own candle-lighting offset**
  (`locationDefaultCandleMins`, via hebcal-go's `IsraelCandleLightingMins`: 20
  minutes, or 40/30 for Jerusalem/Haifa/Zikhron Yaakov), unless the request set a non-default offset. `applyIsraelCandleMins`.
- **Candle-lighting is switched off for early years** — Gregorian before 1900,
  Hebrew before 5661 — even with a location. End of `DecodeParams`.
- **`lg=ah` / `lg=sh` set `appendHebrewToSubject`** — each event shows the
  transliteration *and* its Hebrew name (`Params.AppendHebrew`, computed into
  `Event.HebrewBrief`, drawn by `appendHebrew` in render.go, which fits it
  inline or wraps it).
- **Alternate dates go on the day-number line** for Gregorian-month calendars —
  the HEBREW_DATE event's brief form (no year, via `altDateBrief`) is drawn by
  `renderAltDateOnLine` and the event is skipped as a row (`Event.AltDate`).
- **The 12/24-hour clock default follows the country**, not a blanket 12-hour:
  only the dozen countries `event.Uses12HourClock` names (never Israel) default to
  12-hour. Ghana draws `17:49`, not `5:49p`. `use12Hour`.
- **`Tammuz` → `Tamuz` in event subjects**, not only month titles
  (`fixMonthSpelling` in events.go).
- **Unknown location → 404, out-of-range year → 410** (`notFoundError`,
  `outOfRangeError`), rather than a blanket 400. The 410 range check also stops
  a far-future request before it reaches the generator. The range
  (`model.YearIsSupported`, shared with `/shabbat` and the short-URL redirects)
  is Hebrew 5513-6260 / Gregorian 1752-2499, and applies to both dates of a
  start/end range and to the `/holidays/` calendars as well.
- **Explicit `numYears` is capped at 10** (`maxNumYears`).
- **A month in a Hebrew-year calendar is a Hebrew month** (Nisan is 1, 1-12
  only, so Adar II cannot be named), set as an explicit `Start`/`End` because
  hebcal-go's `CalOptions.Month` is Gregorian. The range runs from the 1st
  through the 1st of the following month, inclusive -- one day more than the
  month, as `@hebcal/core` has it. A feed's title then names the year alone
  (`calendarTitle` checks `Start` as well as `Month`).
- **Havdalah is suppressed when `M=off` and `m=0`** — the default for a download
  that did not set a Havdalah time. A zero `m` means "no Havdalah", but
  hebcal-go reads a zero `HavdalahMins` as "use the default tzeit" and would
  draw one, so `params.go` sets `CalOptions.SuppressHavdalah`. A
  non-default offset (`m>0`) or tzeit (`M=on`) keeps it.

Also derived: the Shabbat Mevarchim implication, `hebrewMonths` /
`gematriyaNumerals` from `mm`, and the category filters.

- **Hebrew-month mode alternate dates.** In `mm=1`/`mm=2` with `d=on`/`D=on`
  the day-number line carries the *Gregorian* date, which hebcal-go does not
  generate as an event. `addGregorianAltDates` in events.go synthesizes them
  (every day in range for `d=on`, event-days only for `D=on`), and
  `gregorianAltText` renders the "MMM D" / "D MMM" form. In an RTL Hebrew
  calendar a number-first date like "25 אוק" is prefixed with a right-to-left
  mark, because `x/text/bidi` resolves a number-first string to an LTR
  paragraph and would otherwise draw the month to the right of the digits.
  params.go only sets `AddHebrewDates` in Gregorian-month mode, so the two
  mechanisms never both fire.

The far-future `dropDailyLearningOutsideRange` is moot for PDF: the 410 range
check rejects the whole request first.

## Testing strategy: do not trust a hand-written probe

Bugs in this service have mostly been found by rendering calendars for a varied
set of real-world URLs and looking at the result, not by the unit tests and not
by the hand-picked calendars used during development, because those all happen
to set the same handful of options. When changing rendering, vary the location
(Israel, a 24-hour country, a ZIP, a lat/long), the locale (including `ah`/`sh`
and Hebrew), `mm=1`/`mm=2`, alternate dates and daily learning.

A failure of method worth naming, because it produced a confident and wrong
statement: a grep for candle-lighting times used `[0-9]{1,2}:[0-9]{2}[ap]`,
which finds nothing in a **24-hour** locale. Ghana has no am/pm, the pattern
matched zero lines in a file containing 133 times, and that zero was reported
as "the document contains no candle-lighting time".

So: before asserting something about a calendar, check the claim with a tool
that understands PDFs -- `pdffonts`, `pdfinfo`, `pdftotext -bbox` -- rather
than a regex over extracted text.

## Where things stand

This service is live: the PDF, CSV, Yahrzeit (`/v3/` included), zmanim-feed,
JSON and MCP routes are all served from here. The static `/ical/` generator
is not yet in production; the Node dl hosts still build those feeds.

### Runtime requirements

- **Fonts** live in this repository (`fonts/Source_Sans_Pro/`,
  `fonts/Adobe_Hebrew/`) and `make install` copies them to `/var/www/fonts`.
  Missing fonts are not fatal: the two PDF routes answer 503 and the JSON
  APIs keep working. The two SQLite databases are passed with `-zips-db` and
  `-geonames-db` (`etc/hebcal-api.service`).
- **readings-svc** answers on `/run/hebcal/readings-svc.sock`; without it
  `/shabbat` answers 503 and any PDF naming one of the eight daily-learning
  series answers 503. Its systemd unit's `RuntimeDirectory=hebcal` creates
  the socket directory.
- **MySQL** for the saved Yahrzeit calendars: `/etc/hebcal-dot-com.ini` on the
  api host (installed by hebcal-devops' `cloud-config/web-api.yaml`, mode 0640
  root:www-data). Without it, `/v3/` answers 503 and Varnish serves stale
  copies; nothing else is affected.
- **Varnish** sends the PDF and CSV download URLs to **port 8082**
  (`download.hebcal.com/v4/`, `/v2/h/` and `/hebcal/index.cgi/`, and
  `www.hebcal.com/holidays/hebcal-*.pdf`). Nothing else under `/holidays/`
  is ours: the HTML pages there are hebcal-web's, and this service answers
  them 404. A 503 from here carries `Retry-After`.

### Links and campaigns

Every event's tracked link follows three rules that are invisible on a plain
Diaspora calendar, so check an Israel calendar and a location whose name has
punctuation in it:

- **`i=on` is set, not appended.** A holiday whose canonical URL already asks
  for the Israel schedule -- ben-gurion-day, family-day, herzl-day and the
  other Israel-only modern holidays -- comes out with one parameter:
  `/h/ben-gurion-day-2026?i=on`, never `?i=on&i=on`.
- **The parsha links set `i=on` before shortening.** The shortener spends it
  on the path and deletes the parameter, giving `/s/5786i/12?uc=…`, not
  `/s/5786i/12?i=on&uc=…`.
- **The campaign uses the location's ASCII name.** `campaignName()` builds the
  title with `preferAsciiName`, and `shortLocationName()` then takes the raw
  geonames **asciiname** rather than the display short name. So the document
  title and the `uc=` disagree on purpose: geonameid 2657896 renders "Hebcal
  Zürich 2026" and tags `pdf-zuerich-2026`, and 5128581 renders "Hebcal New
  York 2026" and tags `pdf-new-york-city-2026`. Deriving the campaign from the
  document title would give `pdf-z-rich-2026`. `Params.CityNameAscii` and
  `CampaignName` carry the distinction; a ZIP or lat/long location has no
  asciiname, so the display name stands in and `washington-dc` still comes out
  of ZIP 20001.

`urlSlug` hyphenates everything outside ASCII `[A-Za-z0-9_]`, collapses the
runs and trims the ends (matters for "St. Louis", "Washington, D.C" and the
`40°42′N 74°0′W` name).

github.com/hebcal/learning gives each schedule event a `URL()` method, so it
satisfies hebcal-go's `event.URLer` interface and the `event.URL(ev)`
call in `events.go` picks it up. Two schedules carry links on only some rows,
deliberately: **Schottenstein Yerushalmi** has no Sefaria mapping at all, and
**Rambam 3-chapters** has no link on days whose three chapters do not collapse
to a single reading (single-reading days keep it). `learning_test.go` asserts
every supported schedule but Schottenstein produces at least one URL.

### Daily-learning order

The daily-learning **rows sort into one slot, in the download form's order
with 929 first** (`dailyLearningConfig.json`, read through a JavaScript object
whose integer-like key iterates first). Only four of the thirteen schedules
have a flag of their own; the rest carry the generic `DAILY_LEARNING`, so
hebcal-go's `TimeOfDay()` keys on `DAILY_LEARNING` as well as the four named
flags, and `internal/service/pdf/events_test.go` pins the resulting order.

hebcal-go orders the series it generates by the `DailyLearning` list, which is
already in that order. The readings-svc series do not come after them, though:
Sefer HaMitzvot sits between Rambam and Yerushalmi, Daf Weekly and Pirkei Avot
between Dirshu Amud HaYomi and Arukh HaShulchan. So every format sorts a day by
`TimeOfDay` and then by `download.LearningOrder` of the event's category (an
item's `Category`), the table in `learning.go`; appending the fetched rows
after the generated ones was visible on any day carrying both kinds, and
decided which rows a subscription cut at 2399 events kept on its last day.

On a crowded day the last row overflows its cell, so a different order loses a
different link: a link-set difference between two calendars is not evidence
about the rows.

### Locales

Localized `/v4/` calendars translate the Hebrew-month subtitle per locale (es
"Jeshvan", de "Cheschwan"/"Tammus", fr "Tammouz"). The `/holidays/` calendars
take no locale at all (see above).

### Known limitations

- `hdate` spells Tamuz with two m's (worked around in `render.go` for month
  titles and `events.go` for event subjects, via `fixMonthSpelling`).
- A few timed events (three of 143 in 2028) can come out a minute off -- a
  zmanim question for hebcal-go and noaa-go, not a rendering one.

## Testing

`make test` (`go test -tags sqlite_fts5,sqlite_math_functions ./...`; the tags
are for `/complete`'s full-text queries, not for anything the PDF code does).
Tests needing the fonts look for `$FONT_DIR`, then a `fonts/` directory at the
repository root, and skip rather than fail when neither is there. The PDF tests
sit where the code does: `internal/service/pdf` for rendering, shaping, params
and links, `internal/service/holidaypdf` for the holiday URLs, and
`internal/handler/pdf_test.go` for the HTTP behaviour (status codes, cache
headers, the conditional request, the daily-learning failure modes).

They concentrate on what actually went wrong rather than on easy targets, and
several encode behaviour that looks wrong and is not — the two `links_test.go`
cases above especially. When a test looks like it is asserting a bug, check its
comment before changing it.

## Port provenance: Go symbol → JS source

Where each Go symbol came from. `src/*` is `hebcal-web`; `@hebcal/*` is the npm
package. This is the lineage that used to live in the doc comments. Add rows as
more code is ported.

**The PDF calendars** (`internal/service/pdf`, `internal/service/holidaypdf` and
the PDF routes in `internal/handler`) began as a port of hebcal-web's
`src/pdf.js`, `src/holidayPdf.js` and `src/hebcal-download.js`, which drew with
pdfkit and fontkit. That JavaScript implementation has been retired, so its
Go↔JS mapping is no longer kept here and the Go code is the only source of
truth; `git log` has the history. The tables below cover the code the PDF path
shares with the CSV and iCalendar feeds, which are still tracked against their
JavaScript sources.

### `internal/service/download/params.go` — `deserializeDownload.js` + parts of `makeDownloadProps.js`

| Go | JS source |
|---|---|
| `ParamsFromMessage`, `Params` struct | `deserializeDownload()` → the calendar-relevant parts of `makeDownloadProps.js`; writes straight to `hebcal.CalOptions` instead of a query map |
| `maxNumYears` (10) | `getNumYears()` in `src/calendar.js` |
| Hebrew-year month → `Start`/`End` | `startEndHebrew()` (`@hebcal/core` `getStartAndEnd.js`), with `makeHebcalOptions`' 1-12 month check |
| `decodeBase64` | Node `Buffer.from(s,'base64')` permissiveness |
| Shabbat Mevarchim implication (RC + specialShabbat + sedrot) | `src/calendar.js` sets the `SHABBAT_MEVARCHIM` query-mask bit |
| Gregorian vs Hebrew alt-date branch | `src/calendar.js` sets `addHebrewDates` only when `!hebrewMonths` |
| `SuppressHavdalah` when `M=off` & `m=0` | `deserializeDownload.js` `q.m = havdalahMins`; `@hebcal/core` `calendar.js` drops HavdalahEvent when `havdalahMins===0` |
| `defaultCandleMins` (18) | `DEFAULT_CANDLE_MINS` in `src/urlArgs.js` |
| `geonameIDCandleOffset` | `geonameIdCandleOffset` in `src/urlArgs.js` |
| `locationDefaultCandleMins` | `locationDefaultCandleMins()` in `src/urlArgs.js` |
| `applyIsraelCandleMins` | Israel branch of `src/calendar.js` |
| `setLocation` "location ⇒ candle-lighting" | `if (location) options.candlelighting = true` in `src/calendar.js` |
| `parseISODate` | `makeDownloadProps` date handling before `@hebcal/core` |
| `OutOfRangeError` / 410 | `hebcal-download.js` |
| `NotFoundError` / 404 | `getLocationFromQuery` |
| `applyLocation` | `getLocationFromQuery` + `@hebcal/geo-sqlite` |
| legacy `cityName` branch | `downloadHref2` sets `cityName` only alongside `geoPos` |
| `holidayMask`, `majorHolidayMask` | `optsToMask` / `getMaskFromQuery()` in `src/calendar.js`; hebcal-web never sets `noHolidays` (`maj=off` becomes `noMajor`, which `@hebcal/core` ignores), so `NoHolidays` must not be derived from `major` |
| `learningSchedules`, `dw`→`dafWeeklySunday` | `dailyLearningConfig.json` |
| `fastTimeFields` / `setFastTimes` | `deserializeDownload.js`'s fast fields + `floatToString()` (`toPrecision(6)`), then `getFastTimeOpts()` |
| `unsupportedSeries` | the eight series with no `github.com/hebcal/learning` schedule |

### `internal/service/download/v2.go` — `downloadHref2()` (`src/makeDownloadProps.js`)

Whole file = `parseV2Path()` (`src/app-download.js`) + `downloadHref2()`, minus
the base64 encode. `v2Query.on/off/empty/truthy/getInt/getFloat` ←
`makeDownloadProps.js` `on()` / `urlArgs.js` `off()` / `empty.js` `empty()` /
bare `if (q.x)` / `getInt()` / `Number.parseFloat()`. `primaryGeoKeys`,
`allGeoKeys`, `geoKeysToRemove`, `normalize` ← `urlArgs.js` lists +
`getGeoKeysToRemove()` + `urlArgsObj()`. `applyV2Location`'s default (deg/min)
branch ← `getLocationFromQuery` legacy branch, and is a **deliberate divergence
from the 301**. `applyV2DailyLearning` ← `downloadHref2`'s `dailyLearningConfig`
loop. The fast fields ← `downloadHref2`'s `getFastTimeOpts(q)` block; the
fast keys in `geoKeysToRemove("none")` ← `fastTimeKeys` in `getGeoKeysToRemove()`. `checkISODate` ← `isoDateStringToDate()` 400 guard.
`typeaheadZip` ← the `city-typeahead` → `zip` lines at the top of
`getLocationFromQuery` (`src/location.js`); the `a=on` → `lg=a` rewrite ←
`makeHebcalOptions`' `options.ashkenazi && empty(query.lg)` (`src/calendar.js`).

### `internal/service/download/cgi.go` — `makeHebcalOptions(query)` path

`makeHebcalOptions(query)` ≡ `ParamsFromMessage(downloadHref2(query))` (the
equivalence `v2.go` was measured to hold), so `ParseCGIPath` builds the same
`v2Query` and runs `DecodeV2`. `normalizeCGIQuery` ← the CGI half of
`fixup2`'s querystring rewriting (`;` separator, double-encoded `%3B`,
Latin-1 `unescape()`). `applyCGILegacyParams` ← `makeHebcalOptions`
pre-read rewrites (`nh=on` expansion, lowercase `m=on`, `v=now`).
`unescapeLatin1` ← JavaScript `unescape()`.

### `internal/service/download/querylog.go`

`MessageToQuery`, `learningQueryParams`, `queryBuilder` ← `deserializeDownload.js`
field-for-field, in `dailyLearningConfig.json` order. `formatFloat` ← JS
`String(number)`. `queryBuilder.set()` encoding ← `encodeURIComponent` (spaces
as `%20`, not `+`).

### `internal/service/download/learning.go`, `links.go`

`learningOrder` / `LearningOrder` ← the order of `dailyLearningConfig.json`
as hebcal-web's `options.dailyLearning` object iterates it (`929` first, an
integer-like key). The `lg=he` second fetch in `FetchItems` ← the
`IcalEvent` constructor's `ev.renderBrief('he')` for `appendHebrewToSubject`.
`fallbackSeries` codes ← readings-svc's documented codes (also hebcal-web's
`/hebcal` query params). `canonicalLearningURL` ← strips `@hebcal/rest-api`'s
`utm_*` tracking. `links.go` UTM defaults ← `appendIsraelAndTracking`.
`shortenSedrot`'s trim-to-`/s/` fallthrough ← `@hebcal/rest-api`. `appendParams`
ordering ← `URLSearchParams` emission order.

### `internal/service/csv/` — `src/csv.ts` (`@hebcal/rest-api`) + `src/parshaCommon.js`

`Header` ← `CSV_HEADER`. `category` ← `CATEGORY`. `eventToCSV` / `formatRow` ←
`eventToCsv()`. `formatTime` ← `reformatTimeStr(ev.eventTimeStr, ' PM', options)`.
`shouldRenderBrief` ← `shouldRenderBrief()` (`common.ts`). `eventCategories` ←
`getEventCategories()`. `holidayDescription` ← `getHolidayDescription(ev, true,
locale)`. `memo` ← `options.memo || ev.memo || getHolidayDescription() ||
linkedEvent.render()`. `ownMemo` ← `ev.memo`, which hebcal-go carries as `event.Memo`. `parshaMemo` ← `getCsvParshaMemo()`. `Render` ←
`eventsWithParshaToCsv()` + `byteOrderMark()` (now unconditional).
`numYears` ← `getNumYears()` + `maxNumYear` (`src/calendar.js`).
`learningToCSV` renders readings-svc's classic-API items.

### `pkg/icalendar/` — `@hebcal/icalendar` (hebcal-icalendar 7.0.6)

`FoldLine` / `writeFolded` ← `foldLine()` (`src/foldLine.ts`); `clusterStart`
← its `Intl.Segmenter` `containing()` lookup. `Event.write` ←
`IcalEvent.getLongLines()`; `Event.startEnd` ← the constructor's
DTSTART/DTEND half and `formatTimedStartEnd()`. `Calendar.writePreamble` ←
`makeIcalPreamble()`; `Calendar.Marshal` ← `icalEventsToString()`. `Escape` ←
`IcalEvent.escape()`; `FormatUTC` ← `makeDtstamp()`; `Date.String` ←
`formatYYYYMMDD()`. `Alarm` and the three `Alarm…` defaults ← `getAlarm()`.
`DefaultUID` ← `getUid()`; `Murmur32Hex` ← `murmur32HexSync`
(`@hebcal/murmurhash3`). `ZoneInfo.VTimezone` ← `getVtimezone()` (which
slices fixed line numbers; this finds the component). `Version` ← the
package version in the PRODID. **Not ported, on purpose** (see "pkg/icalendar
is a serializer"): the rest of the `IcalEvent` constructor -- render/brief,
emoji, `CATEGORY`, `appendHebrewToSubject`, `locationName` -- and
`appendTrackingToUrl`, `eventsToIcalendar`'s `getCalendarTitle` default.

### `internal/service/yahrzeit/` — `yahrzeitDownload.js`, `yahrzeitEvents.js`, `yahrzeitCommon.js`

| Go | JS source |
|---|---|
| `ParseRequest` | `fixup2` + `fixup3` + the `/v2` branch in `src/app-download.js` |
| `ParseQuery`, `unescapeComponent` | Koa's `ctx.query` (`node:querystring`), first value of a repeated key |
| `unescapeLatin1` | JavaScript `unescape()` |
| `decodeBase64ASCII` | `Buffer.from(s, 'base64').toString('ascii')` |
| `Calendar.ICS` | the `.ics` branch of `yahrzeitDownload()` + `makeIcalOpts()` (`src/urlArgs.js`) + the `IcalEvent` constructor's emoji/category choices |
| `Calendar.Variant` | the `attrs` `yahrzeitDownload()` gives `checkFreshETag()` |
| `Calendar.placeholder` | `makeDummyEvent()` |
| `googleLocation` | `makeLocation()` + `Location.legacyTzToTzid()` |
| `calendarTitle`, `anniversaryTypes` | `makeCalendarTitle()`, `getCalendarNames()`, `summarizeAnniversaryTypes()`, `getAnniversaryTypes()` |
| `numYears`, `includeNumber`, `entryIDs`, `entryFor`, `anniversaryType` | `getNumYears()`, `includeAnniversaryNumber()`, `getYahrzeitIds()`/`getMaxYahrzeitId()`, `getYahrzeitDetailForId()`, `getAnniversaryType()` |
| `rangeFor` | `getDateRange()` + `getDefaultStartYear()` |
| `makeEvents` | `makeYahrzeitEvents()` + `getEventsForId()` |
| `makeEvent`, `makeSubject`, `makeMemo` | `makeYahrzeitEvent()`, `makeSubject()`, `makeMemo()` |
| `makeReminder` | `makeReminderEvents()` + `getAlarmTime()` |
| `makeYizkorEvents`, `editMemo` | `makeYizkorEvents()`, `makeEditMemo()` |
| `candlesWhen` | `lightCandlesWhen()` (`src/common.js`) |
| `dayAndMonth` | `hd.render('en', false)` with a smart apostrophe |
| `hdate.GetYahrzeit` / `GetBirthdayOrAnniversary` | `yahrzeit()` / `birthdayOrAnniversary()` (`@hebcal/hdate`); in github.com/hebcal/hdate, not hebcal-go |

`Calendar.CSV` ← the `.csv` branch of `yahrzeitDownload()` (`reminder =
false`, no look-back filter, `Boolean(query.euro)`), with `eventsToCsv()`'s
columns (`Personal`, show-time-as 3) written by `csv.Document`.

`saved.go` `NewSavedRequest` ← `getDetailsFromDb()` (`yahrzeitDownload.js`)
+ `getYahrzeitDetailsFromDb()` (`yahrzeitCommon.js`) + the `{...details,
...ctx.request.query}` merge; `expandRow` ← its x/t/s loop; `SavedID` ←
`path.substring(4, 30)`. `internal/repository/yahrzeitdb` ← the SELECT, the
`downloaded` UPDATE and the `yahrzeit_atime` REPLACE there, and the pool in
`src/db.js`. `config.ReadINI` ← hebcal-web's `hebcal-dot-com.ini` reading
(`createBaseApp`).

`internal/handler/yahrzeit.go` ← the headers `yahrzeitDownload()` sets and
does not set (`Vary: User-Agent`, attachment on `dl=1`, no Cache-Control);
`yahrzeitCSV` ← its `.csv` branch (`text/x-csv`, ETag attrs without `sunday`).

### `internal/service/zmanim/feed.go` — `src/zmanimDownload.js` + `src/zmanimCommon.js`

`FeedKindFor` / `days` ← `zmanimIcalendar()`'s path tests and durations.
`NewFeed`'s today ← `nowInTimezone()`. `compute` / `computeTimes(…, klAtNight)`
← `getTimesForRange()` / `getTimes()` with `roundMinute` and the Kiddush
Levana alos/tzeit adjustment. `hasSolarTimes` ← `hasSolarTimes()`.
`timedEvents` ← `makeEvents()` + `makeTimedEvent()`; `allDayEvents` ←
`makeAllDayEvents()`. `place` ← `IcalEvent.getUid()`'s
`getGeoId() || makeAnchor(getName())`. `prodidLang` ← `makeIcalPreamble()`'s
`localeMap` after `lgToLocale`.
`zmanTitle` / `zmanMemo` ← `Locale.lookupTranslation(TITLE_PREFIX + zman, 'en')`
and its `MEMO_PREFIX` twin, over hebcal-web's `po/*.po` as carried by
github.com/hebcal/locales. `handler.zmanimFeed` ← the headers `zmanimIcalendar()`
sets (`CACHE_CONTROL_7DAYS` after the 400s; `checkFreshETag` with `dt`).

### `internal/service/ical/` — the rest of the `IcalEvent` constructor + `src/icalCommon.js`

`Event` ← the `IcalEvent` constructor's subject (`shouldRenderBrief`, emoji
prefix/suffix, `appendHebrewToSubject`), `CATEGORY` table (`categories`),
`locationName` for learning (`seriesNames`, the `@hebcal/learning` events'
`category`), CHAG → OOF, `getAlarm()`, `getUid()` (`uidDesc` ← `getDesc()`).
`Memo` ← `createMemo()`; `ownMemo` ← `ev.memo`; the Omer branch ←
`getTodayIs()` / `sefira()`, via hebcal-go's `TodayIs` / `Sefira`. `torahMemo` / `noReading` ← `makeTorahMemoText()` /
`HOLIDAY_IGNORE_MASK` (`src/torahMemo.js`, now run by readings-svc).
`LearningLink` ←
`appendTrackingToUrl()`. `download.ParshaSummary` ← `getParshaSummary()` +
`makeSummaryHtml()` (`src/parshaCommon.js`); `download.HolidayDescription` ←
`getHolidayDescription()` (`@hebcal/rest-api` 7.3.3, parsha guard and
`normalize()` included) and, with `firstSentence` false and the English
fallback, `localizedHolidayDescription()`. `download.EventLink` ←
`appendIsraelAndTracking()` + `shortenSedrotUrl()` (moved from `pdf/links.go`).

### `internal/service/ical/feed.go`, `kiddushlevana.go`, `torahmemo.go` -- `src/hebcal-download.js`, `src/icalCommon.js`

`Service.Parse` / `Generate` ← `hebcalDownload()` and `makeHebrewCalendar()`
(`download.NumYears` ← `getNumYears()`); `Service.Marshal` ← the rest of
`hebcalDownload()` and `icalEventsToString()`. `calendarTitle` ←
`getCalendarTitle()` (`@hebcal/rest-api`); `campaignName` ← its `campaignName()`;
`subscriptionTitle` ← `makeIcalOpts()` with `makeCalendarSubtitleFromOpts()` and
`queryDescription()` (`download.ICSQuery.OptionNames`, from `queryToName` and
`queryLongDescr` in `src/urlArgs.js`). `Service.Limit` ←
`limitIcsFeedLength()` and `makeTruncationNoticeEvent()`; `placeholder` ←
`makeDummyEvent()`. `Event`'s timed half and `Options.omerAlarm` ← the
`IcalEvent` constructor and `addLocationOmerAlarms()`, its 7.083° fallback ←
`makeOmerEvent()` (`@hebcal/core`); `kiddushLevanaMemo` ←
`makeKiddushLevanaMemo()` (`src/kiddushLevanaMemo.js`, dates by
`klDateFormats`); `Service.torahMemos` ← `makeTorahMemoText()`, run by
readings-svc. `handler.icsDownload` ← the headers `hebcalDownload()` sets and
the dispatcher's 14-day `Cache-Control`. `tools/readings-stub.mjs` runs the
last of those for comparing without a sidecar.

### `internal/service/staticcal/`, `cmd/hebcal-static-ical/` — `src/staticCalendars.js`, `src/makeStaticCalendars.js`, `src/kindness.js`

`Feeds` ← `src/staticCalendars.json` + the `downloadSlug` entries of
`src/dailyLearningConfig.json` (`learning`/`sidecar` ← `buildLearningCalendar`
+ `makeIcalOpts()`). `Window` ← `getStartAndEnd()`. `Render` ←
`buildRegularCalendar()` / `buildLearningCalendar()` + `renderCalendar()`
(`publishedTTL` P7D, `utmCampaign` `ical-<file>`). `renderChofetzChaim` ←
`ChofetzChaimShemiratHaLashonEvent` + `buildChofetzChaimCalendar()`.
`Kindness` ← `src/kindness.js` (+ `areyvut-kindness-a-day.json` as
`kindness.json`). `cmd/hebcal-static-ical` ← `makeStaticCalendars.js`'s
wrapper (`TODAY` to the minute, file mtimes, compression).
`gen-static-ical.sh` ← hebcal-web's, minus the two `node` steps.
`handler.staticICal` / `serveStatic` ← `sendStatic()` in `src/app-download.js`
(koa-send's `.br`/`.gz` selection and `Last-Modified`, `CACHE_CONTROL_7DAYS`,
`DOCUMENT_ROOT`) + `koa-conditional-get`. `readings.Client.TorahMemos` ← readings-svc's
`/torahMemo`.

### `internal/handler/download.go`

`writeDownloadError` ← hebcal-web's status mapping (shared by `csvDownload`,
whose `Content-Disposition` ← Koa's `ctx.attachment()`). `writeCalendarError` ←
`getLocationFromQuery`'s 404/400 split.

### `internal/handler/` (JSON routes)

| Go | JS source |
|---|---|
| `converter.go` date-less redirect | `src/converter.js`'s noCache/message check; HEAD parity and `cfg=xml` preservation are deliberate fixes |
| `converter.go` `redirectConverterNoCache` | `src/converter.js`; no per-request location because `/converter` has no IP geo |
| `geo.go` `/geo` route | `src/router.js` `/geo` (`getLocationFromQuery` serialized by Koa); `ctx.body = null` ⇒ 204 |
| `shabbat.go` default New York | hebcal-web default when no location given |
| `shabbat.go` locale validation | `makeHebcalOptions`'s `Locale.useLocale` throw; `/shabbat`-only |
| `shabbat.go` yto-after-empty-check | **deliberate divergence** — production filters before its check and 400s |
| `shabbat.go` `queryHour12` | `options.hour12 = !off(query.h12)` |
| `shabbat.go` `writeShabbatBody` JSONP | `jsonpBody()` in `src/common.js` |
| `zmanim.go` `checkMelacha` | `checkMelacha()` in `zmanim.js` |
| `shorturl.go` (whole file) | `src/shortUrlRedir.js`; `utmSourceFromRef` ← `utmSourceFromRef()`/`isTrustedHostname()` (`src/common.js`); the query string is a plain `url.Values`, so the `Location` sorts its parameters (`URLSearchParams` keeps insertion order), escapes `*` rather than `~`, and repeats a duplicated key rather than comma-joining it -- **deliberate**: Varnish `std.querysort()`s and strips `utm_*` before the destination is fetched, so none of it is observable; `yyyymmdd` ← `dayjs(hd.greg()).format('YYYYMMDD')` (proleptic); `numParshiot` ← `@hebcal/core`'s 54-entry `parshiot`. The GET/HEAD-only 405 is a **deliberate divergence**: hebcal-web redirects any method |

### `internal/service/converter/`

`parse.go` `ParseQuery` ← `parseConverterQuery()` (`src/converter.js`).
`parseStartAndEnd` ← `getStartAndEnd()` (`src/dateUtil.js`). `render.go`
`RenderCSV` / `FutureYearsHeb` ← `dateConverterCsv()` / `makeFutureYearsHeb()`
(`src/converter.js`).

### `internal/service/zmanim/` — `getZmanim` (`src/zman.js`)

`zmanim.go` `package` / `Times` ← `getZmanim` / `Times()`. The
`TIMES` / `TZEIT_TIMES` tables and their order ← `zman.js`. `RoundTime` ←
`@hebcal/core` `Zmanim.RoundTime`. `formatISOWithTimeZone`'s `null` ← `zman.js`
emitting `null` for a time that does not occur. `dates.go` `ParseMelachaDate` ←
JS `new Date(dateStr)` + the location-offset fixup (`im=1` branch). `StartAndEnd`
← `getStartAndEnd()` (`src/dateUtil.js`). `ExpiresTomorrow` ← `expires()` in
`zmanim.js`.
`moladTimes` ← `@hebcal/core` `Zmanim.getZmanMolad()`,
`getTchilasZmanKidushLevana3Days()`/`7Days()`,
`getSofZmanKidushLevanaBetweenMoldos()`, `getSofZmanKidushLevana15Days()`
(called with no `alos`/`tzais`, so unadjusted), ported to hebcal-go's
`zmanim` and `molad` packages; hebcal-web's `/zmanim` has no counterpart, so
this is an addition rather than a parity target. `ceilMinute` /
`floorMinute` ← `KiddushLevanaEvent.ts`'s rounding of the same times.

### `internal/service/shabbat/` — `shabbatApp` (`src/shabbat.js`)

| Go | JS source |
|---|---|
| package `shabbat` | `shabbatApp` in `src/shabbat.js` |
| `QueryDate` | `getTodayDate()`; `date`/`start` are added here |
| `QueryLang` | `makeHebcalOptions()`'s `a=on` → `lg=a` rewrite; `/shabbat`-only |
| `WeekRange` | `shabbatWeekRange` + `getStartAndEnd` (`src/dateUtil.js`) |
| `CalOptions` / `setCandleOptions` | `makeHebcalOptions()` precedence + `shabbatApp()` default |
| `locationDefaultCandleMins` | `locationDefaultCandleMins()` (`src/urlArgs.js`) |
| `FilterYomTovOnly` | `makeHebrewCalendar()`'s `yto` filter |
| `title` | `getCalendarTitle` |
| `item` | `@hebcal/rest-api` `eventToClassicApiObject` (key order) |
| `holidayMemo` | `getHolidayDescription` |
| `moladObj` | `eventToClassicApiObject`'s molad member |
| `moladInstant` | `getMoladAsDate()` (`@hebcal/core`), via hebcal-go `molad.Instant()` |
| `MevarchimMoladMemo` | `MevarchimChodeshEvent.memo`, via hebcal-go `event.RenderMolad` |
| `eventBasename` | `@hebcal/core` `basename()` |
| `categoriesOf` | `@hebcal/rest-api` `getEventCategories` + `@hebcal/core` `getCategories` |
| `baseCategory` | `@hebcal/core` `Event.getCategories`'s `flagToCategory` table |
| `reformatTimeStr` | `@hebcal/core` `reformatTimeStr`, via hebcal-go `event.ReformatTimeStr` |
| `itemLeyning` | the inverse of the `getLeyningForHoliday()` lookup |
| `normMonth` | `hdate` `Tammuz` vs `@hebcal/core` `Tamuz`; keeps `Tzom Tammuz` |
| `dates.go` `ExpiresAfterWeek` | `expiresSaturdayNight`, derived from the window rather than the clock |

### `internal/service/mcp/` — `@hebcal/mcp` (`../hebcal-mcp`)

`package` / tools ← `@hebcal/mcp`'s seven tools over stateless streamable-HTTP.
`torah-portion`'s reading summary and chag reading ← readings-svc's
`/shabbatTorahReading` (`getLeyningForParshaHaShavua().summary` /
`getLeyningForHoliday()`; the merged summary is `makeSummaryFromParts`, which is
not in the classic-API `leyning` object). **The chag branch deliberately emits
more than the Node original** (`Name in Hebrew:` and `Reading:`). `reISODate` /
`parseISODate` ← JS `isoDateStringToDate`. `doYahrzeit` ← Node `doYahrzeit`.
`hebDayMonthEn` ← Node `hd.render('en', false)`. `renderEn` ← `@hebcal/core`
`render` + smart apostrophe + one-m Tamuz. `daf-yomi`'s brief render ← hebcal-go's
`dafYomiEvent.Render` (already the brief form, no `Daf Yomi:` prefix, ==
Node `renderBrief`). `errorCard` ← Node `errorCard` (non-`IsError` text).
`lines` ← Node `results.join('\n')`. The sidecar-less chag label ← hebcal-go
`sedra`'s chag-week `Parsha.Name`, the counterpart of `@hebcal/core`'s.

### `internal/repository/readings/` — the readings-svc sidecar

Replaces two hebcal-web dependencies: an HTTP call to `/leyning?cfg=json`
(`@hebcal/leyning` + `@hebcal/triennial`) and one out through the www front door
to `/hebcal?cfg=json` for daily learning. Both endpoints answer in
`@hebcal/rest-api`'s classic-API shape (`formatLeyningResult()`), so `leyning`
is passed through with no reformatting. `learning.go` `learningLocale` ←
`@hebcal/locales` names (`ah`/`sh` → `a`/`s`). `/leyning` takes no `lg` (English
by design). `summary.go` `ShabbatTorahReading` ←
`getLeyningForParshaHaShavua().summary` / `getLeyningForHoliday()`
(`makeSummaryFromParts`).

### `internal/jsutil/` — JS-language-semantics compat shims

These keep their `JavaScript …` phrasing because the semantics *are* the
contract (the package is literally named `jsutil`). `ParseInt` / `ParseFloat` /
`IsoDateString` ← JS `parseInt` / `Number.parseFloat` / `Date.toISOString`.
`SmartApostrophe` ← `@hebcal/core`'s event-title apostrophe. `MakeAnchor` ←
`@hebcal/rest-api` `makeAnchor()` (CSV filename slug). `json.go` `OrderedObj` ←
JS object key order (zmanim `ALL_TIMES`, classic-API items, `/geo`). `query.go`
`QueryGet` / `QueryEmpty` / `IsOn` ← JS `undefined`/falsy conventions and the
`booleanOpts` loop in `src/calendar.js`. `pkg/geodb/parseint.go` is a copy of
`ParseInt`, kept separate so `geodb` has no internal deps.

### `internal/httpx/`

`etag.go` `MakeETag` ← hebcal-web's murmurhash3 weak ETag + encoding-class vary
(FNV-1a here; weak ETags need not match). `httpx.go` Cache-Control consts ←
`cacheControl(days)`: 14 (`app-download.js`) and 60 (`holidayPdf.js`). `SetCORS`
← hebcal-web's cfg-param-present rule. `middleware.go` `brotliQuality` 6 ←
`app-www.js`. The Vary rule (only on a compressible body over `CompressThreshold`, the only
size that ever varies by encoding) is a **deliberate divergence** from
hebcal-web, which also sent it on small non-JSON responses such as redirects. `logAccess` ← `makeLogInfo()`; the `host`
field is a deliberate addition for the dual vhost.

### `internal/logger/logger.go`

pino-compatible JSON lines, so the access log is interchangeable with the rest
of the Hebcal.com fleet's.

### `internal/model/`

| Go | JS source |
|---|---|
| `locale.go` `AliasLocale` | `lgToLocale` map (`src/lang.js`) |
| `locale.go` `LocaleSupported` | `@hebcal/core` `Locale.useLocale()`'s accepted set after `lgToLocale` |
| `locale.go` `FixMonthSpelling` | `hdate` `Tammuz` vs `@hebcal/core` `Tamuz`; `Tzom Tammuz` exception |
| `hebdate.go` `IsoDateStringToDate` | JS `isoDateStringToDate` (format-only validate, JS-Date rollover) |
| `hebdate.go` `MakeGregDate` / `MakeHebDate` | `src/dateUtil.js` `makeGregDate()` / `makeHebDate()` |
| `hebdate.go` `YearIsSupported` | `yearIsOutsideGregRange` / `yearIsOutsideHebRange` in `src/dateUtil.js` |
| `hebdate.go` `enMonthNames` | `@hebcal/hdate`'s transliterated names (`Tamuz`, single m) |
| `hebdate.go` `MonthNameEn` / `HDMonthNameEn` / `HDateString` | classic-API `hm` / `HDate.toString()` format |
| `hebdate.go` `NewHDateLenient` | JS `new HDate(day, month, year)` rollover |
| `hebdate.go` `gematriyaMonthNames` | `src/gematriyaDate.js` |
| `calendarnames.go` | dayjs locales (`src/dayjs-locales.js`); `lg` map via `localeMap` (`src/lang.js`); regenerate with `tools/dump-locales.mjs` |
| `calendar.go` `GetEvents` / `parshaEvents` / `omerEvents` | `converter.js` `getEvents()` / `getParshaEvents()` / `makeOmer()` |
| `calendar.go` `hasHolidayReading` | approximates `@hebcal/leyning` `getLeyningOnDate()` (`fullkriyah && !parshaNum`) |
| `cache.go` `yearMemo` | `@hebcal/core`'s `getHolidaysForYear_` `QuickLRU` memo (`maxSize: 120`) |
| `gregdate.go` `MaxRangeDays` (399) | hebcal-web's `/converter` + `/zmanim` limit |
| `gregdate.go` `String` / `ReIsoDate` | JS `Date.toISOString` / the route validation regex |
| `event.go` `RenderEvent` | `converter.js` `renameChanukah()` |
| `fasttimes.go` `SetFastTimes`, `FastTimeParams` | `getFastTimeOpts()` / `fastTimeOpts` (`src/urlArgs.js`) |

### `internal/service/location/` — `getLocationFromQuery` (`src/location.js`)

`package` / `FromQuery` ← `getLocationFromQuery`, the four query methods only
(no GeoIP, no `ladeg`/`lamin` here — those live in `download/v2.go`). `geoposLegacy`
← `src/urlArgs.js`. `fromLatLongLegacy` ← the `hasLatLongLegacy` branch.
`legacyTzToTzid` ← `@hebcal/core` `Location.legacyTzToTzid`. `makeGeoCityName` ←
the `37°25′N 122°5′W America/Los_Angeles` name. `tz.go` `TimezoneAt` ←
hebcal-web's geo-tz shape lookup when a lat/long names no tzid, done here with
`github.com/ringsaturn/tzf/v2` (`NewDefaultFinder`, built lazily); both
lat/long forms and the PDF `geoPos` branch in `params.go` use it, and the
"Timezone required" 400 is left only for a lookup that finds nothing. The two
polygon datasets can disagree near a border or offshore (tzf answers open
ocean with `Etc/GMT±N`). `render.go` `ToGeoJSON` ← Koa's
serialization of an `@hebcal/core` `Location` + `@hebcal/geo-sqlite`
`makeGeonameLocation`'s own-property order. `ToPlainObj` ← `@hebcal/rest-api`
`locationToPlainObj`.

### `pkg/geodb/` — `@hebcal/geo-sqlite` `GeoDb`

`package` / `DB` ← the `GeoDb` class + its `QuickLRU` cache sizes.
`LookupGeoname`'s `293396`→`293397` ← `@hebcal/geo-sqlite`'s legacy alias fixup.
ZIP `Location` carrying no `asciiname` ← hebcal-web's ZIP `Location`.
`geonameCityDescr` ← `GeoDb.geonameCityDescr`. `foldAccents` ←
`@hebcal/geo-sqlite`'s city-description dedup transliteration. `location.go`
`Is5DigitZip` ← `GeoDb.is5DigitZip`. `statenames.go` `StateNames` ←
`@hebcal/cities`. `tz.go` `ZipcodesTzMap` / `UsaTzid` ← `@hebcal/core`
`Location.ZIPCODES_TZ_MAP` / `getUsaTzid`.

### `pkg/downloadpb/doc.go`

The `Download` message / `download.proto` share a schema with the hebcal.com
download form (the form serializes against it; `downloadHref2` produces the
`/v4/` URLs). Change the two together.
