package db

// adminsessionlifetime_test.go -- T183 (migration 00085, ADR 0035; the user's decision of 2026-10-31,
// "Sunucuda 12 saat mutlak"): a panel session ends AdminSessionLifetime after its admin_sessions row
// was born, on every path a session hash reaches the database.
//
// Each path is measured at the one-second boundary against the Go constant -- one second inside the
// lifetime it passes, at the lifetime it is refused -- which is what pins the SQL literal
// (`interval '12 hours'`) to AdminSessionLifetime. And each path carries its own CONTROL: the
// predicate cut out of the live body (in a savepoint, by the owner) and the expired session passing
// again, so the refusal is shown to be the predicate's and nothing else's -- the mutation proof, kept.
//
// now() is the TRANSACTION's clock here, as it is in the bodies: every age below is written relative to
// it in the same transaction, so a slow test cannot move a session across the boundary.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// aslFile is migration 00085's file name.
const aslFile = "00085_end_panel_sessions_after_twelve_hours.sql"

// aslBelow00085 takes the test's transaction to 00084 by running 00085's Down (rolled back with the
// transaction). The Down/Up tests of 00079, 00080, 00081 and 00084 compare HEAD's bodies with their own
// file's; 00085 redefines those bodies once more (the lifetime predicate), so their HEAD is 00084's.
// 00085's Down names no table (downTables: none), so it takes no table lock to wait on.
func aslBelow00085(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	_, down := opMigrationSections(t, aslFile)
	if tables := downTables(down); len(tables) > 0 {
		t.Fatalf("PREMISE: 00085's Down locks %v", tables)
	}
	opRunSection(t, ctx, tx, down, "00085 Down")
}

// aslAge sets the session's row birth to `age` before the transaction's now().
func aslAge(t *testing.T, ctx context.Context, sp pgx.Tx, hash string, age time.Duration) {
	t.Helper()
	n, err := aiRows(ctx, sp, `UPDATE admin_sessions SET created_at = now() - make_interval(secs => $2) WHERE token_hash = $1`,
		hash, age.Seconds())
	if err != nil || n != 1 {
		t.Fatalf("PREMISE: ageing the session touched %d rows (%v)", n, err)
	}
}

// aslAges are the boundary's two sides and the control's age.
var aslAges = []struct {
	name  string
	age   time.Duration
	alive bool
}{
	{"one second inside the lifetime", AdminSessionLifetime - time.Second, true},
	{"at the lifetime", AdminSessionLifetime, false},
	{"a day past it", AdminSessionLifetime + 24*time.Hour, false},
}

