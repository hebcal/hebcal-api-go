// Prints pdfkit's text measurements, which are the reference values the Go
// shaper is checked against in width_parity_test.go.
//
//   node tools/measure-widths.mjs                        # the pinned strings
//   node tools/measure-widths.mjs hebrew 12 'פרשת ויחי'   # one measurement
//
// Two things make this worth having rather than eyeballing a rendered page.
//
// pdfkit draws Hebrew only after reverseHebrewWords() has rewritten it, and
// that function rejoins words with two spaces, so the string on the page is
// not the string in the event. The width to match is the rewritten one, which
// is what the "as drawn" column reports.
//
// And advances have to be taken at the size being drawn. Shaping directly at
// 8.5pt quantises them to the pixel grid -- an 8.5pt space came out 1.797pt
// instead of 1.700, about 5.8% wide -- which is why the Go side shapes at a
// reference size and scales. These numbers are what caught that.
import {fontDoc, reverseHebrewWords} from './hebcalweb.mjs';

const doc = await fontDoc();

/** Returns true if the string contains any Hebrew letters. */
function isHebrew(s) {
  return /[֐-׿]/.test(s);
}

/**
 * Measures one string, reporting both the source width and the width of what
 * pdfkit actually draws.
 * @param {string} font
 * @param {number} size
 * @param {string} s
 */
function measure(font, size, s) {
  doc.font(font).fontSize(size);
  const source = doc.widthOfString(s);
  const drawn = isHebrew(s) ? doc.widthOfString(reverseHebrewWords(s)) : source;
  const note = drawn === source ? '' : `  (source ${source.toFixed(4)})`;
  console.log(
      `${font.padEnd(12)}${String(size).padStart(5)}  ${JSON.stringify(s).padEnd(30)}` +
    `${drawn.toFixed(4).padStart(10)}${note}`);
}

const [font, size, str] = process.argv.slice(2);
if (font && size && str) {
  measure(font, Number(size), str);
  process.exit(0);
}

console.log('font          size  string                              as drawn');
// The strings pinned in width_parity_test.go, plus the space that exposed the
// quantisation.
for (const [f, sz, s] of [
  ['plain', 10, 'Candle lighting: 6:56pm'],
  ['plain', 10, 'Parashat Vayakhel-Pekudei'],
  ['semi', 10, 'Rosh Chodesh Adar II'],
  ['semi', 26, 'August 2026'],
  ['bold', 8.5, '7:12pm '],
  ['bold', 8.5, '8:51p '],
  ['bold', 8.5, ' '],
  ['hebrew', 12, 'פרשת ויחי'],
  ['hebrew', 12, 'ראש חודש שבט'],
  ['hebrew', 12, 'ינואר 2027'],
  ['hebrew', 12, 'פָּרָשַׁת וַיְחִי'],
  ['hebrew', 12, 'רֹאשׁ חֹדֶשׁ שְׁבָט'],
]) {
  measure(f, sz, s);
}
