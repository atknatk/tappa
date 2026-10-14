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

	"github.com/atknatk/tappa/internal/store"
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
	outcome, status, _, lastCtr = holdingAdvanceGapIn(t, ctx, tx, tenant, uid, ctr)
	return outcome, status, lastCtr
}

// holdingAdvanceGapIn is holdingAdvanceIn with the statement's ctr_gap too (migration 00045, the
// user's decision K-1i).
func holdingAdvanceGapIn(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID, uid string, ctr int32) (outcome string, status *string, gap *int32, lastCtr int64) {
	t.Helper()
	if err := holdingAppIn(t, ctx, tx, tenant, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT outcome, tag_status, ctr_gap FROM public.holding_advance_tag_counter($1, $2)`, uid, ctr).Scan(&outcome, &status, &gap)
	}); err != nil {
		t.Fatalf("holding_advance_tag_counter: %v", err)
	}
	return outcome, status, gap, opInt(t, ctx, tx, `SELECT coalesce((SELECT last_ctr FROM tags WHERE uid = $1), -1)`, uid)
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

	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
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
	// holding_site_locations (00047, API-1c): an outsider's context reads no venue of H's members,
	// KM's context no venue of H2's, and no context no venue at all.
	for _, p := range []struct {
		who       string
		ctx       uuid.UUID
		forbidden []uuid.UUID
	}{
		{"C (H2)", w.c, []uuid.UUID{w.kf, w.km}},
		{"D (H2, C's peer)", w.d, []uuid.UUID{w.kf, w.km}},
		{"KM", w.km, []uuid.UUID{w.c, w.d}},
		{"no context", uuid.Nil, nil},
	} {
		// Every page, not the first: under a wrong filter the function answers every venue of every
		// tenant in the database, 1000 a page by id, and the outsider's venue need not be on page one
		// (measured on the shared database: M1's leak was on a later page and the first-page probe
		// stayed green).
		var n int64
		if err := holdingAppIn(t, ctx, tx, p.ctx, func(sp pgx.Tx) error {
			var cursor *uuid.UUID
			for range 200 {
				rows, err := sp.Query(ctx, `SELECT site_tenant_id, location_id FROM public.holding_site_locations($1)`, cursor)
				if err != nil {
					return err
				}
				got := 0
				for rows.Next() {
					var site, loc uuid.UUID
					if err := rows.Scan(&site, &loc); err != nil {
						rows.Close()
						return err
					}
					got++
					cursor = &loc
					if p.forbidden == nil || slices.Contains(p.forbidden, site) {
						n++
					}
				}
				rows.Close()
				if err := rows.Err(); err != nil {
					return err
				}
				if got < 1000 {
					return nil
				}
			}
			return fmt.Errorf("holding_site_locations answered more than 200 pages")
		}); err != nil {
			t.Fatalf("holding_site_locations: %v", err)
		}
		functions = append(functions, leak(n != 0, fmt.Sprintf("%s read %d venue(s) it is no peer of through holding_site_locations", p.who, n)))
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

// holdingFeedProbes is K-33's question to holding_feed's projection (API-1c; plan §5 API-1c's
// devir: "M1, M2, M3 mutantlarının her biri holding_feed'de de kırmızı"). The transactions arm does
// not ask holding_peer (the gate's acceptance is the answer -- ADR 0027 §6), so the arm a filter
// reaches is the APPROVAL's: KM's employee's flagged row on KF's plaque is written while the two
// are peers; then KM leaves H and joins H2 (C's holding) and KM's manager approves the row. KM and
// KF are no longer peers -- the site is not told -- yet each wrong filter answers "peer" (M1: KM is
// a member of H2; M2: KF of H; M3: both of some holding) and writes KF an 'approved' row. All in a
// nested savepoint, rolled back. "" when nothing leaked.
func holdingFeedProbes(t *testing.T, ctx context.Context, tx pgx.Tx, w holdingWorld) []string {
	t.Helper()
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}()
	reviewer := holdingNewAdmin(t, ctx, sp, w.km)
	var id uuid.UUID
	if err := holdingAppIn(t, ctx, sp, w.km, func(q pgx.Tx) error {
		return q.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr,
		                            site_tenant_id, site_location_id, site_tag_uid)
		                        VALUES ($1, $2, now(), 'flag', 'nfc', 'in', true, 100, $3, $4, $5) RETURNING id`,
			w.km, w.kmEmployee, w.kf, w.kfVenue, w.kfTag).Scan(&id)
	}); err != nil {
		t.Fatalf("KM's flagged row on KF's plaque: %v", err)
	}
	for _, s := range []string{
		`UPDATE holding_members SET left_at = clock_timestamp() - interval '2 minutes' WHERE member_tenant_id = $1 AND holding_id = $2 AND left_at IS NULL`,
		`INSERT INTO holding_members (holding_id, member_tenant_id, joined_at) VALUES ($2, $1, clock_timestamp() - interval '1 minute')`,
	} {
		h := w.h
		if strings.Contains(s, "INSERT") {
			h = w.h2
		}
		if _, err := sp.Exec(ctx, s, w.km, h); err != nil {
			t.Fatalf("move KM to H2: %v", err)
		}
	}
	if err := holdingAppIn(t, ctx, sp, w.km, func(q pgx.Tx) error {
		_, err := q.Exec(ctx, `INSERT INTO transaction_reviews (tenant_id, transaction_id, reviewer_id, outcome) VALUES ($1, $2, $3, 'approved')`, w.km, id, reviewer)
		return err
	}); err != nil {
		return []string{"the approval after the move was not written (it must be: the employer's own record): " + err.Error()}
	}
	if n := opInt(t, ctx, sp, `SELECT count(*) FROM site_punches WHERE transaction_id = $1 AND outcome = 'approved'`, id); n != 0 {
		return []string{fmt.Sprintf("KF was told of an approval %d time(s) after KM left H for H2", n)}
	}
	if n := opInt(t, ctx, sp, `SELECT count(*) FROM site_punches WHERE transaction_id = $1 AND outcome = 'pending_review'`, id); n != 1 {
		return []string{fmt.Sprintf("PREMISE: the flagged row while KM was KF's peer reached KF %d time(s), want 1", n)}
	}
	return nil
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
	for _, c := range []struct {
		who         string
		ctx, venue  uuid.UUID
		owner, name string
	}{{"KM", w.km, w.kfVenue, "KF", "KF St Julians"}, {"D", w.d, w.cVenue, "C", "C Venue"}} {
		var n int64
		if err := holdingAppIn(t, ctx, tx, c.ctx, func(sp pgx.Tx) error {
			return sp.QueryRow(ctx, `SELECT count(*) FROM public.holding_site_locations(NULL) WHERE location_id = $1 AND location_name = $2`, c.venue, c.name).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("CONTROL: %s reads its peer %s's venue through holding_site_locations %d time(s), want 1", c.who, c.owner, n)
		}
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
// Since API-1c the probes reach holding_site_locations (00047) and the second trigger, holding_feed
// (00046), through its approval arm -- the one that asks holding_peer (holdingFeedProbes).
func TestHoldingFilter_MutantsM1M2M3AreRedInFunctionsAndTriggers(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	functions, gate := holdingOutsiderProbes(t, ctx, tx, w)
	feed := holdingFeedProbes(t, ctx, tx, w)
	if len(functions)+len(gate)+len(feed) != 0 {
		t.Fatalf("the real holding_peer leaks: %v %v %v", functions, gate, feed)
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
				feed := holdingFeedProbes(t, ctx, sp, w)
				if len(functions) == 0 {
					t.Errorf("%s in holding_peer: the functions' probes stayed green -- K-33's test does not catch it", name)
				}
				if !slices.ContainsFunc(functions, func(f string) bool { return strings.Contains(f, "holding_site_locations") }) {
					t.Errorf("%s in holding_peer: holding_site_locations' probes stayed green -- K-33's test does not catch it", name)
				}
				if len(gate) == 0 {
					t.Errorf("%s in holding_peer: the gate's probes stayed green -- K-33's test does not catch it", name)
				}
				if len(feed) == 0 {
					t.Errorf("%s in holding_peer: holding_feed's probe stayed green -- K-33's test does not catch it", name)
				}
				t.Logf("%s: functions leaked %d, gate leaked %d, feed leaked %d", name, len(functions), len(gate), len(feed))
			})
		})
	}
}

