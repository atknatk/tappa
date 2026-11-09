package tenant

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestNameBreaksIsolation_IsTheSchemasCheck (T216): tenants_name_keeps_its_isolate (migration
// 00093) and NameBreaksIsolation are one rule over the corpus internal/handler/operator's
// TestShownName_TheSetIsTheDomains walks -- every code point but the surrogates -- each inside a name
// ("Rusty" + r + "Bar"). The predicate is the CATALOGUE's (pg_get_expr of the stored constraint),
// evaluated by the database, so the test reads what the table enforces rather than a copy of it.
// U+0000 is the one rune the two cannot agree on and need not: text cannot hold it (chr(0) is
// refused), so the database's corpus starts at U+0001 and the Go rule's own answer for it is checked
// apart. CONTROL: 75 code points are refused, the 76 of the Go set less U+0000.
func TestNameBreaksIsolation_IsTheSchemasCheck(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping (real Postgres required)")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(context.Background()); err != nil {
			t.Logf("close: %v", err)
		}
	})
	var expr string
	var validated bool
	if err := conn.QueryRow(ctx, `SELECT pg_get_expr(conbin, conrelid), convalidated FROM pg_constraint
	                               WHERE conrelid = 'public.tenants'::regclass AND conname = 'tenants_name_keeps_its_isolate'`).Scan(&expr, &validated); err != nil {
		t.Fatalf("read tenants_name_keeps_its_isolate: %v (migration 00093 not applied?)", err)
	}
	if !validated {
		t.Error("tenants_name_keeps_its_isolate is NOT VALID")
	}
	rows, err := conn.Query(ctx, `SELECT g.r FROM generate_series(1, 1114111) AS g(r)
	                               CROSS JOIN LATERAL (SELECT 'Rusty' || chr(g.r) || 'Bar' AS name) AS s
	                               WHERE (g.r < 55296 OR g.r > 57343) AND NOT (`+expr+`)`)
	if err != nil {
		t.Fatalf("evaluate the constraint over the code points: %v", err)
	}
	refused, err := pgx.CollectRows(rows, pgx.RowTo[int32])
	if err != nil {
		t.Fatal(err)
	}
	byDB := make(map[rune]bool, len(refused))
	for _, r := range refused {
		byDB[rune(r)] = true
	}
	for r := rune(1); r <= 0x10FFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		if goSays := NameBreaksIsolation("Rusty" + string(r) + "Bar"); goSays != byDB[r] {
			t.Errorf("U+%04X: NameBreaksIsolation says %v, the schema %v", r, goSays, byDB[r])
		}
	}
	if !NameBreaksIsolation("Rusty\x00Bar") {
		t.Error("U+0000: NameBreaksIsolation lets it through (the schema never sees it)")
	}
	if len(byDB) != 75 {
		t.Errorf("CONTROL: the schema refuses %d code points, want 75", len(byDB))
	}
}
