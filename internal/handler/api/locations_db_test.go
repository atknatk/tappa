package api

// locations_db_test.go -- PUT /v1/locations/{ref} (M11 API-5; ADR 0028 §9, §10, K-7, K-18, K-20,
// K-23, K-24) over the REAL chain, runner, checkpoint, venue domain and database, and -- for plan
// §2 item 10 and K-20 -- the REAL tap path behind it (internal/domain/checkin.Service.Record).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// putLoc PUTs body to /v1/locations/{ref} with key raw.
func (r *dbRig) putLoc(raw, ref, body string, hdr ...string) *httptest.ResponseRecorder {
	return r.do(http.MethodPut, "/v1/locations/"+ref, raw, body, hdr...)
}

// locOf decodes a location answer.
func locOf(t *testing.T, w *httptest.ResponseRecorder) locationBody {
	t.Helper()
	var b locationBody
	dec := json.NewDecoder(strings.NewReader(w.Body.String()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		t.Fatalf("status %d: the answer %q is not a location: %v", w.Code, w.Body.String(), err)
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type %q", w.Header().Get("Content-Type"))
	}
	return b
}

// venueRow is a locations row as the owner reads it.
type venueRow struct {
	id               uuid.UUID
	name             string
	active           bool
	ips              int
	hasGPS, hasShift bool
	wifi             string
	externalRef      string
}

// venues reads every row of tenant holding ref.
func (r *dbRig) venuesByRef(tenant uuid.UUID, ref string) []venueRow {
	r.t.Helper()
	rows, err := r.owner.Query(context.Background(), `SELECT id, name, active, cardinality(static_ips), gps_lat IS NOT NULL, shift_start IS NOT NULL, coalesce(wifi_ssid, ''), coalesce(external_ref, '')
	                                                  FROM locations WHERE tenant_id = $1 AND external_ref = $2`, tenant, ref)
	if err != nil {
		r.t.Fatal(err)
	}
	defer rows.Close()
	var out []venueRow
	for rows.Next() {
		var v venueRow
		if err := rows.Scan(&v.id, &v.name, &v.active, &v.ips, &v.hasGPS, &v.hasShift, &v.wifi, &v.externalRef); err != nil {
			r.t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		r.t.Fatal(err)
	}
	return out
}

// trailRow is one audit_log row of a venue.
type trailRow struct {
	action string
	actor  *uuid.UUID
	detail map[string]any
	raw    string
}

func (r *dbRig) venueTrail(tenant, venue uuid.UUID) []trailRow {
	r.t.Helper()
	rows, err := r.owner.Query(context.Background(), `SELECT action, actor_id, detail::text FROM audit_log
	                                                  WHERE tenant_id = $1 AND target = $2 ORDER BY at, id`, tenant, venue.String())
	if err != nil {
		r.t.Fatal(err)
	}
	defer rows.Close()
	var out []trailRow
	for rows.Next() {
		var tr trailRow
		if err := rows.Scan(&tr.action, &tr.actor, &tr.raw); err != nil {
			r.t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(tr.raw), &tr.detail); err != nil {
			r.t.Fatal(err)
		}
		out = append(out, tr)
	}
	if err := rows.Err(); err != nil {
		r.t.Fatal(err)
	}
	return out
}

// ledgerRows counts tenant's api_idempotency rows under idem.
func (r *dbRig) ledgerRows(tenant uuid.UUID, idem string) (n int64) {
	r.t.Helper()
	if err := r.owner.QueryRow(context.Background(), `SELECT count(*) FROM api_idempotency WHERE tenant_id = $1 AND idem_key = $2`, tenant, idem).Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

func newRef(prefix string) string {
	return prefix + "-" + strings.ReplaceAll(uuid.NewString()[:13], "-", "")
}

// TestLocationsPut_CreatesUpdatesAndAnswersUnchanged (ADR 0028 §9, K-8, K-20, K-23): a new ref is
// 201 with loc_<uuid>, the ref, the name, the business's zone and the label; the row carries NO
// evidence (static_ips '{}', no coordinate, no shift, no network name) and one location.created
// trail row whose actor_id is the KEY's id and whose detail says via "api", lost_proof_of_place
// true, with exactly the documented keys and nothing of the key; the same PUT again is 200 with
// the same id and NO trail row; the LABEL alone -- the same name with active:false, then true
// again -- is 200 each time, the row follows, and each leaves one location.updated with from_active
// and to_active; a rename + active:false is 200 and one location.updated with the before and after.
// K-19: the write noticed the key's tenant.
//
// MUTANTS (measured, API-5 report): the "unchanged" short-circuit removed -> a second trail row;
// the short-circuit comparing the name alone (API-5 review) -> the label stays true, red.
func TestLocationsPut_CreatesUpdatesAndAnswersUnchanged(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, keyID := r.key(biz, owner)
	ref := newRef("BR")

	w := r.putLoc(raw, ref, `{"name":"  St Julians  ","active":true,"timezone":"Europe/Malta"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("a new ref: %d %s, want 201", w.Code, w.Body.String())
	}
	got := locOf(t, w)
	rows := r.venuesByRef(biz, ref)
	if len(rows) != 1 {
		t.Fatalf("%d rows for the ref, want 1", len(rows))
	}
	v := rows[0]
	if got.ID != "loc_"+v.id.String() || got.ExternalRef != ref || got.Name != "St Julians" || got.Timezone != "Europe/Malta" || !got.Active || got.CreatedAt.IsZero() {
		t.Fatalf("the answer %+v does not describe the row %+v", got, v)
	}
	if v.name != "St Julians" || !v.active || v.ips != 0 || v.hasGPS || v.hasShift || v.wifi != "" {
		t.Fatalf("the row %+v: the API must write the name and the label and no evidence", v)
	}
	trail := r.venueTrail(biz, v.id)
	if len(trail) != 1 || trail[0].action != tenant.ActionLocationCreated || trail[0].actor == nil || *trail[0].actor != keyID {
		t.Fatalf("the trail after a create: %+v, want one location.created by the key %s", trail, keyID)
	}
	d := trail[0].detail
	wantKeys := map[string]bool{"via": true, "external_ref": true, "to_name": true, "to_active": true, "lost_proof_of_place": true}
	for k := range d {
		if !wantKeys[k] {
			t.Errorf("the created row's detail carries %q", k)
		}
	}
	if d["via"] != "api" || d["external_ref"] != ref || d["to_name"] != "St Julians" || d["to_active"] != true || d["lost_proof_of_place"] != true {
		t.Errorf("the created row's detail %s", trail[0].raw)
	}
	if strings.Contains(trail[0].raw, raw) || strings.Contains(trail[0].raw, raw[len("tt_live_"):]) || strings.Contains(strings.ToLower(trail[0].raw), "bearer") {
		t.Fatal("the trail row carries the key")
	}

	w = r.putLoc(raw, ref, `{"name":"St Julians","active":true}`)
	if w.Code != http.StatusOK || locOf(t, w).ID != got.ID {
		t.Fatalf("the same PUT again: %d %s, want 200 with the same id", w.Code, w.Body.String())
	}
	if n := len(r.venueTrail(biz, v.id)); n != 1 {
		t.Fatalf("an unchanged PUT left %d trail rows, want the create's 1", n)
	}

	// The LABEL alone, both ways (API-5 review): the same name with active:false, then true again.
	for i, label := range []bool{false, true} {
		w = r.putLoc(raw, ref, fmt.Sprintf(`{"name":"St Julians","active":%v}`, label))
		if w.Code != http.StatusOK {
			t.Fatalf("the label %v alone: %d %s", label, w.Code, w.Body.String())
		}
		if b := locOf(t, w); b.ID != got.ID || b.Active != label {
			t.Fatalf("the label %v alone: the answer %+v", label, b)
		}
		if rows := r.venuesByRef(biz, ref); len(rows) != 1 || rows[0].active != label || rows[0].name != "St Julians" {
			t.Fatalf("the label %v alone: the row %+v", label, rows)
		}
		trail = r.venueTrail(biz, v.id)
		if len(trail) != 2+i || trail[1+i].action != tenant.ActionLocationUpdated || *trail[1+i].actor != keyID {
			t.Fatalf("the label %v alone: the trail %+v, want one location.updated more", label, trail)
		}
		if d := trail[1+i].detail; d["from_active"] != !label || d["to_active"] != label || d["from_name"] != "St Julians" || d["to_name"] != "St Julians" || d["via"] != "api" {
			t.Errorf("the label %v alone: the detail %s", label, trail[1+i].raw)
		}
	}

	w = r.putLoc(raw, ref, `{"name":"St Julian's","active":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("a rename: %d %s", w.Code, w.Body.String())
	}
	if b := locOf(t, w); b.ID != got.ID || b.Name != "St Julian's" || b.Active {
		t.Fatalf("the rename's answer %+v", b)
	}
	trail = r.venueTrail(biz, v.id)
	if len(trail) != 4 || trail[3].action != tenant.ActionLocationUpdated || *trail[3].actor != keyID {
		t.Fatalf("the trail after a rename: %+v", trail)
	}
	if d := trail[3].detail; d["from_name"] != "St Julians" || d["to_name"] != "St Julian's" || d["from_active"] != true || d["to_active"] != false || d["via"] != "api" {
		t.Errorf("the update's detail %s", trail[3].raw)
	}
	if rows := r.venuesByRef(biz, ref); len(rows) != 1 || rows[0].name != "St Julian's" || rows[0].active {
		t.Fatalf("after the rename: %+v", rows)
	}
	if !containsTenant(r.work.got(), biz) {
		t.Error("K-19: the write did not notice the key's tenant")
	}
}

func containsTenant(ts []uuid.UUID, t uuid.UUID) bool {
	for _, x := range ts {
		if x == t {
			return true
		}
	}
	return false
}

// TestLocationsPut_APanelBoundVenueIsUpdatedNeverMatchedByName (K-18, plan §2 item 10): a panel
// venue with evidence and no ref is NOT found by its name -- a PUT of a new ref with the same name
// opens a SECOND venue (201); once the owner binds the panel venue to a ref (the panel's
// ChangeExternalRef), a PUT of that ref is 200 on THAT row, renames it, and leaves its address
// ranges, coordinate and shift exactly as the panel set them.
func TestLocationsPut_APanelBoundVenueIsUpdatedNeverMatchedByName(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	ctx := context.Background()
	panel, err := r.venues.SaveVenue(ctx, tenant.VenueCommand{TenantID: biz, ActorID: owner, Name: "Rusty Bar",
		StaticIPs: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/28")}, GPS: &tenant.GPS{Lat: 35.9, Lng: 14.5},
		Shift: &tenant.Shift{Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true}})
	if err != nil {
		t.Fatal(err)
	}
	byName := newRef("NAME")
	if w := r.putLoc(raw, byName, `{"name":"Rusty Bar","active":true}`); w.Code != http.StatusCreated {
		t.Fatalf("a new ref with the panel venue's name: %d %s, want 201 (K-18: no match by name)", w.Code, w.Body.String())
	} else if locOf(t, w).ID == "loc_"+panel.ID.String() {
		t.Fatal("the PUT matched the panel venue by its name")
	}
	bound := newRef("BOUND")
	if _, err := r.venues.ChangeExternalRef(ctx, tenant.ExternalRefCommand{TenantID: biz, SubjectID: panel.ID, ActorID: owner, ExternalRef: bound}); err != nil {
		t.Fatal(err)
	}
	w := r.putLoc(raw, bound, `{"name":"Rusty Bar Paceville","active":true}`)
	if w.Code != http.StatusOK || locOf(t, w).ID != "loc_"+panel.ID.String() {
		t.Fatalf("the bound ref: %d %s, want 200 on the panel venue", w.Code, w.Body.String())
	}
	rows := r.venuesByRef(biz, bound)
	if len(rows) != 1 || rows[0].id != panel.ID || rows[0].name != "Rusty Bar Paceville" || rows[0].ips != 1 || !rows[0].hasGPS || !rows[0].hasShift {
		t.Fatalf("the bound venue after the PUT: %+v -- renamed, evidence and shift untouched", rows)
	}
	trail := r.venueTrail(biz, panel.ID)
	if last := trail[len(trail)-1]; last.action != tenant.ActionLocationUpdated || last.detail["lost_proof_of_place"] != false {
		t.Fatalf("the PUT's trail row on a venue with evidence: %s", last.raw)
	}
}

// TestLocationsPut_DifferentTimezoneIs422 (K-7; ADR 0028 §9): a zone other than the business's --
// another zone, another spelling of the business's ("europe/malta") -- is 422 timezone_mismatch
// naming the business's zone, for a new ref (no row, no trail, no Idempotency-Key row) and for an
// existing one (unchanged); the business's own zone and no zone at all are accepted.
//
// MUTANT (measured, API-5 report): the comparison removed from PutVenueTx -> 201 for Europe/Rome.
func TestLocationsPut_DifferentTimezoneIs422(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	ref := newRef("TZ")
	for _, zone := range []string{"Europe/Rome", "europe/malta", "UTC", "Europe/Malta "} {
		idem := "tz-" + uuid.NewString()
		w := r.putLoc(raw, ref, fmt.Sprintf(`{"name":"Valletta","active":true,"timezone":%q}`, zone), "Idempotency-Key", idem)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("timezone %q on a new ref: %d %s, want 422", zone, w.Code, w.Body.String())
		}
		p := problemOf(t, w)
		if p.Code != CodeTimezoneMismatch || p.Details["businessTimezone"] != "Europe/Malta" || p.Details["field"] != "timezone" {
			t.Fatalf("timezone %q: %+v", zone, p)
		}
		if n := r.ledgerRows(biz, idem); n != 0 {
			t.Fatalf("a refused PUT left %d Idempotency-Key rows", n)
		}
	}
	if rows := r.venuesByRef(biz, ref); len(rows) != 0 {
		t.Fatalf("refused PUTs opened %d venues", len(rows))
	}
	if w := r.putLoc(raw, ref, `{"name":"Valletta","active":true}`); w.Code != http.StatusCreated {
		t.Fatalf("no zone: %d %s", w.Code, w.Body.String())
	}
	id := r.venuesByRef(biz, ref)[0].id
	if w := r.putLoc(raw, ref, `{"name":"Valletta Harbour","active":true,"timezone":"Europe/Rome"}`); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("another zone on an existing ref: %d", w.Code)
	}
	if rows := r.venuesByRef(biz, ref); rows[0].name != "Valletta" {
		t.Fatalf("a refused PUT renamed the venue: %+v", rows)
	}
	if w := r.putLoc(raw, ref, `{"name":"Valletta Harbour","active":true,"timezone":"Europe/Malta"}`); w.Code != http.StatusOK || locOf(t, w).Timezone != "Europe/Malta" {
		t.Fatalf("the business's zone: %d %s", w.Code, w.Body.String())
	}
	if n := len(r.venueTrail(biz, id)); n != 2 {
		t.Fatalf("%d trail rows, want create + update", n)
	}
}

// TestLocationsPut_RepeatOpensNoRow (ADR 0028 §9 "tekrar PUT yeni satır açmaz", K-24): concurrent PUTs
// of ONE new ref, without an Idempotency-Key, open ONE venue. A free-for-all burst rarely makes two
// PUTs meet at the INSERT (measured: a mutant without the retry passed 2 runs of 3), so the meeting
// is FORCED: a transaction on another connection holds an uncommitted row for the ref, and the test
// waits until every PUT is blocked on it inside the INSERT (pg_locks: a transactionid lock not
// granted) --
//
//	the holder commits     every PUT gets ON CONFLICT's no-row, locks the holder's venue and
//	                       answers 200 on it: no second row, no 201, no trail row (same name)
//	the holder rolls back  the PUTs race again; exactly ONE opens the venue (201), the others wait
//	                       on ITS index entry and answer 200 on it: one row, one location.created
//
// then a burst of 16 on a third ref for good measure, and a later PUT is 200 on the row. Under -race.
//
// MUTANTS (measured, API-5 report, in a throwaway container): the retry after ON CONFLICT DO NOTHING
// removed -> 503s (3 runs of 3); a plain INSERT -> the unique constraint refuses the second row and
// the PUTs behind it answer 503 -- the constraint is the guard, the lookup is not.
func TestLocationsPut_RepeatOpensNoRow(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	ctx := context.Background()
	watcher, err := pgx.Connect(ctx, os.Getenv("DATABASE_MIGRATE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watcher.Close(context.Background()) }()

	type answer struct {
		code int
		id   string
		body string
	}
	burst := func(ref string, n int, meanwhile func()) (created, ok int, ids map[string]bool) {
		answers := make(chan answer, n)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				w := r.putLoc(raw, ref, `{"name":"Sliema","active":true}`)
				a := answer{code: w.Code, body: w.Body.String()}
				if w.Code/100 == 2 {
					var b locationBody
					if json.Unmarshal(w.Body.Bytes(), &b) == nil {
						a.id = b.ID
					}
				}
				answers <- a
			}()
		}
		close(start)
		if meanwhile != nil {
			meanwhile()
		}
		wg.Wait()
		close(answers)
		ids = map[string]bool{}
		for a := range answers {
			switch a.code {
			case http.StatusCreated:
				created++
			case http.StatusOK:
				ok++
			default:
				t.Errorf("a concurrent PUT answered %d %s", a.code, a.body)
			}
			ids[a.id] = true
		}
		return created, ok, ids
	}
	// held opens the ref's row in an uncommitted transaction and returns it with its xid.
	held := func(ref string) (pgx.Tx, string) {
		holder, err := pgx.Connect(ctx, os.Getenv("DATABASE_MIGRATE_URL"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = holder.Close(context.Background()) })
		tx, err := holder.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO locations (tenant_id, name, external_ref) VALUES ($1, 'Sliema', $2)`, biz, ref); err != nil {
			t.Fatal(err)
		}
		var xid string
		if err := tx.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&xid); err != nil {
			t.Fatal(err)
		}
		return tx, xid
	}
	// blockedOn waits until n backends wait for xid's transaction lock.
	blockedOn := func(xid string, n int) {
		deadline := time.Now().Add(15 * time.Second)
		for {
			var waiting int
			if err := watcher.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype = 'transactionid' AND transactionid::text = $1 AND NOT granted`, xid).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting >= n {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%d of %d PUTs reached the INSERT behind the held row", waiting, n)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	const met = 4 // pgx's smallest default pool: every PUT holds a connection while it waits

	committed := newRef("RACE")
	tx, xid := held(committed)
	created, ok, ids := burst(committed, met, func() {
		blockedOn(xid, met)
		if err := tx.Commit(ctx); err != nil {
			t.Error(err)
		}
	})
	rows := r.venuesByRef(biz, committed)
	if len(rows) != 1 || created != 0 || ok != met || len(ids) != 1 || !ids["loc_"+rows[0].id.String()] {
		t.Fatalf("the holder committed: %d rows, %d x 201, %d x 200, ids %v; want 1 row and %d x 200 on it", len(rows), created, ok, ids, met)
	}
	if n := len(r.venueTrail(biz, rows[0].id)); n != 0 {
		t.Fatalf("PUTs identical to the committed row left %d trail rows", n)
	}

	rolled := newRef("RACE")
	tx, xid = held(rolled)
	created, ok, ids = burst(rolled, met, func() {
		blockedOn(xid, met)
		if err := tx.Rollback(ctx); err != nil {
			t.Error(err)
		}
	})
	rows = r.venuesByRef(biz, rolled)
	if len(rows) != 1 || created != 1 || ok != met-1 || len(ids) != 1 || !ids["loc_"+rows[0].id.String()] {
		t.Fatalf("the holder rolled back: %d rows, %d x 201, %d x 200, ids %v; want 1, 1, %d, one id", len(rows), created, ok, ids, met-1)
	}
	if trail := r.venueTrail(biz, rows[0].id); len(trail) != 1 || trail[0].action != tenant.ActionLocationCreated {
		t.Fatalf("the trail: %+v, want one location.created", trail)
	}

	free := newRef("RACE")
	const n = 16
	created, ok, ids = burst(free, n, nil)
	rows = r.venuesByRef(biz, free)
	if len(rows) != 1 || created != 1 || ok != n-1 || len(ids) != 1 {
		t.Fatalf("a burst of %d: %d rows, %d x 201, %d x 200, ids %v", n, len(rows), created, ok, ids)
	}
	if w := r.putLoc(raw, free, `{"name":"Sliema","active":true}`); w.Code != http.StatusOK {
		t.Fatalf("a later PUT: %d", w.Code)
	}
	if rows := r.venuesByRef(biz, free); len(rows) != 1 {
		t.Fatalf("%d rows after a later PUT", len(rows))
	}
}

// TestLocationsPut_IdempotencyReplay (ADR 0028 §8 on this endpoint): with an Idempotency-Key the
// first PUT is 201; the same request again is 201 with Idempotent-Replayed and the same id, and
// it ran nothing (one trail row); 8 at once with a fresh key on another ref -> one 201 that did the
// work and 7 replays; the same key with another body is 422 idempotency_key_reused; a replay
// after another PUT renamed the venue answers the venue as it is NOW (no body is stored).
func TestLocationsPut_IdempotencyReplay(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	ref, idem := newRef("IDEM"), "loc-"+uuid.NewString()
	body := `{"name":"Mosta","active":true}`
	first := r.putLoc(raw, ref, body, "Idempotency-Key", idem)
	if first.Code != http.StatusCreated || first.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("the first: %d %v", first.Code, first.Header())
	}
	again := r.putLoc(raw, ref, body, "Idempotency-Key", idem)
	if again.Code != http.StatusCreated || again.Header().Get("Idempotent-Replayed") != "true" || locOf(t, again) != locOf(t, first) {
		t.Fatalf("the replay: %d %v %s", again.Code, again.Header(), again.Body.String())
	}
	id := r.venuesByRef(biz, ref)[0].id
	if n := len(r.venueTrail(biz, id)); n != 1 {
		t.Fatalf("the replay ran the work: %d trail rows", n)
	}
	if w := r.putLoc(raw, ref, `{"name":"Mosta","active":false}`, "Idempotency-Key", idem); w.Code != http.StatusUnprocessableEntity || problemOf(t, w).Code != CodeIdempotencyKeyReused {
		t.Fatalf("another body under the key: %d %s", w.Code, w.Body.String())
	}
	if w := r.putLoc(raw, ref, `{"name":"Mosta Rotunda","active":true}`); w.Code != http.StatusOK {
		t.Fatalf("a rename: %d", w.Code)
	}
	late := r.putLoc(raw, ref, body, "Idempotency-Key", idem)
	if late.Code != http.StatusCreated || late.Header().Get("Idempotent-Replayed") != "true" || locOf(t, late).Name != "Mosta Rotunda" {
		t.Fatalf("a replay after a rename: %d %s, want 201 replayed with the venue as it is now", late.Code, late.Body.String())
	}

	ref2, idem2 := newRef("IDEM"), "loc-"+uuid.NewString()
	const n = 8
	codes := make(chan string, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w := r.putLoc(raw, ref2, body, "Idempotency-Key", idem2)
			codes <- fmt.Sprintf("%d/%s", w.Code, w.Header().Get("Idempotent-Replayed"))
		}()
	}
	close(start)
	wg.Wait()
	close(codes)
	tally := map[string]int{}
	for c := range codes {
		tally[c]++
	}
	if tally["201/"] != 1 || tally["201/true"] != n-1 {
		t.Fatalf("%d PUTs under one key: %v, want one fresh 201 and %d replays", n, tally, n-1)
	}
	rows := r.venuesByRef(biz, ref2)
	if len(rows) != 1 || len(r.venueTrail(biz, rows[0].id)) != 1 {
		t.Fatalf("%d rows under one key", len(rows))
	}
}

// TestLocationsPut_OutsiderKeyCannotTouchAnotherTenant (ADR 0028 §2, CLAUDE.md §4.5; plan §5 "C
// anahtarı başka tenant'ı göremez"): A's venue has ref R. C's key PUTs R with another name and
// active:false -> 201: C opens ITS OWN venue (refs are per business, K-24); A's row keeps its
// name and label, A's trail has no row of C's key, and C's answer does not carry A's id. C reusing
// A's Idempotency-Key is C's own first request.
func TestLocationsPut_OutsiderKeyCannotTouchAnotherTenant(t *testing.T) {
	r := newDBRig(t)
	a, aOwner := r.business(true)
	c, cOwner := r.business(true)
	aRaw, _ := r.key(a, aOwner)
	cRaw, cKey := r.key(c, cOwner)
	ref, idem := newRef("A"), "shared-"+uuid.NewString()
	wa := r.putLoc(aRaw, ref, `{"name":"A's venue","active":true}`, "Idempotency-Key", idem)
	if wa.Code != http.StatusCreated {
		t.Fatalf("A: %d", wa.Code)
	}
	aVenue := r.venuesByRef(a, ref)[0]
	wc := r.putLoc(cRaw, ref, `{"name":"C was here","active":false}`, "Idempotency-Key", idem)
	if wc.Code != http.StatusCreated || wc.Header().Get("Idempotent-Replayed") != "" || locOf(t, wc).ID == locOf(t, wa).ID {
		t.Fatalf("C's PUT of A's ref: %d %s %v; want C's own new venue", wc.Code, wc.Body.String(), wc.Header())
	}
	if rows := r.venuesByRef(a, ref); len(rows) != 1 || rows[0] != aVenue {
		t.Fatalf("A's venue after C's PUT: %+v, was %+v", rows, aVenue)
	}
	if rows := r.venuesByRef(c, ref); len(rows) != 1 || rows[0].id == aVenue.id || rows[0].name != "C was here" {
		t.Fatalf("C's venue: %+v", rows)
	}
	for _, tr := range r.venueTrail(a, aVenue.id) {
		if tr.actor != nil && *tr.actor == cKey {
			t.Fatalf("A's trail carries C's key: %s", tr.raw)
		}
	}
}

// TestLocationsPut_AHoldingPeersVenueIsNeitherSeenNorChanged (ADR 0028 §2 -- writes are the key's
// tenant's alone; ADR 0027: the holding opens no write path): KF and KM are in one holding; KF's
// venue has ref R. KM's key PUTs R -> 201 on a NEW KM venue; KF's row is unchanged and still
// KF's only row under R.
func TestLocationsPut_AHoldingPeersVenueIsNeitherSeenNorChanged(t *testing.T) {
	r := newDBRig(t)
	kf, kfOwner := r.business(true)
	km, kmOwner := r.business(true)
	ctx := context.Background()
	var h uuid.UUID
	if err := r.owner.QueryRow(ctx, `INSERT INTO holdings (name) VALUES ($1) RETURNING id`, "api5 "+uuid.NewString()[:8]).Scan(&h); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		if _, err := r.owner.Exec(bg, `DELETE FROM holding_members WHERE holding_id = $1`, h); err != nil {
			t.Errorf("cleanup memberships: %v", err)
		}
		if _, err := r.owner.Exec(bg, `DELETE FROM holdings WHERE id = $1`, h); err != nil {
			t.Errorf("cleanup holding: %v", err)
		}
	})
	for _, m := range []uuid.UUID{kf, km} {
		if _, err := r.owner.Exec(ctx, `INSERT INTO holding_members (holding_id, member_tenant_id, joined_at) VALUES ($1, $2, clock_timestamp() - interval '10 minutes')`, h, m); err != nil {
			t.Fatal(err)
		}
	}
	kfRaw, _ := r.key(kf, kfOwner)
	kmRaw, _ := r.key(km, kmOwner)
	ref := newRef("KF")
	if w := r.putLoc(kfRaw, ref, `{"name":"KF Sliema","active":true}`); w.Code != http.StatusCreated {
		t.Fatalf("KF: %d", w.Code)
	}
	kfVenue := r.venuesByRef(kf, ref)[0]
	w := r.putLoc(kmRaw, ref, `{"name":"KM took it","active":false}`)
	if w.Code != http.StatusCreated || locOf(t, w).ID == "loc_"+kfVenue.id.String() {
		t.Fatalf("KM's PUT of KF's ref: %d %s, want KM's own new venue", w.Code, w.Body.String())
	}
	if rows := r.venuesByRef(kf, ref); len(rows) != 1 || rows[0] != kfVenue {
		t.Fatalf("KF's venue after KM's PUT: %+v, was %+v", rows, kfVenue)
	}
	if rows := r.venuesByRef(km, ref); len(rows) != 1 || rows[0].name != "KM took it" {
		t.Fatalf("KM's venue: %+v", rows)
	}
}

