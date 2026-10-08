package operator

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// ONE TENANT'S VAT NUMBER AND ITS RE-CHECK WITH VIES (M10 OP-16, phase B; ADR 0020 §4, §9;
// ADR 0021, OP-16 note and its B annex). GET /operator/tenants/{id}/vat shows the number and
// the verdict on file; POST /operator/tenants/{id}/vat asks the EU VAT register (VIES) about
// that number now and, when it answers, records the answer. Both sit in the console's group
// (routes.go, mount): host gate, security headers, flood gate, same-origin gate (a GET a
// browser labels same-site or cross-site is the sign-in redirect; a POST from another origin
// is 403 before any store call), requireOperator, sessionGate. The overview links the screen;
// the console does not -- the number belongs to one tenant.
//
// 🔴 THE NUMBER IS NEVER THE CLIENT'S (the orchestrator's K16-1 (ii)). The POST reads nothing
// from its body: the number sent to VIES and to the write is the one this request's own
// audited read returned (op_begin_read + op_read_tenant_vat). The form carries no field, and
// a field posted anyway is never parsed -- so VIES is never sent a number somebody typed, and
// the verdict cannot be written against a number the tenant does not hold.
//
// 🔴 NO ANSWER IS NEVER WRITTEN (CLAUDE.md §4.6; K16-2). VATChecker answers in three values,
// and only VATAnswerValid and VATAnswerInvalid reach the write; VATAnswerUnknown -- an outage,
// a refusal, a body that could not be read (internal/domain/signup's rule since OP-16C) -- and
// any value this file does not name are answered 503 with the same screen and a sentence
// saying nothing changed. The write takes a bool, and 00034 refuses a NULL verdict as well:
// the rule has three copies (the checker, this switch, the database).
//
// 🔴 VIES IS ASKED WITH NO DATABASE WORK OPEN (the card's T2). The POST's order: the read --
// both its phases, each its own transaction on the operator pool, returned before the next
// line runs --, then the format check, the VIES budget and the VIES call, then the write in
// its own single statement. Nothing of the database is held while VIES takes up to its
// three seconds (signup.Checker's bound). The write is bound to the committed read by 00034
// itself (ADR 0021, OP-16 note md. 3 (b2)), which this order meets by construction.
//
// 🔴 AN ASK RUNS TO ITS END WHATEVER THE CLIENT DOES -- AND NOTHING OF IT RUNS UNBOUNDED (OP-14
// E's answered; legal.go's precedent for the write). Past the id check, the POST runs on
// answered(r) -- the request's values, none of its cancellation, and none of the router's
// deadline (httpx.RequestTimeout) either. So each of its three database-or-network steps carries
// a bound of its own: the read vatReadTimeout, the VIES call vatAskTimeout (on top of the
// checker's own bound), the write vatWriteTimeout (with context.WithoutCancel spelled out again
// where it is made). Their sum is the longest a POST runs past its session gate, and it fits
// inside the HTTP drain on shutdown (VATRecheckWorstCase; cmd/tappa's
// TestShutdownBudget_TheVATRecheckNestsInsideTheHTTPGrace). A browser that closes the tab while
// VIES is being asked still gets VIES's answer on file, and a client that half-closes its
// connection gets the same status and bytes as one that stayed -- not a 500 from a page
// rendered on a cancelled context (backlog T109's class). The session gate in front still
// follows the client: a request whose client left before it is checked is the sign-in's answer,
// as on every console route. The GET follows the client and its read carries vatReadTimeout too.
//
// 🔴 THE NUMBER IS ON NO LOG LINE (the orchestrator's K16-4; backlog T107). It is a public
// register's key, not a credential, and CLAUDE.md §7's never-log list does not name it; it
// stays out of the process log all the same: no line of this file takes it as an argument,
// internal/db's errors carry a call and a SQLSTATE, and the checker logs nothing. Measured
// on every branch: TestVATRecheck_TheNumberIsOnNoLogLineOrHeader.

