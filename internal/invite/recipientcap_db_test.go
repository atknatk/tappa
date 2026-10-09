package invite

// recipientcap_db_test.go — M10 EM-7C's per-mailbox cap as the invitation route asks it,
// against REAL Postgres (email_db_test.go's fixtures): the question inside the minting
// transaction rolls a full mailbox's press back, and a cap reached between the question
// and the send leaves the minted row and an undelivered row with the cap's class. The
// sink is a fake that answers the question as told; the cap's own windows are
// internal/mail's, and the real cap through real HTTP is internal/handler's
// TestInviteEmailDB_ACappedMailboxIsRefusedBeforeAnythingIsMinted.
//
// WHAT ONE RUN LEAVES BEHIND (nothing here can be deleted by tappa_app), counted from the
// code: tenants ("EM7C Test …") 2, each with one venue and one administrator; employees
// 2; employee_invites rows 3 (the capped test's seeded link and its control press, the
// raced test's minted press — the capped press itself rolled back); audit_log rows 3 —
// 1 invite.email_refused, 1 invite.code_emailed (the control), 1 invite.undelivered (the
// test fake channel that seeds the earlier link writes none). Every address is under
// example.test.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/mail"
	"github.com/google/uuid"
)

// TestEmailRouteDB_ACappedMailboxMintsNothing is K7C-5: the sink is asked about the
// address ON THE ROW, for the PRESSING business (EM-7C round 3: the cap is counted per
// business and mailbox), after the four address refusals, inside the minting transaction;
// a full mailbox answers ErrRecipientCapped and the press keeps nothing — no new row,
// the person's earlier link still spendable, the sink never sent — and leaves ONE
// invite.email_refused row with reason recipient_limit and no address. CONTROL: the
// same person, the sink answering "not full", mints and sends once.
func TestEmailRouteDB_ACappedMailboxMintsNothing(t *testing.T) {
	m, d := testManager(t)
	f := newMailFixture(t, d, "EM7C Test capped", ptrBool(true))
	a := addr("Capped")
	emp := f.person(t, "Capped Borg", a)
	if _, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: emp}, &captureChannel{}); err != nil {
		t.Fatalf("seeding an earlier link: %v", err)
	}

	sink := &fakeMailSink{capped: true}
	_, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: emp}, mailRoute(t, d, sink))
	if !errors.Is(err, ErrRecipientCapped) {
		t.Fatalf("err = %v, want ErrRecipientCapped", err)
	}
	if reason, ok := RefusalReason(err); !ok || reason != "recipient_limit" {
		t.Errorf("RefusalReason = %q, %v; want recipient_limit", reason, ok)
	}
	if len(sink.asked) != 1 || sink.asked[0] != *a {
		t.Errorf("the sink was asked about %q, want exactly the address on the row", sink.asked)
	}
	if len(sink.scopes) != 1 || sink.scopes[0] != f.tenantID {
		t.Errorf("the sink was asked for business %v, want the pressing business %v only -- another "+
			"business's count must never decide this press", sink.scopes, f.tenantID)
	}
	if n := len(sink.calls()); n != 0 {
		t.Errorf("the sink sent %d time(s) for a full mailbox", n)
	}
	if n := f.invitesFor(t, emp); n != 1 {
		t.Errorf("%d invitation row(s), want the earlier one only", n)
	}
	if n := f.spendable(t, emp); n != 1 {
		t.Errorf("%d spendable link(s), want the earlier one still alive", n)
	}
	rows := f.trail(t, ActionEmailRefused, emp)
	if len(rows) != 1 || !strings.Contains(rows[0], `"reason": "recipient_limit"`) || strings.Contains(rows[0], "@") {
		t.Errorf("invite.email_refused rows %v, want one with reason recipient_limit and no address", rows)
	}

	control := &fakeMailSink{}
	if _, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: emp}, mailRoute(t, d, control)); err != nil {
		t.Fatalf("CONTROL: the same press with the mailbox not full: %v", err)
	}
	if len(control.calls()) != 1 || f.invitesFor(t, emp) != 2 {
		t.Error("CONTROL: the press with the mailbox not full did not mint and send once")
	}
}

// TestEmailRouteDB_ACapReachedAfterTheMintIsUndelivered is K7C-5's race, counted: the
// question is not a reservation, so a send to the same mailbox between it and this
// press's own send can take the last slot. Then the press is minted (its row stands,
// B12) and the cap's *mail.SendError comes back like any failed send — ONE
// invite.undelivered row with class recipient_cap and reply code 0, no address.
func TestEmailRouteDB_ACapReachedAfterTheMintIsUndelivered(t *testing.T) {
	m, d := testManager(t)
	f := newMailFixture(t, d, "EM7C Test raced", nil)
	emp := f.person(t, "Raced Borg", addr("Raced"))
	sink := &fakeMailSink{err: &mail.SendError{Class: mail.ClassRecipientCap}}
	inv, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: emp}, mailRoute(t, d, sink))
	var se *mail.SendError
	if !errors.As(err, &se) || se.Class != mail.ClassRecipientCap || inv.ID == uuid.Nil {
		t.Fatalf("err %v, invite %s: want the cap's SendError after a mint", err, inv.ID)
	}
	if _, refused := RefusalReason(err); refused {
		t.Error("a cap met at the SEND is reported as a refusal: the row was minted")
	}
	if n := f.invitesFor(t, emp); n != 1 {
		t.Errorf("%d invitation row(s), want the minted one standing", n)
	}
	rows := f.trail(t, ActionUndelivered, emp)
	if len(rows) != 1 || !strings.Contains(rows[0], `"class": "recipient_cap"`) || !strings.Contains(rows[0], `"smtp_code": 0`) ||
		strings.Contains(rows[0], "@") {
		t.Errorf("invite.undelivered rows %v, want one with class recipient_cap and reply code 0", rows)
	}
}
