// Package session owns the persistent browser session that answers the "who"
// of a tap — the second of the four evidences in CLAUDE.md §5 ("oturum çerezi =
// kim"). It issues tokens, verifies them, refreshes them on use and revokes
// them.
//
// WHAT THIS PACKAGE DOES NOT DO — and must not start doing:
//
//   - It does NOT decide. It reports facts (this hash resolves / does not
//     resolve; this session is revoked / live) and leaves the verdict to
//     internal/policy + internal/domain/tap, exactly as migration 00003 says of
//     revoked_at ("iptal karari domain katmanindadir, veri katmani yalnizca
//     gercegi tasir"). §5 distinguishes row 3 (no session -> activation page,
//     NO record) from row 4 (deactivated employee -> reject + alert + record);
//     collapsing a real session into "no session" here would erase a
//     deactivated employee's attempt and break §4.6. That is why Verify returns
//     the resolved facts EVEN when it returns ErrRevoked.
//
//   - It reads NOTHING from the phone beyond the cookie it set itself. No
//     device probing, no attestation, no platform-authenticator ceremony, and
//     nothing derived from the person's body — §4.1 is absolute and the phone's
//     own screen lock is not our layer. sessions.device_info is a coarse label
//     ("iPhone Safari") the caller supplies, nullable, and nothing branches on
//     it.
//
//   - It enforces no device limit. The card marks that optional ("config ile
//     kapali gelebilir"); M5-01 ships the query surface it needs
//     (ListForEmployee, RevokeAllForEmployee) and no policy.
//
// §4.7: the raw token exists only as a Token (token.go), which has no exported
// accessor and does not render itself on any ordinary printing path — measured
// from an external test package, with the one remaining limit (deliberate
// reflection) written down there rather than glossed over. The database sees
// only an HMAC.
package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Database is the narrow slice of *db.DB this package needs, declared HERE at
// the consumer (CLAUDE.md §7) rather than in internal/db.
//
// The two methods are deliberately different in kind:
//
//   - WithTenant runs the tenant-scoped writes/reads. Every query it wraps
//     carries an explicit tenant_id predicate on top of RLS (§4.5).
//   - GetEmployeeBySessionHash is the ONE context-less lookup (ADR 0002 madde
//     7): a cookie carries no tenant, so the tenant is the RESULT. It reaches
//     the row through the SECURITY DEFINER resolve_session_by_token_hash, whose
//     blast radius is four columns of one table.
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
	GetEmployeeBySessionHash(ctx context.Context, tokenHash string) (db.ResolvedSession, error)
}

// Sentinel outcomes. Both mean "this cookie cannot authorise a tap", but they do
// NOT lead to the same §5 outcome, and conflating them loses a record.
//
// ⚠️ API TRAP — `if err != nil { serve the activation page }` IS WRONG.
// That shortcut is correct for ErrNoSession and WRONG for ErrRevoked: it routes
// a real, attributable session to §5 row 3, which writes NO record. Whenever the
// resolved employee turns out to be deactivated, §5 row 4 requires that attempt
// to be recorded with a security alert (§4.6 "kayıt asla kaybolmaz"). The trap is
// named here because the shortcut is the natural thing to write.
var (
	// ErrNoSession means there is nothing to identify: no cookie, a malformed
	// value, or a hash that resolves to no row. Resolved is the ZERO value —
	// there is no employee to attribute anything to, and no record can be
	// written even in principle. This is §5 row 3: activation page, NO record.
	ErrNoSession = errors.New("session: no valid session")

	// ErrRevoked means the session EXISTS, resolves to a real employee, and was
	// revoked. Resolved is POPULATED (ID, TenantID, EmployeeID, RevokedAt), which
	// is the whole point: the caller holds the tenant and the employee, so it can
	// look the employee up and let the policy engine decide.
	//
	// What the caller MUST do with it (this is the contract, not a suggestion):
	//   1. load the employee inside WithTenant(Resolved.TenantID);
	//   2. if the employee is DEACTIVATED -> §5 row 4: build tap.Input with a
	//      non-nil Employee{Status: deactivated}, let sys:employee-deactivated
	//      fire, WRITE THE RECORD and raise the alert;
	//   3. otherwise (an active employee whose old phone was revoked, e.g. after
	//      a second activation) -> §5 row 3: nil Employee, activation page, no
	//      record. Re-activation is the correct remedy there.
	//
	// The session layer cannot make that call itself: the one resolution query
	// does not return employees.status (migration 00003), and deciding here would
	// collapse row 4 into row 3. See the card correction in
	// docs/plan/m5-tap-akisi.md.
	ErrRevoked = errors.New("session: revoked")
)