// aslResolve is the production resolver statement (getAdminSessionByTokenHashSQL), as tappa_app.
func aslResolve(t *testing.T, ctx context.Context, sp pgx.Tx, hash string) ResolvedAdminSession {
	t.Helper()
	var s ResolvedAdminSession
	if err := opAs(t, ctx, sp, "tappa_app", func(app pgx.Tx) error {
		return app.QueryRow(ctx, getAdminSessionByTokenHashSQL, hash).Scan(&s.ID, &s.TenantID, &s.AdminUserID, &s.RevokedAt)
	}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return s
}

// aslRecreate replaces one function's live body, in the caller's savepoint, after checking that `cut`
// occurs exactly once in its definition (a control that cut nothing would prove nothing).
func aslRecreate(t *testing.T, ctx context.Context, sp pgx.Tx, fn string, cut *regexp.Regexp, put string) {
	t.Helper()
	var defs []string
	rows, err := sp.Query(ctx, `SELECT pg_get_functiondef(p.oid) FROM pg_proc p WHERE p.proname = $1 AND p.pronamespace = 'public'::regnamespace`, fn)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		defs = append(defs, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("PREMISE: %d functions named %s", len(defs), fn)
	}
	if n := len(cut.FindAllStringIndex(defs[0], -1)); n != 1 {
		t.Fatalf("PREMISE: the control's cut %s occurs %d times in %s", cut, n, fn)
	}
	aiExec(t, ctx, sp, cut.ReplaceAllString(defs[0], put))
}

// TestAdminSessionLifetime_TheResolverReportsTheEnd: resolve_admin_session_by_token_hash (00011, md.7)
// returns a session past its lifetime as REVOKED -- revoked_at = its birth + the lifetime -- and a
// session inside it as live; a session revoked earlier keeps its own revocation. Returned, not filtered:
// the row comes back either way (the caller -- the binary before 00085 included -- refuses it on
// RevokedAt). CONTROL: with the CASE cut back to the bare column, the expired session resolves live.
func TestAdminSessionLifetime_TheResolverReportsTheEnd(t *testing.T) {
	ctx, tx := opTx(t)
	tenant := idTenant(t, ctx, tx, "T183 resolver")
	admin := aiAdmin(t, ctx, tx, tenant, aiEmail())
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	for _, c := range aslAges {
		t.Run(c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				h := idSession(t, ctx, sp, tenant, admin, nil)
				aslAge(t, ctx, sp, h, c.age)
				got := aslResolve(t, ctx, sp, h)
				if got.TenantID != tenant || got.AdminUserID != admin {
					t.Fatalf("resolved to %v/%v, want the session's own tenant and admin (returned, not filtered)", got.TenantID, got.AdminUserID)
				}
				if c.alive {
					if got.RevokedAt != nil {
						t.Fatalf("a session %v old resolves revoked at %v; it is inside its lifetime", c.age, got.RevokedAt)
					}
					return
				}
				want := now.Add(-c.age).Add(AdminSessionLifetime)
				if got.RevokedAt == nil || !got.RevokedAt.Equal(want) {
					t.Fatalf("a session %v old resolves with revoked_at %v, want its end %v", c.age, got.RevokedAt, want)
				}
			})
		})
	}
	t.Run("an earlier revocation is kept", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			h := idSession(t, ctx, sp, tenant, admin, nil)
			aslAge(t, ctx, sp, h, AdminSessionLifetime+time.Hour)
			aiExec(t, ctx, sp, `UPDATE admin_sessions SET revoked_at = now() - interval '13 hours' WHERE token_hash = $1`, h)
			got := aslResolve(t, ctx, sp, h)
			if want := now.Add(-13 * time.Hour); got.RevokedAt == nil || !got.RevokedAt.Equal(want) {
				t.Fatalf("revoked_at = %v, want the revocation's own %v", got.RevokedAt, want)
			}
		})
	})
	t.Run("CONTROL: without the CASE the expired session resolves live", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			aslRecreate(t, ctx, sp, "resolve_admin_session_by_token_hash",
				regexp.MustCompile(`(?s)CASE WHEN s\.revoked_at IS NOT NULL THEN s\.revoked_at.*?END\n`), "s.revoked_at\n")
			h := idSession(t, ctx, sp, tenant, admin, nil)
			aslAge(t, ctx, sp, h, AdminSessionLifetime)
			if got := aslResolve(t, ctx, sp, h); got.RevokedAt != nil {
				t.Fatalf("the cut resolver still reports the end (%v): the control proves nothing", got.RevokedAt)
			}
		})
	})
}

// TestAdminSessionLifetime_TouchRefusesAnEndedSession: store.TouchAdminSession -- the per-request
// authority, as tappa_app in the session's tenant -- answers 0 rows for a session past its lifetime even
// when the caller never looked at the resolver's answer, and refreshes one inside it. (Its mutation
// proof is by hand -- the statement is sqlc's constant: see the T183 report.)
func TestAdminSessionLifetime_TouchRefusesAnEndedSession(t *testing.T) {
	ctx, tx := opTx(t)
	tenant := idTenant(t, ctx, tx, "T183 touch")
	admin := aiAdmin(t, ctx, tx, tenant, aiEmail())
	for _, c := range aslAges {
		t.Run(c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				h := idSession(t, ctx, sp, tenant, admin, nil)
				aslAge(t, ctx, sp, h, c.age)
				var id uuid.UUID
				if err := sp.QueryRow(ctx, `SELECT id FROM admin_sessions WHERE token_hash = $1`, h).Scan(&id); err != nil {
					t.Fatal(err)
				}
				err := opAs(t, ctx, sp, "tappa_app", func(app pgx.Tx) error {
					if _, err := app.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
						return err
					}
					_, err := store.New(app).TouchAdminSession(ctx, store.TouchAdminSessionParams{ID: id, TenantID: tenant})
					return err
				})
				switch {
				case c.alive && err != nil:
					t.Fatalf("a session %v old was refused: %v", c.age, err)
				case !c.alive && !errors.Is(err, pgx.ErrNoRows):
					t.Fatalf("a session %v old answered %v, want pgx.ErrNoRows (not signed in)", c.age, err)
				}
			})
		})
	}
}

