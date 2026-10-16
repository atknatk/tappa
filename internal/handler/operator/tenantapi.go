package operator

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// THE TENANT'S INTEGRATION API, ON ITS OVERVIEW (M11 API-13a; K-22 -- ADR 0028 §2; migrations 00039,
// 00042's enable, 00057). The platform operator opens and closes a business's API here, and nowhere
// else: op_enable_tenant_api and op_disable_tenant_api are the only writers of tenants.api_enabled_at
// and tenants.api_code. Two routes, both in the console's group (routes.go, mount) -- host gate,
// security headers, flood gate, same-origin gate (a POST from another origin is 403 before any store
// call), requireOperator, sessionGate -- beside the overview that shows the state:
//
//	GET  /operator/tenants/{id}            the overview (tenants.go) -- its read, op_read_tenant_detail,
//	                                       returns api_enabled_at and api_code since 00057, and the
//	                                       sentence of the last write (below)
//	POST /operator/tenants/{id}/integration  open the API under the posted code, or change the code of
//	                                       an open one (op_enable_tenant_api)
//	POST /operator/tenants/{id}/integration/close
//	                                       close it (op_disable_tenant_api); the code is kept
//
// 🔴 A WRITE'S ANSWER SAYS NOTHING ABOUT WHAT IT FOUND (ADR 0021 §2 v 7, B14) -- holdings.go's rule.
// The enable writes only when the business exists and, in a holding, no live member (or one inside
// its 60-second exit window) carries the code; it answers the same either way. So an accepted write
// is a 303 to the overview with what was ASKED in the query (done, and for an open the code), and the
// overview's own audited read says what HAPPENED (apiResultNotice). The code in the query is used only
// to compare it with the read's; the page prints the read's code, never the URL's. An API code is a
// public label (00039's header: the spec's `company`), not personal data, so it may sit in a URL.
//
// EVERY CALL LEAVES ITS TRAIL in the database, not here: the operator's 'tenant_api_enabled' /
// 'tenant_api_disabled' row on every accepted call (operator_audit_log, which also holds the
// overview's 'read' rows -- CLAUDE.md §4.5's "every access leaves a trace"), and the tenant's own
// 'tenant.api_enabled' / 'tenant.api_disabled' row when the tenant was written.
//
// K-19 (ADR 0028 §6, the API-10 fix round's hand-over): the operator surface is mounted by the SAME
// process as the webhook worker (cmd/tappa's run builds both), so an accepted open puts the business
// into the worker's set (NoticeWorkTo; WorkNoticer below) and the deliveries that waited while the
// API was closed go now -- not at delivery.GateRecheck (15 minutes), and not only at the business's
// next tap after a restart. A notice for a business the database did not open is harmless: the
// worker reads the gate and finds it closed.
//
// WHAT IS LOGGED: the business's id, the operator's id and internal/db's error (a call and a
// SQLSTATE). Never the session token or its hash (ADR 0020 §5); the code is not logged either -- it
// is public, but it has no reason to be in the process log.

// WorkNoticer is K-19's set of businesses with work (internal/webhook's Worker), declared at the
// consumer (CLAUDE.md §7).
type WorkNoticer interface {
	Notice(tenant uuid.UUID)
}

// NoticeWorkTo makes an accepted API open put its business into n. cmd/tappa calls it once, after
// the worker is built; a surface that was never handed one (Off, Unavailable, the tests that do not
// ask) notices nothing.
func (s *Surface) NoticeWorkTo(n WorkNoticer) {
	if s != nil {
		s.work = n
	}
}

// The two bounds (holdings.go's, for the same reasons): a write -- one statement through an op_* --
// detached from the client; a read of the overview on a POST whose refusal re-renders it (that POST
// runs on answered(r), so its read needs a bound of its own).
const (
	tenantAPIWriteTimeout = 10 * time.Second
	tenantAPIReadTimeout  = 5 * time.Second
)

// apiCodeRule is db.APICodePattern -- op_enable_tenant_api's own rule and 00038's CHECK -- applied at
// the handler's boundary (CLAUDE.md §7), so a code the database would refuse with 22023 is a sentence
// on the overview, not a fault page.
var apiCodeRule = regexp.MustCompile(db.APICodePattern)

// apiCode is the posted code as op_enable_tenant_api is sent it, or false: its ends trimmed of white
// space, then the database's rule exactly -- no case folding (a lower-case code is refused and the
// sentence says capitals; the code is what every punch carries as its `company`, so it is typed as
// meant, not guessed).
func apiCode(raw string) (string, bool) {
	code := strings.TrimSpace(raw)
	if !apiCodeRule.MatchString(code) {
		return "", false
	}
	return code, true
}

