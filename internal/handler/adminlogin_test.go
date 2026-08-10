package handler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/review"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/session"
)

// adminlogin_test.go -- the HTTP half of M6-01 phase B.
//
// It drives the handler against a FAKE adminauth.Manager, deliberately. What is
// under test here is the SHAPE OF THE RESPONSE and the ORDER OF THE CHECKS -- that
// three outcomes are byte-identical, that a rate limit refuses before bcrypt runs,
// that a tenant outside the signed set cannot buy a session -- and every one of
// those is a property of this file, not of Postgres. The database-backed proofs
// (the resolver, the cross-tenant bypass with real rows, the disabled-admin kill
// switch) live in internal/adminauth/manager_db_test.go and run against real
// Postgres.
//
// THE FAKE IS DRIVEN BY A FUNCTION, not by a fixed value, so one router can answer
// three different outcomes in three consecutive requests -- which is exactly what
// the byte-identity test needs.

const testBaseURL = "https://panel.example.test"

type fakeAdmins struct {
	mu sync.Mutex

	authenticate func(email, password string) (adminauth.Authentication, error)
	choices      func(v []adminauth.Verified) ([]adminauth.Choice, error)
	issue        func(v adminauth.Verified) (adminauth.Issued, error)
	verify       func() (adminauth.Resolved, error)

	authCalls   int
	issuedFor   []adminauth.Verified
	revokeCalls int
	verifyCalls int
}

func (f *fakeAdmins) Authenticate(_ context.Context, email, password string) (adminauth.Authentication, error) {
	f.mu.Lock()
	f.authCalls++
	f.mu.Unlock()
	if f.authenticate == nil {
		return adminauth.Authentication{}, adminauth.ErrBadCredentials
	}
	return f.authenticate(email, password)
}

func (f *fakeAdmins) TenantChoices(_ context.Context, v []adminauth.Verified) ([]adminauth.Choice, error) {
	if f.choices == nil {
		out := make([]adminauth.Choice, 0, len(v))
		for i, one := range v {
			out = append(out, adminauth.Choice{
				AdminUserID: one.AdminUserID, TenantID: one.TenantID,
				TenantName: fmt.Sprintf("Business %d", i+1), Role: "owner", FullName: "Owner",
			})
		}
		return out, nil
	}
	return f.choices(v)
}

func (f *fakeAdmins) Issue(_ context.Context, v adminauth.Verified) (adminauth.Issued, error) {
	f.mu.Lock()
	f.issuedFor = append(f.issuedFor, v)
	f.mu.Unlock()
	if f.issue == nil {
		return adminauth.Issued{
			Session: adminauth.Session{ID: uuid.New(), TenantID: v.TenantID, AdminUserID: v.AdminUserID},
			Token:   fakeIssuedToken(),
		}, nil
	}
	return f.issue(v)
}

// fakeIssuedToken builds a POPULATED adminauth.Token from outside that package,
// through the only public door that accepts one: reading a request cookie.
//
// IT IS NEEDED BECAUSE THE ZERO Token IS REFUSED, which is the point rather than
// an inconvenience — Cookies.Set rejects an empty value, so a fake that returned
// `Issued{}` made completeLogin answer 500. That is the correct behaviour (a
// session cookie with no value would look like a successful login and behave like
// no session at all) and the first version of this fake measured it by accident.
func fakeIssuedToken() adminauth.Token {
	req := httptest.NewRequest(http.MethodGet, adminauth.CookiePath, nil)
	req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: "FAKEfakeFAKEfakeFAKEfakeFAKEfakeFAKEfake123"})
	var c adminauth.Cookies
	tok, _ := c.Read(req)
	return tok
}

func (f *fakeAdmins) Verify(_ context.Context, _ adminauth.Token) (adminauth.Resolved, error) {
	// COUNTED, because "the gate refused" and "the gate refused WITHOUT paying for
	// the resolver" are different claims and only the second one is the design rule
	// adminlogin.go states for sign-out ("a FREE refusal, BEFORE the resolver").
	// A status-only assertion cannot tell them apart.
	f.mu.Lock()
	f.verifyCalls++
	f.mu.Unlock()
	if f.verify == nil {
		return adminauth.Resolved{}, adminauth.ErrNoSession
	}
	return f.verify()
}

// verifiedCount is how many times the panel resolved a session — i.e. how many
// database round trips a sequence of requests cost before any gate refused them.
func (f *fakeAdmins) verifiedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.verifyCalls
}

func (f *fakeAdmins) Revoke(_ context.Context, _, _ uuid.UUID) error {
	f.mu.Lock()
	f.revokeCalls++
	f.mu.Unlock()
	return nil
}

// fakeTrail counts audit rows without a database. audit_log is append-only at the
// DATABASE level, so "how many rows did this produce" is the number that matters
// (M5-02's lesson) and it is counted here.
type fakeTrail struct {
	mu     sync.Mutex
	events []audit.Event
}

func (f *fakeTrail) Record(_ context.Context, e audit.Event) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return uuid.New(), nil
}

func (f *fakeTrail) count(action string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e.Action == action {
			n++
		}
	}
	return n
}

// eventsSnapshot is a copy of every event recorded so far, for the tests that read
// the PAYLOAD rather than count the action.
func (f *fakeTrail) eventsSnapshot() []audit.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]audit.Event, len(f.events))
	copy(out, f.events)
	return out
}

func (f *fakeTrail) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

// fakeLedger is the panel's read side without a database.
//
// 🔴 IT DEFAULTS TO "QUERIED, AND THE DAY WAS EMPTY", which is a real answer and
// not the zero value. A fake that returned a zero ledger.Page would have Queried
// false, and the transactions template deliberately renders NOTHING in that state
// (§4.6: the product may not say "no taps" without having asked) — so a test using
// the zero fake would be reading a blank section and would not notice.
type fakeLedger struct {
	mu     sync.Mutex
	page   ledger.Page
	opts   ledger.Options
	err    error
	calls  []ledger.Filter
	tenant []uuid.UUID

	// M6-04's read side. queue/pending default to "asked, and there is nothing
	// waiting", for the same reason page does: an unqueried zero value renders
	// nothing at all, so a test using it would be reading a blank section.
	queue      ledger.QueuePage
	queueErr   error
	pending    ledger.Pending
	pendingErr error
	// queueTenant and pendingTenant record who was asked, which is what the
	// isolation assertions read.
	queueTenant   []uuid.UUID
	pendingTenant []uuid.UUID
	queueCursors  []*ledger.Cursor

	// M6-04 round 6: what the database would say a record was decided as. The
	// confirmation banner is checked against this rather than believed from the URL.
	decisions      map[uuid.UUID]string
	decisionErr    error
	decisionTenant []uuid.UUID

	// M6-05 phase A's read side. Same rule as page and queue: it defaults to
	// "asked, and nobody is on the books", because an unqueried zero value renders
	// nothing at all and a test using it would be reading a blank section.
	roster        ledger.RosterScreen
	rosterErr     error
	rosterTenant  []uuid.UUID
	rosterFilters []ledger.RosterFilter
}

func newFakeLedger() *fakeLedger {
	return &fakeLedger{
		page:   ledger.Page{Queried: true, Zone: time.UTC},
		queue:  ledger.QueuePage{Queried: true, Zone: time.UTC},
		roster: ledger.RosterScreen{RosterPage: ledger.RosterPage{Queried: true, Zone: time.UTC}},
	}
}

// Roster is M6-05 phase A's read. It records who was asked and with what, which is
// what the isolation and filter assertions read.
func (f *fakeLedger) Roster(_ context.Context, tenantID uuid.UUID, filter ledger.RosterFilter) (ledger.RosterScreen, error) {
	f.mu.Lock()
	f.rosterTenant = append(f.rosterTenant, tenantID)
	f.rosterFilters = append(f.rosterFilters, filter)
	s, err := f.roster, f.rosterErr
	f.mu.Unlock()
	if err != nil {
		return ledger.RosterScreen{}, err
	}
	if s.Zone == nil {
		s.Zone = time.UTC
	}
	return s, nil
}

// lastRosterFilter is the roster twin of lastFilter.
func (f *fakeLedger) lastRosterFilter(t *testing.T) ledger.RosterFilter {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rosterFilters) == 0 {
		t.Fatal("the roster was never asked anything; the handler answered without querying")
	}
	return f.rosterFilters[len(f.rosterFilters)-1]
}

func (f *fakeLedger) Queue(_ context.Context, tenantID uuid.UUID, after *ledger.Cursor) (ledger.QueuePage, error) {
	f.mu.Lock()
	f.queueTenant = append(f.queueTenant, tenantID)
	f.queueCursors = append(f.queueCursors, after)
	q, err := f.queue, f.queueErr
	f.mu.Unlock()
	if err != nil {
		return ledger.QueuePage{}, err
	}
	if q.Zone == nil {
		q.Zone = time.UTC
	}
	return q, nil
}

// decisions is what the fake claims is on record, keyed by transaction id. Empty by
// default, which is the honest zero value: nothing has been decided.
func (f *fakeLedger) Decision(_ context.Context, tenantID, txnID uuid.UUID) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decisionTenant = append(f.decisionTenant, tenantID)
	if f.decisionErr != nil {
		return "", false, f.decisionErr
	}
	out, ok := f.decisions[txnID]
	return out, ok, nil
}

func (f *fakeLedger) decided(txnID uuid.UUID, outcome string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.decisions == nil {
		f.decisions = map[uuid.UUID]string{}
	}
	f.decisions[txnID] = outcome
}

func (f *fakeLedger) Pending(_ context.Context, tenantID uuid.UUID) (ledger.Pending, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pendingTenant = append(f.pendingTenant, tenantID)
	if f.pendingErr != nil {
		return ledger.Pending{}, f.pendingErr
	}
	return f.pending, nil
}

// fakeReviewer is the write side without a database. It records what it was asked
// to do and answers with whatever the test set.
type fakeReviewer struct {
	mu    sync.Mutex
	err   error
	calls []review.Decision
}

func (f *fakeReviewer) Record(_ context.Context, d review.Decision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, d)
	return f.err
}

func (f *fakeReviewer) recorded() []review.Decision {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]review.Decision, len(f.calls))
	copy(out, f.calls)
	return out
}

// fakeStaff is the employees section's write side without a database (M6-05 phase
// B). It records what it was asked to do and answers with whatever the test set.
//
// ⚠️ WHAT A FAKE CAN AND CANNOT PROVE HERE. It proves the BOUNDARY: which command
// the handler builds from a request, that the tenant and the actor come from the
// session rather than from the body, and that a refusal reaches the manager as a
// sentence. It cannot prove anything about §4.5 or about the audit row sharing a
// transaction — a fake accepts whatever it is told, so those live in
// employeeactions_db_test.go against real Postgres.
type fakeStaff struct {
	mu            sync.Mutex
	person        *tenant.Person
	personErr     error
	deactivateErr error
	moveErr       error
	lookups       []uuid.UUID
	deactivations []tenant.DeactivateCommand
	moves         []tenant.MoveCommand
}

func (f *fakeStaff) Person(_ context.Context, _, employeeID uuid.UUID) (tenant.Person, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups = append(f.lookups, employeeID)
	if f.personErr != nil {
		return tenant.Person{}, f.personErr
	}
	if f.person != nil {
		p := *f.person
		p.ID = employeeID
		return p, nil
	}
	return tenant.Person{
		ID:           employeeID,
		Name:         "Maria Borg",
		Status:       ledger.StatusActive,
		LocationID:   panelTestLocation,
		LocationName: "KF St Julians",
	}, nil
}

func (f *fakeStaff) Deactivate(_ context.Context, c tenant.DeactivateCommand) (tenant.Person, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deactivations = append(f.deactivations, c)
	if f.deactivateErr != nil {
		return tenant.Person{}, f.deactivateErr
	}
	return tenant.Person{ID: c.EmployeeID, Name: "Maria Borg", Status: ledger.StatusDeactivated}, nil
}

func (f *fakeStaff) Move(_ context.Context, c tenant.MoveCommand) (tenant.Person, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, c)
	if f.moveErr != nil {
		return tenant.Person{}, f.moveErr
	}
	return tenant.Person{
		ID: c.EmployeeID, Name: "Maria Borg", Status: ledger.StatusActive,
		LocationID: c.LocationID, DepartmentID: c.DepartmentID,
	}, nil
}

func (f *fakeStaff) commands() ([]tenant.DeactivateCommand, []tenant.MoveCommand) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := make([]tenant.DeactivateCommand, len(f.deactivations))
	copy(d, f.deactivations)
	m := make([]tenant.MoveCommand, len(f.moves))
	copy(m, f.moves)
	return d, m
}

