package db

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// feed_test.go -- M11 API-2 (ADR 0028 §4-§6; ADR 0027 §8): the punch feed's tables (00048) and its
// two writers (00049) against real Postgres, inside opTx's rolled-back transaction. The concurrent
// halves (the stress, the missing counter rows, the mutants) are feedstress_test.go's; the restore
// verifier, the fresh pod and the backfill's time are feedrestore_test.go's.

const (
	op00048File = "00048_create_the_punch_feed_tables.sql"
	op00049File = "00049_write_the_punch_feed.sql"
	op00050File = "00050_backfill_the_punch_feed.sql"
	// T130: the retention's column, trigger, policy and DELETE (walked with the feed), and its index
	// (not walked -- 00048's Down drops the table, the index with it; the Up again builds it).
	op00058File = "00058_keep_the_deliveries_ninety_days.sql"
	op00059File = "00059_index_the_settled_deliveries.sql"
)

// feedHoldingFeed00046MD5 is md5(prosrc) of 00046's holding_feed -- the body 00049's precondition
// requires and its Down gives back.
const feedHoldingFeed00046MD5 = "afcbc54933fbb408c56c2420d837d5cc"

// feedTables are the four tables 00048 creates.
var feedTables = []string{"feed_counters", "punch_events", "webhook_endpoints", "webhook_deliveries"}

// ------------------------------------------------------------------ helpers --

// feedEvent is one punch_events row as a test reads it (as the owner).
type feedEvent struct {
	tenant, punchTenant, employee                   uuid.UUID
	seq                                             int64
	roles, channel, direction                       string
	employeeRef, employeeCode, employeeCompany      *string
	locationID                                      *uuid.UUID
	locationRef, locationName, locationCompany, tag *string
	punchTime, recordedAt, createdAt                time.Time
	version                                         int32
	status                                          string
	directionSource                                 *string // 00067 (M15-13)
}

// feedEventsOf reads every event of punch, the employer's first. direction_source is read through the
// row's JSON, so a walk below 00067 (no such column) reads it as nil instead of failing.
func feedEventsOf(t *testing.T, ctx context.Context, q opQuerier, punch uuid.UUID) []feedEvent {
	t.Helper()
	rows, err := q.Query(ctx, `
		SELECT tenant_id, punch_tenant_id, employee_id, seq, array_to_string(roles, ','), channel, direction, employee_ref, employee_code,
		       employee_company, location_id, location_ref, location_name, location_company, tag_uid, punch_time, recorded_at,
		       created_at, version, status, to_jsonb(e) ->> 'direction_source'
		  FROM punch_events AS e WHERE punch_id = $1
		 ORDER BY (tenant_id = punch_tenant_id) DESC, tenant_id`, punch)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (feedEvent, error) {
		var e feedEvent
		err := r.Scan(&e.tenant, &e.punchTenant, &e.employee, &e.seq, &e.roles, &e.channel, &e.direction, &e.employeeRef, &e.employeeCode,
			&e.employeeCompany, &e.locationID, &e.locationRef, &e.locationName, &e.locationCompany, &e.tag, &e.punchTime,
			&e.recordedAt, &e.createdAt, &e.version, &e.status, &e.directionSource)
		return e, err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// feedCounter is tenant's last_seq (-1 when it has no counter row).
func feedCounter(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID) int64 {
	t.Helper()
	return opInt(t, ctx, q, `SELECT coalesce((SELECT last_seq FROM feed_counters WHERE tenant_id = $1), -1)`, tenant)
}

// feedDeliveries is how many delivery rows tenant holds for the event seq.
func feedDeliveries(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID, seq int64) int64 {
	t.Helper()
	return opInt(t, ctx, q, `SELECT count(*) FROM webhook_deliveries WHERE tenant_id = $1 AND punch_event_seq = $2`, tenant, seq)
}

// feedCode gives tenant the company code (the operator's K-22 write, done here as the owner).
func feedCode(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID, code string) {
	t.Helper()
	if _, err := q.Exec(ctx, `UPDATE tenants SET api_code = $2 WHERE id = $1`, tenant, code); err != nil {
		t.Fatalf("give %s the code %s: %v", tenant, code, err)
	}
}

// feedOpen opens tenant's API with code (K-22) and gives it an endpoint (active or not), as the
// owner. It returns the endpoint's id.
func feedOpen(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID, code string, active bool) uuid.UUID {
	t.Helper()
	if _, err := q.Exec(ctx, `UPDATE tenants SET api_code = $2, api_enabled_at = now() WHERE id = $1`, tenant, code); err != nil {
		t.Fatalf("open %s's API: %v", code, err)
	}
	var id uuid.UUID
	if err := q.QueryRow(ctx, `INSERT INTO webhook_endpoints (tenant_id, url, signing_key_sealed, active)
	                           VALUES ($1, 'https://hooks.example.test/tappa', '\x00'::bytea, $2) RETURNING id`, tenant, active).Scan(&id); err != nil {
		t.Fatalf("an endpoint for %s: %v", code, err)
	}
	return id
}

// feedRow is one transactions row a test writes as tappa_app in the employer's context.
type feedRow struct {
	tenant, employee   uuid.UUID
	location           *uuid.UUID
	tag                *string
	verdict, channel   string
	kind               string // "" = no direction
	practice           bool
	site, siteLocation *uuid.UUID
	siteTag            *string
	ctr                *int32
	sunValid           *bool
	source             *string // transactions.direction_source (00065); nil: NULL, every row before M15-11
}

// feedInsert writes r and returns its id.
func feedInsert(t *testing.T, ctx context.Context, tx pgx.Tx, r feedRow) (uuid.UUID, error) {
	t.Helper()
	var kind any
	if r.kind != "" {
		kind = r.kind
	}
	var id uuid.UUID
	err := holdingAppIn(t, ctx, tx, r.tenant, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `
			INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type, practice,
			                          site_tenant_id, site_location_id, site_tag_uid, ctr, sun_valid, direction_source)
			VALUES ($1, $2, $3, $4, now() - interval '5 seconds', $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id`,
			r.tenant, r.employee, r.location, r.tag, r.verdict, r.channel, kind, r.practice,
			r.site, r.siteLocation, r.siteTag, r.ctr, r.sunValid, r.source).Scan(&id)
	})
	return id, err
}

// feedMust is feedInsert that fails the test on an error.
func feedMust(t *testing.T, ctx context.Context, tx pgx.Tx, r feedRow) uuid.UUID {
	t.Helper()
	id, err := feedInsert(t, ctx, tx, r)
	if err != nil {
		t.Fatalf("write %s/%s: %v", r.channel, r.verdict, err)
	}
	return id
}

// feedReview writes reviewer's decision on the row id as tappa_app in tenant's context.
func feedReview(t *testing.T, ctx context.Context, tx pgx.Tx, tenant, reviewer, id uuid.UUID, outcome string) error {
	t.Helper()
	return holdingAppIn(t, ctx, tx, tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `INSERT INTO transaction_reviews (tenant_id, transaction_id, reviewer_id, outcome) VALUES ($1, $2, $3, $4)`,
			tenant, id, reviewer, outcome)
		return err
	})
}

// feedWorld is holdingWorldTx's world (KF and KM peers in H; C and D in H2) with the feed's
// identities: company codes KF and KM, an employee of each with a ref and a code, and KF's own
// venue and plaque for its single-tenant taps.
type feedWorld struct {
	holdingWorld
	kfEmployee uuid.UUID
	kmVenue    uuid.UUID
	kmTag      string
}

