# iCalendar port: what is done, and the plan for the rest

A working plan for moving download.hebcal.com's `.ics` feeds from hebcal-web
(Node.js) to this service. It is written to be picked up cold: a new session
should be able to start from this file and `CLAUDE.md` without re-deriving
anything below. Update it as phases land, and delete a phase's section once
its notes have moved into `CLAUDE.md`.

| Phase | Feeds | State |
|---|---|---|
| 1 | Yahrzeit feeds that carry their calendar in the URL: `/v2/y/`, `/yahrzeit/yahrzeit.cgi/`, `/export/…?v=yahrzeit` | **done** (November 2026), `.csv` included |
| 2 | Saved Yahrzeit calendars: `/v3/<ulid>/<name>.ics` (MySQL) | planned below |
| 3 | Holiday feeds: `/v4/`, `/v2/h/`, `/hebcal/index.cgi/`, `/export/…?v=1` | planned below |
| 4 | Left on Node until someone decides otherwise: `/zmanim` and `/sunrs` feeds, the static `/ical/*.ics` files | notes at the end |

## What phase 1 left you

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
The dev server runs without MySQL for phase 1 and phase 3 URLs. For `/v3/` it
needs a database (below).

Never check real names into tests. Production log lines are personal data:
substitute Ploni Almoni, Jane Doe, Golda Meir, Theodor Herzl, יצחק בן אברהם.

---

## Phase 2: `/v3/` saved Yahrzeit calendars (MySQL)

`/v3/<ulid>/<filename>.ics` is the URL of every saved calendar.
The 26-character ULID keys a row whose JSON `contents` is the same set of
parameters a legacy URL carries in its query string. So phase 2 is a lookup
in front of the code that already exists. Estimate: one focused session.

### What hebcal-web does

`getDetailsFromDb()` in `src/yahrzeitDownload.js` and
`getYahrzeitDetailsFromDb()` in `src/yahrzeitCommon.js`:

1. `id = path.substring(4, 30)`.
2. `SELECT contents, updated, downloaded FROM yahrzeit WHERE id = ?`
   (7 s timeout). No row → **404** `Yahrzeit/Anniversary id not found: <id>`,
   with Cache-Control removed.
3. If `downloaded` is 0: `UPDATE yahrzeit SET downloaded = 1, updated = NOW()
   WHERE id = ?`. Fire-and-forget (3 s timeout, failure only logged).
   **This is load-bearing**: hebcal-shabbat-email's retention cron prunes
   calendars that were never downloaded.
4. `REPLACE INTO yahrzeit_atime (id, ts) VALUES (?, NOW())`. Fire-and-forget,
   on **every** request -- including the ones that then answer 304.
5. Expand the compact row into query form, for each entry number:
   `x<N>` (`"1978-12-08"`) → `y<N>`/`m<N>`/`d<N>` with leading zeros stripped;
   `t<N>` one letter (`y`/`b`/`a`/`o`) → the full word; `s<N>` number
   (`0`/`1`) → `off`/`on`.
6. Set `lastModified = row.updated`, `v = 'yahrzeit'`, and `ulid = id`.
7. `query = {...details, ...ctx.request.query}`: **the URL's own query string
   overrides the row** (`?yrem=0`, `?dl=1`, and anything else).

Then the common path, with these differences from a legacy URL:

- `relcalid` is `hebcal-<ulid>` (`X-WR-RELCALID`).
- UIDs use the ULID where legacy URLs use a hash of the name:
  `yahrzeit-5787-<ulid>-1`, `reminder-20260224-<ulid>-1`.
- Every memo ends with `https://www.hebcal.com/yahrzeit/edit/<ulid>#row<N>`
  unless `dl=1`; Yizkor events get the "To edit this Hebcal Yahrzeit +
  Anniversary Calendar, visit …" memo; the empty-calendar placeholder uses
  that memo and the ULID in its UID.
- The ETag input is the merged query, which includes `lastModified` -- so
  editing the calendar changes the tag.

Schema (`../hebcal-web/hebcal.sql`):

```sql
CREATE TABLE yahrzeit (
  id char(26) NOT NULL,
  created datetime NOT NULL,
  updated timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  ip varchar(39) DEFAULT NULL,
  downloaded tinyint(1) NOT NULL DEFAULT '0',
  contents JSON NOT NULL,
  PRIMARY KEY (id)
);
CREATE TABLE yahrzeit_atime (
  id char(26) NOT NULL,
  ts timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id)
);
```

A stored row (`compactJsonToSave()` writes it; `test/mock-mysql.js` has
examples): `{"x1": "1978-12-08", "n1": "Golda Meir", "t1": "y", "s1": 0,
"years": 20, "hebdate": "on", "yizkor": "on", "an": "off", "em": "…"}`.
`years` is a JSON **number**; `an` is present only as `"off"`; `em` is the
owner's email and must never reach the output or the log.

### Steps

