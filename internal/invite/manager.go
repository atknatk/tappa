// Package invite owns the activation code: minting it, delivering it through
// exactly one seam, and spending it to turn an 'invited' employee into an
// 'active' one who holds a session cookie (internal/session).
//
// WHAT THIS PACKAGE DOES NOT DO — and must not start doing:
//
//   - It does NOT render and does NOT speak HTTP. It returns facts and typed
//     errors; internal/handler decides what the employee is told. That split is
//     what keeps the "no oracle" rule (§4.7) checkable: this package
//     distinguishes expired from consumed from unknown FOR THE AUDIT TRAIL, and
//     the handler collapses all of them into one message FOR THE VISITOR.
//   - It does NOT issue sessions. Activation and session issuance are separate
//     transactions on purpose (see Activate); the caller sequences them.
//   - It reads NOTHING from the phone (§4.1). No attestation, no fingerprinting,
//     nothing derived from a person's body.
//
// §4.7: the raw code exists only as a Code (code.go) and leaves this package
// through ONE seam — Channel.DeliverInvite (channel.go). Nothing else can turn a
// Code back into a string.
package invite

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Database is the narrow slice of *db.DB this package needs, declared HERE at
// the consumer (CLAUDE.md §7).
//
// The two methods differ in kind, exactly as they do in internal/session:
//
//   - WithTenant runs the tenant-scoped reads and the consuming statement. Every
//     query it wraps carries an explicit tenant_id predicate on top of RLS (§4.5).
//   - GetInviteByCodeHash is the context-less lookup (ADR 0002 madde 7): an
//     activation link carries only a code, so the tenant is the RESULT.
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
	GetInviteByCodeHash(ctx context.Context, codeHash string) (db.ResolvedInvite, error)
}

// Sentinel outcomes.
//
// ⚠️ THESE ARE FOR THE AUDIT TRAIL, NOT FOR THE VISITOR. Telling a caller which
// of the four happened is an ORACLE: "expired" confirms the code once existed,
// "unknown" says it never did, and an attacker holding a shortened or guessed
// link learns which. internal/handler therefore renders ONE message and ONE
// status for all four, and a test asserts the bodies are byte-identical. Here
// the distinction is kept because §4.6 wants the trail to say WHY.
var (
	// ErrUnknownCode: the value is malformed, or its hash resolves to no row.
	// The returned Context is the ZERO value — there is no tenant, so the
	// attempt CANNOT be written to audit_log (db/queries/audit.sql states this
	// limit; it is not silently ignored, the caller logs it).
	ErrUnknownCode = errors.New("invite: unknown code")

	// ErrCodeExpired: the invite resolves but now() >= expires_at. The Context is
	// POPULATED (tenant + employee + invite id) so the caller can audit it.
	ErrCodeExpired = errors.New("invite: code expired")

	// ErrCodeUsed: the invite resolves but used_at is set — a replay, or the link
	// opened twice. Context POPULATED.
	ErrCodeUsed = errors.New("invite: code already used")

	// ErrNotActivatable: the invite is fine but the employee is not activatable —
	// deactivated, or the consuming statement lost the race documented in
	// db/queries/invites.sql. Context POPULATED.
	//
	// PRECISION on the deactivated case: what refuses it is the EXISTS guard and
	// the status test inside ConsumeInviteAndActivate (db/queries/invites.sql),
	// not migration 00009 itself — the schema has no such constraint. The
	// guarantee is therefore "no path through the generated Querier can
	// reactivate a deactivated employee with a link", which is the scope
	// invites.sql states, and it is measured
	// (TestActivate_DeactivatedEmployeeDoesNotBurnTheCode). Hand-written SQL
	// outside the store is not bound by it.
	ErrNotActivatable = errors.New("invite: employee cannot be activated")
)

// TTL bounds and default for a new invitation.
//
// THE M5-02 CARD LEFT THIS TO PHASE B ON PURPOSE (00009: expires_at is NOT NULL
// and cannot be 'infinity', but a finite-yet-absurd year 9999 is still writable
// at the schema level — "that is a TTL policy question, and the TTL is phase B's
// decision"). This is that decision, made in Go rather than in a CHECK because
// a maximum TTL is a product rule that will move (a chain onboarding seasonal
// staff wants a longer window than one hiring a single person), and moving a
// CHECK costs a migration.
//
//   - defaultTTL = 7 days: long enough that a paper invitation handed out on
//     Monday still works on Sunday, short enough that a photographed link in a
//     WhatsApp group dies within a week.
//   - minTTL = 1 hour: below that the invitation is a practical joke — the
//     employee has not necessarily read their message yet.
//   - maxTTL = 30 days: the point past which "invitation" becomes "standing
//     credential". It is the number the schema deliberately did not pick.
//
// LIMIT, stated rather than claimed: this bound is enforced HERE, in the only Go
// path that inserts an invite, and NOT by the database. Hand-written SQL or a
// future query could still write a longer expiry; making it structural needs a
// bounded CHECK in a new migration (00009 says the same thing from its side).
const (
	defaultTTL = 7 * 24 * time.Hour
	minTTL     = time.Hour
	maxTTL     = 30 * 24 * time.Hour
)

