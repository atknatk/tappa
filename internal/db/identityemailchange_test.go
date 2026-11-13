package db

// identityemailchange_test.go -- T173 EC-1 (migrations 00101-00103; ADR 0044 §5.2-§5.9, the user's
// decisions of 2026-11-12) against real Postgres, owner session, everything rolled back (opTx): the
// sign-in address change's request, confirmation and undo, the two redefinitions (identity_open_reset's
// recipient, identity_unlink_backfilled's undo-link refusal) and the widened sweep -- every refusal with
// "nothing written", every guard with its MUTATION (the body rewritten without it, in a savepoint ->
// the case it holds turns), and the Downs. The committed, concurrent half is
// identityemailchange_race_test.go.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	// ecFirstMigration is 00101's number: a Down walk that reaches it runs 00103's, 00102's and 00101's
	// Downs, each of which refuses over a live undo link or an address event (ecLiftDownRefusals).
	ecFirstMigration = 101
	ec00101File      = "00101_widen_the_identity_links_for_an_address_change.sql"
	ec00102File      = "00102_change_an_identity_address_by_mailbox_proof.sql"
	ec00103File      = "00103_sweep_the_spent_address_change_links.sql"

	ecRequestFn = "public.identity_request_email_change(text, text, text, citext)"
	ecChangeFn  = "public.identity_change_email(text, text, text)"
	ecRevertFn  = "public.identity_revert_email_change(text)"
	ecOpenFn    = "public.identity_open_reset(uuid, text, timestamptz, text)"
	ecUnlinkFn  = "public.identity_unlink_backfilled(text, uuid)"
	ecSweepFn   = "public.identity_sweep_events()"
)

// ecLiftDownRefusals sets aside, inside the test's transaction, what 00101-00103's Downs refuse over: the
// live undo links (cancelled) and the address events (deleted). Both come back with the rollback. The
// caller holds the two tables (downTables names them).
func ecLiftDownRefusals(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	for _, s := range []string{
		`UPDATE public.admin_identity_verifications SET cancelled_at = now()
		  WHERE purpose = 'revert' AND used_at IS NULL AND cancelled_at IS NULL`,
		`DELETE FROM public.admin_identity_events WHERE event IN ('email_change_requested', 'email_changed', 'email_reverted')`,
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("lift 00101-00103's Down refusals inside the transaction: %v", err)
		}
	}
}

// ecBelow00102 takes the test's transaction to 00101: 00103's Down, then 00102's (the refusals lifted
// first, their tables locked first).
func ecBelow00102(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	_, d103 := opMigrationSections(t, ec00103File)
	_, d102 := opMigrationSections(t, ec00102File)
	tables := append(downTables(d103), downTables(d102)...)
	slices.Sort(tables)
	lockForDowns(t, ctx, tx, slices.Compact(tables))
	ecLiftDownRefusals(t, ctx, tx)
	opRunSection(t, ctx, tx, d103, "00103 Down")
	opRunSection(t, ctx, tx, d102, "00102 Down")
}

// ecDownTables are the tables 00103's, 00102's and 00101's Downs lock.
func ecDownTables(t *testing.T) []string {
	t.Helper()
	var tables []string
	for _, f := range []string{ec00103File, ec00102File, ec00101File} {
		_, down := opMigrationSections(t, f)
		tables = append(tables, downTables(down)...)
	}
	slices.Sort(tables)
	return slices.Compact(tables)
}

// ecBelow00101 takes the test's transaction to 00100: 00103's, 00102's and 00101's Downs, the refusals
// lifted first. A test that re-runs an older file's Up on top of HEAD needs it where that Up's
// postcondition pins the identity definer WITHOUT 00101's e-mail cells (00072, 00074, 00077, 00078: "no
// e-mail write"). The caller takes ecDownTables with its own tables in one lockForDowns first.
func ecBelow00101(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	lockForDowns(t, ctx, tx, ecDownTables(t))
	ecLiftDownRefusals(t, ctx, tx)
	for _, f := range []string{ec00103File, ec00102File, ec00101File} {
		_, down := opMigrationSections(t, f)
		opRunSection(t, ctx, tx, down, f+" Down")
	}
}

// ecWorld is one person: a VERIFIED identity, a linked row in each of its businesses (the identity's
// digest), and a live session of the first.
type ecWorld struct {
	email   string
	ident   uuid.UUID
	tenants []uuid.UUID
	rows    []uuid.UUID
	session string
}

func newECWorld(t *testing.T, ctx context.Context, tx pgx.Tx, n int) ecWorld {
	t.Helper()
	w := ecWorld{email: aiEmail()}
	w.ident = aiIdentity(t, ctx, tx, w.email)
	aiExec(t, ctx, tx, `UPDATE admin_identities SET email_verified_at = now() - interval '1 day' WHERE id = $1`, w.ident)
	for i := 0; i < n; i++ {
		tn := idTenant(t, ctx, tx, fmt.Sprintf("T173 %d", i))
		r := aiAdmin(t, ctx, tx, tn, w.email)
		aiExec(t, ctx, tx, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now() - interval '2 days' WHERE id = $1`, r, w.ident)
		w.tenants = append(w.tenants, tn)
		w.rows = append(w.rows, r)
	}
	w.session = idSession(t, ctx, tx, w.tenants[0], w.rows[0], nil)
	return w
}

// ecNew is a new address nobody holds.
func ecNew() string { return "t173-" + uuid.NewString() + "@identity.example" }

// ecDigest is the digest newECWorld's identity carries (aiIdentity's).
func ecDigest() string { return opFakeDigest("a") }

// ecRequest calls identity_request_email_change as tappa_app: (0, "") for 0 rows, (1, refusal) for a row
// -- refusal "" when the link was written.
func ecRequest(t *testing.T, ctx context.Context, tx pgx.Tx, session, digest, link, address string) (int, string) {
	t.Helper()
	rows, err := ipApp(t, ctx, tx, `SELECT change_id IS NOT NULL, coalesce(refusal, '') FROM identity_request_email_change($1, $2, $3, $4)`,
		session, digest, link, address)
	if err != nil {
		t.Fatalf("identity_request_email_change: %v", err)
	}
	if len(rows) == 0 {
		return 0, ""
	}
	if written, _ := rows[0][0].(bool); written != (rows[0][1] == "") {
		t.Fatalf("a request row with change_id written=%v and refusal %q: a refusal carries no link and a link no refusal", written, rows[0][1])
	}
	return len(rows), rows[0][1].(string)
}

// ask writes a change link of w to address and returns its hash.
func (w ecWorld) ask(t *testing.T, ctx context.Context, tx pgx.Tx, address string) string {
	t.Helper()
	link := opRandHex(t)
	if n, r := ecRequest(t, ctx, tx, w.session, ecDigest(), link, address); n != 1 || r != "" {
		t.Fatalf("PREMISE: the request answered %d rows, refusal %q", n, r)
	}
	return link
}

type ecChanged struct {
	ok       bool
	old, new string
	revoked  int32
	revert   *time.Time
}

func ecChange(t *testing.T, ctx context.Context, tx pgx.Tx, link, session, revert string) ecChanged {
	t.Helper()
	rows, err := ipApp(t, ctx, tx, `SELECT old_email, new_email, revoked_sessions, revert_expires_at FROM identity_change_email($1, $2, $3)`, link, session, revert)
	if err != nil {
		t.Fatalf("identity_change_email: %v", err)
	}
	if len(rows) == 0 {
		return ecChanged{}
	}
	c := ecChanged{ok: true, old: rows[0][0].(string), new: rows[0][1].(string), revoked: rows[0][2].(int32)}
	if at, ok := rows[0][3].(time.Time); ok {
		c.revert = &at
	}
	return c
}

type ecReverted struct {
	ok       bool
	restored string
	revoked  int32
}

func ecRevert(t *testing.T, ctx context.Context, tx pgx.Tx, revert string) ecReverted {
	t.Helper()
	rows, err := ipApp(t, ctx, tx, `SELECT restored_email, revoked_sessions FROM identity_revert_email_change($1)`, revert)
	if err != nil {
		t.Fatalf("identity_revert_email_change: %v", err)
	}
	if len(rows) == 0 {
		return ecReverted{}
	}
	return ecReverted{ok: true, restored: rows[0][0].(string), revoked: rows[0][1].(int32)}
}

// ecFootprint is everything a call of the three could write about w, as one string: the identity, every
// row of it or of its address, the live sessions, the links, the resets, the audit rows of its
// businesses and its events. Equal before and after = nothing written.
func ecFootprint(t *testing.T, ctx context.Context, tx pgx.Tx, w ecWorld) string {
	t.Helper()
	var s string
	if err := tx.QueryRow(ctx, `SELECT concat_ws(' | ',
	    (SELECT row(i.email::text, i.password_hash, i.email_verified_at, i.confirm_opened_at, i.confirm_open_until, i.confirm_scope)::text
	       FROM admin_identities i WHERE i.id = $1),
	    (SELECT string_agg(a.id::text || ':' || a.email::text || ':' || a.password_hash || ':' || coalesce(a.identity_id::text, '-')
	                       || ':' || coalesce(a.unlinked_at::text, '-'), ',' ORDER BY a.id)
	       FROM admin_users a WHERE a.tenant_id = ANY ($2)),
	    (SELECT count(*) FROM admin_sessions s WHERE s.tenant_id = ANY ($2) AND s.revoked_at IS NULL),
	    (SELECT string_agg(v.purpose || ':' || coalesce(v.used_at::text, '-') || ':' || coalesce(v.cancelled_at::text, '-'), ',' ORDER BY v.id)
	       FROM admin_identity_verifications v WHERE v.identity_id = $1),
	    (SELECT string_agg(coalesce(r.used_at::text, '-') || ':' || coalesce(r.cancelled_at::text, '-'), ',' ORDER BY r.id)
	       FROM admin_identity_resets r WHERE r.identity_id = $1),
	    (SELECT count(*) FROM password_resets p WHERE p.tenant_id = ANY ($2) AND p.cancelled_at IS NULL AND p.used_at IS NULL),
	    (SELECT count(*) FROM audit_log l WHERE l.tenant_id = ANY ($2)),
	    (SELECT count(*) FROM admin_identity_events e WHERE e.identity_id = $1),
	    (SELECT count(*) FROM admin_identities o WHERE o.id <> $1 AND o.email = $3::citext))`,
		w.ident, w.tenants, w.email).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// ecRewrite rewrites fn's live body (in the caller's savepoint): every cut, each present, replaced by
// its put.
func ecRewrite(t *testing.T, ctx context.Context, sp pgx.Tx, fn string, pairs ...[2]string) {
	t.Helper()
	var def string
	if err := sp.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure)`, fn).Scan(&def); err != nil {
		t.Fatal(err)
	}
	for _, p := range pairs {
		if !strings.Contains(def, p[0]) {
			t.Fatalf("PREMISE: %s's body lacks %q", fn, p[0])
		}
		def = strings.ReplaceAll(def, p[0], p[1])
	}
	aiExec(t, ctx, sp, def)
}

// ecRuleBlock is the 6b refusal of the change / the undo (variable v_new / v_old), written by 00102.
func ecRuleBlock(t *testing.T, ctx context.Context, sp pgx.Tx, fn, variable string) string {
	t.Helper()
	var def string
	if err := sp.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure)`, fn).Scan(&def); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?s)    IF \(pg_catalog\.octet_length\(` + variable + `::text\) <= 254.*?\) IS NOT TRUE THEN\n(        RETURN QUERY [^\n]*\n)?        RETURN;\n    END IF;\n`)
	m := re.FindAllString(def, -1)
	if len(m) != 1 {
		t.Fatalf("PREMISE: %s holds %d 6b blocks on %s", fn, len(m), variable)
	}
	return m[0]
}

