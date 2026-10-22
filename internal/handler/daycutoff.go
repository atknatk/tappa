package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE BUSINESS DAY SETTINGS — M15-07 (ADR 0032 §A.2, §A.7, §A.8). Four places a cutoff is
// set (K1): the business (Account, OWNER only — mayEditAccount, the Account form's own
// gate), a branch (the venue's edit card), a department (the department's edit card) and a
// person (a block on the person card). Each draws the same block (components.DayCutoffCard /
// DayCutoffBlock) and posts to the same two routes.
//
// 🔴 TWO PRESSES, AND THE FIRST WRITES NOTHING. businessDayReview answers with the review
// page — the shifts the new value cuts, a suggestion outside every band, the check-ins open
// right now it would split, the switch's status — and mints a confirmation bound to the
// level, the subject and the value (deactivateconfirm.go's mechanism, confirmActionDayCutoff).
// businessDayApply writes only with that confirmation, through M15-02's setters, with the
// trail row in the same transaction (tenant.DayCutoffs.Set). Every change goes through the
// review, not only one that cuts a shift: the open check-ins a change splits are worth
// reading for any change, and one path means no POST writes unreviewed.
//
// 🔴 ITS OWN FORMS, NOT FIELDS OF THE VENUE AND DEPARTMENT FORMS. The card names the venue
// form's path; the cutoff is drawn on that form's page but posts here, for the reason the
// venue's external ref does (saveVenueExternalRef): a two-step write inside the eight-field
// venue save would hold the address ranges, the coordinate and the shift hostage to a
// confirmation about a different fact, and would split one save across two transactions.
//
// THE ROUTES ARE IN mountWriting's MAIN GROUP (ProtectWriting): the Origin check runs before
// the resolver, and a suspended business is refused (ADR 0025) — a settings change is not
// one of the exceptions a suspension leaves open.
//
// §4.7: nothing secret passes through. The body is a level, an id and a time of day; the
// trail row holds the old and new value and the subject's id; the log line the same.

var (
	businessDayHref      = "/admin/business-day"
	businessDayApplyHref = businessDayHref + "/apply"
)

// maxBusinessDayBody bounds the form: a level, a uuid, a time and the roster position the
// person card carries back (employeeactions.go's ceiling, for the same fields).
const maxBusinessDayBody = maxEmployeeActionBody

// businessDayAnchor is the block's id on every page that draws it; the return address
// scrolls to it.
const businessDayAnchor = "business-day"

// businessDayParam carries the outcome of the last press back to the block, from a closed
// list — a word, never a sentence (oneOfWords). It is the block's own parameter rather than
// a word in each section's done/problem vocabulary, the way the brand editor carries its own.
const businessDayParam = "day"

var businessDayWords = []string{
	"saved", "same", "bad-time", "required", "confirm-required", "confirm-stale", "unavailable", "too-large",
	// "cuts" is a venue's or department's SAVE whose shift the business day cuts: the save
	// went through and lands on the form, where the block below says what is cut (§A.7:
	// the first shift entered is warned about in that form). The section's own "saved"
	// word travels with it (savedButCut), so the ordinary banner is not replaced.
	"cuts",
}

// ActionBusinessDayRefused records a manager's attempt at the business level, which the role
// gate refused (the `<subject>.<verb>_<refusal>` shape of tenant.account_update_refused).
const ActionBusinessDayRefused = "tenant.day_cutoff_update_refused"

// panelBusinessDays is the slice of internal/domain/tenant this file needs, declared at the
// consumer (§7).
type panelBusinessDays interface {
	Review(ctx context.Context, c tenant.CutoffReviewCommand) (tenant.CutoffReview, error)
	Set(ctx context.Context, c tenant.SetCutoffCommand) (tenant.CutoffChange, error)
	Person(ctx context.Context, tenantID, employeeID uuid.UUID) (tenant.PersonCutoff, error)
	Move(ctx context.Context, c tenant.MoveCutoffCommand) (tenant.MoveCutoff, error)
	Version(ctx context.Context, tenantID uuid.UUID, level tenant.CutoffLevel, subject uuid.UUID) (string, error)
	Switch(now time.Time) tenant.BusinessDaySwitch
}

// BusinessDays gives the panel the business-day service. cmd/tappa always passes it; a panel
// built without it draws no business-day block, moves people without the cutoff warning, and
// answers the two routes with 503.
func BusinessDays(d panelBusinessDays) AdminAuthOption {
	return func(a *AdminAuth) { a.businessDays = d }
}

// mayChangeBusinessDay is the role gate: the business level is the owner's (it is the
// Account form's fact, mayEditAccount); a branch, a department and a person are changed by
// whoever may save that venue, department or person today — every signed-in admin.
func mayChangeBusinessDay(id httpx.AdminIdentity, level tenant.CutoffLevel) bool {
	if level == tenant.CutoffBusiness {
		return mayEditAccount(id)
	}
	return id.Live()
}

// problemBusinessDayUnavailable answers the routes on a panel built without the service.
var problemBusinessDayUnavailable = pages.ProblemView{
	Title:   "The business day settings are not available here",
	Message: "This deployment was started without the business day service, so nothing was changed.",
	Hint:    "Tell us and we will look.",
}

