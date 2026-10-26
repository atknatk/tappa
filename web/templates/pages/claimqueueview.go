package pages

// claimqueueview.go -- the view of the Spike's second section, "Time claims" (M15-18; ADR 0032 §C.4):
// the punch claims nobody has decided yet, one in focus, its decision form. Every field is a word
// the handler already formatted (a time in the business's zone, a duration, a sentence); the template
// draws them and decides nothing.

// SpikeQueuesView is the pair of links at the top of the Spike: the flagged taps and the time claims.
// ClaimsHref is "" on a panel built without the claim service, and the pair is then not drawn (the
// Spike is the page it was before M15-18).
type SpikeQueuesView struct {
	ReviewHref string
	ClaimsHref string
	// OnClaims marks the claims link current; otherwise the flagged taps link is.
	OnClaims bool
}

// ClaimQueueView is the claims section.
type ClaimQueueView struct {
	PanelChrome
	Queues SpikeQueuesView
	// Action is where the decision form posts (the handler's href, never a literal here).
	Action string
	// Queried: the database answered. A zero view never says "nothing is waiting".
	Queried bool
	Items   []ClaimQueueItem
	// More: more claims are waiting than the list holds (ListCap).
	More    bool
	ListCap int
	// ZoneLabel is the clock every time on the page is in.
	ZoneLabel string
	// Pager is the phone's pager over the listed claims.
	Pager ReviewPager
	// Focus is the claim being decided; nil when the list is empty.
	Focus *ClaimFocusView
	// Done is "approved" or "rejected" -- only after the handler read the decision back.
	Done string
	// Clipped: the note just sent was longer than the limit and was cut.
	Clipped bool
	// Problem is one closed word (claimProblemHeading); "" draws nothing.
	Problem string
}

// ClaimQueueItem is one link of the list.
type ClaimQueueItem struct {
	ID        string
	Who       string
	What      string // "Clock-out missing" / "Clock-in missing"
	When      string // the claimed moment, with its day
	FocusHref string
	Current   bool
}

// ClaimFocusView is the one claim in focus: what a manager is shown before deciding (ADR 0032 §C.4,
// K3, G-Y1) and the form.
type ClaimFocusView struct {
	ID      string
	Who     string
	Heading string
	// The anchor: its direction word, its moment, its channel, and whether the PERSON chose the
	// direction (tap.RecordedDirectionSource said employee).
	AnchorDir     string
	AnchorAt      string
	AnchorChannel string
	AnchorChose   bool
	// Before and After are the neighbouring records, already in words ("none", "none within 18 h").
	Before string
	After  string
	// The claim.
	ClaimedDir string
	ClaimedAt  string
	Sent       string
	// Offered is what the form offered the person ("17:00, the shift's end", or a sentence saying it
	// offered none); DefaultUnchanged is drawn only while a prefill existed.
	Offered          string
	DefaultUnchanged bool
	// The approval's assessment: the interval and its duration, the shift, both ends' deviation.
	Interval        string
	Shift           string
	ClaimedDeviates string
	AnchorDeviates  string
	// NeedsStep: Approve does not write on the first press (out of the band, or no shift).
	NeedsStep bool
	// StateLine says why Approve is not offered or what an Edit must do; "" for a pending claim.
	StateLine string
	// CanApprove draws Approve and the Edit field; Reject is always drawn.
	CanApprove bool
	// EditOpen opens the Edit field (the claimed moment no longer fits the window).
	EditOpen bool
	// Note is the manager's sentence carried back into the form on a re-render.
	Note string
	// Confirm is the confirmation step: non-nil when Approve was pressed outside the band.
	Confirm *ClaimConfirmView
}

// ClaimConfirmView is the confirmation step (ADR 0032 §C.4): the interval and the deviations
// repeated, and the second press. Shown and Token travel back as hidden fields: Shown is the moment
// the step was drawn for, Token the server's signature over it (the claim, the moment and the shift
// it was judged against, this panel session) -- a second press for any other moment is refused.
type ClaimConfirmView struct {
	Shown           string
	Token           string
	Edited          bool
	At              string
	Interval        string
	Shift           string
	ClaimedDeviates string
	AnchorDeviates  string
}