// ---------------------------------------------------------------- the request --

// TestEmailChange00102_TheRequestsRefusalsAndTheirGuards: every refusal of identity_request_email_change
// answers its own closed-set value (or 0 rows for no session / a malformed link hash) and WRITES NOTHING;
// each guard with its mutation (cut out of the body in a savepoint -> the same case writes a link). The
// lifetime predicate's mutation is TestAdminSessionLifetime_EveryDefinerRefusesAnEndedSession's (the
// exercise's request step).
func TestEmailChange00102_TheRequestsRefusalsAndTheirGuards(t *testing.T) {
	ctx, tx := opTx(t)
	cases := []struct {
		name   string
		setup  func(sp pgx.Tx, w ecWorld) (session, digest, address string)
		want   string // the refusal; "-" for 0 rows
		mutate [][2]string
	}{
		{name: "no session", want: "-", setup: func(sp pgx.Tx, w ecWorld) (string, string, string) { return opRandHex(t), ecDigest(), ecNew() }},
		{name: "an ended session", want: "-", setup: func(sp pgx.Tx, w ecWorld) (string, string, string) {
			aiExec(t, ctx, sp, `UPDATE admin_sessions SET authenticated_at = now() - interval '13 hours' WHERE token_hash = $1`, w.session)
			return w.session, ecDigest(), ecNew()
		}},
		{name: "a session of an unlinked row", want: "-", setup: func(sp pgx.Tx, w ecWorld) (string, string, string) {
			plain := aiAdmin(t, ctx, sp, w.tenants[0], aiEmail())
			return idSession(t, ctx, sp, w.tenants[0], plain, nil), ecDigest(), ecNew()
		}},
		{name: "U+0130", want: "invalid_address", setup: func(sp pgx.Tx, w ecWorld) (string, string, string) {
			return w.session, ecDigest(), strings.Replace(ecNew(), "t173", "tİ73", 1)
		}, mutate: [][2]string{{"    IF (pg_catalog.octet_length(p_new_email::text) <= 254", "    IF false AND (pg_catalog.octet_length(p_new_email::text) <= 254"}}},
		{name: "U+212A", want: "invalid_address", setup: func(sp pgx.Tx, w ecWorld) (string, string, string) {
			return w.session, ecDigest(), strings.Replace(ecNew(), "identity", "Kidentity", 1)
		}, mutate: [][2]string{{"    IF (pg_catalog.octet_length(p_new_email::text) <= 254", "    IF false AND (pg_catalog.octet_length(p_new_email::text) <= 254"}}},
		{name: "a stale digest", want: "stale", setup: func(sp pgx.Tx, w ecWorld) (string, string, string) {
			return w.session, opFakeDigest("b"), ecNew()
		}, mutate: [][2]string{{"IF (v_digest = p_verified_digest) IS NOT TRUE THEN", "IF false THEN"}}},
		{name: "its own address in another case", want: "same_address", setup: func(sp pgx.Tx, w ecWorld) (string, string, string) {
			return w.session, ecDigest(), strings.ToUpper(w.email)
		}, mutate: [][2]string{{"IF (p_new_email OPERATOR(public.=) v_email) IS TRUE THEN", "IF false THEN"}}},
		{name: "an unverified address", want: "unverified", setup: func(sp pgx.Tx, w ecWorld) (string, string, string) {
			aiExec(t, ctx, sp, `UPDATE admin_identities SET email_verified_at = NULL WHERE id = $1`, w.ident)
			return w.session, ecDigest(), ecNew()
		}, mutate: [][2]string{{"    IF v_verified IS NULL THEN\n        RETURN QUERY", "    IF false THEN\n        RETURN QUERY"}}},
		{name: "an open window", want: "window_open", setup: func(sp pgx.Tx, w ecWorld) (string, string, string) {
			ipOpenWindow(t, ctx, sp, w.ident, "reset")
			return w.session, ecDigest(), ecNew()
		}, mutate: [][2]string{{"IF (v_until > v_now) IS TRUE THEN", "IF false THEN"}}},
		{name: "an unlinked row of its address", want: "unlinked_rows", setup: func(sp pgx.Tx, w ecWorld) (string, string, string) {
			aiAdmin(t, ctx, sp, idTenant(t, ctx, sp, "T173 unlinked"), w.email)
			return w.session, ecDigest(), ecNew()
		}, mutate: [][2]string{{"       AND a.status = 'active';\n    IF FOUND THEN\n        RETURN QUERY", "       AND a.status = 'active';\n    IF false THEN\n        RETURN QUERY"}}},
		{name: "a live undo link", want: "revert_live", setup: func(sp pgx.Tx, w ecWorld) (string, string, string) {
			aiExec(t, ctx, sp, `INSERT INTO admin_identity_verifications (identity_id, token_hash, expires_at, purpose, address)
			                    VALUES ($1, $2, now() + interval '7 days', 'revert', $3)`, w.ident, opRandHex(t), aiEmail())
			return w.session, ecDigest(), ecNew()
		}, mutate: [][2]string{{"    IF FOUND THEN\n        RETURN QUERY SELECT NULL::uuid, v_revert,", "    IF false THEN\n        RETURN QUERY SELECT NULL::uuid, v_revert,"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w := newECWorld(t, ctx, sp, 2)
				session, digest, address := c.setup(sp, w)
				before := ecFootprint(t, ctx, sp, w)
				n, refusal := ecRequest(t, ctx, sp, session, digest, opRandHex(t), address)
				switch {
				case c.want == "-" && n != 0:
					t.Fatalf("%d rows (refusal %q), want 0", n, refusal)
				case c.want != "-" && (n != 1 || refusal != c.want):
					t.Fatalf("%d rows, refusal %q; want one row refusing %q", n, refusal, c.want)
				}
				if after := ecFootprint(t, ctx, sp, w); after != before {
					t.Fatalf("a refused request wrote something:\n before %s\n after  %s", before, after)
				}
			})
			if c.mutate == nil {
				return
			}
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w := newECWorld(t, ctx, sp, 2)
				session, digest, address := c.setup(sp, w)
				ecRewrite(t, ctx, sp, ecRequestFn, c.mutate...)
				if n, refusal := ecRequest(t, ctx, sp, session, digest, opRandHex(t), address); n != 1 || refusal != "" {
					t.Fatalf("MUTATION: with the guard cut the request answered %d rows, refusal %q -- the case does not test the guard", n, refusal)
				}
			})
		})
	}
	t.Run("a malformed link hash", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			w := newECWorld(t, ctx, sp, 1)
			before := ecFootprint(t, ctx, sp, w)
			if n, _ := ecRequest(t, ctx, sp, w.session, ecDigest(), "not-a-hash", ecNew()); n != 0 {
				t.Fatalf("a malformed link hash answered %d rows", n)
			}
			if ecFootprint(t, ctx, sp, w) != before {
				t.Fatal("a malformed link hash wrote something")
			}
		})
	})
}