// businessDayForm is one posted block, validated at the boundary (§7).
type businessDayForm struct {
	level   tenant.CutoffLevel
	subject uuid.UUID // the tenant for the business level
	value   *time.Duration
	roster  ledger.RosterFilter
}

// readBusinessDayForm bounds, parses and validates the body. A malformed body or level, or a
// subject id that is not a uuid, is answered here with a redirect and false; a time that is
// not a time is answered at the block (the word "bad-time"), since the block is known.
func (a *AdminAuth) readBusinessDayForm(w http.ResponseWriter, r *http.Request) (businessDayForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBusinessDayBody)
	if err := r.ParseForm(); err != nil {
		// THE PARSE ERROR IS CLASSIFIED, NOT PRINTED (readAction's reason: net/url quotes the
		// offending input).
		reason := "malformed form"
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reason = "body over the limit"
		}
		a.log.Warn("panel business day refused: the form could not be read",
			"limit_bytes", maxBusinessDayBody, "reason", reason)
		a.redirect(w, accountReturn("", "unreadable"))
		return businessDayForm{}, false
	}
	level, ok := tenant.ParseCutoffLevel(strings.TrimSpace(r.PostFormValue("level")))
	if !ok {
		a.log.Warn("panel business day refused: the level is not one of the four")
		a.redirect(w, accountReturn("", "unreadable"))
		return businessDayForm{}, false
	}
	f := businessDayForm{level: level, subject: httpx.AdminOf(r).TenantID()}
	if level != tenant.CutoffBusiness {
		id, err := uuid.Parse(strings.TrimSpace(r.PostFormValue("id")))
		if err != nil || id == uuid.Nil {
			a.log.Warn("panel business day refused: the subject id is not a uuid", "level", string(level))
			a.redirect(w, businessDayUnreadable(level))
			return businessDayForm{}, false
		}
		f.subject = id
	}
	if level == tenant.CutoffPerson {
		// The roster position travels through the URL's own validator (readAction's rule).
		f.roster = rosterFilterFrom(r.PostForm)
	}
	return f, true
}

// parseCutoffValue reads the posted time: "" is inherit (nil — never 00:00), a value is
// "15:04" or "15:04:00" (what <input type="time"> sends), in whole minutes.
//
// THE SHAPE IS CHECKED BEFORE parseClock, and only here (review DÜŞÜK-3): time.Parse accepts
// a fractional second after "05" even with no fraction in the layout, so parseClock reads
// "05:00:00.5" as 05:00. The shift fields share parseClock and keep its behaviour; a cutoff
// is whole minutes (00064), and a value that is not is refused rather than rounded.
func parseCutoffValue(raw string) (*time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	if !cutoffShape.MatchString(raw) {
		return nil, false
	}
	d, ok := parseClock(raw)
	if !ok || !tenant.ValidDayCutoff(d) {
		return nil, false
	}
	return &d, true
}

// cutoffShape is a whole-minute wall clock as a time input posts it.
var cutoffShape = regexp.MustCompile(`^[0-2][0-9]:[0-5][0-9](:00)?$`)

// businessDayUnreadable is where a form with no readable subject goes: its level's section.
func businessDayUnreadable(level tenant.CutoffLevel) string {
	switch level {
	case tenant.CutoffBranch, tenant.CutoffDepartment:
		return venueReturn("", "unreadable")
	case tenant.CutoffPerson:
		return employeesHref + "?problem=unreadable"
	}
	return accountReturn("", "unreadable")
}

// businessDayReturn is the block's own address with the outcome word. Every part is
// server-built: the level from a closed list, the id a parsed uuid, the word from
// businessDayWords, the roster position re-validated (rosterReturn).
func businessDayReturn(f businessDayForm, word string) string {
	word = oneOfWords(word, businessDayWords...)
	q := url.Values{}
	if word != "" {
		q.Set(businessDayParam, word)
	}
	var base string
	switch f.level {
	case tenant.CutoffBranch:
		q.Set("venue", f.subject.String())
		base = locationsHref
	case tenant.CutoffDepartment:
		q.Set("department", f.subject.String())
		base = locationsHref
	case tenant.CutoffPerson:
		target := rosterReturn(f.roster, f.subject, "", "")
		if word == "" {
			return target + "#" + businessDayAnchor
		}
		sep := "?"
		if strings.Contains(target, "?") {
			sep = "&"
		}
		return target + sep + q.Encode() + "#" + businessDayAnchor
	default:
		base = accountHref
	}
	if len(q) == 0 {
		return base + "#" + businessDayAnchor
	}
	return base + "?" + q.Encode() + "#" + businessDayAnchor
}

// savedButCut is where a venue's or department's save goes when the business day cuts the
// shift it stored: its form, with the section's own done word (the ordinary "saved" banner,
// review DÜŞÜK-7) AND the block's "cuts" notice, so neither replaces the other.
func savedButCut(level tenant.CutoffLevel, subject uuid.UUID, done string) string {
	target := businessDayReturn(businessDayForm{level: level, subject: subject}, "cuts")
	path, frag, _ := strings.Cut(target, "#")
	if w := oneOfWords(done, venueDoneWords...); w != "" {
		path += "&done=" + w
	}
	return path + "#" + frag
}