// TestLocationsPut_SuspendedAndClosedWriteNothing (ADR 0028 §10: PUT /v1/locations is closed while
// suspended -- for a new ref, an existing one and an identical PUT alike -- 403 tenant_suspended;
// K-22: a business whose API is closed is 403 api_disabled). Neither leaves a venue, a trail row
// or an Idempotency-Key row; CONTROL: the same PUT once reinstated is 201.
//
// MUTANT (measured, API-5 report): the handler admits WriteEmployeeCreate (an open class) -> 201
// while suspended.
func TestLocationsPut_SuspendedAndClosedWriteNothing(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	existing := newRef("SUSP")
	if w := r.putLoc(raw, existing, `{"name":"Gzira","active":true}`); w.Code != http.StatusCreated {
		t.Fatal(w.Code)
	}
	id := r.venuesByRef(biz, existing)[0].id
	r.suspended.suspend(biz)
	fresh := newRef("SUSP")
	for _, c := range []struct{ ref, body string }{
		{fresh, `{"name":"Msida","active":true}`},
		{existing, `{"name":"Gzira Strand","active":false}`},
		{existing, `{"name":"Gzira","active":true}`},
	} {
		idem := "susp-" + uuid.NewString()
		w := r.putLoc(raw, c.ref, c.body, "Idempotency-Key", idem)
		if w.Code != http.StatusForbidden || problemOf(t, w).Code != CodeTenantSuspended {
			t.Fatalf("suspended, %s %s: %d %s", c.ref, c.body, w.Code, w.Body.String())
		}
		if r.ledgerRows(biz, idem) != 0 {
			t.Fatal("a suspended PUT left an Idempotency-Key row")
		}
	}
	if rows := r.venuesByRef(biz, fresh); len(rows) != 0 {
		t.Fatal("a suspended PUT opened a venue")
	}
	if rows := r.venuesByRef(biz, existing); rows[0].name != "Gzira" || !rows[0].active || len(r.venueTrail(biz, id)) != 1 {
		t.Fatalf("a suspended PUT changed the venue: %+v", rows)
	}

	closed, cOwner := r.business(true)
	cRaw, _ := r.key(closed, cOwner) // a key is issued only while the API is open (API-3)
	r.setAPI(closed, false)
	shut := newRef("SHUT")
	if w := r.putLoc(cRaw, shut, `{"name":"Closed","active":true}`); w.Code != http.StatusForbidden || problemOf(t, w).Code != CodeAPIDisabled {
		t.Fatalf("a closed API: %d %s", w.Code, w.Body.String())
	}
	if rows := r.venuesByRef(closed, shut); len(rows) != 0 {
		t.Fatal("a closed API opened a venue")
	}

	other, oOwner := r.business(true)
	oRaw, _ := r.key(other, oOwner)
	if w := r.putLoc(oRaw, fresh, `{"name":"Msida","active":true}`); w.Code != http.StatusCreated {
		t.Fatalf("CONTROL, a business that is not suspended: %d", w.Code)
	}
}