// TestEmailChange00102_TheRequestNeverAsksWhoHoldsTheNewAddress (ADR 0044 §5.4, K-15's fourth requirement):
// a new address nobody holds, one another identity holds, and one another row of one of the person's
// businesses holds -- the three answer the same shape (a link, refusal NULL, 24 hours) and write the same
// things (one link with that address, one event, the earlier link retired). And the refusal set is the
// closed seven (db.EmailChangeRefusals) -- nothing in the body names another.
func TestEmailChange00102_TheRequestNeverAsksWhoHoldsTheNewAddress(t *testing.T) {
	ctx, tx := opTx(t)
	type answer struct {
		rows    int
		shape   string
		written string
	}
	ask := func(prepare func(sp pgx.Tx, w ecWorld, address string)) answer {
		var a answer
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			w := newECWorld(t, ctx, sp, 2)
			address := ecNew()
			prepare(sp, w, address)
			earlier := w.ask(t, ctx, sp, ecNew())
			rows, err := ipApp(t, ctx, sp, `SELECT change_id IS NOT NULL, expires_at = now() + interval '24 hours', refusal IS NULL
			                                  FROM identity_request_email_change($1, $2, $3, $4)`, w.session, ecDigest(), opRandHex(t), address)
			if err != nil {
				t.Fatal(err)
			}
			a.rows = len(rows)
			a.shape = fmt.Sprint(rows)
			if err := sp.QueryRow(ctx, `SELECT concat_ws(',',
			      (SELECT count(*) FROM admin_identity_verifications WHERE identity_id = $1 AND purpose = 'change' AND address = $2::citext
			                                                           AND used_at IS NULL AND cancelled_at IS NULL),
			      (SELECT cancelled_at IS NOT NULL FROM admin_identity_verifications WHERE token_hash = $3),
			      (SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'email_change_requested'))`,
				w.ident, address, earlier).Scan(&a.written); err != nil {
				t.Fatal(err)
			}
		})
		return a
	}
	free := ask(func(pgx.Tx, ecWorld, string) {})
	taken := ask(func(sp pgx.Tx, _ ecWorld, address string) { aiIdentity(t, ctx, sp, address) })
	member := ask(func(sp pgx.Tx, w ecWorld, address string) { aiAdmin(t, ctx, sp, w.tenants[1], address) })
	if free.rows != 1 || free.shape != "[[true true true]]" || free.written != "1,t,2" {
		t.Fatalf("PREMISE: the free address answered %+v", free)
	}
	if taken != free || member != free {
		t.Errorf("the request tells the new address's owner apart: free %+v, another identity's %+v, a row in the person's business %+v", free, taken, member)
	}

	var src string
	if err := tx.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE oid = $1::regprocedure`, ecRequestFn).Scan(&src); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, m := range regexp.MustCompile(`RETURN QUERY SELECT NULL::uuid, [^,]+, '([a-z_]+)'::text;`).FindAllStringSubmatch(src, -1) {
		got[m[1]] = true
	}
	var want []string
	for _, r := range EmailChangeRefusals {
		want = append(want, string(r))
		if !got[string(r)] {
			t.Errorf("db.EmailChangeRefusals names %q, which the body never answers", r)
		}
	}
	for r := range got {
		if !slices.Contains(want, r) {
			t.Errorf("the body answers the refusal %q, which db.EmailChangeRefusals does not name (a closed set)", r)
		}
	}
	if len(got) != 7 || len(EmailChangeRefusals) != 7 {
		t.Errorf("anti-vacuity: %d refusals in the body, %d in Go; want the seven of ADR 0044 §5.3", len(got), len(EmailChangeRefusals))
	}
}

// ---------------------------------------------------------------- the change --

// TestEmailChange00102_TheConfirmationsRefusalsAndTheirGuards: every refusal of identity_change_email is 0
// rows and writes nothing; each guard with its mutation (the same case changes the address with the guard
// cut).
func TestEmailChange00102_TheConfirmationsRefusalsAndTheirGuards(t *testing.T) {
	ctx, tx := opTx(t)
	type press struct{ link, session, revert string }
	cases := []struct {
		name   string
		setup  func(sp pgx.Tx, w ecWorld) press
		mutate [][2]string
	}{
		{name: "an unknown link", setup: func(sp pgx.Tx, w ecWorld) press { return press{opRandHex(t), w.session, opRandHex(t)} }},
		{name: "a cancelled link", setup: func(sp pgx.Tx, w ecWorld) press {
			first := w.ask(t, ctx, sp, ecNew())
			w.ask(t, ctx, sp, ecNew())
			return press{first, w.session, opRandHex(t)}
		}, mutate: [][2]string{{"AND v.cancelled_at IS NULL", "AND v.id IS NOT NULL"}}},
		{name: "a spent link", setup: func(sp pgx.Tx, w ecWorld) press {
			link := w.ask(t, ctx, sp, ecNew())
			aiExec(t, ctx, sp, `UPDATE admin_identity_verifications SET used_at = now() WHERE token_hash = $1`, link)
			return press{link, w.session, opRandHex(t)}
		}, mutate: [][2]string{{"AND v.used_at IS NULL", "AND v.id IS NOT NULL"}}},
		{name: "an expired link", setup: func(sp pgx.Tx, w ecWorld) press {
			link := w.ask(t, ctx, sp, ecNew())
			aiExec(t, ctx, sp, `UPDATE admin_identity_verifications SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day' WHERE token_hash = $1`, link)
			return press{link, w.session, opRandHex(t)}
		}, mutate: [][2]string{{"       AND v.expires_at > v_now;\n    IF NOT FOUND", "    ;\n    IF NOT FOUND"}, {"(v_found AND v_expires > v_now AND", "(v_found AND"}}},
		{name: "a verification link", setup: func(sp pgx.Tx, w ecWorld) press {
			h := opRandHex(t)
			aiExec(t, ctx, sp, `INSERT INTO admin_identity_verifications (identity_id, token_hash, expires_at, purpose) VALUES ($1, $2, now() + interval '1 hour', 'verify')`, w.ident, h)
			return press{h, w.session, opRandHex(t)}
		}},
		{name: "another identity's session", setup: func(sp pgx.Tx, w ecWorld) press {
			other := newECWorld(t, ctx, sp, 1)
			return press{w.ask(t, ctx, sp, ecNew()), other.session, opRandHex(t)}
		}, mutate: [][2]string{{"       AND a.identity_id = v_identity;\n    IF NOT FOUND", "       AND a.identity_id IS NOT NULL;\n    IF NOT FOUND"},
			{"       AND a.identity_id = v_identity\n       FOR UPDATE OF s;", "       AND a.identity_id IS NOT NULL\n       FOR UPDATE OF s;"}}},
		{name: "an ended session", setup: func(sp pgx.Tx, w ecWorld) press {
			link := w.ask(t, ctx, sp, ecNew())
			aiExec(t, ctx, sp, `UPDATE admin_sessions SET authenticated_at = now() - interval '13 hours' WHERE token_hash = $1`, w.session)
			return press{link, w.session, opRandHex(t)}
		}},
		{name: "a malformed undo hash", setup: func(sp pgx.Tx, w ecWorld) press { return press{w.ask(t, ctx, sp, ecNew()), w.session, "x"} }},
		{name: "the address no longer proved", setup: func(sp pgx.Tx, w ecWorld) press {
			link := w.ask(t, ctx, sp, ecNew())
			aiExec(t, ctx, sp, `UPDATE admin_identities SET email_verified_at = NULL WHERE id = $1`, w.ident)
			return press{link, w.session, opRandHex(t)}
		}, mutate: [][2]string{{"IF v_verified IS NULL OR (v_until > v_now) IS TRUE THEN", "IF (v_until > v_now) IS TRUE THEN"}}},
		{name: "a window opened meanwhile", setup: func(sp pgx.Tx, w ecWorld) press {
			link := w.ask(t, ctx, sp, ecNew())
			ipOpenWindow(t, ctx, sp, w.ident, "reset")
			return press{link, w.session, opRandHex(t)}
		}, mutate: [][2]string{{"IF v_verified IS NULL OR (v_until > v_now) IS TRUE THEN", "IF v_verified IS NULL THEN"}}},
		{name: "an unlinked row of the old address born meanwhile", setup: func(sp pgx.Tx, w ecWorld) press {
			link := w.ask(t, ctx, sp, ecNew())
			aiAdmin(t, ctx, sp, idTenant(t, ctx, sp, "T173 unlinked"), w.email)
			return press{link, w.session, opRandHex(t)}
		}, mutate: [][2]string{{"       AND a.status = 'active';\n    IF FOUND THEN\n        RETURN;", "       AND a.status = 'active';\n    IF false THEN\n        RETURN;"}}},
		{name: "a live undo link written meanwhile", setup: func(sp pgx.Tx, w ecWorld) press {
			link := w.ask(t, ctx, sp, ecNew())
			aiExec(t, ctx, sp, `INSERT INTO admin_identity_verifications (identity_id, token_hash, expires_at, purpose, address)
			                    VALUES ($1, $2, now() + interval '7 days', 'revert', $3)`, w.ident, opRandHex(t), aiEmail())
			return press{link, w.session, opRandHex(t)}
		}, mutate: [][2]string{{"       AND v.expires_at > v_now;\n    IF FOUND THEN\n        RETURN;", "       AND v.expires_at > v_now;\n    IF false THEN\n        RETURN;"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w := newECWorld(t, ctx, sp, 2)
				p := c.setup(sp, w)
				before := ecFootprint(t, ctx, sp, w)
				if got := ecChange(t, ctx, sp, p.link, p.session, p.revert); got.ok {
					t.Fatalf("refused case changed the address: %+v", got)
				}
				if after := ecFootprint(t, ctx, sp, w); after != before {
					t.Fatalf("a refused confirmation wrote something:\n before %s\n after  %s", before, after)
				}
			})
			if c.mutate == nil {
				return
			}
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w := newECWorld(t, ctx, sp, 2)
				p := c.setup(sp, w)
				ecRewrite(t, ctx, sp, ecChangeFn, c.mutate...)
				if got := ecChange(t, ctx, sp, p.link, p.session, p.revert); !got.ok {
					t.Fatal("MUTATION: with the guard cut the confirmation is still refused -- the case does not test the guard")
				}
			})
		})
	}
	// The undo hash's shape guard: without it a malformed hash meets the link table's CHECK, whose DETAIL
	// prints the failing row -- the old address.
	t.Run("a malformed undo hash/mutation", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			w := newECWorld(t, ctx, sp, 1)
			link := w.ask(t, ctx, sp, ecNew())
			ecRewrite(t, ctx, sp, ecChangeFn, [2]string{"IF (p_revert_hash ~ '^[0-9a-f]{64}$') IS NOT TRUE THEN", "IF false THEN"})
			_, err := ipApp(t, ctx, sp, `SELECT * FROM identity_change_email($1, $2, 'x')`, link, w.session)
			if code, _ := opCode(err); code != "23514" {
				t.Fatalf("MUTATION: without the shape guard the call answered %v, want the CHECK's 23514", err)
			}
		})
	})
	// 6b on the address the identity takes (a 'change' row written by the owner: the request refuses one).
	for _, glyph := range []struct{ name, from, to string }{{"U+0130", "t173", "tİ73"}, {"U+212A", "identity", "Kidentity"}} {
		t.Run("6b/"+glyph.name, func(t *testing.T) {
			plant := func(sp pgx.Tx, w ecWorld) string {
				h := opRandHex(t)
				aiExec(t, ctx, sp, `INSERT INTO admin_identity_verifications (identity_id, token_hash, expires_at, purpose, address)
				                    VALUES ($1, $2, now() + interval '1 day', 'change', $3)`, w.ident, h, strings.Replace(ecNew(), glyph.from, glyph.to, 1))
				return h
			}
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w := newECWorld(t, ctx, sp, 1)
				h := plant(sp, w)
				before := ecFootprint(t, ctx, sp, w)
				if ecChange(t, ctx, sp, h, w.session, opRandHex(t)).ok || ecFootprint(t, ctx, sp, w) != before {
					t.Fatal("a homoglyph address was taken or something was written")
				}
			})
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w := newECWorld(t, ctx, sp, 1)
				h := plant(sp, w)
				ecRewrite(t, ctx, sp, ecChangeFn, [2]string{ecRuleBlock(t, ctx, sp, ecChangeFn, "v_new"), ""})
				if !ecChange(t, ctx, sp, h, w.session, opRandHex(t)).ok {
					t.Fatal("MUTATION: with 6b cut the homoglyph is still refused -- the case does not test 6b")
				}
			})
		})
	}
}

// TestEmailChange00102_TheAddressRechecksHoldWithoutTheUniqueKeys: the confirmation's "no identity holds
// the new address" and "no other row of one of the person's businesses holds it", and the undo's two
// twins, each hold their case ALONE -- with the UNIQUE key behind them dropped in the savepoint (the second
// belt: with the key there, a writer that skipped the re-check would meet unique_violation, which the body
// turns into the same 0 rows). MUTATION: the re-check cut as well -> the address is written twice.
func TestEmailChange00102_TheAddressRechecksHoldWithoutTheUniqueKeys(t *testing.T) {
	ctx, tx := opTx(t)
	lockForDowns(t, ctx, tx, []string{"admin_identities", "admin_users"})
	dropIdentityKey := `ALTER TABLE admin_identities DROP CONSTRAINT admin_identities_email_key`
	dropRowKey := `DROP INDEX admin_users_tenant_email_key`
	cases := []struct {
		name, fn, drop string
		setup          func(sp pgx.Tx, w ecWorld) func() bool
		cut            [2]string
		twice          string // the count that is 2 when the mutation went through
	}{
		{name: "the change: another identity holds the new address", fn: ecChangeFn, drop: dropIdentityKey,
			setup: func(sp pgx.Tx, w ecWorld) func() bool {
				address := ecNew()
				link := w.ask(t, ctx, sp, address)
				aiIdentity(t, ctx, sp, address)
				return func() bool { return ecChange(t, ctx, sp, link, w.session, opRandHex(t)).ok }
			},
			cut:   [2]string{"PERFORM 1 FROM public.admin_identities AS o WHERE o.email OPERATOR(public.=) v_new;\n    IF FOUND THEN", "PERFORM 1;\n    IF false THEN"},
			twice: `SELECT count(*) FROM admin_identities WHERE email = (SELECT address FROM admin_identity_verifications WHERE identity_id = $1 AND purpose = 'change' LIMIT 1)`},
		{name: "the change: another row of the person's business holds the new address", fn: ecChangeFn, drop: dropRowKey,
			setup: func(sp pgx.Tx, w ecWorld) func() bool {
				address := ecNew()
				link := w.ask(t, ctx, sp, address)
				aiAdmin(t, ctx, sp, w.tenants[1], address)
				return func() bool { return ecChange(t, ctx, sp, link, w.session, opRandHex(t)).ok }
			},
			cut:   [2]string{"           AND o.email OPERATOR(public.=) v_new\n           AND o.id <> m.id;\n        IF FOUND THEN", "           AND o.email OPERATOR(public.=) v_new\n           AND o.id <> m.id;\n        IF false THEN"},
			twice: `SELECT max(n) FROM (SELECT count(*) AS n FROM admin_users WHERE tenant_id IN (SELECT tenant_id FROM admin_users WHERE identity_id = $1) GROUP BY tenant_id, email) AS g`},
		{name: "the undo: another identity holds the old address", fn: ecRevertFn, drop: dropIdentityKey,
			setup: func(sp pgx.Tx, w ecWorld) func() bool {
				revert := opRandHex(t)
				if !ecChange(t, ctx, sp, w.ask(t, ctx, sp, ecNew()), w.session, revert).ok {
					t.Fatal("PREMISE: the change")
				}
				aiIdentity(t, ctx, sp, w.email)
				return func() bool { return ecRevert(t, ctx, sp, revert).ok }
			},
			cut:   [2]string{"WHERE o.email OPERATOR(public.=) v_old AND o.id <> v_identity;\n    IF FOUND THEN", "WHERE o.email OPERATOR(public.=) v_old AND o.id <> v_identity;\n    IF false THEN"},
			twice: `SELECT count(*) FROM admin_identities WHERE email = (SELECT address FROM admin_identity_verifications WHERE identity_id = $1 AND purpose = 'revert' LIMIT 1)`},
		// YÜKSEK-1 (ADR 0044 §8 risk 8): the row the person refused in a business keeps the old address;
		// the person came back to that business with the new one.
		{name: "the undo: a row the person refused keeps the old address", fn: ecRevertFn, drop: dropRowKey,
			setup: func(sp pgx.Tx, w ecWorld) func() bool {
				revert := opRandHex(t)
				if !ecChange(t, ctx, sp, w.ask(t, ctx, sp, ecNew()), w.session, revert).ok {
					t.Fatal("PREMISE: the change")
				}
				refused := aiAdmin(t, ctx, sp, w.tenants[1], w.email)
				aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, refused)
				return func() bool { return ecRevert(t, ctx, sp, revert).ok }
			},
			cut:   [2]string{"           AND o.email OPERATOR(public.=) v_old\n           AND o.id <> m.id;\n        IF FOUND THEN", "           AND o.email OPERATOR(public.=) v_old\n           AND o.id <> m.id;\n        IF false THEN"},
			twice: `SELECT max(n) FROM (SELECT count(*) AS n FROM admin_users WHERE tenant_id IN (SELECT tenant_id FROM admin_users WHERE identity_id = $1) GROUP BY tenant_id, email) AS g`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// With the key in place: 0 rows, nothing written, no error (no 500, no address in a DETAIL).
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w := newECWorld(t, ctx, sp, 2)
				call := c.setup(sp, w)
				before := ecFootprint(t, ctx, sp, w)
				if call() || ecFootprint(t, ctx, sp, w) != before {
					t.Fatal("refused case went through or wrote something")
				}
			})
			// The re-check alone (the key dropped): still 0 rows.
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				aiExec(t, ctx, sp, c.drop)
				w := newECWorld(t, ctx, sp, 2)
				call := c.setup(sp, w)
				if call() {
					t.Fatal("with the key dropped the re-check alone did not hold the case")
				}
			})
			// MUTATION: the re-check cut too -> the address is held twice.
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				aiExec(t, ctx, sp, c.drop)
				w := newECWorld(t, ctx, sp, 2)
				call := c.setup(sp, w)
				ecRewrite(t, ctx, sp, c.fn, c.cut)
				if !call() || opInt(t, ctx, sp, c.twice, w.ident) != 2 {
					t.Fatal("MUTATION: with the re-check and the key gone the case is still refused -- the re-check is not what held it")
				}
			})
		})
	}
}

