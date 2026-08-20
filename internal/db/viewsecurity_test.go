package db

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// viewsecurity_test.go -- THE GATE FOR THE VIEW CLASS (CLAUDE.md section 4.5).
//
// 🔴 WHY IT EXISTS: THE CLASS HAD NO GATE AT ALL, AND A SCRIPT SAID IT DID.
// scripts/redline-check.sh's R5b greps db/migrations for `CREATE VIEW` and, until
// this file, wrote that section 4.5's real doors were "code review, pg-restore-verify.sh
// and the RLS isolation suite". Measured on this tree, all three miss a view:
//
//	pg-restore-verify.sh ... does not run in the migration loop at all (CI runs
//	                         make up/migrate/seed/check/audit; that script is called
//	                         only from the operator's restore step), AND its own
//	                         object counts are relkind='r', so a view is not counted
//	                         even when it does run
//	rls_test.go ............ TestRLS_ReadIsolation_AllTables walks a list of 17 TABLE
//	                         names; nothing in the suite looked at pg_views
//	db.New's boot probe .... measures what the CONNECTING role may reach; it says
//	                         nothing about a view being evaluated with its OWNER's
//	                         rights
//
// And the hole is not theoretical. A view is evaluated with its DEFINER's rights
// unless it carries `security_invoker = true` -- so a view the table owner creates
// over a tenant-scoped table hands every tenant's rows to tappa_app, while
// `make audit`, `make check` and CI all stay green.
//
// 🔴 WHY A CATALOGUE TEST RATHER THAN A PATTERN. The exemption R5b uses is the text
// `security_invoker = true` appearing in the statement, and that is forgeable: a view
// whose first column is the string literal 'security_invoker = true' satisfies the
// grep and is a DEFINER view. This test reads pg_class.reloptions, which is the
// value PostgreSQL actually acts on, so the spoof cannot reach it -- and the spoof is
// driven below as a case rather than described. Same for `ALTER VIEW ... SET
// (security_invoker = false)` afterwards: reloptions carries the current truth.
//
// 🔴 AND THE LIST IS DERIVED, NEVER WRITTEN. The query walks pg_rewrite/pg_depend
// from every view to the relations it reads and keeps the ones whose base relation
// has row level security enabled -- so a view added by a migration nobody remembers
// is covered on the day it lands (M6-06's "a net that carries a list by hand lives as
// long as the list is maintained").
//
// 🔴 AND THE LIMIT OF "THE GATE FOR THE VIEW CLASS", MEASURED RATHER THAN GUESSED
// (4th round). A view can reach a tenant-scoped table WITHOUT depending on it, by
// going through a SECURITY DEFINER function, and then nothing below sees it. Measured
// on this database, all inside one BEGIN ... ROLLBACK, with SET LOCAL ROLE tappa_app
// and a foreign app.tenant_id:
//
//	CREATE FUNCTION zzf() RETURNS TABLE(id uuid, tenant_id uuid) SECURITY DEFINER
//	  LANGUAGE sql AS $f$ SELECT id, tenant_id FROM transactions $f$;
//	CREATE VIEW zzv AS SELECT * FROM zzf();
//
//	direct SELECT ...... 0 rows
//	SELECT FROM zzv .... 408 188 rows / 81 777 tenants
//	this catalogue gate  does NOT list zzv
//
// The discriminator was measured too: `RETURNS SETOF transactions` IS caught, because
// pg_depend records the function's dependency on the table's composite TYPE and the
// rewrite rule then points at the table; `RETURNS TABLE(...)` names its columns
// individually and depends on nothing. So the escape is one specific spelling, not
// the whole definer-function family.
//
// It is COUNTED rather than closed, for the reason scripts/redline-check.sh gives for
// the same shape: the product's OWN tag resolver is a SECURITY DEFINER function (ADR
// 0002 madde 7), there are ten legitimate `security definer` statements on a clean
// tree, and no catalogue predicate distinguishes the legitimate resolver from a
// malicious one. What distinguishes them is code review. Saying so here is the point:
// a gate that names itself THE gate has to name what it is not.

