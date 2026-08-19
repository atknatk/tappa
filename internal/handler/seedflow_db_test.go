package handler

// THE SEEDED DEMO DATA, DRIVEN THROUGH THE PRODUCTION ROUTER (M5-09).
//
// Every other DB test in this package builds its own throwaway tenant, location
// and plaque. That is the right shape for pinning one rule, and it is the wrong
// shape for two questions this task exists to answer:
//
//   - Does a plaque FROM test/fixtures/seed.sql actually work? It did not: the
//     fixture wrote a 42-byte ASCII placeholder into tags.aes_key_ref, sun.Unwrap
//     demands exactly 44, and every NFC tap on a seeded plaque answered 500. A
//     harness that mints its own correctly-wrapped tag can never see that.
//   - Does a realistic DAY come out of the decision engine? (day_db_test.go.)
//
// So this harness uses the SEEDED tenant, the SEEDED venues (their real static
// IPs, coordinates and shifts) and the SEEDED plaques — with EPHEMERAL employees,
// because transactions are immutable (§4.3) and a re-runnable test cannot inherit
// yesterday's open check-in or the last run's debounce predecessor.
//
// It also drives httpx.NewRouter rather than a bare chi router: RequestID,
// RealIP, Recoverer, Timeout and the tap group's own middleware, over a real
// socket with a real cookie jar. The venue address arrives as X-Forwarded-For
// from a TRUSTED loopback hop — the production shape behind a reverse proxy —
// because httptest's peer is always 127.0.0.1 and proof-of-place is 50 of the 100
// trust points (§5).
//
// Fixtures are NOT cleaned up (tappa_app has REVOKE DELETE on the tables
// involved). `make db-reset` clears the dev database.

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/test/fixtures"
)

// seedDebounce is the person-debounce window this file runs under: the LOWEST
// value a real deployment may configure (policy.DebounceMinSeconds, enforced by
// config.Load's range check on TAPPA_DEBOUNCE_SECONDS).
//
// 🔴 IT IS THE SINGLE BIGGEST COST OF SIMULATING A DAY, and it is paid in real
// seconds on purpose. ADR 0006 made the debounce distance the SMALLER of the
// client-declared gap and a server-measured age (clock_timestamp() - created_at),
// so declaring "this tap happened eight hours ago" no longer buys anything: two
// POSTs seconds apart by the same person are one recorded tap and one `ignored`,
// whatever they claim. Measured, before this file chose to wait: firing the whole
// day back to back leaves 19 of the day's 31 rows `ignored`, all nine check-outs
// among them. NOT "every tap after a person's first" — that quantifier is one row
// too wide. Twenty of the 31 are somebody's second or later tap; the twentieth is
// Maria's tap on the RETIRED plaque, which §5 row 1 refuses before the debounce is
// ever consulted. (Nineteen, not twelve: the day turned out to be 31 rows rather
// than the card's estimate, for the reason day_db_test.go's header gives — on a
// fresh database §5 gives every worker a TRAINING row first, and that row is what
// each person's real check-in then collides with.)
//
// The alternatives were weighed and rejected:
//
//	weaken the guardrail        the engine does not bend to the fixture (§5).
//	configure below 30 s        config.Load REFUSES it (measured: 29 -> startup
//	                            error), so a test that did it would be driving a
//	                            configuration no deployment can have — the
//	                            degenerate-value trap from M5-04/M5-05.
//	give each tap a new person   a day where nobody checks out is not a day.
//	hand-write the aged rows     then the records are not the engine's output,
//	                            which is the one thing the card insists on.
//
// So the day waits, twice, and the cost is stated rather than hidden — measured
// on one machine with -race -count=1, across four sessions: `make test` 84.7-138 s
// with the day and 32.9-35 s with it skipped, the day itself 62.9-64.3 s. Read
// the RANGE, not any single figure: everything except the sleeping moves with
// machine load, so the day is somewhere between half and two thirds of the
// suite's wall time depending on the day. The ~62 s of time.Sleep is the part no
// machine changes.
//
// The user's decision on paying it (2026-08-02) is: `make test` keeps paying it
// and so does CI, and `make test-short` exists for the inner loop — see the
// testing.Short() block on TestSeedDB_ADayAtKFStJulians and the Makefile.
const seedDebounce = policy.DebounceMinSeconds * time.Second

// seedDebounceWait is how long a phase waits before the same person may tap
// again. The extra second covers the round trip between reading the clock and
// Postgres reading its own.
const seedDebounceWait = seedDebounce + time.Second

