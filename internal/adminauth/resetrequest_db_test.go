package adminauth

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The tests for "who gets a recovery link" (M7-04 phase B), against REAL Postgres.
//
// THEY CANNOT BE FAKED, and that is not a preference: the question this file answers
// is decided by a SECURITY DEFINER function's ORDER BY (00017), by RLS on
// admin_users, and by citext equality. A fake would answer whatever the fake's author
// believed those three do.

// TestIssueForEmail_AnswersTheSameShapeWhateverItFinds is the fifth acceptance
// criterion at the layer that decides it: there is ONE return shape, and an unknown
// address is an empty slice rather than an error a caller could branch on.
func TestIssueForEmail_AnswersTheSameShapeWhateverItFinds(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	r := cheapResets(t, d)
	tenantID := newTenantRow(t, d, "Recovery Shape Ltd")
	known := randEmail(t)
	newAdminRow(t, d, tenantID, known, "p", "active", "owner", "Known")

	cases := []struct {
		name  string
		email string
		want  int
	}{
		{"a registered address", known, 1},
		{"an unregistered address", randEmail(t), 0},
		{"an empty address", "", 0},
		{"an address carrying a NUL byte", "a\x00b@example.test", 0},
		{"an address that is not valid UTF-8", "a\xffb@example.test", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.IssueForEmail(ctx, tc.email)
			if err != nil {
				t.Fatalf("IssueForEmail: %v -- every arm has to answer the same way, and an "+
					"error is a difference a caller can branch on", err)
			}
			if len(got) != tc.want {
				t.Fatalf("got %d link(s), want %d", len(got), tc.want)
			}
		})
	}
}

// TestIssueForEmail_DeliversToTheAddressOnTheRow is acceptance criterion 3, and the
// SPELLING is the whole measurement.
//
// 🔴 THE CONTROL INPUT IS NOT DERIVED FROM THE MECHANISM. The address is written into
// admin_users in one spelling and looked up in another, and what proves the test is
// looking at the right thing is that the LOOKUP SUCCEEDS AT ALL: citext equality is
// what makes the two one identity, and if it did not the arm would find zero rows and
// the assertion below would never run. So the same fact that creates the hazard also
// arms the test.
func TestIssueForEmail_DeliversToTheAddressOnTheRow(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	r := cheapResets(t, d)
	tenantID := newTenantRow(t, d, "Recovery Spelling Ltd")

	stored := randEmail(t) // lower case, by construction
	typed := strings.ToUpper(stored)
	if typed == stored {
		t.Fatal("the two spellings are identical, so this test would pass without measuring anything")
	}
	newAdminRow(t, d, tenantID, stored, "p", "active", "owner", "Spelling")

	grants, err := r.IssueForEmail(ctx, typed)
	if err != nil {
		t.Fatalf("IssueForEmail: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("got %d link(s) for a differently-cased spelling of a real address, want 1 -- "+
			"if this is 0, citext equality is not doing what the rest of the test assumes", len(grants))
	}
	if grants[0].Recipient != stored {
		t.Errorf("recipient = %q, want the address ON THE ROW (%q). The caller typed %q, and "+
			"citext equality is case-insensitive while an SMTP local-part is not -- so "+
			"delivering the typed spelling can be delivering to a different mailbox.",
			grants[0].Recipient, stored, typed)
	}
}

// TestIssueForEmail_MintsForTheSignInWindowAndNoWider is ResetWindow's argument,
// executed.
//
// ⚠️ THE ROWS ARE PLANTED IN SEPARATE TENANTS, which is the only shape the schema
// allows: admin_users.email is UNIQUE per tenant, so an address that resolves to N
// identities is N businesses. That is exactly the shape the sign-in picker exists for
// and the shape M7-02 made an attacker able to create.
func TestIssueForEmail_MintsForTheSignInWindowAndNoWider(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	r := cheapResets(t, d)
	shared := randEmail(t)

	const planted = MaxCandidates + 3
	var tenants []uuid.UUID
	for i := 0; i < planted; i++ {
		tid := newTenantRow(t, d, "Recovery Window Ltd")
		newAdminRow(t, d, tid, shared, "p", "active", "owner", "Crowded")
		tenants = append(tenants, tid)
	}

	grants, err := r.IssueForEmail(ctx, shared)
	if err != nil {
		t.Fatalf("IssueForEmail: %v", err)
	}
	if len(grants) != ResetWindow {
		t.Fatalf("got %d link(s) for an address resolving to %d identities, want exactly "+
			"ResetWindow (%d).\nFewer would refuse recovery to somebody who CAN sign in; "+
			"more would mint a credential that cannot restore a sign-in, because "+
			"Authenticate compares only the first %d.",
			len(grants), planted, ResetWindow, MaxCandidates)
	}
	// AND THEY ARE THE FIRST ONES, which is what makes the window the SAME window
	// sign-in uses. The resolver orders by created_at, so the first ResetWindow
	// tenants planted are the ones that must appear.
	inWindow := map[uuid.UUID]bool{}
	for _, g := range grants {
		inWindow[g.Issued.Reset.TenantID] = true
	}
	for i, tid := range tenants[:ResetWindow] {
		if !inWindow[tid] {
			t.Errorf("the identity planted at position %d is inside the sign-in window and got "+
				"no link", i)
		}
	}
	for i, tid := range tenants[ResetWindow:] {
		if inWindow[tid] {
			t.Errorf("the identity planted at position %d is OUTSIDE the sign-in window and got "+
				"a link; resetting a password there cannot restore a sign-in",
				i+ResetWindow)
		}
	}
}

