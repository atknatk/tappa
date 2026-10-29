package adminauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// identitylinkemail_db_test.go -- M14-22 against real Postgres: the link of a person's other businesses
// by e-mail proof (00084, identityaccount.go) end to end through this package -- the banner's answer,
// the request, the press -- with 1, 2 and 10 businesses, every refusal, two presses at once, and the
// lock order against the person's other acts.

// forgetLinks takes back the verification table's rows of w before newIDWorld's cleanup takes the
// identity (t.Cleanup is last-in, first-out).
func forgetLinks(t *testing.T, w *idWorld) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := w.owner.Exec(context.Background(), `DELETE FROM admin_identity_verifications WHERE identity_id = $1`, w.ident); err != nil {
			t.Errorf("cleanup the links: %v", err)
		}
	})
}

// homoglyph is w's address with its first ASCII 'i' written as U+0130 -- citext folds it onto the
// address (00076's measurement); a row under it must never be linked.
func (w *idWorld) homoglyph() string {
	return strings.Replace(w.email, "i", "İ", 1)
}

// TestLinkByEmail_OneTwoAndTenBusinessesJoinInOneClick is the card's behaviour (criteria 3-6, 10): a
// person signed in to A, with n other businesses whose rows carry the same address and their OWN
// passwords (unlinked -- M14-06's backfill refused to merge them), asks for the link and spends it with
// A's session. Then: the banner said yes before and says no after; every one of the n rows is linked
// ('email'), carries the identity's digest, its live session is revoked and its business has ONE
// admin.membership.linked row naming nothing else; the old passwords no longer sign in, the identity's
// does; the person's businesses are n + 1 and a switch reaches the last one; the link is spent; the
// address is verified (the first mailbox proof). Never linked: a refused row, a disabled row, a row in
// a business where the identity already is, and a homoglyph row (6b).
func TestLinkByEmail_OneTwoAndTenBusinessesJoinInOneClick(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	owner := ownerConn(t)
	for _, n := range []int{1, 2, 10} {
		t.Run(fmt.Sprintf("%d businesses", n), func(t *testing.T) {
			w := newIDWorld(t, owner, true)
			forgetLinks(t, w)
			ta := w.tenant("M14-22 Home")
			w.linked(ta, "active", "backfill", 72*time.Hour)
			oldPW := "old-" + uuid.NewString()
			tenants := make([]uuid.UUID, n)
			rows := make([]uuid.UUID, n)
			for i := range n {
				tenants[i] = w.tenant(fmt.Sprintf("M14-22 Other %d", i))
				// rows[0] the oldest: the legacy sign-in below compares the incumbent first (MaxCandidates).
				rows[i] = w.row(tenants[i], fixtureDigest(t, oldPW), "active", time.Duration(n-i+1)*time.Hour)
			}
			// The never-linked: refused, disabled, a homoglyph spelling.
			tr, td, th := w.tenant("M14-22 Refused"), w.tenant("M14-22 Disabled"), w.tenant("M14-22 Homoglyph")
			refused := w.row(tr, fixtureDigest(t, oldPW), "active", time.Hour)
			w.exec(`UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, refused)
			disabled := w.row(td, fixtureDigest(t, oldPW), "disabled", time.Hour)
			glyph := uuid.New()
			w.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			        VALUES ($1, $2, 'M14-22 Glyph', $3, $4, 'owner', 'active')`, glyph, th, w.homoglyph(), fixtureDigest(t, oldPW))
			t.Cleanup(func() {
				w.exec(`UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL WHERE id = $1`, glyph)
			})

			ctx := context.Background()
			// A live session on the first other business, opened with its old password.
			_, legacyTok := w.legacyOn(m, oldPW, tenants[0])
			ra, tok := w.verifiedOn(m, ta)
			if add, err := m.HasBusinessToAdd(ctx, ra); err != nil || !add {
				t.Fatalf("the banner before the link: %v, %v; want yes", add, err)
			}
			issued, err := m.RequestLink(ctx, ra, tok)
			if err != nil || issued.Recipient != w.email || !issued.ExpiresAt.After(time.Now().Add(23*time.Hour)) || issued.ExpiresAt.After(time.Now().Add(24*time.Hour+time.Minute)) {
				t.Fatalf("RequestLink = %+v, %v; want the identity's address and a day's link", issued, err)
			}
			res, err := m.LinkByEmail(ctx, issued.token.reveal(), tok)
			if err != nil || res.Linked != n || !res.FirstVerification || res.Recipient != w.email {
				t.Fatalf("LinkByEmail = %+v, %v; want %d linked, the first verification, the identity's address", res, err, n)
			}
			for i, row := range rows {
				if id, via := w.link(row); id != w.ident || via != "email" {
					t.Errorf("business %d: linked to %s via %q, want the identity via email", i, id, via)
				}
				if c := w.audit(tenants[i], "admin.membership.linked"); c != 1 {
					t.Errorf("business %d: %d admin.membership.linked rows, want 1", i, c)
				}
				var detail string
				if err := owner.QueryRow(ctx, `SELECT detail::text FROM audit_log WHERE tenant_id = $1 AND action = 'admin.membership.linked'`, tenants[i]).Scan(&detail); err != nil {
					t.Fatal(err)
				}
				if detail != `{"outcome": "ok"}` {
					t.Errorf("business %d: the audit detail is %s, want {\"outcome\": \"ok\"} alone", i, detail)
				}
				for _, other := range append([]uuid.UUID{ta}, tenants...) {
					if other != tenants[i] && strings.Contains(detail, other.String()) {
						t.Errorf("business %d's audit row names another business", i)
					}
				}
			}
			w.digestInvariant()
			if c := w.sessions(tenants[0], true); c != 0 {
				t.Errorf("%d live sessions left on a linked business (its old-password session must be revoked)", c)
			}
			if _, err := m.Verify(ctx, legacyTok); !errors.Is(err, ErrNoSession) {
				t.Errorf("the old-password session after the link: %v, want ErrNoSession", err)
			}
			for _, row := range []uuid.UUID{refused, disabled, glyph} {
				if id, _ := w.link(row); id != uuid.Nil {
					t.Errorf("row %s was linked; a refused, disabled or homoglyph row never is", row)
				}
			}
			// The old passwords no longer sign in; the identity's does, and reaches every business.
			// (The homoglyph row is still its own sign-in under the old password -- it is no business of
			// this identity; none of the linked ones may verify.)
			in, err := m.AuthenticatePerson(ctx, w.email, oldPW, uuid.Nil, generousBudget())
			if in.Session != nil {
				t.Errorf("the old password opened the identity's session: %v", err)
			}
			for _, v := range in.Legacy.Verified() {
				for _, tenant := range tenants {
					if v.TenantID == tenant {
						t.Errorf("the old password still signs in to linked business %s", tenant)
					}
				}
			}
			ms, err := m.Memberships(ctx, tok)
			if err != nil || len(ms) != n+1 {
				t.Fatalf("Memberships after the link = %d, %v; want %d", len(ms), err, n+1)
			}
			// THE NAMED RESIDUE (HasBusinessToAdd's comment): the homoglyph row still counts for the
			// banner -- the resolver cannot tell it apart -- and is never linked. Without it: no banner.
			if add, err := m.HasBusinessToAdd(ctx, ra); err != nil || !add {
				t.Errorf("the banner after the link with the homoglyph row left: %v, %v; want the named residue (yes)", add, err)
			}
			w.exec(`UPDATE admin_users SET status = 'disabled' WHERE id = $1`, glyph)
			if add, err := m.HasBusinessToAdd(ctx, ra); err != nil || add {
				t.Errorf("the banner after the link: %v, %v; want no", add, err)
			}
			if _, err := m.LinkByEmail(ctx, issued.token.reveal(), tok); !errors.Is(err, ErrLinkRefused) {
				t.Errorf("the link pressed again: %v, want ErrLinkRefused (spent)", err)
			}
			if c := w.int(`SELECT count(*) FROM admin_identities WHERE id = $1 AND email_verified_at IS NOT NULL AND confirm_scope = 'first_verify'`, w.ident); c != 1 {
				t.Error("the first mailbox proof did not verify the address and open the first_verify window")
			}
			moved, err := m.Switch(ctx, tok, tenants[n-1])
			if err != nil || moved.Session.TenantID != tenants[n-1] {
				t.Fatalf("a switch to the last added business: %+v, %v", moved.Session, err)
			}
			if in, err := w.signInOn(m, w.pw, tenants[0]); err != nil || in.Session.TenantID != tenants[0] {
				t.Errorf("the identity's password into an added business: %+v, %v", in.Session, err)
			}
		})
	}
}