// seedFreshness is the tap-page freshness window the simulated day runs under,
// and like seedDebounce it is the LOWEST value a real deployment may configure
// (policy.FreshnessMinSeconds).
//
// 🔴 IT IS SET BECAUSE OMITTING IT USED TO SWITCH THE GUARDRAIL OFF. This file's
// config went to checkin.New without a Freshness, which fell back to
// policy.DefaultParams()'s 900 s — the same number as the signed context's TTL,
// so sys:tap-freshness had an empty band and `make simulate-day` exercised a
// production path with one guardrail inert. checkin.New now refuses a zero, and
// this constant is the answer to it.
//
// The tightest legal window costs the day nothing, and that is measured rather
// than argued: every `make test` runs the whole day at this 60 s and it passes.
// The structural reason is that tapNFC/tapQR do the GET and the POST inside a
// single call, so a page here is milliseconds old, and the two dayWait sleeps sit
// BETWEEN phases rather than between a mint and its post.
//
// ⚠️ SO THE VALUE ITSELF CARRIES NO LOAD, and nobody should read a mutation-proof
// into it. An audit measured the converse: at 900 s the day is green too (65 s).
// The day is insensitive to this number in BOTH directions — exactly what the
// structural reason predicts. 60 is picked because a harness should exercise the
// strictest deployment a customer may configure, not because the day needs it.
const seedFreshness = policy.FreshnessMinSeconds * time.Second

// offSiteAddr, the mobile-data case: a documentation-range address that belongs
// to no KF venue, so proof-of-place must come from GPS or not at all.
const seedOffSiteAddr = "198.51.100.9"

// seedFlow is the activation flow and the tap flow behind ONE production router,
// pointed at the seeded tenant.
type seedFlow struct {
	server   *httptest.Server
	baseURL  *url.URL
	cfg      *config.Config
	tap      *Tap
	checkins *checkin.Service
	data     *db.DB
	sessions *session.Manager
	cookies  session.Cookies
	invites  *invite.Manager
	trail    *audit.Recorder
	kek      []byte
	tenantID uuid.UUID
	runStamp string
	// logs holds every record the wired services wrote during this test, as one
	// JSON object per line (M8-03). It replaced io.Discard so the alert signals in
	// deploy/README.md can be asserted where they are PRODUCED — the decision
	// engine's own write path — rather than only where their names are declared.
	logs *syncBuffer
}

// syncBuffer is a bytes.Buffer that survives -race.
//
// slog serialises its own writes, but this repository's suite drives concurrent
// taps on purpose (the debounce and advisory-lock tests), and a second writer
// reaching the same buffer from another goroutine would be a data race in the
// TEST rather than in the product — the kind of failure that gets "fixed" by
// deleting the assertion.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	// panicOn makes this writer raise when a record containing the string is
	// written, which is the ONLY way to model a panicking slog.Handler from
	// outside slog (M8-03 round 5). checkin.go's own comment already grants that a
	// handler CAN panic — internal/httpx/requestlog.go contains a recover for
	// exactly that, measured — so the hazard is stated in the tree; until this
	// field existed nothing DROVE it. Empty means "behave like a buffer", which is
	// every other test in this package.
	panicOn string
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.panicOn != "" && bytes.Contains(p, []byte(b.panicOn)) {
		panic("syncBuffer: the log handler raised on " + b.panicOn)
	}
	return b.buf.Write(p)
}