// unsafeView is one finding: a view that reads a relation with row level security
// enabled and is NOT evaluated with the CALLER's rights.
type unsafeView struct {
	name    string // schema-qualified
	kind    string // "view" or "materialized view"
	options string // pg_class.reloptions, joined -- empty when there are none
	bases   string // the RLS-enabled relations it reads
}

func (u unsafeView) String() string {
	return fmt.Sprintf("%s (%s) over %s [reloptions: %q]", u.name, u.kind, u.bases, u.options)
}

// rlsDependentViewsQuery lists every view and materialized view in a non-system
// schema that reads at least one relation with row level security enabled, together
// with its reloptions.
//
// ⚠️ DIRECT DEPENDENCIES ARE ENOUGH, AND THE ARGUMENT WAS CORRECTED IN THE 4th ROUND
// AFTER BEING MEASURED. It used to say "the definer link in the chain is always
// reported", and that sentence is FALSE: `t.relkind IN ('r','p')` keeps base
// RELATIONS only, so an outer view over an inner view is not listed at all, in either
// role. What is true is the thing that matters, and it was measured in one
// BEGIN ... ROLLBACK under SET LOCAL ROLE tappa_app with a foreign app.tenant_id: an
// outer DEFINER view over an inner INVOKER function/view returns the SAME 0 rows as a
// direct SELECT, because the inner link is where the rights are decided. So the outer
// view is not a finding, and not reporting it is right for that reason rather than
// for the reason previously written down. Where the chain DOES leak -- an inner
// SECURITY DEFINER function -- nothing here sees it either, which is the counted
// limit recorded in this file's header.
//
// ⚠️ relkind IN ('r','p'): an ordinary table and a PARTITIONED table both carry
// relrowsecurity, and a policy on a partitioned parent is the shape a growing
// transactions table would take.
const rlsDependentViewsQuery = `
	SELECT vn.nspname || '.' || v.relname                             AS view_name,
	       v.relkind,
	       COALESCE(array_to_string(v.reloptions, ','), '')            AS opts,
	       string_agg(DISTINCT tn.nspname || '.' || t.relname, ', ')   AS bases
	  FROM pg_class v
	  JOIN pg_namespace vn ON vn.oid = v.relnamespace
	  JOIN pg_rewrite rw   ON rw.ev_class = v.oid
	  JOIN pg_depend d     ON d.objid = rw.oid
	                      AND d.classid = 'pg_rewrite'::regclass
	  JOIN pg_class t      ON t.oid = d.refobjid
	  JOIN pg_namespace tn ON tn.oid = t.relnamespace
	 WHERE v.relkind IN ('v', 'm')
	   AND vn.nspname NOT IN ('pg_catalog', 'information_schema')
	   AND t.relkind IN ('r', 'p')
	   AND t.relrowsecurity
	   AND t.oid <> v.oid
	 GROUP BY 1, 2, 3
	 ORDER BY 1`