// Manager is the invite lifecycle. Dependencies are injected as struct fields
// (§7): no package-level state, no init() magic, no singleton.
type Manager struct {
	data Database
	// hmacKey is a private copy of Config.InviteHMACKey, so a later mutation of
	// the caller's slice cannot silently change how codes hash.
	hmacKey []byte
	// baseURL is the origin the activation link points at.
	baseURL string
	// now is injectable for tests only; production leaves it nil and reads the
	// wall clock. Expiry is ALSO enforced by the database inside
	// ConsumeInviteAndActivate (`now() < expires_at`), so a wrong clock here can
	// make this layer generous, never the database.
	now func() time.Time
}

// New builds a Manager. It refuses a wrong-sized HMAC key rather than degrading:
// an empty or short key still produces plausible-looking hex, so the failure
// would be invisible until someone noticed codes were forgeable.
func New(data Database, cfg *config.Config) (*Manager, error) {
	if data == nil {
		return nil, errors.New("invite: nil database")
	}
	if cfg == nil {
		return nil, errors.New("invite: nil config")
	}
	if len(cfg.InviteHMACKey) != hmacKeyLen {
		return nil, fmt.Errorf("invite: hmac key must be %d bytes, got %d", hmacKeyLen, len(cfg.InviteHMACKey))
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("invite: base url is required to build an activation link")
	}
	return &Manager{
		data:    data,
		hmacKey: append([]byte(nil), cfg.InviteHMACKey...),
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
	}, nil
}

func (m *Manager) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// Invite is one stored invitation. It carries NO code and NO hash: nothing
// outside this package needs either, and a struct without them cannot leak them
// through %+v or a structured log (§4.7).
type Invite struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// IssueParams describes the invitation to create.
type IssueParams struct {
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	// TTL is the validity window; zero means defaultTTL. Values outside
	// [minTTL, maxTTL] are REJECTED, not clamped (§7: no silent acceptance).
	TTL time.Duration
}

// IssueAndDeliver mints a code, stores its hash and hands the activation link to
// ch. It is the ONLY exported way to create an invitation, and it deliberately
// does NOT return the code.
//
// WHY THE CODE IS NOT RETURNED. If it were, every caller would hold a raw
// credential and the "one egress" claim in code.go would be false — the value
// would flow into handlers, templates and logs at each caller's discretion.
// Routing it through a Channel makes delivery a NAMED, greppable seam, which is
// what the Q02 migration needs: when the invite stops going to the manager's
// screen and starts going to the employee's own inbox, exactly one
// implementation changes and no call site does (docs/adr/0005 Y-D).
//
// ORDER OF EFFECTS: the row is committed BEFORE delivery is attempted. A
// delivered code that was never stored would be unusable and unexplainable; a
// stored code that failed to deliver is visible in the panel as an outstanding
// invitation (ListPendingInvitesForEmployee) and can simply be reissued. The
// error is returned, never swallowed (§7), and it names the invite id rather
// than anything about the code.
func (m *Manager) IssueAndDeliver(ctx context.Context, p IssueParams, ch Channel) (Invite, error) {
	if p.TenantID == uuid.Nil || p.EmployeeID == uuid.Nil {
		return Invite{}, errors.New("invite: issue: tenant and employee are required")
	}
	if ch == nil {
		return Invite{}, errors.New("invite: issue: a delivery channel is required")
	}
	ttl := p.TTL
	if ttl == 0 {
		ttl = defaultTTL
	}
	if ttl < minTTL || ttl > maxTTL {
		return Invite{}, fmt.Errorf("invite: issue: ttl must be within [%s, %s], got %s", minTTL, maxTTL, ttl)
	}

	code, err := newCode()
	if err != nil {
		return Invite{}, fmt.Errorf("invite: issue: %w", err)
	}
	hash, err := code.hash(m.hmacKey)
	if err != nil {
		return Invite{}, fmt.Errorf("invite: issue: %w", err)
	}

	var row store.CreateInviteRow
	err = m.data.WithTenant(ctx, p.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).CreateInvite(ctx, store.CreateInviteParams{
			TenantID:   p.TenantID,
			EmployeeID: p.EmployeeID,
			CodeHash:   hash,
			ExpiresAt:  m.clock().Add(ttl),
		})
		return e
	})
	if err != nil {
		return Invite{}, fmt.Errorf("invite: issue: %w", err)
	}

	inv := Invite{
		ID:         row.ID,
		TenantID:   row.TenantID,
		EmployeeID: row.EmployeeID,
		CreatedAt:  row.CreatedAt,
		ExpiresAt:  row.ExpiresAt,
	}
	if err := ch.DeliverInvite(ctx, Delivery{
		Invite:        inv,
		ActivationURL: m.activationURL(code),
	}); err != nil {
		return inv, fmt.Errorf("invite: deliver %s: %w", inv.ID, err)
	}
	return inv, nil
}

