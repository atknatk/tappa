package pages

// The business day's release surfaces (M15-08, ADR 0032 §A.8): the owner's "Business day
// check" card on Account, the notice the panel's shell draws while that check finds a cut
// shift, and the release announcement on the anomaly and report screens.
//
// EVERY STRING IS SERVER-BUILT (internal/handler/businessdaycheck.go) from the domain's answer
// and fixed prose. Every leaf is a string, a count or a flag -- the §4.7 walls walk the views
// that embed these by type.

// BusinessDayCheckView is the Account card: every level of the business that sets a cutoff,
// checked at its value on file. Read-only -- each cutting line links to the form that changes
// that level.
type BusinessDayCheckView struct {
	// Anchor is the card's id, the notice's link target.
	Anchor string
	// Intro says what the card checks.
	Intro string
	// Status is the switch's line (ADR 0032 §A.8): "Not active yet — …" or "Active since …".
	Status string
	// Summary is the card's verdict in one sentence: how many shifts are cut, or none.
	Summary string
	// Lines are the levels whose value cuts a shift, business first.
	Lines []BusinessDayCheckLine
	// Clear names the levels checked that cut nothing ("" when there are none).
	Clear string
	// Failed is the sentence when the check could not be read; the other fields are then
	// empty.
	Failed string
}

// BusinessDayCheckLine is one level whose value cuts a shift.
type BusinessDayCheckLine struct {
	// Label names the level and its value ("Gzira — 01:00").
	Label string
	// Facts is "3 people · 4 shifts checked".
	Facts string
	// Warnings are the conflict sentences (M15-07's), Suggestion the value outside every
	// band or the sentence that there is none.
	Warnings   []string
	Suggestion string
	// Href is the form that changes this level, LinkText the link's words.
	Href     string
	LinkText string
}

// PanelBusinessDayCheck is the shell's notice while the owner's check finds a cut shift
// (M15-08 step 3a). The zero value draws nothing.
type PanelBusinessDayCheck struct {
	Heading  string
	Sentence string
	Href     string
	LinkText string
}

// Shown reports whether there is a notice to draw.
func (n PanelBusinessDayCheck) Shown() bool { return n.Heading != "" }

// BusinessDayRelease is the release announcement (M15-08 step 4): one notice, up to two
// sentences, each its own main clause. The zero value draws nothing.
type BusinessDayRelease struct {
	Heading string
	// List is the open check-in list's sentence, from the release (T0).
	List string
	// Day is the business day's sentence, only while the switch is on (from T2).
	Day string
}

// Shown reports whether there is an announcement to draw.
func (n BusinessDayRelease) Shown() bool { return n.List != "" || n.Day != "" }
