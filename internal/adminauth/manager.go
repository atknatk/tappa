package adminauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// Database is the narrow slice of *db.DB this package needs, declared HERE at the
// consumer (section 7).
//
// The three methods are deliberately different in kind:
//
//   - WithTenant runs every tenant-scoped read and write. Each query it wraps
//     carries an explicit tenant_id predicate on top of RLS (section 4.5).
//   - GetAdminByEmail and GetAdminSessionByTokenHash are the two CONTEXT-LESS
//     lookups (ADR 0002 madde 7, migration 00011): a login carries an email and a
//     panel request carries a cookie, and neither carries a tenant — the tenant is
//     the RESULT. They reach their rows through SECURITY DEFINER functions owned by
//     tappa_resolver, whose blast radius is a fixed column list on one table each.
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
	GetAdminByEmail(ctx context.Context, email string) ([]db.ResolvedAdmin, error)
	GetAdminSessionByTokenHash(ctx context.Context, tokenHash string) (db.ResolvedAdminSession, error)
}

// MaxCandidates bounds how many resolved identities ONE login request will
// bcrypt-compare — migration 00011's PHASE B OBLIGATION 4, the DoS bound, decided
// here against this package's own measurement as that file required.
//
// 🔴 IT IS EXPORTED, AND THAT IS A CORRECTION RATHER THAN CONVENIENCE. It used to
// be unexported, and internal/handler carried its OWN literal 8
// (adminChoiceMaxEntries) with a comment claiming the two were equal "by
// construction". An audit measured that claim FALSE: two independent literals in
// two packages, cross-referenced only by prose. Raising this one alone made a
// legitimate multi-business operator get HTTP 500 at the picker (9 verified
// candidates -> the blob refused to mint -> 500). Exporting it makes the handler's
// bound a DERIVED constant, so the two cannot drift — the equality is now held by
// the compiler instead of by a sentence.
//
// THE AMPLIFICATION, RE-MEASURED AND WORSE THAN 00011 SAYS. That file computes
// "~500x amplification, ~30-50 s of CPU off one credential-less request" from
// "at cost 10 one comparison is ~60-100 ms". Cost 10 is not what is deployed:
// test/fixtures/seed.sql stores `$2a$12$` digests, and cost 12 measures at
// 376-427 ms median on this machine (see the Cost comment for the full table).
// So an uncapped request against an address planted in 500 tenants would buy
// ~190 SECONDS of CPU, not 30-50. The direction of the correction matters more
// than the number: every figure derived from cost 10 is optimistic by ~4x.
//
// EIGHT — AND THE VALUE IS PART OF THE DELIVERABLE, NOT ONLY THE MECHANISM.
// 00011 calls this "the COST limit, MEASURED, and the largest number in this
// file", so a test that pins only "the loop stops at MaxCandidates" pins nothing:
// it is written in terms of this constant and moves with it. The NUMBER is pinned
// by TestMaxCandidates_IsTheMeasuredCPUBound, which carries the literal 8 and the
// arithmetic below, and turns red if anyone changes this line alone.
//
// THE ARITHMETIC IS THE ARGUMENT. The bound that matters is not
// per-request latency but CPU-per-window-per-address, because the login endpoint
// also carries a failure budget (internal/handler/adminratelimit.go, 10 failures
// per 10 minutes per address):
//
//	8 candidates x 10 failed attempts x ~380 ms = ~30.4 s of CPU
//	per 10-minute window per source address = ~5% of one core, sustained.
//
// 🔴 AND SINCE M7-02 THAT ARITHMETIC DESCRIBES EVERY LOGIN, NOT THE WORST ONE. This
// paragraph used to end "worst-case latency for a single login is 8 x 380 ms = ~3.0 s,
// and that shape is unreachable today: it needs one email registered in eight
// businesses. The ORDINARY case is one candidate, ~380 ms." padComparisons made the
// count CONSTANT, so the ordinary case IS the eight-comparison case — measured at
// ~1.75 s on this machine, ~1.89 s under -race.
//
// THE BOUND ABOVE IS UNCHANGED BY THAT, WHICH IS THE WHOLE REASON THE TRADE WAS
// AFFORDABLE: it was already written for eight comparisons per request. What moved is
// the TYPICAL cost, not the ceiling — an unregistered address simply stopped getting a
// discount that told the caller it was unregistered.
//
// 🔴 WHAT IT COSTS, NAMED RATHER THAN HIDDEN — A LOCKOUT, NOT A SLOWDOWN. The cap
// takes the FIRST eight rows the resolver returns. If an address is registered in
// more than eight businesses, the ninth onwards is never compared, so an owner whose
// row sorts late CANNOT LOG IN and the screen tells them only that the credentials
// are wrong (it must — OBLIGATION 1). From M7-02 that is reachable on purpose: the
// sign-up wizard is public and RLS lets a tenant write ANY email into its OWN
// admin_users. THE CAP CONVERTS A CPU DoS INTO AN ACCOUNT-LOCKOUT DoS.
//
// ✅ TWO THINGS CHANGED IN M7-02 AND THIS PARAGRAPH IS CORRECTED FOR BOTH, because it
// used to say "ORDER BY tenant_id" and "which is M7-02's to build":
//
//   - THE ORDER IS created_at NOW (migration 00017), so the eight compared are the
//     OLDEST. An EXISTING customer can no longer be displaced by later registrations
//     — measured 5/5 — which is the half of the lockout that mattered. What remains
//     is a customer registering INTO an address somebody has already stuffed, and
//     internal/domain/signup.Provisioner.signInBlocked now tells them so on the
//     confirmation instead of letting them discover it at a 401.
//   - EMAIL VERIFICATION WAS **NOT** BUILT. 00011 names it as the thing that closes
//     the class and this comment used to say it was "M7-02's to build"; M7-02 has no
//     email transport (Q02 is open, M7-04 owns it), so it is still open and is still
//     the closure. Saying otherwise would be a sentence declaring a guarantee the
//     product does not provide, which is the defect that blocked this task twice.
//
// THE ALTERNATIVE THAT WAS REJECTED, because it is the one three documents
// suggest: "stop at the first match". It bounds the work perfectly and it DEFEATS
// THE FEATURE — the picker exists because one person owns two businesses, and
// such a person very likely uses the SAME password for both, so the first match
// would silently log them into whichever business sorts first by tenant_id and
// they would never see the choice. (Combined with OBLIGATION 5 it is at least not
// a security hole: Verified() would hold exactly one entry and the picker would
// offer exactly that one. It is a product failure, not a bypass.)
const MaxCandidates = 8

