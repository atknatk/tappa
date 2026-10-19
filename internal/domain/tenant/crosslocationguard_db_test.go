package tenant

// crosslocationguard_db_test.go -- ADR 0029's panel guard (user decision 2026-10-19)
// against real Postgres: the writer refuses to switch an evidence rule off while the
// cross-location rule's OLD allow is in force, from either source (a stored
// 2026-07-26 baseline, a tenant copy bound to a venue), writes the refusal to the
// trail and leaves the rule as it was; a business on the new baseline switches freely;
// the same state cannot be reached from the other side; and one business's old allow
// says nothing about another's switches. SetEnabled is what a POST to the policies
// apply route reaches, so a refusal here is a refusal of a direct POST too.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/policy"
)

// guardDocs are the shipped baseline's names by sid, and its cross-location document.
func guardDocs(t *testing.T) (names map[string]string, cross policy.BaselineDoc) {
	t.Helper()
	names = map[string]string{}
	for _, d := range policy.Baseline() {
		names[d.Document.Statements[0].Sid] = d.Name
		if d.Document.Statements[0].Sid == policy.SidCrossLocationNote {
			cross = d
		}
	}
	if cross.Name == "" {
		t.Fatal("PREMISE: no shipped cross-location document")
	}
	return names, cross
}

// oldCrossLocation is the cross-location statement as it shipped before 2026-10-18:
// the same document with effect allow.
func oldCrossLocation(st policy.Statement) policy.Statement {
	st.Effect = policy.EffectAllow
	return st
}

// seedOldBaseline stores all nine shipped documents as a business provisioned before
// 2026-10-18 holds them: stamp 2026-07-26, the cross-location rule an allow.
func (f *writerFixture) seedOldBaseline(t *testing.T, tenantID uuid.UUID) {
	t.Helper()
	for _, d := range policy.Baseline() {
		doc := d.Document
		doc.Version = "2026-07-26"
		doc.Statements = append([]policy.Statement(nil), doc.Statements...)
		if doc.Statements[0].Sid == policy.SidCrossLocationNote {
			doc.Statements[0] = oldCrossLocation(doc.Statements[0])
		}
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		f.storePolicy(t, tenantID, d.Name, "baseline", true, raw, nil)
	}
}

