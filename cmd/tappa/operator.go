package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/signup"
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
// configuredSurface (whose syntax tree uses it exactly seven times: as operatorauth.New's
// Store, through operatorAuthenticator, as operator.New's LegalStore -- OP-10 --, as its
// TenantStore -- OP-11 --, as its PlaqueStore -- OP-13 --, as its AuditStore -- OP-14 --,
// as its BillingStore -- OP-12 -- and as its VATStore -- OP-16),
// and its Close is returned as a bound method value -- which carries one call and no
// object, so nothing in run() can hand the pool to a customer handler (ADR 0021 §3.6: the
// operator's pool goes to the operator's side only).
// TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator reads the three functions' syntax
// trees for exactly that.
//
// texts is the customer side's legal snapshot (internal/domain/legal.Store, on
// tappa_app's pool) -- the SAME store the public pages read (the wiring test pins one
// legal.NewStore in the command): the operator's legal screen refreshes it after a
// publication, and again when a view finds it behind the version list. It
// goes the other way -- from run() into the operator surface -- and carries two methods
// (operator.LegalTexts: Published, Refresh).
//
// vies is the sign-up wizard's VIES checker -- the SAME value run() hands handler.NewSignup,
// the process's only outbound HTTP client (main.go, where it is built) -- and it reaches the
// surface only through operatorVIES, as the VAT re-check's VATChecker (OP-16).
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
func openOperatorSurface(ctx context.Context, cfg *config.Config, texts operator.LegalTexts, vies viesChecker, log *slog.Logger) (*operator.Surface, func(), error) {
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
	surface, err := configuredSurface(pool, texts, vies, cfg, log)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return surface, pool.Close, nil
}

// operatorStore is what the operator's pool is to the surface: the Authenticator's
// Store, the legal screen's LegalStore (OP-10), the tenant screens' TenantStore (OP-11),
// the plaque screen's PlaqueStore (OP-13), the audit screen's AuditStore (OP-14), the
// billing screen's BillingStore (OP-12) and the VAT screen's VATStore (OP-16) --
// *db.OperatorDB's method set (TestOperatorDB_IsTheStoreAndNothingMore derives it from these
// seven and Close).
type operatorStore interface {
	operatorauth.Store
	operator.LegalStore
	operator.TenantStore
	operator.PlaqueStore
	operator.AuditStore
	operator.BillingStore
	operator.VATStore
}

// viesChecker is what this command needs of the sign-up wizard's VIES client
// (*signup.Checker): its one method, which answers in signup's three values and returns no
// error. Declared here, at the consumer, so the wiring tests can hand in a fake.
type viesChecker interface {
	Check(ctx context.Context, normalisedVAT string) signup.VATStatus
}

// operatorVIES adapts the sign-up wizard's VIES client to the operator surface's VATChecker
// (M10 OP-16, the orchestrator's K16-6: the client stays in internal/domain/signup, and the
// surface names its own three-valued answer). It is WIRING, not a rule: one value in, one
// value out, and signup's two answers that are a verdict are the only ones that become one --
// signup.VATUnknown and every value signup does not name become operator.VATAnswerUnknown,
// which the surface never writes (TestOperatorVIES_OnlyAVerdictBecomesAVerdict). It logs
// nothing and keeps no number.
type operatorVIES struct{ c viesChecker }

// CheckVAT asks VIES through the sign-up wizard's client.
func (a operatorVIES) CheckVAT(ctx context.Context, number string) operator.VATAnswer {
	switch a.c.Check(ctx, number) {
	case signup.VATValid:
		return operator.VATAnswerValid
	case signup.VATInvalid:
		return operator.VATAnswerInvalid
	default:
		return operator.VATAnswerUnknown
	}
}

// viesForOperator is the surface's VATChecker around vies. A nil vies is a nil VATChecker, so
// operator.New refuses it -- a configured surface without its VIES client is not built.
func viesForOperator(vies viesChecker) operator.VATChecker {
	if vies == nil {
		return nil
	}
	return operatorVIES{c: vies}
}

// configuredSurface builds the operator's Authenticator around store and the surface
// that holds it -- store again as its legal store, its tenant store, its plaque store, its
// audit store, its billing store and its VAT store, vies (adapted) as its VIES client, texts
// as its legal snapshot -- and announces the configured state. The process keeps the
// *Authenticator, inside the Surface (m10-platform.md, OP-4 block, OP-7 decision (3)).
//
// It is split from openOperatorSurface so the configured path can be driven without a
// tappa_operator login (that role is NOLOGIN on a development database); store is
// openOperatorSurface's pool in production.
func configuredSurface(store operatorStore, texts operator.LegalTexts, vies viesChecker, cfg *config.Config, log *slog.Logger) (*operator.Surface, error) {
	auth, err := operatorAuthenticator(store, cfg, log)
	if err != nil {
		return nil, err
	}
	surface, err := operator.New(auth, store, store, store, store, store, store, viesForOperator(vies), texts, cfg.OperatorHost,
		cfg.BaseURL, log)
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
