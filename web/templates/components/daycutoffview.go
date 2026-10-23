package components

// DayCutoffView is the business-day block (M15-07, ADR 0032 §A.7): where a level's business
// day starts, where that value comes from, whether the rule is applying yet, what the
// current value cuts, and -- for an admin who may change it -- the small form whose first
// press only REVIEWS the change (the write is the review page's confirmed second press).
//
// EVERY STRING IS BUILT BY THE HANDLER from the database's answer and fixed prose; nothing a
// client posted is echoed here except the time it typed, through templ's escaping.
type DayCutoffView struct {
	// Anchor is the block's id, the target the return address scrolls to.
	Anchor string
	// ValueLine is the stored or effective value and its source ("05:00 — from Sliema").
	ValueLine string
	// Status is the switch's line (ADR 0032 §A.8): "Not active yet — …" or "Active since …".
	Status string
	// Outcome is the notice for the last press on this block, or nil.
	Outcome *DayCutoffOutcome
	// Warnings are the sentences for the shifts the CURRENT value cuts, and Suggestion the
	// value outside every one of them (or the sentence that there is none). Both are empty
	// when nothing is cut.
	Warnings   []string
	Suggestion string

	// CanChange draws the form; WhyNot is the sentence a reader who may not change it reads.
	CanChange bool
	WhyNot    string
	Action    string
	Hidden    []FormField
	// InputID is the time input's id (unique per page); Input its value -- the level's own
	// stored value, "" when it inherits.
	InputID string
	Input   string
	// Hint is the help under the field: what an empty field means at this level.
	Hint string
}

// DayCutoffOutcome is one notice: its tone, heading and sentence.
type DayCutoffOutcome struct {
	Tone    Tone
	Heading string
	Text    string
}
