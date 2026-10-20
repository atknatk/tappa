package pages

// PanelProblemView is the panel's "this did not work" card (M12-10): the panel twin of
// ProblemView, which stays the tap and activation screens' own (their bytes are pinned,
// TestUnbrandedScreens_AreByteIdenticalToTheGolden).
//
// EVERY FIELD IS DECIDED BY THE HANDLER FROM WHAT IT ALREADY HAS, never from the request:
// Status is the status line the response is sent with, OwnerOnly comes from the problem
// constant, and the call to action depends only on whether the request carried a live
// panel session. The words are the constant's own -- this type adds none.
type PanelProblemView struct {
	// Status is the HTTP status the response answers with; the card prints it as a
	// code chip when it is an error status (4xx or 5xx). A problem sent with 200 --
	// one exists, the sign-in step that finds every verified business disabled --
	// draws no chip rather than a "200" that reads as success.
	Status int
	// OwnerOnly draws the OWNER ONLY pill: the refusal is about the caller's role.
	OwnerOnly bool

	Title   string
	Message string
	Hint    string

	// CTAHref and CTALabel are the one way on. A request without a live panel session
	// is sent to the sign-in page and never to The Pass, which would only bounce it
	// there; a signed-in one goes back to The Pass. Both are app-relative paths set by
	// the handler, never request bytes.
	CTAHref  string
	CTALabel string
}

// ShowsStatus reports whether the status is one the card prints as a code chip.
func (v PanelProblemView) ShowsStatus() bool { return v.Status >= 400 && v.Status <= 599 }