// TestLocationsPut_TheBodyAndTheRefContract (ADR 0028 §8, §9 and this endpoint's decisions):
//
//	the ref     the schema's pattern after ONE percent-decoding ("%41"/"%7E" are A/~; "%2F", "%00"
//	            and a double encoding stay 422 validation_failed, field "ref")
//	the body    ONE UTF-8 JSON object (400 invalid_request), no unpaired surrogate escape (400 --
//	            encoding/json would store U+FFFD for it), name and active required and of their
//	            types, timezone optional and non-empty, externalRef optional and equal to the path's,
//	            a key named twice 422 duplicate_field (escaped spellings included), any other key --
//	            matched exactly, so "Name" is unknown -- 422 unknown_field WITHOUT echoing the key
//	the name    the panel's rule: trimmed, 1..80 runes, and for a new name none of the refused
//	            characters (BEL, ESC, a line break, U+2028, U+202E, a zero-width space) -- what it
//	            allows is TestLocationsPut_TheNameRuleJudgesOnlyWhatChanges
//	the bound   64 KiB (one byte over: 400 body_too_large; a 64 KiB object padded with whitespace:
//	            accepted)
//
// No refusal leaves a row.
//
// MUTANTS (measured, API-5 report): the UTF-8 check removed -> a name with U+FFFD 201; the
// surrogate check removed -> "\ud800" 201; the duplicate check removed -> {"active":true,
// "active":false} 201; the decoding removed -> "%41..." 422; the segment read off the
// already-decoded r.URL.Path (decoded twice) -> "%2541BC" 201 as "ABC"; the character rule removed
// -> the refused names 201; the key check made case-insensitive (encoding/json's own matching) ->
// "Name" passes as a known key and the answer is "name required", not unknown_field.
func TestLocationsPut_TheBodyAndTheRefContract(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	ref := newRef("BODY")
	for _, c := range []struct {
		name, ref, body string
		status          int
		code            Code
		field, reason   string
	}{
		{"ref starts with a dot", ".x", `{"name":"n","active":true}`, 422, CodeValidationFailed, "ref", "invalid"},
		{"ref of 65", "a" + strings.Repeat("b", 64), `{"name":"n","active":true}`, 422, CodeValidationFailed, "ref", "invalid"},
		{"ref with @", "a@b", `{"name":"n","active":true}`, 422, CodeValidationFailed, "ref", "invalid"},
		{"ref with an encoded slash", "a%2Fb", `{"name":"n","active":true}`, 422, CodeValidationFailed, "ref", "invalid"},
		{"ref with an encoded NUL", "a%00b", `{"name":"n","active":true}`, 422, CodeValidationFailed, "ref", "invalid"},
		{"ref double-encoded", "%2541BC", `{"name":"n","active":true}`, 422, CodeValidationFailed, "ref", "invalid"},
		{"ref with a leading space", "%20" + ref, `{"name":"n","active":true}`, 422, CodeValidationFailed, "ref", "invalid"},
		{"empty body", ref, ``, 400, CodeInvalidRequest, "", ""},
		{"not an object", ref, `[]`, 400, CodeInvalidRequest, "", ""},
		{"null", ref, `null`, 400, CodeInvalidRequest, "", ""},
		{"two objects", ref, `{"name":"n","active":true}{}`, 400, CodeInvalidRequest, "", ""},
		{"a body that is not UTF-8", ref, "{\"name\":\"Rusty\xffBar\",\"active\":true}", 400, CodeInvalidRequest, "", "invalid_utf8"},
		{"an unpaired surrogate escape", ref, `{"name":"Bar \ud800","active":true}`, 400, CodeInvalidRequest, "", "invalid_utf8"},
		{"an escaped backslash before a surrogate", ref, `{"name":"\\ufffd \ud800","active":true}`, 400, CodeInvalidRequest, "", "invalid_utf8"},
		{"an unpaired low surrogate", ref, `{"name":"Bar \uDC00x","active":true}`, 400, CodeInvalidRequest, "", "invalid_utf8"},
		{"broken JSON", ref, `{"name":"n",`, 400, CodeInvalidRequest, "", ""},
		{"no name", ref, `{"active":true}`, 422, CodeValidationFailed, "name", "required"},
		{"name null", ref, `{"name":null,"active":true}`, 422, CodeValidationFailed, "name", "type"},
		{"name a number", ref, `{"name":5,"active":true}`, 422, CodeValidationFailed, "name", "type"},
		{"name blank", ref, `{"name":"   ","active":true}`, 422, CodeValidationFailed, "name", "invalid"},
		{"name with BEL", ref, `{"name":"Rusty\u0007Bar","active":true}`, 422, CodeValidationFailed, "name", "hidden_character"},
		{"name with ESC", ref, `{"name":"Rusty\u001b[31mBar","active":true}`, 422, CodeValidationFailed, "name", "hidden_character"},
		{"name with a line break inside", ref, `{"name":"Rusty\nBar","active":true}`, 422, CodeValidationFailed, "name", "hidden_character"},
		{"name with RTL override", ref, `{"name":"Rusty\u202eraB","active":true}`, 422, CodeValidationFailed, "name", "hidden_character"},
		{"name with a line separator", ref, `{"name":"Rusty\u2028Bar","active":true}`, 422, CodeValidationFailed, "name", "hidden_character"},
		{"name with a zero-width space", ref, `{"name":"Rusty\u200bBar","active":true}`, 422, CodeValidationFailed, "name", "hidden_character"},
		{"name of 81 runes", ref, `{"name":"` + strings.Repeat("ż", 81) + `","active":true}`, 422, CodeValidationFailed, "name", "invalid"},
		{"no active", ref, `{"name":"n"}`, 422, CodeValidationFailed, "active", "required"},
		{"active a string", ref, `{"name":"n","active":"false"}`, 422, CodeValidationFailed, "active", "type"},
		{"active null", ref, `{"name":"n","active":null}`, 422, CodeValidationFailed, "active", "type"},
		{"empty timezone", ref, `{"name":"n","active":true,"timezone":""}`, 422, CodeValidationFailed, "timezone", "type"},
		{"externalRef not the path's", ref, `{"name":"n","active":true,"externalRef":"other"}`, 422, CodeValidationFailed, "externalRef", "mismatch"},
		{"a key twice", ref, `{"name":"n","active":true,"active":false}`, 422, CodeValidationFailed, "", "duplicate_field"},
		{"a key twice, once escaped", ref, `{"name":"n","n\u0061me":"m","active":true}`, 422, CodeValidationFailed, "", "duplicate_field"},
		{"a misspelt key", ref, `{"name":"n","active":true,"acitve":false}`, 422, CodeValidationFailed, "", "unknown_field"},
		{"a key of another case", ref, `{"Name":"n","active":true}`, 422, CodeValidationFailed, "", "unknown_field"},
		{"a coordinate", ref, `{"name":"n","active":true,"latitude":35.9}`, 422, CodeValidationFailed, "", "unknown_field"},
	} {
		w := r.putLoc(raw, c.ref, c.body)
		if w.Code != c.status {
			t.Errorf("%s: %d %s, want %d", c.name, w.Code, w.Body.String(), c.status)
			continue
		}
		p := problemOf(t, w)
		if p.Code != c.code || (c.field != "" && p.Details["field"] != c.field) || (c.reason != "" && p.Details["reason"] != c.reason) {
			t.Errorf("%s: %+v, want %s field %q reason %q", c.name, p, c.code, c.field, c.reason)
		}
		if strings.Contains(w.Body.String(), "acitve") || strings.Contains(w.Body.String(), "latitude") {
			t.Errorf("%s: the refusal echoes the request: %s", c.name, w.Body.String())
		}
	}
	if rows := r.venuesByRef(biz, ref); len(rows) != 0 {
		t.Fatalf("refusals opened %d venues", len(rows))
	}
	if w := r.putLoc(raw, ref, `{"name":"`+strings.Repeat("ż", 80)+`","active":true,"externalRef":"`+ref+`"}`); w.Code != http.StatusCreated {
		t.Fatalf("80 runes and a matching externalRef: %d %s", w.Code, w.Body.String())
	}
	// CONTROL for the surrogate check: an ESCAPED U+FFFD is the client's own character, accepted.
	if w := r.putLoc(raw, newRef("FFFD"), `{"name":"Bar \ufffd","active":true}`); w.Code != http.StatusCreated {
		t.Fatalf("an escaped U+FFFD: %d %s, want 201", w.Code, w.Body.String())
	}
	// An unnecessarily encoded ref is the ref it spells (decoded once, API-5 review): "%41" is
	// "A" and "%7E" is "~" -- one venue under the decoded ref, answered with it.
	enc := newRef("ANC")
	if w := r.putLoc(raw, "%41"+enc[1:], `{"name":"Encoded","active":true}`); w.Code != http.StatusCreated || locOf(t, w).ExternalRef != enc {
		t.Fatalf("%%41 for A: %d %s, want 201 under %s", w.Code, w.Body.String(), enc)
	}
	if w := r.putLoc(raw, enc, `{"name":"Encoded","active":true,"externalRef":"`+enc+`"}`); w.Code != http.StatusOK {
		t.Fatalf("the decoded spelling of the same ref: %d, want 200 on the same venue", w.Code)
	}
	tilde := newRef("T") + "~x"
	if w := r.putLoc(raw, strings.Replace(tilde, "~", "%7E", 1), `{"name":"Tilde","active":true,"externalRef":"`+tilde+`"}`); w.Code != http.StatusCreated || locOf(t, w).ExternalRef != tilde {
		t.Fatalf("%%7E for ~: %d %s", w.Code, w.Body.String())
	}
	if rows := r.venuesByRef(biz, enc); len(rows) != 1 {
		t.Fatalf("two spellings of one ref made %d venues", len(rows))
	}
	over := `{"name":"n","active":true}` + strings.Repeat(" ", MaxBody-len(`{"name":"n","active":true}`)+1)
	if w := r.putLoc(raw, newRef("BIG"), over); w.Code != 400 || problemOf(t, w).Code != CodeBodyTooLarge {
		t.Fatalf("64 KiB + 1: %d %s", w.Code, w.Body.String())
	}
	at := `{"name":"Big","active":true}`
	at += strings.Repeat(" ", MaxBody-len(at))
	if w := r.putLoc(raw, newRef("BIG"), at); w.Code != http.StatusCreated || len(at) != MaxBody {
		t.Fatalf("exactly 64 KiB: %d %s", w.Code, w.Body.String())
	}
}

