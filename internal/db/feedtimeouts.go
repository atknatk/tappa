package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// feedTimeout is one server setting the customer pool sends at connection startup and reads back.
type feedTimeout struct {
	name, want string
	// tcp marks a TCP keepalive setting: on a Unix-socket connection the server has no socket
	// option to set and reports 0 whatever was sent (measured), so 0 is accepted there.
	tcp bool
}

// feedTimeouts bound a STUCK WRITER (ADR 0028 §5, M11 API-2). A punch writes its feed event under
// its tenant's counter row lock until commit; a transaction that stops talking while holding it
// makes every other ok tap of that tenant -- and, through a holding punch's two counters, of its
// peer -- wait out the triggers' lock_timeout (2 s) and fail "try again" (ADR 0027 §13 item 23).
// Measured in the 3rd round: 8 of a tenant's 8 writers waited behind one idle transaction.
//
//   - idle_in_transaction_session_timeout 30s -- equal to the request timeout
//     (internal/httpx/router.go): no legitimate path holds a transaction idle longer than a
//     request lives, and the server ends the session (and its locks) after it;
//   - tcp_keepalives_idle 30 · interval 10 · count 3 -- the SERVER probes the client's socket, so
//     a pod that vanished mid-transaction (node loss, a network partition) is noticed in about a
//     minute, not after the kernel's two hours.
//
// 🔴 THEY ARE STARTUP PARAMETERS OF THIS POOL, NOT `ALTER ROLE ... SET`. A role setting is not in
// pg_dump's output (roles are cluster objects) and would be lost on a restore without a word (ADR
// 0028 §5); a startup parameter is sent by every connection this binary opens, outranks a role or
// database default, and is READ BACK when the pool opens -- a pool on which a value did not land
// (a pooler that drops startup parameters) is refused, the way the log_parameter_max_length_on_error
// pin refuses a connection (logparams.go).
var feedTimeouts = []feedTimeout{
	{name: "idle_in_transaction_session_timeout", want: "30s"},
	{name: "tcp_keepalives_idle", want: "30", tcp: true},
	{name: "tcp_keepalives_interval", want: "10", tcp: true},
	{name: "tcp_keepalives_count", want: "3", tcp: true},
}

// readFeedTimeoutsSQL reads the four settings and whether the connection is a Unix socket
// (inet_server_addr() is NULL there).
const readFeedTimeoutsSQL = `SELECT pg_catalog.current_setting('idle_in_transaction_session_timeout'),
       pg_catalog.current_setting('tcp_keepalives_idle'),
       pg_catalog.current_setting('tcp_keepalives_interval'),
       pg_catalog.current_setting('tcp_keepalives_count'),
       pg_catalog.inet_server_addr() IS NULL`

// FeedTimeoutsNotSetError is a pool on which one of feedTimeouts did not land.
type FeedTimeoutsNotSetError struct {
	Name, Got, Want string
}

func (e *FeedTimeoutsNotSetError) Error() string {
	return fmt.Sprintf("db: %s is %q on the pool's connection, not %q. The pool sends it as a connection startup parameter "+
		"so that a transaction left open by a stuck or vanished client cannot hold a tenant's feed counter -- and every "+
		"other tap of that tenant -- for longer than a request lives (ADR 0028 §5); a different value means the startup "+
		"parameter did not take effect as sent (for example behind a connection pooler that does not forward startup "+
		"parameters), and the pool is refused rather than run without it", e.Name, e.Got, e.Want)
}

// pinFeedTimeouts puts feedTimeouts into cfg's startup parameters -- replacing any spelling of the
// same key the DSN carried. It writes no hook: pinLogParameters' AfterConnect is the one per-
// connection check (execmodetypes_test.go pins that), and the read-back is newDB's, at open
// (requireFeedTimeouts) -- ADR 0028 §5's "açılışta SHOW ile doğrulanır".
func pinFeedTimeouts(cfg *pgxpool.Config) {
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	for _, ft := range feedTimeouts {
		for k := range cfg.ConnConfig.RuntimeParams {
			if strings.EqualFold(k, ft.name) {
				delete(cfg.ConnConfig.RuntimeParams, k)
			}
		}
		cfg.ConnConfig.RuntimeParams[ft.name] = ft.want
	}
}

// requireFeedTimeouts reads feedTimeouts back on a connection of the pool and refuses the pool
// when one did not land. Every connection of a pool is opened from the same startup parameters,
// so one read stands for the pool; a later SET on a pooled connection is the application's own
// code, and none issues one.
func (d *DB) requireFeedTimeouts(ctx context.Context) error {
	c, err := d.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("db: read the stuck-writer timeouts: %w", err)
	}
	defer c.Release()
	got := make([]string, len(feedTimeouts))
	var unixSocket bool
	if err := c.QueryRow(ctx, readFeedTimeoutsSQL).Scan(&got[0], &got[1], &got[2], &got[3], &unixSocket); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			return fmt.Errorf("db: reading the stuck-writer timeouts failed (SQLSTATE %s), so the pool is refused: a "+
				"check that could not run has not passed", pg.Code)
		}
		return fmt.Errorf("db: reading the stuck-writer timeouts: %w", err)
	}
	for i, ft := range feedTimeouts {
		if got[i] == ft.want || (ft.tcp && unixSocket && got[i] == "0") {
			continue
		}
		return &FeedTimeoutsNotSetError{Name: ft.name, Got: got[i], Want: ft.want}
	}
	return nil
}
