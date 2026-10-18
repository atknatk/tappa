// Package sandbox is the integration sandbox's own code (M11 API-11; ADR 0028 §11, K-3): the tap
// simulation, POST /v1/sandbox/taps, and the virtual plaques it taps.
//
// 🔴 ONLY THE SANDBOX BUILD CONTAINS IT. cmd/tappa's sandbox.go (`//go:build sandbox`) is the one
// importer; the production binary is built without the tag, so this package is not in its
// dependency graph at all (cmd/tappa's TestPackaging_ProdBuildHasNoSandboxPackage and
// TestPackaging_SandboxBuildHasIt read `go list -deps` both ways). New refuses any configuration
// but TAPPA_ENV=sandbox as well, and that value only the sandbox build accepts (internal/config).
//
// 🔴 A SIMULATED TAP IS A REAL TAP FROM THE PLAQUE ON. A sandbox has no wall and no phone, so this
// package stands in for exactly two things -- the chip's touch and the employee's session -- and
// for nothing after them:
//
//	the chip      the venue's VIRTUAL plaque (plaque.go) has a real AES key, wrapped under the
//	              sandbox's own TAPPA_TAG_KEK like every plaque key (CLAUDE.md §4.7); the
//	              simulation unwraps it and mints the SUN URL the chip would have produced for its
//	              next read (sun.MintTapPath -- the construction sun.Verify checks)
//	the page      the minted URL goes through sun.Parse and the tap page's own pre-check,
//	              sun.PreviewWithoutReplayProtection, which resolves the plaque through the
//	              context-less resolver and checks the CMAC
//	the button    checkin.Service.Record -- the atomic counter advance (§4.4: one UPDATE, a replay
//	              is a reject), the per-person lock and debounce (ADR 0006), the policy engine and
//	              tap.Decide, the INSERT into transactions, and every trigger behind it: the punch
//	              feed, the holding projection, the webhook delivery rows
//
// So nothing is skipped and nothing is written by hand: a simulated tap may be ok, flag, reject or
// ignored for the same reasons a real one is (a venue the API opened has no evidence and records
// `flag` until the panel gives it some -- plan §2 item 10), and every one of them is recorded
// (§4.6). transactions is only INSERTed into, by Record (§4.3).
//
// WHAT THE SIMULATION ASSERTS THAT A REAL TAP PROVES: the session (§5 row 3). The key's business
// names the employee by its ref; whether that person ever activated is not asked -- a sandbox has no
// plaque to activate at. The "where" is the API caller's own address (the tap page's SourceIP rule),
// so a venue whose address ranges include the caller's records `ok`; there is no GPS.
//
// AUTHORITY: the key's business, and only it (ADR 0028 §2, §11). The chain has already refused a
// key whose business the sandbox operator has not opened (K-22: 403); the employee is the key's
// business's; the venue is the key's business's or a holding peer's, found through ADR 0027 §2's
// closed list (holding_site_locations), never by a query in the peer's context; a virtual plaque is
// provisioned only at the key's OWN venue. A peer's venue must already have one -- provisioned by
// the peer's own simulated tap there -- because this package never writes into another business.
package sandbox

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/handler/api"
	"github.com/atknatk/tappa/internal/sun"
)

// Database is the slice of *db.DB the simulation needs, declared at the consumer (CLAUDE.md §7).
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
	// GetTagByUID is the context-less plaque resolver every tap takes; on a sandbox pool it refuses a
	// UID outside the virtual range (internal/db/virtualtags.go).
	GetTagByUID(ctx context.Context, uid string) (db.ResolvedTag, error)
	// AdmitTagUID is the insert door the minter passes before its INSERT.
	AdmitTagUID(uid string) error
}

// Previewer is the tap page's SUN pre-check (*sun.Verifier).
type Previewer interface {
	PreviewWithoutReplayProtection(ctx context.Context, p sun.Params) (sun.Preview, error)
}

// Recorder is the tap's recording (*checkin.Service).
type Recorder interface {
	Record(ctx context.Context, req checkin.Request) (checkin.Result, error)
}

// Trail is the audit trail's in-transaction writer (*audit.Recorder).
type Trail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
}

// Config is what New needs. Every field but PreviousKEK and Log is required.
type Config struct {
	// Env is the deployment's TAPPA_ENV; New refuses anything but config.EnvSandbox.
	Env      string
	Data     Database
	Verifier Previewer
	Taps     Recorder
	Trail    Trail
	// KEK is TAPPA_TAG_KEK -- the SANDBOX's own: it wraps every virtual plaque's key. PreviousKEK is
	// TAPPA_TAG_KEK_PREVIOUS during a rotation, nil otherwise.
	KEK, PreviousKEK []byte
	Log              *slog.Logger
}

// Simulation is the sandbox's /v1 extension (api.Extension).
type Simulation struct {
	data     Database
	verifier Previewer
	taps     Recorder
	trail    Trail
	kek      []byte
	keks     [][]byte
	log      *slog.Logger
}

var _ api.Extension = (*Simulation)(nil)

// ErrNotSandbox: New was handed a configuration that is not the sandbox's.
var ErrNotSandbox = errors.New("sandbox: the simulation runs only with TAPPA_ENV=sandbox (ADR 0028 §11)")

// New builds the simulation, refusing a configuration that is not the sandbox's and a missing
// dependency.
func New(c Config) (*Simulation, error) {
	switch {
	case c.Env != config.EnvSandbox:
		return nil, ErrNotSandbox
	case c.Data == nil:
		return nil, errors.New("sandbox: nil database")
	case c.Verifier == nil:
		return nil, errors.New("sandbox: nil SUN pre-check")
	case c.Taps == nil:
		return nil, errors.New("sandbox: nil tap recorder")
	case c.Trail == nil:
		return nil, errors.New("sandbox: nil audit trail")
	case len(c.KEK) != sun.KEKLen:
		return nil, errors.New("sandbox: the tag KEK must be 32 bytes")
	}
	log := c.Log
	if log == nil {
		log = slog.Default()
	}
	keks := [][]byte{c.KEK}
	if len(c.PreviousKEK) > 0 {
		keks = append(keks, c.PreviousKEK)
	}
	return &Simulation{data: c.Data, verifier: c.Verifier, taps: c.Taps, trail: c.Trail, kek: c.KEK, keks: keks, log: log}, nil
}

// The routes, under api.Prefix.
const (
	// TapsPath is the simulation (POST).
	TapsPath = "/sandbox/taps"
	// OpenAPIPath is the sandbox's own OpenAPI document (GET, no key).
	OpenAPIPath = "/sandbox/openapi.json"
)

// MountPublic registers the sandbox's OpenAPI document beside the shipped one.
func (s *Simulation) MountPublic(r chi.Router) { r.Get(OpenAPIPath, serveOpenAPI) }

// MountAuthenticated registers the simulation behind the chain.
func (s *Simulation) MountAuthenticated(r chi.Router, write api.WriteFunc) {
	r.Post(TapsPath, func(w http.ResponseWriter, req *http.Request) { s.simulate(w, req, write) })
}
