// Regenerates internal/service/staticcal/testdata: golden feeds rendered by
// hebcal-web's own src/staticCalendars.js, and the readings-svc responses the
// Go side needs to render the same feeds.
//
//   READINGS_SOCKET=/path/to/readings-svc.sock node tools/static-ical-goldens.mjs
//
// It needs a hebcal-web checkout with its dependencies installed (see
// hebcalweb.mjs) and a running readings-svc. Everything is pinned to the date
// in testdata/TODAY, so the goldens do not drift until it is changed.
import {gzipSync} from 'node:zlib';
import {mkdirSync, readFileSync, writeFileSync} from 'node:fs';
import http from 'node:http';
import {importFromWeb} from './hebcalweb.mjs';

const out = 'internal/service/staticcal/testdata';
const socketPath = process.env.READINGS_SOCKET;
if (!socketPath) {
  console.error('Set READINGS_SOCKET to a running readings-svc socket');
  process.exit(1);
}

// The feeds kept as goldens: every kind of event and description, not every
// feed (most of the daily-learning ones differ only in their series).
const goldens = new Set([
  'jewish-holidays-v2', 'torah-readings-israel-he', 'rosh-chodesh', 'omer',
  'mevarchim', 'yom-kippur-katan', 'yizkor-diaspora', 'yizkor-il',
  'hdate-en', 'hdate-he-v2', 'tanakh-yomi', 'yerushalmi-vilna',
  'dirshu-daf-halacha', 'chofetz-chaim',
]);

const {IcalEvent} = await importFromWeb('node_modules/@hebcal/icalendar/dist/icalendar.js');
const {buildAllCalendars, renderCalendar, getStartAndEnd} =
  await importFromWeb('src/staticCalendars.js');

const today = new Date(readFileSync(`${out}/TODAY`, 'utf-8').trim() + 'T12:00:00Z');
const dtstamp = IcalEvent.makeDtstamp(today);
mkdirSync(`${out}/golden`, {recursive: true});
for (const cal of buildAllCalendars(today)) {
  if (!goldens.has(cal.file)) {
    continue;
  }
  const {ics, csv} = await renderCalendar(cal, dtstamp);
  writeFileSync(`${out}/golden/${cal.file}.ics.gz`, gzipSync(ics));
  // The .csv is written differently on purpose (quoting, commas); this
  // copy is the reference for its fields, not its bytes.
  writeFileSync(`${out}/golden/${cal.file}.csv.gz`, gzipSync(csv));
  console.log(cal.file, cal.events.length);
}

/**
 * GETs a path from readings-svc and returns the parsed JSON.
 * @param {string} path
 * @return {Promise<any>}
 */
function fetchSidecar(path) {
  return new Promise((resolve, reject) => {
    http.get({socketPath, path}, (res) => {
      let body = '';
      res.setEncoding('utf8');
      res.on('data', (chunk) => body += chunk);
      res.on('end', () => res.statusCode === 200 ?
        resolve(JSON.parse(body)) :
        reject(new Error(`${path}: ${res.statusCode} ${body}`)));
    }).on('error', reject);
  });
}

const iso = (hd) => hd.greg().toISOString().slice(0, 10);
const span = (years) => {
  const {start, end} = getStartAndEnd(today, years);
  return `start=${iso(start)}&end=${iso(end)}`;
};

// The widest window each schedule's feeds use; the stub serves a narrower
// range out of it.
const fixtures = {
  'torahMemo-diaspora': `/torahMemo?${span(10)}`,
  'torahMemo-israel': `/torahMemo?${span(10)}&i=on`,
  'learning': `/learning?${span(2)}&ddh=on&dcc=on&dshl=on`,
};
for (const [name, path] of Object.entries(fixtures)) {
  let json = await fetchSidecar(path);
  if (name === 'learning') {
    json = json.items;
  }
  writeFileSync(`${out}/readings/${name}.json.gz`, gzipSync(JSON.stringify(json)));
  console.log(name, json.length);
}