func feedWorldTx(t *testing.T, ctx context.Context, tx pgx.Tx, kmLeftAgo string) feedWorld {
	t.Helper()
	w := feedWorld{holdingWorld: holdingWorldTx(t, ctx, tx, kmLeftAgo)}
	feedCode(t, ctx, tx, w.kf, "KF")
	feedCode(t, ctx, tx, w.km, "KM")
	w.kfEmployee = holdingNewEmployee(t, ctx, tx, w.kf, "active")
	for _, e := range []uuid.UUID{w.kfEmployee, w.kmEmployee} {
		if _, err := tx.Exec(ctx, `UPDATE employees SET employee_code = 'C-' || left(id::text, 6) WHERE id = $1`, e); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.QueryRow(ctx, `INSERT INTO locations (tenant_id, name, external_ref) VALUES ($1, 'KM Plant', 'KM-PL') RETURNING id`, w.km).Scan(&w.kmVenue); err != nil {
		t.Fatal(err)
	}
	w.kmTag = holdingNewTag(t, ctx, tx, w.km, &w.kmVenue, "active", 100)
	return w
}

// own is an ok tap of employee on its own tenant's plaque and venue.
func (w feedWorld) own(tenant, employee, venue uuid.UUID, tag string, channel, verdict string) feedRow {
	kind := "in"
	if verdict == "reject" || verdict == "ignored" {
		kind = ""
	}
	return feedRow{tenant: tenant, employee: employee, location: &venue, tag: &tag, verdict: verdict, channel: channel, kind: kind}
}

// kmAtKF is KM's employee on KF's plaque (a holding punch).
func (w feedWorld) kmAtKF(channel, verdict string) feedRow {
	kind := "in"
	if verdict == "reject" || verdict == "ignored" {
		kind = ""
	}
	r := feedRow{tenant: w.km, employee: w.kmEmployee, verdict: verdict, channel: channel, kind: kind,
		site: &w.kf, siteLocation: &w.kfVenue, ctr: ptr(int32(100)), sunValid: ptr(true)}
	if channel != "manual" {
		r.siteTag = &w.kfTag
	} else {
		r.ctr, r.sunValid = nil, nil
	}
	return r
}

// kfAtKM is KF's employee on KM's plaque (the reverse holding punch).
func (w feedWorld) kfAtKM(channel, verdict string) feedRow {
	return feedRow{tenant: w.kf, employee: w.kfEmployee, verdict: verdict, channel: channel, kind: "in",
		site: &w.km, siteLocation: &w.kmVenue, siteTag: &w.kmTag, ctr: ptr(int32(100)), sunValid: ptr(true)}
}

// feedSetAsideRows removes, inside the savepoint, every row of the three feed tables a Down refuses
// over (punch_events' append-only TRUNCATE trigger switched off and on, inside the savepoint) -- so
// 00048's Down can be walked on a database other tests committed events to. The caller holds
// holdingDownTables (holdingLockedSavepoint).
//
// 🔴 TRUNCATE, NOT DELETE -- AND THIS IS A FIX (M11 API-10, CI run 38819939609). opTx is REPEATABLE
// READ: its snapshot is taken by the test's first query, BEFORE holdingLockedSavepoint's ACCESS
// EXCLUSIVE locks. A DELETE sees that snapshot only, so rows another package committed in between
// survived it -- and since API-10 another package (internal/egress's worker tests, run in parallel by
// `go test ./...`) commits webhook deliveries: `DELETE FROM webhook_deliveries` left them,
// `DELETE FROM webhook_endpoints` then met them in the foreign-key check (which reads the latest
// rows) and failed 23503. Reproduced on a throwaway database: this test ×40 beside the egress worker
// tests -> red on the first run that met one; with TRUNCATE green ×40 twice. TRUNCATE empties the
// table whatever the snapshot (under the lock this savepoint already holds); the three go in ONE
// statement because deliveries reference both others. Rolled back with the savepoint like the rest.
func feedSetAsideRows(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	if opInt(t, ctx, sp, `SELECT count(*) FROM pg_class WHERE oid = to_regclass('public.punch_events')`) == 0 {
		return
	}
	for _, s := range []string{
		`ALTER TABLE punch_events DISABLE TRIGGER punch_events_no_truncate`,
		`TRUNCATE webhook_deliveries, webhook_endpoints, punch_events`,
		`ALTER TABLE punch_events ENABLE TRIGGER punch_events_no_truncate`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// feedBelow48 takes the savepoint's database below 00048 -- 00067's Down (M15-13), 00062's Down (GRP-1), 00060's Down (T124), 00058's (T130), 00055's (API-8), 00049's, then
// 00048's -- with the
// feed rows set aside first. The caller's savepoint is a holdingLockedSavepoint; the lock is taken
// again here, as holdingBelow46 does.
func feedBelow48(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	holdingLockForDowns(t, ctx, sp)
	feedSetAsideRows(t, ctx, sp)
	// M15-13: 00067's Down first -- it gives 00049's two trigger bodies back before 00049's Down
	// replaces holding_feed and drops punch_feed, and drops punch_events.direction_source; the rows
	// are set aside above, so it does not refuse. Its one table lock is punch_events, in
	// holdingDownTables. feedUpAgain gives it back.
	_, down67 := opMigrationSections(t, ds00067File)
	opRunSection(t, ctx, sp, down67, "00067 Down")
	// API-8: 00055's Down first -- holding_directory is a definer's function and its four employees
	// grants are the definer's, so a walk below 00047 must not find them (feedUpAgain gives them back).
	// It takes no table lock; the directory's index is 00056's and is not walked.
	// T130: 00058's Down first -- its trigger function outlives the table 00048's Down drops, and
	// feedUpAgain gives the whole of it back.
	// T124: 00060's Down before 00055's -- 00055's Down drops holding_directory, whose 00060 body is the
	// one 00055's own does not know; 00060's gives 00055's bodies back and drops holding_peer's set form
	// (a walk below 00041 must not find a second holding_peer). No table lock.
	// GRP-1: 00062's Down before 00060's -- 00060's Up refuses a third holding_peer, and a walk below
	// 00041 must not find the roster form or holding_group (feedUpAgain gives them back). No table lock.
	_, down62 := opMigrationSections(t, op00062File)
	opRunSection(t, ctx, sp, down62, "00062 Down")
	_, down60 := opMigrationSections(t, op00060File)
	opRunSection(t, ctx, sp, down60, "00060 Down")
	_, down58 := opMigrationSections(t, op00058File)
	opRunSection(t, ctx, sp, down58, "00058 Down")
	_, down55 := opMigrationSections(t, op00055File)
	opRunSection(t, ctx, sp, down55, "00055 Down")
	_, down49 := opMigrationSections(t, op00049File)
	_, down48 := opMigrationSections(t, op00048File)
	opRunSection(t, ctx, sp, down49, "00049 Down")
	opRunSection(t, ctx, sp, down48, "00048 Down")
}

// feedUpAgain runs 00048's, 00049's, 00055's, 00058's, 00059's, 00060's, 00062's and 00067's Ups -- after feedBelow48 and whatever the test walked
// below it, to compare with HEAD.
func feedUpAgain(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	up48, _ := opMigrationSections(t, op00048File)
	up49, _ := opMigrationSections(t, op00049File)
	opRunSection(t, ctx, sp, up48, "00048 Up again")
	opRunSection(t, ctx, sp, up49, "00049 Up again")
	up55, _ := opMigrationSections(t, op00055File)
	opRunSection(t, ctx, sp, up55, "00055 Up again")
	up58, _ := opMigrationSections(t, op00058File)
	up59, _ := opMigrationSections(t, op00059File)
	opRunSection(t, ctx, sp, up58, "00058 Up again")
	opRunSection(t, ctx, sp, up59, "00059 Up again")
	up60, _ := opMigrationSections(t, op00060File)
	opRunSection(t, ctx, sp, up60, "00060 Up again")
	up62, _ := opMigrationSections(t, op00062File)
	opRunSection(t, ctx, sp, up62, "00062 Up again")
	// M15-13: 00067's Up last -- its precondition asks for 00049's two bodies, which the Ups above
	// gave back.
	up67, _ := opMigrationSections(t, ds00067File)
	opRunSection(t, ctx, sp, up67, "00067 Up again")
}

func feedStr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// --------------------------------------------------------------- (a) a plain INSERT --

// TestFeed_PlainInsertIntoTransactionsBearsAnEvent is plan §5 API-2 (a), ADR 0028 §4: no Go code, a
// bare `INSERT INTO transactions` as tappa_app in the tenant's context -- the shape an old binary
// or a rollback writes -- is an event in that tenant's feed: one row, roles {employer,location},
// seq one past the counter, the counter moved to it, punch_time the row's occurred_at, recorded_at
// the server's now() (the transaction's, never a column a caller writes), version 1, 'valid', and
// the snapshot (K-32): the employee's id, ref, code and company code, the venue's id, ref, name
// and company code, the plaque's uid. A second tap is the next seq. And the counter row was
// opened by the tenants trigger (00048): a tenant inserted as the owner, with no context, has one.
func TestFeed_PlainInsertIntoTransactionsBearsAnEvent(t *testing.T) {
	ctx, tx := opTx(t)
	w := feedWorldTx(t, ctx, tx, "")
	if c := feedCounter(t, ctx, tx, w.kf); c != 0 {
		t.Fatalf("a new tenant's counter row is %d, want 0 (feed_open_counter)", c)
	}
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	id := feedMust(t, ctx, tx, w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "ok"))
	ev := feedEventsOf(t, ctx, tx, id)
	if len(ev) != 1 {
		t.Fatalf("%d events for one plain INSERT, want 1", len(ev))
	}
	e := ev[0]
	var occurred time.Time
	var ref, code string
	if err := tx.QueryRow(ctx, `SELECT t.occurred_at, e.external_ref, e.employee_code FROM transactions t JOIN employees e ON e.id = t.employee_id WHERE t.id = $1`, id).
		Scan(&occurred, &ref, &code); err != nil {
		t.Fatal(err)
	}
	switch {
	case e.tenant != w.kf || e.punchTenant != w.kf || e.roles != "employer,location" || e.seq != 1 || e.channel != "nfc":
		t.Errorf("event tenant/punch tenant/roles/seq/channel = %v/%v/%s/%d/%s", e.tenant, e.punchTenant, e.roles, e.seq, e.channel)
	case !e.punchTime.Equal(occurred) || !e.recordedAt.Equal(now) || !e.createdAt.Equal(now):
		t.Errorf("punch_time %v (occurred_at %v), recorded_at %v and created_at %v (now %v)", e.punchTime, occurred, e.recordedAt, e.createdAt, now)
	case e.version != 1 || e.status != "valid":
		t.Errorf("version %d status %s, want 1 valid (K-5)", e.version, e.status)
	case e.employee != w.kfEmployee || feedStr(e.employeeRef) != ref || feedStr(e.employeeCode) != code || feedStr(e.employeeCompany) != "KF":
		t.Errorf("employee snapshot %v %s %s %s", e.employee, feedStr(e.employeeRef), feedStr(e.employeeCode), feedStr(e.employeeCompany))
	case e.locationID == nil || *e.locationID != w.kfVenue || feedStr(e.locationRef) != "KF-SJ" || feedStr(e.locationName) != "KF St Julians" ||
		feedStr(e.locationCompany) != "KF" || feedStr(e.tag) != w.kfTag:
		t.Errorf("venue snapshot %v %s %s %s tag %s", e.locationID, feedStr(e.locationRef), feedStr(e.locationName), feedStr(e.locationCompany), feedStr(e.tag))
	}
	if c := feedCounter(t, ctx, tx, w.kf); c != 1 {
		t.Errorf("the counter after one event is %d, want 1", c)
	}
	id2 := feedMust(t, ctx, tx, w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "qr", "ok"))
	if ev := feedEventsOf(t, ctx, tx, id2); len(ev) != 1 || ev[0].seq != 2 || ev[0].channel != "qr" {
		t.Errorf("the second tap's events: %+v, want one at seq 2", ev)
	}
	// The snapshot does not follow a later rename (K-32: the punch's values are the punch's).
	if _, err := tx.Exec(ctx, `UPDATE locations SET name = 'Renamed' WHERE id = $1`, w.kfVenue); err != nil {
		t.Fatal(err)
	}
	if ev := feedEventsOf(t, ctx, tx, id); feedStr(ev[0].locationName) != "KF St Julians" {
		t.Errorf("the event's venue name followed a rename: %s", feedStr(ev[0].locationName))
	}
}

// ----------------------------------------------- (d) flag, approval, manual, practice --

// TestFeed_FlagApprovalManualPractice is plan §5 API-2 (d), K-4/K-6: of a tenant's rows only an ok
// tap on nfc or qr that is not practice is an event at its INSERT; a flag is NOT -- its approval
// is, at the approval (recorded_at the approval's now(), punch_time the tap's occurred_at), once;
// a rejected flag never; a reject, an ignored repeat, a manual record (ok or flag-approved) and a
// practice row (ok, or a flag approved) never. The same for a holding punch: KM's flagged row on
// KF's plaque is two events at its approval, none before. And the counter moved once per event.
func TestFeed_FlagApprovalManualPractice(t *testing.T) {
	ctx, tx := opTx(t)
	w := feedWorldTx(t, ctx, tx, "")
	kfReviewer := holdingNewAdmin(t, ctx, tx, w.kf)
	kmReviewer := holdingNewAdmin(t, ctx, tx, w.km)
	start := feedCounter(t, ctx, tx, w.kf)
	none := map[string]feedRow{
		"a reject":          w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "reject"),
		"an ignored repeat": w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "ignored"),
		"a QR reject":       w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "qr", "reject"),
		"a manual ok":       {tenant: w.kf, employee: w.kfEmployee, location: &w.kfVenue, verdict: "ok", channel: "manual", kind: "in"},
		"a practice ok (nfc)": func() feedRow {
			r := w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "ok")
			r.practice = true
			return r
		}(),
		"a flag (before review)": w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "flag"),
	}
	for what, r := range none {
		id := feedMust(t, ctx, tx, r)
		if ev := feedEventsOf(t, ctx, tx, id); len(ev) != 0 {
			t.Errorf("%s is an event (%+v) -- K-4 lets in an ok tap of a real channel alone", what, ev)
		}
	}
	if c := feedCounter(t, ctx, tx, w.kf); c != start {
		t.Errorf("rows that are no event moved the counter %d -> %d", start, c)
	}

	// An approval is the event, once; a rejection is none.
	flag := feedMust(t, ctx, tx, w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "qr", "flag"))
	if err := feedReview(t, ctx, tx, w.kf, kfReviewer, flag, "approved"); err != nil {
		t.Fatal(err)
	}
	var occurred, now time.Time
	if err := tx.QueryRow(ctx, `SELECT occurred_at, now() FROM transactions WHERE id = $1`, flag).Scan(&occurred, &now); err != nil {
		t.Fatal(err)
	}
	if ev := feedEventsOf(t, ctx, tx, flag); len(ev) != 1 || ev[0].roles != "employer,location" || !ev[0].punchTime.Equal(occurred) ||
		!ev[0].recordedAt.Equal(now) || ev[0].seq != start+1 {
		t.Errorf("the approved flag's events: %+v, want one at seq %d", ev, start+1)
	}
	rejected := feedMust(t, ctx, tx, w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "flag"))
	if err := feedReview(t, ctx, tx, w.kf, kfReviewer, rejected, "rejected"); err != nil {
		t.Fatal(err)
	}
	if ev := feedEventsOf(t, ctx, tx, rejected); len(ev) != 0 {
		t.Errorf("a rejected flag is an event: %+v", ev)
	}
	// A flag that is practice, or manual, approved: still none (K-6 -- the approval trigger applies
	// the whole rule: the dev database holds practice flags, ADR 0028 K-4).
	pf := w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "flag")
	pf.practice = true
	mf := feedRow{tenant: w.kf, employee: w.kfEmployee, location: &w.kfVenue, verdict: "flag", channel: "manual", kind: "in"}
	for what, r := range map[string]feedRow{"a practice flag": pf, "a manual flag": mf} {
		id := feedMust(t, ctx, tx, r)
		if err := feedReview(t, ctx, tx, w.kf, kfReviewer, id, "approved"); err != nil {
			t.Fatal(err)
		}
		if ev := feedEventsOf(t, ctx, tx, id); len(ev) != 0 {
			t.Errorf("%s, approved, is an event: %+v", what, ev)
		}
	}
	if c := feedCounter(t, ctx, tx, w.kf); c != start+1 {
		t.Errorf("the counter is %d, want %d (one event)", c, start+1)
	}

	// A holding approval in THIS transaction -- which already wrote KF's own events -- is refused:
	// a transaction writes events under ONE counter set (SQLSTATE TF001, ADR 0028 §5). The holding
	// half in a transaction of its own: TestFeed_HoldingFlagApprovalManualReject.
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		hf := feedMust(t, ctx, sp, w.kmAtKF("nfc", "flag"))
		if ev := feedEventsOf(t, ctx, sp, hf); len(ev) != 0 {
			t.Errorf("KM's flagged row on KF's plaque is an event before review: %+v", ev)
		}
		if err := feedReview(t, ctx, sp, w.km, kmReviewer, hf, "approved"); err == nil {
			t.Error("the holding approval in a transaction that wrote KF's own events was written -- TF001 expected")
		} else if code, _ := opCode(err); code != "TF001" {
			t.Errorf("the holding approval after KF's own events: %v, want TF001", err)
		}
	})
}

