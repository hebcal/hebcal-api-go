# hebcal-converter

A small Go microservice implementing the Hebcal.com [Hebrew Date Converter
REST API](https://www.hebcal.com/home/219/hebrew-date-converter-rest-api)
(JSON and XML), ported from the Node.js implementation in
[hebcal-web](https://github.com/hebcal/hebcal-web) `src/converter.js`.

Date conversions use [hebcal/hdate](https://github.com/hebcal/hdate)
(`FromProlepticGregorian`, matching the JavaScript `Date` behavior) and
holidays/parshiyot come from
[hebcal/hebcal-go](https://github.com/hebcal/hebcal-go).

## Endpoints

- `GET|POST|HEAD /converter?cfg=json|xml&…` — Gregorian ⇄ Hebrew date
  conversion. The `cfg` parameter is required and must be `json` or `xml`
  (400 otherwise). POST requests are accepted, but any request body is
  ignored; conversion parameters always come from the URL query string.
  - `g2h=1` with `date=YYYY-MM-DD` or `gy`/`gm`/`gd` (+ optional `gs=on`
    for after sunset)
  - `h2g=1` with `hy`/`hm`/`hd` (+ optional `ndays=2..180` for a batch)
  - `start=YYYY-MM-DD&end=YYYY-MM-DD` for a batch of Gregorian dates
    (cfg=json only, truncated to 180 days)
  - `strict=1`, `i=on`, `lg=<lang>`, `callback=<fn>` as documented
  - If no date is given, the current date in `America/New_York` is used
    (and the response is marked non-cacheable).
- `GET|HEAD /converter/csv?…` — CSV download listing the Gregorian dates
  of the given Hebrew calendar date from 5 years before to 75 years after.
- `GET /ping` — health check, responds `pong`.
- `GET /metrics` — Prometheus metrics, including `http_requests_total`.

Responses include weak `ETag` validators (FNV-1a; the Node.js service uses
murmurhash3 — weak ETags do not need to match across implementations),
proper `Cache-Control`, CORS headers, and dynamic brotli or gzip
compression (brotli preferred) for bodies larger than 512 bytes — a
threshold chosen empirically: multi-day batches and event-heavy XML just
above it shrink 40–60%, while typical single-date JSON below it saves
almost nothing (see `TestThresholdExperiment`).

### Known differences from the Node.js implementation

- Same-day events may appear in a slightly different order within the
  `events` array.
- Molad announcements are rendered in hebcal-go's format rather than
  @hebcal/core's.
- `strict=1` validation errors return a clean `{"error": "..."}` object
  without the stack trace that koa-error appends in development mode.

## Build and test

Requires Go >= 1.24.

```bash
make build     # builds ./hebcal-converter
make test      # runs the unit tests
```

## Run

```bash
./hebcal-converter                # listens on :8082, logs to stdout
./hebcal-converter -port 8082 -logfile /var/log/hebcal-converter/access.log
```

The port defaults to `8082` (or the `PORT` environment variable).

Access logs are pino-compatible JSON lines, e.g.:

```json
{"level":30,"time":1783224620662,"pid":46493,"hostname":"w44","status":200,"length":217,"duration":1,"ip":"1.2.3.4","method":"GET","url":"/converter?cfg=json&gy=2026&gm=7&gd=4&g2h=1","ua":"curl/8.5.0"}
```

Sending `SIGUSR1` (or `SIGHUP`) makes the server close and reopen the
access log file, for use with logrotate.

## Deploy (Debian 13)

```bash
sudo make install       # installs binary, systemd unit, logrotate config
sudo systemctl start hebcal-converter
```

`make install` creates a `hebcal-converter` system user, installs the
binary to `/usr/local/bin`, the systemd unit to
`/etc/systemd/system/hebcal-converter.service`, and the logrotate
drop-in to `/etc/logrotate.d/hebcal-converter`. Logs are written to
`/var/log/hebcal-converter/access.log` and rotated daily; logrotate
signals the service with `SIGUSR1` to reopen the file after rotation.
