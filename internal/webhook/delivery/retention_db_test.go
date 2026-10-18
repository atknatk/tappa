package delivery

// retention_db_test.go -- T130: the sweep's number is the database's number. The query takes the
// retention as a parameter (RetentionDays); 00058's restrictive policy writes its own 90 into the
// catalogue. Two spellings of one rule must not drift: a Go constant moved without the policy would
// still be held by the policy (deleting less than the panel and the guide promise), a policy moved
// without the constant would delete what the panel still shows.

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestWebhookRetention_TheQueryAndThePolicyAgree reads webhook_deliveries_delete_settled from the
// catalogue: RESTRICTIVE, FOR DELETE, its USING the context's tenant (NULLIF form, CLAUDE.md §6), the
// two settled states and settled_at older than exactly RetentionDays by statement_timestamp() -- the
// clock the query's bound reads. And the sweep's batch is a positive bound.
//
// MUTANTS (measured, T130): RetentionDays = 9 -> red; the policy's interval written '9 days' -> red;
// (fix round 1) `OR settled_at IS NULL` appended to the USING (F1) -> red.
func TestWebhookRetention_TheQueryAndThePolicyAgree(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping (real Postgres required)")
	}
	ctx := context.Background()
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	var permissive, cmd, qual, roles string
	var check *string
	if err := c.QueryRow(ctx, `SELECT permissive, cmd, qual, with_check, roles::text FROM pg_policies
	                           WHERE schemaname = 'public' AND tablename = 'webhook_deliveries' AND policyname = 'webhook_deliveries_delete_settled'`).
		Scan(&permissive, &cmd, &qual, &check, &roles); err != nil {
		t.Fatalf("the delete policy: %v", err)
	}
	if permissive != "RESTRICTIVE" || cmd != "DELETE" || check != nil || roles != "{public}" {
		t.Errorf("the delete policy is %s FOR %s TO %s (WITH CHECK %v), want RESTRICTIVE FOR DELETE TO public, no WITH CHECK",
			permissive, cmd, roles, check)
	}
	// 🔴 THE WHOLE USING, BY EQUALITY (T130 fix round 1). The first round looked for the three
	// conjuncts as substrings, and `... OR settled_at IS NULL` appended to the policy (mutant F1) kept
	// all three and stayed green -- a restrictive policy that admits a pending row. The catalogue's
	// deparse (pg_get_expr) is one spelling per server major, so the expected text is that deparse
	// of exactly the three conjuncts; whitespace is collapsed on both sides, nothing else.
	want := "((tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid)" +
		" AND (state = ANY (ARRAY['delivered'::text, 'dead'::text]))" +
		" AND (settled_at < (statement_timestamp() - '" + strconv.Itoa(RetentionDays) + " days'::interval)))"
	if norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }; norm(qual) != norm(want) {
		t.Errorf("the delete policy's USING is\n %s\nwant exactly\n %s", qual, want)
	}
	if RetentionDays != 90 {
		t.Errorf("RetentionDays is %d; the user's decision (2026-10-18) is 90", RetentionDays)
	}
	if sweepMax <= 0 || sweepMax > 1000 {
		t.Errorf("sweepMax %d: a sweep's batch must be a small positive bound", sweepMax)
	}
}