// apiDone is what a write asked for, as the 303 after it names it.
type apiDone string

const (
	apiOpened apiDone = "api-opened"
	apiClosed apiDone = "api-closed"
)

// apiResult is the query of the 303, parsed. Its zero value asks nothing.
type apiResult struct {
	done apiDone
	code string
}

// apiResultOf parses the overview's query: a word that is not one of the two, or an open without a
// code of the rule, is the zero result -- no sentence, and nothing of the query on the page.
func apiResultOf(done, code string) apiResult {
	switch apiDone(done) {
	case apiOpened:
		c, ok := apiCode(code)
		if !ok || c != code {
			return apiResult{}
		}
		return apiResult{done: apiOpened, code: c}
	case apiClosed:
		return apiResult{done: apiClosed}
	}
	return apiResult{}
}

// apiResultPath is the overview with what a write asked for -- the 303's target. Every value is a word
// of apiDone, a uuid's canonical text and a code that passed apiCode, so nothing a client sent reaches
// the Location but values the handler parsed.
func apiResultPath(tenant uuid.UUID, done apiDone, code string) string {
	p := pathTenants + "/" + tenant.String() + "?done=" + string(done)
	if done == apiOpened {
		p += "&api_code=" + code
	}
	return p
}

// tenantAPIPath is the open form's action, /operator/tenants/<id>/integration; tenantAPIClosePath the close
// button's, /operator/tenants/<id>/integration/close.
func tenantAPIPath(tenant uuid.UUID) string {
	return pathTenants + "/" + tenant.String() + "/integration"
}
func tenantAPIClosePath(tenant uuid.UUID) string { return tenantAPIPath(tenant) + "/close" }

// apiNotice is the sentence on the overview: a write's outcome as the read shows it, or a refused
// code. apiNoticeNone draws nothing.
type apiNotice int

const (
	apiNoticeNone apiNotice = iota
	apiNoticeOpened
	apiNoticeNotOpened
	apiNoticeClosed
	apiNoticeNotClosed
	apiNoticeCode
)

// apiSentence is one notice's tone, heading and sentence; rules says the enable's rules follow it.
type apiSentence struct {
	tone              operatorpages.HoldingNoticeTone
	heading, sentence string
	rules             bool
}

// apiSentences are the notices' words (skill tappa-brand: say what happened and what to do). Each
// outcome is a fact the read showed: "Opened." only when the read shows the API open under the code
// that was asked for. Pinned word for word: TestOperatorAPI_TheResultIsReadNotPosted.
var apiSentences = map[apiNotice]apiSentence{
	apiNoticeOpened: {operatorpages.HoldingNoticeOK, "Opened.",
		"The integration API is open for this business under the code below. Its keys work from the next request.", false},
	apiNoticeNotOpened: {operatorpages.HoldingNoticeWarn, "Not opened — nothing changed.",
		"The database opens the API under a code only when every rule below holds. The API section shows the business as it is now.", true},
	apiNoticeClosed: {operatorpages.HoldingNoticeOK, "Closed.",
		"The integration API is closed for this business: its keys are refused from the next request and no webhook is sent. The code is kept.", false},
	apiNoticeNotClosed: {operatorpages.HoldingNoticeWarn, "It is still open.",
		"The API section shows the business as the database holds it now. Close it again from there if it should be closed.", false},
	apiNoticeCode: {operatorpages.HoldingNoticeWarn, "That code cannot be used",
		"An API code is a capital letter followed by 1 to 15 capital letters or digits, such as KF or KM. Nothing was changed.", false},
}

// apiEnableRules are op_enable_tenant_api's rules (00039, 00042), in the order the overview shows the
// facts that decide them.
var apiEnableRules = []string{
	"The code is a capital letter followed by 1 to 15 capital letters or digits.",
	"No other business in this business's holding has the code, and none that left it less than 60 seconds ago.",
	"The business exists.",
}

// apiResultNotice is the sentence the overview's own read says about a write: never what the POST was
// answered. Like holdingResultNotice it says what IS, not who did it -- a form posted twice gets the
// same sentence (a stated limit).
func apiResultNotice(res apiResult, o db.TenantOverview) apiNotice {
	open := o.APIEnabledAt != nil
	switch res.done {
	case apiOpened:
		if open && o.APICode != nil && *o.APICode == res.code {
			return apiNoticeOpened
		}
		return apiNoticeNotOpened
	case apiClosed:
		if !open {
			return apiNoticeClosed
		}
		return apiNoticeNotClosed
	}
	return apiNoticeNone
}

