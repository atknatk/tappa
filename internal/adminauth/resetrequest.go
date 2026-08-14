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
// INSIDE the reset window, and returns each link with the address on that
// administrator's own row.
//
// 🔴 IT ANSWERS THE SAME WAY FOR A REGISTERED AND AN UNREGISTERED ADDRESS — there is
// ONE return shape (a slice and a nil error) and an unknown address simply yields an
// empty slice. There is no ErrNoSuchAdmin here and no "not found" error for a caller
// to branch on, because a caller that CAN branch is a caller that can leak
// (00011's OBLIGATION 1). Issue's own ErrNoSuchAdmin is swallowed for exactly that
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
func (r *Resets) IssueForEmail(ctx context.Context, email string) ([]ResetGrant, error) {
	// The same three refusals Authenticate makes, in the same place and for the same
	// reasons: an address that cannot be a lookup key never reaches the driver, where
	// a NUL byte or invalid UTF-8 would come back as a DATABASE ERROR and turn an
	// unauthenticated form into a 500.
	email = strings.TrimSpace(email)
	if email == "" || !isLookupableEmail(email) {
		return nil, nil
	}

	candidates, err := r.data.GetAdminByEmail(ctx, email)
	if err != nil {
		// A database failure is NOT "unknown address". Collapsing them would hide an
		// outage behind a screen saying a link is on its way.
		return nil, fmt.Errorf("adminauth: issue reset for address: %w", err)
	}
	if len(candidates) > ResetWindow {
		candidates = candidates[:ResetWindow]
	}

	grants := make([]ResetGrant, 0, len(candidates))
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
			return nil, err
		}
		if to == "" {
			// Disabled or gone between the resolver and this read. Skipped in silence
			// here and recorded by the caller's trail, never by a different response.
			continue
		}
		issued, err := r.Issue(ctx, c.TenantID, c.ID)
		if err != nil {
			if errors.Is(err, ErrNoSuchAdmin) {
				continue
			}
			return nil, err
		}
		grants = append(grants, ResetGrant{Issued: issued, Recipient: to})
	}
	return grants, nil
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
