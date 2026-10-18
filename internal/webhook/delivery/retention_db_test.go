package delivery

// retention_db_test.go -- T130: the sweep's number is the database's number. The query takes the
// retention as a parameter (RetentionDays); 00058's restrictive policy writes its own 90 into the
// catalogue. Two spellings of one rule must not drift: a Go constant moved without the policy would
// still be held by the policy (deleting less than the panel and the guide promise), a policy moved
// without the constant would delete what the panel still shows.

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestWebhookRetention_TheQueryAndThePolicyAgree reads webhook_deliveries_delete_settled from the
// catalogue: RESTRICTIVE, FOR DELETE, its USING the context's tenant (NULLIF form, CLAUDE.md §6), the
// two settled states and settled_at older than exactly RetentionDays by statement_timestamp() -- the
// clock the query's bound reads. And the sweep's batch is a positive bound.
//
// MUTANTS (measured, T130): RetentionDays = 9 -> red; the policy's interval written '9 days' -> red.
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
	var permissive, cmd, qual string
	if err := c.QueryRow(ctx, `SELECT permissive, cmd, qual FROM pg_policies
	                           WHERE schemaname = 'public' AND tablename = 'webhook_deliveries' AND policyname = 'webhook_deliveries_delete_settled'`).
		Scan(&permissive, &cmd, &qual); err != nil {
		t.Fatalf("the delete policy: %v", err)
	}
	if permissive != "RESTRICTIVE" || cmd != "DELETE" {
		t.Errorf("the delete policy is %s FOR %s, want RESTRICTIVE FOR DELETE", permissive, cmd)
	}
	for _, want := range []string{
		"(tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid)",
		"(state = ANY (ARRAY['delivered'::text, 'dead'::text]))",
		"(settled_at < (statement_timestamp() - '" + strconv.Itoa(RetentionDays) + " days'::interval))",
	} {
		if !regexp.MustCompile(regexp.QuoteMeta(want)).MatchString(qual) {
			t.Errorf("the delete policy's USING %s has no %s", qual, want)
		}
	}
	if RetentionDays != 90 {
		t.Errorf("RetentionDays is %d; the user's decision (2026-10-18) is 90", RetentionDays)
	}
	if sweepMax <= 0 || sweepMax > 1000 {
		t.Errorf("sweepMax %d: a sweep's batch must be a small positive bound", sweepMax)
	}
}