// PickerCap bounds how many businesses the tenant picker may OFFER — one fewer than
// the number of candidates a request compares.
//
// 🔴 IT IS THE FIFTH CLOSURE, AND IT REFUTES A "GENERAL STATEMENT" M7-02 WROTE AND
// HAD TO WITHDRAW. Migration 00017 made the resolver incumbent-first, which fixed an
// account lockout and made a COUNT signal deterministic: an attacker who plants
// MaxCandidates rows for an address and signs in with their own password sees the
// picker offer 8 businesses if the address was unknown and 7 if somebody was already
// there, because the incumbent reliably takes the first slot and reliably fails to
// verify. M7-02 built four closures, measured all four as worse, and concluded that
// "any bound the caller can saturate reproduces the signal at the bound". THAT WAS
// WRONG, and an audit produced the counter-example by bounding the DISPLAY rather
// than the WINDOW.
//
// THE MECHANISM: with a comparison cap C and a display cap P, an unknown address
// shows min(k, C, P) and a registered one shows min(k, C-1, P), because the
// incumbent eats exactly one slot of the window. Setting P = C-1 makes the two equal
// FOR EVERY k. Measured over k = 0..40:
//
//	P = C  (no cap)   leaking k: 8, 9, 10, … 40
//	P = C-1           leaking k: none
//
// IT DOES NOT VIOLATE PHASE B OBLIGATION 5. That obligation forbids the picker being
// WIDER than the set whose hash verified; this makes it NARROWER, which is the
// direction the obligation is silent about because narrowing can only refuse access,
// never grant it.
//
// ⚠️ IT CLOSES THE COUNT CHANNEL AND NOT THE TIMING ONE. Authenticate's response TIME
// still varies with the number of candidates unless the comparisons are padded — see
// the padding note on Authenticate. The two are different channels with different
// prices and must not be confused.
//
// WHAT IT COSTS, NAMED: an operator whose password genuinely verifies in
// MaxCandidates businesses is offered one fewer and cannot reach the last one from
// the picker. That is the same class of cost MaxCandidates itself carries and it is
// reachable only by somebody with eight businesses under one address.
const PickerCap = MaxCandidates - 1

// Sentinel outcomes.
var (
	// ErrBadCredentials is the SINGLE failure of Authenticate, and its singleness
	// is migration 00011's PHASE B OBLIGATION 1 expressed in the type system: an
	// unknown email, a wrong password and a disabled admin all produce this exact
	// value, so a handler CANNOT branch on them even if it wanted to. The
	// distinction survives where it belongs — Authentication.Attempts carries it
	// for the audit trail, and audit_log is not a response body.
	ErrBadCredentials = errors.New("adminauth: invalid credentials")

	// ErrNoSession means the panel cookie identifies nothing: absent, malformed,
	// unknown, revoked, or belonging to an admin who is no longer active. All five
	// mean "go to the login page", and TouchAdminSession makes the last three
	// indistinguishable in SQL by design (db/queries/admins.sql).
	ErrNoSession = errors.New("adminauth: no valid admin session")
)

// Manager is the panel authentication lifecycle. Dependencies are injected as
// struct fields (section 7): no package-level state, no init() magic.
type Manager struct {
	data Database
	// hmacKey is this package's DERIVED key (token.go), held privately so a later
	// mutation of the caller's config slice cannot change how tokens hash.
	hmacKey []byte
	// dummy is the digest padComparisons falls back to when a login resolved NO
	// candidate, i.e. when there is no real stored digest to take the cost from.
	//
	// 🔴 IT IS A FIELD RATHER THAN THE PACKAGE CONSTANT BECAUSE THE PADDING'S COST
	// MUST TRACK THE ENVIRONMENT'S REAL COST, and an audit measured what a hardcoded
	// one costs: the shipped dummy is $2a$12$, this repository's fixtures are
	// bcrypt.MinCost, so every test login paid PRODUCTION price. Measured, 8
	// comparisons: cost 4 -> 10 ms, cost 10 -> 644 ms, cost 12 -> 2.52 s, and the
	// hardcoded dummy -> 3.42 s. A 342x overcharge on the test path for no security.
	//
	// PRODUCTION NEVER SETS IT — New installs dummyDigest, at the package Cost, which
	// is the cost every digest this repository writes carries (Hash). Only the
	// package's own test helper substitutes a cheaper one, the same way `now func()`
	// is injected elsewhere in this repo.
	dummy string
}