// tapRig is the real tap path over the rig's database: checkin.Service.Record, as the tap page
// calls it after the SUN check.
func (r *dbRig) tapService() *checkin.Service {
	r.t.Helper()
	cfg := &config.Config{
		Env: config.EnvDev, BaseURL: "http://localhost:8080", DatabaseURL: os.Getenv("DATABASE_URL"),
		SessionHMACKey: []byte("SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS"), InviteHMACKey: []byte("IIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII"),
		TagKEK: make([]byte, 32), RetentionYears: 2, GPSRadiusMeters: 150, Debounce: 60 * time.Second, Freshness: 180 * time.Second,
	}
	svc, err := checkin.New(r.d, r.trail, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		r.t.Fatal(err)
	}
	return svc
}

// plaqueAndPerson mounts an active plaque (last_ctr 100) at venue and adds an active employee whose
// home is that venue -- both as the owner, the seed's way.
func (r *dbRig) plaqueAndPerson(biz, venue uuid.UUID) (uid string, emp uuid.UUID) {
	r.t.Helper()
	b, key := make([]byte, 7), make([]byte, 44)
	if _, err := rand.Read(b); err != nil {
		r.t.Fatal(err)
	}
	if _, err := rand.Read(key); err != nil {
		r.t.Fatal(err)
	}
	uid = strings.ToUpper(hex.EncodeToString(b))
	ctx := context.Background()
	if _, err := r.owner.Exec(ctx, `INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 100, 'active')`,
		uid, biz, venue, key); err != nil {
		r.t.Fatal(err)
	}
	emp = uuid.New()
	if _, err := r.owner.Exec(ctx, `INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at) VALUES ($1, $2, $3, 'API-5 Staff', 'active', now())`,
		emp, biz, venue); err != nil {
		r.t.Fatal(err)
	}
	return uid, emp
}

