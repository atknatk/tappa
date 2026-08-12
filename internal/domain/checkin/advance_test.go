package checkin

// 🔴 THE TEST THAT WAS MISSING, AND THE MEASUREMENT THAT PROVED IT MISSING.
//
// A security audit rewrote the atomic advance in checkin.go into a genuine
// TOCTOU — SELECT last_ctr, compare it in Go, then UPDATE, all inside the same
// transaction — and ran the full suite:
//
//	make test -race (.env loaded)  ->  ok, all THIRTEEN packages
//	the same mutation + an 80 ms sleep between the read and the write
//	                              ->  "12 of 12 concurrent taps were SUN-valid"
//
// So the mutation is a REAL replay hole and the suite's timing could not see it.
// The reason is a seam between two proofs: internal/sun's contention test proves
// that sun.AdvanceCounter resolves N racers to one winner (an auditor confirmed
// it by breaking that function — 50 of 50 winners, RED), and this package's DB
// tests prove that a tap ends in the right record. Nothing proved the tap path
// CALLS that function, so a mutation walked between them.
//
// Testing harder for concurrency here would not close it: the window between a
// SELECT and an UPDATE is sub-millisecond while each request does several other
// round trips, so an HTTP-level race test stays green against the broken shape
// (measured twice — with and without a start barrier). What belongs at THIS
// layer is the WIRING, and wiring is assertable without waiting for a race:
// exactly one AdvanceTagCounter, with this tap's tenant, uid and counter, inside
// a transaction scoped to the TAG's tenant.
//
// This is the third appearance of one lesson in this milestone (M5-04's audit
// recorder, M5-04's branded 429, M5-05's debounce window): a test must pin what
// the product actually does, not what a neighbouring package can do.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/store"
)

// countingAdvancer records every AdvanceTagCounter it is asked for.
type countingAdvancer struct {
	calls []store.AdvanceTagCounterParams
	gap   int32
	err   error
}

func (c *countingAdvancer) AdvanceTagCounter(_ context.Context, arg store.AdvanceTagCounterParams) (store.AdvanceTagCounterRow, error) {
	c.calls = append(c.calls, arg)
	if c.err != nil {
		return store.AdvanceTagCounterRow{}, c.err
	}
	return store.AdvanceTagCounterRow{CtrGap: c.gap}, nil
}

// countingTx implements pgx.Tx by EMBEDDING the interface, the same trick
// internal/sun's tests use: every method not overridden below is nil and panics
// if called. Here that is the desired behaviour — the only legitimate use of the
// transaction on this path is building the querier, which the seam has already
// replaced, so a body that issues its OWN SQL is doing something this path must
// not do.
//
// QueryRow and Exec ARE implemented, and only so that the auditor's TOCTOU
// mutation (SELECT ... then UPDATE ...) runs to completion and fails on the
// COUNTING ASSERTION with a sentence that says what went wrong, rather than on a
// nil-pointer panic that reads like flakiness.
type countingTx struct {
	pgx.Tx
	statements *int
}

func (c countingTx) QueryRow(context.Context, string, ...any) pgx.Row {
	*c.statements++
	return noRow{}
}

func (c countingTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	*c.statements++
	return pgconn.CommandTag{}, nil
}

type noRow struct{}

func (noRow) Scan(...any) error { return nil }

// tenantRecorder is a Database that runs the callback and remembers which tenant
// it was scoped to, plus how many statements the callback issued against the
// transaction ITSELF (which must be zero — see countingTx).
type tenantRecorder struct {
	tenants    []uuid.UUID
	statements int
	err        error
}

func (t *tenantRecorder) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	t.tenants = append(t.tenants, tenantID)
	if t.err != nil {
		return t.err
	}
	return fn(ctx, countingTx{statements: &t.statements})
}

func (t *tenantRecorder) GetTagByUID(context.Context, string) (db.ResolvedTag, error) {
	return db.ResolvedTag{}, errors.New("not used by these tests")
}

// mountedAt is a plaque's wall as the nullable column returns it. It exists so a
// fixture has to SAY it is building a mounted plaque rather than getting one by
// accident: nil means "loaded but not on a wall", which is a real state since
// migration 00013.
func mountedAt(id uuid.UUID) *uuid.UUID { return &id }