// New builds a Manager. It refuses a wrong-sized HMAC key rather than degrading:
// a short or empty key still produces plausible-looking hex, so the failure would
// stay invisible until somebody noticed sessions were forgeable.
func New(data Database, cfg *config.Config) (*Manager, error) {
	if data == nil {
		return nil, errors.New("adminauth: nil database")
	}
	if cfg == nil {
		return nil, errors.New("adminauth: nil config")
	}
	key, err := deriveKey(cfg.SessionHMACKey)
	if err != nil {
		return nil, err
	}
	return &Manager{data: data, hmacKey: key, dummy: dummyDigest}, nil
}

// Attempt is ONE candidate identity this request actually compared a password
// against. It exists so the caller can write an attributable audit row (section
// 4.6: a refused attempt still leaves a trace) — never so the caller can vary the
// response.
//
// NO DIGEST FIELD, AND THAT IS THE POINT: there is nothing here a password hash
// could travel in, so no later edit adds one by accident and no %+v of this type
// can leak one.
type Attempt struct {
	AdminUserID uuid.UUID
	TenantID    uuid.UUID
	// PasswordMatched is whether THIS row's stored digest verified.
	PasswordMatched bool
	// Active is whether this row's admin_users.status was 'active'. It is carried
	// separately from PasswordMatched because "the right password on a disabled
	// account" is a genuinely different security event from "the wrong password",
	// and the trail should be able to say which.
	Active bool
}

// Authentication is everything one login attempt established.
type Authentication struct {
	// Attempts is every candidate that was actually compared, in the resolver's
	// ORDER BY tenant_id order, capped at MaxCandidates.
	Attempts []Attempt
	// Resolved is how many candidates the resolver returned BEFORE the cap. It is
	// carried so the caller can log that a cap truncated the list — the one signal
	// that would otherwise make the lockout named at MaxCandidates invisible.
	Resolved int
}

// Truncated reports that the resolver returned more candidates than were
// compared, i.e. that an identity may exist which this login never tested.
func (a Authentication) Truncated() bool { return a.Resolved > len(a.Attempts) }

// Verified is one identity whose stored password digest ACTUALLY VERIFIED and
// whose account is active.
//
// 🔴 THIS TYPE IS PHASE B OBLIGATION 5. Migration 00011: "THE SESSION IS ISSUED
// ONLY TO THE CANDIDATE WHOSE HASH MATCHED, AND THE PICKER SHOWS ONLY MATCHED
// CANDIDATES." The obligation is a rule about a LINK between two steps, and the
// link is enforced by making this the ONLY type Issue accepts and the only type
// the picker is built from.
//
// ⚠️ IT IS NOT A SINGLE-PRODUCER TYPE, and an earlier version of this comment said
// it was ("there is no constructor for it outside Authentication.Verified()").
// THAT WAS FALSE and an audit named the second producer: internal/handler's
// adminChoices.parse builds Verified values out of the signed cookie, because the
// fields are exported and the struct is. So the honest statement of the guarantee
// has TWO legs, not one:
//
//	LEG 1 (this package)  Verified() is the only place a PASSWORD COMPARISON can
//	                      turn into an identity, and it ANDs matched AND active.
//	LEG 2 (the handler)   the only OTHER producer reconstructs identities from a
//	                      payload THIS DEPLOYMENT SIGNED (HMAC-SHA256 over the
//	                      verified set), and POST /admin/login/choose additionally
//	                      re-proves membership of that payload (selectVerified).
//	                      internal/handler/logincontext.go states this from its side.
//
// MAKING IT GENUINELY SINGLE-PRODUCER WAS CONSIDERED AND MEASURED AGAINST THE
// PACKAGE BOUNDARY, not against taste: unexported fields plus a constructor would
// stop the handler building one, so the signed-cookie FORMAT would have to move
// into this package — i.e. an HTTP cookie encoding would become a data-layer
// concern, which is the split CLAUDE.md section 3 draws (handler parses and
// renders; this package holds the authentication lifecycle). The result would be a
// named exported constructor taking the same two uuids, which is a second producer
// with a longer name rather than none. The sentence is corrected instead, and the
// second leg is where it belongs: in the file that owns it.
//
// THE ATTACK IT CLOSES, measured live in 00011 and reachable from M7-02: an
// attacker signs up their own tenant through the public wizard, writes the
// VICTIM'S EMAIL with a password THEY know into their OWN admin_users (RLS permits
// it — it is their tenant), and logs in. Two candidates resolve; the password
// matches the ATTACKER'S row. A picker built from the RESOLVED list would then
// offer the victim's business, and choosing it issues a session stamped with the
// victim's tenant — every subsequent request passing TouchAdminSession as
// role=owner. A picker built from THIS list offers one entry: the attacker's own.
type Verified struct {
	AdminUserID uuid.UUID
	TenantID    uuid.UUID
}