// businessDaySubject is what the confirmation is bound to: the level, the subject, the
// VERSION the review was served against and the new value together (the compound subject
// of record.manual and policy.edit), so a warning served for one value cannot be spent on
// another. The version (tenant.CutoffReview.Version: the value on file and its change
// count) is the orchestrator's DÜŞÜK-1 decision: a confirmation is spent once in effect --
// the write moves the count -- and one served before somebody else changed the value is
// "confirm-stale", not a write over a change the page never showed. A bare "from" value
// would not do: A -> B -> A puts the old value back and the old token would verify again.
// None of the parts can carry "|".
func businessDaySubject(f businessDayForm, version string) string {
	v := tenant.ClockText(f.value)
	if v == "" {
		v = "inherit"
	}
	return string(f.level) + ":" + f.subject.String() + ":" + version + ":" + v
}

// businessDayReview is the first press: it reads what the change would do and answers with
// the review page and a confirmation. It writes nothing — no setting, no trail row.
func (a *AdminAuth) businessDayReview(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	f, ok := a.readBusinessDayForm(w, r)
	if !ok {
		return
	}
	// THE ROLE IS CHECKED BEFORE THE VALUE AND BEFORE ANY READ (accountSave's order).
	if !mayChangeBusinessDay(id, f.level) {
		a.refuseBusinessDay(r, id, "review")
		a.redirect(w, accountReturn("", "not-permitted"))
		return
	}
	value, ok := parseCutoffValue(r.PostFormValue("cutoff"))
	if !ok {
		a.log.Warn("panel business day refused: the time is not a time of day", "level", string(f.level))
		a.redirect(w, businessDayReturn(f, "bad-time"))
		return
	}
	f.value = value
	if a.businessDays == nil {
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemBusinessDayUnavailable)
		return
	}
	now := time.Now()
	rv, err := a.businessDays.Review(r.Context(), tenant.CutoffReviewCommand{
		TenantID: id.TenantID(), Level: f.level, SubjectID: f.subject, Value: f.value, Now: now,
	})
	if a.businessDayRefusal(w, r, f, err) {
		return
	}
	if !rv.Changes() {
		a.redirect(w, businessDayReturn(f, "same"))
		return
	}
	token, err := a.setConfirmation(w, r, confirmActionDayCutoff, businessDaySubject(f, rv.Version))
	if err != nil {
		a.log.Error("panel: could not mint the business day confirmation", "err", err)
		a.redirect(w, businessDayReturn(f, "unavailable"))
		return
	}
	v := a.businessDayReviewView(r, f, rv, token, now)
	a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminBusinessDayReview(v))
}

// businessDayApply is the confirmed press: the role again (the write must refuse on its own),
// the confirmation bound to this level, subject and value, then the one write.
func (a *AdminAuth) businessDayApply(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	f, ok := a.readBusinessDayForm(w, r)
	if !ok {
		return
	}
	if !mayChangeBusinessDay(id, f.level) {
		a.refuseBusinessDay(r, id, "write")
		a.redirect(w, accountReturn("", "not-permitted"))
		return
	}
	value, ok := parseCutoffValue(r.PostFormValue("cutoff"))
	if !ok {
		a.log.Warn("panel business day refused: the time is not a time of day", "level", string(f.level))
		a.redirect(w, businessDayReturn(f, "bad-time"))
		return
	}
	f.value = value
	if a.businessDays == nil {
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemBusinessDayUnavailable)
		return
	}
	// The version on file NOW, which the confirmation must have been served against.
	version, err := a.businessDays.Version(r.Context(), id.TenantID(), f.level, f.subject)
	if a.businessDayRefusal(w, r, f, err) {
		return
	}
	// 🔴 THE CONFIRMATION BEFORE ANYTHING IS WRITTEN; a refusal writes nothing. A value
	// changed since the review (by anybody, or by this same token already) is
	// "confirm-stale": the subject no longer matches.
	if why := a.confirmationRefusalFor(r, confirmActionDayCutoff, businessDaySubject(f, version)); why != "" {
		a.log.Warn("panel business day refused: the review step was not completed",
			"level", string(f.level), "reason", why)
		a.redirect(w, businessDayReturn(f, why))
		return
	}
	// Cleared on the way in (employeeDeactivate's reason).
	a.short.clear(w, adminConfirmCookieName)
	change, err := a.businessDays.Set(r.Context(), tenant.SetCutoffCommand{
		TenantID: id.TenantID(), ActorID: id.Admin.AdminUserID, Level: f.level, SubjectID: f.subject, Value: f.value,
	})
	if errors.Is(err, tenant.ErrSameDayCutoff) {
		a.redirect(w, businessDayReturn(f, "same"))
		return
	}
	if a.businessDayRefusal(w, r, f, err) {
		return
	}
	a.log.Info("panel business day saved", "level", string(f.level), "subject_id", f.subject,
		"actor_id", id.Admin.AdminUserID, "from", tenant.ClockText(change.From), "to", tenant.ClockText(change.To))
	a.redirect(w, businessDayReturn(f, "saved"))
}

