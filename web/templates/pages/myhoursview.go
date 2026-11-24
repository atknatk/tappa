package pages

// MyHoursView is the browser's My hours page (M16-32): one week of the person's own records, in the
// business's zone, already formatted by the handler. It carries no business's brand -- no logo, no
// accent (CLAUDE.md §9's numbered slots do not grow) -- and nothing about a record the person's own
// screens never show (no trust score, no rule note, no coordinate, no address).
type MyHoursView struct {
	// Week is the heading's week ("Week of 23 Nov 2026").
	Week string
	// Total is the counted time ("12h 30m"); Awaiting the time waiting for a manager ("" when none).
	Total, Awaiting string
	// Notes are the plain sentences that say why the total is not complete yet (open check-ins, a
	// check-in missing, a read past its bound); empty when it is complete.
	Notes []string
	// Waiting is the window-wide line about records and claims waiting for a manager ("" when none).
	Waiting string
	Days    []MyHoursDay
	// PrevHref / NextHref are the neighbouring weeks' pages ("" when outside the window); MoreHref is
	// this week's next page of records ("" on the last page).
	PrevHref, NextHref, MoreHref string
}

// MyHoursDay is one day of the week: its counted time and the records on it.
type MyHoursDay struct {
	Label   string // "Mon 23 Nov"
	Total   string // "8h 00m"
	Records []MyHoursRecord
}

// MyHoursRecord is one record in the person's words (decision (b) of the M16-32 card: plain words, no
// stamps).
type MyHoursRecord struct {
	At        string // "08:02"
	Direction string // "In", "Out" or ""
	Venue     string
	State     string // Counted · Waiting for your manager · Not counted · Repeat
	Claim     string // "" or a short line about a claim anchored on the record
}