// activationURL builds the link the employee opens. This and Delivery are the
// only places a raw code becomes a string outside code.go.
//
// The code travels as a QUERY PARAMETER and is url-escaped even though base64url
// needs no escaping: relying on "this alphabet happens to be safe" is how an
// encoding change becomes a broken link.
func (m *Manager) activationURL(c Code) string {
	q := url.Values{}
	q.Set("code", c.reveal())
	return m.baseURL + "/activate?" + q.Encode()
}

// Context is what a valid code resolves to: everything the activation page must
// render, plus the ids the caller needs to audit and to consume.
//
// It carries NO code and NO hash.
type Context struct {
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	InviteID   uuid.UUID
	LocationID uuid.UUID
	// FullName is the employee's name ("Hello Maria"), TenantName the employer's
	// — the GDPR Art. 13 data controller.
	FullName   string
	TenantName string
	// LocationName and WiFiSSID drive the Wi-Fi step (Q14). WiFiSSID is "" when
	// the location has no network to show (locations.wifi_ssid IS NULL, migration
	// 00010) — NOT an error: the step simply renders without a name and the
	// employee falls back to the GPS path on later taps.
	LocationName string
	WiFiSSID     string
	// Status is employees.status at read time: 'invited' for a first activation,
	// 'active' for a second device.
	Status    string
	ExpiresAt time.Time
}

// SecondDevice reports whether this activation replaces an existing one — an
// already-active employee presenting a valid, unused invitation.
func (c Context) SecondDevice() bool { return c.Status == statusActive }

// FirstActivation reports whether this is the employee's first ever activation
// (still 'invited'). The page greets the two cases differently: a first
// activation is a welcome, a second one is a warning that the other phone is
// about to be signed out.
func (c Context) FirstActivation() bool { return c.Status == statusInvited }

// employees.status values this package cares about. The full set and its CHECK
// live in migration 00003.
const (
	statusInvited     = "invited"
	statusActive      = "active"
	statusDeactivated = "deactivated"
)

// Lookup resolves a code to its activation context WITHOUT consuming anything.
// It backs GET /activate.
//
// OUTCOMES — the second component of the pair matters as much as the first:
//
//	(Context{...}, nil)                usable now
//	(Context{...}, ErrCodeExpired)     resolved, past expiry — Context POPULATED
//	(Context{...}, ErrCodeUsed)        resolved, already consumed — POPULATED
//	(Context{...}, ErrNotActivatable)  resolved, employee deactivated — POPULATED
//	(Context{},    ErrUnknownCode)     nothing to attribute — ZERO
//	(Context{},    other error)        the database failed; NOT "unknown code"
//
// WHY THE POPULATED CONTEXT ON FAILURE — this is the same contract
// session.Verify makes when it returns ErrRevoked, and for the same reason
// (§4.6): the caller must be able to write the failed attempt to audit_log, and
// it cannot do that without a tenant. Returning a zero Context with the error
// would silently discard every replayed-code attempt.
//
// ⚠️ WHAT LOOKUP IS NOT: it is NOT the authority on whether an activation may
// proceed. Its used/expired tests are a READ, and a read-then-write over the
// same state is the tags.last_ctr TOCTOU in another costume (§4.4,
// internal/db/resolve.go says so at the resolver). The authority is Activate,
// whose single statement carries the state test in its WHERE.
func (m *Manager) Lookup(ctx context.Context, c Code) (Context, error) {
	_, res, err := m.resolve(ctx, c)
	if err != nil {
		return Context{}, err
	}

	out := Context{
		TenantID:   res.TenantID,
		EmployeeID: res.EmployeeID,
		InviteID:   res.ID,
		ExpiresAt:  res.ExpiresAt,
	}

	// Load the employee and venue facts FIRST, so even a failed lookup returns a
	// context rich enough to audit and (for an expired code) to greet by name.
	full, err := m.ActivationContext(ctx, res.TenantID, res.EmployeeID)
	if err != nil {
		return out, err
	}
	full.InviteID = out.InviteID
	full.ExpiresAt = out.ExpiresAt
	out = full

	// Order is deliberate: consumed beats expired. A code that was USED and has
	// since expired is a replay, and the trail should say so.
	if res.UsedAt != nil {
		return out, ErrCodeUsed
	}
	if !m.clock().Before(res.ExpiresAt) {
		return out, ErrCodeExpired
	}
	if out.Status == statusDeactivated {
		return out, ErrNotActivatable
	}
	return out, nil
}

