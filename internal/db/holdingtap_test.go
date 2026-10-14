package db

// holdingtap_test.go -- M11 API-1b at the database: migrations 00043 and 00044 (transactions'
// site columns and their CHECKs, holding_tap_site, holding_advance_tag_counter, holding_logo,
// holding_site_gate) against real Postgres. ADR 0027 §2-§6 and "Test edilecekler → Çapraz
// dokunuş (API-1b)"; plan §5's API-1b acceptance core, the items noted at each test.
//
// THE WORLD (holdingWorldIn): KF and KM, peers in holding H; C and D, peers in a SECOND holding
// H2 (K-33: with C in no holding the M3 mutant passes every test -- 7th round); a KF venue
// carrying distinctive evidence and three KF plaques (active at counter 100, lost, unassigned);
// a C venue and an active C plaque at 100; a KM employee; KF VIES verified with a logo. Single-
// session cases build it in opTx's rolled-back transaction; the races build it COMMITTED on
// holdingLiveFixture (its cleanup removes the memberships and the holdings).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ------------------------------------------------------------------- the world --

type holdingWorld struct {
	kf, km, c, d    uuid.UUID
	h, h2           uuid.UUID
	kfVenue, cVenue uuid.UUID
	kfTag, cTag     string // active, last_ctr 100
	kfLost, kfLoose string // lost (on the KF venue), unassigned (no wall)
	kmEmployee      uuid.UUID
	cEmployee       uuid.UUID
	kfDigest        string
}

// holdingNewTag inserts a plaque of tenant's (a random uid, a 44-byte stand-in envelope -- no
// test here unwraps a key) and returns its uid.
func holdingNewTag(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID, wall *uuid.UUID, status string, ctr int32) string {
	t.Helper()
	b := make([]byte, 7)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	uid := strings.ToUpper(hex.EncodeToString(b))
	key := make([]byte, 44)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Exec(ctx, `INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, $5, $6)`,
		uid, tenant, wall, key, ctr, status); err != nil {
		t.Fatalf("insert a %s plaque: %v", status, err)
	}
	return uid
}

// holdingNewAdmin inserts an owner of tenant (the brand row's author).
func holdingNewAdmin(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := q.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Owner', $3, $4, 'owner')`,
		id, tenant, "owner-"+id.String()[:12]+"@example.test", opFakeDigest("h")); err != nil {
		t.Fatalf("insert an owner: %v", err)
	}
	return id
}

// holdingNewLogo sets tenant's VIES answer and gives it a brand row with an accent and a logo
// (a few bytes -- the CHECKs hold the digest to them, not their being a picture); it returns
// the digest.
func holdingNewLogo(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID, vies bool) string {
	t.Helper()
	if _, err := q.Exec(ctx, `UPDATE tenants SET vat_verified = $2 WHERE id = $1`, tenant, vies); err != nil {
		t.Fatalf("set the VIES answer: %v", err)
	}
	logo := make([]byte, 24)
	if _, err := rand.Read(logo); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(logo)
	digest := hex.EncodeToString(sum[:])
	admin := holdingNewAdmin(t, ctx, q, tenant)
	if _, err := q.Exec(ctx, `INSERT INTO tenant_branding (tenant_id, accent, logo, logo_sha256, logo_mime, logo_width, logo_height, updated_by)
	                          VALUES ($1, '1F5C41', $2, $3, 'image/png', 24, 24, $4)`, tenant, logo, digest, admin); err != nil {
		t.Fatalf("insert a brand row: %v", err)
	}
	return digest
}

// holdingNewEmployee inserts an active employee of tenant with an external ref and no home venue
// (K-14).
func holdingNewEmployee(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := q.Exec(ctx, `INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, $3, 'Holding Staff', $4, now())`,
		id, tenant, "ext-"+id.String()[:8], status); err != nil {
		t.Fatalf("insert an employee: %v", err)
	}
	return id
}

// holdingWorldIn builds the world on q (opTx's transaction, or holdingLive's owner connection
// through its helpers). Every tenant is Europe/Malta (K-36).
func holdingWorldIn(t *testing.T, ctx context.Context, q opQuerier, newTenant func(name string) uuid.UUID, newHolding func() uuid.UUID,
	member func(holding, tenant uuid.UUID, joinedAgo, leftAgo string), kmLeftAgo string) holdingWorld {
	t.Helper()
	var w holdingWorld
	w.kf, w.km, w.c, w.d = newTenant("KF"), newTenant("KM"), newTenant("C"), newTenant("D")
	w.h, w.h2 = newHolding(), newHolding()
	member(w.h, w.kf, "10 minutes", "")
	member(w.h, w.km, "10 minutes", kmLeftAgo)
	member(w.h2, w.c, "10 minutes", "")
	member(w.h2, w.d, "10 minutes", "")
	if err := q.QueryRow(ctx, `INSERT INTO locations (tenant_id, name, external_ref, static_ips, gps_lat, gps_lng, shift_start, shift_end)
	                           VALUES ($1, 'KF St Julians', 'KF-SJ', ARRAY['203.0.113.0/24'::cidr], 35.918, 14.489, '09:00', '17:00') RETURNING id`,
		w.kf).Scan(&w.kfVenue); err != nil {
		t.Fatalf("KF venue: %v", err)
	}
	if err := q.QueryRow(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, 'C Venue') RETURNING id`, w.c).Scan(&w.cVenue); err != nil {
		t.Fatalf("C venue: %v", err)
	}
	w.kfTag = holdingNewTag(t, ctx, q, w.kf, &w.kfVenue, "active", 100)
	w.kfLost = holdingNewTag(t, ctx, q, w.kf, &w.kfVenue, "lost", 100)
	w.kfLoose = holdingNewTag(t, ctx, q, w.kf, nil, "unassigned", 0)
	w.cTag = holdingNewTag(t, ctx, q, w.c, &w.cVenue, "active", 100)
	w.kmEmployee = holdingNewEmployee(t, ctx, q, w.km, "active")
	w.cEmployee = holdingNewEmployee(t, ctx, q, w.c, "active")
	w.kfDigest = holdingNewLogo(t, ctx, q, w.kf, true)
	return w
}

// holdingWorldTx is the world inside opTx's transaction; kmLeftAgo "" keeps KM a live member.
func holdingWorldTx(t *testing.T, ctx context.Context, tx pgx.Tx, kmLeftAgo string) holdingWorld {
	t.Helper()
	return holdingWorldIn(t, ctx, tx,
		func(name string) uuid.UUID {
			return opNewTenant(t, ctx, tx, "api1b "+name+" "+opToken(t), 0)
		},
		func() uuid.UUID { return holdingNew(t, ctx, tx, "api1b "+opToken(t)) },
		func(h, tenant uuid.UUID, joinedAgo, leftAgo string) {
			holdingInsertMember(t, ctx, tx, h, holdingMember{tenant, joinedAgo, leftAgo})
		}, kmLeftAgo)
}

// holdingWorldLive is the world COMMITTED on f (for the races).
func holdingWorldLive(t *testing.T, f *holdingLive) holdingWorld {
	t.Helper()
	return holdingWorldIn(t, f.ctx, f.owner,
		func(string) uuid.UUID { return f.tenant(t, "Europe/Malta", nil) },
		func() uuid.UUID { return f.holding(t) },
		func(h, tenant uuid.UUID, joinedAgo, leftAgo string) { f.member(t, h, tenant, joinedAgo, leftAgo) }, "")
}

// holdingAppIn runs fn as tappa_app with app.tenant_id = tenant inside a savepoint of tx -- the
// customer pool's shape. uuid.Nil is "no context": the GUC set to ” (a released savepoint keeps
// a SET LOCAL for the rest of the transaction, so an earlier call's tenant would otherwise still
// be there -- measured, the first version of this helper read KF's plaque "with no context").
func holdingAppIn(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID, fn func(sp pgx.Tx) error) error {
	t.Helper()
	return opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		guc := ""
		if tenant != uuid.Nil {
			guc = tenant.String()
		}
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, guc); err != nil {
			return err
		}
		return fn(sp)
	})
}

// holdingSiteRows is how many rows holding_tap_site answers tenant's context for uid.
func holdingSiteRows(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID, uid string) int64 {
	t.Helper()
	var n int64
	if err := holdingAppIn(t, ctx, tx, tenant, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT count(*) FROM public.holding_tap_site($1)`, uid).Scan(&n)
	}); err != nil {
		t.Fatalf("holding_tap_site: %v", err)
	}
	return n
}