// seedVenueCopy stores a tenant rule made from the cross-location document by the
// authoring form before 2026-10-18: the customer's sid, one venue, effect allow.
func (f *writerFixture) seedVenueCopy(t *testing.T, tenantID uuid.UUID, cross policy.BaselineDoc) uuid.UUID {
	t.Helper()
	st := oldCrossLocation(cross.Document.Statements[0])
	st.Sid = tenantSid(st.Sid)
	st.Resource = []string{"location/" + uuid.NewString()}
	doc := policy.Document{Version: "2026-07-26", Name: "Rusty Bar movers", Statements: []policy.Statement{st}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.ParseAndValidate(raw, policy.LayerTenant, policy.DefaultLimits()); err != nil {
		t.Fatalf("PREMISE: the old venue copy is not a valid tenant document: %v", err)
	}
	return f.storePolicy(t, tenantID, doc.Name, "tenant", true, raw, &f.adminID)
}

func (f *writerFixture) switchAs(tenantID uuid.UUID, actor Actor, id uuid.UUID, on bool) error {
	_, err := f.writer.SetEnabled(context.Background(), EnableCommand{TenantID: tenantID, Actor: actor, PolicyID: id, Enabled: on})
	return err
}

// TestCrossLocationGuardDB_EveryEvidenceRuleUnderEitherSourceOfTheOldAllow: for each of
// policy.CrossLocationEvidenceSids and each source of the old allow -- the stored
// 2026-07-26 baseline, and a venue-bound tenant copy over the new baseline -- switching
// the evidence rule off is ErrCrossLocationFirst, the rule stays on, no policy.disabled
// row is written and one policy.switch_refused row names the rule that stands in the
// way. Switching that rule off first lets the evidence rule go.
func TestCrossLocationGuardDB_EveryEvidenceRuleUnderEitherSourceOfTheOldAllow(t *testing.T) {
	names, cross := guardDocs(t)
	for _, source := range []string{"stored 2026-07-26 baseline", "venue copy over the new baseline"} {
		for _, sid := range policy.CrossLocationEvidenceSids() {
			t.Run(source+"/"+sid, func(t *testing.T) {
				f := newWriterFixture(t)
				var blocker uuid.UUID
				var blockerName string
				if strings.HasPrefix(source, "stored") {
					f.seedOldBaseline(t, f.tenantID)
					blocker, blockerName = f.baselineID(t, f.tenantID, cross.Name), cross.Name
				} else {
					f.seedAuthzDocument(t, f.tenantID)
					blocker, blockerName = f.seedVenueCopy(t, f.tenantID, cross), "Rusty Bar movers"
				}
				evidence := f.baselineID(t, f.tenantID, names[sid])
				err := f.switchAs(f.tenantID, f.owner, evidence, false)
				if !errors.Is(err, ErrCrossLocationFirst) {
					t.Fatalf("switching %s off returned %v, want ErrCrossLocationFirst", sid, err)
				}
				if !f.enabledOf(t, evidence) {
					t.Fatal("the refused switch changed the rule")
				}
				if rows := f.auditRows(t, f.tenantID, ActionPolicyDisabled); len(rows) != 0 {
					t.Errorf("a refused switch left %d policy.disabled row(s)", len(rows))
				}
				rows := f.auditRows(t, f.tenantID, ActionPolicySwitchRefused)
				if len(rows) != 1 {
					t.Fatalf("the trail holds %d policy.switch_refused row(s), want 1", len(rows))
				}
				for _, want := range []string{`"reason": "` + reasonOldAllowInForce + `"`, `"requested_enabled": false`,
					`"was_enabled": true`, `"name": "` + names[sid] + `"`, blocker.String(), `"name": "` + blockerName + `"`} {
					if !strings.Contains(rows[0], want) {
						t.Errorf("the refusal row does not carry %s:\n%s", want, rows[0])
					}
				}
				// The way out the screen names.
				if err := f.switchAs(f.tenantID, f.owner, blocker, false); err != nil {
					t.Fatalf("switching the old allow off: %v", err)
				}
				if err := f.switchAs(f.tenantID, f.owner, evidence, false); err != nil {
					t.Fatalf("with the old allow off, switching %s off returned %v", sid, err)
				}
				if f.enabledOf(t, evidence) {
					t.Fatal("the evidence rule is still on")
				}
			})
		}
	}
}

// TestCrossLocationGuardDB_TheNewBaselineSwitchesFreely: a business on the 2026-10-18
// baseline (the note, no old copy) switches every evidence rule off and on again, and
// the trail holds no refusal.
func TestCrossLocationGuardDB_TheNewBaselineSwitchesFreely(t *testing.T) {
	names, _ := guardDocs(t)
	f := newWriterFixture(t)
	f.seedAuthzDocument(t, f.tenantID)
	for _, sid := range policy.CrossLocationEvidenceSids() {
		id := f.baselineID(t, f.tenantID, names[sid])
		if err := f.switchAs(f.tenantID, f.owner, id, false); err != nil {
			t.Fatalf("new baseline, %s off: %v", sid, err)
		}
	}
	for _, sid := range policy.CrossLocationEvidenceSids() {
		if f.enabledOf(t, f.baselineID(t, f.tenantID, names[sid])) {
			t.Errorf("%s is still on", sid)
		}
	}
	if rows := f.auditRows(t, f.tenantID, ActionPolicySwitchRefused); len(rows) != 0 {
		t.Errorf("the new baseline left %d refusal row(s)", len(rows))
	}
}

// TestCrossLocationGuardDB_TheOldAllowCannotComeBackOverAnEvidenceRuleOff: the same
// state from the other side. The old baseline's cross-location rule switched off, then
// GPS-only allowance off (allowed: nothing stands in the way), then the old rule back
// on -> ErrEvidenceRuleOff with its trail row; with GPS-only allowance back on, it
// comes back. Switching an evidence rule ON is never refused.
func TestCrossLocationGuardDB_TheOldAllowCannotComeBackOverAnEvidenceRuleOff(t *testing.T) {
	names, cross := guardDocs(t)
	f := newWriterFixture(t)
	f.seedOldBaseline(t, f.tenantID)
	old, gps := f.baselineID(t, f.tenantID, cross.Name), f.baselineID(t, f.tenantID, names[policy.SidGPSOnlyAllow])
	for _, step := range []struct {
		id   uuid.UUID
		on   bool
		want error
	}{{old, false, nil}, {gps, false, nil}, {old, true, ErrEvidenceRuleOff}, {gps, true, nil}, {old, true, nil}} {
		if err := f.switchAs(f.tenantID, f.owner, step.id, step.on); !errors.Is(err, step.want) {
			t.Fatalf("switch %s to %v: %v, want %v", f.policyName(t, step.id), step.on, err, step.want)
		}
	}
	rows := f.auditRows(t, f.tenantID, ActionPolicySwitchRefused)
	if len(rows) != 1 || !strings.Contains(rows[0], `"reason": "`+reasonEvidenceRuleOff+`"`) ||
		!strings.Contains(rows[0], gps.String()) || !strings.Contains(rows[0], `"requested_enabled": true`) {
		t.Fatalf("the trail holds %v; want one evidence_rule_off refusal naming GPS-only allowance", rows)
	}
}

// TestCrossLocationGuardDB_OneBusinesssOldAllowSaysNothingAboutAnother (§4.5): the
// fixture's business holds the old baseline and the other business the new one. The
// other business's owner switches its evidence rules off freely (the old allow it
// would be refused for is not its own); the first business is still refused for its
// own; and the first business's owner reaching for the other's rule gets
// ErrUnknownPolicy -- the guard reads nothing across the boundary and writes no row.
func TestCrossLocationGuardDB_OneBusinesssOldAllowSaysNothingAboutAnother(t *testing.T) {
	names, _ := guardDocs(t)
	f := newWriterFixture(t)
	f.seedOldBaseline(t, f.tenantID)
	for _, d := range policy.Baseline() {
		raw, err := json.Marshal(d.Document)
		if err != nil {
			t.Fatal(err)
		}
		f.storePolicy(t, f.foreignTenant, d.Name, "baseline", true, raw, nil)
	}
	foreignOwner := Actor{ID: f.foreignAdmin, Role: "owner"}
	foreignGPS := f.baselineID(t, f.foreignTenant, names[policy.SidGPSOnlyAllow])
	if err := f.switchAs(f.tenantID, f.owner, foreignGPS, false); !errors.Is(err, ErrUnknownPolicy) {
		t.Fatalf("the old-baseline business switching the other's rule: %v, want ErrUnknownPolicy", err)
	}
	if err := f.switchAs(f.foreignTenant, foreignOwner, foreignGPS, false); err != nil {
		t.Fatalf("the new-baseline business switching its own evidence rule off: %v", err)
	}
	if err := f.switchAs(f.tenantID, f.owner, f.baselineID(t, f.tenantID, names[policy.SidGPSOnlyAllow]), false); !errors.Is(err, ErrCrossLocationFirst) {
		t.Fatalf("the old-baseline business switching its own evidence rule off: %v, want ErrCrossLocationFirst", err)
	}
	if rows := f.auditRows(t, f.foreignTenant, ActionPolicySwitchRefused); len(rows) != 0 {
		t.Errorf("the other business holds %d refusal row(s)", len(rows))
	}
	if rows := f.auditRows(t, f.tenantID, ActionPolicySwitchRefused); len(rows) != 1 {
		t.Errorf("the old-baseline business holds %d refusal row(s), want its own one", len(rows))
	}
}

// TestCrossLocationGuardDB_RefusesExactlyTheInventorysRules: under the stored
// 2026-07-26 baseline, every shipped tap rule is switched off in turn (and back on when
// it went). The refused ones are exactly ADR 0029's three inventory rows -- IP proof,
// GPS-only allowance, no-evidence review -- spelled here, not read from
// policy.CrossLocationEvidenceSids, so a rule dropped from that list is red here as
// well as in internal/policy's inventory test; every other rule switches freely.
func TestCrossLocationGuardDB_RefusesExactlyTheInventorysRules(t *testing.T) {
	f := newWriterFixture(t)
	f.seedOldBaseline(t, f.tenantID)
	var refused []string
	for _, d := range AuthorableRules() {
		sid := d.Document.Statements[0].Sid
		if sid == policy.SidCrossLocationNote {
			continue
		}
		id := f.baselineID(t, f.tenantID, d.Name)
		switch err := f.switchAs(f.tenantID, f.owner, id, false); {
		case errors.Is(err, ErrCrossLocationFirst):
			refused = append(refused, sid)
		case err != nil:
			t.Fatalf("%s off: %v", sid, err)
		default:
			if err := f.switchAs(f.tenantID, f.owner, id, true); err != nil {
				t.Fatalf("%s back on: %v", sid, err)
			}
		}
	}
	want := []string{policy.SidIPOrGPSOK, policy.SidGPSOnlyAllow, policy.SidNoEvidenceReview}
	if strings.Join(refused, ",") != strings.Join(want, ",") {
		t.Fatalf("refused %v, want exactly ADR 0029's three %v", refused, want)
	}
}

// --- the guard under concurrency (the 2026-10-19 low round) ---------------------------
//
// 🔴 THE GUARD'S "IT RE-READS UNDER THE ROWS' LOCKS" IS A CLAIM ABOUT ORDER, AND ONLY
// A TEST THAT FIXES THE ORDER CAN HOLD IT. Both lenses removed the lock loop and every
// earlier test stayed green, while their own race probes found the hole: GPS-only
// allowance off AND the old allow on, both presses answered nil. The tests below are
// that probe made part of the suite: one in which an outer transaction lines the two
// presses up behind its locks and lets them go together, and one of plain racing
// pairs; both count how each pair ended, deadlocks included.

// pairOutcome is how one pair of opposite presses ended.
type pairOutcome struct {
	landed, refused, deadlocked, other int
}

func (o *pairOutcome) count(t *testing.T, err error) {
	t.Helper()
	var pg *pgconn.PgError
	switch {
	case err == nil:
		o.landed++
	case errors.Is(err, ErrCrossLocationFirst), errors.Is(err, ErrEvidenceRuleOff):
		o.refused++
	case errors.As(err, &pg) && pg.Code == "40P01":
		o.deadlocked++
	default:
		o.other++
		t.Errorf("a press failed for a reason outside the guard: %v", err)
	}
}

// auditCount is how many trail rows of these actions this tenant holds.
func (f *writerFixture) auditCount(t *testing.T, actions ...string) int {
	t.Helper()
	n := 0
	for _, a := range actions {
		n += len(f.auditRows(t, f.tenantID, a))
	}
	return n
}

// holdAllPolicies opens a transaction that holds every policy row of the fixture's
// business FOR UPDATE and returns its backend pid and a release function. Holding ALL
// of them, rather than the two the presses target, keeps the test about the guard's
// order rather than about which row a given order happens to take first.
func (f *writerFixture) holdAllPolicies(t *testing.T) (pid int32, release func()) {
	t.Helper()
	held, done, finished := make(chan int32, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			var p int32
			if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&p); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `SELECT id FROM policies WHERE tenant_id = $1 FOR UPDATE`, f.tenantID); err != nil {
				return err
			}
			held <- p
			<-done
			return nil
		})
	}()
	select {
	case pid = <-held:
	case err := <-finished:
		t.Fatalf("the outer transaction did not take its locks: %v", err)
	}
	return pid, func() {
		close(done)
		if err := <-finished; err != nil {
			t.Fatalf("the outer transaction: %v", err)
		}
	}
}

