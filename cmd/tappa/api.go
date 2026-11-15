package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
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
// 🔴 THE CHAIN IS BUILT WITH THE PANEL'S PREFIX. apikey.NewAuthenticator takes apiKeyPrefix and
// cfg.APIKeyHMACKey -- the two values run() hands apikey.NewKeys -- so a key the owner's panel issues
// is a key this chain accepts, and a key of the other build's prefix is refused by the shape check
// before the database (plan §5, API-3's hand-over). apiKeyPrefix is the BUILD's (M11 API-11):
// apikey.LivePrefix (tt_live_) in nosandbox.go, apikey.SandboxPrefix (tt_test_) in sandbox.go;
// TestAPIWiring_TheChainTakesThePanelsPrefixAndTheOneWorker pins the pair.
//
// extensions are the sandbox build's /v1 additions (sandboxExtensions) -- none in production.
//
// The suspension checkpoint reads OP-15's column (tenants.suspended_at, 00063) through
// tenant.WriteSuspension, in each write's own transaction (internal/handler/api/suspension.go; M11
// API-4b; TestAPIWiring_TheSuspensionReaderFollowsTheColumn).
//
// PUT /v1/locations (API-5) is handed venues -- run()'s *tenant.Venues, the PANEL's venue domain
// with the panel's audit recorder -- so a venue the API writes passes the rules a manager's does
// and leaves its trail in the same table (TestAPIWiring_TheChainTakesThePanelsPrefixAndTheOneWorker).
//
// The employee endpoints (API-6) are handed staff -- run()'s *tenant.Staff, the PANEL's employee
// domain -- for the same reason: an employee the API writes passes the panel's address and name
// rules and leaves the panel's trail rows (TestAPIWiring_AnEmployeeRunsThroughTheShippedSurfaceAndTheQueue
// measures the whole path, the invitation queue included).
//
// GET /v1/punches (API-7) reads the feed through a tenant.PunchFeed built HERE over the same pool:
// it is a read with no trail and no rule of the panel's to share, so there is nothing of run()'s to
// hand it (TestAPIWiring_ThePunchFeedRunsThroughTheShippedSurface reads it through this surface).
//
// GET /v1/directory (API-8) reads through a tenant.EmployeeDirectory built here the same way -- a read,
// no trail (TestAPIWiring_TheDirectoryRunsThroughTheShippedSurface).
//
// GET /v1/timesheets (API-14, ADR 0046) is handed timesheets -- run()'s *ledger.Reader, the PANEL's
// report reader -- so the API's hours are the report's, computed by the same engine.
//
// OAuth access tokens (issue #37, ADR 0047 §8.1) are resolved by grants -- run()'s *oauth.Server, the
// authorization server the panel's consent and the token endpoint share -- so a token the token
// endpoint mints under TAPPA_OAUTH_HMAC_KEY is one this chain resolves under the same key. GET
// /v1/business reads through a tenant.BusinessCard built here.
func openAPISurface(cfg *config.Config, data *db.DB, work api.WorkNoticer, venues api.Locations, staff api.Employees, extensions []api.Extension, timesheets api.Timesheets, log *slog.Logger, grants api.Grants) (httpx.Mounter, error) {
	if cfg.APIHost == "" {
		log.Info("the integration API is off: TAPPA_API_HOST is not set, so /v1 answers 404 on every host",
			integrationAPIState, integrationAPIOff)
		return nil, nil
	}
	chain, err := apikey.NewAuthenticator(apiKeyPrefix, cfg.APIKeyHMACKey.Reveal(), data, data)
	if err != nil {
		return nil, err
	}
	writes, err := apikey.NewWrites(data)
	if err != nil {
		return nil, err
	}
	punches, err := tenant.NewPunchFeed(data)
	if err != nil {
		return nil, err
	}
	directory, err := tenant.NewEmployeeDirectory(data)
	if err != nil {
		return nil, err
	}
	business, err := tenant.NewBusinessCard(data)
	if err != nil {
		return nil, err
	}
	surface, err := api.New(api.Config{
		Host: cfg.APIHost, Keys: chain, Grants: grants, Writes: writes, Suspension: tenant.WriteSuspension{}, Work: work, Locations: venues,
		Employees: staff, Punches: punches, Directory: directory, Timesheets: timesheets, Business: business, Extensions: extensions, Log: log,
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
