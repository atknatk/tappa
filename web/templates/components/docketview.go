package components

// The TRANSACTION DOCKET and the FILTER BAR's inputs (M6-03).
//
// 🔴 WHY THESE TYPES LIVE IN components RATHER THAN IN pages, because the rest of
// the product's view models are in pages and this looks like an exception. It is
// the rule this package already follows: components.Tab is defined here too, next
// to the TabBar that renders it. The reason is mechanical rather than stylistic —
// pages imports components, so a component taking a pages type would close an
// import cycle. A component owns the shape of its own input.
//
// 🔴 §4.7 — THE ABSENCE OF THREE FIELDS ON DocketView IS THE SPEC, and it is the
// same discipline AdminLoginView applies to a password: a template cannot render
// what the view has no field to carry. There is no Latitude, no Longitude and no
// SourceIP here. The record on disk has all three (migration 0005); the query does
// not select them (db/queries/transactions.sql, ListPanelTransactions), the domain
// type has no field for them (internal/domain/ledger), and neither does this.
// Three independent walls — removing any one still leaves a coordinate unable to
// reach a screen, an HTML attribute, a log line or a CSV.
//
// WHAT IS SHOWN INSTEAD IS THE SIGNS. §5 rows 6-7 decide on "did the IP match" and
// "was the phone inside the radius", not on the values, so the signs are what a
// manager needs in order to read a verdict — and they are exactly what skill
// tappa-brand's docket sketch prints: `TRUST 100  IP ✓ GPS ✓`.
//
// EVERY TIME IS A PRE-FORMATTED STRING, as in ResultView: CLAUDE.md §6 keeps
// instants in UTC everywhere below the render layer, and the conversion into the
// tenant's zone happens once, in the handler, against the zone the database gave.