// holdingLogoRows is how many rows holding_logo answers.
func holdingLogoRows(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID, uid, digest string) int64 {
	t.Helper()
	var n int64
	if err := holdingAppIn(t, ctx, tx, tenant, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT count(*) FROM public.holding_logo($1, $2)`, uid, digest).Scan(&n)
	}); err != nil {
		t.Fatalf("holding_logo: %v", err)
	}
	return n
}

// holdingAdvanceIn runs holding_advance_tag_counter in tenant's context, and the plaque's
// counter after it (read as the owner).
func holdingAdvanceIn(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID, uid string, ctr int32) (outcome string, status *string, lastCtr int64) {
	t.Helper()
	if err := holdingAppIn(t, ctx, tx, tenant, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT outcome, tag_status FROM public.holding_advance_tag_counter($1, $2)`, uid, ctr).Scan(&outcome, &status)
	}); err != nil {
		t.Fatalf("holding_advance_tag_counter: %v", err)
	}
	return outcome, status, opInt(t, ctx, tx, `SELECT coalesce((SELECT last_ctr FROM tags WHERE uid = $1), -1)`, uid)
}

// holdingStr is a nullable text as a test compares it ("" for NULL).
func holdingStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// holdingRow is one transactions row as a test writes it -- as tappa_app in the employer's
// context, the way the product's writers do.
type holdingRow struct {
	employer, employee     uuid.UUID
	channel, verdict, kind string // kind: the direction ("" = none)
	sunValid               *bool
	ctr                    *int32
	siteTenant, siteVenue  *uuid.UUID
	siteTag                *string
	localVenue             *uuid.UUID
	localTag               *string
}

func holdingInsertRow(t *testing.T, ctx context.Context, tx pgx.Tx, r holdingRow) error {
	t.Helper()
	var kind any
	if r.kind != "" {
		kind = r.kind
	}
	return holdingAppIn(t, ctx, tx, r.employer, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `
			INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr,
			                          site_tenant_id, site_location_id, site_tag_uid, location_id, tag_uid)
			VALUES ($1, $2, now(), $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			r.employer, r.employee, r.verdict, r.channel, kind, r.sunValid, r.ctr,
			r.siteTenant, r.siteVenue, r.siteTag, r.localVenue, r.localTag)
		return err
	})
}

func ptr[T any](v T) *T { return &v }

// holdingWantGate fails unless err is the site gate's refusal (23514 naming holding_site_gate).
func holdingWantGate(t *testing.T, err error, what string) {
	t.Helper()
	opWantConstraint(t, err, "holding_site_gate", what)
}

// ------------------------------------------------------- (j) the CHECK by channel --

// TestHoldingCheck_ChannelRules is K-30 (ADR 0027 §4) and plan §5 API-1b (j), as tappa_app in
// KM's context:
//   - the refusal of a KF plaque with no wall (unassigned) is WRITTEN: site_location_id NULL is
//     allowed on a tap channel (§4.6);
//   - a manual record for KF's venue (site_location_id, no plaque) is written;
//   - site_tenant_id = tenant_id is refused -- by the gate first (a tenant is never its own
//     peer), and with the gate disabled by transactions_site_is_another_tenant;
//   - site_tenant_id NULL with a site location or a site plaque is refused (the 7th round's
//     MATCH SIMPLE hole: C's location id carried past both keys);
//   - a tap channel with no site plaque, a manual record with one or with no site location, and
//     a site row that also names a local venue or plaque are each refused by their CHECK;
//   - and a site plaque or location of ANOTHER tenant than site_tenant_id is refused by the
//     composite keys (23503).
//
// The four CHECKs are judged with the gate disabled (in a savepoint), so each refusal is the
// CHECK's own -- not the gate's, which a BEFORE trigger reaches first.
func TestHoldingCheck_ChannelRules(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	refuse := ptr(false)
	if err := holdingInsertRow(t, ctx, tx, holdingRow{employer: w.km, employee: w.kmEmployee, channel: "nfc", verdict: "reject",
		sunValid: refuse, ctr: ptr(int32(5)), siteTenant: &w.kf, siteTag: &w.kfLoose}); err != nil {
		t.Errorf("the refusal of a KF plaque with no wall was not written: %v", err)
	}
	if err := holdingInsertRow(t, ctx, tx, holdingRow{employer: w.km, employee: w.kmEmployee, channel: "manual", verdict: "flag", kind: "in",
		siteTenant: &w.kf, siteVenue: &w.kfVenue}); err != nil {
		t.Errorf("a manual record for KF's venue was not written: %v", err)
	}
	holdingWantGate(t, holdingInsertRow(t, ctx, tx, holdingRow{employer: w.km, employee: w.kmEmployee, channel: "qr", verdict: "flag", kind: "in",
		siteTenant: &w.km, siteTag: &w.kfTag}), "a row naming its own tenant as the site (the gate)")

	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, `ALTER TABLE transactions DISABLE TRIGGER transactions_holding_site_gate`); err != nil {
			t.Fatal(err)
		}
		kmVenue := uuid.New()
		if _, err := sp.Exec(ctx, `INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'KM Plant')`, kmVenue, w.km); err != nil {
			t.Fatal(err)
		}
		kmTag := holdingNewTag(t, ctx, sp, w.km, &kmVenue, "active", 1)
		for _, c := range []struct {
			what, constraint string
			row              holdingRow
		}{
			{"its own tenant as the site", "transactions_site_is_another_tenant",
				holdingRow{channel: "qr", verdict: "flag", kind: "in", siteTenant: &w.km, siteTag: &kmTag}},
			{"no site tenant, C's location (7th round)", "transactions_site_needs_its_tenant",
				holdingRow{channel: "manual", verdict: "flag", kind: "in", siteVenue: &w.cVenue}},
			{"no site tenant, C's plaque", "transactions_site_needs_its_tenant",
				holdingRow{channel: "qr", verdict: "flag", kind: "in", siteTag: &w.cTag}},
			{"a tap with no site plaque", "transactions_site_by_channel",
				holdingRow{channel: "nfc", verdict: "reject", siteTenant: &w.kf, siteVenue: &w.kfVenue}},
			{"a manual record with a site plaque", "transactions_site_by_channel",
				holdingRow{channel: "manual", verdict: "flag", kind: "in", siteTenant: &w.kf, siteVenue: &w.kfVenue, siteTag: &w.kfTag}},
			{"a manual record with no site location", "transactions_site_by_channel",
				holdingRow{channel: "manual", verdict: "flag", kind: "in", siteTenant: &w.kf}},
			{"a site row with a local venue", "transactions_site_excludes_the_local_venue",
				holdingRow{channel: "qr", verdict: "flag", kind: "in", siteTenant: &w.kf, siteTag: &w.kfTag, localVenue: &kmVenue}},
			{"a site row with a local plaque", "transactions_site_excludes_the_local_venue",
				holdingRow{channel: "qr", verdict: "flag", kind: "in", siteTenant: &w.kf, siteTag: &w.kfTag, localTag: &kmTag}},
		} {
			c.row.employer, c.row.employee = w.km, w.kmEmployee
			opWantConstraint(t, holdingInsertRow(t, ctx, sp, c.row), c.constraint, c.what)
		}
		for _, c := range []struct {
			what string
			row  holdingRow
		}{
			{"KF as the site, C's plaque", holdingRow{channel: "qr", verdict: "flag", kind: "in", siteTenant: &w.kf, siteTag: &w.cTag}},
			{"KF as the site, C's venue", holdingRow{channel: "manual", verdict: "flag", kind: "in", siteTenant: &w.kf, siteVenue: &w.cVenue}},
		} {
			c.row.employer, c.row.employee = w.km, w.kmEmployee
			opWant(t, holdingInsertRow(t, ctx, sp, c.row), sqlstateForeignKeyViolation, c.what)
		}
	})
}

// --------------------------------------------- (b) K-33, both directions, with H2 --

// holdingOutsiderProbes are (b)'s questions, each answered "nothing" by a correct filter: what
// an outsider's context or an outsider's key gets out of the three functions and the gate. They
// run on the real holding_peer and on each mutant (TestHoldingFilter_MutantsM1M2M3AreRedInFunctionsAndTriggers). Each
// returns "" when nothing leaked, else what leaked.
func holdingOutsiderProbes(t *testing.T, ctx context.Context, tx pgx.Tx, w holdingWorld) (functions, gate []string) {
	t.Helper()
	leak := func(cond bool, what string) string {
		if cond {
			return what
		}
		return ""
	}
	before := map[string]int64{w.kfTag: opInt(t, ctx, tx, `SELECT last_ctr FROM tags WHERE uid = $1`, w.kfTag),
		w.cTag: opInt(t, ctx, tx, `SELECT last_ctr FROM tags WHERE uid = $1`, w.cTag)}
	for _, p := range []struct {
		who, whose string
		ctx        uuid.UUID
		uid        string
		digest     string
	}{
		{"C (H2)", "KF's plaque", w.c, w.kfTag, w.kfDigest},
		{"D (H2, C's peer)", "KF's plaque", w.d, w.kfTag, w.kfDigest},
		{"KM", "C's plaque", w.km, w.cTag, w.kfDigest},
		{"no context", "KF's plaque", uuid.Nil, w.kfTag, w.kfDigest},
		{"KM", "an unknown uid", w.km, "00112233445566", w.kfDigest},
	} {
		functions = append(functions,
			leak(holdingSiteRows(t, ctx, tx, p.ctx, p.uid) != 0, p.who+" read "+p.whose+" through holding_tap_site"),
			leak(holdingLogoRows(t, ctx, tx, p.ctx, p.uid, p.digest) != 0, p.who+" read "+p.whose+"'s logo through holding_logo"))
		out, status, last := holdingAdvanceIn(t, ctx, tx, p.ctx, p.uid, 150)
		functions = append(functions,
			leak(out != "not_peer" || status != nil, fmt.Sprintf("%s advancing %s got (%s, %s), want (not_peer, NULL)", p.who, p.whose, out, holdingStr(status))))
		if want, ok := before[p.uid]; ok {
			functions = append(functions, leak(last != want, fmt.Sprintf("%s moved %s's counter %d -> %d", p.who, p.whose, want, last)))
		}
	}
	// The gate: KM names C's venue on a manual record and C's plaque on a QR; C names KF's.
	gate = append(gate,
		leak(holdingInsertRow(t, ctx, tx, holdingRow{employer: w.km, employee: w.kmEmployee, channel: "manual", verdict: "flag", kind: "in",
			siteTenant: &w.c, siteVenue: &w.cVenue}) == nil, "KM's manual record named C's venue"),
		leak(holdingInsertRow(t, ctx, tx, holdingRow{employer: w.km, employee: w.kmEmployee, channel: "qr", verdict: "flag", kind: "in",
			siteTenant: &w.c, siteTag: &w.cTag}) == nil, "KM's QR row named C's plaque"),
		leak(holdingInsertRow(t, ctx, tx, holdingRow{employer: w.c, employee: w.cEmployee, channel: "qr", verdict: "flag", kind: "in",
			siteTenant: &w.kf, siteTag: &w.kfTag}) == nil, "C's QR row named KF's plaque"))
	return holdingNonEmpty(functions), holdingNonEmpty(gate)
}

func holdingNonEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// TestHoldingFunctions_ReturnNothingForAnOutsiderOrAnOutsiderKey is K-33's two directions (plan
// §5 API-1b (b)), C a member of H2: C's context and D's (C's own peer in H2) get nothing of a
// KF plaque; KM's context gets nothing of C's plaque; an unknown uid and no tenant context are
// the same answer -- 0 rows from holding_tap_site and holding_logo, ('not_peer', NULL) from
// holding_advance_tag_counter, and neither plaque's counter moves. CONTROL: KM's context reads KF's
// plaque (1 row), its logo (1 row) and advances it.
func TestHoldingFunctions_ReturnNothingForAnOutsiderOrAnOutsiderKey(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	functions, _ := holdingOutsiderProbes(t, ctx, tx, w)
	for _, f := range functions {
		t.Error(f)
	}
	if n := holdingSiteRows(t, ctx, tx, w.km, w.kfTag); n != 1 {
		t.Errorf("CONTROL: KM reads KF's plaque through holding_tap_site: %d rows, want 1", n)
	}
	if n := holdingLogoRows(t, ctx, tx, w.km, w.kfTag, w.kfDigest); n != 1 {
		t.Errorf("CONTROL: KM reads KF's logo: %d rows, want 1", n)
	}
	if out, _, last := holdingAdvanceIn(t, ctx, tx, w.km, w.kfTag, 101); out != "advanced" || last != 101 {
		t.Errorf("CONTROL: KM advancing KF's plaque: %s, last_ctr %d; want advanced, 101", out, last)
	}
}

// TestHoldingManual_PeerManagerCannotNameAnOutsiderLocation (plan §5 API-1b (b)): KM's manual
// record naming C's venue is refused at the gate, and NO row anywhere names C as a site. (The
// projection's half -- "C's site_punches is 0" -- is API-1c's: site_punches is born there; the
// gate is what keeps the row from existing at all, so there is nothing for a projection to
// carry.) CONTROL: KM's manual record for KF's venue is written.
func TestHoldingManual_PeerManagerCannotNameAnOutsiderLocation(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	holdingWantGate(t, holdingInsertRow(t, ctx, tx, holdingRow{employer: w.km, employee: w.kmEmployee, channel: "manual", verdict: "flag", kind: "in",
		siteTenant: &w.c, siteVenue: &w.cVenue}), "KM's manual record at C's venue")
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM transactions WHERE site_tenant_id = $1`, w.c); n != 0 {
		t.Errorf("%d rows name C as a site", n)
	}
	if err := holdingInsertRow(t, ctx, tx, holdingRow{employer: w.km, employee: w.kmEmployee, channel: "manual", verdict: "flag", kind: "in",
		siteTenant: &w.kf, siteVenue: &w.kfVenue}); err != nil {
		t.Errorf("CONTROL: KM's manual record at KF's venue: %v", err)
	}
}