// fakeVenues stands in for internal/domain/tenant.Venues (M6-06 phase A).
//
// 🔴 WHAT A FAKE HERE CAN AND CANNOT PROVE, stated because the same sentence had to
// be written for fakeStaff. It can prove that the SECTION renders what it is given,
// that a refusal reaches the manager as a sentence, that the tenant and the actor
// come from the session rather than from the body, and that a rejected form keeps
// the manager's typing. It cannot prove anything about §4.5, about NULL shifts
// actually reaching the column as NULL, or about the audit row sharing a
// transaction — a fake accepts whatever it is told, so those live in
// internal/domain/tenant/venue_db_test.go (the domain's writes and reads against real
// Postgres) and internal/db/venuewrites_test.go (the RLS layer underneath them, with
// no tenant predicate of its own).
type fakeVenues struct {
	mu        sync.Mutex
	screen    tenant.VenueScreen
	screenErr error
	saveErr   error
	deptErr   error
	venues    []tenant.VenueCommand
	depts     []tenant.DepartmentCommand

	// beyondVenues / beyondDepts are rows that EXIST in the business but are not in
	// the (capped) screen lists. byIDErr makes the fallback read fail; byIDCalls
	// counts it, so a test can assert the fast path did NOT pay for a round trip.
	beyondVenues map[uuid.UUID]tenant.Venue
	beyondDepts  map[uuid.UUID]tenant.Department
	byIDErr      error
	byIDCalls    int

	// refs is what still points at each row; deleted records the removals the
	// section asked for; deleteErr is what the domain answers, so the FK race is
	// drivable without a database.
	refs    map[uuid.UUID]tenant.References
	refsErr error
	// refCalls counts the reference lookups, because "a manager pays nothing" is a
	// claim about a NUMBER OF QUERIES and a flag could not see it.
	refCalls  int
	deleteErr error
	deleted   []tenant.DeleteCommand

	// receipts is the trail C' reads, keyed "action|target"; confirmCalls counts the
	// lookups so the fast path can be measured in QUERIES rather than in flags.
	receipts     map[string]tenant.RemovalReceipt
	confirmErr   error
	confirmCalls int
	confirmSeen  [][4]string
}

func (f *fakeVenues) Screen(_ context.Context, _ uuid.UUID) (tenant.VenueScreen, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.screenErr != nil {
		return tenant.VenueScreen{}, f.screenErr
	}
	return f.screen, nil
}

// Venue and Department are the BY-ID fallback behind the capped screen lists. The
// fake answers them from a map the test fills, which is what lets a test express
// "this row exists in the business but is NOT in the (capped) list" -- the state the
// section used to describe as "not one of this business's".
// VenueReferences / DepartmentReferences and the two removals (M6-06 phase A, user
// decision 2026-08-08). refs is what the fake reports; deleteErr is what the domain
// would answer, so a test can drive the FK race without a database.
// ConfirmRemoval answers the trail lookup C' depends on. receipts is keyed by
// action+target so a test can express "this admin removed THAT row" precisely;
// confirmCalls counts the query, because "the fast path pays nothing" is a claim about
// a NUMBER OF QUERIES and a boolean flag would not measure it.
func (f *fakeVenues) ConfirmRemoval(_ context.Context, tenantID, actorID uuid.UUID, action, target string) (tenant.RemovalReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.confirmCalls++
	f.confirmSeen = append(f.confirmSeen, [4]string{
		tenantID.String(), actorID.String(), action, target,
	})
	if f.confirmErr != nil {
		return tenant.RemovalReceipt{}, f.confirmErr
	}
	r, ok := f.receipts[action+"|"+target]
	if !ok {
		return tenant.RemovalReceipt{}, nil
	}
	return r, nil
}

// confirmations returns the query count and what each call was scoped to.
func (f *fakeVenues) confirmations() (int, [][4]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][4]string, len(f.confirmSeen))
	copy(out, f.confirmSeen)
	return f.confirmCalls, out
}

func (f *fakeVenues) VenueReferences(_ context.Context, _ uuid.UUID, id uuid.UUID) (tenant.References, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refCalls++
	if f.refsErr != nil {
		return tenant.References{}, f.refsErr
	}
	return f.refs[id], nil
}

func (f *fakeVenues) DepartmentReferences(_ context.Context, _ uuid.UUID, id uuid.UUID) (tenant.References, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refCalls++
	if f.refsErr != nil {
		return tenant.References{}, f.refsErr
	}
	return f.refs[id], nil
}

func (f *fakeVenues) DeleteVenue(_ context.Context, c tenant.DeleteCommand) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, c)
	return f.deleteErr
}

func (f *fakeVenues) DeleteDepartment(_ context.Context, c tenant.DeleteCommand) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, c)
	return f.deleteErr
}

// removals returns copies of the delete commands the section built.
func (f *fakeVenues) removals() []tenant.DeleteCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]tenant.DeleteCommand, len(f.deleted))
	copy(out, f.deleted)
	return out
}

func (f *fakeVenues) Venue(_ context.Context, _ uuid.UUID, id uuid.UUID) (tenant.Venue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byIDCalls++
	if f.byIDErr != nil {
		return tenant.Venue{}, f.byIDErr
	}
	if v, ok := f.beyondVenues[id]; ok {
		return v, nil
	}
	return tenant.Venue{}, tenant.ErrUnknownVenue
}

func (f *fakeVenues) Department(_ context.Context, _ uuid.UUID, id uuid.UUID) (tenant.Department, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byIDCalls++
	if f.byIDErr != nil {
		return tenant.Department{}, f.byIDErr
	}
	if d, ok := f.beyondDepts[id]; ok {
		return d, nil
	}
	return tenant.Department{}, tenant.ErrUnknownDepartment
}

func (f *fakeVenues) SaveVenue(_ context.Context, c tenant.VenueCommand) (tenant.Venue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.venues = append(f.venues, c)
	if f.saveErr != nil {
		return tenant.Venue{}, f.saveErr
	}
	id := c.VenueID
	if id == uuid.Nil {
		id = uuid.New()
	}
	return tenant.Venue{
		ID: id, Name: c.Name, StaticIPs: c.StaticIPs,
		GPS: c.GPS, Shift: c.Shift, WiFiSSID: c.WiFiSSID,
	}, nil
}

func (f *fakeVenues) SaveDepartment(_ context.Context, c tenant.DepartmentCommand) (tenant.Department, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.depts = append(f.depts, c)
	if f.deptErr != nil {
		return tenant.Department{}, f.deptErr
	}
	id := c.DepartmentID
	if id == uuid.Nil {
		id = uuid.New()
	}
	return tenant.Department{
		ID: id, LocationID: c.LocationID, Name: c.Name, Shift: c.Shift,
	}, nil
}

// referenceCalls is how many reference lookups the section made.
func (f *fakeVenues) referenceCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refCalls
}

// saved returns copies of what the section asked for, so an assertion reads the
// COMMAND the handler built rather than the form it was given.
func (f *fakeVenues) saved() ([]tenant.VenueCommand, []tenant.DepartmentCommand) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := make([]tenant.VenueCommand, len(f.venues))
	copy(v, f.venues)
	d := make([]tenant.DepartmentCommand, len(f.depts))
	copy(d, f.depts)
	return v, d
}

// fakePlaques stands in for internal/domain/tenant.Plaques (M6-06 phase B).
//
// 🔴 WHAT IT CAN AND CANNOT PROVE, said here rather than discovered later. It can
// prove what the SECTION does — which lists it builds, which control it offers,
// which word it redirects with, how many trail lookups it makes. It cannot prove
// anything about §4.5, about the retire and the bind sharing ONE transaction, or
// about a CHECK constraint firing, because a fake accepts whatever it is told.
// Those live in internal/domain/tenant/plaque_db_test.go, against real Postgres.
//
// 🔴 IT HAS NO KEY FIELD EITHER, and that is not decoration: if a test double
// could carry an aes_key_ref, a screen test could pass while the production type
// grew one. The wall is a property of the TYPES, so the double has to obey it too.
type fakePlaques struct {
	mu        sync.Mutex
	screen    tenant.PlaqueScreen
	screenErr error

	// beyond is the by-uid fallback behind the capped list: rows that EXIST but are
	// not in screen.Plaques. byUIDErr makes the fallback fail; byUIDCalls counts it,
	// so a test can assert the fast path did NOT pay for a round trip.
	beyond     map[string]tenant.Plaque
	byUIDErr   error
	byUIDCalls int

	// mountErr / replaceErr are what the domain answers, so every refusal the
	// section maps can be driven without a database.
	mountErr   error
	replaceErr error
	unmountErr error
	mounted    []tenant.MountCommand
	replaced   []tenant.ReplaceCommand
	unmounted  []tenant.UnmountCommand

	// history is the audit trail the CARD reads, keyed by uid; historyCalls counts
	// the query, because "the LIST pays nothing for it" is a claim about a NUMBER and
	// a boolean could not see it.
	history      map[string][]tenant.PlaqueEvent
	historyErr   error
	historyCalls int

	// receipts is the trail C' reads, keyed "action|target"; confirmCalls counts the
	// lookups so "the fast path pays nothing" is measured in QUERIES rather than in
	// a flag.
	receipts     map[string]tenant.RemovalReceipt
	confirmErr   error
	confirmCalls int
	confirmSeen  [][4]string
}

func (f *fakePlaques) Screen(_ context.Context, _ uuid.UUID) (tenant.PlaqueScreen, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.screenErr != nil {
		return tenant.PlaqueScreen{}, f.screenErr
	}
	return f.screen, nil
}

func (f *fakePlaques) Plaque(_ context.Context, _ uuid.UUID, uid string) (tenant.Plaque, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byUIDCalls++
	if f.byUIDErr != nil {
		return tenant.Plaque{}, f.byUIDErr
	}
	if p, ok := f.beyond[uid]; ok {
		return p, nil
	}
	return tenant.Plaque{}, tenant.ErrUnknownPlaque
}

func (f *fakePlaques) Mount(_ context.Context, c tenant.MountCommand) (tenant.Plaque, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mounted = append(f.mounted, c)
	if f.mountErr != nil {
		return tenant.Plaque{}, f.mountErr
	}
	loc := c.LocationID
	return tenant.Plaque{
		UID: c.UID, Status: tenant.PlaqueActive, LocationID: &loc, Canonical: true,
	}, nil
}

func (f *fakePlaques) Unmount(_ context.Context, c tenant.UnmountCommand) (tenant.Plaque, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unmounted = append(f.unmounted, c)
	if f.unmountErr != nil {
		return tenant.Plaque{}, f.unmountErr
	}
	return tenant.Plaque{UID: c.UID, Status: tenant.PlaqueUnassigned, Canonical: true}, nil
}

func (f *fakePlaques) Replace(_ context.Context, c tenant.ReplaceCommand) (tenant.Replacement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replaced = append(f.replaced, c)
	if f.replaceErr != nil {
		return tenant.Replacement{}, f.replaceErr
	}
	return tenant.Replacement{
		Retired: tenant.Plaque{UID: c.RetiringUID, Status: tenant.PlaqueRetired,
			ReplacedBy: string(c.SuccessorUID), Canonical: true},
		Mounted: tenant.Plaque{UID: string(c.SuccessorUID), Status: tenant.PlaqueActive,
			Replaces: c.RetiringUID, Canonical: true},
	}, nil
}

func (f *fakePlaques) History(_ context.Context, _ uuid.UUID, uid string) ([]tenant.PlaqueEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.historyCalls++
	if f.historyErr != nil {
		return nil, f.historyErr
	}
	return f.history[uid], nil
}

func (f *fakePlaques) ConfirmPlaqueAct(_ context.Context, tenantID, actorID uuid.UUID, action, target string) (tenant.RemovalReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.confirmCalls++
	f.confirmSeen = append(f.confirmSeen, [4]string{
		tenantID.String(), actorID.String(), action, target,
	})
	if f.confirmErr != nil {
		return tenant.RemovalReceipt{}, f.confirmErr
	}
	r, ok := f.receipts[action+"|"+target]
	if !ok {
		return tenant.RemovalReceipt{}, nil
	}
	return r, nil
}

// plaqueConfirmations returns the query count and what each call was scoped to.
func (f *fakePlaques) plaqueConfirmations() (int, [][4]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][4]string, len(f.confirmSeen))
	copy(out, f.confirmSeen)
	return f.confirmCalls, out
}

// acts returns copies of what the section asked for, so an assertion reads the
// COMMAND the handler built rather than the form it was given.
func (f *fakePlaques) acts() ([]tenant.MountCommand, []tenant.ReplaceCommand) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := make([]tenant.MountCommand, len(f.mounted))
	copy(m, f.mounted)
	r := make([]tenant.ReplaceCommand, len(f.replaced))
	copy(r, f.replaced)
	return m, r
}

func (f *fakePlaques) unmounts() []tenant.UnmountCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]tenant.UnmountCommand, len(f.unmounted))
	copy(out, f.unmounted)
	return out
}

// fakeInviter mints nothing but DRIVES THE REAL CHANNEL, which is the point.
//
// 🔴 IT CALLS ch.DeliverInvite RATHER THAN RETURNING A LINK, because that is the
// only way the panel can obtain one and the shape under test is exactly that seam:
// invite.ManagerVisibleChannel writes the disclosure to audit_log and then hands the
// link to the sink. A fake that returned a string would test a mechanism the product
// does not have.
type fakeInviter struct {
	mu     sync.Mutex
	err    error
	link   string
	issued []invite.IssueParams
}

const fakeActivationLink = "https://panel.example/activate?code=FAKE-CODE-VALUE"

func (f *fakeInviter) IssueAndDeliver(ctx context.Context, p invite.IssueParams, ch invite.Channel) (invite.Invite, error) {
	f.mu.Lock()
	f.issued = append(f.issued, p)
	err, link := f.err, f.link
	f.mu.Unlock()
	if err != nil {
		return invite.Invite{}, err
	}
	if link == "" {
		link = fakeActivationLink
	}
	now := time.Now().UTC()
	inv := invite.Invite{
		ID: uuid.New(), TenantID: p.TenantID, EmployeeID: p.EmployeeID,
		CreatedAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour),
	}
	if err := ch.DeliverInvite(ctx, invite.Delivery{Invite: inv, ActivationURL: link}); err != nil {
		return inv, err
	}
	return inv, nil
}

func (f *fakeInviter) issuedParams() []invite.IssueParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]invite.IssueParams, len(f.issued))
	copy(out, f.issued)
	return out
}