// TestLinkByEmail_EveryRefusalLinksNothing (criteria 2, 3, 9): the link spends nothing and links nothing
// when it is pressed from another identity's browser, from an unlinked row's (the business that would
// be linked -- the attacker-held row of ADR 0031 §5.8 is one), from a revoked session, with a malformed
// value, after it expired, or when it is a VERIFICATION link (the purpose column; also the hash key). A
// person with nothing to add gets no link at all (ErrNothingToAdd, no row written), an unlinked session
// none either (ErrNotLinked), and the banner says no to both.
func TestLinkByEmail_EveryRefusalLinksNothing(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	owner := ownerConn(t)
	w := newIDWorld(t, owner, true)
	forgetLinks(t, w)
	ta, tz := w.tenant("M14-22 Refuse A"), w.tenant("M14-22 Refuse Z")
	w.linked(ta, "active", "backfill", 72*time.Hour)
	zPW := "z-" + uuid.NewString()
	z := w.row(tz, fixtureDigest(t, zPW), "active", time.Hour)
	ctx := context.Background()
	ra, tok := w.verifiedOn(m, ta)
	issued, err := m.RequestLink(ctx, ra, tok)
	if err != nil {
		t.Fatalf("RequestLink: %v", err)
	}
	raw := issued.token.reveal()

	other := newIDWorld(t, owner, true)
	to := other.tenant("M14-22 Refuse Other")
	other.linked(to, "active", "backfill", 48*time.Hour)
	_, tokOther := other.verifiedOn(m, to)
	rz, tokZ := w.legacyOn(m, zPW, tz)
	_, tokRevoked := w.verifiedOn(m, ta)
	revoked, _ := m.Verify(ctx, tokRevoked)
	if err := m.Revoke(ctx, revoked.TenantID, revoked.SessionID); err != nil {
		t.Fatal(err)
	}
	// A verification link of the same identity (the identity is unverified, so one is issued).
	ver, err := m.RequestVerification(ctx, ra, tok)
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}
	verHash, err := ver.token.hash(m.linkKey)
	if err != nil {
		t.Fatal(err)
	}
	verRow := w.int(`SELECT count(*) FROM admin_identity_verifications WHERE id = $1 AND purpose = 'verify'`, ver.ID)
	if verRow != 1 {
		t.Fatal("PREMISE: the verification link is not a 'verify' row")
	}
	for _, c := range []struct {
		name  string
		raw   string
		token Token
	}{
		{"another identity's browser", raw, tokOther},
		{"the unlinked row's own browser", raw, tokZ},
		{"a revoked session", raw, tokRevoked},
		{"a malformed link", "not-a-token", tok},
		{"a verification link", ver.token.reveal(), tok},
	} {
		if _, err := m.LinkByEmail(ctx, c.raw, c.token); !errors.Is(err, ErrLinkRefused) {
			t.Errorf("%s: %v, want ErrLinkRefused", c.name, err)
		}
	}
	// The verification link, hashed as a link would be AND as itself, straight at the database: the
	// purpose column refuses it (the key separation is the first wall, this the second).
	sh, _ := tok.hash(m.hmacKey)
	verOwn, _ := ver.token.hash(m.verifyKey)
	for _, h := range []string{verHash, verOwn} {
		if _, err := d.LinkByEmail(ctx, h, sh); err == nil {
			t.Error("identity_link_by_email accepted a verification link's hash")
		}
	}
	if id, _ := w.link(z); id != uuid.Nil {
		t.Fatal("a refused press linked the row")
	}
	if c := w.int(`SELECT count(*) FROM admin_identity_verifications WHERE id = $1 AND used_at IS NULL`, issued.ID); c != 1 {
		t.Fatal("a refused press spent the link")
	}
	// Expired: the link's end in the past.
	w.exec(`UPDATE admin_identity_verifications SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day' WHERE id = $1`, issued.ID)
	if _, err := m.LinkByEmail(ctx, raw, tok); !errors.Is(err, ErrLinkRefused) {
		t.Errorf("an expired link: %v, want ErrLinkRefused", err)
	}
	if id, _ := w.link(z); id != uuid.Nil {
		t.Fatal("an expired link linked the row")
	}

	// The unlinked row's session: no banner, no link.
	if add, err := m.HasBusinessToAdd(ctx, rz); err != nil || add {
		t.Errorf("the banner on an unlinked session: %v, %v; want no", add, err)
	}
	if _, err := m.RequestLink(ctx, rz, tokZ); !errors.Is(err, ErrNotLinked) {
		t.Errorf("an unlinked session asked for a link: %v, want ErrNotLinked", err)
	}
	// A person with nothing to add: no banner, no link written.
	lone := newIDWorld(t, owner, true)
	forgetLinks(t, lone)
	tl := lone.tenant("M14-22 Lone")
	lone.linked(tl, "active", "backfill", 48*time.Hour)
	rl, tokL := lone.verifiedOn(m, tl)
	if add, err := m.HasBusinessToAdd(ctx, rl); err != nil || add {
		t.Errorf("the banner with nothing to add: %v, %v; want no", add, err)
	}
	if _, err := m.RequestLink(ctx, rl, tokL); !errors.Is(err, ErrNothingToAdd) {
		t.Errorf("RequestLink with nothing to add: %v, want ErrNothingToAdd", err)
	}
	if c := lone.int(`SELECT count(*) FROM admin_identity_verifications WHERE identity_id = $1`, lone.ident); c != 0 {
		t.Errorf("%d links written with nothing to add, want 0", c)
	}
}

