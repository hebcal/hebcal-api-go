# iCalendar port: what is done, and the plan for the rest

A working plan for moving download.hebcal.com's `.ics` feeds from hebcal-web
(Node.js) to this service. It is written to be picked up cold: a new session
should be able to start from this file and `CLAUDE.md` without re-deriving
anything below. Update it as phases land, and delete a phase's section once
its notes have moved into `CLAUDE.md`.

| Phase | Feeds | State |
|---|---|---|
| 1 | Yahrzeit feeds that carry their calendar in the URL: `/v2/y/`, `/yahrzeit/yahrzeit.cgi/`, `/export/…?v=yahrzeit` | **done** (November 2026), `.csv` included |
| 2 | Saved Yahrzeit calendars: `/v3/<ulid>/<name>.ics` (MySQL) | **done** (November 2026), `.csv` included; notes in `CLAUDE.md`, "Saved calendars: `/v3/` and MySQL" |
| 3 | Holiday feeds: `/v4/`, `/v2/h/`, `/hebcal/index.cgi/`, `/export/…?v=1` | planned below |
| 4 | Left on Node until someone decides otherwise: `/zmanim` and `/sunrs` feeds, the static `/ical/*.ics` files | notes at the end |

## What phases 1 and 2 left you

Read these before writing anything; `CLAUDE.md`'s "The iCalendar feeds"
section explains the non-obvious parts.

| Path | What it is |
|---|---|
| `pkg/icalendar/` | The RFC 5545 writer. `Calendar.Marshal([]Event)`, `FoldLine`, `ZoneInfo` (VTIMEZONE files), `Alarm`, `Murmur32Hex`, `DefaultUID`. A serializer only: it takes resolved `Event` structs and does not import hebcal-go. |
| `internal/service/yahrzeit/request.go` | `ParseRequest(requestURI)` → `*Request{Path, RawQuery, Query, Opaque}` for every URL shape; `IsYahrzeit()`, `Ext()`. |
| `internal/service/yahrzeit/query.go` | `Query`: an ordered, first-value-wins query string. Order matters (it names the calendar). |
| `internal/service/yahrzeit/events.go` | Entries → events: subjects, memos, reminders, Yizkor. Reads the calendar ID from the query's `ulid` key, already. |
| `internal/service/yahrzeit/yahrzeit.go` | `New(req, today)` → `*Calendar`; `Variant(userAgent)` for the ETag; `ICS(userAgent, zones, now)`. `Calendar.RelCalID` exists and is unused. |
| `internal/handler/yahrzeit.go` | Headers, ETag/304, error statuses. |
| `internal/handler/download.go` | `download()` dispatches to `yahrzeitDownload` when `ParseRequest` says the query's `v` starts with `y`. |
| `internal/service/yahrzeit/testdata/` | `cases.tsv` (name, User-Agent, URL), `golden/*.ics` captured from hebcal-web, `TODAY`. |
| `internal/service/yahrzeit/saved.go` | `/v3/`: `SavedID`, `NewSavedRequest` (row JSON + URL query → `Request`). |
| `internal/repository/yahrzeitdb/` | The MySQL reader and its background access writes; `Memory` for tests. |
| `tools/yahrzeit-goldens.sh` | Regenerates the goldens from a local hebcal-web. |

### Standing up the reference server

Parity is measured against a local hebcal-web, not production (see
`CLAUDE.md`, "the two production hosts are not running the same build").

```bash
cd ../hebcal-web
npm install
node_modules/@hebcal/geo-sqlite/bin/make-test-dbs
touch hebcal-dot-com.ini
npm run build
ln -s ../hebcal-icalendar/zoneinfo zoneinfo   # app reads ./zoneinfo/<tzid>.ics
npm run dev:download                          # port 8081; needs Node >= 24
```

Then run this service on another port and diff, with `DTSTAMP` normalized --
it is the only line that differs between two renderings of one URL:

```bash
make build && ./hebcal-api -port 8082 -zoneinfo ../hebcal-icalendar/zoneinfo
```

```bash
diff <(curl -s localhost:8081$URL | sed 's/^DTSTAMP:.*/DTSTAMP:X/') \
     <(curl -s localhost:8082$URL | sed 's/^DTSTAMP:.*/DTSTAMP:X/')
```

