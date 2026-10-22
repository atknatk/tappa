package tenant

// daycutoffcheck.go -- the BUSINESS DAY CHECK (M15-08 steps 3 and 3a, ADR 0032 §A.7, §A.8):
// M15-07's conflict function applied to EVERY level of one business at once, with the value on
// file -- what the owner opens before the release (T0) to set the cutoffs, and what the panel's
// notice ("Your business day setting cuts 2 shifts — review it") counts.
//
// 🔴 ONE BUSINESS, ITS OWN CONTEXT, AN ORDINARY READ. The card's security lens: a report "for
// every tenant" cannot be made under tappa_app's RLS, and crossing the tenant boundary outside an
// op_* function is a §4.5 breach. So there is no cross-tenant report, no new op_* and no command
// that bypasses RLS: each owner reads their own business through WithTenant, every statement
// carrying its own tenant_id predicate (db/queries/daycutoff.sql) -- the same five statements a
// settings card runs, the people unfiltered.
//
// IT REUSES THE REVIEW, IT DOES NOT RESTATE IT. Who a level reaches is cutoffWorld.inherits, what
// it cuts is cutoffWorld.cuts -- the function Review calls -- with no override (the value on file).
// The one thing added is a place the per-level review does not reach: a department nobody is
// placed in, with no value of its own, under a venue that HAS one. ownShifts' business case
// covers such a department only when its venue is empty too, and a venue's own review reads only
// the venue's profile people (peopleFilter), so it cannot tell which departments are empty. The
// check reads everybody, so it can, and checks that department at the venue's value -- the value
// its first people would get from the chain ownShifts estimates with.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// LevelCheck is one level that sets a cutoff, as the check reads it.
type LevelCheck struct {
	Level CutoffLevel
	// SubjectID is the venue's, the department's or the person's id; the tenant's for the
	// business level.
	SubjectID uuid.UUID
	// SubjectName is the business's, the venue's or the department's name; "" for a person
	// (the panel's people reads carry no name -- the line links to the person's card).
	SubjectName string
	// Value is the level's value on file.
	Value time.Duration
	// People is how many people take their cutoff from this level.
	People int
	// Shifts is how many distinct shifts were checked against Value.
	Shifts int
	// Conflicts are the shifts Value cuts.
	Conflicts []CutoffConflict
	// Suggested is a cutoff outside every band of the shifts checked, when there is one -- and
	// only for a level whose value cuts a shift (the only line that shows a suggestion).
	Suggested *time.Duration
}

// BusinessDayCheck is every level of one business that sets a cutoff -- the business first,
// then the venues and the departments by name, then the people -- with what each one's value
// on file cuts.
type BusinessDayCheck struct {
	Zone   string
	Levels []LevelCheck
	// CutShifts is how many distinct shifts some level's value cuts: the number the panel's
	// notice says.
	CutShifts int
}

// Cuts reports whether any level's value cuts a shift.
func (c BusinessDayCheck) Cuts() bool { return c.CutShifts > 0 }

// Check reads the business day check for one business at now (the start of the touched-venue
// window, CutoffTouchedWindow). It writes nothing and counts no open check-in: nothing changes,
// so nothing is split (Review's Unchanged rule).
//
// THE READS ARE IN THE TRANSACTION, THE WORK IS NOT. The snapshot and the touched venues are
// read in one WithTenant; the check itself runs after the transaction has closed, so the pooled
// connection is not held while the levels are worked out. It runs on every owner panel page
// (the shell's notice), so it is LINEAR in the business (check says how), and it stops when the
// request is cancelled.
//
// MEASURED (BenchmarkBusinessDayCheck, 2026-10-22, go1.26.10, 16 cores; people / own cutoffs /
// venues; the CPU half only, the reads are the five statements of the file header):
//
//	shape                before: heirs per level,     after: one pass, membership once,
//	                     membership per level         suggestion only for a cutting level,
//	                                                  allocation-free and per shift set
//	400 / 0 / 10              2.0 ms                       0.27 ms
//	400 / 400 / 10           66.8 ms                       1.3 ms
//	2000 / 2000 / 50        513 ms                         6.8 ms
//	5000 / 5000 / 200      2264 ms                        20.7 ms
//
// (The security lens's own figure for 5000 / 5000 was 1.40 s on its machine.)
func (s *DayCutoffs) Check(ctx context.Context, tenantID uuid.UUID, now time.Time) (BusinessDayCheck, error) {
	if tenantID == uuid.Nil {
		return BusinessDayCheck{}, errors.New("tenant: tenant id is required")
	}
	var (
		w      cutoffWorld
		tapped map[uuid.UUID][]uuid.UUID
	)
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		var err error
		if w, err = load(ctx, q, tenantID, store.ListDayCutoffPeopleParams{}); err != nil {
			return err
		}
		ids := make([]uuid.UUID, 0, len(w.people))
		for _, p := range w.people {
			ids = append(ids, p.id)
		}
		tapped, err = touched(ctx, q, tenantID, ids, now)
		return err
	})
	if err != nil {
		return BusinessDayCheck{}, err
	}
	return w.check(ctx, tenantID, tapped)
}

// levelKey names one level a person can take their cutoff from.
type levelKey struct {
	level   CutoffLevel
	subject uuid.UUID
}

