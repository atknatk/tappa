// Package db owns the single pgx connection pool and the tenant-scoped
// transaction helper that is the ONLY way the application reaches Postgres.
//
// Handlers never see *pgxpool.Pool. They go through WithTenant (tenant.go),
// which sets the tenant context inside a transaction so RLS is always in force
// (CLAUDE.md §4.5, ADR 0002 madde 2). Making the pool unreachable from handlers
// turns "did we set the tenant context?" from a discipline question into a
// structural guarantee.
package db

import (
	"context"
	"fmt"

	"github.com/atknatk/tappa/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps the application connection pool. It connects as tappa_app
// (NOSUPERUSER, NOBYPASSRLS, not the table owner — ADR 0002 madde 1) via
// cfg.DatabaseURL, so RLS applies to every query it runs.
type DB struct {
	pool *pgxpool.Pool
}

// New builds the pool from cfg.DatabaseURL and verifies connectivity with a
// ping. Pool sizing is read from the connection string (pgx honours
// pool_max_conns, pool_min_conns, pool_max_conn_lifetime, ... as query
// parameters); absent those, pgx defaults apply. Keeping pool tuning in the DSN
// means config carries it without this package growing knobs it does not need.
func New(ctx context.Context, cfg *config.Config) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: parse DATABASE_URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("db: new pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Close drains and closes the pool. Call it once during graceful shutdown.
func (d *DB) Close() { d.pool.Close() }

// Tenant resolution (ADR 0002 madde 7) is intentionally NOT provided here.
//
// The two context-less lookups (resolve_session_by_token_hash, resolve_tag_by_uid)
// run WITHOUT a tenant context — they are what *produces* the tenant id — so
// they cannot go through WithTenant. Their queries do not exist yet: they are
// defined as SECURITY DEFINER functions in the M1-04/M1-05 migrations and called
// via the generated store in M1-08 (db/queries/resolve.sql). Adding a bounded
// "context-less" pool accessor now would ship an unused API surface whose exact
// shape depends on the store/Querier type that M1-08 introduces, and a loose one
// risks becoming the general bypass door ADR 0002 forbids. So M1-07 keeps
// WithTenant as the sole DB entry point; M1-08 adds a narrow, named resolver
// accessor (which must NOT call set_config) when the resolve queries land.