// DocketView is one transaction as a kitchen docket — the motif this product is
// recognised by. It is NOT a table row, and that is a brand rule (skill
// tappa-brand's "Yapma" list ends with exactly that substitution).
type DocketView struct {
	// Venue is the location's name, or "" when the record carries none. Empty is
	// a legitimate state: §4.6 requires a record to be written even when almost
	// nothing about it could be resolved.
	Venue string
	// Department is the department's name, "" when the person has none.
	Department string
	// Who is the employee's full name, or "" for a record written before the
	// session was resolved — a stolen plaque touched with no cookie (§5 rows 1-2
	// come BEFORE row 3). The template says so in words rather than leaving a gap.
	Who string
	// Direction is "in", "out", or "" for a verdict that carries none (migration
	// 0005: reject and ignored have a NULL type).
	Direction string
	// At is the wall-clock time in the tenant's zone, already formatted.
	At string
	// Trust is the confidence score as text ("100"), or "" when the engine
	// recorded none. A STRING RATHER THAN AN INT because 0 is a real score and
	// "absent" must not render as one.
	Trust string
	// IPSign and GPSSign are the two evidence signs, as text. THREE VALUES, NOT
	// TWO — migration 0005 keeps both columns three-state, and collapsing "not
	// assessed on this channel" into "failed" would report a manual entry as
	// having failed a check nobody ran.
	IPSign  string
	GPSSign string
	// TagUID is the plaque's uid, "" for a manual entry that had no plaque. It is
	// not a secret: skill tappa-brand records that it is printed on the plaque and
	// already appears in the address bar, so it is outside §4.7.
	TagUID string
	// Ctr is the chip's read counter as text, "" when there is none.
	Ctr string
	// Verdict is ok | flag | reject | ignored. It picks the stamp, and the stamp
	// carries the WORD as well as the colour, so status is never told by colour
	// alone.
	Verdict string
	// Practice marks the post-activation training tap with a TRAINING stamp.
	//
	// ⚠️ NOTHING IS CLAIMED HERE ABOUT HOURS. §5 says a practice tap never counts
	// toward worked time and the employee's own result screen says so — but the
	// panel has no hours to count yet (totalling is M6-07 and does not exist in
	// the repo), so a sentence about what this does not add up to would be a
	// screen describing arithmetic the product has not done.
	Practice bool
	// Manual marks a record a manager typed rather than one somebody tapped. §5
	// requires it to be distinguishable wherever it is reported; this is the
	// panel's half of that.
	Manual bool
	// Queued marks a record the tap engine put in the approval queue. It is a FACT
	// about the record, not a control: the docket itself approves nothing, because
	// approving writes and §4.3 makes this view read-only. The review section
	// (M6-04) renders its own form BESIDE the card rather than inside it.
	Queued bool
	// Review is the MANAGER's decision — "approved", "rejected", or "" for none
	// yet.
	//
	// 🔴 IT DOES NOT REPLACE Verdict AND THE TEMPLATE RENDERS BOTH. The stamp is
	// what the ENGINE decided and §4.3 makes it permanent; this is what a human
	// decided afterwards, and it lives in transaction_reviews. A card that swapped
	// FLAGGED for APPROVED would be rewriting the record on screen — the exact edit
	// Q20 exists to prevent, performed in the read path instead of the write path.
	Review string
	// ReviewNote is what the deciding manager typed, "" when they typed nothing.
	//
	// IT IS THE ONLY FIELD ON THIS TYPE A HUMAN COMPOSED. A round of M8-04 struck
	// that sentence out on the ground that Note below could also be tenant-written
	// prose; the audit after it measured the write paths and put the sentence back
	// (see Note). What a customer chooses about a policy is which shipped statement
	// applies and where — never its words.
	//
	// templ escapes it on output, proved rather than asserted, by
	// TestReviewDB_AHostileNoteIsEscapedWhereItIsRendered — a note stored through the
	// real POST, containing a script tag, a quote and an ampersand, read back off the
	// real page. Note below has its own proof
	// (TestPolicyNote_IsEscapedOnBothSurfacesThatPrintIt); the two are separate
	// because they arrive by different routes and one test cannot stand for both.
	//
	// ⚠️ THE ESCAPING IS templ's AND IT DOES NOT TRAVEL. M6-07's CSV export will
	// read the same column and get none of it, and a spreadsheet treats a cell
	// beginning = + - or @ as a formula. That rule belongs to whoever writes the
	// export; rendering the note here does not discharge it.
	//
	// IT IS RENDERED BESIDE THE DECISION IT BELONGS TO, not in a separate panel:
	// the manager who decides and the manager who reads have to share a surface, or
	// the note is written into a place nobody looks.
	ReviewNote string
	// Note is the deciding rule's sentence. It carries no coordinate and no secret,
	// and its WORDS are ours: tenant.copyOfShipped copies a shipped statement whole
	// and changes only its sid and its resource list, so a customer document repeats
	// prose this binary ships. Pinned by
	// TestAuthoredRule_TheProseIsOursAndOnlyTheScopeIsTheirs and
	// TestNoteProvenance_OnlyTwoProductionCallSitesWriteAPolicyDocument, and escaped
	// on output by TestPolicyNote_IsEscapedOnBothSurfacesThatPrintIt.
	//
	// ⚠️ A ROUND OF M8-04 SAID THE OPPOSITE HERE — "straight from internal/policy …
	// fixed strings written by us" was struck out as false, and a claim about
	// "author-written reasons" put in its place. The audit after it measured the
	// write paths and reversed that; the strike-out is recorded because the mistake
	// is invisible from the corrected text, and because M9-07's raw-JSON editor
	// (deferred by Q22) is what would make the struck-out claim right after all.
	Note string
	// NoteIsTenants marks a note whose DECISION came from the organisation's own
	// policy document rather than from ours, so the card can say whose rule it is
	// reporting.
	//
	// 🔴 IT IS ABOUT THE RULE, NOT THE WORDS, AND THE DIFFERENCE IS THE WHOLE LABEL.
	// The sentence in Note is ours either way (see Note). What a customer controls is
	// which shipped statement applies and at which venues, and the record has always
	// said so — matched_sid reads 'tenant:…', policy_layer reads 'tenant'. The label
	// carries that one column onto the screen, so a manager reading a note knows
	// which document to open to find the rule behind it.
	//
	// ⚠️ WHAT IT DOES NOT DO, said rather than left to be discovered. It does not make
	// the sentence true, or false. And it does not mark the one note class this
	// product has actually measured saying something untrue: that was a BASELINE
	// sentence — "network proof of place: the source IP matches the location" on a
	// venue whose stored range was too wide to tell it apart from anywhere else
	// (backlog T40) — which reads policy_layer='baseline' and so is never labelled
	// here. That class is closed on both sides now (netx.TooWideForProofOfPlace at
	// save time, tap.ipMatches at read time), but §4.3 means the rows already written
	// keep the sentence. Watching for it is the evidence signs' job (IPSign/GPSSign)
	// and the ANOMALY report's, not this label's.
	NoteIsTenants bool
}