// TestLinkByEmail_ConcurrentPressesOfOneLinkLinkOnce (criterion 10, -race): N goroutines press the SAME
// link with the same session at once. Exactly one links; every other answer is the one refusal (no 500,
// no deadlock); the businesses are linked once, each with one audit row.
func TestLinkByEmail_ConcurrentPressesOfOneLinkLinkOnce(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	forgetLinks(t, w)
	ta := w.tenant("M14-22 Race A")
	w.linked(ta, "active", "backfill", 72*time.Hour)
	tb, tc := w.tenant("M14-22 Race B"), w.tenant("M14-22 Race C")
	w.row(tb, fixtureDigest(t, "b-"+uuid.NewString()), "active", time.Hour)
	w.row(tc, fixtureDigest(t, "c-"+uuid.NewString()), "active", time.Hour)
	ctx := context.Background()
	ra, tok := w.verifiedOn(m, ta)
	issued, err := m.RequestLink(ctx, ra, tok)
	if err != nil {
		t.Fatal(err)
	}
	raw := issued.token.reveal()
	const n = 12
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		ok      int
		refused int
		other   []error
	)
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := m.LinkByEmail(ctx, raw, tok)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && res.Linked == 2:
				ok++
			case errors.Is(err, ErrLinkRefused):
				refused++
			default:
				other = append(other, fmt.Errorf("%+v, %w", res, err))
			}
		}()
	}
	close(start)
	wg.Wait()
	if ok != 1 || refused != n-1 || len(other) != 0 {
		t.Fatalf("%d presses at once: %d linked, %d refused, others %v; want exactly one", n, ok, refused, other)
	}
	for _, tenant := range []uuid.UUID{tb, tc} {
		if c := w.audit(tenant, "admin.membership.linked"); c != 1 {
			t.Errorf("%d audit rows in %s, want 1", c, tenant)
		}
	}
	w.digestInvariant()
}

