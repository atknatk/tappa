package handler

// panelstart_test.go -- M7-03 phase B: what the panel says to a business that has
// not started yet.
//
// 🔴 THE DEFECT THESE MEASURE WAS FOUND BY DRIVING THE PRODUCT, NOT BY READING IT.
// A tenant was registered through the real wizard against real Postgres and then
// signed in, and every panel screen it landed on was read back in order. The
// wizard's last screen explains the plaques, /admin/locations explains the plaques
// ("No plaques have been loaded for this business yet"), and the screen IN BETWEEN
// -- the one the "Sign in to your dashboard" button actually opens -- said:
//
//	Nothing here yet / No taps recorded on this day
//	Nothing was recorded here on this day. Pick another day above.
//
// A business forty seconds old was being sent through a date picker.
//
// 🔴 AND THE SENTENCE THAT FIXES IT IS THE DANGEROUS PART. "Your plaques are on the
// way" is the obvious wording and it is FORBIDDEN: nothing in this product knows
// whether anything has been ordered, packed or posted. There is no order, no
// shipment and no delivery date in the schema, so those words would be an invention
// at the exact moment a new customer is deciding whether to trust us. What is
// derivable is how many plaque rows this business has and what state they are in --
// and TestPanelLanding_MakesNoDeliveryClaim keeps every future edit to that.
//
// ⚠️ AND THE WIZARD'S OWN LAST SCREEN WAS BREAKING THAT RULE WHILE THIS FILE QUOTED
// IT APPROVINGLY. pages/signup.templ said "Tappa encodes them and posts them to
// you"; the first version of the scan below held the literal "posted to you" and the
// inflection walked straight through it, on a page the scan did not read anyway. Both
// halves are fixed: the sentence is gone, the patterns cover conjugations, and
// TestSignupSurface_MakesNoDeliveryClaim points them at the sign-up surface too.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/web/templates/pages"
)

// plaqueStateFixture is one reading of a business's plaques and the sentence the
// landing section must answer with.
type plaqueStateFixture struct {
	name    string
	plaques ledger.Plaques
	state   pages.PlaqueState
	// want is a phrase the notice must carry, and "" means the screen must stay
	// silent about plaques altogether. Every other row's `want` becomes a phrase
	// that must NOT appear, which is where the mutual-exclusion assertion comes
	// from — the table needs no second list.
	want string
}

// plaqueStates is the closed table: one row per state pages.PlaqueState can hold,
// including the two in which the screen must stay silent.
var plaqueStates = []plaqueStateFixture{
	{
		name:    "never measured",
		plaques: ledger.Plaques{},
		state:   pages.PlaqueStateUnknown,
	},
	{
		name:    "one plaque in service",
		plaques: ledger.Plaques{Queried: true, InService: 1, Loaded: 1},
		state:   pages.PlaqueStateWorking,
	},
	{
		name:    "nothing loaded at all",
		plaques: ledger.Plaques{Queried: true},
		state:   pages.PlaqueStateNoneLoaded,
		want:    "No plaques have been loaded for this business yet",
	},
	{
		name:    "loaded, none mounted",
		plaques: ledger.Plaques{Queried: true, InStock: 3, Loaded: 3},
		state:   pages.PlaqueStateInStock,
		want:    "None of this business's plaques is mounted",
	},
	{
		// A BUSINESS WITH A HISTORY AND NO WORKING PLAQUE. It is not new, it is
		// broken: everything it has was retired or reported lost, so it has records
		// behind it and no way to add one.
		name:    "every plaque retired or lost",
		plaques: ledger.Plaques{Queried: true, Loaded: 2},
		state:   pages.PlaqueStateOutOfService,
		want:    "Every plaque this business has is out of service",
	},
}

