// Shared helper for the tools that need hebcal-web's Node dependencies.
//
// These tools compare this renderer against the Node implementation, so they
// import pdfkit, @hebcal/core and dayjs out of hebcal-web's node_modules
// rather than vendoring a second copy. Node resolves bare specifiers relative
// to the importing file, so a tool living here cannot simply `import 'pdfkit'`
// even when run with hebcal-web as the working directory; the imports have to
// be absolute file URLs, which is what resolveWeb builds.
import {existsSync} from 'node:fs';
import {resolve} from 'node:path';
import {pathToFileURL} from 'node:url';

/**
 * webRoot is the hebcal-web checkout. Override with HEBCAL_WEB when it is not
 * a sibling of this repository.
 */
export const webRoot = resolve(process.env.HEBCAL_WEB || '../hebcal-web');

if (!existsSync(resolve(webRoot, 'node_modules'))) {
  console.error(
      `hebcal-web not found at ${webRoot}, or its dependencies are not installed.\n` +
    'These tools compare against the Node implementation, so they need it.\n' +
    'Set HEBCAL_WEB to the checkout, and run "npm install" there first.');
  process.exit(1);
}

/**
 * Imports a module from hebcal-web's dependency tree.
 * @param {string} rel path relative to the hebcal-web checkout
 * @return {Promise<object>} the module namespace
 */
export function importFromWeb(rel) {
  return import(pathToFileURL(resolve(webRoot, rel)).href);
}

/** Imports pdfkit, the reference for every text measurement. */
export async function pdfkit() {
  const m = await importFromWeb('node_modules/pdfkit/js/pdfkit.js');
  return m.default;
}

/** Imports @hebcal/core, the reference for events and their URLs. */
export function hebcalCore() {
  return importFromWeb('node_modules/@hebcal/core/dist/esm/index.js');
}

/**
 * Returns a pdfkit document with the calendar fonts registered under the same
 * names src/pdf.js uses, which are also the names in fonts.go.
 * @return {Promise<object>}
 */
export async function fontDoc() {
  const PDFDocument = await pdfkit();
  const doc = new PDFDocument({autoFirstPage: false});
  const fonts = {
    'plain': 'fonts/Source_Sans_Pro/SourceSansPro-Regular.ttf',
    'semi': 'fonts/Source_Sans_Pro/SourceSansPro-SemiBold.ttf',
    'bold': 'fonts/Source_Sans_Pro/SourceSansPro-Bold.ttf',
    'hebrew': 'fonts/Adobe_Hebrew/adobehebrew-regular.otf',
    'hebrew-bold': 'fonts/Adobe_Hebrew/adobehebrew-bold.otf',
  };
  for (const [name, rel] of Object.entries(fonts)) {
    doc.registerFont(name, resolve(webRoot, rel));
  }
  doc.addPage();
  return doc;
}

/**
 * reverseHebrewWords is a verbatim port of the function in hebcal-web's
 * src/pdf.js. It exists there because pdfkit offers no bidi support: it
 * reverses word order by hand, swaps parentheses, and moves a trailing comma
 * to the front. Crucially it rejoins with *two* spaces, which is why published
 * Hebrew calendars carry wider word gaps than a single space would give.
 *
 * This renderer does not use it -- it applies the real bidi algorithm -- but
 * the string pdfkit ends up drawing is the one to measure against, so the
 * width tools need it.
 * @param {string} subj
 * @return {string}
 */
export function reverseHebrewWords(subj) {
  const s1 = String.fromCharCode(1);
  const s2 = String.fromCharCode(2);
  subj = subj.replace('(', s1).replace(')', s2);
  subj = subj.replace(s1, ') ').replace(s2, '(');
  const words = subj.split(' ').reverse();
  for (let i = 0; i < words.length; i++) {
    if (words[i].endsWith(',')) {
      words[i] = ',' + words[i].substring(0, words[i].length - 1);
    }
  }
  return words.join('  ').replace('   ', '  ');
}
