#!/bin/bash
# Regenerates the golden iCalendar files the holiday-feed service is tested
# against, from a locally running hebcal-web download server, and the Torah
# readings the tests answer in place of readings-svc:
#
#   (cd ../hebcal-web && npm run dev:download)          # port 8081
#   node tools/readings-stub.mjs /tmp/readings.sock &
#   tools/ics-goldens.sh [http://localhost:8081] [/tmp/readings.sock]
#
# Each line of internal/service/ical/testdata/cases.tsv is a name and a URL.
# DTSTAMP is normalized, being the one line that differs on every request. The
# readings are the stub's answers over every Hebrew year the cases reach, one
# file for each schedule.
set -euo pipefail
base=${1:-http://localhost:8081}
sock=${2:-/tmp/readings.sock}
dir=$(dirname "$0")/../internal/service/ical/testdata
mkdir -p "$dir/golden"
rm -f "$dir"/golden/*.ics
while IFS=$'\t' read -r name url; do
  [ -n "$name" ] || continue
  curl -sS --fail "$base$url" |
    sed -E 's/^DTSTAMP:[0-9TZ]+/DTSTAMP:20261111T000000Z/' > "$dir/golden/$name.ics"
done < "$dir/cases.tsv"
for il in diaspora israel; do
  suffix=""; [ "$il" = israel ] && suffix="&i=on"
  curl -sS --fail --unix-socket "$sock" \
    "http://x/torahMemo?start=2025-09-01&end=2028-12-31$suffix" > "$dir/torahmemo-$il.json"
done
echo "wrote $(ls "$dir/golden" | wc -l) goldens"
