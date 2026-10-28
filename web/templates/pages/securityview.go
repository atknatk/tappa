package pages

import (
	"strconv"
	"strings"

	"github.com/atknatk/tappa/web/templates/layout"
)

// THE SECURITY PAGE (LP-07, 2026-10-22): /security, drawn from the Security /
// T-Security / M-Security boards (docs/design/landing-redesign/boards). It joins the
// public site the way the pricing page did (pricingview.go records the shape): a row of
// PublicPages, a named route in internal/handler's Mount, every sentence a value here
// with the facts it rests on, and every number handed in by internal/handler.
//
// 🔴 THIS IS THE PAGE THAT DESCRIBES THE RED LINES (CLAUDE.md §4.1, §4.2, §4.3, §4.4,
// §4.5, §4.6, §4.7), so it is the page where an overstatement costs the most. Every
// sentence below says what the code does and no more; where the board said more, the
// value carries the edit and the reason. The edits, once, for the reader looking for
// them:
//
//   - "and the rejected tap is still recorded" (the one-time link) is gone: a replay is
//     recorded when the person holds a session, and the page would need the other cases
//     measured to say it of every replay.
//   - "Plaque keys ... are never written to a log" is gone: the keys are stored wrapped
//     (pinned), but "never in a log" is a property of every caller, which no derivation
//     here reads. The red line still holds it (CLAUDE.md §4.7, redline R7).
//   - "A phone gets its session once" became "through activation": a person can move to
//     a new phone, which activates again and switches the old one off.
//   - Two engine sentences say "by default": a far position and a QR check-in without
//     the network are the BASELINE's rules, which a business can change (§5).
//   - The privacy card's heading says "No fingerprints, no face scans", and the board's
//     word for a virtual fence round the venue is gone: scripts/redline-check.sh's R1
//     reads the board's heading word as a violation unless its line also carries the
//     denial, and R2 refuses the fence word outright. The meaning is the board's.
//   - "the database filters every query by it" names row-level security, and "a role
//     that cannot switch that filter off" says what the code holds: in production the
//     application refuses to start under such a role.
//   - "every look is written to an operator audit log" says "every read" and pins the
//     read's own first step.
//   - "Hosted in Germany ... runs in Falkenstein, Germany." is "Hosted in the EU
//     (Germany)" (user decision K2): no town, data centre or provider is published.
//   - The ladder's footnote now also names the baseline rules the cards do not show, so
//     "Your rules" does not read as the whole baseline.
//
// THE SECURITY REVIEW'S EDITS (LP-07, 2026-10-22, second round), each a sentence that
// said more than the product keeps:
//
//   - "nobody can decide on their own record" is gone, and so is the guardrail note's
//     "stop anyone reviewing their own record": see FactReviewIsOnceAndFinal.
//   - "The database refuses to change … for the database owner's own account too" now
//     says the owner's guard is a trigger, which a deliberate change can switch off. A
//     database superuser gets past it without touching the schema (an ordinary trigger
//     does not fire under session_replication_role = replica), and since LP-15's
//     security review the sentence says so.
//   - "When Taptime's own staff look into an account … every read" is about the support
//     console: staff with the database owner's account read outside it (deploy runbook).
//   - "narrow enough to describe one place" is gone: the limit is a /7's worth of
//     addresses, wide enough for a whole provider's block, so the page says what it
//     refuses and asks for the venue's own connection inside it.
//   - The opening says "Four checks behind every tap." (a QR check-in carries no
//     signature, and a position is optional), and the flagged record's lede says "by
//     default": an owner can write a rule that allows without evidence.
//   - The counter: "already at or past it" (the strict comparison refuses an equal one).
//
// THE NUMBERS ARE NOT TYPED HERE. SecurityFigures carries every one (the signature's
// length, the key and token sizes, the address limits, the radius, the window, the
// trust weights and the guardrail count), filled by internal/handler, and SecurityCopy
// builds the sentences from them. internal/handler's tests hold each figure to the code
// it comes from.
//
// THE DRAWINGS ARE SAMPLES. The two records drawn are the landing panel's own samples
// (LandingPanelPassDockets), which internal/handler already runs through the engine;
// the corrected shift, the link's values and the counter's values are invented and
// labelled "sample".

// SecurityPath is the page's URL.
const SecurityPath = "/security"

// SecurityProofsID is the id of the four proofs' section: the landing's "How each proof
// works" links to SecurityPath + "#" + SecurityProofsID.
const SecurityProofsID = "proofs"

// SecurityView is the security page.
type SecurityView struct {
	// Share is the page's link preview (LP-13): its description and the full URL of
	// its share image, built by internal/handler. The zero value writes neither.
	Share layout.Share

	// SignupHref and SignInHref are the chrome's two ways in, as on LandingView.
	SignupHref string
	SignInHref string
	// FreeMonths is the founding offer's count, for the sign-off.
	FreeMonths int
	// Figures are the page's numbers; Copy is every sentence built from them, computed
	// once by internal/handler (SecurityCopy).
	Figures SecurityFigures
	Copy    SecurityText
}

