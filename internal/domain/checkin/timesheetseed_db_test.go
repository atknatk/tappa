//go:build timesheetseed

package checkin

// timesheetseed_db_test.go -- `make seed-timesheets` (M11 API-14, ADR 0046): a test business for
// Malta Calculator to read GET /v1/timesheets against, with ONE PAST WEEK of records written by the
// decision engine itself.
//
// 🔴 IT IS A DATA GENERATOR, NOT A TEST, AND THE BUILD TAG IS WHAT KEEPS IT OUT OF `make test`. Behind
// `timesheetseed` no `go test ./...`, `make test` or `make check` compiles it (so it adds no SKIP to a
// suite whose rule is "no SKIP with .env"), and nothing of it is in any binary: a _test.go file never
// is. It lives in package checkin for one reason -- Service.now, the decision's only clock, is
// settable from this package's tests and from nowhere else in the product. A past week cannot be
// tapped live, and a tap that DECLARES a past instant is the offline-queue path (flagged after 120 s,
// refused after 72 h -- sys:occurred-at-bound), so the seed stands the engine's clock at each tap's
// instant instead: every tap goes through Record -- the plaque resolve, the atomic counter advance
// (§4.4), the policy engine's verdict, the business day, the INSERT and the feed's triggers -- with
// that clock. The one leg it cannot move is the debounce's database clock (ADR 0006: the age of the
// person's last record, clock_timestamp() − created_at), so the seed waits that out in real time,
// per person and measured on the database's own clock (seedWaitOut: 65 s since that person's last
// record) -- the people are written in turn, so the waits overlap: about ten minutes in all.
// The manual records go through internal/domain/manual's Recorder -- the panel's own write.
//
// 🔴 IT REFUSES ANY DATABASE BUT A DEVELOPMENT OR A SANDBOX ONE, before it writes anything:
//
//	TAPPA_ENV           dev or sandbox
//	the two DSNs        on a loopback host (the compose database, or a sandbox reached by
//	                    `kubectl port-forward`)
//	the database itself dev: the dev seed's Kebab Factory tenant (fixtures.TenantKF) exists;
//	                    sandbox: every plaque in it is virtual (db.IsVirtualTagUID) -- a production
//	                    database holds real NXP plaques and has no dev seed
//
// 🔴 NOTHING IS DELETED, EVER. transactions is immutable (§4.3) and a tags row is never removed (a
// plaque's key goes with it): a business that already holds records is refused ("seeded once per
// database"), and a run that stopped half-way is resumed only if it wrote no record yet.
//
// TWO MODES. Without TAPPA_TIMESHEET_SEED_TENANT the seed creates "Malta Calculator Test Ltd"
// (fixed id below) with the API enabled (company code MCT) and an owner who cannot sign in
// (fixtures.UnusablePasswordHash; address at example.com).
//
// IF A TAP IS NOT JUDGED AS THE WEEK WANTS (an ignored repeat, a refusal) THE SEED STOPS AT ONCE and
// says in one message how much it wrote, why that business is now spent, and how to start again
// (seedStopped). The debounce wait is measured on the database's clock per person (seedWaitOut). With it -- the id of a business signed up
// through the sandbox's own panel, whose API the sandbox operator enabled, so that its owner can
// issue a tt_test_ key -- the seed fills THAT business, which must have "test" in its name.
//
// THE WEEK is the last full Monday-Sunday week before today, in Europe/Malta. Six employees, each
// showing one thing a payroll reads:
//
//	MCT-001 Alex Sample         Mon-Fri 09:00-17:00, NFC              -- the ordinary week
//	MCT-002 Jamie Example       Mon-Fri 08:00-16:30, Wednesday by QR  -- the QR channel
//	MCT-003 Morgan Night        Tue-Sat 18:00-02:00                   -- a night shift, on the day it starts
//	MCT-004 Robin Placeholder   Mon-Thu 10:00-18:00; Friday typed     -- manualShifts, days[].manual
//	                            by a manager at both ends
//	MCT-005 Casey Demo          Mon-Wed 09:00-17:00; Thursday checked -- openEntries, days[].open
//	                            in and never out
//	MCT-006 Taylor Test         Mon-Thu 12:00-20:00; Friday's check-  -- awaitingMinutes (complete: false)
//	                            in from outside the venue's network,
//	                            no GPS: flagged, nobody decided
//
// Names are invented and obviously so (no real person), there is no e-mail address on an employee,
// and the venue's network is a documentation range (203.0.113.0/24).

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/test/fixtures"
)

