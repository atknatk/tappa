package handler

// identityreset_test.go -- M14-14 at the handler (fakes; the database half is
// internal/adminauth/identityreset_db_test.go and identityreset_db_test.go here): the per-identity
// request is the flag's (ADR 0031 §8's row "Kimlik başına sıfırlama talebi"), its grants are delivered
// as "every business" e-mails and leave no audit row (the link belongs to no business), an undelivered
// one is withdrawn by its own token, a spent one sends the person ONE notice, and a refused one answers
// the screen every unusable link gets.

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/db"
)

// newResetRouterFlag is newResetRouter with TAPPA_ADMIN_IDENTITY set, and the log kept.
func newResetRouterFlag(t *testing.T, resets panelResets, mail ResetChannel, trail *fakeTrail, on bool, logged *bytes.Buffer) (http.Handler, *AdminReset) {
	t.Helper()
	cfg := adminTestConfig()
	cfg.AdminIdentity = on
	log := slog.New(slog.DiscardHandler)
	if logged != nil {
		log = slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	h, err := NewAdminReset(resets, mail, trail, cfg, log)
	if err != nil {
		t.Fatalf("NewAdminReset: %v", err)
	}
	stopWorkerAtCleanup(t, h)
	r := chi.NewRouter()
	h.Mount(r)
	return r, h
}

// randomLinkValue is a link value of the shipped shape (43 characters of base64url), drawn per call so
// no credential-shaped literal sits in the source.
func randomLinkValue() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// errSendFailed is a relay's failure, as the fake channel returns it.
var errSendFailed = errors.New("relay refused")

// identityGrantFor is a per-identity grant as adminauth.IssueForPerson builds it: no business, no
// administrator, the identity's id and registered address.
func identityGrantFor(recipient string) adminauth.ResetGrant {
	return adminauth.ResetGrant{
		Recipient:  recipient,
		IdentityID: uuid.New(),
		Issued: adminauth.IssuedReset{
			Reset: adminauth.Reset{ID: uuid.New(), ExpiresAt: time.Now().Add(adminauth.ResetTTL)},
			Token: adminauth.ParseResetToken(randomLinkValue()),
		},
	}
}

// TestAdminReset_TheFlagChoosesTheRequest is ADR 0031 §8's row: with TAPPA_ADMIN_IDENTITY off the
// request is today's per-row one (IssueForEmail, never IssueForPerson) and with it on the per-identity
// one (IssueForPerson, never IssueForEmail). MUTATION: the gate test inverted or removed -> red.
func TestAdminReset_TheFlagChoosesTheRequest(t *testing.T) {
	t.Parallel()
	for _, on := range []bool{false, true} {
		resets := &fakeResets{}
		router, h := newResetRouterFlag(t, resets, &recordingChannel{}, &fakeTrail{}, on, nil)
		requestReset(t, newBrowser(t, router), "someone@example.test")
		drain(t, h)
		issue, _ := resets.calls()
		resets.mu.Lock()
		person := resets.personCalls
		resets.mu.Unlock()
		if on && (person != 1 || issue != 0) || !on && (person != 0 || issue != 1) {
			t.Errorf("flag %v: IssueForPerson %d, IssueForEmail %d", on, person, issue)
		}
	}
}

// TestAdminReset_AnIdentityLinkIsDeliveredForEveryBusinessAndWritesNoRow: the per-identity grant goes
// to the address the grant names with EveryBusiness set, and leaves no audit row (no business owns it)
// but one log line with ids only; a per-row grant of the same request is today's (EveryBusiness off,
// its requested row). The body is the page every address gets.
func TestAdminReset_AnIdentityLinkIsDeliveredForEveryBusinessAndWritesNoRow(t *testing.T) {
	t.Parallel()
	known := "person@example.test"
	ig, rg := identityGrantFor("Person@Example.test"), grantFor(known)
	resets := &fakeResets{personFor: map[string]adminauth.PersonResets{known: {Grants: []adminauth.ResetGrant{ig, rg}}}}
	mail, trail, logged := &recordingChannel{}, &fakeTrail{}, &bytes.Buffer{}
	router, h := newResetRouterFlag(t, resets, mail, trail, true, logged)
	body := requestReset(t, newBrowser(t, router), known)
	unknown := requestReset(t, newBrowser(t, router), "nobody@example.test")
	drain(t, h)
	if body != unknown {
		t.Error("the per-identity request's answer differs from an unknown address's")
	}
	got := mail.all()
	if len(got) != 2 {
		t.Fatalf("%d deliveries, want 2", len(got))
	}
	for _, d := range got {
		switch d.ResetID {
		case ig.Issued.Reset.ID:
			if !d.EveryBusiness || d.Recipient != ig.Recipient {
				t.Errorf("the identity's link: every business %v, to %q; want true, %q", d.EveryBusiness, d.Recipient, ig.Recipient)
			}
		case rg.Issued.Reset.ID:
			if d.EveryBusiness || d.Recipient != known {
				t.Errorf("the row's link: every business %v, to %q", d.EveryBusiness, d.Recipient)
			}
		}
	}
	if n := trail.count(ActionAdminResetRequested); n != 1 {
		t.Errorf("%d requested rows, want the per-row grant's 1 (the identity's link writes none)", n)
	}
	for _, e := range trail.eventsSnapshot() {
		if e.TenantID == uuid.Nil || strings.Contains(e.Target, ig.IdentityID.String()) {
			t.Errorf("an audit row for the identity's link: %+v", e)
		}
	}
	log := logged.String()
	if !strings.Contains(log, "identity_id="+ig.IdentityID.String()) || !strings.Contains(log, "outcome=ok") {
		t.Errorf("no log line for the identity's link:\n%s", log)
	}
	if strings.Contains(strings.ToLower(log), "person@example.test") {
		t.Error("the log carries the address")
	}
}

// TestAdminReset_AnUndeliveredIdentityLinkIsWithdrawnByItsToken: a failed send of a per-identity link
// withdraws it (WithdrawIdentity), never through the per-row Withdraw, and writes no audit row.
// MUTATION: recordOutcome's identity branch removed -> the per-row path runs (Withdraw + a row) -> red.
func TestAdminReset_AnUndeliveredIdentityLinkIsWithdrawnByItsToken(t *testing.T) {
	t.Parallel()
	known := "person@example.test"
	ig := identityGrantFor(known)
	resets := &fakeResets{personFor: map[string]adminauth.PersonResets{known: {Grants: []adminauth.ResetGrant{ig}}}}
	trail := &fakeTrail{}
	router, h := newResetRouterFlag(t, resets, &recordingChannel{err: errSendFailed}, trail, true, nil)
	requestReset(t, newBrowser(t, router), known)
	drain(t, h)
	resets.mu.Lock()
	withdrawn := resets.identityWithdrawn
	resets.mu.Unlock()
	if withdrawn != 1 || len(resets.withdrawals()) != 0 {
		t.Errorf("WithdrawIdentity %d, Withdraw %d; want 1, 0", withdrawn, len(resets.withdrawals()))
	}
	if n := len(trail.eventsSnapshot()); n != 0 {
		t.Errorf("%d audit rows for an undelivered identity link, want 0", n)
	}
}

// TestAdminReset_ASpentIdentityLinkSendsThePersonOneNotice: Submit with a per-identity consumption
// writes no audit row (the definer wrote each business's), sends ONE notice to the identity's
// registered address with EveryBusiness (and reads no row's address), and ends on the sign-in form.
// MUTATION: Submit treating it as a per-row consumption (the identity branch removed) -> red.
func TestAdminReset_ASpentIdentityLinkSendsThePersonOneNotice(t *testing.T) {
	t.Parallel()
	ident := uuid.New()
	resets := &fakeResets{consumed: adminauth.ConsumedReset{IdentityID: ident, NoticeTo: "Person@Example.test", RevokedSessions: 3, Recorded: true}}
	mail, trail := &recordingChannel{}, &fakeTrail{}
	router, _ := newResetRouterFlag(t, resets, mail, trail, false, nil) // the consumption is not the flag's
	rec := submitLink(t, newBrowser(t, router), randomLinkValue(), "a-new-password-1")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), adminLoginPath) {
		t.Fatalf("the spend answered %d %q, want 303 to sign-in", rec.Code, rec.Header().Get("Location"))
	}
	notices := mail.allNotices()
	if len(notices) != 1 || notices[0].Recipient != "Person@Example.test" || !notices[0].EveryBusiness || notices[0].IdentityID != ident || notices[0].TenantID != uuid.Nil {
		t.Fatalf("notices = %+v; want one, to the identity's address, every business", notices)
	}
	resets.mu.Lock()
	read := resets.recipientCalls
	resets.mu.Unlock()
	if read != 0 {
		t.Errorf("the notice read %d row addresses, want 0", read)
	}
	if n := len(trail.eventsSnapshot()); n != 0 {
		t.Errorf("%d audit rows from the handler, want 0 (the definer wrote each business's)", n)
	}
}

