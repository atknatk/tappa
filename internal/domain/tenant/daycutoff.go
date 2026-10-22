package tenant

// daycutoff.go -- the business-day cutoff's PANEL side (M15-07, ADR 0032 §A.2, §A.7, §A.8):
// the four levels it is set at, the shift-conflict warning, the suggestion, the count of
// open check-ins a change would split, and the one write per level with its trail row.
//
// THE RULE ITSELF IS NOT HERE. Where a business day starts is tap.BusinessDayStart and
// tap.NextBusinessDayStart (M15-03); whether the rule applies at all is the switch
// (tap.SwitchedDayCutoff, read through Directory); the tap resolves a person's cutoff in
// one SQL read (GetEmployeeForTap's effective_day_cutoff). This file only asks those
// functions questions: "where would THIS check-in's day end under THAT cutoff".
//
// WHAT THE WARNING IS ABOUT (§A.7). A cutoff inside a shift splits it: the clock-in before
// the cutoff belongs to one business day, the clock-out after it to the next, and the next
// tap is recorded as `in`. The band a cutoff must stay out of is (start − 1 h, end + 2 h]
// -- an early arrival and a late departure are still the shift. The lower end is OPEN
// because the boundary instant belongs to the new day (a 05:00 cutoff and a 06:00 start:
// the earliest expected arrival, 05:00, already starts the new day); the upper end is
// CLOSED for the same reason (a cutoff exactly at end + 2 h makes the latest expected
// departure the new day's first tap). An overnight shift's band crosses midnight; it is
// measured on the 24-hour circle, never by comparing two clock values.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

// The named margins of ADR 0032 §A.7 ("adlandırılmış sabitler", tuned in the pilot).
const (
	// CutoffBandBefore is how early before its start a shift's first clock-in is expected.
	CutoffBandBefore = time.Hour
	// CutoffBandAfter is how late after its end a shift's last clock-out is expected.
	CutoffBandAfter = 2 * time.Hour
	// CutoffTouchedWindow is how far back the venues a person tapped at count as the
	// venues they work at (§A.7 (2)): their shifts are checked as well as the profile's.
	CutoffTouchedWindow = 28 * 24 * time.Hour
	// CutoffSuggestionStep is the grid a suggested cutoff is rounded to.
	CutoffSuggestionStep = 15 * time.Minute
)

const wallDay = 24 * time.Hour

// CutoffLevel is one of the four places a cutoff is set (K1): the most specific wins.
type CutoffLevel string

const (
	CutoffBusiness   CutoffLevel = "business"
	CutoffBranch     CutoffLevel = "branch"
	CutoffDepartment CutoffLevel = "department"
	CutoffPerson     CutoffLevel = "person"
)

// ParseCutoffLevel reads a posted level from the closed list.
func ParseCutoffLevel(s string) (CutoffLevel, bool) {
	switch l := CutoffLevel(s); l {
	case CutoffBusiness, CutoffBranch, CutoffDepartment, CutoffPerson:
		return l, true
	}
	return "", false
}

// The trail's actions, one per level, each in its subject's own prefix so `action LIKE
// 'employee.%'` stays the whole of what was done to a person (staff.go's rule) and the
// tenant's, venue's and department's prefixes keep theirs.
const (
	ActionTenantDayCutoffUpdated     = "tenant.day_cutoff_updated"
	ActionLocationDayCutoffUpdated   = "location.day_cutoff_updated"
	ActionDepartmentDayCutoffUpdated = "department.day_cutoff_updated"
	ActionEmployeeDayCutoffUpdated   = "employee.day_cutoff_updated"
)

var (
	// ErrDayCutoff: not a wall-clock time this product stores (00064's CHECK: from 00:00
	// to 23:59, whole minutes).
	ErrDayCutoff = errors.New("tenant: a business day cutoff is a time of day in whole minutes")
	// ErrDayCutoffRequired: the business level never inherits -- it is the end of every
	// coalesce (tenants.day_cutoff NOT NULL).
	ErrDayCutoffRequired = errors.New("tenant: the business level needs a cutoff")
	// ErrSameDayCutoff: the level already holds that value; nothing was written.
	ErrSameDayCutoff = errors.New("tenant: that is already the cutoff on file")
	// ErrDayCutoffStale: the level was changed after the change was reviewed (its version
	// moved); nothing was written.
	ErrDayCutoffStale = errors.New("tenant: the cutoff changed after it was reviewed")
	// ErrDayCutoffUnknownLevel: not one of the four levels.
	ErrDayCutoffUnknownLevel = errors.New("tenant: no such business day level")
	// ErrDayCutoffTooLarge: the business is past the screen's design ceiling, so a review
	// would be a claim about part of it. Said, never silently cut (§4.6).
	ErrDayCutoffTooLarge = errors.New("tenant: too many rows to review the business day")
)

// ValidDayCutoff reports whether d is a cutoff 00064's CHECK accepts.
func ValidDayCutoff(d time.Duration) bool {
	return d >= 0 && d < wallDay && d%time.Minute == 0
}

// --- the pure half: the band, the suggestion, the split ----------------------------------

// CutoffShift is a shift a cutoff is checked against, named by whose shift it is (a
// department's or a venue's -- never a person's: a shift is a place's, ADR 0032 §A.7).
type CutoffShift struct {
	Owner     string
	Start     time.Duration
	End       time.Duration
	Overnight bool
}

// span is the shift's length. An overnight shift ends on the next day; a stored end at or
// before the start can only mean the same, so it is read that way rather than as a
// negative length -- the "compare two clocks flat" trap of the card.
func (s CutoffShift) span() time.Duration {
	d := s.End - s.Start
	if s.Overnight || d <= 0 {
		d += wallDay
	}
	return d
}

// bandLength is the length of (start − 1 h, end + 2 h].
func (s CutoffShift) bandLength() time.Duration {
	return s.span() + CutoffBandBefore + CutoffBandAfter
}

// circular is d on the 24-hour circle, in [0, 24 h).
func circular(d time.Duration) time.Duration {
	d %= wallDay
	if d < 0 {
		d += wallDay
	}
	return d
}

// CutoffInBand reports whether a cutoff falls inside s's band (start − 1 h, end + 2 h]:
// lower end excluded, upper end included, measured on the circle so an overnight band
// crosses midnight. A band of 24 hours or more (a shift of 21 hours or more) holds every
// cutoff.
func CutoffInBand(cutoff time.Duration, s CutoffShift) bool {
	length := s.bandLength()
	if length >= wallDay {
		return true
	}
	off := circular(cutoff - (s.Start - CutoffBandBefore))
	return off > 0 && off <= length
}

