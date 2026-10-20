package ledger

// roster.go -- the READ side of the EMPLOYEES section (M6-05 phase A).
//
// 🔴 WHY IT IS IN THIS PACKAGE, WHICH IS NAMED AFTER THE TRANSACTION LEDGER. The
// obvious alternative was a new internal/domain/roster, and it was weighed rather
// than dismissed. What decided it is query_test.go: §4.5's SECOND defence -- the
// explicit tenant predicate beside RLS -- is asserted by exactly one mechanism in
// this repository, a go/ast walk that DERIVES the queries to check by parsing THIS
// package. A new package starts with no such net, so paying for one means a third
// copy of a 600-line test (internal/domain/review has the second), which is the
// "second representation" shape this repo has paid for repeatedly: three copies of
// one rule, and the drift invisible because each copy passes on its own.
//
// WHAT THE PACKAGE COMMENT HAD TO GIVE UP FOR THAT, stated rather than smuggled:
// "reads the immutable record" is now too narrow -- employees is a MUTABLE table
// (status changes, and phase B will change it). The claim that does NOT weaken is
// the operative one: there is still no store call in this package that is not a
// SELECT, and this file adds one SELECT and nothing else. The write path for a
// person -- invite, re-invite, deactivate, move -- is M6-05 phase B and does not
// live here.
//
// 🔴 §4.7 -- WHAT A Person CANNOT CARRY. The query this maps from does not select
// sessions.token_hash, sessions.device_info, sessions.id or employees.email, and
// Person below has no field for any of them. Two independent walls, the same shape
// Record uses for coordinates: a template cannot render what the struct has no
// field for, and a field added later would have nothing to fill it from. What the
// screen shows about a device is that one EXISTS and when it was last used.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// RosterPageSize is how many people one page of the roster returns.
//
// 🔴 IT IS 50 AND IT IS NOT PageSize, WHICH IS A USER DECISION (2026-08-07) TAKEN ON
// ONE MEASURED EDGE CASE. It shipped as `= PageSize` (25) with the argument that two
// page sizes in one panel is a difference a reader has to learn for no reason. That
// argument lost to arithmetic: the two lists have different lengths and therefore
// different failure modes.
//
//	A DAY of records   is bounded by traffic. Measured across 40 850 tenant-days,
//	                   the median day is 1 record and the 99th percentile is 30, so
//	                   a whole day is one or two pages at any sane page size.
//	A ROSTER           is bounded by the PAYROLL, which is the one quantity in this
//	                   product that grows without limit. Walking it costs
//	                   ceil(E / RosterPageSize) charged requests, and adminSessionLimit
//	                   is 300 per session per ten minutes.
//
// 🔴 THE ARGUMENT IS STATED AS AN INEQUALITY, AND NO ROSTER COUNT APPEARS IN IT.
// This block used to open with a measured headcount for the development database's
// largest tenant. That quantity is not a fact, it is a CLOCK: the simulated-day
// fixture hires into that tenant on every `make test`
// (internal/handler/seedflow_db_test.go, insertEmployee against fixtures.TenantKF)
// and migration 00003 REVOKEs DELETE on employees, so it only ever climbs. It was
// re-quoted, refreshed and re-broken in three consecutive audit rounds, twice moving
// inside a single review, and at one point four files disagreed with each other and
// with the plan card. This block is the FIRST thing anybody reopening the constant
// reads, so it is the last place that should carry a number with a shelf life.
// TestComments_DoNotQuoteTheDriftingRosterSize is what keeps it out.
//
// SO THE INVARIANT IS THE ARGUMENT: RosterPageSize x adminSessionLimit is the
// largest roster one session can walk end to end, and it must reach
// internal/handler's rosterDesignCeiling. At 50 that product clears the promise; at
// 25 it is half of it and the promise breaks — and the break is not hypothetical,
// because the development database already holds a tenant between the two. Which
// tenant, and how big it is today, is a question with a QUERY rather than an answer:
// internal/handler/adminratelimit.go prints the SQL and the reader runs it.
//
// It is ASSERTED rather than remembered:
// TestRosterPageSize_KeepsAWholeRosterInsideTheSessionBudget compares those three
// constants and is measured going red at 25.
//
// ⚠️ THE TWO CONSTANTS ARE DELIBERATELY NOT TIED TOGETHER (user decision, same day).
// Writing `RosterPageSize = 2 * PageSize` would make a future change to the
// transactions page size silently move the roster's budget arithmetic, and the two
// were sized by different arguments against different loads.
//
// WHAT IT COSTS, MEASURED ON THE REAL HANDLER rather than estimated — the full table
// and the command that produced it are in internal/handler/employees.go: a page goes
// from 22 028 B to 40 103 B (+82.1%). Those two are stable, because a page holds
// RosterPageSize rows whatever the payroll behind it. That cost is accepted: it is
// still one twenty-first of the 867 233 B control M6-03 measured and removed.
const RosterPageSize = 50

