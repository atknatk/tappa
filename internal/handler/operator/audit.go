package operator

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/legal"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// THE OPERATOR'S OWN AUDIT LOG (M10 OP-14, phase B; ADR 0020 §4, §5, §9; ADR 0021 §1, OP-14
// note). ADR 0020's risk 1 -- one operator carries the platform's whole power, and the only
// control is the audit -- is a control only once the log is READ; tappa_operator holds no
// SELECT on operator_audit_log, so this screen is the way it is read. GET /operator/audit
// and POST /operator/audit sit in the console's group (routes.go, mount): host gate,
// security headers, flood gate, same-origin gate (a GET a browser labels same-site or
// cross-site is the sign-in redirect), requireOperator, sessionGate.
//
// EVERY VIEW IS ONE TWO-PHASE READ through AuditStore: op_begin_read commits the view's own
// 'read' row (target_scope operator_audit, the page, the filter as a value of a closed set),
// then op_read_audit consumes the ticket and returns a page of the log, newest first. The
// ticket never reaches this package and this file writes no SQL. The view's own row is
// therefore IN the log it shows -- first on an unfiltered first page unless a row dated
// later exists (ADR 0021 limits L1, L2); the screen does not assume it is first.
//
// THE FILTER AND THE PAGE ARE SENT IN THE REQUEST'S BODY, NOT IN ITS URL -- the tenant
// search's one rule (tenants.go), kept although a kind is a value of a closed set and not
// personal data: GET /operator/audit is the FIRST PAGE OF EVERY KIND and reads nothing from
// the URL; POST /operator/audit carries the kind (kind) and the page (page) in its form, and
// every page is another POST (the pager's forms carry both as hidden fields). The POST
// answers 200 with the page -- no redirect, so a reload is one more read, audited as one.
//
// WHAT THE SCREEN CAN SHOW IS WHAT db.OperatorAuditEntry CARRIES: the row's time and kind,
// the session's id (not its hash -- no op_* returns a hash), the operator's and the target
// account's display names, the tenant's id and name, the read's scope and page, and what
// op_read_audit's closed list of detail shapes reads out (a search CLASS, a filter, a legal
// document's slug and byte count). The detail itself is never returned; the type has no
// field for a hash, a ticket, an address or a term (its field list and the view's are
// pinned: TestAuditScreen_NoCredentialFieldReachesThePage).
//
// EACH VALUE IS READ THROUGH A CLOSED MAP AND FAILS CLOSED: a kind, a scope, a search class,
// a filter or a slug this build has no word for is drawn as an unrecognised value -- its raw
// text only when it is a lower-case token (printableToken), in the data face -- and the row
// is never dropped or read as a neighbouring kind. A row whose detail op_read_audit did not
// recognise says "Detail not shown".
//
// THE NAMES ARE OTHER PEOPLE'S TEXT: an operator's display name (written by the platform
// owner) and a tenant's name (written by the tenant) reach the page escaped by templ and
// isolated for bidirectional text; a name with no visible character is replaced by a
// placeholder that names the account or the tenant by its id (namedVisibly). The definer
// reads names with BYPASSRLS, so a pending or a disabled operator's name is shown too (ADR
// 0021 limit L7; the OP-14 B annex).

// AuditStore is the operator database's slice the audit screen needs, declared at the
// consumer (CLAUDE.md §7). *db.OperatorDB implements it by delegating to internal/db's
// OperatorAudit (TestOperatorDB_EveryMethodDelegatesVerbatim), and that type's method set
// is derived from this interface, PlaqueStore, TenantStore, LegalStore and
// operatorauth.Store (TestOperatorDB_IsTheStoreAndNothingMore). sessionHash is
// operatorauth's SessionHash of the request's token -- on ADR 0020 §5's never-log list. The
// signature carries no read ticket: the method is both phases.
type AuditStore interface {
	// OperatorAudit is op_begin_read + op_read_audit: a page of the log, newest first,
	// filtered to q.Kind ("" for every kind). A kind outside db.OperatorAuditKinds is
	// db.ErrOperatorAuditFilterRefused before any statement; a dead session is
	// db.ErrOperatorRefused.
	OperatorAudit(ctx context.Context, sessionHash string, q db.OperatorAuditQuery) ([]db.OperatorAuditEntry, error)
}

// auditPageSize is the screen's page: the tenant list's 50 (the orchestrator's K14-4). The
// database takes a page of up to 200 (00031); the size is this constant, never the
// client's. With db.MaxOperatorAuditPage pages the screen reaches the newest 50 000 rows
// of the kind chosen -- fewer than the database's own bound of 200 000 (ADR 0021 limit L14;
// the OP-14 B annex measures the screen's number).
const auditPageSize = tenantPageSize