// ------------------------------------------------- (l) the advance keeps replay --

// TestHoldingAdvance_KeepsReplayProtection is plan §5 API-1b (l) and ADR 0027 §2's four outcomes,
// in KM's context on KF's plaque at counter 100: 101 advances (last_ctr 101, status active, ctr_gap
// 0); 101 again is a replay and 50 is a replay, the counter staying at 101; 150 advances with
// ctr_gap 48 (150 - 101 - 1: the OLD counter, read inside the statement -- K-1i); a lost plaque is
// peer_inactive with its status and its counter unmoved; and a plaque that turns lost before the
// advance is peer_inactive with the status of NOW. Outside the holding -- C's plaque -- not_peer,
// unmoved. ctr_gap is NULL on every outcome but advanced (ADR 0027 §2).
func TestHoldingAdvance_KeepsReplayProtection(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	gapText := func(g *int32) string {
		if g == nil {
			return "NULL"
		}
		return fmt.Sprint(*g)
	}
	for _, c := range []struct {
		uid        string
		ctr        int32
		outcome    string
		status     string
		gap        string
		wantLast   int64
		whatBefore string
	}{
		{w.kfTag, 101, "advanced", "active", "0", 101, "the next counter"},
		{w.kfTag, 101, "replay", "active", "NULL", 101, "an equal counter"},
		{w.kfTag, 50, "replay", "active", "NULL", 101, "a lower counter"},
		{w.kfTag, 150, "advanced", "active", "48", 150, "a jump"},
		{w.kfLost, 500, "peer_inactive", "lost", "NULL", 100, "a lost plaque"},
		{w.cTag, 500, "not_peer", "", "NULL", 100, "an outsider's plaque"},
	} {
		out, status, gap, last := holdingAdvanceGapIn(t, ctx, tx, w.km, c.uid, c.ctr)
		if out != c.outcome || holdingStr(status) != c.status || gapText(gap) != c.gap || last != c.wantLast {
			t.Errorf("%s at %d: (%s, %s, %s), last_ctr %d; want (%s, %s, %s), %d", c.whatBefore, c.ctr, out, holdingStr(status), gapText(gap), last,
				c.outcome, c.status, c.gap, c.wantLast)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE tags SET status = 'retired' WHERE uid = $1`, w.kfTag); err != nil {
		t.Fatal(err)
	}
	if out, status, gap, last := holdingAdvanceGapIn(t, ctx, tx, w.km, w.kfTag, 200); out != "peer_inactive" || holdingStr(status) != "retired" || gap != nil || last != 150 {
		t.Errorf("a plaque retired before the advance: (%s, %s, %s), %d; want (peer_inactive, retired, NULL), 150", out, holdingStr(status), gapText(gap), last)
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
//	QR ok, sun_valid true, ctr <= last_ctr (arm i's shape)      refused   refused   (strict)
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
		// Arm (i) is NFC's alone: a QR row shaped like an advanced tap -- sun_valid true, a counter
		// the plaque has reached -- is still strict. MUTANT (the third-eye review's, measured): arm
		// (i) without its channel test writes this row 10 s after left_at.
		{"QRAdvancedShapeAfterLeftIsRefused", func(w holdingWorld) holdingRow {
			return holdingRow{channel: "qr", verdict: "ok", kind: "in", sunValid: ptr(true), ctr: ptr(int32(100)), siteTenant: &w.kf, siteVenue: &w.kfVenue, siteTag: &w.kfTag}
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
// "...AND THE AFTER TRIGGER DOES NOT SKIP KF's SIDE" (API-1c, taken over from API-1b; ADR 0027
// §6: holding_feed's transactions arm does not re-ask the membership -- a row the gate accepted is
// a peer's):
//
//  4. AN UNCOMMITTED REMOVE, A TOLERANT ROW: an advanced NFC row (sun_valid, the counter reached)
//     waits in the gate for the remove, the remove commits, the gate's tolerant arm accepts it --
//     and KF's projection HAS the row, in the same transaction. MUTANT (holding_feed's transactions
//     arm made to ask 'strict_stmt' again, in the writer's own transaction): the row is written at
//     KM and NOT at KF -- the "BEFORE kabul etti, AFTER KF tarafını atladı" row.
//  5. THE KEY WAIT (3. above): the accepted row, committed after its key wait, is in KF's
//     projection too.
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
	// 4. The tolerant row through an uncommitted remove, and KF's side of it.
	tolerant := func(t *testing.T, feedMutant bool) (atKM, atKF int64) {
		t.Helper()
		if _, err := f.owner.Exec(f.ctx, `DELETE FROM holding_members WHERE member_tenant_id = $1`, w.km); err != nil {
			t.Fatal(err)
		}
		f.member(t, w.h, w.km, "10 minutes", "")
		rt := holdingBegin(t, f.ctx, remover, "")
		if _, err := rt.Exec(f.ctx, removeKM, w.h, w.km); err != nil {
			t.Fatal(err)
		}
		wt := holdingBegin(t, f.ctx, writer, "")
		if feedMutant {
			var body string
			if err := wt.QueryRow(f.ctx, `SELECT pg_get_functiondef('public.holding_feed()'::regprocedure)`).Scan(&body); err != nil {
				t.Fatal(err)
			}
			anchor := "        v_txid     := NEW.id;\n"
			if !strings.Contains(body, anchor) {
				t.Fatal("the mutant's anchor is not in holding_feed's body")
			}
			mutant := strings.Replace(body, anchor, anchor+
				"        IF public.holding_peer(NEW.tenant_id, NEW.site_tenant_id, 'strict_stmt') IS NOT TRUE THEN RETURN NULL; END IF;\n", 1)
			if _, err := wt.Exec(f.ctx, mutant); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := wt.Exec(f.ctx, `SET LOCAL SESSION AUTHORIZATION tappa_app`); err != nil {
			t.Fatal(err)
		}
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
			t.Error("the tolerant row did not wait for the uncommitted remove")
		}
		if err := rt.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-errs; err != nil {
			t.Fatalf("the tolerant row 0 s after a remove it waited for: %v -- arm (i) must write it", err)
		}
		if _, err := wt.Exec(f.ctx, `SET LOCAL SESSION AUTHORIZATION DEFAULT`); err != nil {
			t.Fatal(err)
		}
		atKM = opInt(t, f.ctx, wt, `SELECT count(*) FROM transactions WHERE id = $1`, id)
		atKF = opInt(t, f.ctx, wt, `SELECT count(*) FROM site_punches WHERE transaction_id = $1 AND tenant_id = $2`, id, w.kf)
		_ = wt.Rollback(f.ctx)
		return atKM, atKF
	}
	t.Run("an uncommitted remove, a tolerant row, and KF's side", func(t *testing.T) {
		if atKM, atKF := tolerant(t, false); atKM != 1 || atKF != 1 {
			t.Errorf("the row the gate accepted: %d at KM, %d at KF; want 1 and 1 -- AFTER must not skip KF's side", atKM, atKF)
		}
	})
	t.Run("MUTANT: holding_feed asks the membership again", func(t *testing.T) {
		if atKM, atKF := tolerant(t, true); atKM != 1 || atKF != 0 {
			t.Errorf("the mutant: %d at KM, %d at KF; want 1 and 0 (the measurement the subtest above stands on)", atKM, atKF)
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
		if n := opInt(t, f.ctx, f.owner, `SELECT count(*) FROM site_punches WHERE transaction_id = $1 AND tenant_id = $2`, id, w.kf); n != 1 {
			t.Errorf("the accepted row is not in KF's projection after the key wait: %d, want 1", n)
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
// N racers on N connections released together -- HALF the single-tenant statement (the store
// package's own store.AdvanceTagCounter, the query the tap path runs, as tappa_app in KF's
// context), HALF holding_advance_tag_counter (as tappa_app in KM's context) -- and exactly ONE
// advances. Five rounds, a fresh counter each. tags_counter_monotonic does not stop an equal
// counter (it refuses only a smaller one), so the single winner is the two statements' FOR UPDATE
// + strict `<` alone. MUTANT (measured, ADR 0027's API-1b note): FOR UPDATE taken out of
// internal/store's advanceTagCounter -> more than one winner. (The test once carried a copy of
// that SQL; a copy would have stayed green while the real query lost its lock.)
func TestHoldingAdvance_ConcurrentSameCounterExactlyOneWins(t *testing.T) {
	f := holdingLiveFixture(t)
	w := holdingWorldLive(t, f)
	const racers = 20
	conns := make([]*pgx.Conn, racers)
	for i := range conns {
		conns[i] = f.conn(t)
	}
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
						_, e := store.New(tx).AdvanceTagCounter(f.ctx, store.AdvanceTagCounterParams{Ctr: ctr, TenantID: w.kf, Uid: w.kfTag})
						switch {
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

// TestHoldingDefiner_EveryGrantIsUsed re-earns ADR 0027 §3's matrix rows 00043 and 00046 grant
// (the matrix is a ceiling -- "gereksiz çıkan sütun düşer"): each (table, verb, column) beyond
// 00041's holding_members row is revoked ALONE, in a savepoint, and the callable functions, the
// gate and the projection are driven through the paths that read it -- and at least one of them
// is refused 42501. A column none of them needed would stay green here, and this test is red on
// it. Since API-1c the drive also writes the two refusal shapes (each dedup target's ON CONFLICT
// needs SELECT on its own columns) and an approval (the only path that reads transactions).
func TestHoldingDefiner_EveryGrantIsUsed(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	reviewer := holdingNewAdmin(t, ctx, tx, w.km)
	drive := func(sp pgx.Tx) []error {
		var errs []error
		for _, c := range []struct {
			sql  string
			args []any
		}{
			{`SELECT * FROM public.holding_tap_site($1)`, []any{w.kfTag}},
			{`SELECT * FROM public.holding_logo($1, $2)`, []any{w.kfTag, w.kfDigest}},
			{`SELECT * FROM public.holding_advance_tag_counter($1, 101)`, []any{w.kfTag}},
			{`SELECT * FROM public.holding_site_locations(NULL)`, nil},
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
		for _, sun := range []bool{true, false} {
			errs = append(errs, holdingInsertRow(t, ctx, sp, holdingRow{employer: w.km, employee: w.kmEmployee, channel: "nfc", verdict: "reject",
				sunValid: ptr(sun), ctr: ptr(int32(100)), siteTenant: &w.kf, siteVenue: &w.kfVenue, siteTag: &w.kfTag}))
		}
		errs = append(errs, holdingAppIn(t, ctx, sp, w.km, func(q pgx.Tx) error {
			var id uuid.UUID
			if err := q.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr,
			                               site_tenant_id, site_location_id, site_tag_uid)
			                           VALUES ($1, $2, now(), 'flag', 'nfc', 'in', true, 100, $3, $4, $5) RETURNING id`,
				w.km, w.kmEmployee, w.kf, w.kfVenue, w.kfTag).Scan(&id); err != nil {
				return err
			}
			_, err := q.Exec(ctx, `INSERT INTO transaction_reviews (tenant_id, transaction_id, reviewer_id, outcome) VALUES ($1, $2, $3, 'approved')`,
				w.km, id, reviewer)
			return err
		}))
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
					t.Errorf("%s (%s) ON %s: revoked, and every function, the gate and the projection still ran -- a grant nothing reads (drop it from its migration and the matrix)", verb, col, table)
				}
			})
		}
	}
	if checked < 60 {
		t.Fatalf("anti-vacuity: %d columns checked", checked)
	}
}