func advanceService(t *testing.T, data Database, adv counterAdvancer) *Service {
	t.Helper()
	return &Service{
		data:     data,
		advancer: func(pgx.Tx) counterAdvancer { return adv },
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// TestAdvance_IssuesTheAtomicQueryExactlyOnce is the pin. It fails on any body
// that stops routing the advance through AdvanceTagCounter — including the exact
// TOCTOU the audit wrote, which issues its own SELECT and UPDATE and never
// touches the querier.
func TestAdvance_IssuesTheAtomicQueryExactlyOnce(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	adv := &countingAdvancer{gap: 3}
	data := &tenantRecorder{}
	s := advanceService(t, data, adv)

	tagRow := db.ResolvedTag{
		// LocationID is a POINTER since M6-06 phase B (00013 made tags.location_id
		// nullable for the inventory model). This fixture is a MOUNTED plaque, so it
		// is non-nil; a nil here would mean a plaque still in its box.
		UID: "04AC7E55000601", TenantID: tenantID, LocationID: mountedAt(uuid.New()),
		Status: string(tap.TagActive),
		// LastCtr is deliberately populated and deliberately never used: a body
		// that compared it in Go would be the TOCTOU §4.4 forbids by name, and it
		// is sitting right here for anyone tempted.
		LastCtr: 700,
	}
	req := Request{
		SessionTenantID: tenantID, EmployeeID: uuid.New(),
		TagUID: tagRow.UID, Ctr: 701, Channel: tap.ChannelNFC, CMACVerified: true,
	}

	sunValid, gap, err := s.advance(context.Background(), req, tagRow)
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if !sunValid {
		t.Fatal("a verified CMAC with an advancing counter must be SUN-valid")
	}
	if len(adv.calls) != 1 {
		t.Fatalf("AdvanceTagCounter was called %d times, want exactly 1 — the advance is not going through "+
			"the atomic statement (§4.4). A read-then-compare-then-write body reaches this line with 0.",
			len(adv.calls))
	}
	if gap != 3 {
		t.Fatalf("gap = %d, want the value the atomic query returned (3)", gap)
	}
	got := adv.calls[0]
	if got.TenantID != tenantID || got.Uid != tagRow.UID || got.Ctr != 701 {
		t.Fatalf("AdvanceTagCounter(%+v), want tenant=%s uid=%s ctr=701", got, tenantID, tagRow.UID)
	}
	if len(data.tenants) != 1 || data.tenants[0] != tenantID {
		t.Fatalf("the advance ran under tenants %v, want exactly [%s]: it must execute inside the TAG's "+
			"tenant context so the UPDATE runs under RLS (§4.5)", data.tenants, tenantID)
	}
	if data.statements != 0 {
		t.Fatalf("the advance issued %d statement(s) of its own against the transaction: the counter is "+
			"moved by ONE query and that query is AdvanceTagCounter (§4.4). A read-then-compare-then-write "+
			"body lands here with 2.", data.statements)
	}
}

// TestAdvance_SkipsTheQueryEntirelyWhenItMust. Every condition that stops the
// advance must stop it BEFORE the statement is issued — not by having the
// statement decline. Each of these is a way to move a counter that must not move.
func TestAdvance_SkipsTheQueryEntirelyWhenItMust(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	other := uuid.New()
	base := db.ResolvedTag{UID: "04AC7E55000601", TenantID: tenantID, Status: string(tap.TagActive)}
	baseReq := Request{SessionTenantID: tenantID, TagUID: base.UID, Ctr: 701,
		Channel: tap.ChannelNFC, CMACVerified: true}

	cases := []struct {
		name string
		tag  func(db.ResolvedTag) db.ResolvedTag
		req  func(Request) Request
		why  string
	}{
		{
			name: "a QR arrival has no counter",
			req:  func(r Request) Request { r.Channel = tap.ChannelQR; return r },
			why:  "QR carries no ctr; there is nothing to advance",
		},
		{
			name: "a manual record has no chip",
			req:  func(r Request) Request { r.Channel = tap.ChannelManual; return r },
			why:  "a manager typing a record must not move a plaque's counter",
		},
		{
			name: "the CMAC did not verify",
			req:  func(r Request) Request { r.CMACVerified = false; return r },
			why:  "§4.4's denial of service: pushing last_ctr with rubbish MACs rejects every real tap after it",
		},
		{
			name: "the plaque is retired",
			tag:  func(g db.ResolvedTag) db.ResolvedTag { g.Status = string(tap.TagRetired); return g },
			why:  "a dead plaque never reaches cryptography or the counter (§5 row 1)",
		},
		{
			name: "the plaque is lost",
			tag:  func(g db.ResolvedTag) db.ResolvedTag { g.Status = string(tap.TagLost); return g },
			why:  "same as retired, plus a security alert downstream",
		},
		{
			name: "the plaque belongs to another organisation",
			tag:  func(g db.ResolvedTag) db.ResolvedTag { g.TenantID = other; return g },
			why:  "§4.5: a session must not change another tenant's state (the F1 finding)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tag, req := base, baseReq
			if tc.tag != nil {
				tag = tc.tag(tag)
			}
			if tc.req != nil {
				req = tc.req(req)
			}
			adv := &countingAdvancer{}
			data := &tenantRecorder{}
			s := advanceService(t, data, adv)

			sunValid, gap, err := s.advance(context.Background(), req, tag)
			if err != nil {
				t.Fatalf("advance: %v", err)
			}
			if sunValid || gap != 0 {
				t.Fatalf("sunValid=%v gap=%d, want false/0", sunValid, gap)
			}
			if len(adv.calls) != 0 {
				t.Fatalf("the atomic statement was issued anyway (%d times): %s", len(adv.calls), tc.why)
			}
			if len(data.tenants) != 0 {
				t.Fatalf("a transaction was opened against %v: %s", data.tenants, tc.why)
			}
		})
	}
}

// TestAdvance_ReplayIsNotAnErrorAndOutageIs. The two non-success outcomes are
// different in kind and the caller depends on the difference: a replay is §5 row
// 2 and must end in a RECORDED reject, while an outage must surface so nothing
// claims a tap was judged when it was not.
func TestAdvance_ReplayIsNotAnErrorAndOutageIs(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	tagRow := db.ResolvedTag{UID: "04AC7E55000601", TenantID: tenantID, Status: string(tap.TagActive)}
	req := Request{SessionTenantID: tenantID, TagUID: tagRow.UID, Ctr: 701,
		Channel: tap.ChannelNFC, CMACVerified: true}

	// The atomic UPDATE matched no row: the counter did not advance.
	replay := advanceService(t, &tenantRecorder{}, &countingAdvancer{err: pgx.ErrNoRows})
	sunValid, _, err := replay.advance(context.Background(), req, tagRow)
	if err != nil {
		t.Fatalf("a replay must not be an error, got %v", err)
	}
	if sunValid {
		t.Fatal("a replay reported SUN-valid")
	}

	// The database was unreachable.
	outage := advanceService(t, &tenantRecorder{err: errors.New("connection refused")}, &countingAdvancer{})
	if _, _, err := outage.advance(context.Background(), req, tagRow); err == nil {
		t.Fatal("an outage was swallowed: the tap would be recorded as SUN-invalid rather than surfaced")
	}
}

// TestNew_WiresTheProductionAdvancer. The seam exists for tests, so the thing
// that must not slip is production still getting the real one — a nil advancer
// would panic at the first NFC tap, and a zero Service must not be constructible
// into the request path.
func TestNew_WiresTheProductionAdvancer(t *testing.T) {
	t.Parallel()
	s, err := New(&tenantRecorder{}, stubRecorder{}, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.advancer == nil {
		t.Fatal("New left the advancer nil: the first NFC tap would panic")
	}
	// It must be the store-backed one. Calling it with a nil transaction returns a
	// *store.Queries built on nil, which is exactly what production builds from a
	// real tx — the point is that it is store.New and not a leftover double.
	if _, ok := s.advancer(nil).(*store.Queries); !ok {
		t.Fatalf("New wired %T as the advancer, want *store.Queries", s.advancer(nil))
	}
}

// TestNew_RefusesAZeroFreshnessWindow pins the asymmetry between the two
// configured windows, because without a test a reader would call it an
// inconsistency and "fix" it in the wrong direction.
//
// Debounce MAY be omitted: its fallback is policy.DefaultParams()'s 60 s, which
// is also TAPPA_DEBOUNCE_SECONDS' shipped default, so omission lands on shipped
// behaviour. Freshness may NOT: its fallback was 900 s, the range maximum and the
// same number as the signed context's TTL, so omission was not a looser window —
// it was sys:tap-freshness unable to fire on any page at all. Measured while the
// wiring was still conditional: a Config with no Freshness built with err == nil
// and ran at 15m0s, and two in-tree callers were doing it.
//
// ⚠️ THE FIRST ASSERTION READS THE MESSAGE, NOT JUST err != nil, and that is the
// whole reason it can fail. checkin.New's wiring is unconditional now, so a zero
// reaches policy.Params.Validate() and is refused there too; an err != nil
// assertion therefore stayed GREEN with the explicit check deleted (measured).
// The check survives for the MESSAGE — "config Freshness is zero" names the unset
// field, where Validate can only say the window is out of range — so the message
// is what this pins.
//
// The last assertion is the non-DB falsifier for the wiring itself: 120 s is not
// policy.DefaultParams()'s 900 s fallback, so deleting
// `params.FreshnessWindow = cfg.Freshness` turns this red without a Postgres.
//
// THE NEGATIVE CASE IS NOT DECORATION. The guard is `<= 0`, and a negative value
// is REACHABLE: TAPPA_FRESHNESS_SECONDS=NaN survives config's range check (NaN
// comparisons are all false — a limit documented on floatEnvRange) and becomes
// the int64-minimum duration. The message used to say "is zero" for it, which
// sends the operator looking for an unset field when the fault is an unparseable
// one, so the wording is pinned here for BOTH shapes.
func TestNew_RefusesAZeroFreshnessWindow(t *testing.T) {
	t.Parallel()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	for _, tc := range []struct {
		name string
		val  time.Duration
	}{
		{"zero", 0},
		// What time.Duration(math.NaN() * float64(time.Second)) actually is.
		{"nan_derived_negative", time.Duration(math.MinInt64)},
	} {
		stale := testConfig()
		stale.Freshness = tc.val
		_, err := New(&tenantRecorder{}, stubRecorder{}, stale, quiet)
		if err == nil {
			t.Fatalf("%s: New accepted a non-positive Freshness: the service would run the 900s fallback, "+
				"which equals the signed context TTL and leaves sys:tap-freshness with an empty band", tc.name)
		}
		if !strings.Contains(err.Error(), "config Freshness is not positive") {
			t.Fatalf("%s: New refused Freshness with %q, but not at the config layer: want the message that "+
				"names the unset field, because policy.Validate's range error alone sends the reader after a "+
				"policy bug that is not there", tc.name, err)
		}
	}

	cfg := testConfig()
	cfg.Debounce = 0
	s, err := New(&tenantRecorder{}, stubRecorder{}, cfg, quiet)
	if err != nil {
		t.Fatalf("New refused a zero Debounce, but that one has a safe fallback: %v", err)
	}
	if got := s.policies.params.DebounceWindow; got != 60*time.Second {
		t.Fatalf("zero Debounce -> %v, want the shipped 60s fallback", got)
	}
	if got, want := s.policies.params.FreshnessWindow, testConfig().Freshness; got != want {
		t.Fatalf("FreshnessWindow = %v, want the configured %v", got, want)
	}
}

type stubRecorder struct{}

func (stubRecorder) Record(context.Context, audit.Event) (uuid.UUID, error) { return uuid.New(), nil }

// testConfig is the smallest config New will accept. Freshness is present
// because New REFUSES a zero one (checkin.go): omitting it used to build a
// service whose freshness window was policy.DefaultParams()'s 900 s, i.e. the
// guardrail switched off, and this file was one of the two callers doing it.
func testConfig() *config.Config {
	return &config.Config{
		Env: config.EnvDev, GPSRadiusMeters: 150, Debounce: 90 * time.Second,
		// 120 s carries ONE load-bearing property: it is not policy.DefaultParams()'s
		// 900 s fallback, so an assertion on the effective window can tell "the
		// configured value arrived" from "the wiring is missing". Being unequal to
		// config.Load's shipped 180 buys nothing HERE and the comment used to claim it
		// did — checkin.New never calls config.Load, so 180 is not a value this test
		// could land on by accident (measured: set this to 180, delete the wiring, and
		// the test is still red). It stays 120 only so a reader does not mistake the
		// harness value for the shipped one.
		Freshness: 120 * time.Second,
	}
}

// TestParams_ReportsTheConfiguredWindowsRatherThanTheShippedDefaults.
//
// 🔴 THE PANEL PRINTS WHAT THIS RETURNS (M6-09), so a getter that quietly handed
// back policy.DefaultParams() would put a number on a customer's screen that no
// guardrail compares against — the hand-off N3 defect, one layer out and with a
// screen vouching for it. Both configured windows here differ from the shipped
// fallbacks (90 s vs 60 s debounce, 120 s vs 900 s freshness), so returning the
// defaults is RED rather than coincidentally right.
//
// The third window is asserted the other way round: occurred_at skew has NO config
// field today, so it MUST be the declared range maximum. If a future config knob
// appears and is not wired, this is the assertion that notices.
func TestParams_ReportsTheConfiguredWindowsRatherThanTheShippedDefaults(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	s, err := New(&tenantRecorder{}, stubRecorder{}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := s.Params()
	if got.DebounceWindow != cfg.Debounce {
		t.Errorf("Params().DebounceWindow = %v, want the configured %v (the shipped fallback is %v)",
			got.DebounceWindow, cfg.Debounce, policy.DefaultParams().DebounceWindow)
	}
	if got.FreshnessWindow != cfg.Freshness {
		t.Errorf("Params().FreshnessWindow = %v, want the configured %v (the shipped fallback is %v)",
			got.FreshnessWindow, cfg.Freshness, policy.DefaultParams().FreshnessWindow)
	}
	if want := time.Duration(policy.OccurredAtSkewMaxSeconds) * time.Second; got.OccurredAtSkewMax != want {
		t.Errorf("Params().OccurredAtSkewMax = %v, want the declared range maximum %v", got.OccurredAtSkewMax, want)
	}
	// Whatever it reports must be a set the guardrails would accept; a screen that
	// printed an out-of-range window would be describing a service that cannot run.
	if err := got.Validate(); err != nil {
		t.Errorf("Params() reported a set the guardrail bounds reject: %v", err)
	}
}