// Offered is the set the PICKER may show: Verified, truncated to PickerCap.
//
// IT IS A SEPARATE METHOD FROM Verified AND THAT IS DELIBERATE. Verified answers
// "did anything authenticate" — the question Authenticate itself decides on, and the
// question Issue's parameter type is about. Offered answers "what may be DISPLAYED",
// which is a narrower thing and the only place the display cap belongs. Folding the
// cap into Verified would mean a login that verified PickerCap+1 identities reported
// itself as having verified fewer, which is false and would eventually be read as an
// authentication fact.
//
// THE TRUNCATION TAKES THE FIRST P, i.e. the OLDEST, because the resolver is ordered
// created_at-first (migration 00017). So the entry an eight-business operator loses
// is their NEWEST — the one they are most likely to be able to reach another way.
func (a Authentication) Offered() []Verified {
	v := a.Verified()
	if len(v) > PickerCap {
		return v[:PickerCap]
	}
	return v
}

// Verified returns the candidates whose password verified AND whose account is
// active.
//
// IT IS THE ONLY WAY OUT OF Authentication INTO AN IDENTITY — precisely scoped:
// no PASSWORD COMPARISON in this repo becomes an identity except through this
// expression, and the two conditions are ANDed here in one place. That is what
// makes OBLIGATION 5 a property of the code rather than of the caller's memory.
//
// IT IS NOT THE ONLY PRODUCER OF A Verified VALUE, and the type comment says which
// the other one is and what holds the line there (a MAC this deployment computed,
// plus a membership re-check at the second step). Do not restate this as "nothing
// else can build one" — an audit refuted exactly that wording.
func (a Authentication) Verified() []Verified {
	out := make([]Verified, 0, len(a.Attempts))
	for _, at := range a.Attempts {
		if at.PasswordMatched && at.Active {
			out = append(out, Verified{AdminUserID: at.AdminUserID, TenantID: at.TenantID})
		}
	}
	return out
}

