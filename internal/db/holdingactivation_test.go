package db

// holdingactivation_test.go -- migration 00100 at the database (the user's decision of 2026-11-11;
// ADR 0027 §2's closed list, its "Aktivasyon eşin plaketinde" note): holding_activation_tap advances
// a LIVE peer's plaque through holding_advance_tag_counter's one statement and, only then, writes the
// peer's own trail row -- the plaque's uid and wall, nothing else.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// holdingActivationTapAs calls holding_activation_tap as tappa_app in caller's context (uuid.Nil: none).
func holdingActivationTapAs(t *testing.T, ctx context.Context, tx pgx.Tx, caller uuid.UUID, uid string, ctr int32) (string, *string) {
	t.Helper()
	var (
		outcome string
		status  *string
	)
	if err := holdingAppIn(t, ctx, tx, caller, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT outcome, tag_status FROM public.holding_activation_tap($1, $2)`, uid, ctr).Scan(&outcome, &status)
	}); err != nil {
		t.Fatalf("holding_activation_tap: %v", err)
	}
	return outcome, status
}

// holdingActivationTrail is tenant's trail rows of the activation action (read by the owner).
func holdingActivationTrail(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID) []string {
	t.Helper()
	rows, err := tx.Query(ctx, `SELECT coalesce(actor_id::text, '-') || ' ' || coalesce(target, '-') || ' ' || detail::text
	                              FROM audit_log WHERE tenant_id = $1 AND action = 'holding.activation_tap' ORDER BY at`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestHoldingActivationTap_TrailsOnlyAnAdvance: KM (a live member) on KF's active plaque at 100 --
// 101 advances it and writes KF's ONE row (no actor, the uid, {"location_id": the wall}) and nothing
// in KM's trail; 101 again is a replay (no row); KF's lost plaque is peer_inactive (no row, nothing
// moved); C's plaque (another holding) and no context are not_peer (no row, nothing moved); KF's own
// context on its own plaque is not_peer too (a tenant is not its own peer).
func TestHoldingActivationTap_TrailsOnlyAnAdvance(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	ctrOf := func(uid string) int64 { return opInt(t, ctx, tx, `SELECT last_ctr FROM tags WHERE uid = $1`, uid) }

	if out, status := holdingActivationTapAs(t, ctx, tx, w.km, w.kfTag, 101); out != "advanced" || status == nil || *status != "active" {
		t.Fatalf("KM on KF's plaque at 101: (%s, %v), want (advanced, active)", out, status)
	}
	if got := ctrOf(w.kfTag); got != 101 {
		t.Errorf("KF's counter %d, want 101", got)
	}
	trail := holdingActivationTrail(t, ctx, tx, w.kf)
	if want := fmt.Sprintf(`- %s {"location_id": "%s"}`, w.kfTag, w.kfVenue); len(trail) != 1 || trail[0] != want {
		t.Fatalf("KF's trail %q, want exactly [%q]", trail, want)
	}
	if n := len(holdingActivationTrail(t, ctx, tx, w.km)); n != 0 {
		t.Errorf("KM's trail holds %d activation rows", n)
	}

	for _, c := range []struct {
		name    string
		caller  uuid.UUID
		uid     string
		ctr     int32
		outcome string
	}{
		{"a replay", w.km, w.kfTag, 101, "replay"},
		{"a lost plaque", w.km, w.kfLost, 500, "peer_inactive"},
		{"another holding's plaque", w.km, w.cTag, 500, "not_peer"},
		{"no context", uuid.Nil, w.kfTag, 500, "not_peer"},
		{"its own plaque", w.kf, w.kfTag, 500, "not_peer"},
	} {
		before := ctrOf(c.uid)
		if out, _ := holdingActivationTapAs(t, ctx, tx, c.caller, c.uid, c.ctr); out != c.outcome {
			t.Errorf("%s: %s, want %s", c.name, out, c.outcome)
		}
		if got := ctrOf(c.uid); got != before {
			t.Errorf("%s: the counter moved %d -> %d", c.name, before, got)
		}
	}
	for _, tenant := range []uuid.UUID{w.kf, w.km, w.c} {
		want := map[uuid.UUID]int{w.kf: 1}[tenant]
		if n := len(holdingActivationTrail(t, ctx, tx, tenant)); n != want {
			t.Errorf("%s's trail holds %d activation rows after the refusals, want %d", tenant, n, want)
		}
	}
}

// TestHoldingActivationTap_AnExMemberWithinTheWindowIsNotAPeer: KM left 30 s ago -- inside K-29's
// 60 s window, which is the transactions gate's: the activation's advance is strict, not_peer,
// nothing moved, no row.
func TestHoldingActivationTap_AnExMemberWithinTheWindowIsNotAPeer(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "30 seconds")
	if out, _ := holdingActivationTapAs(t, ctx, tx, w.km, w.kfTag, 101); out != "not_peer" {
		t.Errorf("an ex-member 30 s after leaving: %s, want not_peer", out)
	}
	if got := opInt(t, ctx, tx, `SELECT last_ctr FROM tags WHERE uid = $1`, w.kfTag); got != 100 {
		t.Errorf("KF's counter moved to %d", got)
	}
	if n := len(holdingActivationTrail(t, ctx, tx, w.kf)); n != 0 {
		t.Errorf("KF's trail holds %d activation rows", n)
	}
}

// TestHoldingActivationTap_NoAdvanceWithoutItsRow: the advance and the row are one: with the
// definer's INSERT on audit_log taken back (a savepoint), the call is refused 42501 and KF's counter
// is NOT left advanced -- the peer's counter never moves on this path untrailed.
func TestHoldingActivationTap_NoAdvanceWithoutItsRow(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, `REVOKE INSERT (target) ON audit_log FROM tappa_holdingdefiner`); err != nil {
			t.Fatal(err)
		}
		err := holdingAppIn(t, ctx, sp, w.km, func(q pgx.Tx) error {
			_, err := q.Exec(ctx, `SELECT * FROM public.holding_activation_tap($1, 101)`, w.kfTag)
			return err
		})
		if code, _ := opCode(err); code != sqlstateInsufficientPrivi {
			t.Fatalf("without the trail's grant: %v, want 42501", err)
		}
		if got := opInt(t, ctx, sp, `SELECT last_ctr FROM tags WHERE uid = $1`, w.kfTag); got != 100 {
			t.Errorf("the counter was left at %d with no trail row", got)
		}
	})
}

// holdingActivationSurface is what 00100 changes, read off the catalogue: holding_activation_tap (body
// md5, result, owner, ACL, SECURITY DEFINER, config) or "-", and the definer's INSERT and SELECT
// columns on audit_log.
func holdingActivationSurface(t *testing.T, ctx context.Context, q opQuerier) string {
	t.Helper()
	var s string
	if err := q.QueryRow(ctx, `SELECT coalesce((SELECT md5(prosrc) || ' ' || pg_get_function_result(oid) || ' ' || pg_get_userbyid(proowner) || ' ' ||
	                                 coalesce(array_to_string(proacl, ','), '') || ' ' || prosecdef::text || ' ' || coalesce(array_to_string(proconfig, ','), '')
	                            FROM pg_proc WHERE oid = to_regprocedure('public.holding_activation_tap(character, integer)')), '-')`).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s + " | insert:" + opColumns(t, ctx, q, "tappa_holdingdefiner", "audit_log", "INSERT") +
		" | select:" + opColumns(t, ctx, q, "tappa_holdingdefiner", "audit_log", "SELECT")
}

// TestHolding00100_DownGivesBackAndUpTakesItAgain: 00100's Down drops holding_activation_tap and takes
// the definer's INSERT on audit_log back -- the surface before it -- and its Up gives HEAD's surface
// back exactly. The precondition refuses a second run (the function is there), a definer that already
// reaches audit_log (the Down takes the table's INSERT back whole), and a holding_advance_tag_counter
// that is not 00045's.
func TestHolding00100_DownGivesBackAndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, op00100File)
	pre := holdingPrecondition(t, op00100File)
	head := holdingActivationSurface(t, ctx, tx)
	if !strings.Contains(head, " TABLE(outcome text, tag_status text) tappa_holdingdefiner ") ||
		!strings.Contains(head, "tappa_app=X/tappa_holdingdefiner") || !strings.Contains(head, " true search_path=pg_catalog, pg_temp ") ||
		!strings.HasSuffix(head, " | insert:tenant_id,action,target,detail | select:") {
		t.Fatalf("PREMISE at HEAD: %s", head)
	}
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		opWant(t, holdingNestedRun(t, ctx, sp, pre), sqlstatePrerequisiteState, "00100's precondition at HEAD")
		opRunSection(t, ctx, sp, down, "00100 Down")
		if got := holdingActivationSurface(t, ctx, sp); got != "- | insert: | select:" {
			t.Errorf("after 00100's Down: %s", got)
		}
		if err := holdingNestedRun(t, ctx, sp, pre); err != nil {
			t.Fatalf("00100's precondition at 00099: %v", err)
		}
		for _, c := range []struct{ name, ddl string }{
			{"a holding_activation_tap already there", `CREATE FUNCTION public.holding_activation_tap() RETURNS boolean LANGUAGE sql AS 'SELECT false'`},
			{"a definer that already inserts into audit_log", `GRANT INSERT (action) ON audit_log TO tappa_holdingdefiner`},
			{"a definer that already reads audit_log", `GRANT SELECT (id) ON audit_log TO tappa_holdingdefiner`},
			{"an advance that is not 00045's", `DROP FUNCTION public.holding_advance_tag_counter(character, integer)`},
		} {
			// A plain savepoint inside the locked one (holdingInSavepoint would read the outer
			// savepoint's Down locks as its own).
			bad, err := sp.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := bad.Exec(ctx, c.ddl); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			opWant(t, holdingNestedRun(t, ctx, bad, pre), sqlstatePrerequisiteState, "00100's precondition over "+c.name)
			if err := bad.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
		}
		opRunSection(t, ctx, sp, up, "00100 Up again")
		if got := holdingActivationSurface(t, ctx, sp); got != head {
			t.Errorf("after the Up again:\n %s\nwant HEAD's\n %s", got, head)
		}
	})
}