// CutoffConflicts are the shifts whose band holds the cutoff, in the given order.
func CutoffConflicts(cutoff time.Duration, shifts []CutoffShift) []CutoffShift {
	var out []CutoffShift
	for _, s := range shifts {
		if CutoffInBand(cutoff, s) {
			out = append(out, s)
		}
	}
	return out
}

// inAnyBand is len(CutoffConflicts(cutoff, shifts)) > 0 without building the list:
// SuggestCutoff asks it for each of the day's 1440 minutes.
func inAnyBand(cutoff time.Duration, shifts []CutoffShift) bool {
	for _, s := range shifts {
		if CutoffInBand(cutoff, s) {
			return true
		}
	}
	return false
}

// SuggestCutoff is the middle of the widest stretch of the day that no band covers,
// rounded to CutoffSuggestionStep -- and, when that rounding lands inside a band, the
// nearest step that does not (ADR 0032 §A.7, product lens Ü-O7). A stretch with no step in
// it at all is passed over for the next widest, so a narrow widest gap does not hide a step
// that a shorter one holds. ok is false when no free stretch holds a step (or there is no
// shift to keep clear of): the screen then says there is no cutoff outside every shift.
//
// The day is walked minute by minute because a cutoff is stored in whole minutes (00064):
// "outside every band" is then exact for every value a cutoff can take.
func SuggestCutoff(shifts []CutoffShift) (time.Duration, bool) {
	if len(shifts) == 0 {
		return 0, false
	}
	const minutes = int(wallDay / time.Minute)
	free := make([]bool, minutes)
	busy := -1
	for m := range free {
		c := time.Duration(m) * time.Minute
		free[m] = !inAnyBand(c, shifts)
		if !free[m] && busy < 0 {
			busy = m
		}
	}
	if busy < 0 {
		// Unreachable with a shift (a band always covers something); said rather than assumed.
		return 0, false
	}
	// The free runs on the circle, walked from a covered minute so none is cut at midnight.
	type run struct{ start, length int }
	var runs []run
	cur := run{length: 0}
	for i := busy + 1; i <= busy+minutes; i++ {
		if free[i%minutes] {
			if cur.length == 0 {
				cur.start = i
			}
			cur.length++
			continue
		}
		if cur.length > 0 {
			runs = append(runs, cur)
			cur = run{}
		}
	}
	sort.SliceStable(runs, func(a, b int) bool { return runs[a].length > runs[b].length })
	step := int(CutoffSuggestionStep / time.Minute)
	for _, r := range runs {
		mid := r.start + (r.length-1)/2
		// The step nearest the middle that lies in the run.
		best, bestDist := -1, 0
		for k := r.start; k < r.start+r.length; k++ {
			if k%step != 0 {
				continue
			}
			dist := k - mid
			if dist < 0 {
				dist = -dist
			}
			if best < 0 || dist < bestDist {
				best, bestDist = k, dist
			}
		}
		if best >= 0 {
			return time.Duration(best%minutes) * time.Minute, true
		}
	}
	return 0, false
}

// SplitsOpenCheckIn reports whether a check-in made at `in`, still open at `now`, would be
// split by moving its person's cutoff from oldCutoff to newCutoff (ADR 0032 §A.7): B_old =
// Next_old(S_old(in)), B_new = Next_new(S_new(in)); it splits ⇔ B_old > now ∧ B_new < B_old
// ∧ B_new ≤ in + StaleOpenIn.
//
//   - B_old > now: an entry whose business day has ALREADY ended under the old cutoff is left
//     open by the old rule now; changing the cutoff changes nothing for it (review ORTA-1 (a):
//     Rusty Bar's forgotten 18:00 check-in, looked at the next morning).
//   - The last bound is ≤ because closing is ≤ too: a checkout exactly StaleOpenIn after the
//     check-in closes it today, and if B_new is exactly then, the new rule puts that tap in
//     the next day (engine lens N2).
//
// Which entries are asked about at all -- only the one the engine could close, and only
// within StaleOpenIn of now -- is the caller's (splitCount). A cutoff that cannot be
// resolved splits nothing: the engine does not invent a boundary, and neither does this.
func SplitsOpenCheckIn(in, now time.Time, oldCutoff, newCutoff tap.DayCutoff) bool {
	sOld, ok := tap.BusinessDayStart(in, oldCutoff)
	if !ok {
		return false
	}
	bOld, ok := tap.NextBusinessDayStart(sOld, oldCutoff)
	if !ok || !bOld.After(now) {
		return false
	}
	sNew, ok := tap.BusinessDayStart(in, newCutoff)
	if !ok {
		return false
	}
	bNew, ok := tap.NextBusinessDayStart(sNew, newCutoff)
	if !ok {
		return false
	}
	return bNew.Before(bOld) && !bNew.After(in.Add(tap.StaleOpenIn))
}

// --- the database half --------------------------------------------------------------

// dayCutoffRowLimit is the design ceiling of the review's reads: far above any business in
// the two markets this ships to (the largest seed tenant has 9 venues, 5 departments and
// 25 people), and reaching it is ErrDayCutoffTooLarge rather than a partial answer.
const dayCutoffRowLimit = 20000

// openCheckInLimit bounds the open check-ins counted; past it the count says "N+".
const openCheckInLimit = 5000

// DayCutoffs is the panel's business-day service: the reads behind the four settings
// cards, the review a change goes through, and the one write per level.
type DayCutoffs struct {
	data  Database
	trail Trail
	// dir carries the switch (TAPPA_BUSINESS_DAY_FROM): the status line asks the same
	// gate the tap asks (Directory.DayCutoff -> tap.SwitchedDayCutoff), so the screen
	// cannot call the rule active while the engine runs without it.
	dir *Directory
	log *slog.Logger
	// beforeWrite, when set, runs inside Set's transaction after the first version check and
	// right before the UPDATE that locks the row -- a test's meeting point, so two
	// confirmations of one version reach the lock together (Staff.beforeBudget's pattern).
	// nil in production.
	beforeWrite func(ctx context.Context)
}

// NewDayCutoffs wires it. Every dependency is required: without the trail a change would
// leave no row, and without the directory the status line would have no switch to read.
func NewDayCutoffs(data Database, trail Trail, dir *Directory, log *slog.Logger) (*DayCutoffs, error) {
	switch {
	case data == nil:
		return nil, errors.New("tenant: nil database")
	case trail == nil:
		return nil, errors.New("tenant: nil audit trail")
	case dir == nil:
		return nil, errors.New("tenant: nil directory")
	}
	if log == nil {
		log = slog.Default()
	}
	return &DayCutoffs{data: data, trail: trail, dir: dir, log: log}, nil
}