// Authenticate resolves an email globally and verifies the password against every
// candidate it is willing to compare.
//
// IT RETURNS ErrBadCredentials AND NOTHING ELSE WHEN NOBODY VERIFIES, whatever the
// underlying reason — that is OBLIGATION 1, and it is enforced here rather than in
// the handler so that a second handler (M7-04's reset, a future API) inherits it.
//
// THE DUMMY COMPARISON (OBLIGATION 2) IS IN THIS FUNCTION, not in the caller, for
// the same reason: a caller that forgets it turns the response TIME into the
// answer the body refuses to give. Zero candidates costs one full cost-12 bcrypt
// (~380 ms measured), which is what one candidate with a wrong password costs.
//
// ✅ THE RESIDUAL TIMING SIGNAL IS CLOSED AS OF M7-02, AND THE SENTENCE THAT USED TO
// STAND HERE WAS AN EXPIRING ASSUMPTION THAT EXPIRED. It read: an attacker "learns
// this address is registered in roughly N businesses — a weaker fact than 'this
// address exists', and one they can only obtain for an address that IS registered
// more than once". The public sign-up wizard lets an attacker CREATE that condition
// with a single registration, at which point the same measurement answers "does this
// address exist" outright: measured 216 ms against 437 ms with one planted row, with
// no overlap between the two ranges.
//
// Every login now performs exactly MaxCandidates comparisons — see padComparisons for
// the measurement, the alternatives and what it costs.
//
// A SECOND, SMALLER ASYMMETRY RIDES ALONG, measured and named rather than left
// for the next audit: a failure against a KNOWN address writes one audit_log row
// PER CANDIDATE, and a failure against an unknown one writes none.
//
// ⚠️ THIS FIGURE HAS BEEN WRONG TWICE — 0.3%, then 0.8-1.9% — AND BOTH TIMES IT
// WAS WRITTEN FROM A SINGLE RUN ON AN IDLE MACHINE. It is now measured in two
// stated load conditions, and the "it is inside the noise" claim is WITHDRAWN.
//
//	condition          one audit_log row   one cost-12 compare   row as % of login
//	idle                        2.29 ms             205.6 ms                1.11%
//	4 busy cores                3.64 ms             222.3 ms                1.64%
//
// An independent audit measured it end to end instead, with the three arms this
// comment describes, and got a WRITE-ONLY delta of +2.58% (known-with-row +3.18%
// vs unknown; known-with-writes-suppressed +0.58%) — larger than the per-row
// figure because a live request also pays transaction and connection overhead.
// So the honest band is ~1-3% of a ~300 ms response, i.e. roughly 2-10 ms.
//
// 🔴 AND IT IS NOT "INSIDE THE NOISE". That sentence was written from one idle
// run in which a median came out at -1.21%; the audit's fifteen interleaved rounds
// separated the two arms CLEANLY. What is true is narrower and worth stating
// exactly: the STRUCTURAL claim holds — the whole difference is the write, and it
// falls to +0.58% when the account's audit budget suppresses it — and the signal
// is small enough that extracting it needs many samples over a network. It is a
// REAL, measurable asymmetry that this code does not close, and it leaks strictly
// less than the candidate-COUNT signal above, which is cheaper to read. Counted,
// bounded by the account budget, not closed.
//
// ✅ AND THE TRADE THIS PARAGRAPH DECLINED HAS BEEN RE-TAKEN WITH THE CORRECTED
// PREMISE. It used to end "named as a limit; the numbers are here so the trade can be
// re-taken with different ones" — which is exactly what M7-02 did. padComparisons
// carries the new measurement and the reasoning; the short version is that the DoS
// bound MaxCandidates exists to hold is ALREADY sized for eight comparisons per
// request (internal/handler/adminratelimit.go), so padding spends no headroom that
// was not already budgeted.
func (m *Manager) Authenticate(ctx context.Context, email, password string) (Authentication, error) {
	// An email that cannot be a lookup key is refused HERE, and the dummy is paid
	// anyway, so the shape AND the timing of the response are identical to every
	// other failure.
	//
	// THREE CASES, and the third was found by a closing security audit:
	//   · empty email or password — cannot match a row (the resolver's equality
	//     never matches NULL, and admin_users.email is unique per tenant), but it
	//     would still reach the database.
	//   · an email carrying a NUL byte, or one that is not valid UTF-8. Postgres
	//     refuses these outright — `ERROR: invalid byte sequence for encoding
	//     "UTF8": 0x00 (SQLSTATE 22021)` — so before this check they travelled all
	//     the way to the driver and came back as a DATABASE ERROR, which the
	//     handler correctly reported as HTTP 500. Measured on the shipped stack:
	//
	//	NUL byte in the address      500 in 1.43 ms
	//	invalid UTF-8 in the address 500 in 1.43 ms
	//	an ordinary unknown address  401 in 201.6 ms
	//
	// IT WAS NOT AN ORACLE — the branch is decided entirely by the caller's own
	// bytes and never by server state — which is why it was reported as a limit to
	// be written down rather than a hole. It is CLOSED instead of documented
	// because closing it is strictly better on three counts: the panel stops
	// answering 500 on an unauthenticated path, the response joins the single
	// uniform refusal PHASE B OBLIGATION 1 asks for, and the request stops being
	// FREE — reaching this return means failLogin runs, so the attempt budget is
	// charged like any other failure. Before the fix none of the three held.
	if email == "" || password == "" || !isLookupableEmail(email) {
		m.pad(password, "", 0)
		return Authentication{}, ErrBadCredentials
	}

	candidates, err := m.data.GetAdminByEmail(ctx, email)
	if err != nil {
		// A database failure is NOT bad credentials. Collapsing them would turn an
		// outage into "your password is wrong" for every operator at once, and
		// would hide the outage behind a UX message (the same distinction
		// activate.go's rejectCode draws).
		return Authentication{}, fmt.Errorf("adminauth: authenticate: %w", err)
	}

	auth := Authentication{Resolved: len(candidates)}
	if len(candidates) > MaxCandidates {
		candidates = candidates[:MaxCandidates]
	}

	if len(candidates) == 0 {
		// OBLIGATION 2, generalised — see pad.
		m.pad(password, "", 0)
		return auth, ErrBadCredentials
	}

	auth.Attempts = make([]Attempt, 0, len(candidates))
	for _, c := range candidates {
		// EVERY candidate is compared, INCLUDING DISABLED ONES, and the status test
		// happens afterwards. Skipping the comparison for a disabled row would make
		// "disabled admin" cheaper than "wrong password" by a full bcrypt — the
		// third of OBLIGATION 1's three outcomes, leaking through the clock instead
		// of through the body.
		//
		// RevealForPasswordComparison is called HERE and nowhere else in the repo:
		// db.PasswordHash redacts every other path, so a grep for this name lists
		// every place a stored digest is in the clear, and this is the one.
		matched := Compare(c.PasswordHash.RevealForPasswordComparison(), password)
		auth.Attempts = append(auth.Attempts, Attempt{
			AdminUserID:     c.ID,
			TenantID:        c.TenantID,
			PasswordMatched: matched,
			Active:          c.Status == "active",
		})
	}

	// EVERY LOGIN PAYS FOR EXACTLY MaxCandidates COMPARISONS — see pad. The padding
	// is done against the FIRST candidate's own digest, so it costs exactly what the
	// real comparisons in this login cost.
	m.pad(password, candidates[0].PasswordHash.RevealForPasswordComparison(), len(candidates))

	if len(auth.Verified()) == 0 {
		return auth, ErrBadCredentials
	}
	return auth, nil
}

