package adminauth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WHO GETS A RESET LINK — the question db/queries/passwordresets.sql calls "phase
// B's hardest question rather than an omission", and ADR 0015's rejected
// alternative 3 refuses to answer by re-asking for the address on the reset page.
//
// 🔴 THE ANSWER LIVES HERE AND NOT IN A HANDLER, because it is a rule about
// identities: CLAUDE.md §3 keeps business rules out of internal/handler, and the
// rule this file implements is the SAME rule Authenticate implements one file away.
// Splitting them would let the two windows drift, and the whole argument below is
// that they must not.

// ResetWindow is how many resolved identities ONE reset request mints a link for.
//
// 🔴 IT IS MaxCandidates, DELIBERATELY, AND THE M7-04 CARD WARNED THAT REUSING THAT
// WINDOW "REPRODUCES 00017's RESIDUAL LOCK ON THIS SURFACE". It does — and the
// measurement below says the alternative is WORSE, so the reuse is a decision with a
// price rather than an inheritance.
//
// WHAT A RESET LINK IS FOR: restoring the ability to SIGN IN. Signing in compares
// the first MaxCandidates identities an address resolves to (manager.go), so an
// identity OUTSIDE that window cannot sign in whatever its password is — the M7-04
// card's own correction 4 says it in one line: "parolayı değiştirmek satırı pencereye
// taşımaz". Minting a link for such an identity would hand somebody a credential that
// CANNOT do the thing they asked for, i.e. a silent failure dressed as a remedy,
// which is the §4.6 shape. So the reset window is the login window: never narrower
// (that would refuse recovery to an account that CAN sign in) and never wider (that
// would promise recovery to one that cannot).
//
// THE THREE READINGS THAT WERE MEASURED, over the development database
// (`SELECT max(n) FROM (SELECT count(*) n FROM admin_users GROUP BY email) k` and its
// siblings, 48 273 distinct addresses, 57 235 rows):
//
//	UNCAPPED, one link per candidate    max 500 candidates for one address, i.e. up
//	                                    to 500 INSERTs and 1000 tenant-scoped
//	                                    transactions for ONE unauthenticated POST.
//	                                    ELIMINATED: an amplifier an attacker sizes
//	                                    themselves, on a public form.
//	CAP OF 1 (the incumbent only)       constant work, and it silently refuses
//	                                    recovery to the SECOND business of anybody
//	                                    who owns two under one address — 1 675
//	                                    addresses in this database resolve to more
//	                                    than one row. ELIMINATED: it is the same
//	                                    "cannot recover" harm the cap is supposed to
//	                                    bound, aimed at a real customer instead of an
//	                                    attacker.
//	CAP OF MaxCandidates (SHIPPED)      bounded work, and exactly the identities that
//	                                    could sign in if they knew the password.
//
// ⚠️ THE RESIDUAL IS REAL AND IS NOT CLAIMED AWAY: 378 addresses in this database
// resolve to MORE than MaxCandidates rows, and an identity past the window gets no
// link. That is 00017's stated limit ("an attacker who plants eight or more rows
// carrying an address BEFORE that address ever signs up still owns the whole
// window"), reaching this surface unchanged rather than newly created by it — and the
// thing that closes it is the same thing 00017 names: address verification, which
// needs the transport Q02 is about.
const ResetWindow = MaxCandidates

// ResetGrant is one issued link plus the address it must be delivered to.
//
// 🔴 Recipient IS READ FROM THE ADMINISTRATOR'S OWN ROW, NEVER FROM THE REQUEST, and
// that is the M7-04 acceptance criterion "link yalnızca yöneticinin KENDİ satırındaki
// adrese gider" made structural rather than promised. The typed-in address and the
// row's address are equal under citext (00011's resolver compares with
// OPERATOR(public.=)), which is a CASE-INSENSITIVE equality — so they can differ in
// case, and SMTP local-parts are case-sensitive in principle. Delivering to the typed
// spelling would therefore be delivering to an address the database never stored.
//
// ⚠️ IT IS PERSONAL DATA AND IT IS NOT A §4.7 SECRET. Neither is it loggable: every
// log line in this flow is written WITHOUT the address, for the reason
// internal/handler/adminlogin.go gives on the unauthenticated sign-in path.
type ResetGrant struct {
	Issued    IssuedReset
	Recipient string
}