// TestEmailChange00102_OneTwoAndTenBusinessesMoveTogether (ADR 0044 §5.3 steps 4-7, §5.5, §5.6, §5.7):
// the confirmation moves the identity and the mirror of EVERY linked row -- a disabled membership too --
// to the new address, leaves a refused row at the old one, keeps the confirming session and ends every
// other in every business, cancels the identity's live reset link and every row's live per-row links,
// leaves 'verify' / 'link' links alone, writes one admin.email.changed row per business (outcome, and the
// count only when not zero), one email_changed event and ONE undo link for the old address (7 days); it
// answers both addresses, the confirming business's ended sessions and the undo link's end. The request
// wrote one email_change_requested event; no detail of an event or an audit row names an address, a
// link, the identity or another business.
func TestEmailChange00102_OneTwoAndTenBusinessesMoveTogether(t *testing.T) {
	ctx, tx := opTx(t)
	for _, n := range []int{1, 2, 10} {
		t.Run(fmt.Sprintf("%d businesses", n), func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w := newECWorld(t, ctx, sp, n)
				// Every other business: a second live session; the last business's row disabled.
				for i := 1; i < n; i++ {
					idSession(t, ctx, sp, w.tenants[i], w.rows[i], nil)
				}
				otherHere := idSession(t, ctx, sp, w.tenants[0], w.rows[0], nil)
				if n > 1 {
					aiExec(t, ctx, sp, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, w.rows[n-1])
				}
				refused := aiAdmin(t, ctx, sp, idTenant(t, ctx, sp, "T173 refused"), w.email)
				aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, refused)
				// The live links the change meets.
				idReset, rowReset, verify := opRandHex(t), opRandHex(t), opRandHex(t)
				aiExec(t, ctx, sp, `INSERT INTO admin_identity_resets (identity_id, token_hash, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, w.ident, idReset)
				aiExec(t, ctx, sp, `INSERT INTO password_resets (tenant_id, admin_user_id, token_hash, expires_at) VALUES ($1, $2, $3, now() + interval '1 hour')`, w.tenants[n-1], w.rows[n-1], rowReset)
				aiExec(t, ctx, sp, `INSERT INTO admin_identity_verifications (identity_id, token_hash, expires_at, purpose) VALUES ($1, $2, now() + interval '1 hour', 'link')`, w.ident, verify)

				address := ecNew()
				link := w.ask(t, ctx, sp, address)
				revert := opRandHex(t)
				got := ecChange(t, ctx, sp, link, w.session, revert)
				if !got.ok || got.old != w.email || got.new != address || got.revoked != 1 || got.revert == nil {
					t.Fatalf("the change answered %+v; want the two addresses, 1 session ended here, the undo link's end", got)
				}
				if left := time.Until(*got.revert); left < 7*24*time.Hour-time.Hour || left > 7*24*time.Hour+time.Minute {
					t.Errorf("the undo link lives %v, want 7 days (K-E3)", left)
				}
				if opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE id = $1 AND email = $2 AND email_verified_at > now() - interval '1 minute' AND confirm_scope IS NULL`, w.ident, address) != 1 {
					t.Error("the identity's address is not the new one, freshly proved, with no window")
				}
				for i, r := range w.rows {
					if opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE id = $1 AND email = $2 AND identity_id = $3`, r, address, w.ident) != 1 {
						t.Errorf("business %d's linked row does not mirror the new address", i)
					}
				}
				if opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE id = $1 AND email = $2`, refused, w.email) != 1 {
					t.Error("the refused row moved")
				}
				if opInt(t, ctx, sp, `SELECT count(*) FROM admin_sessions WHERE token_hash = $1 AND revoked_at IS NULL`, w.session) != 1 {
					t.Error("the confirming session ended (K-E6 (a): it stays)")
				}
				if live := opInt(t, ctx, sp, `SELECT count(*) FROM admin_sessions WHERE tenant_id = ANY ($1) AND revoked_at IS NULL AND token_hash <> $2`, w.tenants, w.session); live != 0 {
					t.Errorf("%d other sessions live after the change", live)
				}
				if opInt(t, ctx, sp, `SELECT count(*) FROM admin_sessions WHERE token_hash = $1 AND revoked_at IS NOT NULL`, otherHere) != 1 {
					t.Error("the other session in the confirming business lives")
				}
				if opInt(t, ctx, sp, `SELECT count(*) FROM admin_identity_resets WHERE token_hash = $1 AND cancelled_at IS NOT NULL`, idReset) != 1 ||
					opInt(t, ctx, sp, `SELECT count(*) FROM password_resets WHERE token_hash = $1 AND cancelled_at IS NOT NULL`, rowReset) != 1 {
					t.Error("a live reset link (the identity's or a row's) survived the change")
				}
				if opInt(t, ctx, sp, `SELECT count(*) FROM admin_identity_verifications WHERE token_hash = $1 AND used_at IS NULL AND cancelled_at IS NULL`, verify) != 1 {
					t.Error("a 'link' link was touched (two secrets: it stays)")
				}
				if opInt(t, ctx, sp, `SELECT count(*) FROM admin_identity_verifications WHERE identity_id = $1 AND purpose = 'revert' AND address = $2 AND token_hash = $3
				                         AND used_at IS NULL AND cancelled_at IS NULL`, w.ident, w.email, revert) != 1 ||
					opInt(t, ctx, sp, `SELECT count(*) FROM admin_identity_verifications WHERE token_hash = $1 AND used_at IS NOT NULL`, link) != 1 {
					t.Error("the undo link is not the one live 'revert' row of the old address, or the link was not spent")
				}
				for _, e := range []string{"email_change_requested", "email_changed"} {
					if opInt(t, ctx, sp, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = $2 AND detail = '{}'::jsonb`, w.ident, e) != 1 {
						t.Errorf("not exactly one %s event with an empty detail", e)
					}
				}
				for i, tn := range w.tenants {
					var detail string
					if err := sp.QueryRow(ctx, `SELECT string_agg(detail::text, ';') FROM audit_log WHERE tenant_id = $1 AND action = 'admin.email.changed' AND actor_id = $2 AND target = $2::text`,
						tn, w.rows[i]).Scan(&detail); err != nil {
						t.Fatal(err)
					}
					// Each business had one session besides the confirming one (otherHere in business 0).
					want := `{"outcome": "ok", "revoked_other_sessions": 1}`
					if detail != want {
						t.Errorf("business %d's admin.email.changed detail is %q, want %q", i, detail, want)
					}
					for _, leak := range append([]string{w.email, address, w.ident.String(), link, revert}, uuidStrings(w.tenants, tn)...) {
						if strings.Contains(strings.ToLower(detail), strings.ToLower(leak)) {
							t.Errorf("business %d's audit row names %q", i, leak)
						}
					}
				}
			})
		})
	}
	// The count only when not zero: a business with no other session gets {"outcome": "ok"} alone.
	// MUTATION: the IF cut -> {"outcome": "ok", "revoked_other_sessions": 0}, red.
	zero := func(sp pgx.Tx) string {
		w := newECWorld(t, ctx, sp, 1)
		if !ecChange(t, ctx, sp, w.ask(t, ctx, sp, ecNew()), w.session, opRandHex(t)).ok {
			t.Fatal("PREMISE: the change")
		}
		var d string
		if err := sp.QueryRow(ctx, `SELECT detail::text FROM audit_log WHERE tenant_id = $1 AND action = 'admin.email.changed'`, w.tenants[0]).Scan(&d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		if d := zero(sp); d != `{"outcome": "ok"}` {
			t.Errorf("no other session: the detail is %s, want {\"outcome\": \"ok\"} alone", d)
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		ecRewrite(t, ctx, sp, ecChangeFn, [2]string{"        IF v_n > 0 THEN\n            v_detail := v_detail || pg_catalog.jsonb_build_object('revoked_other_sessions', v_n);\n        END IF;",
			"        v_detail := v_detail || pg_catalog.jsonb_build_object('revoked_other_sessions', v_n);"})
		if d := zero(sp); d == `{"outcome": "ok"}` {
			t.Error("MUTATION: with the IF cut the detail still drops the zero")
		}
	})
}

func uuidStrings(ids []uuid.UUID, except uuid.UUID) []string {
	var out []string
	for _, id := range ids {
		if id != except {
			out = append(out, id.String())
		}
	}
	return out
}

// TestEmailChange00102_TheLinksTheChangeRetires: each retirement of §5.5 with its mutation -- the identity's
// live reset link (cut: it stays live and its consumption still writes the identity), the rows' per-row
// links (cut: one stays live), and the confirmation's identity_id pin on every admin_users write of the
// three bodies (a structural pin -- the row's id is its key, so no behaviour tells the predicate apart).
func TestEmailChange00102_TheLinksTheChangeRetires(t *testing.T) {
	ctx, tx := opTx(t)
	for _, c := range []struct {
		name, plant, live string
		cut               [2]string
	}{
		{name: "the identity's reset link",
			plant: `INSERT INTO admin_identity_resets (identity_id, token_hash, expires_at) VALUES ($1, $2, now() + interval '1 hour')`,
			live:  `SELECT count(*) FROM admin_identity_resets WHERE token_hash = $1 AND cancelled_at IS NULL`,
			cut: [2]string{"    UPDATE public.admin_identity_resets AS r SET cancelled_at = pg_catalog.now()\n     WHERE r.identity_id = v_identity\n       AND r.used_at IS NULL\n       AND r.cancelled_at IS NULL;\n    -- The link is spent",
				"    -- The link is spent"}},
		{name: "a row's per-row link",
			plant: `INSERT INTO password_resets (tenant_id, admin_user_id, token_hash, expires_at)
			        SELECT tenant_id, id, $2, now() + interval '1 hour' FROM admin_users WHERE identity_id = $1 LIMIT 1`,
			live: `SELECT count(*) FROM password_resets WHERE token_hash = $1 AND cancelled_at IS NULL`,
			cut: [2]string{"        UPDATE public.password_resets AS r SET cancelled_at = pg_catalog.now()\n         WHERE r.tenant_id = m.tenant_id AND r.admin_user_id = m.id\n           AND r.used_at IS NULL AND r.cancelled_at IS NULL AND r.expires_at > v_now;\n        -- The business's trail",
				"        -- The business's trail"}},
	} {
		run := func(mutate bool) int64 {
			var live int64
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w := newECWorld(t, ctx, sp, 1)
				h := opRandHex(t)
				aiExec(t, ctx, sp, c.plant, w.ident, h)
				if mutate {
					ecRewrite(t, ctx, sp, ecChangeFn, c.cut)
				}
				if !ecChange(t, ctx, sp, w.ask(t, ctx, sp, ecNew()), w.session, opRandHex(t)).ok {
					t.Fatal("PREMISE: the change")
				}
				live = opInt(t, ctx, sp, c.live, h)
			})
			return live
		}
		if n := run(false); n != 0 {
			t.Errorf("%s: live after the change", c.name)
		}
		if n := run(true); n != 1 {
			t.Errorf("MUTATION %s: with the retirement cut the link is still cancelled -- the case does not test it", c.name)
		}
	}
	// The pins of every admin_users write in the three bodies: the loop record's id AND tenant AND the
	// identity. MUTATION (measured): `AND a.identity_id = v_identity` cut from the change's mirror -> red.
	pin := regexp.MustCompile(`UPDATE public\.admin_users AS a SET [^;]*?\n\s+WHERE a\.id = m\.id AND a\.tenant_id = m\.tenant_id AND a\.identity_id = v_identity;`)
	all := regexp.MustCompile(`UPDATE public\.admin_users AS a SET`)
	check := func(q pgx.Tx) []string {
		var out []string
		for _, fn := range []string{ecChangeFn, ecRevertFn} {
			var src string
			if err := q.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE oid = $1::regprocedure`, fn).Scan(&src); err != nil {
				t.Fatal(err)
			}
			if a, p := len(all.FindAllString(src, -1)), len(pin.FindAllString(src, -1)); a != 1 || p != 1 {
				out = append(out, fmt.Sprintf("%s: %d admin_users writes, %d pinned to the row's id, tenant and identity", fn, a, p))
			}
		}
		return out
	}
	for _, f := range check(tx) {
		t.Error(f)
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		ecRewrite(t, ctx, sp, ecChangeFn, [2]string{"SET email = v_new\n         WHERE a.id = m.id AND a.tenant_id = m.tenant_id AND a.identity_id = v_identity;",
			"SET email = v_new\n         WHERE a.id = m.id AND a.tenant_id = m.tenant_id;"})
		if len(check(sp)) == 0 {
			t.Error("MUTATION: the mirror without its identity pin passed the structural check")
		}
	})
}