// TestLocationsPut_AVenueWithoutEvidenceRecordsATapAsFlag (ADR 0028 §9, plan §2 item 10, CLAUDE.md
// §4.6 and §5 row 7): a venue the API opened has no address range and no coordinate, so a REAL NFC
// tap there (valid SUN, a live session, a source address and no GPS) is RECORDED -- one
// transactions row -- with verdict flag (base:no-evidence-review), trust 20, in the approval queue:
// never refused, never silently approved. CONTROL: once the panel gives the venue an address range
// (SaveVenue -- the evidence is the panel's), the next person's tap from that range is ok.
//
// MUTANT (measured, API-5 report): CreateLocationFromAPI writing an address range ('{203.0.113.0/24}')
// -> the first tap is ok, red.
func TestLocationsPut_AVenueWithoutEvidenceRecordsATapAsFlag(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	ref := newRef("NOEV")
	if w := r.putLoc(raw, ref, `{"name":"Marsaxlokk","active":true}`); w.Code != http.StatusCreated {
		t.Fatal(w.Code)
	}
	venue := r.venuesByRef(biz, ref)[0].id
	svc := r.tapService()
	ctx := context.Background()
	from := netip.MustParseAddr("203.0.113.7")
	uid, emp := r.plaqueAndPerson(biz, venue)
	res, err := svc.Record(ctx, checkin.Request{SessionTenantID: biz, EmployeeID: emp, TagUID: uid, Ctr: 101, Channel: tap.ChannelNFC,
		CMACVerified: true, TagTenantID: biz, SourceIP: from})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if res.Outcome != checkin.OutcomeRecorded || res.TransactionID == uuid.Nil || res.Decision.Verdict != tap.VerdictFlag || res.Decision.Trust != 20 {
		t.Fatalf("a tap at an API venue with no evidence: outcome %s verdict %s trust %d sid %s, want a recorded flag at 20",
			res.Outcome, res.Decision.Verdict, res.Decision.Trust, res.Decision.MatchedSid)
	}
	var verdict string
	var loc uuid.UUID
	if err := r.owner.QueryRow(ctx, `SELECT verdict, location_id FROM transactions WHERE id = $1`, res.TransactionID).Scan(&verdict, &loc); err != nil || verdict != "flag" || loc != venue {
		t.Fatalf("the written row: %q at %s (%v), want flag at the API venue", verdict, loc, err)
	}
	if _, err := r.venues.SaveVenue(ctx, tenant.VenueCommand{TenantID: biz, VenueID: venue, ActorID: owner, Name: "Marsaxlokk",
		StaticIPs: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}}); err != nil {
		t.Fatal(err)
	}
	uid2, emp2 := r.plaqueAndPerson(biz, venue)
	res, err = svc.Record(ctx, checkin.Request{SessionTenantID: biz, EmployeeID: emp2, TagUID: uid2, Ctr: 101, Channel: tap.ChannelNFC,
		CMACVerified: true, TagTenantID: biz, SourceIP: from})
	if err != nil || res.Outcome != checkin.OutcomeRecorded || res.Decision.Verdict != tap.VerdictOK {
		t.Fatalf("CONTROL, after the panel set an address range: %v %s %s", err, res.Outcome, res.Decision.Verdict)
	}
}

