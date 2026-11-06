package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OperatorStatementTimeout bounds every statement of the operator's pool (T98). Before it only the
// billing read was bounded; the other reads were limited by MFA and the session's read budget, and
// a slow one held a connection and the operator's request for as long as it ran.
//
// WHY 15 SECONDS, the billing read's own arithmetic (operator.go, TenantBillingReadTimeout):
//
//	the slowest read outside billing (audit log by kind, 2 M rows), max of 3             0.7 s
//	the slowest billing read measured (T95; it keeps its own bound of the same number)   6.1 s
//	x 2 headroom                                                                         12.2 s  <= 15 s
//	the request's own deadline (httpx.RequestTimeout) / 2: the bound fires first, with
//	room for the session predicate, the first phase and the render, so a slow statement
//	is the database's 57014 and the screen's designed 503 -- not the router's deadline   15 s
//
// Measured 2026-11-06 on a disposable database built far past any real business (backlog T98 has its
// size): every other read under 120 ms. The writes are not bound by it: each runs under a 10 s
// context of its handler's.
//
// A STATEMENT PAST IT LEAVES THE TRAIL TRUE (CLAUDE.md section 4.5): a read's 'read' row commits in
// its first phase, so a second phase cut off keeps the row and returned nothing; a first phase cut
// off rolls back with no ticket, so nothing can follow it; a write and its row are one statement,
// so they go together. TestOperatorReads_ACutOffReadLeavesTheTrailTrue and
// TestOperatorWrites_ACutOffWriteLeavesNeitherTheChangeNorItsRow measure it.
//
// 🔴 A STARTUP PARAMETER OF THIS POOL, NOT `ALTER ROLE ... SET`, for pinFeedTimeouts' reasons: a role
// setting is not in pg_dump's output and would be lost on a restore without a word, and a startup
// parameter is read back when the pool opens. It bounds statements only; a transaction held idle by
// someone who has the DSN is T108's, which is a role-level question.
const OperatorStatementTimeout = 15 * time.Second

// operatorStatementTimeoutSetting spells the bound as the server reports it.
var operatorStatementTimeoutSetting = strconv.FormatInt(int64(OperatorStatementTimeout/time.Second), 10) + "s"

// readOperatorStatementTimeoutSQL is qualified with pg_catalog for roleFactsQuery's reason (pool.go).
const readOperatorStatementTimeoutSQL = `SELECT pg_catalog.current_setting('statement_timeout')`

// pinOperatorStatementTimeout puts the bound into cfg's startup parameters, replacing any spelling
// of the key the DSN carried. Like pinFeedTimeouts it writes no hook; the read-back is the opener's.
func pinOperatorStatementTimeout(cfg *pgxpool.Config) {
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	for k := range cfg.ConnConfig.RuntimeParams {
		if strings.EqualFold(k, "statement_timeout") {
			delete(cfg.ConnConfig.RuntimeParams, k)
		}
	}
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = operatorStatementTimeoutSetting
}

// requireOperatorStatementTimeout reads the bound back on a connection of the pool and refuses the
// pool when it did not land (a pooler that drops startup parameters would do that). A read that
// could not run has not passed. The refusal stops the boot like the role gate's: the log parameter
// pin of the same connection would refuse that pooler too, so this adds no new way to stop it.
func requireOperatorStatementTimeout(ctx context.Context, pool *pgxpool.Pool) error {
	var got string
	if err := pool.QueryRow(ctx, readOperatorStatementTimeoutSQL).Scan(&got); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			return fmt.Errorf("db: operator pool: reading statement_timeout failed (SQLSTATE %s), so the pool is "+
				"refused: a check that could not run has not passed", pg.Code)
		}
		return operatorStepErr("read statement_timeout", err)
	}
	if got != operatorStatementTimeoutSetting {
		return fmt.Errorf("db: operator pool: statement_timeout is %q on the pool's connection, not %q. The pool sends "+
			"it as a connection startup parameter so that no operator statement runs without a bound; a different "+
			"value means the parameter did not take effect as sent, and the pool is refused rather than run without it",
			got, operatorStatementTimeoutSetting)
	}
	return nil
}
