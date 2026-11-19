package liveqr

// Against a REAL Postgres (CLAUDE.md §8): the pairing's one statement, the resolvers, RLS and the
// audit rows belong to the database. Rows are not cleaned up: tappa_app cannot delete a screen
// (00111) or an audit row (00005); fresh random tenants keep runs apart.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

func liveDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping live QR DB tests (real Postgres required)")
	}
	d, err := db.New(context.Background(), &config.Config{DatabaseURL: config.NewDSN(dsn)})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

type screenWorld struct {
	d              *db.DB
	s              *Screens
	tenant, venue  uuid.UUID
	actor          uuid.UUID
	other, otherAt uuid.UUID // another business and its venue
}

func newScreenWorld(t *testing.T) screenWorld {
	t.Helper()
	d := liveDB(t)
	trail, err := audit.New(d)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewScreens(testKeys(t, "db"), d, trail)
	if err != nil {
		t.Fatal(err)
	}
	w := screenWorld{d: d, s: s, tenant: uuid.New(), venue: uuid.New(), actor: uuid.New(), other: uuid.New(), otherAt: uuid.New()}
	for _, b := range [][2]uuid.UUID{{w.tenant, w.venue}, {w.other, w.otherAt}} {
		err := d.WithTenant(context.Background(), b[0], func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
				VALUES ($1, 'm16-20 screens', $2, 'bar', 'single')`, b[0], "VAT-M1620-"+b[0].String()); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Rusty Bar')`, b[1], b[0])
			return err
		})
		if err != nil {
			t.Fatalf("world: %v", err)
		}
	}
	return w
}

type trailRow struct {
	action, target string
	actor          *uuid.UUID
	detail         string
}