// scanRLSDependentViews runs the catalogue query on q and returns one finding per
// view that is NOT evaluated with the caller's rights.
//
// A materialized view can NEVER be safe here: PostgreSQL has no security_invoker for
// one, so it is always reported. That is the honest answer rather than a silent pass.
func scanRLSDependentViews(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
},
) ([]unsafeView, error) {
	rows, err := q.Query(ctx, rlsDependentViewsQuery)
	if err != nil {
		return nil, fmt.Errorf("scan rls-dependent views: %w", err)
	}
	defer rows.Close()

	var out []unsafeView
	for rows.Next() {
		var name, opts, bases string
		var kind uint8
		if err := rows.Scan(&name, &kind, &opts, &bases); err != nil {
			return nil, fmt.Errorf("scan rls-dependent views: %w", err)
		}
		v := unsafeView{name: name, options: opts, bases: bases, kind: "view"}
		if kind == 'm' {
			v.kind = "materialized view"
		}
		if kind == 'v' && securityInvokerOn(opts) {
			continue
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan rls-dependent views: %w", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// securityInvokerOn reads pg_class.reloptions rather than the view's source text.
//
// PostgreSQL normalises a boolean relopt to `name=value`, and it accepts the whole
// boolean vocabulary (true/on/1/yes and the bare name, which means true). All of
// them are accepted here for the same reason internal/config canonicalises before
// comparing: a check that sees a different spelling than the server acts on is the
// class this repository has paid three findings for.
func securityInvokerOn(reloptions string) bool {
	for _, opt := range strings.Split(reloptions, ",") {
		opt = strings.TrimSpace(strings.ToLower(opt))
		name, value, hasValue := strings.Cut(opt, "=")
		if strings.TrimSpace(name) != "security_invoker" {
			continue
		}
		if !hasValue {
			return true // a bare relopt name is true
		}
		switch strings.TrimSpace(value) {
		case "true", "on", "1", "yes":
			return true
		}
		return false
	}
	return false
}

// unsafeViewsRefusal turns a scan result into the failure the gate reports, or nil.
//
// 🔴 IT IS A FUNCTION SO THAT THE POSITIVE CONTROL CAN DRIVE THE SHIPPED REFUSAL
// RATHER THAN A COPY OF IT. The gate below reads a COMMITTED schema, and the control
// creates its probe views inside a transaction it rolls back -- so the control can
// never make the gate itself go red without leaving a view behind on a shared
// database. Sharing the refusal is what closes that gap: the control asserts that
// THIS function, given the catalogue as the probe left it, returns an error. A
// checker that re-types the thing it checks stops checking the moment the two drift
// (roleFactsQuery carries the same argument).
func unsafeViewsRefusal(found []unsafeView) error {
	if len(found) == 0 {
		return nil
	}
	var b strings.Builder
	for _, v := range found {
		fmt.Fprintf(&b, "\n  %s", v)
	}
	return fmt.Errorf("%d view(s) read a relation with row level security enabled WITHOUT "+
		"security_invoker, so they are evaluated with their owner's rights and hand every "+
		"tenant's rows to whoever may select from them (CLAUDE.md §4.5):%s\n"+
		"Fix: add WITH (security_invoker = true), or drop the view. A materialized view "+
		"cannot be made safe this way and must not read a tenant-scoped relation",
		len(found), b.String())
}

// TestViews_OverTenantScopedTablesAreEvaluatedAsTheCaller is the gate itself: on
// this schema there is either no view over a tenant-scoped relation, or every such
// view carries security_invoker.
func TestViews_OverTenantScopedTablesAreEvaluatedAsTheCaller(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	found, err := scanRLSDependentViews(context.Background(), app.pool)
	if err != nil {
		t.Fatalf("catalogue scan: %v", err)
	}
	if err := unsafeViewsRefusal(found); err != nil {
		t.Fatal(err)
	}
}

// TestViews_TheCatalogueGateActuallyFires is the POSITIVE CONTROL, and without it
// the test above is satisfied by a schema that simply has no views -- which this one
// does, so the assertion would otherwise be vacuous forever.
//
// 🔴 EVERY CASE RUNS INSIDE ONE BEGIN ... ROLLBACK on the owner pool. The views are
// created, measured and thrown away; nothing is committed to the shared development
// database. The owner pool is used because creating a view needs CREATE on the
// schema, which the application role deliberately does not have.
//
// The third case is the one that made a catalogue test necessary rather than a
// pattern: a DEFINER view whose first column is a string that spells the exemption.
// A scanner reading the statement text passes it; reloptions does not have it.
func TestViews_TheCatalogueGateActuallyFires(t *testing.T) {
	owner := ownerDB(t)
	assertOwnerRole(t, owner)

	ctx := context.Background()
	tx, err := owner.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	// The rollback is the whole safety property of this test: no probe view survives
	// it, whatever happens below.
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !strings.Contains(err.Error(), "closed") {
			t.Errorf("rollback: %v", err)
		}
	}()

	// A control measurement first: whatever the schema already has, the probes must
	// change the answer RELATIVE to it.
	before, err := scanRLSDependentViews(ctx, tx)
	if err != nil {
		t.Fatalf("baseline scan: %v", err)
	}
	baseline := map[string]bool{}
	for _, v := range before {
		baseline[v.name] = true
	}

	for _, tc := range []struct {
		name       string
		ddl        string
		view       string
		wantCaught bool
	}{
		{
			name:       "a plain view over a tenant-scoped table is caught",
			ddl:        `CREATE VIEW zz_probe_definer AS SELECT * FROM public.transactions`,
			view:       "public.zz_probe_definer",
			wantCaught: true,
		},
		{
			name: "the same view WITH security_invoker is not caught",
			ddl: `CREATE VIEW zz_probe_invoker WITH (security_invoker = true) AS ` +
				`SELECT * FROM public.transactions`,
			view: "public.zz_probe_invoker",
		},
		{
			name: "a view that only SPELLS the exemption in a column is caught",
			ddl: `CREATE VIEW zz_probe_spoof AS ` +
				`SELECT 'security_invoker = true'::text AS looks_safe, t.* FROM public.transactions t`,
			view:       "public.zz_probe_spoof",
			wantCaught: true,
		},
		{
			name: "an invoker view turned back into a definer view is caught",
			ddl: `CREATE VIEW zz_probe_flipped WITH (security_invoker = true) AS ` +
				`SELECT * FROM public.transactions;` + "\n" +
				`ALTER VIEW zz_probe_flipped SET (security_invoker = false)`,
			view:       "public.zz_probe_flipped",
			wantCaught: true,
		},
		{
			name:       "a materialized view can never carry security_invoker and is caught",
			ddl:        `CREATE MATERIALIZED VIEW zz_probe_matview AS SELECT * FROM public.transactions`,
			view:       "public.zz_probe_matview",
			wantCaught: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A savepoint per case, so one probe cannot leave state for the next and a
			// failing probe does not abort the enclosing transaction.
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatalf("savepoint: %v", err)
			}
			defer func() { _ = sp.Rollback(ctx) }()

			if _, err := sp.Exec(ctx, tc.ddl); err != nil {
				t.Fatalf("probe DDL: %v", err)
			}
			found, err := scanRLSDependentViews(ctx, sp)
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			var caught bool
			for _, v := range found {
				if v.name == tc.view {
					caught = true
				}
				if !baseline[v.name] && v.name != tc.view {
					t.Errorf("the scan reported %s, which this case did not create", v)
				}
			}
			if caught != tc.wantCaught {
				t.Fatalf("%s caught = %v, want %v — the catalogue gate does not do what "+
					"TestViews_OverTenantScopedTablesAreEvaluatedAsTheCaller claims it does",
					tc.view, caught, tc.wantCaught)
			}
			// AND THE SHIPPED REFUSAL ITSELF, not just the scan: this is the exact
			// function the gate above calls, fed the catalogue as this probe left it.
			refusal := unsafeViewsRefusal(found)
			switch {
			case tc.wantCaught && refusal == nil:
				t.Fatalf("the scan found %s but the shipped refusal stayed silent — the gate "+
					"would be green with this view in the schema", tc.view)
			case tc.wantCaught && !strings.Contains(refusal.Error(), tc.view):
				t.Errorf("the refusal does not name %s:\n%v", tc.view, refusal)
			case !tc.wantCaught && refusal != nil && strings.Contains(refusal.Error(), tc.view):
				t.Fatalf("the shipped refusal names a SAFE view:\n%v", refusal)
			}
		})
	}
}
