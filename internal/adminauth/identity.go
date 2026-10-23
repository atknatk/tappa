package adminauth

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// identity.go -- signing a PERSON in through their identity (ADR 0031 §5.6, §5.7; M14-07).
//
// WHAT IT IS AND WHAT IT IS NOT YET. AuthenticatePerson is the identity path of the login: the address
// resolves to at most one identity (resolve_identity_by_email), the typed password is compared with
// the identity's digest, and a proven identity with an active membership gets a session from
// identity_open_session. Beside it runs the transition-period LEGACY ARM: the address's UNLINKED rows
// (at most MaxCandidates, incumbent first) are compared as Authenticate compares them today, and a
// row whose password ALSO verifies is linked to the identity with two fresh sessions
// (identity_link_legacy). Nothing in the product calls it yet: the flag TAPPA_ADMIN_IDENTITY and its
// gate are M14-08's, the login handler's identity path M14-09's. Until then the login is today's
// Authenticate + Issue for everyone -- the flag-off behaviour of ADR 0031 §8 -- and the two changes
// this card makes to that path (Issue's Proof and CreateAdminSession's digest predicate) hold whatever
// the flag says, because a membership can be refused from a pod where the flag is on while a pod
// where it is off signs the same person in (the rollout runs two pods for a while).
//
// 🔴 THE FLAT TIMING (ADR 0031 §5.6, R14, S15) -- five cases must not be told apart by the clock: an
// identity or none x an unlinked row or none (an unknown address included), and the right password
// on an identity whose memberships are all disabled. What makes them one shape:
//
//	every sign-in   resolve_identity_by_email · resolve_admin_by_email
//	                1 + MaxCandidates bcrypt comparisons -- the identity's (or a dummy) and the
//	                legacy window padded to MaxCandidates (Manager.pad) -- ALWAYS, before any
//	                branch: has_active_membership is read only AFTER the comparisons, so it can
//	                neither skip nor advance one
//	                ONE "open" step: identity_open_session when a proven identity has an active
//	                membership, otherwise the decoy (decoy below), which costs what the open costs
//	every failure   ONE event step: identity_record_failed_login for the identity, or -- with no
//	                identity, or past the identity's budget -- the same call for an id no identity
//	                carries, which writes nothing
//
// ⚠️ WHAT IS LEFT, NAMED: the event ROW. A failure against a known identity writes one
// admin_identity_events row; an unknown address cannot (identity_id is NOT NULL and there is no
// identity to name). The round trip is the same, the row's insert and commit flush are not -- the
// shape and the size of today's audit-row asymmetry (Authenticate's comment, ~1-3% of a response),
// bounded the same way, by the per-identity budget. TestAuthenticatePerson_FiveFailuresTakeOneTime
// measures it inside the five cases.
//
// THE FAILURE IS ONE VALUE. Unknown address, wrong password, an identity without an active
// membership and every legacy refusal return ErrBadCredentials and nothing else (00011 OBLIGATION 1).

// FailureBudget is the per-identity budget of failed-sign-in event rows (ADR 0031 §5.2 named exception
// 3: an adminAccountLimit bucket per identity). Declared here, at the consumer; internal/handler's
// account limiter (*httpx.Limiter) satisfies it, so one budget meters the account rows and the
// identity rows under distinct keys (identityBudgetKey).
type FailureBudget interface {
	Allowed(key string) bool
	Charge(key string) int
	FirstOverLimit(count int) bool
}

// PersonSignIn is what one sign-in through the identity established.
type PersonSignIn struct {
	// Session is the identity session, nil when the identity did not sign the person in.
	//
	// 🔴 ITS LOGIN AUDIT ROW IS ALREADY WRITTEN: identity_open_session writes the business's
	// admin.login.succeeded row itself (00071), byte for byte the handler's. The caller writes NO
	// login-succeeded row for it -- a second one would double the trail.
	Session *Issued
	// Legacy is the legacy arm: the UNLINKED rows of the address that were compared -- never the
	// identity's own rows (T2), never a refused row (00072's resolver drops unlinked_at). It is
	// meaningful only when Session is nil: then Legacy.Verified() non-empty means the person signs in
	// as today, without the identity (Issue, or the picker while two or more verify -- until
	// contract A), and an empty one comes with ErrBadCredentials. Legacy.Resolved counts unlinked
	// rows only.
	Legacy Authentication
	// Linked is how many legacy rows this sign-in linked to the identity (identity_link_legacy wrote
	// each business its own admin.membership.linked row). A number for a log line, never for an
	// audit row (R21).
	Linked int
}

