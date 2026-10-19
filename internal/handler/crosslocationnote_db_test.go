package handler

// crosslocationnote_db_test.go -- the employee's result screen and the
// cross-location note (ADR 0029 follow-up, user decision 2026-10-19: "görünmesin").
// The note is the record's and the report's; the screen at the plaque stays what it
// was before the note existed.

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/policy"
)

// TestCheckinDB_ACrossLocationNoteStaysOffTheEmployeesScreen: one business on the
// shipped (2026-10-18) baseline, one plaque. Its own employee taps at home; a second
// employee, whose home is ANOTHER of the business's venues, taps the same plaque on
// the same network. The second record's note carries the cross-location sentence
// after the rule's; the second screen does not, and reads word for word like the
// first (the clock aside). The first screen is the production ok screen
// TestCheckinDB_ScreenTextIsWhatProductionRenders pins.
//
// MUTANT (measured): the result view reading Decision.Note instead of EmployeeNote
// -> the sentence is on the second screen, red.
func TestCheckinDB_ACrossLocationNoteStaysOffTheEmployeesScreen(t *testing.T) {
	var sentence string
	for _, d := range policy.Baseline() {
		if st := d.Document.Statements[0]; st.Sid == policy.SidCrossLocationNote {
			if st.Effect != policy.EffectNote {
				t.Fatalf("PREMISE: the shipped cross-location rule is %q, not a note", st.Effect)
			}
			sentence = st.Reason
		}
	}
	h := newTapHarness(t)
	var elsewhere uuid.UUID
	mover := uuid.New()
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, 'Sliema') RETURNING id`, h.tenantID).Scan(&elsewhere); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at, activated_at)
			 VALUES ($1, $2, $3, 'Joseph Camilleri', 'active', now(), now())`, mover, h.tenantID, elsewhere)
		return err
	}); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	clockRE := regexp.MustCompile(`\d{2}:\d{2}:\d{2}`)

	home := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), h.sessionCookieFor(t), nil)
	homeRec := h.lastRecord(t, h.employeeID)
	away := h.postTap(t, h.nfcContext(uint32(h.startCtr)+2), h.cookieForEmployee(t, mover), nil)
	awayRec := h.lastRecord(t, mover)

	if homeRec.Verdict != "ok" || awayRec.Verdict != "ok" {
		t.Fatalf("verdicts %q / %q, want ok / ok (sids %q / %q)", homeRec.Verdict, awayRec.Verdict,
			deref(homeRec.MatchedSid), deref(awayRec.MatchedSid))
	}
	if strings.Contains(deref(homeRec.Note), sentence) {
		t.Fatalf("PREMISE: the home tap's record carries the cross-location sentence: %q", deref(homeRec.Note))
	}
	if want := noteIPMatched + "; " + sentence; deref(awayRec.Note) != want {
		t.Fatalf("the other-venue record's note is %q, want %q -- the record keeps the note", deref(awayRec.Note), want)
	}
	homeText := clockRE.ReplaceAllString(screenText(t, home.Body.String()), "<clock>")
	awayText := clockRE.ReplaceAllString(screenText(t, away.Body.String()), "<clock>")
	if strings.Contains(away.Body.String(), sentence) {
		t.Errorf("the employee's screen at another venue prints the cross-location sentence:\n%s", awayText)
	}
	if awayText != homeText {
		t.Errorf("the screen at another venue differs from the screen at home:\n away: %s\n home: %s", awayText, homeText)
	}
	want := shellText + " St Julians Tapped in <clock> APPROVED Trust 70 " + noteIPMatched +
		" Have a great shift — keep those kebabs rolling! 🌯 All done — you can close this page."
	if homeText != want {
		t.Errorf("the home screen is not production's ok screen:\n got: %s\nwant: %s", homeText, want)
	}
}