// resolve hashes the code and runs the context-less lookup. It returns the hash
// as well, so the consuming statement re-uses the value that was just matched
// rather than deriving it a second time.
//
// The hash is a §4.7 secret in its own right (a bearer credential: hash →
// resolver → tenant → consumption, 00009), so it stays a local string inside
// this package and never enters a struct field, an error or a log line.
func (m *Manager) resolve(ctx context.Context, c Code) (string, db.ResolvedInvite, error) {
	hash, err := c.hash(m.hmacKey)
	if err != nil {
		if errors.Is(err, errMalformed) {
			// A value that cannot be one of ours is indistinguishable from an
			// unknown one for the caller. Mapped, not swallowed — and NOT
			// wrapped, so the value can never travel inside an error string.
			return "", db.ResolvedInvite{}, ErrUnknownCode
		}
		return "", db.ResolvedInvite{}, fmt.Errorf("invite: %w", err)
	}
	res, err := m.data.GetInviteByCodeHash(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", db.ResolvedInvite{}, ErrUnknownCode
		}
		// A database failure is NOT "unknown code": collapsing it would turn an
		// outage into a stream of "your link is invalid" pages with no trail.
		return "", db.ResolvedInvite{}, fmt.Errorf("invite: resolve: %w", err)
	}
	return hash, res, nil
}

// ActivationContext assembles everything the activation screens display about an
// employee — WITHOUT a code. It backs two callers: Lookup (which has just
// resolved a code) and the confirmation page, which identifies the visitor by
// their brand-new session cookie instead.
//
// Both reads run in ONE transaction, so the venue named on the page belongs to
// the employee row that was just read rather than to a snapshot taken a moment
// later. The invite half of the Context (InviteID, ExpiresAt) is left zero: this
// function knows nothing about codes, which is precisely why the confirmation
// page can use it.
//
// §4.5: both queries carry an explicit tenant_id predicate on top of RLS, and the
// location id is the one the EMPLOYEE ROW gave us — never a client-supplied
// value, which is the intent db/queries/locations.sql asks callers to honour.
func (m *Manager) ActivationContext(ctx context.Context, tenantID, employeeID uuid.UUID) (Context, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil {
		return Context{}, errors.New("invite: activation context: tenant and employee are required")
	}
	out := Context{TenantID: tenantID, EmployeeID: employeeID}
	err := m.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		emp, err := q.GetEmployeeActivationContext(ctx, store.GetEmployeeActivationContextParams{
			TenantID:   tenantID,
			EmployeeID: employeeID,
		})
		if err != nil {
			return fmt.Errorf("load employee: %w", err)
		}
		out.FullName = emp.FullName
		out.TenantName = emp.TenantName
		out.Status = emp.Status
		out.LocationID = emp.LocationID

		venue, err := q.GetLocationWiFi(ctx, store.GetLocationWiFiParams{
			TenantID: tenantID,
			ID:       emp.LocationID,
		})
		if err != nil {
			return fmt.Errorf("load venue: %w", err)
		}
		out.LocationName = venue.Name
		if venue.WifiSsid != nil {
			out.WiFiSSID = *venue.WifiSsid
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("invite: activation context: %w", err)
	}
	return out, nil
}

// Activation is a completed activation: who was activated, where they work, and
// which invitation paid for it.
type Activation struct {
	Context
	// SecondDeviceReplaced is true when the employee was ALREADY active before
	// this activation — a new phone (or an attacker with the link). The caller
	// must revoke the older sessions; see the decision note on Activate.
	SecondDeviceReplaced bool
}