// TestFeed_HoldingFlagApprovalManualReject is TestFeed_FlagApprovalManualPractice's holding half,
// in a transaction of its own: KM's flagged row on KF's plaque is no event; its approval is two
// (K-31), each carrying the flagged row's direction (K-40); a holding manual record and a holding
// reject: none.
func TestFeed_HoldingFlagApprovalManualReject(t *testing.T) {
	ctx, tx := opTx(t)
	w := feedWorldTx(t, ctx, tx, "")
	kmReviewer := holdingNewAdmin(t, ctx, tx, w.km)
	r := w.kmAtKF("nfc", "flag")
	r.kind = "out"
	hf := feedMust(t, ctx, tx, r)
	if ev := feedEventsOf(t, ctx, tx, hf); len(ev) != 0 {
		t.Errorf("KM's flagged row on KF's plaque is an event before review: %+v", ev)
	}
	if err := feedReview(t, ctx, tx, w.km, kmReviewer, hf, "approved"); err != nil {
		t.Fatal(err)
	}
	if ev := feedEventsOf(t, ctx, tx, hf); len(ev) != 2 || ev[0].tenant != w.km || ev[1].tenant != w.kf ||
		ev[0].direction != "out" || ev[1].direction != "out" {
		t.Errorf("the approved holding flag's events: %+v, want KM's and KF's, both 'out'", ev)
	}
	for _, r := range []feedRow{w.kmAtKF("manual", "ok"), w.kmAtKF("nfc", "reject")} {
		id := feedMust(t, ctx, tx, r)
		if ev := feedEventsOf(t, ctx, tx, id); len(ev) != 0 {
			t.Errorf("a holding %s/%s is an event: %+v", r.channel, r.verdict, ev)
		}
	}
}

// ---------------------------------------------- (j) the target rows and the snapshot --

// TestFeed_TargetRowsAndSnapshots is plan §5 API-2 (j), ADR 0028 §4 "Hedef satırlar", K-31/K-32:
//   - KM's employee on KF's plaque (ok, nfc): exactly TWO events, both written by holding_feed --
//     KM's {employer} and KF's {location}, the employer's exactly once -- each carrying the same
//     snapshot: KM's employee id, ref, code and company code KM; KF's venue id, ref, name and
//     company code KF; KF's plaque uid; punch_tenant KM in both;
//   - the reverse (KF's employee on KM's plaque): KF {employer}, KM {location};
//   - each feed's counter moved once;
//   - and the OWN-TENANT trigger never writes for a site row or its approval: with holding_feed
//     switched off (a locked savepoint), the same holding tap and the approval of a holding flag
//     are no event at all -- punch_feed returned on them.
func TestFeed_TargetRowsAndSnapshots(t *testing.T) {
	ctx, tx := opTx(t)
	w := feedWorldTx(t, ctx, tx, "")
	var kmRef, kmCode string
	if err := tx.QueryRow(ctx, `SELECT external_ref, employee_code FROM employees WHERE id = $1`, w.kmEmployee).Scan(&kmRef, &kmCode); err != nil {
		t.Fatal(err)
	}
	kf0, km0 := feedCounter(t, ctx, tx, w.kf), feedCounter(t, ctx, tx, w.km)
	id := feedMust(t, ctx, tx, w.kmAtKF("nfc", "ok"))
	ev := feedEventsOf(t, ctx, tx, id)
	if len(ev) != 2 {
		t.Fatalf("KM@KF: %d events, want 2 (%+v)", len(ev), ev)
	}
	for i, want := range []struct {
		tenant uuid.UUID
		roles  string
		seq    int64
	}{{w.km, "employer", km0 + 1}, {w.kf, "location", kf0 + 1}} {
		e := ev[i]
		if e.tenant != want.tenant || e.roles != want.roles || e.seq != want.seq || e.punchTenant != w.km {
			t.Errorf("KM@KF event %d: tenant %v roles %s seq %d punch tenant %v; want %v %s %d %v", i, e.tenant, e.roles, e.seq, e.punchTenant,
				want.tenant, want.roles, want.seq, w.km)
		}
		if e.employee != w.kmEmployee || feedStr(e.employeeRef) != kmRef || feedStr(e.employeeCode) != kmCode || feedStr(e.employeeCompany) != "KM" ||
			e.locationID == nil || *e.locationID != w.kfVenue || feedStr(e.locationRef) != "KF-SJ" || feedStr(e.locationName) != "KF St Julians" ||
			feedStr(e.locationCompany) != "KF" || feedStr(e.tag) != w.kfTag {
			t.Errorf("KM@KF event %d's snapshot: employee %v %s %s %s, venue %v %s %s %s, tag %s", i, e.employee, feedStr(e.employeeRef),
				feedStr(e.employeeCode), feedStr(e.employeeCompany), e.locationID, feedStr(e.locationRef), feedStr(e.locationName),
				feedStr(e.locationCompany), feedStr(e.tag))
		}
	}
	if feedCounter(t, ctx, tx, w.kf) != kf0+1 || feedCounter(t, ctx, tx, w.km) != km0+1 {
		t.Errorf("counters after KM@KF: KF %d KM %d, want %d %d", feedCounter(t, ctx, tx, w.kf), feedCounter(t, ctx, tx, w.km), kf0+1, km0+1)
	}
	rev := feedEventsOf(t, ctx, tx, feedMust(t, ctx, tx, w.kfAtKM("qr", "ok")))
	if len(rev) != 2 || rev[0].tenant != w.kf || rev[0].roles != "employer" || rev[1].tenant != w.km || rev[1].roles != "location" ||
		feedStr(rev[0].employeeCompany) != "KF" || feedStr(rev[0].locationCompany) != "KM" || feedStr(rev[0].locationRef) != "KM-PL" {
		t.Errorf("KF@KM: %+v, want KF {employer} then KM {location}, KF's employee at KM's venue", rev)
	}

	// The own-tenant trigger alone, on a site row and on the approval of one.
	reviewer := holdingNewAdmin(t, ctx, tx, w.km)
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		for _, s := range []string{
			`ALTER TABLE transactions DISABLE TRIGGER transactions_holding_feed`,
			`ALTER TABLE transaction_reviews DISABLE TRIGGER transaction_reviews_holding_feed`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatal(err)
			}
		}
		ok := feedMust(t, ctx, sp, w.kmAtKF("nfc", "ok"))
		flag := feedMust(t, ctx, sp, w.kmAtKF("nfc", "flag"))
		if err := feedReview(t, ctx, sp, w.km, reviewer, flag, "approved"); err != nil {
			t.Fatal(err)
		}
		for what, id := range map[string]uuid.UUID{"a site ok row": ok, "the approval of a site flag": flag} {
			if ev := feedEventsOf(t, ctx, sp, id); len(ev) != 0 {
				t.Errorf("with holding_feed off, %s is %d event(s) -- punch_feed wrote for a site row", what, len(ev))
			}
		}
		// CONTROL: punch_feed is live in this savepoint -- a single-tenant tap is its event.
		if ev := feedEventsOf(t, ctx, sp, feedMust(t, ctx, sp, w.own(w.km, w.kmEmployee, w.kmVenue, w.kmTag, "nfc", "ok"))); len(ev) != 1 {
			t.Errorf("CONTROL: a KM tap with holding_feed off is %d events, want 1", len(ev))
		}
	})
}

// TestWebhookDeliveries_KMatKFIsTwo is plan §5 API-2 (j)'s delivery count and ADR 0028 §6's routing:
// with both tenants' API open (K-22) and an active endpoint each, every target event is one
// delivery to ITS tenant's endpoint -- KF@KF 1, KM@KM 1, KM@KF 2 (KM's to KM's endpoint for its
// {employer} event, KF's to KF's for {location}), KF@KM 2 -- and nothing else is delivered.
func TestWebhookDeliveries_KMatKFIsTwo(t *testing.T) {
	ctx, tx := opTx(t)
	w := feedWorldTx(t, ctx, tx, "")
	hooks := map[uuid.UUID]uuid.UUID{w.kf: feedOpen(t, ctx, tx, w.kf, "KF", true), w.km: feedOpen(t, ctx, tx, w.km, "KM", true)}
	for _, c := range []struct {
		what string
		row  feedRow
	}{
		{"KF@KF", w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "ok")},
		{"KM@KM", w.own(w.km, w.kmEmployee, w.kmVenue, w.kmTag, "qr", "ok")},
		{"KM@KF", w.kmAtKF("nfc", "ok")},
		{"KF@KM", w.kfAtKM("nfc", "ok")},
	} {
		// Each shape in a savepoint of its own, rolled back: a transaction writes events under one
		// counter set (SQLSTATE TF001, ADR 0028 §5).
		holdingInSavepoint(t, tx, func(sp pgx.Tx) {
			before := opInt(t, ctx, sp, `SELECT count(*) FROM webhook_deliveries WHERE tenant_id IN ($1, $2)`, w.kf, w.km)
			ev := feedEventsOf(t, ctx, sp, feedMust(t, ctx, sp, c.row))
			for _, e := range ev {
				var endpoint uuid.UUID
				var state string
				var attempt int32
				if err := sp.QueryRow(ctx, `SELECT endpoint_id, state, attempt FROM webhook_deliveries WHERE tenant_id = $1 AND punch_event_seq = $2`,
					e.tenant, e.seq).Scan(&endpoint, &state, &attempt); err != nil {
					t.Errorf("%s: no delivery for %v's event %d: %v", c.what, e.tenant, e.seq, err)
					continue
				}
				if endpoint != hooks[e.tenant] || state != "pending" || attempt != 0 {
					t.Errorf("%s: %v's delivery goes to %v (%s, attempt %d), want its own endpoint %v, pending, 0", c.what, e.tenant, endpoint, state, attempt, hooks[e.tenant])
				}
			}
			after := opInt(t, ctx, sp, `SELECT count(*) FROM webhook_deliveries WHERE tenant_id IN ($1, $2)`, w.kf, w.km)
			want := int64(1)
			if c.what[:2] != c.what[3:] {
				want = 2
			}
			if after-before != want || int64(len(ev)) != want {
				t.Errorf("%s: %d events and %d deliveries, want %d of each", c.what, len(ev), after-before, want)
			}
		})
	}
}