1. **Config.** hebcal-web reads `hebcal.mysql.{host,port,user,password,dbname}`
   from `/etc/hebcal-dot-com.ini` (hebcal-devops `etc/hebcal-dot-com.ini`;
   the managed database listens on a non-default port over TLS). Add a
   `-ini` flag (default `/etc/hebcal-dot-com.ini`) and read those five keys --
   a ten-line parser, no INI dependency. **Never put the password in a flag,
   the unit file, or a log line.** With no file or no keys, `/v3/` answers
   503 and everything else keeps working, the way a missing geonames database
   or font directory already behaves.
2. **Driver.** `github.com/go-sql-driver/mysql` behind `database/sql` (the
   repo already uses `database/sql` for SQLite). Small pool
   (`SetMaxOpenConns(8)`, `SetConnMaxLifetime(5m)` -- the managed database
   drops idle connections), `tls=true` or a registered TLS config, context
   timeouts of 7 s for the SELECT and 3 s for the writes.
3. **Repository.** `internal/repository/yahrzeitdb` (next to `readings`):
   `Get(ctx, id) (contents []byte, updated time.Time, downloaded bool, err)`,
   `Touch(id, downloaded bool)` running the two writes on a background
   goroutine with its own timeout, errors logged. Bound the goroutines (a
   buffered channel and one worker) so a slow database cannot pile them up.
   Validate `id` as 26 Crockford base32 characters before querying, and
   lowercase it the way hebcal-web's stored IDs are (check a production URL).
4. **Row → `yahrzeit.Query`.** Add `yahrzeit.QueryFromJSON(contents)` doing
   step 5 above. Decode with `json.Decoder.Token()` (or an ordered decode), not
   `map[string]any`: **key order names the calendar** -- the title lists names
   in the order the `n` keys appear. MySQL's JSON type normalizes objects, returning keys sorted
   by length then bytes (`n1, n2, …, n10`); confirm against a real row that this
   is the order hebcal-web sees, and preserve whatever the driver returns.
   Numbers become their decimal strings.
5. **Merge.** `Request.Query` = row parameters, then every URL query parameter
   overriding (`Query.Set` keeps the row's position for an existing key, which
   matches a JS object spread). Then set `v=yahrzeit`, `ulid`, and
   `Calendar.RelCalID = id`. `events.go` and `placeholder()` already key
   everything off `ulid`/`RelCalID`.
6. **ETag.** Add `updated` (and the ULID, which the path already carries) to
   `Calendar.Variant`. The 304 short-circuit still needs the SELECT, since
   the tag depends on `updated`; that is one indexed primary-key read. The
   atime write happens either way, as in hebcal-web.
7. **Handler.** Mount `/v3/` on the download limiter. 404 for an unknown ID
   (no caching headers), 503 + `Retry-After` when the database is down or not
   configured -- Varnish then serves the stale object (`beresp.grace = 24h`),
   which is what a subscriber should get during an outage.
8. **The `.csv`.** hebcal-web's test suite covers `/v3/…/x.csv?yrem=0`.
   `Calendar.CSV` already renders the legacy URLs' `.csv` (CLAUDE.md, "The
   Yahrzeit `.csv`"), so `/v3/**.csv` needs only the same dispatch once the
   query comes from the database; route it with the `.ics`.
9. **Tests.** Replicate `describe('v3 yahrzeit subscription downloads')` and
   `describe('yahrzeit anniversary numbers (an)')` from
   `../hebcal-web/test/download.test.js` against a fake repository (an
   interface with an in-memory map, as `test/mock-mysql.js` does). Add: URL
   overrides the row; unknown ID 404; database down 503; `em` never appears;
   name order with ten or more entries; the edit link and its absence under
   `dl=1`; ETag changes with `updated`.
10. **Goldens.** Extend `tools/yahrzeit-goldens.sh` for `/v3/`. The reference
    server needs MySQL: a local `mysqld` (or MariaDB) with `hebcal.sql` loaded
    and a few hand-written rows, and `hebcal.mysql.*` in hebcal-web's
    `hebcal-dot-com.ini`. Do not point a development server at production.
11. **Varnish** (`hebcal-devops/etc/varnish/download.vcl`): add
    `^/v3/[^?]*\.ics(\?|$)` to the api rule. The `private` Cache-Control in
    `vcl_deliver` already covers `/v3/`.
12. **Deployment.** The api hosts need `/etc/hebcal-dot-com.ini` (check
    whether `cloud-config/web-api.yaml` already installs it) and the managed
    database's trusted-sources list has to include them. `ProtectSystem=strict`
    in `etc/hebcal-api.service` still allows reading `/etc`.

### Open questions for phase 2

- Is a connection pool on every api host acceptable to the managed
  database's connection limit? (Count hosts × `MaxOpenConns`.)
- Should a 304 skip the atime write? It would halve the write load, but
  hebcal-web writes it, and something may read `yahrzeit_atime` as "last
  polled". Default: keep writing.

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
  CLAUDE.md, "The Yahrzeit `.csv`". `/v3/…csv` follows with phase 2.
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
