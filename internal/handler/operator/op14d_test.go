package operator_test

// op14d_test.go -- M10 OP-14 D (migration 00033) on the audit screen, with the fake store
// (rig_test.go): the platform owner's opadmin rows are named as what they are -- their kind's
// word, the account, and the owner as the one who acted -- and are a filter like any kind.

import (
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
)

// TestAuditScreen_TheOwnersRowsNameTheOwner (M10 OP-14 D):
//
// PART I -- a log of six rows below the view's own: the three owner kinds, each naming an
// account by id and name with no session and no actor (the shape 00033's actor_shape allows
// them), a 'password_ok' row (a pre-session row: the CONTROL that "Before sign-in" stays
// theirs) and two sessionless rows of kinds this build does not name -- one of them
// 'operator_enabled', which LOOKS like the owner's (2nd round of the OP-14 D review: an owner
// label read off the "operator_" prefix stayed green) -- fail-closed: neither is the owner's.
// Each owner row opens with its time and its kind's word and carries "By the platform owner,
// with opadmin" and the account's name, and neither "Before sign-in" nor a session; the
// 'password_ok' row carries "Before sign-in" and not the owner; each unknown kind carries its
// raw text and the unrecognised chip, and not the owner. The filter offers each owner kind
// under its word, and a POST filtered to one asks the store for exactly that kind.
//
// PART II -- red on: an owner kind with no word or another's word, an owner row said to be
// "Before sign-in", an owner label on a pre-session row or on an unknown kind (an "operator_"
// one included), an owner kind missing from the filter. PART III -- These rows and these
// strings only (no completeness claim).
func TestAuditScreen_TheOwnersRowsNameTheOwner(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	account, name := uuid.New(), "FAKE Ops Fourteen D"
	owner := func(kind string, sec int) db.OperatorAuditEntry {
		e := auditEntry(kind, sec, nil, nil, nil)
		e.TargetAdminID, e.TargetAdminName = &account, &name
		return e
	}
	words := map[string]string{
		string(db.OperatorAuditOperatorCreated):  "Operator account created",
		string(db.OperatorAuditOperatorMFAReset): "Operator account reset to a new setup link",
		string(db.OperatorAuditOperatorDisabled): "Operator account disabled",
	}
	log := []db.OperatorAuditEntry{
		owner(string(db.OperatorAuditOperatorDisabled), 59),
		owner(string(db.OperatorAuditOperatorMFAReset), 58),
		owner(string(db.OperatorAuditOperatorCreated), 57),
		owner(string(db.OperatorAuditPasswordOK), 56),
		owner("zz_later_sessionless", 55),
		owner("operator_enabled", 54),
	}
	g.seedAudit(log...)

	const byOwner = `<span class="text-sm">By the platform owner, with opadmin</span>`
	const beforeSignIn = `<span class="text-sm">Before sign-in</span>`
	accountFact := `<span class="text-sm">Account <bdi>` + html.EscapeString(name) + `</bdi></span>`
	w := g.get("/operator/audit", c)
	if w.Code != http.StatusOK {
		t.Fatalf("the audit screen = %d", w.Code)
	}
	body := w.Body.String()
	rows := auditRowRe.FindAllStringSubmatch(body, -1)
	if len(rows) != 1+len(log) {
		t.Fatalf("the screen lists %d row(s), want %d (the view's own and the six)", len(rows), 1+len(log))
	}
	for i, e := range log[:3] {
		r := rows[i+1][1]
		for _, f := range []string{`<span class="font-display font-bold">` + html.EscapeString(words[e.Kind]) + `</span>`, byOwner, accountFact} {
			if !strings.Contains(r, f) {
				t.Errorf("the %s row does not carry %q", e.Kind, f)
			}
		}
		for _, f := range []string{beforeSignIn, "Session", "By <bdi>", "Unrecognised", "Detail not shown"} {
			if strings.Contains(r, f) {
				t.Errorf("the %s row carries %q", e.Kind, f)
			}
		}
	}
	if r := rows[4][1]; !strings.Contains(r, beforeSignIn) || strings.Contains(r, byOwner) {
		t.Errorf("CONTROL: the password_ok row does not say \"Before sign-in\", or names the owner")
	}
	for i, kind := range []string{"zz_later_sessionless", "operator_enabled"} {
		r := rows[5+i][1]
		if strings.Contains(r, byOwner) || !strings.Contains(r, `<span class="font-mono font-bold">`+kind+`</span>`) ||
			!strings.Contains(r, unrecognisedChip) {
			t.Errorf("the unknown sessionless kind %s is said to be the owner's, or is not drawn as itself and unrecognised", kind)
		}
	}
	if n := strings.Count(body, byOwner); n != 3 {
		t.Errorf("%d row(s) name the platform owner, want the three owner rows", n)
	}
	for kind, word := range words {
		if !strings.Contains(body, `<option value="`+kind+`">`+html.EscapeString(word)+`</option>`) {
			t.Errorf("the filter does not offer %s under its word", kind)
		}
	}

	if w := g.post("/operator/audit", auditForm(string(db.OperatorAuditOperatorMFAReset), ""), c); w.Code != http.StatusOK {
		t.Fatalf("the audit screen filtered to operator_mfa_reset = %d", w.Code)
	}
	asks := g.auditAsks()
	if len(asks) != 2 || asks[1].Kind != db.OperatorAuditOperatorMFAReset {
		t.Errorf("the store was asked %+v; want the second view filtered to operator_mfa_reset", asks)
	}
}