// verifiedIdentity is an identity whose digest this package compared the typed password with and
// found matching -- 00011 OBLIGATION 5's twin for the identity (ADR 0031 §5.2 named exception 1:
// identity_open_session is not keyed by a secret, so THIS TYPE is the guarantee).
//
// 🔴 ONE PRODUCER, ONE CONSUMER, BOTH IN THIS FILE, and a test holds both by reading the source
// (TestVerifiedIdentity_HasOneProducerAndOneConsumer): AuthenticatePerson builds it in one place,
// right after the comparison returned true, and openIdentitySession -- the only caller of
// Database.OpenIdentitySession -- takes nothing else. Unlike Verified it is unexported, so no other
// package can build one: there is no second leg to write down.
//
// digest is the resolver's PasswordHash, handed to identity_open_session UNCHANGED (the user's
// decision of 2026-10-23): the function opens nothing unless it is still the identity's digest under
// the person's lock, so a reset that commits while this sign-in waits drops it.
type verifiedIdentity struct {
	id     uuid.UUID
	digest db.PasswordHash
}

// identityBudgetKey namespaces the identity's bucket in a budget it may share with account keys (an
// admin_user_id) -- recordCandidateProbe's "probe:" precedent: same limiter, distinct key, so one
// cannot spend the other. The identity id stays inside this process's memory.
func identityBudgetKey(id uuid.UUID) string { return "identity:" + id.String() }