func (f *fakeLedger) Screen(_ context.Context, tenantID uuid.UUID, filter ledger.Filter) (ledger.Screen, error) {
	f.record(tenantID, filter)
	if f.err != nil {
		return ledger.Screen{}, f.err
	}
	return ledger.Screen{Page: f.snapshot(filter), Options: f.opts}, nil
}

func (f *fakeLedger) Day(_ context.Context, tenantID uuid.UUID, filter ledger.Filter) (ledger.Page, error) {
	f.record(tenantID, filter)
	if f.err != nil {
		return ledger.Page{}, f.err
	}
	return f.snapshot(filter), nil
}

// snapshot resolves the day the way the real reader does, so the view under test
// sees a resolved date rather than a zero one.
func (f *fakeLedger) snapshot(filter ledger.Filter) ledger.Page {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.page
	if p.Zone == nil {
		p.Zone = time.UTC
	}
	if p.Day.Zero() {
		if !filter.Date.Zero() {
			p.Day = filter.Date
		} else {
			now := time.Now().In(p.Zone)
			p.Day = ledger.Date{Year: now.Year(), Month: now.Month(), Day: now.Day()}
		}
	}
	return p
}

func (f *fakeLedger) record(tenantID uuid.UUID, filter ledger.Filter) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, filter)
	f.tenant = append(f.tenant, tenantID)
}

func (f *fakeLedger) lastFilter(t *testing.T) ledger.Filter {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		t.Fatal("the ledger was never asked anything; the handler answered without querying")
	}
	return f.calls[len(f.calls)-1]
}

func adminTestConfig() *config.Config {
	return &config.Config{
		Env:            config.EnvDev,
		BaseURL:        testBaseURL,
		SessionHMACKey: []byte("0123456789abcdef0123456789abcdef"),
	}
}

// newAdminRouter wires the REAL handler with the REAL budgets and the REAL
// middleware, so a test drives what production drives (the M5-04 lesson: a test
// that builds its own limiter measures its own limiter).
func newAdminRouter(t *testing.T, admins *fakeAdmins, trail *fakeTrail) http.Handler {
	t.Helper()
	return newAdminRouterWithLedger(t, admins, trail, newFakeLedger())
}

// newAdminRouterWithLedger is the same wiring with a ledger the caller controls,
// for the tests that care what the transactions section was given.
func newAdminRouterWithLedger(t *testing.T, admins *fakeAdmins, trail *fakeTrail, records *fakeLedger) http.Handler {
	t.Helper()
	return newAdminRouterWithReviewer(t, admins, trail, records, &fakeReviewer{})
}

// newAdminRouterWithReviewer is the same wiring with the review queue's write side
// under the caller's control (M6-04). The SAME fakeLedger is passed as the day
// reader and as the queue reader, which is what production does with the one real
// *ledger.Reader.
func newAdminRouterWithReviewer(t *testing.T, admins *fakeAdmins, trail *fakeTrail, records *fakeLedger, reviewer panelReviewer) http.Handler {
	t.Helper()
	return newAdminRouterWithActions(t, admins, trail, records, reviewer, &fakeStaff{}, &fakeInviter{})
}

// newAdminRouterWithActions is the same wiring with the employees section's two
// write sides under the caller's control (M6-05 phase B). Every other helper in this
// file funnels through it, so a test drives the REAL router, the REAL middleware
// chain and the REAL budgets — the M5-04 lesson, which is that a test building its
// own limiter measures its own limiter.
func newAdminRouterWithActions(t *testing.T, admins *fakeAdmins, trail *fakeTrail, records *fakeLedger, reviewer panelReviewer, staff panelStaff, invites panelInviter) http.Handler {
	t.Helper()
	h, err := NewAdminAuth(admins, trail, records, records, reviewer, staff, invites, &fakeVenues{}, &fakePlaques{}, adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

// browser is a minimal cookie jar over httptest: it keeps whatever the handler
// sets and sends it back, which is what makes the two-step picker testable at all.
type browser struct {
	t       *testing.T
	h       http.Handler
	cookies map[string]string
	// ip is the source address, so a test can drive one account from many
	// addresses (the shape the per-account audit budget exists for).
	ip string
	// origin is what the browser claims; "" omits the header entirely.
	origin string
}

func newBrowser(t *testing.T, h http.Handler) *browser {
	return &browser{t: t, h: h, cookies: map[string]string{}, ip: "203.0.113.9:1234", origin: testBaseURL}
}

func (b *browser) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	b.t.Helper()
	var req *http.Request
	if form == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.RemoteAddr = b.ip
	if method == http.MethodPost && b.origin != "" {
		req.Header.Set("Origin", b.origin)
	}
	for name, value := range b.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	rec := httptest.NewRecorder()
	b.h.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.MaxAge < 0 || ck.Value == "" {
			delete(b.cookies, ck.Name)
			continue
		}
		b.cookies[ck.Name] = ck.Value
	}
	return rec
}

// doRaw posts a body this helper does NOT encode, so a test can drive a size the
// url.Values path would hide. Same cookies, same origin, same address as do().
func (b *browser) doRaw(method, path, body string) *httptest.ResponseRecorder {
	b.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = b.ip
	if method == http.MethodPost && b.origin != "" {
		req.Header.Set("Origin", b.origin)
	}
	for name, value := range b.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	rec := httptest.NewRecorder()
	b.h.ServeHTTP(rec, req)
	return rec
}

func htmlOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	res := rec.Result()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// csrfFrom lifts the synchronizer token out of a rendered form. It is the value
// the page is SUPPOSED to expose (it is not a section 4.7 secret), which is why a
// test may read it this way.
func csrfFrom(t *testing.T, html string) string {
	t.Helper()
	const marker = `name="csrf" value="`
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatalf("no csrf field in the rendered page")
	}
	rest := html[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatalf("malformed csrf field")
	}
	return rest[:j]
}

// ---------------------------------------------------------------------------
// OBLIGATION 1 -- IDENTICAL RESPONSE
// ---------------------------------------------------------------------------

// TestAdminLogin_ThreeFailuresAreByteIdentical is migration 00011's PHASE B
// OBLIGATION 1, measured at the byte level and in BOTH readings the brief asks
// for.
//
// READING A (the strong one): the three failures are driven through the SAME
// browser, so they carry the SAME login cookie and therefore the same synchronizer
// token. Their bodies must be equal with NO normalisation at all.
//
// READING B: three FRESH browsers, so the tokens differ. The bodies must be equal
// after normalising ONE NAMED FIELD -- the csrf value -- and nothing else. Naming
// it is the point: a test that normalised "anything that differs" would pass over
// a leaked reason.
func TestAdminLogin_ThreeFailuresAreByteIdentical(t *testing.T) {
	tenantID, adminID := uuid.New(), uuid.New()
	outcomes := map[string]adminauth.Authentication{
		"unknown email": {},
		"wrong password": {Resolved: 1, Attempts: []adminauth.Attempt{
			{AdminUserID: adminID, TenantID: tenantID, PasswordMatched: false, Active: true},
		}},
		"disabled admin, correct password": {Resolved: 1, Attempts: []adminauth.Attempt{
			{AdminUserID: adminID, TenantID: tenantID, PasswordMatched: true, Active: false},
		}},
	}
	order := []string{"unknown email", "wrong password", "disabled admin, correct password"}

	t.Run("same browser: identical with no normalisation", func(t *testing.T) {
		var current string
		admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
			return outcomes[current], adminauth.ErrBadCredentials
		}}
		b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
		page := htmlOf(t, b.do(http.MethodGet, "/admin/login", nil))
		csrf := csrfFrom(t, page)

		var bodies []string
		var statuses []int
		for _, name := range order {
			current = name
			rec := b.do(http.MethodPost, "/admin/login", url.Values{
				"csrf": {csrf}, "email": {"probe@example.test"}, "password": {"whatever"},
			})
			bodies = append(bodies, htmlOf(t, rec))
			statuses = append(statuses, rec.Code)
		}
		for i := 1; i < len(bodies); i++ {
			if statuses[i] != statuses[0] {
				t.Fatalf("%q answered %d, %q answered %d — the status distinguishes them",
					order[i], statuses[i], order[0], statuses[0])
			}
			if bodies[i] != bodies[0] {
				t.Fatalf("%q and %q differ by %d bytes.\n--- %s ---\n%s\n--- %s ---\n%s",
					order[0], order[i], len(bodies[i])-len(bodies[0]), order[0], bodies[0], order[i], bodies[i])
			}
		}
		if statuses[0] != http.StatusUnauthorized {
			t.Fatalf("status %d, want 401", statuses[0])
		}
		t.Logf("three outcomes, one browser: status %d, %d identical bytes each", statuses[0], len(bodies[0]))

		// Vacuity guard: the body must actually be the login page, not "".
		if !strings.Contains(bodies[0], "We could not sign you in") {
			t.Fatalf("the failure body does not carry the failure notice: %q", bodies[0])
		}
	})

	t.Run("fresh browsers: identical after normalising the csrf field only", func(t *testing.T) {
		var normalised []string
		for _, name := range order {
			outcome := outcomes[name]
			admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
				return outcome, adminauth.ErrBadCredentials
			}}
			b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
			page := htmlOf(t, b.do(http.MethodGet, "/admin/login", nil))
			csrf := csrfFrom(t, page)
			rec := b.do(http.MethodPost, "/admin/login", url.Values{
				"csrf": {csrf}, "email": {"probe@example.test"}, "password": {"whatever"},
			})
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s: status %d, want 401", name, rec.Code)
			}
			got := htmlOf(t, rec)
			// THE ONE NAMED NORMALISATION. The synchronizer token is per-cookie and
			// carries no information about the outcome.
			normalised = append(normalised, strings.ReplaceAll(got, csrfFrom(t, got), "<CSRF>"))
		}
		for i := 1; i < len(normalised); i++ {
			if normalised[i] != normalised[0] {
				t.Fatalf("%q and %q differ after normalising only the csrf field", order[0], order[i])
			}
		}
		if strings.Contains(normalised[0], "<CSRF>") != true {
			t.Fatalf("the normalisation did not fire — the token was not found in the body")
		}
		t.Logf("three outcomes, three browsers: identical after one named substitution (%d bytes)", len(normalised[0]))
	})
}

// TestAdminLogin_FailureBodyNamesNoCause is the tripwire for the SENTENCE rather
// than for the bytes: even if all three responses stayed identical, a body that
// said "no such account" would be an oracle the moment somebody added a second
// outcome.
func TestAdminLogin_FailureBodyNamesNoCause(t *testing.T) {
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{}, adminauth.ErrBadCredentials
	}}
	b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
	got := strings.ToLower(htmlOf(t, b.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {"probe@example.test"}, "password": {"whatever"},
	})))

	forbidden := []string{
		"no such", "not registered", "unknown email", "does not exist", "no account",
		"wrong password", "incorrect password", "disabled", "deactivated", "suspended",
		"locked", "too long", "not found",
	}
	for _, word := range forbidden {
		if strings.Contains(got, word) {
			t.Fatalf("the failure page says %q — that names a cause (PHASE B OBLIGATION 1)", word)
		}
	}
}

// ---------------------------------------------------------------------------
// OBLIGATION 3 -- RATE LIMIT AND THE AUDIT TRAIL
// ---------------------------------------------------------------------------

// TestAdminLogin_AttemptBudgetRefusesBeforeAnyPasswordWork is the CPU bound, and
// it measures the thing that matters: how many times the expensive call is
// REACHED, not how many 429s came back.
func TestAdminLogin_AttemptBudgetRefusesBeforeAnyPasswordWork(t *testing.T) {
	const requests = 25
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{}, adminauth.ErrBadCredentials
	}}
	trail := &fakeTrail{}
	b := newBrowser(t, newAdminRouter(t, admins, trail))
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))

	var refused, unauthorised int
	for i := 0; i < requests; i++ {
		rec := b.do(http.MethodPost, "/admin/login", url.Values{
			"csrf": {csrf}, "email": {"probe@example.test"}, "password": {"whatever"},
		})
		switch rec.Code {
		case http.StatusTooManyRequests:
			refused++
		case http.StatusUnauthorized:
			unauthorised++
		default:
			t.Fatalf("request %d answered %d", i, rec.Code)
		}
	}
	t.Logf("%d POSTs from one address: %d x 401, %d x 429; Authenticate reached %d times "+
		"(limit %d)", requests, unauthorised, refused, admins.authCalls, adminAttemptLimit)

	if admins.authCalls > adminAttemptLimit {
		t.Fatalf("the password path ran %d times against a budget of %d — the limiter is behind "+
			"the expensive call instead of in front of it", admins.authCalls, adminAttemptLimit)
	}
	if admins.authCalls != adminAttemptLimit {
		t.Fatalf("the password path ran %d times, want exactly %d (the budget must be spent, "+
			"not merely capped)", admins.authCalls, adminAttemptLimit)
	}
	if refused != requests-adminAttemptLimit {
		t.Fatalf("%d refusals, want %d", refused, requests-adminAttemptLimit)
	}
}