// EmployeeStatuses is the lifecycle vocabulary, and it is migration 00003's CHECK
// spelled once in Go.
//
// 🔴 THE ORDER IS THE LIFECYCLE, NOT ALPHABETICAL, and "deactivated" is IN the list
// rather than filtered out of it. §4.6: somebody who has left stays reachable. The
// filter NARROWS to a status on request; the default is no predicate at all.
var EmployeeStatuses = []string{StatusInvited, StatusActive, StatusDeactivated}

const (
	StatusInvited     = "invited"
	StatusActive      = "active"
	StatusDeactivated = "deactivated"
)

// 🔴 THE ROSTER'S PAGING ANCHOR IS AN ID, AND IT USED TO BE A NAME. That change is a
// §4.6 fix, measured rather than argued.
//
// The cursor is a keyset position on (full_name, id), so the client used to carry
// BOTH halves in the query string. A query string is not a place for an unbounded
// value, so the name half was bounded — and employees.full_name is unbounded `text`
// with no CHECK, so a longer one was dropped. A dropped cursor is not a smaller page,
// it is the SAME page: measured by a security audit on real Postgres, a 605-rune name
// sitting on a page boundary made "Next page" serve page one forever, so everybody
// behind it became unreachable by browsing and the first page was listed twice.
// Reproduced here before it was fixed, and pinned by
// TestPanelEmployeesDB_PagesPastAnAbsurdlyLongName.
//
// Now the client carries the anchor's ID and this package reads the name from the
// database, in the SAME transaction as the page. Three things follow:
//
//	the length problem cannot recur   an id is 36 characters whatever the payroll
//	                                  holds, so nothing is bounded and nothing is
//	                                  dropped
//	no name travels in a URL          which closes the privacy wrinkle recorded in
//	                                  internal/handler/adminlogin.go's Referrer-Policy
//	                                  note: a "next page" link used to contain a real
//	                                  employee's full name
//	a renamed anchor is followed      the old URL carried whatever the name was when
//	                                  the link was built; this reads what it is now
//
// It is still client-supplied and still safe for the reason the old one was: it can
// only move the reader within their own tenant, because the lookup carries the tenant
// predicate and a foreign id simply returns no row.

// RosterFilter is what narrows the roster.
//
// 🔴 THE ZERO VALUE IS "EVERYONE, INCLUDING THE PEOPLE WHO HAVE LEFT", and that is
// the §4.6 requirement M6-03 handed to this section rather than a default somebody
// picked. An unset Status is a NULL parameter, which the query's
// `status IS NULL OR e.status = status` reads as "do not narrow"; there is no
// status predicate to remove because none is added.
type RosterFilter struct {
	// Name is a NAME FRAGMENT, "" meaning everyone. The caller has NOT escaped
	// ILIKE's wildcards -- this layer does, for the same reason Filter.EmployeeName
	// documents: the escaping is an artifact of the operator, so what travels
	// through the handler, the view and the URL is exactly what was typed.
	Name string
	// Status is one of EmployeeStatuses, or "" for every status. The handler
	// validates it against that closed set before it gets here (§7).
	Status       string
	LocationID   *uuid.UUID
	DepartmentID *uuid.UUID
	// AfterID is the id of the last person already shown. nil starts at the first
	// name. The NAME half of the keyset is read from the database — see the block
	// above for why it is not carried here.
	AfterID *uuid.UUID
}