// pad brings the number of bcrypt comparisons this request performed up to
// MaxCandidates, so the total is CONSTANT whatever the resolver returned.
//
// 🔴 IT IS PHASE B OBLIGATION 2 GENERALISED, AND IT EXISTS BECAUSE THE PREMISE THAT
// LET THIS PACKAGE REJECT IT DIED IN M7-02. Authenticate's own comment costed padding
// and declined it, on the ground that the residual candidate-COUNT timing signal is
// "one they can only obtain for an address that IS registered more than once". The
// public sign-up wizard makes an attacker able to CREATE that condition: one
// registration carrying a victim's address is enough, and then the response time
// answers the question the body refuses to.
//
// MEASURED ON THE SHIPPED CODE, ONE planted row, nine interleaved rounds:
//
//	address UNKNOWN     median 216 ms   (208 – 226)
//	address REGISTERED  median 437 ms   (422 – 478)
//	delta 220 ms (102%), and the two ranges DO NOT OVERLAP
//
// So a single sign-in answered "does this address have a Tappa account", for the
// price of one sign-up. That is exactly what OBLIGATION 1 spends a dummy to refuse.
//
// WHY THE FULL CAP AND NOT A SMALLER FLOOR. A floor of F closes every k below F and
// leaks at k = F — the attacker simply plants F rows. MaxCandidates is the only value
// that cannot be saturated, because the WINDOW is capped there too: the comparison
// count is then min(resolved, 8) real plus the remainder in dummies, i.e. always 8.
// Measured cost per login on this machine, comparisons -> wall clock:
// 1 -> 207 ms · 2 -> 463 ms · 3 -> 656 ms · 8 -> ~1.8 s. (The arrow and two
// significant figures are deliberate: written as a bare count-then-duration pair, a
// sub-second figure reads as a thousands-separated number, which is the habit
// TestComments_DoNotQuoteTheDriftingRosterSize exists to break — and the digits past
// the second are machine state rather than a property of the code.)
//
// 🔴 THE TRADE, WITH BOTH READINGS, BECAUSE IT IS A REAL ONE:
//
//	LEAVE IT OPEN  an ordinary sign-in stays ~216 ms, and one sign-up buys a
//	               reliable answer to "is this address registered" — a question this
//	               product spends a full dummy bcrypt per failed login refusing.
//	CLOSE IT       every sign-in costs ~1.75 s, including every successful one.
//
// CLOSING IS CHOSEN, and the deciding measurement is that it needs NO NEW DoS
// HEADROOM: internal/handler/adminratelimit.go already sizes the attempt budget on
// "10 failures x 8 candidates x ~380 ms = ~30 s of CPU per window per address =
// ~5% of one core". Padding makes the TYPICAL request cost what that budget was
// already written for; it does not widen the ceiling, it removes the discount an
// unregistered address used to get. The latency lands on an administrative act
// performed once or twice a day, never on the tap path.
//
// ⚠️ WHAT IS STILL NOT CLOSED, so this is not read as more than it is: the audit_log
// WRITE asymmetry this file already documents (a failure against a KNOWN address
// writes a row per candidate, an unknown one writes none — measured at ~1-3% of the
// response). Against a ~1.75 s response that residual is proportionally smaller than
// it was against ~300 ms, but it is not gone, and it is bounded by the account budget
// rather than removed.
// 🔴 IT PADS AGAINST A **REAL** DIGEST WHEN THERE IS ONE, AND THE FIRST VERSION DID
// NOT — it always called CompareDummy, which carries a hardcoded $2a$12$. An audit
// measured the consequence on the test path: this repository's fixtures are
// bcrypt.MinCost, so a login whose real comparisons cost 1.3 ms each was padded with
// comparisons costing 315 ms each. Eight of them: 10 ms of real work followed by
// 3.42 s of padding.
//
// THE INVARIANT IS "THE PADDING COSTS WHAT A REAL COMPARISON HERE COSTS", not "the
// padding costs Cost". Deriving it from the digest actually being compared makes that
// true by construction in every environment, and it is the same technique Compare
// already uses for an over-long password ("pay exactly what an in-range comparison
// against THIS digest costs").
//
// digest == "" means the resolver returned nothing, so there is no real cost to copy
// and the package dummy is used: with zero candidates the only honest reference is
// what a real comparison WOULD have cost, which is Cost.
//
// ⚠️ THE PROPERTY HOLDS WHEN THE STORED DIGESTS SHARE A COST, which is what Hash
// guarantees for everything this product writes.
//
// 🔴 AND WHERE THEY DO NOT, THIS FUNCTION MAKES THE PRE-EXISTING MISMATCH CHANNEL
// EIGHT TIMES LOUDER — an earlier version of this paragraph said it "neither creates
// it nor closes it", which is wrong in the second half. Deriving the padding from the
// candidate's own digest means the whole login is priced at that digest's cost, so a
// MinCost row against the shipped cost-12 dummy measures about 7 ms for ONE candidate
// against about 1.7 s for zero — a ratio of roughly 240x, where a single unpadded
// comparison would have shown about 30x. (Two significant figures, for the reason
// given at the cost table above.)
//
// IT IS NOT REACHABLE TODAY and the reason is the same one Compare gives for the
// digest-side arm: adminauth.Hash is the only writer and it always uses Cost, so every
// stored digest shares a cost. It becomes reachable the moment anything writes a
// digest at another work factor — which is the shape migration 00017 declined to close
// with a CHECK constraint, and it is now one more reason that constraint is worth
// having.
func (m *Manager) pad(password, digest string, done int) {
	if digest == "" {
		digest = m.dummy
		if digest == "" {
			// A zero Manager would pad with nothing, i.e. not pad at all. Refuse to be
			// silently free: fall back to the package constant.
			digest = dummyDigest
		}
	}
	for i := done; i < MaxCandidates; i++ {
		// The result is discarded: for a real candidate this repeats a comparison
		// already recorded in Attempts, and for the dummy there is no account behind
		// it. What is wanted is the WORK, not the answer.
		_ = Compare(digest, password)
	}
}

// isLookupableEmail reports whether an address can be sent to Postgres at all.
//
// It checks the two things the DATABASE refuses rather than trying to validate an
// email address: a NUL byte, which Postgres rejects in any text value, and invalid
// UTF-8, which it rejects for a UTF8-encoded database. Anything else — a missing
// '@', a thousand characters, unusual unicode — is left alone deliberately: it is
// the resolver's job to find no row, and a Go-side notion of "a valid email" is a
// second source of truth about what admin_users.email accepts.
func isLookupableEmail(email string) bool {
	if strings.ContainsRune(email, 0) {
		return false
	}
	return utf8.ValidString(email)
}

// Choice is one row of the "which business?" picker: the business name plus this
// operator's identity within it.
type Choice struct {
	AdminUserID uuid.UUID
	TenantID    uuid.UUID
	TenantName  string
	FullName    string
	Role        string
}