// TestAdminSessionLifetime_TheListShowsAnEndedSessionEnded: ListAdminSessionsForAdmin (the read side of
// "sign out everywhere" and of a device list) reports a session past its lifetime with its end in
// revoked_at -- never as live -- and one inside it with NULL.
func TestAdminSessionLifetime_TheListShowsAnEndedSessionEnded(t *testing.T) {
	ctx, tx := opTx(t)
	tenant := idTenant(t, ctx, tx, "T183 list")
	admin := aiAdmin(t, ctx, tx, tenant, aiEmail())
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	for _, c := range aslAges {
		t.Run(c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				aiExec(t, ctx, sp, `UPDATE admin_sessions SET revoked_at = now() WHERE admin_user_id = $1 AND revoked_at IS NULL`, admin)
				h := idSession(t, ctx, sp, tenant, admin, nil)
				aslAge(t, ctx, sp, h, c.age)
				var list []store.ListAdminSessionsForAdminRow
				if err := opAs(t, ctx, sp, "tappa_app", func(app pgx.Tx) error {
					if _, err := app.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
						return err
					}
					var err error
					list, err = store.New(app).ListAdminSessionsForAdmin(ctx, store.ListAdminSessionsForAdminParams{TenantID: tenant, AdminUserID: admin})
					return err
				}); err != nil {
					t.Fatal(err)
				}
				var live int
				for _, r := range list {
					if r.RevokedAt == nil {
						live++
					}
				}
				want := 0
				if c.alive {
					want = 1
				}
				if live != want {
					t.Fatalf("the list shows %d live session(s), want %d", live, want)
				}
				if !c.alive {
					end := now.Add(-c.age).Add(AdminSessionLifetime)
					if !slices.ContainsFunc(list, func(r store.ListAdminSessionsForAdminRow) bool { return r.RevokedAt != nil && r.RevokedAt.Equal(end) }) {
						t.Fatalf("no row carries the session's end %v", end)
					}
				}
			})
		})
	}
}

// TestAdminSessionLifetime_ASwitchStartsAFreshLifetime: the switch births a new row (and the handler
// sets a new 12-hour cookie with it), so a session one second from its end switches into a session
// whose lifetime starts now; one at its end cannot switch at all.
func TestAdminSessionLifetime_ASwitchStartsAFreshLifetime(t *testing.T) {
	ctx, tx := opTx(t)
	w := idNewWorld(t, ctx, tx)
	t.Run("one second before the end", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			old := idSession(t, ctx, sp, w.a, w.ma, nil)
			aslAge(t, ctx, sp, old, AdminSessionLifetime-time.Second)
			fresh := opRandHex(t)
			if rows := idSwitch(t, ctx, sp, old, w.b, fresh); len(rows) != 1 {
				t.Fatalf("the switch returned %d rows", len(rows))
			}
			var born bool
			if err := sp.QueryRow(ctx, `SELECT created_at = now() FROM admin_sessions WHERE token_hash = $1`, fresh).Scan(&born); err != nil || !born {
				t.Fatalf("the new session's row was not born now (%v): its lifetime would not be fresh", err)
			}
			if got := aslResolve(t, ctx, sp, fresh); got.RevokedAt != nil || got.TenantID != w.b {
				t.Fatalf("the new session resolves %v in %v, want live in B", got.RevokedAt, got.TenantID)
			}
			// Its own lifetime, measured: an hour from now it would still be one second younger than its
			// predecessor was when it switched.
			aslAge(t, ctx, sp, fresh, AdminSessionLifetime-time.Second)
			if got := aslResolve(t, ctx, sp, fresh); got.RevokedAt != nil {
				t.Fatalf("the new session ends with its predecessor: %v", got.RevokedAt)
			}
		})
	})
	t.Run("at the end", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			old := idSession(t, ctx, sp, w.a, w.ma, nil)
			aslAge(t, ctx, sp, old, AdminSessionLifetime)
			if rows := idSwitch(t, ctx, sp, old, w.b, opRandHex(t)); len(rows) != 0 {
				t.Fatalf("an ended session switched (%d rows): a switch would make any kept cookie immortal", len(rows))
			}
		})
	})
}