// Narrowed reports whether anything is filtering the roster. The empty state uses
// it to pick which claim it is making -- "nobody works here" and "nobody matches
// that" are different sentences and only one can be true.
func (f RosterFilter) Narrowed() bool {
	return f.Name != "" || f.Status != "" || f.LocationID != nil || f.DepartmentID != nil
}

// Person is one row of the roster.
//
// 🔴 THE FIELDS THAT ARE NOT HERE ARE THE POINT -- see the file comment. There is
// no Email, no DeviceLabel, no SessionID and no TokenHash.
type Person struct {
	ID   uuid.UUID
	Name string
	// Status is one of EmployeeStatuses. It is never derived or defaulted here:
	// migration 00003 makes the column NOT NULL with a CHECK, so an empty string
	// would mean the query stopped selecting it.
	Status string
	// Role is the job title a manager typed (employees.role), "" when none was. Free text
	// no decision reads (00003); the screen prints it beside the venue.
	Role string
	// LastTap is this person's newest non-practice record, nil when they have none. nil
	// means "no tap yet" ONLY when the page's LastTaps reading is Measured -- a page whose
	// last-tap read failed carries nil on every row and says so (RosterPage.LastTaps).
	LastTap *LastTap
	// HasHome is false for an employee the integration manages who has no home venue
	// (K-14, migration 00037). It is what tells "no venue" apart from a missed join.
	HasHome bool
	// LocationName and DepartmentName are "" when the row carries no such name. With
	// HasHome true an empty LocationName means a join missed rather than that a person
	// has no venue -- the screen says "unknown" rather than printing nothing, because a
	// blank cell reads as "none".
	LocationName   string
	DepartmentName string
	// LiveDevices is how many of this person's sessions are still valid. 0 means
	// "no phone is signed in": either they have not activated yet, or every device
	// they had was revoked.
	//
	// IT IS A COUNT RATHER THAN A BOOL because the count is what a manager chasing
	// a lost phone needs, it costs nothing extra to carry, and "1" and "3" are
	// different situations that a bool renders identically.
	LiveDevices int
	// LastUsed is when the most recently used LIVE device was last used, nil when
	// there is none.
	//
	// 🔴 nil HAS TWO MEANINGS AND THE SCREEN MUST NOT MERGE THEM. With LiveDevices
	// == 0 it means "no device to have used"; with LiveDevices > 0 it means "signed
	// in and not yet used", which is the ordinary state of a phone activated a
	// minute ago (00003: last_used_at is stamped on USE and is nullable). Rendering
	// both as a blank would tell a manager a fresh activation had gone stale.
	LastUsed *time.Time
	// Since is the lifecycle stamp that MATCHES Status: invited_at, activated_at or
	// deactivated_at. It is nil when that stamp was never written -- the three
	// columns are nullable by design (00003) and seed rows predate some of them.
	//
	// ONE FIELD RATHER THAN THREE, because only one of the three is ever the
	// answer to "since when?" and carrying the other two would put two dates on
	// the row that contradict the word beside them.
	Since *time.Time
}

// LastTap is the newest non-practice record of one person, as the roster row prints it
// (M12-18): when, the engine's verdict word and the channel -- "14:31 today", "refused",
// "by hand".
//
// 🔴 K-39 / §4.7 -- WHAT IT CANNOT CARRY. No venue, no plaque, no note, no trust, no
// evidence sign: the statement (ListLastTapsForEmployees) selects three columns and this
// type has a field for each and nothing else, so a tap at a holding peer's venue reads
// exactly like any other. The verdict word is ADR 0027 §13 item 5's named residue.
type LastTap struct {
	At time.Time
	// Verdict is the engine's word (ok, flag, reject, ignored; migration 0005's CHECK) --
	// never a reviewer's, which lives in transaction_reviews.
	Verdict string
	// Channel is nfc, qr or manual: a record a manager typed is not a tap, and the screen
	// may say so.
	Channel string
}

