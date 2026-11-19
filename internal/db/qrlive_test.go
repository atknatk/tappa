package db

// qrlive_test.go -- migrations 00112 and 00113 (M16-19, ADR 0045 §2.5, Bedel 6; Q38 (a), Q43 (b)):
// the channel value 'qr_live' in every closed channel set and in the punch feed's triggers. Everything
// runs in an owner transaction (opTx) that is rolled back; app-side writes go through holdingAppIn
// (tappa_app in the tenant's context), so the triggers fire as they do for a tap. Nothing here
// commits a qr_live row. From M16-21 the product writes them: the Down test below then needs those
// rows set aside first, like the other refusing Downs' walks.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	op00112File = "00112_add_the_qr_live_channel.sql"
	op00113File = "00113_validate_the_qr_live_checks.sql"
)

// qrLiveSets are the five constraints 00112 widens, with whether each is VALID at HEAD.
var qrLiveSets = []struct {
	table, name string
	valid       bool
}{
	{"transactions", "transactions_channel_check", true},
	{"transactions", "transactions_site_by_channel", true},
	{"transactions", "transactions_shift_snapshot_shape", false},
	{"punch_events", "punch_events_channel", true},
	{"site_punches", "site_punches_channel_check", true},
}

// qrLiveBodies are the md5s of the three bodies before 00112 (00067's two, 00043's gate) and after.
var qrLiveBodies = []struct{ fn, before, after string }{
	{"punch_feed()", "8878c609a1e745c3753dffe7730bad9a", "1bda5aeed5b21a4bc7f83358cc980c9c"},
	{"holding_feed()", "2043e7eca5758c33974bcb0a8785d493", "2320bcb86611f56737851c1fc4c4eb9f"},
	{"holding_site_gate()", "032199e94329be7ce926b77746695ec8", "3c2a06f02032eaa251c299e8730f8033"},
}

// qrLiveDownTables are the tables 00112's and 00113's Downs lock (ACCESS EXCLUSIVE, in a tap's order).
var qrLiveDownTables = []string{"transactions", "punch_events", "site_punches"}

// qlDown runs 00113's and 00112's Downs inside sp: the step an older migration's walk (00043's,
// 00048's, 00067's, 00095's -- each Down re-adds a set or a body 00112 replaced) takes first, in
// goose's own order, before clDown. Since M16-21 the tap path commits qr_live rows (other packages'
// tests write them), so the rows are set aside first (qrLiveRowsSetAsideSQL), inside the same
// rolled-back savepoint. The caller holds transactions, punch_events and site_punches
// (lockForDowns / holdingLockedSavepoint).
func qlDown(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	_, down13 := opMigrationSections(t, op00113File)
	_, down12 := opMigrationSections(t, op00112File)
	opRunSection(t, ctx, sp, down13, "00113 Down")
	opRunSection(t, ctx, sp, qrLiveRowsSetAsideSQL, "set the live QR rows aside")
	opRunSection(t, ctx, sp, down12, "00112 Down")
}

// qrLiveChannelMigration is 00112's number.
const qrLiveChannelMigration = 112

// qrLiveRowsSetAsideSQL relabels every committed live QR row inside a walk's transaction so 00112's
// Down -- which refuses while transactions, punch_events or site_punches holds one -- can run; the
// rollback brings them back (each table's append-only trigger off for its UPDATE only). M16-21 is the
// first card whose tap path commits such rows (the M16-19 card's walk note). The labels chosen are the
// ones the OLD sets admit for that row's shape: a holding refusal names a site and no plaque, which
// before 00112 only the manual arm of transactions_site_by_channel allowed -- and a manual row carries
// no client (00109) -- so it becomes manual without a client; every other live QR row becomes qr
// (no plaque is required of qr by any set). The feed's two tables have no shape rule on the channel.
const qrLiveRowsSetAsideSQL = `ALTER TABLE transactions DISABLE TRIGGER transactions_no_mutation;
UPDATE transactions SET channel = 'manual', client = NULL WHERE channel = 'qr_live' AND site_tenant_id IS NOT NULL;
UPDATE transactions SET channel = 'qr' WHERE channel = 'qr_live';
ALTER TABLE transactions ENABLE TRIGGER transactions_no_mutation;
ALTER TABLE punch_events DISABLE TRIGGER punch_events_no_mutation;
UPDATE punch_events SET channel = 'qr' WHERE channel = 'qr_live';
ALTER TABLE punch_events ENABLE TRIGGER punch_events_no_mutation;
ALTER TABLE site_punches DISABLE TRIGGER site_punches_no_mutation;
UPDATE site_punches SET channel = 'qr' WHERE channel = 'qr_live';
ALTER TABLE site_punches ENABLE TRIGGER site_punches_no_mutation;`

