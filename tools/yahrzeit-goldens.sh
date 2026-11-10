#!/bin/bash
# Regenerates the golden iCalendar files the yahrzeit service is tested
# against, from a locally running hebcal-web download server:
#
#   (cd ../hebcal-web && ln -s ../hebcal-icalendar/zoneinfo zoneinfo && npm run dev:download)
#   tools/yahrzeit-goldens.sh [http://localhost:8081]
#
# Each line of testdata/cases.tsv is a name, a User-Agent ("-" for none in
# particular) and a URL. The calendars depend on the day they are generated,
# so the day is recorded in testdata/TODAY and the tests pin "today" to it.
# DTSTAMP is normalized, being the one line that differs on every request.
set -euo pipefail
base=${1:-http://localhost:8081}
dir=$(dirname "$0")/../internal/service/yahrzeit/testdata
mkdir -p "$dir/golden"
rm -f "$dir"/golden/*.ics
while IFS=$'\t' read -r name ua url; do
  [ -n "$name" ] || continue
  [ "$ua" != "-" ] || ua="yahrzeit-goldens"
  curl -sS --fail -A "$ua" "$base$url" |
    sed -E 's/^DTSTAMP:[0-9TZ]+/DTSTAMP:20260101T000000Z/' > "$dir/golden/$name.ics"
done < "$dir/cases.tsv"
date +%Y-%m-%d > "$dir/TODAY"
echo "wrote $(ls "$dir/golden" | wc -l) files, as of $(cat "$dir/TODAY")"