// Manager is the session lifecycle. Dependencies are injected as struct fields
// (§7): no package-level state, no init() magic, no singleton.
type Manager struct {
	data Database
	// hmacKey is a private copy of Config.SessionHMACKey, so a later mutation
	// of the caller's slice cannot silently change how tokens hash.
	hmacKey []byte
}

// New builds a Manager. It refuses a wrong-sized HMAC key rather than degrading:
// an empty or short key still produces plausible-looking hex, so the failure
// would be invisible until an attacker noticed sessions were forgeable.
func New(data Database, cfg *config.Config) (*Manager, error) {
	if data == nil {
		return nil, errors.New("session: nil database")
	}
	if cfg == nil {
		return nil, errors.New("session: nil config")
	}
	if len(cfg.SessionHMACKey) != hmacKeyLen {
		return nil, fmt.Errorf("session: hmac key must be %d bytes, got %d", hmacKeyLen, len(cfg.SessionHMACKey))
	}
	return &Manager{
		data:    data,
		hmacKey: append([]byte(nil), cfg.SessionHMACKey...),
	}, nil
}

// Session is one stored session row. It carries no hash and no token: nothing
// outside this package needs either, and a struct without them cannot leak them
// through %+v or a structured log.
type Session struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	// DeviceInfo is the coarse label supplied at issue time, "" when absent.
	DeviceInfo string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// Live reports whether the session has not been revoked.
func (s Session) Live() bool { return s.RevokedAt == nil }

// Resolved is what a cookie resolves to: the exact four columns
// resolve_session_by_token_hash returns. RevokedAt is carried, NOT filtered —
// see the package doc.
type Resolved struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	RevokedAt  *time.Time
}

// Issued is the result of Issue: the stored record plus the raw token, which the
// caller must hand straight to Cookies.Set and then drop. The Token field cannot
// print itself (token.go), so logging an Issued value is safe.
type Issued struct {
	Session Session
	Token   Token
}

// IssueParams describes the session to create.
type IssueParams struct {
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	// DeviceInfo is an OPTIONAL coarse label such as "iPhone Safari". It is
	// never derived by probing the browser (§4.1), and it is bounded: Issue
	// rejects anything over deviceLabelMaxLen or carrying control characters
	// rather than silently truncating it. Do NOT pass r.UserAgent() here — that
	// is attacker-controlled, unbounded, and turns a coarse label into a browser
	// identity. See deviceLabel for the full reasoning.
	DeviceInfo string
}

// Issue mints a new session for an already-activated employee: 32 random bytes
// to the cookie, their HMAC to the database.
//
// TENANT SCOPING, stated precisely (§4.5). The write runs inside
// WithTenant(p.TenantID), so app.tenant_id is established and the RLS WITH CHECK
// on sessions confirms the inserted row's tenant matches the transaction's. Note
// what that does and does not prove: this function uses p.TenantID for BOTH, so
// those two can never disagree here — the WITH CHECK is a backstop against a
// future refactor, not the active guard.
//
// The guard that actually bites is the composite FK (employee_id, tenant_id) from
// migration 00003: an employee_id belonging to a DIFFERENT tenant is rejected by
// the database, so a caller who pairs its own tenant with someone else's employee
// gets an error, not a row. That is the case TestIssue_RejectsCrossTenantEmployee
// measures against real Postgres.
func (m *Manager) Issue(ctx context.Context, p IssueParams) (Issued, error) {
	if p.TenantID == uuid.Nil || p.EmployeeID == uuid.Nil {
		return Issued{}, errors.New("session: issue: tenant and employee are required")
	}
	label, err := deviceLabel(p.DeviceInfo)
	if err != nil {
		return Issued{}, fmt.Errorf("session: issue: %w", err)
	}
	t, err := newToken()
	if err != nil {
		return Issued{}, fmt.Errorf("session: issue: %w", err)
	}
	h, err := t.hash(m.hmacKey)
	if err != nil {
		return Issued{}, fmt.Errorf("session: issue: %w", err)
	}

	var row store.CreateSessionRow
	err = m.data.WithTenant(ctx, p.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).CreateSession(ctx, store.CreateSessionParams{
			TenantID:   p.TenantID,
			EmployeeID: p.EmployeeID,
			TokenHash:  h,
			DeviceInfo: optional(label),
		})
		return e
	})
	if err != nil {
		return Issued{}, fmt.Errorf("session: issue: %w", err)
	}
	return Issued{Session: fromCreateRow(row), Token: t}, nil
}