// TestAdminReset_ARefusedIdentityLinkIsTheSameScreen: a per-identity link that resolved and could not
// be spent answers byte for byte what an unknown link answers, writes no audit row, and logs one line
// with its facts and ids only.
func TestAdminReset_ARefusedIdentityLinkIsTheSameScreen(t *testing.T) {
	t.Parallel()
	used := time.Now()
	ir := db.ResolvedIdentityReset{ID: uuid.New(), IdentityID: uuid.New(), ExpiresAt: time.Now().Add(time.Hour), UsedAt: &used}
	refusedIdentity := &fakeResets{consumeErr: adminauth.ErrResetUnusable, consumed: adminauth.ConsumedReset{IdentityRefusal: &ir}}
	unknown := &fakeResets{consumeErr: adminauth.ErrResetUnusable}
	trail, logged := &fakeTrail{}, &bytes.Buffer{}
	r1, _ := newResetRouterFlag(t, refusedIdentity, &recordingChannel{}, trail, true, logged)
	r2, _ := newResetRouterFlag(t, unknown, &recordingChannel{}, &fakeTrail{}, true, nil)
	a := submitLink(t, newBrowser(t, r1), randomLinkValue(), "a-new-password-1")
	b := submitLink(t, newBrowser(t, r2), randomLinkValue(), "a-new-password-1")
	if a.Code != b.Code || a.Body.String() != b.Body.String() {
		t.Errorf("a refused identity link answers %d, an unknown one %d, bodies equal %v", a.Code, b.Code, a.Body.String() == b.Body.String())
	}
	if n := len(trail.eventsSnapshot()); n != 0 {
		t.Errorf("%d audit rows for a refused identity link, want 0", n)
	}
	if log := logged.String(); !strings.Contains(log, "reset_id="+ir.ID.String()) || !strings.Contains(log, "already_used=true") {
		t.Errorf("the refusal's line is missing:\n%s", log)
	}
}

