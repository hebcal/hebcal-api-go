#!/usr/bin/env python3
"""Compare two renderings of the same calendar, word position by word position.

    tools/compare-pdfs.py production.pdf mine.pdf
    tools/compare-pdfs.py production.pdf mine.pdf --latin-only
    tools/compare-pdfs.py production.pdf mine.pdf --links

This is how every layout bug in this renderer was found. Looking at two PDFs
side by side tells you something is off; this tells you which word, by how
much, and in which direction, which is usually enough to name the cause.

Requires poppler's pdftotext (brew install poppler).

Reading the output
------------------

The match rate is over words that appear in both documents at nearly the same
place. A word that moved more than the tolerance counts as unmatched and is
listed, so a handful of unmatched entries clustered on one page usually means
one cell rendered its events in a different order, not that the page is wrong
everywhere.

`worst dx` is the more useful of the two numbers. Horizontal position comes
from text measurement, so a systematic dx means the shaper disagrees with
pdfkit -- that is how the pixel-grid quantisation turned up, as a worst dx of
1.96pt that fell to 0.11pt once runs were shaped at a reference size.

`--latin-only` exists because pdftotext segments Hebrew differently in the two
documents. hebcal-web draws Hebrew through reverseHebrewWords(), which leaves
combining marks split from their letters, so the same line extracts as a
different set of "words" than this renderer produces -- comparing them
directly reports differences that are not there. Restricting to the Latin and
numeric text (day numbers, times, years) compares positions that are directly
comparable, and those are laid out by the same code as the Hebrew.
"""

import argparse
import collections
import re
import subprocess
import sys
import zlib

WORD = re.compile(
    r'<word xMin="([\d.]+)" yMin="([\d.]+)" xMax="([\d.]+)" yMax="([\d.]+)">([^<]*)</word>')


def page_count(path):
    out = subprocess.run(['pdfinfo', path], capture_output=True, text=True).stdout
    m = re.search(r'^Pages:\s+(\d+)', out, re.M)
    return int(m.group(1)) if m else 0


def words(path, page):
    """Returns (x, y, text) for every word on a page, in PDF points."""
    out = subprocess.run(
        ['pdftotext', '-f', str(page), '-l', str(page), '-bbox-layout', path, '-'],
        capture_output=True, text=True).stdout
    return [(float(m.group(1)), float(m.group(2)), m.group(5))
            for m in WORD.finditer(out)]


def is_latin(s):
    return bool(s) and all(ord(c) < 0x0590 for c in s)


def inflate_all(path):
    """Document bytes plus every stream that decompresses, where links live."""
    data = open(path, 'rb').read()
    out = [data]
    for m in re.finditer(rb'stream\r?\n', data):
        end = data.find(b'endstream', m.end())
        if end < 0:
            continue
        try:
            out.append(zlib.decompress(data[m.end():end]))
        except Exception:
            pass
    return b''.join(out)


def compare_links(a, b):
    ja, jb = inflate_all(a), inflate_all(b)
    uri = re.compile(rb'/URI\s*\(([^)]{0,200})\)')
    link = re.compile(rb'/Subtype\s*/Link')
    sa = sorted({u.decode('latin-1') for u in uri.findall(ja)})
    sb = sorted({u.decode('latin-1') for u in uri.findall(jb)})
    print(f'  annotations: {len(link.findall(ja))} vs {len(link.findall(jb))}')
    print(f'  distinct URLs: {len(sa)} vs {len(sb)}')
    only_a, only_b = set(sa) - set(sb), set(sb) - set(sa)
    print(f'  common={len(set(sa) & set(sb))}  only-first={len(only_a)}  only-second={len(only_b)}')
    for u in sorted(only_a)[:10]:
        print(f'    only in first:  {u}')
    for u in sorted(only_b)[:10]:
        print(f'    only in second: {u}')
    return not only_a and not only_b


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('reference', help='the PDF to match, usually production')
    ap.add_argument('candidate', help='the PDF under test')
    ap.add_argument('--latin-only', action='store_true',
                    help='skip Hebrew words, which the two documents segment differently')
    ap.add_argument('--links', action='store_true', help='compare link annotations too')
    ap.add_argument('--tolerance', type=float, default=25.0,
                    help='points a word may move and still count as matched')
    ap.add_argument('--show', type=int, default=10, help='how many unmatched words to list')
    args = ap.parse_args()

    pages = min(page_count(args.reference), page_count(args.candidate))
    if pages == 0:
        sys.exit('could not read page counts; is pdfinfo installed?')
    pa, pb = page_count(args.reference), page_count(args.candidate)
    if pa != pb:
        print(f'PAGE COUNT DIFFERS: {pa} vs {pb}; comparing the first {pages}')

    total = matched = 0
    worst_dx = worst_dy = 0.0
    missing = []
    for page in range(1, pages + 1):
        a = words(args.reference, page)
        b = words(args.candidate, page)
        if args.latin_only:
            a = [w for w in a if is_latin(w[2])]
            b = [w for w in b if is_latin(w[2])]
        index = collections.defaultdict(list)
        for x, y, t in a:
            index[t].append((x, y))
        for x, y, t in b:
            total += 1
            cands = index.get(t)
            if cands:
                bx, by = min(cands, key=lambda p: abs(p[0] - x) + abs(p[1] - y))
                if abs(bx - x) < args.tolerance and abs(by - y) < args.tolerance:
                    matched += 1
                    worst_dx = max(worst_dx, abs(bx - x))
                    worst_dy = max(worst_dy, abs(by - y))
                    continue
            missing.append((page, t, round(x, 1), round(y, 1)))

    if total == 0:
        sys.exit('no words extracted; are these text PDFs?')
    print(f'{pages} pages: matched {matched}/{total} words ({100 * matched / total:.1f}%)')
    print(f'worst dx={worst_dx:.2f}pt  worst dy={worst_dy:.2f}pt')
    if missing:
        print(f'unmatched ({len(missing)}, showing {min(args.show, len(missing))}):')
        for page, t, x, y in missing[:args.show]:
            print(f'   p{page} {t!r} at x={x} y={y}')

    ok = not missing
    if args.links:
        print('links:')
        ok = compare_links(args.reference, args.candidate) and ok
    sys.exit(0 if ok else 1)


if __name__ == '__main__':
    main()
