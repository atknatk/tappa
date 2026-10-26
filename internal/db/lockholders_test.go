package db

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// lockHolders lists the other sessions holding a granted lock on one of tables: what lockForDowns
// prints when it gives up, so a CI run says which session kept the tables busy.
func lockHolders(t *testing.T, ctx context.Context, tx pgx.Tx, tables []string) string {
	t.Helper()
	const q = `SELECT l.relation::regclass::text, l.mode, a.pid,
	                  coalesce(a.state, 'none'), coalesce(a.wait_event_type, 'none'), coalesce(a.wait_event, 'none'),
	                  coalesce(extract(epoch FROM now() - a.xact_start)::int, -1),
	                  left(coalesce(a.query, 'none'), 160)
	             FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid
	            WHERE l.granted AND a.pid <> pg_backend_pid()
	              AND l.relation = ANY (SELECT to_regclass(x) FROM unnest($1::text[]) AS x)
	            ORDER BY 7 DESC`
	rows, err := tx.Query(ctx, q, tables)
	if err != nil {
		return fmt.Sprintf("(reading pg_locks failed: %v)", err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var rel, mode, state, wtype, wevent, query string
		var pid, age int
		if err := rows.Scan(&rel, &mode, &pid, &state, &wtype, &wevent, &age, &query); err != nil {
			return fmt.Sprintf("(scanning pg_locks failed: %v)", err)
		}
		fmt.Fprintf(&b, "  %s %s pid=%d state=%s wait=%s/%s xact_age=%ds query=%q\n", rel, mode, pid, state, wtype, wevent, age, query)
	}
	if err := rows.Err(); err != nil {
		return fmt.Sprintf("(reading pg_locks failed: %v)", err)
	}
	if b.Len() == 0 {
		return "  (none held now)"
	}
	return b.String()
}