// enableTenantAPI is POST /operator/tenants/{id}/integration: ask the database to open the business's API
// under the posted code (api_code), or to change the code of an open one.
//
//	the session (storeSession)                 -> the sign-in
//	an id that is not the hyphenated form      -> 404, no store call
//	a body over maxFormBytes / unreadable      -> 413 / 400, no store call
//	a code the boundary refuses (apiCode)      -> 422, the overview with the code's sentence: one
//	                                              read (and a unit of the read budget), no write
//	the write: a dead session                  -> the sign-in
//	the write: any other error                 -> 503, a fault page that sends the operator to the
//	                                              tenants (the overview shows what the database holds)
//	the write accepted                         -> K-19's notice, then 303 to the overview, done and
//	                                              the code -- and the overview's read says what happened
func (s *Surface) enableTenantAPI(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	tenant, ok := tenantID(chi.URLParam(r, "id"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, problemTenantNotAnID)
		return
	}
	if !s.readForm(w, r, maxFormBytes, problemTenantAPIFormTooLarge) {
		return
	}
	r = answered(r)
	code, ok := apiCode(r.PostForm.Get("api_code"))
	if !ok {
		s.refuseTenantAPIForm(w, r, id, hash, tenant)
		return
	}
	if !s.writeTenantAPI(w, r, tenant, func(ctx context.Context) error {
		return s.tenantStore.EnableTenantAPI(ctx, hash, tenant, code)
	}) {
		return
	}
	if s.work != nil {
		s.work.Notice(tenant)
	}
	s.log.InfoContext(r.Context(), "operator asked to open a business's integration API",
		"tenant_id", tenant.String(), "admin_id", id.AdminID.String())
	s.redirect(w, apiResultPath(tenant, apiOpened, code))
}

// disableTenantAPI is POST /operator/tenants/{id}/integration/close: ask the database to close the business's
// API. The id is the path's (the overview's Close button posts to it); the body is not read.
//
//	the session (storeSession)                 -> the sign-in
//	an id that is not the hyphenated form      -> 404, no store call
//	the write: a dead session                  -> the sign-in
//	the write: any other error                 -> 503, a fault page
//	the write accepted                         -> 303 to the overview, done=api-closed
func (s *Surface) disableTenantAPI(w http.ResponseWriter, r *http.Request) {
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
	if !s.writeTenantAPI(w, r, tenant, func(ctx context.Context) error {
		return s.tenantStore.DisableTenantAPI(ctx, hash, tenant)
	}) {
		return
	}
	s.log.InfoContext(r.Context(), "operator asked to close a business's integration API",
		"tenant_id", tenant.String(), "admin_id", id.AdminID.String())
	s.redirect(w, apiResultPath(tenant, apiClosed, ""))
}

// writeTenantAPI runs one of the two writes under tenantAPIWriteTimeout, detached from the client
// (a client that leaves still gets the write, as holdings.go's writeMembership), and answers its two
// failures itself. It reports false when it has answered.
func (s *Surface) writeTenantAPI(w http.ResponseWriter, r *http.Request, tenant uuid.UUID, write func(context.Context) error) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), tenantAPIWriteTimeout)
	defer cancel()
	err := write(ctx)
	switch {
	case errors.Is(err, db.ErrOperatorRefused):
		s.redirect(w, pathSignIn)
		return false
	case err != nil:
		s.log.ErrorContext(r.Context(), "operator: a change of a business's integration API was not confirmed",
			"tenant_id", tenant.String(), "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemTenantAPINotConfirmed)
		return false
	}
	return true
}

// refuseTenantAPIForm answers a code the boundary refused: 422 and the overview with the code's
// sentence. It is a read -- one unit of the read budget, the overview's own refusals -- and it writes
// nothing.
func (s *Surface) refuseTenantAPIForm(w http.ResponseWriter, r *http.Request, id operatorauth.Identity, hash string, tenant uuid.UUID) {
	if !s.spendRead(w, r, id) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), tenantAPIReadTimeout)
	defer cancel()
	o, ok := s.readOverview(w, r.WithContext(ctx), hash, tenant)
	if !ok {
		return
	}
	s.renderOverview(w, r, http.StatusUnprocessableEntity, o, apiNoticeCode)
}