// BusinessDayFrom is the switch's value (nil when unset), a copy.
func (d *Directory) BusinessDayFrom() *time.Time {
	if d.businessDayFrom == nil {
		return nil
	}
	v := *d.businessDayFrom
	return &v
}

// BusinessDaySwitch is the status line's two facts (ADR 0032 §A.8): whether the rule
// applies now and, when it does, since when.
type BusinessDaySwitch struct {
	Active bool
	// Since is the switch's value; zero while the rule is not active.
	Since time.Time
}

// Switch answers the status line at the SERVER's clock (security lens F-2) through the
// directory's one gate.
func (s *DayCutoffs) Switch(now time.Time) BusinessDaySwitch {
	if s.dir.DayCutoff(now, 0, "UTC") == nil {
		return BusinessDaySwitch{}
	}
	from := s.dir.BusinessDayFrom()
	if from == nil {
		return BusinessDaySwitch{}
	}
	return BusinessDaySwitch{Active: true, Since: *from}
}

// CutoffSource says where a person's cutoff comes from: the level and, below the business,
// the place's name.
type CutoffSource struct {
	Level CutoffLevel
	// Name is the department's or the venue's name; "" for the person and business levels.
	Name string
}

// CutoffConflict is one shift a cutoff falls inside, and that cutoff.
type CutoffConflict struct {
	Shift  CutoffShift
	Cutoff time.Duration
}

// CutoffReview is what a change at one level would do, read now.
type CutoffReview struct {
	Level       CutoffLevel
	SubjectID   uuid.UUID
	SubjectName string
	// Zone is the business's timezone -- the only zone the code has (§A.2).
	Zone string
	// Business is the business level's value: what a branch inherits when emptied.
	Business time.Duration
	// Stored is this level's value now; nil = it inherits. Never nil for the business.
	Stored *time.Duration
	// Proposed is the value asked about; nil = inherit.
	Proposed *time.Duration
	// People is how many people take their cutoff from this level.
	People int
	// Conflicts are the shifts the PROPOSED value cuts, for the people who would use it
	// and the level's own shift.
	Conflicts []CutoffConflict
	// Suggested is a cutoff outside every band of those shifts, when there is one.
	Suggested *time.Duration
	// Shifts is how many distinct shifts were checked.
	Shifts int
	// OpenCounted says the open check-ins were counted. They are counted only for a CHANGE
	// (not Unchanged) and only while the switch is on: with it off no cutoff applies, so a
	// change splits nothing now (orchestrator's decision, review ORTA-1 (c)) -- the screen
	// then says nothing about open check-ins rather than "would also split 1" under "Not
	// active yet".
	OpenCounted bool
	// Version is the value on file and how many times it was changed (CountDayCutoffChanges):
	// what a confirmation is bound to beside the new value (review DÜŞÜK-1).
	Version string
	// OpenSplit is how many check-ins open right now the change would split; OpenCapped
	// says the count stopped at the ceiling.
	OpenSplit  int
	OpenCapped bool
}

// Changes reports whether the proposed value differs from the stored one.
func (r CutoffReview) Changes() bool { return !sameCutoff(r.Stored, r.Proposed) }

// PersonCutoff is the person card's line: the value the tap would use and where it is from.
type PersonCutoff struct {
	Value  time.Duration
	Source CutoffSource
	// Own is the person-level value, nil when they inherit.
	Own  *time.Duration
	Zone string
}

// MoveCutoff is what a move would do to the person's cutoff (§A.7 "Taşınma").
type MoveCutoff struct {
	From, To             time.Duration
	FromSource, ToSource CutoffSource
	Conflicts            []CutoffConflict
	Suggested            *time.Duration
	Zone                 string
}

// Changes reports whether the move changes the person's effective cutoff.
func (m MoveCutoff) Changes() bool { return m.From != m.To }

// CutoffReviewCommand asks what setting Value at (Level, SubjectID) would do. SubjectID is
// ignored for the business level (the tenant is the subject). Unchanged asks about the value
// already stored instead -- what the current setting cuts, for the settings cards -- and
// Value is then ignored.
type CutoffReviewCommand struct {
	TenantID  uuid.UUID
	Level     CutoffLevel
	SubjectID uuid.UUID
	Value     *time.Duration
	Unchanged bool
	Now       time.Time
}

// SetCutoffCommand writes Value at (Level, SubjectID). ExpectedVersion is the Version the
// change was reviewed against (CutoffReview.Version); Set re-checks it in its own
// transaction and writes nothing when it no longer holds (ErrDayCutoffStale).
type SetCutoffCommand struct {
	TenantID        uuid.UUID
	ActorID         uuid.UUID
	Level           CutoffLevel
	SubjectID       uuid.UUID
	Value           *time.Duration
	ExpectedVersion string
}

// CutoffChange is what was written: the value before and after (nil = inherit).
type CutoffChange struct {
	From, To *time.Duration
}

// MoveCutoffCommand asks what moving one person to (LocationID, DepartmentID) would do.
type MoveCutoffCommand struct {
	TenantID     uuid.UUID
	EmployeeID   uuid.UUID
	LocationID   uuid.UUID
	DepartmentID *uuid.UUID
	Now          time.Time
}

// cutoffPlace is a venue or a department as the review reads it.
type cutoffPlace struct {
	id     uuid.UUID
	venue  uuid.UUID // a department's venue; the venue's own id for a venue
	name   string
	cutoff *time.Duration
	shift  *CutoffShift
}

type cutoffPerson struct {
	id         uuid.UUID
	branch     *uuid.UUID
	department *uuid.UUID
	own        *time.Duration
}

// cutoffWorld is one tenant's snapshot, read in one transaction.
type cutoffWorld struct {
	name        string
	zone        string
	business    time.Duration
	venues      map[uuid.UUID]cutoffPlace
	departments map[uuid.UUID]cutoffPlace
	people      []cutoffPerson
}

// override replaces one level's stored value while resolving.
type override struct {
	level   CutoffLevel
	subject uuid.UUID
	value   *time.Duration
}

func durationOf(t pgtype.Time) *time.Duration {
	if !t.Valid {
		return nil
	}
	d := time.Duration(t.Microseconds) * time.Microsecond
	return &d
}