// TestPanelLanding_SaysWhyNobodyCanTapYet is the property phase B exists for: the
// screen a new customer lands on explains itself.
//
// IT ASSERTS MUTUAL EXCLUSION, NOT JUST PRESENCE. Every row checks that the OTHER
// states' sentences are absent, so a template that printed all three (or that fell
// through to the wrong branch) fails rather than passing on a substring match.
func TestPanelLanding_SaysWhyNobodyCanTapYet(t *testing.T) {
	for _, tc := range plaqueStates {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeLedger()
			fake.plaques = tc.plaques
			b := panelBrowserWith(t, fake)

			rec := b.do(http.MethodGet, transactionsHref, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: %d, want 200", transactionsHref, rec.Code)
			}
			body := htmlOf(t, rec)

			if tc.want != "" && !strings.Contains(body, tc.want) {
				t.Errorf("the landing section does not say %q.\n"+
					"A business in this state cannot record a tap and the screen must say so;\n"+
					"otherwise its empty day reads as a quiet day.", tc.want)
			}
			for _, other := range plaqueStates {
				if other.want == "" || other.want == tc.want {
					continue
				}
				if strings.Contains(body, other.want) {
					t.Errorf("state %q rendered another state's sentence %q.\n"+
						"The three cannot both be true: one of them is telling this customer\n"+
						"something about their plaques that is not so.", tc.state, other.want)
				}
			}
		})
	}
}

// TestPanelLanding_MakesNoDeliveryClaim is the "declared guarantee" tripwire for the
// panel's landing section. The patterns and their positive control are shared with
// the sign-up surface — see deliveryClaims.
//
// ⚠️ IT SCANS THE WHOLE RENDERED PAGE, not just the notice, so a later edit cannot
// move the claim into the empty state or the chrome and stay green.
func TestPanelLanding_MakesNoDeliveryClaim(t *testing.T) {
	for _, tc := range plaqueStates {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeLedger()
			fake.plaques = tc.plaques
			b := panelBrowserWith(t, fake)
			assertNoDeliveryClaim(t, "the landing section",
				screenText(t, htmlOf(t, b.do(http.MethodGet, transactionsHref, nil))))
		})
	}
	assertDeliveryScannerWorks(t)
}

