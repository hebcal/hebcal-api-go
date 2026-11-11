package pdf

import "github.com/hebcal/hebcal-api-go/internal/service/download"

// utmMedium tags the external links a PDF carries.
const utmMedium = "document"

// eventLink turns the canonical URL from hebcal-go into the short, tracked
// form that appears in production PDFs, e.g.
//
//	https://www.hebcal.com/sedrot/eikev-20260808  ->  https://hebcal.com/s/5786/46?uc=pdf-palo-alto-2026-2027
//	https://www.hebcal.com/holidays/sukkot-2026   ->  https://hebcal.com/h/sukkot-2026?uc=pdf-palo-alto-2026-2027
//
// Returns "" when the event has no page, so callers can skip the annotation.
func eventLink(rawURL string, hyear int, campaign string, il bool) string {
	return download.EventLink(rawURL, hyear, campaign, utmMedium, il)
}