// pgTime renders a cutoff for a setter: nil is SQL NULL (inherit), never 00:00 -- the
// "empty = 00:00" trap of the card.
func pgTime(d *time.Duration) pgtype.Time {
	if d == nil {
		return pgtype.Time{}
	}
	return pgtype.Time{Microseconds: int64(*d / time.Microsecond), Valid: true}
}

func cutoffShiftOf(owner string, start, end pgtype.Time, overnight bool) *CutoffShift {
	if !start.Valid || !end.Valid {
		return nil
	}
	return &CutoffShift{
		Owner:     owner,
		Start:     time.Duration(start.Microseconds) * time.Microsecond,
		End:       time.Duration(end.Microseconds) * time.Microsecond,
		Overnight: overnight,
	}
}

func sameCutoff(a, b *time.Duration) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// load reads the snapshot. people narrows the people read (a level's filter).
func load(ctx context.Context, q *store.Queries, tenantID uuid.UUID, people store.ListDayCutoffPeopleParams) (cutoffWorld, error) {
	b, err := q.GetDayCutoffBusiness(ctx, tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cutoffWorld{}, ErrUnknownTenantAccount
		}
		return cutoffWorld{}, fmt.Errorf("read the business cutoff: %w", err)
	}
	w := cutoffWorld{name: b.Name, zone: b.Timezone, venues: map[uuid.UUID]cutoffPlace{}, departments: map[uuid.UUID]cutoffPlace{}}
	if v := durationOf(b.DayCutoff); v != nil {
		w.business = *v
	}
	venues, err := q.ListDayCutoffVenues(ctx, store.ListDayCutoffVenuesParams{TenantID: tenantID, RowLimit: dayCutoffRowLimit})
	if err != nil {
		return cutoffWorld{}, fmt.Errorf("read the venues' cutoffs: %w", err)
	}
	departments, err := q.ListDayCutoffDepartments(ctx, store.ListDayCutoffDepartmentsParams{TenantID: tenantID, RowLimit: dayCutoffRowLimit})
	if err != nil {
		return cutoffWorld{}, fmt.Errorf("read the departments' cutoffs: %w", err)
	}
	people.TenantID, people.RowLimit = tenantID, dayCutoffRowLimit
	staff, err := q.ListDayCutoffPeople(ctx, people)
	if err != nil {
		return cutoffWorld{}, fmt.Errorf("read the people's cutoffs: %w", err)
	}
	if len(venues) >= dayCutoffRowLimit || len(departments) >= dayCutoffRowLimit || len(staff) >= dayCutoffRowLimit {
		return cutoffWorld{}, ErrDayCutoffTooLarge
	}
	for _, v := range venues {
		w.venues[v.ID] = cutoffPlace{id: v.ID, venue: v.ID, name: v.Name, cutoff: durationOf(v.DayCutoff),
			shift: cutoffShiftOf(v.Name, v.ShiftStart, v.ShiftEnd, v.Overnight)}
	}
	for _, d := range departments {
		w.departments[d.ID] = cutoffPlace{id: d.ID, venue: d.LocationID, name: d.Name, cutoff: durationOf(d.DayCutoff),
			shift: cutoffShiftOf(d.Name, d.ShiftStart, d.ShiftEnd, d.Overnight)}
	}
	for _, p := range staff {
		w.people = append(w.people, cutoffPerson{id: p.ID, branch: p.LocationID, department: p.DepartmentID, own: durationOf(p.DayCutoff)})
	}
	return w, nil
}

// stored is a level's own value under an override.
func (o *override) at(level CutoffLevel, subject uuid.UUID, stored *time.Duration) *time.Duration {
	if o != nil && o.level == level && (level == CutoffBusiness || o.subject == subject) {
		return o.value
	}
	return stored
}

// effective is GetEmployeeForTap's coalesce(person, department, PROFILE branch, business)
// with one level replaced -- the same order, evaluated in Go so "what would they get if"
// can be asked (the file header of db/queries/daycutoff.sql).
func (w cutoffWorld) effective(p cutoffPerson, o *override) (time.Duration, CutoffSource) {
	if v := o.at(CutoffPerson, p.id, p.own); v != nil {
		return *v, CutoffSource{Level: CutoffPerson}
	}
	if p.department != nil {
		if d, ok := w.departments[*p.department]; ok {
			if v := o.at(CutoffDepartment, d.id, d.cutoff); v != nil {
				return *v, CutoffSource{Level: CutoffDepartment, Name: d.name}
			}
		}
	}
	if p.branch != nil {
		if l, ok := w.venues[*p.branch]; ok {
			if v := o.at(CutoffBranch, l.id, l.cutoff); v != nil {
				return *v, CutoffSource{Level: CutoffBranch, Name: l.name}
			}
		}
	}
	business := w.business
	if v := o.at(CutoffBusiness, uuid.Nil, &business); v != nil {
		business = *v
	}
	return business, CutoffSource{Level: CutoffBusiness}
}

// inherits reports whether p takes its cutoff from (level, subject) -- every more specific
// level of theirs is empty. The level's own value does not matter: this is who it REACHES.
func (w cutoffWorld) inherits(p cutoffPerson, level CutoffLevel, subject uuid.UUID) bool {
	deptEmpty := true
	if p.department != nil {
		if d, ok := w.departments[*p.department]; ok && d.cutoff != nil {
			deptEmpty = false
		}
	}
	branchEmpty := true
	if p.branch != nil {
		if l, ok := w.venues[*p.branch]; ok && l.cutoff != nil {
			branchEmpty = false
		}
	}
	switch level {
	case CutoffPerson:
		return p.id == subject
	case CutoffDepartment:
		return p.own == nil && p.department != nil && *p.department == subject
	case CutoffBranch:
		return p.own == nil && deptEmpty && p.branch != nil && *p.branch == subject
	case CutoffBusiness:
		return p.own == nil && deptEmpty && branchEmpty
	}
	return false
}

// resolvedShift is the person's shift (§5): their department's when it has one, else their
// PROFILE branch's -- never the tapped venue's (the card's first trap; the touched venues
// are added separately, under their own names).
func (w cutoffWorld) resolvedShift(p cutoffPerson) *CutoffShift {
	if p.department != nil {
		if d, ok := w.departments[*p.department]; ok && d.shift != nil {
			return d.shift
		}
	}
	if p.branch != nil {
		if l, ok := w.venues[*p.branch]; ok && l.shift != nil {
			return l.shift
		}
	}
	return nil
}