// AuthenticatePerson signs a person in through their identity (see this file's header). want is the
// business asked for (uuid.Nil for none): the session lands there if it is an active membership,
// else in the one used last, else in the oldest (K-4; identity_open_session decides). budget meters
// the failure event rows per identity and must not be nil.
//
// RESULTS: a non-nil Session (signed in as the identity; any legacy rows the same password verified
// are linked); or Legacy.Verified() non-empty with a nil error (signed in as today, without the
// identity); or ErrBadCredentials. Any other error is the database's, not the credentials'.
func (m *Manager) AuthenticatePerson(ctx context.Context, email, password string, want uuid.UUID, budget FailureBudget) (PersonSignIn, error) {
	if budget == nil {
		return PersonSignIn{}, errors.New("adminauth: authenticate person: no failure budget")
	}
	// The caller's own bytes decide this branch, never server state -- Authenticate's argument.
	// Every comparison is still paid.
	if email == "" || password == "" || !isLookupableEmail(email) {
		m.payOneComparison(password, "")
		m.pad(password, "", 0)
		return PersonSignIn{}, ErrBadCredentials
	}

	ident, err := m.data.ResolveIdentityByEmail(ctx, email)
	hasIdentity := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return PersonSignIn{}, fmt.Errorf("adminauth: authenticate person: %w", err)
	}
	rows, err := m.data.GetAdminByEmail(ctx, email)
	if err != nil {
		return PersonSignIn{}, fmt.Errorf("adminauth: authenticate person: %w", err)
	}

	// THE LEGACY ARM'S CANDIDATES ARE THE UNLINKED ROWS (T2). The identity's own rows carry its
	// digest copy: left in, they fill the eight-row window, a correct password "verifies a legacy
	// row" that is already linked, and a person with eight memberships never gets the ninth, unlinked
	// one linked. A row linked to ANOTHER identity cannot share this address (every link is made
	// between equal addresses and an identity's address is unique), and if one ever did it is no
	// row identity_link_legacy could link -- it is excluded by the same test.
	legacy := make([]db.ResolvedAdmin, 0, len(rows))
	for _, r := range rows {
		if r.IdentityID == nil {
			legacy = append(legacy, r)
		}
	}
	auth := Authentication{Resolved: len(legacy)}
	if len(legacy) > MaxCandidates {
		legacy = legacy[:MaxCandidates]
	}

	// The padding's reference: a real digest of this sign-in when there is one, so the padding
	// costs what a real comparison here costs (Manager.pad's invariant).
	ref := ""
	switch {
	case hasIdentity:
		ref = ident.PasswordHash.RevealForPasswordComparison()
	case len(legacy) > 0:
		ref = legacy[0].PasswordHash.RevealForPasswordComparison()
	}

	// 1 -- the identity's comparison, or its dummy.
	var proven *verifiedIdentity
	if hasIdentity {
		if m.compare(ident.PasswordHash.RevealForPasswordComparison(), password) {
			proven = &verifiedIdentity{id: ident.ID, digest: ident.PasswordHash}
		}
	} else {
		m.payOneComparison(password, ref)
	}

	// + MaxCandidates -- the legacy window, padded. Every row is compared, disabled ones included,
	// and the status is read afterwards (Authenticate's argument).
	digests := make([]db.PasswordHash, 0, len(legacy))
	auth.Attempts = make([]Attempt, 0, len(legacy))
	auth.proofs = make([]Proof, 0, len(legacy))
	for _, c := range legacy {
		digest := c.PasswordHash.RevealForPasswordComparison()
		auth.Attempts = append(auth.Attempts, Attempt{
			AdminUserID:     c.ID,
			TenantID:        c.TenantID,
			PasswordMatched: m.compare(digest, password),
			Active:          c.Status == "active",
		})
		auth.proofs = append(auth.proofs, m.proofOf(c.ID, c.TenantID, digest))
		digests = append(digests, c.PasswordHash)
	}
	m.pad(password, ref, len(legacy))

	// THE BRANCH IS CHOSEN HERE, AFTER EVERY COMPARISON (S15): has_active_membership is read for the
	// first time on the next line.
	if proven != nil && ident.HasActiveMembership {
		in, err := m.signInAsIdentity(ctx, *proven, want, auth, digests)
		if err == nil {
			return in, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return PersonSignIn{}, fmt.Errorf("adminauth: authenticate person: %w", err)
		}
		// identity_open_session refused under the person's lock -- a reset committed since the
		// resolver read the digest, or the last active membership went. The open step was paid by
		// the real call; the sign-in continues as one the identity did not open.
	} else if err := m.decoy(ctx); err != nil {
		return PersonSignIn{}, fmt.Errorf("adminauth: authenticate person: %w", err)
	}

	if len(auth.Verified()) > 0 {
		// Only legacy rows verify: the person signs in as today, without the identity (§5.6).
		return PersonSignIn{Legacy: auth}, nil
	}
	if err := m.recordFailure(ctx, hasIdentity, ident.ID, budget); err != nil {
		return PersonSignIn{}, fmt.Errorf("adminauth: authenticate person: %w", err)
	}
	return PersonSignIn{Legacy: auth}, ErrBadCredentials
}

// signInAsIdentity opens the identity session and links every legacy row the same password verified
// (ADR 0031 §5.6, §5.7 (1)): a FRESH session on the legacy row, then a FRESH identity session, then
// identity_link_legacy with the two hashes, which revokes the legacy one. pgx.ErrNoRows means
// identity_open_session refused; every legacy session opened for the proof is revoked then.
func (m *Manager) signInAsIdentity(ctx context.Context, v verifiedIdentity, want uuid.UUID, auth Authentication, digests []db.PasswordHash) (PersonSignIn, error) {
	var proofs []proofSession
	for i, at := range auth.Attempts {
		if !at.PasswordMatched || !at.Active {
			continue
		}
		p, err := m.openLegacyProofSession(ctx, at.AdminUserID, at.TenantID, digests[i])
		if errors.Is(err, pgx.ErrNoRows) {
			// The row changed since it verified (disabled, re-passworded, linked by a concurrent
			// sign-in): nothing to link.
			continue
		}
		if err != nil {
			return PersonSignIn{}, errors.Join(err, m.revokeProofs(ctx, proofs))
		}
		proofs = append(proofs, p)
	}

	issued, identityHash, err := m.openIdentitySession(ctx, v, want)
	if err != nil {
		if rerr := m.revokeProofs(ctx, proofs); rerr != nil {
			// NOT joined: a joined pgx.ErrNoRows would read as "the open was refused" and the
			// sign-in would go on as a legacy one with proof sessions it could not revoke. A failed
			// revocation is the database failing, and the sign-in fails with it.
			return PersonSignIn{}, fmt.Errorf("adminauth: revoke the proof sessions after the open failed (%v): %w", err, rerr)
		}
		return PersonSignIn{}, err
	}

	linked := 0
	for i, p := range proofs {
		_, err := m.data.LinkLegacySession(ctx, identityHash, p.hash)
		if errors.Is(err, pgx.ErrNoRows) {
			// Refused (identity_link_legacy wrote the identity its link_refused event) and the
			// legacy session left live: revoked here, so no proof session outlives the sign-in.
			if err := m.revokeProofs(ctx, []proofSession{p}); err != nil {
				return PersonSignIn{}, errors.Join(err, m.revokeProofs(ctx, proofs[i+1:]))
			}
			continue
		}
		if err != nil {
			// The database failed mid-way: the sign-in fails (its token is dropped, so the identity
			// session it opened is unusable -- completeLogin's cookie branch is the same shape), and
			// the proof sessions not yet consumed are revoked.
			return PersonSignIn{}, errors.Join(err, m.revokeProofs(ctx, proofs[i:]))
		}
		linked++
	}
	return PersonSignIn{Session: &issued, Linked: linked}, nil
}

