package pages

import "github.com/atknatk/tappa/web/templates/components"

// BusinessDayReviewView is the page between the first press and the write (M15-07, ADR 0032
// §A.7): what the change is, the shifts it would cut, a suggestion, the check-ins open now
// that it would split, whether the rule applies yet -- and the confirmed second press. The
// person move's warning (§A.7 "Taşınma") is the same page with the move's facts.
//
// EVERY STRING IS SERVER-BUILT from the database's answer and fixed prose (internal/handler/
// daycutoff.go); the hidden fields are values the handler already validated.
type BusinessDayReviewView struct {
	PanelChrome

	// Summary is the change in one sentence.
	Summary string
	// Lines are the facts of the change, label and value.
	Lines []DayLine
	// Warnings are the conflict sentences; Suggestion the value outside every band (or the
	// sentence that there is none); Splits the open check-ins sentence ("" on the move page).
	Warnings   []string
	Suggestion string
	Splits     string
	// Status is the switch's line (ADR 0032 §A.8).
	Status string

	// Action, Hidden and Submit are the confirmed press: Hidden carries the change and the
	// confirmation, never anything the review did not just show.
	Action   string
	Hidden   []components.FormField
	Submit   string
	BackHref string

	// Suggest, when set, is a second form that reviews the suggested value instead.
	Suggest *DaySuggestForm
}

// DayLine is one fact of the change.
type DayLine struct {
	Label string
	Value string
}

// DaySuggestForm posts the suggested value back to the review route.
type DaySuggestForm struct {
	Action string
	Hidden []components.FormField
	Label  string
}
