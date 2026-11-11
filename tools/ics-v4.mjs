// Prints the /v4/ download path hebcal-web would link for a query string, so
// the three URL shapes of a calendar can be compared.
//
//   node tools/ics-v4.mjs 'v=1&year=2026&maj=on&c=on&geonameid=5128581' hebcal.ics
import {importFromWeb} from './hebcalweb.mjs';

const {downloadHref2} = await importFromWeb('src/makeDownloadProps.js');
const query = Object.fromEntries(new URLSearchParams(process.argv[2]));
const href = downloadHref2(query, process.argv[3] || 'hebcal.ics');
console.log(href.replace(/^https?:\/\/[^/]+/, ''));