// TestLocationsPut_ActiveIsALabelTheTapNeverReads (K-20; plan §2 item 8): the same four taps -- at a
// venue the API labelled active:true and one it labelled active:false, each with no evidence and
// with an address range the panel set -- are recorded with the SAME verdict and trust: flag / 20
// without evidence, ok / 70 from the range. The label changes nothing on the tap path.
//
// MUTANT (measured, API-5 report): GetLocationForTap made to skip inactive venues (`AND active`) ->
// the active:false taps are no longer the active:true ones, red.
func TestLocationsPut_ActiveIsALabelTheTapNeverReads(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	svc := r.tapService()
	ctx := context.Background()
	from := netip.MustParseAddr("203.0.113.9")
	type seen struct {
		verdict tap.Verdict
		trust   int
	}
	results := map[string]seen{}
	for _, active := range []bool{true, false} {
		for _, evidence := range []bool{false, true} {
			ref := newRef("LABEL")
			if w := r.putLoc(raw, ref, fmt.Sprintf(`{"name":"Label %v %v","active":%v}`, active, evidence, active)); w.Code != http.StatusCreated {
				t.Fatal(w.Code)
			}
			venue := r.venuesByRef(biz, ref)[0]
			if venue.active != active {
				t.Fatalf("the label %v was stored as %v", active, venue.active)
			}
			if evidence {
				if _, err := r.venues.SaveVenue(ctx, tenant.VenueCommand{TenantID: biz, VenueID: venue.id, ActorID: owner, Name: venue.name,
					StaticIPs: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}}); err != nil {
					t.Fatal(err)
				}
				// SaveVenue is the panel's write and must not reset the API's label.
				if got := r.venuesByRef(biz, ref)[0].active; got != active {
					t.Fatalf("the panel's save turned the label %v into %v", active, got)
				}
			}
			uid, emp := r.plaqueAndPerson(biz, venue.id)
			res, err := svc.Record(ctx, checkin.Request{SessionTenantID: biz, EmployeeID: emp, TagUID: uid, Ctr: 101, Channel: tap.ChannelNFC,
				CMACVerified: true, TagTenantID: biz, SourceIP: from})
			if err != nil || res.Outcome != checkin.OutcomeRecorded || res.TransactionID == uuid.Nil {
				t.Fatalf("active %v evidence %v: %v %s -- every tap is recorded", active, evidence, err, res.Outcome)
			}
			results[fmt.Sprintf("%v/%v", active, evidence)] = seen{res.Decision.Verdict, res.Decision.Trust}
		}
	}
	want := map[bool]seen{false: {tap.VerdictFlag, 20}, true: {tap.VerdictOK, 70}}
	for _, evidence := range []bool{false, true} {
		on, off := results[fmt.Sprintf("true/%v", evidence)], results[fmt.Sprintf("false/%v", evidence)]
		if on != want[evidence] || off != want[evidence] {
			t.Errorf("evidence %v: active:true %+v, active:false %+v, want both %+v", evidence, on, off, want[evidence])
		}
	}
}