// panicWhenWriting arms the writer. It is set AFTER the services are wired,
// because the pointer — not the value — is what the handler holds.
func (b *syncBuffer) panicWhenWriting(marker string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.panicOn = marker
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// seedVenue is what a seeded location actually says about itself, read from the
// database rather than copied out of seed.sql — a constant duplicated into a test
// stops being a check and becomes a second source of truth.
type seedVenue struct {
	ID       uuid.UUID
	Name     string
	OnSiteIP string
	Lat, Lng float64
	Timezone string
}

func newSeedFlow(t *testing.T) *seedFlow {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the seeded end-to-end tests (real Postgres required)")
	}

	cfg := &config.Config{
		Env:         config.EnvDev,
		BaseURL:     "http://localhost:8080",
		DatabaseURL: dsn,
		// Obviously fake, distinct 32-byte keys (agent-brief madde 2). They MUST
		// differ: config.Load refuses equal keys and this fixture honours the same
		// rule even though it does not go through Load.
		SessionHMACKey: []byte("SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS"),
		InviteHMACKey:  []byte("IIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII"),
		RetentionYears: 2,
		// The loopback hop httptest gives us is TRUSTED, so X-Forwarded-For can
		// carry the venue's address. Nothing wider: RealIP walks right to left and
		// stops at the first untrusted hop, so a wider list would let the request
		// choose its own address.
		TrustedProxies: []netip.Prefix{
			netip.MustParsePrefix("127.0.0.1/32"),
			netip.MustParsePrefix("::1/128"),
		},
		GPSRadiusMeters: 150,
		Debounce:        seedDebounce,
		Freshness:       seedFreshness,
	}

	data, err := db.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	// The seed must be present before anything else is worth doing. Skipping (not
	// failing) keeps a migrated-but-unseeded database — a developer's, straight
	// after `make migrate` or a `make db-reset` that stopped halfway — from turning
	// into a red run about data it never loaded.
	//
	// 🔴 CI IS NOT THAT SHAPE, and an earlier version of this line said it was.
	// MEASURED against .github/workflows/ci.yml: the workflow starts Postgres
	// (`make up`) but never runs `make migrate` and never supplies TAPPA_TAG_KEK,
	// so CI's database has no schema at all — not "migrated but unseeded",
	// UNMIGRATED. Measured by pointing `make test` at an empty database: 140
	// top-level tests fail across 17 packages, 136 of them predating this file.
	// So these four fail there too, and that is a PRE-EXISTING CI condition about
	// CI's database step rather than a regression this task introduced.
	if !seedIsLoaded(t, data) {
		t.Skip("the KF demo fixture is not in this database; run `make seed` (see skill tappa-seed)")
	}

	// The REAL KEK. Everything this file exists to check is a property of values
	// wrapped under the operator's key, so a fake one here would test nothing. It
	// is read only AFTER the seed is known to be present, so the failure is about
	// a broken environment rather than an empty one.
	raw := os.Getenv("TAPPA_TAG_KEK")
	if raw == "" {
		t.Fatal("TAPPA_TAG_KEK is not set, but the seeded plaques can only be checked against it. " +
			"Run `make test` (the Makefile exports .env) rather than a bare `go test`.")
	}
	kek, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(kek) != 32 {
		// The message never carries the value (§4.7).
		t.Fatalf("TAPPA_TAG_KEK is not 32 base64 bytes (decoded length %d)", len(kek))
	}
	cfg.TagKEK = kek

	sessions, err := session.New(data, cfg)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	invites, err := invite.New(data, cfg)
	if err != nil {
		t.Fatalf("invite.New: %v", err)
	}
	directory, err := tenant.NewDirectory(data)
	if err != nil {
		t.Fatalf("tenant.NewDirectory: %v", err)
	}
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	// 🔴 CAPTURED, NOT DISCARDED (M8-03). It used to be io.Discard, which meant the
	// only end-to-end harness in the repository threw away the one thing M8-03's
	// alert rules are computed from. Nothing else reads it, so a test that does not
	// look at seedFlow.logs behaves exactly as before.
	logs := &syncBuffer{}
	quiet := slog.New(slog.NewJSONHandler(logs, nil))
	checkins, err := checkin.New(data, trail, cfg, quiet)
	if err != nil {
		t.Fatalf("checkin.New: %v", err)
	}
	tp, err := NewTap(sun.NewVerifier(data, kek), directory, sessions, checkins, trail, cfg, quiet)
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	act, err := NewActivation(invites, sessions, trail, cfg, quiet)
	if err != nil {
		t.Fatalf("NewActivation: %v", err)
	}

	srv := httptest.NewServer(httpx.NewRouter(cfg, nil, tp, act))
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("server url: %v", err)
	}

	return &seedFlow{
		server: srv, baseURL: base, cfg: cfg, tap: tp, checkins: checkins,
		data: data, sessions: sessions, cookies: session.NewCookies(cfg),
		invites: invites, trail: trail, kek: kek, tenantID: fixtures.TenantKF,
		runStamp: newRunStamp(), logs: logs,
	}
}

// newRunStamp labels one run's ephemeral crew, and it answers a MEASURED problem
// rather than decorating the data.
//
// The crew below carries the SEEDED crew's names (skill tappa-seed: no invented
// people) but is inserted fresh every run, and `transactions` are immutable with
// DELETE revoked for tappa_app (§4.3) — so nothing ever removes them. Measured
// before this stamp existed: after a handful of `make test` runs the demo tenant
// held THIRTEEN employees called "Maria Borg", twelve of them carrying the taps,
// while the Maria Borg in seed.sql still had none. That is the opposite of what
// the fixture is for: M6's dashboard is supposed to read this database, and a
// roster of same-named duplicates is worse than no simulated day at all.
//
// The stamp does not make the rows go away — it makes them TELL YOU WHAT THEY
// ARE. A reader (or a dashboard query) can separate master data from simulation
// output on the name alone, and two runs never look like one person tapping
// twice. Why the crew is not simply the seeded crew instead is written out in
// day_db_test.go's LIMITS section (L4), with the measurement that decided it.
//
// The second half of that sentence — two RUNS, not just this run against the
// seeded roster — is what TestRunStampVariesBetweenRuns checks;
// assertTellableApart structurally cannot (see its own comment).
func newRunStamp() string {
	return "sim " + time.Now().UTC().Format("01-02T15:04:05") + " " + uuid.NewString()[:4]
}

// TestRunStampVariesBetweenRuns asks the one question assertTellableApart cannot
// ask about itself, and it exists because the gap was MEASURED: mutating
// newRunStamp to `return "CONSTANT"` left the whole suite green. Both sides of
// assertTellableApart's comparison are derived from the same f.runStamp, so a
// degenerate stamp satisfies it perfectly — the degenerate-value trap seedDebounce
// names at the top of this file, walked into by the check meant to prevent it.
//
// This test takes no database and no fixture: it is a property of the function.
// If the stamp does not VARY, it cannot tell one run's simulated Maria Borg from
// the next one's, and the demo database M6 has to read silently rots.
//
// WHY "not all equal" AND NOT "all distinct". The stamp is a one-second clock
// reading plus 16 bits of UUID, so two calls inside the same second collide with
// probability 1/65536 — real, and worth writing down as a property of the stamp
// rather than hiding. Demanding all three distinct would import that flake into
// every run for nothing; demanding they are not ALL equal still kills any
// constant with certainty, and misfires only if three draws collide twice in a
// row (~2e-10).
func TestRunStampVariesBetweenRuns(t *testing.T) {
	const draws = 3
	seen := make(map[string]struct{}, draws)
	for i := 0; i < draws; i++ {
		seen[newRunStamp()] = struct{}{}
	}
	if len(seen) == 1 {
		t.Fatalf("newRunStamp returned the same value %d times: a stamp that does not vary "+
			"cannot separate one simulated run from the next, and assertTellableApart "+
			"cannot notice because it compares the stamp against itself", draws)
	}
}