// waitBlockedBy polls until n backends are waiting behind blocker, directly or behind
// another waiter: the second press queued for a row waits on the FIRST waiter's tuple
// lock, so pg_blocking_pids names that waiter rather than the holder (measured -- the
// direct count stopped at 1).
func (f *writerFixture) waitBlockedBy(t *testing.T, blocker int32, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var got int
		if err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`WITH RECURSIVE q(pid) AS (
				   SELECT pid FROM pg_catalog.pg_stat_activity WHERE $1::int = ANY (pg_catalog.pg_blocking_pids(pid))
				   UNION
				   SELECT a.pid FROM pg_catalog.pg_stat_activity a JOIN q ON q.pid = ANY (pg_catalog.pg_blocking_pids(a.pid)))
				 SELECT count(*) FROM q`,
				blocker).Scan(&got)
		}); err != nil {
			t.Fatalf("reading the lock queue: %v", err)
		}
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d press(es) queued behind the outer transaction after 10s, want %d", got, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// resetPair puts the pair back where a race can open the hole: the old allow off and
// the evidence rule on (each alone is a switch the guard lets through).
func (f *writerFixture) resetPair(t *testing.T, old, ev uuid.UUID) {
	t.Helper()
	if f.enabledOf(t, old) {
		if err := f.switchAs(f.tenantID, f.owner, old, false); err != nil {
			t.Fatalf("reset: old allow off: %v", err)
		}
	}
	if !f.enabledOf(t, ev) {
		if err := f.switchAs(f.tenantID, f.owner, ev, true); err != nil {
			t.Fatalf("reset: evidence rule on: %v", err)
		}
	}
}