// RosterPage is one page of people.
type RosterPage struct {
	// Queried is the anti-fabrication flag, the same one Page and QueuePage carry
	// and for the same reason: "nobody works here" is a claim about the world and a
	// zero RosterPage renders identically to an empty business unless something
	// separates them (§4.6, the class M5-11 closed).
	Queried bool
	People  []Person
	// LastTaps is the state of the page's last-tap read (M12-18): Measured, every row's
	// LastTap is the answer (nil = none); ReadFailed, the read was asked inside the page's
	// transaction and failed in its own savepoint, so no row's LastTap means anything and
	// the screen says so rather than "—" (§4.6); NotQueried, nobody asked.
	LastTaps Reading
	// NextID is the anchor for the following page, nil when this is the last one.
	NextID *uuid.UUID
	// Zone is the tenant's timezone; every instant above is UTC and is converted at
	// render (CLAUDE.md §6).
	Zone *time.Location
}

// RosterScreen is a RosterPage plus what only the full document needs.
type RosterScreen struct {
	RosterPage
	Options    Options
	TenantName string
	// Tally is the per-status count the People screen's chips and kicker print (M12-17). It is
	// read in the same transaction as the page, in its own savepoint: a failure leaves it
	// ReadFailed and the page standing.
	Tally StatusTally
}

// StatusCounts is one count per lifecycle status, read through Of.
//
// THE COUNTS ARE INDEXED BY EmployeeStatuses RATHER THAN NAMED FIELDS, and the reason is the
// §4.7 field walker (internal/handler's bannedFieldName): it refuses any field whose name holds
// "invite", because that is where an invitation code would travel -- and a field named after the
// lifecycle word "invited" is exactly such a name. The words stay the database's; they are the
// keys, not identifiers.
type StatusCounts struct {
	counts [3]int
}

// statusIndex is a status word's place in EmployeeStatuses, -1 for any other word.
func statusIndex(status string) int {
	for i, s := range EmployeeStatuses {
		if s == status {
			return i
		}
	}
	return -1
}

// Of is the count for one status word, 0 for a word outside EmployeeStatuses -- a word the
// handler validated against that closed set never reaches that branch.
func (c StatusCounts) Of(status string) int {
	if i := statusIndex(status); i >= 0 {
		return c.counts[i]
	}
	return 0
}

// Total is the sum of the three. It is the only total there is: migration 00003's CHECK admits
// no fourth status, and a row carrying one fails the read rather than vanishing from the sum.
func (c StatusCounts) Total() int {
	n := 0
	for _, v := range c.counts {
		n += v
	}
	return n
}

// StatusTally is how many people the roster holds per lifecycle status (M12-17, D3).
//
// 🔴 TWO COUNTS, AND THEY ANSWER DIFFERENT QUESTIONS. Matching honours the roster's venue,
// department and name filters and ignores its status filter -- each chip says how many the list
// would hold if it were picked. Everyone honours nothing but the tenant: its Total is the
// kicker's "N on the books", which does not shrink because a manager typed a name.
//
// State is the §4.6 flag (reading.go): only a Measured tally may print a number, 0 included.
type StatusTally struct {
	State    Reading
	Matching StatusCounts
	Everyone StatusCounts
}