// SecurityFigures is every number the page prints, each read from the product by
// internal/handler (and held to its source by internal/handler's tests).
type SecurityFigures struct {
	// SignatureHexChars is the length of the signature the plaque writes into its link,
	// in hex characters (internal/sun's cmacHexLen).
	SignatureHexChars int
	// PlaqueKeyBits is the size of a plaque's own AES key (internal/sun's tagKeyLen).
	PlaqueKeyBits int
	// TokenBits is the size of a phone's session token (internal/session's tokenBytes).
	TokenBits int
	// IPv4Prefix and IPv6Prefix are the widest a venue's address list may be and still
	// count as proof of place, as the prefix length of a block that size
	// (internal/netx).
	IPv4Prefix int
	IPv6Prefix int
	// GPSRadiusM is the deployment's default radius (internal/config).
	GPSRadiusM int
	// DebounceSeconds is the deployment's default window for the same person
	// (internal/config, policy.DefaultParams).
	DebounceSeconds int
	// Guardrails is how many built-in guardrails the engine runs
	// (policy.Guardrails).
	Guardrails int
	// TrustBase, TrustIP and TrustGPS are the trust score's three parts
	// (internal/domain/tap).
	TrustBase int
	TrustIP   int
	TrustGPS  int
	// WebhookHeader is the header a webhook delivery is signed in
	// (webhook.SignatureHeader).
	WebhookHeader string
}