// 🔴 TestAdminLogin_RefusalsAreNotFreeAuditWrites is the M5-02 lesson, measured
// before it could be repeated: "a protection's COST can be an attack on the thing
// it protects". There, 300 requests produced 290 x 429 AND 300 permanent rows.
//
// THE NUMBER TO WATCH IS ROWS, NOT REFUSALS. audit_log is append-only at the
// database level -- not even tappa_owner can delete from it -- so an unbounded
// writer reachable by an anonymous caller is a denial of service against the
// trail section 4.6 exists to protect.
func TestAdminLogin_RefusalsAreNotFreeAuditWrites(t *testing.T) {
	const requests = 60
	tenantID, adminID := uuid.New(), uuid.New()
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{Resolved: 1, Attempts: []adminauth.Attempt{
			{AdminUserID: adminID, TenantID: tenantID, PasswordMatched: false, Active: true},
		}}, adminauth.ErrBadCredentials
	}}
	trail := &fakeTrail{}
	h := newAdminRouter(t, admins, trail)

	// EACH REQUEST FROM A DIFFERENT ADDRESS. That is the shape the per-account
	// budget exists for: the address budget would otherwise trip first and the
	// account budget would never be exercised. It is also the honest threat model
	// -- adminratelimit.go chooses NOT to gate on the account precisely because an
	// attacker has many addresses.
	var refused, unauthorised int
	for i := 0; i < requests; i++ {
		b := newBrowser(t, h)
		b.ip = fmt.Sprintf("198.51.100.%d:1234", i%254+1)
		csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
		rec := b.do(http.MethodPost, "/admin/login", url.Values{
			"csrf": {csrf}, "email": {"victim@example.test"}, "password": {"guess"},
		})
		switch rec.Code {
		case http.StatusTooManyRequests:
			refused++
		case http.StatusUnauthorized:
			unauthorised++
		default:
			t.Fatalf("request %d answered %d", i, rec.Code)
		}
	}

	failed := trail.count(ActionAdminLoginFailed)
	limited := trail.count(ActionAdminLoginLimited)
	t.Logf("%d failed sign-ins against ONE account from %d addresses: %d x 401, %d x 429; "+
		"audit rows = %d failed + %d rate_limited = %d total (account budget %d)",
		requests, requests, unauthorised, refused, failed, limited, trail.total(), adminAccountLimit)

	if failed > adminAccountLimit {
		t.Fatalf("%d audit rows for one account against a budget of %d — the trail is an "+
			"unbounded writer (M5-02)", failed, adminAccountLimit)
	}
	if limited != 1 {
		t.Fatalf("%d rate_limited rows, want exactly 1 (FirstOverLimit fires once per window)", limited)
	}
	if total := trail.total(); total >= requests {
		t.Fatalf("%d requests produced %d audit rows — the budget did nothing", requests, total)
	}
	// Vacuity guard: the trail must have written SOMETHING, or "bounded" is
	// satisfied by "broken".
	if failed == 0 {
		t.Fatalf("no failure was audited at all — the criterion 'failed sign-ins are written to " +
			"audit_log' is not met")
	}
}

// TestAdminLogin_UnknownEmailCannotBeAudited is the LIMIT, asserted rather than
// hidden.
//
// audit_log.tenant_id is NOT NULL with an FK to tenants (migration 00005), so an
// attempt against an address that resolves to nothing has no tenant to attribute
// it to and CANNOT be written. The M6-01 card says "failed sign-ins are rate
// limited and written to audit_log"; this test pins exactly how much of that is
// true, so nobody reads the criterion as fully met.
func TestAdminLogin_UnknownEmailCannotBeAudited(t *testing.T) {
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{}, adminauth.ErrBadCredentials
	}}
	trail := &fakeTrail{}
	b := newBrowser(t, newAdminRouter(t, admins, trail))
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
	rec := b.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {"nobody@example.test"}, "password": {"whatever"},
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rec.Code)
	}
	if got := trail.total(); got != 0 {
		t.Fatalf("%d audit rows for an unattributable attempt — audit_log.tenant_id is NOT NULL, "+
			"so a row should be impossible; if this now works, the LIMIT documented in "+
			"failLogin is stale", got)
	}
	t.Logf("unknown email: 0 audit rows (documented limit — no tenant to attribute the attempt to)")

	// POSITIVE CONTROL: the same handler DOES write a row when the address
	// resolves, so the zero above is about attributability and not about auditing
	// being switched off.
	tenantID, adminID := uuid.New(), uuid.New()
	admins.authenticate = func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{Resolved: 1, Attempts: []adminauth.Attempt{
			{AdminUserID: adminID, TenantID: tenantID, PasswordMatched: false, Active: true},
		}}, adminauth.ErrBadCredentials
	}
	b.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {"known@example.test"}, "password": {"whatever"},
	})
	if trail.count(ActionAdminLoginFailed) != 1 {
		t.Fatalf("control: a KNOWN address produced %d audit rows, want 1", trail.count(ActionAdminLoginFailed))
	}
}

// TestAdminLogin_DisabledAccountIsAuditedDifferently: the visitor is told nothing
// extra, but the owner's trail must be able to say "the correct password was used
// on a disabled account", which is a different security event from a guess.
func TestAdminLogin_DisabledAccountIsAuditedDifferently(t *testing.T) {
	tenantID, adminID := uuid.New(), uuid.New()
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{Resolved: 1, Attempts: []adminauth.Attempt{
			{AdminUserID: adminID, TenantID: tenantID, PasswordMatched: true, Active: false},
		}}, adminauth.ErrBadCredentials
	}}
	trail := &fakeTrail{}
	b := newBrowser(t, newAdminRouter(t, admins, trail))
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
	b.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {"disabled@example.test"}, "password": {"right"},
	})

	if trail.total() != 1 {
		t.Fatalf("%d audit rows, want 1", trail.total())
	}
	e := trail.events[0]
	if e.TenantID != tenantID {
		t.Fatalf("row written to tenant %v, want %v", e.TenantID, tenantID)
	}
	detail, ok := e.Detail.(adminLoginDetail)
	if !ok {
		t.Fatalf("detail is %T, want adminLoginDetail", e.Detail)
	}
	if !strings.Contains(detail.Reason, "disabled") {
		t.Fatalf("reason = %q, want it to name the disabled account", detail.Reason)
	}
}

// ---------------------------------------------------------------------------
// OBLIGATION 5 -- CANDIDATE <-> PASSWORD BINDING, AT THE HTTP LAYER
// ---------------------------------------------------------------------------

// 🔴 TestAdminChoose_RefusesATenantOutsideTheSignedSet is the cross-tenant
// authentication bypass, refused at the second step.
//
// The browser completes step 1 legitimately (two verified businesses), then posts
// a THIRD tenant id -- the shape the attack takes once an attacker knows the
// victim's tenant id. Nothing may be issued for it.
func TestAdminChoose_RefusesATenantOutsideTheSignedSet(t *testing.T) {
	verifiedA := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	verifiedB := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	victimTenant := uuid.New()

	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{Resolved: 2, Attempts: []adminauth.Attempt{
			{AdminUserID: verifiedA.AdminUserID, TenantID: verifiedA.TenantID, PasswordMatched: true, Active: true},
			{AdminUserID: verifiedB.AdminUserID, TenantID: verifiedB.TenantID, PasswordMatched: true, Active: true},
		}}, nil
	}}
	trail := &fakeTrail{}
	b := newBrowser(t, newAdminRouter(t, admins, trail))

	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
	rec := b.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {"owner@example.test"}, "password": {"right"},
	})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login/choose" {
		t.Fatalf("two verified businesses: status %d location %q, want 303 to the picker",
			rec.Code, rec.Header().Get("Location"))
	}
	if len(admins.issuedFor) != 0 {
		t.Fatalf("a session was issued before the business was chosen")
	}

	picker := htmlOf(t, b.do(http.MethodGet, "/admin/login/choose", nil))
	pickerCSRF := csrfFrom(t, picker)

	// CONTROL FIRST: the picker really does offer the two verified businesses, so
	// the refusal below is not passing because the picker was empty.
	for _, v := range []adminauth.Verified{verifiedA, verifiedB} {
		if !strings.Contains(picker, v.TenantID.String()) {
			t.Fatalf("control: the picker does not offer verified tenant %v", v.TenantID)
		}
	}
	if strings.Contains(picker, victimTenant.String()) {
		t.Fatalf("the picker offers a tenant nobody verified")
	}

	tests := []struct {
		name     string
		tenantID string
	}{
		{"a tenant that never resolved (the victim's)", victimTenant.String()},
		{"the nil uuid", uuid.Nil.String()},
		{"not a uuid at all", "../../etc/passwd"},
		{"empty", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := len(admins.issuedFor)
			rec := b.do(http.MethodPost, "/admin/login/choose", url.Values{
				"csrf": {pickerCSRF}, "tenant_id": {tc.tenantID},
			})
			if rec.Code == http.StatusSeeOther && rec.Header().Get("Location") == "/admin" {
				t.Fatalf("posting %q signed somebody in", tc.name)
			}
			if len(admins.issuedFor) != before {
				t.Fatalf("a session was ISSUED for %q — cross-tenant authentication bypass "+
					"(section 4.5): %+v", tc.name, admins.issuedFor[before:])
			}
		})
	}

	// The refusal of a REAL foreign tenant is loud: it writes a row, and it writes
	// it into a tenant we DID authenticate for -- never into the posted one, which
	// would be the cross-tenant write this endpoint refuses.
	if got := trail.count(ActionAdminLoginRefused); got == 0 {
		t.Fatalf("a refused cross-tenant choice left no audit row")
	}
	for _, e := range trail.events {
		if e.Action == ActionAdminLoginRefused && e.TenantID == victimTenant {
			t.Fatalf("the refusal wrote a row INTO the posted tenant — that is a write primitive " +
				"into another tenant's append-only audit_log")
		}
	}
}

// TestAdminChoose_AcceptsATenantInsideTheSignedSet is the POSITIVE CONTROL for the
// test above: the same flow with a legitimate choice must succeed, or the refusals
// prove only that the endpoint is broken.
func TestAdminChoose_AcceptsATenantInsideTheSignedSet(t *testing.T) {
	verifiedA := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	verifiedB := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{Resolved: 2, Attempts: []adminauth.Attempt{
			{AdminUserID: verifiedA.AdminUserID, TenantID: verifiedA.TenantID, PasswordMatched: true, Active: true},
			{AdminUserID: verifiedB.AdminUserID, TenantID: verifiedB.TenantID, PasswordMatched: true, Active: true},
		}}, nil
	}}
	trail := &fakeTrail{}
	b := newBrowser(t, newAdminRouter(t, admins, trail))
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
	b.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {"owner@example.test"}, "password": {"right"},
	})
	pickerCSRF := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login/choose", nil)))

	rec := b.do(http.MethodPost, "/admin/login/choose", url.Values{
		"csrf": {pickerCSRF}, "tenant_id": {verifiedB.TenantID.String()},
	})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
		t.Fatalf("status %d location %q, want 303 /admin", rec.Code, rec.Header().Get("Location"))
	}
	if len(admins.issuedFor) != 1 || admins.issuedFor[0] != verifiedB {
		t.Fatalf("issued for %+v, want exactly %+v", admins.issuedFor, verifiedB)
	}
	if trail.count(ActionAdminLoginSucceeded) != 1 {
		t.Fatalf("%d success rows, want 1", trail.count(ActionAdminLoginSucceeded))
	}
	// The short-lived cookies are spent.
	if _, ok := b.cookies[adminChoiceCookieName]; ok {
		t.Fatalf("the choice blob is still in the browser after it was spent")
	}
}

// TestAdminLogin_OneVerifiedBusinessSkipsThePicker: the ordinary path never mints
// the blob at all.
func TestAdminLogin_OneVerifiedBusinessSkipsThePicker(t *testing.T) {
	v := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{Resolved: 1, Attempts: []adminauth.Attempt{
			{AdminUserID: v.AdminUserID, TenantID: v.TenantID, PasswordMatched: true, Active: true},
		}}, nil
	}}
	b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
	rec := b.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {"owner@example.test"}, "password": {"right"},
	})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
		t.Fatalf("status %d location %q, want 303 /admin", rec.Code, rec.Header().Get("Location"))
	}
	if len(admins.issuedFor) != 1 || admins.issuedFor[0] != v {
		t.Fatalf("issued for %+v, want %+v", admins.issuedFor, v)
	}
	if _, ok := b.cookies[adminChoiceCookieName]; ok {
		t.Fatalf("a choice blob was minted for a single-business login")
	}
	// And the session cookie really was set, under the panel's own name and path.
	if _, ok := b.cookies[adminauth.CookieName]; !ok {
		t.Fatalf("no panel session cookie was written")
	}
}

// TestAdminChoose_RefusesABlobFromAnotherBrowser is the MAC binding: a blob is
// bound to the `bind` half of the login cookie, so lifting it into another browser
// (a shared machine, a copied profile) does not spend it.
func TestAdminChoose_RefusesABlobFromAnotherBrowser(t *testing.T) {
	verifiedA := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	verifiedB := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{Resolved: 2, Attempts: []adminauth.Attempt{
			{AdminUserID: verifiedA.AdminUserID, TenantID: verifiedA.TenantID, PasswordMatched: true, Active: true},
			{AdminUserID: verifiedB.AdminUserID, TenantID: verifiedB.TenantID, PasswordMatched: true, Active: true},
		}}, nil
	}}
	h := newAdminRouter(t, admins, &fakeTrail{})

	victim := newBrowser(t, h)
	csrf := csrfFrom(t, htmlOf(t, victim.do(http.MethodGet, "/admin/login", nil)))
	victim.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {"owner@example.test"}, "password": {"right"},
	})
	blob := victim.cookies[adminChoiceCookieName]
	if blob == "" {
		t.Fatalf("control: no choice blob was minted")
	}

	// A different browser: its own login cookie (its own bind), the STOLEN blob.
	thief := newBrowser(t, h)
	thiefCSRF := csrfFrom(t, htmlOf(t, thief.do(http.MethodGet, "/admin/login", nil)))
	thief.cookies[adminChoiceCookieName] = blob

	before := len(admins.issuedFor)
	rec := thief.do(http.MethodPost, "/admin/login/choose", url.Values{
		"csrf": {thiefCSRF}, "tenant_id": {verifiedA.TenantID.String()},
	})
	if rec.Code == http.StatusSeeOther && rec.Header().Get("Location") == "/admin" {
		t.Fatalf("a blob minted for another browser was spent")
	}
	if len(admins.issuedFor) != before {
		t.Fatalf("a session was issued from a blob bound to a different browser")
	}
}