// pressPair runs the two opposite presses at once -- the evidence rule off, the old
// allow on -- after start is closed, and returns both answers.
func (f *writerFixture) pressPair(old, ev uuid.UUID, start <-chan struct{}) (evErr, oldErr error) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-start; evErr = f.switchAs(f.tenantID, f.owner, ev, false) }()
	go func() { defer wg.Done(); <-start; oldErr = f.switchAs(f.tenantID, f.owner, old, true) }()
	wg.Wait()
	return evErr, oldErr
}

// TestCrossLocationGuardDB_QueuedOppositePressesNeverBothLand: under the stored
// 2026-07-26 baseline, for each evidence rule: the old allow off and the evidence rule
// on (each opposite press alone would land), an outer transaction holds every policy
// row, the two opposite presses queue behind it (measured in pg_blocking_pids, not
// slept for), and the outer transaction lets them go together. Every time: the rulebook
// never ends with the evidence rule off AND the old allow on; exactly one press lands
// and writes exactly one change row; the other is refused by the guard, writes one
// policy.switch_refused row and no change; and neither is a deadlock, because both
// presses take the guarded rows in one order (crossLocationLocks).
func TestCrossLocationGuardDB_QueuedOppositePressesNeverBothLand(t *testing.T) {
	names, cross := guardDocs(t)
	f := newRaceWriterFixture(t)
	f.seedOldBaseline(t, f.tenantID)
	old := f.baselineID(t, f.tenantID, cross.Name)
	const rounds = 5
	for _, sid := range policy.CrossLocationEvidenceSids() {
		ev := f.baselineID(t, f.tenantID, names[sid])
		var o pairOutcome
		for i := 0; i < rounds; i++ {
			f.resetPair(t, old, ev)
			changes := f.auditCount(t, ActionPolicyEnabled, ActionPolicyDisabled)
			refusals := f.auditCount(t, ActionPolicySwitchRefused)
			pid, release := f.holdAllPolicies(t)
			start := make(chan struct{})
			var evErr, oldErr error
			pressed := make(chan struct{})
			go func() { evErr, oldErr = f.pressPair(old, ev, start); close(pressed) }()
			close(start)
			f.waitBlockedBy(t, pid, 2)
			release()
			<-pressed
			if f.enabledOf(t, old) && !f.enabledOf(t, ev) {
				t.Fatalf("%s round %d: BOTH landed -- the evidence rule off and the old allow on (%v / %v)", sid, i, evErr, oldErr)
			}
			var this pairOutcome
			this.count(t, evErr)
			this.count(t, oldErr)
			if this.landed != 1 || this.refused != 1 {
				t.Errorf("%s round %d: %+v (evidence off: %v; old allow on: %v), want one landed and one refused",
					sid, i, this, evErr, oldErr)
			}
			if got := f.auditCount(t, ActionPolicyEnabled, ActionPolicyDisabled) - changes; got != this.landed {
				t.Errorf("%s round %d: %d change row(s) for %d landed press(es)", sid, i, got, this.landed)
			}
			if got := f.auditCount(t, ActionPolicySwitchRefused) - refusals; got != this.refused {
				t.Errorf("%s round %d: %d refusal row(s) for %d refused press(es)", sid, i, got, this.refused)
			}
			o.landed, o.refused, o.deadlocked, o.other = o.landed+this.landed, o.refused+this.refused,
				o.deadlocked+this.deadlocked, o.other+this.other
		}
		t.Logf("%s: %d queued pairs -> landed %d, refused %d, deadlocked %d", sid, rounds, o.landed, o.refused, o.deadlocked)
	}
}