// businessDayRefusal answers a domain error and reports whether it did. An id that is not
// this business's goes to its section with that section's own "unknown" word.
func (a *AdminAuth) businessDayRefusal(w http.ResponseWriter, r *http.Request, f businessDayForm, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, tenant.ErrUnknownVenue):
		a.log.Warn("panel business day refused: no such venue in this tenant")
		a.redirect(w, venueReturn("", "unknown-venue"))
	case errors.Is(err, tenant.ErrUnknownDepartment):
		a.log.Warn("panel business day refused: no such department in this tenant")
		a.redirect(w, venueReturn("", "unknown-department"))
	case errors.Is(err, tenant.ErrUnknownEmployee):
		a.log.Warn("panel business day refused: no such person in this tenant (or deactivated)")
		a.redirect(w, rosterReturn(f.roster, uuid.Nil, "", "unknown"))
	case errors.Is(err, tenant.ErrUnknownTenantAccount):
		a.log.Error("panel business day: the session's own tenant row could not be read")
		a.redirect(w, accountReturn("", "unknown-business"))
	case errors.Is(err, tenant.ErrDayCutoffRequired):
		a.redirect(w, businessDayReturn(f, "required"))
	case errors.Is(err, tenant.ErrDayCutoff):
		a.redirect(w, businessDayReturn(f, "bad-time"))
	case errors.Is(err, tenant.ErrDayCutoffTooLarge):
		a.log.Error("panel business day: the business is past the review's design ceiling")
		a.redirect(w, businessDayReturn(f, "too-large"))
	default:
		a.log.Error("panel: could not review or save the business day", "err", err, "level", string(f.level))
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	}
	return true
}

// refusedBusinessDayDetail is refusedAccountDetail's shape plus the step: the act, the role
// that was insufficient and the role required — nothing that was posted.
type refusedBusinessDayDetail struct {
	Outcome      string `json:"outcome"`
	Reason       string `json:"reason"`
	Step         string `json:"step"`
	Role         string `json:"role"`
	RequiredRole string `json:"required_role"`
}

// refuseBusinessDay records a manager's attempt at the business level (refuseAccountSave's
// reasoning: the screen never offers it, so reaching here means the UI was bypassed, and the
// owner is the one who should know). Its own transaction; a trail outage does not change the
// answer.
func (a *AdminAuth) refuseBusinessDay(r *http.Request, id httpx.AdminIdentity, step string) {
	a.log.Warn("panel business day refused: the business level is an owner's",
		"actor_id", id.Admin.AdminUserID, "role", id.Admin.Role, "step", step)
	a.record(r.Context(), audit.Event{
		TenantID: id.TenantID(),
		ActorID:  ptr(id.Admin.AdminUserID),
		Action:   ActionBusinessDayRefused,
		Target:   id.TenantID().String(),
		Detail: refusedBusinessDayDetail{
			Outcome:      "refused",
			Reason:       "changing the business's day cutoff is reserved for an owner",
			Step:         step,
			Role:         id.Admin.Role,
			RequiredRole: adminRoleOwner,
		},
	})
}

// --- the words --------------------------------------------------------------------

// clockOf is a duration as a wall clock.
func clockOf(d time.Duration) string { return tenant.ClockText(&d) }

// businessDayStatus is the switch's line (ADR 0032 §A.8), the date in the business's zone.
func businessDayStatus(s tenant.BusinessDaySwitch, zone string) string {
	if !s.Active {
		return "Not active yet — the business day starts applying when Taptime switches it on"
	}
	loc, err := time.LoadLocation(zone)
	if err != nil || zone == "" {
		loc = time.UTC
	}
	return "Active since " + s.Since.In(loc).Format("2 January 2006, 15:04")
}

// possessive is "<name>'s".
func possessive(name string) string {
	if strings.HasSuffix(name, "s") {
		return name + "'"
	}
	return name + "'s"
}

// conflictSentence is ADR 0032 §A.7's sentence for one cut shift, with times that show the
// cut: a clock-in a little before the shift (and before the cutoff) and a clock-out a little
// after its end (and not before the cutoff) fall in two business days.
func conflictSentence(c tenant.CutoffConflict) string {
	s := c.Shift
	lo := s.Start - tenant.CutoffBandBefore
	span := s.End - s.Start
	if s.Overnight || span <= 0 {
		span += 24 * time.Hour
	}
	// off is the cutoff's place in the band, measured from its open lower end; in [0, 24 h).
	// It is 0 only for a band of 24 h or more (a shift of 21 h or more), whose lower end is
	// then inside the band too.
	off := wrapDay(c.Cutoff - lo)
	// The clock-in: two minutes before the shift, or two minutes before the cutoff when that
	// is earlier -- ALWAYS before the cutoff, so it belongs to the previous business day. It
	// may lie before the band's lower end (off < 2 min); it is never clamped up to it, which
	// put the example clock-in ON the cutoff -- the new day's first minute (review DÜŞÜK-5).
	in := tenant.CutoffBandBefore - 2*time.Minute
	if off-2*time.Minute < in {
		in = off - 2*time.Minute
	}
	// The clock-out: five minutes after the shift, or the cutoff itself when that is later --
	// at or after the cutoff and before the next one (24 h on), so it is the new day's.
	out := tenant.CutoffBandBefore + span + 5*time.Minute
	if off > out {
		out = off
	}
	if out >= off+24*time.Hour {
		out = off + 24*time.Hour - time.Minute
	}
	return fmt.Sprintf("%s falls inside %s shift (%s–%s): a %s clock-in would count for the previous day "+
		"and the %s clock-out would be recorded as a clock-in.",
		clockOf(c.Cutoff), possessive(s.Owner), clockOf(s.Start), clockOf(s.End),
		clockOf(wrapDay(lo+in)), clockOf(wrapDay(lo+out)))
}