// qlUp runs 00112's and 00113's Ups inside sp: the step such a walk takes last, after clUp.
func qlUp(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	up12, _ := opMigrationSections(t, op00112File)
	up13, _ := opMigrationSections(t, op00113File)
	opRunSection(t, ctx, sp, up12, "00112 Up")
	opRunSection(t, ctx, sp, up13, "00113 Up")
}

// qrLiveSurface is what 00112 and 00113 change, read from the catalog: each set's text and VALID flag,
// and each body's md5.
func qrLiveSurface(t *testing.T, ctx context.Context, q opQuerier) string {
	t.Helper()
	var b strings.Builder
	for _, c := range qrLiveSets {
		var def string
		var valid bool
		if err := q.QueryRow(ctx, `SELECT pg_get_constraintdef(oid), convalidated FROM pg_constraint
		                            WHERE conrelid = ('public.' || $1)::regclass AND conname = $2`, c.table, c.name).Scan(&def, &valid); err != nil {
			t.Fatalf("read %s.%s: %v", c.table, c.name, err)
		}
		b.WriteString(c.name + " valid=" + map[bool]string{true: "t", false: "f"}[valid] + " " + def + "\n")
	}
	for _, f := range qrLiveBodies {
		var sum string
		if err := q.QueryRow(ctx, `SELECT md5(prosrc) FROM pg_proc WHERE oid = ('public.' || $1)::regprocedure`, f.fn).Scan(&sum); err != nil {
			t.Fatalf("read %s: %v", f.fn, err)
		}
		b.WriteString(f.fn + " " + sum + "\n")
	}
	return b.String()
}