// feedDirectionFindings is K-40 on q: every one of the four shapes (KF@KF in, KM@KM out, KM@KF
// out, KF@KM in) and an approved flag (the flagged row's 'out') -- each in a savepoint of its own,
// rolled back -- answers in every target row the direction the EMPLOYER computed
// (transactions.type). Empty when all hold.
func feedDirectionFindings(t *testing.T, ctx context.Context, q pgx.Tx, w feedWorld, reviewer uuid.UUID) []string {
	t.Helper()
	with := func(r feedRow, kind string) feedRow { r.kind = kind; return r }
	var out []string
	for _, c := range []struct {
		what   string
		row    feedRow
		review bool
	}{
		{"KF@KF in", with(w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "ok"), "in"), false},
		{"KM@KM out", with(w.own(w.km, w.kmEmployee, w.kmVenue, w.kmTag, "qr", "ok"), "out"), false},
		{"KM@KF out", with(w.kmAtKF("nfc", "ok"), "out"), false},
		{"KF@KM in", with(w.kfAtKM("qr", "ok"), "in"), false},
		{"KM@KM approved flag out", with(w.own(w.km, w.kmEmployee, w.kmVenue, w.kmTag, "nfc", "flag"), "out"), true},
	} {
		sp, err := q.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		id, err := feedInsert(t, ctx, sp, c.row)
		if err == nil && c.review {
			err = feedReview(t, ctx, sp, c.row.tenant, reviewer, id, "approved")
		}
		if err != nil {
			out = append(out, c.what+": "+err.Error())
		} else {
			ev := feedEventsOf(t, ctx, sp, id)
			if len(ev) == 0 {
				out = append(out, c.what+": no event")
			}
			for _, e := range ev {
				if e.direction != c.row.kind {
					out = append(out, fmt.Sprintf("%s: %v's event says %q", c.what, e.tenant, e.direction))
				}
			}
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// TestFeed_DirectionIsTheEmployers is the user's decision K-40 (2026-10-14; ADR 0028 §4): a punch
// event carries the direction Tappa computed at the employer -- transactions.type; an approval's,
// the approved row's; both rows of a holding punch the employer's. Four shapes and an approved
// flag (feedDirectionFindings). MUTANTS, each in a savepoint, both writers replaced by CREATE OR
// REPLACE (owner and grants kept): the direction written reversed -> red; not written (NULL) ->
// the INSERT is refused (punch_events.direction NOT NULL) -> red. The night shift across midnight,
// through the tap path: internal/domain/checkin's TestFeedDirection_NightShiftInThenOutAcrossMidnight.
func TestFeed_DirectionIsTheEmployers(t *testing.T) {
	ctx, tx := opTx(t)
	w := feedWorldTx(t, ctx, tx, "")
	reviewer := holdingNewAdmin(t, ctx, tx, w.km)
	for _, f := range feedDirectionFindings(t, ctx, tx, w, reviewer) {
		t.Error(f)
	}
	for name, written := range map[string]string{
		"reversed":    "CASE v_type WHEN 'in' THEN 'out' ELSE 'in' END",
		"not written": "NULL",
	} {
		holdingInSavepoint(t, tx, func(sp pgx.Tx) {
			for _, fn := range []string{"punch_feed", "holding_feed"} {
				var def string
				if err := sp.QueryRow(ctx, `SELECT pg_get_functiondef(('public.' || $1 || '()')::regprocedure)`, fn).Scan(&def); err != nil {
					t.Fatal(err)
				}
				n := strings.Count(def, "v_type, v_employee")
				if n == 0 {
					t.Fatalf("%s writes no v_type into an event -- the mutant does not fit", fn)
				}
				if _, err := sp.Exec(ctx, strings.ReplaceAll(def, "v_type, v_employee", written+", v_employee")); err != nil {
					t.Fatalf("install the %s mutant in %s: %v", name, fn, err)
				}
			}
			if got := feedDirectionFindings(t, ctx, sp, w, reviewer); len(got) == 0 {
				t.Errorf("the direction %s: the four shapes and the approval stayed green", name)
			} else {
				t.Logf("mutant %s: %d findings, e.g. %s", name, len(got), got[0])
			}
		})
	}
}

// --------------------------------------------------------------- (k) a closed API --

// TestFeed_ClosedApiWritesEventsButNoDeliveries is plan §5 API-2 (k), ADR 0028 §2 (K-22, "kapı her
// yolda"): a tenant whose API the operator has not opened -- or closed again -- still gets its
// events (the cursor feed is continuous), and no delivery row, even with an active endpoint; an
// open API with an INACTIVE endpoint gets no delivery either (K-26's defensive switch); open and
// active, one. For a holding punch the gate is each target's own: KF open, KM closed -> KF's event
// delivered, KM's not.
func TestFeed_ClosedApiWritesEventsButNoDeliveries(t *testing.T) {
	ctx, tx := opTx(t)
	w := feedWorldTx(t, ctx, tx, "")
	feedOpen(t, ctx, tx, w.kf, "KF", true)
	kmHook := feedOpen(t, ctx, tx, w.km, "KM", true)
	// A holding punch, each target's gate its own: KM closed, KF open -- first, in a savepoint
	// rolled back (a transaction writes events under one counter set, SQLSTATE TF001).
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, `UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`, w.km); err != nil {
			t.Fatal(err)
		}
		ev := feedEventsOf(t, ctx, sp, feedMust(t, ctx, sp, w.kmAtKF("nfc", "ok")))
		if len(ev) != 2 || feedDeliveries(t, ctx, sp, w.km, ev[0].seq) != 0 || feedDeliveries(t, ctx, sp, w.kf, ev[1].seq) != 1 {
			t.Errorf("KM@KF with KM closed and KF open: %d events; KM delivered %d, KF delivered %d -- want 2, 0, 1", len(ev),
				feedDeliveries(t, ctx, sp, w.km, ev[0].seq), feedDeliveries(t, ctx, sp, w.kf, ev[1].seq))
		}
	})
	tap := func() []feedEvent {
		return feedEventsOf(t, ctx, tx, feedMust(t, ctx, tx, w.own(w.km, w.kmEmployee, w.kmVenue, w.kmTag, "nfc", "ok")))
	}
	for _, c := range []struct {
		what, sql string
		args      []any
		delivered int64
	}{
		{"open and active", `SELECT 1`, nil, 1},
		{"closed by the operator (api_enabled_at NULL), endpoint active", `UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`, []any{w.km}, 0},
		{"opened again, endpoint switched off", `UPDATE tenants SET api_enabled_at = now() WHERE id = $1`, []any{w.km}, 1},
	} {
		if _, err := tx.Exec(ctx, c.sql, c.args...); err != nil {
			t.Fatal(err)
		}
		if c.what == "opened again, endpoint switched off" {
			if _, err := tx.Exec(ctx, `UPDATE webhook_endpoints SET active = false WHERE id = $1`, kmHook); err != nil {
				t.Fatal(err)
			}
			c.delivered = 0
		}
		ev := tap()
		if len(ev) != 1 {
			t.Errorf("%s: %d events, want 1 -- the feed does not stop with the API", c.what, len(ev))
			continue
		}
		if n := feedDeliveries(t, ctx, tx, w.km, ev[0].seq); n != c.delivered {
			t.Errorf("%s: %d deliveries, want %d", c.what, n, c.delivered)
		}
	}
}

// ------------------------------------------------- (p) an approval past the membership --

// TestFeed_ApprovalAfterExitWritesEmployerEventAndDeliveryOnce is plan §5 API-2 (p), ADR 0027 §8 and
// §13 item 7: KM's employee's flagged row on KF's plaque is written while the two are peers; KM
// leaves H; KM's manager approves it. The approval stands (the employer's own record) and HOLDING
// FEED writes KM's {employer} event and its delivery EXACTLY ONCE -- the counterpart's snapshot
// (KF's venue id, ref, name and company code) NULL, the plaque uid kept (the employer's own row
// carries it); KF's side is skipped with no error: no KF event, no KF delivery, no 'approved' in
// KF's projection, KF's counter unmoved. Both APIs open, both endpoints active.
func TestFeed_ApprovalAfterExitWritesEmployerEventAndDeliveryOnce(t *testing.T) {
	ctx, tx := opTx(t)
	w := feedWorldTx(t, ctx, tx, "")
	feedOpen(t, ctx, tx, w.kf, "KF", true)
	feedOpen(t, ctx, tx, w.km, "KM", true)
	reviewer := holdingNewAdmin(t, ctx, tx, w.km)
	// CONTROL first, in a savepoint rolled back (a transaction writes events under one counter set,
	// SQLSTATE TF001): the same while the two are peers is two events.
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		f2 := feedMust(t, ctx, sp, w.kmAtKF("nfc", "flag"))
		if err := feedReview(t, ctx, sp, w.km, reviewer, f2, "approved"); err != nil {
			t.Fatal(err)
		}
		if n := len(feedEventsOf(t, ctx, sp, f2)); n != 2 {
			t.Errorf("CONTROL: an approval while peers is %d events, want 2", n)
		}
	})
	flag := feedMust(t, ctx, tx, w.kmAtKF("nfc", "flag"))
	if _, err := tx.Exec(ctx, `UPDATE holding_members SET left_at = clock_timestamp() - interval '2 minutes' WHERE member_tenant_id = $1 AND left_at IS NULL`, w.km); err != nil {
		t.Fatal(err)
	}
	kf0, km0 := feedCounter(t, ctx, tx, w.kf), feedCounter(t, ctx, tx, w.km)
	if err := feedReview(t, ctx, tx, w.km, reviewer, flag, "approved"); err != nil {
		t.Fatalf("the approval past the membership was not written (it must be -- the employer's own record): %v", err)
	}
	ev := feedEventsOf(t, ctx, tx, flag)
	if len(ev) != 1 {
		t.Fatalf("%d events, want KM's alone: %+v", len(ev), ev)
	}
	e := ev[0]
	if e.tenant != w.km || e.roles != "employer" || e.seq != km0+1 || e.employee != w.kmEmployee || feedStr(e.employeeCompany) != "KM" ||
		e.locationID != nil || e.locationRef != nil || e.locationName != nil || e.locationCompany != nil || feedStr(e.tag) != w.kfTag {
		t.Errorf("KM's event: %+v -- want {employer} at seq %d, KF's venue NULL, the plaque uid kept", e, km0+1)
	}
	if n := feedDeliveries(t, ctx, tx, w.km, e.seq); n != 1 {
		t.Errorf("KM's delivery: %d, want 1", n)
	}
	if feedCounter(t, ctx, tx, w.kf) != kf0 || opInt(t, ctx, tx, `SELECT count(*) FROM webhook_deliveries WHERE tenant_id = $1`, w.kf) != 0 {
		t.Errorf("KF's side was written past the membership: counter %d -> %d", kf0, feedCounter(t, ctx, tx, w.kf))
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM site_punches WHERE transaction_id = $1 AND outcome = 'approved'`, flag); n != 0 {
		t.Errorf("KF's projection was told of the approval %d time(s)", n)
	}
}

// --------------------------------------------------------- (e) the outsider sees nothing --

// TestFeed_OutsiderSeesNothing is plan §5 API-2 (e), ADR 0028 §2 / ADR 0027 §12.1 (C a member of
// H2): after KM's and KF's single-tenant and holding taps, C's context reads NO row of
// punch_events, feed_counters or webhook_deliveries but its own (RLS, as tappa_app), C's counter
// is unmoved and no event names C; KM's context reads its own events and not KF's. (K-33's mutants
// on the event branch: TestHoldingFilter_MutantsM1M2M3AreRedInFunctionsAndTriggers, whose feed
// probe now counts KF's events too.)
func TestFeed_OutsiderSeesNothing(t *testing.T) {
	ctx, tx := opTx(t)
	w := feedWorldTx(t, ctx, tx, "")
	feedOpen(t, ctx, tx, w.kf, "KF", true)
	feedOpen(t, ctx, tx, w.km, "KM", true)
	c0 := feedCounter(t, ctx, tx, w.c)
	var ids []uuid.UUID
	for _, r := range []feedRow{w.kmAtKF("nfc", "ok"), w.kfAtKM("qr", "ok"), w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "ok")} {
		ids = append(ids, feedMust(t, ctx, tx, r))
	}
	read := func(tenant uuid.UUID) (events, counters, deliveries, foreign int64) {
		if err := holdingAppIn(t, ctx, tx, tenant, func(sp pgx.Tx) error {
			return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM punch_events WHERE punch_id = ANY($1)),
			                                (SELECT count(*) FROM feed_counters),
			                                (SELECT count(*) FROM webhook_deliveries d JOIN punch_events e ON e.tenant_id = d.tenant_id AND e.seq = d.punch_event_seq
			                                  WHERE e.punch_id = ANY($1)),
			                                (SELECT count(*) FROM punch_events WHERE tenant_id <> $2)`, ids, tenant).
				Scan(&events, &counters, &deliveries, &foreign)
		}); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, who := range []uuid.UUID{w.c, w.d} {
		if e, c, d, f := read(who); e != 0 || d != 0 || f != 0 || c != 1 {
			t.Errorf("an outsider's context reads %d of the peers' events, %d deliveries, %d foreign events and %d counter rows (want 0, 0, 0, its own 1)", e, d, f, c)
		}
	}
	if e, _, d, f := read(w.km); e != 2 || d != 2 || f != 0 {
		t.Errorf("CONTROL: KM reads %d events and %d deliveries of the three taps (want 2 and 2: its {employer} of KM@KF, its {location} of KF@KM) and %d foreign", e, d, f)
	}
	if feedCounter(t, ctx, tx, w.c) != c0 || opInt(t, ctx, tx, `SELECT count(*) FROM punch_events WHERE punch_id = ANY($1) AND tenant_id IN ($2, $3)`, ids, w.c, w.d) != 0 {
		t.Error("an event of the peers' taps landed in C's or D's feed")
	}
}