// auditLog is GET /operator/audit: the first page of every kind, newest first. It reads
// nothing from the URL (the file's comment).
func (s *Surface) auditLog(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	s.readAudit(w, r, id, hash, "", 1)
}

// filterAudit is POST /operator/audit: a page of the kind the form names.
//
// The refusals, each before any store call and before the read budget is charged, each a
// fixed page that says nothing was read: a body over maxFormBytes (413), an unreadable
// form (400, readForm), a kind that is neither empty nor one of db.OperatorAuditKinds
// (400), a page outside 1..db.MaxOperatorAuditPage (400).
func (s *Surface) filterAudit(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	if !s.readForm(w, r, maxFormBytes, problemAuditFormTooLarge) {
		return
	}
	kind, ok := auditKind(r.PostForm.Get("kind"))
	if !ok {
		s.problem(w, r, http.StatusBadRequest, problemAuditKindRefused)
		return
	}
	page, ok := auditPage(r.PostForm.Get("page"))
	if !ok {
		s.problem(w, r, http.StatusBadRequest, problemAuditPageRefused)
		return
	}
	s.readAudit(w, r, id, hash, kind, page)
}

// readAudit charges the read's unit of the read budget, reads the page and renders it.
func (s *Surface) readAudit(w http.ResponseWriter, r *http.Request, id operatorauth.Identity, hash string,
	kind db.OperatorAuditKind, page int32) {
	if !s.spendRead(w, r, id) {
		return
	}
	rows, err := s.auditStore.OperatorAudit(r.Context(), hash,
		db.OperatorAuditQuery{Kind: kind, Number: page, Size: auditPageSize})
	switch {
	case errors.Is(err, db.ErrOperatorRefused):
		// The session predicate passed in sessionGate and refused now: the session ended in
		// between. The sign-in, as the gate would answer.
		s.redirect(w, pathSignIn)
		return
	case errors.Is(err, db.ErrOperatorAuditFilterRefused):
		// auditKind refuses exactly what internal/db refuses, so this is not reached through
		// the form; it is answered as that refusal would be.
		s.problem(w, r, http.StatusBadRequest, problemAuditKindRefused)
		return
	case err != nil:
		// internal/db's error is the call and a SQLSTATE (operatorErr); the session hash is
		// not an argument of this line. The kind is a value of a closed set, the page a
		// number. A database refusal of the page (22023) is not reached either -- auditPage
		// bounds it first -- and is answered here, as a fault, with the rest.
		s.log.ErrorContext(r.Context(), "operator: the audit log could not be read",
			"kind", string(kind), "page", int(page), "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemAuditUnreadable)
		return
	}
	s.render(w, r, http.StatusOK, operatorpages.AuditLog(auditLogView(rows, kind, page)))
}

// auditKind is the posted kind, or false for one the screen refuses: "" is every kind;
// otherwise it must be a member of db.OperatorAuditKinds as written -- no trimming, no case
// folding (OperatorAuditKind.Known's rule, which is internal/db's own refusal).
func auditKind(raw string) (db.OperatorAuditKind, bool) {
	k := db.OperatorAuditKind(raw)
	if raw == "" || k.Known() {
		return k, true
	}
	return "", false
}

// auditPage is the posted page number -- the tenant pager's rule (tenantPage), bounded by
// db.MaxOperatorAuditPage: absent or empty is the first page; otherwise decimal digits
// only, 1..db.MaxOperatorAuditPage. No sign, no space, no exponent.
func auditPage(raw string) (int32, bool) {
	if raw == "" {
		return 1, true
	}
	if len(raw) > len(strconv.Itoa(db.MaxOperatorAuditPage)) {
		return 0, false
	}
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > db.MaxOperatorAuditPage {
		return 0, false
	}
	return int32(n), true
}

// auditKindWords is the screen's word for each audit kind -- one entry for each member of
// db.OperatorAuditKinds, password_ok among them (its writer is OP-14 phase C; the screen
// only names it). TestAuditWords_NameEveryKindAndNothingElse derives the members from
// internal/db's constants of type OperatorAuditKind and holds this map's keys equal to
// them, so a kind a later migration adds is red there until the screen has a word for it.
// The words state what the row records and nothing more: a row is not a verdict, and no
// word combines two rows into one (the orchestrator's K14-6: no derived "compromised").
var auditKindWords = map[db.OperatorAuditKind]string{
	db.OperatorAuditLoginFailed:      "Sign-in refused: wrong password",
	db.OperatorAuditUnknownEmail:     "Sign-in refused: no active account with that address",
	db.OperatorAuditTOTPFailed:       "Code refused",
	db.OperatorAuditLocked:           "Account locked after wrong codes",
	db.OperatorAuditEnrollmentFailed: "Setup link refused",
	db.OperatorAuditPasswordOK:       "Password accepted, before the code",
	db.OperatorAuditLogin:            "Signed in",
	db.OperatorAuditEnrollment:       "Set up and signed in",
	db.OperatorAuditLogout:           "Signed out",
	db.OperatorAuditRead:             "Read",
	db.OperatorAuditLegalPublish:     "Legal text published",
	// The platform owner's opadmin actions (M10 OP-14 D, migration 00033): the row names the
	// account; auditRow says who acted (ByOwner), so the word says only what happened.
	db.OperatorAuditOperatorCreated:  "Operator account created",
	db.OperatorAuditOperatorMFAReset: "Operator account reset to a new setup link",
	db.OperatorAuditOperatorDisabled: "Operator account disabled",
}

// auditScopeWords is the word for each read kind op_read_audit returns as a row's scope --
// the six kinds of operator_read_tickets_kind_check (00032 added OP-12's tenant_billing).
// The end-to-end test reads that CHECK and op_read_audit's own list from the catalog and
// holds this map's keys equal to both
// (TestE2E_AuditWordsCoverEveryScopeAndSearchClassTheDatabaseReturns), and a test without a
// database holds them equal to the list in op_read_audit's newest definition among the
// migrations (TestAuditWords_NameEveryScopeTheNewestMigrationReturns), so a read kind a
// later migration adds is red in both until the screen has a word for it.
var auditScopeWords = map[string]string{
	"legal_versions": "the legal texts",
	"tenants":        "the tenant list",
	"tenant_detail":  "a tenant's overview",
	"tenant_plaques": "a tenant's plaques",
	"operator_audit": "this audit log",
	"tenant_billing": "a tenant's billing",
}

// auditSearchWords is the word for each search class a tenant-list read records (00029:
// the class of the term, never the term) -- the closed list op_read_audit reads out of a
// 'tenants' row, held equal to that function's own list by the same end-to-end test.
var auditSearchWords = map[string]string{
	"none":    "No search term",
	"text":    "Searched by name",
	"address": "Searched by address",
	"id":      "Searched by tenant id",
}

// auditEveryKind is the filter that names no kind: the option's label, the docket's heading
// and an operator_audit read's recorded filter ("all", 00031).
const (
	auditEveryKind      = "Every kind"
	auditEveryKindValue = "all"
)

// auditLogView builds the screen. Next is offered only after a FULL page and never past
// db.MaxOperatorAuditPage; the filter's options are every kind of db.OperatorAuditKinds in
// the CHECK's order, the chosen one selected.
func auditLogView(rows []db.OperatorAuditEntry, kind db.OperatorAuditKind, page int32) operatorpages.AuditLogView {
	v := operatorpages.AuditLogView{
		Kind:     string(kind),
		Filter:   auditEveryKind,
		Page:     int(page),
		LastPage: int(page) == db.MaxOperatorAuditPage,
		Reach:    thousands(db.MaxOperatorAuditPage * auditPageSize),
		HasNext:  len(rows) >= auditPageSize && page < db.MaxOperatorAuditPage,
		Options:  []operatorpages.AuditKindOption{{Value: "", Label: auditEveryKind, Selected: kind == ""}},
	}
	if kind != "" {
		v.Filter = auditKindWords[kind]
	}
	for _, k := range db.OperatorAuditKinds() {
		v.Options = append(v.Options, operatorpages.AuditKindOption{Value: string(k), Label: auditKindWords[k], Selected: k == kind})
	}
	for _, e := range rows {
		v.Rows = append(v.Rows, auditRow(e))
	}
	return v
}

// auditRow is one entry of the log as the screen says it.
func auditRow(e db.OperatorAuditEntry) operatorpages.AuditRow {
	row := operatorpages.AuditRow{
		At:           utcSecond(e.At),
		Kind:         auditKindWord(e.Kind),
		DetailHidden: !e.DetailRecognised,
		// A row of the platform owner's opadmin SQL (00033) has no actor because no operator
		// acted -- not because it was made before a sign-in. internal/db names the kinds.
		ByOwner: db.OperatorAuditKind(e.Kind).ByOwner(),
	}
	if e.SessionID != nil {
		// The first eight hex digits: enough to tell the sessions of one page apart and to
		// find one in the process log, which names a session by its id (spendSession's WARN).
		row.Session = e.SessionID.String()[:8]
	}
	if e.ActorID != nil {
		row.ActorID, row.Actor = e.ActorID.String(), visibleName(e.ActorName)
	}
	if e.TargetAdminID != nil {
		row.AccountID, row.Account = e.TargetAdminID.String(), visibleName(e.TargetAdminName)
	}
	if e.TargetTenantID != nil {
		row.TenantID = e.TargetTenantID.String()
		if e.TargetTenantName != nil {
			// A tenants row holds this id: its overview is a link (one more read when
			// followed). No row holds it -- an unknown id a read was asked for -- and there
			// is nothing to link.
			row.TenantPath = pathTenants + "/" + row.TenantID
			row.Tenant = visibleName(e.TargetTenantName)
		}
	}
	switch {
	case e.Scope != nil:
		row.Scope = closedWord(*e.Scope, auditScopeWords)
	case e.Kind == string(db.OperatorAuditRead):
		// op_read_audit returns a scope only when it is one of the read kinds it names.
		row.ScopeHidden = true
	}
	if e.PageNumber != nil {
		row.Page = strconv.Itoa(int(*e.PageNumber))
	}
	if e.PageSize != nil {
		row.PageSize = strconv.Itoa(int(*e.PageSize))
	}
	if e.SearchClass != nil {
		row.Search = closedWord(*e.SearchClass, auditSearchWords)
	}
	if e.FilterKind != nil {
		row.Filter = auditFilterWord(*e.FilterKind)
	}
	if e.LegalSlug != nil {
		if legal.Valid(*e.LegalSlug) {
			row.Legal = operatorpages.AuditWord{Text: "/legal/" + *e.LegalSlug}
		} else {
			row.Legal = unknownWord(*e.LegalSlug)
		}
	}
	if e.LegalBytes != nil {
		row.LegalBytes = strconv.Itoa(int(*e.LegalBytes))
	}
	return row
}

// auditKindWord is a row's kind as the screen says it: its word, or -- for a kind this build
// does not name -- an unrecognised value.
func auditKindWord(raw string) operatorpages.AuditWord {
	if w, ok := auditKindWords[db.OperatorAuditKind(raw)]; ok {
		return operatorpages.AuditWord{Text: w}
	}
	return unknownWord(raw)
}

// auditFilterWord is an operator_audit read's recorded filter: every kind, a kind's word, or
// an unrecognised value.
func auditFilterWord(raw string) operatorpages.AuditWord {
	if raw == auditEveryKindValue {
		return operatorpages.AuditWord{Text: auditEveryKind}
	}
	return auditKindWord(raw)
}

// closedWord is raw's word in words, or an unrecognised value.
func closedWord(raw string, words map[string]string) operatorpages.AuditWord {
	if w, ok := words[raw]; ok {
		return operatorpages.AuditWord{Text: w}
	}
	return unknownWord(raw)
}

// unknownWord is a value the screen has no word for: marked unrecognised, with its raw text
// only when that is a lower-case token (printableToken) -- anything else is not printed.
func unknownWord(raw string) operatorpages.AuditWord {
	if printableToken(raw) {
		return operatorpages.AuditWord{Text: raw, Unknown: true}
	}
	return operatorpages.AuditWord{Unknown: true}
}

// printableToken reports whether raw has the shape of the log's own closed values -- a
// lower-case letter, then at most 62 lower-case letters, digits or underscores: the shape of
// operator_audit_log's target_scope CHECK (00026: `^[a-z][a-z_]{0,62}$`, which every kind,
// scope, class and slug today also has) plus the digits. The digits are added so a later
// closed-set member with a number in its name (a versioned kind, say) still prints as itself;
// a digit carries no markup, white space or direction override. A value of another shape --
// markup, white space, a hyphen, a direction override, a long string -- did not come from a
// closed set, and the screen does not print it.
func printableToken(raw string) bool {
	if len(raw) == 0 || len(raw) > 63 || raw[0] < 'a' || raw[0] > 'z' {
		return false
	}
	for i := 1; i < len(raw); i++ {
		c := raw[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

// visibleName is a stored name the screen may print: the name, or "" when there is none or
// it has no visible character (namedVisibly) -- the row then names the account or the
// tenant by its id.
func visibleName(name *string) string {
	if name == nil || !namedVisibly(*name) {
		return ""
	}
	return *name
}

// thousands is n in decimal with a comma between groups of three digits (50,000) -- the
// screen's English.
func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// utcSecond is a time as the audit screen prints it: UTC, to the second (the other screens
// print minutes -- utcStamp; an audit row is read against the rows around it and against
// the process log).
func utcSecond(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") }