func wrapDay(d time.Duration) time.Duration {
	d %= 24 * time.Hour
	if d < 0 {
		d += 24 * time.Hour
	}
	return d
}

// conflictWords is the warning: one sentence per cut shift, and the suggestion line.
func conflictWords(conflicts []tenant.CutoffConflict, suggested *time.Duration) ([]string, string) {
	if len(conflicts) == 0 {
		return nil, ""
	}
	var out []string
	for _, c := range conflicts {
		out = append(out, conflictSentence(c))
	}
	if suggested == nil {
		return out, "No time of day is outside every one of these shifts, so there is no suggestion."
	}
	return out, "Suggested: " + clockOf(*suggested) + " — outside every shift."
}

// splitsSentence is the open check-ins line.
func splitsSentence(n int, capped bool) string {
	count := strconv.Itoa(n)
	if capped {
		count = "at least " + count
	}
	switch {
	case n == 0 && !capped:
		return "No check-in that is open right now would be split."
	case n == 1 && !capped:
		return "Changing it now would also split 1 check-in that is open right now."
	}
	return "Changing it now would also split " + count + " check-ins that are open right now."
}

// valueText is a level's stored value as the review says it.
func valueText(v *time.Duration, inherit string) string {
	if v == nil {
		return "Not set — " + inherit
	}
	return clockOf(*v)
}

// changeSummary is the review's first sentence: what the change does, then what is on file.
func changeSummary(where string, stored, proposed *time.Duration, inherit string) string {
	after := "would start at " + tenant.ClockText(proposed)
	if proposed == nil {
		after = "would no longer be set here: " + inherit
	}
	before := "Today it starts at " + tenant.ClockText(stored) + "."
	if stored == nil {
		before = "Today it is not set here: " + inherit + "."
	}
	return "The business day for " + where + " " + after + ". " + before +
		" A check-in is closed only by a clock-out of the same business day."
}

// sourceText is where a person's cutoff comes from, as the card says it.
func sourceText(s tenant.CutoffSource) string {
	switch s.Level {
	case tenant.CutoffPerson:
		return "set for this person"
	case tenant.CutoffDepartment, tenant.CutoffBranch:
		return "from " + s.Name
	}
	return "the business setting"
}

// levelInherit is what an empty value means at a level.
func levelInherit(level tenant.CutoffLevel, business time.Duration) string {
	switch level {
	case tenant.CutoffBranch:
		return "uses the business setting (" + clockOf(business) + ")"
	case tenant.CutoffDepartment:
		return "each person uses their branch's or the business's setting"
	case tenant.CutoffPerson:
		return "uses their department's, branch's or the business's setting"
	}
	return ""
}

// businessDayHint is the help under the field (the card's two sentences, and the person's).
func businessDayHint(level tenant.CutoffLevel, business time.Duration) string {
	switch level {
	case tenant.CutoffBusiness:
		return "Every venue, department and person without a setting of their own starts the " +
			"business day at this time. A check-in is closed only by a clock-out of the same business day."
	}
	l := levelInherit(level, business)
	return "Empty — " + l
}

func businessDayOutcome(r *http.Request) *components.DayCutoffOutcome {
	switch oneOfWords(strings.TrimSpace(r.URL.Query().Get(businessDayParam)), businessDayWords...) {
	case "saved":
		return &components.DayCutoffOutcome{Tone: components.ToneOK, Heading: "Saved",
			Text: "The business day setting below is what is now on file."}
	case "cuts":
		return &components.DayCutoffOutcome{Tone: components.ToneWarn, Heading: "Saved — but the business day cuts this shift",
			Text: "The shift was saved. The business day these people use starts inside it; what that does and a time outside every shift are below."}
	case "same":
		return &components.DayCutoffOutcome{Tone: components.ToneOK, Heading: "Nothing to change",
			Text: "That is already the setting on file, so nothing was written."}
	case "bad-time":
		return &components.DayCutoffOutcome{Tone: components.ToneAlert, Heading: "That was not saved",
			Text: "The business day starts at a time of day in whole minutes, like 05:00. Nothing was changed."}
	case "required":
		return &components.DayCutoffOutcome{Tone: components.ToneAlert, Heading: "That was not saved",
			Text: "The business needs a time: it is what every venue, department and person without their own uses."}
	case "confirm-required":
		return &components.DayCutoffOutcome{Tone: components.ToneAlert, Heading: "That was not saved",
			Text: "The change was not confirmed on its review page, or the page was older than ten minutes. Review it again."}
	case "confirm-stale":
		return &components.DayCutoffOutcome{Tone: components.ToneAlert, Heading: "That was not saved",
			Text: "Another review was opened after this one, so this one no longer counts. Review the change again."}
	case "unavailable":
		return &components.DayCutoffOutcome{Tone: components.ToneAlert, Heading: "That was not saved",
			Text: "The change could not be prepared. Nothing was changed; try again in a minute."}
	case "too-large":
		return &components.DayCutoffOutcome{Tone: components.ToneAlert, Heading: "That was not saved",
			Text: "This business has more venues, departments or people than this screen can check at once, " +
				"so the change was not reviewed. Tell us and we will make it with you."}
	}
	return nil
}