// seedIsLoaded reports whether the KF fixture is in this database.
func seedIsLoaded(t *testing.T, data *db.DB) bool {
	t.Helper()
	var n int
	err := data.WithTenant(context.Background(), fixtures.TenantKF, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM tags WHERE tenant_id = $1 AND uid = $2`,
			fixtures.TenantKF, fixtures.TagKFStJulians).Scan(&n)
	})
	if err != nil {
		t.Fatalf("look for the seed: %v", err)
	}
	return n == 1
}

// venue reads a seeded location's own facts. The on-site address is the FIRST
// registered prefix's address, so it is by construction the address that matches
// — there is no second copy of 203.0.113.15 in this package to drift.
func (f *seedFlow) venue(t *testing.T, id uuid.UUID) seedVenue {
	t.Helper()
	var (
		v        seedVenue
		prefixes []netip.Prefix
	)
	v.ID = id
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT l.name, l.static_ips, l.gps_lat, l.gps_lng, t.timezone
			  FROM locations l JOIN tenants t ON t.id = l.tenant_id
			 WHERE l.tenant_id = $1 AND l.id = $2`,
			f.tenantID, id).Scan(&v.Name, &prefixes, &v.Lat, &v.Lng, &v.Timezone)
	})
	if err != nil {
		t.Fatalf("read venue %s: %v", id, err)
	}
	if len(prefixes) == 0 {
		t.Fatalf("venue %s has no registered address range", v.Name)
	}
	v.OnSiteIP = prefixes[0].Addr().String()
	return v
}

// --- identities --------------------------------------------------------------

// phone is one employee's browser: its own cookie jar, so a session cannot leak
// between simulated people the way a shared client would let it.
type phone struct {
	name   string
	id     uuid.UUID
	client *http.Client
}

// hire inserts an employee at a seeded venue and gives them a live session,
// WITHOUT going through activation — the shape of somebody who activated weeks
// ago. activatedAt controls the practice flag: a NULL means "never activated",
// which is not what an existing employee looks like.
func (f *seedFlow) hire(t *testing.T, name string, venueID uuid.UUID, status string) *phone {
	t.Helper()
	p := f.insertEmployee(t, name, venueID, status, true)
	f.attachSession(t, p)
	return p
}

