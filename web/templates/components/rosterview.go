package components

// The ROSTER ROW and its filter bar (M6-05 phase A).
//
// SAME PLACEMENT RULE AS docketview.go, and for the same mechanical reason: pages
// imports components, so a component taking a pages type would close an import
// cycle. A component owns the shape of its own input.
//
// 🔴 §4.7 — THE ABSENCE OF FOUR FIELDS IS THE SPEC. There is no InviteCode, no
// SessionToken, no DeviceLabel and no Email on RosterRowView. The first is phase
// B's and is shown exactly once, at the moment it is created, and never read back
// from storage (it is stored hashed); the second never leaves the database at all
// (db/queries/sessions.sql: token_hash exists to be matched); the third is not
// selected by the query; the fourth has no reader on a roster. Three independent
// walls again — the query does not select them, the domain type has no field for
// them, and this view could not carry one if it tried.
//
// ⚠️ THE PARAGRAPH THAT WAS HERE SAID "THERE IS NO ACTION FIELD" AND PHASE B MADE IT
// FALSE, so it is replaced rather than left standing. What it was protecting is the
// rule, not the emptiness: a view model must not carry a control the server does not
// implement. The four actions arrived WITH their handlers, their authorisation and
// their audit rows, in one edit — RosterActionsView below is that edit, and TWO tests
// hold the rule between them: TestEmployeesSection_EveryControlLeadsSomewhereThatExists
// (every form target and every link resolves through the real router) and
// TestEmployeesSection_OffersNoActionTheServerWouldRefuse (the conditional fields
// below are not offered when the database would refuse them). The second was missing
// for one round and an audit measured what that cost: CanInvite and CanDeactivate
// could both be hard-wired to true with the whole package still green.
//
// 🔴 WHAT IS STILL ABSENT IS THE SECRET. There is no InviteCode field on any type in
// this file. The activation link reaches exactly one template — pages.InviteIssued,
// rendered by the POST that mints it — and nothing on the roster or its action card
// has a field it could travel in.

// RosterRowView is one person as a kitchen docket.
//
// EVERY DATE IS A PRE-FORMATTED STRING, as on DocketView: CLAUDE.md §6 keeps
// instants in UTC below the render layer and converts once, in the handler, against
// the zone the database gave for this tenant.
type RosterRowView struct {
	// Name is the person's full name. It is never empty — employees.full_name is
	// NOT NULL (migration 00003) — so unlike DocketView.Who there is no "nobody
	// recognised" branch to write.
	Name string
	// Venue is the person's home location, "" when they have none (NoHome) or the
	// join missed. The template says which in words instead of leaving a blank cell
	// that reads as "none".
	Venue string
	// NoHome is true for somebody the integration manages who has no home venue (K-14,
	// migration 00037) -- a state, not a defect.
	NoHome bool
	// Department is "" for a business that does not model departments — a bar, in
	// the two markets this ships to. That is a first-class state, not a gap.
	Department string
	// Status is the RAW lifecycle value: invited | active | deactivated. It picks
	// the pill, and the pill carries the WORD as well as the tone, so status is
	// never told by colour alone (skill tappa-brand).
	Status string
	// Initials are the two letters of the row's square (Initials), drawing only: the
	// template hides them from a screen reader, which reads the name.
	Initials string
	// Role is the job title a manager typed, "" when none was -- the row then prints
	// the venue without a separator in front of it (panel redesign, M12-18).
	Role string
	// LastTap is when this person's newest record happened, in the tenant's zone
	// ("14:31 today", "02:14 yesterday", "Mon 09:02", "2 Oct 18:00"); "—" when the
	// page's last-tap read was measured and they have none; "could not be read" when
	// that read failed (§4.6: unknown is never "none").
	LastTap string
	// LastTapNote is the one word beside it that changes what the time means:
	// "refused" for a refused tap (§5 row 4 records those too), "by hand" for a
	// record a manager typed, "" otherwise.
	LastTapNote string
	// Devices is what this row says about signed-in browsers: the count, as the
	// card's longer sentence starts ("1 signed in", "None signed in"). The card, which
	// has the room, says when they were last used (deviceLine).
	//
	// 🔴 A DEACTIVATED PERSON'S COUNT IS THEIR REAL ONE: deactivating does not sign a
	// browser out (user decision, 2026-08-07), so the row never prints "none" for them
	// by rule.
	Devices string
	// Selected is true on the row whose card is open (?manage=). The row says so with
	// aria-current, and the frame follows the attribute.
	Selected bool

	// ManageHref opens this person's action card (M6-05 phase B). It is the SAME
	// section URL with ?manage=<id> added and the reader's filters and page kept, so
	// closing the card returns them to the row they left.
	//
	// 🔴 IT IS A LINK TO A GET AND NOT A CONTROL THAT CHANGES ANYTHING. Every write
	// on this screen is a POST from the card, so a row cannot act on somebody by
	// itself and a crawler following every link on the page changes nothing.
	ManageHref string
}

