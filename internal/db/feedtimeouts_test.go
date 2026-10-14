package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/config"
)

// feedtimeouts_test.go -- M11 API-2 (ADR 0028 §5 "Takılı yazıcı"; plan §5 API-2 (i)): the customer
// pool's stuck-writer timeouts, the triggers' lock_timeout, and the role gate's "member of any
// role" (ADR 0028 §4 "Rollerin yaşam döngüsü").

// TestFeedTimeouts_ShownAtStartup is plan §5 API-2 (i):
//   - a connection of the pool db.New builds reads (SHOW, current_setting) every one of
//     feedTimeouts at its value -- idle_in_transaction_session_timeout 30s, TCP keepalives 30/10/3
//     (0 accepted on a Unix socket only) -- and the idle timeout equals the request timeout;
//   - a DSN that asks for other values does not get them (the startup parameter is the pool's);
//   - a connection on which a value did not land (SET after startup, the before hook) is
//     REFUSED at open -- FeedTimeoutsNotSetError, no pool handed back; CONTROL: the same open
//     without the hook succeeds;
//   - and the triggers' lock_timeout is what a stuck writer meets: KF's counter row held FOR UPDATE
//     in another session, an ok tap's INSERT fails 55P03 inside punch_feed after ~2 s (the
//     function's attribute, not the session's 20 s) and writes nothing.
func TestFeedTimeouts_ShownAtStartup(t *testing.T) {
	app, _ := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	read := func(d *DB) map[string]string {
		t.Helper()
		got := map[string]string{}
		c, err := d.pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Release()
		for _, ft := range feedTimeouts {
			var v string
			if err := c.QueryRow(ctx, "SHOW "+ft.name).Scan(&v); err != nil {
				t.Fatal(err)
			}
			got[ft.name] = v
		}
		return got
	}
	for what, dsn := range map[string]string{
		"the pool as configured": app,
		"a DSN asking for no idle timeout and no keepalives": withParam(withParam(app, "idle_in_transaction_session_timeout", "0"),
			"tcp_keepalives_idle", "0"),
	} {
		d, err := New(ctx, &config.Config{DatabaseURL: dsn})
		if err != nil {
			t.Fatalf("%s: New: %v", what, err)
		}
		got := read(d)
		d.Close()
		for _, ft := range feedTimeouts {
			if got[ft.name] != ft.want {
				t.Errorf("%s: SHOW %s = %q, want %q", what, ft.name, got[ft.name], ft.want)
			}
		}
	}
	if idle, err := time.ParseDuration(feedTimeouts[0].want); err != nil || idle != 30*time.Second {
		t.Errorf("the idle timeout %q is not the request timeout's 30 s (internal/httpx)", feedTimeouts[0].want)
	}
	unset := func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, `SET idle_in_transaction_session_timeout = 0`)
		return err
	}
	d, err := newDB(ctx, &config.Config{DatabaseURL: app}, unset)
	var ns *FeedTimeoutsNotSetError
	if err == nil || !errors.As(err, &ns) || ns.Name != "idle_in_transaction_session_timeout" || ns.Got != "0" {
		if d != nil {
			d.Close()
		}
		t.Fatalf("a connection without the idle timeout opened the pool (err %v)", err)
	}
	if d != nil {
		t.Fatal("the pool refused AND returned a pool")
	}
	ok, err := newDB(ctx, &config.Config{DatabaseURL: app}, nil)
	if err != nil {
		t.Fatalf("CONTROL: the pool does not open at all: %v", err)
	}
	ok.Close()

	// The trigger's lock_timeout, met by a writer behind a held counter.
	f := holdingLiveFixture(t)
	kf := f.tenant(t, "Europe/Malta", "KF")
	emp := holdingNewEmployee(t, f.ctx, f.owner, kf, "active")
	holder := holdingBegin(t, f.ctx, f.conn(t), "")
	if _, err := holder.Exec(f.ctx, `SELECT 1 FROM feed_counters WHERE tenant_id = $1 FOR UPDATE`, kf); err != nil {
		t.Fatal(err)
	}
	writer := holdingBegin(t, f.ctx, f.conn(t), "tappa_app")
	if _, err := writer.Exec(f.ctx, `SELECT set_config('app.tenant_id', $1, true)`, kf.String()); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = writer.Exec(f.ctx, `INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type) VALUES ($1, $2, now(), 'ok', 'qr', 'in')`, kf, emp)
	took := time.Since(start)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "55P03" || !strings.Contains(pg.Where, "punch_feed") {
		t.Fatalf("an ok tap behind a held counter: %v, want punch_feed's 55P03", err)
	}
	if took < 1900*time.Millisecond || took > 5*time.Second {
		t.Errorf("the wait was %v, want the trigger's 2 s (the session's lock_timeout is 20 s)", took)
	}
	if err := holder.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if n := opInt(t, f.ctx, f.owner, `SELECT count(*) FROM transactions WHERE employee_id = $1`, emp); n != 0 {
		t.Errorf("%d rows written by the refused INSERT", n)
	}
}

// TestRoleGate_MemberOfAnyRoleIsRefused is ADR 0028 §4's working gate for a stray
// `GRANT tappa_feedwriter TO tappa_app` after the migrations: tappa_feedwriter obeys RLS, so the
// gate's InheritsPrivilege (superuser or BYPASSRLS membership) does not see it -- MemberOfARole
// does. roleFactsQuery, run as tappa_app inside a rolled-back transaction: false as shipped; true
// once tappa_app is a member of the feed writer -- and roleRefusal then refuses in production;
// CONTROL: the same facts outside production open (the development warning).
func TestRoleGate_MemberOfAnyRoleIsRefused(t *testing.T) {
	ctx, tx := opTx(t)
	facts := func() RoleFacts {
		var f RoleFacts
		if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
			return sp.QueryRow(ctx, roleFactsQuery).Scan(&f.User, &f.Super, &f.BypassRLS, &f.OwnsScopedTable, &f.InheritsPrivilege, &f.MemberOfARole)
		}); err != nil {
			t.Fatal(err)
		}
		f.Session = f.User
		return f
	}
	if f := facts(); f.MemberOfARole || f.Privileged() {
		t.Fatalf("tappa_app as shipped: %+v -- it must be a member of nothing", f)
	}
	if _, err := tx.Exec(ctx, `GRANT tappa_feedwriter TO tappa_app`); err != nil {
		t.Fatal(err)
	}
	f := facts()
	if !f.MemberOfARole || f.InheritsPrivilege || !f.Privileged() {
		t.Fatalf("tappa_app a member of tappa_feedwriter: %+v -- want MemberOfARole (and not InheritsPrivilege: the feed writer obeys RLS)", f)
	}
	err := roleRefusal(f, true)
	if err == nil || !strings.Contains(err.Error(), "member_of_any_role=true") {
		t.Errorf("roleRefusal in production: %v, want a refusal naming member_of_any_role", err)
	}
	if roleRefusal(f, false) != nil {
		t.Error("CONTROL: outside production the gate warns, it does not refuse")
	}
}
