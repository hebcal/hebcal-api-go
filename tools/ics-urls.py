#!/usr/bin/env python3
"""Compares holiday-feed URLs as hebcal-web and this service render them.

    tools/ics-urls.py urls.txt result.json

Reads one URL path per line (quotes stripped), fetches each from hebcal-web
(WEB, default localhost:8081; a comma-separated list is used round-robin, so
several Node servers can share the load) and from this service (API, default
localhost:8082), and writes the URLs that differ, with their diff lines, to
result.json. J sets the concurrency (default 6), AGENT the User-Agent.

What is normalized away before comparing: line folding, DTSTAMP, the minute of
a sun-derived DTSTART/DTEND or absolute alarm (and the date of an alarm a
minute either side of 00:00Z), Rosh Hashana's year in Hebrew,
the order of the VEVENTs (which no calendar client depends on), and the
documented differences in CLAUDE.md that recur in every sweep -- Yom Kippur
Katan's emoji and apostrophe, the candle-lighting time inside a Kiddush Levana
description, the Hebrew half of an lg=ah/sh Mevarchim title, and the escaping
of commas and the nikud in the Hebrew half of any lg=ah/sh title, and the
repeated English of an lg=ah/sh truncation notice.
"""
import sys, re, datetime, itertools, threading, concurrent.futures as cf, urllib.request, urllib.error, difflib, json, os
WEBS=os.environ.get('WEB','localhost:8081').split(','); API=os.environ.get('API','localhost:8082')
_web=itertools.cycle(WEBS); _lock=threading.Lock()
def next_web():
    with _lock: return next(_web)
nikud=re.compile('[\u0591-\u05bd\u05bf\u05c1\u05c2\u05c4\u05c5\u05c7]')
hashana=re.compile(r'((רֹאשׁ הַשָּׁנָה|ראש השנה)) [^ \\]*')
def fetch(host,u):
    req=urllib.request.Request('http://'+host+u, headers={'User-Agent':os.environ.get('AGENT','curl')})
    try:
        with urllib.request.urlopen(req, timeout=120) as r: return r.status, r.read().decode('utf-8','replace')
    except urllib.error.HTTPError as e: return e.code, e.read().decode('utf-8','replace')
    except Exception as e: return -1, str(e)
def unfold(t):
    return t.replace('\r\n ','').replace('\r','').split('\n')
def norm(lines):
    out=[]
    for l in lines:
        if l.startswith('DTSTAMP:'): l='DTSTAMP:X'
        l=re.sub(r'^(DT(START|END);TZID=[^:]*:\d{8}T)\d+$', r'\1XXXX', l)
        m=re.match(r'^TRIGGER;VALUE=DATE-TIME:(\d{8}T\d{6})Z$', l)
        if m:
            # An Omer alarm is an evening's zman in UTC; a minute's difference
            # can straddle 00:00Z and change the date. The date 12 hours on
            # is the same either way, as no Omer evening is near 12:00Z.
            t=datetime.datetime.strptime(m.group(1),'%Y%m%dT%H%M%S')+datetime.timedelta(hours=12)
            l='TRIGGER;VALUE=DATE-TIME:'+t.strftime('%Y%m%d')+'+12h'
        l=hashana.sub(r'\1 YEAR', l)
        out.append(l)
    return out
def blocks(lines):
    # VEVENT order is irrelevant to clients: compare the events as a sorted list
    head=[];evs=[];cur=None
    for l in lines:
        if l=='BEGIN:VEVENT': cur=[l]
        elif cur is not None:
            cur.append(l)
            if l=='END:VEVENT': evs.append(cur); cur=None
        else: head.append(l)
    for e in evs:  # documented differences
        ykk=any('Minor Day of Atonement' in l or re.search(r'Kip+ur Katan|קָטָן|קטן',l) for l in e)
        for i,l in enumerate(e):
            if ykk:
                l=l.replace('✡️ ','').replace("Sh'vat",'Sh’vat')
            if l.startswith('DESCRIPTION:') and re.search('Kiddush Levana|קידוש לבנה|קִדּוּשׁ לְבָנָה|Kidush Levana',l):
                l=re.sub(r'\(([^()]*?)\d{1,2}:\d{2}(am|pm)?',r'(\1T',l)
            if l.startswith('SUMMARY:') and re.search('Mev[oa]r[cg]him Chodesh',l):
                l=l.split(' / ')[0]
            if l=='SUMMARY:Hebcal calendar feed truncated / Hebcal calendar feed truncated':
                l='SUMMARY:Hebcal calendar feed truncated'  # known difference 11
            if l.startswith('SUMMARY:') and ' / ' in l:
                # lg=ah/sh: hebcal-web leaves the Hebrew half's commas bare
                a,_,b=l.partition(' / ')
                # and writes nikud there for readings-svc series; this does not
                l=a+' / '+nikud.sub('',b.replace('\\,',','))
            e[i]=l
    evs.sort(key=lambda e:'\n'.join(e))
    return head+[l for e in evs for l in e]
def one(u):
    a=fetch(next_web(),u); b=fetch(API,u)
    if a[0]!=b[0]: return u, {'status':(a[0],b[0])}, []
    if a[0]!=200: return u, None, []
    d=[l for l in difflib.unified_diff(blocks(norm(unfold(a[1]))),blocks(norm(unfold(b[1]))),lineterm='',n=0) if l[:1] in '+-' and not l.startswith(('+++','---'))]
    return u, ({'diff':len(d)} if d else None), d
urls=[l.strip().strip('"') for l in open(sys.argv[1]) if l.strip()]
res=[]
with cf.ThreadPoolExecutor(int(os.environ.get('J','6'))) as ex:
    for u,s,d in ex.map(one, urls):
        if s: res.append({'url':u,**s,'lines':d[:40]})
json.dump(res, open(sys.argv[2],'w'), ensure_ascii=False, indent=1)
print(len(urls),'checked',len(res),'differ')
