package components

// The WALL PLAQUE view models (M6-06 phase B).
//
// 🔴 SECTION 4.7 IS ENFORCED HERE BY ABSENCE, AND THE ABSENCE IS COUNTED RATHER
// THAN ASSERTED. Every field below is a string or a bool the handler already
// formatted; there is no []byte anywhere in this file, so the KEK-wrapped per-tag
// key (tags.aes_key_ref) has no field of a shape that could hold one -- and the
// claim is exactly as wide as the test under it, no wider.
// TestPlaqueViewModels_CannotCarryAKey (internal/handler) enumerates the FIELD NAMES
// of the types below exactly -- the list lives in the test and is not counted here,
// because a count in a comment beside a list that owns it goes stale, and this one
// did -- and walks their SHAPES to any depth against an
// allow-list of kinds — so an array, a map, an interface or a []byte fails on shape
// alone, and any new field fails until somebody adds it there.
//
// ⚠️ THE PREVIOUS WALL WAS A DENYLIST OF NAMES AND AN AUDIT BEAT IT IN THREE SHAPES:
// `Material [44]byte` (the envelope's exact length, an ARRAY rather than a slice),
// `Ref string` (a neutral name), `Extras map[string][]byte` (the byte slice one
// indirection away). "A forbidden-string scan protects against what you thought of;
// an allow-list protects against what you did not" is this repository's own lesson,
// already written for audit_log.detail.
//
// ⚠️ IT IS TWO TESTS IN TWO PACKAGES, NOT ONE, AND THE REASON IS A DEPENDENCY. An
// internal/domain package cannot import the render layer, so the domain's own walk
// (over tenant.Plaque and its neighbours) cannot reach this file. An earlier version
// of this comment named the domain test alone and said it covered "the view models",
// which it did not — the overclaim class this repository keeps paying for.
//
// ⚠️ AND NEITHER HALF REFUSES A KEY *ENCODED* INTO A PERMITTED FIELD — hex in a
// string, base64 in a name. No type system does. What makes that unreachable is that
// nothing on this path READS the column: no shipped tag query selects aes_key_ref,
// asserted against db/queries/tags.sql itself.
//
// 🔴 KeyState's NAME CONTAINS "key" AND THE FIELD CONTAINS NONE: it holds one of two
// fixed sentences, produced by handler.keyStateOf from whether the plaque has a wall,
// and that closed value set is pinned by the same test.
//
// 🔴 WHAT KeyState IS ACTUALLY BACKED BY, because "encoded" is a claim about
// cryptography and this screen never sees any. It is derived from ONE fact:
// whether the plaque has a wall. What licenses the word "encoded" is the SCHEMA --
// tags.aes_key_ref is `bytea NOT NULL` and only Tappa's loader writes rows, so a
// row that exists is a plaque Tappa encoded and loaded. What NOTHING checks is
// that the stored value is a well-formed 44-byte KEK envelope (backlog T7): a
// corrupt one reads "Encoded" here and fails at TAP time inside sun.Unwrap. The
// sentence on the card says what it means rather than implying a verification
// nobody performed.
//
// 🔴 THERE IS NO RUBBER STAMP ON THESE CARDS, for VenueRow's reason: a stamp is a
// VERDICT -- the engine's judgement of a tap -- and a plaque's lifecycle is not a
// judgement of anything. The status is a word in a labelled field.