// TestAdminAuth_RefusesForgedAndCrossSiteRequests is the table of every guard on
// the two state-changing POSTs.
func TestAdminAuth_RefusesForgedAndCrossSiteRequests(t *testing.T) {
	v := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	newRouter := func() (http.Handler, *fakeAdmins) {
		admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
			return adminauth.Authentication{Resolved: 1, Attempts: []adminauth.Attempt{
				{AdminUserID: v.AdminUserID, TenantID: v.TenantID, PasswordMatched: true, Active: true},
			}}, nil
		}}
		return newAdminRouter(t, admins, &fakeTrail{}), admins
	}

	tests := []struct {
		name       string
		origin     string
		csrf       func(real string) string
		dropCookie bool
		wantStatus int
	}{
		{"a foreign Origin", "https://evil.example", func(s string) string { return s }, false, http.StatusBadRequest},
		{"no Origin and no fetch metadata", "", func(s string) string { return s }, false, http.StatusBadRequest},
		{"a forged synchronizer token", testBaseURL, func(string) string { return "not-the-token" }, false, http.StatusBadRequest},
		{"no synchronizer token", testBaseURL, func(string) string { return "" }, false, http.StatusBadRequest},
		{"no login cookie at all", testBaseURL, func(s string) string { return s }, true, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, admins := newRouter()
			b := newBrowser(t, h)
			csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
			if tc.dropCookie {
				delete(b.cookies, adminLoginCookieName)
			}
			b.origin = tc.origin
			rec := b.do(http.MethodPost, "/admin/login", url.Values{
				"csrf": {tc.csrf(csrf)}, "email": {"owner@example.test"}, "password": {"right"},
			})
			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d", rec.Code, tc.wantStatus)
			}
			if len(admins.issuedFor) != 0 {
				t.Fatalf("a session was issued despite %s", tc.name)
			}
			if admins.authCalls != 0 {
				t.Fatalf("the password path ran %d times despite %s — the guard is behind the "+
					"expensive call", admins.authCalls, tc.name)
			}
		})
	}

	// POSITIVE CONTROL: the same request with everything correct succeeds, so the
	// refusals above are about the guards and not about the endpoint being broken.
	t.Run("control: everything correct", func(t *testing.T) {
		h, admins := newRouter()
		b := newBrowser(t, h)
		csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
		rec := b.do(http.MethodPost, "/admin/login", url.Values{
			"csrf": {csrf}, "email": {"owner@example.test"}, "password": {"right"},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status %d, want 303", rec.Code)
		}
		if len(admins.issuedFor) != 1 {
			t.Fatalf("no session was issued on the happy path")
		}
	})
}

// ---------------------------------------------------------------------------
// THE GATE
// ---------------------------------------------------------------------------

// TestAdminHome_IsBehindTheGate, in BOTH directions: no cookie and an EMPLOYEE
// cookie are equally refused, and only a resolving panel cookie gets through.
func TestAdminHome_IsBehindTheGate(t *testing.T) {
	adminID, tenantID, sessionID := uuid.New(), uuid.New(), uuid.New()

	tests := []struct {
		name       string
		cookies    map[string]string
		verify     func() (adminauth.Resolved, error)
		wantStatus int
	}{
		{
			name:       "no cookie at all",
			cookies:    nil,
			verify:     func() (adminauth.Resolved, error) { return adminauth.Resolved{}, adminauth.ErrNoSession },
			wantStatus: http.StatusSeeOther,
		},
		{
			name:       "an EMPLOYEE session cookie",
			cookies:    map[string]string{session.CookieName: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
			verify:     func() (adminauth.Resolved, error) { return adminauth.Resolved{}, adminauth.ErrNoSession },
			wantStatus: http.StatusSeeOther,
		},
		{
			name:       "a panel cookie that resolves to nothing",
			cookies:    map[string]string{adminauth.CookieName: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
			verify:     func() (adminauth.Resolved, error) { return adminauth.Resolved{}, adminauth.ErrNoSession },
			wantStatus: http.StatusSeeOther,
		},
		{
			name:       "a database outage",
			cookies:    map[string]string{adminauth.CookieName: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
			verify:     func() (adminauth.Resolved, error) { return adminauth.Resolved{}, fmt.Errorf("connection refused") },
			wantStatus: http.StatusSeeOther,
		},
		{
			name:    "a live panel session",
			cookies: map[string]string{adminauth.CookieName: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
			verify: func() (adminauth.Resolved, error) {
				return adminauth.Resolved{
					SessionID: sessionID, TenantID: tenantID, AdminUserID: adminID,
					Role: "owner", FullName: "KF Owner",
				}, nil
			},
			wantStatus: http.StatusOK,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			admins := &fakeAdmins{verify: tc.verify}
			b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
			for name, value := range tc.cookies {
				b.cookies[name] = value
			}
			rec := b.do(http.MethodGet, "/admin", nil)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusSeeOther {
				if got := rec.Header().Get("Location"); got != "/admin/login" {
					t.Fatalf("location %q, want /admin/login", got)
				}
			} else if !strings.Contains(htmlOf(t, rec), "KF Owner") {
				t.Fatalf("the panel page does not name the signed-in operator")
			}
		})
	}
}

// TestAdminLogout_RevokesAndClears.
func TestAdminLogout_RevokesAndClears(t *testing.T) {
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: uuid.New(), TenantID: uuid.New(), AdminUserID: uuid.New(),
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	trail := &fakeTrail{}
	b := newBrowser(t, newAdminRouter(t, admins, trail))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	rec := b.do(http.MethodPost, "/admin/logout", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
	if admins.revokeCalls != 1 {
		t.Fatalf("Revoke called %d times, want 1 — clearing the cookie without revoking leaves a "+
			"live session behind a browser that thinks it signed out", admins.revokeCalls)
	}
	if _, ok := b.cookies[adminauth.CookieName]; ok {
		t.Fatalf("the panel cookie survived sign-out")
	}
	if trail.count(ActionAdminLoggedOut) != 1 {
		t.Fatalf("%d sign-out rows, want 1", trail.count(ActionAdminLoggedOut))
	}

	t.Run("a cross-site sign-out is refused", func(t *testing.T) {
		b2 := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
		b2.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		b2.origin = "https://evil.example"
		before := admins.revokeCalls
		b2.do(http.MethodPost, "/admin/logout", url.Values{})
		if admins.revokeCalls != before {
			t.Fatalf("a cross-origin POST signed somebody out")
		}
	})
}

// TestAdminScreens_CarryTheSecurityHeaders. The activation family does NOT set a
// CSP (a known M5-02 asymmetry); these screens carry PASSWORD FORMS, so the new
// surface starts with the header rather than inheriting the gap.
func TestAdminScreens_CarryTheSecurityHeaders(t *testing.T) {
	v := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	admins := &fakeAdmins{
		authenticate: func(string, string) (adminauth.Authentication, error) {
			return adminauth.Authentication{Resolved: 1, Attempts: []adminauth.Attempt{
				{AdminUserID: v.AdminUserID, TenantID: v.TenantID, PasswordMatched: true, Active: true},
			}}, nil
		},
		verify: func() (adminauth.Resolved, error) {
			return adminauth.Resolved{SessionID: uuid.New(), TenantID: v.TenantID,
				AdminUserID: v.AdminUserID, Role: "owner", FullName: "KF Owner"}, nil
		},
	}
	b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	for _, path := range []string{"/admin/login", "/admin"} {
		t.Run(path, func(t *testing.T) {
			rec := b.do(http.MethodGet, path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d", rec.Code)
			}
			h := rec.Header()
			csp := h.Get("Content-Security-Policy")
			for _, want := range []string{"default-src 'none'", "form-action 'self'", "frame-ancestors 'none'", "base-uri 'none'"} {
				if !strings.Contains(csp, want) {
					t.Fatalf("CSP %q is missing %q", csp, want)
				}
			}
			// 🔴 THE CLAIM IS NOW A CORRESPONDENCE, NOT A PROHIBITION, and it is
			// STRICTLY STRONGER than the sentence it replaces.
			//
			// This used to read "script-src must never appear". M6-03 gave the
			// transactions section a script (vendored HTMX, for paging), so the flat
			// prohibition had to go — and replacing it with nothing, or with an
			// exemption for /admin, would have retired the only thing keeping the
			// policy honest. What is asserted instead is the PROPERTY the flat rule
			// was a special case of: a page names script-src IF AND ONLY IF it
			// actually loads a script.
			//
			// It catches both directions, which the old one could not: a page that
			// widens the policy for a script it does not load, AND a page that loads
			// a script the policy does not permit (which would be a broken page
			// shipped green).
			loadsScript := strings.Contains(strings.ToLower(htmlOf(t, rec)), "<script")
			namesScript := strings.Contains(csp, "script-src")
			switch {
			case namesScript && !loadsScript:
				t.Fatalf("CSP names script-src on a page that loads no script: %q", csp)
			case loadsScript && !namesScript:
				t.Fatalf("the page loads a script but its CSP does not permit one: %q", csp)
			case loadsScript && !strings.Contains(csp, "connect-src"):
				// htmx pages with XMLHttpRequest and connect-src falls back to
				// default-src, which is 'none'. A scripted page without it loads the
				// library and then has every request it makes blocked.
				t.Fatalf("a scripted panel page names no connect-src, so its own "+
					"requests would be refused by default-src 'none': %q", csp)
			}
			for _, never := range []string{"unsafe-inline", "unsafe-eval"} {
				if strings.Contains(csp, never) {
					t.Fatalf("CSP allows %s: %q", never, csp)
				}
			}
			if got := h.Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control %q, want no-store", got)
			}
			if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Fatalf("X-Content-Type-Options %q", got)
			}
		})
	}
}

// TestAdminScreens_NeverRenderACredential. The login form takes a password and an
// email; neither may come back in the body, and no page may carry a token.
func TestAdminScreens_NeverRenderACredential(t *testing.T) {
	const probeEmail = "PROBE-EMAIL@example.test"
	const probePassword = "PROBE-PASSWORD-9f2c"

	v1 := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	v2 := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{Resolved: 2, Attempts: []adminauth.Attempt{
			{AdminUserID: v1.AdminUserID, TenantID: v1.TenantID, PasswordMatched: true, Active: true},
			{AdminUserID: v2.AdminUserID, TenantID: v2.TenantID, PasswordMatched: true, Active: true},
		}}, nil
	}}
	b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))

	screens := map[string]string{}
	screens["login form"] = htmlOf(t, b.do(http.MethodGet, "/admin/login", nil))
	csrf := csrfFrom(t, screens["login form"])
	b.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {probeEmail}, "password": {probePassword},
	})
	screens["picker"] = htmlOf(t, b.do(http.MethodGet, "/admin/login/choose", nil))

	// And a failure render, which is the one that would most naturally echo the
	// email back into the form.
	admins.authenticate = func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{}, adminauth.ErrBadCredentials
	}
	b2 := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
	csrf2 := csrfFrom(t, htmlOf(t, b2.do(http.MethodGet, "/admin/login", nil)))
	screens["failed login"] = htmlOf(t, b2.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf2}, "email": {probeEmail}, "password": {probePassword},
	}))

	blob := b.cookies[adminChoiceCookieName]
	for name, html := range screens {
		t.Run(name, func(t *testing.T) {
			if html == "" {
				t.Fatalf("rendered nothing — the assertions below are vacuous")
			}
			if strings.Contains(html, probePassword) {
				t.Fatalf("the page echoes the PASSWORD")
			}
			if strings.Contains(html, probeEmail) {
				t.Fatalf("the page echoes the email address back into the body")
			}
			if blob != "" && strings.Contains(html, blob) {
				t.Fatalf("the page carries the signed choice blob, which lives in an HttpOnly cookie")
			}
			if strings.Contains(html, adminauth.CookieName) {
				t.Fatalf("the page names the session cookie")
			}
		})
	}
}

// TestAdminRoutes_AreAllUnderTheCookiePath. The panel cookie is Path=/admin, so a
// route outside that prefix would arrive with no cookie and look permanently
// signed out. This is a structural tripwire for the next person adding a route.
func TestAdminRoutes_AreAllUnderTheCookiePath(t *testing.T) {
	nilCfgLedger := newFakeLedger()
	h, err := NewAdminAuth(&fakeAdmins{}, &fakeTrail{}, nilCfgLedger, nilCfgLedger, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)

	var routes []string
	err = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, method+" "+route)
		// ⚠️ THE PREFIX TEST ALONE IS VACUOUS FOR CookiePath == "/", which an audit
		// measured: every route starts with "/", so the mutation that widened the
		// constant left this green. The guard below removes that hole here, and the
		// BEHAVIOURAL net lives in admincookiepath_test.go.
		if adminauth.CookiePath == "/" {
			t.Fatalf("adminauth.CookiePath is %q — this check is vacuous and every panel "+
				"cookie reaches the tap surface", adminauth.CookiePath)
		}
		if !strings.HasPrefix(route, adminauth.CookiePath) {
			t.Errorf("route %s %s is outside %s — the panel cookie would not be sent to it",
				method, route, adminauth.CookiePath)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if len(routes) == 0 {
		t.Fatalf("no routes were mounted — the assertion is vacuous")
	}
	t.Logf("panel routes (%d): %s", len(routes), strings.Join(routes, ", "))
}

