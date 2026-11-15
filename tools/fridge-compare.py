#!/usr/bin/env python3
# Compares /shabbat/fridge.cgi on this service (GO, default :8082) with
# hebcal-web's (NODE, default :8080) for every query string in a file:
#
#   python3 tools/fridge-compare.py tools/fridge-cases.txt
#
# The pages are compared as rendered, not byte for byte: the text and class of
# every table cell, the title, heading, footnotes, the links to other years
# and their base URL (as a set of parameters), and the CSV rows. Masked,
# because they are known: a time one minute apart (counted as "minute"); the
# rows hebcal-web names "Latest Kiddush Levana" (counted as "kl"); and the
# explicit b= this service always writes into the base URL.
import os, sys, re, html, urllib.request, urllib.parse, itertools
GO=os.environ.get('GO','http://127.0.0.1:8082')+'/shabbat/fridge.cgi?'
NODE=os.environ.get('NODE','http://127.0.0.1:8080')+'/shabbat/fridge.cgi?'
def fetch(u):
    try:
        with urllib.request.urlopen(u) as r:
            return r.status, r.read().decode('utf-8'), dict(r.headers)
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode('utf-8','replace'), dict(e.headers)
def parse(body):
    out={}
    out['lang']=re.search(r'<html lang="([^"]*)" dir="([^"]*)"',body).groups()
    out['title']=html.unescape(re.search(r'<title>(.*?)</title>',body,re.S).group(1))
    out['h3']=re.sub(r'\s+',' ',html.unescape(re.sub(r'<[^>]+>',' ',re.search(r'<h3.*?</h3>',body,re.S).group(0)))).strip()
    tb=re.search(r'<tbody>(.*?)</tbody>',body,re.S).group(1)
    out['cells']=[(c or '',html.unescape(t)) for c,t in re.findall(r'<td(?: class="([^"]*)")?>(.*?)</td>',tb)]
    foot=body[body.index('id="fridge-nav"'):body.index('</table>',body.index('id="fridge-nav"'))]
    out['foot']=re.sub(r'\s+',' ',html.unescape(re.sub(r'<[^>]+>',' ',foot[foot.index('>')+1:]))).strip()
    out['nav']=re.findall(r'title="([^"]*)"\s+data-year="(\d+)"( data-yt="G")?',foot)
    base=html.unescape(re.search(r'data-base(?:-url)?="([^"]*)"',body).group(1))
    out['base']=sorted(urllib.parse.parse_qsl(base.split('?',1)[1]))
    out['fonts']=('fonts.googleapis' in body, 'adobehebrew-bold' in body, 'adobehebrew-regular' in body)
    return out
def tdiff(a,b):
    # times a minute apart
    ma=re.fullmatch(r'(\d+):(\d\d)',a); mb=re.fullmatch(r'(\d+):(\d\d)',b)
    if not ma or not mb: return False
    x=int(ma[1])*60+int(ma[2]); y=int(mb[1])*60+int(mb[2])
    return abs(x-y)<=1 or abs(x-y)>=719
KLRE=re.compile('Kiddush|Kiddouch|Kidusz|Levana|Lewana|Левана|Лєвана|Kidush|Kiddusch|Kidoesj|levana',re.I)
stats={'urls':0,'same':0,'cells':0,'minute':0,'diff':0}
def compare(qs):
    stats['urls']+=1
    sg,bg,hg=fetch(GO+qs); sn,bn,hn=fetch(NODE+qs)
    if sg!=sn:
        print('STATUS',qs,'go',sg,bg[:80].strip(),'node',sn); stats['diff']+=1; return
    if sg!=200:
        stats['same']+=1; return
    if 'cfg=csv' in qs:
        la=bg.split('\r\n'); lb=bn.split('\r\n')
        bad=0
        if len(la)!=len(lb): print('CSVLEN',qs,len(la),len(lb)); bad=1
        for x,y in zip(la,lb):
            stats['cells']+=1
            if x==y: continue
            fx=x.split(','); fy=y.split(',')
            if len(fx)==4 and fx[:2]==fy[:2] and all(p==q or tdiff(p,q) for p,q in zip(fx[2:],fy[2:])): stats['minute']+=1; continue
            print('CSV',qs,repr(x),repr(y)); bad=1
        dg=hg.get('Content-Disposition'); dn=hn.get('Content-Disposition')
        if dg!=dn: print('DISP',qs,dg,dn); bad=1
        stats['diff' if bad else 'same']+=1
        return
    a=parse(bg); b=parse(bn); bad=0
    a['base']=[kv for kv in a['base'] if kv[0]!='b' or any(k=='b' for k,_ in b['base'])]
    kl=0
    for i in range(0,min(len(a['cells']),len(b['cells']))):
        if 'Kiddush Levana' in b['cells'][i][1] or 'קִדּוּשׁ לְבָנָה' in b['cells'][i][1] or 'קידוש לבנה' in b['cells'][i][1] or KLRE.search(b['cells'][i][1]):
            kl+=1
            # the 11-cell table row holds two 5-cell halves; copy Go's half over Node's
            r=i-i%11; off=i%11; h=r if off<5 else r+6
            b['cells'][h:h+5]=a['cells'][h:h+5]
    stats['kl']=stats.get('kl',0)+kl
    for k in a:
        if k=='cells': continue
        if a[k]!=b[k]:
            print('DIFF',k,qs,'\n  go  ',a[k],'\n  node',b[k]); bad=1
    if len(a['cells'])!=len(b['cells']):
        print('NCELLS',qs,len(a['cells']),len(b['cells'])); bad=1
    for i,(x,y) in enumerate(zip(a['cells'],b['cells'])):
        stats['cells']+=1
        if x==y: continue
        if x[0]==y[0] and tdiff(x[1],y[1]): stats['minute']+=1; continue
        print('CELL',qs,i,x,y, a['cells'][i-i%11:i-i%11+3]); bad=1
    stats['diff' if bad else 'same']+=1
for line in open(sys.argv[1]):
    line=line.strip()
    if line and not line.startswith('#'): compare(line)
print(stats)
