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
| 3 | Holiday feeds: `/v4/`, `/v2/h/`, `/hebcal/index.cgi/`, `/export/…?v=1` | **done** (November 2026); notes in `CLAUDE.md`, "The holiday feeds". Varnish still routes `/v4/…ics` to Node |
| 4a | Zmanim feeds: `/zmanim`, `/zmanim2`, `/sunrs` | **done** (November 2026); notes in `CLAUDE.md`, "The zmanim feeds" |
| 4b | The static `/ical/*.ics` and `.csv` files | **done** (November 2026): `cmd/hebcal-static-ical` + `/ical/`; notes in `CLAUDE.md`, "The static calendars". Varnish still routes `/ical/` to Node |

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

## Phase 3: holiday feeds -- done

`/v4/<protobuf>/<name>.ics` and its legacy spellings are served by
`internal/service/ical` (`feed.go`: the request, title, limits, placeholder;
`ical.go`: one event; `kiddushlevana.go`, `torahmemo.go`) and
`internal/handler/ics.go`. What it does, what was measured and the handful of
differences from hebcal-web are in `CLAUDE.md`, "The holiday feeds"; how to
re-measure is `tools/README.md`.

**What is left is the cutover.** Nothing routes `download.hebcal.com/v4/…ics`
(or `/v2/h/…ics`, `/hebcal/index.cgi/…ics`, `/export/…ics` with `v=1`) here
yet:

1. Deploy a readings-svc that has `/torahMemo` to every api host first. The
   feed refuses to render without it -- 503 while it is down, 501 with no
   socket -- and a readings-svc that predates the route answers 404, which is
   an error here too.
2. Send those URLs to the api backend in `download.vcl`, as the PDF and CSV
   ones are. A subscription's URL does not change, so subscribers' UIDs and
   events carry over (that is what the sweeps measured).
3. Watch `http_requests_total` for 501/503 from the new route and the
   readings-svc socket's health; Varnish's grace serves the cached copy through
   a 503.

## Translations that live in hebcal-web: now in `locales` (v1.1.4)

Found while porting the zmanim feeds, and worth knowing for phase 3.
hebcal-web loads its own `po/*.po` catalogs **on top of** `@hebcal/core`'s
(`src/locale.js`: `Locale.addTranslations` per locale, then `he-x-NoNikud`
derived from `he` by `copyLocaleNoNikud` with `po/he-x-NoNikud.po` over it).
Up to v1.1.3, github.com/hebcal/locales was generated from the `@hebcal/*`
packages only, so it lacked hebcal-web's additions -- above all the zman
`TITLE:`/`MEMO:` keys and their Hebrew titles -- and lost where hebcal-web
overrides a core string (seven Hebrew nikud spellings of zman names). The
zmanim feed vendored those strings in a generated `titles.go` meanwhile.

**Resolved in locales v1.1.4** (November 2026), which adds hebcal-web's `po/`
with that precedence. Re-measured against it: every entry of `po/en.po`,
`he.po`, `he-x-NoNikud.po`, `ashkenazi.po`, `es.po`, `fr.po`, `nl.po` and
`pt.po` is present and identical, and the vendored table was removed after a
string-by-string comparison (42 titles and memos, eight locales, no
differences).

Phase 3 measured it: holiday descriptions, the Kiddush Levana labels and the
zman and parsha texts in every locale family render identically in the sweeps.
What still holds: a string added to or edited in hebcal-web's `po/` reaches Go
only through a new `locales` release, so check that the release in `go.mod` is
current before measuring parity.

---

## Phase 4 and loose ends

- **Yahrzeit `.csv`**: done for the legacy URLs (November 2026), written in
  the holiday CSV service's format rather than `eventsToCsv()`'s -- see
  CLAUDE.md, "The Yahrzeit `.csv`". `/v3/…csv` is served too.
- **`/zmanim`, `/zmanim2` and `/sunrs` feeds**: done (November 2026), in
  `internal/service/zmanim/feed.go`, routed by `download.vcl`. hebcal-web's
  `zmanimDownload.js` is left in place as the fallback.
- **`/ical/*.ics`**: done (November 2026). `cmd/hebcal-static-ical`, run
  weekly by `gen-static-ical.sh` from hebcal-devops' `cron.d/hebcal-api-ical`
  on the api hosts, and served by `/ical/`. To switch, route
  `download.hebcal.com/ical/` to the api backend in `download.vcl`; the Node
  hosts' cron (`cron.d/hebcal-download`) can go once nothing serves from
  them.
- **`redirectDownload.json`**: hebcal-web 301s a curated list of old
  `/export/` URLs with no query string. Varnish's `download-redir.vcl`
  already holds them.
- **Retiring `@hebcal/icalendar` from hebcal-web** is possible only after the
  cutovers above (holiday feeds, `/zmanim` feeds, `/ical/`), since
  `yahrzeit-email.js` and the static calendars use it too.