// --------------------------------------------------------------- the migrations --

const (
	op00043File = "00043_record_a_tap_on_a_peers_plaque.sql"
	op00044File = "00044_validate_the_site_constraints.sql"
	op00045File = "00045_report_the_counter_gap_of_a_peers_plaque.sql"
	op00046File = "00046_project_a_holding_tap_to_its_site.sql"
	op00047File = "00047_list_a_peers_venues.sql"
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

// TestHolding00043And00044_DownGiveBackAndUpTakesItAgain: 00045's Down gives back 00043's
// two-column holding_advance_tag_counter with its owner and its one grant, and nothing else; 00044's
// Down gives back 00043's state
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
	up45, down45 := opMigrationSections(t, op00045File)
	type surface struct {
		defs, validated, columns, functions, trigger, reach, advance string
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
			&s.advance: `SELECT coalesce((SELECT pg_get_function_result(p.oid) || ' ' || coalesce(array_to_string(p.proacl, ','), '')
			                                FROM pg_proc p WHERE p.proname = 'holding_advance_tag_counter'), '')`,
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
	up46, _ := opMigrationSections(t, op00046File)
	up47, _ := opMigrationSections(t, op00047File)
	atHead := read(tx)
	if !strings.Contains(atHead.trigger, "transactions_holding_feed") || !strings.Contains(atHead.functions, "holding_site_locations") {
		t.Fatalf("PREMISE at HEAD (00047): %+v", atHead)
	}
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		// 00047's and 00046's Downs first (their own test is TestHolding00046And00047_DownGiveBackAndUpTakesItAgain);
		// what this test walks is 00045 down to 00043 and back, from the state they leave.
		holdingBelow46(t, ctx, sp)
		head := read(sp)
		if head.validated != strings.Join(holdingSiteConstraints, ",") || head.columns != "site_tenant_id,site_location_id,site_tag_uid" ||
			!strings.Contains(head.trigger, "transactions_holding_site_gate") ||
			!strings.HasPrefix(head.advance, "TABLE(outcome text, tag_status text, ctr_gap integer) ") {
			t.Fatalf("PREMISE at 00045: %+v", head)
		}
		opRunSection(t, ctx, sp, down45, "00045 Down")
		at44 := read(sp)
		want44 := head
		want44.advance = strings.Replace(head.advance, ", ctr_gap integer)", ")", 1)
		if at44 != want44 {
			t.Errorf("after 00045's Down:\n %+v\nwant 00044's (00043's two-column advance, the same grants)\n %+v", at44, want44)
		}
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
		opRunSection(t, ctx, sp, up45, "00045 Up again")
		if again := read(sp); again != head {
			t.Errorf("after the Ups again:\n %+v\nwant 00045's\n %+v", again, head)
		}
		opRunSection(t, ctx, sp, up46, "00046 Up again")
		opRunSection(t, ctx, sp, up47, "00047 Up again")
		if again := read(sp); again != atHead {
			t.Errorf("after 00046's and 00047's Ups again:\n %+v\nwant HEAD's\n %+v", again, atHead)
		}
	})
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		holdingBelow46(t, ctx, sp)
		head := read(sp)
		w := holdingWorldTx(t, ctx, sp, "")
		if err := holdingInsertRow(t, ctx, sp, holdingRow{employer: w.km, employee: w.kmEmployee, channel: "manual", verdict: "flag", kind: "in",
			siteTenant: &w.kf, siteVenue: &w.kfVenue}); err != nil {
			t.Fatal(err)
		}
		opRunSection(t, ctx, sp, down45, "00045 Down with a site row")
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