// deliveryClaims are the phrases that assert a PHYSICAL DELIVERY -- that something
// was ordered, packed, sent, is on its way, or will arrive.
//
// 🔴 NOT ONE OF THEM HAS A COLUMN, A TABLE OR A TIMESTAMP BEHIND IT anywhere in
// db/migrations. A sentence using one is the product stating something it never
// measured, which is the class M5-11 closed and which M7-01, M7-02 and M7-03 phase A
// each shipped once.
//
// 🔴 THEY ARE REGEXPS BECAUSE AN INFLECTION SLIPPED PAST THE FIRST VERSION. That
// list held the literal "posted to you" while pages/signup.templ said "posts them to
// you" -- so the product forbade a promise on one screen and sold it on another, one
// click apart, and the scan could not see it. Every entry now covers its own
// conjugations.
//
// ⚠️ THEY ARE APPLIED TO VISIBLE TEXT (screenText), NOT TO RAW HTML. Every form in
// this product carries method="post", so a `\bpost\b` pattern over markup would fire
// on every page in the panel and the scan would have to be weakened to survive.
// 🔴 AND THE FIRST REGEXP SET STILL MISSED THE VERY SENTENCE IT REPLACED. An audit
// ran the twelve patterns against eight known-bad sentences and SIX walked through,
// including "Your plaques will be posted to you next week" — the passive voice of
// the literal that had just been removed. The cause was one pattern demanding an
// object pronoun (`post(s|ed|ing)? (it|them|your…)`), which the passive does not
// supply, and two others pinned to a single subject ("we … sent", "will arrive").
// The patterns below match a VERB plus a nearby recipient instead of a fixed phrase.
// 🔴 AND THE SECOND SET STILL DEMANDED A PRONOUN. A third audit wrote twenty
// known-bad sentences and SIXTEEN escaped, two of them straight through the hole
// this file claimed to have closed: "We are posting your plaques tomorrow" and "We
// will send the plaques on Monday". The comment said "verb plus a nearby recipient";
// the recipient list was still `to you|them|it|one|out|off` — pronouns only — so a
// NOUN object walked past. The same audit measured the fix: anchoring on
// `(the|your|our|their) plaques?` catches both AND leaves the negative control 9/9
// clean, so the round-3 defence ("widening breaks it") was true only of a BARE
// `plaques?` and not of an anchored one.
var deliveryClaims = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bon (the|its|their) way\b`),
	regexp.MustCompile(`(?i)\ben[- ]route\b`),
	regexp.MustCompile(`(?i)\bin transit\b`),
	regexp.MustCompile(`(?i)\b(tracking|consignment|reference)\s*(:|(number|code|link))`),
	regexp.MustCompile(`(?i)\b(courier|carrier|parcel|package|shipment|consignment|warehouse|depot)\b`),
	regexp.MustCompile(`(?i)\b(dhl|fedex|dpd|gls|maltapost|royal mail)\b`),
	regexp.MustCompile(`(?i)\bpost(s|ed|ing)?\b[^.]{0,30}\b(to you|to your|them|it|one|out|off|(the|your|our|their) (plaques?|wall tags?|tags?))\b`),
	regexp.MustCompile(`(?i)\b(in|by) the post\b`),
	regexp.MustCompile(`(?i)\bmail(s|ed|ing)?\b`),
	regexp.MustCompile(`(?i)\b(send|sends|sending|sent)\b[^.]{0,30}\b(you|your|them|it|these|(the|our|their) (plaques?|wall tags?|tags?))\b`),
	regexp.MustCompile(`(?i)\bexpect (it|them|your|these|the plaques?)\b`),
	regexp.MustCompile(`(?i)\bship(s|ped|ping)\b`),
	regexp.MustCompile(`(?i)\bd[ie]spatch(es|ed|ing)?\b`),
	regexp.MustCompile(`(?i)\bdeliver(s|ed|ing|y|ies)?\b`),
	regexp.MustCompile(`(?i)\barriv(e|es|ed|ing|al)\b`),
	regexp.MustCompile(`(?i)\b(will|should|shall) (arrive|reach you|be with you|get to you|be there)\b`),
	regexp.MustCompile(`(?i)\bwithin \w+ (working|business) days?\b`),
	regexp.MustCompile(`(?i)\bpack(s|ed|ing)?\b`),
	regexp.MustCompile(`(?i)\b(leaves?|left|leaving) (our|the) (warehouse|depot|office)\b`),
	regexp.MustCompile(`(?i)\b(has|have|had) (been )?(sent|posted|shipped|mailed|dispatched|despatched)\b`),
}

// deliveryClaimCorpus is the KNOWN-BAD list: sentences that were shipped, drafted,
// or built by an audit to walk past this scan. Every one of them must be caught.
//
// 🔴 IT EXISTS BECAUSE THE OLD POSITIVE CONTROL VALIDATED ITSELF. That control built
// its sample sentence OUT OF THE PATTERNS and then asked "did every pattern match?",
// which is true by construction and says nothing about coverage — so it stayed GREEN
// while six known-bad sentences walked through. This repo's fifth vacuous test, and
// the shape was new: a positive control that can only ever confirm what it was
// derived from.
//
// THE CORPUS IS THE ORACLE NOW, AND IT IS WRITTEN INDEPENDENTLY OF THE PATTERNS.
// The first eight rows are an auditor's, verbatim.
var deliveryClaimCorpus = []string{
	"Tappa encodes them and posts them to you.",
	"Your plaques will be posted to you next week.",
	"Your plaques are posted to you as soon as they are encoded.",
	"Tappa will send them to you.",
	"We will send your plaques.",
	"Your plaques are en route.",
	"We have mailed your plaques.",
	"Expect them within five working days.",
	// The claims the first version was written for, kept so a later edit cannot
	// trade the old coverage for the new.
	"Your plaques are on the way.",
	"They are in transit.",
	"Your tracking number is 7.",
	"We sent them by courier.",
	"They have shipped.",
	"It was dispatched yesterday.",
	"Delivery is next week.",
	"They will arrive next week.",
	"It is already in the post.",
	"Your plaque will reach you shortly.",
	// --- round 4: a third audit's escapes. SIXTEEN of its twenty sentences walked
	// through the round-3 patterns; these are the shapes that did it. The first two
	// are the pronoun hole in its pure form.
	"We are posting your plaques tomorrow.",
	"We will send the plaques on Monday.",
	"Your plaques are with DHL.",
	"The carrier has them.",
	"Your consignment number is 88421.",
	"Your plaques are packed and ready.",
	"A parcel is coming.",
	"They should be with you by Friday.",
	"Your order leaves our warehouse today.",
	"Your plaques were despatched this morning.",
	"They have been sent.",
	"The package is at our depot.",
	// --- round 5: a fifth audit. The round-4 anchor missed THE PRODUCT'S OWN SECOND
	// NAME FOR A PLAQUE: the panel section is called "Locations & Wall Tags"
	// (PanelSections), and that very phrase sits in the negative corpus below -- so
	// the noun-object hole was still open in exactly the words the product uses.
	// The other three are conjugations and punctuation the round-4 set could not see.
	"We are printing and posting the wall tags this week.",
	"We will send the wall tags on Monday.",
	"Tracking: XYZ",
	"Your hardware is en-route.",
	"They left our office this morning.",
}

// deliveryAllowedCorpus is the NEGATIVE control: sentences the product really ships
// on the two scanned surfaces. None may match.
//
// WITHOUT IT, `.*` WOULD PASS THE TEST ABOVE. A scan that catches every known-bad
// sentence and every good one is not a scan, it is a ban on prose.
var deliveryAllowedCorpus = []string{
	"Tappa encodes each plaque and loads it here; ask us for the ones you need and " +
		"they will appear in stock, ready to mount.",
	"No plaques have been loaded for this business yet, so nobody can tap in or out.",
	"None of this business's plaques is mounted, so nobody can tap in or out. " +
		"In stock, ready to mount: 3.",
	"Every plaque this business has is out of service - retired, or reported lost - " +
		"so nobody can tap in or out. Ask us for a replacement and it will appear in " +
		"stock, ready to mount.",
	"Locations & Wall Tags shows every plaque this business has.",
	"Tappa encodes each one and loads it into your dashboard; ask us for the ones " +
		"you need. Until one is mounted there is nothing to tap at.",
	"Nothing was recorded here on this day. Pick another day above.",
	"Sign in. Use the email address and password you just chose.",
	"Invite your team. Each person opens their own link once, on their own phone.",
}

// assertNoDeliveryClaim is the shared check. Both the panel's landing section and
// the sign-up surface run it, because the sentence this exists to stop lived on the
// sign-up surface while only the panel was watched.
func assertNoDeliveryClaim(t *testing.T, where, text string) {
	t.Helper()
	for _, re := range deliveryClaims {
		if m := re.FindString(text); m != "" {
			t.Errorf("%s says %q.\n"+
				"Nothing in this product knows whether a plaque has been ordered, packed\n"+
				"or sent -- there is no order, no shipment and no date in the schema --\n"+
				"so this is a promise the product cannot keep or even check.\n"+
				"pages/locations.templ shows the shape that is allowed: say what OUR\n"+
				"records hold (\"loads it here\"), never what a courier is doing.", where, m)
		}
	}
}

// assertDeliveryScannerWorks is the control, AND IT RUNS IN BOTH DIRECTIONS.
//
// 🔴 IT ASKS "IS EVERY KNOWN-BAD SENTENCE CAUGHT", NOT "DID EVERY PATTERN MATCH".
// The old version asked the second question against a sample built out of the
// patterns, which is true by construction — it was green while six of an auditor's
// eight sentences walked straight through the scan it was supposed to be validating.
// Removing a pattern must turn this red; so must weakening one.
func assertDeliveryScannerWorks(t *testing.T) {
	t.Helper()
	t.Run("every known delivery claim is caught", func(t *testing.T) {
		if len(deliveryClaimCorpus) < 33 {
			t.Fatalf("the known-bad corpus holds %d sentence(s); it is too small to be an "+
				"oracle for %d patterns", len(deliveryClaimCorpus), len(deliveryClaims))
		}
		for _, sentence := range deliveryClaimCorpus {
			caught := false
			for _, re := range deliveryClaims {
				if re.MatchString(sentence) {
					caught = true
					break
				}
			}
			if !caught {
				t.Errorf("%q walked through all %d delivery patterns.\n"+
					"That is a promise about a physical delivery this product cannot make, and\n"+
					"nothing would have stopped it reaching a customer's first screen.",
					sentence, len(deliveryClaims))
			}
		}
	})
	t.Run("the product's own sentences are not caught", func(t *testing.T) {
		for _, sentence := range deliveryAllowedCorpus {
			for _, re := range deliveryClaims {
				if m := re.FindString(sentence); m != "" {
					t.Errorf("pattern matched %q inside a sentence the product SHIPS:\n  %s\n"+
						"A scan that catches everything is a ban on prose, not a tripwire.",
						m, sentence)
				}
			}
		}
	})
}

// TestPanelLanding_ClaimsOnlyThatNOBODYCANTAP is the second half of the same
// discipline, and it caught a draft of this very notice.
//
// 🔴 "NOTHING CAN APPEAR ON THIS PAGE" IS FALSE AND WAS NEARLY SHIPPED. Two things
// reach the transactions list with no working plaque: a record a manager types by
// hand (channel='manual', reached from the Employees roster), and a tap on a plaque
// that is in stock or retired -- which is REJECTED and STILL WRITTEN DOWN, because
// §4.6 forbids dropping evidence. "Nobody can tap in or out" survives both, because
// ADR 0006 is explicit that a typed row is not a touch.
func TestPanelLanding_ClaimsOnlyThatNOBODYCANTAP(t *testing.T) {
	overclaims := []string{
		"nothing can appear", "nothing will appear", "no records can",
		"this page cannot fill", "nothing can be recorded",
		"no hours can be recorded", "nothing reaches this page",
	}
	for _, tc := range plaqueStates {
		if tc.want == "" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeLedger()
			fake.plaques = tc.plaques
			b := panelBrowserWith(t, fake)
			body := strings.ToLower(htmlOf(t, b.do(http.MethodGet, transactionsHref, nil)))

			for _, claim := range overclaims {
				if strings.Contains(body, claim) {
					t.Errorf("the landing section says %q.\n"+
						"A manager-typed record needs no plaque and a rejected tap is written "+
						"down anyway (§4.6); both land on this page.", claim)
				}
			}
			// POSITIVE CONTROL: the narrower claim the product IS entitled to make is
			// actually on the page, so the absence above is not the absence of a notice.
			if !strings.Contains(body, "nobody can tap in or out") {
				t.Error("the notice does not carry the one claim it is entitled to " +
					"(\"nobody can tap in or out\") -- the check above is passing because " +
					"there is no sentence, not because the sentence is careful")
			}
		})
	}
}

// TestPanelLanding_WithdrawsDatePickerAdviceOnlyWhereItCannotWork pins the second,
// smaller edit: "Pick another day above" is advice, and advice that cannot possibly
// work is worse than silence.
//
// 🔴 IT KEYS OFF THE RECORDS, NOT OFF THE PLAQUES, AND THE FIRST VERSION HAD IT
// EXACTLY BACKWARDS. It withdrew the advice when no plaque had been loaded — which
// is precisely the state in which a manager may have typed last week in by hand,
// because a manual record needs no plaque at all (an audit proved it with a real
// INSERT into a zero-plaque tenant, and TestPanelLandingDB_ManualRecordsKeepTheDate
// PickerOffered reproduces it against Postgres). The advice was being taken away
// from the only person whose records really were on another day.
//
// THE ZERO ROW IS THE SAFE DIRECTION: nothing measured means the advice STAYS.
func TestPanelLanding_WithdrawsDatePickerAdviceOnlyWhereItCannotWork(t *testing.T) {
	const advice = "Pick another day above"
	cases := []struct {
		name    string
		history ledger.History
		// plaques is varied ALONGSIDE history to prove the two are independent: the
		// worst row below has no plaques and a record, which is the audited case.
		plaques ledger.Plaques
		want    bool
	}{
		{
			name:    "never measured keeps the advice",
			history: ledger.History{},
			plaques: ledger.Plaques{Queried: true},
			want:    true,
		},
		{
			name:    "no records at all withdraws it",
			history: ledger.History{Queried: true, Any: false},
			plaques: ledger.Plaques{Queried: true},
			want:    false,
		},
		{
			name:    "records but no plaque KEEPS it (the manual-entry case)",
			history: ledger.History{Queried: true, Any: true},
			plaques: ledger.Plaques{Queried: true}, // nothing loaded, and yet records exist
			want:    true,
		},
		{
			name:    "records and a working plaque keeps it",
			history: ledger.History{Queried: true, Any: true},
			plaques: ledger.Plaques{Queried: true, InService: 1, Loaded: 1},
			want:    true,
		},
		{
			name:    "no records, plaques in the box, still withdrawn",
			history: ledger.History{Queried: true, Any: false},
			plaques: ledger.Plaques{Queried: true, InStock: 2, Loaded: 2},
			want:    false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeLedger() // Queried true, no records: a genuinely empty day
			fake.plaques = tc.plaques
			fake.history = tc.history
			b := panelBrowserWith(t, fake)
			body := htmlOf(t, b.do(http.MethodGet, transactionsHref, nil))

			// POSITIVE CONTROL, every row: the empty state really is on the page, so a
			// missing sentence cannot be mistaken for a withdrawn one.
			if !strings.Contains(body, "Nothing was recorded here on this day") {
				t.Fatalf("the empty state is missing entirely; this test measures nothing")
			}
			if got := strings.Contains(body, advice); got != tc.want {
				t.Errorf("%q present = %v, want %v", advice, got, tc.want)
			}
		})
	}
}

// TestPanelLanding_LinksOnlyToASectionThatExists keeps M7-01's rule inside the
// panel: a dead href looks like a working control.
//
// THE HREF IS DERIVED FROM PanelSections rather than compared with a literal, so
// moving the section moves the assertion with it.
func TestPanelLanding_LinksOnlyToASectionThatExists(t *testing.T) {
	href := pages.SectionHref(pages.TabLocations)
	label := pages.SectionLabel(pages.TabLocations)
	if href == "" || label == "" {
		t.Fatal("the panel has no Locations section; the notice must then render no link " +
			"at all, and this test needs rewriting rather than deleting")
	}

	fake := newFakeLedger()
	fake.plaques = ledger.Plaques{Queried: true}
	b := panelBrowserWith(t, fake)
	body := htmlOf(t, b.do(http.MethodGet, transactionsHref, nil))

	if !strings.Contains(body, `href="`+href+`"`) {
		t.Errorf("the notice does not link to %s", href)
	}
	// THE LABEL IS COMPARED IN ITS ESCAPED FORM, because "Locations & Wall Tags"
	// reaches the page as "Locations &amp; Wall Tags" and a raw comparison would be
	// a test that fails for a reason that is not a defect.
	if !strings.Contains(body, html.EscapeString(label)) {
		t.Errorf("the notice does not name the section the way the navigation does (%q)", label)
	}
}

// TestPanelLanding_SaysNothingWhenNothingWasMeasured is the anti-fabrication flag
// one layer down, where it can be mutated on its own.
//
// A ZERO PlaqueCounts IS WHAT A WIRING BUG PRODUCES -- a handler that forgets to
// copy the counts across -- and the screen must then say NOTHING about plaques
// rather than announce that a working business has none.
// 🔴 THE TWO ZERO VALUES POINT IN OPPOSITE DIRECTIONS, ON PURPOSE. An unmeasured
// PLAQUE reading must say nothing (a claim nobody measured), while an unmeasured
// HISTORY must keep the advice (withdrawing it would hide a working business's own
// records from it). Both are what a wiring bug produces — a handler that forgets to
// copy a field across — so both directions are asserted here rather than trusted.
func TestPanelLanding_SaysNothingWhenNothingWasMeasured(t *testing.T) {
	unmeasured := pages.TransactionsView{Queried: true}
	if got := unmeasured.PlaqueState(); got != pages.PlaqueStateUnknown {
		t.Errorf("an unmeasured view reports plaque state %q, want %q",
			got, pages.PlaqueStateUnknown)
	}
	if !unmeasured.OffersAnotherDay() {
		t.Error("an unmeasured view withdrew the date-picker advice. " +
			"\"We did not look\" must not take a control away from a working business.")
	}

	// POSITIVE CONTROLS: the same view, measured, does move in both directions.
	measured := pages.TransactionsView{Queried: true, Plaques: pages.PlaqueCounts{Queried: true}}
	if got := measured.PlaqueState(); got != pages.PlaqueStateNoneLoaded {
		t.Errorf("a measured, plaqueless view reports %q, want %q -- the check above is "+
			"passing because the finding does not exist", got, pages.PlaqueStateNoneLoaded)
	}
	withdrawn := pages.TransactionsView{Queried: true, History: pages.HistoryReading{Queried: true}}
	if withdrawn.OffersAnotherDay() {
		t.Error("a business measured to hold no records at all is STILL offered another " +
			"day -- the check above is passing because nothing can ever withdraw it")
	}
}

// TestPlaqueState_IsNotInferredFromASubtraction is the arithmetic the query
// deliberately does not do, asserted where the mistake would be made.
//
// 🔴 in_stock IS COUNTED, NOT DERIVED FROM loaded - in_service. The leftover also
// contains retired and lost plaques, so a business whose last plaque was retired
// would be told its replacement is waiting in the box -- a sentence that sends a
// manager to look in a drawer that is empty.
func TestPlaqueState_IsNotInferredFromASubtraction(t *testing.T) {
	retiredOnly := pages.TransactionsView{
		Plaques: pages.PlaqueCounts{Queried: true, InService: 0, InStock: 0, Loaded: 2},
	}
	if got := retiredOnly.PlaqueState(); got != pages.PlaqueStateOutOfService {
		t.Errorf("two retired plaques report state %q, want %q. "+
			"loaded - in_service would have made this look like stock.",
			got, pages.PlaqueStateOutOfService)
	}
	mixed := pages.TransactionsView{
		Plaques: pages.PlaqueCounts{Queried: true, InService: 0, InStock: 1, Loaded: 3},
	}
	if got := mixed.PlaqueState(); got != pages.PlaqueStateInStock {
		t.Errorf("one spare beside two retired plaques reports state %q, want %q",
			got, pages.PlaqueStateInStock)
	}
}

// --- job one: the structure choice ------------------------------------------

// TestSignupStructure_DecidesNothingAfterSignUp is the measurement that decided
// M7-03 phase B's first question, kept as a test so the decision cannot rot.
//
// 🔴 THE CARD SAYS `structure='multi'` "OPENS" DEPARTMENTS AND THE PRODUCT DOES NOT
// AGREE. tenants.structure is written by CreateTenant and read back by nothing: no
// screen, no panel query, no policy branches on it. The wizard used to tell the
// `multi` half of our customers that departments were theirs, which left the
// `single` half -- the shape our own demo data uses departments in -- unaware they
// exist at all.
//
// IT IS DERIVED WITH go/ast RATHER THAN grep, so a selector spelled across a line
// break or hidden behind a rename is still counted. If a future task DOES make the
// structure decide something, this test goes red and the wizard's wording is what
// must be revisited -- which is exactly the moment to revisit it.
func TestSignupStructure_DecidesNothingAfterSignUp(t *testing.T) {
	root := repoRootFrom(t)

	// WHAT IS ALLOWED TO NAME IT, BY FILE RATHER THAN BY PACKAGE. The sign-up path
	// legitimately carries the answer between its three steps, and internal/store is
	// generated from the INSERT. Everything else -- every panel screen, every other
	// handler, every domain package -- reading it would mean the choice decides
	// something after the row exists.
	//
	// A whole-package exemption was tried first and was too coarse: it would have
	// exempted every panel screen in web/templates/pages, which is precisely where a
	// department gate would appear.
	allowedDirs := map[string]bool{
		filepath.Join("internal", "domain", "signup"): true,
		filepath.Join("internal", "store"):            true,
	}
	allowedFilePrefix := "signup"

	var offenders []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", ".tools", "node_modules", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if allowedDirs[filepath.Dir(rel)] ||
			strings.HasPrefix(filepath.Base(rel), allowedFilePrefix) {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil // not our business to fail on a file the compiler will reject
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Structure" {
				return true
			}
			offenders = append(offenders, rel+":"+fset.Position(sel.Pos()).String())
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk the repo: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("something outside the sign-up path reads a tenant's structure:\n  %s\n\n"+
			"That would make the choice decide something after the row is written, which\n"+
			"the wizard's wording assumes it does not (web/templates/pages/signup.templ).\n"+
			"If the coupling is intended, the wizard must say so -- and it must say so for\n"+
			"the shape that actually loses out, which our own seed data says is `single`.",
			strings.Join(offenders, "\n  "))
	}

	// POSITIVE CONTROL FOR THE WALKER. Without it this passes just as happily when
	// the walk visits nothing, the parse fails silently, or the selector name is
	// misspelled -- three ways this shape of test has been vacuous in this repo.
	seen := 0
	fset := token.NewFileSet()
	src := "package p\ntype r struct{ Structure string }\nfunc f(x r) string { return x.Structure }\n"
	pf, perr := parser.ParseFile(fset, "probe.go", src, 0)
	if perr != nil {
		t.Fatalf("parse the control: %v", perr)
	}
	ast.Inspect(pf, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Structure" {
			seen++
		}
		return true
	})
	if seen != 1 {
		t.Fatalf("the selector scan found %d reads in a file built to contain exactly one; "+
			"the scan above is not measuring anything", seen)
	}

	// SECOND POSITIVE CONTROL, FOR THE WALK RATHER THAN THE SCAN: the walk really
	// reaches the sign-up files it exempts. Without this, an exemption that
	// accidentally matched EVERY file would look identical to a clean repo.
	exempted := 0
	if walkErr := filepath.Walk(filepath.Join(root, "internal", "domain", "signup"),
		func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(path, ".go") {
				exempted++
			}
			return err
		}); walkErr != nil {
		t.Fatalf("walk the exempted directory: %v", walkErr)
	}
	if exempted == 0 {
		t.Fatal("the exempted sign-up package holds no Go files; the exemption above is " +
			"pointing at nothing and the scan is therefore unanchored")
	}
}

// repoRootFrom walks up from the test's working directory to the module root.
func repoRootFrom(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find go.mod above the working directory")
	return ""
}