// TestQRLive00112_EverySetAdmitsTheLiveQR is the card's "every closed set, at once": at HEAD the five
// sets name qr_live and are VALID where they were (the shift snapshot's stays NOT VALID, 00095), the
// three bodies are 00112's, and -- through the triggers, as tappa_app -- a live QR tap is a tap:
//   - an ok qr_live tap with no plaque and a shift snapshot is written, and punch_feed writes its
//     event (Q38 (a): the feed stores the REAL channel; tag NULL; the direction's source 'auto');
//   - a flagged one is no event until its approval, which writes one (the approval arm);
//   - practice and an undirected flag are no event, the manual channel is still no event;
//   - a spelling outside the set ('QR_LIVE') is refused by transactions_channel_check.
func TestQRLive00112_EverySetAdmitsTheLiveQR(t *testing.T) {
	ctx, tx := opTx(t)
	surface := qrLiveSurface(t, ctx, tx)
	for _, c := range qrLiveSets {
		line := ""
		for _, l := range strings.Split(surface, "\n") {
			if strings.HasPrefix(l, c.name+" ") {
				line = l
			}
		}
		if !strings.Contains(line, "'qr_live'") {
			t.Errorf("%s does not name qr_live: %s", c.name, line)
		}
		if want := map[bool]string{true: "valid=t", false: "valid=f"}[c.valid]; !strings.Contains(line, want) {
			t.Errorf("%s: %s, want %s", c.name, line, want)
		}
	}
	for _, f := range qrLiveBodies {
		if !strings.Contains(surface, f.fn+" "+f.after) {
			t.Errorf("%s is not 00112's body:\n%s", f.fn, surface)
		}
	}

	w := feedWorldTx(t, ctx, tx, "")
	reviewer := holdingNewAdmin(t, ctx, tx, w.kf)
	live := func(verdict, kind string, practice bool) feedRow {
		return feedRow{tenant: w.kf, employee: w.kfEmployee, location: &w.kfVenue, verdict: verdict, channel: "qr_live", kind: kind, practice: practice}
	}

	var ok uuid.UUID
	if err := holdingAppIn(t, ctx, tx, w.kf, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `
			INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, verdict, channel, type, practice, shift_source)
			VALUES ($1, $2, $3, now() - interval '5 seconds', 'ok', 'qr_live', 'in', false, 'none') RETURNING id`,
			w.kf, w.kfEmployee, w.kfVenue).Scan(&ok)
	}); err != nil {
		t.Fatalf("an ok live QR tap with a shift snapshot: %v", err)
	}
	ev := feedEventsOf(t, ctx, tx, ok)
	if len(ev) != 1 || ev[0].tenant != w.kf || ev[0].channel != "qr_live" || ev[0].direction != "in" || ev[0].tag != nil ||
		ev[0].locationID == nil || *ev[0].locationID != w.kfVenue || ev[0].directionSource == nil || *ev[0].directionSource != "auto" {
		t.Fatalf("an ok live QR tap's events: %+v; want one KF event, channel qr_live, no plaque, the venue, source auto", ev)
	}

	flag := feedMust(t, ctx, tx, live("flag", "in", false))
	if n := len(feedEventsOf(t, ctx, tx, flag)); n != 0 {
		t.Fatalf("a flagged live QR tap wrote %d events before its approval", n)
	}
	if err := feedReview(t, ctx, tx, w.kf, reviewer, flag, "approved"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if ev := feedEventsOf(t, ctx, tx, flag); len(ev) != 1 || ev[0].channel != "qr_live" {
		t.Fatalf("an approved live QR flag's events: %+v, want its one event", ev)
	}
	for _, c := range []struct {
		what string
		row  feedRow
	}{
		{"a practice live QR tap", live("ok", "in", true)},
		{"an undirected live QR flag", live("flag", "", false)},
		{"a manual record", feedRow{tenant: w.kf, employee: w.kfEmployee, location: &w.kfVenue, verdict: "ok", channel: "manual", kind: "in"}},
	} {
		if n := len(feedEventsOf(t, ctx, tx, feedMust(t, ctx, tx, c.row))); n != 0 {
			t.Errorf("%s wrote %d events, want none", c.what, n)
		}
	}
	bad := live("ok", "in", false)
	bad.channel = "QR_LIVE"
	_, err := feedInsert(t, ctx, tx, bad)
	opWantConstraint(t, err, "transactions_channel_check", "a channel spelled outside the set")
}

