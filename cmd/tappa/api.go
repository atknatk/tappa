package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/api"
	"github.com/atknatk/tappa/internal/httpx"
)

// The start-up line's attribute: exactly one of the two values is logged by every process (the
// operator surface's reasoning -- a positive fact either way, never an absence).
const (
	integrationAPIState = "integration_api"
	integrationAPIOff   = "off"
	integrationAPIOn    = "on"
)

// openAPISurface wires the integration API's /v1 surface (M11 API-4, ADR 0028 §3, §13) -- or, with
// no TAPPA_API_HOST, nothing: /v1 then answers 404 on every host (the router has no such route) and
// the start-up line says so.
//
// 🔴 THE CHAIN IS BUILT WITH THE PANEL'S PREFIX. apikey.NewAuthenticator takes apikey.LivePrefix and
// cfg.APIKeyHMACKey -- the two values run() hands apikey.NewKeys -- so a key the owner's panel issues
// is a key this chain accepts, and a key of the other build's prefix (tt_test_, the sandbox's) is
// refused by the shape check before the database (plan §5, API-3's hand-over). API-11 changes BOTH
// to the build's prefix together; TestAPIWiring_TheChainTakesThePanelsPrefixAndTheOneWorker pins the
// pair.
//
// The suspension checkpoint is api.NoSuspensionColumn until OP-15 adds tenants.suspended_at
// (internal/handler/api/suspension.go; API-4b).
func openAPISurface(cfg *config.Config, data *db.DB, work api.WorkNoticer, log *slog.Logger) (httpx.Mounter, error) {
	if cfg.APIHost == "" {
		log.Info("the integration API is off: TAPPA_API_HOST is not set, so /v1 answers 404 on every host",
			integrationAPIState, integrationAPIOff)
		return nil, nil
	}
	chain, err := apikey.NewAuthenticator(apikey.LivePrefix, cfg.APIKeyHMACKey, data, data)
	if err != nil {
		return nil, err
	}
	writes, err := apikey.NewWrites(data)
	if err != nil {
		return nil, err
	}
	surface, err := api.New(api.Config{
		Host: cfg.APIHost, Keys: chain, Writes: writes, Suspension: api.NoSuspensionColumn{}, Work: work, Log: log,
	})
	if err != nil {
		return nil, err
	}
	log.Info("the integration API is on; /v1 is served on its own host",
		integrationAPIState, integrationAPIOn, "api_host", cfg.APIHost)
	return surface, nil
}

// drainers runs several shutdown drains at once and returns when all are done: the reset outbox and
// K-19's worker drain beside the HTTP server (shutdown's concurrency argument), inside one budget.
type drainers []drainer

// Drain drains every member concurrently under ctx and joins their errors.
func (ds drainers) Drain(ctx context.Context) error {
	errs := make(chan error, len(ds))
	for _, d := range ds {
		go func() { errs <- d.Drain(ctx) }()
	}
	var all []error
	for range ds {
		all = append(all, <-errs)
	}
	return errors.Join(all...)
}