// TestEmailChange00102_TheTriggerStillGuardsTheMirror: after a change, neither tappa_app nor the migration
// role (tappa_owner) may write a linked row's e-mail -- 00069's first arm (restrict_violation); the definer
// alone writes it.
func TestEmailChange00102_TheTriggerStillGuardsTheMirror(t *testing.T) {
	ctx, tx := opTx(t)
	w := newECWorld(t, ctx, tx, 1)
	if !ecChange(t, ctx, tx, w.ask(t, ctx, tx, ecNew()), w.session, opRandHex(t)).ok {
		t.Fatal("PREMISE: the change")
	}
	for _, role := range []string{"tappa_app", "tappa_owner"} {
		err := opAs(t, ctx, tx, role, func(sp pgx.Tx) error {
			if role == "tappa_app" {
				if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, w.tenants[0].String()); err != nil {
					return err
				}
			}
			_, err := sp.Exec(ctx, `UPDATE admin_users SET email = $1 WHERE id = $2`, ecNew(), w.rows[0])
			return err
		})
		// tappa_owner meets 00069's trigger (restrict_violation); tappa_app holds no UPDATE (email) at all
		// (00069's REVOKE) -- the privilege is its first wall, the trigger its second.
		want := sqlstateRestrictViolation
		if role == "tappa_app" {
			want = sqlstateInsufficientPrivi
		}
		if code, _ := opCode(err); code != want {
			t.Errorf("%s writing a linked row's e-mail: %v, want SQLSTATE %s", role, err, want)
		}
	}
}

// TestEmailChange00102_InvitationsFollowTheIdentitysAddress (ADR 0044 §5.5): after a change an invitation of
// the OLD address is accepted by nobody's session (0 rows), one of the NEW address is.
func TestEmailChange00102_InvitationsFollowTheIdentitysAddress(t *testing.T) {
	ctx, tx := opTx(t)
	w := newECWorld(t, ctx, tx, 1)
	address := ecNew()
	g, h := idTenant(t, ctx, tx, "T173 invite old"), idTenant(t, ctx, tx, "T173 invite new")
	toOld, toNew := opRandHex(t), opRandHex(t)
	for _, inv := range []struct {
		tenant  uuid.UUID
		address string
		hash    string
	}{{g, w.email, toOld}, {h, address, toNew}} {
		by := aiAdmin(t, ctx, tx, inv.tenant, aiEmail())
		aiExec(t, ctx, tx, `INSERT INTO admin_invites (tenant_id, email, full_name, role, token_hash, invited_by, expires_at)
		                    VALUES ($1, $2, 'T173 Invitee', 'manager', $3, $4, now() + interval '1 day')`, inv.tenant, inv.address, inv.hash, by)
	}
	if !ecChange(t, ctx, tx, w.ask(t, ctx, tx, address), w.session, opRandHex(t)).ok {
		t.Fatal("PREMISE: the change")
	}
	if n := ipRows(t, ctx, tx, `SELECT * FROM identity_accept_invite($1, $2)`, toOld, w.session); n != 0 {
		t.Errorf("an invitation of the old address was accepted (%d rows)", n)
	}
	if n := ipRows(t, ctx, tx, `SELECT * FROM identity_accept_invite($1, $2)`, toNew, w.session); n != 1 {
		t.Errorf("an invitation of the new address answered %d rows, want 1", n)
	}
}

// ---------------------------------------------------------------- the undo --