// TestQRLive00112_HoldingRefusalReachesTheSiteOncePerHour is Q43 (b) through the triggers: KM's
// employee at KF's live QR screen is refused, the refusal is RECORDED at KM with the site columns
// (site_location_id KF's venue, no plaque), it is no feed event, and holding_feed projects it to KF as
// one `refused` row on the qr_live channel -- the widened site_punches CHECK admits it.
//
// THE ACCEPTED DIFFERENCE (00112's header, the card's either/or): a second refusal of the same person
// in the same hour at ANOTHER of KF's venues is recorded at KM too, and the site sees no second row --
// site_punches_other_refusal_hourly keys site_tag_uid, NULL for a screen, so it meets the first row's
// conflict and DO NOTHING. A static QR refusal at another plaque in the same hour is a second site row
// (its plaque differs) -- the control that the probe reaches the projection.
func TestQRLive00112_HoldingRefusalReachesTheSiteOncePerHour(t *testing.T) {
	ctx, tx := opTx(t)
	w := feedWorldTx(t, ctx, tx, "")
	var otherVenue uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, 'KF Msida') RETURNING id`, w.kf).Scan(&otherVenue); err != nil {
		t.Fatal(err)
	}
	otherTag := holdingNewTag(t, ctx, tx, w.kf, &otherVenue, "active", 100)
	refusal := func(venue uuid.UUID) feedRow {
		return feedRow{tenant: w.km, employee: w.kmEmployee, verdict: "reject", channel: "qr_live", site: &w.kf, siteLocation: &venue}
	}
	sitePunches := func(channel string) int64 {
		return opInt(t, ctx, tx, `SELECT count(*) FROM site_punches WHERE tenant_id = $1 AND employee_id = $2 AND channel = $3`, w.kf, w.kmEmployee, channel)
	}

	first := feedMust(t, ctx, tx, refusal(w.kfVenue))
	var outcome, channel string
	var loc *uuid.UUID
	var tag *string
	if err := tx.QueryRow(ctx, `SELECT outcome, channel, site_location_id, site_tag_uid FROM site_punches WHERE transaction_id = $1`, first).
		Scan(&outcome, &channel, &loc, &tag); err != nil {
		t.Fatalf("KF's projection of the refusal: %v", err)
	}
	if outcome != "refused" || channel != "qr_live" || loc == nil || *loc != w.kfVenue || tag != nil {
		t.Fatalf("KF sees %s on %s at %v with plaque %v; want refused, qr_live, KF's venue, no plaque", outcome, channel, loc, tag)
	}
	if n := len(feedEventsOf(t, ctx, tx, first)); n != 0 {
		t.Fatalf("a refused holding live QR tap wrote %d feed events", n)
	}

	second := feedMust(t, ctx, tx, refusal(otherVenue))
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM transactions WHERE id = ANY($1) AND tenant_id = $2 AND channel = 'qr_live'`,
		[]uuid.UUID{first, second}, w.km); n != 2 {
		t.Fatalf("%d of the two refusals are recorded at KM, want both (§4.6)", n)
	}
	if n := sitePunches("qr_live"); n != 1 {
		t.Fatalf("KF sees %d live QR refusal rows for the person in one hour, want 1 (the accepted dedup)", n)
	}

	// CONTROL: static QR refusals at two plaques in the same hour are two site rows.
	for _, uid := range []string{w.kfTag, otherTag} {
		u := uid
		feedMust(t, ctx, tx, feedRow{tenant: w.km, employee: w.kmEmployee, verdict: "reject", channel: "qr", site: &w.kf, siteTag: &u, sunValid: ptr(false)})
	}
	if n := sitePunches("qr"); n != 2 {
		t.Fatalf("CONTROL: two static QR refusals at two plaques reach KF as %d rows, want 2", n)
	}
}