// IssueForEmail mints a reset link for every administrator an address resolves to
// INSIDE the reset window WHO DOES NOT ALREADY HOLD A LIVE ONE, and returns each new
// link with the address on that administrator's own row (grants) — and, for every
// administrator who does hold one, that live link's record (kept), so the caller can
// write the trail of a request that minted nothing.
//
// 🔴 ONE LIVE LINK PER ACCOUNT (M10 EM-7C, ADR 0022's EM-7C note). While an
// administrator holds a link that could be spent RIGHT NOW — not consumed, not
// retired, not expired, ListLivePasswordResetsForAdmin's three conditions, which are
// the consume statement's own — a request for their address mints NOTHING for them,
// retires NOTHING and sends NOTHING. Two harms close together:
//
//   - ADR 0015's harm (a) ON THE REQUEST PATH. Before EM-7C every request retired the
//     account's live link (CreatePasswordReset's fused retirement), so anybody who
//     knew an address could kill the owner's pending link by asking again — once per
//     request, bounded only by the request budgets. Now a request that finds a live
//     link leaves it alone: the owner's link stays spendable until it expires or is
//     spent, whatever anybody else types into the form.
//   - THE PER-RECIPIENT SEND CEILING for recovery (EM-5B (b)). An account receives at
//     most ONE recovery e-mail per ResetTTL from this path: the next can be minted
//     only after the last one died. The ceiling IS ResetTTL — shortening the TTL
//     raises it, and migration 00019's 24-hour span ceiling is its floor (one a day).
//
// 🔴 AND IT NEVER LEAVES A WINDOW WITH NO LIVE LINK AND NO WAY TO GET ONE. That is
// what a per-MAILBOX send counter did (EM-7C round 2, measured by a security audit):
// whoever filled the count could keep the owner from asking for an hour or a day
// while the owner's last link had already expired. Here the rule's state is the
// link itself: the moment it stops being spendable, the next request mints. And
// because the state is per ACCOUNT, an account somebody else registered under the
// same mailbox (a "+tag" or a capitalised spelling in another business — open
// signup allows it) has its own link and changes nothing about the owner's.
//
// WHY THE DATABASE AND NOT A MAP IN THIS PROCESS: the link's liveness is decided by
// the database's clock, at the instant the live-link read runs (statement_timestamp()
// — after the lock was granted, so a link that expired while the request waited is not
// "live"; db/queries/passwordresets.sql says why that and the consume statement's now()
// mean the same instant) — so a request is never refused because of a link that could
// not actually be spent, and never keeps one that could not. A
// map in the process would not see a link consumed or expired, would forget
// everything at a restart (and every deploy restarts), and would disagree between
// two processes; the database is already the authority on all three.
//
// THE READ-THEN-MINT IS SERIALISED PER ADMINISTRATOR (LockAdminForResetIssue, the
// first statement of the transaction that reads and mints): concurrent requests for
// one administrator would otherwise all read "no live link" and all mint.
//
// 🔴 IT ANSWERS THE SAME WAY FOR A REGISTERED AND AN UNREGISTERED ADDRESS — there is
// ONE return shape (two slices and a nil error) and an unknown address simply yields
// two empty slices. There is no ErrNoSuchAdmin here and no "not found" error for a caller
// to branch on, because a caller that CAN branch is a caller that can leak
// (00011's OBLIGATION 1). The split between grants and kept is NOT a difference the
// caller may show: it decides what is sent and what the trail says, never the answer
// (internal/handler's Request renders one page after one floor for all three). issue's own ErrNoSuchAdmin is swallowed for exactly that
// reason and only for that error: the row was resolved and then found inactive
// between two statements, which is not a fact the requester may learn.
//
// ⚠️ WHAT IT DOES NOT DO IS EQUALISE TIMING, and that is deliberate rather than
// forgotten: the work here is proportional to the number of identities in the window,
// so an unregistered address costs one resolver read and a registered one costs two
// transactions per identity. The RESPONSE is what must be indistinguishable, and
// flattening it is the caller's job because only the caller owns the clock the
// visitor sees (internal/handler's resetRequestFloor). This function is measured, not
// padded — padding a database round trip would mean holding a pool connection to
// waste it.
//
// DISABLED ADMINISTRATORS ARE INSIDE THE WINDOW AND OUTSIDE THE RESULT. They occupy a
// slot exactly as they do in Authenticate's comparison loop — dropping them before
// the cap would make the window's CONTENTS depend on how many disabled rows an
// address has, which is a fact about the database answered by how many links arrive.
func (r *Resets) IssueForEmail(ctx context.Context, email string) ([]ResetGrant, []Reset, error) {
	// The same three refusals Authenticate makes, in the same place and for the same
	// reasons: an address that cannot be a lookup key never reaches the driver, where
	// a NUL byte or invalid UTF-8 would come back as a DATABASE ERROR and turn an
	// unauthenticated form into a 500.
	email = strings.TrimSpace(email)
	if email == "" || !isLookupableEmail(email) {
		return nil, nil, nil
	}

	candidates, err := r.data.GetAdminByEmail(ctx, email)
	if err != nil {
		// A database failure is NOT "unknown address". Collapsing them would hide an
		// outage behind a screen saying a link is on its way.
		return nil, nil, fmt.Errorf("adminauth: issue reset for address: %w", err)
	}
	if len(candidates) > ResetWindow {
		candidates = candidates[:ResetWindow]
	}

	grants := make([]ResetGrant, 0, len(candidates))
	var kept []Reset
	for _, c := range candidates {
		// ⚠️ THIS TEST IS THE THIRD OF THREE AND IT IS MEASURED NOT TO BE LOAD-BEARING,
		// which is written down rather than left for somebody to discover by deleting
		// it. A disabled administrator is refused independently by: (1) this line,
		// (2) the status test inside recipient below, and (3) CreatePasswordReset's own
		// `a.status = 'active'` predicate, which is the AUTHORITY (db/queries/
		// passwordresets.sql: "0 rows -> pgx.ErrNoRows -> the caller says nothing
		// different to the user").
		//
		// MEASURED: replacing THIS test with `false` left the whole IssueForEmail suite
		// green, and so did removing this one AND recipient's together — the SQL alone
		// holds. It is kept for two reasons that are honest but small: it saves a
		// pointless round trip for a row the database will refuse anyway, and the SQL
		// predicate lives in a file this one does not compile against, so the cheapest
		// possible change over there would otherwise land here silently.
		if c.Status != "active" {
			continue
		}
		// THE ADDRESS IS READ BEFORE THE TOKEN IS MINTED, which is the fail-closed
		// order: a link nobody can be given is a link that should not exist, and
		// minting first would leave a live credential behind whenever this read fails.
		// It also costs nothing extra in the common case, because the read runs only
		// for identities the resolver already returned.
		to, err := r.recipient(ctx, c.TenantID, c.ID)
		if err != nil {
			return nil, nil, err
		}
		if to == "" {
			// Disabled or gone between the resolver and this read. Skipped in silence
			// here and recorded by the caller's trail, never by a different response.
			continue
		}
		issued, live, err := r.issueUnlessLive(ctx, c.TenantID, c.ID)
		if err != nil {
			if errors.Is(err, ErrNoSuchAdmin) {
				continue
			}
			return nil, nil, err
		}
		if live != nil {
			kept = append(kept, *live)
			continue
		}
		grants = append(grants, ResetGrant{Issued: issued, Recipient: to})
	}
	return grants, kept, nil
}