// TestEmailChange00102_TheUndoGivesTheOldAddressBack (ADR 0044 §5.3, §5.6): the undo restores the old
// address in the identity and every linked row, makes the password a fresh sentinel (bcrypt, cost 12, no
// longer the old digest, the rows' copies equal to it), closes a confirmation window (one a reset at the
// new address opened), ends EVERY session (the confirming one too), cancels the identity's live reset link,
// the rows' per-row links and the identity's live change link, spends itself, writes one
// admin.email.reverted per business and one email_reverted event, and answers the old address and the
// ended sessions. A second press of the link is 0 rows. MUTATION: the window's close cut -> it stays open.
func TestEmailChange00102_TheUndoGivesTheOldAddressBack(t *testing.T) {
	ctx, tx := opTx(t)
	setup := func(sp pgx.Tx) (ecWorld, string, string) {
		w := newECWorld(t, ctx, sp, 2)
		idSession(t, ctx, sp, w.tenants[1], w.rows[1], nil)
		address := ecNew()
		revert := opRandHex(t)
		if !ecChange(t, ctx, sp, w.ask(t, ctx, sp, address), w.session, revert).ok {
			t.Fatal("PREMISE: the change")
		}
		// The attacker's moves at the new address: a window (a reset there), a reset link, a per-row link,
		// a second change request -- and a fresh session (the reset's).
		ipOpenWindow(t, ctx, sp, w.ident, "reset")
		aiExec(t, ctx, sp, `INSERT INTO admin_identity_resets (identity_id, token_hash, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, w.ident, opRandHex(t))
		aiExec(t, ctx, sp, `INSERT INTO password_resets (tenant_id, admin_user_id, token_hash, expires_at) VALUES ($1, $2, $3, now() + interval '1 hour')`, w.tenants[1], w.rows[1], opRandHex(t))
		aiExec(t, ctx, sp, `INSERT INTO admin_identity_verifications (identity_id, token_hash, expires_at, purpose, address) VALUES ($1, $2, now() + interval '1 hour', 'change', $3)`, w.ident, opRandHex(t), ecNew())
		idSession(t, ctx, sp, w.tenants[0], w.rows[0], nil)
		return w, address, revert
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		w, _, revert := setup(sp)
		live := opInt(t, ctx, sp, `SELECT count(*) FROM admin_sessions WHERE tenant_id = ANY ($1) AND revoked_at IS NULL`, w.tenants)
		got := ecRevert(t, ctx, sp, revert)
		if !got.ok || got.restored != w.email || int64(got.revoked) != live {
			t.Fatalf("the undo answered %+v; want the old address and %d sessions ended", got, live)
		}
		if opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE id = $1 AND email = $2 AND email_verified_at > now() - interval '1 minute'
		                         AND confirm_opened_at IS NULL AND confirm_open_until IS NULL AND confirm_scope IS NULL
		                         AND password_hash ~ '^\$2[aby]\$12\$[./A-Za-z0-9]{53}$' AND password_hash <> $3`, w.ident, w.email, ecDigest()) != 1 {
			t.Error("the identity is not back at the old address with a fresh sentinel and no window")
		}
		if opInt(t, ctx, sp, `SELECT count(*) FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id
		                        WHERE a.identity_id = $1 AND a.email = $2 AND a.password_hash = i.password_hash`, w.ident, w.email) != 2 {
			t.Error("a linked row does not mirror the old address or carry the sentinel's copy")
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_sessions WHERE tenant_id = ANY ($1) AND revoked_at IS NULL`, w.tenants); n != 0 {
			t.Errorf("%d sessions live after the undo, want 0", n)
		}
		if n := opInt(t, ctx, sp, `SELECT (SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1 AND used_at IS NULL AND cancelled_at IS NULL)
		                              + (SELECT count(*) FROM password_resets WHERE tenant_id = ANY ($2) AND used_at IS NULL AND cancelled_at IS NULL)
		                              + (SELECT count(*) FROM admin_identity_verifications WHERE identity_id = $1 AND purpose IN ('change', 'revert')
		                                                                                     AND used_at IS NULL AND cancelled_at IS NULL)`, w.ident, w.tenants); n != 0 {
			t.Errorf("%d live links after the undo", n)
		}
		for i, tn := range w.tenants {
			var d string
			if err := sp.QueryRow(ctx, `SELECT string_agg(detail::text, ';') FROM audit_log WHERE tenant_id = $1 AND action = 'admin.email.reverted' AND actor_id = $2`, tn, w.rows[i]).Scan(&d); err != nil {
				t.Fatal(err)
			}
			// Business 0 had the confirming session and a fresh one; business 1's ended at the change.
			want := `{"outcome": "ok"}`
			if i == 0 {
				want = `{"outcome": "ok", "revoked_sessions": 2}`
			}
			if d != want {
				t.Errorf("business %d's admin.email.reverted detail is %q, want %q", i, d, want)
			}
		}
		if opInt(t, ctx, sp, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'email_reverted' AND detail = '{}'::jsonb`, w.ident) != 1 {
			t.Error("not one email_reverted event with an empty detail")
		}
		if ecRevert(t, ctx, sp, revert).ok {
			t.Error("the undo link was spent twice")
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		w, _, revert := setup(sp)
		ecRewrite(t, ctx, sp, ecRevertFn, [2]string{",\n           confirm_opened_at = NULL, confirm_open_until = NULL, confirm_scope = NULL\n", "\n"})
		if !ecRevert(t, ctx, sp, revert).ok || opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE id = $1 AND confirm_scope IS NOT NULL`, w.ident) != 1 {
			t.Error("MUTATION: with the close cut the window is still closed")
		}
	})
	// Its refusals: unknown, cancelled, spent, expired, a change link, and 6b on the old address.
	for _, c := range []struct {
		name string
		link func(sp pgx.Tx, w ecWorld, revert string) string
	}{
		{"an unknown link", func(pgx.Tx, ecWorld, string) string { return opRandHex(t) }},
		{"a cancelled link", func(sp pgx.Tx, _ ecWorld, r string) string {
			aiExec(t, ctx, sp, `UPDATE admin_identity_verifications SET cancelled_at = now() WHERE token_hash = $1`, r)
			return r
		}},
		{"a spent link", func(sp pgx.Tx, _ ecWorld, r string) string {
			aiExec(t, ctx, sp, `UPDATE admin_identity_verifications SET used_at = now() WHERE token_hash = $1`, r)
			return r
		}},
		{"an expired link", func(sp pgx.Tx, _ ecWorld, r string) string {
			aiExec(t, ctx, sp, `UPDATE admin_identity_verifications SET created_at = now() - interval '8 days', expires_at = now() - interval '1 day' WHERE token_hash = $1`, r)
			return r
		}},
		{"a change link", func(sp pgx.Tx, w ecWorld, _ string) string {
			return w.ask(t, ctx, sp, ecNew())
		}},
	} {
		t.Run("refused/"+c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w := newECWorld(t, ctx, sp, 1)
				revert := opRandHex(t)
				if !ecChange(t, ctx, sp, w.ask(t, ctx, sp, ecNew()), w.session, revert).ok {
					t.Fatal("PREMISE: the change")
				}
				if c.name == "a change link" {
					aiExec(t, ctx, sp, `UPDATE admin_identity_verifications SET cancelled_at = now() WHERE token_hash = $1`, revert)
				}
				h := c.link(sp, w, revert)
				before := ecFootprint(t, ctx, sp, w)
				if ecRevert(t, ctx, sp, h).ok || ecFootprint(t, ctx, sp, w) != before {
					t.Fatal("a refused undo went through or wrote something")
				}
			})
		})
	}
	for _, glyph := range []struct{ name, from, to string }{{"U+0130", "t173", "tİ73"}, {"U+212A", "identity", "Kidentity"}} {
		t.Run("6b/"+glyph.name, func(t *testing.T) {
			plant := func(sp pgx.Tx) (ecWorld, string) {
				w := newECWorld(t, ctx, sp, 1)
				h := opRandHex(t)
				aiExec(t, ctx, sp, `INSERT INTO admin_identity_verifications (identity_id, token_hash, expires_at, purpose, address)
				                    VALUES ($1, $2, now() + interval '1 day', 'revert', $3)`, w.ident, h, strings.Replace(ecNew(), glyph.from, glyph.to, 1))
				return w, h
			}
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w, h := plant(sp)
				before := ecFootprint(t, ctx, sp, w)
				if ecRevert(t, ctx, sp, h).ok || ecFootprint(t, ctx, sp, w) != before {
					t.Fatal("a homoglyph address was given back or something was written")
				}
			})
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				_, h := plant(sp)
				ecRewrite(t, ctx, sp, ecRevertFn, [2]string{ecRuleBlock(t, ctx, sp, ecRevertFn, "v_old"), ""})
				if !ecRevert(t, ctx, sp, h).ok {
					t.Fatal("MUTATION: with 6b cut the homoglyph is still refused")
				}
			})
		})
	}
}

// TestEmailChange00102_EachBodyIsOneUniqueViolationBlock (00084's pattern, ADR 0044 §5.3 step 8): a
// unique_violation the re-checks do not foresee -- here a stand-in UNIQUE (identity_id, event) on the event
// log, its row already there, met by the body's LAST statement -- makes the change and the undo 0 rows with
// NOTHING kept and no error. MUTATIONS, each red: the handler cut (23505 reaches the caller), and the
// handler narrowed to an inner block around the last statement (the writes before it are kept).
func TestEmailChange00102_EachBodyIsOneUniqueViolationBlock(t *testing.T) {
	ctx, tx := opTx(t)
	lockForDowns(t, ctx, tx, []string{"admin_identity_events"})
	for _, c := range []struct {
		name, fn, event string
		last            string // the body's last statement before RETURN QUERY
		prepare         func(sp pgx.Tx, w ecWorld) func() (bool, error)
	}{
		{name: "the change", fn: ecChangeFn, event: "email_changed",
			last: "    INSERT INTO public.admin_identity_events (identity_id, event) VALUES (v_identity, 'email_changed');\n",
			prepare: func(sp pgx.Tx, w ecWorld) func() (bool, error) {
				link := w.ask(t, ctx, sp, ecNew())
				return func() (bool, error) {
					rows, err := ipApp(t, ctx, sp, `SELECT * FROM identity_change_email($1, $2, $3)`, link, w.session, opRandHex(t))
					return len(rows) == 1, err
				}
			}},
		{name: "the undo", fn: ecRevertFn, event: "email_reverted",
			last: "    INSERT INTO public.admin_identity_events (identity_id, event) VALUES (v_identity, 'email_reverted');\n",
			prepare: func(sp pgx.Tx, w ecWorld) func() (bool, error) {
				revert := opRandHex(t)
				if !ecChange(t, ctx, sp, w.ask(t, ctx, sp, ecNew()), w.session, revert).ok {
					t.Fatal("PREMISE: the change")
				}
				return func() (bool, error) {
					rows, err := ipApp(t, ctx, sp, `SELECT * FROM identity_revert_email_change($1)`, revert)
					return len(rows) == 1, err
				}
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			arrange := func(sp pgx.Tx) (ecWorld, func() (bool, error)) {
				w := newECWorld(t, ctx, sp, 2)
				call := c.prepare(sp, w)
				aiExec(t, ctx, sp, `INSERT INTO admin_identity_events (identity_id, event) VALUES ($1, $2)`, w.ident, c.event)
				aiExec(t, ctx, sp, `CREATE UNIQUE INDEX zz_t173_one_event ON admin_identity_events (identity_id, event) WHERE event IN ('email_changed', 'email_reverted')`)
				return w, call
			}
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w, call := arrange(sp)
				before := ecFootprint(t, ctx, sp, w)
				ok, err := call()
				if ok || err != nil || ecFootprint(t, ctx, sp, w) != before {
					t.Fatalf("the stand-in violation: ok=%v err=%v, footprint kept=%v; want 0 rows, no error, nothing kept", ok, err, ecFootprint(t, ctx, sp, w) == before)
				}
			})
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				_, call := arrange(sp)
				ecRewrite(t, ctx, sp, c.fn, [2]string{"EXCEPTION WHEN unique_violation THEN", "EXCEPTION WHEN division_by_zero THEN"})
				if _, err := call(); func() string { code, _ := opCode(err); return code }() != "23505" {
					t.Fatalf("MUTATION: with the handler cut the call answered %v, want 23505", err)
				}
			})
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w, call := arrange(sp)
				before := ecFootprint(t, ctx, sp, w)
				ecRewrite(t, ctx, sp, c.fn, [2]string{"EXCEPTION WHEN unique_violation THEN", "EXCEPTION WHEN division_by_zero THEN"},
					[2]string{c.last, "    BEGIN\n" + c.last + "    EXCEPTION WHEN unique_violation THEN\n        RETURN;\n    END;\n"})
				if ok, err := call(); ok || err != nil || ecFootprint(t, ctx, sp, w) == before {
					t.Fatalf("MUTATION: with the handler narrowed to an inner block nothing was kept (ok=%v err=%v) -- the case does not tell the two apart", ok, err)
				}
			})
		})
	}
}

// ---------------------------------------------------------------- K-E9 (d), K-E16 --

// TestEmailChange00102_OneChangePerUndoWindow (K-E9 (d)): after a change a second request answers
// revert_live with the undo link's end (K-E18 (a)) and writes nothing; once that link expired a request is
// possible again. (The guard's mutation is the request refusals test's "a live undo link".)
func TestEmailChange00102_OneChangePerUndoWindow(t *testing.T) {
	ctx, tx := opTx(t)
	w := newECWorld(t, ctx, tx, 1)
	revert := opRandHex(t)
	got := ecChange(t, ctx, tx, w.ask(t, ctx, tx, ecNew()), w.session, revert)
	if !got.ok {
		t.Fatal("PREMISE: the change")
	}
	rows, err := ipApp(t, ctx, tx, `SELECT change_id, expires_at, refusal FROM identity_request_email_change($1, $2, $3, $4)`, w.session, ecDigest(), opRandHex(t), ecNew())
	if err != nil || len(rows) != 1 || rows[0][0] != nil || rows[0][2] != "revert_live" {
		t.Fatalf("the second request: %v, %v; want one revert_live row", rows, err)
	}
	if at, _ := rows[0][1].(time.Time); !at.Equal(*got.revert) {
		t.Errorf("revert_live's expires_at is %v, want the undo link's end %v (K-E18 (a))", rows[0][1], *got.revert)
	}
	aiExec(t, ctx, tx, `UPDATE admin_identity_verifications SET created_at = now() - interval '7 days', expires_at = now() - interval '1 second' WHERE token_hash = $1`, revert)
	if n, r := ecRequest(t, ctx, tx, w.session, ecDigest(), opRandHex(t), ecNew()); n != 1 || r != "" {
		t.Errorf("after the undo link expired the request answered %d rows, refusal %q; want a link", n, r)
	}
}

// TestEmailChange00102_NoUnlinkWhileAnUndoLinkLives (YÜKSEK-1, K-E16 (a)): change -> a reset at the NEW
// address opens a reset window -> identity_unlink_backfilled answers 0 rows and the row stays linked; the
// undo closes the window; K-E11's reset at the old address opens one again, and then the unlink works.
// MUTATION: the undo-link block cut -> the unlink inside the undo window goes through, red. (Its clock is
// TestEmailChange00102_TheUnlinksUndoClockIsReadAfterTheLocks, committed.)
func TestEmailChange00102_NoUnlinkWhileAnUndoLinkLives(t *testing.T) {
	ctx, tx := opTx(t)
	consumeAt := func(sp pgx.Tx, w ecWorld, recipient string) string {
		h := opRandHex(t)
		if n := ipRows(t, ctx, sp, `SELECT * FROM identity_open_reset($1, $2, now() + interval '1 hour', $3)`, w.ident, h, recipient); n != 1 {
			t.Fatalf("PREMISE: the reset for %s did not open", recipient)
		}
		if n := ipRows(t, ctx, sp, `SELECT * FROM identity_consume_reset($1, $2)`, h, opFakeDigest("q")); n != 1 {
			t.Fatal("PREMISE: the reset was not consumed")
		}
		// A fresh session of the row in the second business (the consumption ended every session).
		fresh := opRandHex(t)
		aiExec(t, ctx, sp, `INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash, authenticated_at) VALUES ($1, $2, $3, clock_timestamp())`, w.tenants[0], w.rows[0], fresh)
		return fresh
	}
	unlink := func(sp pgx.Tx, session string, tenant uuid.UUID) int {
		return ipRows(t, ctx, sp, `SELECT * FROM identity_unlink_backfilled($1, $2)`, session, tenant)
	}
	setup := func(sp pgx.Tx) (ecWorld, string, string) {
		w := newECWorld(t, ctx, sp, 2)
		address, revert := ecNew(), opRandHex(t)
		if !ecChange(t, ctx, sp, w.ask(t, ctx, sp, address), w.session, revert).ok {
			t.Fatal("PREMISE: the change")
		}
		return w, consumeAt(sp, w, address), revert
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		w, fresh, revert := setup(sp)
		if opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE id = $1 AND confirm_scope = 'reset'`, w.ident) != 1 {
			t.Fatal("PREMISE: the reset at the new address opened no reset window")
		}
		if n := unlink(sp, fresh, w.tenants[1]); n != 0 {
			t.Fatalf("an unlink inside the undo window answered %d rows", n)
		}
		if id, _ := ileLinkedTo(t, ctx, sp, w.rows[1]); id != w.ident {
			t.Fatal("the row is no longer linked")
		}
		if !ecRevert(t, ctx, sp, revert).ok {
			t.Fatal("the undo")
		}
		if opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE id = $1 AND confirm_opened_at IS NULL AND confirm_open_until IS NULL AND confirm_scope IS NULL`, w.ident) != 1 {
			t.Error("the undo left the window open")
		}
		fresh = consumeAt(sp, w, w.email)
		if n := unlink(sp, fresh, w.tenants[1]); n != 1 {
			t.Errorf("after K-E11's reset the unlink answered %d rows, want 1", n)
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		w, fresh, _ := setup(sp)
		ecRewrite(t, ctx, sp, ecUnlinkFn, [2]string{"       AND v.expires_at > v_now;\n    IF FOUND THEN\n        RETURN;", "       AND v.expires_at > v_now;\n    IF false THEN\n        RETURN;"})
		if n := unlink(sp, fresh, w.tenants[1]); n != 1 {
			t.Errorf("MUTATION: with the block cut the unlink still answered %d rows", n)
		}
	})
}

// ---------------------------------------------------------------- S-1 --

// TestEmailChange00102_TheResetOpensOnlyForTheAddressTheIdentityHolds (S-1): with a recipient the identity
// no longer holds (byte for byte: a case variant too) identity_open_reset answers 0 rows and writes
// nothing; with its own address it opens; without one (the previous binary's three arguments, the DEFAULT)
// it behaves as 00072's. Through the Go wrapper the refusal is pgx.ErrNoRows. MUTATION: the recipient
// block cut -> a link opens for the address the identity left, red.
func TestEmailChange00102_TheResetOpensOnlyForTheAddressTheIdentityHolds(t *testing.T) {
	ctx, tx := opTx(t)
	open := func(sp pgx.Tx, w ecWorld, recipient *string) int {
		if recipient == nil {
			return ipRows(t, ctx, sp, `SELECT * FROM identity_open_reset($1, $2, now() + interval '1 hour')`, w.ident, opRandHex(t))
		}
		return ipRows(t, ctx, sp, `SELECT * FROM identity_open_reset($1, $2, now() + interval '1 hour', $3)`, w.ident, opRandHex(t), *recipient)
	}
	ptr := func(s string) *string { return &s }
	for _, c := range []struct {
		name      string
		recipient func(w ecWorld) *string
		want      int
	}{
		{"another address", func(ecWorld) *string { return ptr(ecNew()) }, 0},
		{"its own address in another case", func(w ecWorld) *string { return ptr(strings.ToUpper(w.email)) }, 0},
		{"its own address", func(w ecWorld) *string { return ptr(w.email) }, 1},
		{"the previous binary's call", func(ecWorld) *string { return nil }, 1},
	} {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			w := newECWorld(t, ctx, sp, 1)
			before := opInt(t, ctx, sp, `SELECT (SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1) + (SELECT count(*) FROM admin_identity_events WHERE identity_id = $1)`, w.ident)
			if n := open(sp, w, c.recipient(w)); n != c.want {
				t.Errorf("%s: %d rows, want %d", c.name, n, c.want)
			}
			after := opInt(t, ctx, sp, `SELECT (SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1) + (SELECT count(*) FROM admin_identity_events WHERE identity_id = $1)`, w.ident)
			if c.want == 0 && after != before {
				t.Errorf("%s: the refusal wrote %d rows", c.name, after-before)
			}
		})
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		ecRewrite(t, ctx, sp, ecOpenFn, [2]string{"        IF (v_address::text = p_recipient) IS NOT TRUE THEN", "        IF false THEN"})
		w := newECWorld(t, ctx, sp, 1)
		if n := open(sp, w, ptr(ecNew())); n != 1 {
			t.Error("MUTATION: with the recipient test cut a link still did not open for another address")
		}
	})
	if !strings.Contains(openIdentityResetSQL, "identity_open_reset($1, $2, $3, $4)") {
		t.Errorf("openIdentityResetSQL does not pass the recipient: %q", openIdentityResetSQL)
	}
}

// TestOpenIdentityResetSQL_NamesTheRecipient pins the Go side of S-1 (ADR 0044 §6, until T295 takes the
// DEFAULT away): the one statement that calls identity_open_reset passes four placeholders, and no other
// .go file of the module calls the function at all.
func TestOpenIdentityResetSQL_NamesTheRecipient(t *testing.T) {
	if strings.Count(openIdentityResetSQL, "$") != 4 || !strings.Contains(openIdentityResetSQL, "identity_open_reset($1, $2, $3, $4)") {
		t.Fatalf("openIdentityResetSQL = %q, want the four-argument call", openIdentityResetSQL)
	}
	var callers []string
	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && (info.Name() == ".git" || info.Name() == ".tools" || info.Name() == "node_modules") {
			return filepath.SkipDir
		}
		// A nested checkout (cmd/tappa's skipScanDir says why) is another tree's source.
		if info.IsDir() && path != root {
			if _, statErr := os.Lstat(filepath.Join(path, ".git")); statErr == nil {
				return filepath.SkipDir
			}
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "identity_open_reset(") {
			callers = append(callers, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(callers)
	if len(callers) != 1 || filepath.Base(callers[0]) != "identity.go" {
		t.Errorf("identity_open_reset( is named in %v, want internal/db/identity.go alone", callers)
	}
}

// TestOpenIdentityReset_AnAddressTheIdentityLeftIsNoRows: the Go wrapper, on the pool, against an identity
// that no longer holds the recipient -- pgx.ErrNoRows, nothing written (its error carries no address).
func TestOpenIdentityReset_AnAddressTheIdentityLeftIsNoRows(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	owner, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	email := ecNew()
	var ident uuid.UUID
	if err := owner.QueryRow(ctx, `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2) RETURNING id`, email, opFakeDigest("c")).Scan(&ident); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, s := range []string{`DELETE FROM admin_identity_events WHERE identity_id = $1`, `DELETE FROM admin_identity_resets WHERE identity_id = $1`, `DELETE FROM admin_identities WHERE id = $1`} {
			if _, err := owner.Exec(context.Background(), s, ident); err != nil {
				t.Errorf("cleanup %s: %v", s, err)
			}
		}
	})
	left := ecNew()
	_, err = d.OpenIdentityReset(ctx, ident, opRandHex(t), time.Now().Add(time.Hour), left, time.Second)
	if !strings.Contains(fmt.Sprint(err), pgx.ErrNoRows.Error()) || strings.Contains(fmt.Sprint(err), left) {
		t.Fatalf("OpenIdentityReset for an address the identity does not hold: %v; want pgx.ErrNoRows without the address", err)
	}
	if o, err := d.OpenIdentityReset(ctx, ident, opRandHex(t), time.Now().Add(time.Hour), email, time.Second); err != nil || !o.Issued {
		t.Fatalf("OpenIdentityReset for its own address: %+v, %v", o, err)
	}
}

// ---------------------------------------------------------------- the sweep --

// TestEmailChange00103_TheSweepTakesTheEndedAddressLinksAfterADay (K-E1 (iii) (a)): a 'change' / 'revert'
// row spent, cancelled or expired more than a day ago goes; one that ended less than a day ago, or is
// live, stays; 'verify' / 'link' rows of any age stay. The answer is still the event log's count.
// MUTATIONS: the address predicate cut (a verify row goes, red); one DELETE cut (its kind stays, red).
func TestEmailChange00103_TheSweepTakesTheEndedAddressLinksAfterADay(t *testing.T) {
	ctx, tx := opTx(t)
	type row struct {
		name, purpose, set string
		gone               bool
	}
	rows := []row{
		{"change spent two days ago", "change", "used_at = now() - interval '2 days'", true},
		{"revert cancelled two days ago", "revert", "cancelled_at = now() - interval '2 days'", true},
		{"change expired two days ago", "change", "created_at = now() - interval '3 days', expires_at = now() - interval '2 days'", true},
		{"revert spent an hour ago", "revert", "used_at = now() - interval '1 hour'", false},
		{"change cancelled an hour ago", "change", "cancelled_at = now() - interval '1 hour'", false},
		{"revert live", "revert", "", false},
		{"verify spent two days ago", "verify", "used_at = now() - interval '2 days'", false},
		{"link expired two days ago", "link", "created_at = now() - interval '3 days', expires_at = now() - interval '2 days'", false},
	}
	run := func(sp pgx.Tx) map[string]bool {
		w := newECWorld(t, ctx, sp, 1)
		ids := map[string]string{}
		for _, r := range rows {
			h := opRandHex(t)
			var address any
			if r.purpose == "change" || r.purpose == "revert" {
				address = ecNew()
			}
			aiExec(t, ctx, sp, `INSERT INTO admin_identity_verifications (identity_id, token_hash, expires_at, purpose, address) VALUES ($1, $2, now() + interval '1 hour', $3, $4)`, w.ident, h, r.purpose, address)
			if r.set != "" {
				aiExec(t, ctx, sp, `UPDATE admin_identity_verifications SET `+r.set+` WHERE token_hash = $1`, h)
			}
			ids[r.name] = h
		}
		got, err := ipApp(t, ctx, sp, `SELECT deleted FROM identity_sweep_events()`)
		if err != nil || len(got) != 1 {
			t.Fatalf("the sweep: %v, %v", got, err)
		}
		gone := map[string]bool{}
		for name, h := range ids {
			gone[name] = opInt(t, ctx, sp, `SELECT count(*) FROM admin_identity_verifications WHERE token_hash = $1`, h) == 0
		}
		return gone
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		gone := run(sp)
		for _, r := range rows {
			if gone[r.name] != r.gone {
				t.Errorf("%s: gone=%v, want %v", r.name, gone[r.name], r.gone)
			}
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		ecRewrite(t, ctx, sp, ecSweepFn, [2]string{"WHERE v.address IS NOT NULL AND v.used_at < v_links;", "WHERE v.used_at < v_links;"})
		if !run(sp)["verify spent two days ago"] {
			t.Error("MUTATION: without the address predicate the verify row still stays")
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		ecRewrite(t, ctx, sp, ecSweepFn, [2]string{"    DELETE FROM public.admin_identity_verifications AS v WHERE v.address IS NOT NULL AND v.cancelled_at < v_links;\n", ""})
		if run(sp)["revert cancelled two days ago"] {
			t.Error("MUTATION: without the cancelled DELETE the cancelled row still goes")
		}
	})
}

// ---------------------------------------------------------------- the bodies --

// TestEmailChange00102_TheRedefinitionsAreTheOldBodiesPlusTheirBlocks: identity_open_reset is 00072's body
// with the DEFAULT argument, one DECLARE line and the recipient block; identity_unlink_backfilled is
// 00079's with the undo-link block; identity_sweep_events is 00072's with one DECLARE line and the three
// DELETEs -- nothing else changed (ADR 0044 §5.3, the card's items 9a, 9b, 10).
func TestEmailChange00102_TheRedefinitionsAreTheOldBodiesPlusTheirBlocks(t *testing.T) {
	ctx, tx := opTx(t)
	body := func(file, create string) string {
		up, _ := opMigrationSections(t, file)
		i := strings.Index(up, create)
		if i < 0 {
			t.Fatalf("%s holds no %q", file, create)
		}
		a := strings.Index(up[i:], "AS $$") + i + len("AS $$")
		b := strings.Index(up[a:], "\n$$;") + a
		return up[a : b+1]
	}
	prosrc := func(fn string) string {
		var s string
		if err := tx.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE oid = $1::regprocedure`, fn).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	added := regexp.MustCompile(`(?m)^    v_address public\.citext; -- 00102[^\n]*\n|(?s)    -- 00102 \(T173, ADR 0044 §5\.3, S-1\): THE RECIPIENT\..*?    END IF;\n    END IF;\n`)
	if got := added.ReplaceAllString(prosrc(ecOpenFn), ""); got != body("00072_set_identity_passwords_and_link_legacy_rows.sql", "CREATE FUNCTION public.identity_open_reset(") {
		t.Error("identity_open_reset differs from 00072's body by more than its DECLARE line and the recipient block")
	}
	block := regexp.MustCompile(`(?s)    -- 00102 \(T173, ADR 0044 §5\.6, K-E16 \(a\)\): NO UNLINK.*?    IF FOUND THEN\n        RETURN;\n    END IF;\n`)
	if got := block.ReplaceAllString(prosrc(ecUnlinkFn), ""); got != body("00079_list_and_keep_the_confirmation_window.sql", "CREATE OR REPLACE FUNCTION public.identity_unlink_backfilled(") {
		t.Error("identity_unlink_backfilled differs from 00079's body by more than the undo-link block")
	}
	sweep := regexp.MustCompile(`(?m)^    v_links  timestamptz; -- 00103[^\n]*\n|(?s)    -- 00103 \(T173, ADR 0044 §5\.9.*?v\.expires_at < v_links;\n`)
	if got := sweep.ReplaceAllString(prosrc(ecSweepFn), ""); got != body("00072_set_identity_passwords_and_link_legacy_rows.sql", "CREATE FUNCTION public.identity_sweep_events(") {
		t.Error("identity_sweep_events differs from 00072's body by more than its DECLARE line and the three DELETEs")
	}
}