// insertEmployee writes the employee row and nothing else. The stored full_name
// carries the run stamp (see newRunStamp); p.name keeps the short one, because a
// failure message wants "Maria Borg", not a timestamp.
func (f *seedFlow) insertEmployee(t *testing.T, name string, venueID uuid.UUID, status string, activated bool) *phone {
	t.Helper()
	id := uuid.New()
	stamped := name + " [" + f.runStamp + "]"
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var activatedAt any
		if activated {
			activatedAt = time.Now().Add(-30 * 24 * time.Hour)
		}
		// 🔴 deactivated_at IS WRITTEN WHEN THE STATUS SAYS SO, and it was not until
		// M6-12 phase A found out the hard way. This helper is the ONLY thing in the
		// repository that produces employees whose status contradicts their lifecycle
		// stamps: the two product statements both COALESCE their stamp
		// (DeactivateEmployee, ConsumeInviteAndActivate) and test/fixtures/seed.sql
		// derives both from the status it inserts, so a `deactivated` row with a NULL
		// deactivated_at could only ever come from here.
		//
		// It cost two rounds of an audit: those rows accumulate in the development
		// database -- day_db_test.go hires one deactivated person per run -- and both
		// an implementer and an orchestrator read the pile as evidence about the SEED,
		// which then appeared in a migration comment as a claim about production data.
		// A fixture that produces a shape the product cannot produce is a fixture that
		// will be mistaken for evidence.
		//
		// The stamp does not change what this file tests: §5 row 4 keys on `status`,
		// which tap.Decide reads and sys:employee-deactivated denies on.
		var deactivatedAt any
		if status == "deactivated" {
			deactivatedAt = time.Now().Add(-7 * 24 * time.Hour)
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at, activated_at, deactivated_at)
			 VALUES ($1, $2, $3, $4, $5, now() - interval '60 days', $6, $7)`,
			id, f.tenantID, venueID, stamped, status, activatedAt, deactivatedAt)
		return e
	})
	if err != nil {
		t.Fatalf("hire %s: %v", name, err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &phone{name: name, id: id, client: &http.Client{Jar: jar, Timeout: 20 * time.Second}}
}

// assertTellableApart checks that this run's person is stamped in the DATABASE
// and that the seeded master row of the same name is a DIFFERENT row this run
// never touched. Without it, deleting the stamp left the whole suite green
// (measured) — and the thing that then rots is the demo database M6 has to read,
// days away from the change that broke it.
//
// 🔴 IT DOES NOT, AND CANNOT, PROVE THE STAMP IS UNIQUE. Measured: with
// newRunStamp mutated to `return "CONSTANT"` the suite stayed green, because the
// expected value on the left and the stored value on the right are BOTH derived
// from f.runStamp — the check compares the stamp against itself. That is the
// degenerate-value trap seedDebounce names at the top of this file, and naming it
// did not stop it. Two things close it, and neither belongs here: the row count
// below (a stamped name must belong to exactly ONE row, so a constant stamp
// collides the second time the suite meets the same database) and
// TestRunStampVariesBetweenRuns, which asks the function directly.
func (f *seedFlow) assertTellableApart(t *testing.T, p *phone, seededName string) {
	t.Helper()
	var (
		mine      string
		seeded    uuid.UUID
		namesakes int
	)
	stamped := seededName + " [" + f.runStamp + "]"
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx,
			`SELECT full_name FROM employees WHERE tenant_id = $1 AND id = $2`,
			f.tenantID, p.id).Scan(&mine); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx,
			`SELECT count(*) FROM employees WHERE tenant_id = $1 AND full_name = $2`,
			f.tenantID, stamped).Scan(&namesakes); e != nil {
			return e
		}
		return tx.QueryRow(ctx,
			`SELECT id FROM employees WHERE tenant_id = $1 AND full_name = $2`,
			f.tenantID, seededName).Scan(&seeded)
	})
	if err != nil {
		t.Fatalf("read employee names: %v", err)
	}
	if mine != stamped {
		t.Fatalf("the simulated %s is stored as %q, want %q: an unstamped row is "+
			"indistinguishable from the seeded roster and from every earlier run", seededName, mine, stamped)
	}
	if namesakes != 1 {
		t.Fatalf("%d employees are stored as %q, want exactly 1: the stamp is supposed to be "+
			"this run's alone, so more than one means it is not varying between runs", namesakes, stamped)
	}
	if seeded == p.id {
		t.Fatalf("this run overwrote the seeded %s instead of standing beside her", seededName)
	}
}

// attachSession issues a real session and drops the cookie into the phone's jar,
// exactly as a Set-Cookie would have.
func (f *seedFlow) attachSession(t *testing.T, p *phone) {
	t.Helper()
	issued, err := f.sessions.Issue(context.Background(), session.IssueParams{
		TenantID: f.tenantID, EmployeeID: p.id, DeviceInfo: "sim",
	})
	if err != nil {
		t.Fatalf("session.Issue for %s: %v", p.name, err)
	}
	rec := httptest.NewRecorder()
	if err := f.cookies.Set(rec, issued.Token); err != nil {
		t.Fatalf("cookies.Set: %v", err)
	}
	p.client.Jar.SetCookies(f.baseURL, rec.Result().Cookies())
}

// sessionID resolves the phone's live session id — needed to re-sign a tap
// context, and obtained the same way the server does it (read the cookie, verify
// it), so no test-only accessor into internal/session exists.
func (f *seedFlow) sessionID(t *testing.T, p *phone) uuid.UUID {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	for _, c := range p.client.Jar.Cookies(f.baseURL) {
		req.AddCookie(c)
	}
	tok, err := f.cookies.Read(req)
	if err != nil {
		t.Fatalf("%s has no readable session cookie: %v", p.name, err)
	}
	res, err := f.sessions.Verify(context.Background(), tok)
	if err != nil {
		t.Fatalf("%s's session does not verify: %v", p.name, err)
	}
	return res.ID
}

// --- driving the tap flow ----------------------------------------------------

// nfcURL is what the chip writes into the address bar: the plaque's URL plus the
// read counter and the truncated SDM MAC.
//
// IT IS ASSEMBLED BY CONCATENATION, NOT fmt.Sprintf, AND THAT IS DELIBERATE —
// written down so nobody "tidies" it back. redline R7 flags any fmt/log/slog call
// whose arguments mention `cmac`, and a single Sprintf holding the whole query
// string trips it (measured: `make audit` FAILed on exactly this line). The
// finding would be a false positive — this builds a URL and logs nothing, and the
// MAC here is the fixed wrong value zeroCMAC — but the right answer is not to
// widen the net for test files: R7 is one of the few mechanical checks that would
// catch a structured log line handing the chip's MAC to a field. The neighbouring
// helpers (tap_db_test.go's tapURLFor, qr_db_test.go's qrURLFor) already
// concatenate, so this is the file's convention rather than a dodge. Only the
// COUNTER is formatted, and that call mentions nothing R7 cares about.
//
// A SECOND, SMALLER LESSON, also measured: the first version of this very comment
// spelled out an example log call, and R7 matched the PROSE. The scanner reads
// source text, not syntax — so a paragraph explaining a rule can break it.
func nfcURL(uid string, ctr uint32, cmacHex string) string {
	return "/t?tag=" + uid + "&ctr=" + fmt.Sprintf("%06X", ctr) + "&cmac=" + cmacHex
}

// seedQRURL is the same plaque with no SUN parameters at all — a printed code.
func seedQRURL(uid string) string { return "/t?tag=" + uid }

// openPage performs GET target from the venue address and returns the status and
// body. Every simulated tap starts here, through the real router.
func (f *seedFlow) openPage(t *testing.T, p *phone, target, fromIP string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, f.server.URL+target, nil)
	if err != nil {
		t.Fatalf("build GET: %v", err)
	}
	req.Header.Set("X-Forwarded-For", fromIP)
	resp, err := p.client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(b)
}

// postCheckin submits a signed context from fromIP. The form carries only what a
// browser sends: the context, and optionally a coordinate and a declared time.
func (f *seedFlow) postCheckin(t *testing.T, p *phone, signed, fromIP string, extra url.Values) (int, string) {
	t.Helper()
	form := url.Values{"ctx": {signed}}
	for k, vs := range extra {
		form[k] = vs
	}
	req, err := http.NewRequest(http.MethodPost, f.server.URL+"/api/checkin", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build POST: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-For", fromIP)
	resp, err := p.client.Do(req)
	if err != nil {
		t.Fatalf("POST /api/checkin: %v", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(b)
}

// --- reading the result ------------------------------------------------------

// lastRow reads back one employee's most recent record. It is lastRecord's twin
// for the seeded tenant (checkin_db_test.go's version is bound to that harness).
func (f *seedFlow) lastRow(t *testing.T, employeeID uuid.UUID) recordedTap {
	t.Helper()
	var r recordedTap
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT id, verdict, type, sun_valid, ip_match, gps_match, trust, channel,
			       practice, queued, matched_sid, policy_layer, policy_version_id,
			       policy_context, ctr, tag_uid, location_id, occurred_at, note
			  FROM transactions
			 WHERE tenant_id = $1 AND employee_id = $2
			 ORDER BY created_at DESC, occurred_at DESC LIMIT 1`,
			f.tenantID, employeeID).Scan(
			&r.ID, &r.Verdict, &r.Type, &r.SunValid, &r.IPMatch, &r.GPSMatch, &r.Trust, &r.Channel,
			&r.Practice, &r.Queued, &r.MatchedSid, &r.PolicyLayer, &r.PolicyVersionID,
			&r.PolicyContext, &r.Ctr, &r.TagUID, &r.LocationID, &r.OccurredAt, &r.Note)
	})
	if err != nil {
		t.Fatalf("read back the record: %v", err)
	}
	return r
}