// TestQRLive00112And00113_DownsRefuseALiveQRRowAndOtherwiseGiveBack: with a qr_live row in
// transactions, in punch_events or in site_punches -- each alone, one case per arm of the Down's
// refusal -- 00113's Down (it holds no data) runs and 00112's REFUSES (55000) and changes nothing;
// with none, the two Downs give back the five sets by their old texts -- none names qr_live, VALID
// where they were before 00112 -- and the three bodies 00067 and 00043 installed (md5); the Ups take
// HEAD again, byte for byte. The tables the Downs lock are taken first (lockForDowns), and the Downs
// take no strong lock outside them.
func TestQRLive00112And00113_DownsRefuseALiveQRRowAndOtherwiseGiveBack(t *testing.T) {
	ctx, tx := opTx(t)
	up12, down12 := opMigrationSections(t, op00112File)
	up13, down13 := opMigrationSections(t, op00113File)
	tables := qrLiveDownTables
	lockForDowns(t, ctx, tx, tables)
	// Since M16-21 other packages' tests COMMIT live QR rows; they are set aside for this test's
	// transaction (rolled back with it), so each case below holds exactly the one row it seeds.
	opRunSection(t, ctx, tx, qrLiveRowsSetAsideSQL, "set the committed live QR rows aside")
	head := qrLiveSurface(t, ctx, tx)

	// One case per table the Down reads, each with qr_live in THAT table alone: the transactions row
	// that wrote an event or a site row is relabelled inside the savepoint (its append-only trigger
	// off for the statement -- never committed), so the refusal is that arm's.
	relabel := func(sp pgx.Tx, id uuid.UUID, channel string) {
		t.Helper()
		opRunSection(t, ctx, sp, `ALTER TABLE transactions DISABLE TRIGGER transactions_no_mutation;
UPDATE transactions SET channel = '`+channel+`' WHERE id = '`+id.String()+`';
ALTER TABLE transactions ENABLE TRIGGER transactions_no_mutation;`, "relabel the row")
	}
	for _, c := range []struct {
		table string
		seed  func(sp pgx.Tx)
	}{
		{"transactions", func(sp pgx.Tx) {
			w := newAppDeviceWorld(t, ctx, sp)
			if err := holdingAppIn(t, ctx, sp, w.tenant, func(as pgx.Tx) error {
				_, err := as.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, verdict, channel, type)
				                         VALUES ($1, $2, $3, now(), 'flag', 'qr_live', 'in')`, w.tenant, w.employee, w.venue)
				return err
			}); err != nil {
				t.Fatalf("a live QR row: %v", err)
			}
		}},
		{"punch_events", func(sp pgx.Tx) {
			w := feedWorldTx(t, ctx, sp, "")
			id := feedMust(t, ctx, sp, feedRow{tenant: w.kf, employee: w.kfEmployee, location: &w.kfVenue, verdict: "ok", channel: "qr_live", kind: "in"})
			relabel(sp, id, "qr")
		}},
		{"site_punches", func(sp pgx.Tx) {
			w := feedWorldTx(t, ctx, sp, "")
			id := feedMust(t, ctx, sp, feedRow{tenant: w.km, employee: w.kmEmployee, verdict: "reject", channel: "qr_live", site: &w.kf, siteLocation: &w.kfVenue})
			relabel(sp, id, "manual")
		}},
	} {
		t.Run("a qr_live row in "+c.table+" alone", func(t *testing.T) {
			dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				c.seed(sp)
				for _, tb := range qrLiveDownTables {
					n := opInt(t, ctx, sp, `SELECT count(*) FROM public.`+tb+` WHERE channel = 'qr_live'`)
					if want := map[bool]int64{true: 1, false: 0}[tb == c.table]; n != want {
						t.Fatalf("PREMISE: %d qr_live row(s) in %s, want %d", n, tb, want)
					}
				}
				opRunSection(t, ctx, sp, down13, "00113 Down with a live QR row")
				at12 := qrLiveSurface(t, ctx, sp)
				opWant(t, holdingTryDown(t, ctx, sp, down12), sqlstatePrerequisiteState, "00112's Down while "+c.table+" holds a live QR row")
				if again := qrLiveSurface(t, ctx, sp); again != at12 {
					t.Errorf("the refused Down changed something:\n%s\nwant\n%s", again, at12)
				}
			})
		})
	}

	dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down13, "00113 Down")
		for _, c := range qrLiveSets {
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_constraint WHERE conrelid = ('public.' || $1)::regclass AND conname = $2 AND NOT convalidated`,
				c.table, c.name); n != 1 {
				t.Errorf("after 00113's Down %s is VALID, want 00112's NOT VALID", c.name)
			}
		}
		opRunSection(t, ctx, sp, down12, "00112 Down")
		at111 := qrLiveSurface(t, ctx, sp)
		if strings.Contains(at111, "qr_live") {
			t.Errorf("after the Downs a set still names qr_live:\n%s", at111)
		}
		for _, c := range qrLiveSets {
			want := map[bool]string{true: "valid=t", false: "valid=f"}[c.valid]
			if !strings.Contains(at111, c.name+" "+want) {
				t.Errorf("after the Downs %s is not %s as before 00112:\n%s", c.name, want, at111)
			}
		}
		for _, f := range qrLiveBodies {
			if !strings.Contains(at111, f.fn+" "+f.before) {
				t.Errorf("after 00112's Down %s is not the body before it:\n%s", f.fn, at111)
			}
		}
		if extra := strongTableLocksOutside(t, ctx, sp, tables); len(extra) > 0 {
			t.Errorf("the Downs took a strong lock on %v, outside the three they name", extra)
		}
		opRunSection(t, ctx, sp, up12, "00112 Up again")
		opRunSection(t, ctx, sp, up13, "00113 Up again")
		if again := qrLiveSurface(t, ctx, sp); again != head {
			t.Errorf("after the Ups:\n%s\nwant HEAD's\n%s", again, head)
		}
	})
}
