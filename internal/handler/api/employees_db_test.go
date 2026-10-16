package api

// employees_db_test.go -- the employee endpoints (M11 API-6; ADR 0028 §8, §9, §10, §12; K-14 ...
// K-18, K-23 ... K-25) over the REAL chain, runner, checkpoint, employee domain and database, with
// the REAL invitation queue worker (internal/invite) behind a fake relay -- no e-mail leaves the
// process.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/invite"
)

// relay is a fake invite.MailSink: it records what it was handed.
type relay struct {
	mu  sync.Mutex
	got []invite.Delivery
}

func (s *relay) SendInvitation(_ context.Context, d invite.Delivery) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, d)
	return "<msg@relay.example.test>", nil
}

func (s *relay) RecipientCapped(uuid.UUID, string) bool { return false }

func (s *relay) Refusing() bool { return false }

func (s *relay) sent() []invite.Delivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]invite.Delivery(nil), s.got...)
}

// queue builds the invitation queue worker over the rig's database, with its own code key.
func (r *dbRig) queue() (*invite.Queue, *relay, *invite.Manager) {
	r.t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		r.t.Fatal(err)
	}
	m, err := invite.New(r.d, &config.Config{InviteHMACKey: key, BaseURL: "https://taptime.mt"})
	if err != nil {
		r.t.Fatal(err)
	}
	sink := &relay{}
	q, err := invite.NewQueue(m, sink, r.trail)
	if err != nil {
		r.t.Fatal(err)
	}
	return q, sink, m
}

func runQueue(t *testing.T, q *invite.Queue, tenant uuid.UUID) {
	t.Helper()
	if err := q.RunFor(context.Background(), tenant); err != nil {
		if _, later := err.(*invite.Later); !later {
			t.Fatalf("the queue: %v", err)
		}
	}
}

func (r *dbRig) putEmp(raw, ref, body string, hdr ...string) *httptest.ResponseRecorder {
	return r.do(http.MethodPut, "/v1/employees/"+ref, raw, body, hdr...)
}

func empBody(first, last string, email *string, active bool) string {
	return jsonBody(map[string]any{"firstName": first, "lastName": last, "email": email, "active": active})
}

// jsonBody marshals a request body (a map, so a test can send any key set).
func jsonBody(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func uniqueEmail(label string) string {
	return label + "." + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:10]) + "@API6.example.test"
}

// empOf decodes an employee answer, refusing any key the contract does not name.
func empOf(t *testing.T, w *httptest.ResponseRecorder) employeeBody {
	t.Helper()
	var b employeeBody
	dec := json.NewDecoder(strings.NewReader(w.Body.String()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		t.Fatalf("status %d: the answer %q is not an employee: %v", w.Code, w.Body.String(), err)
	}
	return b
}

func idOfEmp(t *testing.T, b employeeBody) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(strings.TrimPrefix(b.ID, "emp_"))
	if err != nil || !strings.HasPrefix(b.ID, "emp_") {
		t.Fatalf("the id %q is not emp_<uuid>", b.ID)
	}
	return id
}