// Roster loads one page of the people who work for this tenant.
//
// ONE TRANSACTION, THREE QUERIES -- the same shape Screen uses. The tenant's zone
// has to come from the same snapshot as the rows or a page could render its dates
// against a zone that changed between round trips, and the two dropdowns have to
// agree with the list they filter.
//
// 🔴 THERE IS NO FRAGMENT VARIANT OF THIS, unlike Day/Screen, and that is a
// decision rather than an omission: the employees section loads NO SCRIPT, so its
// "next page" is an ordinary link to a whole document. See
// internal/handler/employees.go for why, and what it costs.
func (r *Reader) Roster(ctx context.Context, tenantID uuid.UUID, f RosterFilter) (RosterScreen, error) {
	var out RosterScreen
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		clock, err := q.GetTenantClock(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load tenant clock: %w", err)
		}
		out.TenantName = clock.Name
		out.Zone = r.loadZone(clock.Timezone)

		narrow := rosterNarrowingOf(f)
		params := store.ListPanelEmployeesParams{
			TenantID:     tenantID,
			LocationID:   narrow.location,
			DepartmentID: narrow.department,
			FullName:     narrow.name,
			PageSize:     RosterPageSize + 1, // one extra: see below
		}
		if f.Status != "" {
			s := f.Status
			params.Status = &s
		}
		// THE ANCHOR IS RESOLVED INSIDE THIS TRANSACTION, so the name the keyset is
		// built from and the rows it selects come from one snapshot.
		//
		// AN UNRESOLVABLE ID IS "NO CURSOR", NOT AN ERROR. It means a stale bookmark,
		// a hand-edited URL, or an id belonging to another tenant — none of which the
		// product emitted. Answering with the FIRST page shows more of the caller's own
		// roster rather than less, which is the direction §4.6 cares about; answering
		// with an error page would turn a stale link into a dead end.
		if f.AfterID != nil {
			name, err := q.GetRosterCursorAnchor(ctx, store.GetRosterCursorAnchorParams{
				TenantID: tenantID,
				ID:       *f.AfterID,
			})
			switch {
			case err == nil:
				id := *f.AfterID
				params.CursorName = &name
				params.CursorID = &id
			case errors.Is(err, pgx.ErrNoRows):
				r.log.Info("panel: roster cursor does not resolve; showing the first page",
					"tenant_id", tenantID)
			default:
				return fmt.Errorf("resolve roster cursor: %w", err)
			}
		}

		rows, err := q.ListPanelEmployees(ctx, params)
		if err != nil {
			return fmt.Errorf("list employees: %w", err)
		}

		// ONE EXTRA ROW IS FETCHED AND DROPPED, which answers "is there another
		// page" without a COUNT over the roster. A count would be a second scan of
		// every employee and would still be a guess by the time the next page is
		// asked for; the extra row is proof, and it costs one tuple.
		out.Queried = true
		more := len(rows) > RosterPageSize
		if more {
			rows = rows[:RosterPageSize]
		}
		out.People = make([]Person, 0, len(rows))
		for _, row := range rows {
			out.People = append(out.People, person(row))
		}
		if more && len(out.People) > 0 {
			id := out.People[len(out.People)-1].ID
			out.NextID = &id
		}

		// EACH ROW'S LAST TAP, for THIS PAGE'S people only (ListLastTapsForEmployees says
		// why it is a second statement). In the page's snapshot, in its own savepoint: a
		// failure leaves every row without one and the page saying it could not read them.
		out.LastTaps = ReadFailed
		if err := inSavepoint(ctx, tx, func(q *store.Queries) error {
			return lastTaps(ctx, q, tenantID, out.People)
		}); err != nil {
			for i := range out.People {
				out.People[i].LastTap = nil
			}
			r.log.Error("ledger: could not read the roster's last taps; the rows will say so "+
				"rather than claim nobody tapped", "err", err, "tenant_id", tenantID)
		} else {
			out.LastTaps = Measured
		}

		if out.Options, err = options(ctx, q, tenantID); err != nil {
			return err
		}

		// THE CHIPS ARE A WIDGET, NOT THE LIST. Their read rides in the page's transaction --
		// one snapshot, so a chip cannot disagree with the rows under it because somebody was
		// hired between two round trips -- but inside a savepoint, so a failed count costs
		// the chips and never the roster (§4.6; advisories_db_test.go measured why the
		// savepoint, not the swallowed error, is what keeps the page).
		out.Tally = StatusTally{State: ReadFailed}
		if err := inSavepoint(ctx, tx, func(q *store.Queries) error {
			tally, err := statusTally(ctx, q, tenantID, narrow)
			if err != nil {
				return err
			}
			out.Tally = tally
			return nil
		}); err != nil {
			r.log.Error("ledger: could not count the roster per status; the chips will say so "+
				"rather than print a number", "err", err, "tenant_id", tenantID)
		}
		return nil
	})
	if err != nil {
		return RosterScreen{}, fmt.Errorf("ledger: roster: %w", err)
	}
	return out, nil
}