// PlaqueRowView is one plaque as the docket renders it.
type PlaqueRowView struct {
	// UID is the chip id printed on the plaque. It is PUBLIC: it is on the wall and
	// in the tap URL's address bar.
	UID string

	// Status is the lifecycle in the manager's words, not the column's.
	Status string

	// Venue is the wall it is mounted at, or "" when there is none to name.
	Venue string

	// VenueUnknown says this plaque IS mounted but the venue's name could not be
	// resolved, which is a DIFFERENT fact from "not mounted" and must not be
	// rendered as one.
	//
	// 🔴 TWO STATES BEHIND ONE EMPTY STRING WAS THE DEFECT. Venue == "" happened
	// both for a plaque in stock (ordinary, the whole inventory model) and for one
	// mounted at a venue past venuePageLimit, whose name the section never read — so
	// a plaque ON A WALL rendered "Not mounted yet" inside the "Plaques on the wall"
	// list. Unreachable today (the largest tenant has 9 venues against a ceiling of
	// 200) and false anyway, which is the same class VenueRow's "Venue could not be
	// read" already answers for departments.
	VenueUnknown bool

	// Counter is tags.last_ctr as text -- how many times the chip has been read. It
	// is a NUMBER and therefore mono (skill tappa-brand).
	Counter string

	// LastSeen is when this plaque was last tapped, or "" for NEVER.
	//
	// 🔴 "" MEANS NEVER AND THE TEMPLATE SAYS SO IN WORDS. A blank cell reads as
	// "we did not look"; a zero date would read as 1 January year one. The absence
	// travels as an absence the whole way from ListTagLastSeen, which returns no row
	// at all for an untapped plaque.
	LastSeen string

	// KeyState is the "encoded/pending" state the M6-06 card asks for. See the file
	// header for exactly what backs it. It never contains, encodes or hints at a key.
	KeyState string

	// Replaces / ReplacedBy are the two ends of the replacement chain, as uids.
	// Empty when this plaque is not part of one.
	Replaces   string
	ReplacedBy string

	// RetiredAt is when it left the wall, or "".
	RetiredAt string

	// Loaded is when Tappa put this plaque into the customer's inventory.
	Loaded string

	// Frozen says the database will refuse ANY write to this row.
	//
	// 🔴 IT IS A DEVELOPMENT REALITY, NOT A HYPOTHETICAL, AND THE CARD SAYS SO
	// RATHER THAN OFFERING A CONTROL THAT MUST FAIL. Migration 00013 shipped
	// tags_uid_canonical_hex as NOT VALID because 18 010 rows carry a lower-case uid
	// that cannot be normalised (their `transactions` are append-only). A CHECK is
	// evaluated on every UPDATE, so those rows answer SQLSTATE 23514 to anything.
	// They are listed like any other plaque, so the alternative to this flag is a
	// 500 on a row a manager can see. Production has none.
	Frozen bool

	// OpenHref is the card for this plaque, or "" when there is nothing to open.
	OpenHref string
}

