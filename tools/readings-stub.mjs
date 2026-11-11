// A stand-in for readings-svc's /torahMemo, for comparing the iCalendar feeds
// against hebcal-web without the sidecar.
//
//   node tools/readings-stub.mjs /tmp/readings-svc.sock
//
// It runs hebcal-web's own makeTorahMemoText() over every holiday and weekly
// portion in the range, which is what the sidecar's route does, and answers on
// a unix socket (the -readings-socket flag of hebcal-api). Only /torahMemo is
// served; anything else is a 404, as with an older sidecar.
import http from 'node:http';
import {rmSync} from 'node:fs';
import {importFromWeb} from './hebcalweb.mjs';

const {calendar} = await importFromWeb('node_modules/@hebcal/core/dist/esm/index.js');
const {makeTorahMemoText} = await importFromWeb('src/torahMemo.js');

const socketPath = process.argv[2];
if (!socketPath) {
  console.error('usage: readings-stub.mjs <socket>');
  process.exit(1);
}
rmSync(socketPath, {force: true});

const pad = (n, w) => String(n).padStart(w, '0');
const iso = (hd) => {
  const d = hd.greg();
  return `${pad(d.getFullYear(), 4)}-${pad(d.getMonth() + 1, 2)}-${pad(d.getDate(), 2)}`;
};
const parse = (s) => {
  const [y, m, d] = s.split('-').map(Number);
  return new Date(y, m - 1, d);
};

http.createServer((req, res) => {
  const u = new URL(req.url, 'http://x');
  if (u.pathname !== '/torahMemo') {
    res.writeHead(404);
    res.end('not found');
    return;
  }
  const il = u.searchParams.get('i') === 'on';
  const events = calendar({
    start: parse(u.searchParams.get('start')),
    end: parse(u.searchParams.get('end')),
    il,
    sedrot: true,
  });
  const rows = [];
  for (const ev of events) {
    const memo = makeTorahMemoText(ev, il);
    if (memo) {
      rows.push({date: iso(ev.getDate()), desc: ev.getDesc(), memo});
    }
  }
  res.writeHead(200, {'Content-Type': 'application/json'});
  res.end(JSON.stringify(rows));
}).listen(socketPath, () => console.error('readings stub on', socketPath));