// The created business's fixed ids, so a second run finds the first's rows.
var (
	seedTenant = uuid.MustParse("5eed7135-0000-4000-8000-000000000001")
	seedOwner  = uuid.MustParse("5eed7135-0000-4000-8000-0000000000a1")
	seedVenue  = uuid.MustParse("5eed7135-0000-4000-8000-0000000000b1")
)

const (
	seedName       = "Malta Calculator Test Ltd"
	seedCode       = "MCT"
	seedVenueRef   = "MCT-VLT"
	seedVenueName  = "MCT Valletta (seed)"
	seedOwnerEmail = "owner@malta-calculator-test.example.com"
	seedTenantEnv  = "TAPPA_TIMESHEET_SEED_TENANT"
	// seedGap is how old one person's last record must be, on the DATABASE's clock, before their next
	// tap is written: ADR 0006's 60 seconds and a margin (seedWaitOut).
	seedGap = 65 * time.Second
)

var (
	seedOnNet  = netip.MustParseAddr("203.0.113.40")  // inside the venue's documentation range
	seedOffNet = netip.MustParseAddr("198.51.100.40") // a phone on mobile data, outside it
)

// seedTap is one tap of the week: who, when (Malta wall clock), how, and the verdict the engine must
// reach -- the seed stops if it reaches another, rather than seed a week nobody designed.
type seedTap struct {
	who     int
	at      time.Time
	channel tap.Channel
	from    netip.Addr
	want    tap.Verdict
}

// seedTyped is one record a manager types.
type seedTyped struct {
	who int
	at  time.Time
	dir manual.Direction
}

type seedPerson struct {
	ref, first, last, code string
	id                     uuid.UUID
}