// holdingPeerMutants are K-33's three wrong filters (ADR 0027 §12.2), written as holding_peer
// bodies -- the ONE predicate, so a mutant there reaches every function and both gates:
//
//	M1  only the caller (a) is a member of some holding
//	M2  only the key's tenant (b) is a member of some holding
//	M3  both are members of SOME holding -- the holding_id equality forgotten
//
// Each answers its question for every deciding mode and NULL for 'lock'.
var holdingPeerMutants = map[string]string{
	"M1": `EXISTS (SELECT 1 FROM public.holding_members m WHERE m.member_tenant_id = a AND m.left_at IS NULL)`,
	"M2": `EXISTS (SELECT 1 FROM public.holding_members m WHERE m.member_tenant_id = b AND m.left_at IS NULL)`,
	"M3": `EXISTS (SELECT 1 FROM public.holding_members m WHERE m.member_tenant_id = a AND m.left_at IS NULL)
	       AND EXISTS (SELECT 1 FROM public.holding_members m WHERE m.member_tenant_id = b AND m.left_at IS NULL)`,
}

// TestHoldingFilter_MutantsM1M2M3AreRedInFunctionsAndTriggers is plan §5 API-1b (b)'s mutant
// arm: with the real holding_peer every outsider probe (holdingOutsiderProbes) finds nothing;
// with each of M1, M2 and M3 put in its place -- in a savepoint, by CREATE OR REPLACE, the owner
// and the grants kept -- the probes of the FUNCTIONS find a leak AND the probes of the GATE find
// a leak. C is a member of H2: without it M3 would answer false for C and stay green (7th round).
// (The second trigger -- holding_feed, the AFTER one -- is API-1c's; it is born asking
// holding_peer too, and this test will reach it through its probes then.)
func TestHoldingFilter_MutantsM1M2M3AreRedInFunctionsAndTriggers(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	functions, gate := holdingOutsiderProbes(t, ctx, tx, w)
	if len(functions)+len(gate) != 0 {
		t.Fatalf("the real holding_peer leaks: %v %v", functions, gate)
	}
	for _, name := range []string{"M1", "M2", "M3"} {
		t.Run(name, func(t *testing.T) {
			holdingInSavepoint(t, tx, func(sp pgx.Tx) {
				body := fmt.Sprintf(`CREATE OR REPLACE FUNCTION public.holding_peer(a uuid, b uuid, mod text)
					RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY INVOKER SET search_path = pg_catalog, pg_temp AS $$
					BEGIN
					    IF mod = 'lock' THEN RETURN NULL; END IF;
					    IF a IS NULL OR b IS NULL OR a = b THEN RETURN false; END IF;
					    RETURN %s;
					END $$`, holdingPeerMutants[name])
				if _, err := sp.Exec(ctx, body); err != nil {
					t.Fatalf("install %s: %v", name, err)
				}
				functions, gate := holdingOutsiderProbes(t, ctx, sp, w)
				if len(functions) == 0 {
					t.Errorf("%s in holding_peer: the functions' probes stayed green -- K-33's test does not catch it", name)
				}
				if len(gate) == 0 {
					t.Errorf("%s in holding_peer: the gate's probes stayed green -- K-33's test does not catch it", name)
				}
				t.Logf("%s: functions leaked %d, gate leaked %d", name, len(functions), len(gate))
			})
		})
	}
}

// ------------------------------------------------- (l) the advance keeps replay --

