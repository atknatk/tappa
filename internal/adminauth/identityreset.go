package adminauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// THE PER-IDENTITY RESET REQUEST (M14-14, ADR 0031 §5.9). With TAPPA_ADMIN_IDENTITY on
// (SurfaceResetPerIdentity -- the handler asks the gate, flag.go), a recovery request no longer mints a
// link per administrator row: an address names at most one PERSON (admin_identities.email is globally
// unique), and that person gets ONE link that resets their password in every business linked to them
// (identity_consume_reset). Rows not linked to an identity keep today's per-row link until contract
// (ADR 0031 §5.9: "bağlanmamış satırlar contract'a dek satır başı link alır") -- issueForRows, the
// same code IssueForEmail runs.
//
// ONE LIVE LINK PER IDENTITY (EM-7C at the identity level). identity_open_reset reads the identity's
// live link under a per-identity advisory lock: while one exists it writes nothing, retires nothing and
// returns that link with issued = false -- so nothing is sent (IssueForEmail's argument, verbatim: no
// stranger can kill the owner's pending link by asking again, and the mailbox receives at most one
// recovery e-mail per link lifetime). An undelivered link is withdrawn (WithdrawIdentity) so the next
// request can open a fresh one.
//
// 🔴 WHO RECEIVES IT: THE ADDRESS THE DATABASE STORES FOR THE IDENTITY, BOUND TO THE ID BY A TYPE.
// identity_open_reset is keyed by an identity id, not by a secret (ADR 0031 §5.2's named exception
// "yazar ama kimlik yazmaz"), so the function cannot check where its link will be sent -- M14-01's
// security audit (inherited note (a)) asked for the binding in Go. identityRecipient is that binding:
// its only constructor takes ONE db.ResolvedIdentity, and openForIdentity takes nothing else -- the id
// the link is opened for and the address it goes to are the same resolver row's two columns
// (00075 put the registered address on that row). Never the typed spelling: equal under citext is not
// equal byte for byte (ResetGrant's rule since M7-04). TestIdentityRecipient_IsOneResolverRow pins the
// shape from the source; TestIssueForPerson_SendsToTheIdentitysRegisteredAddress measures it on
// Postgres.
//
// 🔴 THE ANSWER IS THE SAME FOR EVERY ADDRESS (IssueForEmail's rule; the handler renders one page after
// one floor). No error distinguishes "no identity" from "an identity": both are nil errors with
// different trails.

// identityRecipient is one resolve_identity_by_email row's id and registered address, together. It is
// unexported and built only by recipientOf, so no caller can pair an id with an address from somewhere
// else.
type identityRecipient struct {
	id      uuid.UUID
	address string
}

// recipientOf is identityRecipient's only constructor: both fields from ONE resolver row.
func recipientOf(r db.ResolvedIdentity) identityRecipient {
	return identityRecipient{id: r.ID, address: r.Email}
}

// IdentityResetKept is the per-identity link a request found live (EM-7C): nothing was opened, retired
// or sent. It carries no address and no token.
type IdentityResetKept struct {
	IdentityID uuid.UUID
	ResetID    uuid.UUID
	ExpiresAt  time.Time
}

// PersonResets is what IssueForPerson did.
type PersonResets struct {
	// Grants: the identity's new link first (IdentityID set) when one was opened, then one per-row grant
	// per unlinked row that held no live link. Each must be delivered, or withdrawn when it cannot be.
	Grants []ResetGrant
	// Kept: the unlinked rows that already held a live per-row link (IssueForEmail's kept).
	Kept []Reset
	// IdentityKept is set when the identity already held a live link.
	IdentityKept *IdentityResetKept
}

// Empty reports whether the request resolved to nothing at all -- the trail's "unknown address".
func (p PersonResets) Empty() bool {
	return len(p.Grants) == 0 && len(p.Kept) == 0 && p.IdentityKept == nil
}

// IssueForPerson is the per-identity request (this file's header): the address's identity gets at most
// one live link; the address's rows NOT linked to an identity get today's per-row links, inside
// ResetWindow. An error leaves no live link it minted (withdrawAfterFailure). An identity with no active, unrefused membership gets no link -- it could not sign in
// with any password, the per-row path's rule for a disabled row.
//
// The linked rows are taken out BEFORE the window is cut (AuthenticatePerson's legacy arm, T2): left
// in, the identity's own rows -- which the identity link already covers -- would fill the window and
// push an unlinked row out of recovery.
func (r *Resets) IssueForPerson(ctx context.Context, email string) (PersonResets, error) {
	email = strings.TrimSpace(email)
	if email == "" || !isLookupableEmail(email) {
		return PersonResets{}, nil
	}

	// THE READS FIRST, THE IDENTITY'S MINT LAST (M14-14's security audit, measured). Every statement
	// that can fail -- the two resolvers, the per-row mints -- runs before identity_open_reset, so no
	// failure can come AFTER the identity's link committed and leave it minted, undelivered and
	// unwithdrawn: under one live link per identity that orphan answered every later request of the
	// owner "kept" for ResetTTL -- no recovery in ANY business, repeatable by anyone who could make a
	// request fail after the mint (a dropped connection is enough). And whatever DID get minted when an
	// error is returned anyway is withdrawn (withdrawAfterFailure), so an error never leaves a live link
	// nobody holds.
	ident, err := r.data.ResolveIdentityByEmail(ctx, email)
	hasIdentity := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return PersonResets{}, fmt.Errorf("adminauth: issue reset for person: %w", err)
	}
	rows, err := r.data.GetAdminByEmail(ctx, email)
	if err != nil {
		return PersonResets{}, fmt.Errorf("adminauth: issue reset for person: %w", err)
	}
	unlinked := make([]db.ResolvedAdmin, 0, len(rows))
	for _, row := range rows {
		if row.IdentityID == nil {
			unlinked = append(unlinked, row)
		}
	}
	if len(unlinked) > ResetWindow {
		unlinked = unlinked[:ResetWindow]
	}
	grants, kept, err := r.issueForRows(ctx, unlinked)
	if err != nil {
		return PersonResets{}, err
	}
	out := PersonResets{Grants: grants, Kept: kept}
	if hasIdentity && ident.HasActiveMembership {
		grant, identityKept, err := r.openForIdentity(ctx, recipientOf(ident))
		if err != nil {
			return PersonResets{}, r.withdrawAfterFailure(ctx, err, grants)
		}
		if grant != nil {
			out.Grants = append([]ResetGrant{*grant}, out.Grants...)
		}
		out.IdentityKept = identityKept
	}
	return out, nil
}

