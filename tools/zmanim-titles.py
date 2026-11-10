#!/usr/bin/env python3
"""Regenerates internal/service/zmanim/titles.go from hebcal-web's po/
directory: the TITLE:<zman> and MEMO:<zman> strings of po/en.po that the
/zmanim and /sunrs feeds use, and the translations of those titles that
hebcal-web's po/he.po, po/he-x-NoNikud.po and po/ashkenazi.po add on top of
@hebcal/core's catalogs.

    tools/zmanim-titles.py [../hebcal-web/po] && gofmt -w internal/service/zmanim/titles.go

Temporary: once github.com/hebcal/locales carries these keys (via po2catalog),
the feed can look them up there and this file goes away.
"""
import json
import os
import re
import sys

podir = sys.argv[1] if len(sys.argv) > 1 else '../hebcal-web/po'
here = os.path.dirname(os.path.abspath(__file__))
dst = os.path.join(here, '..', 'internal', 'service', 'zmanim', 'titles.go')
PAIR = r'msgid "([^"]*)"\nmsgstr "([^"]*)"'


def read(name):
    with open(os.path.join(podir, name + '.po'), encoding='utf-8') as f:
        return re.findall(PAIR, f.read())


titles, memos = {}, {}
for key, val in read('en'):
    if key.startswith(('TITLE:', 'MEMO:')):
        kind, name = key.split(':', 1)
        (titles if kind == 'TITLE' else memos)[name] = val
english = set(titles.values())
# he-x-NoNikud is he with the nikud stripped, then its own file on top; only
# the overrides are stored here.
translations = {}
for locale in ('he', 'he-x-NoNikud', 'ashkenazi'):
    translations[locale] = {k: v for k, v in read(locale) if k in english and v}


def table(d):
    return ''.join('\t%s: %s,\n' % (json.dumps(k), json.dumps(v, ensure_ascii=False))
                   for k, v in d.items())


with open(dst, 'w', encoding='utf-8') as f:
    f.write('// Code generated from hebcal-web po/en.po by tools/zmanim-titles.py; DO NOT EDIT.\n\n'
            'package zmanim\n\n'
            "// zmanTitles is each zman's English event title, untranslated; it is also\n"
            '// the key its translation is looked up by and the string its UID hashes.\n'
            'var zmanTitles = map[string]string{\n' + table(titles) + '}\n\n'
            "// zmanMemos is each zman's English explanation, an event's DESCRIPTION.\n"
            'var zmanMemos = map[string]string{\n' + table(memos) + '}\n\n'
            '// zmanTitleTranslations are the translations of the titles that take\n'
            "// precedence over the locales package's. he-x-NoNikud holds only its\n"
            '// overrides; the rest of it is he with the nikud stripped.\n'
            'var zmanTitleTranslations = map[string]map[string]string{\n' +
            ''.join('\t%s: {\n%s\t},\n' % (json.dumps(loc), table(d).replace('\t', '\t\t'))
                    for loc, d in translations.items()) + '}\n')
print('wrote %d titles, %d memos' % (len(titles), len(memos)))