// FilterBarView is the six filters M6-03's card names: what can be picked, and
// what is picked.
//
// THE SELECTED VALUES ARE ECHOED BACK, which the sign-in form deliberately does
// NOT do — and the difference is worth stating because the two rules look
// inconsistent. AdminLoginView refuses to echo an email because that form is the
// one surface an unauthenticated stranger can post to. This form is behind the
// session gate, its values are ids and closed-vocabulary words the handler has
// already validated against the tenant's own data, and a filter bar that forgot
// its own state after every submission would be unusable.
type FilterBarView struct {
	// Action is where the form submits — the section's own URL, so filtering is a
	// plain GET and every filtered view is a bookmarkable address.
	Action string
	// Date is the ISO day in the box ("2026-08-05").
	Date string

	Locations   []OptionView
	Departments []OptionView

	// The five selected values. Empty means "do not narrow".
	LocationID   string
	DepartmentID string
	// EmployeeName is TYPED, not picked, and it is the one filter that is not a
	// list (user decision, 2026-08-06).
	//
	// 🔴 THE ROSTER IS THE ONLY FILTER THAT GROWS WITHOUT BOUND, and it was
	// measured doing so: rendering every employee as an <option> made the page
	// 867 233 bytes, of which that one <select> was 835 319 -- 96% -- with
	// Cache-Control: no-store, so it was re-sent on every view and every filter
	// change. Venues and departments are bounded by how many places a business
	// has, so they stay pickable.
	//
	// ⚠️ THE COST IS PAID BY THE MANAGER AND IS WRITTEN DOWN RATHER THAN HIDDEN:
	// they must KNOW the name and TYPE it. "Who works here?" stops being
	// answerable from this control, and becomes the Employees section's job
	// (M6-05). Two alternatives were measured and rejected first -- shortlisting
	// to active staff saved 9%, to recent tappers 0.3%, and any hard cut makes
	// somebody who has left unfindable, which §4.6 refuses.
	EmployeeName string
	Verdict      string
	Channel      string

	// Verdicts and Channels are the closed vocabularies, supplied by the handler
	// from THE SAME LISTS IT VALIDATES AGAINST — so the dropdown cannot offer a
	// value the validator would reject, and the validator cannot accept one the
	// dropdown never showed.
	Verdicts []OptionView
	Channels []OptionView

	// Narrowed is true when anything beyond the day is set. The empty state uses
	// it to say "nothing matches these filters" rather than "nothing happened
	// that day" — different claims, and only one of them is true at a time.
	Narrowed bool
}

// OptionView is one <option>: the value that goes in the URL, the label a human
// reads, and an optional group heading.
type OptionView struct {
	Value string
	Label string
	// Group is the location a department sits in. Rendered as a suffix so
	// "Kitchen" from two different venues is not two identical rows.
	//
	// It used to read "or an employee's lifecycle status". Employees stopped being
	// a list on 2026-08-06 (see FilterBarView.EmployeeName), so the department
	// dropdown is the only caller left.
	Group string
}