// FormField is one hidden input: the roster position an action form carries back so
// the manager returns to the page they acted from.
//
// 🔴 THE VALUES ARE RE-VALIDATED ON ARRIVAL, NOT TRUSTED. The handler builds these
// from the filter it has already parsed, and rosterFilterFrom parses them again on
// the way in — the same function the GET uses, so a POST cannot accept a value a GET
// would reject. Nothing here is a URL: there is no field an open redirect could live
// in, because the target is rebuilt server-side from the parsed filter.
type FormField struct {
	Name  string
	Value string
}

// RosterActionsView is the action card for ONE person (M6-05 phase B): the four
// things a manager can do, and the facts they need to do them.
//
// 🔴 IT IS ONE CARD ON THE PAGE RATHER THAN CONTROLS ON EVERY ROW, and the reason is
// the cost M6-03 measured. The move form needs the venue and department option
// lists; rendering them once per row would multiply exactly the kind of control that
// was 96% of a page in the transactions section, on a page that is
// Cache-Control: no-store.
//
// 🔴 §4.7 — THERE IS NO FIELD FOR AN INVITATION CODE HERE, AND THERE CANNOT BE ONE.
// The activation link exists in exactly one response body — the answer to the POST
// that minted it, which renders InviteIssuedView — and this card is rendered by a
// GET. A field for it here would be a field a GET could fill.
type RosterActionsView struct {
	// Name, Status, Venue and Department are read from the database in the SAME
	// request that renders this card, so what the manager acts on is what they see.
	Name       string
	Status     string
	Venue      string
	Department string
	// NoHome is RosterRowView.NoHome for the person the card is about.
	NoHome bool
	// Initials is the card's square, drawing only (Initials).
	Initials string

	// THE SUMMARY (M12-20). Every line below is "" when the summary was not read or
	// its read failed, and SummaryFailed says which: a card that cannot say when
	// somebody signed in says so, rather than reading as "never" (§4.6).
	//
	// Role is the job title, "" when none was typed.
	Role string
	// SinceLine is the lifecycle date that matches Status ("Active since 3 Sep 2026",
	// "Deactivated on 2 Oct 2026"), "" when that stamp was never written. 🔴 An
	// INVITED person added in the panel has no such stamp (no statement writes
	// invited_at), so their card prints none -- it never borrows another date.
	// It is the card's only "Deactivated on" date (M12-48: the deactivate block
	// printed it a second time).
	SinceLine string
	// SignedInLine is the card's sentence about live browser sessions: the count,
	// since when the oldest one is signed in, and when one was last used. It is the
	// real count for a deactivated person too: their sessions stay alive.
	SignedInLine string
	// LiveDevices is the count behind that sentence, so the card can add what it
	// means for a deactivated person only when there is something signed in.
	LiveDevices int
	// LinkLine says whether an activation link is outstanding ("A link was created
	// 3 days ago and works until 27 Oct", "No working link yet"), "" when it was not
	// read. It says "created", never "sent": the product does not know delivery.
	LinkLine string
	// SummaryFailed is true when the summary was asked and did not answer.
	SummaryFailed bool

	// Recent is the person's last records (M12-19): day, direction, time and the
	// engine's word, and nothing else -- so a record at a partner venue reads like any
	// other (K-39). RecentState is "measured", "failed" or "" (not read).
	Recent      []MiniTapView
	RecentState string

	// Hidden is the roster position, posted back by every form on the card.
	Hidden []FormField
	// CloseHref is this roster without the card — where "Close" goes.
	CloseHref string

	// CanInvite is false for somebody deactivated: db/queries/invites.sql refuses to
	// spend an invitation for them, so the button would mint a dead credential. The
	// template says WHY in that case rather than simply omitting the control — a
	// missing button teaches nothing and reads as a bug.
	CanInvite    bool
	InviteLabel  string
	InviteAction string
	// InviteByEmail is true in the e-mail mode (TAPPA_INVITE_DELIVERY=email, M10
	// EM-7B): the invite button mails the link to the address on file (Email) and the
	// screen never shows it. With no address on file the button is NOT offered — the
	// server would refuse it — and the card says why. False is the panel mode, whose
	// card is the one it always was.
	InviteByEmail bool
	// CanShowLink is the e-mail mode's owner-only fallback (ADR 0022 §7, K3): true only
	// for an OWNER, an invitable person and NO address on file — the conditions the
	// server checks again when the form arrives. ShowLinkField and ShowLinkValue are
	// the one extra field that form posts to InviteAction; the server owns both.
	CanShowLink   bool
	ShowLinkField string
	ShowLinkValue string

	// RecordHref opens the manual record entry screen for this person (M6-08).
	//
	// 🔴 IT IS A LINK TO A GET AND THERE IS NO CanRecord BESIDE IT, WHICH IS THE ONE
	// UNCONDITIONAL ACTION ON THIS CARD. Every other control here is gated on a state
	// the database would refuse; this one is not gated because the server refuses
	// nobody — a deactivated person's last shift still has to be payable, and
	// deactivation is one-way (docs/adr/0010) so there is no route back if it is not
	// typed. A boolean that was always true would be the shape
	// TestEmployeesSection_OffersNoActionTheServerWouldRefuse exists to catch.
	RecordHref string

	// CanDeactivate is false for somebody already deactivated — pressing it again
	// writes nothing, and offering it would suggest otherwise.
	CanDeactivate bool
	// Confirming is true on the second step of the deactivation: the card has said
	// what it cannot undo and is now showing the button. The first step is a plain
	// LINK (ConfirmHref) because this section loads no script and a confirmation a
	// server cannot require is not a confirmation.
	Confirming  bool
	ConfirmHref string
	// ConfirmToken is the one-shot value the confirmation screen minted, and
	// ConfirmField is the input name the server reads it back from. They are FIELDS
	// rather than constants in the template because the server owns both halves: the
	// token is bound to this person in an HttpOnly cookie, and a form that carried
	// the wrong name would fail closed rather than silently skip the gate.
	//
	// 🔴 THE TOKEN IS NOT A SECRET IN THE §4.7 SENSE — it is rendered into the page on
	// purpose, exactly like the login form's synchronizer token, and the cookie half
	// is what makes it worth anything.
	ConfirmToken     string
	ConfirmField     string
	DeactivateAction string

	// The move form. LocationID and DepartmentID are the person's CURRENT placement,
	// pre-selected, so the form opens saying where they are rather than proposing a
	// change nobody asked for.
	MoveAction   string
	Locations    []OptionView
	Departments  []OptionView
	LocationID   string
	DepartmentID string

	// Email is the address on file, "" when there is none (M10 EM-6). It is read in
	// the SAME request from its own query (GetEmployeeEmail) and rendered as data —
	// it is personal data, not a credential, and the handler never logs it.
	//
	// 🔴 IT IS THE ONLY ADDRESS ON THIS SCREEN. RosterRowView still has no address:
	// the list shows fifty people and none of their addresses; the card shows the one
	// person a manager opened.
	Email string
	// CanChangeEmail is true for an OWNER (the POST's own gate,
	// mayChangeEmployeeEmail); a manager sees the address and the sentence that says
	// who can change it, never a form the server would refuse.
	CanChangeEmail bool
	EmailAction    string

	// ExternalRef is the integration identity on file, "" when unbound (M11 API-1,
	// K-18). It is the customer's own HR/ERP key for this person -- data, not a
	// credential. CanChangeExternalRef is the POST's own gate (an OWNER), so a manager
	// sees the ref and the sentence that says who can change it, never a refused form.
	ExternalRef          string
	CanChangeExternalRef bool
	ExternalRefAction    string
}