// TestHoldingAdvance_KeepsReplayProtection is plan §5 API-1b (l) and ADR 0027 §2's four outcomes,
// in KM's context on KF's plaque at counter 100: 101 advances (last_ctr 101, status active); 101
// again is a replay and 50 is a replay, the counter staying at 101; a lost plaque is
// peer_inactive with its status and its counter unmoved; and a plaque that turns lost before the
// advance is peer_inactive with the status of NOW. Outside the holding -- C's plaque -- not_peer,
// unmoved.
func TestHoldingAdvance_KeepsReplayProtection(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	for _, c := range []struct {
		uid        string
		ctr        int32
		outcome    string
		status     string
		wantLast   int64
		whatBefore string
	}{
		{w.kfTag, 101, "advanced", "active", 101, ""},
		{w.kfTag, 101, "replay", "active", 101, "an equal counter"},
		{w.kfTag, 50, "replay", "active", 101, "a lower counter"},
		{w.kfLost, 500, "peer_inactive", "lost", 100, "a lost plaque"},
		{w.cTag, 500, "not_peer", "", 100, "an outsider's plaque"},
	} {
		out, status, last := holdingAdvanceIn(t, ctx, tx, w.km, c.uid, c.ctr)
		if out != c.outcome || holdingStr(status) != c.status || last != c.wantLast {
			t.Errorf("%s at %d: (%s, %s), last_ctr %d; want (%s, %s), %d", c.whatBefore, c.ctr, out, holdingStr(status), last, c.outcome, c.status, c.wantLast)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE tags SET status = 'retired' WHERE uid = $1`, w.kfTag); err != nil {
		t.Fatal(err)
	}
	if out, status, last := holdingAdvanceIn(t, ctx, tx, w.km, w.kfTag, 200); out != "peer_inactive" || holdingStr(status) != "retired" || last != 101 {
		t.Errorf("a plaque retired before the advance: (%s, %s), %d; want (peer_inactive, retired), 101", out, holdingStr(status), last)
	}
}

// TestHoldingWindow_AdvanceAfterLeftIsNotPeer is K-29's strict advance (ADR 0027 §6): KM left
// H 10 seconds ago -- inside the 60-second window the gate tolerates -- and its advance on KF's
// plaque is already not_peer, the counter unmoved. The window is the WRITE's, never the advance's.
func TestHoldingWindow_AdvanceAfterLeftIsNotPeer(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "10 seconds")
	if out, status, last := holdingAdvanceIn(t, ctx, tx, w.km, w.kfTag, 101); out != "not_peer" || status != nil || last != 100 {
		t.Errorf("KM 10 s after leaving: (%s, %s), last_ctr %d; want (not_peer, NULL), 100", out, holdingStr(status), last)
	}
}

// TestHoldingReads_AfterLeftReturnNothing (ADR 0027 §6: the reads are strict): 10 seconds after
// KM left, holding_tap_site and holding_logo answer KM nothing of KF's plaque -- and KF, the
// member that stayed, nothing of a KM plaque.
func TestHoldingReads_AfterLeftReturnNothing(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "10 seconds")
	if n := holdingSiteRows(t, ctx, tx, w.km, w.kfTag); n != 0 {
		t.Errorf("holding_tap_site after left_at: %d rows", n)
	}
	if n := holdingLogoRows(t, ctx, tx, w.km, w.kfTag, w.kfDigest); n != 0 {
		t.Errorf("holding_logo after left_at: %d rows", n)
	}
	kmVenue := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'KM Plant')`, kmVenue, w.km); err != nil {
		t.Fatal(err)
	}
	if n := holdingSiteRows(t, ctx, tx, w.kf, holdingNewTag(t, ctx, tx, w.km, &kmVenue, "active", 1)); n != 0 {
		t.Errorf("KF reading a KM plaque after KM left: %d rows", n)
	}
}

// ------------------------------------------------- (g) the gate's channel x time --

// TestHoldingWindow_GateArms is the gate's K-29 model (ADR 0027 §6) row by row, KM having left H
// 10 seconds ago (inside the window) and, for the second column, 61 seconds ago (past it):
//
//	row                                                         10 s      61 s
//	NFC, sun_valid, ctr <= the plaque's last_ctr (advanced)     written   refused   (arm i)
//	NFC refusal, sun_valid false (replay / peer_inactive)       written   refused   (arm ii)
//	QR refusal (a deactivated employee's attempt)               written   refused   (arm ii)
//	NFC refusal, sun_valid true, ctr <= last_ctr (deactivated)  written   refused   (arm ii)
//	NFC ok, sun_valid, ctr > last_ctr (not advanced)            refused   refused
//	NFC flag, sun_valid false (not advanced)                    refused   refused
//	NFC refusal, sun_valid true, ctr > last_ctr (raw writer)    refused   refused
//	QR flag                                                     refused   refused   (strict)
//	manual                                                      refused   refused   (strict)
//
// and with KM a live member every row is written. Named rows of ADR 0027's list it carries:
// HoldingWindow_AdvancedBeforeLeftWrittenAfterIsRecorded, ..._RefusalsSurviveTheLeftRace,
// ..._QRRefusalSurvivesTheLeftRace, ..._UnadvancedNFCRowAfterLeftIsRefused,
// ..._ManualAfterLeftIsRefused -- see the subtests' names.
func TestHoldingWindow_GateArms(t *testing.T) {
	type rowCase struct {
		name     string
		row      func(w holdingWorld) holdingRow
		inWindow bool // written 10 s after left_at
	}
	nfc := func(verdict, kind string, sun bool, ctr int32) func(w holdingWorld) holdingRow {
		return func(w holdingWorld) holdingRow {
			return holdingRow{channel: "nfc", verdict: verdict, kind: kind, sunValid: ptr(sun), ctr: ptr(ctr), siteTenant: &w.kf, siteVenue: &w.kfVenue, siteTag: &w.kfTag}
		}
	}
	cases := []rowCase{
		{"AdvancedBeforeLeftWrittenAfterIsRecorded", nfc("ok", "in", true, 100), true},
		{"AdvancedBeforeLeftWrittenAfterIsRecorded/flag", nfc("flag", "in", true, 99), true},
		{"RefusalsSurviveTheLeftRace/replay", nfc("reject", "", false, 100), true},
		{"RefusalsSurviveTheLeftRace/peer_inactive", func(w holdingWorld) holdingRow {
			return holdingRow{channel: "nfc", verdict: "reject", sunValid: ptr(false), ctr: ptr(int32(700)), siteTenant: &w.kf, siteVenue: &w.kfVenue, siteTag: &w.kfLost}
		}, true},
		{"RefusalsSurviveTheLeftRace/deactivated_advanced", nfc("reject", "", true, 100), true},
		{"QRRefusalSurvivesTheLeftRace", func(w holdingWorld) holdingRow {
			return holdingRow{channel: "qr", verdict: "reject", sunValid: ptr(false), siteTenant: &w.kf, siteVenue: &w.kfVenue, siteTag: &w.kfTag}
		}, true},
		{"UnadvancedNFCRowAfterLeftIsRefused/ok", nfc("ok", "in", true, 101), false},
		{"UnadvancedNFCRowAfterLeftIsRefused/flag", nfc("flag", "in", false, 100), false},
		{"UnadvancedNFCRowAfterLeftIsRefused/raw_reject", nfc("reject", "", true, 101), false},
		{"QRFlagAfterLeftIsRefused", func(w holdingWorld) holdingRow {
			return holdingRow{channel: "qr", verdict: "flag", kind: "in", sunValid: ptr(false), siteTenant: &w.kf, siteVenue: &w.kfVenue, siteTag: &w.kfTag}
		}, false},
		{"ManualAfterLeftIsRefused", func(w holdingWorld) holdingRow {
			return holdingRow{channel: "manual", verdict: "flag", kind: "in", siteTenant: &w.kf, siteVenue: &w.kfVenue}
		}, false},
	}
	ctx, tx := opTx(t)
	for _, left := range []string{"", "10 seconds", "61 seconds"} {
		w := holdingWorldTx(t, ctx, tx, left)
		for _, c := range cases {
			t.Run(fmt.Sprintf("left=%q/%s", left, c.name), func(t *testing.T) {
				r := c.row(w)
				r.employer, r.employee = w.km, w.kmEmployee
				err := holdingInsertRow(t, ctx, tx, r)
				want := left == "" || (left == "10 seconds" && c.inWindow)
				switch {
				case want && err != nil:
					t.Errorf("refused, want written: %v", err)
				case !want:
					holdingWantGate(t, err, c.name)
				}
			})
		}
	}
}

// TestHoldingWindow_NoRowPassesBeforeAndSkipsAfter is the gate's FOR SHARE ordering (ADR 0027
// §6; plan §5 API-1b (g)), on COMMITTED memberships over three connections:
//
//  1. AN UNCOMMITTED REMOVE: a remover writes KM's left_at (what op_remove_holding_member does --
//     FOR NO KEY UPDATE on the row) and does not commit; KM's QR row (strict) then WAITS in the
//     gate's 'lock' (pg_locks); the remover commits; the row is REFUSED -- the gate re-read the
//     committed left_at. MUTANT (the 'lock' call made a no-op, in the writer's own transaction):
//     the row does not wait and is WRITTEN for a membership that has ended.
//  2. THE OTHER ORDER: the gate's share lock taken first (the row inserted, not committed) makes
//     the remover WAIT -- a remove cannot slip between the gate's decision and the row's commit.
//  3. THE KEY WAIT: an advanced NFC row, accepted by the gate, whose foreign-key check then waits
//     on the plaque (another transaction holds it FOR UPDATE, as the advance does); a remover
//     tries to end KM's membership meanwhile and WAITS too -- the gate's share lock is held to the
//     row's commit -- so the membership cannot end between the gate's decision and the commit;
//     the row is written, and only then the remove goes through. (Measured while writing this:
//     the first version of this subtest let the remover commit during the key wait and hung on
//     exactly that lock.)
//
// "...and the AFTER trigger does not skip KF's side" is API-1c's half: holding_feed is born there
// and does not re-ask the membership for a transactions row (ADR 0027 §6).
func TestHoldingWindow_NoRowPassesBeforeAndSkipsAfter(t *testing.T) {
	f := holdingLiveFixture(t)
	w := holdingWorldLive(t, f)
	appRow := func(tx pgx.Tx, r holdingRow) error {
		r.employer, r.employee = w.km, w.kmEmployee
		if _, err := tx.Exec(f.ctx, `SELECT set_config('app.tenant_id', $1, true)`, w.km.String()); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `
			INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, site_tenant_id, site_location_id, site_tag_uid)
			VALUES ($1, $2, now(), $3, $4, $5, $6, $7, $8, $9, $10)`,
			r.employer, r.employee, r.verdict, r.channel, nilIfEmpty(r.kind), r.sunValid, r.ctr, r.siteTenant, r.siteVenue, r.siteTag)
		return err
	}
	qr := holdingRow{channel: "qr", verdict: "flag", kind: "in", sunValid: ptr(false), siteTenant: &w.kf, siteVenue: &w.kfVenue, siteTag: &w.kfTag}
	remover, writer := f.conn(t), f.conn(t)
	removeKM := `UPDATE holding_members SET left_at = clock_timestamp() WHERE holding_id = $1 AND member_tenant_id = $2 AND left_at IS NULL`

	run := func(t *testing.T, mutant bool) (waited bool, err error) {
		t.Helper()
		// a fresh membership for KM each run (the previous run ended it)
		if _, err := f.owner.Exec(f.ctx, `DELETE FROM holding_members WHERE member_tenant_id = $1`, w.km); err != nil {
			t.Fatal(err)
		}
		f.member(t, w.h, w.km, "10 minutes", "")
		rt := holdingBegin(t, f.ctx, remover, "")
		if _, err := rt.Exec(f.ctx, removeKM, w.h, w.km); err != nil {
			t.Fatal(err)
		}
		wt := holdingBegin(t, f.ctx, writer, "")
		if mutant {
			// The mutant: the gate's 'lock' answers without locking (in the writer's transaction only).
			if _, err := wt.Exec(f.ctx, fmt.Sprintf(`CREATE OR REPLACE FUNCTION public.holding_peer(a uuid, b uuid, mod text)
				RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY INVOKER SET search_path = pg_catalog, pg_temp AS $$
				BEGIN IF mod = 'lock' THEN RETURN NULL; END IF; RETURN %s; END $$`, holdingRealPeerExpr)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := wt.Exec(f.ctx, `SET LOCAL SESSION AUTHORIZATION tappa_app`); err != nil {
			t.Fatal(err)
		}
		errs := make(chan error, 1)
		go func() { errs <- appRow(wt, qr) }()
		waited = holdingWaitsWithin(t, f, writer, 2*time.Second)
		if err := rt.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		err = <-errs
		_ = wt.Rollback(f.ctx)
		return waited, err
	}
	t.Run("an uncommitted remove", func(t *testing.T) {
		waited, err := run(t, false)
		if !waited {
			t.Error("the row did not wait for the uncommitted remove -- the gate's FOR SHARE is not taken")
		}
		holdingWantGate(t, err, "a strict row against a remove that committed while it waited")
	})
	t.Run("MUTANT: no lock", func(t *testing.T) {
		waited, err := run(t, true)
		if waited || err != nil {
			t.Errorf("the mutant: waited=%v err=%v; want no wait and the row written (the ordering's measurement)", waited, err)
		}
	})
	t.Run("the remover waits for the gate", func(t *testing.T) {
		if _, err := f.owner.Exec(f.ctx, `DELETE FROM holding_members WHERE member_tenant_id = $1`, w.km); err != nil {
			t.Fatal(err)
		}
		f.member(t, w.h, w.km, "10 minutes", "")
		wt := holdingBegin(t, f.ctx, writer, "tappa_app")
		if err := appRow(wt, qr); err != nil {
			t.Fatalf("the row while KM is a member: %v", err)
		}
		rt := holdingBegin(t, f.ctx, remover, "")
		done := make(chan error, 1)
		go func() {
			_, err := rt.Exec(f.ctx, removeKM, w.h, w.km)
			done <- err
		}()
		if !holdingWaitsWithin(t, f, remover, 2*time.Second) {
			t.Error("the remover did not wait for the gate's share lock")
		}
		_ = wt.Rollback(f.ctx)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		_ = rt.Rollback(f.ctx)
	})
	t.Run("the key wait", func(t *testing.T) {
		if _, err := f.owner.Exec(f.ctx, `DELETE FROM holding_members WHERE member_tenant_id = $1`, w.km); err != nil {
			t.Fatal(err)
		}
		f.member(t, w.h, w.km, "10 minutes", "")
		holder := f.conn(t)
		ht := holdingBegin(t, f.ctx, holder, "")
		if _, err := ht.Exec(f.ctx, `SELECT 1 FROM tags WHERE uid = $1 FOR UPDATE`, w.kfTag); err != nil {
			t.Fatal(err)
		}
		wt := holdingBegin(t, f.ctx, writer, "tappa_app")
		var id uuid.UUID
		errs := make(chan error, 1)
		go func() {
			if _, err := wt.Exec(f.ctx, `SELECT set_config('app.tenant_id', $1, true)`, w.km.String()); err != nil {
				errs <- err
				return
			}
			errs <- wt.QueryRow(f.ctx, `
				INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, site_tenant_id, site_location_id, site_tag_uid)
				VALUES ($1, $2, now(), 'ok', 'nfc', 'in', true, 100, $3, $4, $5) RETURNING id`,
				w.km, w.kmEmployee, w.kf, w.kfVenue, w.kfTag).Scan(&id)
		}()
		if !holdingWaitsWithin(t, f, writer, 2*time.Second) {
			t.Error("the row's key check did not wait on the plaque")
		}
		rt := holdingBegin(t, f.ctx, remover, "")
		removed := make(chan error, 1)
		go func() {
			_, err := rt.Exec(f.ctx, removeKM, w.h, w.km)
			removed <- err
		}()
		if !holdingWaitsWithin(t, f, remover, 2*time.Second) {
			t.Error("the remover did not wait while an accepted row's key check was pending -- the membership could end between the gate and the commit")
		}
		_ = ht.Rollback(f.ctx)
		if err := <-errs; err != nil {
			t.Fatalf("the row accepted by the gate failed after its key wait: %v", err)
		}
		if err := wt.Commit(f.ctx); err != nil {
			t.Fatalf("commit the accepted row: %v", err)
		}
		if err := <-removed; err != nil {
			t.Fatal(err)
		}
		if err := rt.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		if n := opInt(t, f.ctx, f.owner, `SELECT count(*) FROM transactions WHERE id = $1 AND site_tenant_id = $2`, id, w.kf); n != 1 {
			t.Errorf("the accepted row is not there after the race: %d", n)
		}
	})
}

// holdingRealPeerExpr is holding_peer's strict / strict_stmt / tolerant question for the
// ordering mutant: the same holding, both live by statement_timestamp() (the strict_stmt row of
// ADR 0027 §2 -- all a QR row asks).
const holdingRealPeerExpr = `EXISTS (SELECT 1 FROM public.holding_members ma JOIN public.holding_members mb ON mb.holding_id = ma.holding_id
	WHERE ma.member_tenant_id = a AND mb.member_tenant_id = b
	  AND (ma.left_at IS NULL OR ma.left_at > statement_timestamp()) AND (mb.left_at IS NULL OR mb.left_at > statement_timestamp()))`

// holdingWaitsWithin reports whether c comes to wait on a lock within d.
func holdingWaitsWithin(t *testing.T, f *holdingLive, c *pgx.Conn, d time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if opInt(t, f.ctx, f.owner, `SELECT count(*) FROM pg_locks WHERE NOT granted AND pid = $1`, c.PgConn().PID()) > 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// ------------------------------------------------- (c) one counter, two statements --

// TestHoldingAdvance_ConcurrentSameCounterExactlyOneWins is CLAUDE.md §4.4 and §8 for the second
// copy of the replay statement (plan §5 API-1b (c); ADR 0027 §12.5): one KF plaque, one counter,
// N racers on N connections released together -- HALF the single-tenant statement
// (store.AdvanceTagCounter's SQL, as tappa_app in KF's context), HALF holding_advance_tag_counter
// (as tappa_app in KM's context) -- and exactly ONE advances. Five rounds, a fresh counter each.
// tags_counter_monotonic does not stop an equal counter (it refuses only a smaller one), so the
// single winner is the two statements' FOR UPDATE + strict `<` alone.
func TestHoldingAdvance_ConcurrentSameCounterExactlyOneWins(t *testing.T) {
	f := holdingLiveFixture(t)
	w := holdingWorldLive(t, f)
	const racers = 20
	conns := make([]*pgx.Conn, racers)
	for i := range conns {
		conns[i] = f.conn(t)
	}
	const single = `WITH prev AS (SELECT p.uid AS old_uid, p.last_ctr AS old_ctr FROM tags p WHERE p.uid = $1 AND p.tenant_id = $2 FOR UPDATE)
		UPDATE tags t SET last_ctr = $3 FROM prev WHERE t.uid = prev.old_uid AND t.tenant_id = $2 AND prev.old_ctr < $3 RETURNING t.uid`
	for round := 0; round < 5; round++ {
		ctr := int32(200 + round)
		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			wins    int
			byPath  = map[string]int{}
			start   = make(chan struct{})
			failure []error
		)
		for i, c := range conns {
			wg.Add(1)
			go func(i int, c *pgx.Conn) {
				defer wg.Done()
				tx, err := c.BeginTx(f.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
				if err != nil {
					mu.Lock()
					failure = append(failure, err)
					mu.Unlock()
					return
				}
				defer func() { _ = tx.Rollback(context.Background()) }()
				path, tenant := "single", w.kf
				if i%2 == 1 {
					path, tenant = "holding", w.km
				}
				_, err = tx.Exec(f.ctx, `SET LOCAL SESSION AUTHORIZATION tappa_app`)
				if err == nil {
					_, err = tx.Exec(f.ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String())
				}
				<-start
				won := false
				if err == nil {
					if path == "single" {
						var uid string
						switch e := tx.QueryRow(f.ctx, single, w.kfTag, w.kf, ctr).Scan(&uid); {
						case e == nil:
							won = true
						case errors.Is(e, pgx.ErrNoRows):
						default:
							err = e
						}
					} else {
						var outcome string
						var status *string
						if err = tx.QueryRow(f.ctx, `SELECT outcome, tag_status FROM public.holding_advance_tag_counter($1, $2)`, w.kfTag, ctr).Scan(&outcome, &status); err == nil {
							won = outcome == "advanced"
							if outcome != "advanced" && outcome != "replay" {
								err = fmt.Errorf("holding path answered %s", outcome)
							}
						}
					}
				}
				if err == nil {
					err = tx.Commit(f.ctx)
				}
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					failure = append(failure, err)
					return
				}
				if won {
					wins++
					byPath[path]++
				}
			}(i, c)
		}
		close(start)
		wg.Wait()
		for _, e := range failure {
			t.Errorf("round %d: %v", round, e)
		}
		if wins != 1 {
			t.Errorf("round %d: %d of %d racers advanced KF's plaque to %d (single %d, holding %d), want exactly 1", round, wins, racers, ctr, byPath["single"], byPath["holding"])
		}
		if got := opInt(t, f.ctx, f.owner, `SELECT last_ctr FROM tags WHERE uid = $1`, w.kfTag); got != int64(ctr) {
			t.Errorf("round %d: last_ctr %d, want %d", round, got, ctr)
		}
	}
}

// ---------------------------------------------------------- the definer's grants --

// TestHoldingDefiner_EveryGrantIsUsed re-earns ADR 0027 §3's matrix rows 00043 grants (the
// matrix is a ceiling -- "gereksiz çıkan sütun düşer"): each (table, verb, column) beyond 00041's
// holding_members row is revoked ALONE, in a savepoint, and the three functions and the gate are
// driven through the paths that read it -- and at least one of them is refused 42501. A column
// none of them needed would stay green here, and this test is red on it.
func TestHoldingDefiner_EveryGrantIsUsed(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	drive := func(sp pgx.Tx) []error {
		var errs []error
		for _, c := range []struct {
			sql  string
			args []any
		}{
			{`SELECT * FROM public.holding_tap_site($1)`, []any{w.kfTag}},
			{`SELECT * FROM public.holding_logo($1, $2)`, []any{w.kfTag, w.kfDigest}},
			{`SELECT * FROM public.holding_advance_tag_counter($1, 101)`, []any{w.kfTag}},
		} {
			errs = append(errs, holdingAppIn(t, ctx, sp, w.km, func(q pgx.Tx) error {
				rows, err := q.Query(ctx, c.sql, c.args...)
				if err != nil {
					return err
				}
				for rows.Next() {
				}
				rows.Close()
				return rows.Err()
			}))
		}
		errs = append(errs, holdingInsertRow(t, ctx, sp, holdingRow{employer: w.km, employee: w.kmEmployee, channel: "nfc", verdict: "ok", kind: "in",
			sunValid: ptr(true), ctr: ptr(int32(100)), siteTenant: &w.kf, siteVenue: &w.kfVenue, siteTag: &w.kfTag}))
		return errs
	}
	for _, e := range drive(tx) {
		if e != nil {
			t.Fatalf("CONTROL: with every grant, a call failed: %v", e)
		}
	}
	checked := 0
	for key, cols := range holdingDefinerMatrix {
		table, verb, _ := strings.Cut(key, ":")
		if table == "holding_members" {
			continue // 00041's row; TestHoldingDefiner_LockAnchorIsTheOnlyMembershipUpdate re-measures it
		}
		for _, col := range strings.Split(cols, ",") {
			checked++
			holdingInSavepoint(t, tx, func(sp pgx.Tx) {
				if _, err := sp.Exec(ctx, fmt.Sprintf(`REVOKE %s (%s) ON %s FROM tappa_holdingdefiner`, verb, col, table)); err != nil {
					t.Fatal(err)
				}
				refused := false
				for _, e := range drive(sp) {
					if code, _ := opCode(e); code == sqlstateInsufficientPrivi {
						refused = true
					}
				}
				if !refused {
					t.Errorf("%s (%s) ON %s: revoked, and every function and the gate still ran -- a grant nothing reads (drop it from 00043 and the matrix)", verb, col, table)
				}
			})
		}
	}
	if checked < 25 {
		t.Fatalf("anti-vacuity: %d columns checked", checked)
	}
}

// --------------------------------------------------------------- the migrations --

const (
	op00043File = "00043_record_a_tap_on_a_peers_plaque.sql"
	op00044File = "00044_validate_the_site_constraints.sql"
)

// holdingSiteConstraints are the six constraints 00043 adds NOT VALID and 00044 validates.
var holdingSiteConstraints = []string{
	"transactions_site_by_channel", "transactions_site_excludes_the_local_venue", "transactions_site_is_another_tenant",
	"transactions_site_location_fk", "transactions_site_needs_its_tenant", "transactions_site_tag_fk",
}

// holdingAlterOf returns 00043's ALTER TABLE transactions statement and its CREATE INDEX, from
// the file goose runs.
func holdingAlterOf(t *testing.T) (alter, index string) {
	t.Helper()
	up, _ := opMigrationSections(t, op00043File)
	a := regexp.MustCompile(`(?s)ALTER TABLE transactions\s+ADD COLUMN site_tenant_id.*?;`).FindString(up)
	i := regexp.MustCompile(`(?s)CREATE INDEX transactions_site_idx.*?;`).FindString(up)
	if a == "" || i == "" {
		t.Fatalf("00043's ALTER (%d bytes) or its index (%d bytes) not found", len(a), len(i))
	}
	return a, i
}

// TestHoldingMigration_AlterWithinTwoSeconds is plan §5 API-1b (k) -- ADR 0027 §4: "eşik 2 sn,
// aşılırsa CHECK/FK NOT VALID + ayrı VALIDATE". The measurement (ADR 0027's "API-1b uygulama
// notu"; a throwaway PostgreSQL 17.10, production-shaped rows): validated in the ALTER, 0.68-0.90
// s at 688 000 rows and 2.06-3.46 s at 2 000 000 -- past the threshold -- so 00043 takes the NOT
// VALID path and 00044 validates. Pinned here:
//   - every ADD CONSTRAINT of 00043's ALTER is NOT VALID, and 00044 validates exactly those six
//     -- and at HEAD all six are validated;
//   - BY MEASUREMENT, not text: 00043's ALTER, run against a scratch copy of transactions' shape
//     holding rows (its keys against scratch copies of locations and tags), SCANS THE TABLE ZERO
//     TIMES (pg_stat_xact_user_tables.seq_scan), the index build once, and 00044's VALIDATEs do
//     scan it; the same ALTER with NOT VALID removed (the CONTROL -- the direct path) scans it;
//   - and the measured time of 00043's ALTER plus its index on the scratch rows is under 2 s.
func TestHoldingMigration_AlterWithinTwoSeconds(t *testing.T) {
	ctx, tx := opTx(t)
	alter, index := holdingAlterOf(t)
	var named []string
	for _, piece := range strings.Split(alter, "ADD CONSTRAINT ")[1:] {
		name := strings.Fields(piece)[0]
		named = append(named, name)
		if !strings.Contains(piece, "NOT VALID") {
			t.Errorf("00043 adds %s without NOT VALID -- it would scan transactions under ACCESS EXCLUSIVE", name)
		}
	}
	slices.Sort(named)
	if strings.Join(named, ",") != strings.Join(holdingSiteConstraints, ",") {
		t.Errorf("00043 adds %v, want %v", named, holdingSiteConstraints)
	}
	up44, _ := opMigrationSections(t, op00044File)
	var validated []string
	for _, m := range regexp.MustCompile(`VALIDATE CONSTRAINT (\w+);`).FindAllStringSubmatch(up44, -1) {
		validated = append(validated, m[1])
	}
	slices.Sort(validated)
	if strings.Join(validated, ",") != strings.Join(holdingSiteConstraints, ",") {
		t.Errorf("00044 validates %v, want %v", validated, holdingSiteConstraints)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_constraint WHERE conrelid = 'public.transactions'::regclass AND conname = ANY($1) AND convalidated`, holdingSiteConstraints); n != 6 {
		t.Errorf("at HEAD %d of the six site constraints are validated, want 6", n)
	}

	// The scratch: transactions' columns (no site columns, no constraints), locations' and tags'
	// keys, and rows -- created in this rolled-back transaction, so no other test waits on them.
	const rows = 50000
	for _, s := range []string{
		`CREATE TABLE zz_api1b_tx (LIKE transactions INCLUDING DEFAULTS)`,
		`ALTER TABLE zz_api1b_tx DROP COLUMN site_tenant_id, DROP COLUMN site_location_id, DROP COLUMN site_tag_uid`,
		`CREATE TABLE zz_api1b_loc (id uuid, tenant_id uuid, UNIQUE (id, tenant_id))`,
		`CREATE TABLE zz_api1b_tags (uid character(14), tenant_id uuid, UNIQUE (uid, tenant_id))`,
		fmt.Sprintf(`INSERT INTO zz_api1b_tx (tenant_id, occurred_at, verdict, channel, note)
		             SELECT gen_random_uuid(), now(), 'reject', 'nfc', 'the tap signature could not be verified' FROM generate_series(1, %d)`, rows),
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	scratch := func(sql string) string {
		r := strings.NewReplacer("ALTER TABLE transactions", "ALTER TABLE zz_api1b_tx", "ON transactions", "ON zz_api1b_tx",
			"REFERENCES locations", "REFERENCES zz_api1b_loc", "REFERENCES tags", "REFERENCES zz_api1b_tags",
			"transactions_site_idx", "zz_api1b_site_idx", "ADD CONSTRAINT transactions_", "ADD CONSTRAINT zz_api1b_")
		return r.Replace(sql)
	}
	scans := func(sp pgx.Tx, sql string) (int64, time.Duration) {
		t.Helper()
		before := opInt(t, ctx, sp, `SELECT coalesce((SELECT seq_scan FROM pg_stat_xact_user_tables WHERE relid = 'zz_api1b_tx'::regclass), 0)`)
		start := time.Now()
		if _, err := sp.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		took := time.Since(start)
		return opInt(t, ctx, sp, `SELECT coalesce((SELECT seq_scan FROM pg_stat_xact_user_tables WHERE relid = 'zz_api1b_tx'::regclass), 0)`) - before, took
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		n, took := scans(sp, scratch(alter))
		if n != 0 {
			t.Errorf("00043's ALTER scanned the table %d times under its ACCESS EXCLUSIVE, want 0", n)
		}
		ni, tookIndex := scans(sp, scratch(index))
		if ni != 1 {
			t.Errorf("00043's index build scanned the table %d times, want 1 (the one scan the file makes under the lock)", ni)
		}
		if took+tookIndex > 2*time.Second {
			t.Errorf("00043's ALTER and index took %v on %d rows -- past ADR 0027 §4's 2 s", took+tookIndex, rows)
		}
		t.Logf("00043's ALTER %v (0 scans) + index %v on %d rows", took, tookIndex, rows)
		var validateScans int64
		for _, c := range holdingSiteConstraints {
			v, _ := scans(sp, `ALTER TABLE zz_api1b_tx VALIDATE CONSTRAINT `+strings.Replace(c, "transactions_", "zz_api1b_", 1))
			validateScans += v
		}
		if validateScans == 0 {
			t.Error("CONTROL: 00044's VALIDATEs scanned nothing -- the measure cannot tell a scan")
		}
	})
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		direct := strings.ReplaceAll(scratch(alter), " NOT VALID", "")
		if n, took := scans(sp, direct); n == 0 {
			t.Errorf("CONTROL: the direct path (NOT VALID removed) scanned nothing (%v) -- the measure is blind", took)
		}
	})
}

// TestHolding00043And00044_DownGiveBackAndUpTakesItAgain: 00044's Down gives back 00043's state
// (the six constraints by the same text, NOT VALID); 00043's Down then takes the gate, the three
// functions, the definer's 00043 grants and the site columns away -- and REFUSES, changing nothing,
// while a row names a site (a recorded tap's "where" is not erased -- §4.3, §4.6). The Ups take
// HEAD's state again. Site rows this database already holds (the tap tests commit them) are set
// aside in the savepoint -- the append-only trigger disabled inside it -- so the Down can be
// driven; the rollback puts them back.
func TestHolding00043And00044_DownGiveBackAndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up43, down43 := opMigrationSections(t, op00043File)
	up44, down44 := opMigrationSections(t, op00044File)
	type surface struct {
		defs, validated, columns, functions, trigger, reach string
	}
	read := func(q pgx.Tx) surface {
		var s surface
		for dst, sql := range map[*string]string{
			&s.defs: `SELECT coalesce(string_agg(conname || ' ' || pg_get_constraintdef(oid), ' | ' ORDER BY conname), '')
			            FROM pg_constraint WHERE conrelid = 'public.transactions'::regclass AND conname LIKE 'transactions_site%'`,
			&s.validated: `SELECT coalesce(string_agg(conname, ',' ORDER BY conname), '') FROM pg_constraint
			                WHERE conrelid = 'public.transactions'::regclass AND conname LIKE 'transactions_site%' AND convalidated`,
			&s.columns: `SELECT coalesce(string_agg(attname, ',' ORDER BY attnum), '') FROM pg_attribute
			              WHERE attrelid = 'public.transactions'::regclass AND attname LIKE 'site\_%' AND NOT attisdropped`,
			&s.functions: `SELECT coalesce(string_agg(proname, ',' ORDER BY proname), '') FROM pg_proc WHERE proowner = 'tappa_holdingdefiner'::regrole`,
			&s.trigger:   `SELECT coalesce(string_agg(tgname, ',' ORDER BY tgname), '') FROM pg_trigger WHERE tgrelid = 'public.transactions'::regclass AND NOT tgisinternal`,
		} {
			if err := q.QueryRow(ctx, sql).Scan(dst); err != nil {
				t.Fatal(err)
			}
		}
		for _, tb := range []string{"tags", "locations", "tenants", "tenant_branding"} {
			s.reach += tb + ":" + opColumns(t, ctx, q, "tappa_holdingdefiner", tb, "SELECT") + "/" + opColumns(t, ctx, q, "tappa_holdingdefiner", tb, "UPDATE") + " "
		}
		return s
	}
	head := read(tx)
	if head.validated != strings.Join(holdingSiteConstraints, ",") || head.columns != "site_tenant_id,site_location_id,site_tag_uid" ||
		!strings.Contains(head.trigger, "transactions_holding_site_gate") {
		t.Fatalf("PREMISE at HEAD: %+v", head)
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		holdingLockForDowns(t, ctx, sp)
		holdingSetAsideSiteRows(t, ctx, sp)
		opRunSection(t, ctx, sp, down44, "00044 Down")
		at43 := read(sp)
		if at43.validated != "" || strings.ReplaceAll(at43.defs, " NOT VALID", "") != head.defs {
			t.Errorf("after 00044's Down: validated %q (want none); definitions\n %s\nwant HEAD's, NOT VALID\n %s", at43.validated, at43.defs, head.defs)
		}
		if strings.Count(at43.defs, "NOT VALID") != 6 {
			t.Errorf("after 00044's Down: %d constraints NOT VALID, want 6", strings.Count(at43.defs, "NOT VALID"))
		}
		opRunSection(t, ctx, sp, down43, "00043 Down")
		at42 := read(sp)
		if at42.defs != "" || at42.columns != "" || at42.functions != "holding_peer,holding_timezone_lock" ||
			strings.Contains(at42.trigger, "holding") || strings.TrimSpace(at42.reach) != "tags:/ locations:/ tenants:/ tenant_branding:/" {
			t.Errorf("after 00043's Down: %+v", at42)
		}
		opRunSection(t, ctx, sp, up43, "00043 Up again")
		opRunSection(t, ctx, sp, up44, "00044 Up again")
		if again := read(sp); again != head {
			t.Errorf("after the Ups again:\n %+v\nwant HEAD's\n %+v", again, head)
		}
	})
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		w := holdingWorldTx(t, ctx, sp, "")
		if err := holdingInsertRow(t, ctx, sp, holdingRow{employer: w.km, employee: w.kmEmployee, channel: "manual", verdict: "flag", kind: "in",
			siteTenant: &w.kf, siteVenue: &w.kfVenue}); err != nil {
			t.Fatal(err)
		}
		holdingLockForDowns(t, ctx, sp)
		opRunSection(t, ctx, sp, down44, "00044 Down with a site row")
		err := holdingTryDown(t, ctx, sp, down43)
		opWant(t, err, sqlstatePrerequisiteState, "00043's Down while a row names a site")
		if s := read(sp); s.columns != head.columns || s.functions != head.functions {
			t.Errorf("the refused Down changed something: %+v", s)
		}
	})
}

// holdingTryDown runs a Down section's statements in a savepoint and returns the first error.
func holdingTryDown(t *testing.T, ctx context.Context, tx pgx.Tx, section string) error {
	t.Helper()
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Conn().PgConn().Exec(ctx, section).ReadAll(); err != nil {
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			t.Fatal(rbErr)
		}
		return err
	}
	return sp.Commit(ctx)
}

// holdingLockForDowns takes, inside the savepoint sp, ACCESS EXCLUSIVE on every table 00043's
// and 00044's Downs and Ups (and the API-1a Downs that follow them) lock -- in ONE statement,
// NOWAIT, retried until it is granted -- so the DDL after it never WAITS while holding a lock.
// Measured in the first full -race run: the Downs taking their locks one by one, against the
// other packages' taps writing transactions, tags and locations at the same time, ended in a
// deadlock (40P01 in TestHolding00041_PreconditionRefusesAWrongCluster): this test held
// transactions and waited for tags while a tap held tags and waited for transactions. A
// transaction that never waits cannot be in a wait cycle. The locks go with the savepoint.
func holdingLockForDowns(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		nested, err := sp.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = nested.Exec(ctx, `LOCK TABLE public.transactions, public.transaction_reviews, public.tags, public.locations,
		                                       public.tenants, public.tenant_branding IN ACCESS EXCLUSIVE MODE NOWAIT`)
		if err == nil {
			if err := nested.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			return
		}
		if rbErr := nested.Rollback(ctx); rbErr != nil {
			t.Fatal(rbErr)
		}
		if code, _ := opCode(err); code != "55P03" || time.Now().After(deadline) {
			t.Fatalf("lock the tables the Downs touch: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// holdingSetAsideSiteRows deletes, inside the savepoint sp, every transactions row that names a
// site and the reviews of those rows (both tables are append-only: their row triggers are
// disabled for the delete and enabled again) -- the rows the tap tests commit, which 00043's Down
// refuses to drop the columns under. The savepoint's rollback puts them back.
func holdingSetAsideSiteRows(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	for _, s := range []string{
		`ALTER TABLE transaction_reviews DISABLE TRIGGER transaction_reviews_no_mutation`,
		`ALTER TABLE transactions DISABLE TRIGGER transactions_no_mutation`,
		`DELETE FROM transaction_reviews r USING transactions x WHERE x.id = r.transaction_id AND x.site_tenant_id IS NOT NULL`,
		`DELETE FROM transactions WHERE site_tenant_id IS NOT NULL`,
		`ALTER TABLE transactions ENABLE TRIGGER transactions_no_mutation`,
		`ALTER TABLE transaction_reviews ENABLE TRIGGER transaction_reviews_no_mutation`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// holdingBelow43 takes the savepoint's database below 00043 -- 00044's Down, then 00043's -- for
// the API-1a tests that walk 00042 and 00041 back: 00041's precondition refuses a definer that
// reaches anything but its own holding_members row, 00043 gives it more, and 00043's functions
// are the definer's too. The site rows are set aside first (holdingSetAsideSiteRows).
func holdingBelow43(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	holdingLockForDowns(t, ctx, sp)
	holdingSetAsideSiteRows(t, ctx, sp)
	_, down44 := opMigrationSections(t, op00044File)
	_, down43 := opMigrationSections(t, op00043File)
	opRunSection(t, ctx, sp, down44, "00044 Down")
	opRunSection(t, ctx, sp, down43, "00043 Down")
}

// TestHoldingWindow_UnadvancedNFCRowAfterLeftIsRefused is K-29's "ilerletilmemiş NFC satırı"
// (ADR 0027 §6; plan §5 API-1b (g)): KM left H 10 seconds ago, and an NFC row the counter was
// NOT advanced for -- ok with a counter past the plaque's (101 > 100), flag with sun_valid false,
// or a refusal claiming sun_valid with a counter the plaque never reached (the raw writer arm
// (ii)'s last condition bounds) -- is refused at the gate. CONTROL: the advanced shape (sun_valid,
// 100 <= 100) is written. (The tap path never writes the first two shapes itself -- an NFC tap whose
// counter did not move is §5 row 2's reject; these are the rows a raw writer would try.)
func TestHoldingWindow_UnadvancedNFCRowAfterLeftIsRefused(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "10 seconds")
	row := func(verdict, kind string, sun bool, ctr int32) holdingRow {
		return holdingRow{employer: w.km, employee: w.kmEmployee, channel: "nfc", verdict: verdict, kind: kind, sunValid: ptr(sun), ctr: ptr(ctr),
			siteTenant: &w.kf, siteVenue: &w.kfVenue, siteTag: &w.kfTag}
	}
	for name, r := range map[string]holdingRow{
		"ok, a counter past the plaque's":           row("ok", "in", true, 101),
		"flag, sun_valid false":                     row("flag", "in", false, 100),
		"a refusal claiming sun_valid at a new ctr": row("reject", "", true, 101),
	} {
		holdingWantGate(t, holdingInsertRow(t, ctx, tx, r), name)
	}
	if err := holdingInsertRow(t, ctx, tx, row("ok", "in", true, 100)); err != nil {
		t.Errorf("CONTROL: the advanced shape 10 s after left_at: %v", err)
	}
}

// TestHoldingWindow_ManualAfterLeftIsRefused (ADR 0027 §6, §9: a manual record for the site is
// STRICT; plan §5 API-1b (g)): 10 seconds after KM left, KM's manual record for KF's venue is
// refused; while KM is a member the same record is written.
func TestHoldingWindow_ManualAfterLeftIsRefused(t *testing.T) {
	ctx, tx := opTx(t)
	for _, left := range []string{"", "10 seconds"} {
		w := holdingWorldTx(t, ctx, tx, left)
		err := holdingInsertRow(t, ctx, tx, holdingRow{employer: w.km, employee: w.kmEmployee, channel: "manual", verdict: "flag", kind: "in",
			siteTenant: &w.kf, siteVenue: &w.kfVenue})
		if left == "" {
			if err != nil {
				t.Errorf("CONTROL: a manual site record while KM is a member: %v", err)
			}
			continue
		}
		holdingWantGate(t, err, "a manual site record 10 s after left_at")
	}
}

// TestHoldingTapSite_LogoOnlyForAVIESVerifiedSite is ADR 0027 §2/§10's "logo özeti yalnız
// VIES-doğrulanmış tenant için": holding_tap_site gives KF's logo digest, type, box and name (the
// logo's alt) only while KF's VIES answer is true -- false and never asked (NULL) give all five
// NULL -- and the accent in every case.
func TestHoldingTapSite_LogoOnlyForAVIESVerifiedSite(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	for _, vies := range []any{true, false, nil} {
		if _, err := tx.Exec(ctx, `UPDATE tenants SET vat_verified = $2 WHERE id = $1`, w.kf, vies); err != nil {
			t.Fatal(err)
		}
		var (
			accent          *string
			sha, mime, name *string
			width, height   *int32
		)
		if err := holdingAppIn(t, ctx, tx, w.km, func(sp pgx.Tx) error {
			return sp.QueryRow(ctx, `SELECT accent, logo_sha256, logo_mime, logo_width, logo_height, business_name FROM public.holding_tap_site($1)`, w.kfTag).
				Scan(&accent, &sha, &mime, &width, &height, &name)
		}); err != nil {
			t.Fatalf("VIES %v: %v", vies, err)
		}
		logo := sha != nil && mime != nil && width != nil && height != nil && name != nil
		none := sha == nil && mime == nil && width == nil && height == nil && name == nil
		switch {
		case holdingStr(accent) != "1F5C41":
			t.Errorf("VIES %v: accent %q, want the stored one whatever the answer", vies, holdingStr(accent))
		case vies == true && (!logo || *sha != w.kfDigest):
			t.Errorf("VIES true: the logo is not given (digest %q)", holdingStr(sha))
		case vies != true && !none:
			t.Errorf("VIES %v: a logo field is given (digest %q, name %q)", vies, holdingStr(sha), holdingStr(name))
		}
	}
}