// ------------------------------------------------------ (g) (h) errors and savepoints --

// TestFeedTrigger_ErrorRollsBackTheRecord is plan §5 API-2 (g), ADR 0028 §4's named cost: neither
// trigger swallows an error, so a failure of the event write fails the employer's INSERT whole --
// no record, no event, no delivery, the counter where it was (never "the record without its
// event", which a cursor would never see again). Forced two ways, each in a savepoint: the
// tenant's counter at bigint's ceiling (22003 from the own-tenant trigger) and, for a holding
// punch, the site's counter there (from holding_feed -- the site's failure costs the employer's
// row too: the named cross-tenant dependency, ADR 0027 §8). CONTROL: the same rows written after.
func TestFeedTrigger_ErrorRollsBackTheRecord(t *testing.T) {
	ctx, tx := opTx(t)
	w := feedWorldTx(t, ctx, tx, "")
	feedOpen(t, ctx, tx, w.km, "KM", true)
	for _, c := range []struct {
		what    string
		ceiling uuid.UUID
		row     feedRow
	}{
		{"KM's own tap, KM's counter at the ceiling", w.km, w.own(w.km, w.kmEmployee, w.kmVenue, w.kmTag, "nfc", "ok")},
		{"KM's tap on KF's plaque, KF's counter at the ceiling", w.kf, w.kmAtKF("nfc", "ok")},
	} {
		holdingInSavepoint(t, tx, func(sp pgx.Tx) {
			kf0, km0 := feedCounter(t, ctx, sp, w.kf), feedCounter(t, ctx, sp, w.km)
			before := opInt(t, ctx, sp, `SELECT count(*) FROM transactions WHERE employee_id = $1`, c.row.employee)
			delivered := opInt(t, ctx, sp, `SELECT count(*) FROM webhook_deliveries WHERE tenant_id = $1`, w.km)
			if _, err := sp.Exec(ctx, `UPDATE feed_counters SET last_seq = 9223372036854775807 WHERE tenant_id = $1`, c.ceiling); err != nil {
				t.Fatal(err)
			}
			_, err := feedInsert(t, ctx, sp, c.row)
			if code, _ := opCode(err); code != "22003" {
				t.Errorf("%s: %v, want 22003 (numeric out of range) from the trigger", c.what, err)
			}
			if _, err := sp.Exec(ctx, `UPDATE feed_counters SET last_seq = CASE tenant_id WHEN $1 THEN $2::bigint ELSE $3::bigint END
			                            WHERE tenant_id IN ($1, $4)`, w.kf, kf0, km0, w.km); err != nil {
				t.Fatal(err)
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM transactions WHERE employee_id = $1`, c.row.employee); n != before {
				t.Errorf("%s: the record was written without its event (%d -> %d rows)", c.what, before, n)
			}
			if opInt(t, ctx, sp, `SELECT count(*) FROM webhook_deliveries WHERE tenant_id = $1`, w.km) != delivered {
				t.Errorf("%s: a delivery was left behind", c.what)
			}
			if _, err := feedInsert(t, ctx, sp, c.row); err != nil {
				t.Errorf("CONTROL %s: with the counters back the row is written: %v", c.what, err)
			}
		})
	}
}

// TestFeedTrigger_SavepointKeepsCounterAndEventTogether is plan §5 API-2 (h), ADR 0028 §5
// "Savepoint": a tap written in a savepoint that is rolled back takes its event AND the counter's
// raise with it -- the next tap's seq is the one the rolled-back tap had (no gap, ever: `seq` is
// dense) -- and the counter row's lock goes with the savepoint: another session raises the same
// counter at once while this transaction is still open (measured with a 1 s lock_timeout there).
func TestFeedTrigger_SavepointKeepsCounterAndEventTogether(t *testing.T) {
	f := holdingLiveFixture(t)
	ctx := f.ctx
	kf := f.tenant(t, "Europe/Malta", "KF")
	var venue uuid.UUID
	if err := f.owner.QueryRow(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, 'api2 sp venue') RETURNING id`, kf).Scan(&venue); err != nil {
		t.Fatal(err)
	}
	emp := holdingNewEmployee(t, ctx, f.owner, kf, "active")
	insert := func(q pgx.Tx) (uuid.UUID, error) {
		var id uuid.UUID
		if _, err := q.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, kf.String()); err != nil {
			return id, err
		}
		err := q.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, verdict, channel, type)
		                        VALUES ($1, $2, $3, now(), 'ok', 'nfc', 'in') RETURNING id`, kf, emp, venue).Scan(&id)
		return id, err
	}
	a := holdingBegin(t, ctx, f.conn(t), "tappa_app")
	sp, err := a.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gone, err := insert(sp)
	if err != nil {
		t.Fatal(err)
	}
	if n := opInt(t, ctx, sp, `SELECT seq FROM punch_events WHERE punch_id = $1`, gone); n != 1 {
		t.Fatalf("PREMISE: the tap in the savepoint is seq %d, want 1", n)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// The lock went with the savepoint: another session raises the counter while `a` is open.
	b := holdingBegin(t, ctx, f.conn(t), "tappa_app")
	if _, err := b.Exec(ctx, `SET LOCAL lock_timeout = '1s'`); err != nil {
		t.Fatal(err)
	}
	other, err := insert(b)
	if err != nil {
		t.Fatalf("another session waited on a counter a rolled-back savepoint had locked: %v", err)
	}
	if err := b.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	kept, err := insert(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var seqs []int64
	rows, err := f.owner.Query(ctx, `SELECT seq FROM punch_events WHERE tenant_id = $1 ORDER BY seq`, kf)
	if err != nil {
		t.Fatal(err)
	}
	if seqs, err = pgx.CollectRows(rows, pgx.RowTo[int64]); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(seqs, []int64{1, 2}) || opInt(t, ctx, f.owner, `SELECT count(*) FROM punch_events WHERE punch_id = $1`, gone) != 0 ||
		opInt(t, ctx, f.owner, `SELECT last_seq FROM feed_counters WHERE tenant_id = $1`, kf) != 2 {
		t.Errorf("seqs %v (want [1 2] -- dense, the rolled-back tap's 1 reused), the rolled-back tap's events %d, the counter %d",
			seqs, opInt(t, ctx, f.owner, `SELECT count(*) FROM punch_events WHERE punch_id = $1`, gone),
			opInt(t, ctx, f.owner, `SELECT last_seq FROM feed_counters WHERE tenant_id = $1`, kf))
	}
	if opInt(t, ctx, f.owner, `SELECT seq FROM punch_events WHERE punch_id = $1`, other) != 1 || opInt(t, ctx, f.owner, `SELECT seq FROM punch_events WHERE punch_id = $1`, kept) != 2 {
		t.Error("the commit order is not the seq order")
	}
}

// ---------------------------------------------------- (f) verbs, binding, the relid --

// TestFeedTriggers_RefuseVerbsUnderArwdAndTempTableBinding is plan §5 API-2 (f) for BOTH definers,
// on this database (production's `arwd` default -- TestM11Catalog_TheWitnessReadsTheDefaultAndItsOrder):
//   - tappa_app cannot write the feed by hand: INSERT into punch_events, feed_counters,
//     webhook_deliveries; UPDATE of punch_events and feed_counters; DELETE of punch_events,
//     feed_counters or webhook_endpoints -- each 42501 (the triggers are the one writer -- ADR 0028 §4
//     "tappa_app olay yazamaz"); a DELETE of a fresh delivery removes nothing (T130's policy);
//   - tappa_app cannot bind any of the three trigger functions (punch_feed, feed_open_counter,
//     holding_feed) to a temporary table of its own (no EXECUTE: 42501 -- the 4th round's path to
//     an event for a refused record);
//   - bound by the owner to the wrong table, each refuses at fire time (TG_RELID: 55000);
//   - and the owner itself cannot UPDATE or DELETE an event (the append-only trigger, P0001).
func TestFeedTriggers_RefuseVerbsUnderArwdAndTempTableBinding(t *testing.T) {
	ctx, tx := opTx(t)
	w := feedWorldTx(t, ctx, tx, "")
	feedOpen(t, ctx, tx, w.kf, "KF", true)
	id := feedMust(t, ctx, tx, w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "ok"))
	seq := feedEventsOf(t, ctx, tx, id)[0].seq
	for _, c := range []struct {
		what, sql string
		args      []any
	}{
		{"INSERT an event", `INSERT INTO punch_events (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel, employee_id)
		  VALUES ($1, 999999, $2, $1, '{employer,location}', now(), now(), 'nfc', $3)`, []any{w.kf, id, w.kfEmployee}},
		{"INSERT a counter", `INSERT INTO feed_counters (tenant_id, last_seq) VALUES ($1, 0) ON CONFLICT DO NOTHING`, []any{w.kf}},
		{"INSERT a delivery", `INSERT INTO webhook_deliveries (tenant_id, endpoint_id, punch_event_seq) SELECT $1, id, $2 FROM webhook_endpoints`, []any{w.kf, seq}},
		{"UPDATE an event", `UPDATE punch_events SET location_name = 'x' WHERE tenant_id = $1`, []any{w.kf}},
		{"UPDATE a counter", `UPDATE feed_counters SET last_seq = 0 WHERE tenant_id = $1`, []any{w.kf}},
		{"DELETE an event", `DELETE FROM punch_events WHERE tenant_id = $1`, []any{w.kf}},
		{"DELETE a counter", `DELETE FROM feed_counters WHERE tenant_id = $1`, []any{w.kf}},
		{"DELETE an endpoint", `DELETE FROM webhook_endpoints WHERE tenant_id = $1`, []any{w.kf}},
	} {
		err := holdingAppIn(t, ctx, tx, w.kf, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, c.sql, c.args...)
			return err
		})
		opWant(t, err, sqlstateInsufficientPrivi, "tappa_app: "+c.what)
	}
	// T130 (00058): tappa_app holds DELETE on webhook_deliveries -- the retention sweep's -- and the
	// restrictive policy confines it to a settled row older than 90 days, so the tenant's fresh
	// (pending) delivery is not removed: 0 rows, no error. The policy's own cases are
	// webhookretention_test.go's.
	if err := holdingAppIn(t, ctx, tx, w.kf, func(sp pgx.Tx) error {
		tag, err := sp.Exec(ctx, `DELETE FROM webhook_deliveries WHERE tenant_id = $1`, w.kf)
		if err == nil && tag.RowsAffected() != 0 {
			return fmt.Errorf("deleted %d fresh deliveries", tag.RowsAffected())
		}
		return err
	}); err != nil {
		t.Errorf("tappa_app: DELETE a fresh delivery: %v", err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM webhook_deliveries WHERE tenant_id = $1`, w.kf); n == 0 {
		t.Error("PREMISE: KF has no delivery, so the DELETE above proved nothing")
	}
	for _, fn := range []string{"public.punch_feed()", "public.feed_open_counter()", "public.holding_feed()"} {
		opWant(t, opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
			if _, err := sp.Exec(ctx, `CREATE TEMP TABLE zz_api2_bind (id uuid, tenant_id uuid, site_tenant_id uuid)`); err != nil {
				return err
			}
			_, err := sp.Exec(ctx, `CREATE TRIGGER zz_api2_bound AFTER INSERT ON zz_api2_bind FOR EACH ROW EXECUTE FUNCTION `+fn)
			return err
		}), sqlstateInsufficientPrivi, "tappa_app binding "+fn+" to a table of its own")
		holdingInSavepoint(t, tx, func(sp pgx.Tx) {
			for _, s := range []string{
				`CREATE TEMP TABLE zz_api2_owner_bind (id uuid, tenant_id uuid, site_tenant_id uuid, outcome text)`,
				`CREATE TRIGGER zz_api2_owner_bound AFTER INSERT ON zz_api2_owner_bind FOR EACH ROW EXECUTE FUNCTION ` + fn,
			} {
				if _, err := sp.Exec(ctx, s); err != nil {
					t.Fatalf("%s: %v", s, err)
				}
			}
			opWant(t, opTry(t, ctx, sp, `INSERT INTO zz_api2_owner_bind VALUES ($1, $2, NULL, 'approved')`, uuid.New(), w.kf),
				sqlstatePrerequisiteState, fn+" fired on a table it is not for")
		})
	}
	for _, s := range []string{
		`UPDATE punch_events SET location_name = 'x' WHERE tenant_id = $1 AND seq = $2`,
		`DELETE FROM punch_events WHERE tenant_id = $1 AND seq = $2`,
	} {
		err := opTry(t, ctx, tx, s, w.kf, seq)
		if code, _ := opCode(err); code != "P0001" && code != "23001" {
			t.Errorf("the owner's %s: %v, want the append-only trigger's refusal", strings.Fields(s)[0], err)
		}
	}
}

