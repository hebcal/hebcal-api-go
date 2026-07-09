# hebcal-api

A small Go microservice implementing a subset of the Hebcal.com REST APIs,
ported from the Node.js implementation in
[hebcal-web](https://github.com/hebcal/hebcal-web). It reimplements the
JSON/XML APIs in Go for higher throughput and lower latency.

Currently implemented:

- **Hebrew Date Converter** (JSON and XML) — ported from `src/converter.js`
- **Zmanim** (halachic times, JSON) — ported from `src/zmanim.js`
- **Assur Melacha** ("is work prohibited right now", JSON) — the `im=1` mode
  of the zmanim API
- **Shabbat** (candle-lighting / Torah portion, JSON) — ported from
  `src/shabbat.js`
- **Geolocation** (`/geo`) — resolve query parameters to a location, JSON
- **Geo autocomplete** (`/complete`) — city/ZIP typeahead, JSON

Date conversions use [hebcal/hdate](https://github.com/hebcal/hdate)
(`FromProlepticGregorian`, matching JavaScript `Date` behavior); holidays,
parshiyot and zmanim come from
[hebcal/hebcal-go](https://github.com/hebcal/hebcal-go) (v0.16.2+), whose
solar calculations are backed by [hebcal/noaa-go](https://github.com/hebcal/noaa-go).

## Endpoints

### Hebrew Date Converter

- `GET|POST|HEAD /converter?cfg=json|xml&…` — Gregorian ⇄ Hebrew date
  conversion. The `cfg` parameter is required and must be `json` or `xml`
  (400 otherwise). POST requests are accepted, but any request body is
  ignored; conversion parameters always come from the URL query string.
  - `g2h=1` with `date=YYYY-MM-DD` or `gy`/`gm`/`gd` (+ optional `gs=on`
    for after sunset)
  - `h2g=1` with `hy`/`hm`/`hd` (+ optional `ndays=2..399` for a batch)
  - `start=YYYY-MM-DD&end=YYYY-MM-DD` for a batch of Gregorian dates
    (cfg=json only, truncated to 399 days)
  - `strict=1`, `i=on`, `lg=<lang>`, `callback=<fn>` as documented
  - If no date is given, the current date in `America/New_York` is used
    (and the response is marked non-cacheable).
- `GET|HEAD /converter/csv?…` — CSV download listing the Gregorian dates
  of the given Hebrew calendar date from 5 years before to 75 years after.

### Zmanim

- `GET|HEAD /zmanim?cfg=json&…` — halachic times for a location and date.
  `cfg=json` is required. Requires the geonames/zips databases (see
  [Location databases](#location-databases)); without them this route
  returns 503 while the other APIs keep working.
  - **Date:** `date=YYYY-MM-DD` for a single day, or
    `start=YYYY-MM-DD&end=YYYY-MM-DD` for a range (capped at 399 days). If
    omitted, "today" in the location's timezone is used.
  - **Location** — one of (see [Location resolution](#location-resolution)):
    `geonameid`, `zip`, `city`, decimal `latitude`+`longitude`+`tzid`, or
    the legacy `ladeg`/`lamin`/`ladir` + `lodeg`/`lomin`/`lodir` form.
  - `ue=1` includes the location's elevation in sunrise/sunset (and the
    times derived from them); the `seaLevelSunrise`/`seaLevelSunset` times
    are only present when elevation is enabled.
  - `sec=1` returns seconds instead of rounding each time to the minute.
  - Times that do not occur on a given day (e.g. no astronomical dawn in
    the polar summer) are returned as `null`.
- `GET|HEAD /zmanim?cfg=json&im=1&…` — **Assur Melacha** check: whether
  melacha (work) is prohibited at a given instant (Shabbat or Yom Tov).
  Same location parameters as above.
  - `dt=<ISO 8601>` selects the instant (a bare `YYYY-MM-DD` is UTC
    midnight; a datetime without a zone is interpreted in the location's
    timezone; a trailing `Z` or `±HH:MM` offset is honored). If `dt` is
    omitted the current time is used and the response is cached for 60s.

### Geolocation

- `GET|HEAD /geo?…` — resolve a location from query parameters and
  return the location as JSON (ported from the `/geo` route in
  hebcal-web's `src/router.js`). `OPTIONS` returns a CORS preflight; other
  methods return `405`. Accepts the same location parameters as
  `/zmanim` (see [Location resolution](#location-resolution)), and returns
  the raw `@hebcal/core` Location shape:

  ```json
  {"latitude":31.76904,"longitude":35.21633,"locationName":"Jerusalem, Israel","timeZoneId":"Asia/Jerusalem","elevation":786,"il":true,"cc":"IL","geoid":281184,"admin1":"Jerusalem District","geo":"geoname","population":801000,"asciiname":"Jerusalem","geonameid":281184}
  ```

  This differs from the trimmed `location` object embedded in the `/zmanim`
  and `/shabbat` responses (different key names, and it always includes
  `elevation`, `il`, `geoid` and `population`). A request with no location
  parameters returns `204 No Content`; an unknown `geonameid`/`zip`/`city`
  returns `404`, and malformed input returns `400`. Requires the
  geonames/zips databases (503 otherwise).

### Geo autocomplete

- `GET /complete?q=<prefix>` (also `/complete.php`) — city and US-ZIP
  typeahead, ported from hebcal-web's `src/complete.js`. Returns a JSON
  array of up to 12 matches, each with a country-flag emoji. A leading
  digit is treated as a ZIP code (exact 5-digit or numeric prefix);
  otherwise both the geonames and US-ZIP full-text indexes are searched,
  merged (GeoNames winning ties), and sorted by population.
  - `g=on` (or `g=1`) additionally returns
    `latitude`/`longitude`/`timezone`/`population`.
  - An empty `q` or no matches returns `404 {"error":"Not Found"}`.
  - Responses are cached for 3 days with a weak `ETag`.

  ```json
  [{"id":281184,"value":"Jerusalem, Israel","admin1":"Jerusalem District","country":"Israel","cc":"IL","geo":"geoname","asciiname":"Jerusalem","flag":"🇮🇱"}]
  ```

  The full-text queries use SQLite FTS5, so the `mattn/go-sqlite3` driver
  must be built with the `sqlite_fts5` tag (the `Makefile` and CI already
  pass `-tags sqlite_fts5`).

### Operational

- `GET /ping` — health check. Serves the contents of `/var/www/html/ping`
  (override with `-pingfile`) as `text/plain`, the same file hebcal-web
  serves; returns 404 when the file is absent, so removing it takes the
  host out of load-balancer rotation.
- `GET /metrics` — Prometheus metrics, including `http_requests_total`.

## Location resolution

The `/zmanim` API accepts the same location parameters as hebcal-web, in
this order of precedence:

1. `geonameid=<id>` — a [GeoNames](https://www.geonames.org/) numeric id.
2. `zip=<5-digit>` — a US ZIP code.
3. `city=<id>` — a legacy Hebcal city identifier (e.g. `GB-London`).
4. `latitude=<deg>&longitude=<deg>&tzid=<IANA tz>` — decimal degrees, with
   south/west expressed as negative numbers. `elev=<meters>` is optional
   (used only with `ue=1`), and `i=on` selects the Israel schedule.
5. `ladeg`/`lamin`/`ladir` + `lodeg`/`lomin`/`lodir` — the legacy
   degree/minute/direction form, where south/west are positive magnitudes
   with a direction letter (`s`/`w`). A legacy `tz`/`dst` pair is mapped to
   an IANA timezone when `tzid` is absent.

Unlike hebcal-web, this service does **not** guess a timezone from
latitude/longitude shape data, so `tzid` (or a resolvable `tz`/`dst`) is
required for the positional forms. GeoIP-based location is also out of
scope.

### Location databases

Location resolution reads two prebuilt SQLite databases,
`geonames.sqlite3` and `zips.sqlite3`, produced by
[@hebcal/geo-sqlite](https://github.com/hebcal/geo-sqlite). Their paths
default to the working directory and can be set with the `-zips-db` /
`-geonames-db` flags or the `ZIPS_DB` / `GEONAMES_DB` environment
variables. Small sample databases used by the tests live in `testdata/`.

## Caching and compression

Responses include weak `ETag` validators (FNV-1a; the Node.js service uses
murmurhash3 — weak ETags do not need to match across implementations),
appropriate `Cache-Control` or `Expires` headers, CORS headers, and
dynamic brotli or gzip compression (brotli preferred) for bodies larger
than 512 bytes — a threshold chosen empirically: multi-day batches and
event-heavy XML just above it shrink 40–60%, while typical single-date
JSON below it saves almost nothing (see `TestThresholdExperiment`).

Zmanim caching mirrors hebcal-web: a single live date expires at the next
local midnight; an explicit date or range is cached for 30 days with an
`ETag`; the live Assur Melacha check is cached for 60 seconds.

## Known differences from the Node.js implementation

- Same-day events may appear in a slightly different order within the
  `events` array.
- Molad announcements are rendered in hebcal-go's format rather than
  @hebcal/core's.
- `strict=1` validation errors return a clean `{"error": "..."}` object
  without the stack trace that koa-error appends in development mode.
- Zmanim times agree with @hebcal/core to within ~2 seconds (the inherent
  difference between the noaa-go and @hebcal/core NOAA implementations);
  minute-rounded output matches except where a value falls within 2s of a
  rounding boundary.

## Build and test

Requires Go >= 1.24 and **cgo** (a C compiler), because the location
lookups use the cgo-based `github.com/mattn/go-sqlite3` driver. The driver
must be built with the `sqlite_fts5` tag so the `/complete` full-text
queries work; the `Makefile` targets pass it for you.

```bash
make build     # builds ./hebcal-api (CGO_ENABLED=1 -tags sqlite_fts5)
make test      # runs the unit tests (-tags sqlite_fts5)
```

If you invoke `go` directly rather than through the `Makefile`, add the
tag yourself, e.g. `go test -tags sqlite_fts5 ./...`.

## Run

```bash
./hebcal-api                      # listens on :8082, logs to stdout
./hebcal-api -port 8082 -logfile /var/log/hebcal/api.log \
    -zips-db /var/lib/hebcal/zips.sqlite3 \
    -geonames-db /var/lib/hebcal/geonames.sqlite3
```

The port defaults to `8082` (or the `PORT` environment variable); the
access log defaults to stdout (pass `-logfile <path>`). The geonames/zips
database paths default to the working directory (see
[Location databases](#location-databases)).

Access logs are pino-compatible JSON lines, e.g.:

```json
{"level":30,"time":1783224620662,"pid":46493,"hostname":"w44","status":200,"length":217,"duration":1,"ip":"1.2.3.4","method":"GET","url":"/converter?cfg=json&gy=2026&gm=7&gd=4&g2h=1","ua":"curl/8.5.0"}
```

Sending `SIGUSR1` (or `SIGHUP`) makes the server close and reopen the
access log file, for use with logrotate.

## Deploy (Debian 13)

```bash
sudo make install       # installs binary, systemd unit, logrotate config
sudo systemctl start hebcal-api
```

`make install` installs the binary to `/usr/local/bin`, the systemd unit
to `/etc/systemd/system/hebcal-api.service`, and the logrotate drop-in to
`/etc/logrotate.d/hebcal-api`. The service runs as `www-data` and writes
its access log to `/var/log/hebcal/api.log` (same directory hebcal-web
uses), rotated daily; logrotate signals the service with `SIGUSR1` to
reopen the file after rotation.