// Verify resolves a cookie value to its session in a SINGLE query
// (GetEmployeeBySessionHash) and reports whether that session is live.
//
// Outcomes:
//
//	(Resolved{...}, nil)          live session; Resolved carries tenant+employee
//	(Resolved{...}, ErrRevoked)   real session, revoked — Resolved is POPULATED and
//	                              the caller MUST branch on employees.status before
//	                              choosing §5 row 4 (record + alert) or row 3
//	                              (activation page, no record). See the API TRAP on
//	                              ErrRevoked above; `if err != nil { activate }`
//	                              loses the row-4 record and breaks §4.6.
//	(Resolved{}, ErrNoSession)    absent, malformed or unknown value → §5 row 3
//	(Resolved{}, other error)     the database failed; never treat as "no session"
//
// It does NOT read employees.status: the single resolution query cannot return it
// (migration 00003). Whether the resolved employee is deactivated is decided by
// the sys:employee-deactivated guardrail through tap.Decide (§5 row 4), which
// needs a record written; deciding it here would turn row 4 into row 3 and
// silently drop the attempt. See the card correction in docs/plan/m5-tap-akisi.md.
//
// TIMING (card: "sabit zamanlı"). The lookup is an equality probe on the UNIQUE
// index over sessions.token_hash, which Postgres does NOT execute in constant
// time — and that is acceptable here, by argument rather than by hand-wave:
// the value being matched is HMAC-SHA256 output under a server-side key. To
// steer the index probe (the only way a timing signal could be walked toward a
// real row) an attacker must choose the HASH, which requires evaluating the HMAC,
// which requires the key. Cookie values they can choose freely; the hashes those
// map to they cannot. A leaked timing signal about neighbouring hashes is also
// worthless on its own: the cookie must carry a PREIMAGE, and HMAC gives no way
// back. There is deliberately NO Go-side comparison of a hash or a token in this
// package — the database does the matching — so there is no byte-by-byte
// comparison to make constant-time; if one is ever added it must use
// crypto/subtle.ConstantTimeCompare (redline R7), never == or bytes.Equal.
func (m *Manager) Verify(ctx context.Context, t Token) (Resolved, error) {
	h, err := t.hash(m.hmacKey)
	if err != nil {
		if errors.Is(err, errMalformed) {
			// A cookie that cannot be one of ours is indistinguishable from no
			// cookie for the caller. Mapped, not swallowed: the shape error is
			// classified into a sentinel here and nowhere else, and it is not
			// wrapped so the value can never travel inside an error string.
			return Resolved{}, ErrNoSession
		}
		return Resolved{}, fmt.Errorf("session: verify: %w", err)
	}

	rs, err := m.data.GetEmployeeBySessionHash(ctx, h)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Resolved{}, ErrNoSession
		}
		// A database failure is NOT "no session": returning ErrNoSession here
		// would turn an outage into a silent stream of activation redirects
		// with no records written.
		return Resolved{}, fmt.Errorf("session: verify: %w", err)
	}

	r := Resolved{ID: rs.ID, TenantID: rs.TenantID, EmployeeID: rs.EmployeeID, RevokedAt: rs.RevokedAt}
	if r.RevokedAt != nil {
		return r, ErrRevoked
	}
	return r, nil
}