// VATStore is the operator database's slice the VAT screen needs, declared at the consumer
// (CLAUDE.md §7). *db.OperatorDB implements it by delegating to internal/db's TenantVAT and
// RecordTenantVATCheck (TestOperatorDB_EveryMethodDelegatesVerbatim), and that type's method set
// is derived from this interface and the other stores (TestOperatorDB_IsTheStoreAndNothingMore).
// sessionHash is operatorauth's SessionHash of the request's token -- on ADR 0020 §5's
// never-log list. Neither method takes a function: a store method that ran the VIES call
// inside its own transaction would need one (TestVATRecheck_VIESIsAskedWithNoStoreCallOpen).
type VATStore interface {
	// TenantVAT is op_begin_read + op_read_tenant_vat: the tenant's name, its VAT number and
	// the verdict on file. An id no tenant has is db.ErrNoSuchTenant -- after the 'read' row
	// naming it committed; a dead session is db.ErrOperatorRefused.
	TenantVAT(ctx context.Context, sessionHash string, tenantID uuid.UUID) (db.TenantVATStatus, error)
	// RecordTenantVATCheck is op_record_vat_check: VIES's verdict on number for the tenant --
	// written only while number is still the tenant's (the database compares it), with the
	// tenant's own audit row before and after, and the operator's row in every case. A dead
	// session is db.ErrOperatorRefused; a call this session's committed read does not bind is
	// SQLSTATE 22023, which this flow never makes (the read comes first).
	RecordTenantVATCheck(ctx context.Context, sessionHash string, tenantID uuid.UUID, number string, valid bool) error
}

// VATAnswer is what VIES said about a number: one of three values, and its zero value is the
// one that writes nothing.
//
// IT IS THIS PACKAGE'S OWN TYPE, not internal/domain/signup's VATStatus (the orchestrator's
// K16-6): cmd/tappa adapts the sign-up wizard's one VIES client to VATChecker, so the
// surface names what it consumes and signup's client stays the process's only outbound HTTP
// client (cmd/tappa's main.go says so where it builds it).
type VATAnswer int

const (
	// VATAnswerUnknown: VIES did not answer -- an outage, a refusal, a body that could not be
	// read, or anything else that is not a verdict. Nothing is written.
	VATAnswerUnknown VATAnswer = iota
	// VATAnswerValid: VIES confirmed the number.
	VATAnswerValid
	// VATAnswerInvalid: VIES processed the request and does not know the number.
	VATAnswerInvalid
)

// VATChecker asks VIES about one normalised, format-valid VAT number. It returns no error: a
// failure is VATAnswerUnknown (signup.Checker.Check's own contract, which cmd/tappa adapts).
type VATChecker interface {
	CheckVAT(ctx context.Context, number string) VATAnswer
}

// THE THREE BOUNDS OF A RE-CHECK, each one step's, each written as a number and pinned as one
// (TestVATRecheck_TheThreeBoundsAreTheirNumbers):
//
//   - vatReadTimeout bounds the read -- both its phases (op_begin_read's INSERT, then
//     op_read_tenant_vat's one row), on the GET and on the detached POST alike. Five seconds is
//     far above two one-row statements on a reachable database; what it bounds is a lock wait or
//     a stalled connection, which would otherwise hold one of the operator pool's connections
//     until TCP gave up (the repository sets no statement, lock or idle timeout for this role --
//     measured on the development database: all three 0 -- ADR 0021's OP-16 B annex). It is not
//     the billing read's 15 s (a SET LOCAL statement bound over a month aggregation) and not the
//     router's 30 s: the three bounds must fit in the HTTP drain together (below).
//   - vatAskTimeout bounds the VIES call: the production checker bounds itself at three seconds
//     (internal/domain/signup's viesTimeout, its client's and its own context's), and this is the
//     surface's own ceiling over whatever VATChecker it was given -- a second above the checker's.
//   - vatWriteTimeout bounds the recording of an answer: one statement through
//     op_record_vat_check -- legalWriteTimeout's reasoning and number, and the same value.
//
// Run in sequence, the three are VATRecheckWorstCase.
const (
	vatReadTimeout  = 5 * time.Second
	vatAskTimeout   = 4 * time.Second
	vatWriteTimeout = 10 * time.Second
)

// VATRecheckWorstCase is the longest a POST /operator/tenants/{id}/vat runs past its session
// gate: the read, the VIES call and the write in sequence, each on its own bound, detached from
// the client. cmd/tappa holds it inside the HTTP drain on shutdown
// (TestShutdownBudget_TheVATRecheckNestsInsideTheHTTPGrace), so a re-check in flight at a deploy
// finishes its write before the process stops serving.
const VATRecheckWorstCase = vatReadTimeout + vatAskTimeout + vatWriteTimeout