// TestIssueForEmail_SkipsDisabledIdentitiesWithoutFreeingTheirSlot holds both halves
// of a decision that is easy to get half-right.
func TestIssueForEmail_SkipsDisabledIdentitiesWithoutFreeingTheirSlot(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	r := cheapResets(t, d)
	shared := randEmail(t)

	// One disabled identity first (oldest, so it takes slot 0), then MaxCandidates
	// active ones. The disabled row occupies a slot: if it did NOT, the last active
	// identity would slide into the window and get a link.
	disabledTenant := newTenantRow(t, d, "Recovery Disabled Ltd")
	newAdminRow(t, d, disabledTenant, shared, "p", "disabled", "owner", "Switched Off")
	var active []uuid.UUID
	for i := 0; i < MaxCandidates; i++ {
		tid := newTenantRow(t, d, "Recovery Active Ltd")
		newAdminRow(t, d, tid, shared, "p", "active", "owner", "Still Here")
		active = append(active, tid)
	}

	grants, err := r.IssueForEmail(ctx, shared)
	if err != nil {
		t.Fatalf("IssueForEmail: %v", err)
	}
	if len(grants) != MaxCandidates-1 {
		t.Fatalf("got %d link(s), want %d: the disabled identity gets none AND keeps its slot, "+
			"so the last active one stays outside the window. If this is %d, dropping "+
			"disabled rows BEFORE the cap has made the window's contents depend on how "+
			"many disabled rows an address has -- which is a fact about the database that "+
			"the number of arriving emails would then answer.",
			len(grants), MaxCandidates-1, MaxCandidates)
	}
	for _, g := range grants {
		if g.Issued.Reset.TenantID == disabledTenant {
			t.Error("a disabled administrator was handed a recovery link; that is a write to an " +
				"account somebody deliberately switched off")
		}
		if g.Issued.Reset.TenantID == active[len(active)-1] {
			t.Error("the identity outside the window got a link, so the disabled row did not " +
				"keep its slot")
		}
	}
}

// TestIssueForEmail_RetiresTheEarlierLinkAndSaysHowMany is ADR 0015's accepted harm
// (a), made observable — which is the only defence the trail can offer against it.
func TestIssueForEmail_RetiresTheEarlierLinkAndSaysHowMany(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	r := cheapResets(t, d)
	tenantID := newTenantRow(t, d, "Recovery Retirement Ltd")
	email := randEmail(t)
	newAdminRow(t, d, tenantID, email, "p", "active", "owner", "Victim")

	first, err := r.IssueForEmail(ctx, email)
	if err != nil || len(first) != 1 {
		t.Fatalf("first IssueForEmail: %v, %d link(s)", err, len(first))
	}
	if got := first[0].Issued.Reset.RetiredCount; got != 0 {
		t.Errorf("the first request retired %d link(s), want 0", got)
	}

	second, err := r.IssueForEmail(ctx, email)
	if err != nil || len(second) != 1 {
		t.Fatalf("second IssueForEmail: %v, %d link(s)", err, len(second))
	}
	if got := second[0].Issued.Reset.RetiredCount; got != 1 {
		t.Errorf("the second request retired %d link(s), want 1. That number is the only place "+
			"ADR 0015's harm (a) becomes visible: an investigation reading 'requested, "+
			"retired 1' a minute after 'requested, retired 0' is reading the attack.", got)
	}
	// AND THE FIRST LINK IS REALLY DEAD, which is the harm rather than the counter.
	if _, _, err := r.Consume(ctx, first[0].Issued.Token, "a-brand-new-password"); err == nil {
		t.Error("the superseded link still worked; the counter above would then be describing " +
			"something that did not happen")
	}
}