// The facts only this page claims. Each is derived from the product in
// internal/handler/security_facts_test.go; the closure test reads this block like the
// one in landingview.go.
const (
	// The plaque's link carries a signature of SignatureHexChars hex characters, made
	// with the plaque's own AES key of PlaqueKeyBits. Derived from internal/sun's
	// cmacHexLen and tagKeyLen and sun.Params carrying the plaque id, the counter and
	// the signature.
	FactSignatureLengthAndKey Fact = "sun:cmacHexLen+tagKeyLen"
	// A plaque's key is stored only wrapped: AES-256-GCM under the deployment's key
	// (internal/sun's Wrap), in tags.aes_key_ref, which migration 00021 holds to the
	// envelope's exact length.
	FactPlaqueKeysAreWrapped Fact = "sun:Wrap AES-256-GCM+tags.aes_key_ref envelope"
	// The checks run in the server: the signature is checked by sun.Verifier and the
	// counter advanced by AdvanceTagCounter. Delegates to SourceSignatureIsVerified and
	// SourceCounterAdvanceIsGuarded.
	FactChecksRunOnTheServer Fact = "fact:checks-run-on-the-server"
	// A phone's session comes from activation, which ends on a real NFC tap at an
	// active plaque of the same business, checked by sun.Verify, and writes no
	// attendance record (ADR 0026). Derived from Activation.CompleteByTap.
	FactActivationEndsOnARealTap Fact = "handler:Activation.CompleteByTap"
	// The session token is TokenBits of crypto/rand and the database keeps only an
	// HMAC-SHA256 of it (sessions.token_hash). Derived from internal/session and
	// migration 00003.
	FactSessionTokenIsStoredHashed Fact = "session:tokenBytes+hmac+sessions.token_hash"
	// A tap on a switched-off account raises a security alert and leaves an audit row.
	// Derived from sys:employee-deactivated's Alert and checkin's dec.Security branch.
	FactDeactivatedTapIsASecurityEvent Fact = "policy:sys:employee-deactivated Alert+checkin audit"
	// The address that counts is the request's own source address, matched against the
	// tapped plaque's venue's ranges; no network name is evidence. Derived from
	// tap.Decide's ipMatches call and from the engine carrying no network-name input.
	FactAddressIsTheRequestsOwn Fact = "tap:ipMatches(SourceIP, LocationIPs)"
	// A list wider than IPv4Prefix / IPv6Prefix is refused when a venue is saved and is
	// not counted when a tap is judged: the same netx.TooWideForProofOfPlace on both
	// sides. Derived from internal/netx's limits and its two callers.
	FactWideAddressListIsNoEvidence Fact = "netx:TooWideForProofOfPlace on save and on judge"
	// The radius is a deployment setting, GPSRadiusM by default. Delegates to
	// SourceGPSRadiusDefault.
	FactGPSRadiusIsADefault Fact = "fact:gps-radius-default"
	// A position read outside the radius sends the tap to review under the baseline,
	// even with an address match: base:gps-conflict-review is a review on a
	// conflict computed from the distance alone, and review beats allow. Derived from
	// the baseline and the engine.
	FactFarPositionIsReviewed Fact = "policy:base:gps-conflict-review"
	// The order: guardrails first and terminal, then the baseline and the business's
	// own rules together (most specific, then strictest), then the default. Derived
	// from the engine.
	FactRulesRunInOrder Fact = "policy:guardrails, then baseline+tenant, then default"
	// A tap no rule decides is flagged: tap:record's default is review, and tap.Decide
	// maps review to flag. Derived from the engine.
	FactUndecidedTapIsFlagged Fact = "policy:default review for tap:record"
	// The guardrails are policy.Guardrails' list, Guardrails long, and the page names
	// every one of them. Derived from the list.
	FactGuardrailsAreTheList Fact = "policy:Guardrails()"
	// The baseline is the business's to change: the panel applies a policy change, and
	// the baseline is a layer below the guardrails. Derived from the route and the
	// engine.
	FactBaselineCanBeChanged Fact = "route:POST policyApplyHref+baseline layer"
	// The trust score is TrustBase + TrustIP + TrustGPS, computed from the evidence and
	// not from the verdict, carried on the record and read by no rule. Derived from
	// tap.Decide and the engine's input vocabulary.
	FactTrustScoreDecidesNothing Fact = "tap:trustScore, no policy input"
	// The same person tapping again inside the window is ignored, timed on the
	// database's clock as well as the declared one, and the window is a deployment
	// setting of DebounceSeconds by default. Derived from sys:person-debounce, the
	// SecondsSinceLastRecordedTap query and internal/config.
	FactDebounceIsPerPersonOnTheServerClock Fact = "policy:sys:person-debounce+clock_timestamp"
	// A flagged record's hours are held out of the payroll figure until a manager
	// decides, and The Roll prints them as waiting (or refused). Derived from the
	// ledger and The Roll's template.
	FactFlaggedHoursWaitOutsideTheTotal Fact = "ledger:HoursAwaiting outside Worked"
	// A flagged record is decided once and the decision cannot be changed afterwards:
	// it is a row of its own in transaction_reviews, UNIQUE (transaction_id), with
	// UPDATE and DELETE revoked from the application and a trigger refusing both.
	// Derived from migration 00005 and every migration after it.
	//
	// 🔴 IT SAYS NOTHING ABOUT WHO DECIDES (LP-07 security review, 2026-10-22). The
	// first version also said "nobody can decide on their own record", and that is not
	// what the product does: a reviewer is an admin_users id and a record's subject an
	// employees id, so the self-review trigger compares two id spaces that never meet,
	// review.ErrSelfReview is "unreachable today" by its own comment, and
	// sys:no-self-review never runs because the review path calls no policy engine. A
	// manager who is also an employee can decide their own flag.
	FactReviewIsOnceAndFinal Fact = "schema:transaction_reviews unique+no_mutation"
	// transactions cannot be changed or deleted, by the application (UPDATE and DELETE
	// revoked) or by the owner (a trigger). Derived from migration 00005 and every
	// migration after it.
	FactRecordsAreImmutable Fact = "schema:transactions REVOKE+transactions_no_mutation"
	// A manual entry writes an audit row (manual.Action) in the same transaction.
	// Derived from internal/domain/manual.
	FactManualEntryIsAudited Fact = "domain:manual.Action via Trail.RecordTx"
	// Every cookie the product sets is a first-party HttpOnly cookie with a stated job
	// (the cookie notice, which TestCookieNotice_ListsExactlyTheCookiesTheProductSets
	// holds to the code), no shipped script writes a cookie, and the public pages' policy
	// loads nothing from another site. Derived from the notice, the scripts and the
	// policy.
	FactNoAdOrAnalyticsCookies Fact = "notice:every cookie first-party HttpOnly+self-only policy"
	// Every business record carries its tenant_id under forced row-level security,
	// production refuses a role that could bypass it, and every query names the
	// tenant again. Derived from the core tables' migrations, internal/db and
	// db/queries.
	FactBusinessesAreIsolatedByRLS Fact = "schema:tenant_id+FORCE RLS+role gate+queries"
	// Taptime's staff sign in separately (internal/operatorauth's own cookie), a read
	// into a business starts with a row in the append-only operator_audit_log, and every
	// op_* function that writes outside the operator's own tables writes that log's row
	// in its own body, so no change commits without its row. Only that direction holds:
	// the functions change conditionally and write the row on every successful call, so
	// a row may record a call that changed nothing (00034 says so; 00063's suspend of a
	// tenant that does not exist). The change comes first inside the function: "first"
	// is said of reads only. Derived from the two cookie names, the latest op_begin_read
	// and the latest definition of every op_* function and of every function one calls.
	FactOperatorReadsAreAudited Fact = "operatorauth+op_begin_read audit first+op_* writes audited"
	// The one crossing between businesses is a holding Taptime sets up: outside one,
	// a tap on another business's plaque redirects and writes nothing; inside one, it
	// is recorded at the employer with the site named. Derived from
	// sys:tenant-mismatch, migration 00041's grants and 00043's site columns.
	FactHoldingIsTheOneCrossing Fact = "policy:sys:tenant-mismatch unless SameHolding"
	// A second phone's activation signs the first out: finishActivation revokes every
	// live session of the employee before it issues the new one. Derived from
	// internal/handler/activate.go (the call, and its place before the issue).
	FactNewPhoneSignsOutTheOld Fact = "handler:finishActivation RevokeAllForEmployee before Issue"
	// Inside a holding, with the API on, a company can list its peers' staff: names,
	// codes, external references and whether each is active (never an e-mail address).
	// Derived from migration 00055's holding_directory, internal/db's caller and the
	// API's /directory route.
	FactGroupListsPeersStaff Fact = "sql:holding_directory+GET /v1/directory"
	// Webhook deliveries are signed: WebhookHeader carries "sha256=" and an HMAC-SHA256
	// of the body under the endpoint's key. Derived from internal/webhook.
	FactWebhooksAreSigned Fact = "webhook:SignatureHeader HMAC-SHA256"
	// Production runs in Germany, in the EU. A DEPLOYMENT FACT, pinned to the document
	// that records the deployment (deploy/README.md: the one node is fsn1). The page
	// names the country and the union only (user decision K2).
	FactHostedInTheEU Fact = "deploy:the production node is in Germany"
)