// --- the blocks ---------------------------------------------------------------------

// businessDayBlock is the block's common part: the status line and the outcome notice.
func (a *AdminAuth) businessDayBlock(r *http.Request, zone string) components.DayCutoffView {
	return components.DayCutoffView{
		Anchor:  businessDayAnchor,
		Status:  businessDayStatus(a.businessDays.Switch(time.Now()), zone),
		Outcome: businessDayOutcome(r),
		Action:  businessDayHref,
		InputID: "business-day-cutoff",
	}
}

// accountBusinessDay is the Account card: the business level, the owner's form.
func (a *AdminAuth) accountBusinessDay(r *http.Request, id httpx.AdminIdentity) *components.DayCutoffView {
	if a.businessDays == nil {
		return nil
	}
	rv, err := a.businessDays.Review(r.Context(), tenant.CutoffReviewCommand{
		TenantID: id.TenantID(), Level: tenant.CutoffBusiness, Unchanged: true, Now: time.Now(),
	})
	if err != nil {
		a.log.Error("panel: could not read the business day for the account card", "err", err)
		v := a.businessDayBlock(r, "")
		v.ValueLine = "The business day setting could not be read."
		return &v
	}
	v := a.businessDayBlock(r, rv.Zone)
	v.ValueLine = clockOf(rv.Business) + " — the business setting"
	v.Input = clockOf(rv.Business)
	v.Hint = businessDayHint(tenant.CutoffBusiness, rv.Business)
	v.Hidden = []components.FormField{{Name: "level", Value: string(tenant.CutoffBusiness)}}
	v.CanChange = mayChangeBusinessDay(id, tenant.CutoffBusiness)
	if !v.CanChange {
		v.WhyNot = "The business day is changed by an owner of the business."
	}
	return &v
}

// placeBusinessDay is the venue's or the department's card, with the shifts the CURRENT value
// cuts — so the warning appears on the form the first shift is entered in (§A.7).
func (a *AdminAuth) placeBusinessDay(r *http.Request, level tenant.CutoffLevel, subject uuid.UUID) *components.DayCutoffView {
	if a.businessDays == nil {
		return nil
	}
	id := httpx.AdminOf(r)
	probe, err := a.businessDays.Review(r.Context(), tenant.CutoffReviewCommand{
		TenantID: id.TenantID(), Level: level, SubjectID: subject, Unchanged: true, Now: time.Now(),
	})
	if err != nil {
		a.log.Error("panel: could not read the business day for the card", "err", err, "level", string(level))
		v := a.businessDayBlock(r, "")
		v.ValueLine = "The business day setting could not be read."
		return &v
	}
	v := a.businessDayBlock(r, probe.Zone)
	switch {
	case probe.Stored != nil && level == tenant.CutoffBranch:
		v.ValueLine = clockOf(*probe.Stored) + " — set for this venue"
	case probe.Stored != nil:
		v.ValueLine = clockOf(*probe.Stored) + " — set for this department"
	default:
		v.ValueLine = "Not set — " + levelInherit(level, probe.Business)
	}
	v.Input = tenant.ClockText(probe.Stored)
	v.Hint = businessDayHint(level, probe.Business)
	v.Warnings, v.Suggestion = conflictWords(probe.Conflicts, probe.Suggested)
	v.Hidden = []components.FormField{{Name: "level", Value: string(level)}, {Name: "id", Value: subject.String()}}
	v.CanChange = mayChangeBusinessDay(id, level)
	return &v
}

// personBusinessDay is the person card's block: the effective value and its source.
func (a *AdminAuth) personBusinessDay(r *http.Request, employeeID uuid.UUID, hidden []components.FormField) *components.DayCutoffView {
	if a.businessDays == nil {
		return nil
	}
	id := httpx.AdminOf(r)
	p, err := a.businessDays.Person(r.Context(), id.TenantID(), employeeID)
	if errors.Is(err, tenant.ErrUnknownEmployee) {
		// Deactivated (or gone): they tap nothing, so there is no business day to show.
		return nil
	}
	if err != nil {
		a.log.Error("panel: could not read the person's business day", "err", err)
		v := a.businessDayBlock(r, "")
		v.ValueLine = "The business day setting could not be read."
		return &v
	}
	v := a.businessDayBlock(r, p.Zone)
	v.ValueLine = clockOf(p.Value) + " — " + sourceText(p.Source)
	v.Input = tenant.ClockText(p.Own)
	v.Hint = businessDayHint(tenant.CutoffPerson, 0)
	v.InputID = "person-business-day-cutoff"
	v.Hidden = append([]components.FormField{{Name: "level", Value: string(tenant.CutoffPerson)}}, hidden...)
	v.CanChange = mayChangeBusinessDay(id, tenant.CutoffPerson)
	return &v
}

