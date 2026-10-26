package claim

// chain.go -- the pure half: the person's direction chain as the READER classifies it, the claim
// window, the prefill, the band and the K4 rule. No database, no clock: every instant is a
// parameter, so each line of ADR 0032 §C.1 and §C.3 is a table test.

import (
	"bytes"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// Event is one directed, non-practice record of the person's chain, reduced to what the
// classification reads (ADR 0032 §A.6): its direction, its instant, its write instant (W) and its
// frozen business day (NULL on a manual row and before M15).
type Event struct {
	ID        uuid.UUID
	Direction tap.Type
	// At is occurred_at -- the order of the chain, with ID breaking a tie (§A.4).
	At time.Time
	// WrittenAt is created_at, the database's write instant (W: "P written later").
	WrittenAt time.Time
	// DayStart is business_day_start: S, frozen at the tap; nil on a manual row and before M15.
	DayStart *time.Time
	Channel  tap.Channel
}

// before is the chain's order: (occurred_at, id) -- the order every reader of the classification
// uses (ListWorkedShiftEvents, GetLastOpenTransaction, tappa_checkout_class's callers). uuid
// compares as Postgres compares it: byte by byte.
func before(a, b Event) bool {
	if !a.At.Equal(b.At) {
		return a.At.Before(b.At)
	}
	return bytes.Compare(a.ID[:], b.ID[:]) < 0
}

// Chain is a contiguous slice of one person's chain in (occurred_at, id) order.
//
// COMPLETE SAYS WHETHER ITS FIRST EVENT'S PREDECESSOR IS KNOWN. The loader reads a window of time
// and the one event right before it (ListPunchClaimChain's before_range row); when that row exists
// it is the first event here and ITS predecessor lies outside, so its own class cannot be computed.
// When no event precedes the window the slice starts at the chain's start and the first event's
// predecessor is truly "none". Every answer that would need what lies outside says so (ok=false)
// rather than guessing (§4.6: no silent answer -- a claim refused is said, a claim on a guessed
// class is not).
type Chain struct {
	events   []Event
	complete bool
}

// NewChain is a chain of events (any order -- they are sorted) whose first event's predecessor is
// known exactly when complete: the slice starts at the person's first directed record, or the
// caller knows nothing directed precedes it.
//
// AN EVENT WITH NO DIRECTION IS DROPPED: a refused or an ignored tap is not in the chain (ADR 0032
// §C.1 -- "yönsüzdürler"), so it is never an anchor, never K4's T1 and never a showing. The loader's
// `type IS NOT NULL` already keeps them out; this is the same rule for a hand-built chain.
func NewChain(events []Event, complete bool) Chain {
	c := Chain{complete: complete}
	for _, e := range events {
		if e.Direction == tap.TypeIn || e.Direction == tap.TypeOut {
			c.events = append(c.events, e)
		}
	}
	sort.SliceStable(c.events, func(i, j int) bool { return before(c.events[i], c.events[j]) })
	return c
}

// Index is the position of the record id in the chain, or -1.
func (c Chain) Index(id uuid.UUID) int {
	for i, e := range c.events {
		if e.ID == id {
			return i
		}
	}
	return -1
}

// predecessorKnown reports whether the event before c.events[i] is known (it may be "none").
func (c Chain) predecessorKnown(i int) bool { return i > 0 || c.complete }

// prevOf is ADR 0032 §A.6's P and W for the out at i: the event right before it, and whether that
// event was written after it. nil when none precedes it.
func (c Chain) prevOf(i int) *tap.Prev {
	if i == 0 {
		return nil
	}
	p := c.events[i-1]
	return &tap.Prev{Direction: p.Direction, At: p.At, WrittenLater: p.WrittenAt.After(c.events[i].WrittenAt)}
}

// dayHasIn is §A.6's I for the out at i: an `in` before it, in chain order, at or after its
// business day's start. ok is false when the start lies before what the slice holds.
func (c Chain) dayHasIn(i int, dayStart time.Time) (has, ok bool) {
	if !c.complete && (len(c.events) == 0 || !c.events[0].At.Before(dayStart)) {
		// The slice does not reach back past the day's start: an `in` of the day could lie outside.
		return false, false
	}
	for j := 0; j < i; j++ {
		if c.events[j].Direction == tap.TypeIn && !c.events[j].At.Before(dayStart) {
			return true, true
		}
	}
	return false, true
}

// ClassOf is the reader's class of the out at i (tap.ClassifyCheckout fed as §A.6 defines P, I and
// W), or ok=false when the slice does not hold what the class needs.
func (c Chain) ClassOf(i int) (tap.CheckoutClass, bool) {
	o := c.events[i]
	if o.Direction != tap.TypeOut || !c.predecessorKnown(i) {
		return "", false
	}
	has := false
	if o.DayStart != nil {
		var ok bool
		if has, ok = c.dayHasIn(i, *o.DayStart); !ok {
			return "", false
		}
	}
	return tap.ClassifyCheckout(c.prevOf(i), has, o.At, o.DayStart), true
}

// Missing is the claim the record at i admits -- the half it is missing -- by the READER's
// classification (ADR 0032 §A.6, §C.3; never Decision.LeftOpen or Decision.MissingIn, which say what
// one tap did, not what the chain is now):
//
//	an `in` that is OPEN -- the next directed record is not an `out` that closes it   -> KindOut
//	an `out` of class (b) other_day or (c) orphan                                      -> KindIn
//	anything else, and any record without a business day (Ü-Y2: before M15, manual)    -> ""
//
// ok is false when the slice cannot answer (the out's predecessor or its day's start lies outside).
func (c Chain) Missing(i int) (Kind, bool) {
	e := c.events[i]
	if e.DayStart == nil {
		return "", true
	}
	switch e.Direction {
	case tap.TypeIn:
		if i+1 < len(c.events) {
			n := c.events[i+1]
			if n.Direction == tap.TypeOut &&
				tap.ClassifyCheckout(&tap.Prev{Direction: tap.TypeIn, At: e.At}, false, n.At, n.DayStart) == tap.CheckoutCloses {
				return "", true
			}
		}
		return KindOut, true
	case tap.TypeOut:
		class, ok := c.ClassOf(i)
		if !ok {
			return "", false
		}
		if class == tap.CheckoutOtherDay || class == tap.CheckoutOrphan {
			return KindIn, true
		}
	}
	return "", true
}

// Bound is one end of a window: an instant and whether it is admitted.
type Bound struct {
	At        time.Time
	Inclusive bool
}

// Window is the set of instants a claim may name (ADR 0032 §C.3).
type Window struct {
	From, To Bound
}

// Contains reports whether t is inside the window.
func (w Window) Contains(t time.Time) bool {
	lo := t.After(w.From.At) || (w.From.Inclusive && t.Equal(w.From.At))
	hi := t.Before(w.To.At) || (w.To.Inclusive && t.Equal(w.To.At))
	return lo && hi
}

// Empty reports whether no instant is inside the window.
func (w Window) Empty() bool {
	if w.From.At.Before(w.To.At) {
		return false
	}
	return !(w.From.At.Equal(w.To.At) && w.From.Inclusive && w.To.Inclusive)
}

// lower is the latest of the bounds; at one instant an exclusive bound beats an inclusive one (the
// stricter reading wins).
func lower(bs ...Bound) Bound {
	out := bs[0]
	for _, b := range bs[1:] {
		if b.At.After(out.At) || (b.At.Equal(out.At) && !b.Inclusive) {
			out = b
		}
	}
	return out
}

// upper is the earliest of the bounds, an exclusive one beating an inclusive one at one instant.
func upper(bs ...Bound) Bound {
	out := bs[0]
	for _, b := range bs[1:] {
		if b.At.Before(out.At) || (b.At.Equal(out.At) && !b.Inclusive) {
			out = b
		}
	}
	return out
}

// WindowOf is the window of the claim anchored at i (ADR 0032 §C.3; security G-O4, engine M-O1),
// the ends open at the events and clipped at 18 h:
//
//	out claim (anchor an open in A):   (A.At, min(next, Next(A.S), A.At + StaleOpenIn, now))
//	                                    next excluded · Next(A.S) excluded (the boundary instant is
//	                                    the new day's) · A.At + 18 h included (closing is ≤) ·
//	                                    now included
//	in claim (anchor an orphan out X): (max(prev, X.S, X.At − StaleOpenIn), X.At)
//	                                    prev excluded · X.S included (in.At ≥ S) · X.At − 18 h
//	                                    included · X.At excluded
//
// next is the first directed record after the anchor, prev the one before it -- in chain order,
// any channel. Next(A.S) is tap.NextBusinessDayStart of the anchor's FROZEN business day under the
// person's cutoff now; with no resolvable cutoff there is no window (ok=false: no day end is
// invented), and none either when the cutoff moved since the anchor so that its business day now
// ends EARLIER than the day it was recorded in (DayCutShort). ok is also false when the record at i
// admits no claim or the slice cannot tell.
func (c Chain) WindowOf(i int, cutoff *tap.DayCutoff, now time.Time) (Window, bool) {
	kind, ok := c.Missing(i)
	if !ok || kind == "" {
		return Window{}, false
	}
	a := c.events[i]
	switch kind {
	case KindOut:
		if cutoff == nil || c.DayCutShort(i, cutoff) {
			return Window{}, false
		}
		dayEnd, ok := tap.NextBusinessDayStart(*a.DayStart, *cutoff)
		if !ok {
			return Window{}, false
		}
		ends := []Bound{
			{At: dayEnd, Inclusive: false},
			{At: a.At.Add(tap.StaleOpenIn), Inclusive: true},
			{At: now, Inclusive: true},
		}
		if i+1 < len(c.events) {
			ends = append(ends, Bound{At: c.events[i+1].At, Inclusive: false})
		}
		return Window{From: Bound{At: a.At, Inclusive: false}, To: upper(ends...)}, true
	default: // KindIn
		if !c.predecessorKnown(i) {
			return Window{}, false
		}
		starts := []Bound{
			{At: *a.DayStart, Inclusive: true},
			{At: a.At.Add(-tap.StaleOpenIn), Inclusive: true},
		}
		if i > 0 {
			starts = append(starts, Bound{At: c.events[i-1].At, Inclusive: false})
		}
		return Window{From: lower(starts...), To: Bound{At: a.At, Inclusive: false}}, true
	}
}

// DayCutShort reports whether the open check-in at i sits, under the cutoff in force NOW, in a
// business day that ends before the one it was recorded in -- the cutoff moved since the tap (05:00
// to 06:00 and an 05:30 check-in: recorded in Monday's day, today's rule puts it in Sunday's, which
// ended at Monday 06:00). Such an anchor admits no claim, at Offer, Submit and Decide alike (M15-16
// review D-1): the frozen day would draw a window the engine's rule today no longer pairs, so Submit
// would take a claim no approval could pass (Closes reads the engine's rule). ADR 0032 §A.6: while
// a cutoff changes, lean to the conservative side -- the anomaly list keeps the entry and the
// manager can still enter a record.
//
// ⚠️ A CHANGE THIS DOES NOT REFUSE STILL MOVES THE WINDOW (the M15-16 general lens's D-7, measured
// by its sweep; an earlier version of this comment said such a change "does not touch the window",
// which was wrong). WindowOf's daily end is NextBusinessDayStart(the FROZEN S, today's cutoff) --
// neither the old rule's day end nor today's. Two shapes, both counted by the lens: (i) today's
// cutoff falls between the frozen S and the anchor (05:00 → 14:00, a Mon 15:00 check-in) and the
// window is EMPTY -- no card, Submit ErrOutsideWindow (54 123 anchors in the sweep); (ii) the
// window runs past the frozen day's real end (00:00 → 01:00, a Sat 06:10 check-in, the window to
// Sun 00:10; 8 422 cases). Neither inflates an hour or loses a record: (i) asks less, (ii) agrees
// with today's rule, which Closes reads. Drawing the frozen day's end from S's own wall clock is
// backlog T165; the code is unchanged.
//
// WHY NOT ONLY ADD Next(S_now(A.At)) TO THE WINDOW'S ENDS (the other option the review offered): it
// would also make Submit and Decide agree, but it leaves a sliver -- (05:30, 06:00) above -- on the
// card, a question whose honest answer is almost never in it, about a day the rule redrew after the
// fact. Refusing the anchor is one rule in one place (here, read by WindowOf), and it asks less,
// never more.
func (c Chain) DayCutShort(i int, cutoff *tap.DayCutoff) bool {
	a := c.events[i]
	if cutoff == nil || a.DayStart == nil || a.Direction != tap.TypeIn {
		return false
	}
	frozenEnd, ok1 := tap.NextBusinessDayStart(*a.DayStart, *cutoff)
	nowStart, ok2 := tap.BusinessDayStart(a.At, *cutoff)
	if !ok1 || !ok2 {
		return false
	}
	nowEnd, ok3 := tap.NextBusinessDayStart(nowStart, *cutoff)
	return ok3 && nowEnd.Before(frozenEnd)
}

// Closes is the decision's second check (ADR 0032 §C.3 "Onayda yeniden doğrulama"): the manual
// record an approval would write -- the claimed half at `at`, channel manual, no business day --
// is added to the chain and the READER classifies the pair it forms. It must truly close:
//
//	out claim: the new out's predecessor is the anchor itself, and the out closes it -- as the
//	           report reads the stored row (business day NULL: the 18 h bound), AND as the engine
//	           would have read a tap at that instant (business day S(at) under today's cutoff: the
//	           anchor's day must not have ended). The second is what makes a window that ran past
//	           Next(A.S) red here too.
//	in claim:  the anchor's predecessor becomes the new in, and the anchor (with its frozen
//	           business day) closes it.
//
// It does not trust WindowOf: a window arithmetic that let `at` past the next record, past 18 h or
// across the day would give a pair this calls not closing. An instant shared with a record already
// in the chain is refused -- the pair's order would then be the new row's random id's.
func (c Chain) Closes(i int, at time.Time, cutoff *tap.DayCutoff) bool {
	kind, ok := c.Missing(i)
	if !ok || kind == "" {
		return false
	}
	for _, e := range c.events {
		if e.At.Equal(at) {
			return false
		}
	}
	a := c.events[i]
	// the position the new record takes: after every record before `at`
	pos := sort.Search(len(c.events), func(j int) bool { return c.events[j].At.After(at) })
	switch kind {
	case KindOut:
		if pos != i+1 {
			return false // something lies between the anchor and the new out, or it is before the anchor
		}
		prev := &tap.Prev{Direction: tap.TypeIn, At: a.At}
		if tap.ClassifyCheckout(prev, false, at, nil) != tap.CheckoutCloses {
			return false
		}
		if cutoff == nil {
			return false
		}
		s, ok := tap.BusinessDayStart(at, *cutoff)
		return ok && tap.ClassifyCheckout(prev, false, at, &s) == tap.CheckoutCloses
	default: // KindIn
		if pos != i || (i > 0 && !c.events[i-1].At.Before(at)) {
			return false // the new in would not be the anchor's predecessor
		}
		return tap.ClassifyCheckout(&tap.Prev{Direction: tap.TypeIn, At: at}, true, a.At, a.DayStart) == tap.CheckoutCloses
	}
}

// Shown is K4 (ADR 0032 §C.1, "en çok iki kez -- tür başına"): whether the claim card for the record
// at anchor is offered on the result screen of the record at tapAt. Stateless -- read from the
// chain, so no showing is counted anywhere (§4.3):
//
//	out claim (anchor an in A):  (1) T1, the first directed record after A -- the one that left A
//	                             open; (2) the first `out` after T1 within T1's business day.
//	in claim (anchor an out X):  (1) X's own result screen; (2) the first `out` after X within X's
//	                             business day.
//
// "Within the day" is the frozen business day: the tap's S equal to T1's (or X's). A T1 with no
// business day (a manual record) gives no second showing -- nobody saw a screen for it either, and
// an error here leans to asking less. Neither does an `out` that is not the FIRST after T1 (or X):
// a manual `out` in between is that first one, and a manual row has no result screen. Only
// directed records are in the chain, so an `ignored` or `reject` tap is never a showing and never
// counts as one -- it is not there.
//
// Shown does not ask whether the anchor still misses its half, whether a claim was sent, or
// whether a window is left: Offer does.
func (c Chain) Shown(anchor, tapAt int) bool {
	if tapAt < anchor || anchor < 0 || tapAt >= len(c.events) {
		return false
	}
	a, t := c.events[anchor], c.events[tapAt]
	var first int // the record the second showing is counted from
	switch a.Direction {
	case tap.TypeIn:
		if tapAt == anchor {
			return false
		}
		if tapAt == anchor+1 {
			return true
		}
		first = anchor + 1
	case tap.TypeOut:
		if tapAt == anchor {
			return true
		}
		first = anchor
	default:
		return false
	}
	day := c.events[first].DayStart
	if t.Direction != tap.TypeOut || day == nil || t.DayStart == nil || !t.DayStart.Equal(*day) {
		return false
	}
	for j := first + 1; j < tapAt; j++ {
		if c.events[j].Direction == tap.TypeOut {
			return false
		}
	}
	return true
}

// Candidate is the one claim a result screen offers.
type Candidate struct {
	Anchor Event
	Kind   Kind
	Window Window
}

// Offer is the claim the result screen of the record at tapAt asks for, if any (ADR 0032 §C.1):
// among the records up to and including the tap, each one that still misses its half (Missing),
// carries a business day, has no claim yet (claimed), is shown here by K4 (Shown) and leaves a
// window with an instant no older than manual.MaxBackdate -- and of those:
//
//	a check-in claim beats a checkout claim ("İkisi birden mümkünse giriş talebi -- bugünkü, taze
//	olan"); among checkout claims the freshest anchor wins (the race N3: A open since yesterday and
//	B left open today by the person's own "Not right? Clock in" -- B is asked, A stays on the
//	anomaly list).
//
// The tap at tapAt must be a directed record (it is in the chain); a refused or ignored tap is not,
// and has no card.
func (c Chain) Offer(tapAt int, claimed func(uuid.UUID) bool, cutoff *tap.DayCutoff, now time.Time) (Candidate, bool) {
	var best Candidate
	found := false
	for i := 0; i <= tapAt && i < len(c.events); i++ {
		kind, ok := c.Missing(i)
		if !ok || kind == "" || !c.Shown(i, tapAt) || (claimed != nil && claimed(c.events[i].ID)) {
			continue
		}
		w, ok := c.WindowOf(i, cutoff, now)
		if !ok || w.Empty() || now.Sub(w.To.At) > manual.MaxBackdate {
			continue
		}
		cand := Candidate{Anchor: c.events[i], Kind: kind, Window: w}
		switch {
		case !found:
			best, found = cand, true
		case cand.Kind == KindIn && best.Kind == KindOut:
			best = cand
		case cand.Kind == best.Kind:
			best = cand // later in the chain: fresher
		}
	}
	return best, found
}

// Span is one instance of a shift: its start and end instants.
type Span struct {
	Start, End time.Time
}

// SpanOf is the instance of the anchor's resolved shift the anchor belongs to (tap.ShiftSpanAt), or
// false when there is no shift or its zone is unresolvable.
func SpanOf(anchorAt time.Time, shift *tap.Shift) (Span, bool) {
	if shift == nil {
		return Span{}, false
	}
	start, end, ok := tap.ShiftSpanAt(anchorAt, *shift)
	if !ok {
		return Span{}, false
	}
	return Span{Start: start, End: end}, true
}

// Prefill is the moment the claim form offers (ADR 0032 §C.2, K3): the REAL end of the anchor's
// shift for a checkout claim, its real start for a check-in claim -- only when that moment is
// inside the window. Otherwise nil: the field is empty and required. There is NO prefill clipped to
// the window (KF 10–22, 11:00 in, 18:00 "Not right? Clock in": 22:00 is outside (11:00, 18:00), and
// 17:59 would be a one-click ~7 h for a true 4 h).
func Prefill(kind Kind, anchorAt time.Time, shift *tap.Shift, w Window) *time.Time {
	span, ok := SpanOf(anchorAt, shift)
	if !ok {
		return nil
	}
	end := span.End
	if kind == KindIn {
		end = span.Start
	}
	if !w.Contains(end) {
		return nil
	}
	return &end
}

// InBand reports whether t is inside the shift instance's §A.7 band, (start − 1 h, end + 2 h]: the
// lower end open, the upper closed (tenant.CutoffBandBefore and CutoffBandAfter, the one copy of
// the margins).
func InBand(span Span, t time.Time) bool {
	return t.After(span.Start.Add(-tenant.CutoffBandBefore)) && !t.After(span.End.Add(tenant.CutoffBandAfter))
}

// Side is where a moment lies against its shift.
type Side string

const (
	// WithinShift: at or after the start and at or before the end.
	WithinShift Side = "within"
	// BeforeStart: before the shift starts ("4 h before the shift starts").
	BeforeStart Side = "before_start"
	// AfterEnd: after the shift ends.
	AfterEnd Side = "after_end"
	// NoShift: the anchor has no resolved shift.
	NoShift Side = "no_shift"
)

// Deviation is how far a moment lies from its shift instance (ADR 0032 §C.4: "shift 09:00–17:00 ·
// 4 h before the shift starts"). By is zero within the shift and without one.
type Deviation struct {
	Side Side
	By   time.Duration
}

// Interval is the worked interval an approval would make: the claimed half and the anchor.
type Interval struct {
	In, Out time.Time
}

// Duration is Out − In.
func (iv Interval) Duration() time.Duration { return iv.Out.Sub(iv.In) }

// Assessment is what a manager is shown before deciding, computed here so the queue only renders
// it (ADR 0032 §C.4, K3, G-Y1).
type Assessment struct {
	Kind Kind
	// At is the moment assessed: the claimed one, or the manager's Edit.
	At       time.Time
	Interval Interval
	// Shift is the anchor's shift instance; nil without one.
	Shift *Span
	// Deviation is the claimed (or edited) moment's against the shift; AnchorDeviation the
	// anchor's -- the interval's two ends, both shown (M15-16 review, orchestrator's decision).
	Deviation       Deviation
	AnchorDeviation Deviation
	// DefaultUnchanged: the form offered the shift's real end (prefilled) and At is it. Meaningful
	// only while a prefill exists (§C.2: a computation, not a column).
	DefaultUnchanged bool
	// OutsideBand: either END of the interval the approval would make is outside the band -- the
	// claimed (or edited) moment or the anchor -- or there is no shift.
	OutsideBand bool
	// AnchorChosen: the PERSON chose the anchor's direction ("Not right?" -- direction_source
	// employee, read through tap.RecordedDirectionSource; a manual row's stored value is not
	// believed). Set by WithAnchorChosen, never by Assess, which knows no anchor row.
	AnchorChosen bool
	// RequiresConfirmation is OutsideBand OR AnchorChosen: Approve does not write on the first press
	// (ADR 0032 §C.4; the manager's Edit is held to the same rule, ürün N8; the chosen anchor is the
	// M15-19 orchestrator's decision, S5).
	RequiresConfirmation bool
}

// WithAnchorChosen is a with the anchor's chooser folded in (M15-19, S5 -- the orchestrator's
// decision, 2026-10-26): when the person chose the anchor's direction the step is asked ALWAYS, band
// or no band. The measured shape: one tap 1 h 55 m after the shift, "Not right? Clock out" -- an
// orphan out (c) -- and the card's prefill, the shift's start, sent untouched: both ends of the
// 9 h 55 m interval are the person's choices and both sit inside the band, so the band alone let
// one Approve write it. The rule only ever asks MORE often ("onay adımı daha sık çıkar, hiçbir
// zaman daha az"); the confirmation subject (the claim, the moment, the shift instance) is
// unchanged by it.
func (a Assessment) WithAnchorChosen(chosen bool) Assessment {
	a.AnchorChosen = chosen
	a.RequiresConfirmation = a.OutsideBand || chosen
	return a
}

// deviationOf is where t lies against the shift instance.
func deviationOf(span Span, t time.Time) Deviation {
	switch {
	case t.Before(span.Start):
		return Deviation{Side: BeforeStart, By: span.Start.Sub(t)}
	case t.After(span.End):
		return Deviation{Side: AfterEnd, By: t.Sub(span.End)}
	default:
		return Deviation{Side: WithinShift}
	}
}

// Assess is the assessment of an approval at `at` of a claim of kind on the anchor at anchorAt,
// whose form offered prefilled.
//
// THE BAND IS JUDGED AT BOTH ENDS (M15-16 review, G-Y1 widened by the orchestrator, 2026-10-25):
// the interval an approval writes runs from the anchor to the claimed moment, and either end can be
// the one that inflates it -- a 05:30 check-in at a 10:00–22:00 venue claimed out at 23:30 is 18 h
// with the claimed end inside the band. So the step is asked when EITHER end is outside it; the
// rule can only ask more often than the claimed end alone did, never less.
func Assess(kind Kind, anchorAt, at time.Time, shift *tap.Shift, prefilled *time.Time) Assessment {
	a := Assessment{Kind: kind, At: at, Interval: Interval{In: anchorAt, Out: at}}
	if kind == KindIn {
		a.Interval = Interval{In: at, Out: anchorAt}
	}
	a.DefaultUnchanged = prefilled != nil && prefilled.Equal(at)
	span, ok := SpanOf(anchorAt, shift)
	if !ok {
		a.Deviation = Deviation{Side: NoShift}
		a.AnchorDeviation = Deviation{Side: NoShift}
		a.OutsideBand, a.RequiresConfirmation = true, true
		return a
	}
	a.Shift = &span
	a.Deviation = deviationOf(span, at)
	a.AnchorDeviation = deviationOf(span, anchorAt)
	a.OutsideBand = !InBand(span, at) || !InBand(span, anchorAt)
	a.RequiresConfirmation = a.OutsideBand
	return a
}