// SecurityHeroDocket is the opening's sample record: the landing panel's first
// (LandingPanelPassDockets, which internal/handler runs through the engine) -- a real
// NFC tap on the venue's network, the position asked for and none given, approved on
// the network at its score. The position chip says "Asked · none given": the tap page
// asks for a position on every press, and here the phone gave none, which is why the
// docket reads "GPS no" and the score is the network's.
//
// WHY NOT A SAMPLE WITH A POSITION INSIDE THE RADIUS: with the network matched too it
// scores the full hundred, and the public site's unmeasured-number scanner refuses that
// figure on any page (TestLanding_MakesNoUnmeasuredNumericClaim). The scanner is not
// widened for a drawing.
var SecurityHeroDocket = LandingPanelPassDockets[0]

// SecurityRule is one rung of a ladder card: what happens, which engine statements
// decide it (Sids), and what it ends in. Verdict is the verdict's code ("ok", "flag",
// "reject", "ignored") or "" when there is none, and Note is the small mono text
// beside it ("" for none).
type SecurityRule struct {
	Sids    []string
	Text    Line
	Verdict string
	// Notes is the rung's small text beside its stamp: none or one. A slice rather than
	// a Line that may be empty, so every Line on the page has words (internal/handler's
	// walk refuses an empty one).
	Notes []Line
}

// SecurityNamed is a statement of the engine the page names in a sentence rather than
// in a card: its sid and the words that describe it.
type SecurityNamed struct {
	Sid    string
	Phrase string
}

// SecurityText is every sentence and rung of the page, built from the figures by
// SecurityCopy. Every Line field is collected by internal/handler's tests by
// reflection, so a Line added here is checked for its pins and for being rendered.
type SecurityText struct {
	// The opening, and the four chips of its sample check (ChipGPS matches
	// SecurityHeroDocket's "GPS no").
	Server      Line
	ChipSUN     Line
	ChipSession Line
	ChipIP      Line
	ChipGPS     Line

	// The four proofs' headings under the hood, the activation drawing's label and the
	// small headings of the ladder and privacy cards: each says something about the
	// product, so each is a Line.
	LinkTitle       Line
	PersonTitle     Line
	PlaceTitle      Line
	BackupTitle     Line
	ActivationLabel Line
	DebounceTitle   Line
	FingerTitle     Line
	LocationTitle   Line
	CookiesTitle    Line

	// The four proofs.
	ProofsLede Line

	LinkFresh    Line
	LinkOnce     Line
	LinkKeys     Line
	LinkSigLabel Line
	CtrAccepted  Line
	CtrSame      Line
	CtrOlder     Line

	PersonActivation Line
	PersonToken      Line
	PersonOff        Line
	PersonPhoneKeeps Line
	PersonDBKeeps    Line
	PersonNoRecord   Line

	PlaceAddress Line
	PlaceWidth   Line
	PlaceNarrow  Line
	PlaceWide    Line
	PlaceV6      Line

	BackupOnce    Line
	BackupRadius  Line
	BackupQR      Line
	BackupInside  Line
	BackupOutside Line
	BackupDefault Line

	// The ladder.
	LadderHeading  Line
	LadderLede     Line
	GuardrailLabel Line
	BaselineLabel  Line
	Guardrails     []SecurityRule
	Baseline       []SecurityRule
	Default        SecurityRule
	DefaultLabel   Line
	TrustNote      Line
	DebounceQueue  Line
	DebounceClock  Line
	// OtherGuards and OtherBaseline are the engine's statements the cards do not show,
	// named in LadderNote; UnnamedGuards are the guardrails the page deliberately does
	// not describe, each with its reason; NotedBaseline is the one baseline statement
	// that writes a note and decides nothing.
	OtherGuards   []SecurityNamed
	UnnamedGuards []SecurityNamed
	OtherBaseline []SecurityNamed
	NotedBaseline SecurityNamed
	LadderNote    Line

	// Flagged, never dropped.
	NeverLostHeading Line
	NeverLostLede    Line
	RollCounted      Line
	RollRefused      Line
	RollWaiting      Line
	NeverLostOnce    Line
	NeverLostHeld    Line
	NeverLostBeside  Line

	// Corrections.
	ImmutableHeading Line
	ImmutableRefused Line
	ImmutableManual  Line

	// Privacy.
	PrivacyHeading  Line
	PrivacyFinger   Line
	PrivacyNone     Line
	PrivacyLocation Line
	PrivacyCookies  Line

	// Isolation.
	IsolationHeading  Line
	IsolationRLS      Line
	IsolationOperator Line
	IsolationGroup    Line
	IsolationWebhook  Line
	IsolationDrawing  Line

	// Hosting.
	HostingWhere  Line
	HostingServer Line
	HostingGDPR   Line
}

// The ladder's verdict codes, as tap.Verdict spells them.
const (
	securityOK      = "ok"
	securityFlag    = "flag"
	securityReject  = "reject"
	securityIgnored = "ignored"
)