// ---------------------------------------------------------------- RLS --

// TestEmailChange00102_ATenantStillSeesOnlyItsOwnRows: after a change of a person with businesses A and B,
// tappa_app in A's context reads A's row and not B's (the probe carries NO tenant filter: RLS alone answers).
func TestEmailChange00102_ATenantStillSeesOnlyItsOwnRows(t *testing.T) {
	ctx, tx := opTx(t)
	w := newECWorld(t, ctx, tx, 2)
	address := ecNew()
	if !ecChange(t, ctx, tx, w.ask(t, ctx, tx, address), w.session, opRandHex(t)).ok {
		t.Fatal("PREMISE: the change")
	}
	var mine, theirs int64
	if err := aiApp(t, ctx, tx, w.tenants[0], func(sp pgx.Tx) error {
		if err := sp.QueryRow(ctx, `SELECT count(*) FROM admin_users WHERE email = $1::citext AND id = $2`, address, w.rows[0]).Scan(&mine); err != nil {
			return err
		}
		return sp.QueryRow(ctx, `SELECT count(*) FROM admin_users WHERE id = $1`, w.rows[1]).Scan(&theirs)
	}); err != nil {
		t.Fatal(err)
	}
	if mine != 1 || theirs != 0 {
		t.Errorf("tappa_app in A's context: its own row %d (want 1), B's row %d (want 0)", mine, theirs)
	}
}