// issueUnlessLive is IssueForEmail's per-account step: under the administrator's
// lock, the live links are read and a new one is minted only when there is none.
// Exactly one of the two results is set: the minted link, or the newest live one.
//
// THE TOKEN IS DRAWN BEFORE THE TRANSACTION so the lock is held for three statements
// and no crypto; when a live link is found it is simply dropped (it was never stored,
// and ResetToken cannot print itself).
//
// The mint is CreatePasswordReset, unchanged, so its fused retirement still runs —
// and retires nothing on this path, because the read just above found nothing live
// under a lock every other issuer on this path takes too.
func (r *Resets) issueUnlessLive(ctx context.Context, tenantID, adminUserID uuid.UUID) (IssuedReset, *Reset, error) {
	t, h, err := r.newHashedToken()
	if err != nil {
		return IssuedReset{}, nil, fmt.Errorf("adminauth: issue reset: %w", err)
	}
	var (
		row  store.CreatePasswordResetRow
		live *Reset
	)
	err = r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if e := q.LockAdminForResetIssue(ctx, store.LockAdminForResetIssueParams{
			AdminUserID: adminUserID, TenantID: tenantID,
		}); e != nil {
			return e
		}
		rows, e := q.ListLivePasswordResetsForAdmin(ctx, store.ListLivePasswordResetsForAdminParams{
			TenantID: tenantID, AdminUserID: adminUserID,
		})
		if e != nil {
			return e
		}
		if len(rows) > 0 {
			// Newest first (the query's ORDER BY). More than one live link exists only
			// if something minted outside this path (this package's unexported issue,
			// or a row from before EM-7C); the newest is the one a mailbox received last.
			live = &Reset{
				ID:          rows[0].ID,
				TenantID:    rows[0].TenantID,
				AdminUserID: rows[0].AdminUserID,
				CreatedAt:   rows[0].CreatedAt,
				ExpiresAt:   rows[0].ExpiresAt,
			}
			return nil
		}
		row, e = r.create(ctx, q, tenantID, adminUserID, h)
		return e
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return IssuedReset{}, nil, ErrNoSuchAdmin
	}
	if err != nil {
		return IssuedReset{}, nil, fmt.Errorf("adminauth: issue reset: %w", err)
	}
	if live != nil {
		return IssuedReset{}, live, nil
	}
	return issuedFrom(row, t), nil, nil
}