// TenantChoices reads the display data for a set of VERIFIED identities.
//
// IT TAKES []Verified AND NOT []Attempt, WHICH IS THE POINT (OBLIGATION 5): there
// is no signature through which an unverified candidate can reach the picker.
//
// ⚠️ ONE TENANT-SCOPED TRANSACTION PER CANDIDATE, and that is structural rather
// than lazy: a single statement cannot span two tenant contexts, because the
// context is a transaction-local GUC (ADR 0002 madde 2). db/queries/admins.sql
// names this as "the picker's OTHER O(N)" and asks phase B to size it against the
// same measurement as the bcrypt bound.
//
// MEASURED ANSWER: it is bounded as a SIDE EFFECT of OBLIGATION 5 and needs no cap
// of its own. The loop runs over verified candidates, which is at most
// MaxCandidates (8) because that is how many were compared at all — so the 500
// transactions 00011 worried about are not reachable: 500 candidates yield at most
// 8 comparisons and therefore at most 8 verifications and therefore at most 8
// transactions. It also runs only AFTER a password has verified, so it is never
// credential-less work.
//
// A candidate that has been disabled between the comparison and this read simply
// does not come back (GetAdminForTenantChoice filters status='active') and is
// dropped from the picker rather than shown as a door that would not open.
func (m *Manager) TenantChoices(ctx context.Context, verified []Verified) ([]Choice, error) {
	out := make([]Choice, 0, len(verified))
	for _, v := range verified {
		if v.AdminUserID == uuid.Nil || v.TenantID == uuid.Nil {
			return nil, errors.New("adminauth: tenant choices: incomplete identity")
		}
		var row store.GetAdminForTenantChoiceRow
		err := m.data.WithTenant(ctx, v.TenantID, func(ctx context.Context, tx pgx.Tx) error {
			var e error
			row, e = store.New(tx).GetAdminForTenantChoice(ctx, store.GetAdminForTenantChoiceParams{
				ID: v.AdminUserID, TenantID: v.TenantID,
			})
			return e
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// Disabled (or removed) since the comparison. Skip it: an entry the
			// picker cannot honour is worse than one it does not show.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("adminauth: tenant choices: %w", err)
		}
		out = append(out, Choice{
			AdminUserID: row.ID,
			TenantID:    row.TenantID,
			TenantName:  row.TenantName,
			FullName:    row.FullName,
			Role:        row.Role,
		})
	}
	return out, nil
}

// Session is one stored panel session row. It carries no hash and no token:
// nothing outside this package needs either, and a struct without them cannot
// leak them through %+v or a structured log.
type Session struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	AdminUserID uuid.UUID
	CreatedAt   time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
}

// Issued is the result of Issue: the stored record plus the raw token, which the
// caller must hand straight to Cookies.Set and then drop. Token cannot print
// itself (token.go), so logging an Issued value is safe.
type Issued struct {
	Session Session
	Token   Token
}

// Issue mints a panel session for a VERIFIED identity and stamps last_login_at.
//
// 🔴 THE PARAMETER TYPE IS THE ENFORCEMENT (OBLIGATION 5). It takes Verified, not
// two uuids, so there is no call through which an identity the password did not
// verify can be handed a session. The database cannot make this check — 00011
// says so at length: CreateAdminSession never sees a password, and the pair a
// picker hands back IS a consistent pair of real rows, so the INSERT ... SELECT
// guard, the explicit tenant predicate and the RLS WITH CHECK are all satisfied by
// the attack. The missing bind exists nowhere in SQL; it exists in this signature.
//
// BOTH WRITES SHARE ONE TRANSACTION, and the order is load-bearing in one
// direction only: MarkAdminLoggedIn is the weaker statement (it stamps a column),
// CreateAdminSession is the one that must not happen for a disabled admin. Sharing
// the transaction means a login that cannot create a session does not leave a
// last_login_at claiming it did.
//
// TENANT SCOPING (section 4.5): the whole transaction runs inside
// WithTenant(v.TenantID), both queries carry an explicit tenant_id predicate, and
// CreateAdminSession additionally takes the session's tenant_id FROM THE ADMIN ROW
// rather than from the parameter — so a session cannot be stamped with a tenant
// the admin does not belong to even if this function were called with a mismatched
// pair.
func (m *Manager) Issue(ctx context.Context, v Verified) (Issued, error) {
	if v.AdminUserID == uuid.Nil || v.TenantID == uuid.Nil {
		return Issued{}, errors.New("adminauth: issue: admin and tenant are required")
	}
	t, err := newToken()
	if err != nil {
		return Issued{}, fmt.Errorf("adminauth: issue: %w", err)
	}
	h, err := t.hash(m.hmacKey)
	if err != nil {
		return Issued{}, fmt.Errorf("adminauth: issue: %w", err)
	}

	var row store.CreateAdminSessionRow
	err = m.data.WithTenant(ctx, v.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		var e error
		row, e = q.CreateAdminSession(ctx, store.CreateAdminSessionParams{
			TokenHash:   h,
			AdminUserID: v.AdminUserID,
			TenantID:    v.TenantID,
		})
		if e != nil {
			return e
		}
		// The stamp is best-effort WITHIN the transaction: it uses the same
		// status='active' predicate as the insert, so it cannot fail while the
		// insert succeeds. pgx.ErrNoRows here would mean the two disagreed, which
		// is worth failing the whole login over rather than papering over.
		_, e = q.MarkAdminLoggedIn(ctx, store.MarkAdminLoggedInParams{
			ID: v.AdminUserID, TenantID: v.TenantID,
		})
		return e
	})
	if err != nil {
		return Issued{}, fmt.Errorf("adminauth: issue: %w", err)
	}
	return Issued{
		Session: Session{
			ID:          row.ID,
			TenantID:    row.TenantID,
			AdminUserID: row.AdminUserID,
			CreatedAt:   row.CreatedAt,
			LastUsedAt:  row.LastUsedAt,
			RevokedAt:   row.RevokedAt,
		},
		Token: t,
	}, nil
}