// rowsFor counts one employee's records, so runs of other tests cannot move a
// number under an assertion.
func (f *seedFlow) rowsFor(t *testing.T, employeeID uuid.UUID) int {
	t.Helper()
	var n int
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM transactions WHERE tenant_id = $1 AND employee_id = $2`,
			f.tenantID, employeeID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

// show renders a nullable column for a failure message. It exists because every
// interesting column on recordedTap is a POINTER (type, trust, sun_valid, ctr,
// note), and %v on a pointer prints an ADDRESS — a failure message that costs a
// second run to understand. deref (checkin_db_test.go) does this for *string
// only; this is its shape for the rest.
func show[T any](p *T) string {
	if p == nil {
		return "<none>"
	}
	return fmt.Sprint(*p)
}

// auditRows counts the tenant's audit rows for one action. It runs under
// WithTenant so RLS scopes it and another tenant's fixtures cannot inflate it.
func (f *seedFlow) auditRows(t *testing.T, action string) int {
	t.Helper()
	var n int
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`,
			f.tenantID, action).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	return n
}

// assertEnteredBy reads the provenance column a manual record must carry: §5
// requires a manual entry to name the manager who typed it, and a record nobody
// is answerable for is exactly what that rule exists to prevent.
func (f *seedFlow) assertEnteredBy(t *testing.T, txID, want uuid.UUID) {
	t.Helper()
	var got *uuid.UUID
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT entered_by FROM transactions WHERE tenant_id = $1 AND id = $2`,
			f.tenantID, txID).Scan(&got)
	})
	if err != nil {
		t.Fatalf("read entered_by: %v", err)
	}
	if got == nil || *got != want {
		t.Fatalf("entered_by = %v, want %s", got, want)
	}
}

// openCheckIns counts an employee's check-ins with no later check-out — the shape
// the "forgotten checkout" anomaly is made of.
func (f *seedFlow) openCheckIns(t *testing.T, employeeID uuid.UUID) int {
	t.Helper()
	var n int
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT count(*) FROM transactions t
			 WHERE t.tenant_id = $1 AND t.employee_id = $2 AND t.type = 'in'
			   AND NOT t.practice
			   AND NOT EXISTS (
			       SELECT 1 FROM transactions o
			        WHERE o.tenant_id = t.tenant_id AND o.employee_id = t.employee_id
			          AND o.type = 'out' AND o.occurred_at > t.occurred_at)`,
			f.tenantID, employeeID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count open check-ins: %v", err)
	}
	return n
}

func (f *seedFlow) assertNoOpenCheckIn(t *testing.T, employeeID uuid.UUID) {
	t.Helper()
	if n := f.openCheckIns(t, employeeID); n != 0 {
		t.Fatalf("%d check-in(s) left open, want 0: the shift did not close", n)
	}
}

func (f *seedFlow) assertOpenCheckIn(t *testing.T, employeeID uuid.UUID) {
	t.Helper()
	if n := f.openCheckIns(t, employeeID); n != 1 {
		t.Fatalf("%d open check-in(s), want exactly 1: §5 says the system NEVER invents "+
			"the missing checkout, it leaves the row open for a manager", n)
	}
}