// ------------------------------------------------ (m) the catalogue of the feed --

// feedTriggerOrder are the AFTER INSERT row triggers of the two tables, in the order PostgreSQL
// fires them (name order) -- the RI triggers aside. holding_feed FIRST (K-31), punch_feed second.
var feedTriggerOrder = map[string][]string{
	"transactions":        {"transactions_holding_feed", "transactions_punch_feed"},
	"transaction_reviews": {"transaction_reviews_holding_feed", "transaction_reviews_punch_feed"},
}

// feedWrittenColumns are the four tables the holding trigger writes, column by column (attnum
// order) -- ADR 0027 §3's "yazılan tabloların sütun pini": no coordinate, address, distance, note,
// policy context, verdict, matched rule or e-mail; a new column is red here.
var feedWrittenColumns = map[string]string{
	"site_punches": "tenant_id,id,transaction_id,employer_tenant_id,employer_api_code,employee_id,employee_ref,employee_code," +
		"employee_name,site_location_id,site_tag_uid,ctr,sun_valid,bucket_hour,occurred_at,channel,type,outcome,ctr_gap,created_at",
	// direction_source (M15-13, 00067): who chose the direction -- the site's row carries it too (ADR
	// 0027 §9 item 3's K note).
	"punch_events": "tenant_id,seq,punch_id,punch_tenant_id,roles,version,status,punch_time,recorded_at,channel,direction,employee_id," +
		"employee_ref,employee_code,employee_company,location_id,location_ref,location_name,location_company,tag_uid,created_at,direction_source",
	// settled_at (T130, 00058): when the delivery arrived or died -- the retention's clock, the
	// database's (its trigger), not a fact about the punch.
	"webhook_deliveries": "tenant_id,id,endpoint_id,punch_event_seq,attempt,next_attempt_at,lease_until,state,last_status," +
		"last_error_class,created_at,delivered_at,settled_at",
	"feed_counters": "tenant_id,last_seq",
}