// lastTaps fills each person's LastTap from ListLastTapsForEmployees. A page with nobody on it
// asks nothing and is measured: there is no row whose tap could be unknown.
func lastTaps(ctx context.Context, q *store.Queries, tenantID uuid.UUID, people []Person) error {
	if len(people) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(people))
	at := make(map[uuid.UUID]int, len(people))
	for i, p := range people {
		ids[i] = p.ID
		at[p.ID] = i
	}
	rows, err := q.ListLastTapsForEmployees(ctx, store.ListLastTapsForEmployeesParams{
		TenantID:    tenantID,
		EmployeeIds: ids,
	})
	if err != nil {
		return fmt.Errorf("list last taps: %w", err)
	}
	for _, row := range rows {
		i, ok := at[row.EmployeeID]
		if !ok {
			return fmt.Errorf("list last taps: a row for an employee the page does not hold")
		}
		people[i].LastTap = &LastTap{At: row.OccurredAt, Verdict: row.Verdict, Channel: row.Channel}
	}
	return nil
}

// rosterNarrowing is the part of a RosterFilter the list and the status tally share: venue,
// department and the escaped name fragment. Built once, handed to both statements, so the chips
// cannot be counted against a filter the list was not read with.
type rosterNarrowing struct {
	location   *uuid.UUID
	department *uuid.UUID
	name       *string
}

func rosterNarrowingOf(f RosterFilter) rosterNarrowing {
	n := rosterNarrowing{location: f.LocationID, department: f.DepartmentID}
	if f.Name != "" {
		escaped := escapeLike(f.Name)
		n.name = &escaped
	}
	return n
}

// statusTally reads CountPanelEmployeesByStatus.
//
// A STATUS OUTSIDE EmployeeStatuses IS AN ERROR, NOT A DROPPED ROW. migration 00003's CHECK makes
// it unreachable today; if that ever changes, a tally that silently left those people out of
// the total would be a smaller number presented as a measurement.
func statusTally(ctx context.Context, q *store.Queries, tenantID uuid.UUID, n rosterNarrowing) (StatusTally, error) {
	rows, err := q.CountPanelEmployeesByStatus(ctx, store.CountPanelEmployeesByStatusParams{
		TenantID:     tenantID,
		LocationID:   n.location,
		DepartmentID: n.department,
		FullName:     n.name,
	})
	if err != nil {
		return StatusTally{}, fmt.Errorf("count employees by status: %w", err)
	}
	out := StatusTally{State: Measured}
	for _, row := range rows {
		i := statusIndex(row.Status)
		if i < 0 {
			return StatusTally{}, fmt.Errorf("count employees by status: a status outside the vocabulary: %q", row.Status)
		}
		out.Matching.counts[i], out.Everyone.counts[i] = int(row.Matching), int(row.Everyone)
	}
	return out, nil
}

// person maps one store row onto one roster row.
//
// IT IS A NARROWING, NOT A COPY, exactly like record(): the query is already free
// of the session's identity and label, and this step additionally picks the ONE
// lifecycle stamp that matches the status and flattens the nullable names.
func person(row store.ListPanelEmployeesRow) Person {
	return Person{
		ID:             row.ID,
		Name:           row.FullName,
		Role:           deref(row.Role),
		Status:         row.Status,
		HasHome:        row.LocationID != nil,
		LocationName:   deref(row.LocationName),
		DepartmentName: deref(row.DepartmentName),
		LiveDevices:    int(row.LiveSessions),
		LastUsed:       row.SessionLastUsedAt,
		Since:          lifecycleStamp(row),
	}
}

