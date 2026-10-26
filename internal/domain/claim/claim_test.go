package claim

// claim_test.go -- the refusals that need no database, and the mapping of the database's.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

// stubDB fails the test if anything reaches it (manual's stubDB): the refusals below must happen
// before a transaction opens.
type stubDB struct{ t *testing.T }

func (s stubDB) WithTenant(context.Context, uuid.UUID, db.TxFunc) error {
	s.t.Helper()
	s.t.Fatal("the database was reached")
	return nil
}

type stubTrail struct{}

func (stubTrail) RecordTx(context.Context, pgx.Tx, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, nil
}

type stubRecorder struct{ t *testing.T }

func (s stubRecorder) Within(context.Context, uuid.UUID, uuid.UUID, manual.TxFunc) error {
	s.t.Helper()
	s.t.Fatal("the recorder was reached")
	return nil
}

func stubService(t *testing.T) *Service {
	t.Helper()
	s, err := New(stubDB{t}, stubTrail{}, stubRecorder{t}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestNew_RefusesToBeBuiltWithoutItsDependencies.
func TestNew_RefusesToBeBuiltWithoutItsDependencies(t *testing.T) {
	t.Parallel()
	if _, err := New(nil, stubTrail{}, stubRecorder{t}, nil, nil); err == nil {
		t.Error("built without a database")
	}
	if _, err := New(stubDB{t}, nil, stubRecorder{t}, nil, nil); err == nil {
		t.Error("built without a trail")
	}
	if _, err := New(stubDB{t}, stubTrail{}, nil, nil, nil); err == nil {
		t.Error("built without a recorder")
	}
}

// TestSubmit_RefusesBeforeItReachesTheDatabase.
func TestSubmit_RefusesBeforeItReachesTheDatabase(t *testing.T) {
	t.Parallel()
	s := stubService(t)
	ok := Claim{TenantID: uuid.New(), EmployeeID: uuid.New(), AnchorID: uuid.New(), Kind: KindOut, ClaimedAt: time.Now()}
	for _, tc := range []struct {
		name string
		mut  func(*Claim)
		want error
	}{
		{"no employee", func(c *Claim) { c.EmployeeID = uuid.Nil }, ErrUnknownAnchor},
		{"no anchor", func(c *Claim) { c.AnchorID = uuid.Nil }, ErrUnknownAnchor},
		// "No moment" is NOT refused here any more (M15-17 fix round): it is answered after the
		// "already sent" read, so a second send the handler could not place is "Sent" --
		// TestSubmitDB_EveryRefusalWritesNothing and TestSubmitDB_ASecondSendIsAlreadySent.
		{"an unknown kind", func(c *Claim) { c.Kind = "both" }, ErrResolved},
	} {
		c := ok
		tc.mut(&c)
		if _, err := s.Submit(context.Background(), c); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	c := ok
	c.TenantID = uuid.Nil
	if _, err := s.Submit(context.Background(), c); err == nil {
		t.Error("no tenant: accepted")
	}
}

// TestDecide_RefusesBeforeItReachesTheDatabase.
func TestDecide_RefusesBeforeItReachesTheDatabase(t *testing.T) {
	t.Parallel()
	s := stubService(t)
	ok := Decision{TenantID: uuid.New(), ClaimID: uuid.New(), DecidedBy: uuid.New(), Outcome: Approved}
	for _, tc := range []struct {
		name string
		mut  func(*Decision)
	}{
		{"no tenant", func(d *Decision) { d.TenantID = uuid.Nil }},
		{"no claim", func(d *Decision) { d.ClaimID = uuid.Nil }},
		{"no decider", func(d *Decision) { d.DecidedBy = uuid.Nil }},
		{"an unknown outcome", func(d *Decision) { d.Outcome = "maybe" }},
	} {
		d := ok
		tc.mut(&d)
		if _, err := s.Decide(context.Background(), d); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
	if _, err := s.Inspect(context.Background(), uuid.Nil, uuid.New(), nil); !errors.Is(err, ErrUnknownClaim) {
		t.Errorf("Inspect without a tenant: %v", err)
	}
	if _, err := s.Offer(context.Background(), uuid.New(), uuid.Nil, uuid.New()); !errors.Is(err, ErrUnknownAnchor) {
		t.Errorf("Offer without an employee: %v", err)
	}
}

// TestInsertErrors_MapTheDatabasesRefusals: each SQLSTATE and constraint of migration 00068 to its
// sentence; anything else passes through untouched (an outage is never "already sent").
func TestInsertErrors_MapTheDatabasesRefusals(t *testing.T) {
	t.Parallel()
	pg := func(code, constraint, msg string) error {
		return &pgconn.PgError{Code: code, ConstraintName: constraint, Message: msg}
	}
	other := errors.New("connection reset")
	for _, tc := range []struct {
		name string
		got  error
		want error
	}{
		{"claim: no row", insertClaimError(pgx.ErrNoRows), ErrUnknownAnchor},
		{"claim: one per anchor", insertClaimError(pg("23505", "punch_claims_one_per_anchor", "")), ErrAlreadySent},
		{"claim: the trigger", insertClaimError(pg("23514", "", "punch claim: the anchor is another person's record")), ErrRefused},
		{"claim: another unique", insertClaimError(pg("23505", "punch_claims_id_tenant_key", "")), nil},
		{"claim: an outage", insertClaimError(other), other},
		{"decision: no row", insertDecisionError(pgx.ErrNoRows), ErrUnknownClaim},
		{"decision: one per claim", insertDecisionError(pg("23505", "punch_claim_decisions_one_per_claim", "")), ErrAlreadyDecided},
		{"decision: the claimant", insertDecisionError(pg("23514", "", "punch claim decision: the decider is the claimant")), ErrSelfDecision},
		{"decision: the trigger", insertDecisionError(pg("23514", "", "punch claim decision: the approving record is another person's")), ErrRefused},
		{"decision: one per record", insertDecisionError(pg("23505", "punch_claim_decisions_one_per_record", "")), nil},
		{"decision: an outage", insertDecisionError(other), other},
	} {
		switch {
		case tc.want == nil:
			if isTyped(tc.got) {
				t.Errorf("%s: mapped to %v, want it passed through", tc.name, tc.got)
			}
		case !errors.Is(tc.got, tc.want):
			t.Errorf("%s: %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestChain_TiesAreOrderedByID (§A.4): two records at one instant are read in id order.
func TestChain_TiesAreOrderedByID(t *testing.T) {
	t.Parallel()
	at := wall(19, 9, 0, 0)
	c := NewChain([]Event{
		{ID: id(9), Direction: tap.TypeOut, At: at, WrittenAt: at},
		{ID: id(3), Direction: tap.TypeIn, At: at, WrittenAt: at},
	}, true)
	if c.Index(id(3)) != 0 || c.Index(id(9)) != 1 {
		t.Errorf("order %v, %v; want the smaller id first", c.Index(id(3)), c.Index(id(9)))
	}
	if c.Index(id(7)) != -1 {
		t.Error("an absent id was found")
	}
}

// TestSpanOf_NoShiftNoSpan: no shift, or one whose zone is unresolvable, has no instance.
func TestSpanOf_NoShiftNoSpan(t *testing.T) {
	t.Parallel()
	if _, ok := SpanOf(wall(19, 9, 0, 0), nil); ok {
		t.Error("a span without a shift")
	}
	if _, ok := SpanOf(wall(19, 9, 0, 0), &tap.Shift{Start: 9 * time.Hour, End: 17 * time.Hour, Timezone: ""}); ok {
		t.Error("a span in no zone")
	}
}

// TestAnchorOf_TheDirectionsSourceIsReadThroughTheReaderRule: the anchor's "they chose this direction"
// is tap.RecordedDirectionSource's answer over the row's own channel, type and stored value (M15-18,
// the card's step 4) -- never the raw column compared with 'employee'. The manual row is the case a raw
// comparison gets wrong.
func TestAnchorOf_TheDirectionsSourceIsReadThroughTheReaderRule(t *testing.T) {
	t.Parallel()
	in, employee, auto, other := "in", "employee", "auto", "manager"
	cases := []struct {
		name    string
		channel string
		typ     *string
		stored  *string
		chose   bool
	}{
		{"nfc, employee", "nfc", &in, &employee, true},
		{"qr, employee", "qr", &in, &employee, true},
		{"nfc, auto", "nfc", &in, &auto, false},
		{"nfc, NULL (before M15)", "nfc", &in, nil, false},
		{"manual, employee (not believed)", "manual", &in, &employee, false},
		{"nfc, undirected", "nfc", nil, &employee, false},
		{"nfc, a value the CHECK refuses", "nfc", &in, &other, false},
	}
	for _, tc := range cases {
		a := anchorOf(store.GetPunchClaimAnchorRow{ID: uuid.New(), Type: tc.typ, Channel: tc.channel, DirectionSource: tc.stored}, "UTC")
		if got := a.sourceKnown && a.source == tap.DirectionEmployee; got != tc.chose {
			t.Errorf("%s: chose = %v, want %v", tc.name, got, tc.chose)
		}
	}
}