// Touch refreshes last_used_at (the card's "kullanımda yenilenir"). It is a
// separate call rather than a side effect of Verify so that read paths stay
// read-only and the caller decides how often to pay for a write.
//
// A revoked or vanished session updates 0 rows and yields ErrNoSession: the
// refresh fails closed rather than stamping a dead session as freshly used.
func (m *Manager) Touch(ctx context.Context, s Resolved) error {
	if s.TenantID == uuid.Nil || s.ID == uuid.Nil {
		return errors.New("session: touch: tenant and session id are required")
	}
	err := m.data.WithTenant(ctx, s.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).TouchSession(ctx, store.TouchSessionParams{
			ID: s.ID, TenantID: s.TenantID,
		})
		return e
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoSession
	}
	if err != nil {
		return fmt.Errorf("session: touch: %w", err)
	}
	return nil
}

// Revoke kills one session. Revocation is a revoked_at stamp, never a DELETE:
// migration 00003 REVOKEs DELETE on sessions from tappa_app precisely so the
// trail survives. Repeating it is idempotent and keeps the FIRST stamp.
//
// An unknown session (wrong id, or an id belonging to another tenant — the query
// filters on tenant_id and RLS enforces it independently) yields ErrNoSession.
func (m *Manager) Revoke(ctx context.Context, tenantID, sessionID uuid.UUID) error {
	if tenantID == uuid.Nil || sessionID == uuid.Nil {
		return errors.New("session: revoke: tenant and session id are required")
	}
	err := m.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).RevokeSession(ctx, store.RevokeSessionParams{
			ID: sessionID, TenantID: tenantID,
		})
		return e
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoSession
	}
	if err != nil {
		return fmt.Errorf("session: revoke: %w", err)
	}
	return nil
}

// RevokeAllForEmployee kills every live session of one employee and returns how
// many it killed (0 when there were none — idempotent). It is the "instant
// invalidation" surface the card asks for, and the ONLY one this layer owns.
//
// LEGITIMATE CALLERS: a lost or stolen phone, and the second-activation decision
// (M5-02) when a new device replaces an old one. In both, the point is that a
// cookie in someone else's hands must stop working — the session itself is the
// thing that went wrong.
//
// ⚠️ DEACTIVATION MUST NOT CALL THIS. An earlier version of this comment named
// M6-05 as a caller; that was backwards and an audit measured why. The reject of
// a deactivated employee does not depend on revocation at all: tap.Decide builds
// policy.CtxEmployeeStatus straight from employees.status (decide.go), and the
// sys:employee-deactivated guardrail denies with a security alert on that field
// alone. So revoking on deactivation adds NOTHING to the reject, while it does
// add the one thing we cannot afford: it pushes every subsequent tap by that
// person onto the ErrRevoked branch, where a caller taking the natural shortcut
// writes no record and §4.6 is broken. Leaving the sessions live keeps those taps
// on the plain path, where the guardrail rejects them AND the record is written.
// Deactivation is a status change; it is not a session problem.
func (m *Manager) RevokeAllForEmployee(ctx context.Context, tenantID, employeeID uuid.UUID) (int, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil {
		return 0, errors.New("session: revoke all: tenant and employee id are required")
	}
	var ids []uuid.UUID
	err := m.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		ids, e = store.New(tx).RevokeSessionsForEmployee(ctx, store.RevokeSessionsForEmployeeParams{
			TenantID: tenantID, EmployeeID: employeeID,
		})
		return e
	})
	if err != nil {
		return 0, fmt.Errorf("session: revoke all: %w", err)
	}
	return len(ids), nil
}

// ListForEmployee returns every session of one employee, newest first, revoked
// ones included. It is the read side of the optional device limit and of M5-02's
// "new phone or attack?" decision. No hash and no token appear in the result.
func (m *Manager) ListForEmployee(ctx context.Context, tenantID, employeeID uuid.UUID) ([]Session, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil {
		return nil, errors.New("session: list: tenant and employee id are required")
	}
	var out []Session
	err := m.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := store.New(tx).ListSessionsForEmployee(ctx, store.ListSessionsForEmployeeParams{
			TenantID: tenantID, EmployeeID: employeeID,
		})
		if e != nil {
			return e
		}
		out = make([]Session, 0, len(rows))
		for _, r := range rows {
			out = append(out, fromListRow(r))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("session: list: %w", err)
	}
	return out, nil
}

