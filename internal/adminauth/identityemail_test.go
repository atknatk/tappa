package adminauth

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// TestEmailChangeKeys_NoPurposeHashesToAnother (ADR 0044 §5.3, card EC-2 item 10): the change link's and
// the undo link's HMAC keys are derived under labels of their own, so ONE token hashes to five different
// values under the session, verification, link, change and undo keys -- a token of one purpose presented
// as another hashes to a value no row of that purpose holds. And the Manager holds the two new keys as
// the derivations say, never one key twice. MUTATION (measured): undoKeyLabel set to changeKeyLabel ->
// red.
func TestEmailChangeKeys_NoPurposeHashesToAnother(t *testing.T) {
	session := make([]byte, hmacKeyLen)
	for i := range session {
		session[i] = byte(i + 7)
	}
	derivers := map[string]func([]byte) ([]byte, error){
		"session": deriveKey, "verify": deriveVerifyKey, "link": deriveLinkKey, "change": deriveChangeKey, "undo": deriveUndoKey,
	}
	tok, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	hashes := map[string]string{}
	for name, derive := range derivers {
		k, err := derive(session)
		if err != nil {
			t.Fatal(err)
		}
		if len(k) != sha256.Size {
			t.Fatalf("%s key is %d bytes", name, len(k))
		}
		h, err := tok.hash(k)
		if err != nil {
			t.Fatal(err)
		}
		for other, oh := range hashes {
			if oh == h {
				t.Errorf("the %s and %s keys hash one token to the same value", name, other)
			}
		}
		hashes[name] = h
	}
	m, err := New(&db.DB{}, &config.Config{SessionHMACKey: config.NewKey(session)})
	if err != nil {
		t.Fatal(err)
	}
	wantChange, _ := deriveChangeKey(session)
	wantUndo, _ := deriveUndoKey(session)
	if string(m.changeKey.Reveal()) != string(wantChange) || string(m.undoKey.Reveal()) != string(wantUndo) {
		t.Error("the Manager does not hold the change and undo keys the derivations give")
	}
	if _, err := deriveChangeKey(session[:10]); err == nil {
		t.Error("a short session key was accepted for the change key")
	}
}

// TestEmailChangeLinks_AreUnderTheirPaths: the two links are base + path + "?t=" + the token, and a
// refusal, the linkless note and a change that wrote no undo link have no link at all.
func TestEmailChangeLinks_AreUnderTheirPaths(t *testing.T) {
	tok, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	const base = "https://time.example.test"
	if got := (EmailChangeRequested{token: tok}).Link(base); got != base+ChangeEmailPath+"?t="+tok.reveal() {
		t.Errorf("change link %q", got)
	}
	for name, r := range map[string]EmailChangeRequested{
		"refusal": {Refusal: db.EmailChangeStale, token: tok}, "taken": {Taken: true, token: tok}, "no token": {},
	} {
		if r.Link(base) != "" {
			t.Errorf("%s: a link was built", name)
		}
	}
	until := time.Now().Add(7 * 24 * time.Hour)
	if got := (EmailChanged{UndoExpiresAt: &until, undo: tok}).UndoLink(base); got != base+UndoEmailPath+"?t="+tok.reveal() {
		t.Errorf("undo link %q", got)
	}
	if (EmailChanged{undo: tok}).UndoLink(base) != "" {
		t.Error("a change that wrote no undo link built one")
	}
}

// movingResolverDB moves the identity to another address right after the resolver read it -- the S-1
// window: an address change committed between resolve_identity_by_email and identity_open_reset.
type movingResolverDB struct {
	*db.DB
	move func()
}

func (m movingResolverDB) ResolveIdentityByEmail(ctx context.Context, email string) (db.ResolvedIdentity, error) {
	r, err := m.DB.ResolveIdentityByEmail(ctx, email)
	if err == nil && m.move != nil {
		m.move()
	}
	return r, err
}

// TestIssueForPerson_AnAddressChangedAfterTheResolverGetsNoLink (S-1's Go half, card EC-2 item 9b; ADR
// 0044 §5.3): the resolver returns the identity's address, then an address change commits, then
// identity_open_reset runs with the recipient openForIdentity passes -- the identity no longer holds it,
// so NO link is minted for the identity and none is handed back to mail to the address it left (the
// "no identity" answer). CONTROL: without the move the same request mints the identity's link.
// MUTATION (measured): openIdentityResetSQL's fourth argument replaced by NULL (the old three-argument
// call, no recipient check) -> red.
func TestIssueForPerson_AnAddressChangedAfterTheResolverGetsNoLink(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	w := newPersonResetWorld(t)

	control := cheapResets(t, d)
	p, err := control.IssueForPerson(ctx, w.registered)
	if err != nil {
		t.Fatal(err)
	}
	if !hasIdentityGrant(p, w.ident) {
		t.Fatal("CONTROL FAILED: without a change the identity gets no link")
	}
	w.exec(`UPDATE admin_identity_resets SET cancelled_at = now() WHERE identity_id = $1 AND used_at IS NULL AND cancelled_at IS NULL`, w.ident)

	moved := "moved-" + uuid.NewString() + "@identity.example"
	r := cheapResets(t, movingResolverDB{DB: d, move: func() {
		w.exec(`UPDATE admin_identities SET email = $2 WHERE id = $1`, w.ident, moved)
	}})
	before := w.int(`SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1`, w.ident)
	p, err = r.IssueForPerson(ctx, w.registered)
	if err != nil {
		t.Fatal(err)
	}
	if hasIdentityGrant(p, w.ident) || p.IdentityKept != nil {
		t.Fatal("a link was minted (or a kept one reported) for an identity that had left the address")
	}
	for _, g := range p.Grants {
		if g.Recipient == w.registered {
			t.Errorf("a grant is addressed to the address the identity left")
		}
	}
	if after := w.int(`SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1`, w.ident); after != before {
		t.Errorf("identity links %d -> %d: identity_open_reset wrote for an address the identity left", before, after)
	}
}

func hasIdentityGrant(p PersonResets, ident uuid.UUID) bool {
	for _, g := range p.Grants {
		if g.IdentityID == ident {
			return true
		}
	}
	return false
}