// TestLocationsPut_TheNameRuleJudgesOnlyWhatChanges (API-5 review, second round): the character
// rule allows what real names carry -- the chef emoji (ZWJ), the Scotland flag (tag characters), a
// Persian name with its ZWNJ, Arabic, Hebrew -- and removes a soft hyphen, an LRM and a leading
// BOM; and it judges what a PUT INTRODUCES: a venue already named with U+202E (before the rule, or
// in the panel) takes a PUT of that same name and a new label (200, the label written), while a
// NEW name with U+202E is 422 and changes nothing.
//
// A name on file that carries a refused AND a removable character (`Rusty<RLO><LRM>Bar`,
// `Ru<ZWSP>sty<BOM>`) is compared cleaned, so it stays writable; the refusal's reason is
// "hidden_character", told apart from a length refusal ("invalid").
//
// MUTANTS (measured, API-5 report): the name on file compared raw -> the cleaned cases 422; the
// "unchanged name is exempt" arm removed -> the first PUT 422;
// the rule widened back to every Cc and Cf -> the emoji, the flag and the Persian name 422.
func TestLocationsPut_TheNameRuleJudgesOnlyWhatChanges(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	for _, c := range []struct{ in, stored string }{
		{"Kitchen \U0001F468\u200D\U0001F373", "Kitchen \U0001F468\u200D\U0001F373"},
		{"Glasgow \U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F", "Glasgow \U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F"},
		{"\u0645\u06cc\u200c\u062e\u0627\u0646\u0647", "\u0645\u06cc\u200c\u062e\u0627\u0646\u0647"},
		{"\u0645\u0637\u0639\u0645", "\u0645\u0637\u0639\u0645"},
		{"\u05de\u05e1\u05e2\u05d3\u05d4", "\u05de\u05e1\u05e2\u05d3\u05d4"},
		{"Rusty\u00adBar", "RustyBar"},
		{"Rusty\u200e Bar", "Rusty Bar"},
		{"\ufeffSliema", "Sliema"},
	} {
		ref := newRef("NAME")
		body, _ := json.Marshal(map[string]any{"name": c.in, "active": true})
		w := r.putLoc(raw, ref, string(body))
		if w.Code != http.StatusCreated || locOf(t, w).Name != c.stored {
			t.Errorf("the name %q: %d %s, want 201 stored as %q", c.in, w.Code, w.Body.String(), c.stored)
		}
	}

	const onFile = "Rusty\u202eBar"
	ref := newRef("ONFILE")
	if _, err := r.owner.Exec(context.Background(), `INSERT INTO locations (tenant_id, name, external_ref) VALUES ($1, $2, $3)`, biz, onFile, ref); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"name": onFile, "active": false})
	if w := r.putLoc(raw, ref, string(body)); w.Code != http.StatusOK || locOf(t, w).Active {
		t.Fatalf("the name on file resent with a new label: %d %s, want 200", w.Code, w.Body.String())
	}
	if rows := r.venuesByRef(biz, ref); len(rows) != 1 || rows[0].active || rows[0].name != onFile {
		t.Fatalf("after the label change: %+v", rows)
	}
	// A name on file carrying a refused AND a removable character is compared cleaned (fourth round).
	for _, c := range []struct{ stored, sent, kept string }{
		{"Rusty\u202e\u200eBar", "Rusty\u202e\u200eBar", "Rusty\u202eBar"},
		{"Rusty\u202e\u200eBar", "Rusty\u202eBar", "Rusty\u202eBar"},
		{"Ru\u200bsty\ufeff", "Ru\u200bsty\ufeff", "Ru\u200bsty"},
	} {
		cref := newRef("CLEAN")
		if _, err := r.owner.Exec(context.Background(), `INSERT INTO locations (tenant_id, name, external_ref) VALUES ($1, $2, $3)`, biz, c.stored, cref); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]any{"name": c.sent, "active": true})
		if w := r.putLoc(raw, cref, string(b)); w.Code != http.StatusOK || locOf(t, w).Name != c.kept {
			t.Fatalf("stored %q, sent %q: %d %s, want 200 as %q", c.stored, c.sent, w.Code, w.Body.String(), c.kept)
		}
	}
	body, _ = json.Marshal(map[string]any{"name": "Rusty\u202eBaz", "active": false})
	w := r.putLoc(raw, ref, string(body))
	if p := problemOf(t, w); w.Code != http.StatusUnprocessableEntity || p.Details["field"] != "name" || p.Details["reason"] != "hidden_character" || !strings.Contains(w.Body.String(), "direction override") {
		t.Fatalf("a NEW name with U+202E: %d %s, want 422 naming the rule", w.Code, w.Body.String())
	}
	if rows := r.venuesByRef(biz, ref); rows[0].name != onFile {
		t.Fatalf("a refused rename changed the name to %q", rows[0].name)
	}
}