// touched reads the venues each person tapped at within CutoffTouchedWindow of now.
func touched(ctx context.Context, q *store.Queries, tenantID uuid.UUID, ids []uuid.UUID, now time.Time) (map[uuid.UUID][]uuid.UUID, error) {
	out := map[uuid.UUID][]uuid.UUID{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.ListDayCutoffTouchedVenues(ctx, store.ListDayCutoffTouchedVenuesParams{
		TenantID: tenantID, EmployeeIds: ids, Since: now.Add(-CutoffTouchedWindow),
	})
	if err != nil {
		return nil, fmt.Errorf("read the venues tapped at: %w", err)
	}
	for _, r := range rows {
		if r.EmployeeID == nil || r.LocationID == nil {
			continue
		}
		out[*r.EmployeeID] = append(out[*r.EmployeeID], *r.LocationID)
	}
	return out, nil
}

// shiftSet collects distinct shifts in first-seen order.
type shiftSet struct {
	seen map[CutoffShift]bool
	list []CutoffShift
}

func (s *shiftSet) add(sh *CutoffShift) {
	if sh == nil {
		return
	}
	if s.seen == nil {
		s.seen = map[CutoffShift]bool{}
	}
	if !s.seen[*sh] {
		s.seen[*sh] = true
		s.list = append(s.list, *sh)
	}
}

// conflictSet collects distinct (shift, cutoff) pairs.
type conflictSet struct {
	seen map[CutoffConflict]bool
	list []CutoffConflict
}

func (c *conflictSet) check(cutoff time.Duration, sh *CutoffShift) {
	if sh == nil || !CutoffInBand(cutoff, *sh) {
		return
	}
	k := CutoffConflict{Shift: *sh, Cutoff: cutoff}
	if c.seen == nil {
		c.seen = map[CutoffConflict]bool{}
	}
	if !c.seen[k] {
		c.seen[k] = true
		c.list = append(c.list, k)
	}
}

func (c *conflictSet) sorted() []CutoffConflict {
	out := append([]CutoffConflict(nil), c.list...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Shift.Owner != out[j].Shift.Owner {
			return out[i].Shift.Owner < out[j].Shift.Owner
		}
		return out[i].Shift.Start < out[j].Shift.Start
	})
	return out
}

// peopleFilter narrows the people read to the ones a level can reach.
func peopleFilter(level CutoffLevel, subject uuid.UUID) store.ListDayCutoffPeopleParams {
	var p store.ListDayCutoffPeopleParams
	switch level {
	case CutoffBranch:
		p.LocationID = &subject
	case CutoffDepartment:
		p.DepartmentID = &subject
	case CutoffPerson:
		p.EmployeeID = &subject
	}
	return p
}

// subject resolves (level, id) to its name and stored value, or the level's unknown error.
func (w cutoffWorld) subject(level CutoffLevel, id uuid.UUID) (string, *time.Duration, error) {
	switch level {
	case CutoffBusiness:
		b := w.business
		return w.name, &b, nil
	case CutoffBranch:
		if l, ok := w.venues[id]; ok {
			return l.name, l.cutoff, nil
		}
		return "", nil, ErrUnknownVenue
	case CutoffDepartment:
		if d, ok := w.departments[id]; ok {
			return d.name, d.cutoff, nil
		}
		return "", nil, ErrUnknownDepartment
	case CutoffPerson:
		for _, p := range w.people {
			if p.id == id {
				return "", p.own, nil
			}
		}
		return "", nil, ErrUnknownEmployee
	}
	return "", nil, ErrDayCutoffUnknownLevel
}

// ownShifts are the level's own shifts for a place NOBODY IS PLACED IN yet -- the card's
// "first shift entered" case: a new department with a 04:00 start and the business's 05:00
// is warned on its own form before anybody is in it. With nobody there, the cutoff its first
// people would take is estimated from the place's chain: coalesce(venue, business) for a
// venue, coalesce(department, the department's venue, business) for a department.
//
// 🔴 ONLY FOR A PLACE WITH NO MEMBERS (review ORTA-2). Once people are placed, the place's
// shift reaches the review as THEIR resolved shift, checked against THEIR effective cutoff
// (Review's heirs loop). The estimate is wrong for a member: a department's venue need not
// be the member's profile branch (ADR 0032 §A.2) -- measured: Bakery at Plant (06:00), shift
// 05-13, its one member's profile Shop, business 23:00: the member's cutoff is 23:00, and the
// estimate said "06:00 falls inside Bakery's shift". A member who does not inherit the level
// (their own value, a department's) is not affected by it at all.
func (w cutoffWorld) ownShifts(level CutoffLevel, subject uuid.UUID, o *override, m membership) []CutoffConflict {
	business := w.business
	if v := o.at(CutoffBusiness, uuid.Nil, &business); v != nil {
		business = *v
	}
	venueCutoff := func(id uuid.UUID) time.Duration {
		l, ok := w.venues[id]
		if !ok {
			return business
		}
		if v := o.at(CutoffBranch, l.id, l.cutoff); v != nil {
			return *v
		}
		return business
	}
	deptCutoff := func(d cutoffPlace) time.Duration {
		if v := o.at(CutoffDepartment, d.id, d.cutoff); v != nil {
			return *v
		}
		return venueCutoff(d.venue)
	}
	venueMembers, deptMembers := m.venues, m.departments
	var out []CutoffConflict
	switch level {
	case CutoffBranch:
		if l, ok := w.venues[subject]; ok && l.shift != nil && !venueMembers[l.id] {
			out = append(out, CutoffConflict{Shift: *l.shift, Cutoff: venueCutoff(l.id)})
		}
	case CutoffDepartment:
		if d, ok := w.departments[subject]; ok && d.shift != nil && !deptMembers[d.id] {
			out = append(out, CutoffConflict{Shift: *d.shift, Cutoff: deptCutoff(d)})
		}
	case CutoffBusiness:
		// The business level reaches an empty place only through its chain: a venue with no
		// value of its own, a department with none whose venue has none either.
		for _, l := range w.venues {
			if l.cutoff == nil && l.shift != nil && !venueMembers[l.id] {
				out = append(out, CutoffConflict{Shift: *l.shift, Cutoff: business})
			}
		}
		for _, d := range w.departments {
			if d.cutoff == nil && d.shift != nil && !deptMembers[d.id] {
				if l, ok := w.venues[d.venue]; !ok || l.cutoff == nil {
					out = append(out, CutoffConflict{Shift: *d.shift, Cutoff: business})
				}
			}
		}
	}
	return out
}