// levelOf is the ONE level p takes their cutoff from -- the first set one of person,
// department, profile branch, business (effective's order, with no override). It is
// inherits(p, level, subject) answered for every level at once: for a level that SETS a value
// (the only ones check asks about) the two agree, which TestBusinessDayCheck_TheOnePassIsTheReviewPerLevel
// pins over random businesses.
func (w cutoffWorld) levelOf(p cutoffPerson, tenantID uuid.UUID) levelKey {
	if p.own != nil {
		return levelKey{CutoffPerson, p.id}
	}
	if p.department != nil {
		if d, ok := w.departments[*p.department]; ok && d.cutoff != nil {
			return levelKey{CutoffDepartment, d.id}
		}
	}
	if p.branch != nil {
		if l, ok := w.venues[*p.branch]; ok && l.cutoff != nil {
			return levelKey{CutoffBranch, l.id}
		}
	}
	return levelKey{CutoffBusiness, tenantID}
}

// checkCtxEvery is how many levels check works out between two looks at the request's context.
const checkCtxEvery = 256

// check is Check's pure half, over a snapshot already read.
//
// LINEAR: every person is placed in their one level in a single pass (levelOf) instead of each
// level scanning everybody (heirsOf), where people are placed (membershipOf) is built once
// instead of once per level inside ownShifts, and a venue's departments are grouped once. Each
// level then costs its own heirs and their touched venues.
func (w cutoffWorld) check(ctx context.Context, tenantID uuid.UUID, tapped map[uuid.UUID][]uuid.UUID) (BusinessDayCheck, error) {
	out := BusinessDayCheck{Zone: w.zone}
	m := w.membershipOf()
	heirs := map[levelKey][]cutoffPerson{}
	for _, p := range w.people {
		k := w.levelOf(p, tenantID)
		heirs[k] = append(heirs[k], p)
	}
	cut := map[CutoffShift]bool{}
	// SuggestCutoff walks the day's 1440 minutes; levels that check the same shifts (the people
	// of one venue, each with a value of their own) get the same answer, so it is asked once per
	// set of shifts.
	suggestions := map[string]*time.Duration{}
	suggest := func(shifts []CutoffShift) *time.Duration {
		keys := make([]string, 0, len(shifts))
		for _, s := range shifts {
			keys = append(keys, fmt.Sprintf("%d|%d|%t|%s", s.Start, s.End, s.Overnight, s.Owner))
		}
		sort.Strings(keys)
		k := strings.Join(keys, "\n")
		if v, seen := suggestions[k]; seen {
			return v
		}
		var v *time.Duration
		if d, ok := SuggestCutoff(shifts); ok {
			v = &d
		}
		suggestions[k] = v
		return v
	}
	worked := 0
	level := func(l CutoffLevel, subject uuid.UUID, name string, value time.Duration, extra func(*shiftSet, *conflictSet)) error {
		if worked++; worked%checkCtxEvery == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		hs := heirs[levelKey{l, subject}]
		shifts, conflicts, _, _ := w.cuts(l, subject, nil, hs, tapped, m)
		if extra != nil {
			extra(&shifts, &conflicts)
		}
		lc := LevelCheck{Level: l, SubjectID: subject, SubjectName: name, Value: value,
			People: len(hs), Shifts: len(shifts.list), Conflicts: conflicts.sorted()}
		if len(lc.Conflicts) > 0 {
			lc.Suggested = suggest(shifts.list)
		}
		for _, c := range lc.Conflicts {
			cut[c.Shift] = true
		}
		out.Levels = append(out.Levels, lc)
		return nil
	}
	if err := ctx.Err(); err != nil {
		return BusinessDayCheck{}, err
	}
	if err := level(CutoffBusiness, tenantID, w.name, w.business, nil); err != nil {
		return BusinessDayCheck{}, err
	}

	venues := make([]cutoffPlace, 0, len(w.venues))
	for _, l := range w.venues {
		if l.cutoff != nil {
			venues = append(venues, l)
		}
	}
	sortPlaces(venues)
	departments := make([]cutoffPlace, 0, len(w.departments))
	for _, d := range w.departments {
		departments = append(departments, d)
	}
	sortPlaces(departments)
	// The department the per-level review cannot see (the file header), grouped by venue once.
	unplaced := map[uuid.UUID][]cutoffPlace{}
	for _, d := range departments {
		if d.cutoff == nil && d.shift != nil && !m.departments[d.id] {
			unplaced[d.venue] = append(unplaced[d.venue], d)
		}
	}
	for _, l := range venues {
		venue := l
		if err := level(CutoffBranch, venue.id, venue.name, *venue.cutoff, func(shifts *shiftSet, conflicts *conflictSet) {
			for _, d := range unplaced[venue.id] {
				shifts.add(d.shift)
				conflicts.check(*venue.cutoff, d.shift)
			}
		}); err != nil {
			return BusinessDayCheck{}, err
		}
	}
	for _, d := range departments {
		if d.cutoff != nil {
			if err := level(CutoffDepartment, d.id, d.name, *d.cutoff, nil); err != nil {
				return BusinessDayCheck{}, err
			}
		}
	}
	for _, p := range w.people {
		if p.own != nil {
			if err := level(CutoffPerson, p.id, "", *p.own, nil); err != nil {
				return BusinessDayCheck{}, err
			}
		}
	}
	out.CutShifts = len(cut)
	return out, nil
}

// sortPlaces orders venues or departments by name, then id (the reads' own order).
func sortPlaces(ps []cutoffPlace) {
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].name != ps[j].name {
			return ps[i].name < ps[j].name
		}
		return ps[i].id.String() < ps[j].id.String()
	})
}