// TestWrites_AQueryParameterIsRefusedNotIgnored (M11 API-13a, backlog T131 -- API-12's pin of the
// opposite rule, renamed and turned round; guide §3.7, OpenAPI info): a write takes no query
// string. A PUT sent with ?dryRun=1 is 400 invalid_request (details.allowed empty) and writes NOTHING --
// no location, and its Idempotency-Key is not spent: the same key on the query-less request is a first
// request (201), not a mismatch. A DELETE ?dryRun=1 leaves the employee active, a PUT of an employee and
// the resend with a query are refused the same way, and so is a query string that does not parse. A
// bare `?` is no query (201). TestWrites_EveryWriteRefusesAQueryString walks every write of the router.
func TestWrites_AQueryParameterIsRefusedNotIgnored(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	refused := func(what string, w *httptest.ResponseRecorder) {
		t.Helper()
		p := problemOf(t, w)
		if allowed, ok := p.Details["allowed"].([]any); w.Code != http.StatusBadRequest || p.Code != CodeInvalidRequest || !ok || len(allowed) != 0 {
			t.Fatalf("%s: %d %s, want 400 invalid_request with details.allowed []", what, w.Code, w.Body.String())
		}
	}
	loc := newRef("DRY")
	refused("PUT location ?dryRun=1", r.do(http.MethodPut, "/v1/locations/"+loc+"?dryRun=1", raw, `{"name":"Dry Run","active":true}`, "Idempotency-Key", "dry-1"))
	refused("PUT location ?%zz", r.do(http.MethodPut, "/v1/locations/"+loc+"?%zz", raw, `{"name":"Dry Run","active":true}`))
	if n := r.ownerCount(`SELECT count(*) FROM locations WHERE tenant_id = $1 AND external_ref = $2`, biz, loc); n != 0 {
		t.Fatalf("a refused PUT ?dryRun=1 left %d locations, want none", n)
	}
	if w := r.do(http.MethodPut, "/v1/locations/"+loc, raw, `{"name":"Dry Run","active":true}`, "Idempotency-Key", "dry-1"); w.Code != http.StatusCreated || w.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("the query-less PUT with the refused request's key: %d %s, want a first 201 (the refusal spent no key)", w.Code, w.Body.String())
	}
	if w := r.do(http.MethodPut, "/v1/locations/"+newRef("DRY")+"?", raw, `{"name":"Bare Mark","active":true}`); w.Code != http.StatusCreated {
		t.Fatalf("CONTROL a bare ?: %d %s, want 201", w.Code, w.Body.String())
	}
	emp := newRef("DRY")
	refused("PUT employee ?dryRun=1", r.do(http.MethodPut, "/v1/employees/"+emp+"?dryRun=1", raw, empBody("Dry", "Run", nil, true)))
	if w := r.putEmp(raw, emp, empBody("Dry", "Run", nil, true)); w.Code != http.StatusCreated {
		t.Fatal(w.Body.String())
	}
	refused("DELETE employee ?dryRun=1", r.do(http.MethodDelete, "/v1/employees/"+emp+"?dryRun=1", raw, ""))
	refused("POST resend ?dryRun=1", r.do(http.MethodPost, "/v1/employees/"+emp+"/resend-activation?dryRun=1", raw, ""))
	if w := r.do(http.MethodGet, "/v1/employees/"+emp, raw, ""); w.Code != http.StatusOK || !empOf(t, w).Active {
		t.Fatalf("after a refused DELETE ?dryRun=1: %d %s, want the employee still active", w.Code, w.Body.String())
	}
}