// TestFeedCatalog_TwoRolesTriggerOrderExecuteOwnerOnlyRelid is plan §5 API-2 (m), ADR 0028 §4
// "Rollerin yaşam döngüsü ve katalog":
//   - the two definers: NOLOGIN; tappa_feedwriter NOBYPASSRLS, tappa_holdingdefiner BYPASSRLS; no
//     member and a member of nothing; no role-level setting;
//   - every trigger function in public (prorettype = trigger) of the two: EXECUTE in its owner alone
//     (aclexplode, PUBLIC seen); SECURITY DEFINER; proconfig exactly search_path + lock_timeout=2s
//     (feed_open_counter also app.tenant_id=” -- 00048's header says why);
//   - the trigger NAME ORDER on transactions and transaction_reviews: holding_feed first, punch_feed
//     second, nothing else AFTER INSERT; punch_feed's WHEN on transactions is site_tenant_id IS NULL;
//     tenants_feed_counter bound AFTER INSERT on tenants;
//   - the four written tables' columns, exactly (feedWrittenColumns);
//   - K-34's two endpoint pins: neither definer, nor PUBLIC, may SELECT webhook_endpoints.url or
//     signing_key_sealed.
//
// CONTROLS (savepoints): a trigger named transactions_a_feed sorts before holding_feed and is
// flagged; EXECUTE on punch_feed to PUBLIC is flagged; a column added to punch_events is flagged.
func TestFeedCatalog_TwoRolesTriggerOrderExecuteOwnerOnlyRelid(t *testing.T) {
	ctx, tx := opTx(t)
	findings := func(q opQuerier) []string {
		var out []string
		for _, r := range []struct {
			role   string
			bypass bool
		}{{"tappa_feedwriter", false}, {"tappa_holdingdefiner", true}} {
			var login, bypass bool
			var members, memberOf, settings int64
			if err := q.QueryRow(ctx, `SELECT rolcanlogin, rolbypassrls,
			                                  (SELECT count(*) FROM pg_auth_members m WHERE m.roleid = r.oid),
			                                  (SELECT count(*) FROM pg_auth_members m WHERE m.member = r.oid),
			                                  (SELECT count(*) FROM pg_db_role_setting s WHERE s.setrole = r.oid)
			                             FROM pg_roles r WHERE rolname = $1`, r.role).Scan(&login, &bypass, &members, &memberOf, &settings); err != nil {
				t.Fatalf("read %s: %v", r.role, err)
			}
			if login || bypass != r.bypass || members != 0 || memberOf != 0 || settings != 0 {
				out = append(out, fmt.Sprintf("%s: login=%v bypassrls=%v members=%d member_of=%d settings=%d", r.role, login, bypass, members, memberOf, settings))
			}
		}
		rows, err := q.Query(ctx, `
			SELECT p.oid::regprocedure::text, pg_get_userbyid(p.proowner), p.prosecdef, coalesce(array_to_string(p.proconfig, ';'), ''),
			       coalesce((SELECT string_agg(CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END, ',' ORDER BY 1)
			                   FROM aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) a WHERE a.privilege_type = 'EXECUTE'), '')
			  FROM pg_proc p
			 WHERE p.prorettype = 'trigger'::regtype AND p.proowner IN ('tappa_feedwriter'::regrole, 'tappa_holdingdefiner'::regrole)
			 ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		seen := 0
		for rows.Next() {
			var fn, owner, config, execute string
			var secdef bool
			if err := rows.Scan(&fn, &owner, &secdef, &config, &execute); err != nil {
				t.Fatal(err)
			}
			seen++
			want := "search_path=pg_catalog, pg_temp;lock_timeout=2s"
			if fn == "feed_open_counter()" {
				want += ";app.tenant_id="
			}
			if !secdef || execute != owner || config != want {
				out = append(out, fmt.Sprintf("%s: secdef=%v EXECUTE=%s config=%q (want the owner %s alone and %q)", fn, secdef, execute, config, owner, want))
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if seen != 5 {
			out = append(out, fmt.Sprintf("%d trigger functions of the two definers, want 5 (punch_feed, feed_open_counter, holding_feed, holding_site_gate, holding_timezone_lock)", seen))
		}
		for table, want := range feedTriggerOrder {
			rows, err := q.Query(ctx, `SELECT tgname FROM pg_trigger WHERE tgrelid = ('public.' || $1)::regclass AND NOT tgisinternal
			                             AND (tgtype & 1) = 1 AND (tgtype & 2) = 0 AND (tgtype & 4) = 4 ORDER BY tgname COLLATE "C"`, table)
			if err != nil {
				t.Fatal(err)
			}
			got, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, want) {
				out = append(out, fmt.Sprintf("%s's AFTER INSERT row triggers in firing order: %v, want %v", table, got, want))
			}
		}
		var def, tdef string
		if err := q.QueryRow(ctx, `SELECT coalesce((SELECT pg_get_triggerdef(oid) FROM pg_trigger WHERE tgname = 'transactions_punch_feed'), ''),
		                                  coalesce((SELECT pg_get_triggerdef(oid) FROM pg_trigger WHERE tgname = 'tenants_feed_counter'), '')`).Scan(&def, &tdef); err != nil {
			t.Fatal(err)
		}
		if def != `CREATE TRIGGER transactions_punch_feed AFTER INSERT ON public.transactions FOR EACH ROW WHEN ((new.site_tenant_id IS NULL)) EXECUTE FUNCTION punch_feed()` {
			out = append(out, "transactions_punch_feed is bound as: "+def)
		}
		if tdef != `CREATE TRIGGER tenants_feed_counter AFTER INSERT ON public.tenants FOR EACH ROW EXECUTE FUNCTION feed_open_counter()` {
			out = append(out, "tenants_feed_counter is bound as: "+tdef)
		}
		for table, want := range feedWrittenColumns {
			var got string
			if err := q.QueryRow(ctx, `SELECT string_agg(attname, ',' ORDER BY attnum) FROM pg_attribute
			                            WHERE attrelid = ('public.' || $1)::regclass AND attnum > 0 AND NOT attisdropped`, table).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				out = append(out, fmt.Sprintf("%s's columns are %s, want %s", table, got, want))
			}
		}
		for _, role := range []string{"tappa_feedwriter", "tappa_holdingdefiner", "public"} {
			for _, col := range []string{"url", "signing_key_sealed"} {
				var may bool
				if err := q.QueryRow(ctx, `SELECT has_column_privilege($1, 'public.webhook_endpoints', $2, 'SELECT')`, role, col).Scan(&may); err != nil {
					t.Fatal(err)
				}
				if may {
					out = append(out, role+" may SELECT webhook_endpoints."+col+" (K-34)")
				}
			}
		}
		return out
	}
	for _, f := range findings(tx) {
		t.Error(f)
	}
	for _, c := range []struct{ what, sql, want string }{
		{"a trigger that sorts first", `CREATE TRIGGER transactions_a_feed AFTER INSERT ON transactions FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation()`, "firing order"},
		{"EXECUTE on punch_feed to PUBLIC", `GRANT EXECUTE ON FUNCTION public.punch_feed() TO PUBLIC`, "punch_feed()"},
		{"a column added to the events", `ALTER TABLE punch_events ADD COLUMN note text`, "punch_events's columns"},
		{"the url readable by the feed writer", `GRANT SELECT (url) ON webhook_endpoints TO tappa_feedwriter`, "webhook_endpoints.url"},
		{"a member of the feed writer", `GRANT tappa_feedwriter TO tappa_app`, "tappa_feedwriter: login"},
	} {
		holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx, c.sql); err != nil {
				t.Fatalf("%s: %v", c.what, err)
			}
			if got := findings(sp); !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, c.want) }) {
				t.Errorf("CONTROL %s: not flagged (%v)", c.what, got)
			}
		})
	}
}

// ---------------------------------------- (o) the feed writer's grants, each one needed --

// feedwriterMatrix is tappa_feedwriter's whole reach (ADR 0028 §4's list, re-measured): "table:VERB"
// -> the exact columns in attnum order. Everything not listed is no privilege.
var feedwriterMatrix = map[string]string{
	"feed_counters:SELECT":      "tenant_id,last_seq",
	"feed_counters:INSERT":      "tenant_id,last_seq",
	"feed_counters:UPDATE":      "last_seq",
	"punch_events:INSERT":       feedEventInsertColumns,
	"webhook_deliveries:INSERT": "tenant_id,endpoint_id,punch_event_seq",
	"webhook_endpoints:SELECT":  "tenant_id,id,active",
	// direction_source (M15-13, 00067): the approval arm's read of the approved row.
	"transactions:SELECT": "id,tenant_id,employee_id,location_id,tag_uid,type,occurred_at,verdict,channel,practice,site_tenant_id,direction_source",
	"employees:SELECT":    "id,tenant_id,external_ref,employee_code",
	"locations:SELECT":    "id,tenant_id,name,external_ref",
	"tenants:SELECT":      "id,api_enabled_at,api_code",
}

// TestFeedwriter_EveryGrantIsIndividuallyNecessary is plan §5 API-2 (o), ADR 0028 §4 ("bu modelde
// yeniden ölçülecek liste ... her biri tek başına zorunlu"):
//   - the role's whole reach on this database is feedwriterMatrix (every relation, verb and column
//     of public; DELETE/TRUNCATE/REFERENCES/TRIGGER none; no sequence; USAGE on public alone);
//   - EACH (table, verb, column) of it, revoked ALONE in a savepoint, refuses (42501) at least one
//     of the paths that use it: an ok tap of a tenant with an open API and an active endpoint, the
//     approval of a flag, and a new tenant -- and so does USAGE on public. A grant nothing needs
//     would stay green here. CONTROL: with every grant the three paths write.
func TestFeedwriter_EveryGrantIsIndividuallyNecessary(t *testing.T) {
	ctx, tx := opTx(t)
	for _, f := range feedwriterReachFindings(t, ctx, tx) {
		t.Error(f)
	}
	w := feedWorldTx(t, ctx, tx, "")
	feedOpen(t, ctx, tx, w.kf, "KF", true)
	reviewer := holdingNewAdmin(t, ctx, tx, w.kf)
	drive := func(sp pgx.Tx) []error {
		var errs []error
		_, err := feedInsert(t, ctx, sp, w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "ok"))
		errs = append(errs, err)
		errs = append(errs, holdingAppIn(t, ctx, sp, w.kf, func(q pgx.Tx) error {
			var id uuid.UUID
			if err := q.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type)
			                           VALUES ($1, $2, $3, $4, now(), 'flag', 'qr', 'in') RETURNING id`, w.kf, w.kfEmployee, w.kfVenue, w.kfTag).Scan(&id); err != nil {
				return err
			}
			_, err := q.Exec(ctx, `INSERT INTO transaction_reviews (tenant_id, transaction_id, reviewer_id, outcome) VALUES ($1, $2, $3, 'approved')`, w.kf, id, reviewer)
			return err
		}))
		errs = append(errs, opTry(t, ctx, sp, `INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'api2 grant', $2, 'restaurant', 'single')`,
			uuid.New(), "VAT-API2-"+uuid.NewString()))
		return errs
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		before := opInt(t, ctx, sp, `SELECT count(*) FROM webhook_deliveries WHERE tenant_id = $1`, w.kf)
		for _, e := range drive(sp) {
			if e != nil {
				t.Fatalf("CONTROL: with every grant a path failed: %v", e)
			}
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM webhook_deliveries WHERE tenant_id = $1`, w.kf); n != before+2 {
			t.Fatalf("CONTROL: the drive delivered %d, want 2 (the tap and the approval) -- the gate and the endpoint paths are not driven", n-before)
		}
	})
	refusedBy := func(stmt string) bool {
		refused := false
		holdingInSavepoint(t, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx, stmt); err != nil {
				t.Fatal(err)
			}
			for _, e := range drive(sp) {
				if code, _ := opCode(e); code == sqlstateInsufficientPrivi {
					refused = true
				}
			}
		})
		return refused
	}
	checked := 0
	for key, cols := range feedwriterMatrix {
		table, verb, _ := strings.Cut(key, ":")
		for _, col := range strings.Split(cols, ",") {
			checked++
			if !refusedBy(fmt.Sprintf(`REVOKE %s (%s) ON %s FROM tappa_feedwriter`, verb, col, table)) {
				t.Errorf("%s (%s) ON %s: revoked, and every feed path still ran -- a grant nothing reads (drop it from its migration and the matrix)", verb, col, table)
			}
		}
	}
	// USAGE on public: PUBLIC holds it too on these clusters (PostgreSQL 15+'s default, measured on
	// the development, CI and throwaway databases), so the role's own grant is the belt for a
	// cluster where PUBLIC's has been taken away (ADR 0028 §4's 4th round measured one) -- and is
	// measured as that: with PUBLIC's revoked, the role's own grant alone carries every path, and
	// without it every path is refused.
	if refusedBy(`REVOKE USAGE ON SCHEMA public FROM PUBLIC`) {
		t.Error("CONTROL: with PUBLIC's USAGE on public revoked, a feed path was refused -- the role's own grant does not carry it")
	}
	if !refusedBy(`REVOKE USAGE ON SCHEMA public FROM PUBLIC; REVOKE USAGE ON SCHEMA public FROM tappa_feedwriter`) {
		t.Error("USAGE on public: revoked from the role and from PUBLIC, and every feed path still ran")
	}
	if checked < 30 {
		t.Fatalf("anti-vacuity: %d columns checked", checked)
	}
}

// feedwriterReachFindings: tappa_feedwriter's privileges on every relation in public against
// feedwriterMatrix, its table-level verbs (none), sequences (none), schemas (USAGE on public alone)
// and EXECUTE on a SECURITY DEFINER it does not own (none).
func feedwriterReachFindings(t *testing.T, ctx context.Context, q opQuerier) []string {
	t.Helper()
	var out []string
	rels, err := q.Query(ctx, `SELECT c.relname FROM pg_class c WHERE c.relnamespace = 'public'::regnamespace
	                             AND c.relkind IN ('r', 'p', 'v', 'm', 'f') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rels, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE"} {
			if got, want := opColumns(t, ctx, q, "tappa_feedwriter", table, priv), feedwriterMatrix[table+":"+priv]; got != want {
				out = append(out, fmt.Sprintf("%s on %s = (%s), want (%s)", priv, table, got, want))
			}
		}
		for _, priv := range []string{"DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
			if opInt(t, ctx, q, `SELECT has_table_privilege('tappa_feedwriter', 'public.' || quote_ident($1), $2)::int`, table, priv) == 1 {
				out = append(out, priv+" on "+table)
			}
		}
	}
	for _, check := range []struct{ what, sql string }{
		{"a sequence privilege", `SELECT s.relname FROM pg_class s WHERE s.relkind = 'S'
			AND (has_sequence_privilege('tappa_feedwriter', s.oid, 'USAGE') OR has_sequence_privilege('tappa_feedwriter', s.oid, 'UPDATE'))`},
		{"a schema privilege beyond USAGE on public", `SELECT n.nspname FROM pg_namespace n
			WHERE n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
			  AND (has_schema_privilege('tappa_feedwriter', n.oid, 'CREATE')
			       OR (n.nspname <> 'public' AND has_schema_privilege('tappa_feedwriter', n.oid, 'USAGE')))`},
		{"EXECUTE on another role's SECURITY DEFINER", `SELECT p.oid::regprocedure::text FROM pg_proc p
			WHERE p.proowner <> 'tappa_feedwriter'::regrole AND p.prosecdef AND has_function_privilege('tappa_feedwriter', p.oid, 'EXECUTE')`},
	} {
		rows, err := q.Query(ctx, check.sql)
		if err != nil {
			t.Fatal(err)
		}
		hits, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hits {
			out = append(out, check.what+": "+h)
		}
	}
	return out
}

// ------------------------------------------------------------- the Down and the Up --