// PlaqueFormView is the card for ONE plaque: what it is, what happened to it, and
// the one act available on it.
//
// 🔴 ONE CARD, ONE ACT AT A TIME. A stock plaque can be MOUNTED; a plaque on a wall
// can be REPLACED or taken off it; a retired or lost one can be neither, and the card
// says why instead of rendering a disabled control. The states are the Mode values
// enumerated just below, which is the only place they are listed. That is
// the property TestLocationsSection_OffersNoActionTheServerWouldRefuse already
// holds for the department form, applied to plaques.
type PlaqueFormView struct {
	Heading string
	// Action is where the form posts -- supplied by the handler from the SAME
	// constant the router registers, so the card cannot submit to a URL nothing
	// mounts.
	Action string
	// CloseHref is this section without the card.
	CloseHref string

	// Plaque is the row itself, rendered as the docket's detail block.
	Plaque PlaqueRowView

	// Mode is which act this card offers: "mount", "onwall", "unmount" or "" for none.
	//
	//	mount    a plaque in stock, with somewhere to put it
	//	onwall   a plaque in service: the replacement form, PLUS a link to take it
	//	         down. Both are offered because "the plaque is on the wrong door" is
	//	         a real state and replacing it would spend a spare to fix a mistake.
	//	unmount  the second step of that link: the warning and the button
	//	""       nothing can be done, and Blocked says why
	//
	// 🔴 EXACTLY ONE CONFIRMATION IS MINTED PER RENDER, which is why "onwall" and
	// "unmount" are different modes rather than one card with two armed forms. The
	// confirmation cookie is ONE per browser, so a card carrying two tokens would
	// leave whichever form the manager did not submit unusable — a control that can
	// only fail, which is the defect this whole card is built to avoid.
	Mode string

	// Venues is the wall list a STOCK plaque may be mounted on. Empty means this
	// business has no venues yet, and the card says that rather than offering an
	// empty dropdown.
	Venues []OptionView

	// Successors is the STOCK a plaque on a wall may be replaced with. Empty means
	// there is no spare plaque, and the card says how to get one.
	Successors []OptionView

	// Blocked is the sentence explaining why no act is offered, or "".
	//
	// 🔴 IT IS A SERVER-CHOSEN SENTENCE FROM A FIXED SET, never anything a client
	// sent. The handler picks it; nothing here is reflected.
	Blocked string

	// ErrorMessage is the refusal shown above the control when a submission was
	// rejected. Section 4.6: a refused act says why.
	ErrorMessage string

	Submit string

	// ConfirmField and ConfirmToken carry the server-minted confirmation the
	// REPLACEMENT must echo. They are empty for every other mode.
	//
	// 🔴 THE CARD IS THE WARNING HERE, not a second screen. Phase A's removals arm a
	// one-click button behind ?confirm=remove because there is nothing else on that
	// card to decide; a replacement already needs a choice, and internal/handler's
	// plaqueCard carries the argument for minting on the card's own GET instead.
	ConfirmField string
	ConfirmToken string

	// UnmountHref opens the "take it off the wall" warning, or "" when that is not
	// on offer. It is a LINK rather than a second armed form for the reason Mode
	// gives: one confirmation per render.
	UnmountHref string

	// Trail is WHO did WHAT to this plaque, newest first — the audit half of the
	// card's history, which the plaque row itself cannot carry because `tags` has no
	// actor column.
	Trail []PlaqueEventView
}

// PlaqueEventView is one line of that trail, already formatted.
type PlaqueEventView struct {
	// What is the act in the manager's words ("Mounted", "Retired"), never the
	// trail's own vocabulary.
	What string
	When string
	// Who is the person, or "" when there is no name to show. BySystem says the trail
	// row carried NO ACTOR AT ALL, which is a different fact: an admin may be stored
	// with an empty full_name (`text NOT NULL`, no non-blank CHECK), and calling that
	// "the system" would attribute their act to the product.
	Who      string
	BySystem bool
}

// PlaqueReceiptView is a plaque act the TRAIL confirmed (the C' reading M6-06
// phase A established).
//
// 🔴 IT IS A POINTER ON THE VIEW AND nil MEANS "SAY NOTHING". A `?done=` word
// alone proves nothing -- phase A measured one rendering "The venue has been
// removed" in a browser that had never posted anything -- so the section builds
// this only after finding the SIGNED-IN ADMIN'S OWN audit entry for the plaque in
// the address, inside the window. A stranger's URL, a stale bookmark, another
// business's uid and a colleague's act all produce nil.
//
// ⚠️ AND EACH WORD IS VERIFIED AGAINST A DIFFERENT ACTION. A replace writes
// BOTH plaque.retired and plaque.mounted, while a plain mount writes only
// plaque.mounted -- so "replaced" is checked against plaque.retired for the OLD
// uid. Checking both against plaque.mounted would let a mere mount print
// "replaced", which is the M6-04 defect class this mechanism exists to close.
type PlaqueReceiptView struct {
	// Replaced says this acknowledges a replacement rather than a first mount;
	// Unmounted says the plaque came OFF a wall. At most one is ever true — each is
	// verified against the audit action ITS OWN act writes, so a mount cannot back
	// either of the other two words.
	Replaced  bool
	Unmounted bool
	// Name is what the trail recorded beside it: the VENUE for a mount, the
	// SUCCESSOR uid for a retirement. "" when the entry carries none, in which case
	// the heading says what happened without naming it.
	Name string
}