// errVATOfAnotherTenant is the screen's refusal of a read that names another tenant than the
// path's. internal/db cannot return one (the statement filters on the id, and readTenantVAT
// refuses a row of another tenant); this is the handler's copy of that rule, because the
// banner -- and the number sent to VIES -- would otherwise be another tenant's.
var errVATOfAnotherTenant = errors.New("operator: the VAT read names another tenant than the path")

// tenantVAT is GET /operator/tenants/{id}/vat: the number and the verdict on file.
//
// The refusals, in order: the session (storeSession); an id that is not the 36-character
// hyphenated form (tenantID) -- 404 with no store call and no unit of the read budget. Then
// readVAT.
func (s *Surface) tenantVAT(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	tenant, ok := tenantID(chi.URLParam(r, "id"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, problemTenantNotAnID)
		return
	}
	st, ok := s.readVAT(w, r, id, hash, tenant)
	if !ok {
		return
	}
	s.renderVAT(w, r, http.StatusOK, st, vatNoticeNone)
}

// recheckVAT is POST /operator/tenants/{id}/vat: ask VIES about the number on file and
// record its answer.
//
// The order, and what each refusal costs (none writes anything):
//
//	the session (storeSession)                        -> the sign-in
//	an id that is not the hyphenated form             -> 404, no store call, no read unit
//	readVAT: the read budget, then the read           -> 429, 404, 503 or the sign-in
//	a number VIES does not take (signup.ValidVATFormat) -> 422, the screen and its sentence;
//	                                                     no VIES call, no VIES unit
//	the VIES budget (spendVIES)                       -> 429, the screen and its sentence;
//	                                                     no VIES call
//	VIES's answer: Unknown (or a value not named)     -> 503, the screen and its sentence
//	the write: a dead session                         -> the sign-in
//	the write: any other error                        -> 503, a fault page
//	the write accepted                                -> 303 to the screen (POST -> 303 -> GET)
//
// The body is not read: the form carries nothing, and the number is the read's. From the read on,
// the request runs on answered(r) (the file's comment).
func (s *Surface) recheckVAT(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	tenant, ok := tenantID(chi.URLParam(r, "id"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, problemTenantNotAnID)
		return
	}
	r = answered(r)
	st, ok := s.readVAT(w, r, id, hash, tenant)
	if !ok {
		return
	}
	if !signup.ValidVATFormat(st.Number) {
		s.renderVAT(w, r, http.StatusUnprocessableEntity, st, vatNoticeFormat)
		return
	}
	if !s.spendVIES(r, id) {
		s.renderVAT(w, r, http.StatusTooManyRequests, st, vatNoticeBudget)
		return
	}
	askCtx, cancelAsk := context.WithTimeout(r.Context(), vatAskTimeout)
	answer := s.vies.CheckVAT(askCtx, st.Number)
	cancelAsk()
	var valid bool
	switch answer {
	case VATAnswerValid:
		valid = true
	case VATAnswerInvalid:
		valid = false
	default:
		// A WARN, because the operator asked and nothing could be recorded; the tenant's id
		// names the case, and the number is not an argument of this line.
		s.log.WarnContext(r.Context(), "operator: VIES did not answer a VAT re-check; nothing was recorded",
			"tenant_id", tenant.String())
		s.renderVAT(w, r, http.StatusServiceUnavailable, st, vatNoticeNoAnswer)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), vatWriteTimeout)
	defer cancel()
	err := s.vatStore.RecordTenantVATCheck(ctx, hash, tenant, st.Number, valid)
	switch {
	case errors.Is(err, db.ErrOperatorRefused):
		// The session predicate passed in sessionGate and refused now: the session ended in
		// between. The sign-in, as the gate would answer; VIES's answer is not recorded.
		s.redirect(w, pathSignIn)
		return
	case err != nil:
		// internal/db's error is the call and a SQLSTATE (operatorErr) -- 22023 among them,
		// which only a write this session's committed read does not bind can draw, and this
		// order never makes; the number and the session hash are not arguments of this line.
		s.log.ErrorContext(r.Context(), "operator: a VAT re-check could not be recorded",
			"tenant_id", tenant.String(), "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemVATNotRecorded)
		return
	}
	s.log.InfoContext(r.Context(), "operator recorded a VAT re-check",
		"tenant_id", tenant.String(), "admin_id", id.AdminID.String(), "answer", vatAnswerWord(valid))
	// The screen the redirect lands on is a fresh read: it shows what the database holds now
	// -- the answer, or the unchanged verdict when the tenant's number changed between the
	// read and the write (the database compared it; the operator's row records the ask).
	s.redirect(w, vatPath(tenant))
}