// --- the review page ----------------------------------------------------------------

// businessDayReviewView builds the review from the domain's answer.
func (a *AdminAuth) businessDayReviewView(r *http.Request, f businessDayForm, rv tenant.CutoffReview, token string, now time.Time) pages.BusinessDayReviewView {
	tab := pages.TabAccount
	where := "the business"
	back := accountHref + "#" + businessDayAnchor
	switch f.level {
	case tenant.CutoffBranch:
		tab, where = pages.TabLocations, rv.SubjectName
		back = locationsHref + "?venue=" + f.subject.String() + "#" + businessDayAnchor
	case tenant.CutoffDepartment:
		tab, where = pages.TabLocations, rv.SubjectName
		back = locationsHref + "?department=" + f.subject.String() + "#" + businessDayAnchor
	case tenant.CutoffPerson:
		tab, where = pages.TabEmployees, "this person"
		back = rosterReturn(f.roster, f.subject, "", "") + "#" + businessDayAnchor
	}
	heading := "Business day for " + where
	if f.level == tenant.CutoffBusiness {
		heading = "Business day"
	}
	chrome := a.chrome(r, tab)
	chrome.Kicker = "Review · nothing is saved until you confirm"
	chrome.Title = heading
	chrome.Crumbs = []pages.Crumb{{Label: pages.SectionLabel(tab), Href: back}, {Label: heading}}

	inherit := levelInherit(f.level, rv.Business)
	v := pages.BusinessDayReviewView{
		PanelChrome: chrome,
		Summary:     changeSummary(where, rv.Stored, rv.Proposed, inherit),
		Lines: []pages.DayLine{
			{Label: "Now", Value: valueText(rv.Stored, inherit)},
			{Label: "After", Value: valueText(rv.Proposed, inherit)},
			{Label: "People it applies to", Value: strconv.Itoa(rv.People)},
			{Label: "Shifts checked", Value: strconv.Itoa(rv.Shifts)},
			{Label: "Timezone", Value: rv.Zone},
		},
		// Only when they were counted: with the switch off a change splits nothing now, and
		// the page says nothing about open check-ins (ORTA-1 (c)).
		Status:   businessDayStatus(a.businessDays.Switch(now), rv.Zone),
		Action:   businessDayApplyHref,
		Submit:   "Save the business day",
		BackHref: back,
	}
	if rv.OpenCounted {
		v.Splits = splitsSentence(rv.OpenSplit, rv.OpenCapped)
	}
	v.Warnings, v.Suggestion = conflictWords(rv.Conflicts, rv.Suggested)
	fields := businessDayFields(f, tenant.ClockText(f.value))
	v.Hidden = append(fields, components.FormField{Name: confirmField, Value: token})
	if len(rv.Conflicts) > 0 && rv.Suggested != nil && !sameDuration(rv.Suggested, f.value) {
		v.Suggest = &pages.DaySuggestForm{
			Action: businessDayHref,
			Hidden: businessDayFields(f, clockOf(*rv.Suggested)),
			Label:  "Review " + clockOf(*rv.Suggested) + " instead",
		}
	}
	return v
}

func sameDuration(a, b *time.Duration) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// businessDayFields are the posted facts of a change: the level, the subject (below the
// business), the value and — for a person — the roster position.
func businessDayFields(f businessDayForm, cutoff string) []components.FormField {
	out := []components.FormField{{Name: "level", Value: string(f.level)}}
	if f.level != tenant.CutoffBusiness {
		out = append(out, components.FormField{Name: "id", Value: f.subject.String()})
	}
	out = append(out, components.FormField{Name: "cutoff", Value: cutoff})
	if f.level == tenant.CutoffPerson {
		q := rosterQuery(f.roster)
		if f.roster.AfterID != nil {
			q.Set("after_id", f.roster.AfterID.String())
		}
		for key, values := range q {
			for _, value := range values {
				out = append(out, components.FormField{Name: key, Value: value})
			}
		}
	}
	return out
}

// --- the move (ADR 0032 §A.7 "Taşınma") ----------------------------------------------

// moveConfirmSubject binds the move's warning to the statement: the person, the venue and
// the department together, so a warning served for one destination cannot be spent on
// another.
func moveConfirmSubject(employeeID, locationID uuid.UUID, departmentID *uuid.UUID) string {
	dep := "none"
	if departmentID != nil {
		dep = departmentID.String()
	}
	return employeeID.String() + ":" + locationID.String() + ":" + dep
}

