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
	"sort"
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
	// Suggested is a cutoff outside every band of the shifts checked, when there is one.
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
// window, CutoffTouchedWindow), in one transaction. It writes nothing and counts no open
// check-in: nothing changes, so nothing is split (Review's Unchanged rule).
func (s *DayCutoffs) Check(ctx context.Context, tenantID uuid.UUID, now time.Time) (BusinessDayCheck, error) {
	if tenantID == uuid.Nil {
		return BusinessDayCheck{}, errors.New("tenant: tenant id is required")
	}
	var out BusinessDayCheck
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		w, err := load(ctx, q, tenantID, store.ListDayCutoffPeopleParams{})
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, 0, len(w.people))
		for _, p := range w.people {
			ids = append(ids, p.id)
		}
		tapped, err := touched(ctx, q, tenantID, ids, now)
		if err != nil {
			return err
		}
		out = w.check(tenantID, tapped)
		return nil
	})
	if err != nil {
		return BusinessDayCheck{}, err
	}
	return out, nil
}

// check is Check's pure half, over a snapshot already read.
func (w cutoffWorld) check(tenantID uuid.UUID, tapped map[uuid.UUID][]uuid.UUID) BusinessDayCheck {
	out := BusinessDayCheck{Zone: w.zone}
	members := map[uuid.UUID]bool{}
	for _, p := range w.people {
		if p.department != nil {
			members[*p.department] = true
		}
	}
	cut := map[CutoffShift]bool{}
	level := func(l CutoffLevel, subject uuid.UUID, name string, value time.Duration, extra func(*shiftSet, *conflictSet)) {
		heirs, _ := w.heirsOf(l, subject)
		shifts, conflicts, _, _ := w.cuts(l, subject, nil, heirs, tapped)
		if extra != nil {
			extra(&shifts, &conflicts)
		}
		lc := LevelCheck{Level: l, SubjectID: subject, SubjectName: name, Value: value,
			People: len(heirs), Shifts: len(shifts.list), Conflicts: conflicts.sorted()}
		if v, ok := SuggestCutoff(shifts.list); ok {
			lc.Suggested = &v
		}
		for _, c := range lc.Conflicts {
			cut[c.Shift] = true
		}
		out.Levels = append(out.Levels, lc)
	}

	level(CutoffBusiness, tenantID, w.name, w.business, nil)

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
	for _, l := range venues {
		venue := l
		level(CutoffBranch, venue.id, venue.name, *venue.cutoff, func(shifts *shiftSet, conflicts *conflictSet) {
			// The department the per-level review cannot see (the file header).
			for _, d := range departments {
				if d.venue == venue.id && d.cutoff == nil && d.shift != nil && !members[d.id] {
					shifts.add(d.shift)
					conflicts.check(*venue.cutoff, d.shift)
				}
			}
		})
	}
	for _, d := range departments {
		if d.cutoff != nil {
			level(CutoffDepartment, d.id, d.name, *d.cutoff, nil)
		}
	}
	for _, p := range w.people {
		if p.own != nil {
			level(CutoffPerson, p.id, "", *p.own, nil)
		}
	}
	out.CutShifts = len(cut)
	return out
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