// RosterFilterView is the employees section's filter bar: what can be picked, and
// what is picked.
//
// IT IS A SEPARATE TYPE FROM FilterBarView RATHER THAN A WIDENING OF IT. The two
// bars filter different things — a day of records versus a list of people — and the
// transactions bar's six controls include three (day, verdict, channel) that mean
// nothing here. One struct serving both would carry fields that are always empty on
// one of the two screens, and "always empty" is how a control eventually gets
// rendered on the screen it does not belong to. What the two DO share is the CSS,
// which is the part that would actually drift.
type RosterFilterView struct {
	// Action is where the form submits — the section's own URL, so filtering is a
	// plain GET and every filtered view is a bookmarkable, shareable address.
	Action string

	Locations   []OptionView
	Departments []OptionView
	// Statuses is the closed lifecycle vocabulary, supplied by the handler from THE
	// SAME LIST IT VALIDATES AGAINST — so the dropdown cannot offer a value the
	// validator would reject, and the validator cannot accept one the dropdown never
	// showed.
	Statuses []OptionView

	// The four selected values. Empty means "do not narrow".
	//
	// 🔴 AN EMPTY Status MEANS EVERYONE, INCLUDING PEOPLE WHO HAVE LEFT, and that is
	// §4.6 rather than a default somebody happened to pick. M6-03 measured and
	// rejected three ways of shortlisting a roster for exactly this reason and handed
	// the question to this section; a status filter that defaulted to "active" would
	// reopen it here.
	Name         string
	Status       string
	LocationID   string
	DepartmentID string

	// Narrowed is true when anything is set. The empty state uses it to say "nobody
	// matches these filters" rather than "nobody works here" — different claims, and
	// only one of them can be true at a time.
	Narrowed bool

	// Chips are the status links: "Everyone" first, then one per word of Statuses, in
	// that slice's order (panel redesign: the status select became links). Each keeps
	// the name, venue and department filters and drops the cursor.
	Chips []RosterChipView
}