// readVAT charges the read's unit of the read budget, reads the tenant's number and verdict
// under vatReadTimeout, and answers the read's refusals itself: an id no tenant has is a 404
// after the 'read' row naming it committed; a session the read refuses is the sign-in's 303;
// any other error -- a read that names another tenant included, whose row is dropped before
// the line -- is a 503 whose log line names the tenant's id and not the session's hash or the
// number. It reports false when it has answered.
func (s *Surface) readVAT(w http.ResponseWriter, r *http.Request, id operatorauth.Identity, hash string,
	tenant uuid.UUID) (db.TenantVATStatus, bool) {
	if !s.spendRead(w, r, id) {
		return db.TenantVATStatus{}, false
	}
	readCtx, cancel := context.WithTimeout(r.Context(), vatReadTimeout)
	defer cancel()
	st, err := s.vatStore.TenantVAT(readCtx, hash, tenant)
	if err == nil && st.TenantID != tenant {
		// Another tenant's row: its name and NUMBER are dropped here, so no line below can
		// carry them (the log test's arm for this branch searches both tenants' numbers).
		st, err = db.TenantVATStatus{}, errVATOfAnotherTenant
	}
	switch {
	case errors.Is(err, db.ErrNoSuchTenant):
		s.problem(w, r, http.StatusNotFound, problemNoSuchTenant)
		return db.TenantVATStatus{}, false
	case errors.Is(err, db.ErrOperatorRefused):
		s.redirect(w, pathSignIn)
		return db.TenantVATStatus{}, false
	case err != nil:
		s.log.ErrorContext(r.Context(), "operator: a tenant's VAT number could not be read", "tenant_id", tenant.String(), "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemVATUnreadable)
		return db.TenantVATStatus{}, false
	}
	return st, true
}

// spendVIES charges one unit of the session's VIES budget (viesLimit, surface.go) and reports
// whether it was within it -- the window's first refusal logged at WARN with the session's id.
// It is charged only when a lookup is about to happen (after the format check), like the
// sign-up wizard's outbound budget: a refusal before it costs VIES nothing.
func (s *Surface) spendVIES(r *http.Request, id operatorauth.Identity) bool {
	if n := s.viesAsks.Charge(id.SessionID.String()); n > viesLimit {
		if s.viesAsks.FirstOverLimit(n) {
			s.log.WarnContext(r.Context(), "operator VIES budget reached",
				"session_id", id.SessionID.String(), "limit", viesLimit, "period", viesPeriod.String())
		}
		return false
	}
	return true
}

// vatPath is a tenant's VAT screen, /operator/tenants/<id>/vat -- the overview's link to it,
// its form's action and the redirect after a recorded answer.
func vatPath(tenant uuid.UUID) string {
	return pathTenants + "/" + tenant.String() + "/vat"
}

// vatAnswerWord is a recorded answer as the process log names it.
func vatAnswerWord(valid bool) string {
	if valid {
		return "valid"
	}
	return "invalid"
}

// vatState is the verdict on file in migration 00017's four states. The partition is
// internal/domain/tenant's VATCheck.State, the customer's account screen's
// (TestVATScreen_TheFourStatesAreTheAccountScreensFour holds the two equal on every shape of
// the two columns): no time is never asked; a time and no verdict is asked with no answer; a
// verdict is that verdict, with or without a time.
type vatState int

const (
	vatNotChecked vatState = iota
	vatNoAnswer
	vatConfirmed
	vatNotFound
)

func vatStateOf(st db.TenantVATStatus) vatState {
	switch {
	case st.Verified == nil && st.CheckedAt == nil:
		return vatNotChecked
	case st.Verified == nil:
		return vatNoAnswer
	case *st.Verified:
		return vatConfirmed
	default:
		return vatNotFound
	}
}

// vatWord is how the screen says one state: the chip's word, the sentence beside it and the
// chip's tone.
type vatWord struct {
	label, sentence string
	tone            operatorpages.VATTone
}

// vatWords are the four states' words. THE LABELS ARE THE CUSTOMER'S ACCOUNT SCREEN'S
// (internal/handler/account.go's fillAccountVAT): an operator on the phone with a business
// reads the word the business reads (TestVATScreen_TheFourStatesAreCalledWhatTheAccountScreenCallsThem
// reads that function's literals). The sentences are the operator's: they say what the record
// holds and do not judge the number. "Not checked" names the sign-up cohort the account
// screen names (TestAccount_TheNoAnswerCohortIsCalledTheSameThingOnBothScreens): a sign-up
// that could not reach the register stores no time, so the record cannot tell it from one
// that never asked.
var vatWords = map[vatState]vatWord{
	vatNotChecked: {"Not checked",
		"No answer from VIES is on file: nobody asked, or the sign-up asked and could not reach the register — the record cannot tell them apart.",
		operatorpages.VATToneNotChecked},
	vatNoAnswer: {"No answer",
		"VIES was asked about this number and did not answer — an outage at their end, not a verdict about the business.",
		operatorpages.VATToneNoAnswer},
	vatConfirmed: {"Confirmed",
		"VIES confirmed this number when it last answered.",
		operatorpages.VATToneConfirmed},
	vatNotFound: {"Not found",
		"VIES did not recognise this number when it last answered. Nothing in the product depends on it; an invoice to a number the register does not know is a matter for the business's accountant.",
		operatorpages.VATToneNotFound},
}

// vatNotice is the sentence a POST that changed nothing answers with, above the screen.
type vatNotice int

const (
	vatNoticeNone vatNotice = iota
	vatNoticeNoAnswer
	vatNoticeFormat
	vatNoticeBudget
)

// vatNotices are the three notices' heading and sentence. Each says that nothing changed,
// and what to do next (skill tappa-brand: a message does not blame). For a number VIES does not
// take, the next step is the platform owner's: the business cannot change its own number
// (M7-05 -- the account screen shows it read-only and says "tell us"; tappa_app holds no UPDATE
// on it, 00024), and neither can this surface (00034's definer holds no UPDATE on vat_number --
// TestOperator00034_TheDefinerWritesTheVerdictAndNotTheNumber).
var vatNotices = map[vatNotice][2]string{
	vatNoticeNoAnswer: {"VIES did not answer",
		"The EU VAT register (VIES) did not answer. Nothing was changed; the result below still stands. Try again in a few minutes."},
	vatNoticeFormat: {"This number cannot be sent to VIES",
		"It is not in the format VIES accepts for its country, so it was not sent. Nothing was changed. " +
			"The number can only be corrected by the platform owner, in the database: neither the business nor this screen can change it."},
	vatNoticeBudget: {"Too many VAT re-checks",
		"This session has asked VIES ten times in ten minutes. Wait ten minutes, then try again. Nothing was changed."},
}

// renderVAT renders the screen for st with notice. A tenant whose stored name shows nothing is
// named by its id (tenantBanner).
func (s *Surface) renderVAT(w http.ResponseWriter, r *http.Request, status int, st db.TenantVATStatus, notice vatNotice) {
	name, err := tenantBanner(st.TenantID, st.TenantName)
	if err != nil {
		// Not reached: tenantBanner names a tenant without a visible name by its id. Were it
		// reached, the zero name makes TenantScreen refuse and render answers a plain 500 --
		// this line says why first.
		s.log.ErrorContext(r.Context(), "operator: a tenant could not be named on its VAT screen", "tenant_id", st.TenantID.String(), "err", err)
	}
	word := vatWords[vatStateOf(st)]
	v := operatorpages.TenantVATView{
		Name:         name,
		ID:           st.TenantID.String(),
		OverviewPath: pathTenants + "/" + st.TenantID.String(),
		VATPath:      vatPath(st.TenantID),
		Number:       st.Number,
		Label:        word.label,
		Sentence:     word.sentence,
		Tone:         word.tone,
		Askable:      signup.ValidVATFormat(st.Number),
	}
	if st.CheckedAt != nil {
		v.AskedAt = utcStamp(*st.CheckedAt)
		// Beside a verdict the time is VIES's last answer, not its last ask: an ask it did not
		// answer writes nothing (recheckVAT), so a newer one may exist that the record cannot
		// show -- the 503 screen after it says "VIES did not answer" above this very line.
		v.Answered = st.Verified != nil
	}
	if n, ok := vatNotices[notice]; ok {
		v.NoticeHeading, v.Notice = n[0], n[1]
	}
	s.render(w, r, status, operatorpages.TenantVAT(v))
}