func TestSeedDB_MaltaCalculatorTestWeek(t *testing.T) {
	env := os.Getenv("TAPPA_ENV")
	if env != config.EnvDev && env != config.EnvSandbox {
		t.Fatalf("refused: TAPPA_ENV is %q; this seed writes only to a dev or a sandbox database", env)
	}
	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	for name, dsn := range map[string]string{"DATABASE_URL": appDSN, "DATABASE_MIGRATE_URL": ownerDSN} {
		if !seedLoopback(dsn) {
			t.Fatalf("refused: %s is not a database on a loopback host (the compose database, or a sandbox through kubectl port-forward)", name)
		}
	}
	kek, err := base64.StdEncoding.DecodeString(os.Getenv("TAPPA_TAG_KEK"))
	if err != nil || len(kek) != 32 {
		t.Fatal("refused: TAPPA_TAG_KEK must be the database's own 32-byte key (base64): the plaque's key is wrapped under it")
	}
	defer sun.Zero(kek)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cfg := &config.Config{
		Env: config.EnvDev, BaseURL: "http://localhost:8080", DatabaseURL: config.NewDSN(appDSN),
		GPSRadiusMeters: 150, Debounce: 60 * time.Second, Freshness: 180 * time.Second,
		BusinessDayFrom: fixtures.BusinessDaySwitchOn(),
	}
	data, err := db.New(ctx, cfg)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer data.Close()
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	defer func() { _ = owner.Close(context.Background()) }()
	if err := seedDatabaseIs(ctx, owner, env); err != nil {
		t.Fatalf("refused: %v", err)
	}

	trail, err := audit.New(data)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(data, trail, cfg, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	typist, err := manual.NewRecorder(data, trail, quietLogger())
	if err != nil {
		t.Fatal(err)
	}

	// --- the business ----------------------------------------------------------------------------
	tenantID, ownerID, venue := seedTenant, seedOwner, seedVenue
	if v := os.Getenv(seedTenantEnv); v != "" {
		if tenantID, err = uuid.Parse(v); err != nil {
			t.Fatalf("%s is not a business id", seedTenantEnv)
		}
		if ownerID, venue, err = seedAttach(ctx, owner, tenantID); err != nil {
			t.Fatalf("refused: %v", err)
		}
	} else if err := seedCreate(ctx, owner, data); err != nil {
		t.Fatal(err)
	}
	var records int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE tenant_id = $1`, tenantID).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records > 0 {
		t.Fatalf("refused: the business already holds %d records -- the week is seeded once per database (transactions are immutable, §4.3)", records)
	}

	// --- the plaque: virtual, its key wrapped under this database's KEK -----------------------------
	uid := db.VirtualTagUIDPrefix + strings.ToUpper(hex.EncodeToString(venue[:5]))
	var plaques int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM tags WHERE uid = $1`, uid).Scan(&plaques); err != nil {
		t.Fatal(err)
	}
	if plaques == 0 {
		uidBytes, _ := hex.DecodeString(uid)
		key := make([]byte, 16)
		if _, err := rand.Read(key); err != nil {
			t.Fatal(err)
		}
		wrapped, err := sun.Wrap(kek, uidBytes, key)
		sun.Zero(key)
		if err != nil {
			t.Fatalf("wrap the plaque key: %v", err)
		}
		if err := fixtures.LoadPlaque(ctx, owner, fixtures.Plaque{UID: uid, TenantID: tenantID, Wall: venue, Key: wrapped}); err != nil {
			t.Fatalf("load the plaque: %v", err)
		}
	}
	var ctr uint32
	if err := owner.QueryRow(ctx, `SELECT last_ctr FROM tags WHERE uid = $1 AND tenant_id = $2 AND location_id = $3 AND status = 'active'`,
		uid, tenantID, venue).Scan(&ctr); err != nil {
		t.Fatalf("the venue's plaque is not this business's, on this venue, in service: %v", err)
	}

	// --- the crew ----------------------------------------------------------------------------------
	crew := []seedPerson{
		{ref: "MCT-001", first: "Alex", last: "Sample", code: "1001"},
		{ref: "MCT-002", first: "Jamie", last: "Example", code: "1002"},
		{ref: "MCT-003", first: "Morgan", last: "Night", code: "1003"},
		{ref: "MCT-004", first: "Robin", last: "Placeholder", code: "1004"},
		{ref: "MCT-005", first: "Casey", last: "Demo", code: "1005"},
		{ref: "MCT-006", first: "Taylor", last: "Test", code: "1006"},
	}
	for i := range crew {
		if crew[i].id, err = seedEmployee(ctx, owner, tenantID, venue, crew[i]); err != nil {
			t.Fatal(err)
		}
	}

	// --- the week ----------------------------------------------------------------------------------
	zone, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(zone)
	thisWeek := ledger.WeekOf(ledger.Date{Year: now.Year(), Month: now.Month(), Day: now.Day()}, zone)
	monday := thisWeek.From.AddDate(0, 0, -7)
	wall := func(day, hh, mm int) time.Time {
		return time.Date(monday.Year(), monday.Month(), monday.Day()+day, hh, mm, 0, 0, zone).UTC()
	}
	nfc := func(who, day, hh, mm int) seedTap {
		return seedTap{who: who, at: wall(day, hh, mm), channel: tap.ChannelNFC, from: seedOnNet, want: tap.VerdictOK}
	}
	var taps []seedTap
	for d := 0; d < 5; d++ { // Monday..Friday
		taps = append(taps, nfc(0, d, 9, 0), nfc(0, d, 17, 0))
		in := nfc(1, d, 8, 0)
		if d == 2 {
			in.channel = tap.ChannelQR
		}
		taps = append(taps, in, nfc(1, d, 16, 30))
		taps = append(taps, nfc(2, d+1, 18, 0), nfc(2, d+2, 2, 0)) // Tuesday..Saturday nights
		if d < 4 {
			taps = append(taps, nfc(3, d, 10, 0), nfc(3, d, 18, 0))
			taps = append(taps, nfc(5, d, 12, 0), nfc(5, d, 20, 0))
		}
		if d < 3 {
			taps = append(taps, nfc(4, d, 9, 0), nfc(4, d, 17, 0))
		}
	}
	taps = append(taps, nfc(4, 3, 9, 0)) // Casey: Thursday in, never out
	friday := seedTap{who: 5, at: wall(4, 12, 0), channel: tap.ChannelNFC, from: seedOffNet, want: tap.VerdictFlag}
	taps = append(taps, friday, nfc(5, 4, 20, 0)) // Taylor: Friday flagged, then out on site
	typed := []seedTyped{{who: 3, at: wall(4, 10, 0), dir: manual.In}, {who: 3, at: wall(4, 18, 0), dir: manual.Out}}

	// Each person's taps in time order; round k writes everybody's k-th, each after that person's own
	// debounce wait (seedWaitOut) -- the first person of a round waits about seedGap, the rest little.
	perPerson := map[int][]seedTap{}
	for _, tp := range taps {
		perPerson[tp.who] = append(perPerson[tp.who], tp)
	}
	rounds := 0
	for who := range perPerson {
		sort.Slice(perPerson[who], func(i, j int) bool { return perPerson[who][i].at.Before(perPerson[who][j].at) })
		rounds = max(rounds, len(perPerson[who]))
	}
	written := 0
	for k := 0; k < rounds; k++ {
		for who := range crew {
			if k >= len(perPerson[who]) {
				continue
			}
			tp := perPerson[who][k]
			// The debounce's server leg (ADR 0006) is the age of THIS person's last record on the
			// database's clock, so the wait is measured there, per person -- not on this process's
			// clock from a round's start, which a slow link (a port-forward) eats into (the API-14
			// review measured a 59.33 s gap and an ignored tap that way).
			if err := seedWaitOut(ctx, owner, tenantID, crew[who].id); err != nil {
				t.Fatal(seedStopped(ctx, owner, tenantID, crew[who].ref, tp.at.In(zone), err.Error()))
			}
			if tp.channel == tap.ChannelNFC {
				// Only a chip's read advances the counter; a QR code carries none. Advancing it for a QR
				// tap would leave a gap the next NFC read is flagged for (base:ctr-gap-review).
				ctr++
			}
			svc.now = func() time.Time { return tp.at }
			res, err := svc.Record(ctx, Request{SessionTenantID: tenantID, EmployeeID: crew[who].id, TagUID: uid, Ctr: ctr,
				Channel: tp.channel, CMACVerified: tp.channel == tap.ChannelNFC, TagTenantID: tenantID, SourceIP: tp.from,
				PageIssuedAt: tp.at.Add(-3 * time.Second)})
			svc.now = nil
			if err != nil {
				t.Fatal(seedStopped(ctx, owner, tenantID, crew[who].ref, tp.at.In(zone), err.Error()))
			}
			if res.Outcome != OutcomeRecorded || res.Decision.Verdict != tp.want {
				t.Fatal(seedStopped(ctx, owner, tenantID, crew[who].ref, tp.at.In(zone),
					fmt.Sprintf("the engine answered %s/%s (%s), the week wants a recorded %s", res.Outcome, res.Decision.Verdict, res.Decision.MatchedSid, tp.want)))
			}
			written++
		}
		t.Logf("round %d/%d written (%d taps so far)", k+1, rounds, written)
	}
	for _, e := range typed {
		if _, err := typist.Record(ctx, manual.Entry{TenantID: tenantID, EmployeeID: crew[e.who].id, EnteredBy: ownerID,
			Direction: e.dir, At: e.at, Note: "Phone left at home (seed)", LocationID: venue}); err != nil {
			t.Fatal(seedStopped(ctx, owner, tenantID, crew[e.who].ref, e.at.In(zone), "the typed "+string(e.dir)+": "+err.Error()))
		}
		written++
	}

	// --- what the timesheet now answers -------------------------------------------------------------
	reader, err := ledger.NewReader(data, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	sunday := monday.AddDate(0, 0, 6)
	page, err := reader.Timesheet(ctx, tenantID, ledger.TimesheetQuery{
		From: ledger.Date{Year: monday.Year(), Month: monday.Month(), Day: monday.Day()},
		To:   ledger.Date{Year: sunday.Year(), Month: sunday.Month(), Day: sunday.Day()}, Limit: 100, Bound: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("seeded %q (%s): %d employees, %d records, the week %s..%s (Europe/Malta)", seedNameOf(ctx, owner, tenantID), tenantID,
		len(crew), written, monday.Format("2006-01-02"), sunday.Format("2006-01-02"))
	for _, p := range page.People {
		h := p.Hours
		t.Logf("  %-8s worked %4d min, awaiting %3d min, shifts %d, open %d, check-in missing %d, manual shifts %d",
			seedDeref(p.Employee.ExternalRef), int64(h.Worked/time.Minute), int64(h.Awaiting/time.Minute), h.Shifts, h.Open, h.OutWithoutIn, h.ManualShifts)
	}
	t.Logf("GET /v1/timesheets?from=%s&to=%s", monday.Format("2006-01-02"), sunday.Format("2006-01-02"))
}

func seedDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// seedWaitOut waits until the person's last nfc/qr record is seedGap old on the database's clock --
// the clock the debounce's server leg reads (clock_timestamp() - created_at, ADR 0006). It asks the
// database again after each sleep, so a slow link costs time, never a tap.
func seedWaitOut(ctx context.Context, owner *pgx.Conn, tenantID, emp uuid.UUID) error {
	for {
		var left int64
		if err := owner.QueryRow(ctx, `
			SELECT coalesce(ceil(EXTRACT(EPOCH FROM (max(created_at) + make_interval(secs => $3) - clock_timestamp())) * 1000), 0)::bigint
			  FROM transactions WHERE tenant_id = $1 AND employee_id = $2 AND channel IN ('nfc', 'qr')`,
			tenantID, emp, int64(seedGap/time.Second)).Scan(&left); err != nil {
			return fmt.Errorf("reading the debounce's clock: %w", err)
		}
		if left <= 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(left) * time.Millisecond):
		}
	}
}

// seedStopped is the one message a seed that stopped part-way gives: what is written, why the
// business is now spent, and how to start again.
func seedStopped(ctx context.Context, owner *pgx.Conn, tenantID uuid.UUID, ref string, at time.Time, why string) string {
	var n int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE tenant_id = $1`, tenantID).Scan(&n); err != nil {
		n = -1
	}
	return fmt.Sprintf("STOPPED at %s's tap of %s: %s.\n"+
		"  Written: %d records to business %s. They stay: transactions are immutable (CLAUDE.md §4.3), nothing deletes them.\n"+
		"  This business can no longer be seeded: a second run refuses a business that holds records, so its week would stay half-written.\n"+
		"  To start again: give the seed another business -- sign one up in the panel (a name with \"test\" in it), have the operator\n"+
		"  enable its API, and run again with %s=<its id> -- or, on a local sandbox, a new database (make sandbox-local SANDBOX_DB=<new name>).",
		ref, at.Format("Mon 2006-01-02 15:04"), why, n, tenantID, seedTenantEnv)
}

// seedLoopback says whether a DSN is a postgres:// URL whose one host is a loopback address or
// localhost. libpq lets the query's host= or hostaddr= override the authority (`…@localhost:1/db?host=
// 10.0.0.5` connects to 10.0.0.5) and reads a keyword=value string with no authority at all: both
// shapes are refused rather than parsed, and so is a host list (a,b).
func seedLoopback(dsn string) bool {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || strings.Contains(u.Host, ",") {
		return false
	}
	for k := range u.Query() {
		if k = strings.ToLower(k); k == "host" || k == "hostaddr" {
			return false
		}
	}
	if u.Hostname() == "localhost" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}

// seedDatabaseIs asks the database itself which kind it is (see the file comment).
func seedDatabaseIs(ctx context.Context, owner *pgx.Conn, env string) error {
	// THE OWNER DSN MUST SEE EVERY ROW (scripts/rotate-kek.sh's guard): tags and transactions carry
	// FORCE ROW LEVEL SECURITY, so a role RLS filters would read no plaque and no record -- and the
	// two checks below (every plaque virtual, the business holds no record) would pass on nothing.
	var bypass bool
	if err := owner.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&bypass); err != nil {
		return fmt.Errorf("the RLS-bypass probe: %w", err)
	}
	if !bypass {
		return errors.New("DATABASE_MIGRATE_URL's role neither is a superuser nor has BYPASSRLS: row level security would hide the rows the seed's checks count")
	}
	switch env {
	case config.EnvDev:
		var seeded bool
		if err := owner.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tenants WHERE id = $1)`, fixtures.TenantKF).Scan(&seeded); err != nil {
			return err
		}
		if !seeded {
			return errors.New("TAPPA_ENV=dev, but this database has no dev seed (make seed): it is not a development database")
		}
	default:
		rows, err := owner.Query(ctx, `SELECT uid FROM tags`)
		if err != nil {
			return err
		}
		uids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, u := range uids {
			if !db.IsVirtualTagUID(u) {
				return errors.New("TAPPA_ENV=sandbox, but this database holds a plaque outside the virtual range: it is not a sandbox database")
			}
		}
	}
	return nil
}

// seedCreate writes the business, its owner and its venue -- idempotently: a second run finds them.
func seedCreate(ctx context.Context, owner *pgx.Conn, data *db.DB) error {
	if _, err := owner.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
	        VALUES ($1, $2, 'MT-TEST-MALTA-CALCULATOR', 'restaurant', 'multi') ON CONFLICT (id) DO NOTHING`, seedTenant, seedName); err != nil {
		return fmt.Errorf("the business: %w", err)
	}
	if _, err := owner.Exec(ctx, `UPDATE tenants SET api_code = $2, api_enabled_at = coalesce(api_enabled_at, now())
	        WHERE id = $1 AND (api_code IS DISTINCT FROM $2 OR api_enabled_at IS NULL)`, seedTenant, seedCode); err != nil {
		return fmt.Errorf("the business's API: %w", err)
	}
	return data.WithTenant(ctx, seedTenant, func(ctx context.Context, tx pgx.Tx) error {
		// The owner is written ONCE, as one plain INSERT of a fresh row (cmd/opadmin's
		// TestTablesLock_AdminRowsAreWrittenUnderTheHolder: no ON CONFLICT, no link column), after a
		// read that finds the row a first run already wrote.
		var have bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM admin_users WHERE tenant_id = $1 AND id = $2)`, seedTenant, seedOwner).Scan(&have); err != nil {
			return fmt.Errorf("the owner: %w", err)
		}
		if !have {
			if _, err := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			        VALUES ($1, $2, 'Test Owner', $3, $4, 'owner', 'active')`,
				seedOwner, seedTenant, seedOwnerEmail, fixtures.UnusablePasswordHash); err != nil {
				return fmt.Errorf("the owner: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name, external_ref, static_ips)
		        VALUES ($1, $2, $3, $4, '{203.0.113.0/24}') ON CONFLICT (id) DO NOTHING`, seedVenue, seedTenant, seedVenueName, seedVenueRef); err != nil {
			return fmt.Errorf("the venue: %w", err)
		}
		return nil
	})
}

// seedAttach checks a business the sandbox's own panel signed up: its name says test, its API is
// enabled, it has an active owner (who types the manual records). Its venue MCT-VLT is created when
// it has none.
func seedAttach(ctx context.Context, owner *pgx.Conn, tenantID uuid.UUID) (ownerID, venue uuid.UUID, err error) {
	var name string
	var enabled *time.Time
	if err = owner.QueryRow(ctx, `SELECT name, api_enabled_at FROM tenants WHERE id = $1`, tenantID).Scan(&name, &enabled); err != nil {
		return ownerID, venue, fmt.Errorf("no such business: %w", err)
	}
	if !strings.Contains(strings.ToLower(name), "test") {
		return ownerID, venue, errors.New("the business's name does not say it is a test business")
	}
	if enabled == nil {
		return ownerID, venue, errors.New("the business's API is not enabled: the sandbox operator opens it first (ADR 0028 §2)")
	}
	if err = owner.QueryRow(ctx, `SELECT id FROM admin_users WHERE tenant_id = $1 AND role = 'owner' AND status = 'active' ORDER BY created_at LIMIT 1`,
		tenantID).Scan(&ownerID); err != nil {
		return ownerID, venue, fmt.Errorf("the business has no active owner: %w", err)
	}
	err = owner.QueryRow(ctx, `INSERT INTO locations (tenant_id, name, external_ref, static_ips) VALUES ($1, $2, $3, '{203.0.113.0/24}')
	        ON CONFLICT DO NOTHING RETURNING id`, tenantID, seedVenueName, seedVenueRef).Scan(&venue)
	if errors.Is(err, pgx.ErrNoRows) {
		err = owner.QueryRow(ctx, `SELECT id FROM locations WHERE tenant_id = $1 AND external_ref = $2`, tenantID, seedVenueRef).Scan(&venue)
	}
	return ownerID, venue, err
}

// seedEmployee finds or writes one employee: active, activated (ADR 0026: the activation touch is no
// record, so the first tap is an ordinary check-in), homed at the venue, no e-mail address.
func seedEmployee(ctx context.Context, owner *pgx.Conn, tenantID, venue uuid.UUID, p seedPerson) (uuid.UUID, error) {
	var id uuid.UUID
	err := owner.QueryRow(ctx, `SELECT id FROM employees WHERE tenant_id = $1 AND external_ref = $2 AND status <> 'deactivated'`, tenantID, p.ref).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return id, err
	}
	err = owner.QueryRow(ctx, `INSERT INTO employees (tenant_id, location_id, external_ref, employee_code, first_name, last_name, full_name, status, invited_at, activated_at)
	        VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', now(), now()) RETURNING id`,
		tenantID, venue, p.ref, p.code, p.first, p.last, p.first+" "+p.last).Scan(&id)
	if err != nil {
		return id, fmt.Errorf("employee %s: %w", p.ref, err)
	}
	return id, nil
}

func seedNameOf(ctx context.Context, owner *pgx.Conn, tenantID uuid.UUID) string {
	var name string
	if err := owner.QueryRow(ctx, `SELECT name FROM tenants WHERE id = $1`, tenantID).Scan(&name); err != nil {
		return "?"
	}
	return name
}

// TestSeedLoopback_RefusesAHostTheQueryOrAKeywordStringNames: the gate reads what libpq connects to.
// Run with the tag: go test -tags timesheetseed -run TestSeedLoopback_RefusesAHostTheQueryOrAKeywordStringNames ./internal/domain/checkin.
func TestSeedLoopback_RefusesAHostTheQueryOrAKeywordStringNames(t *testing.T) {
	for dsn, want := range map[string]bool{
		"postgres://tappa_app:x@127.0.0.1:55610/tappa?sslmode=disable":             true,
		"postgresql://tappa_app:x@localhost/tappa":                                 true,
		"postgres://tappa_app:x@[::1]:5432/tappa":                                  true,
		"postgres://tappa_app:x@localhost:1/tappa?host=127.0.0.1&port=55610":       false,
		"postgres://tappa_app:x@localhost/tappa?sslmode=disable&HostAddr=10.0.0.5": false,
		"postgres://tappa_app:x@localhost,db.example.com/tappa":                    false,
		"host=127.0.0.1 port=55610 user=tappa_app dbname=tappa":                    false,
		"postgres://tappa_app:x@db.example.com/tappa":                              false,
	} {
		if got := seedLoopback(dsn); got != want {
			t.Errorf("seedLoopback(%q) = %v, want %v", dsn, got, want)
		}
	}
}