// SecurityCopy is every sentence of the page for the figures f. Each number in a
// sentence is one of f's; no number is written below.
func SecurityCopy(f SecurityFigures) SecurityText {
	n := strconv.Itoa
	debounce := n(f.DebounceSeconds) + " seconds"
	radius := n(f.GPSRadiusM) + " m"
	others := []SecurityNamed{
		{Sid: "sys:tap-freshness", Phrase: "refuse a tap page that is too old"},
		{Sid: "sys:occurred-at-bound", Phrase: "refuse a declared time that is implausible"},
		{Sid: "sys:tenant-mismatch", Phrase: "keep a plaque of a business outside your group out of your records"},
		{Sid: "sys:cross-tenant-qr-site-ip", Phrase: "hold a QR scan at a partner business to that venue's own network"},
		{Sid: "sys:policy-edit-owner-only", Phrase: "keep rule changes to an owner"},
	}
	// sys:no-self-review is the one guardrail the page does not describe: the review
	// path never asks the engine, so describing it would describe nothing that happens
	// (see FactReviewIsOnceAndFinal).
	unnamed := []SecurityNamed{{Sid: "sys:no-self-review", Phrase: "not described: the review path does not run it"}}
	otherBase := []SecurityNamed{
		{Sid: "base:queued-window", Phrase: "a tap whose declared time is well in the past"},
		{Sid: "base:ctr-gap-review", Phrase: "a tap after reads of the plaque the server never saw"},
	}
	noted := SecurityNamed{Sid: "base:cross-location-note", Phrase: "note a tap at another of your venues without judging it"}
	// l is a Line: the rungs' words carry their pins like every other sentence, and the
	// ladder test holds each rung's stamp to the statement that decides it.
	l := func(text string, facts ...Fact) Line { return Line{Text: text, Facts: facts} }
	guards := []SecurityRule{
		{Sids: []string{"sys:tag-not-active"}, Verdict: securityReject,
			Text: l("Plaque not in service", FactGuardrailsAreTheList, FactPlaqueCanBeReplaced)},
		{Sids: []string{"sys:sun-invalid"}, Verdict: securityReject,
			Text: l("Signature fails, or the counter did not move", FactGuardrailsAreTheList, FactCopiedLinkIsRefused)},
		{Sids: []string{"sys:no-session"},
			Text:  l("No session on this phone", FactGuardrailsAreTheList, FactActivationEndsOnARealTap),
			Notes: []Line{l("activation page · nothing recorded", FactGuardrailsAreTheList, FactActivationEndsOnARealTap)}},
		{Sids: []string{"sys:employee-deactivated"}, Verdict: securityReject,
			Text:  l("Account switched off", FactGuardrailsAreTheList, FactDeactivatedPersonIsRefused),
			Notes: []Line{l("security event", FactDeactivatedTapIsASecurityEvent)}},
		{Sids: []string{"sys:person-debounce"}, Verdict: securityIgnored,
			Text: l("Same person again within "+debounce, FactGuardrailsAreTheList, FactDebounceIsPerPersonOnTheServerClock)},
	}
	baseline := []SecurityRule{
		{Sids: []string{"base:ip-or-gps-ok", "base:gps-only-allow"}, Verdict: securityOK,
			Text: l("Venue network matched, or GPS inside the radius", FactBaselineCanBeChanged, FactGPSAloneApprovesATap)},
		{Sids: []string{"base:gps-conflict-review"}, Verdict: securityFlag,
			Text: l("GPS read, but outside the radius", FactBaselineCanBeChanged, FactFarPositionIsReviewed)},
		{Sids: []string{"base:qr-requires-ip"}, Verdict: securityFlag,
			Text: l("QR code without the venue network", FactBaselineCanBeChanged, FactCodelessTapNeedsAddress)},
		{Sids: []string{"base:no-evidence-review"}, Verdict: securityFlag,
			Text: l("Neither network nor GPS", FactBaselineCanBeChanged, FactFlaggedQueueDecidedByAManager)},
	}
	return SecurityText{
		Server: Line{
			Text:  "The checks run in the server, not on the phone.",
			Facts: []Fact{FactChecksRunOnTheServer, FactRulesRunInOrder},
		},
		ChipSUN:     l("SUN valid ✓", FactSignatureLengthAndKey, FactCopiedLinkIsRefused),
		ChipSession: l("Known person ✓", FactSessionTokenIsStoredHashed),
		ChipIP:      l("IP match ✓", FactAddressIsTheRequestsOwn),
		ChipGPS:     l("Asked · none given", FactPositionReadOnlyOnPress),

		LinkTitle:       l("A link that works once", FactCopiedLinkIsRefused),
		PersonTitle:     l("One phone, one person", FactNewPhoneSignsOutTheOld, FactSessionTokenIsStoredHashed),
		PlaceTitle:      l("A network list too wide to mean anything is ignored", FactWideAddressListIsNoEvidence),
		BackupTitle:     l("One position, only at the tap", FactPositionReadOnlyOnPress),
		ActivationLabel: l("Activation · once per phone", FactActivationEndsOnARealTap),
		DebounceTitle:   l("Debounce is per person", FactDebounceIsPerPersonOnTheServerClock),
		// 🔴 THE DENIAL IS ON THE SAME LINE AS THE WORD (scripts/redline-check.sh R1).
		FingerTitle:   l("No fingerprints, no face scans", FactNoAuthenticatorAPI),
		LocationTitle: l("No background location", FactPositionReadOnlyOnPress),
		CookiesTitle:  l("No ad or analytics cookies", FactNoAdOrAnalyticsCookies),

		ProofsLede: Line{
			Text: "Moment, person, place — and a backup for place, for when the network can't answer.",
			Facts: []Fact{FactCopiedLinkIsRefused, FactSessionTokenIsStoredHashed, FactAddressIsTheRequestsOwn,
				FactGPSAloneApprovesATap},
		},

		LinkFresh: Line{
			Text: "On every read the chip writes a fresh link: the plaque's id, a counter that only goes up, and a " +
				n(f.SignatureHexChars) + "-character signature (CMAC) made with the plaque's AES-" + n(f.PlaqueKeyBits) + " key.",
			Facts: []Fact{FactSignatureLengthAndKey, FactPlaqueIsPassive},
		},
		LinkOnce: Line{
			Text: "The server checks the signature, then moves the plaque's counter forward in one database statement " +
				"that refuses to go backwards. A second use of the same link finds the counter already at or past it and is rejected.",
			Facts: []Fact{FactChecksRunOnTheServer, FactCopiedLinkIsRefused},
		},
		LinkKeys: Line{
			Text:  "Plaque keys are stored only in encrypted form, under a separate key.",
			Facts: []Fact{FactPlaqueKeysAreWrapped},
		},
		LinkSigLabel: Line{
			Text:  n(f.SignatureHexChars) + " hex",
			Facts: []Fact{FactSignatureLengthAndKey},
		},

		CtrAccepted: Line{
			Text:  "Accepted — the counter moves to 000642",
			Facts: []Fact{FactCopiedLinkIsRefused},
		},
		CtrSame: Line{
			Text:  "The same link again — rejected",
			Facts: []Fact{FactCopiedLinkIsRefused},
		},
		CtrOlder: Line{
			Text:  "An older link — rejected",
			Facts: []Fact{FactCopiedLinkIsRefused},
		},

		PersonActivation: Line{
			Text: "A phone gets its session through activation: the employee opens their invite link, agrees, and " +
				"makes a first real NFC tap at one of your plaques. That tap writes no record; the next one clocks in.",
			Facts: []Fact{FactActivationEndsOnARealTap, FactPeopleAreInvitedFromTheDashboard},
		},
		PersonToken: Line{
			Text: "The phone keeps a random " + n(f.TokenBits) + "-bit token. The database keeps only a keyed hash " +
				"of it, so the stored value cannot be used to sign in.",
			Facts: []Fact{FactSessionTokenIsStoredHashed},
		},
		PersonOff: Line{
			Text:  "When you switch someone off, their next tap is rejected and the attempt is logged as a security event.",
			Facts: []Fact{FactDeactivatedPersonIsRefused, FactDeactivatedTapIsASecurityEvent},
		},
		PersonPhoneKeeps: Line{
			Text:  "A random " + n(f.TokenBits) + "-bit token",
			Facts: []Fact{FactSessionTokenIsStoredHashed},
		},
		PersonDBKeeps: Line{
			Text:  "A keyed hash of it, never the token",
			Facts: []Fact{FactSessionTokenIsStoredHashed},
		},

		PersonNoRecord: Line{
			Text:  "Session on this phone — no record written",
			Facts: []Fact{FactActivationEndsOnARealTap},
		},

		PlaceAddress: Line{
			Text: "The address that counts is the one the request arrives from, checked against the ranges saved for " +
				"the plaque's venue. The Wi-Fi name is not evidence.",
			Facts: []Fact{FactAddressIsTheRequestsOwn, FactPlaqueIdentifiesThePlace},
		},
		PlaceWidth: Line{
			Text: "A list of ranges stops counting once it is too wide to stand for a single network: more addresses " +
				"than a /" + n(f.IPv4Prefix) + " holds in IPv4, or a /" + n(f.IPv6Prefix) + " in IPv6. A wider list is " +
				"refused when you save it and is not counted when a tap is judged. Inside that limit, keep the list to " +
				"your venue's own connection.",
			Facts: []Fact{FactWideAddressListIsNoEvidence},
		},
		PlaceNarrow: Line{
			Text:  "Within the limit — up to a /" + n(f.IPv4Prefix) + "'s worth of addresses: can count as proof of place.",
			Facts: []Fact{FactWideAddressListIsNoEvidence},
		},
		PlaceWide: Line{
			Text:  "Too wide — refused when saved, not counted when a tap is judged.",
			Facts: []Fact{FactWideAddressListIsNoEvidence},
		},
		PlaceV6: Line{
			Text:  "IPv6 follows the same rule, with the limit at /" + n(f.IPv6Prefix) + ".",
			Facts: []Fact{FactWideAddressListIsNoEvidence},
		},

		BackupOnce: Line{
			Text: "The tap page asks the phone for its position once, when the button is pressed. Nothing reads it in " +
				"the background, before or after.",
			Facts: []Fact{FactPositionReadOnlyOnPress},
		},
		BackupRadius: Line{
			Text: "The radius is a deployment setting, " + radius + " by default. A position inside it counts as proof " +
				"of place; by default, a position outside it sends the tap to a manager, even if the network matched.",
			Facts: []Fact{FactGPSRadiusIsADefault, FactGPSAloneApprovesATap, FactFarPositionIsReviewed},
		},
		BackupQR: Line{
			Text: "A QR check-in has no signature, so by default a position alone is not enough for it: it needs the " +
				"venue's network.",
			Facts: []Fact{FactCodelessTapNeedsAddress},
		},
		BackupInside: Line{
			Text:  "Inside — counts as proof of place.",
			Facts: []Fact{FactGPSAloneApprovesATap},
		},
		BackupOutside: Line{
			Text:  "Outside — sent to a manager by default, even with a network match.",
			Facts: []Fact{FactFarPositionIsReviewed},
		},
		BackupDefault: Line{
			Text:  "radius: " + radius + " by default",
			Facts: []Fact{FactGPSRadiusIsADefault},
		},

		LadderHeading: Line{
			Text:  "Built-in rules first, your rules next, a person last.",
			Facts: []Fact{FactRulesRunInOrder, FactFlaggedQueueDecidedByAManager},
		},
		LadderLede: Line{
			Text:  "Nothing is approved by default. When no rule decides a tap, it is flagged for a manager.",
			Facts: []Fact{FactUndecidedTapIsFlagged, FactFlaggedQueueDecidedByAManager},
		},
		GuardrailLabel: Line{
			Text:  "Built in · no business can switch them off · first match wins",
			Facts: []Fact{FactRulesRunInOrder, FactGuardrailsAreTheList},
		},
		BaselineLabel: Line{
			Text:  "The baseline · you can change these · most specific wins, then the strictest",
			Facts: []Fact{FactRulesRunInOrder, FactBaselineCanBeChanged},
		},
		Guardrails: guards,
		Baseline:   baseline,
		Default: SecurityRule{Verdict: securityFlag,
			Text:  l("No rule above decided the tap", FactUndecidedTapIsFlagged),
			Notes: []Line{l("queued for a manager", FactFlaggedQueueDecidedByAManager)}},
		DefaultLabel: Line{
			Text:  "Built in · the engine's own last rung",
			Facts: []Fact{FactUndecidedTapIsFlagged, FactRulesRunInOrder},
		},
		TrustNote: Line{
			Text:  "Every record carries this score. It is shown on the record and decides nothing: the rules do.",
			Facts: []Fact{FactTrustScoreDecidesNothing},
		},
		DebounceQueue: Line{
			Text: "A queue of people can tap the same plaque one after another, and every tap is recorded. Only the " +
				"same person tapping again within " + debounce + " is ignored.",
			Facts: []Fact{FactDebounceIsPerPersonOnTheServerClock},
		},
		DebounceClock: Line{
			Text:  "The " + debounce + " are timed on the server's clock, so a phone's clock cannot shorten them.",
			Facts: []Fact{FactDebounceIsPerPersonOnTheServerClock},
		},
		OtherGuards:   others,
		UnnamedGuards: unnamed,
		OtherBaseline: otherBase,
		NotedBaseline: noted,
		LadderNote: Line{
			Text: countWordCap(len(guards)) + " of the " + countWord(f.Guardrails) + " guardrails are shown. The others include rules that " +
				joinPhrases(others) + ". Your rules also flag " + joinPhrasesOr(otherBase) + ", and " + noted.Phrase + ". The " + n(f.DebounceSeconds) + "-second window and the " +
				radius + " radius are deployment settings; the figures shown are the defaults.",
			Facts: []Fact{FactGuardrailsAreTheList, FactBaselineCanBeChanged, FactDebounceIsPerPersonOnTheServerClock,
				FactGPSRadiusIsADefault},
		},

		NeverLostHeading: Line{
			Text:  "Flagged, never dropped.",
			Facts: []Fact{FactFlaggedQueueDecidedByAManager, FactUndecidedTapIsFlagged},
		},
		NeverLostLede: Line{
			Text:  "Anything unverified is stored, flagged and queued for manager review; by default nothing is approved without evidence.",
			Facts: []Fact{FactFlaggedQueueDecidedByAManager, FactUndecidedTapIsFlagged},
		},
		RollCounted: Line{
			Text:  "Approved — hours counted",
			Facts: []Fact{FactFlaggedHoursWaitOutsideTheTotal, FactReviewIsApproveOrReject},
		},
		RollRefused: Line{
			Text:  "Rejected — hours refused",
			Facts: []Fact{FactFlaggedHoursWaitOutsideTheTotal, FactReviewIsApproveOrReject},
		},
		RollWaiting: Line{
			Text:  "Until then — waiting, not counted",
			Facts: []Fact{FactFlaggedHoursWaitOutsideTheTotal},
		},
		NeverLostOnce: Line{
			Text:  "A manager decides each flagged record once, and the decision cannot be changed afterwards.",
			Facts: []Fact{FactReviewIsOnceAndFinal, FactFlaggedQueueDecidedByAManager},
		},
		NeverLostHeld: Line{
			Text:  "Until then its hours are held out of the total, and The Roll shows them as waiting.",
			Facts: []Fact{FactFlaggedHoursWaitOutsideTheTotal},
		},
		NeverLostBeside: Line{
			Text:  "The decision is stored beside the record; the record itself never changes.",
			Facts: []Fact{FactReviewIsOnceAndFinal, FactRecordsAreImmutable},
		},

		ImmutableHeading: Line{
			Text:  "Records are never edited. Corrections are new records.",
			Facts: []Fact{FactRecordsAreImmutable, FactManualEntryNamesAManager},
		},
		ImmutableRefused: Line{
			Text: "The application has no permission to change or delete a stored record, and a guard in the database " +
				"refuses the database owner's account too; switching that guard off would take a deliberate change " +
				"to the database itself, or a database superuser.",
			Facts: []Fact{FactRecordsAreImmutable},
		},
		ImmutableManual: Line{
			Text: "When a shift needs fixing, a manager adds an entry by hand. It is marked “Entered by a manager”, " +
				"stays apart from tapped records in reports, and the action is written to an audit log.",
			Facts: []Fact{FactManualEntryNamesAManager, FactManualEntryIsAudited},
		},

		// 🔴 THE HEADING CARRIES ITS DENIAL ON ITS OWN LINE (scripts/redline-check.sh R1).
		PrivacyHeading: Line{
			Text:  "No fingerprints. No tracking. No ad cookies.",
			Facts: []Fact{FactNoAuthenticatorAPI, FactPositionReadOnlyOnPress, FactNoAdOrAnalyticsCookies},
		},
		PrivacyFinger: Line{
			Text:  "Nothing in the product reads a finger or a face. The phone's own screen lock stays the phone's.",
			Facts: []Fact{FactNoAuthenticatorAPI},
		},
		PrivacyNone: Line{
			Text:  "None collected, ever",
			Facts: []Fact{FactNoAuthenticatorAPI},
		},
		PrivacyLocation: Line{
			Text:  "GPS is read only at the moment of the tap. Nothing follows the phone between taps.",
			Facts: []Fact{FactPositionReadOnlyOnPress},
		},
		PrivacyCookies: Line{
			Text:  "Taptime sets no advertising or analytics cookie, and its public pages load nothing from another site.",
			Facts: []Fact{FactNoAdOrAnalyticsCookies},
		},

		IsolationHeading: Line{
			Text:  "Every business is walled off in the database itself.",
			Facts: []Fact{FactBusinessesAreIsolatedByRLS},
		},
		IsolationRLS: Line{
			Text: "Every record of a business carries that business's id, and row-level security in the database " +
				"filters every query by it. In production the application refuses to start under a database role " +
				"that could switch that filter off, and its queries name the business a second time.",
			Facts: []Fact{FactBusinessesAreIsolatedByRLS},
		},
		IsolationOperator: Line{
			Text: "Taptime's support console has its own sign-in, separate from yours. Every read it makes into " +
				"an account is written to an operator audit log first, and every change it makes to an account is " +
				"written there in the same transaction as the change: no change is stored without its entry.",
			Facts: []Fact{FactOperatorReadsAreAudited},
		},
		IsolationGroup: Line{
			Text: "The one crossing between businesses is a group Taptime sets up for companies that belong together: " +
				"their staff can clock in at each other's venues, and the record lands with the person's own employer. " +
				"The venue they clocked in at sees the name, the time, the direction and the outcome, never the " +
				"location evidence. With the API switched on, each company in the group can also list the others' " +
				"staff: their names, codes and external references, and whether each is active.",
			Facts: []Fact{FactHoldingIsTheOneCrossing, FactGroupListsPeersStaff, FactAPIEnabledPerBusinessByTaptime},
		},
		IsolationWebhook: Line{
			Text: "When counted taps are sent to your own systems by webhook, every delivery is signed: its " +
				f.WebhookHeader + " header is an HMAC-SHA256 of the body under your endpoint's own key.",
			Facts: []Fact{FactWebhooksAreSigned, FactAPIFeedsEveryCountedTap},
		},
		IsolationDrawing: Line{
			Text:  "Each row carries its business. A query from one side returns none of the other's.",
			Facts: []Fact{FactBusinessesAreIsolatedByRLS},
		},

		HostingWhere: Line{
			Text:  "Hosted in the EU (Germany)",
			Facts: []Fact{FactHostedInTheEU},
		},
		HostingServer: Line{
			Text:  "Taptime's production server runs in Germany.",
			Facts: []Fact{FactHostedInTheEU},
		},
		HostingGDPR: Line{
			Text:  "Built for GDPR from day one.",
			Facts: []Fact{FactNoAuthenticatorAPI, FactPositionReadOnlyOnPress, FactHostedInTheEU},
		},
	}
}

// countWordCap is countWord with a capital, for the start of a sentence.
func countWordCap(n int) string {
	w := countWord(n)
	return strings.ToUpper(w[:1]) + w[1:]
}

// joinPhrases joins the phrases as a list: "a, b and c".
func joinPhrases(ns []SecurityNamed) string { return joinWith(ns, " and ") }

// joinPhrasesOr joins the phrases as alternatives: "a, b or c".
func joinPhrasesOr(ns []SecurityNamed) string { return joinWith(ns, " or ") }

func joinWith(ns []SecurityNamed, last string) string {
	ps := make([]string, len(ns))
	for i, x := range ns {
		ps[i] = x.Phrase
	}
	if len(ps) < 2 {
		return strings.Join(ps, "")
	}
	return strings.Join(ps[:len(ps)-1], ", ") + last + ps[len(ps)-1]
}

// SecurityStampWord is the word the product's stamp prints for a verdict
// (components.VerdictStamp), or "" for a rung that ends in no verdict.
func SecurityStampWord(verdict string) string {
	switch verdict {
	case securityOK:
		return "APPROVED"
	case securityFlag:
		return "FLAGGED"
	case securityReject:
		return "REJECTED"
	case securityIgnored:
		return "IGNORED"
	}
	return ""
}
