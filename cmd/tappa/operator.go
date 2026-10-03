package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// operatorDialTimeout bounds the operator pool's start-up dial, as the customer pool's
// is bounded in run(): an unreachable operator database must not hang the boot (when it
// times out, the surface is unavailable and the boot goes on -- openOperatorSurface).
const operatorDialTimeout = 10 * time.Second

// The start-up line's attribute: exactly one of the two values is logged by every
// process, so the operator surface runbook (deploy/README.md) reads a POSITIVE fact
// either way rather than an absence -- announceKEKRotationWindow's reasoning.
const (
	operatorSurfaceKey         = "operator_surface"
	operatorSurfaceOff         = "off"
	operatorSurfaceConfigured  = "configured"
	operatorSurfaceUnavailable = "unavailable"
)

// openOperatorSurface wires the platform operator's surface (M10 OP-7) and returns it
// with the function that closes what it opened.
//
// 🔴 THE OPERATOR'S POOL NEVER LEAVES THIS FUNCTION. It is opened here, handed to
// configuredSurface (whose syntax tree uses it exactly twice: as operatorauth.New's Store,
// through operatorAuthenticator, and as operator.New's LegalStore -- OP-10), and its Close
// is returned as a bound method value -- which carries one call and no object, so nothing
// in run() can hand the pool to a customer handler (ADR 0021 §3.6: the operator's pool
// goes to the operator's side only). TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator
// reads the three functions' syntax trees for exactly that.
//
// texts is the customer side's legal snapshot (internal/domain/legal.Store, on
// tappa_app's pool) -- the SAME store the public pages read (the wiring test pins one
// legal.NewStore in the command): the operator's legal screen refreshes it after a
// publication, and again when a view finds it behind the version list. It
// goes the other way -- from run() into the operator surface -- and carries two methods
// (operator.LegalTexts: Published, Refresh).
//
// THREE OUTCOMES, AND WHICH ONE STOPS THE BOOT IS THE DECISION:
//
//   - OFF, not an error: no operator variable is set (internal/config refuses a partial
//     set before this runs). /operator answers 503 and nothing is opened.
//   - UNAVAILABLE, not an error: the configuration is complete but the operator's
//     database could not be REACHED (db.ErrOperatorUnreachable -- the network, name
//     resolution, a timeout, a cancelled start-up, and the closed list of server answers
//     in internal/db's unreachableSQLSTATEs:
//     connection exception, authentication, a missing database, too many connections,
//     a shutdown or a start-up in progress). The surface answers 503, the line is an ERROR, and
//     the customer product serves. The customer product's availability must not hang on
//     the operator's credential (ADR 0020 risk 7, ADR 0021 §4: "müşteri paneli
//     etkilenmez"). The case it was decided on is a restore onto a fresh cluster:
//     01-roles.sql creates tappa_operator NOLOGIN again and a database dump carries no
//     role password, so a configured DSN fails authentication (28P01 -- which
//     internal/db's unreachability test drives with a wrong password; the restore itself
//     was not run).
//   - EVERY OTHER FAILURE STOPS THE BOOT: a server that answered with any other code
//     (42501, 22023, 42704, ...), a TLS failure or an error internal/db does not
//     recognise, a multi-attempt connection of which even one attempt was not
//     unreachable, a refusal of what WAS reached (the role gate, the log-parameter
//     read-back), a malformed DSN, a key of the wrong size.
//     Those are a configuration that is present and dangerous or broken, and must be
//     loud -- the customer pool's rule; with the Deployment's maxUnavailable: 0 the
//     previous pod keeps serving while the new one refuses.
func openOperatorSurface(ctx context.Context, cfg *config.Config, texts operator.LegalTexts, log *slog.Logger) (*operator.Surface, func(), error) {
	if !cfg.OperatorSurfaceConfigured() {
		log.Info("operator surface is off: no TAPPA_OPERATOR_* variable is set, so /operator answers 503 and the customer product is unaffected",
			operatorSurfaceKey, operatorSurfaceOff)
		return operator.Off(), func() {}, nil
	}
	dialCtx, cancel := context.WithTimeout(ctx, operatorDialTimeout)
	defer cancel()
	pool, err := db.NewOperatorDB(dialCtx, cfg)
	if errors.Is(err, db.ErrOperatorUnreachable) {
		log.Error("operator surface is unavailable: its database could not be reached at start-up; /operator answers 503 "+
			"until a restart after the cause is fixed, and the customer product is unaffected",
			operatorSurfaceKey, operatorSurfaceUnavailable, "err", err)
		return operator.Unavailable(), func() {}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	surface, err := configuredSurface(pool, texts, cfg, log)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return surface, pool.Close, nil
}

// operatorStore is what the operator's pool is to the surface: the Authenticator's
// Store and the legal screen's LegalStore (OP-10) -- *db.OperatorDB's method set
// (TestOperatorDB_IsTheStoreAndNothingMore derives it from these two and Close).
type operatorStore interface {
	operatorauth.Store
	operator.LegalStore
}

// configuredSurface builds the operator's Authenticator around store and the surface
// that holds it -- store again as its legal store, texts as its legal snapshot -- and
// announces the configured state. The process keeps the *Authenticator, inside the
// Surface (m10-platform.md, OP-4 block, OP-7 decision (3)).
//
// It is split from openOperatorSurface so the configured path can be driven without a
// tappa_operator login (that role is NOLOGIN on a development database); store is
// openOperatorSurface's pool in production.
func configuredSurface(store operatorStore, texts operator.LegalTexts, cfg *config.Config, log *slog.Logger) (*operator.Surface, error) {
	auth, err := operatorAuthenticator(store, cfg, log)
	if err != nil {
		return nil, err
	}
	surface, err := operator.New(auth, store, texts, cfg.OperatorHost, cfg.BaseURL, log)
	if err != nil {
		return nil, err
	}
	log.Info("operator surface is configured; /operator is served on the operator host",
		operatorSurfaceKey, operatorSurfaceConfigured, "operator_host", cfg.OperatorHost)
	return surface, nil
}

// operatorAuthenticator is operatorauth.New with this deployment's two keys.
//
// THE KEYS BECOME operatorauth.Keys HERE (m10-platform.md, OP-4 block, OP-7 decision
// (2)): internal/config holds them as raw bytes like every other key, and NewKey copies
// them into the redacting type at the one place they are handed over. The
// operatorauth.Config is a literal argument and is not kept anywhere. Which key goes
// into which slot is pinned by TestOperatorAuthenticator_EachKeyIsInItsOwnSlot: a TOTP
// secret sealed under the token key (or the reverse) would still sign people in, and
// would quietly undo the separation config.Load enforces.
func operatorAuthenticator(store operatorauth.Store, cfg *config.Config, log *slog.Logger) (*operatorauth.Authenticator, error) {
	return operatorauth.New(store, operatorauth.Config{
		TOTPKEK:      operatorauth.NewKey(cfg.OperatorTOTPKEK),
		TokenHMACKey: operatorauth.NewKey(cfg.OperatorTokenHMACKey),
		Log:          log,
	})
}