// heirsOf is the people who take their cutoff from (level, subject), and their ids.
func (w cutoffWorld) heirsOf(level CutoffLevel, subject uuid.UUID) ([]cutoffPerson, []uuid.UUID) {
	var heirs []cutoffPerson
	var ids []uuid.UUID
	for _, p := range w.people {
		if w.inherits(p, level, subject) {
			heirs = append(heirs, p)
			ids = append(ids, p.id)
		}
	}
	return heirs, ids
}

// cuts is a review's heart, shared by Review (one level, one proposed value) and Check (every
// level, the value on file -- o nil): the shifts checked for the people who take their cutoff
// from (level, subject) -- each heir's resolved shift and the venues they tapped at (tapped) --
// plus the level's own shift for a place nobody is placed in yet (ownShifts), the ones the
// value under o cuts, and each heir's cutoff before and after o.
func (w cutoffWorld) cuts(level CutoffLevel, subject uuid.UUID, o *override, heirs []cutoffPerson,
	tapped map[uuid.UUID][]uuid.UUID, m membership) (shifts shiftSet, conflicts conflictSet, before, next map[uuid.UUID]time.Duration) {
	for _, own := range w.ownShifts(level, subject, o, m) {
		sh := own.Shift
		shifts.add(&sh)
		conflicts.check(own.Cutoff, &sh)
	}
	next = map[uuid.UUID]time.Duration{}
	before = map[uuid.UUID]time.Duration{}
	for _, p := range heirs {
		now, _ := w.effective(p, o)
		was, _ := w.effective(p, nil)
		next[p.id], before[p.id] = now, was
		sh := w.resolvedShift(p)
		shifts.add(sh)
		conflicts.check(now, sh)
		for _, v := range tapped[p.id] {
			if l, ok := w.venues[v]; ok {
				shifts.add(l.shift)
				conflicts.check(now, l.shift)
			}
		}
	}
	return shifts, conflicts, before, next
}

// membership is where people are placed: the venues that are someone's profile branch and the
// departments that have someone in them. It is built ONCE per snapshot (membershipOf) and
// handed to ownShifts, which asks it for every level -- rebuilt per level, as it first was, it
// made the business day check quadratic in the people (M15-08 security lens, DÜŞÜK).
type membership struct {
	venues, departments map[uuid.UUID]bool
}

func (w cutoffWorld) membershipOf() membership {
	m := membership{venues: map[uuid.UUID]bool{}, departments: map[uuid.UUID]bool{}}
	for _, p := range w.people {
		if p.branch != nil {
			m.venues[*p.branch] = true
		}
		if p.department != nil {
			m.departments[*p.department] = true
		}
	}
	return m
}

func (c CutoffReviewCommand) validate() error {
	switch {
	case c.TenantID == uuid.Nil:
		return errors.New("tenant: tenant id is required")
	case c.Unchanged:
	case c.Value != nil && !ValidDayCutoff(*c.Value):
		return ErrDayCutoff
	case c.Level == CutoffBusiness && c.Value == nil:
		return ErrDayCutoffRequired
	}
	if _, ok := ParseCutoffLevel(string(c.Level)); !ok {
		return ErrDayCutoffUnknownLevel
	}
	return nil
}

// Review is what setting the value would do, read now in one transaction: the people who
// take their cutoff from the level, the shifts the proposed value cuts (their resolved
// shifts, the venues they tapped at in the last CutoffTouchedWindow, and the level's own
// shift), a suggestion outside every band, and how many check-ins open right now it would
// split. It writes nothing.
func (s *DayCutoffs) Review(ctx context.Context, c CutoffReviewCommand) (CutoffReview, error) {
	if err := c.validate(); err != nil {
		return CutoffReview{}, err
	}
	if c.Level == CutoffBusiness {
		c.SubjectID = c.TenantID
	}
	var out CutoffReview
	err := s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		w, err := load(ctx, q, c.TenantID, peopleFilter(c.Level, c.SubjectID))
		if err != nil {
			return err
		}
		name, stored, err := w.subject(c.Level, c.SubjectID)
		if err != nil {
			return err
		}
		if c.Unchanged {
			c.Value = stored
		}
		o := &override{level: c.Level, subject: c.SubjectID, value: c.Value}
		out = CutoffReview{Level: c.Level, SubjectID: c.SubjectID, SubjectName: name, Zone: w.zone,
			Business: w.business, Stored: stored, Proposed: c.Value}
		if out.Version, err = version(ctx, q, c.TenantID, c.Level, c.SubjectID, stored); err != nil {
			return err
		}

		heirs, ids := w.heirsOf(c.Level, c.SubjectID)
		out.People = len(heirs)
		tapped, err := touched(ctx, q, c.TenantID, ids, c.Now)
		if err != nil {
			return err
		}
		shifts, conflicts, before, next := w.cuts(c.Level, c.SubjectID, o, heirs, tapped, w.membershipOf())
		out.Conflicts = conflicts.sorted()
		out.Shifts = len(shifts.list)
		if v, ok := SuggestCutoff(shifts.list); ok {
			out.Suggested = &v
		}
		// An Unchanged review (the settings cards, the save of a venue or a department) changes
		// nothing, so it splits nothing: ListOpenCheckIns and the per-person reads are not run
		// on a page view (review DÜŞÜK-2). The read count of that path is load + the touched
		// venues + the version -- six statements, none over transactions but the touched one.
		if c.Unchanged || !s.Switch(c.Now).Active {
			return nil
		}
		out.OpenCounted = true
		out.OpenSplit, out.OpenCapped, err = splitCount(ctx, q, c.TenantID, w.zone, before, next, c.Now)
		return err
	})
	if err != nil {
		return CutoffReview{}, err
	}
	return out, nil
}