// Resolved is who a panel request is acting as, once its cookie has been resolved
// AND the session proved live.
type Resolved struct {
	SessionID   uuid.UUID
	TenantID    uuid.UUID
	AdminUserID uuid.UUID
	Role        string
	FullName    string
}

// Verify resolves a panel cookie and proves the session is still usable.
//
// TWO STEPS, AND THE SECOND ONE IS THE AUTHORITY. The context-less resolver
// answers "whose session is this and is it revoked"; TouchAdminSession answers
// "may this request proceed", and it is the only statement that also checks the
// ADMIN's status — so a manager disabled thirty seconds ago stops passing here
// without any revocation having been issued (db/queries/admins.sql).
//
// ⚠️ IT WRITES. TouchAdminSession refreshes last_used_at, so every authenticated
// panel request costs one UPDATE. That is a deliberate difference from the tap
// path, where httpx.Identify refuses to Touch precisely so a database write does
// not sit in front of the rate limiter. The panel takes the opposite trade because
// the alternative is worse: the ONLY in-SQL check that an admin is still active
// lives in this statement, and a panel that skipped it would keep serving a
// disabled admin until their cookie expired — and the cookie has no server-side
// expiry (cookie.go). The write only happens for a cookie that ALREADY resolved to
// a real session row, so an unauthenticated flood costs reads, not writes.
//
// EVERY FAILURE COLLAPSES TO ErrNoSession, deliberately: absent, malformed,
// unknown, revoked and disabled-admin all mean "go to the login page" for the
// panel. That is the opposite of internal/session's API TRAP, and the reason the
// two differ is that no ATTENDANCE RECORD is at stake here — section 5 row 4 exists
// because a tap by a deactivated employee must still be written, and there is no
// equivalent obligation for a panel page load. A database FAILURE is not collapsed:
// it comes back as itself, so an outage cannot read as "signed out".
func (m *Manager) Verify(ctx context.Context, t Token) (Resolved, error) {
	h, err := t.hash(m.hmacKey)
	if err != nil {
		if errors.Is(err, errMalformed) {
			// Classified into the sentinel here and nowhere else, and NOT wrapped,
			// so the value can never travel inside an error string.
			return Resolved{}, ErrNoSession
		}
		return Resolved{}, fmt.Errorf("adminauth: verify: %w", err)
	}

	rs, err := m.data.GetAdminSessionByTokenHash(ctx, h)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Resolved{}, ErrNoSession
		}
		return Resolved{}, fmt.Errorf("adminauth: verify: %w", err)
	}
	if rs.RevokedAt != nil {
		// Short-circuit: TouchAdminSession would refuse it anyway (revoked_at IS
		// NULL is one of its predicates), and not issuing a doomed UPDATE keeps a
		// stolen-but-revoked cookie from costing a write per request.
		return Resolved{}, ErrNoSession
	}

	var row store.TouchAdminSessionRow
	err = m.data.WithTenant(ctx, rs.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).TouchAdminSession(ctx, store.TouchAdminSessionParams{
			ID: rs.ID, TenantID: rs.TenantID,
		})
		return e
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Revoked between the two statements, or the admin is no longer active.
		return Resolved{}, ErrNoSession
	}
	if err != nil {
		return Resolved{}, fmt.Errorf("adminauth: verify: %w", err)
	}
	return Resolved{
		SessionID:   row.ID,
		TenantID:    rs.TenantID,
		AdminUserID: row.AdminUserID,
		Role:        row.Role,
		FullName:    row.FullName,
	}, nil
}

// Revoke kills one panel session (sign out). Revocation is a revoked_at stamp,
// never a DELETE: migration 00006 REVOKEs DELETE on admin_sessions from tappa_app
// precisely so the trail survives (section 4.6), and 00011's trigger makes the
// stamp monotonic — a revoked session can never be un-revoked, not even by
// tappa_owner.
//
// The underlying query writes COALESCE(revoked_at, now()), which is what keeps a
// repeat revocation idempotent AND keeps 00011's trigger quiet under concurrency
// (db/queries/admins.sql measures both). An unknown session — wrong id, or an id
// belonging to another tenant — yields ErrNoSession.
func (m *Manager) Revoke(ctx context.Context, tenantID, sessionID uuid.UUID) error {
	if tenantID == uuid.Nil || sessionID == uuid.Nil {
		return errors.New("adminauth: revoke: tenant and session id are required")
	}
	err := m.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).RevokeAdminSession(ctx, store.RevokeAdminSessionParams{
			ID: sessionID, TenantID: tenantID,
		})
		return e
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoSession
	}
	if err != nil {
		return fmt.Errorf("adminauth: revoke: %w", err)
	}
	return nil
}