// holdingDownTables are the tables the Downs and Ups these tests walk take a STRONG lock on --
// 00045/00044/00043 (transactions and its review table, tags, locations, tenants and
// tenant_branding: ALTER TABLE, DROP/CREATE TRIGGER, keys added or dropped, the site rows set
// aside), 00042 (operator_audit_log, operator_read_tickets: kind CHECKs; holdingClearKinds' delete
// and trigger switch) and 00041 (tenants: the timezone trigger; holdings and holding_members: the
// tables and their keys to tenants). GRANT and REVOKE take no relation lock (measured), so the
// column grants of 00042/00043 are not why a table is here.
var holdingDownTables = []string{
	"transactions", "transaction_reviews", "tags", "locations", "tenants", "tenant_branding",
	"holding_members", "holdings", "operator_audit_log", "operator_read_tickets",
	// 00046 (API-1c): site_punches itself (its rows set aside, the table dropped); its Down's DROP
	// TABLE also takes the four tables its keys name -- transactions, locations, tags, tenants --
	// and its DROP TRIGGERs transactions and transaction_reviews, all already above.
	"site_punches",
}

// holdingLockForDowns takes, inside the savepoint sp, ACCESS EXCLUSIVE on every one of
// holdingDownTables (lockForDowns: never blocking) -- FIRST in every savepoint that walks a Down,
// because holdingLockedSavepoint takes it before the body runs, so no statement after it ever waits
// for a lock on one of those tables while holding one. That is the claim, and
// it is narrower than "this transaction never waits": it can still wait on an object outside the
// list (a catalog row, an advisory lock), none of which a tap or a membership write holds while
// waiting for these tables.
//
// MEASURED TWICE. The first full -race run: the Downs taking their locks one by one while other
// packages' taps wrote transactions, tags and locations ended in 40P01 in
// TestHolding00041_PreconditionRefusesAWrongCluster (this test held transactions and waited for
// tags; a tap held tags and waited for transactions). The API-1b review then measured the same
// class on the API-1a half: with six tables locked, a membership written concurrently (checkin's
// and handler's fixtures hold holding_members and wait for tenants on the key check, this test
// held tenants and waited for holding_members) deadlocked in 35 of 75 inserts; with the four
// tables added, 0 of 76. Re-measured (ADR 0027's API-1b note): ten runs of that test beside a
// membership-writing loop -- six tables, 90 deadlocks in 1 361 writes; ten tables, 0 in 2 324.
// The locks go with the savepoint, and holdingLockedSavepoint fails the test when the savepoint ends
// holding a strong lock on a table outside this list (strongTableLocksOutside) -- the list cannot
// fall behind the Downs silently (except for a table the savepoint dropped and left dropped: its
// lock is on an OID with no name left to read) -- or without every listed table held.
func holdingLockForDowns(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	lockForDowns(t, ctx, sp, holdingDownTables)
}