// RosterChipView is one status chip: a link, not a control that acts.
type RosterChipView struct {
	Label string
	Href  string
	// Current marks the chip of the status the list is filtered on ("Everyone" when
	// none); the template says it with aria-current, the frame follows.
	Current bool
	// Count is how many people the list would hold with this chip picked, honouring
	// the other filters. Counted is false when the count was not measured, and then
	// the chip prints no number at all -- never 0 (§4.6).
	Count   int
	Counted bool
}

// MiniTapView is one mini docket of the card's strip of recent records.
//
// 🔴 K-39 / §4.7 -- FOUR WORDS AND NOTHING ELSE: the day, the direction, the time and
// the engine's verdict. No venue, no plaque, no note, no trust, no evidence: the read
// under it (ledger.RecentTap) has no field for them, so a record at a holding
// partner's venue is drawn exactly like one at home.
type MiniTapView struct {
	// Day is "Tue 20" in the tenant's zone.
	Day string
	// Direction is "IN" or "OUT", "—" for a record with none (a refusal, an ignored
	// repeat): never a guess.
	Direction string
	// Time is "14:26" in the tenant's zone.
	Time string
	// Verdict is the engine's raw word (ok, flag, reject, ignored); it picks the frame,
	// and Word is what the template prints.
	Verdict string
	Word    string
}
