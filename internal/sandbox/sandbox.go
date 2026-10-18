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
// ALL OF IT ON ONE CONNECTION (API-11 fix 1, runner.go): the pre-check and the recorder above are
// the tap page's code, built once more by New over the /v1 write runner's transaction, so the
// plaque's provisioning, the counter's advance, the record and the Idempotency-Key's result commit
// together and a simulation never waits for a second pool connection.
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
// the peer's own simulated tap there -- because this package never PROVISIONS A PLAQUE in another
// business. A group tap does write into the venue's business, through the holding path the tap
// page's group tap takes and nothing else (ADR 0027 §5): the plaque's counter
// (holding_advance_tag_counter), the record's projection to the site and the site's punch feed.
package sandbox

import (
	"context"
	"errors"
	"fmt"
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
	// WithTenant is the pool's: the answer's read-back, after the runner's transaction committed.
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
	// GetTagByUIDOn is the context-less plaque resolver every tap takes, on the runner's
	// transaction (runner.go); on a sandbox pool it refuses a UID outside the virtual range
	// (internal/db/virtualtags.go).
	GetTagByUIDOn(ctx context.Context, tx pgx.Tx, uid string) (db.ResolvedTag, error)
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

// Noticer is K-19's set of tenants with work (*webhook.Worker).
type Noticer interface {
	Notice(tenant uuid.UUID)
}

// Config is what New needs. Every field but Log is required.
type Config struct {
	// App is the deployment's configuration. New refuses it unless App.Env is config.EnvSandbox;
	// App.TagKEK -- the SANDBOX's own -- wraps every virtual plaque's key (App.TagKEKPrevious too
	// during a rotation), and the tap recorder takes its windows from it as the tap page's does.
	App  *config.Config
	Data Database
	// Work is told, after the runner's transaction committed, which businesses a recorded
	// simulation gave work: the key's, and the venue's for a group tap (K-19).
	Work Noticer
	Log  *slog.Logger
}

// Simulation is the sandbox's /v1 extension (api.Extension).
type Simulation struct {
	data     Database
	verifier Previewer
	taps     Recorder
	trail    Trail
	work     Noticer
	kek      []byte
	keks     [][]byte
	// mint is the chip: sun.MintTapPath. A field so that a test can hand the pre-check a URL whose
	// MAC is not the key's (TestSandbox_TheSimulationIsJudgedByTheRealCMAC).
	mint func(key, uid []byte, ctr uint32) (string, error)
	log  *slog.Logger
}

var _ api.Extension = (*Simulation)(nil)

// ErrNotSandbox: New was handed a configuration that is not the sandbox's.
var ErrNotSandbox = errors.New("sandbox: the simulation runs only with TAPPA_ENV=sandbox (ADR 0028 §11)")

// New builds the simulation, refusing a configuration that is not the sandbox's and a missing
// dependency.
//
// THE TAP PATH IS THE TAP PAGE'S CODE, BUILT ONCE MORE OVER THE RUNNER (runner.go): the SUN
// pre-check is sun.NewVerifier, the recorder checkin.New, the trail audit.New -- the constructors
// cmd/tappa's run() gives the tap page, with the same configuration and KEKs -- over onRunner, so
// that every statement of a simulated tap runs on the /v1 write runner's one transaction. The
// decision, the atomic advance, the debounce, the INSERT and its triggers are the same functions;
// only the connection they run on is the runner's (TestNew_TheTapPathIsTheTapPagesCodeOnTheRunner).
func New(c Config) (*Simulation, error) {
	switch {
	case c.App == nil || c.App.Env != config.EnvSandbox:
		return nil, ErrNotSandbox
	case c.Data == nil:
		return nil, errors.New("sandbox: nil database")
	case c.Work == nil:
		return nil, errors.New("sandbox: nil work set")
	case len(c.App.TagKEK) != sun.KEKLen:
		return nil, errors.New("sandbox: the tag KEK must be 32 bytes")
	}
	log := c.Log
	if log == nil {
		log = slog.Default()
	}
	route := onRunner{data: c.Data}
	trail, err := audit.New(route)
	if err != nil {
		return nil, fmt.Errorf("sandbox: %w", err)
	}
	taps, err := checkin.New(route, trail, c.App, log)
	if err != nil {
		return nil, fmt.Errorf("sandbox: %w", err)
	}
	keks := [][]byte{c.App.TagKEK}
	if len(c.App.TagKEKPrevious) > 0 {
		keks = append(keks, c.App.TagKEKPrevious)
	}
	return &Simulation{
		data: c.Data, verifier: sun.NewVerifier(route, c.App.TagKEK, c.App.TagKEKPrevious), taps: taps, trail: trail,
		work: c.Work, kek: c.App.TagKEK, keks: keks, mint: sun.MintTapPath, log: log,
	}, nil
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