// Withdraw retires ONE link that was minted and could not be handed to its
// recipient (M10 EM-7C): the outbox was full or closing, the breaker refused, the
// relay failed, or the process stopped first.
//
// 🔴 WITHOUT IT, ONE LIVE LINK PER ACCOUNT WOULD TURN A FAILED SEND INTO AN HOUR OF
// NOTHING: the undelivered link is live, so every request for the account in the
// next ResetTTL would find it and mint nothing, while no inbox holds it. Withdrawing
// it makes the next request mint a fresh one. It is fail-closed in the other
// direction too: a send that timed out may still have reached the mailbox, and that
// link then dies — the owner asks again and gets a new one at once.
//
// Only cancelled_at moves, only from NULL, and only for a link neither spent nor
// already retired (WithdrawPasswordReset); a link that is gone already is not an
// error.
func (r *Resets) Withdraw(ctx context.Context, tenantID, resetID uuid.UUID) error {
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return store.New(tx).WithdrawPasswordReset(ctx, store.WithdrawPasswordResetParams{
			TenantID: tenantID, ID: resetID,
		})
	})
	if err != nil {
		return fmt.Errorf("adminauth: withdraw reset: %w", err)
	}
	return nil
}

// NoticeRecipient is the address the "your password was changed" notice goes to (M10
// EM-9): the administrator's OWN row, read inside their own tenant AFTER the change
// committed. It is recipient below, exported, and nothing more — the same "read
// from the row, never from a request" rule ResetGrant states, applied to the second
// e-mail that goes to an administrator.
//
// "" means there is nowhere to send it: the row is gone, no longer active, or has no
// address (admin_users.email is nullable). The caller records that; it is not an
// error. The address is personal data and is not logged here or by the caller.
func (r *Resets) NoticeRecipient(ctx context.Context, tenantID, adminUserID uuid.UUID) (string, error) {
	return r.recipient(ctx, tenantID, adminUserID)
}

// recipient reads one administrator's own address inside their own tenant. It
// returns "" when the row is gone or no longer active, which the caller treats as
// "no link for this identity" rather than as an error.
func (r *Resets) recipient(ctx context.Context, tenantID, adminUserID uuid.UUID) (string, error) {
	var email string
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).GetAdminByID(ctx, store.GetAdminByIDParams{
			ID: adminUserID, TenantID: tenantID,
		})
		if e != nil {
			return e
		}
		if row.Status != "active" || row.Email == nil {
			// admin_users.email is nullable in the schema (00006). A row with no
			// address has nowhere for a link to go, and inventing one is the one
			// mistake this whole function exists to make impossible.
			return nil
		}
		email = *row.Email
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("adminauth: read recovery address: %w", err)
	}
	return email, nil
}