// proofSession is the legacy side of one link's proof: a fresh session on a row the password
// verified.
type proofSession struct {
	tenant, id uuid.UUID
	hash       string
}

// openIdentitySession is the ONE caller of Database.OpenIdentitySession, and it takes a
// verifiedIdentity and nothing else (named exception 1). It returns the session, the token and the
// token's hash -- the hash identity_link_legacy takes as the identity side of the proof. Its duration
// feeds the decoy's (openCost).
func (m *Manager) openIdentitySession(ctx context.Context, v verifiedIdentity, want uuid.UUID) (Issued, string, error) {
	t, err := newToken()
	if err != nil {
		return Issued{}, "", err
	}
	h, err := t.hash(m.hmacKey)
	if err != nil {
		return Issued{}, "", err
	}
	start := time.Now()
	s, err := m.data.OpenIdentitySession(ctx, v.id, want, h, v.digest)
	if err != nil {
		return Issued{}, "", err
	}
	if m.openCost != nil {
		m.openCost.observe(time.Since(start))
	}
	return Issued{
		Session: Session{ID: s.SessionID, TenantID: s.TenantID, AdminUserID: s.AdminUserID},
		Token:   t,
	}, h, nil
}

// openLegacyProofSession writes the legacy side of a link's proof: a session on the row whose
// password just verified, under CreateAdminSession's digest predicate and share lock (the digest is
// the one the password was compared with, from the resolver -- no Proof round trip is needed inside
// one sign-in). authenticated_at takes its DEFAULT, now(): the session is fresh. It writes no audit
// row and stamps no last_login_at: it is a proof that identity_link_legacy revokes, not a sign-in.
func (m *Manager) openLegacyProofSession(ctx context.Context, admin, tenant uuid.UUID, digest db.PasswordHash) (proofSession, error) {
	t, err := newToken()
	if err != nil {
		return proofSession{}, err
	}
	h, err := t.hash(m.hmacKey)
	if err != nil {
		return proofSession{}, err
	}
	var row store.CreateAdminSessionRow
	err = m.data.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).CreateAdminSession(ctx, store.CreateAdminSessionParams{
			TokenHash:      h,
			AdminUserID:    admin,
			TenantID:       tenant,
			VerifiedDigest: digest.RevealForPasswordComparison(),
		})
		return e
	})
	if err != nil {
		return proofSession{}, err
	}
	return proofSession{tenant: tenant, id: row.ID, hash: h}, nil
}

