#!/bin/sh
#
# Fetch the same PDF URLs from the Node and Go services, side by side, for
# visual comparison.
#
#   tail -1200 /var/log/hebcal/access.log | grep pdf | jq .url | tools/fetch-both.sh
#   tools/fetch-both.sh urls.txt
#   tools/fetch-both.sh -o /tmp/cmp -j 8080 -g 8082 urls.txt
#
# Each URL becomes two files named after the URL's basename:
#
#   hebcal_2013_cassiglio-js.pdf    from the Node service
#   hebcal_2013_cassiglio-go.pdf    from this one
#
# Input is one URL or path per line. Lines are tolerated with surrounding
# quotes and whitespace, so `jq .url` output can be piped in unchanged. Blank
# lines and #-comments are skipped.
#
# Only curl is required. If poppler's pdfinfo is present the summary also
# reports page counts, which is the quickest way to spot a structural
# difference before opening anything.

set -eu

OUTDIR=comparison
HOST=localhost
JS_PORT=8080
GO_PORT=8082

usage() {
	sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
	exit "${1:-0}"
}

while getopts 'o:h:j:g:?' opt; do
	case "$opt" in
	o) OUTDIR=$OPTARG ;;
	h) HOST=$OPTARG ;;
	j) JS_PORT=$OPTARG ;;
	g) GO_PORT=$OPTARG ;;
	*) usage 0 ;;
	esac
done
shift $((OPTIND - 1))

mkdir -p "$OUTDIR"

# fetch writes one URL to one file and echoes "status bytes".
fetch() {
	url=$1
	out=$2
	curl -sS -o "$out" -w '%{http_code} %{size_download}' "$url" 2>/dev/null || echo "000 0"
}

printf '%-42s %14s %14s\n' 'calendar' "js:$JS_PORT" "go:$GO_PORT"
printf '%-42s %14s %14s\n' '------------------------------------------' '--------------' '--------------'

total=0
differ=0

# Read from the file named in $1, or from stdin.
if [ $# -gt 0 ]; then
	exec < "$1"
fi

while IFS= read -r line; do
	# Tolerate jq's quoting, surrounding whitespace and a trailing carriage
	# return from a file written on another platform.
	path=$(printf '%s' "$line" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//')
	case "$path" in
	'' | \#*) continue ;;
	esac
	# Accept either a bare path or a full URL; only the path is reused.
	case "$path" in
	http://* | https://*) path=/$(printf '%s' "$path" | cut -d/ -f4-) ;;
	esac

	base=$(basename "$path" .pdf)
	total=$((total + 1))

	js=$(fetch "http://$HOST:$JS_PORT$path" "$OUTDIR/$base-js.pdf")
	go=$(fetch "http://$HOST:$GO_PORT$path" "$OUTDIR/$base-go.pdf")

	js_status=${js%% *}
	js_bytes=${js##* }
	go_status=${go%% *}
	go_bytes=${go##* }

	js_pages=''
	go_pages=''
	if command -v pdfinfo >/dev/null 2>&1; then
		js_pages=$(pdfinfo "$OUTDIR/$base-js.pdf" 2>/dev/null | awk '/^Pages/{print "p"$2}')
		go_pages=$(pdfinfo "$OUTDIR/$base-go.pdf" 2>/dev/null | awk '/^Pages/{print "p"$2}')
	fi

	# Flag anything worth looking at first: a status mismatch, a failure on
	# either side, or a different number of pages.
	mark=' '
	if [ "$js_status" != "$go_status" ]; then
		mark='!'
	elif [ "$js_status" != 200 ]; then
		mark='?'
	elif [ -n "$js_pages" ] && [ "$js_pages" != "$go_pages" ]; then
		mark='!'
	fi
	[ "$mark" = ' ' ] || differ=$((differ + 1))

	printf '%s %-40s %8s %5s %8s %5s\n' \
		"$mark" "$base" "$js_status" "${js_bytes}b" "$go_status" "${go_bytes}b"
	if [ -n "$js_pages" ] && [ "$js_pages" != "$go_pages" ]; then
		printf '  %-40s %14s %14s\n' '' "$js_pages" "$go_pages"
	fi
done

echo
echo "$total calendars into $OUTDIR/ ; $differ flagged (! status or page-count mismatch, ? both non-200)"
