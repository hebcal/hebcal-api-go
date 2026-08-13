# CLAUDE.md

Guidance for Claude Code (claude.ai/code) working in this repository.

## What this is

`hebcal-api` is one Go binary serving two things that used to be two services:

- the Hebcal.com **REST APIs** — `/converter`, `/zmanim`, `/shabbat`, `/geo`,
  `/complete` — ported from [hebcal-web](https://github.com/hebcal/hebcal-web)'s
  `src/converter.js`, `src/zmanim.js` and `src/shabbat.js`;
- the **PDF calendars** — `download.hebcal.com/v4/…pdf` and
  `www.hebcal.com/holidays/hebcal-<year>.pdf` — ported from `src/pdf.js` and
  `src/holidayPdf.js`. These arrived from the separate `hebcal-pdf-go`
  repository in August 2026, which this repository replaces; most of this file
  is that service's engineering notes.

See [README.md](README.md) for the endpoints, the package layout and how to run
it.

**The bar is that a calendar rendered here is indistinguishable from the one
production serves for the same URL.** Not "close", not "better" — the same.
Anything that changes rendering must be measured against production, not
eyeballed.

```sh
tools/compare-pdfs.py production.pdf mine.pdf --links
```

`worst dx` is the number that matters.

To sample real traffic rather than the same few calendars, pipe a production
access log through `tools/fetch-both.sh`, which fetches each URL from both
services into a folder for side-by-side viewing. Ten URLs was enough to find
the alternate-date bug below. Horizontal position comes from text
measurement, so a systematic dx means the shaper disagrees with pdfkit; that is
how the pixel-grid quantisation below was found.

## Architecture of the PDF half

The service follows the repository's usual direction — `handler` → `service` →
`model`/`pkg`, and nothing in a service package writes to an
`http.ResponseWriter`.

| file | role |
|---|---|
| `internal/handler/pdf.go` | both routes' transport: methods, headers, ETag/304, the status each error maps to |
| `internal/service/pdf/pdf.go` | `Service.Prepare`/`Render`, the document title, the 501/503 learning error |
| `internal/service/pdf/params.go` | protobuf → `hebcal.CalOptions`; port of `deserializeDownload.js` |
| `internal/service/pdf/events.go` | event generation via hebcal-go, ordering within a day |
| `internal/service/pdf/hebmonth.go` | Hebrew-month pagination (`mm=1`, `mm=2`) |
| `internal/service/pdf/render.go` | page layout; port of `renderPdf()` |
| `internal/service/pdf/text.go` | bidi and shaping |
| `internal/service/pdf/fonts.go` | font loading, metrics, per-document embedding |
| `internal/service/pdf/links.go` | URL shortening and tracking |
| `internal/service/pdf/fallback.go` | the `/hebcal?cfg=json` fetch for unsupported learning series |
| `internal/service/holidaypdf/` | `/holidays/hebcal-<year>.pdf` URL parsing; port of `holidayPdf.js` |
| `internal/model/calendarnames.go` | month and weekday names, generated |
| `pkg/downloadpb/` | the `Download` protobuf a `/v4/` URL carries |

Everything else it needs was already here, which is why the merge happened: geo
lookup is `pkg/geodb`, the weak ETag and the response middleware are
`internal/httpx`, the pino access log is `internal/logger`, and `/ping` +
`/metrics` are `internal/handler/server.go`. The PDF service had *copies* of all
four — the fourth or fifth time that code had been duplicated between these
repos — and they are now gone. Keep it that way: anything the PDF routes need
from the transport layer belongs in `httpx`, not in a private copy.

One consequence worth knowing: the PDF routes go through the same
`httpx.Middleware` as the JSON ones, so they get `X-Response-Time`, the
`http_requests_total` counter and the access log for free (`application/pdf` is
not in `compressibleType`, so nothing is compressed twice). The handler writes
the whole document and the middleware sets `Content-Length` and drops the body
for a `HEAD`.

Events come from [hebcal-go](https://github.com/hebcal/hebcal-go) **in-process**,
not over HTTP from hebcal-web's `/hebcal?cfg=json`. That endpoint reports a
category string per event, but the renderer needs the numeric flag bitmask —
colour and font weight both switch on it, and the category is a lossy projection
of it. (The learning fallback in `fallback.go` is the one exception, and it is
for series hebcal-go cannot generate at all.)

Text is handled by three libraries rather than one, which is why pdfkit's
`reverseHebrewWords()` has no counterpart here:

- [go-text/typesetting](https://github.com/go-text/typesetting) shapes, a pure
  Go HarfBuzz — real Hebrew GSUB/GPOS: nikud positioning, presentation forms.
- `golang.org/x/text/unicode/bidi` orders, implementing UAX #9.
- [seehuhn.de/go/pdf](https://seehuhn.de/go/pdf) writes the PDF, embedding both
  TrueType and CFF outlines with real subsetting.

`reverseHebrewWords()` exists in hebcal-web because pdfkit offers no bidi at all:
it reverses word order by hand and patches up parentheses and trailing commas.
With the real algorithm the hack is unnecessary — mixed Hebrew and Latin strings
come out right without any per-string special-casing.

## Things that cost real time to work out

These are not obvious from the code, and several look like bugs when they are
not. Read this section before changing layout, shaping or URLs.

### pdfkit's coordinates run top-down

`src/pdf.js` measures from the top-left; PDF measures from the bottom-left. All
layout in `render.go` is written the way `src/pdf.js` writes it, and converts in
exactly two helpers, `baseline()` and `yLine()`. Keep it that way — mixing the
two systems is what originally put the day numbers in the wrong corner.

The constant names mislead: the grid rectangle is
`rect(LMARGIN, BMARGIN, W-L-R, H-T-B)`, so `BMARGIN` is its **top** edge and
`H-TMARGIN` its bottom.

### Baselines come from hhea, not OS/2

pdfkit puts a baseline one ascender below the given y, and takes that ascender
from the font's **hhea** table. `sfnt.Font.Ascent` exposes the OS/2 typographic
ascender instead. For Source Sans Pro they disagree sharply — 984 against 750 —
which sat every day number about 3.3pt high. Adobe Hebrew happens to agree at
727, so only the Latin faces looked wrong. `fonts.go` reads hhea directly.

### Shape at a reference size, never at the drawing size

HarfBuzz quantises advances to the pixel grid at whatever ppem it is given. At
8.5pt a space came out 1.797pt instead of 1.700 — about 5.8% wide, compounding
across a string. fontkit scales linearly from font units, so pdfkit's widths are
the unrounded ones. `text.go` shapes every run at 1000pt and scales.

This is not only about widths: the time string's width positions the event
subject after it, and the same measurement drives centring, right-alignment and
the font-shrinking loop.

### The extra Hebrew word space belongs at the leading edge

`reverseHebrewWords()` rejoins right-to-left text with **two** spaces, and
pdfkit then hands that to fontkit, which lays Hebrew out right-to-left and
reverses it again. The visible result is one space between the words and the
second accumulated before them: `הדלקת נרות` draws 46.92pt of ink starting
2.83pt in, inside a 49.75pt advance.

Widening each space in place gives the same total advance — lines still
right-align correctly — but visibly too much air between words and too little
before the time. `text.go` adds the extra width to the run's `Skip`.

### The two-line break point is not the middle

When a subject is still too wide after four half-point reductions,
`renderPdfEvent` splits it in two -- on `subj.split(/(\s)/)`, which keeps the
separators, at the element just past the array's midpoint, or at the space after
it when that element is a word. For an even number of words that leaves one more
word on the first line than halving does: production draws "Yom HaAliyah School"
/ "Observance". A right-to-left subject reaches the same code already rejoined by
`reverseHebrewWords()` with *two* spaces, which puts an empty element between
every pair, moves the midpoint half a word earlier, and makes the break a plain
halving -- so `splitInTwo` takes an `rtl` flag. With fewer than three words the
left-to-right array has no space past its midpoint and nothing is broken at all,
yet `numLines` is still incremented, so the row advances (and its link box is
sized) as if it had wrapped.

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

Because pdfkit does. A simple font addresses at most 256 glyphs through a custom
encoding, and viewers disagree about one whose encoding is not a standard base —
Chrome rendered the Hebrew heavier than macOS Preview did from the same file.
Preview, poppler and ink measurement never showed a difference, so the composite
switch is a structural match rather than a confirmed fix.

### Holiday filtering goes by category, not by flag

Purim, Erev Purim and Chanukah carry `MINOR_HOLIDAY`, exactly like Tu BiShvat
and Rosh Hashana LaBehemot. A calendar asking only for major holidays keeps the
first three and drops the last two, which no reading of the flags explains.

@hebcal/rest-api's `getEventCategories()` files those three as
`["holiday", "major"]`, and hebcal-web filters on `categories[1] !== "minor"`
rather than on the flag. `eventCategories()` and `keepEvent()` in `events.go`
port both halves; filtering on `MINOR_HOLIDAY` directly loses Purim and
Chanukah from a major-only calendar.

### A calendar with no events is a 400

hebcal-web answers a PDF request that produced no events with
`400 Please select at least one event option`, not an empty document. The
dummy-event path next to it in `src/hebcal-download.js` is `.ics`-only. The
check has to come after the daily-learning fetch, since those rows can be the
whole calendar.

Months before the first event and after the last are omitted; gaps in between
are kept and drawn empty.

### Events within a day have a fixed order

`@hebcal/core` walks the holidays falling on a date and pushes each one's
related events around it: chametz deadlines, fast start, the holiday, fast end;
then the parsha, daily learning, the Omer, Molad, and finally candle lighting
and Havdalah. hebcal-go walks the same holidays in a different order, so 14
Nisan — a fast day that is also Erev Pesach — came out with the chametz
deadlines above "Fast begins".

`eventOrder()` sorts by kind to reproduce the published sequence. Note that a
holiday can carry `LIGHT_CANDLES` itself (Erev Pesach does), so timed events are
told apart by having a clock time, not by that flag.

"Fast begins" and "Fast ends" are both timed and carry the same fast flag, but
they straddle the fast day: `@hebcal/core` pushes the end event inside the fast
holiday's block, so the sequence is "Fast begins", the fast day, "Fast ends",
and only then the parsha and candle lighting. Sorting both to one position put
"Fast ends" above the fast day (e.g. Asara B'Tevet, Tzom Gedaliah); `Event.FastEnds`
(set from the untranslated `Desc` in `isFastEnds`, since the localized subject
would not survive `lg=he`) gives it its own slot after the day.

### The /holidays/ calendars come from a different hebcal-web host

`internal/service/holidaypdf` serves `www.hebcal.com`'s
`/holidays/hebcal-<year>.pdf`, a port of `src/holidayPdf.js`. It is a small
request -- a year and `i=on` -- feeding the same generator and renderer as
`/v4/`, so the whole package is URL parsing: `Parse` hands back a `pdf.Params`
and `pdfHoliday` does the rest. Four things about it are not obvious:

- **The year string keeps its extension.** `holidayPdf.js` parses
  `basename(base.substring(7))`, which is `"2026.pdf"`, and relies on
  `Number.parseInt` stopping at the dot (`leadingInt`). The hyphen test that
  makes `hebcal-2026-2027.pdf` a *Hebrew* year (5787, the year beginning in the
  first of the two Gregorian years, which is how the year-index pages link it)
  runs on that same string.
- **There is no locale support, deliberately.** `holidayPdf.js` resolves a `lg`
  parameter through `localeMap[lgToLocale[lg] || lg] || 'en'`, but nothing on
  www.hebcal.com ever links a localized holiday calendar: the holiday pages and
  the year index emit `hebcal-<year>.pdf` with at most `?i=on`. These are always
  English, so `parseHolidayPDF` hard-codes `Locale: "en"` and ignores `lg`
  rather than carrying a locale through the renderer for URLs nobody requests.
  (Note that the JS resolution is not `aliasLocale` either: neither
  transliteration family survives it, so even `lg=a` rendered plain English
  there.)
- **Every link is tagged with its own event's Hebrew year.** `hebcal-download.js`
  sets `options.utmCampaign` from the document title; `holidayPdf.js` sets
  nothing, so `renderPdfEvent` falls back to
  `'pdf-' + evt.getDate().getFullYear()`. A 2026 calendar therefore carries both
  `uc=pdf-5786` and `uc=pdf-5787` (`Params.PerEventCampaign`).
- **60 days of `Cache-Control`, and none on a refusal.** `holidayPdf.js` sets
  `cacheControl(60)` *after* its three `ctx.throw`s, so the 404, 400 and 410 go
  out uncached; and www sets `nosniff` on every response but CORS only on the
  `cfg=` API responses, so a holiday PDF carries no
  `Access-Control-Allow-Origin`. That is why the two routes have separate error
  mappings (`writeDownloadError` and `writeHolidayError`): the download path's
  410 is cacheable and names the year, the holiday path's is neither.

### The alternate date is hebcal-go's, and it lags @hebcal/hdate

`altDateBrief` renders the day line through
`event.NewHebrewDateEvent(hd).Render(locale)` and trims the year, rather than
formatting the date here. hebcal-go's `hebrewDateEvent.Render()` is a little
behind `@hebcal/hdate`'s `HDate.render()`, in two ways:

- **the apostrophe.** `HDate.render()` finishes with
  `monthName.replace(/'/g, '’')`. That one is patched over locally with
  `smartApostrophe`, the same call `Generate` already makes on every event
  subject, so the day line reads "1st of Sh’vat".
- **the ordinal for a locale that is neither English nor Spanish.**
  `Locale.ordinal()` gives "12.", and `HDate.render()` separates the year with a
  comma; hebcal-go gives "12" and a space. A `/v4/` download with `lg=de` and
  `d=on` therefore draws "12 Tewet" where a current hebcal-web draws
  "12. Tewet". **This belongs in hebcal-go**, not in a second date formatter
  here -- fixing it upstream also fixes every other hebcal-go consumer. The
  `/holidays/` calendars never see it, being English only.

While chasing that, note that **the two production hosts are not running the
same hebcal-web build** (measured 2026-08-11): www draws "12. Tewet" and a smart
apostrophe, the deployed download.hebcal.com draws "12 Tewet" and a typewriter
one, and its day numbers sit where this service puts them rather than 3.28pt
higher. Every published `@hebcal/hdate` back to 0.9 has the newer behaviour, so
the download pool is simply running older `node_modules`. `tools/fetch-both.sh`
compares against a *local* hebcal-web checkout, whose fresh `npm install`
behaves like www -- which is the reference to trust when the two hosts disagree.

### Response headers and caching

A rendered PDF carries `Cache-Control: public, max-age=1209600, s-maxage=1209600`
(14 days), `Access-Control-Allow-Origin: *`, `X-Content-Type-Options: nosniff`,
and a weak `ETag`. The value of the Cache-Control was not obvious: hebcal-web
never sets it in the `.pdf` branch of `src/hebcal-download.js`. Its download
dispatcher (`src/app-download.js`) sets `cacheControl(14)` *before* calling the
handler, and the PDF branch simply leaves it in place — removing it only on the
empty-events 400. So the response codes divide by whether the Cache-Control
survives: 200 and the out-of-range 410 keep it (the 410 because that year never
comes into range); the no-events 400 drops it.

`pdfDownload` and `writeDownloadError` mirror that: the header goes on the 200
and on the 410, and not on the 400. The unknown-location 404 is a **deliberate divergence** — hebcal-web
lets the 14-day Cache-Control survive onto it too, but a missing location may be
added later, so pinning the 404 in Varnish for two weeks is worse than a cache
miss. `Access-Control-Allow-Origin` and `X-Content-Type-Options` come free from
Go's `http.Error` on the error paths but are set explicitly on the 200, which
uses `w.Write`.

The `ETag` (`httpx.MakeETag`) is a weak FNV-1a hash of the
request path, query, encoding class and library versions — not a hash of the
rendered bytes, as hebcal-web's koa-etag is. Because it depends only on the
request and the build, the conditional check can answer `304` *before* the
render runs: a client only holds the tag if it received a 200 for the same URL,
and the calendar is a deterministic function of that URL. An upgrade of this
service or the hebcal libraries changes every tag, since `config.LibraryVersions`
is folded into the hash.

## Deliberate divergences, and things that are not bugs

Do not "fix" these without checking production first.

- **`Achrei Mot-Kedoshim` links use the long URL form.** Keying on the text
  before the first hyphen finds `achrei`, which is not a portion.
  `@hebcal/rest-api` has the identical quirk, so this matches production.
- **`sedra.Parshiot()` has 53 entries, not 54.** V'Zot HaBerachah is read on
  Simchat Torah rather than on a Shabbat.
- **Tamuz is spelled with one m.** `hdate` uses two; `@hebcal/core` and the
  website use one. All fourteen other month names already agree, so
  `hebMonthNameOverrides` is a single entry. The proper fix belongs in `hdate`.
- **Chanukah candle times differ by about a minute**, and one `Fast ends` time
  by four. Of 143 timed events in 2028, four differ. That is a zmanim question
  for hebcal-go and noaa-go, not a rendering one.
- **The `ft` ligature is absent from both documents' ToUnicode maps**, so
  `Shoftim` extracts oddly from either. A pdftotext artifact, not a regression.

## Daily learning

Schedules are not selected through the four dedicated `CalOptions` booleans but
through its generic `DailyLearning []string`, resolved against hebcal-go's
`dailylearning` registry. The registry is populated by importing
`github.com/hebcal/learning` **for its side effects** — each schedule registers
itself in an `init()`. `internal/service/pdf/params.go` carries that blank import, next to the list it
feeds; dropping it silently reduces the service to the four hard-wired series.
(It sat in `main.go` while this was its own repository, which is one import
away from a consumer that forgets it.)

`learningSchedules` in `params.go` maps each protobuf field to a registry name.
`unsupportedSeries` lists the six with no schedule at all, and `fallback.go`
fetches those from hebcal-web's `/hebcal?cfg=json` and merges them. Those three
lists move together.

`-hebcal-url` defaults to `http://www.hebcal.com` (plain http, port 80), not the
loopback: the download backends this runs on do not serve `/hebcal`, so the
fetch has to go out through the `www.hebcal.com` Varnish front door. That is
marginally less direct than a loopback call but rare, and Varnish caches the
`/hebcal?cfg=json` responses for free.

The two failure modes are deliberately different codes, carried out of the
service as one `UnsupportedSeriesError` and split by `writeDownloadError`:
**501** when no hebcal-web URL is configured, because retrying cannot help, and
**503** with `Retry-After` when a configured hebcal-web does not answer. In the default
configuration 501 is unreachable. Keep the two
together: anything the learning package gains moves from the second to the
first. `learning_test.go` asserts every name is registered and produces events.

Note that Pirkei Avot is read only between Pesach and Rosh Hashana, so a test
sampling a winter month finds nothing for it.

## Upstream

Four fixes made during the port are released in hebcal-go v0.19.0: `event.URL()`
and `sedra.Parshiot()`, plus URL-spelling corrections and the `IL_ONLY` /
`CHUL_ONLY` flags that Erev Pesach and Erev Sukkot were missing. That last one
changes filtering for every consumer, and grows the raw holiday table from 127
entries to 129.

They were validated by generating every event for 1990–2089 from both libraries,
on both schedules, and diffing the URLs: zero mismatches across ~27,000 links.
A single year would not have found them — Yom Kippur Katan, a common year's
single Adar, and the Israel schedule each needed a different part of that range.

`tools/dump-urls.mjs` is the reference half of that comparison.

github.com/hebcal/learning v0.5.0 adds the `URL()` method to every schedule
event, which is what closed the daily-learning link gap (see "Where things
stand"). The empty URLs it returns for Schottenstein Yerushalmi and multi-reading
Rambam 3-chapter days are deliberate and match @hebcal/core, not omissions.

hebcal-go v0.19.1 adds `CalOptions.SuppressHavdalah`. hebcal-go reads
`HavdalahMins == 0` as "unset, use the default tzeit" and always draws Havdalah
when candle-lighting is on; @hebcal/core, whose `havdalahMins` is nullable, reads
`havdalahMins === 0` as "no Havdalah" and drops every HavdalahEvent (see the
Havdalah suppression note below). A plain `int` cannot carry that intent, so the
flag makes it explicit. `params.go` sets it whenever `M=off` and `m=0`, which is
the default for a download URL that did not ask for a specific Havdalah time.

## src/calendar.js derivations: now ported (was the biggest gap)

`params.go` is a port of hebcal-web's `deserializeDownload.js`, which turns the
protobuf into query parameters. Those parameters then pass through
`makeHebcalOptions()` in `src/calendar.js` — even on the `/v4/` path, because
`deserializeDownload()` only produces a query map — and that function holds a
dozen derivations that no reading of `deserializeDownload.js` reveals. They were
missing at first, and every parity bug found by sampling real URLs traced back
to them. As of 2026-08-10 the ones below are **ported into `params.go`** (and,
where they touch rendering, `render.go` / `events.go`); each has a regression
test in `calendar_port_test.go`.

- **A location implies candle-lighting** — `if (location) options.candlelighting
  = true`, regardless of `c`. Without it hebcal_2008_prestea had 133 times in
  production and none here. `setLocation` / the geoPos branch in `applyLocation`.
- **An Israel location forces `il` and its own candle-lighting offset**
  (`locationDefaultCandleMins`: 20 minutes, or 40/30 for Jerusalem/Haifa/Zikhron
  Yaakov), unless the request set a non-default offset. `applyIsraelCandleMins`.
- **Candle-lighting is switched off for early years** — Gregorian before 1900,
  Hebrew before 5661 — even with a location. End of `DecodeParams`.
- **`lg=ah` / `lg=sh` set `appendHebrewToSubject`** — each event shows the
  transliteration *and* its Hebrew name (`Params.AppendHebrew`, computed into
  `Event.HebrewBrief`, drawn by `appendHebrew` in render.go, matching
  renderPdfEvent's fit-inline-or-wrap logic). hebcal_2010_6.
- **Alternate dates go on the day-number line** for Gregorian-month calendars —
  the HEBREW_DATE event's brief form (no year, via `altDateBrief`) is drawn by
  `renderAltDateOnLine` and the event is skipped as a row (`Event.AltDate`).
  hebcal_2010_6, which also stops being ~2.4x too large.
- **The 12/24-hour clock default follows the country**, not a blanket 12-hour:
  only the dozen countries in `hour12Countries` (never Israel) default to
  12-hour. Ghana showed `5:49p` where production shows `17:49`. `use12Hour`.
- **`Tammuz` → `Tamuz` in event subjects**, not only month titles
  (`fixMonthSpelling` in events.go). "Rosh Chodesh Tammuz" was the giveaway.
- **Unknown location → 404, out-of-range year → 410** (`notFoundError`,
  `outOfRangeError`), matching getLocationFromQuery and hebcal-download.js
  rather than a blanket 400. The 410 range check also stops a far-future
  request before it reaches the generator.
- **Explicit `numYears` is capped at 10** (`maxNumYears`), as getNumYears does.
- **Havdalah is suppressed when `M=off` and `m=0`** — the default for a download
  that did not set a Havdalah time. `deserializeDownload.js` writes `q.m =
  havdalahMins` whenever `M=off`, and Koa stringifies the query so `q.m` reaches
  `makeHebcalOptions` as `"0"`, which `numberOpts` parses to `options.havdalahMins
  = 0`; `@hebcal/core` then drops every HavdalahEvent (`calendar.js`:
  `havdalahMins === 0 || havdalahDeg === 0`). hebcal-go reads a zero `HavdalahMins`
  as "use the default tzeit" and would draw Havdalah where production draws none,
  so `params.go` sets `CalOptions.SuppressHavdalah` (added in hebcal-go v0.19.1).
  The giveaway was a Havdalah time on the first Saturday of a plain candle-lighting
  calendar, e.g. `5:26p` on 3 January 2026, where production shows none. A
  non-default offset (`m>0`) or tzeit (`M=on`) keeps it.

Also already ported: the Shabbat Mevarchim implication, `hebrewMonths` /
`gematriyaNumerals` from `mm`, and the category filters.

- **Hebrew-month mode alternate dates.** In `mm=1`/`mm=2` with `d=on`/`D=on`,
  hebcal-web inserts *Gregorian* `GregorianDateEvent`s that hebcal-go does not
  generate. `addGregorianAltDates` in events.go synthesizes them (every day in
  range for `d=on`, event-days only for `D=on`), and `gregorianAltText` renders
  the "MMM D" / "D MMM" form on the day-number line. In an RTL Hebrew calendar a
  number-first date like "25 אוק" is prefixed with a right-to-left mark, because
  `x/text/bidi` resolves a number-first string to an LTR paragraph and would
  otherwise draw the month to the right of the digits (the bidi counterpart of
  hebcal-web's `reverseHebrewWords`). params.go only sets `AddHebrewDates` in
  Gregorian-month mode, so the two mechanisms never both fire.

The far-future `dropDailyLearningOutsideRange` is now moot for PDF: the 410
range check rejects the whole request first, exactly as hebcal-download.js does
before it would matter.

## Testing strategy: sample real URLs, and do not trust a hand-written probe

Every bug in the list above was found by pulling URLs out of a production access
log with `tools/fetch-both.sh` and looking at the two PDFs side by side. None
was found by the unit tests, and none by the hand-picked calendars used during
development, because those calendars all happened to set the same handful of
options.

Two failures of method are worth naming, because both produced confident and
wrong statements:

- A grep for candle-lighting times used `[0-9]{1,2}:[0-9]{2}[ap]`, which finds
  nothing in a **24-hour** locale. Ghana has no am/pm, the pattern matched zero
  lines in a file containing 133 times, and that zero was reported as "neither
  document contains a candle-lighting time".
- A claim that "seehuhn subsets fonts more aggressively" was invented to explain
  a size difference. `pdffonts` shows both implementations subset. The actual
  structural difference is that this service writes object streams and a
  cross-reference stream and pdfkit does not.

So: before asserting that two calendars differ in some respect, check the claim
with a tool that understands PDFs — `pdffonts`, `pdfinfo`, `pdftotext -bbox` —
rather than a regex over extracted text, and prefer `tools/compare-pdfs.py`,
which compares positions rather than guessing at patterns.

## Where things stand

The port is complete. Rendering, links, event ordering, locales, alternate
dates, daily learning, and the response headers all match production; every
remaining difference is one of the accepted causes above (a Saturday-night
Chanukah ordering choice, the ~1-minute zmanim offset from noaa-go vs
@hebcal/core, the constant ~3.3pt day-number baseline, and the same-day holiday
ordering below). www.hebcal.com's `/holidays/` calendars are served too, from
`internal/service/holidaypdf`. What is left is operational, not rendering.

### Blocking a deployment

- **The `fonts` directory and both SQLite databases are symlinks** into a
  hebcal-web checkout, so a development build only runs on a machine that has
  one. `etc/hebcal-api.service` passes `-fonts`, `-zips-db` and `-geonames-db`,
  so this is a packaging question rather than a code one. Unlike the databases,
  missing fonts are not fatal: the two PDF routes answer 503 and the JSON APIs
  keep working.
- **Varnish is not configured** to route PDF URLs here, and the 503 path
  assumes it will retry or fall back to Node. Two families need routing now, to
  **port 8082** rather than the retired hebcal-pdf service on 8083:
  `download.hebcal.com/v4/**.pdf` and `www.hebcal.com/holidays/hebcal-*.pdf`.
  Nothing else under `/holidays/` -- the HTML pages there are still hebcal-web's,
  and this service answers them 404.

### Known differences from production

Measured with `tools/compare-pdfs.py --latin-only`. Sampling 76 real production
URLs (2026-08-10, after the calendar.js derivations above landed) gives a mean
Latin/numeric agreement of **99.3%**, with a single calendar below 98%
(hebcal_1953_new_york_city, 95.1%, entirely the zmanim difference below).

The `/holidays/` calendars (2026-08-11, against live www.hebcal.com) sample at
**98.9–99.8%** across 1999, 2024–2031, 5787, 5788 and `i=on`, with `worst dx`
0.00 and identical word counts; their link sets are identical, 86 of 86 with the
same `uc=` campaigns. Every unmatched word in that sample is the same-day holiday
ordering below -- two or three cells in the worst year (2025), none in the best.

Every remaining difference is one of four known causes, none of them the
calendar.js gap and none a missing or extra event:

1. **Chanukah candle-lighting order on Saturday night** (below) — a within-cell
   ordering difference, not an extra or missing row. Deliberately left as-is.
2. **Zmanim differ by about a minute.** hebcal_1953_new_york_city shows a
   *systematic* one-minute offset on every candle/havdalah time (production
   `4:23p`, here `4:22p`); for that era the noaa-go vs @hebcal/core sunset
   disagreement is consistent rather than occasional. A zmanim question for
   hebcal-go and noaa-go, not a rendering one. This is the single largest
   bucket of unmatched words.
3. **Day numbers sit ~3.28pt lower than production** on every page of every
   calendar (and the month title ~6pt), a constant baseline offset in the
   Latin faces. Small, uniform, and independent of everything above; the
   horizontal alignment (`worst dx`) is unaffected. Against the *deployed*
   download.hebcal.com there is no offset at all (`worst dy` 0.00 over 2,131
   words), so this is one more symptom of the two-build skew above.
4. **Two holidays on one day can come out in the other order.** hebcal-go's
   `byDate` breaks a tie on the same date by comparing `Desc` alphabetically;
   @hebcal/core keeps the order `getHolidaysForYear_()` created them in --
   static table, then the variable-date holidays, then the modern ones, then
   Rosh Chodesh, then Yom Kippur Katan, then Shabbat Shirah -- with one twist:
   its `add()` *unshifts* rather than pushes when the date's first event
   carries `EREV`. So production draws "Shabbat Zachor" above "Erev Purim",
   "Shabbat Shekalim" above "Rosh Chodesh Adar" and "Rosh Hashana LaBehemot"
   above "Rosh Chodesh Elul", while `eventOrder()` -- which files all of them
   in one slot and leaves hebcal-go's alphabetical order intact -- draws each
   pair the other way round. One or two cells a year on a full holiday
   calendar, no row missing, added or retimed. Fixing it means ranking the
   holiday slot by that emission order, which needs the untranslated `Desc` on
   `Event`; not attempted, since it changes `/v4/` output too.

Formerly on that list: **event order within a day when a fast fell on the
date**.
"Fast ends" carried the fast flag and sorted to the same slot as "Fast begins",
landing above the fast day instead of below it (Asara B'Tevet, Tzom Gedaliah, and
the Shabbat Chazon / Erev Tish'a B'Av cell). `Event.FastEnds` gives it its own
slot after the fast day, matching @hebcal/core's begins/holiday/ends sequence; a
bbox-level ordering diff of a full-year NY 2026 calendar (special Shabbatot and
Rosh Chodesh included) now finds zero cells out of order.

Localized `/v4/` calendars are handled: the Hebrew-month subtitle is translated
per locale (es "Jeshvan", de "Cheschwan"/"Tammus", fr "Tammouz"), and es/de/fr
sample to 100/100/99.8%. The `/holidays/` calendars take no locale at all (see
above), so none of this applies to them.

Alternate dates (`d=on`/`D=on`) are drawn on the day-number line in both
Gregorian-month (Hebrew date) and Hebrew-month (Gregorian date) calendars — see
the calendar.js section above. Sampling real `mm=1`/`mm=2` URLs gives 99.5–100%
Latin agreement, worst dx 0.14–0.40pt, with only the shared trio above
remaining.

**Chanukah candle-lighting order on Saturday night** is the only remaining
difference on an English `/v4/` calendar with candle-lighting, and it is a
within-cell ordering difference, not a missing, extra, or merged row. This was long misdiagnosed here as "we emit both
where production merges" — a cell-by-cell bbox comparison of December 2026 shows
otherwise:

- Erev Shabbat (Friday, e.g. 4 Dec 2026) is byte-identical: both draw
  "Chanukah: 1 Candle" *and* "Candle lighting", same order. Neither merges.
- Motzei Shabbat (Saturday, e.g. 5 and 12 Dec) has the same rows and the same
  times, but the "Chanukah: N Candles" candle-lighting row sits at the *bottom*
  of the cell here and at the *top* (holiday position) in production.

The cause is @hebcal/core's `TimedChanukahEvent`: on Saturday night the Chanukah
lighting *is* the holiday event carrying a Havdalah-time, emitted at the holiday
position, so it sorts before the parsha and Shabbat Mevarchim. hebcal-go returns
the same merged event as a plain `TimedEvent` with `CHANUKAH_CANDLES`, and
`eventOrder()` files every timed event with candle-lighting (last), so it lands
after the parsha and Mevarchim. hebcal-go's own emission order is already
correct; only our re-sort moves it, so a one-line `eventOrder()` case (timed
`CHANUKAH_CANDLES` → holiday slot) would match production.

**Deliberately not done:** we prefer the Chanukah candle-lighting time grouped
with the day's other timed rows at the foot of the Saturday cell. Every event is
present, correct and identically timed; only the vertical order within that one
cell differs. Left as an accepted divergence.

Daily-learning links are now at parity. github.com/hebcal/learning v0.5.0 gives
each schedule event a `URL()` method, so it satisfies hebcal-go's `event.URLer`
interface and the `event.URL(ev)` call in `events.go` already picks it up — the
in-process rows now carry the same Sefaria (or dafyomi.org) links production
draws, with no code change here beyond the dependency bump. Verified by
comparing the whole link set of a Daf Yomi / Psalms / Mishna Yomi / Rambam
(3ch) / Yerushalmi calendar against the Node reference: identical annotation and
URL counts, zero only-on-one-side. Two schedules carry links on only some rows,
and that too matches @hebcal/core: **Schottenstein Yerushalmi** has no Sefaria
mapping at all, and **Rambam 3-chapters** drops the link on days whose three
chapters do not collapse to a single reading (single-reading days keep it),
because the TypeScript keeps those multi-reading links in the event memo rather
than a single URL. `learning_test.go` asserts every supported schedule but
Schottenstein produces at least one URL.

Also open: `hdate` spells Tamuz with two m's (worked around in `render.go` for
month titles and `events.go` for event subjects, via `fixMonthSpelling`), and
four of 143 timed events in 2028 differ from production by a minute or
more, one `Fast ends` by four minutes -- a zmanim question for hebcal-go and
noaa-go.

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