// TestCrossLocationGuardDB_RacingOppositePressesNeverBothLand: the same pair raced
// without an outer transaction -- 3 evidence rules x racingPairs pairs, both presses
// released by one channel close. The hole never opens and no pair deadlocks; how the
// pairs ended and how long it took are logged (the deadlock count is the low round's
// before/after measurement).
func TestCrossLocationGuardDB_RacingOppositePressesNeverBothLand(t *testing.T) {
	const racingPairs = 20
	names, cross := guardDocs(t)
	f := newRaceWriterFixture(t)
	f.seedOldBaseline(t, f.tenantID)
	old := f.baselineID(t, f.tenantID, cross.Name)
	began := time.Now()
	for _, sid := range policy.CrossLocationEvidenceSids() {
		ev := f.baselineID(t, f.tenantID, names[sid])
		var o pairOutcome
		for i := 0; i < racingPairs; i++ {
			f.resetPair(t, old, ev)
			start := make(chan struct{})
			done := make(chan struct{})
			var evErr, oldErr error
			go func() { evErr, oldErr = f.pressPair(old, ev, start); close(done) }()
			close(start)
			<-done
			if f.enabledOf(t, old) && !f.enabledOf(t, ev) {
				t.Fatalf("%s pair %d: BOTH landed -- the evidence rule off and the old allow on (%v / %v)", sid, i, evErr, oldErr)
			}
			o.count(t, evErr)
			o.count(t, oldErr)
		}
		t.Logf("%s: %d racing pairs -> landed %d, refused %d, deadlocked %d", sid, racingPairs, o.landed, o.refused, o.deadlocked)
		if o.deadlocked != 0 {
			t.Errorf("%s: %d of %d pairs deadlocked; the guarded rows are not locked in one order", sid, o.deadlocked, racingPairs)
		}
	}
	t.Logf("3 x %d racing pairs in %s", racingPairs, time.Since(began).Round(time.Millisecond))
}