func (w screenWorld) trail(t *testing.T, tenant uuid.UUID) []trailRow {
	t.Helper()
	var out []trailRow
	err := w.d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action, target, actor_id, detail::text FROM audit_log
			WHERE tenant_id = $1 AND action LIKE 'live_qr_screen.%' ORDER BY at, id`, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r trailRow
			if err := rows.Scan(&r.action, &r.target, &r.actor, &r.detail); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestScreens_PairResolveRevoke is the whole life of a screen, through the product's own calls: the
// panel creates it (a waiting row, a code shown once), the screen pairs with the code (ONE use),
// its session resolves to the venue, the token it mints opens to the screen and the venue, the
// panel revokes it (once -- a repeat is Already and writes nothing), and the session then resolves
// to ErrRevoked. Every pairing and revocation is one audit row, with its actor.
func TestScreens_PairResolveRevoke(t *testing.T) {
	w := newScreenWorld(t)
	ctx := context.Background()

	created, err := w.s.Create(ctx, CreateCommand{TenantID: w.tenant, ActorID: w.actor, LocationID: w.venue})
	if err != nil {
		t.Fatal(err)
	}
	if left := time.Until(created.ExpiresAt); left < 9*time.Minute || left > 10*time.Minute+time.Second {
		t.Fatalf("the code works for %v, want 10 minutes", left)
	}
	list, err := w.s.List(ctx, w.tenant, w.venue)
	if err != nil || len(list) != 1 || list[0].State != StateWaiting {
		t.Fatalf("list after create: %+v %v", list, err)
	}

	typed, ok := ParsePairingCode(strings.ToLower(created.Code.ForThePanelOnce()))
	if !ok {
		t.Fatal("the shown code does not parse")
	}
	sc, sess, err := w.s.Pair(ctx, typed)
	if err != nil {
		t.Fatal(err)
	}
	if sc.ID != created.ID || sc.TenantID != w.tenant || sc.LocationID != w.venue || sess.IsZero() {
		t.Fatalf("paired %+v", sc)
	}
	if _, _, err := w.s.Pair(ctx, typed); !errors.Is(err, ErrCodeRefused) {
		t.Fatalf("the code paired twice: %v", err)
	}

	got, err := w.s.Resolve(ctx, sess)
	if err != nil || got.ID != created.ID || got.LocationName != "Rusty Bar" || !got.LocationActive {
		t.Fatalf("resolve: %+v %v", got, err)
	}
	tok, expires, err := w.s.Token(got)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := w.s.keys.Open(tok)
	if err != nil || claims.DisplayID != created.ID || claims.LocationID != w.venue || !claims.ExpiresAt().Equal(expires.Truncate(time.Second)) {
		t.Fatalf("token: %+v %v (expires %v)", claims, err, expires)
	}

	res, err := w.s.Revoke(ctx, RevokeCommand{TenantID: w.tenant, ActorID: w.actor, DisplayID: created.ID})
	if err != nil || res.Already || res.LocationID != w.venue {
		t.Fatalf("revoke: %+v %v", res, err)
	}
	if res, err := w.s.Revoke(ctx, RevokeCommand{TenantID: w.tenant, ActorID: w.actor, DisplayID: created.ID}); err != nil || !res.Already {
		t.Fatalf("second revoke: %+v %v", res, err)
	}
	if _, err := w.s.Resolve(ctx, sess); !errors.Is(err, ErrRevoked) {
		t.Fatalf("a revoked screen resolves: %v", err)
	}
	list, err = w.s.List(ctx, w.tenant, w.venue)
	if err != nil || len(list) != 1 || list[0].State != StateRevoked {
		t.Fatalf("list after revoke: %+v %v", list, err)
	}

	rows := w.trail(t, w.tenant)
	var actions []string
	for _, r := range rows {
		actions = append(actions, r.action)
		if r.target != created.ID.String() {
			t.Errorf("%s targets %s", r.action, r.target)
		}
		wantActor := r.action != ActionScreenPaired
		if (r.actor != nil) != wantActor || (wantActor && *r.actor != w.actor) {
			t.Errorf("%s actor %v", r.action, r.actor)
		}
	}
	if strings.Join(actions, ",") != ActionScreenCreated+","+ActionScreenPaired+","+ActionScreenRevoked {
		t.Fatalf("trail %v", actions)
	}
}

// TestScreens_TenantBoundaries: a business cannot add a screen at another business's venue
// (ErrUnknownLocation), list it, or revoke another business's screen (ErrUnknownScreen) -- and the
// screen it tried to revoke still pairs.
func TestScreens_TenantBoundaries(t *testing.T) {
	w := newScreenWorld(t)
	ctx := context.Background()
	if _, err := w.s.Create(ctx, CreateCommand{TenantID: w.tenant, ActorID: w.actor, LocationID: w.otherAt}); !errors.Is(err, ErrUnknownLocation) {
		t.Fatalf("a screen at another business's venue: %v", err)
	}
	theirs, err := w.s.Create(ctx, CreateCommand{TenantID: w.other, ActorID: w.actor, LocationID: w.otherAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.Revoke(ctx, RevokeCommand{TenantID: w.tenant, ActorID: w.actor, DisplayID: theirs.ID}); !errors.Is(err, ErrUnknownScreen) {
		t.Fatalf("revoking another business's screen: %v", err)
	}
	if l, err := w.s.List(ctx, w.tenant, w.otherAt); err != nil || len(l) != 0 {
		t.Fatalf("listing another business's venue: %+v %v", l, err)
	}
	if _, _, err := w.s.Pair(ctx, theirs.Code); err != nil {
		t.Fatalf("the other business's code no longer pairs: %v", err)
	}
}

// TestScreens_ARefusedPairingIsRecordedOnlyWhereItHasABusiness: an unknown code is refused with no
// row anywhere (no tenant is known for it -- audit_log.tenant_id is NOT NULL); a code the panel
// revoked before it was used resolves to nothing (the revocation drops its digest, 00111) and is
// refused the same way; and a code that RESOLVED but lost to a revocation between the resolution and
// the one statement is refused WITH a row in that business's trail ("pairing_refused", reason
// "raced", the screen as target, no actor).
func TestScreens_ARefusedPairingIsRecordedOnlyWhereItHasABusiness(t *testing.T) {
	w := newScreenWorld(t)
	ctx := context.Background()
	before := len(w.trail(t, w.tenant))
	if _, _, err := w.s.Pair(ctx, mustParse(t, "0000-0000-00000")); !errors.Is(err, ErrCodeRefused) {
		t.Fatalf("an unknown code: %v", err)
	}
	if got := len(w.trail(t, w.tenant)); got != before {
		t.Fatalf("an unknown code wrote %d trail row(s)", got-before)
	}

	revoked, err := w.s.Create(ctx, CreateCommand{TenantID: w.tenant, ActorID: w.actor, LocationID: w.venue})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.Revoke(ctx, RevokeCommand{TenantID: w.tenant, ActorID: w.actor, DisplayID: revoked.ID}); err != nil {
		t.Fatal(err)
	}
	before = len(w.trail(t, w.tenant))
	if _, _, err := w.s.Pair(ctx, revoked.Code); !errors.Is(err, ErrCodeRefused) {
		t.Fatalf("a revoked screen's code: %v", err)
	}
	if got := len(w.trail(t, w.tenant)); got != before {
		t.Fatalf("a revoked code's attempt wrote %d trail row(s)", got-before)
	}

	raced, err := w.s.Create(ctx, CreateCommand{TenantID: w.tenant, ActorID: w.actor, LocationID: w.venue})
	if err != nil {
		t.Fatal(err)
	}
	racing := *w.s
	racing.data = revokeAfterResolve{Database: w.d, s: w.s, cmd: RevokeCommand{TenantID: w.tenant, ActorID: w.actor, DisplayID: raced.ID}}
	if _, _, err := racing.Pair(ctx, raced.Code); !errors.Is(err, ErrCodeRefused) {
		t.Fatalf("a pairing raced by a revocation: %v", err)
	}
	rows := w.trail(t, w.tenant)
	last := rows[len(rows)-1]
	if last.action != ActionScreenPairingRefused || last.target != raced.ID.String() || last.actor != nil ||
		!strings.Contains(last.detail, `"reason": "raced"`) {
		t.Fatalf("the raced refusal's row: %+v", last)
	}
}

// revokeAfterResolve is the database with a revocation slipped in between the pairing's resolution
// and its one statement: the race the statement's WHERE exists for.
type revokeAfterResolve struct {
	Database
	s   *Screens
	cmd RevokeCommand
}

func (r revokeAfterResolve) GetLiveQRDisplayByPairingCodeHash(ctx context.Context, h string) (db.ResolvedLiveQRPairing, error) {
	got, err := r.Database.GetLiveQRDisplayByPairingCodeHash(ctx, h)
	if err != nil {
		return got, err
	}
	if _, err := r.s.Revoke(ctx, r.cmd); err != nil {
		return got, err
	}
	return got, nil
}

func mustParse(t *testing.T, s string) PairingCode {
	t.Helper()
	c, ok := ParsePairingCode(s)
	if !ok {
		t.Fatalf("%q does not parse", s)
	}
	return c
}

// TestScreens_TheTrailCarriesNoSecret: across a screen's whole life -- created, paired, revoked --
// and across the two pairing REFUSALS that write a row (live_qr_screen.pairing_refused): a real code
// whose pairing EXPIRED before it was typed, and a real code that lost to a revocation between its
// resolution and the one statement. No audit_log detail or target of the business carries a code
// (any spelling), a code's digest, the session, its digest, or a token (CLAUDE.md §7).
//
// MUTANT (measured, M16-20 fix round): `Code: code.reveal()` added to refusedPairingDetail -> red
// here ("pairing_refused carries a secret").
func TestScreens_TheTrailCarriesNoSecret(t *testing.T) {
	w := newScreenWorld(t)
	ctx := context.Background()
	created, err := w.s.Create(ctx, CreateCommand{TenantID: w.tenant, ActorID: w.actor, LocationID: w.venue})
	if err != nil {
		t.Fatal(err)
	}
	sc, sess, err := w.s.Pair(ctx, created.Code)
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := w.s.Token(Screen{ID: sc.ID, LocationID: sc.LocationID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.Revoke(ctx, RevokeCommand{TenantID: w.tenant, ActorID: w.actor, DisplayID: created.ID}); err != nil {
		t.Fatal(err)
	}

	// The expired refusal: a waiting screen made through the product's own query with a one-second
	// pairing, its code typed after it expired (the sweep is lazy, so the digest still resolves).
	expired := w.waitingScreen(t, 1)
	time.Sleep(1200 * time.Millisecond)
	if _, _, err := w.s.Pair(ctx, expired.code); !errors.Is(err, ErrCodeRefused) {
		t.Fatalf("an expired code: %v", err)
	}
	// The raced refusal: resolved, then revoked before the one statement.
	raced, err := w.s.Create(ctx, CreateCommand{TenantID: w.tenant, ActorID: w.actor, LocationID: w.venue})
	if err != nil {
		t.Fatal(err)
	}
	racing := *w.s
	racing.data = revokeAfterResolve{Database: w.d, s: w.s, cmd: RevokeCommand{TenantID: w.tenant, ActorID: w.actor, DisplayID: raced.ID}}
	if _, _, err := racing.Pair(ctx, raced.Code); !errors.Is(err, ErrCodeRefused) {
		t.Fatalf("a raced code: %v", err)
	}

	codeDigest, _ := w.s.keys.CodeDigest(created.Code)
	sessDigest, _ := w.s.keys.SessionDigest(sess)
	secrets := []string{created.Code.reveal(), created.Code.ForThePanelOnce(), codeDigest, sess.reveal(), sessDigest, tok}
	for _, c := range []PairingCode{expired.code, raced.Code} {
		d, _ := w.s.keys.CodeDigest(c)
		secrets = append(secrets, c.reveal(), c.ForThePanelOnce(), d)
	}
	rows := w.trail(t, w.tenant)
	reasons := map[string]bool{}
	for _, r := range rows {
		var detail map[string]any
		if err := json.Unmarshal([]byte(r.detail), &detail); err != nil {
			t.Fatalf("%s detail is not JSON: %v", r.action, err)
		}
		for k := range detail {
			switch k {
			case "location_id", "pairing_expires_at", "outcome", "reason", "was":
			default:
				t.Errorf("%s detail has the key %q, which no detail type declares", r.action, k)
			}
		}
		for _, s := range secrets {
			if strings.Contains(r.detail, s) || strings.Contains(r.target, s) {
				t.Fatalf("%s carries a secret: %s", r.action, r.detail)
			}
		}
		if r.action == ActionScreenPairingRefused {
			reasons[detail["reason"].(string)] = true
		}
	}
	if !reasons["expired"] || !reasons["raced"] {
		t.Fatalf("PREMISE: the refusals written were %v; the expired and the raced refusal must both be in the trail", reasons)
	}
}

type waitingScreen struct {
	id   uuid.UUID
	code PairingCode
}

// waitingScreen records a screen waiting for a fresh code whose pairing lasts ttl seconds, through
// the product's own query (the service fixes PairingLifetime) -- the only way tappa_app can make an
// expired code without waiting ten minutes.
func (w screenWorld) waitingScreen(t *testing.T, ttl int32) waitingScreen {
	t.Helper()
	code, err := NewPairingCode()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := w.s.keys.CodeDigest(code)
	if err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	err = w.d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).CreateLiveQRDisplay(ctx, store.CreateLiveQRDisplayParams{
			TenantID: w.tenant, LocationID: w.venue, PairingCodeHash: digest, TtlSeconds: ttl,
		})
		id = row.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return waitingScreen{id: id, code: code}
}

// TestScreens_LastSeenIsStampedSparingly (00114, ORTA-1): a paired screen's first resolution stamps
// last_seen_at, the next ones inside ten minutes do not move it, and the panel's list carries it.
func TestScreens_LastSeenIsStampedSparingly(t *testing.T) {
	w := newScreenWorld(t)
	ctx := context.Background()
	created, err := w.s.Create(ctx, CreateCommand{TenantID: w.tenant, ActorID: w.actor, LocationID: w.venue})
	if err != nil {
		t.Fatal(err)
	}
	if l, _ := w.s.List(ctx, w.tenant, w.venue); len(l) != 1 || l[0].LastSeenAt != nil {
		t.Fatalf("a waiting screen was seen: %+v", l)
	}
	_, sess, err := w.s.Pair(ctx, created.Code)
	if err != nil {
		t.Fatal(err)
	}
	var first time.Time
	for i := range 3 {
		if _, err := w.s.Resolve(ctx, sess); err != nil {
			t.Fatal(err)
		}
		l, err := w.s.List(ctx, w.tenant, w.venue)
		if err != nil || len(l) != 1 || l[0].LastSeenAt == nil {
			t.Fatalf("poll %d: %+v %v", i+1, l, err)
		}
		if i == 0 {
			first = *l[0].LastSeenAt
		} else if !l[0].LastSeenAt.Equal(first) {
			t.Fatalf("poll %d moved last_seen_at from %v to %v inside ten minutes", i+1, first, *l[0].LastSeenAt)
		}
	}
}

// TestScreens_ASessionEndsAfterItsLifetime (ORTA-1): a screen session is refused (ErrLapsed) once
// ScreenSessionLifetime has passed since its pairing, whatever it polled meanwhile; the panel lists it
// as lapsed; the cookie lives no longer than the server accepts it.
func TestScreens_ASessionEndsAfterItsLifetime(t *testing.T) {
	if time.Duration(SessionMaxAge)*time.Second != ScreenSessionLifetime || ScreenSessionLifetime != 180*24*time.Hour {
		t.Fatalf("the cookie lives %d s and the session %v", SessionMaxAge, ScreenSessionLifetime)
	}
	w := newScreenWorld(t)
	ctx := context.Background()
	created, err := w.s.Create(ctx, CreateCommand{TenantID: w.tenant, ActorID: w.actor, LocationID: w.venue})
	if err != nil {
		t.Fatal(err)
	}
	sc, sess, err := w.s.Pair(ctx, created.Code)
	if err != nil {
		t.Fatal(err)
	}
	w.s.now = func() time.Time { return time.Now().Add(ScreenSessionLifetime - time.Hour) }
	if _, err := w.s.Resolve(ctx, sess); err != nil {
		t.Fatalf("an hour before the end: %v", err)
	}
	w.s.now = func() time.Time { return time.Now().Add(ScreenSessionLifetime + time.Second) }
	if got, err := w.s.Resolve(ctx, sess); !errors.Is(err, ErrLapsed) || got.ID != sc.ID {
		t.Fatalf("after the end: %+v %v", got, err)
	}
	if l, err := w.s.List(ctx, w.tenant, w.venue); err != nil || len(l) != 1 || l[0].State != StateLapsed {
		t.Fatalf("the list after the end: %+v %v", l, err)
	}
}

// ownerConn is the migration role's connection, the only one that may write tenants.suspended_at in
// a test (the operator's op_suspend_tenant is its product path). Skipped without the variable.
func ownerConn(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the suspension test needs the owner role")
	}
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("owner connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// TestScreens_ASuspendedBusinessIsResolvedAsSuspended (ORTA-2): while the operator has suspended the
// business, its screen resolves with Suspended set (the page then mints nothing); once lifted, it
// resolves as before -- no re-pairing.
func TestScreens_ASuspendedBusinessIsResolvedAsSuspended(t *testing.T) {
	owner := ownerConn(t)
	w := newScreenWorld(t)
	ctx := context.Background()
	created, err := w.s.Create(ctx, CreateCommand{TenantID: w.tenant, ActorID: w.actor, LocationID: w.venue})
	if err != nil {
		t.Fatal(err)
	}
	_, sess, err := w.s.Pair(ctx, created.Code)
	if err != nil {
		t.Fatal(err)
	}
	if sc, err := w.s.Resolve(ctx, sess); err != nil || sc.Suspended {
		t.Fatalf("before: %+v %v", sc, err)
	}
	if _, err := owner.Exec(ctx, `UPDATE tenants SET suspended_at = now(), suspension_reason = 'security_hold' WHERE id = $1`, w.tenant); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = owner.Exec(context.Background(), `UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1`, w.tenant)
	})
	if sc, err := w.s.Resolve(ctx, sess); err != nil || !sc.Suspended {
		t.Fatalf("suspended: %+v %v", sc, err)
	}
	if _, err := owner.Exec(ctx, `UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1`, w.tenant); err != nil {
		t.Fatal(err)
	}
	if sc, err := w.s.Resolve(ctx, sess); err != nil || sc.Suspended {
		t.Fatalf("lifted: %+v %v", sc, err)
	}
}

// TestScreens_LogsNothingOfTheSecrets: with the default logger captured (what internal/db logs on
// this package's behalf goes there too), a screen's whole life -- with a refused and an unknown
// code, a garbage session -- leaves no code, digest, session or token in the capture. A marker logged
// through the same default proves the capture works.
func TestScreens_LogsNothingOfTheSecrets(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	w := newScreenWorld(t)
	ctx := context.Background()
	created, err := w.s.Create(ctx, CreateCommand{TenantID: w.tenant, ActorID: w.actor, LocationID: w.venue})
	if err != nil {
		t.Fatal(err)
	}
	sc, sess, err := w.s.Pair(ctx, created.Code)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _ = w.s.Pair(ctx, created.Code)
	_, _, _ = w.s.Pair(ctx, mustParse(t, "ZZZZ-ZZZZ-ZZZZZ"))
	junk := strings.Repeat("A", 43)
	_, _ = w.s.Resolve(ctx, ScreenSession{v: &junk})
	full, err := w.s.Resolve(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := w.s.Token(full)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.Revoke(ctx, RevokeCommand{TenantID: w.tenant, ActorID: w.actor, DisplayID: sc.ID}); err != nil {
		t.Fatal(err)
	}
	_, _ = w.s.Resolve(ctx, sess)

	slog.Info("liveqr-log-capture-marker")
	got := buf.String()
	if !strings.Contains(got, "liveqr-log-capture-marker") {
		t.Fatal("CONTROL: the captured logger did not see the marker")
	}
	codeDigest, _ := w.s.keys.CodeDigest(created.Code)
	sessDigest, _ := w.s.keys.SessionDigest(sess)
	for _, s := range []string{created.Code.reveal(), created.Code.ForThePanelOnce(), codeDigest, sess.reveal(), sessDigest, tok} {
		if strings.Contains(got, s) {
			t.Fatal("the log carries a live QR secret (code, digest, session or token)")
		}
	}
}
