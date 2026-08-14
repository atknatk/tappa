package pages

// The OPERATOR's legal-text screen (M7-06) — the form that ends three sessions of
// waiting for four documents.
//
// 🔴 IT IS THE ONE PANEL SCREEN THAT IS NOT ABOUT THE SIGNED-IN BUSINESS. Every
// other section reads or writes rows belonging to the tenant whose session made the
// request; this one publishes TAPPA's own text, which every visitor to /legal reads.
// The view carries no tenant, no employee and no record, and nothing on it is
// derived from the caller's tenant — see internal/handler/legaladmin.go for the gate
// and migration 00020 for why the rows have no tenant at all.

// LegalAdminView is the operator screen.
type LegalAdminView struct {
	PanelChrome
	// PostHref is where the form posts. It is the route constant, so the form and
	// the route are the same value at compile time.
	//
	// ⚠️ THERE IS NO SYNCHRONIZER TOKEN ON THIS FORM AND ITS ABSENCE IS DELIBERATE.
	// Measured: every `name="csrf"` field in this product is on an UNAUTHENTICATED
	// surface (sign-in, activation, sign-up) and is read by that flow's own handler;
	// no panel write reads one, because AdminAuth.ProtectWriting puts sameOriginGate
	// ahead of the resolver instead. Minting a token here that nothing verifies
	// would be a control that looks like protection and is not — worse than none.
	PostHref string
	// Docs is one editor per document, in the order LegalPages lists them, so the
	// screen and the public footer read in the same sequence.
	Docs []LegalEditor
	// Saved names the document just published, for the confirmation line. Empty on
	// a plain page load.
	Saved string
	// Problem is the sentence to show when something was refused. It comes from a
	// CLOSED list in internal/handler; nothing a caller types reaches this field.
	Problem string
}

// LegalEditor is one document's row on the operator screen.
type LegalEditor struct {
	// Slug is the value the form posts back. It is validated against the closed set
	// again on the server — this field is a convenience, never an authority.
	Slug string
	// Title and Lede are the same strings the public page shows, read from
	// LegalPages, so the operator is writing into a document they can recognise.
	Title string
	Lede  string
	// Needs is the list the public skeleton prints while the document is
	// unpublished. It is shown HERE too, which is the point of the screen: the
	// person who has to supply those facts can see them next to the box they type
	// into.
	Needs []string
	// Body is the current text, verbatim, so the textarea re-opens on what was
	// published rather than on a re-rendering of it.
	Body string
	// PublishedAt is when the current version was published, already formatted.
	// Empty when there is no version.
	PublishedAt string
	// Versions is how many versions exist for this document. It is 0 or 1 as far as
	// this screen can tell: the snapshot holds only the current one, so the field
	// says "published" or "not published" and does not pretend to a history it has
	// not read.
	Published bool
}

// Published reports whether every document has a text.
//
// 🔴 IT IS "EVERY", NOT "ANY", AND THE DIFFERENCE IS WHAT THE PUBLIC PAGES SAY. A
// deployment that has published two of the four is a deployment where two documents
// are in force and two are not, and each page decides for ITSELF (LegalPage.Publish
// on the view it is rendered with). This is only the operator's own progress line.
func (v LegalAdminView) AllPublished() bool {
	if len(v.Docs) == 0 {
		return false
	}
	for _, d := range v.Docs {
		if !d.Published {
			return false
		}
	}
	return true
}

// PublishedCount is how many of the documents have a text, for the progress line.
// It is DERIVED rather than carried, so it cannot disagree with the rows below it.
func (v LegalAdminView) PublishedCount() int {
	n := 0
	for _, d := range v.Docs {
		if d.Published {
			n++
		}
	}
	return n
}