// lockoutProbeAuthority is the stored rulebook with one call replaced: on call number
// at it first runs between (the other owner's press, landing while the first switch's
// lockout check is being asked) and then answers with the guardrails alone -- what
// checkin's stored-set read answers when it cannot read the rulebook, so the gate
// refuses and the writer restores.
type lockoutProbeAuthority struct {
	inner   storedAuthority
	at, n   int
	between func()
}

func (a *lockoutProbeAuthority) StoredSet(ctx context.Context, tenantID uuid.UUID) policy.Set {
	a.n++
	if a.n == a.at {
		a.between()
		return policy.Set{Guardrails: policy.Guardrails(a.inner.params)}
	}
	return a.inner.StoredSet(ctx, tenantID)
}

// TestCrossLocationGuardDB_ARestoreDoesNotBringTheOldAllowBackOverAnEvidenceRuleOff:
// under the stored 2026-07-26 baseline an owner switches the old cross-location allow
// off; while that switch's lockout check is asked, a second owner switches GPS-only
// allowance off (nothing stands in its way, it lands) and the check reads no rulebook,
// so the writer restores. The restore is refused by the guard: the answer is
// ErrLockoutStands carrying ErrEvidenceRuleOff (the screen says the change STANDS),
// the old allow stays off and GPS-only allowance off -- never the allow on over an
// evidence rule off -- and the trail holds the two changes and the restore's refusal,
// no policy.enabled row.
func TestCrossLocationGuardDB_ARestoreDoesNotBringTheOldAllowBackOverAnEvidenceRuleOff(t *testing.T) {
	names, cross := guardDocs(t)
	f := newWriterFixture(t)
	f.seedOldBaseline(t, f.tenantID)
	old, gps := f.baselineID(t, f.tenantID, cross.Name), f.baselineID(t, f.tenantID, names[policy.SidGPSOnlyAllow])
	ran := false
	probe := &lockoutProbeAuthority{
		inner: storedAuthority{data: f.data, params: testParams()},
		at:    2, // SetEnabled's gate is call 1, its lockout check call 2
		between: func() {
			ran = true
			if err := f.switchAs(f.tenantID, f.owner, gps, false); err != nil {
				t.Errorf("the second owner's press: %v", err)
			}
		},
	}
	trail, err := audit.New(f.data)
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewRuleWriter(f.data, trail, probe, testParams(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.SetEnabled(context.Background(), EnableCommand{TenantID: f.tenantID, Actor: f.owner, PolicyID: old, Enabled: false})
	if !ran {
		t.Fatal("PREMISE: the second press never ran; the lockout check is not call 2 any more")
	}
	if !errors.Is(err, ErrLockoutStands) || !errors.Is(err, ErrEvidenceRuleOff) {
		t.Fatalf("SetEnabled returned %v, want ErrLockoutStands carrying ErrEvidenceRuleOff", err)
	}
	if f.enabledOf(t, old) || f.enabledOf(t, gps) {
		t.Fatalf("old allow on=%v, GPS-only allowance on=%v; want both off (the change stands, the restore was refused)",
			f.enabledOf(t, old), f.enabledOf(t, gps))
	}
	if got := len(f.auditRows(t, f.tenantID, ActionPolicyDisabled)); got != 2 {
		t.Errorf("%d policy.disabled row(s), want 2 (the old allow, GPS-only allowance)", got)
	}
	if got := len(f.auditRows(t, f.tenantID, ActionPolicyEnabled)); got != 0 {
		t.Errorf("%d policy.enabled row(s), want 0 (the restore did not happen)", got)
	}
	rows := f.auditRows(t, f.tenantID, ActionPolicySwitchRefused)
	if len(rows) != 1 || !strings.Contains(rows[0], `"reason": "`+reasonEvidenceRuleOff+`"`) ||
		!strings.Contains(rows[0], `"requested_enabled": true`) || !strings.Contains(rows[0], gps.String()) {
		t.Fatalf("the trail holds %v; want the restore's one evidence_rule_off refusal naming GPS-only allowance", rows)
	}
}