// TestFeed00048And00049_DownGiveBackAndUpTakesItAgain: 00049's Down takes punch_feed, its two
// triggers and every grant of 00049 away and gives 00046's holding_feed back byte for byte (md5),
// KEEPING the events already written; a tap recorded while it is down is no event, 00049's Up
// again writes no history, and 00050's block (its COMMITs taken out -- it cannot commit inside a
// test's transaction) backfills it -- after the counter, no seq reused. 00048's Down REFUSES while the feed holds
// an event (append-only -- an event's seq is a promise), and with the rows set aside takes the
// tables, the trigger and the function away; the Ups give HEAD's surface back.
func TestFeed00048And00049_DownGiveBackAndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up48, down48 := opMigrationSections(t, op00048File)
	up49, down49 := opMigrationSections(t, op00049File)
	read := func(q pgx.Tx) string {
		t.Helper()
		var s string
		if err := q.QueryRow(ctx, `SELECT
			coalesce((SELECT string_agg(proname, ',' ORDER BY proname) FROM pg_proc WHERE proowner IN ('tappa_feedwriter'::regrole, 'tappa_holdingdefiner'::regrole)), '') || ' | ' ||
			coalesce((SELECT string_agg(tgname, ',' ORDER BY tgname) FROM pg_trigger
			           WHERE tgrelid IN ('public.transactions'::regclass, 'public.transaction_reviews'::regclass, 'public.tenants'::regclass) AND NOT tgisinternal), '') || ' | ' ||
			coalesce((SELECT string_agg(c.relname, ',' ORDER BY c.relname) FROM pg_class c WHERE c.relname = ANY($1) AND c.relnamespace = 'public'::regnamespace), '') || ' | ' ||
			(SELECT md5(prosrc) FROM pg_proc WHERE proname = 'holding_feed')`, feedTables).Scan(&s); err != nil {
			t.Fatal(err)
		}
		for _, tb := range []string{"transactions", "tenants", "employees", "locations"} {
			s += " | " + tb + ":" + opColumns(t, ctx, q, "tappa_feedwriter", tb, "SELECT") + "/" + opColumns(t, ctx, q, "tappa_holdingdefiner", tb, "SELECT")
		}
		return s
	}
	head := read(tx)
	if !strings.Contains(head, "punch_feed") || !strings.Contains(head, "punch_events") || strings.Contains(head, feedHoldingFeed00046MD5) {
		t.Fatalf("PREMISE at HEAD: %s", head)
	}
	up67, down67 := opMigrationSections(t, ds00067File)
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		// M15-13: the walk starts at 00066 -- 00067's Down first (it gives 00049's two bodies back), with
		// any direction source another package committed set aside inside the savepoint -- and ends with
		// 00067's Up again, so the last comparison is with HEAD itself.
		feedSetAsideDirectionSources(t, ctx, sp)
		opRunSection(t, ctx, sp, down67, "00067 Down")
		at66 := read(sp)
		if strings.Contains(at66, "direction_source") {
			t.Fatalf("PREMISE after 00067's Down: a feed writer still reads direction_source: %s", at66)
		}
		w := feedWorldTx(t, ctx, sp, "")
		before := feedMust(t, ctx, sp, w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "ok"))
		seqBefore := feedEventsOf(t, ctx, sp, before)[0].seq
		opRunSection(t, ctx, sp, down49, "00049 Down")
		s := read(sp)
		if strings.Contains(s, "punch_feed") || !strings.Contains(s, feedHoldingFeed00046MD5) || !strings.Contains(s, "punch_events") ||
			!strings.Contains(s, "transactions:/id,tenant_id,employee_id,ctr,type,occurred_at,sun_valid,channel,site_tenant_id,site_location_id,site_tag_uid") ||
			!strings.Contains(s, "tenants:/id,name,timezone,vat_verified,api_code") {
			t.Errorf("after 00049's Down: %s", s)
		}
		if len(feedEventsOf(t, ctx, sp, before)) != 1 {
			t.Error("00049's Down took an event away")
		}
		during := feedMust(t, ctx, sp, w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "qr", "ok"))
		if len(feedEventsOf(t, ctx, sp, during)) != 0 {
			t.Error("with 00049 down a tap is an event")
		}
		opRunSection(t, ctx, sp, up49, "00049 Up again")
		if ev := feedEventsOf(t, ctx, sp, during); len(ev) != 0 {
			t.Errorf("00049's Up wrote history (%+v) -- that is 00050's, in small transactions", ev)
		}
		if _, err := sp.Exec(ctx, `CREATE TEMP TABLE zz_api2_chunks (held double precision)`); err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Conn().PgConn().Exec(ctx, feedBackfillBlock(t, false)).ReadAll(); err != nil {
			t.Fatalf("00050's block (its COMMITs taken out): %v", err)
		}
		if ev := feedEventsOf(t, ctx, sp, during); len(ev) != 1 || ev[0].seq != seqBefore+1 {
			t.Errorf("00050's Up again backfilled the tap written while 00049 was down as %+v, want one event at seq %d", ev, seqBefore+1)
		}
		if ev := feedEventsOf(t, ctx, sp, before); len(ev) != 1 || ev[0].seq != seqBefore {
			t.Errorf("00049's Up again rewrote an event: %+v", ev)
		}
		if again := read(sp); again != at66 {
			t.Errorf("after 00049's Up again:\n %s\nwant 00066's\n %s", again, at66)
		}
		opRunSection(t, ctx, sp, down49, "00049 Down (2)")
		opWant(t, holdingTryDown(t, ctx, sp, down48), sqlstatePrerequisiteState, "00048's Down while the feed holds events")
		feedSetAsideRows(t, ctx, sp)
		opRunSection(t, ctx, sp, down48, "00048 Down")
		s = read(sp)
		if strings.Contains(s, "feed_open_counter") || strings.Contains(s, "tenants_feed_counter") || strings.Contains(s, "punch_events") ||
			strings.Contains(s, "feed_counters") || !strings.Contains(s, "transactions:/") {
			t.Errorf("after 00048's Down: %s", s)
		}
		opRunSection(t, ctx, sp, up48, "00048 Up again")
		opRunSection(t, ctx, sp, up49, "00049 Up again")
		if again := read(sp); again != at66 {
			t.Errorf("after the Ups again:\n %s\nwant 00066's\n %s", again, at66)
		}
		opRunSection(t, ctx, sp, up67, "00067 Up again")
		if again := read(sp); again != head {
			t.Errorf("after 00067's Up again:\n %s\nwant HEAD's\n %s", again, head)
		}
	})
}

// feedSetAsideDirectionSources NULLs every punch_events.direction_source inside the caller's savepoint
// (the append-only trigger off for the one UPDATE, on again) -- the events other packages' taps
// committed would make 00067's Down refuse, which is that Down's own test, not the caller's. The
// rollback restores them. The caller holds punch_events (holdingLockedSavepoint).
func feedSetAsideDirectionSources(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	if opInt(t, ctx, sp, `SELECT count(*) FROM punch_events WHERE direction_source IS NOT NULL`) == 0 {
		return
	}
	for _, s := range []string{
		`ALTER TABLE punch_events DISABLE TRIGGER punch_events_no_mutation`,
		`UPDATE punch_events SET direction_source = NULL WHERE direction_source IS NOT NULL`,
		`ALTER TABLE punch_events ENABLE TRIGGER punch_events_no_mutation`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// feedBackfillBlock cuts 00050's DO block from the file goose runs -- its COMMITs taken out (a
// test's transaction cannot commit inside) unless keepCommit -- timed per chunk: each chunk's span
// from holding its tenant's counter to its COMMIT (the time a tap of that tenant can wait behind
// it) goes into pg_temp.zz_api2_chunks (the caller creates it: held double precision, seconds),
// and the count, the longest hold, the longest unlocked window read and the longest wait for the
// counter are RAISEd as a NOTICE.
func feedBackfillBlock(t *testing.T, keepCommit bool) string {
	t.Helper()
	up, _ := opMigrationSections(t, op00050File)
	i := strings.Index(up, "DO $$")
	if i < 0 {
		t.Fatal("00050's Up carries no DO block")
	}
	j := strings.Index(up[i:], "-- +goose StatementEnd")
	if j < 0 {
		t.Fatal("00050's DO block has no end")
	}
	do := up[i : i+j]
	commit := ""
	if keepCommit {
		commit = "\n            COMMIT;"
	}
	for _, r := range [][2]string{
		{"    v_n      bigint;", "    v_n      bigint;\n    v_t      timestamptz;\n    v_w      timestamptz;\n    v_l      timestamptz;\n    v_max    interval := interval '0';\n    v_wmax   interval := interval '0';\n    v_lmax   interval := interval '0';\n    v_chunks integer := 0;"},
		{"            PERFORM 1 FROM public.feed_counters AS c WHERE c.tenant_id = v_tenant FOR UPDATE;", "            PERFORM 1 FROM public.feed_counters AS c WHERE c.tenant_id = v_tenant FOR UPDATE;\n            v_l := clock_timestamp();"},
		{"            -- The window, with no lock: keyset on (occurred_at, id).", "            v_w := clock_timestamp();\n            -- The window, with no lock: keyset on (occurred_at, id)."},
		{"            PERFORM pg_catalog.set_config('lock_timeout', '1s', true);", "            v_t := clock_timestamp();\n            v_wmax := greatest(v_wmax, v_t - v_w);\n            PERFORM pg_catalog.set_config('lock_timeout', '1s', true);"},
		{"            COMMIT;", "            v_chunks := v_chunks + 1; v_max := greatest(v_max, clock_timestamp() - v_l); v_lmax := greatest(v_lmax, v_l - v_t);\n" +
			"            INSERT INTO pg_temp.zz_api2_chunks VALUES (extract(epoch FROM clock_timestamp() - v_l));" + commit},
		{"END\n$$;", "RAISE NOTICE 'api2 chunks % max % window % lockwait %', v_chunks, extract(epoch FROM v_max), extract(epoch FROM v_wmax), extract(epoch FROM v_lmax);\nEND\n$$;"},
	} {
		if strings.Count(do, r[0]) != 1 {
			t.Fatalf("00050's DO block no longer carries %q exactly once -- the instrumentation does not fit", r[0])
		}
		do = strings.Replace(do, r[0], r[1], 1)
	}
	return do
}

// TestFeed_OneCounterSetPerTransaction is the API-2 review's D-1, row by row (ADR 0028 §5): a
// transaction's FIRST event locks its counter set ({tenant} for an own tap, {employer, site} for
// a holding tap); a later event-writing row of the same transaction may use only that set, or it
// is refused with SQLSTATE TF001 before any lock. Each case in a savepoint of its own:
//   - own KF, own KF again: written; own KM after them: TF001;
//   - KM@KF first: then own KF, own KM and KF@KM are all inside {KF, KM}: written;
//   - own KF, then KM@KF (needs KM too): TF001;
//   - a row that writes no event (KM's flag, KM's manual record) after own KF: written -- the
//     check is on the events, not the rows;
//   - an event in a savepoint rolled back leaves no mark: own KF there, rolled back, then own KM:
//     written (checkin's one retry writes its record this way).
func TestFeed_OneCounterSetPerTransaction(t *testing.T) {
	ctx, tx := opTx(t)
	w := feedWorldTx(t, ctx, tx, "")
	kfOwn := w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "ok")
	kmOwn := w.own(w.km, w.kmEmployee, w.kmVenue, w.kmTag, "nfc", "ok")
	refused := func(err error) bool { code, _ := opCode(err); return code == "TF001" }
	for _, c := range []struct {
		what  string
		rows  []feedRow
		wants []bool // true = written, false = TF001
	}{
		{"own KF, own KF, own KM", []feedRow{kfOwn, kfOwn, kmOwn}, []bool{true, true, false}},
		{"KM@KF, own KF, own KM, KF@KM", []feedRow{w.kmAtKF("nfc", "ok"), kfOwn, kmOwn, w.kfAtKM("nfc", "ok")}, []bool{true, true, true, true}},
		{"own KF, KM@KF", []feedRow{kfOwn, w.kmAtKF("nfc", "ok")}, []bool{true, false}},
		{"own KF, KM's flag, KM's manual record", []feedRow{kfOwn, w.own(w.km, w.kmEmployee, w.kmVenue, w.kmTag, "nfc", "flag"),
			{tenant: w.km, employee: w.kmEmployee, location: &w.kmVenue, verdict: "ok", channel: "manual", kind: "in"}}, []bool{true, true, true}},
	} {
		holdingInSavepoint(t, tx, func(sp pgx.Tx) {
			for i, r := range c.rows {
				_, err := feedInsert(t, ctx, sp, r)
				switch {
				case c.wants[i] && err != nil:
					t.Errorf("%s: row %d refused: %v", c.what, i+1, err)
				case !c.wants[i] && !refused(err):
					t.Errorf("%s: row %d: %v, want TF001", c.what, i+1, err)
				}
			}
		})
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		inner, err := sp.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := feedInsert(t, ctx, inner, kfOwn); err != nil {
			t.Fatal(err)
		}
		if err := inner.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := feedInsert(t, ctx, sp, kmOwn); err != nil {
			t.Errorf("own KM after own KF was rolled back to a savepoint: %v, want written", err)
		}
	})
}