// revokeProofs revokes proof sessions that no link consumed. Every one is attempted; the failures
// are joined and returned (an already revoked session is no failure -- RevokeAdminSession keeps the
// first stamp).
func (m *Manager) revokeProofs(ctx context.Context, proofs []proofSession) error {
	var errs []error
	for _, p := range proofs {
		if err := m.Revoke(ctx, p.tenant, p.id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// recordFailure is every failure's ONE event step (this file's header): the identity's
// login_failed row within its budget, its single login_limited row at the first refusal past it
// (adminratelimit.go's FirstOverLimit pattern), and otherwise -- no identity, or a spent budget --
// the same call for an id no identity carries, which writes nothing.
func (m *Manager) recordFailure(ctx context.Context, hasIdentity bool, id uuid.UUID, budget FailureBudget) error {
	target, event := uuid.Nil, db.IdentityEventLoginFailed
	if hasIdentity {
		key := identityBudgetKey(id)
		if budget.Allowed(key) {
			budget.Charge(key)
			target = id
		} else if budget.FirstOverLimit(budget.Charge(key)) {
			target, event = id, db.IdentityEventLoginLimited
		}
	}
	if _, err := m.data.RecordFailedIdentityLogin(ctx, target, event); err != nil {
		return err
	}
	return nil
}

// payOneComparison is one comparison whose answer is discarded, against digest or -- with none --
// the manager's dummy (Manager.pad's fallback, one step of it).
func (m *Manager) payOneComparison(password, digest string) {
	if digest == "" {
		digest = m.dummy
		if digest == "" {
			digest = dummyDigest
		}
	}
	_ = m.compare(digest, password)
}

// decoy is the open step of a sign-in that opens no identity session (R14, S15): ONE round trip
// (db.IdentitySessionDecoy: a transaction id and its commit, no row), then a wait until what
// identity_open_session has been measured to take (openCost: the median of this process's recent real
// opens, at most openCostCeiling). See ADR 0031 §5.6's M14-07 note for why it is a wait and not equal
// database work: no statement tappa_app may run reproduces the open's writes without writing something
// real (every table it can write is tenant-scoped), and the five FAILURE cases do not need it -- they
// are flat because every one of them makes this same call; the wait only makes a failure's open step
// as long as a success's, and success is visible from the response anyway.
//
// THE WAIT HITS A DEADLINE, it does not sleep "about" the rest. A timer wakes late by the machine's
// state (~0.1 ms on an idle workstation, ~0.9 ms on a loaded CI runner under -race, where a plain
// sleep made the decoy 1.57x and 1.60x the open). So it sleeps until 1 ms before the deadline and
// yields (runtime.Gosched) through the last millisecond: the lateness of the sleep is absorbed by the
// spin, and the spin costs at most one millisecond of a scheduler slot per failed sign-in.
func (m *Manager) decoy(ctx context.Context) error {
	start := time.Now()
	if err := m.data.IdentitySessionDecoy(ctx); err != nil {
		return err
	}
	typical := openCostDefault
	if m.openCost != nil {
		typical = m.openCost.typical()
	}
	return waitUntil(ctx, start.Add(typical))
}

// decoySpin is how much of a wait waitUntil yields through instead of sleeping.
const decoySpin = time.Millisecond

// waitUntil returns at deadline (or when ctx ends): a timer for all but the last decoySpin, then
// runtime.Gosched until the clock passes it.
func waitUntil(ctx context.Context, deadline time.Time) error {
	if rest := time.Until(deadline) - decoySpin; rest > 0 {
		timer := time.NewTimer(rest)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		runtime.Gosched()
	}
	return nil
}

// costMeter keeps the last openCostSamples durations of identity_open_session and answers their
// median. Before the first observation it answers its fallback (openCostDefault); it never answers
// more than openCostCeiling, so a stalled database cannot make every failed sign-in wait long.
type costMeter struct {
	mu       sync.Mutex
	samples  []time.Duration
	next     int
	fallback time.Duration
}

const (
	openCostSamples = 32
	// openCostDefault is the open's median measured on the reference stack (see decoy).
	openCostDefault = 2 * time.Millisecond
	openCostCeiling = 250 * time.Millisecond
)

func newCostMeter(fallback time.Duration) *costMeter { return &costMeter{fallback: fallback} }

func (c *costMeter) observe(d time.Duration) {
	if d <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.samples) < openCostSamples {
		c.samples = append(c.samples, d)
		return
	}
	c.samples[c.next] = d
	c.next = (c.next + 1) % openCostSamples
}

func (c *costMeter) typical() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.samples) == 0 {
		return c.fallback
	}
	s := append([]time.Duration(nil), c.samples...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	d := s[len(s)/2]
	if d > openCostCeiling {
		return openCostCeiling
	}
	return d
}
