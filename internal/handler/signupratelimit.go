package handler

import "time"

// The SIGN-UP budgets and the bot defence — the M7-02 card's "bot koruması (rate
// limit + basit challenge), CAPTCHA üçüncü parti değil".
//
// 🔴 THIS SURFACE CANNOT INHERIT M7-01's DECISION AND THE REASON IS WRITTEN IN
// M7-01's OWN FILE. handler.Marketing carries NO limiter, deliberately, and states
// the premise: "A request here renders a fixed component tree and returns; it
// touches no pool, takes no lock and appends to no table." It then names the exact
// moment that premise dies: "The moment this surface gains a POST — the sign-up
// wizard, M7-02 — it stops being true, and that endpoint arrives with its own budget
// and its own bot challenge." This file is that budget.
//
// FOUR BUDGETS, BECAUSE THIS FLOW SPENDS FOUR DIFFERENT SCARCE THINGS. The split is
// the design — internal/handler/ratelimit.go arrived at three for the activation
// flow after an audit measured what a single per-IP bucket bought (60 anonymous
// requests and the next GENUINE activation got a 429), and internal/handler/
// adminratelimit.go arrived at five for the panel the same way. Collapsing these
// would let the cheapest attack starve the most valuable request:
//
//	flood    per address, charged on EVERY wizard request. The DoS shield, and the
//	         only budget that can refuse a page load.
//	check    per address, charged ONLY when a request causes an OUTBOUND HTTP
//	         REQUEST TO VIES. It is the only budget in this product that bounds
//	         somebody ELSE'S service, and ours is not the resource it protects.
//	attempt  per address, charged by every POST to the FINAL step. That is the
//	         request that runs bcrypt at cost 12 and opens a write transaction, so
//	         this is the CPU bound and it is the analogue of adminAttemptLimit.
//	create   per address, charged ONLY by a SUCCESSFUL registration. It bounds
//	         `tenants` ROWS, which nothing else here does — and which is the fuel for
//	         the attack migration 00017 exists to blunt.
//
// LIMITS INHERITED FROM THE PRIMITIVE (httpx.Limiter, stated where it lives):
// in-process, cleared by a restart, per source address, and a FIXED window — so a
// burst spanning a boundary can reach 2x any number below. Every figure here is
// stated with that factor available.
const (
	// signupFloodLimit / signupFloodPeriod: the per-address shield over the whole
	// wizard.
	//
	// 600 IN 10 MINUTES, WHICH IS internal/handler/ratelimit.go's ACTIVATION CEILING
	// AND IS COPIED ON PURPOSE — the two surfaces have the same shape (a short
	// anonymous form flow, several requests per completion, one address shared by
	// everybody behind a NAT) and the activation number has survived two
	// re-derivations. WHAT ONE REGISTRATION COSTS, counted rather than guessed:
	//
	//	GET /signup            1     the business form
	//	POST /signup           1     -> 303
	//	GET /signup/places     1
	//	POST /signup/places    1     -> 303
	//	GET /signup/account    1
	//	POST /signup/account   1     -> 303   (bcrypt + the write transaction)
	//	GET /signup/done       1
	//	                       -     7 requests, plus one per corrected field
	//
	// So 600 carries roughly 85 complete registrations per window from one address,
	// against a realistic legitimate load of ONE — a business registers once, ever.
	// The ceiling is not sized for legitimate traffic here, it is sized so that
	// reaching it takes deliberate automation.
	signupFloodLimit  = 600
	signupFloodPeriod = 10 * time.Minute

	// signupCheckLimit / signupCheckPeriod: how many OUTBOUND VIES lookups one
	// address may make this server issue.
	//
	// 🔴 IT IS THE ONLY BUDGET IN THIS PRODUCT THAT PROTECTS A THIRD PARTY, and that
	// is why it exists separately rather than riding on the flood ceiling. Without
	// it, POST /signup with a format-valid VAT number is a request-amplification
	// primitive: one cheap request from an attacker becomes one request from OUR
	// address to the European Commission, at whatever rate the flood ceiling allows
	// (600 per window). Being the source of that traffic is a reputational and
	// operational problem for this deployment before it is anybody else's.
	//
	// TWENTY PER TEN MINUTES. A person correcting a mistyped VAT number spends two
	// or three; twenty is generous for a human and is 3% of the flood ceiling, so
	// the amplification factor is bounded at 20 rather than 600.
	//
	// ⚠️ IT IS CHARGED ONLY WHEN A LOOKUP ACTUALLY HAPPENS, i.e. after the FORMAT
	// check passes. A caller posting garbage spends the flood budget and nothing
	// here, which is correct — garbage costs no outbound request — and is also why
	// this budget cannot be used to deny a real customer their check: reaching it
	// takes twenty syntactically valid VAT numbers.
	//
	// WHAT A REFUSAL DOES: the registration CONTINUES, with the check recorded as
	// "not verified". That is the same fail-open Q09 requires of a VIES timeout, and
	// it is the only behaviour consistent with "a VIES failure never stops a
	// registration" — a budget that refused the whole request would be a stricter
	// gate than the outage it exists to survive.
	signupCheckLimit  = 20
	signupCheckPeriod = 10 * time.Minute

	// signupAttemptLimit / signupAttemptPeriod: the CPU bound.
	//
	// TEN PER TEN MINUTES, and it is deliberately the same number as
	// adminAttemptLimit because it bounds the same thing in the same units. The
	// arithmetic is the argument:
	//
	//	10 attempts x 1 bcrypt at cost 12 (~380 ms measured, internal/adminauth)
	//	  = ~3.8 s of CPU per 10-minute window per source address
	//	  = ~0.6% of one core, sustained. Across a fixed-window boundary: ~7.6 s.
	//
	// IT IS AN ORDER OF MAGNITUDE CHEAPER THAN THE LOGIN'S SAME-SHAPED BUDGET, and
	// the difference is worth stating because it is structural rather than lucky:
	// POST /admin/login compares up to adminauth.MaxCandidates (8) digests, so its
	// ten attempts buy ~30 s; a registration hashes ONCE, because there is exactly
	// one password and no candidate set to resolve.
	//
	// TEN IS ALSO FAR ABOVE WHAT A PERSON NEEDS. A registration is submitted once;
	// the retries this budget has to leave room for are a rejected VAT number
	// ("already registered") and a password the form refused, which is two or three.
	signupAttemptLimit  = 10
	signupAttemptPeriod = 10 * time.Minute

	// signupCreateLimit / signupCreatePeriod: how many BUSINESSES one address may
	// create.
	//
	// 🔴 THIS IS THE BUDGET MIGRATION 00017 IS ABOUT, and it is the only one whose
	// number was chosen against a security measurement rather than against a load
	// estimate. `tenants` rows are the attacker's fuel in the candidate-stuffing
	// attack that file measures: an address planted in enough tenants pushes a real
	// customer's admin row out of the window adminauth.MaxCandidates compares. 00017
	// makes that attack need FORESIGHT (the ordering is incumbent-first now); this
	// makes it need TIME.
	//
	// THREE PER HOUR. To assemble the eight rows that fill the compared window an
	// attacker needs THREE HOURS from one address, or three addresses — against a
	// legitimate requirement of exactly ONE registration per business, ever.
	//
	// ⚠️ WHAT IT DOES NOT DO, AND THE HONEST BOUND ON ALL OF THIS: it is keyed on a
	// source address, so a caller with several is bounded in proportion to how many
	// they have. httpx.Limiter's own comment says the same about every budget in this
	// product ("No in-process fixed window survives a truly distributed source —
	// that is what the shared store in M8 is for"). What this buys is that the attack
	// stops being FREE and stops being FAST, not that it stops being possible. The
	// thing that would actually close it is email verification before an admin row is
	// resolvable, which needs a transport this repository does not have (Q02, M7-04)
	// — migration 00017 says the same and neither claims otherwise.
	//
	// A GENUINE CUSTOMER CANNOT REACH IT. The one shape that gets close is a
	// consultant registering several clients from one office in an afternoon; they
	// meet a screen that says to wait, which is a cost measured in minutes against an
	// attack measured in hours.
	signupCreateLimit  = 3
	signupCreatePeriod = time.Hour

	// signupMinFillSeconds is the MINIMUM TIME between the wizard being opened and
	// the final submission being accepted — and it is half of this flow's "simple
	// challenge".
	//
	// 🔴 WHY THERE IS NO PUZZLE, WHICH IS THE OBVIOUS READING OF THE CARD'S WORD
	// "challenge" AND WAS REJECTED WITH REASONS RATHER THAN SKIPPED. A visible
	// question ("what is six plus two?") would sit on the LAST step of the one flow
	// in this product that turns a stranger into a paying customer, and it buys very
	// little: a fixed question set is solved once by whoever is attacking, and a
	// generated arithmetic question is solved by four lines of script. So it would
	// cost every real customer friction at the conversion step, in exchange for
	// stopping only the attacker who cannot be bothered — which is the same attacker
	// the two mechanisms below stop for free.
	//
	// WHAT IS IMPLEMENTED INSTEAD, and each is named with what it actually catches:
	//
	//  1. A HONEYPOT FIELD (signupHoneypotField). An input no human sees, with a
	//     plausible name. A form-filling script that posts every field it finds fills
	//     it; a person cannot. Zero friction, and it catches the naive automation a
	//     puzzle would also have caught.
	//  2. THIS MINIMUM FILL TIME. The wizard's signed state carries the instant it
	//     was first minted and NEVER refreshes it (signupstate.go), so this measures
	//     the whole wizard rather than one step. Nobody types a company name, a VAT
	//     number, one or more venue names, their own name, an email address and a
	//     twelve-character password in eight seconds.
	//  3. THE SIGNED THREE-STEP SEQUENCE ITSELF, which is not a challenge but does
	//     the same work: POST /signup/account cannot be reached without a state blob
	//     this server signed, which cannot be obtained without posting the two
	//     earlier steps. A script CAN walk all three — it costs six requests and a
	//     cookie jar — and that is the honest limit of all of this.
	//
	// ⚠️ SO THE CLAIM IS BOUNDED AND IS NOT "BOT PROOF". These three plus the four
	// budgets above make automated mass registration cost real time from real
	// addresses. A determined attacker with a browser automation library and a pool
	// of addresses gets through, and the thing that would stop them is a third-party
	// CAPTCHA, which CLAUDE.md §1 forbids on this surface and which the card forbids
	// by name. What this product has instead is the create budget and 00017's
	// ordering, i.e. defence in the place the damage would land rather than at the
	// door.
	//
	// EIGHT SECONDS, and it is deliberately far below what a person takes. The gate
	// exists to catch a script that posts instantly, not to time a slow typist; a
	// number near the real human minimum would eventually refuse somebody who had
	// their details on the clipboard.
	signupMinFillSeconds = 8

	// signupHoneypotField is the name of the input no human fills in.
	//
	// 🔴 IT WAS "company_website" AND THAT WAS A REAL RISK TO A PAYING CUSTOMER. An
	// audit named it and could not measure it, which is the right way round: several
	// password managers carry a WEBSITE / URL field in their identity profile
	// (1Password's "Identity" item has one), and browser autofill heuristics key on
	// substrings of the field name. An identity autofill that filled this input would
	// make the wizard refuse a genuine registration at its last step — a false
	// positive here costs a customer, where a false negative costs one spam tenant.
	//
	// 🔴 AND "company_reference" WAS STILL WRONG, FOR A REASON THIS COMMENT ITSELF
	// OVERCLAIMED. It said "website"/"url" was "the only token any published autofill
	// taxonomy maps". It is not: `company` is a trigger in Chromium's published
	// COMPANY_NAME pattern set, and this form carries full_name (autocomplete="name")
	// and email beside it — exactly the shape that makes a browser treat it as an
	// identity form and offer profile autofill. A filled honeypot refuses the
	// submission, so that path ends with a PAYING CUSTOMER turned away at the last step
	// by a screen that deliberately will not say why.
	//
	// ⚠️ STILL NOT MEASURED, AND SAID PLAINLY: there is no browser in this repository
	// to drive, so nothing here proves a negative. What is done is remove every token a
	// published taxonomy is known to key on — "website"/"url" AND "company" — from the
	// field name and its label, and keep the two opt-out attributes that are actually
	// honoured (1Password's data-1p-ignore, LastPass's data-lpignore) beside
	// autocomplete="off". "internal_ref" appears in no autofill category and stays
	// plausible to a form-filling script, which is all the plausibility was ever for.
	//
	// 🔴 AND THE FIRST RENAME ONLY DID HALF OF THAT, WHILE THIS COMMENT SAID "AND ITS
	// LABEL". A second security audit measured the rendered page: the constant was
	// clean but the markup still carried `company` FOUR times, in the visible label
	// ("Company reference") and in the id/for pair (signup-company-reference).
	// Chromium's COMPANY_NAME heuristics key on the label and the id as well as the
	// name, so the customer-losing path the rename exists to close was still open. The
	// label is "Reference" and the id is signup-internal-ref now, and the test asserts
	// against the RENDERED BODY rather than against this constant — a constant cannot
	// tell you what the page says.
	//
	// THE ASYMMETRY IS WHY THIS IS WORTH A RENAME RATHER THAN A CAVEAT: a false
	// positive costs a customer, a false negative costs one spam tenant.
	signupHoneypotField = "internal_ref"
)