Repeat with `-A Google-Calendar-Importer`; that client gets a different body.
The dev server runs without MySQL for phase 1 and phase 3 URLs. For the
`/v3/` goldens, give it an `app.context.mysql` whose `query2` returns
`[{contents: JSON.parse(<testdata/saved/<id>.json>), updated, downloaded}]`
(CLAUDE.md, "Saved calendars").

Never check real names into tests. Production log lines are personal data:
substitute Ploni Almoni, Jane Doe, Golda Meir, Theodor Herzl, יצחק בן אברהם.

---

## Phase 3: holiday feeds

`/v4/<protobuf>/<name>.ics` and its legacy spellings. This is by far the larger job, because the feed's
`DESCRIPTION` text is assembled from five sources, two of which have no Go
implementation. Estimate: several sessions, and it should be split as below
so each part can be measured on its own.

### What already exists

The CSV calendars are the same request rendered differently, so most of the
pipeline is done (`CLAUDE.md`, "The CSV calendars"):

- `download.Decode(ctx, u, ext)` -- all three URL shapes → the `Download`
  message. It takes an extension; `.ics` needs only to be allowed.
- `download.ParamsFromMessage` → `hebcal.CalOptions`; `download.GenerateCalEvents`.
- In `internal/service/csv` (unexported today; move the shared ones to
  `download` rather than copying): `numYears` (`getNumYears()`, which applies
  to `.ics` as much as `.csv`), `shouldRenderBrief`, `renderSubject` (with
  the Rosh Hashana year rule), `holidayDescription`, `ownMemo`.
- `download.EventCategories`, `download.AsTimedEvent`.
- `internal/service/pdf/links.go` -- URL shortening and UTM tracking
  (`appendIsraelAndTracking`), including the Israel and parsha quirks that
  took three bugs to get right.
- The readings-svc `/learning` fetch for the eight series with no Go schedule.
- `pkg/icalendar`, including the pieces phase 1 does not use: `Event.Busy`
  (CHAG → `TRANSP:OPAQUE`), `Event.URL`, `Event.Location`, `Event.Geo`,
  `Event.Duration`, `AlarmOmer`, `AlarmCandleLighting`, `AbsoluteAlarm`,
  `DefaultUID`.

### What has to be built

**A. The event adapter** (`internal/service/ical`, new): `event.CalEvent` →
`icalendar.Event`. This is the half of the `IcalEvent` constructor that
`pkg/icalendar` deliberately left out. From `hebcal-icalendar/src/icalendar.ts`:

- Subject: `shouldRenderBrief(ev) ? renderBrief : render`, then the emoji
  (`opts.emoji`, default on for `.ics`: prefix, except the Omer where it is a
  suffix), then escaping, then -- **after** escaping, so unescaped --
  `" / " + renderBrief('he')` for `lg=ah`/`sh`. `icalendar.Event.Summary` is
  escaped whole on output, so this needs a second field (`SummarySuffix`,
  written raw) or a pre-escaped flag. Decide when you get there.
- `CATEGORIES` from the `CATEGORY` table keyed on the first of
  `getEventCategories()`; several categories map to none (hebdate, mevarchim,
  molad, omer, zmanim).
- `LOCATION`: a timed event carries the location's short name; a
  daily-learning event carries its translated category name.
- Timed events: `DTSTART;TZID=`, `GEO`, and `eventTimeStr` (already HH:MM in
  the location's zone -- hebcal-go's `TimedEvent.EventTime` is a `time.Time`
  in that zone; take `Clock()`).
- Alarms: Omer `-P0DT3H30M0S`, candle lighting `-P0DT0H10M0S`, plus
  `addLocationOmerAlarms()` in `hebcal-download.js`, which replaces the Omer
  alarm with an absolute time at dusk (sunset − 30 min on Friday, tzeit 8.5°
  on Saturday) when the calendar has a location, and changes that event's UID
  to `hebcal-omer-<year>-day<n>-<geoid>`.