// withdrawGrace bounds the withdrawals a failed request makes (withdrawAfterFailure, openForIdentity).
// They run without the request's cancellation -- the failure is often that cancellation -- but not
// without a deadline: a hung pool or database must not hold the request's goroutine forever. Five
// seconds, internal/handler's resetAuditGrace for the same kind of after-the-fact write.
const withdrawGrace = 5 * time.Second

// withdrawContext is ctx without its cancellation, bounded by withdrawGrace.
func withdrawContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), withdrawGrace)
}

// withdrawAfterFailure retires every link minted by a request that is about to answer err, so a
// failed request leaves no live link that no inbox will ever hold (EM-7C's Withdraw reason: one live
// link per account -- or per identity -- would otherwise refuse the owner's next request for
// ResetTTL). It runs WITHOUT the request's cancellation: the failure is often that very cancellation
// (a dropped connection). A withdrawal that fails too is joined to err -- never swallowed (§7) -- and
// the caller logs it.
func (r *Resets) withdrawAfterFailure(ctx context.Context, err error, minted []ResetGrant) error {
	base, cancel := withdrawContext(ctx)
	defer cancel()
	errs := []error{err}
	for _, g := range minted {
		var werr error
		if g.IdentityID != uuid.Nil {
			werr = r.WithdrawIdentity(base, g.Issued.Token)
		} else {
			werr = r.Withdraw(base, g.Issued.Reset.TenantID, g.Issued.Reset.ID)
		}
		if werr != nil {
			errs = append(errs, werr)
		}
	}
	return errors.Join(errs...)
}

// openForIdentity opens the identity's link, or reports the live one it holds. Exactly one result is
// non-nil, or neither when the identity vanished between the resolver and this call.
//
// It takes an identityRecipient and nothing else, so the id the link is opened for and the address the
// grant names come from one resolver row (this file's header). The token is drawn before the call and
// simply dropped when the identity already holds a live link (its hash was never stored).
func (r *Resets) openForIdentity(ctx context.Context, who identityRecipient) (*ResetGrant, *IdentityResetKept, error) {
	if who.address == "" {
		// admin_identities.email is NOT NULL; an empty one would be a link nobody can be given, and
		// minting it would leave a live credential behind (IssueForEmail's fail-closed order).
		return nil, nil, nil
	}
	t, h, err := r.newHashedToken()
	if err != nil {
		return nil, nil, fmt.Errorf("adminauth: issue identity reset: %w", err)
	}
	o, err := r.data.OpenIdentityReset(ctx, who.id, h, r.now().Add(r.ttl))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		// THE CALL MAY HAVE COMMITTED: an error here can arrive after the server wrote the link (a
		// cancelled context, a dropped connection between COMMIT and its answer). The link's hash is in
		// hand, so it is withdrawn whatever happened -- a link that was never written withdraws nothing.
		err = fmt.Errorf("adminauth: issue identity reset: %w", err)
		wctx, cancel := withdrawContext(ctx)
		_, werr := r.data.WithdrawIdentityReset(wctx, h)
		cancel()
		if werr != nil {
			err = errors.Join(err, fmt.Errorf("adminauth: withdraw identity reset: %w", werr))
		}
		return nil, nil, err
	}
	if !o.Issued {
		return nil, &IdentityResetKept{IdentityID: who.id, ResetID: o.ResetID, ExpiresAt: o.ExpiresAt}, nil
	}
	return &ResetGrant{
		Issued:     IssuedReset{Reset: Reset{ID: o.ResetID, ExpiresAt: o.ExpiresAt}, Token: t},
		Recipient:  who.address,
		IdentityID: who.id,
	}, nil, nil
}

// WithdrawIdentity retires a per-identity link that could not be handed to its recipient (Withdraw's
// reason, at the identity level: one live link per identity would otherwise turn a failed send into a
// link lifetime of nothing). Keyed by the link's own hash; a link already gone is not an error. It
// writes nothing else (ADR 0031 §5.2: the identity has no tenant, its trail is the opening's
// reset_requested row).
func (r *Resets) WithdrawIdentity(ctx context.Context, t ResetToken) error {
	h, err := t.hash(r.hmacKey)
	if err != nil {
		return fmt.Errorf("adminauth: withdraw identity reset: %w", err)
	}
	if _, err := r.data.WithdrawIdentityReset(ctx, h); err != nil {
		return fmt.Errorf("adminauth: withdraw identity reset: %w", err)
	}
	return nil
}