// lockForDowns takes ACCESS EXCLUSIVE on every one of tables (those that exist) inside tx, so a
// caller that walks Downs afterwards never waits for a lock on one of them while holding another.
// opAtVersion takes it too, with the tables its Downs name. Two ways, in turn, every 25 ms:
//
//  1. ALL AT ONCE, NOWAIT: one LOCK statement over the whole list, in a nested savepoint. It never
//     waits, so it is in no deadlock. The common case.
//  2. IN TURN, AFTER 40 MISSES OF 1. (about a second): the API-1b review's DÜŞÜK-B -- under steady
//     load some table of ten always has a holder, and 1. alone missed for the whole test context
//     (measured: two tests, 90 s each, then 55P03; a NOWAIT one-by-one variant that kept what it
//     took missed too: polling never gets in a table's queue). lockInTurn queues for each table,
//     holding the ones it took -- and the waits are BOUNDED so that this backend is never the
//     one a deadlock check finds a cycle through (see there).
//
// The deadline is 90 s, or the context's deadline less 10 s when that is sooner.
func lockForDowns(t *testing.T, ctx context.Context, tx pgx.Tx, tables []string) {
	t.Helper()
	var list []string
	for _, tb := range tables {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, tb).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			list = append(list, "public."+tb)
		}
	}
	if len(list) == 0 {
		return
	}
	deadline := time.Now().Add(90 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Add(-10*time.Second).Before(deadline) {
		deadline = d.Add(-10 * time.Second)
	}
	order := slices.Clone(list)
	for round := 0; ; round++ {
		if lockNowait(t, ctx, tx, list) {
			return
		}
		if round >= 40 {
			busy, ok := lockInTurn(t, ctx, tx, order)
			if ok {
				return
			}
			if i := slices.Index(order, busy); i > 0 {
				// The table whose wait ran out goes FIRST next time: its holder was most likely
				// waiting on a table this round had taken (a membership writer holds holding_members
				// and waits for tenants on the key check -- measured: with tenants taken first,
				// every round timed out on holding_members, 90 s). Taken first, the writer queues
				// behind it holding nothing.
				order = append([]string{busy}, slices.Delete(order, i, i+1)...)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("lock the tables the Downs touch: %d rounds, still busy", round+1)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// lockNowait runs LOCK ... NOWAIT over tables in a savepoint nested in tx: true and the locks
// kept, or false (55P03) and nothing kept. Any other error fails the test.
func lockNowait(t *testing.T, ctx context.Context, tx pgx.Tx, tables []string) bool {
	t.Helper()
	nested, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = nested.Exec(ctx, `LOCK TABLE `+strings.Join(tables, ", ")+` IN ACCESS EXCLUSIVE MODE NOWAIT`)
	if err == nil {
		if err := nested.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return true
	}
	if rbErr := nested.Rollback(ctx); rbErr != nil {
		t.Fatal(rbErr)
	}
	if code, _ := opCode(err); code != "55P03" {
		t.Fatalf("lock the tables the Downs touch: %v", err)
	}
	return false
}

// lockInTurn is lockForDowns' second way: the tables one at a time, in the order given, each a
// BLOCKING LOCK -- so it takes its place in the table's queue and new holders line up behind it --
// keeping the ones it took; true with all of them held, or false with none (the nested savepoint
// rolled back) and the table whose wait ran out ("" when the hold budget below ran out first).
//
// WHY IT DOES NOT HAND ANOTHER TRANSACTION A DEADLOCK (one exception, named below). Holding some
// tables while waiting for the next is how a cycle forms (a tap holds tags and waits for our
// transactions while we wait for its tags), and PostgreSQL breaks a cycle by failing the backend
// whose deadlock check finds it -- a check each waiter runs deadlock_timeout (D) after ITS wait
// began. Measured: the review's
// proposal, one blocking LOCK over the whole list under lock_timeout 5 s, made three backends of
// the load deadlock victims in one run. Here two bounds keep every check that could see a cycle
// through this backend after the cycle is gone:
//   - each wait is at most L = D/5 (lock_timeout, restored after the LOCK); a wait that times out
//     gives everything back. L < D, so this backend's own check never runs either;
//   - no wait STARTS later than H = D/2 after the first table was taken.
//
// A transaction waiting on a table we hold began waiting after we took it, so its check runs at
// least D after our first lock -- later than H + L, the end of our last possible wait. (Not
// covered by this arithmetic: a cycle of three, whose third member began waiting on the second
// independently of us. The probe -- ADR 0027's API-1b note -- did not produce one; it is named,
// not excluded.)
func lockInTurn(t *testing.T, ctx context.Context, tx pgx.Tx, tables []string) (busy string, ok bool) {
	t.Helper()
	var deadlockMs int64
	var prev string
	if err := tx.QueryRow(ctx, `SELECT setting::bigint, current_setting('lock_timeout') FROM pg_catalog.pg_settings WHERE name = 'deadlock_timeout'`).
		Scan(&deadlockMs, &prev); err != nil {
		t.Fatal(err)
	}
	wait, hold := deadlockMs/5, time.Duration(deadlockMs/2)*time.Millisecond
	held, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var first time.Time
	for _, tb := range tables {
		if !first.IsZero() && time.Since(first) > hold {
			if err := held.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			return "", false
		}
		one, err := held.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = one.Exec(ctx, fmt.Sprintf(`SET LOCAL lock_timeout = '%dms'`, wait))
		if err == nil {
			_, err = one.Exec(ctx, `LOCK TABLE `+tb+` IN ACCESS EXCLUSIVE MODE`)
		}
		if err == nil {
			_, err = one.Exec(ctx, `SELECT set_config('lock_timeout', $1, true)`, prev)
		}
		if err != nil {
			if rbErr := one.Rollback(ctx); rbErr != nil {
				t.Fatal(rbErr)
			}
			if rbErr := held.Rollback(ctx); rbErr != nil {
				t.Fatal(rbErr)
			}
			if code, _ := opCode(err); code != "55P03" {
				t.Fatalf("lock %s in turn: %v", tb, err)
			}
			return tb, false
		}
		if err := one.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if first.IsZero() {
			first = time.Now()
		}
	}
	if err := held.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return "", true
}

// strongTableLocksOutside returns the tables of public this backend holds a lock STRONGER than a
// writer's on (ShareUpdateExclusive and up) that are not in allowed -- a Down that locked a table
// lockForDowns did not take first, i.e. a list that drifted from the Downs it guards. The
// callers fail on a non-empty answer, so a new Down that touches one more table turns red here
// instead of deadlocking now and then in a full run. A test's own scratch tables (this suite names
// every one zz_*) are left out: created in the test's transaction, no other session sees them.
func strongTableLocksOutside(t *testing.T, ctx context.Context, q opQuerier, allowed []string) []string {
	t.Helper()
	if allowed == nil {
		allowed = []string{} // a nil slice is SQL NULL, and `<> ALL (NULL)` is never true: every lock would hide
	}
	rows, err := q.Query(ctx, `
		SELECT DISTINCT c.relname FROM pg_catalog.pg_locks l JOIN pg_catalog.pg_class c ON c.oid = l.relation
		 WHERE l.locktype = 'relation' AND l.granted AND l.pid = pg_backend_pid()
		   AND c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
		   AND l.mode IN ('ShareUpdateExclusiveLock', 'ShareLock', 'ShareRowExclusiveLock', 'ExclusiveLock', 'AccessExclusiveLock')
		   AND c.relname <> ALL ($1) AND c.relname NOT LIKE 'zz\_%'
		 ORDER BY 1`, allowed)
	if err != nil {
		t.Fatalf("read this backend's strong table locks: %v", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("read this backend's strong table locks: %v", err)
	}
	return out
}

// holdingSetAsideSiteRows deletes, inside the savepoint sp, every transactions row that names a
// site, the reviews of those rows and -- while it exists -- every site_punches row (the three
// tables are append-only: their row triggers are disabled for the delete and enabled again) --
// the rows the tap tests commit, which 00046's Down refuses to drop the projection under and
// 00043's to drop the columns under. The savepoint's rollback puts them back.
func holdingSetAsideSiteRows(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	if opInt(t, ctx, sp, `SELECT count(*) FROM pg_class WHERE oid = to_regclass('public.site_punches')`) == 1 {
		for _, s := range []string{
			`ALTER TABLE site_punches DISABLE TRIGGER site_punches_no_mutation`,
			`DELETE FROM site_punches`,
			`ALTER TABLE site_punches ENABLE TRIGGER site_punches_no_mutation`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
		}
	}
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

// holdingBelow43 takes the savepoint's database below 00043 -- 00047's Down, 00046's, 00045's,
// 00044's, then 00043's -- for the API-1a tests that walk 00042 and 00041 back: 00041's precondition refuses a
// definer that reaches anything but its own holding_members row, 00043 gives it more, and
// 00043's functions are the definer's too. The site rows are set aside first
// (holdingSetAsideSiteRows). The caller's savepoint is a holdingLockedSavepoint, which took
// holdingLockForDowns first; it is taken again here (a lock already held is granted at once), and
// called from holdingInSavepoint it would leave strong locks that savepoint refuses.
func holdingBelow43(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	holdingBelow46(t, ctx, sp)
	_, down45 := opMigrationSections(t, op00045File)
	_, down44 := opMigrationSections(t, op00044File)
	_, down43 := opMigrationSections(t, op00043File)
	opRunSection(t, ctx, sp, down45, "00045 Down")
	opRunSection(t, ctx, sp, down44, "00044 Down")
	opRunSection(t, ctx, sp, down43, "00043 Down")
}

// holdingBelow46 takes the savepoint's database below 00046 -- 00047's Down, then 00046's -- with
// the site rows set aside first (00046's Down refuses while site_punches holds a row). The caller's
// savepoint is a holdingLockedSavepoint; the lock is taken again here, as holdingBelow43 does.
func holdingBelow46(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	holdingLockForDowns(t, ctx, sp)
	holdingSetAsideSiteRows(t, ctx, sp)
	_, down47 := opMigrationSections(t, op00047File)
	_, down46 := opMigrationSections(t, op00046File)
	opRunSection(t, ctx, sp, down47, "00047 Down")
	opRunSection(t, ctx, sp, down46, "00046 Down")
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