// aslLookup is a session lookup by hash in a BYPASSRLS body, as 00085 writes it.
var aslLookup = regexp.MustCompile(`WHERE s\.token_hash = (p_\w*session_hash)\n(\s+)AND s\.created_at > v_born_after\n`)

// aslBareLookup is any session lookup by hash, guarded or not.
var aslBareLookup = regexp.MustCompile(`WHERE s\.token_hash = (p_\w*session_hash)\n`)

// TestAdminSessionLifetime_EverySessionLookupInADefinerIsBounded is the fail-closed half of the
// definer test below: in every function tappa_identitydefiner owns, every `WHERE s.token_hash =
// p_*session_hash` is followed by the lifetime predicate, and the bound it reads is the lifetime.
// A new function that finds a session by its hash without it is red here before anyone writes a case
// for it -- the security review's finding (a resolver-only check let the definers treat an expired
// session as alive) cannot come back by omission.
func TestAdminSessionLifetime_EverySessionLookupInADefinerIsBounded(t *testing.T) {
	ctx, tx := opTx(t)
	rows, err := tx.Query(ctx, `SELECT p.proname, p.prosrc FROM pg_proc p
	                             WHERE p.proowner = 'tappa_identitydefiner'::regrole ORDER BY p.proname`)
	if err != nil {
		t.Fatal(err)
	}
	type body struct{ name, src string }
	var bodies []body
	for rows.Next() {
		var b body
		if err := rows.Scan(&b.name, &b.src); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	bound := fmt.Sprintf("v_born_after timestamptz := pg_catalog.now() - interval '%d hours';", int(AdminSessionLifetime/time.Hour))
	if AdminSessionLifetime%time.Hour != 0 {
		t.Fatalf("PREMISE: the lifetime %v is not a whole number of hours, the literal this scan reads", AdminSessionLifetime)
	}
	lookups := 0
	for _, b := range bodies {
		src := identityLineComment.ReplaceAllString(b.src, "")
		all, guarded := len(aslBareLookup.FindAllString(src, -1)), len(aslLookup.FindAllString(src, -1))
		if all != guarded {
			t.Errorf("%s: %d of its %d session lookups by hash carry no lifetime predicate", b.name, all-guarded, all)
		}
		if all > 0 && !strings.Contains(src, bound) {
			t.Errorf("%s finds a session by its hash but does not declare %q", b.name, bound)
		}
		lookups += all
	}
	// identitySessionKeyLookups' session rows (identitybelt_test.go): fifteen functions, sixteen lookups.
	if lookups != 16 {
		t.Errorf("anti-vacuity: %d session lookups by hash across the definer's bodies, want 16 (00085's count)", lookups)
	}
}

// TestAdminSessionLifetime_EveryDefinerRefusesAnEndedSession: every identity_* function keyed by a
// session hash, driven through identityExercise's world (identitycatalog_test.go -- each call answers a
// row when the calls before it have run): for each call and each argument that is a live session's
// hash, the call is replayed with that session one second inside its lifetime (it answers, as in the
// exercise), at its lifetime and a day past it (0 rows), and -- the CONTROL -- at its lifetime with the
// one predicate on THAT argument's lookup cut out of the live body (it answers again). The (function,
// argument) pairs reached must be exactly the sixteen lookups the scan above counts.
func TestAdminSessionLifetime_EveryDefinerRefusesAnEndedSession(t *testing.T) {
	ctx, tx := bfTx(t)
	signup := newIdentitySignupWorld(t, ctx, tx)
	var steps []identityStep
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) { steps = identityExerciseSteps(t, ctx, sp, signup) })
	fnOf := regexp.MustCompile(`\b(identity_[a-z_]+)\(`)
	covered := map[string]bool{}
	for k := range steps {
		step := steps[k]
		m := fnOf.FindStringSubmatch(step.sql)
		if m == nil {
			continue
		}
		fn := m[1]
		for j := range step.args {
			if _, ok := step.args[j].(string); !ok {
				continue
			}
			var param string
			if err := tx.QueryRow(ctx, `SELECT coalesce(p.proargnames[$2], '') FROM pg_proc p WHERE p.proname = $1 AND p.pronamespace = 'public'::regnamespace`,
				fn, j+1).Scan(&param); err != nil {
				t.Fatalf("%s's argument %d: %v", fn, j+1, err)
			}
			if !strings.HasSuffix(param, "session_hash") {
				continue
			}
			// The world is built again for each case: each call depends on the calls before it.
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				all := identityExerciseSteps(t, ctx, sp, signup)
				for _, s := range all[:k] {
					if s.owner != "" {
						aiExec(t, ctx, sp, s.owner)
					}
					if n, err := identityStepRows(t, ctx, sp, s); err != nil || n == 0 {
						t.Fatalf("PREMISE: replaying %s answered %d rows (%v)", s.name, n, err)
					}
				}
				s := all[k]
				if s.owner != "" {
					aiExec(t, ctx, sp, s.owner)
				}
				hash, _ := s.args[j].(string)
				if opInt(t, ctx, sp, `SELECT count(*) FROM admin_sessions WHERE token_hash = $1 AND revoked_at IS NULL`, hash) != 1 {
					t.Fatalf("PREMISE: %s's %s is no live session's hash", s.name, param)
				}
				pair := fn + "." + param
				covered[pair] = true
				t.Run(s.name+" / "+param, func(t *testing.T) {
					for _, c := range aslAges {
						aiSavepoint(t, ctx, sp, func(v pgx.Tx) {
							aslAge(t, ctx, v, hash, c.age)
							n, err := identityStepRows(t, ctx, v, s)
							if err != nil {
								t.Fatalf("%s: %v", c.name, err)
							}
							if c.alive && n == 0 {
								t.Fatalf("%s: 0 rows -- a session inside its lifetime was refused", c.name)
							}
							if !c.alive && n != 0 {
								t.Fatalf("%s: %d rows -- an ended session was treated as alive", c.name, n)
							}
						})
					}
					aiSavepoint(t, ctx, sp, func(v pgx.Tx) {
						aslRecreate(t, ctx, v, fn,
							regexp.MustCompile(`(WHERE s\.token_hash = `+param+`\n)\s+AND s\.created_at > v_born_after\n`), "$1")
						aslAge(t, ctx, v, hash, AdminSessionLifetime)
						n, err := identityStepRows(t, ctx, v, s)
						if err != nil || n == 0 {
							t.Fatalf("CONTROL: with the predicate cut, the ended session still answered %d rows (%v) -- "+
								"something else refuses it, and the case proves nothing about the predicate", n, err)
						}
					})
				})
			})
		}
	}
	want := []string{
		"identity_accept_invite.p_session_hash", "identity_claim_signup.p_session_hash", "identity_confirm_window.p_session_hash",
		"identity_keep_all.p_session_hash", "identity_link_by_email.p_session_hash", "identity_link_legacy.p_identity_session_hash",
		"identity_link_legacy.p_legacy_session_hash", "identity_memberships.p_session_hash", "identity_password_for_session.p_session_hash",
		"identity_relink_refused.p_session_hash", "identity_request_verification.p_session_hash", "identity_revoke_sessions.p_session_hash",
		"identity_set_password.p_session_hash", "identity_switch_session.p_session_hash", "identity_unlink_backfilled.p_session_hash",
		"identity_verify_email.p_session_hash",
	}
	var got []string
	for p := range covered {
		got = append(got, p)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("the session lookups reached:\n  %v\nwant every one of 00085's sixteen:\n  %v", got, want)
	}
}