// deviceLabelMaxLen bounds sessions.device_info. Migration 00003 calls the
// column a COARSE label ("iPhone Safari") and explicitly not a browser identity;
// the column type is plain `text`, so nothing in the database enforces that.
//
// 64 runes: roughly triple the longest label anyone should write ("iPad Safari
// (iPadOS 17)" is 23, "Samsung Internet on Galaxy S24 Ultra" is 36) and below
// every real user agent. The first draft of this constant was 120 and the test
// immediately caught it as decorative — a current desktop Chrome UA is 117
// runes and slid straight under it.
//
// HONEST LIMIT: a length bound cannot DETECT a user agent, and a deliberately
// short one would still pass. What it does is bound the column against unbounded
// attacker-controlled text and catch the realistic mistake — passing
// r.UserAgent() through. The rest is the API contract on IssueParams.DeviceInfo.
const deviceLabelMaxLen = 64

// deviceLabelMaxBytes bounds the RAW input before anything scans it. A UTF-8
// rune is at most 4 bytes, so any label that could legitimately pass the rune
// limit fits here; the gate only refuses absurd padding.
const deviceLabelMaxBytes = 4 * deviceLabelMaxLen

// deviceLabel validates and normalises the caller's device label.
//
// WHY A LIMIT AT ALL. The value is attacker-controlled: M5-02 will be tempted to
// pass r.UserAgent() straight through, and a request header is whatever the
// client sends — unbounded, and unbounded rows written once per activation are a
// cheap way to bloat a table. Worse, a full UA slides the column from "coarse
// label" to "browser identity", which is the direction §4.1 keeps this product
// away from. A `text` column will accept any of that silently.
//
// WHY REJECT RATHER THAN TRUNCATE. §7 forbids silent acceptance, and a silent
// truncation is just a quieter version of the same thing: the caller believes it
// stored a device identity, the row holds the first N characters of one, and
// nobody learns that the boundary was crossed. Returning an error makes the
// caller decide what the label is — which is the point, since only the caller
// knows how it derived the string. The only rewriting done here is trimming
// surrounding whitespace; content is never altered.
//
// EXACTLY WHAT IS REJECTED (this list and the code below must stay in step — two
// audits in a row caught this comment claiming more than the code did: first
// "control characters" while only C0 and DEL were tested, so U+0085 NEL sailed
// through; then the scan running AFTER TrimSpace, so an EDGE control character
// was silently trimmed rather than rejected):
//
//   - more than deviceLabelMaxBytes bytes of raw input;
//   - invalid UTF-8;
//   - Unicode category Cc — every control, i.e. C0 (newline, tab, …), C1
//     (including U+0085 NEL) and DEL. These corrupt log lines and CSV rows;
//   - Unicode category Cf — format characters: zero-width (U+200B, ZWJ/ZWNJ),
//     BiDi overrides and isolates (U+202E, U+2066…), soft hyphen. U+202E is the
//     one that matters: it reverses rendering direction, so a device label could
//     scramble the row order a manager reads in the M6 device list;
//   - Unicode categories Zl and Zp — U+2028 LINE SEPARATOR and U+2029 PARAGRAPH
//     SEPARATOR. Not exploitable through this repo's current sinks, but many
//     renderers and JavaScript parsers treat them as line terminators, and they
//     cost one category test to exclude;
//   - more than deviceLabelMaxLen runes after trimming.
//
// POSITION DOES NOT MATTER: the scan runs on the RAW input, BEFORE trimming, so a
// control character is rejected whether it sits in the middle or at either end.
// The earlier order let strings.TrimSpace eat an edge U+0085/U+000B/tab first —
// fail-safe (nothing bad reached the column) but silent, which is the same
// §7 problem as truncation: the caller would never learn its label was malformed.
// Only ordinary space-category runes survive to be trimmed, so trimming can no
// longer hide anything.
//
// WHAT THIS DELIBERATELY DOES NOT DO — belongs to the layer that renders, not the
// layer that stores:
//
//   - CSV formula injection ("=cmd|'/c calc'!A1" passes through untouched).
//     Neutralising a leading =, +, - or @ is the CSV writer's job (M6-07),
//     because whether it is dangerous depends on the sink, not on the value;
//   - HTML escaping — the templ template's job;
//   - Unicode normalisation (no NFC/NFKC): normalising would rewrite the
//     caller's content, which this function promises not to do.
//
// TRADE-OFF ACCEPTED: rejecting all of Cf also rejects ZWJ, so an exotic label
// built from an emoji sequence is refused. That is a loud error the caller can
// act on, not a silent mangling, and a coarse device label has no business
// needing ZWJ. Legitimate labels ("iPhone Safari", "Android Chrome", "Pixel 8 /
// Firefox 130", "Wéb Tarayıcı") pass untouched, so a correct activation cannot
// be broken by this.
func deviceLabel(s string) (string, error) {
	// Byte gate FIRST, on the raw input, so every step below is bounded: a valid
	// deviceLabelMaxLen-rune label is at most deviceLabelMaxBytes bytes. Padding
	// beyond that is refused rather than trimmed — nobody sends 300 spaces by
	// accident, and refusing keeps the work bounded before any scanning.
	if len(s) > deviceLabelMaxBytes {
		return "", fmt.Errorf("device label must be at most %d bytes, got %d (it is a coarse label such as \"iPhone Safari\", not a full user agent)", deviceLabelMaxBytes, len(s))
	}
	// Before the range loop: ranging over invalid UTF-8 silently yields U+FFFD,
	// which would make the category tests inspect a substitute rune rather than
	// what the caller actually sent.
	if !utf8.ValidString(s) {
		return "", errors.New("device label is not valid UTF-8")
	}
	// Scan the RAW string, before trimming, so an edge control character is
	// rejected instead of quietly disappearing (see the comment above).
	for _, r := range s {
		switch {
		case unicode.IsControl(r): // category Cc: C0, C1 (incl. U+0085), DEL
			return "", fmt.Errorf("device label contains a control character (%#U)", r)
		case unicode.Is(unicode.Cf, r): // zero-width, BiDi overrides, soft hyphen
			return "", fmt.Errorf("device label contains a formatting character (%#U)", r)
		case unicode.Is(unicode.Zl, r), unicode.Is(unicode.Zp, r): // U+2028, U+2029
			return "", fmt.Errorf("device label contains a line separator (%#U)", r)
		}
	}
	// Only space-category runes can be at the edges by now, so trimming cannot
	// hide anything.
	label := strings.TrimSpace(s)
	if label == "" {
		return "", nil // absent is a first-class state: the column is nullable
	}
	if n := utf8.RuneCountInString(label); n > deviceLabelMaxLen {
		return "", fmt.Errorf("device label must be at most %d characters, got %d (it is a coarse label such as \"iPhone Safari\", not a full user agent)", deviceLabelMaxLen, n)
	}
	return label, nil
}

// optional maps "" to a SQL NULL for the nullable device_info column, so an
// unknown device is stored as unknown rather than as an empty label.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// text maps a nullable column back to "" so callers never dereference.
func text(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func fromCreateRow(r store.CreateSessionRow) Session {
	return Session{
		ID:         r.ID,
		TenantID:   r.TenantID,
		EmployeeID: r.EmployeeID,
		DeviceInfo: text(r.DeviceInfo),
		CreatedAt:  r.CreatedAt,
		LastUsedAt: r.LastUsedAt,
		RevokedAt:  r.RevokedAt,
	}
}

func fromListRow(r store.ListSessionsForEmployeeRow) Session {
	return Session{
		ID:         r.ID,
		TenantID:   r.TenantID,
		EmployeeID: r.EmployeeID,
		DeviceInfo: text(r.DeviceInfo),
		CreatedAt:  r.CreatedAt,
		LastUsedAt: r.LastUsedAt,
		RevokedAt:  r.RevokedAt,
	}
}