// TestAdminReset_AKeptIdentityLinkSendsNothingAndAnswersTheSame: the identity already holds a live link
// (EM-7C) -- nothing is delivered, no row is written, one log line says kept, and the page is the one
// every address gets.
func TestAdminReset_AKeptIdentityLinkSendsNothingAndAnswersTheSame(t *testing.T) {
	t.Parallel()
	known := "person@example.test"
	k := &adminauth.IdentityResetKept{IdentityID: uuid.New(), ResetID: uuid.New(), ExpiresAt: time.Now().Add(time.Hour)}
	resets := &fakeResets{personFor: map[string]adminauth.PersonResets{known: {IdentityKept: k}}}
	mail, trail, logged := &recordingChannel{}, &fakeTrail{}, &bytes.Buffer{}
	router, h := newResetRouterFlag(t, resets, mail, trail, true, logged)
	body := requestReset(t, newBrowser(t, router), known)
	other := requestReset(t, newBrowser(t, router), "nobody@example.test")
	drain(t, h)
	if body != other {
		t.Error("a kept identity link answers differently from an unknown address")
	}
	if len(mail.all()) != 0 || len(trail.eventsSnapshot()) != 0 {
		t.Errorf("a kept link sent %d and wrote %d rows, want 0 and 0", len(mail.all()), len(trail.eventsSnapshot()))
	}
	if !strings.Contains(logged.String(), "outcome=kept") || !strings.Contains(logged.String(), "reset_id="+k.ResetID.String()) {
		t.Errorf("no kept line:\n%s", logged.String())
	}
	if strings.Count(logged.String(), "no active admin for that address") != 1 {
		t.Error("the unknown address's line should be the only 'no active admin' line (the kept one resolved)")
	}
}