// aslCatalogue is what 00085 changes, as text: every function that reads admin_sessions (signature,
// owner, SECURITY DEFINER + search_path, ACL, body md5) and admin_sessions' column ACLs.
func aslCatalogue(t *testing.T, ctx context.Context, q pgx.Tx) string {
	t.Helper()
	var s string
	if err := q.QueryRow(ctx, `SELECT
	        (SELECT string_agg(p.oid::regprocedure::text || ' ' || p.proowner::regrole::text || ' ' || p.prosecdef::text || ' ' ||
	                           coalesce(array_to_string(p.proconfig, ','), '') || ' ' || coalesce(p.proacl::text, '-') || ' ' || md5(p.prosrc),
	                           E'\n' ORDER BY p.oid::regprocedure::text)
	           FROM pg_proc p WHERE p.pronamespace = 'public'::regnamespace AND p.prosrc LIKE '%admin_sessions%')
	        || E'\n--\n' ||
	        (SELECT string_agg(a.attname || ' ' || coalesce(a.attacl::text, '-'), E'\n' ORDER BY a.attnum)
	           FROM pg_attribute a WHERE a.attrelid = 'public.admin_sessions'::regclass AND a.attnum > 0 AND NOT a.attisdropped)`).Scan(&s); err != nil {
		t.Fatalf("read the catalogue: %v", err)
	}
	return s
}