func (r *dbRig) ownerCount(q string, args ...any) (n int) {
	r.t.Helper()
	if err := r.owner.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

// TestEmployeesPut_CreatesQueuesAndNoticesAfterTheCommit (ADR 0028 §9, K-14, K-16, K-17, K-19,
// K-23): a new ref is 201 with emp_<uuid>, the names, active, not activated, hasEmail and
// activationEmail "queued"; the row has NO home venue (K-14), its status is 'invited'; one request
// waits in invite_requests and NO invitation exists yet (the code is minted at send time); the
// trail row is employee.added by the KEY with via "api"; K-19 was noticed after the write. The
// queue then sends it: activationEmail "sent". The same PUT again is 200, nothing written. A PUT
// with "email": null opens a row with no request.
func TestEmployeesPut_CreatesQueuesAndNoticesAfterTheCommit(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, keyID := r.key(biz, owner)
	q, sink, _ := r.queue()
	ref, email := newRef("EMP"), uniqueEmail("new")
	before := len(r.work.got())
	w := r.putEmp(raw, ref, empBody("  Maria ", "Borg", &email, true))
	if w.Code != http.StatusCreated {
		t.Fatalf("a new ref: %d %s", w.Code, w.Body.String())
	}
	b := empOf(t, w)
	id := idOfEmp(t, b)
	if b.ExternalRef == nil || *b.ExternalRef != ref || b.FirstName == nil || *b.FirstName != "Maria" || b.FullName != "Maria Borg" ||
		!b.Active || b.Activated || !b.HasEmail || b.ActivationEmail.Status != "queued" || b.ActivationEmail.At == nil {
		t.Fatalf("the answer %+v", b)
	}
	var loc *uuid.UUID
	var status string
	if err := r.owner.QueryRow(context.Background(), `SELECT location_id, status FROM employees WHERE tenant_id = $1 AND id = $2`, biz, id).Scan(&loc, &status); err != nil || loc != nil || status != "invited" {
		t.Fatalf("the row: location %v status %q %v; want no home venue and invited", loc, status, err)
	}
	if r.ownerCount(`SELECT count(*) FROM invite_requests WHERE tenant_id = $1 AND employee_id = $2`, biz, id) != 1 ||
		r.ownerCount(`SELECT count(*) FROM employee_invites WHERE tenant_id = $1 AND employee_id = $2`, biz, id) != 0 {
		t.Fatal("want one queued request and no invitation yet")
	}
	if n := r.ownerCount(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2 AND action = 'employee.added' AND actor_id = $3 AND detail->>'via' = 'api'`, biz, id.String(), keyID); n != 1 {
		t.Fatalf("%d employee.added rows by the key, want 1", n)
	}
	if got := r.work.got(); len(got) < before+2 || got[len(got)-1] != biz {
		t.Fatalf("K-19 notices %v: want the chain's and one more after the commit", got)
	}
	runQueue(t, q, biz)
	if len(sink.sent()) != 1 {
		t.Fatalf("the queue sent %d e-mails, want 1", len(sink.sent()))
	}
	w = r.do(http.MethodGet, "/v1/employees/"+ref, raw, "")
	if g := empOf(t, w); g.ActivationEmail.Status != "sent" || g.ActivationEmail.At == nil {
		t.Fatalf("after the send: %+v", g.ActivationEmail)
	}
	w = r.putEmp(raw, ref, empBody("Maria", "Borg", &email, true))
	if w.Code != http.StatusOK || idOfEmp(t, empOf(t, w)) != id {
		t.Fatalf("the same PUT again: %d %s", w.Code, w.Body.String())
	}
	if n := r.ownerCount(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2`, biz, id.String()); n != 2 {
		// employee.added, invite.code_emailed, and nothing from the repeat.
		t.Fatalf("%d trail rows, want 2", n)
	}
	none := newRef("EMP")
	w = r.putEmp(raw, none, empBody("Joe", "Camilleri", nil, true))
	if w.Code != http.StatusCreated || empOf(t, w).HasEmail || empOf(t, w).ActivationEmail.Status != "none" {
		t.Fatalf("no address: %d %s", w.Code, w.Body.String())
	}
}

// TestEmployeesPut_ActiveEmailElsewhereIs409 (ADR 0028 §9, K-24: "409 email_unavailable — yeni satır
// açılmaz, davet gitmez"): an address another LIVE employee of the business holds -- in any case of
// letters -- is 409 for a new ref and for a change: no row is opened, no request queued, no
// invitation minted, no trail row; the address is never echoed. A DEACTIVATED employee's address is
// free (K-24): the same PUT then opens the row. Another business holding the address is no
// conflict (never searched).
func TestEmployeesPut_ActiveEmailElsewhereIs409(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	email := uniqueEmail("taken")
	holder := newRef("HLD")
	if w := r.putEmp(raw, holder, empBody("Ann", "Holder", &email, true)); w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	// K-18 binds an unbound live employee with the address; give the holder its ref so it is not
	// "linkable" -- the conflict is with a bound, live person.
	ref := newRef("NEW")
	beforeRows := r.ownerCount(`SELECT count(*) FROM employees WHERE tenant_id = $1`, biz)
	beforeTrail := r.ownerCount(`SELECT count(*) FROM audit_log WHERE tenant_id = $1`, biz)
	upper := strings.ToUpper(email)
	w := r.putEmp(raw, ref, empBody("Bob", "Other", &upper, true))
	if w.Code != http.StatusConflict || problemOf(t, w).Code != CodeEmailUnavailable || strings.Contains(strings.ToLower(w.Body.String()), strings.ToLower(email)) {
		t.Fatalf("a held address: %d %s, want 409 email_unavailable without the address", w.Code, w.Body.String())
	}
	if r.ownerCount(`SELECT count(*) FROM employees WHERE tenant_id = $1`, biz) != beforeRows ||
		r.ownerCount(`SELECT count(*) FROM audit_log WHERE tenant_id = $1`, biz) != beforeTrail ||
		r.ownerCount(`SELECT count(*) FROM invite_requests WHERE tenant_id = $1`, biz) != 1 {
		t.Fatal("the 409 left a row, a trail row or a request behind")
	}
	// A change to the held address: 409, nothing changed.
	other := newRef("OTH")
	otherEmail := uniqueEmail("other")
	if w := r.putEmp(raw, other, empBody("Bob", "Other", &otherEmail, true)); w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	if w := r.putEmp(raw, other, empBody("Bob", "Other", &email, true)); w.Code != 409 {
		t.Fatalf("a change to a held address: %d %s", w.Code, w.Body.String())
	}
	// The holder deactivated: the address is free.
	if w := r.do(http.MethodDelete, "/v1/employees/"+holder, raw, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := r.putEmp(raw, ref, empBody("Bob", "Other", &email, true)); w.Code != 201 {
		t.Fatalf("a deactivated person's address: %d %s, want 201", w.Code, w.Body.String())
	}
	// Another business holding an address is no conflict.
	c, cOwner := r.business(true)
	cRaw, _ := r.key(c, cOwner)
	if w := r.putEmp(cRaw, newRef("CEE"), empBody("Bob", "Other", &email, true)); w.Code != 201 {
		t.Fatalf("another business's address: %d %s", w.Code, w.Body.String())
	}
}

// TestEmployeesPut_InactiveToActiveOpensANewRow (ADR 0028 §9, ADR 0010, plan §2 item 9): active:false
// deactivates (200, active false); active:true on that ref opens a NEW row -- 201, a new emp_ id,
// the same externalRef -- and the old row stays deactivated with its history; GET by the ref then
// answers the new row; a PUT {active:false} for a ref never held is 404, and for a ref whose only
// row is deactivated, the row as it is (200).
func TestEmployeesPut_InactiveToActiveOpensANewRow(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	ref, email := newRef("RH"), uniqueEmail("rehire")
	first := idOfEmp(t, empOf(t, r.putEmp(raw, ref, empBody("Maria", "Borg", &email, true))))
	w := r.putEmp(raw, ref, empBody("Maria", "Borg", &email, false))
	if w.Code != 200 || empOf(t, w).Active || idOfEmp(t, empOf(t, w)) != first {
		t.Fatalf("active:false: %d %s", w.Code, w.Body.String())
	}
	if w := r.putEmp(raw, ref, empBody("Maria", "Borg", &email, false)); w.Code != 200 || idOfEmp(t, empOf(t, w)) != first {
		t.Fatalf("active:false again: %d %s, want the deactivated row as it is", w.Code, w.Body.String())
	}
	w = r.putEmp(raw, ref, empBody("Maria", "Borg", &email, true))
	if w.Code != http.StatusCreated {
		t.Fatalf("active:true after a deactivation: %d %s, want 201", w.Code, w.Body.String())
	}
	b := empOf(t, w)
	second := idOfEmp(t, b)
	if second == first || b.ExternalRef == nil || *b.ExternalRef != ref || !b.Active {
		t.Fatalf("the re-hire %+v: want a new id under the same ref", b)
	}
	var oldStatus string
	if err := r.owner.QueryRow(context.Background(), `SELECT status FROM employees WHERE id = $1`, first).Scan(&oldStatus); err != nil || oldStatus != "deactivated" {
		t.Fatalf("the old row: %q %v", oldStatus, err)
	}
	if n := r.ownerCount(`SELECT count(*) FROM employees WHERE tenant_id = $1 AND external_ref = $2`, biz, ref); n != 2 {
		t.Fatalf("%d rows under the ref, want 2", n)
	}
	if g := empOf(t, r.do(http.MethodGet, "/v1/employees/"+ref, raw, "")); idOfEmp(t, g) != second {
		t.Fatalf("GET by the ref answers %s, want the live row", g.ID)
	}
	if w := r.putEmp(raw, newRef("NEVER"), empBody("No", "Body", nil, false)); w.Code != 404 || problemOf(t, w).Code != CodeNotFound {
		t.Fatalf("active:false for a ref never held: %d %s", w.Code, w.Body.String())
	}
}

// TestResendActivation_LoopNeverKillsTheLiveInvite (ADR 0028 §9, K-17; plan §2 item 12: "resend
// istek anında hiçbir daveti öldürmez"): with a request waiting, five resends are 202 and change
// nothing; once the queue sent the invitation, five more resends are 202, queue nothing, mint
// nothing, and the invitation stays spendable (never retired at request time). An hour later (the
// invitation's created_at moved back) a resend queues a request -- and the live invitation is STILL
// spendable until the worker mints the new one at send time.
//
// MUTANT (measured; ADR 0028 "API-6 uygulama notu"): a resend that retires the invitations at
// request time -> the live link dies on the first resend, red.
func TestResendActivation_LoopNeverKillsTheLiveInvite(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	q, sink, m := r.queue()
	ref, email := newRef("RS"), uniqueEmail("resend")
	id := idOfEmp(t, empOf(t, r.putEmp(raw, ref, empBody("Maria", "Borg", &email, true))))
	resend := func() employeeBody {
		t.Helper()
		w := r.do(http.MethodPost, "/v1/employees/"+ref+"/resend-activation", raw, "")
		if w.Code != http.StatusAccepted {
			t.Fatalf("resend: %d %s, want 202", w.Code, w.Body.String())
		}
		return empOf(t, w)
	}
	for range 5 {
		if b := resend(); b.ActivationEmail.Status != "queued" {
			t.Fatalf("a resend while a request waits: %+v", b.ActivationEmail)
		}
	}
	if n := r.ownerCount(`SELECT count(*) FROM invite_requests WHERE tenant_id = $1 AND employee_id = $2`, biz, id); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
	runQueue(t, q, biz)
	if len(sink.sent()) != 1 {
		t.Fatal("the queue did not send")
	}
	code := sink.sent()[0].ActivationURL
	code = code[strings.Index(code, "code=")+len("code="):]
	for range 5 {
		if b := resend(); b.ActivationEmail.Status != "sent" {
			t.Fatalf("a resend after the send: %+v", b.ActivationEmail)
		}
		runQueue(t, q, biz)
	}
	if len(sink.sent()) != 1 || r.ownerCount(`SELECT count(*) FROM employee_invites WHERE tenant_id = $1 AND employee_id = $2`, biz, id) != 1 {
		t.Fatal("a resend within the hour minted or sent again")
	}
	if _, err := m.Lookup(context.Background(), invite.ParseCode(code)); err != nil {
		t.Fatalf("the live invitation after the resend loop: %v, want spendable", err)
	}
	if n := r.ownerCount(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2 AND action = 'invite.requested'`, biz, id.String()); n != 0 {
		t.Fatalf("%d invite.requested rows for no-op resends, want 0", n)
	}
	// An hour later.
	if _, err := r.owner.Exec(context.Background(), `UPDATE employee_invites SET created_at = now() - interval '61 minutes' WHERE tenant_id = $1 AND employee_id = $2`, biz, id); err != nil {
		t.Fatal(err)
	}
	if b := resend(); b.ActivationEmail.Status != "queued" {
		t.Fatalf("a resend an hour later: %+v, want queued", b.ActivationEmail)
	}
	if _, err := m.Lookup(context.Background(), invite.ParseCode(code)); err != nil {
		t.Fatalf("the live invitation right after the resend that queued: %v, want still spendable (retired at send time only)", err)
	}
	runQueue(t, q, biz)
	if len(sink.sent()) != 2 {
		t.Fatal("the queued resend was not sent")
	}
	if _, err := m.Lookup(context.Background(), invite.ParseCode(code)); err == nil {
		t.Fatal("the old invitation survived the new one's mint")
	}
}

// TestResendActivation_ActivatedIs409 (ADR 0028 §9: "aktive çalışana 409 already_activated"): an
// activated employee's resend is 409 already_activated and queues nothing; an unknown ref is 404; an
// employee with no address is 422 (email required).
func TestResendActivation_ActivatedIs409(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	ref, email := newRef("AC"), uniqueEmail("act")
	id := idOfEmp(t, empOf(t, r.putEmp(raw, ref, empBody("Maria", "Borg", &email, true))))
	if _, err := r.owner.Exec(context.Background(), `DELETE FROM invite_requests WHERE tenant_id = $1 AND employee_id = $2`, biz, id); err != nil {
		t.Fatal(err)
	}
	if _, err := r.owner.Exec(context.Background(), `UPDATE employees SET status = 'active', activated_at = now() WHERE tenant_id = $1 AND id = $2`, biz, id); err != nil {
		t.Fatal(err)
	}
	w := r.do(http.MethodPost, "/v1/employees/"+ref+"/resend-activation", raw, "")
	if w.Code != http.StatusConflict || problemOf(t, w).Code != CodeAlreadyActivated {
		t.Fatalf("an activated employee: %d %s, want 409 already_activated", w.Code, w.Body.String())
	}
	if n := r.ownerCount(`SELECT count(*) FROM invite_requests WHERE tenant_id = $1 AND employee_id = $2`, biz, id); n != 0 {
		t.Fatal("the 409 queued a request")
	}
	if w := r.do(http.MethodPost, "/v1/employees/"+newRef("NO")+"/resend-activation", raw, ""); w.Code != 404 {
		t.Fatalf("an unknown ref: %d", w.Code)
	}
	bare := newRef("BARE")
	r.putEmp(raw, bare, empBody("No", "Address", nil, true))
	if w := r.do(http.MethodPost, "/v1/employees/"+bare+"/resend-activation", raw, ""); w.Code != 422 || problemOf(t, w).Details["field"] != "email" {
		t.Fatalf("no address: %d %s", w.Code, w.Body.String())
	}
}

// TestDeactivationBudget_ExceededIs429WithAlertAndAudit (ADR 0028 §12, K-25): with a budget of 2, two
// DELETEs are 200; the third is 429 deactivation_budget_exceeded with Retry-After (1..3600) and the
// same seconds in details, the employee stays live; PUT {active:false} shares the budget (429); ONE
// api.deactivation_budget_exceeded row, whose actor and target are the key, detail {via, budget,
// window_seconds} -- a second refusal adds none; another key of the business still deactivates.
// Deactivating an already deactivated ref spends nothing and is 200 even past the budget.
func TestDeactivationBudget_ExceededIs429WithAlertAndAudit(t *testing.T) {
	r := newDBRig(t, func(c *Config) { c.DeactivationBudget = 2 })
	biz, owner := r.business(true)
	raw, keyID := r.key(biz, owner)
	refs := make([]string, 4)
	for i := range refs {
		refs[i] = newRef("BG")
		if w := r.putEmp(raw, refs[i], empBody("Maria", "Borg", nil, true)); w.Code != 201 {
			t.Fatal(w.Body.String())
		}
	}
	for _, ref := range refs[:2] {
		if w := r.do(http.MethodDelete, "/v1/employees/"+ref, raw, ""); w.Code != 200 || empOf(t, w).Active {
			t.Fatalf("a DELETE within the budget: %d %s", w.Code, w.Body.String())
		}
	}
	w := r.do(http.MethodDelete, "/v1/employees/"+refs[2], raw, "")
	if w.Code != http.StatusTooManyRequests || problemOf(t, w).Code != CodeDeactivationBudgetExceeded {
		t.Fatalf("past the budget: %d %s, want 429 deactivation_budget_exceeded", w.Code, w.Body.String())
	}
	ra, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || ra < 1 || ra > 3600 || problemOf(t, w).Details["retryAfterSeconds"] != float64(ra) {
		t.Fatalf("Retry-After %q, details %v", w.Header().Get("Retry-After"), problemOf(t, w).Details)
	}
	if g := empOf(t, r.do(http.MethodGet, "/v1/employees/"+refs[2], raw, "")); !g.Active {
		t.Fatal("the refused DELETE deactivated the employee")
	}
	if w := r.putEmp(raw, refs[3], empBody("Maria", "Borg", nil, false)); w.Code != 429 {
		t.Fatalf("PUT active:false past the budget: %d %s, want 429 (one budget)", w.Code, w.Body.String())
	}
	if n := r.ownerCount(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'api.deactivation_budget_exceeded'`, biz); n != 1 {
		t.Fatalf("%d alert rows after two refusals, want 1 (one per key per window)", n)
	}
	var actor uuid.UUID
	var detail string
	if err := r.owner.QueryRow(context.Background(), `SELECT actor_id, detail::text FROM audit_log WHERE tenant_id = $1 AND action = 'api.deactivation_budget_exceeded' AND target = $2`,
		biz, keyID.String()).Scan(&actor, &detail); err != nil {
		t.Fatalf("the alert row: %v (want exactly one)", err)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(detail), &d); err != nil || actor != keyID || d["via"] != "api" || d["budget"] != float64(2) || d["window_seconds"] != float64(3600) || len(d) != 3 {
		t.Fatalf("the alert row: actor %s detail %s", actor, detail)
	}
	if w := r.do(http.MethodDelete, "/v1/employees/"+refs[0], raw, ""); w.Code != 200 {
		t.Fatalf("an already deactivated ref past the budget: %d, want 200 (nothing spent)", w.Code)
	}
	other, _ := r.key(biz, owner)
	if w := r.do(http.MethodDelete, "/v1/employees/"+refs[2], other, ""); w.Code != 200 {
		t.Fatalf("another key: %d %s", w.Code, w.Body.String())
	}
}

// TestEmployeesGet_NoEmail (ADR 0028 §8: "GET /v1/employees yanıtı e-posta içermez; activationEmail
// durumu ve zamanı içerir"): GET by ref and the list answer every documented key and no other
// (DisallowUnknownFields), no "email" key, and the address nowhere in the bytes -- in any case; the
// list pages with nextCursor (limit 1) and a cursor of another shape is 400.
func TestEmployeesGet_NoEmail(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	emails := []string{uniqueEmail("ga"), uniqueEmail("gb")}
	refs := []string{newRef("GA"), newRef("GB")}
	for i := range refs {
		if w := r.putEmp(raw, refs[i], empBody("Maria", "Borg", &emails[i], true)); w.Code != 201 {
			t.Fatal(w.Body.String())
		}
	}
	check := func(body string) {
		t.Helper()
		for _, e := range emails {
			if strings.Contains(strings.ToLower(body), strings.ToLower(e)) {
				t.Fatalf("an answer carries an address: %s", body)
			}
		}
		if strings.Contains(body, `"email"`) {
			t.Fatalf("an answer carries an email key: %s", body)
		}
	}
	w := r.do(http.MethodGet, "/v1/employees/"+refs[0], raw, "")
	if w.Code != 200 || !empOf(t, w).HasEmail {
		t.Fatalf("GET by ref: %d %s", w.Code, w.Body.String())
	}
	check(w.Body.String())
	var seen []string
	cursor := ""
	for range 3 {
		path := "/v1/employees?limit=1"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		w := r.do(http.MethodGet, path, raw, "")
		if w.Code != 200 {
			t.Fatalf("the list: %d %s", w.Code, w.Body.String())
		}
		check(w.Body.String())
		var page struct {
			Data       []json.RawMessage `json:"data"`
			NextCursor *string           `json:"nextCursor"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		for _, d := range page.Data {
			var b employeeBody
			dec := json.NewDecoder(strings.NewReader(string(d)))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&b); err != nil {
				t.Fatalf("a listed employee: %v", err)
			}
			seen = append(seen, b.ID)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if len(seen) != 2 {
		t.Fatalf("the pages listed %v, want the two employees", seen)
	}
	if w := r.do(http.MethodGet, "/v1/employees?cursor=nope", raw, ""); w.Code != 400 {
		t.Fatalf("a malformed cursor: %d", w.Code)
	}
	if w := r.do(http.MethodGet, "/v1/employees?email=x", raw, ""); w.Code != 400 {
		t.Fatalf("an unknown parameter: %d", w.Code)
	}
}

// TestEmployees_OutsiderKeyCannotTouchAnotherTenant (ADR 0028 §2; plan §5: "C anahtarı başka
// tenant'ı göremez"): C's key reading, changing, deactivating and resending A's ref is 404 for the
// reads, the DELETE and the resend, and a PUT of the same ref opens C's OWN row -- A's row, trail and
// queue are untouched; C's list holds none of A's employees.
func TestEmployees_OutsiderKeyCannotTouchAnotherTenant(t *testing.T) {
	r := newDBRig(t)
	a, aOwner := r.business(true)
	c, cOwner := r.business(true)
	aRaw, _ := r.key(a, aOwner)
	cRaw, _ := r.key(c, cOwner)
	ref, email := newRef("ISO"), uniqueEmail("iso")
	aID := idOfEmp(t, empOf(t, r.putEmp(aRaw, ref, empBody("Maria", "Borg", &email, true))))
	trail := r.ownerCount(`SELECT count(*) FROM audit_log WHERE tenant_id = $1`, a)
	for _, x := range []struct{ method, path string }{
		{http.MethodGet, "/v1/employees/" + ref},
		{http.MethodDelete, "/v1/employees/" + ref},
		{http.MethodPost, "/v1/employees/" + ref + "/resend-activation"},
	} {
		if w := r.do(x.method, x.path, cRaw, ""); w.Code != 404 {
			t.Fatalf("C %s %s: %d %s, want 404", x.method, x.path, w.Code, w.Body.String())
		}
	}
	w := r.putEmp(cRaw, ref, empBody("Maria", "Borg", &email, true))
	if w.Code != 201 || idOfEmp(t, empOf(t, w)) == aID {
		t.Fatalf("C's PUT of A's ref: %d %s, want C's own new row", w.Code, w.Body.String())
	}
	var status string
	if err := r.owner.QueryRow(context.Background(), `SELECT status FROM employees WHERE id = $1`, aID).Scan(&status); err != nil || status != "invited" {
		t.Fatalf("A's row: %q %v", status, err)
	}
	if r.ownerCount(`SELECT count(*) FROM audit_log WHERE tenant_id = $1`, a) != trail {
		t.Fatal("C's requests wrote into A's trail")
	}
	w = r.do(http.MethodGet, "/v1/employees?limit=1000", cRaw, "")
	if strings.Contains(w.Body.String(), aID.String()) {
		t.Fatal("C's list holds A's employee")
	}
}

// TestEmployees_SuspensionClassesAreSection10 (ADR 0028 §10 -- the class assignment; the cell-by-cell
// table is API-4b's): while the business is suspended a PUT that opens a NEW employee -- a re-hire
// under a deactivated employee's ref included -- is 201 and resend-activation 202 (open), a GET 200;
// every PUT that lands on an existing employee (a change, one that matches them as they are, a K-18
// binding by address, active:false -- also on an employee already deactivated) and every DELETE
// (also of an employee already deactivated) are 403 tenant_suspended and write nothing (guide §10,
// §5.5: "200, nothing spent" is not what a suspended business is answered).
func TestEmployees_SuspensionClassesAreSection10(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	ref, email := newRef("SUS"), uniqueEmail("sus")
	if w := r.putEmp(raw, ref, empBody("Maria", "Borg", &email, true)); w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	gone := newRef("SUSGONE")
	if w := r.putEmp(raw, gone, empBody("Gone", "Away", nil, true)); w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	if w := r.do(http.MethodDelete, "/v1/employees/"+gone, raw, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var venue uuid.UUID
	if err := r.owner.QueryRow(context.Background(), `INSERT INTO locations (tenant_id, name) VALUES ($1, 'Panel venue') RETURNING id`, biz).Scan(&venue); err != nil {
		t.Fatal(err)
	}
	panelAddr := uniqueEmail("panel")
	if _, err := r.owner.Exec(context.Background(), `INSERT INTO employees (id, tenant_id, location_id, full_name, status, email) VALUES ($1, $2, $3, 'Joe Camilleri', 'invited', $4)`,
		uuid.New(), biz, venue, panelAddr); err != nil {
		t.Fatal(err)
	}
	trail := r.ownerCount(`SELECT count(*) FROM audit_log WHERE tenant_id = $1`, biz)
	r.suspended.suspend(biz)
	for _, x := range []struct {
		method, path, body string
	}{
		{http.MethodPut, "/v1/employees/" + ref, empBody("Maria", "Renamed", &email, true)},
		{http.MethodPut, "/v1/employees/" + ref, empBody("Maria", "Borg", &email, true)},
		{http.MethodPut, "/v1/employees/" + ref, empBody("Maria", "Borg", &email, false)},
		{http.MethodDelete, "/v1/employees/" + ref, ""},
		{http.MethodPut, "/v1/employees/" + newRef("SUSBIND"), empBody("Joe", "Camilleri", &panelAddr, true)},
		{http.MethodPut, "/v1/employees/" + gone, empBody("Gone", "Away", nil, false)},
		{http.MethodDelete, "/v1/employees/" + gone, ""},
	} {
		if w := r.do(x.method, x.path, raw, x.body); w.Code != 403 || problemOf(t, w).Code != CodeTenantSuspended {
			t.Fatalf("suspended %s %s: %d %s, want 403", x.method, x.body, w.Code, w.Body.String())
		}
	}
	// A reference the business never held (API-12 second fix, guide §5.5): a PUT with active:false
	// lands on no employee, so it is the 404 it is unsuspended; a DELETE is refused before the lookup.
	never := newRef("SUSNEVER")
	for _, x := range []struct {
		method, body string
		status       int
		code         Code
	}{
		{http.MethodPut, empBody("Never", "Held", nil, false), 404, CodeNotFound},
		{http.MethodDelete, "", 403, CodeTenantSuspended},
	} {
		if w := r.do(x.method, "/v1/employees/"+never, raw, x.body); w.Code != x.status || problemOf(t, w).Code != x.code {
			t.Fatalf("suspended %s of a reference never held: %d %s, want %d %s", x.method, w.Code, w.Body.String(), x.status, x.code)
		}
	}
	if r.ownerCount(`SELECT count(*) FROM audit_log WHERE tenant_id = $1`, biz) != trail {
		t.Fatal("a refused write left a trail row")
	}
	if w := r.putEmp(raw, newRef("SUSNEW"), empBody("New", "Hire", nil, true)); w.Code != 201 {
		t.Fatalf("a NEW employee while suspended: %d %s, want 201", w.Code, w.Body.String())
	}
	if w := r.putEmp(raw, gone, empBody("Gone", "Away", nil, true)); w.Code != 201 {
		t.Fatalf("a re-hire while suspended: %d %s, want 201", w.Code, w.Body.String())
	}
	if w := r.do(http.MethodPost, "/v1/employees/"+ref+"/resend-activation", raw, ""); w.Code != 202 {
		t.Fatalf("resend while suspended: %d %s, want 202", w.Code, w.Body.String())
	}
	if w := r.do(http.MethodGet, "/v1/employees/"+ref, raw, ""); w.Code != 200 {
		t.Fatalf("GET while suspended: %d", w.Code)
	}
}

// TestEmployeesPut_TheBodyContract: API-5's strictness on the employee body -- an unknown key (and a
// case variant) 422 unknown_field, a key twice 422 duplicate_field, a missing email 422 required, a
// non-ASCII or display-name address 422 invalid, an administrator's address 422
// administrator_address, a hidden character in a name 422 hidden_character, the names together past
// 120 runes 422 invalid, externalRef other than the path's 422 mismatch, a body that is not UTF-8
// 400; none opens a row. A K-18 binding: the panel's unbound employee with the address is answered
// 200 under the ref, not duplicated.
func TestEmployeesPut_TheBodyContract(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	var adminEmail string
	if err := r.owner.QueryRow(context.Background(), `SELECT email FROM admin_users WHERE id = $1`, owner).Scan(&adminEmail); err != nil {
		t.Fatal(err)
	}
	ref := newRef("BODY")
	for _, x := range []struct {
		body   string
		status int
		field  string
		reason string
	}{
		{`{"firstName":"A","lastName":"B","email":null,"active":true,"Email":"x"}`, 422, "", reasonUnknownField},
		{`{"firstName":"A","lastName":"B","email":null,"active":true,"active":false}`, 422, "", reasonDuplicateField},
		{`{"firstName":"A","lastName":"B","active":true}`, 422, "email", reasonRequired},
		{`{"firstName":"A","lastName":"B","email":"m\u00e4ria@x.example","active":true}`, 422, "email", reasonInvalid},
		{`{"firstName":"A","lastName":"B","email":"Maria <m@x.example>","active":true}`, 422, "email", reasonInvalid},
		{jsonBody(map[string]any{"firstName": "A", "lastName": "B", "email": strings.ToUpper(adminEmail), "active": true}), 422, "email", reasonAdministratorAddress},
		{`{"firstName":"Ma\u202eria","lastName":"B","email":null,"active":true}`, 422, "firstName", reasonHiddenCharacter},
		{`{"firstName":"A","lastName":"B\tC","email":null,"active":true}`, 422, "lastName", reasonHiddenCharacter},
		{jsonBody(map[string]any{"firstName": strings.Repeat("a", 60), "lastName": strings.Repeat("b", 60), "email": nil, "active": true}), 422, "lastName", reasonInvalid},
		{`{"firstName":"A","lastName":"B","email":null,"active":true,"externalRef":"OTHER"}`, 422, "externalRef", reasonMismatch},
		{"{\"firstName\":\"A\xff\",\"lastName\":\"B\",\"email\":null,\"active\":true}", 400, "", "invalid_utf8"},
	} {
		w := r.putEmp(raw, ref, x.body)
		p := problemOf(t, w)
		if w.Code != x.status || (x.field != "" && p.Details["field"] != x.field) || p.Details["reason"] != x.reason {
			t.Errorf("%s: %d %s, want %d %s/%s", x.body, w.Code, w.Body.String(), x.status, x.field, x.reason)
		}
	}
	if n := r.ownerCount(`SELECT count(*) FROM employees WHERE tenant_id = $1 AND external_ref = $2`, biz, ref); n != 0 {
		t.Fatalf("refused bodies opened %d rows", n)
	}
	// K-18.
	panelRow, addr := uuid.New(), uniqueEmail("panel")
	var venue uuid.UUID
	if err := r.owner.QueryRow(context.Background(), `INSERT INTO locations (tenant_id, name) VALUES ($1, 'Panel venue') RETURNING id`, biz).Scan(&venue); err != nil {
		t.Fatal(err)
	}
	if _, err := r.owner.Exec(context.Background(), `INSERT INTO employees (id, tenant_id, location_id, full_name, status, email) VALUES ($1, $2, $3, 'Joe Camilleri', 'invited', $4)`,
		panelRow, biz, venue, addr); err != nil {
		t.Fatal(err)
	}
	w := r.putEmp(raw, ref, empBody("Joe", "Camilleri", &addr, true))
	if w.Code != 200 || idOfEmp(t, empOf(t, w)) != panelRow {
		t.Fatalf("K-18: %d %s, want the panel's employee bound (200)", w.Code, w.Body.String())
	}
}

// TestEmployees_IdempotencyReplay: a PUT with an Idempotency-Key is replayed with the same status
// and the employee as it is now (a rename in between shows), Idempotent-Replayed: true; the ledger
// row's result_ref is the employee.
func TestEmployees_IdempotencyReplay(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	ref, idem := newRef("IDM"), "idm-"+uuid.NewString()
	w := r.putEmp(raw, ref, empBody("Maria", "Borg", nil, true), "Idempotency-Key", idem)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	id := idOfEmp(t, empOf(t, w))
	if r.putEmp(raw, ref, empBody("Maria", "Grech", nil, true)).Code != 200 {
		t.Fatal("rename")
	}
	again := r.putEmp(raw, ref, empBody("Maria", "Borg", nil, true), "Idempotency-Key", idem)
	if again.Code != 201 || again.Header().Get("Idempotent-Replayed") != "true" || idOfEmp(t, empOf(t, again)) != id || empOf(t, again).FullName != "Maria Grech" {
		t.Fatalf("the replay: %d %v %s", again.Code, again.Header(), again.Body.String())
	}
	var ref2 uuid.UUID
	if err := r.owner.QueryRow(context.Background(), `SELECT result_ref FROM api_idempotency WHERE tenant_id = $1 AND idem_key = $2`, biz, idem).Scan(&ref2); err != nil || ref2 != id {
		t.Fatalf("the ledger: %s %v", ref2, err)
	}
}

// syncBuffer is a log sink the handlers write while the test reads.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestEmployees_NothingOfTheAddressOrTheKeyReachesALogOrATrail (CLAUDE.md §7, ADR 0022 §10, ADR 0028
// §3): a run through every employee path -- a create, an address change, a 409, the administrator's
// address, a non-ASCII address, a resend, deactivations past a budget of 1 (the alert row), the reads
// and a replay -- with the surface's log captured at debug: no address the requests carried, no part
// of the key, and no "bearer" appear in the log, in any trail row of the business, or in any error
// answer. The surface logs nothing on these paths today, so the log arm is a tripwire for the next
// edit: MUTANT (measured) -- the PUT's body logged at debug -> red.
func TestEmployees_NothingOfTheAddressOrTheKeyReachesALogOrATrail(t *testing.T) {
	logs := &syncBuffer{}
	r := newDBRig(t, func(c *Config) {
		c.DeactivationBudget = 1
		c.Log = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	var adminEmail string
	if err := r.owner.QueryRow(context.Background(), `SELECT email FROM admin_users WHERE id = $1`, owner).Scan(&adminEmail); err != nil {
		t.Fatal(err)
	}
	a, b, c := uniqueEmail("canary-a"), uniqueEmail("canary-b"), uniqueEmail("canary-c")
	refA, refB := newRef("CANA"), newRef("CANB")
	var answers []string
	keep := func(w *httptest.ResponseRecorder) {
		if w.Code >= 400 {
			answers = append(answers, w.Body.String())
		}
	}
	keep(r.putEmp(raw, refA, empBody("Maria", "Borg", &a, true), "Idempotency-Key", "canary-"+uuid.NewString()))
	keep(r.putEmp(raw, refA, empBody("Maria", "Borg", &b, true)))
	keep(r.putEmp(raw, refB, empBody("Joe", "Camilleri", &c, true)))
	keep(r.putEmp(raw, refB, empBody("Joe", "Camilleri", &b, true)))
	upperAdmin := strings.ToUpper(adminEmail)
	keep(r.putEmp(raw, newRef("CANX"), empBody("Ann", "Admin", &upperAdmin, true)))
	nonASCII := "cänary." + uuid.NewString()[:6] + "@API6.example.test"
	keep(r.putEmp(raw, newRef("CANY"), empBody("Ann", "Accent", &nonASCII, true)))
	keep(r.do(http.MethodPost, "/v1/employees/"+refA+"/resend-activation", raw, ""))
	keep(r.do(http.MethodDelete, "/v1/employees/"+refA, raw, ""))
	keep(r.do(http.MethodDelete, "/v1/employees/"+refB, raw, ""))
	keep(r.do(http.MethodGet, "/v1/employees/"+refB, raw, ""))
	keep(r.do(http.MethodGet, "/v1/employees", raw, ""))
	if len(answers) < 4 {
		t.Fatalf("the run refused %d requests; the 409, the two 422s and the 429 were expected", len(answers))
	}
	var trail []string
	rows, err := r.owner.Query(context.Background(), `SELECT action || ' ' || detail::text FROM audit_log WHERE tenant_id = $1`, biz)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		trail = append(trail, s)
	}
	rows.Close()
	if !strings.Contains(strings.Join(trail, "\n"), "api.deactivation_budget_exceeded") {
		t.Fatal("anti-vacuity: the run did not reach the budget's alert")
	}
	canaries := []string{a, b, c, adminEmail, nonASCII, raw, raw[len("tt_live_"):], "bearer"}
	for where, hay := range map[string]string{"the log": logs.String(), "the trail": strings.Join(trail, "\n"), "the refusals": strings.Join(answers, "\n")} {
		low := strings.ToLower(hay)
		for _, cn := range canaries {
			if strings.Contains(low, strings.ToLower(cn)) {
				t.Errorf("%s carries %q's kind of value (an address, the key or the scheme)", where, cn[:min(len(cn), 6)])
			}
		}
	}
}

// TestEmployeesPut_APutReplacesALeftOutOptionalFieldIsNull (M11 API-12 fix; guide §5.1): a PUT is a
// whole replacement, not a patch -- an employee whose employeeCode is "77", PUT without the field, is
// 200 with employeeCode null (and so is the row the feed and a partner's directory read); sent again
// with the field it is back. A location's PUT replaces too, but its optional fields hold nothing of
// their own (timezone is the business's, externalRef the path's): left out, the location is answered
// as it was -- 200, unchanged.
func TestEmployeesPut_APutReplacesALeftOutOptionalFieldIsNull(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	ref := newRef("REPL")
	with := jsonBody(map[string]any{"firstName": "Rita", "lastName": "Vella", "email": nil, "active": true, "employeeCode": "77"})
	w := r.putEmp(raw, ref, with)
	if w.Code != 201 || empOf(t, w).EmployeeCode == nil || *empOf(t, w).EmployeeCode != "77" {
		t.Fatalf("the employee with code 77: %d %s", w.Code, w.Body.String())
	}
	w = r.putEmp(raw, ref, empBody("Rita", "Vella", nil, true))
	if w.Code != 200 || empOf(t, w).EmployeeCode != nil {
		t.Fatalf("a PUT without employeeCode: %d %s, want 200 and employeeCode null", w.Code, w.Body.String())
	}
	var code *string
	if err := r.owner.QueryRow(context.Background(), `SELECT employee_code FROM employees WHERE tenant_id = $1 AND external_ref = $2`, biz, ref).Scan(&code); err != nil || code != nil {
		t.Fatalf("the row's employee_code: %v %v, want NULL", code, err)
	}
	if w = r.putEmp(raw, ref, with); w.Code != 200 || empOf(t, w).EmployeeCode == nil {
		t.Fatalf("the code sent again: %d %s", w.Code, w.Body.String())
	}

	loc := newRef("REPLOC")
	if w = r.putLoc(raw, loc, `{"name":"Sliema","active":true,"timezone":"Europe/Malta","externalRef":"`+loc+`"}`); w.Code != http.StatusCreated {
		t.Fatalf("the location: %d %s", w.Code, w.Body.String())
	}
	before := w.Body.String()
	id := r.venuesByRef(biz, loc)[0].id
	trail := len(r.venueTrail(biz, id))
	if w = r.putLoc(raw, loc, `{"name":"Sliema","active":true}`); w.Code != http.StatusOK || w.Body.String() != before {
		t.Fatalf("the location without its optional fields: %d %s, want 200 and %s", w.Code, w.Body.String(), before)
	}
	if n := len(r.venueTrail(biz, id)); n != trail {
		t.Fatalf("the location without its optional fields wrote %d trail rows", n-trail)
	}
}