// splitCount counts the open check-ins of the people in `after` that moving from before to
// after would split (SplitsOpenCheckIn). Which entries are asked about:
//
//   - "Open" is ListOpenCheckIns' -- ADR 0032 §A.6's one definition, the anomaly list's and
//     the report's (M15-05) -- over the last StaleOpenIn: an older entry is already left
//     open by the 18-hour rule and no cutoff changes what happens to it.
//   - ONLY THE ENTRY THE ENGINE COULD CLOSE (review ORTA-1 (b)): the person's next tap is
//     paired with GetLastOpenTransaction's answer -- their latest `in` with no `out` after
//     it -- and with nothing else, so a person's older open entries are not split by any
//     cutoff and are not counted. Per person the latest listed entry is asked, and only when
//     it IS that answer (one read per such person; the people are the level's heirs whose
//     cutoff the change moves and who have an entry listed).
func splitCount(ctx context.Context, q *store.Queries, tenantID uuid.UUID, zone string, before, after map[uuid.UUID]time.Duration, now time.Time) (int, bool, error) {
	if len(after) == 0 {
		return 0, false, nil
	}
	rows, err := q.ListOpenCheckIns(ctx, store.ListOpenCheckInsParams{
		TenantID: tenantID,
		FromAt:   now.Add(-tap.StaleOpenIn),
		// A manual record may be stamped up to manual.FutureGrace ahead; a minute past now
		// keeps such an entry in view without reaching into tomorrow.
		ToAt:     now.Add(time.Minute),
		RowLimit: openCheckInLimit + 1,
	})
	if err != nil {
		return 0, false, fmt.Errorf("list the open check-ins: %w", err)
	}
	capped := len(rows) > openCheckInLimit
	if capped {
		rows = rows[:openCheckInLimit]
	}
	// The rows are oldest first ((occurred_at, id)), so the last one kept per person is
	// their latest open entry.
	latest := map[uuid.UUID]time.Time{}
	var order []uuid.UUID
	for _, r := range rows {
		if r.EmployeeID == nil {
			continue
		}
		next, ok := after[*r.EmployeeID]
		if !ok || before[*r.EmployeeID] == next {
			continue
		}
		if _, seen := latest[*r.EmployeeID]; !seen {
			order = append(order, *r.EmployeeID)
		}
		latest[*r.EmployeeID] = r.OccurredAt
	}
	n := 0
	for _, id := range order {
		person := id
		open, err := q.GetLastOpenTransaction(ctx, store.GetLastOpenTransactionParams{TenantID: tenantID, EmployeeID: &person, At: now})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, false, fmt.Errorf("read the open check-in the engine would close: %w", err)
		}
		if !open.OccurredAt.Equal(latest[id]) {
			continue
		}
		if SplitsOpenCheckIn(open.OccurredAt, now, tap.DayCutoff{Cutoff: before[id], Timezone: zone}, tap.DayCutoff{Cutoff: after[id], Timezone: zone}) {
			n++
		}
	}
	return n, capped, nil
}

// actionOf is a level's one trail action.
func actionOf(level CutoffLevel) string {
	switch level {
	case CutoffBusiness:
		return ActionTenantDayCutoffUpdated
	case CutoffBranch:
		return ActionLocationDayCutoffUpdated
	case CutoffDepartment:
		return ActionDepartmentDayCutoffUpdated
	}
	return ActionEmployeeDayCutoffUpdated
}

// version is the value on file and its change count, as one opaque string ("05:00@3",
// "inherit@0"). It holds no "|" (the confirmation payload's separator).
func version(ctx context.Context, q *store.Queries, tenantID uuid.UUID, level CutoffLevel, subject uuid.UUID, stored *time.Duration) (string, error) {
	target := subject.String()
	n, err := q.CountDayCutoffChanges(ctx, store.CountDayCutoffChangesParams{TenantID: tenantID, Target: &target, Action: actionOf(level)})
	if err != nil {
		return "", fmt.Errorf("count the cutoff's changes: %w", err)
	}
	v := ClockText(stored)
	if v == "" {
		v = "inherit"
	}
	return v + "@" + strconv.FormatInt(n, 10), nil
}

// Version is what a confirmation for (level, subject) must have been served against: the
// value on file now and how many times it was changed. The apply step asks it right before
// the write, so a confirmation served before any later change no longer verifies.
func (s *DayCutoffs) Version(ctx context.Context, tenantID uuid.UUID, level CutoffLevel, subject uuid.UUID) (string, error) {
	if _, ok := ParseCutoffLevel(string(level)); !ok {
		return "", ErrDayCutoffUnknownLevel
	}
	if level == CutoffBusiness {
		subject = tenantID
	}
	var out string
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		w, err := load(ctx, q, tenantID, peopleFilter(level, subject))
		if err != nil {
			return err
		}
		_, stored, err := w.subject(level, subject)
		if err != nil {
			return err
		}
		out, err = version(ctx, q, tenantID, level, subject, stored)
		return err
	})
	return out, err
}

// Person is the person card's line: the cutoff the tap would use for them and its source.
// A deactivated person has none to show (they tap nothing) and reads ErrUnknownEmployee.
func (s *DayCutoffs) Person(ctx context.Context, tenantID, employeeID uuid.UUID) (PersonCutoff, error) {
	var out PersonCutoff
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		w, err := load(ctx, store.New(tx), tenantID, peopleFilter(CutoffPerson, employeeID))
		if err != nil {
			return err
		}
		for _, p := range w.people {
			if p.id == employeeID {
				v, src := w.effective(p, nil)
				out = PersonCutoff{Value: v, Source: src, Own: p.own, Zone: w.zone}
				return nil
			}
		}
		return ErrUnknownEmployee
	})
	if err != nil {
		return PersonCutoff{}, err
	}
	return out, nil
}

// Move is what moving one person would do to their cutoff: the value before and after, and
// the shifts the new value cuts among the shifts they would work (the new placement's
// resolved shift and the venues they tapped at lately). Nothing is written.
func (s *DayCutoffs) Move(ctx context.Context, c MoveCutoffCommand) (MoveCutoff, error) {
	var out MoveCutoff
	err := s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		w, err := load(ctx, q, c.TenantID, peopleFilter(CutoffPerson, c.EmployeeID))
		if err != nil {
			return err
		}
		var p *cutoffPerson
		for i := range w.people {
			if w.people[i].id == c.EmployeeID {
				p = &w.people[i]
			}
		}
		if p == nil {
			return ErrUnknownEmployee
		}
		if _, ok := w.venues[c.LocationID]; !ok {
			return ErrUnknownPlacement
		}
		if c.DepartmentID != nil {
			if _, ok := w.departments[*c.DepartmentID]; !ok {
				return ErrUnknownPlacement
			}
		}
		moved := *p
		loc := c.LocationID
		moved.branch, moved.department = &loc, c.DepartmentID
		out.Zone = w.zone
		out.From, out.FromSource = w.effective(*p, nil)
		out.To, out.ToSource = w.effective(moved, nil)
		tapped, err := touched(ctx, q, c.TenantID, []uuid.UUID{p.id}, c.Now)
		if err != nil {
			return err
		}
		var shifts shiftSet
		var conflicts conflictSet
		sh := w.resolvedShift(moved)
		shifts.add(sh)
		conflicts.check(out.To, sh)
		for _, v := range tapped[p.id] {
			if l, ok := w.venues[v]; ok {
				shifts.add(l.shift)
				conflicts.check(out.To, l.shift)
			}
		}
		out.Conflicts = conflicts.sorted()
		if v, ok := SuggestCutoff(shifts.list); ok {
			out.Suggested = &v
		}
		return nil
	})
	if err != nil {
		return MoveCutoff{}, err
	}
	return out, nil
}