// Activate spends the code and flips the employee to 'active'. It backs
// POST /api/activate.
//
// THE WHOLE OPERATION IS ONE SQL STATEMENT (store.ConsumeInviteAndActivate), so
// "this employee was activated" IMPLIES "an invitation was consumed for them",
// and N concurrent requests carrying the SAME code produce EXACTLY ONE winner
// (§4.4 shape; the reasoning is in db/queries/invites.sql and the N-goroutine
// proof lives in internal/db/invites_test.go and internal/handler).
//
// WHAT THIS FUNCTION DOES NOT DO, on purpose:
//
//   - It does NOT revoke the old sessions of a second device. It REPORTS the
//     situation (SecondDeviceReplaced) and the caller acts, because revocation
//     is a separate transaction and ordering it wrongly is destructive: revoking
//     AFTER issuing kills the session just issued (RevokeAllForEmployee kills
//     every live session of the employee, including the new one). The caller
//     must revoke FIRST, then issue.
//   - It does NOT issue the session. Same reason: separate transaction, and the
//     session layer owns that.
//
// ERROR CLASSIFICATION IS A BEST-EFFORT LABEL, NOT AN AUTHORITY. The consuming
// statement returns "0 rows" for used, expired and not-activatable alike (that
// collapse is deliberate — see invites.sql, it denies the caller an oracle). So
// when it returns nothing, this function classifies the failure from the facts
// the resolver already returned, purely so audit_log can say WHY. The visitor is
// told the same thing either way.
func (m *Manager) Activate(ctx context.Context, c Code) (Activation, error) {
	hash, res, err := m.resolve(ctx, c)
	if err != nil {
		return Activation{}, err
	}

	out := Activation{Context: Context{
		TenantID:   res.TenantID,
		EmployeeID: res.EmployeeID,
		InviteID:   res.ID,
		ExpiresAt:  res.ExpiresAt,
	}}

	var consumed store.ConsumeInviteAndActivateRow
	var priorStatus string
	err = m.data.WithTenant(ctx, res.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		// The prior status is read INSIDE the same transaction as the
		// consumption, so it describes the row the statement is about to act on.
		// It is not a gate: it decides only whether the caller revokes older
		// sessions afterwards. A concurrent change between this read and the
		// UPDATE can at worst cause an unnecessary revocation, which is
		// fail-closed (the employee re-activates or taps again).
		emp, e := q.GetEmployeeActivationContext(ctx, store.GetEmployeeActivationContextParams{
			TenantID:   res.TenantID,
			EmployeeID: res.EmployeeID,
		})
		if e != nil {
			return fmt.Errorf("invite: load employee: %w", e)
		}
		priorStatus = emp.Status
		out.FullName = emp.FullName
		out.TenantName = emp.TenantName
		out.LocationID = emp.LocationID

		venue, e := q.GetLocationWiFi(ctx, store.GetLocationWiFiParams{
			TenantID: res.TenantID,
			ID:       emp.LocationID,
		})
		if e != nil {
			return fmt.Errorf("invite: load venue: %w", e)
		}
		out.LocationName = venue.Name
		if venue.WifiSsid != nil {
			out.WiFiSSID = *venue.WifiSsid
		}

		consumed, e = q.ConsumeInviteAndActivate(ctx, store.ConsumeInviteAndActivateParams{
			TenantID: res.TenantID,
			CodeHash: hash,
		})
		// The error is PROPAGATED, never swallowed. db/queries/invites.sql spells
		// out why: a caller that swallows it and commits spends the code in the
		// one narrow race the file documents. Propagating rolls the transaction
		// back and the invitation survives.
		return e
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			out.Status = priorStatus
			return out, m.classify(res, priorStatus)
		}
		return out, fmt.Errorf("invite: activate: %w", err)
	}

	out.Status = consumed.Status
	out.FullName = consumed.FullName
	out.LocationID = consumed.LocationID
	out.InviteID = consumed.InviteID
	out.SecondDeviceReplaced = priorStatus == statusActive
	return out, nil
}

// classify labels a failed consumption for the audit trail. See Activate.
//
// The order is "what would a reader most want to know": a replayed code is
// reported as a replay even if it has since expired, because the replay is the
// interesting event. priorStatus is the status read inside the same transaction,
// so it distinguishes the ordinary refusal (a deactivated employee, which
// migration 00009 blocks structurally) from the residual race in invites.sql —
// the two produce the same error but not the same story, and only the second one
// should ever make anybody worry.
func (m *Manager) classify(res db.ResolvedInvite, priorStatus string) error {
	switch {
	case res.UsedAt != nil:
		return ErrCodeUsed
	case !m.clock().Before(res.ExpiresAt):
		return ErrCodeExpired
	case priorStatus == statusDeactivated:
		return ErrNotActivatable
	default:
		// Usable invite, activatable employee, and still zero rows: the
		// deactivation-mid-statement window documented in db/queries/invites.sql.
		return ErrNotActivatable
	}
}