// TestAdminSessionLifetime00085_DownGivesBackWhatItFoundAndUpHEAD: the Down leaves every definer body
// exactly HEAD's minus what 00085 adds (the bound's DECLARE line and the predicate after each lookup by
// hash), the resolver exactly 00011's body, every owner / SECURITY DEFINER / search_path / ACL as at
// HEAD, and takes the two created_at cells back; the Up again gives HEAD back byte for byte. No table is
// locked by either half (a column GRANT and CREATE OR REPLACE take none on admin_sessions -- measured).
func TestAdminSessionLifetime00085_DownGivesBackWhatItFoundAndUpHEAD(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, aslFile)
	if tables := downTables(up + down); len(tables) > 0 {
		t.Fatalf("PREMISE: 00085 locks %v", tables)
	}
	head := aslCatalogue(t, ctx, tx)
	bodies := func(q pgx.Tx) map[string]string {
		t.Helper()
		out := map[string]string{}
		rows, err := q.Query(ctx, `SELECT p.proname, p.prosrc FROM pg_proc p WHERE p.pronamespace = 'public'::regnamespace AND p.prosrc LIKE '%admin_sessions%'`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var n, src string
			if err := rows.Scan(&n, &src); err != nil {
				t.Fatal(err)
			}
			out[n] = src
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	headBodies := bodies(tx)
	up11, _ := opMigrationSections(t, "00011_add_admin_resolution.sql")
	i := strings.Index(up11, "CREATE FUNCTION resolve_admin_session_by_token_hash(")
	rest := up11[i:]
	want11 := rest[strings.Index(rest, "AS $$")+len("AS $$") : strings.Index(rest, "\n    $$;")+len("\n    ")]
	declLine := regexp.MustCompile(`(?m)^    v_born_after timestamptz := [^\n]*\n`)
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down, "00085 Down")
		at84 := bodies(sp)
		changed := 0
		for name, src := range headBodies {
			want := src
			if name != "resolve_admin_session_by_token_hash" {
				want = aslLookup.ReplaceAllString(declLine.ReplaceAllString(src, ""), "WHERE s.token_hash = $1\n")
			} else {
				want = want11
			}
			if at84[name] != want {
				t.Errorf("after the Down %s is not HEAD's body minus 00085's lines", name)
			}
			if want != src {
				changed++
			}
		}
		if changed != 16 {
			t.Errorf("anti-vacuity: the Down changed %d bodies, want 00085's sixteen", changed)
		}
		var cells string
		if err := sp.QueryRow(ctx, `SELECT coalesce(attacl::text, '-') FROM pg_attribute WHERE attrelid = 'public.admin_sessions'::regclass AND attname = 'created_at'`).Scan(&cells); err != nil {
			t.Fatal(err)
		}
		if cells != "-" {
			t.Errorf("after the Down admin_sessions.created_at carries %s, want no grant", cells)
		}
		for _, role := range []string{"tappa_resolver", "tappa_identitydefiner"} {
			if opInt(t, ctx, sp, `SELECT count(*) FROM pg_proc p WHERE p.prosrc LIKE '%admin_sessions%' AND p.pronamespace = 'public'::regnamespace
			                       AND p.proowner = $1::regrole AND (NOT p.prosecdef OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, pg_temp']
			                            OR NOT has_function_privilege('tappa_app', p.oid, 'EXECUTE'))`, role) != 0 {
				t.Errorf("after the Down a %s function lost SECURITY DEFINER, its search_path or tappa_app's EXECUTE", role)
			}
		}
		opRunSection(t, ctx, sp, up, "00085 Up again")
		if again := aslCatalogue(t, ctx, sp); again != head {
			t.Error("the Up again does not give HEAD's catalogue back byte for byte")
		}
	})
}
