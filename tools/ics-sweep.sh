#!/bin/bash
# Runs tools/ics-compare.sh over every query string in a file (one per line),
# in each URL shape: the classic /hebcal/index.cgi/ query, the /v2/h/ base64
# query and the /v4/ protobuf. Prints the cases that differ.
#
#   tools/ics-sweep.sh tools/ics-cases.txt
#
# CGI_ONLY=1 skips the /v2/ and /v4/ shapes, which only differ in how the
# request is decoded.
dir=$(dirname "$0")
bad=0
while IFS= read -r q; do
  [ -z "$q" ] && continue
  case "$q" in \#*) continue;; esac
  b64=$(printf '%s' "$q" | base64 -w0 | tr '+/' '-_' | tr -d '=')
  urls=("/hebcal/index.cgi/hebcal.ics?$q")
  if [ -z "$CGI_ONLY" ]; then
    urls+=("/v2/h/$b64/hebcal.ics" "$(node "$dir/ics-v4.mjs" "$q" hebcal.ics 2>/dev/null)")
  fi
  for url in "${urls[@]}"; do
    out=$("$dir/ics-compare.sh" "$url" 2>&1)
    if [ -n "$out" ]; then
      bad=$((bad+1))
      echo "=== $url"
      echo "    ($q)"
      echo "$out" | head -${LINES_PER_CASE:-12}
    fi
  done
done < "$1"
echo "$bad differing"