// TestLinkByEmail_TheLockOrderMeetsTheOtherActsWithoutADeadlock (criterion 10): the press takes the
// identity's lock first and then the memberships -- the order of the sign-in's link, the switch, "sign
// out everywhere" and the password change. Rounds of a press racing a switch and a sign-out of every
// OTHER session of the same person: no error but the acts' own refusals (no 40P01).
func TestLinkByEmail_TheLockOrderMeetsTheOtherActsWithoutADeadlock(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	forgetLinks(t, w)
	ta, tb := w.tenant("M14-22 Order A"), w.tenant("M14-22 Order B")
	w.linked(ta, "active", "backfill", 72*time.Hour)
	w.linked(tb, "active", "backfill", 48*time.Hour)
	ctx := context.Background()
	for round := range 6 {
		tx := w.tenant(fmt.Sprintf("M14-22 Order X%d", round))
		w.row(tx, fixtureDigest(t, "x-"+uuid.NewString()), "active", time.Hour)
		ra, tok := w.verifiedOn(m, ta)
		_, tokB := w.verifiedOn(m, tb)
		issued, err := m.RequestLink(ctx, ra, tok)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		errs := make([]error, 3)
		wg.Add(3)
		go func() { defer wg.Done(); _, errs[0] = m.LinkByEmail(ctx, issued.token.reveal(), tok) }()
		go func() { defer wg.Done(); _, errs[1] = m.Switch(ctx, tokB, ta) }()
		go func() {
			defer wg.Done()
			_, errs[2] = m.RevokeAll(ctx, ra.TenantID, ra.AdminUserID, ra.SessionID, true, tok, true)
		}()
		wg.Wait()
		for i, err := range errs {
			if err != nil && !errors.Is(err, ErrLinkRefused) && !errors.Is(err, ErrSwitchRefused) && !errors.Is(err, ErrNoSession) {
				t.Fatalf("round %d, act %d: %v", round, i, err)
			}
		}
	}
	w.digestInvariant()
}