- UID: `hebcal-<yyyymmdd>-<murmur32(desc)>` plus `-<geoid>` or
  `-<makeAnchor(name)>` for timed events. `desc` is the **untranslated**
  `ev.getDesc()`; hebcal-go's `Render("en")` is not always that string. Check
  every event type against production before trusting it -- this is the UID,
  and a mismatch duplicates events for every subscriber.
- `CLASS:PUBLIC`, `TRANSP:OPAQUE` + `BUSYSTATUS:OOF` on CHAG days.

**B. The memo** (`createMemo()` in `hebcal-web/src/icalCommon.js`), in its
order of precedence:

1. Havdalah and candle lighting: the event's own memo only.
2. Omer: `todayIs('en')`, `todayIs('he')`, and the sefira in English, Hebrew
   and transliteration. hebcal-go's `omer.OmerEvent` has `TodayIs`; check it
   has all three sefira forms.
3. Kiddush Levana: `makeKiddushLevanaMemo()` (`src/kiddushLevanaMemo.js`),
   three labelled times formatted with `Intl.DateTimeFormat` in the calendar's
   locale. The date formatting per locale is the risk; `internal/model/calendarnames.go`
   has the month and weekday names.
4. The parsha summary: `getParshaSummary()` reads `summaryEn`/`summaryHe`
   from `hebcal-web/src/drash.json`. **That data lives in hebcal-web.** Either
   vendor a generated Go table of the 54 (+ doubled) summaries, with a
   `tools/` script to regenerate it, or have readings-svc serve it. Vendoring
   is simpler and the text changes a few times a year.
5. The holiday description (`localizedHolidayDescription`): Hebrew for Hebrew
   locales, else English. The CSV service has `holidayDescription`, but it
   returns the **first sentence** (`getHolidayDescription(ev, true)`); the
   feed wants the whole text (`false`).
6. Failing those, a timed event's linked event rendered in the locale.
7. Then appended, blank-line separated: the Torah memo (C), and the tracked
   URL.

**C. The Torah memo -- needs readings-svc.** `makeTorahMemoText()` in
`hebcal-web/src/torahMemo.js` writes, for every parsha and every holiday
with a reading:

```
Torah: Genesis 12:1-17:27
Haftarah: Isaiah 40:27-41:16 | Shabbat Rosh Chodesh
Haftarah for Sephardim: …
```

from `getLeyningForParshaHaShavua()` / `getLeyningForHoliday()`
(`@hebcal/leyning`): `reading.summary`, `reading.haftara`,
`reading.reason.haftara`, `reading.sephardic`, plus `formatHaftarahTheme()`
(`src/haftarahTheme.js`, the Haftarot of Admonition and Consolation). None of
it exists in Go, and `summary` is `makeSummaryFromParts`, which this
repository has already declined to port twice.

The existing sidecar routes do not fit: `/leyning` returns the classic-API
`leyning` object, which has no `summary`; `/shabbatTorahReading` returns one
Shabbat. A feed is up to ~2400 events over seven years. Add a route to
readings-svc that returns the finished memo text:

```
GET /torahMemo?start=2026-09-12&end=2033-09-30&i=0
→ [{"date":"2026-09-19","desc":"Parashat Ha'azinu","memo":"Torah: …\nHaftarah: …"}, …]
```

- Run `makeTorahMemoText` itself in the sidecar (move `torahMemo.js` and
  `haftarahTheme.js` there, or into a shared package), so the text is the
  same code production runs rather than a copy -- the reason the sidecar
  exists.
- The reading depends on `(date, il, event description)` and nothing else --
  no location, no locale -- so the Go side caches per `(Hebrew year, il)` in
  an LRU and every feed for that year shares it. That makes the sidecar cost
  a handful of calls per deploy rather than one per request. Match events to
  rows by date + untranslated description, as `/shabbat` does (`Item.Desc()`).
- Events it ignores (`HOLIDAY_IGNORE_MASK`): Omer, Mevarchim, Molad, user
  events, Hebrew dates, all daily learning, and anything timed.
- Failure mode, same as daily learning: readings-svc down → 503 +
  `Retry-After`, never a feed silently missing its readings (Varnish serves
  the stale copy).
- **Deploy readings-svc first.** It answers an unknown route with a 404, and
  the feed must treat that as an error, not as "no readings".

**D. Calendar-level behaviour** (`hebcalDownload()` in
`hebcal-web/src/hebcal-download.js`):