// ---------------------------------------------------------------- the Downs --

// TestEmailChange00101_TheDownsGoBackByteForByteAndRefuse: 00103's, 00102's and 00101's Downs in turn give
// goose 100's catalogue back (every function's definition and ACL, the two tables' constraints and column
// ACLs, admin_users' and admin_identities' column ACLs), the Ups again give HEAD's back; and EACH Down
// refuses alone -- 00103's first, so `down-to 100` stops before anything is taken back -- over a live undo
// link and, separately, over an address event. MUTATION: a Down's block cut -> it runs over the live link.
func TestEmailChange00101_TheDownsGoBackByteForByteAndRefuse(t *testing.T) {
	ctx, tx := opTx(t)
	files := []string{ec00101File, ec00102File, ec00103File}
	var ups, downs []string
	var tables []string
	for _, f := range files {
		up, down := opMigrationSections(t, f)
		ups, downs = append(ups, up), append(downs, down)
		tables = append(tables, downTables(down)...)
	}
	tables = append(tables, "admin_users", "admin_identities")
	slices.Sort(tables)
	lockForDowns(t, ctx, tx, slices.Compact(tables))
	ecLiftDownRefusals(t, ctx, tx)
	catalogue := func(q pgx.Tx) string {
		var s string
		if err := q.QueryRow(ctx, `SELECT md5(string_agg(x, E'\n' ORDER BY x)) FROM (
		      SELECT 'fn ' || p.oid::regprocedure::text || ' ' || pg_get_userbyid(p.proowner) || ' ' || p.prosecdef || ' ' ||
		             coalesce(array_to_string(p.proconfig, ','), '') || ' ' || coalesce(p.proacl::text, '-') || ' ' || md5(p.prosrc) || ' ' ||
		             pg_get_function_arguments(p.oid) || ' ' || pg_get_function_result(p.oid) AS x
		        FROM pg_proc p WHERE p.pronamespace = 'public'::regnamespace AND p.proname LIKE 'identity\_%'
		      UNION ALL
		      SELECT 'con ' || c.conrelid::regclass::text || ' ' || c.conname || ' ' || pg_get_constraintdef(c.oid)
		        FROM pg_constraint c WHERE c.conrelid IN ('admin_identity_verifications'::regclass, 'admin_identity_events'::regclass)
		      UNION ALL
		      SELECT 'col ' || a.attrelid::regclass::text || '.' || a.attname || ' ' || format_type(a.atttypid, a.atttypmod) || ' ' || coalesce(a.attacl::text, '-')
		        FROM pg_attribute a WHERE a.attrelid IN ('admin_identity_verifications'::regclass, 'admin_identities'::regclass, 'admin_users'::regclass)
		         AND a.attnum > 0 AND NOT a.attisdropped
		      UNION ALL
		      SELECT 'rel ' || c.relname || ' ' || coalesce(c.relacl::text, '-') FROM pg_class c
		       WHERE c.oid IN ('admin_identity_verifications'::regclass, 'admin_identity_events'::regclass)) AS t`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	head := catalogue(tx)
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		for i := len(files) - 1; i >= 0; i-- {
			opRunSection(t, ctx, sp, downs[i], files[i]+" Down")
		}
		at100 := catalogue(sp)
		if opInt(t, ctx, sp, `SELECT count(*) FROM pg_attribute WHERE attrelid = 'admin_identity_verifications'::regclass AND attname IN ('address', 'cancelled_at') AND NOT attisdropped`) != 0 ||
			opInt(t, ctx, sp, `SELECT count(*) FROM pg_proc WHERE proname IN ('identity_request_email_change', 'identity_change_email', 'identity_revert_email_change')`) != 0 ||
			opInt(t, ctx, sp, `SELECT count(*) WHERE to_regprocedure('public.identity_open_reset(uuid,text,timestamptz)') IS NOT NULL`) != 1 ||
			opInt(t, ctx, sp, `SELECT count(*) WHERE has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'email', 'UPDATE')`) != 0 {
			t.Error("after the three Downs a column, a function or a grant of 00101-00103 is still there")
		}
		var check string
		if err := sp.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'admin_identity_verifications_ttl_ceiling'`).Scan(&check); err != nil || check != "CHECK ((expires_at <= (created_at + '24:00:00'::interval)))" {
			t.Errorf("after the Downs the TTL ceiling is %q (%v), want 00072's", check, err)
		}
		for i := range files {
			opRunSection(t, ctx, sp, ups[i], files[i]+" Up again")
		}
		if catalogue(sp) != head {
			t.Error("the Ups again do not give HEAD's catalogue back")
		}
		if at100 == head {
			t.Error("anti-vacuity: the Downs changed nothing the catalogue reads")
		}
	})
	refuses := func(sp pgx.Tx, down, what string) bool {
		inner, err := sp.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = inner.Conn().PgConn().Exec(ctx, down).ReadAll()
		if rerr := inner.Rollback(ctx); rerr != nil {
			t.Fatal(rerr)
		}
		return err != nil && strings.Contains(err.Error(), "an e-mail change can still be undone")
	}
	for _, c := range []struct {
		name  string
		plant func(sp pgx.Tx, w ecWorld)
	}{
		{"a live undo link", func(sp pgx.Tx, w ecWorld) {
			aiExec(t, ctx, sp, `INSERT INTO admin_identity_verifications (identity_id, token_hash, expires_at, purpose, address) VALUES ($1, $2, now() + interval '7 days', 'revert', $3)`, w.ident, opRandHex(t), ecNew())
		}},
		{"an address event", func(sp pgx.Tx, w ecWorld) {
			aiExec(t, ctx, sp, `INSERT INTO admin_identity_events (identity_id, event) VALUES ($1, 'email_change_requested')`, w.ident)
		}},
	} {
		for i := len(files) - 1; i >= 0; i-- {
			t.Run(c.name+"/"+files[i], func(t *testing.T) {
				aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
					w := newECWorld(t, ctx, sp, 1)
					c.plant(sp, w)
					if !refuses(sp, downs[i], c.name) {
						t.Fatalf("%s's Down ran over %s", files[i], c.name)
					}
				})
				aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
					w := newECWorld(t, ctx, sp, 1)
					c.plant(sp, w)
					cut := regexp.MustCompile(`(?s)-- \+goose StatementBegin\nDO \$\$\nBEGIN\n    IF EXISTS \(SELECT 1 FROM public\.admin_identity_verifications AS v.*?-- \+goose StatementEnd\n`)
					if len(cut.FindAllString(downs[i], -1)) != 1 {
						t.Fatalf("PREMISE: %s's Down holds no single refusal block", files[i])
					}
					mutated := cut.ReplaceAllString(downs[i], "")
					if c.name == "an address event" && files[i] == ec00101File {
						// Without the block 00101's Down meets its own CHECK restore (the event set): it still
						// fails, but not with the refusal -- the block is what turns it into one.
						if refuses(sp, mutated, c.name) {
							t.Fatal("MUTATION: the block cut and the refusal is still the block's")
						}
						return
					}
					if refuses(sp, mutated, c.name) {
						t.Fatal("MUTATION: the block cut and the Down still refuses with it")
					}
					inner, err := sp.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					_, err = inner.Conn().PgConn().Exec(ctx, mutated).ReadAll()
					if rerr := inner.Rollback(ctx); rerr != nil {
						t.Fatal(rerr)
					}
					if err != nil {
						t.Fatalf("MUTATION: the block cut and %s's Down failed otherwise (%v) -- the case does not show the block holding it", files[i], err)
					}
				})
			})
		}
	}
}