// moveNeedsCutoffReview answers a move whose destination changes the person's business day
// with the warning page and reports true; the warning's confirmed press comes back here with
// the confirmation and goes through. A move that leaves the cutoff where it was, a panel
// without the service, and a person or placement the read does not find (a deactivated
// person, a foreign id) report false: Staff.Move then answers as it always has.
//
// 🔴 THE WARNING DOES NOT BLOCK THE MOVE (the card: "uyarı taşımayı engellemez"): it asks once,
// with the old and new value side by side and the shifts the new value cuts.
func (a *AdminAuth) moveNeedsCutoffReview(w http.ResponseWriter, r *http.Request, f ledger.RosterFilter, employeeID, locationID uuid.UUID, departmentID *uuid.UUID) bool {
	if a.businessDays == nil {
		return false
	}
	person := businessDayForm{level: tenant.CutoffPerson, subject: employeeID, roster: f}
	subject := moveConfirmSubject(employeeID, locationID, departmentID)
	if strings.TrimSpace(r.PostFormValue(confirmField)) != "" {
		if why := a.confirmationRefusalFor(r, confirmActionMoveAcrossCutoff, subject); why != "" {
			a.log.Warn("panel move refused: the business day warning was not confirmed",
				"employee_id", employeeID, "reason", why)
			a.redirect(w, businessDayReturn(person, why))
			return true
		}
		a.short.clear(w, adminConfirmCookieName)
		return false
	}
	now := time.Now()
	m, err := a.businessDays.Move(r.Context(), tenant.MoveCutoffCommand{
		TenantID: httpx.AdminOf(r).TenantID(), EmployeeID: employeeID, LocationID: locationID, DepartmentID: departmentID, Now: now,
	})
	switch {
	case errors.Is(err, tenant.ErrUnknownEmployee), errors.Is(err, tenant.ErrUnknownPlacement):
		return false
	case err != nil:
		// §4.6: a move whose warning could not be read is not made without it.
		a.log.Error("panel: could not read what the move does to the business day", "err", err, "employee_id", employeeID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
		return true
	case !m.Changes():
		return false
	}
	token, err := a.setConfirmation(w, r, confirmActionMoveAcrossCutoff, subject)
	if err != nil {
		a.log.Error("panel: could not mint the move's business day confirmation", "err", err)
		a.redirect(w, businessDayReturn(person, "unavailable"))
		return true
	}
	v := a.moveReviewView(r, person, m, locationID, departmentID, token, now)
	a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminBusinessDayReview(v))
	return true
}

// moveReviewView is the move's warning: the old and new business day side by side.
func (a *AdminAuth) moveReviewView(r *http.Request, person businessDayForm, m tenant.MoveCutoff, locationID uuid.UUID, departmentID *uuid.UUID, token string, now time.Time) pages.BusinessDayReviewView {
	back := rosterReturn(person.roster, person.subject, "", "")
	heading := "Move and the business day"
	chrome := a.chrome(r, pages.TabEmployees)
	chrome.Kicker = "Review · the person is not moved until you confirm"
	chrome.Title = heading
	chrome.Crumbs = []pages.Crumb{{Label: pages.SectionLabel(pages.TabEmployees), Href: back}, {Label: heading}}
	v := pages.BusinessDayReviewView{
		PanelChrome: chrome,
		Summary:     "Business day starts " + clockOf(m.From) + " → " + clockOf(m.To) + " after the move",
		Lines: []pages.DayLine{
			{Label: "Now", Value: clockOf(m.From) + " — " + sourceText(m.FromSource)},
			{Label: "After the move", Value: clockOf(m.To) + " — " + sourceText(m.ToSource)},
			{Label: "Timezone", Value: m.Zone},
		},
		Status:   businessDayStatus(a.businessDays.Switch(now), m.Zone),
		Action:   employeeMoveHref,
		Submit:   "Move anyway",
		BackHref: back,
	}
	if len(m.Conflicts) == 0 {
		v.Submit = "Save placement"
	}
	v.Warnings, v.Suggestion = conflictWords(m.Conflicts, m.Suggested)
	hidden := []components.FormField{{Name: "id", Value: person.subject.String()}, {Name: "to_location", Value: locationID.String()}}
	dep := ""
	if departmentID != nil {
		dep = departmentID.String()
	}
	hidden = append(hidden, components.FormField{Name: "to_department", Value: dep})
	q := rosterQuery(person.roster)
	if person.roster.AfterID != nil {
		q.Set("after_id", person.roster.AfterID.String())
	}
	for key, values := range q {
		for _, value := range values {
			hidden = append(hidden, components.FormField{Name: key, Value: value})
		}
	}
	v.Hidden = append(hidden, components.FormField{Name: confirmField, Value: token})
	return v
}

// savedShiftCut reports whether the business day cuts the shift a venue or department save
// just stored -- the card's "first shift entered" case. It asks the same review the card
// draws (the current value, Unchanged) and looks for the saved shift among the conflicts. A
// panel without the service, a shift-less save and a failed read report false: the save has
// happened, and the card says what it can when it is opened.
func (a *AdminAuth) savedShiftCut(r *http.Request, level tenant.CutoffLevel, subject uuid.UUID, owner string, shift *tenant.Shift) bool {
	if a.businessDays == nil || shift == nil {
		return false
	}
	rv, err := a.businessDays.Review(r.Context(), tenant.CutoffReviewCommand{
		TenantID: httpx.AdminOf(r).TenantID(), Level: level, SubjectID: subject, Unchanged: true, Now: time.Now(),
	})
	if err != nil {
		a.log.Error("panel: could not check the saved shift against the business day", "err", err, "level", string(level))
		return false
	}
	want := tenant.CutoffShift{Owner: owner, Start: shift.Start, End: shift.End, Overnight: shift.Overnight}
	for _, c := range rv.Conflicts {
		if c.Shift == want {
			return true
		}
	}
	return false
}