// dayCutoffDetail is the trail row's payload: the level, the subject and the value before
// and after, as wall clocks ("" = inherit). EXPLICIT EMPTIES, NO omitempty -- an absent key
// cannot be told from an empty value (refusedRemovalDetail's lesson).
type dayCutoffDetail struct {
	Level     string `json:"level"`
	SubjectID string `json:"subject_id"`
	From      string `json:"from"`
	To        string `json:"to"`
}

// ClockText is a cutoff as the screen and the trail write it ("05:00"), "" for nil.
func ClockText(d *time.Duration) string {
	if d == nil {
		return ""
	}
	m := int(*d / time.Minute)
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

func (c SetCutoffCommand) validate() error {
	switch {
	case c.TenantID == uuid.Nil:
		return errors.New("tenant: tenant id is required")
	case c.ActorID == uuid.Nil:
		return errors.New("tenant: actor id is required")
	case c.Value != nil && !ValidDayCutoff(*c.Value):
		return ErrDayCutoff
	case c.Level == CutoffBusiness && c.Value == nil:
		return ErrDayCutoffRequired
	case c.Level != CutoffBusiness && c.SubjectID == uuid.Nil:
		return errors.New("tenant: a subject id is required")
	case c.ExpectedVersion == "":
		// A write with no reviewed version would be the one path the check below cannot
		// guard; it is refused rather than allowed through unchecked.
		return errors.New("tenant: the reviewed version is required")
	}
	if _, ok := ParseCutoffLevel(string(c.Level)); !ok {
		return ErrDayCutoffUnknownLevel
	}
	return nil
}

// Set writes the value at one level through M15-02's setter and records it in audit_log IN
// THE SAME TRANSACTION, so a change nobody can attribute rolls back (staff.go's rule). The
// value already on file is ErrSameDayCutoff and writes nothing -- no row, no trail.
//
// 🔴 THE REVIEWED VERSION IS CHECKED TWICE, THE SECOND TIME UNDER THE ROW LOCK (M15-07
// review, orchestrator's decision). First against the version read at the start of this
// transaction; then the setter's UPDATE takes the subject row's lock -- a second writer of
// the same level waits there for the first to commit -- and the change count is read AGAIN,
// in a new statement that sees the first writer's committed trail row. A moved count is
// ErrDayCutoffStale and the transaction rolls back: of two confirmations served against one
// version, exactly one writes. The trail row's "from" is therefore the value the reviewed
// version names, verified unchanged under the lock (this closes the earlier "from read
// without a lock" limit).
func (s *DayCutoffs) Set(ctx context.Context, c SetCutoffCommand) (CutoffChange, error) {
	if err := c.validate(); err != nil {
		return CutoffChange{}, err
	}
	if c.Level == CutoffBusiness {
		c.SubjectID = c.TenantID
	}
	var out CutoffChange
	err := s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		w, err := load(ctx, q, c.TenantID, peopleFilter(c.Level, c.SubjectID))
		if err != nil {
			return err
		}
		_, stored, err := w.subject(c.Level, c.SubjectID)
		if err != nil {
			return err
		}
		// The EARLY answer: a version that moved before this transaction began is refused
		// without taking the lock. It is not the guarantee -- the check under the lock below
		// refuses every case this one does (removing this one is an equivalent mutant,
		// measured) -- it only keeps a plain stale press from reaching the UPDATE.
		if v, err := version(ctx, q, c.TenantID, c.Level, c.SubjectID, stored); err != nil {
			return err
		} else if v != c.ExpectedVersion {
			return ErrDayCutoffStale
		}
		if sameCutoff(stored, c.Value) {
			return ErrSameDayCutoff
		}
		if s.beforeWrite != nil {
			s.beforeWrite(ctx)
		}
		var action string
		switch c.Level {
		case CutoffBusiness:
			_, err = q.SetTenantDayCutoff(ctx, store.SetTenantDayCutoffParams{DayCutoff: pgTime(c.Value), TenantID: c.TenantID})
			action = ActionTenantDayCutoffUpdated
		case CutoffBranch:
			_, err = q.SetLocationDayCutoff(ctx, store.SetLocationDayCutoffParams{DayCutoff: pgTime(c.Value), TenantID: c.TenantID, ID: c.SubjectID})
			action = ActionLocationDayCutoffUpdated
		case CutoffDepartment:
			_, err = q.SetDepartmentDayCutoff(ctx, store.SetDepartmentDayCutoffParams{DayCutoff: pgTime(c.Value), TenantID: c.TenantID, ID: c.SubjectID})
			action = ActionDepartmentDayCutoffUpdated
		case CutoffPerson:
			_, err = q.SetEmployeeDayCutoff(ctx, store.SetEmployeeDayCutoffParams{DayCutoff: pgTime(c.Value), TenantID: c.TenantID, ID: c.SubjectID})
			action = ActionEmployeeDayCutoffUpdated
		}
		if err != nil {
			// The subject was found a moment ago in this transaction, so no row here is a
			// failure, not a refusal.
			return fmt.Errorf("set the %s cutoff: %w", c.Level, err)
		}
		// The row is locked now; read the count again (see the doc comment).
		if v, err := version(ctx, q, c.TenantID, c.Level, c.SubjectID, stored); err != nil {
			return err
		} else if v != c.ExpectedVersion {
			return ErrDayCutoffStale
		}
		if _, err := s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   action,
			Target:   c.SubjectID.String(),
			Detail: dayCutoffDetail{
				Level:     string(c.Level),
				SubjectID: c.SubjectID.String(),
				From:      ClockText(stored),
				To:        ClockText(c.Value),
			},
		}); err != nil {
			return err
		}
		out = CutoffChange{From: stored, To: c.Value}
		return nil
	})
	if err != nil {
		return CutoffChange{}, err
	}
	s.log.Info("business day cutoff changed", "level", string(c.Level), "subject_id", c.SubjectID,
		"actor_id", c.ActorID, "from", ClockText(out.From), "to", ClockText(out.To))
	return out, nil
}