// TestAdminIdentity_ZeroValueIsNotAuthenticated is the polarity lesson for the
// panel's identity type: the state you get by DOING NOTHING must be the harmless
// one.
func TestAdminIdentity_ZeroValueIsNotAuthenticated(t *testing.T) {
	var id httpx.AdminIdentity
	if id.Live() {
		t.Fatalf("the zero AdminIdentity reports Live()")
	}
	if id.TenantID() != uuid.Nil || id.AdminUserID() != uuid.Nil {
		t.Fatalf("the zero AdminIdentity carries ids")
	}
	// And a request that never went through the middleware resolves the same way.
	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	if httpx.AdminOf(r).Live() {
		t.Fatalf("a request with no middleware reports an authenticated admin")
	}
}

// discardLogger is the shared silent logger for the panel tests.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// TestAdminChoose_BlobIsNotSingleUse pins a MEASURED behaviour that a comment had
// implied away.
//
// An audit put the two cookies back after a completed sign-in and posted a
// DIFFERENT business from the same signed set: 303, and the session count went
// 1 -> 2. The comments said "the one path that SPENDS them" and "can finish the
// login" (singular), both of which read as single-use. They are corrected; this
// test is what keeps the correction true.
//
// 🔴 WHAT IT ALSO PINS IS THE BOUNDARY THAT MATTERS: a replay may only ever reach
// a business inside the SAME signed set. The second half of this test posts a
// tenant that is NOT in the set, using the very same replayed cookies, and it must
// still be refused — the set never widens, which is PHASE B OBLIGATION 5 and is
// the reason the replay is a wording bug rather than an escalation.
func TestAdminChoose_BlobIsNotSingleUse(t *testing.T) {
	v1 := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	v2 := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	outsider := uuid.New()

	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{Resolved: 2, Attempts: []adminauth.Attempt{
			{AdminUserID: v1.AdminUserID, TenantID: v1.TenantID, PasswordMatched: true, Active: true},
			{AdminUserID: v2.AdminUserID, TenantID: v2.TenantID, PasswordMatched: true, Active: true},
		}}, nil
	}}
	b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))

	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
	b.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {"owner@example.test"}, "password": {"right"},
	})
	picker := htmlOf(t, b.do(http.MethodGet, "/admin/login/choose", nil))
	pickerCSRF := csrfFrom(t, picker)

	// Keep a copy of the two short-lived cookies BEFORE they are cleared.
	keptLogin := b.cookies[adminLoginCookieName]
	keptChoice := b.cookies[adminChoiceCookieName]
	if keptLogin == "" || keptChoice == "" {
		t.Fatalf("control: the short-lived cookies were not set (login=%q choice=%q)", keptLogin, keptChoice)
	}

	rec := b.do(http.MethodPost, "/admin/login/choose", url.Values{
		"csrf": {pickerCSRF}, "tenant_id": {v1.TenantID.String()},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("control: the first choice did not sign in (%d)", rec.Code)
	}
	if len(admins.issuedFor) != 1 {
		t.Fatalf("control: %d sessions issued, want 1", len(admins.issuedFor))
	}
	// completeLogin cleared them, so a browser that obeyed the headers is done.
	if _, ok := b.cookies[adminChoiceCookieName]; ok {
		t.Fatalf("control: the choice cookie was not cleared on completion")
	}

	// REPLAY: a client that kept a copy puts them back.
	b.cookies[adminLoginCookieName] = keptLogin
	b.cookies[adminChoiceCookieName] = keptChoice

	rec = b.do(http.MethodPost, "/admin/login/choose", url.Values{
		"csrf": {pickerCSRF}, "tenant_id": {v2.TenantID.String()},
	})
	replayWorked := rec.Code == http.StatusSeeOther && rec.Header().Get("Location") == "/admin"
	t.Logf("replay of a completed choice: status %d, sessions issued %d -> %d",
		rec.Code, 1, len(admins.issuedFor))
	if !replayWorked || len(admins.issuedFor) != 2 {
		t.Fatalf("the blob behaved as single-use (status %d, %d sessions). That may be an "+
			"improvement, but logincontext.go and completeLogin both document the OPPOSITE "+
			"as measured behaviour — update those comments together with this test",
			rec.Code, len(admins.issuedFor))
	}
	if admins.issuedFor[1] != v2 {
		t.Fatalf("the replay issued for %+v, want %+v", admins.issuedFor[1], v2)
	}

	// 🔴 THE BOUNDARY: the same replayed cookies cannot reach OUTSIDE the set.
	b.cookies[adminLoginCookieName] = keptLogin
	b.cookies[adminChoiceCookieName] = keptChoice
	before := len(admins.issuedFor)
	rec = b.do(http.MethodPost, "/admin/login/choose", url.Values{
		"csrf": {pickerCSRF}, "tenant_id": {outsider.String()},
	})
	if rec.Code == http.StatusSeeOther && rec.Header().Get("Location") == "/admin" {
		t.Fatalf("a replayed blob reached a tenant OUTSIDE the verified set")
	}
	if len(admins.issuedFor) != before {
		t.Fatalf("a replayed blob issued a session for an unverified tenant: %+v",
			admins.issuedFor[before:])
	}
}

// TestAdminLogin_FullSizeVerifiedSetReachesThePicker drives the auditor's probe
// through the REAL handler: the largest verified set the bcrypt loop can produce
// must reach the picker, not a 500.
//
// It is written in terms of adminauth.MaxCandidates rather than a literal ON
// PURPOSE — here the point is that the handler tracks whatever the cap is, and the
// cap's VALUE is pinned in internal/adminauth (TestMaxCandidates_IsTheMeasuredCPUBound).
func TestAdminLogin_FullSizeVerifiedSetReachesThePicker(t *testing.T) {
	n := adminauth.MaxCandidates
	attempts := make([]adminauth.Attempt, 0, n)
	for i := 0; i < n; i++ {
		attempts = append(attempts, adminauth.Attempt{
			AdminUserID: uuid.New(), TenantID: uuid.New(),
			PasswordMatched: true, Active: true,
		})
	}
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{Resolved: n, Attempts: attempts}, nil
	}}
	b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
	rec := b.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {"owner@example.test"}, "password": {"right"},
	})
	t.Logf("%d verified candidates -> status %d, Location %q",
		n, rec.Code, rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login/choose" {
		t.Fatalf("a verified set at the cap (%d) answered %d %q, want 303 to the picker. "+
			"This is the shape that broke when the handler carried its own copy of the bound",
			n, rec.Code, rec.Header().Get("Location"))
	}
	// The picker really lists all of them.
	picker := htmlOf(t, b.do(http.MethodGet, "/admin/login/choose", nil))
	for _, a := range attempts {
		if !strings.Contains(picker, a.TenantID.String()) {
			t.Fatalf("the picker omits verified tenant %v", a.TenantID)
		}
	}
}

// TestPanelConstants_ShippedValuesArePinned pins the VALUES of the panel's
// budgets and lifetimes, not the mechanisms that use them.
//
// 🔴 WHY IT EXISTS: A FAMILY SCAN, and it is the real lesson of this round. An
// audit found that adminauth.MaxCandidates could be changed alone with the whole
// suite staying green, because every expectation was written in terms of the
// constant itself (which was unexported at the time) rather than as a literal.
// Scanning the other 15 constants this feature introduced found SEVEN more in the
// same state — including four rate-limit budgets whose numbers ARE the product
// decision (adminratelimit.go is nothing but an argument about them).
//
// Each row carries its own justification, so changing a number forces re-deriving
// it rather than updating a fixture. Where a constant is DERIVED from another, it
// is asserted as a relation instead — those cannot drift by construction.
func TestPanelConstants_ShippedValuesArePinned(t *testing.T) {
	tests := []struct {
		name string
		got  int
		want int
		why  string
	}{
		{
			"adminFloodLimit", adminFloodLimit, 3000,
			"RE-DERIVED in round 14 and MEASURED by M6-02: this shield covers the AUTHENTICATED " +
				"routes too, so it carries every page load of every admin behind one address. The " +
				"round-14 estimate was 10 admins x ~20 page views x ~10 HTMX fragments = ~2000 per " +
				"window; M6-02 shipped the dashboard with NO HTMX (plain <a href> sections) and " +
				"measured 1 request per view over real HTTP. Both headroom figures over the SAME " +
				"denominator (page requests PLUS logins, which is how the 1.46x was computed): " +
				"old premise 2000+60 = ~2060 -> 3000/2060 = 1.46x; measured 200+60 = ~260 -> " +
				"3000/260 = 11.5x. It matches httpx.tapAddressLimit " +
				"deliberately. It does NOT bound bcrypt — adminAttemptLimit does. What it bounds " +
				"is DATABASE work, and the cost was re-measured on the RIGHT arm in round 16: an " +
				"authenticated page load is 3.0-5.7 ms (resolver read + TouchAdminSession UPDATE), " +
				"not the 1.5 ms of an invented token, so 3000 buys 9-17 s per window per address " +
				"= 1.5-2.9% of one core. M6-03 brings the fragments and re-counts.",
		},
		{
			"adminSessionLimit", adminSessionLimit, 300,
			"the THIRD stage, keyed on the session UUID (never the token or its hash). ⚠️ It " +
				"bounds work AFTER authentication, NOT the resolver read or the UPDATE — those " +
				"are already paid when it refuses (measured in round 16). It was COPIED from " +
				"httpx.tapSessionLimit rather than derived; M6-02 DERIVED it and the number " +
				"survived. A section view is 1 charged request (measured: 300 x 200 then 429 at " +
				"#301, stylesheet served outside Protect and not charged), so 300 is 300 section " +
				"views per window and the headroom against 20 views is ~15x, not the 1.5x this " +
				"string used to claim. Threshold: it becomes binding at >=15 requests per view, " +
				"which is what M6-03 has to re-count when HTMX fragments arrive.",
		},
		{
			"adminAttemptLimit", adminAttemptLimit, 10,
			"THE CPU BOUND: 10 failures x adminauth.MaxCandidates (8) x ~380 ms = ~30 s of CPU " +
				"per 10-minute window per address, about 5% of one core. Raising it raises that " +
				"linearly — see adminauth.MaxCandidates, which carries the other half.",
		},
		{
			"adminAccountLimit", adminAccountLimit, 10,
			"bounds audit_log writes per account per window and gates NOTHING (a per-account " +
				"gate would be an account lockout an attacker triggers with a known email). " +
				"60 failures from 60 addresses produce 11 rows, measured.",
		},
		{
			"adminLogoutLimit", adminLogoutLimit, 10 * adminFloodLimit,
			"sign-out's OWN address ceiling, deliberately ten times the panel's. It exists " +
				"because exempting sign-out from refusal entirely (round 14) made it UNBOUNDED, " +
				"not merely unbudgeted: 10000 anonymous sign-outs produced 10000 resolver reads " +
				"and zero refusals. It is derived FROM adminFloodLimit on purpose, so widening " +
				"the panel widens sign-out with it.",
		},
		{
			"adminLoginCookieMaxAge", adminLoginCookieMaxAge, 15 * 60,
			"long enough to read a form and type a password, short enough that a synchronizer " +
				"token left on a shared machine goes stale on its own.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("%s is %d, and this test pins %d.\nThis is not a fixture to update: "+
					"%s\nChange the literal here together with the argument in "+
					"adminratelimit.go / logincontext.go.", tc.name, tc.got, tc.want, tc.why)
			}
		})
	}

	// Durations, same rule.
	durations := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"adminFloodPeriod", adminFloodPeriod, 10 * time.Minute},
		{"adminAttemptPeriod", adminAttemptPeriod, 10 * time.Minute},
		{"adminAccountPeriod", adminAccountPeriod, 10 * time.Minute},
		{"adminSessionPeriod", adminSessionPeriod, 10 * time.Minute},
		{"adminLogoutPeriod", adminLogoutPeriod, 10 * time.Minute},
		{"adminChoiceTTL", adminChoiceTTL, 5 * time.Minute},
		{"adminChoiceFutureSkew", adminChoiceFutureSkew, time.Minute},
	}
	for _, tc := range durations {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("%s is %v, and this test pins %v. Every budget window is 10 minutes so "+
					"the three limits compose into one arithmetic; adminChoiceTTL is a BEARER "+
					"credential lifetime and adminChoiceFutureSkew tolerates a clock step "+
					"backwards. Re-derive before changing.", tc.name, tc.got, tc.want)
			}
		})
	}

	// RELATIONS — these cannot drift, and asserting them keeps anyone from turning
	// a derived constant back into a literal (which is how MaxCandidates and
	// adminChoiceMaxEntries came apart).
	t.Run("adminChoiceCookieMaxAge tracks adminChoiceTTL", func(t *testing.T) {
		if want := int(adminChoiceTTL / time.Second); adminChoiceCookieMaxAge != want {
			t.Fatalf("adminChoiceCookieMaxAge = %d s but adminChoiceTTL = %v (%d s). A blob the "+
				"server refuses must not sit in the browser looking usable — derive one from "+
				"the other rather than writing two numbers",
				adminChoiceCookieMaxAge, adminChoiceTTL, want)
		}
	})
}

