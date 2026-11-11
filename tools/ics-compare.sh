#!/bin/bash
# Diffs one holiday-calendar URL as hebcal-web and this service render it.
#
#   tools/ics-compare.sh '/hebcal/index.cgi/h.ics?v=1&year=2026&maj=on&c=on&geonameid=5128581'
#
# Run hebcal-web's download app on :8081 (npm run dev:download) and this
# service on :8082 with a readings-svc, or tools/readings-stub.mjs standing in
# for one, on -readings-socket. DTSTAMP is the one line that differs between two
# renderings of the same URL, so it is normalized. DTSTART and DTEND of timed
# events can be a minute apart (noaa-go against @hebcal/core); they are listed
# separately with TIMES=1 and hidden otherwise. Rosh Hashana's year, which is
# gematriya in Hebrew here and a number in hebcal-web, is hidden too. Set WEB, API and AGENT to change
# the hosts or the User-Agent.
WEB=${WEB:-localhost:8081}
API=${API:-localhost:8082}
url=$1
hashana='s/\(\(רֹאשׁ הַשָּׁנָה\|ראש השנה\)\) [^ ]*$/\1 YEAR/'
norm() {
  if [ -n "$TIMES" ]; then
    tr -d '\r' | sed "s/^DTSTAMP:.*/DTSTAMP:X/; $hashana"
  else
    tr -d '\r' | sed "s/^DTSTAMP:.*/DTSTAMP:X/; s/^\(DT\(START\|END\);TZID=[^:]*:[0-9]*T\)[0-9]*\$/\1XXXX/; $hashana"
  fi
}
diff <(curl -s -A "${AGENT:-curl}" "http://$WEB$url" | norm) \
     <(curl -s -A "${AGENT:-curl}" "http://$API$url" | norm)
