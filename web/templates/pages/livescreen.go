package pages

// The live QR screen page's view models (M16-20, internal/handler/livescreen.go). Neither carries a
// code, a session or a token: the pairing form never echoes what was typed, and the QR page is
// drawn empty -- its code arrives from the poll and only as a drawing.

// LiveScreenPairView is the pairing form.
//
// The form posts to "/screen/pair" written as a literal in the template (handler.LiveScreenPairPath;
// TestLiveScreen_TheFormPostsToThePairRoute holds the two equal), the way the other non-panel forms
// are written (internal/handler's nonPanelPostActions).
type LiveScreenPairView struct {
	// Problem is one of the closed words below, or "".
	Problem string
}

// The pairing form's closed vocabulary: each is one sentence the template owns.
const (
	// LiveScreenProblemRefused is every refused code -- mistyped, used, expired, removed -- in one
	// sentence, so the page tells a guesser nothing about which.
	LiveScreenProblemRefused = "refused"
	// LiveScreenProblemRemoved is a screen whose session names a revoked screen.
	LiveScreenProblemRemoved = "removed"
	// LiveScreenProblemBusy is a spent budget (429).
	LiveScreenProblemBusy = "busy"
	// LiveScreenProblemUnavailable is our failure (503 / 500).
	LiveScreenProblemUnavailable = "unavailable"
	// LiveScreenProblemLapsed is a screen paired longer ago than its session lives (180 days).
	LiveScreenProblemLapsed = "lapsed"
)

// LiveScreenView is the QR page of a paired screen.
type LiveScreenView struct {
	// VenueName is the venue the screen stands at -- a place's name, no person's.
	VenueName string
	// State is why the screen shows no code now: "" (it does), LiveScreenStateInactive or
	// LiveScreenStatePaused. The page carries all three sentences and the script flips between
	// them as the poll answers, so the code comes back by itself.
	State string
	// CodeURL is the poll (handler.LiveScreenCodePath).
	CodeURL string
}

// The QR page's two paused states, the poll's 409 words (internal/handler/livescreen.go).
const (
	// LiveScreenStateInactive is a venue switched off in the panel.
	LiveScreenStateInactive = "inactive"
	// LiveScreenStatePaused is a business the operator suspended (ADR 0025); the screen says only
	// that it is paused.
	LiveScreenStatePaused = "paused"
)