// TestAdminAuth_FloodCeilingRefusesEveryUnauthenticatedRoute closes the panel half
// of a debt state.md has carried since M5-07: the flood budgets had no test at all
// (`grep floodLimiter internal/handler/*_test.go` -> 0 hits), and M6-01 nearly
// doubled the number of routes relying on them.
//
// MEASURED BEFORE WRITING: this is ONE table test, four subtests, 0.01 s, and it
// needs no new harness — the existing browser drives it. That is why it is closed
// here rather than handed on.
//
// EACH ROUTE GETS A FRESH SOURCE ADDRESS, because the flood budget is per address
// and SHARED across routes: without that, the second row would start with the
// first row's counter already spent and the table would measure nothing.
//
// 🔴 IT COVERS EVERY PANEL ROUTE THAT MAY BE REFUSED — five of the six, the sixth
// being sign-out, which is metered but never refused and has its own invariant test
// (TestLogoutGate_BoundsTheAmplifierWithoutBreakingTheInvariant).
//
// ⚠️ THIS COMMENT SAID "ALL SIX PANEL ROUTES, INCLUDING THE TWO BEHIND
// RequireAdmin" while the table held five rows, one of them behind RequireAdmin,
// and while its own next paragraph explained that sign-out was deliberately
// excluded — a sentence contradicting itself three lines apart. It was written
// when the table did carry six, and round 16 removed the sign-out row without
// re-reading the heading.
//
// The table used to carry four routes under a comment
// saying the other two were "not anonymous — each needs a cookie that resolves to
// a live session", so the exposure supposedly required a STOLEN cookie. A closing
// security audit measured that claim false: adminauth.Token.hash applies only a
// SHAPE gate (43 chars, base64url), so an INVENTED token reaches the SECURITY
// DEFINER resolver. Measured, 200 requests per shape against GET /admin:
//
//	no cookie at all                     med 5.9 us   (never reaches the DB)
//	well-formed INVENTED 43-char token   med 1.56 ms  (a REAL resolver read)
//	600 requests, ZERO 429s
//
// The threat model was therefore "anyone", not "somebody with a stolen cookie".
// Both routes now sit behind a.floodGate, mounted AHEAD of Protect — the order
// tap.go uses and httpx/router.go defends.
func TestAdminAuth_FloodCeilingRefusesEveryUnauthenticatedRoute(t *testing.T) {
	routes := []struct {
		name, method, path string
		form               url.Values
	}{
		{"GET /admin/login", http.MethodGet, "/admin/login", nil},
		{"POST /admin/login", http.MethodPost, "/admin/login", url.Values{}},
		{"GET /admin/login/choose", http.MethodGet, "/admin/login/choose", nil},
		{"POST /admin/login/choose", http.MethodPost, "/admin/login/choose", url.Values{}},
		{"GET /admin (behind Protect)", http.MethodGet, "/admin", nil},
	}
	// ⚠️ POST /admin/logout IS DELIBERATELY ABSENT and it is not an omission: it is
	// METERED but never REFUSED, because a third party must not be able to stop
	// somebody ending their own session. TestAdminLogout_CannotBeBlockedByAThirdParty
	// is the invariant that replaces a row here.

	h := newAdminRouter(t, &fakeAdmins{}, &fakeTrail{})

	// ⚠️ THE BUDGET IS BURNT ONCE, NOT ONCE PER ROUTE, and the reason is cost. The
	// ceiling moved from 200 to 3000 in round 14 (adminratelimit.go re-derives it),
	// and driving it separately for every row meant ~15000 request round trips
	// under -race — `make test-short`, whose whole purpose is speed, went from
	// ~40 s to ~66 s. Burning one address once and then asking every route the same
	// question is O(ceiling + routes) instead of O(ceiling x routes), and it proves
	// MORE rather than less: a route that is NOT behind the gate answers normally
	// even when the address is exhausted.
	burner := newBrowser(t, h)
	burner.ip = "198.51.100.1:1234"
	for i := 1; i <= adminFloodLimit+1; i++ {
		code := burner.do(http.MethodGet, "/admin/login", nil).Code
		// POSITIVE CONTROL on every request up to the ceiling: the budget must not
		// refuse early, or "everything is 429 afterwards" is satisfied by a limiter
		// that refuses everything.
		if i <= adminFloodLimit && code == http.StatusTooManyRequests {
			t.Fatalf("request %d was refused before the ceiling of %d", i, adminFloodLimit)
		}
		if i == adminFloodLimit+1 && code != http.StatusTooManyRequests {
			t.Fatalf("request %d answered %d, want 429", i, code)
		}
	}
	t.Logf("address budget burnt with %d requests (ceiling %d)", adminFloodLimit+1, adminFloodLimit)

	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			// Same exhausted address; the invented token is what reaches the
			// resolver on the protected route.
			b := newBrowser(t, h)
			b.ip = "198.51.100.1:1234"
			b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			if got := b.do(tc.method, tc.path, tc.form).Code; got != http.StatusTooManyRequests {
				t.Fatalf("with the address budget exhausted, %s answered %d, want 429 — this "+
					"route is not behind the flood ceiling", tc.name, got)
			}

			// POSITIVE CONTROL per route: a FRESH address is served, so the 429
			// above is the budget and not a broken route.
			fresh := newBrowser(t, h)
			fresh.ip = "198.51.100.200:1234"
			fresh.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			if got := fresh.do(tc.method, tc.path, tc.form).Code; got == http.StatusTooManyRequests {
				t.Fatalf("control: %s refused a FRESH address with 429", tc.name)
			}
		})
	}
}

// 🔴 TestAdminLogin_SuppressionRowCarriesItsStartingOrdinal pins the field the
// round-6 §4.6 fix added and forgot to test.
//
// A security audit mutated `SuppressedFrom: n` to `SuppressedFrom: 0` and the
// WHOLE handler package stayed green (91.5 s, ok). The identifier appeared in
// three source lines, one comment and the plan card — and in ZERO tests.
//
// WHY IT MATTERS: adminratelimit.go's defence of the account budget is precisely
// that the silencing is "an interruption, not a blackout" — one rate_limited row
// per window per account, carrying the ordinal at which suppression began. If that
// field silently becomes 0, an investigator can still see the account was attacked
// but not WHEN the trail stopped, and the sentence that justifies the budget stops
// being true with nothing turning red.
//
// It is the class the round-4 family scan was invented for, missed because the
// field was added AFTER that scan and the scan was not re-run.
func TestAdminLogin_SuppressionRowCarriesItsStartingOrdinal(t *testing.T) {
	const requests = 15
	tenantID, adminID := uuid.New(), uuid.New()
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{Resolved: 1, Attempts: []adminauth.Attempt{
			{AdminUserID: adminID, TenantID: tenantID, PasswordMatched: false, Active: true},
		}}, adminauth.ErrBadCredentials
	}}
	trail := &fakeTrail{}
	h := newAdminRouter(t, admins, trail)

	// One address per request: the ACCOUNT budget is what is under test, and the
	// address budget would trip first otherwise.
	for i := 0; i < requests; i++ {
		b := newBrowser(t, h)
		b.ip = fmt.Sprintf("203.0.113.%d:1234", i+1)
		csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
		b.do(http.MethodPost, "/admin/login", url.Values{
			"csrf": {csrf}, "email": {"victim@example.test"}, "password": {"guess"},
		})
	}

	var limited []audit.Event
	for _, e := range trail.events {
		if e.Action == ActionAdminLoginLimited {
			limited = append(limited, e)
		}
	}
	if len(limited) != 1 {
		t.Fatalf("%d rate_limited rows, want exactly 1 per window per account", len(limited))
	}
	detail, ok := limited[0].Detail.(adminLoginDetail)
	if !ok {
		t.Fatalf("detail is %T, want adminLoginDetail", limited[0].Detail)
	}

	// THE ORDINAL MUST BE THE FIRST SUPPRESSED ATTEMPT: adminAccountLimit failures
	// are written, so the first one that is NOT written is number limit+1.
	want := adminAccountLimit + 1
	t.Logf("%d failures from %d addresses -> %d written + 1 rate_limited, SuppressedFrom=%d",
		requests, requests, trail.count(ActionAdminLoginFailed), detail.SuppressedFrom)
	if detail.SuppressedFrom != want {
		t.Fatalf("SuppressedFrom = %d, want %d (the ordinal of the first failure that was NOT "+
			"written). Without it the trail says an account was attacked but not when the "+
			"record stopped — which is the whole justification adminratelimit.go gives for "+
			"the account budget", detail.SuppressedFrom, want)
	}
	// And it must be a genuine ordinal, not a constant that happens to match.
	if got := trail.count(ActionAdminLoginFailed); got != adminAccountLimit {
		t.Fatalf("%d written failures, want %d — SuppressedFrom is only meaningful if it is "+
			"one past the written ones", got, adminAccountLimit)
	}
}

// TestAdminAuth_FloodGateDoesNotRefuseALegitimateSession is the POSITIVE CONTROL
// for the gate added in the closing round: the ceiling must bound an anonymous
// flood WITHOUT touching ordinary panel use.
//
// The budget is adminFloodLimit per window per address (3000 — this line said 200,
// which was true when it was written and which round 14's re-derivation missed),
// and a complete sign-in costs 4 requests (6 with the picker), so a working session
// is nowhere near the ADDRESS ceiling. It can reach the SESSION ceiling, which is
// why this drives fewer requests than adminSessionLimit.
func TestAdminAuth_FloodGateDoesNotRefuseALegitimateSession(t *testing.T) {
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: uuid.New(), TenantID: uuid.New(), AdminUserID: uuid.New(),
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	const realisticPageLoads = 100
	for i := 1; i <= realisticPageLoads; i++ {
		rec := b.do(http.MethodGet, "/admin", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("page load %d of %d answered %d, want 200 — the new flood gate is "+
				"refusing ordinary panel use (ceiling is %d per window)",
				i, realisticPageLoads, rec.Code, adminFloodLimit)
		}
	}
	t.Logf("%d consecutive authenticated page loads all served (ceiling %d/%s)",
		realisticPageLoads, adminFloodLimit, adminFloodPeriod)
}

// 🔴 TestAdminLogout_CannotBeBlockedByAThirdParty is an INVARIANT, not a budget
// test: ending your own session must never be refusable by somebody else.
//
// THE REGRESSION IT LOCKS OUT was created by round 12's own fix. An audit measured
// it: the victim's own 100 authenticated page loads spent half the address window,
// a third party sharing the address key spent the rest, and the victim's
// POST /admin/logout came back 429 with the session still LIVE — and nothing else
// could have ended it (no "sign out everywhere" route, disabling is M6-05, no
// server-side expiry).
func TestAdminLogout_CannotBeBlockedByAThirdParty(t *testing.T) {
	sid, tid, aid := uuid.New(), uuid.New(), uuid.New()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: sid, TenantID: tid, AdminUserID: aid,
			Role: "owner", FullName: "KF Owner"}, nil
	}}
	h := newAdminRouter(t, admins, &fakeTrail{})

	// A third party on the SAME address key burns the whole address budget.
	attacker := newBrowser(t, h)
	attacker.ip = "203.0.113.7:2222"
	refused := 0
	for i := 0; i < adminFloodLimit+50; i++ {
		if attacker.do(http.MethodGet, "/admin", nil).Code == http.StatusTooManyRequests {
			refused++
		}
	}
	if refused == 0 {
		t.Fatalf("control: the attacker never met the ceiling, so the budget was never burnt")
	}
	t.Logf("third party burnt the address budget: %d refusals past the %d ceiling",
		refused, adminFloodLimit)

	// The victim, sharing that address key, must STILL be able to sign out.
	victim := newBrowser(t, h)
	victim.ip = "203.0.113.7:1111"
	victim.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	rec := victim.do(http.MethodPost, "/admin/logout", url.Values{})
	t.Logf("victim POST /admin/logout with the budget burnt -> %d ; Revoke called %d times",
		rec.Code, admins.revokeCalls)

	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("a third party refused the victim's sign-out. Nothing else can end that " +
			"session in the window: 'sign out everywhere' has no route, disabling is M6-05, " +
			"and there is no server-side expiry.")
	}
	if admins.revokeCalls != 1 {
		t.Fatalf("Revoke called %d times, want 1 — the session is still LIVE server-side",
			admins.revokeCalls)
	}
	if _, ok := victim.cookies[adminauth.CookieName]; ok {
		t.Fatalf("the panel cookie survived the sign-out")
	}
}

// TestProtect_CarriesTheBudget is F6's net: the EXPORTED middleware M6-02 will
// mount must carry the shield, not just the gate.
//
// Round 12 put the shield inside Mount's own group and left Protect() bare, so a
// dashboard mounted in its own group with a.Protect() would have reinstated the
// unbudgeted resolver read. This mounts a route the way M6-02 would and asserts the
// ceiling still bites.
func TestProtect_CarriesTheBudget(t *testing.T) {
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{}, adminauth.ErrNoSession
	}}
	fake := newFakeLedger()
	h, err := NewAdminAuth(admins, &fakeTrail{}, fake, fake, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, adminTestConfig(), discardLogger())
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	// Exactly what M6-02 is expected to write, in ITS OWN group.
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(h.Protect())
		r.Get("/admin/dashboard", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	b := newBrowser(t, r)
	b.ip = "198.18.44.1:1234"
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	var refused, first int
	for i := 1; i <= adminFloodLimit+20; i++ {
		if b.do(http.MethodGet, "/admin/dashboard", nil).Code == http.StatusTooManyRequests {
			refused++
			if first == 0 {
				first = i
			}
		}
	}
	t.Logf("a dashboard route mounted with a.Protect() in ITS OWN group: %d refusals, first at #%d (ceiling %d)",
		refused, first, adminFloodLimit)
	if refused == 0 {
		t.Fatalf("a.Protect() does not carry the address shield — an M6-02 that mounts the " +
			"dashboard in its own group reinstates the unbudgeted resolver read F-A closed")
	}
	if first != adminFloodLimit+1 {
		t.Fatalf("first refusal at #%d, want #%d", first, adminFloodLimit+1)
	}
}