- 410 for a year outside the narrow range (shared `model.YearIsSupported`).
- Title from `getCalendarTitle()` (the PDF path has `CalendarTitle`), unless
  `makeIcalOpts()` finds `title`/`caldesc` in the query; a subscription with
  no location gets `Hebcal <subtitle>` and, when holidays are filtered, a
  `caldesc` from `queryDescription()`. Read `makeIcalOpts()` in
  `src/urlArgs.js` in full.
- `X-APPLE-CALENDAR-COLOR:#800002` unless `color=` says otherwise.
- `utm_campaign` defaults to `ical-<campaignName>`; the PDF path's
  `CampaignName` has the `preferAsciiName` rule already.
- Attachment unless `subscribe` is set (note: the opposite default from
  Yahrzeit, where `dl=1` makes it one).
- `limitIcsFeedLength()`: a subscription over 2399 events drops everything
  older than twelve weeks, and if still over is truncated with a "Hebcal
  calendar feed truncated" user event on the following day. Only then does
  the ETag gain the week.
- Empty calendar → one placeholder event, `max-age=86400`, no ETag (a 400 for
  PDF, a header row for CSV, a placeholder here -- three formats, three
  answers).
- `PRODID` language follows the locale (`localeMap` in `icalendar.ts`).
- `X-WR-TIMEZONE` and the `VTIMEZONE` for the calendar's location.
- 14-day Cache-Control, set by the dispatcher before the handler runs.

### Order of work

1. Adapter (A) with an empty memo; diff everything but `DESCRIPTION` against
   hebcal-web across locales, Israel, a location, daily learning. UIDs first.
2. Memo sources 1, 2, 5, 6 and the tracked URL (all in-process).
3. Parsha summaries (4) and Kiddush Levana (3).
4. readings-svc `/torahMemo` and its cache (C).
5. Calendar-level behaviour (D), then Varnish.

Measure with a harness like the CSV port's: sample real `/v4/…ics` URLs from
an access log, fetch each from both servers, normalize `DTSTAMP`, and diff.
Expect the known zmanim difference (about a minute on some times, noaa-go vs
`@hebcal/core`) to show up in `DTSTART` of timed events -- and, unlike in a
PDF, **a different minute is a different event to a subscriber only if the
UID changes; it does not, so the event just moves**. That is acceptable; a
changed UID is not.

### Risks worth naming before starting

- **UID stability across every event type** is the whole game. Build a
  100-year, both-schedules sweep like `tools/dump-urls.mjs` did for links,
  comparing UIDs from `@hebcal/icalendar` and from the adapter.
- hebcal-go's `Render("en")` vs `@hebcal/core`'s `getDesc()`: Rosh Chodesh,
  Chanukah day names, Shabbat Mevarchim and the daily-learning events have
  all differed at some point (see `untranslatedDesc` in the CSV service).
- The two production pools run different builds. Use a local hebcal-web.
- Feed size: a seven-year feed with candle-lighting is ~2400 events and
  ~1.5 MB. `BenchmarkICS` puts the writer at roughly 100 MB/s, so the writer
  is not the cost; event generation and the memo lookups are. Hold a download
  limiter slot, as PDF and CSV do.

---

## Phase 4 and loose ends

- **Yahrzeit `.csv`**: done for the legacy URLs (November 2026), written in
  the holiday CSV service's format rather than `eventsToCsv()`'s -- see
  CLAUDE.md, "The Yahrzeit `.csv`". `/v3/…csv` is served too.
- **`/zmanim` and `/sunrs` feeds** (`zmanimDownload.js`): a different event
  source and their own caching. Not planned.
- **`/ical/*.ics`**: static files written weekly by `makeStaticCalendars.js`
  on the Node hosts. Porting the feeds does not move these; the cron would
  need its own Go command, built on the phase 3 service.
- **`redirectDownload.json`**: hebcal-web 301s a curated list of old
  `/export/` URLs with no query string. Varnish's `download-redir.vcl`
  already holds them.
- **Retiring `@hebcal/icalendar` from hebcal-web** is possible only after
  phases 2-4, since `yahrzeit-email.js` and the static calendars use it too.