// tagCounter reads a plaque's current last_ctr. The seeded plaques are shared
// across runs, so the next honest counter value is whatever this returns plus one
// — exactly what a real chip would produce.
func (f *seedFlow) tagCounter(t *testing.T, uid string) uint32 {
	t.Helper()
	tag, err := f.data.GetTagByUID(context.Background(), uid)
	if err != nil {
		t.Fatalf("read tag %s: %v", uid, err)
	}
	return uint32(tag.LastCtr) //nolint:gosec // a 24-bit chip counter, always positive
}

// wrappedKeyOf reads a plaque's stored envelope through the APPLICATION role, so
// what is asserted is what the running product can see.
func (f *seedFlow) wrappedKeyOf(t *testing.T, tenantID uuid.UUID, uid string) []byte {
	t.Helper()
	var ref []byte
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT aes_key_ref FROM tags WHERE tenant_id = $1 AND uid = $2`,
			tenantID, uid).Scan(&ref)
	})
	if err != nil {
		t.Fatalf("read aes_key_ref for %s: %v", uid, err)
	}
	return ref
}

// ---------------------------------------------------------------------------
// PART A — the seeded plaques carry real KEK-wrapped keys.
// ---------------------------------------------------------------------------

// TestSeedDB_EveryDemoPlaqueOpensUnderTheConfiguredKEK is the fixture-side half of
// the fix. Before it, tags.aes_key_ref held convert_to('FAKE-WRAPPED-KEY-...') —
// 42 bytes of ASCII — and sun.Unwrap refused it on length alone.
//
// The assertion is not "44 bytes": that would pass for 44 bytes of noise. It is
// that the envelope OPENS under the operator's KEK, with the tag's own raw UID as
// AAD, and yields exactly the key the seed says it should (fixtures.SeedTagKey).
// Two positive controls keep the check from being vacuous — a wrong KEK and a
// wrong UID must both fail, the second being ADR 0003's portability guard.
func TestSeedDB_EveryDemoPlaqueOpensUnderTheConfiguredKEK(t *testing.T) {
	f := newSeedFlow(t)

	for _, tag := range fixtures.SeedTags {
		ref := f.wrappedKeyOf(t, tag.TenantID, tag.UID)
		if len(ref) != 44 {
			t.Fatalf("%s: aes_key_ref is %d bytes, want 44 (nonce 12 || ciphertext 16 || tag 16). "+
				"A short value is the seed's ASCII placeholder and makes every NFC tap on this plaque a 500.",
				tag.UID, len(ref))
		}
		uidBytes, err := fixtures.SeedTagUIDBytes(tag.UID)
		if err != nil {
			t.Fatalf("%s: %v", tag.UID, err)
		}
		key, err := sun.Unwrap(f.kek, uidBytes, ref)
		if err != nil {
			t.Fatalf("%s: the stored envelope does not open under TAPPA_TAG_KEK: %v", tag.UID, err)
		}
		want := fixtures.SeedTagKey(tag.UID)
		// bytes.Equal, not a hand-rolled loop. An earlier draft avoided it on the
		// theory that redline R7 flags bytes.Equal near key material; MEASURED, that
		// was wrong — R7's WARN pattern is
		// `bytes\.Equal\([^)]*([Cc]mac|[Mm]ac|[Tt]oken)`, which this call does not
		// match, and `make audit` is clean with it. The rule R7 is really guarding
		// (constant time) does not apply either: both sides are derived from public
		// repo material, so there is no secret to leak through timing.
		if !bytes.Equal(key, want) {
			// Lengths and equality only — never the bytes (§4.7).
			t.Fatalf("%s: the unwrapped key is not the documented fixture key (got %d bytes, want %d)",
				tag.UID, len(key), len(want))
		}
	}

	// POSITIVE CONTROL 1: a different KEK must NOT open it, or the loop above is
	// asserting that GCM accepts anything.
	wrongKEK := make([]byte, 32)
	for i := range wrongKEK {
		wrongKEK[i] = 0x5A
	}
	ref := f.wrappedKeyOf(t, fixtures.TenantKF, fixtures.TagKFStJulians)
	uidBytes, _ := fixtures.SeedTagUIDBytes(fixtures.TagKFStJulians)
	if _, err := sun.Unwrap(wrongKEK, uidBytes, ref); err == nil {
		t.Fatal("a wrong KEK opened the envelope: the check above proves nothing")
	}

	// POSITIVE CONTROL 2: the ADR 0003 portability guard. The UID is the GCM AAD,
	// so one plaque's envelope must not open against another plaque's identity —
	// which is what stops a rewritten aes_key_ref from being moved between rows.
	otherUID, _ := fixtures.SeedTagUIDBytes(fixtures.TagKFMellieha)
	if _, err := sun.Unwrap(f.kek, otherUID, ref); err == nil {
		t.Fatal("St Julians' envelope opened against Mellieha's UID: the AAD binding is not doing its job")
	}
}

// TestSeedDB_ANFCTapOnASeededPlaqueReachesTheDecisionEngine is the HTTP half, and
// it is the acceptance evidence for the fix: GET /t on a SEEDED plaque with SUN
// parameters used to answer 500 out of sun.Unwrap ("wrapped ref must be 44 bytes,
// got 42"), which meant the NFC channel was dead on every demo plaque.
//
// The CMAC here is deliberately wrong — minting a real one would mean a second
// implementation of the SDM MAC outside internal/sun, which this repo has
// declined more than once (tap_db_test.go states why). That costs the test
// nothing it needs: reaching sun.Unwrap AT ALL is the thing that was broken, and
// a wrong MAC only reaches the comparison if the key was successfully opened
// first. The outcome is therefore a RECORDED reject (§5 row 2, §4.6) rather than
// an `ok`, and the assertions say so.
func TestSeedDB_ANFCTapOnASeededPlaqueReachesTheDecisionEngine(t *testing.T) {
	f := newSeedFlow(t)
	venue := f.venue(t, fixtures.LocKFStJulians)
	p := f.hire(t, "Seed Probe", venue.ID, "active")

	ctr := f.tagCounter(t, fixtures.TagKFStJulians) + 1
	status, page := f.openPage(t, p, nfcURL(fixtures.TagKFStJulians, ctr, "0000000000000000"), venue.OnSiteIP)
	if status == http.StatusInternalServerError {
		t.Fatal("GET /t on a seeded plaque answered 500 — tags.aes_key_ref is not a 44-byte KEK envelope. " +
			"Run `make seed`: the key-wrapping half is scripts/seed.sh step 2.")
	}
	if status != http.StatusOK {
		t.Fatalf("GET /t status = %d, want 200", status)
	}
	if !strings.Contains(page, venue.Name) {
		t.Fatalf("the tap page did not name the seeded venue %q", venue.Name)
	}

	signed := contextFieldOf(t, page)
	decoded, err := f.tap.contexts.parse(signed, f.sessionID(t, p))
	if err != nil {
		t.Fatalf("the page's own context did not parse: %v", err)
	}
	if decoded.Channel != sun.ChannelNFC {
		t.Fatalf("channel = %q, want nfc: a URL with ctr and cmac is an NFC arrival", decoded.Channel)
	}
	if decoded.CMACVerified {
		t.Fatal("a deliberately wrong CMAC verified")
	}
	if decoded.TagTenantID != fixtures.TenantKF || decoded.LocationID != fixtures.LocKFStJulians {
		t.Fatalf("the context resolved to tenant %s / location %s, want the seeded KF St Julians",
			decoded.TagTenantID, decoded.LocationID)
	}

	before := f.rowsFor(t, p.id)
	ctrBefore := f.tagCounter(t, fixtures.TagKFStJulians)
	code, _ := f.postCheckin(t, p, signed, venue.OnSiteIP, nil)
	if code != http.StatusOK {
		t.Fatalf("POST status = %d, want 200 (a reject is a rendered outcome, not an HTTP error)", code)
	}
	if after := f.rowsFor(t, p.id); after != before+1 {
		t.Fatalf("rows went %d -> %d, want +1: §4.6 records the reject", before, after)
	}
	rec := f.lastRow(t, p.id)
	if rec.Verdict != "reject" || rec.MatchedSid == nil || *rec.MatchedSid != "sys:sun-invalid" {
		t.Fatalf("verdict/sid = %q/%q, want reject/sys:sun-invalid", rec.Verdict, deref(rec.MatchedSid))
	}
	// §4.4: an unverified MAC must not advance the plaque's counter.
	//
	// 🔴 THE PRECONDITION IS EXCLUSIVE OWNERSHIP OF A SHARED, UNRESETTABLE COUNTER,
	// and nothing enforces it — so it is written down instead. It holds today
	// because neither this file nor day_db_test.go calls t.Parallel(), and
	// fixtures.TagKF* appears in no other file. If somebody adds t.Parallel(), a
	// concurrent test advancing last_ctr would be reported here as a §4.4
	// VIOLATION, and a red line that cries wolf is how a real violation gets lost
	// in the noise. It has already been seen: the parallelism experiment recorded
	// in the M5-09 card produced exactly this line ("last_ctr moved 192 -> 193 on
	// an unverified MAC") with the guardrail perfectly intact. Hence the message
	// below names the other explanation.
	//
	// The comparison stays `!=` rather than the cheaper-looking `<`. UpsertTagCtr
	// only ever writes under a WHERE clause that requires the stored counter to be
	// strictly below the presented one (db/queries/tags.sql, §4.4), so the counter
	// is monotone non-decreasing and `got < ctrBefore` is UNREACHABLE — it would
	// turn a live check into a dead one, which is the same trap this file keeps
	// naming, one file over. (That predicate is DESCRIBED rather than quoted here
	// on purpose: redline R4 warns on the literal form, and a paragraph explaining
	// a rule should not set off the scanner for it — the lesson nfcURL records.)
	if got := f.tagCounter(t, fixtures.TagKFStJulians); got != ctrBefore {
		t.Fatalf("last_ctr moved %d -> %d on an unverified MAC (§4.4). If this run was "+
			"parallel, suspect a fixture race on the shared seeded plaque before suspecting "+
			"the guardrail: these tests all read last_ctr+1 off the same tag.", ctrBefore, got)
	}
}