// 🔴 TestSessionGate_BoundsWorkAfterAuthentication is the net sessionGate did not
// have.
//
// An audit ran two mutations and BOTH were green: making the gate never refuse,
// and deleting it from Protect() entirely — the second with the full tree under
// -race, 14/14 packages ok. The only thing pinned was the CONSTANT'S VALUE; the
// BEHAVIOUR was measured nowhere. Same shape as isLookupableEmail, four rounds
// running.
//
// ⚠️ WHAT IT ASSERTS IS DELIBERATELY NARROW, because the gate's honest claim is
// narrow: it bounds work AFTER authentication. It does NOT bound the resolver read
// or the TouchAdminSession UPDATE — those happen first, and a refused request has
// already paid them (adminratelimit.go carries the measurement). So the assertion
// is that a single session stops being SERVED past the ceiling, not that it stops
// costing.
func TestSessionGate_BoundsWorkAfterAuthentication(t *testing.T) {
	sid := uuid.New()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: sid, TenantID: uuid.New(), AdminUserID: uuid.New(),
			Role: "owner", FullName: "KF Owner"}, nil
	}}
	b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
	b.ip = "198.18.77.1:1234"
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	// The session ceiling is far below the address ceiling, so what bites here can
	// only be the session gate. Guard it rather than leaving it to the reader.
	if adminSessionLimit >= adminFloodLimit {
		t.Fatalf("adminSessionLimit (%d) is not below adminFloodLimit (%d); this test could "+
			"not tell the two gates apart", adminSessionLimit, adminFloodLimit)
	}

	var served, refused, first int
	for i := 1; i <= adminSessionLimit+20; i++ {
		switch b.do(http.MethodGet, "/admin", nil).Code {
		case http.StatusOK:
			served++
			if refused > 0 {
				t.Fatalf("request %d was served AFTER a refusal — the budget is not monotonic", i)
			}
		case http.StatusTooManyRequests:
			refused++
			if first == 0 {
				first = i
			}
		default:
			t.Fatalf("request %d answered unexpectedly", i)
		}
	}
	t.Logf("one live session: %d served, %d refused, first at #%d (session ceiling %d, address ceiling %d)",
		served, refused, first, adminSessionLimit, adminFloodLimit)

	if refused == 0 {
		t.Fatalf("a single session made %d requests and was never refused — sessionGate is not "+
			"in the chain, or it does not refuse", adminSessionLimit+20)
	}
	if first != adminSessionLimit+1 {
		t.Fatalf("first refusal at #%d, want #%d (the ceiling is per SESSION)", first, adminSessionLimit+1)
	}
	if served != adminSessionLimit {
		t.Fatalf("%d requests served, want exactly %d", served, adminSessionLimit)
	}

	// POSITIVE CONTROL: a DIFFERENT session from the SAME address is unaffected,
	// which is what makes this the session budget rather than the address one.
	other := uuid.New()
	admins.verify = func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: other, TenantID: uuid.New(), AdminUserID: uuid.New(),
			Role: "owner", FullName: "Other Owner"}, nil
	}
	if got := b.do(http.MethodGet, "/admin", nil).Code; got != http.StatusOK {
		t.Fatalf("control: a different session from the same address answered %d, want 200", got)
	}
}

// 🔴 TestSameOriginGate_RefusesBeforeTheResolver measures the thing the middleware
// exists for: COST, not outcome.
//
// Deleting it left the suite green, because Logout re-checks Origin itself — the
// OUTCOME is identical either way. What changes is that without the middleware the
// resolver runs BEFORE the refusal, and sign-out is the panel's widest route, so
// this is its only cheap defence. Asserted by counting resolver calls.
func TestSameOriginGate_RefusesBeforeTheResolver(t *testing.T) {
	var verifyCalls int
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		verifyCalls++
		return adminauth.Resolved{SessionID: uuid.New(), TenantID: uuid.New(),
			AdminUserID: uuid.New(), Role: "owner", FullName: "KF Owner"}, nil
	}}
	h := newAdminRouter(t, admins, &fakeTrail{})

	for _, tc := range []struct {
		name   string
		origin string
	}{
		{"a foreign Origin", "https://evil.example"},
		{"no Origin and no fetch metadata", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verifyCalls = 0
			b := newBrowser(t, h)
			b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			b.origin = tc.origin
			b.do(http.MethodPost, "/admin/logout", url.Values{})
			if verifyCalls != 0 {
				t.Fatalf("the resolver ran %d times for %s, want 0 — the same-origin refusal must "+
					"happen BEFORE the resolver or sign-out has no cheap defence at all",
					verifyCalls, tc.name)
			}
		})
	}

	// POSITIVE CONTROL: a same-origin sign-out DOES reach the resolver.
	verifyCalls = 0
	b := newBrowser(t, h)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if got := b.do(http.MethodPost, "/admin/logout", url.Values{}).Code; got != http.StatusSeeOther {
		t.Fatalf("control: a same-origin sign-out answered %d, want 303", got)
	}
	if verifyCalls != 1 {
		t.Fatalf("control: the resolver ran %d times for a legitimate sign-out, want 1", verifyCalls)
	}
}

// TestLogoutGate_BoundsTheAmplifierWithoutBreakingTheInvariant covers both halves
// of round 16's F-4 decision in one place, because they are one trade.
func TestLogoutGate_BoundsTheAmplifierWithoutBreakingTheInvariant(t *testing.T) {
	var verifyCalls int
	sid := uuid.New()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		verifyCalls++
		return adminauth.Resolved{SessionID: sid, TenantID: uuid.New(), AdminUserID: uuid.New(),
			Role: "owner", FullName: "KF Owner"}, nil
	}}

	t.Run("the infinite amplifier is closed", func(t *testing.T) {
		h := newAdminRouter(t, admins, &fakeTrail{})
		verifyCalls = 0
		b := newBrowser(t, h)
		b.ip = "198.18.55.1:1234"
		b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		var refused int
		for i := 1; i <= adminLogoutLimit+50; i++ {
			// ⚠️ THE COOKIE IS RE-PLANTED EVERY ITERATION. The first successful
			// sign-out clears it, and a cookie-less request never reaches the
			// resolver at all — so without this the probe measured ONE resolver read
			// and 30049 cheap redirects, which is not the anonymous amplifier being
			// bounded. Every request must carry a well-formed token.
			b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			if b.do(http.MethodPost, "/admin/logout", url.Values{}).Code == http.StatusTooManyRequests {
				refused++
			}
		}
		t.Logf("%d sign-outs from one address: %d refused, resolver ran %d times (ceiling %d)",
			adminLogoutLimit+50, refused, verifyCalls, adminLogoutLimit)
		if refused == 0 {
			t.Fatalf("sign-out has no ceiling at all: %d requests, %d resolver reads, nothing "+
				"refused — an unbounded amplifier on the pool the tap surface shares",
				adminLogoutLimit+50, verifyCalls)
		}
		if verifyCalls > adminLogoutLimit {
			t.Fatalf("the resolver ran %d times against a ceiling of %d", verifyCalls, adminLogoutLimit)
		}
	})

	t.Run("the invariant holds: a third party cannot refuse a sign-out", func(t *testing.T) {
		h := newAdminRouter(t, admins, &fakeTrail{})
		attacker := newBrowser(t, h)
		attacker.ip = "203.0.113.7:2222"
		for i := 0; i < adminFloodLimit+50; i++ {
			attacker.do(http.MethodGet, "/admin", nil)
		}
		for i := 0; i < 3000; i++ {
			attacker.do(http.MethodPost, "/admin/logout", url.Values{})
		}

		victim := newBrowser(t, h)
		victim.ip = "203.0.113.7:1111"
		victim.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		before := admins.revokeCalls
		rec := victim.do(http.MethodPost, "/admin/logout", url.Values{})
		t.Logf("panel ceiling burnt + 3000 sign-outs spent -> victim sign-out %d, Revoke %d->%d",
			rec.Code, before, admins.revokeCalls)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("a third party refused the victim's sign-out")
		}
		if admins.revokeCalls != before+1 {
			t.Fatalf("Revoke not called — the session is still LIVE server-side")
		}
	})
}

// 🔴 TestMeterOnly_ChargesTheSharedBudget is the FIFTH blind net this task has had
// to close, and the pattern is identical to the four before it (isLookupableEmail,
// sessionGate, sameOriginGate, and now this): a middleware whose only mention in
// the repo was two comments and one r.Use, with zero tests.
//
// An audit replaced the Charge call with a constant and the whole internal/handler
// package stayed green under -race.
//
// WHAT IT MUST DO: sign-out is exempt from REFUSAL, but it must still CHARGE the
// panel's shared bucket — otherwise it is a free amplifier for the routes that do
// refuse, i.e. an attacker could hammer sign-out to no cost and leave the flood
// budget untouched for a second front. Asserted by driving sign-out and then
// checking that a DIFFERENT route's ceiling has moved.
func TestMeterOnly_ChargesTheSharedBudget(t *testing.T) {
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: uuid.New(), TenantID: uuid.New(),
			AdminUserID: uuid.New(), Role: "owner", FullName: "KF Owner"}, nil
	}}
	h := newAdminRouter(t, admins, &fakeTrail{})

	// Spend most of the PANEL budget through sign-out, which never refuses.
	const spent = 2500
	b := newBrowser(t, h)
	b.ip = "198.18.99.1:1234"
	for i := 0; i < spent; i++ {
		b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		if got := b.do(http.MethodPost, "/admin/logout", url.Values{}).Code; got == http.StatusTooManyRequests {
			t.Fatalf("sign-out refused at request %d — it must never refuse below its own "+
				"ceiling (%d)", i+1, adminLogoutLimit)
		}
	}

	// A DIFFERENT route on the SAME address must now be that much closer to its
	// ceiling: only adminFloodLimit-spent requests should be served.
	other := newBrowser(t, h)
	other.ip = "198.18.99.1:1234"
	var served int
	for i := 0; i < adminFloodLimit; i++ {
		if other.do(http.MethodGet, "/admin/login", nil).Code == http.StatusTooManyRequests {
			break
		}
		served++
	}
	t.Logf("%d sign-outs charged the shared bucket; /admin/login then served %d more "+
		"(ceiling %d, so %d were already spent)", spent, served, adminFloodLimit, adminFloodLimit-served)

	if served >= adminFloodLimit {
		t.Fatalf("after %d sign-outs, /admin/login still served %d requests — sign-out is not "+
			"charging the shared budget, so it is a free amplifier for every route that is "+
			"metered against it", spent, served)
	}
	if served != adminFloodLimit-spent {
		t.Fatalf("/admin/login served %d, want exactly %d (ceiling %d minus the %d already "+
			"spent by sign-out)", served, adminFloodLimit-spent, adminFloodLimit, spent)
	}
}

// TestLogin_EveryBusinessDisabledLeavesATrail covers the branch whose comment used
// to declare an audit trail that did not exist.
//
// A password that verified against two or more REAL accounts, all of which are
// disabled between step one and step two, used to leave no trace anywhere: the
// multi-candidate branch writes nothing, adminauth writes nothing, and this branch
// wrote nothing while its comment said the trail "already carries the successful
// comparison". That is the M5-11 class — a sentence telling the next reader not to
// look.
func TestLogin_EveryBusinessDisabledLeavesATrail(t *testing.T) {
	v1 := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	v2 := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	admins := &fakeAdmins{
		authenticate: func(string, string) (adminauth.Authentication, error) {
			return adminauth.Authentication{Resolved: 2, Attempts: []adminauth.Attempt{
				{AdminUserID: v1.AdminUserID, TenantID: v1.TenantID, PasswordMatched: true, Active: true},
				{AdminUserID: v2.AdminUserID, TenantID: v2.TenantID, PasswordMatched: true, Active: true},
			}}, nil
		},
		// Both were disabled between the password check and the picker.
		choices: func([]adminauth.Verified) ([]adminauth.Choice, error) {
			return nil, nil
		},
	}
	trail := &fakeTrail{}
	b := newBrowser(t, newAdminRouter(t, admins, trail))

	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, "/admin/login", nil)))
	if rec := b.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {"owner@example.test"}, "password": {"right"},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("control: two verified businesses did not reach the picker (%d)", rec.Code)
	}
	rec := b.do(http.MethodGet, "/admin/login/choose", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("the empty picker answered %d, want 200 (the restart screen)", rec.Code)
	}

	if trail.total() == 0 {
		t.Fatalf("a password that VERIFIED against %d real accounts, all disabled before the "+
			"choice, left NO audit row anywhere. Section 4.6 wants a refused attempt visible, "+
			"and this one is worth seeing: somebody held a correct password for accounts that "+
			"were just switched off", 2)
	}
	e := trail.events[0]
	if e.TenantID != v1.TenantID {
		t.Fatalf("the row was written to tenant %v, want %v — it must go to a tenant this "+
			"request AUTHENTICATED against, never one the caller named", e.TenantID, v1.TenantID)
	}
	detail, ok := e.Detail.(adminLoginDetail)
	if !ok {
		t.Fatalf("detail is %T", e.Detail)
	}
	if detail.VerifiedBusiness != 2 {
		t.Fatalf("VerifiedBusiness = %d, want 2", detail.VerifiedBusiness)
	}
	if !strings.Contains(detail.Reason, "disabled") {
		t.Fatalf("reason = %q, want it to name what happened", detail.Reason)
	}
	t.Logf("empty picker after 2 verified businesses -> %d audit row(s), reason=%q",
		trail.total(), detail.Reason)
}