// lifecycleStamp picks the timestamp that belongs to the status the row is in.
//
// 🔴 THE DEFAULT IS nil RATHER THAN "the most recent stamp", and the difference is
// a claim. A row whose status is one this function does not know about has no date
// this screen can honestly print beside it; falling back to whichever stamp happens
// to be set would label an unknown state with a date that means something else.
// Today the switch is total -- migration 00003's CHECK admits exactly these three --
// so the default is unreachable, which is why it costs nothing to be strict.
func lifecycleStamp(row store.ListPanelEmployeesRow) *time.Time {
	switch row.Status {
	case StatusInvited:
		return row.InvitedAt
	case StatusActive:
		return row.ActivatedAt
	case StatusDeactivated:
		return row.DeactivatedAt
	default:
		return nil
	}
}

// RecentTapCount is how many records the People card's strip shows (M12-19).
const RecentTapCount = 7

// RecentTaps is one person's last RecentTapCount non-practice records, newest first (M12-19).
//
// State is the §4.6 flag (reading.go). An empty Taps says "no record yet" ONLY when State is
// Measured; a failed read says it could not be read.
type RecentTaps struct {
	State Reading
	Taps  []RecentTap
}

// RecentTap is one mini docket of the strip.
//
// 🔴 K-39 / §4.7 -- WHAT IT CANNOT CARRY: no venue, no plaque, no note, no trust, no evidence
// sign. The statement (ListRecentTapsForEmployee) selects five columns and this type has a field
// for each, so a record at a holding peer's venue is day / direction / time / word like any other.
type RecentTap struct {
	At time.Time
	// Direction is "in" or "out", "" for a record with none (a refusal or an ignored repeat,
	// 00014's CHECK) -- the screen prints "—", never a guess.
	Direction string
	// Verdict is the engine's word (ok, flag, reject, ignored).
	Verdict string
	// Channel is nfc, qr or manual.
	Channel string
	// Review is a manager's later decision on a flag (approved, rejected), "" when none was
	// taken. It is the manager's word beside the engine's, never in place of it (§4.3).
	Review string
}

// RecentTaps reads one person's last RecentTapCount records.
//
// IT NEVER RETURNS AN ERROR, BECAUSE A FAILED STRIP MUST NOT COST THE CARD. It runs in its own
// transaction -- nothing else rides on it, so there is nothing for a failure to abort -- and a
// failure is ReadFailed and logged here, with the tenant (§7).
//
// employeeID only narrows inside tenantID: a foreign or unknown id finds nothing and answers a
// Measured empty strip, which is true of this tenant (no record of that id). The card itself is
// built from tenant.Staff.Person, which already answers "not on this roster".
func (r *Reader) RecentTaps(ctx context.Context, tenantID, employeeID uuid.UUID) RecentTaps {
	var out RecentTaps
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := store.New(tx).ListRecentTapsForEmployee(ctx, store.ListRecentTapsForEmployeeParams{
			TenantID:   tenantID,
			EmployeeID: &employeeID,
		})
		if err != nil {
			return fmt.Errorf("list recent taps: %w", err)
		}
		out.Taps = make([]RecentTap, 0, len(rows))
		for _, row := range rows {
			out.Taps = append(out.Taps, RecentTap{
				At:        row.OccurredAt,
				Direction: deref(row.Type),
				Verdict:   row.Verdict,
				Channel:   row.Channel,
				Review:    deref(row.ReviewOutcome),
			})
		}
		return nil
	})
	if err != nil {
		r.log.Error("ledger: could not read a person's recent taps; the card will say so "+
			"rather than show an empty strip", "err", err, "tenant_id", tenantID)
		return RecentTaps{State: ReadFailed}
	}
	out.State = Measured
	return out
}
