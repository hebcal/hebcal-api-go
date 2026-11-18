// Package fonts carries the faces the PDF calendars are drawn with, compiled
// into the binary so that a calendar is a function of the request and the
// build alone: the PDF routes' ETag hashes the release version, not the font
// files, and a deployment cannot be missing them.
package fonts

import "embed"

// FS holds Source_Sans_Pro/*.ttf and Adobe_Hebrew/*.otf at those paths.
//
//go:embed Source_Sans_Pro/*.ttf Adobe_Hebrew/*.otf
var FS embed.FS
