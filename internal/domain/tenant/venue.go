package tenant

// venue.go -- the VENUE and DEPARTMENT write side (M6-06 phase A): adding a
// location, rewriting its proof-of-place configuration and its shift, and the
// same two acts for the departments inside it.
//
// 🔴 WHY IT IS IN THIS PACKAGE. CLAUDE.md section 3 assigns "tenant / lokasyon /
// departman / calisan is kurallari" to internal/domain/tenant, and staff.go
// already made that sentence true for employees. The two halves it names first --
// location and department -- land here for the same three reasons staff.go
// weighed: internal/domain/ledger advertises "no store call in ledger is not a
// SELECT" and these are writes; a new package would start with no section 4.5
// scope net; and this one already HAS the net -- query_test.go derives the
// queries it checks from THIS package's own calls with go/ast, so the six
// statements named below joined the belt the moment they were named here rather
// than the day somebody remembered to list them.
//
// 🔴 WHAT MAKES THESE WRITES DIFFERENT FROM EVERY OTHER PANEL WRITE, AND WHY THE
// AUDIT ROW IS NOT OPTIONAL. Deactivating somebody changes what happens to THAT
// PERSON. Changing a venue's static_ips changes HOW EVERY FUTURE TAP AT THAT
// VENUE IS JUDGED -- silently, with no error and no failed request:
//
//	static_ips emptied   the IP half of proof of place disappears (section 5 row
//	                     6, 50 of 100 trust points). Every later tap there falls
//	                     to the GPS path; a tap with no GPS lands on `flag` (row
//	                     7) and goes to the manager's approval queue.
//	gps cleared          the backup half disappears. With static_ips also empty
//	                     the venue has no proof of place at all. Measured on the
//	                     development database (2026-08-08): about 4.6% of location
//	                     rows are already in exactly that state.
//	shift cleared        lateness stops being COMPUTED for that venue -- which is
//	                     different from "nobody is late", and is why Shift is a
//	                     nil POINTER here rather than a zero pair.
//
// `locations` carries no updated_at (migration 00002 has created_at only), so
// without the audit_log row written inside the same transaction below there would
// be NO RECORD ANYWHERE that the evidence a tap is judged on had changed. That is
// section 4.3's argument applied one level out: the transaction rows stay
// immutable, but the configuration that produced their verdicts must not be able
// to change without a trace.
//
// 🔴 WHAT THIS FILE DOES NOT DO, in the order somebody would expect it to:
//
//   - It DOES delete a venue or a department, but ONLY one that nothing points at
//     (user decision, 2026-08-08: option (b) of the three the M6-06 card records).
//     Six foreign keys reference these two tables and all six are ON DELETE RESTRICT
//     (measured from pg_constraint: departments->locations, employees->locations,
//     employees->departments, tags->locations, transactions->locations,
//     transactions->departments), and neither table has a status, archived or
//     deleted_at column -- so a venue IN USE can be neither removed nor retired, and
//     the screen says that as a RULE rather than as a fault. About five sixths of the
//     location rows in the development database are in that state.
//
//     ⚠️ THE COUNT IS THE SCREEN'S AND THE KEYS ARE THE GUARD. See DeleteVenue: the
//     panel counts references to decide whether to OFFER the control, and the
//     foreign keys are what actually refuse -- which is why the race between the two
//     is a sentence rather than a 500.
//
//     ⚠️ THE ROW COUNTS DRIFT AND THE PROPORTION DOES NOT, which is why the
//     proportion is what this comment quotes. The development database grows on
//     every suite run (these tests seed tenants and never delete them --
//     audit_log is append-only). Five measurements on 2026-08-08 read 117 553 to
//     122 136 rows; the share that cannot be deleted was 84.1% in all five. A live
//     absolute in a comment is a claim that is false by the time anybody reads it.
//   - It does NOT move a department between venues. db/queries/departments.sql
//     carries the argument at UpdateDepartment: it would leave every employee in
//     that department holding a (venue, department) pair MoveEmployee refuses.
//   - It does not WRITE to `tags`, and plaques are M6-06 phase B. It does READ them:
//     CountLocationReferences counts the plaques mounted at a venue, because
//     tags_location_fk is one of the six ON DELETE RESTRICT keys and a manager asking
//     why a venue cannot be removed needs "1 plaque" in the answer. Measured: the
//     count runs as tappa_app under the existing grants, and no plaque field — uid,
//     last_ctr, aes_key_ref — is selected, mapped or rendered anywhere in phase A.
//   - It does NOT speak HTTP and does not render. It returns facts and typed
//     errors; internal/handler decides what a manager is told.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// Audit actions this file writes. The vocabulary is free text by schema decision
// (00005), so these constants are the vocabulary, and they follow the shape every
// other action in the product already uses -- `<subject>.<past participle>`,
// matching employee.deactivated, employee.moved, review.recorded and
// activation.completed rather than inventing a second convention.
//
// ONE ACTION PER ACT, with the before and after facts in `detail`. An
// investigator asking "what has been done to this venue" gets every answer from
// `target = <location id>`, and `action LIKE 'location.%'` is the whole of what
// the panel can do to one.
const (
	ActionLocationCreated   = "location.created"
	ActionLocationUpdated   = "location.updated"
	ActionDepartmentCreated = "department.created"
	ActionDepartmentUpdated = "department.updated"
)

// The refusals a caller must be able to tell apart, because each one is a
// different sentence to a manager (section 4.6: no silent failure).
var (
	// ErrUnknownVenue: no location with that id in this tenant. It covers "does
	// not exist" and "belongs to somebody else" together, deliberately -- the
	// reason ErrUnknownEmployee gives: telling them apart would answer "does this
	// id exist elsewhere?" for anybody who can post a form.
	ErrUnknownVenue = errors.New("tenant: no such venue in this tenant")

	// ErrUnknownDepartment: no department with that id in this tenant.
	ErrUnknownDepartment = errors.New("tenant: no such department in this tenant")

	// ErrVenueName / ErrDepartmentName: the name is empty or longer than the
	// bound. It is a REFUSAL rather than a truncation, and that is the whole
	// difference from the roster's NAME FILTER, which does truncate: cutting a
	// search term only widens a search, whereas cutting a name somebody is SAVING
	// stores a different name than the one they typed and says nothing about it.
	ErrVenueName      = errors.New("tenant: that venue name cannot be stored")
	ErrDepartmentName = errors.New("tenant: that department name cannot be stored")
)

// MaxVenueNameRunes bounds a stored venue or department name.
//
// 🔴 IT EXISTS BECAUSE THE COLUMN DOES NOT BOUND IT. locations.name and
// departments.name are unbounded `text` (migration 00002), which is the same
// shape employees.full_name has -- and a security audit measured what that cost
// there: a 605-rune name on a page boundary served page one forever and made
// everybody behind it unreachable by browsing. These two names are worse placed
// than that one, because they are rendered into the LOCATION and DEPARTMENT
// dropdowns of three other sections' filter bars, so one absurd name degrades
// screens its own section does not own.
//
// 80 IS ABOVE EVERY REAL NAME AND FAR BELOW THE DAMAGE. A venue name is a place staff
// say out loud -- "St Julians", "Rusty Bar", "Kebab Manufacturing HQ" -- and a
// department name is one word more often than not. 80 runes is several times the
// longest of those and small enough that a filter dropdown stays readable.
//
// ⚠️ IT IS NO LONGER ARGUED FROM A MEASURED MAXIMUM, AND THE REASON IS A LESSON THIS
// CHANGE LEARNED TWICE. This comment used to say "the longest location name in the
// whole table is 21 characters". Re-measured on 2026-08-08 it is 80 -- because
// TestVenues_TheNameBoundIsEnforcedBeforeTheColumn, in this very package, stores a
// name of exactly MaxVenueNameRunes as its positive control, and the development
// database is never cleaned. The measurement had become a reading of OUR OWN TEST
// DATA, so quoting it to justify the bound was circular. Department names are still 23
// at their longest, which is the figure that has not moved.
//
// The bound is not written into the schema because phase A adds no migration -- it is
// enforced at the boundary and pinned by a test, and a CHECK constraint would be the
// right home for it the next time this schema is touched.
const MaxVenueNameRunes = 80

// Shift is a wall-clock working window as the panel edits it.
//
// 🔴 A nil *Shift MEANS "LATENESS IS NOT COMPUTED", AND THAT IS THE SINGLE MOST
// DESTRUCTIVE THING THIS FILE COULD GET WRONG. A zero-valued Shift is 00:00-00:00,
// which is a real shift starting at midnight and would make every arrival late
// for that venue forever -- with no error, no failed request and nothing on any
// screen to notice. The schema cannot catch it: locations_shift_pair only refuses
// a HALF-filled pair, and 00:00 is a perfectly good `time`. So the distinction
// lives in the type, and TestVenues_EmptyShiftStoresNULLNotMidnight is what turns
// red if a future boundary starts folding an empty form field into a zero.
//
// Start and End are offsets from LOCAL midnight, the same encoding tap.Shift uses
// and the same one pgx yields for a `time` column (microseconds since midnight).
// There is no Timezone field: the panel edits wall-clock numbers, and resolving
// them against a real zone is the decision engine's job (M4-05).
type Shift struct {
	Start time.Duration
	End   time.Duration
	// Overnight says the end falls on the NEXT day (Rusty Bar 18:00 -> 02:00).
	//
	// IT IS INSIDE THE POINTER RATHER THAN BESIDE IT, even though the column is
	// NOT NULL, so that "overnight with no shift" is not a state this package can
	// express. Clearing a shift therefore also clears overnight. Measured before
	// choosing it (2026-08-08): ZERO locations and ZERO departments in the whole
	// table carry overnight with a NULL shift, so nothing in the database is changed by
	// making the combination unrepresentable.
	Overnight bool
}

// GPS is a venue's registered coordinate.
//
// 🔴 nil MEANS "NO COORDINATE REGISTERED" AND IS NOT THE SAME AS 0,0. The zero
// value is a real point in the Gulf of Guinea, it passes locations_gps_pair, and
// it would put every tap at that venue roughly 5 000 km from where somebody is
// standing -- so the GPS half of proof of place would never match and the venue
// would silently fall to IP-only. Same class of defect as the midnight shift
// above, same answer: the absence is a pointer, and it is tested.
//
// float64 IS THE VALIDATION AND TRANSPORT SHAPE, NOT THE STORAGE SHAPE. The
// column is numeric(9,6) and the value reaches it as a SIX-DECIMAL STRING (see
// numericFrom below), which is the argument internal/domain/checkin already makes
// for the same columns: handing pgx a float leaves the rounding to two layers
// that disagree about it. Three integer digits plus six decimals is nine
// significant digits, well inside float64's exact range, so the round trip is
// lossless for every coordinate the CHECK constraints admit.
type GPS struct {
	Lat float64
	Lng float64
}

// Venue is one location as the panel reads it.
type Venue struct {
	ID   uuid.UUID
	Name string
	// StaticIPs is the IP half of proof of place. An EMPTY slice is meaningful and
	// ordinary: migration 00002 stores '{}' for "no IP evidence configured", and
	// `<<= ANY('{}')` is cleanly FALSE, so such a venue simply never matches on IP.
	StaticIPs []netip.Prefix
	GPS       *GPS
	Shift     *Shift
	// WiFiSSID is the network name the ACTIVATION page asks an employee to join,
	// or "" when there is none.
	//
	// 🔴 IT IS NOT A DECISION INPUT AND MUST NEVER BECOME ONE. Migration 00010
	// argues that at length: a client-reported SSID is a string the client chose,
	// so treating it as evidence would add a proof of place anybody can type.
	// Nothing on the tap path reads it (db/queries/locations.sql states this and
	// GetLocationForTap does not select it). Here it is an ordinary editable text
	// field, and "" is a state rather than an error.
	WiFiSSID        string
	CreatedAt       time.Time
	DepartmentCount int
}

// HasProofOfPlace reports whether this venue can supply either half of section
// 5's row-6 evidence. When it is false EVERY tap there reaches row 7 and is
// recorded as `flag` -- the screen says so rather than leaving a manager to
// discover it from the approval queue.
func (v Venue) HasProofOfPlace() bool {
	return len(v.StaticIPs) > 0 || v.GPS != nil
}

// Department is one department as the panel reads it.
type Department struct {
	ID         uuid.UUID
	LocationID uuid.UUID
	// LocationName is the venue it sits in. It is "" only if the join missed,
	// which location_id being NOT NULL makes a repair problem rather than a state.
	LocationName string
	Name         string
	Shift        *Shift
	CreatedAt    time.Time
}

// VenueScreen is everything the locations section renders in one round trip.
type VenueScreen struct {
	Venues      []Venue
	Departments []Department
	// VenuesCapped / DepartmentsCapped say the ceiling was reached and the list is
	// therefore INCOMPLETE.
	//
	// 🔴 SECTION 4.6: A TRUNCATED LIST THAT DOES NOT SAY SO IS A CLAIM ABOUT THE
	// BUSINESS. The bound exists because an unbounded read is what M6-03 measured
	// at 867 KB and removed; saying nothing when it bites would tell a manager they
	// have 200 venues when they have more.
	VenuesCapped      bool
	DepartmentsCapped bool
}

// venuePageLimit is the ceiling on either list.
//
// 🔴 IT IS A CEILING, NOT A PAGER, AND THE DIFFERENCE IS MEASURED. The employees
// section pages at 50 because a payroll is unbounded and walking one is a design
// promise (rosterDesignCeiling). A venue list is bounded by the buildings a
// business operates in: measured on the development database (2026-08-08), the
// largest SINGLE tenant has 9 locations and the largest has 5 departments, mean
// 1.06 departments per tenant. 200 is far above any hospitality or light-
// manufacturing estate in the two markets this ships to, and a business past it
// is told rather than silently cut.
//
// ⚠️ AN EARLIER DRAFT OF THIS CONSTANT WAS CHOSEN FROM A FIGURE OF 56 466
// LOCATIONS FOR ONE TENANT. That number was wrong and the way it was wrong is
// worth recording: the query grouped by tenant NAME, and the development database
// holds hundreds of distinct tenants sharing fixture names, so it summed a name
// rather than measuring a tenant. Re-measured per tenant_id, the maximum is 9.
const venuePageLimit = 200

// Venues performs the manager's acts on venues and departments. Dependencies are
// injected as struct fields (section 7): no package-level state, no singleton.
type Venues struct {
	data  Database
	trail Trail
	log   *slog.Logger
}

// NewVenues wires it. A nil database or a nil trail is REFUSED rather than
// tolerated, for the reason NewStaff gives: a Venues that cannot write the trail
// would change how taps are judged with no audit row, and `locations` has no
// updated_at to fall back on -- so the change would be invisible everywhere. The
// M5-04 lesson is that a capability can be delivered, approved and DEAD in the
// wired product because two halves were never assembled, so the failure has to be
// impossible to construct rather than merely unlikely.
func NewVenues(data Database, trail Trail, log *slog.Logger) (*Venues, error) {
	switch {
	case data == nil:
		return nil, errors.New("tenant: nil database")
	case trail == nil:
		return nil, errors.New("tenant: nil audit trail")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Venues{data: data, trail: trail, log: log}, nil
}

// Screen reads both lists for the section.
//
// THE TWO READS SHARE ONE TRANSACTION so the departments a manager sees belong to
// the venues listed beside them -- two round trips could straddle another
// manager's create and show a department under a venue that is not on the page.
func (v *Venues) Screen(ctx context.Context, tenantID uuid.UUID) (VenueScreen, error) {
	if tenantID == uuid.Nil {
		return VenueScreen{}, errors.New("tenant: tenant id is required")
	}
	var out VenueScreen
	err := v.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		// THE LIMIT IS ASKED FOR AS limit+1 SO THE CAP CAN BE DETECTED WITHOUT A
		// SECOND COUNT: exactly one extra row means "there is more", and it is
		// dropped before the caller sees it. A COUNT(*) would be a second statement
		// answering a question one row already answers.
		locs, err := q.ListPanelLocations(ctx, store.ListPanelLocationsParams{
			TenantID: tenantID,
			RowLimit: venuePageLimit + 1,
		})
		if err != nil {
			return fmt.Errorf("list venues: %w", err)
		}
		if len(locs) > venuePageLimit {
			out.VenuesCapped = true
			locs = locs[:venuePageLimit]
		}
		out.Venues = make([]Venue, 0, len(locs))
		for _, l := range locs {
			out.Venues = append(out.Venues, Venue{
				ID:              l.ID,
				Name:            l.Name,
				StaticIPs:       l.StaticIps,
				GPS:             gpsOf(l.GpsLat, l.GpsLng),
				Shift:           shiftOf(l.ShiftStart, l.ShiftEnd, l.Overnight),
				WiFiSSID:        deref(l.WifiSsid),
				CreatedAt:       l.CreatedAt,
				DepartmentCount: int(l.DepartmentCount),
			})
		}

		deps, err := q.ListPanelDepartments(ctx, store.ListPanelDepartmentsParams{
			TenantID: tenantID,
			RowLimit: venuePageLimit + 1,
		})
		if err != nil {
			return fmt.Errorf("list departments: %w", err)
		}
		if len(deps) > venuePageLimit {
			out.DepartmentsCapped = true
			deps = deps[:venuePageLimit]
		}
		out.Departments = make([]Department, 0, len(deps))
		for _, d := range deps {
			out.Departments = append(out.Departments, Department{
				ID:           d.ID,
				LocationID:   d.LocationID,
				LocationName: d.LocationName,
				Name:         d.Name,
				Shift:        shiftOf(d.ShiftStart, d.ShiftEnd, d.Overnight),
				CreatedAt:    d.CreatedAt,
			})
		}
		return nil
	})
	if err != nil {
		return VenueScreen{}, fmt.Errorf("tenant: venue screen: %w", err)
	}
	return out, nil
}

// Venue reads ONE venue by id, inside the caller's tenant.
//
// 🔴 IT EXISTS BECAUSE Screen IS CAPPED AND A CAP IS NOT A DISAPPEARANCE. The section
// used to resolve ?venue=<id> by scanning the slice Screen had already TRUNCATED, so
// a venue past venuePageLimit could not be opened at all — and the screen said "that
// venue is not one of this business's", which is FALSE: the row is the tenant's, it
// is in the database, and RLS admits it. §4.6 is that a record is never lost; saying
// something untrue about it is worse than saying nothing. "The list says it was cut"
// and "the cut rows are reachable" are two claims, and only the first was true.
//
// ⚠️ AND THE CEILING IS NOT REACHED TODAY, which is the reason this is a repair
// rather than an emergency. Measured on the development database (2026-08-08): the
// largest single tenant has 9 locations and 5 departments against a ceiling of 200 —
// 22x of headroom, and no existing tenant can trigger it. That orders the work; it
// does not excuse leaving a false sentence in the product.
//
// NO ROW is returned as ErrUnknownVenue, which is the answer that IS true: the id
// belongs to another business or to nothing at all.
func (v *Venues) Venue(ctx context.Context, tenantID, venueID uuid.UUID) (Venue, error) {
	if tenantID == uuid.Nil || venueID == uuid.Nil {
		return Venue{}, ErrUnknownVenue
	}
	var out Venue
	err := v.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetPanelLocation(ctx, store.GetPanelLocationParams{
			TenantID: tenantID,
			ID:       venueID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownVenue
			}
			return fmt.Errorf("load venue: %w", err)
		}
		out = Venue{
			ID:        row.ID,
			Name:      row.Name,
			StaticIPs: row.StaticIps,
			GPS:       gpsOf(row.GpsLat, row.GpsLng),
			Shift:     shiftOf(row.ShiftStart, row.ShiftEnd, row.Overnight),
			WiFiSSID:  deref(row.WifiSsid),
			CreatedAt: row.CreatedAt,
			// DepartmentCount is NOT set: GetPanelLocation does not count, and a zero
			// here would be a NUMBER THAT LOOKS MEASURED. The only caller is the edit
			// form, which does not render it.
		}
		return nil
	})
	if err != nil {
		return Venue{}, wrapVenue("read venue", err)
	}
	return out, nil
}

// Department reads ONE department by id, inside the caller's tenant. Same argument as
// Venue above, and the same shape.
func (v *Venues) Department(ctx context.Context, tenantID, departmentID uuid.UUID) (Department, error) {
	if tenantID == uuid.Nil || departmentID == uuid.Nil {
		return Department{}, ErrUnknownDepartment
	}
	var out Department
	err := v.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetPanelDepartment(ctx, store.GetPanelDepartmentParams{
			TenantID: tenantID,
			ID:       departmentID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownDepartment
			}
			return fmt.Errorf("load department: %w", err)
		}
		out = Department{
			ID:           row.ID,
			LocationID:   row.LocationID,
			LocationName: row.LocationName,
			Name:         row.Name,
			Shift:        shiftOf(row.ShiftStart, row.ShiftEnd, row.Overnight),
			CreatedAt:    row.CreatedAt,
		}
		return nil
	})
	if err != nil {
		return Department{}, wrapVenue("read department", err)
	}
	return out, nil
}

// References is what still points at a venue or a department.
//
// 🔴 IT IS WHAT THE SCREEN SAYS, NOT WHAT ENFORCES ANYTHING. The six ON DELETE
// RESTRICT foreign keys are the guard; this exists so the panel can hide a control it
// knows will fail and say WHY with numbers, rather than offering a button and then
// refusing the press. Between reading this and deleting, another manager can hire
// somebody into the venue — so a delete that fails on a key AFTER this said zero is
// the ordinary race, not a bug, and DeleteVenue turns it into a sentence.
type References struct {
	Departments  int
	Employees    int
	Tags         int
	Transactions int
}

// InUse reports whether anything at all points at the row.
func (r References) InUse() bool {
	return r.Departments > 0 || r.Employees > 0 || r.Tags > 0 || r.Transactions > 0
}

// Total is the number of rows that would have to be dealt with first.
func (r References) Total() int {
	return r.Departments + r.Employees + r.Tags + r.Transactions
}

// ErrVenueInUse / ErrDepartmentInUse: something still points at the row, so the
// database refused to remove it. It is a REFUSAL a manager can act on — the screen
// names what is in the way — and never a 500.
//
// 🔴 A VENUE IN USE CANNOT BE ARCHIVED EITHER, AND THAT IS A RULE RATHER THAN A GAP.
// Neither table has a status, archived or deleted_at column, so there is no retirement
// to offer instead; and §4.3's argument says why that is right rather than
// unfortunate: `transactions` rows point at the venue they happened at, and a mesai
// record that can be orphaned is a record whose evidence can be edited away.
var (
	ErrVenueInUse      = errors.New("tenant: that venue is still in use")
	ErrDepartmentInUse = errors.New("tenant: that department is still in use")
)

// Audit actions for removal. Same vocabulary shape as the four above.
const (
	ActionLocationDeleted   = "location.deleted"
	ActionDepartmentDeleted = "department.deleted"
)

// VenueReferences counts what points at one venue.
func (v *Venues) VenueReferences(ctx context.Context, tenantID, venueID uuid.UUID) (References, error) {
	if tenantID == uuid.Nil || venueID == uuid.Nil {
		return References{}, ErrUnknownVenue
	}
	var out References
	err := v.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).CountLocationReferences(ctx, store.CountLocationReferencesParams{
			TenantID: tenantID, ID: venueID,
		})
		if err != nil {
			return fmt.Errorf("count venue references: %w", err)
		}
		out = References{
			Departments:  int(row.DepartmentCount),
			Employees:    int(row.EmployeeCount),
			Tags:         int(row.TagCount),
			Transactions: int(row.TransactionCount),
		}
		return nil
	})
	if err != nil {
		return References{}, wrapVenue("venue references", err)
	}
	return out, nil
}

// DepartmentReferences counts what points at one department. Two keys, not four:
// departments carry no tags and no locations of their own.
func (v *Venues) DepartmentReferences(ctx context.Context, tenantID, departmentID uuid.UUID) (References, error) {
	if tenantID == uuid.Nil || departmentID == uuid.Nil {
		return References{}, ErrUnknownDepartment
	}
	var out References
	err := v.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).CountDepartmentReferences(ctx, store.CountDepartmentReferencesParams{
			TenantID: tenantID, ID: departmentID,
		})
		if err != nil {
			return fmt.Errorf("count department references: %w", err)
		}
		out = References{
			Employees:    int(row.EmployeeCount),
			Transactions: int(row.TransactionCount),
		}
		return nil
	})
	if err != nil {
		return References{}, wrapVenue("department references", err)
	}
	return out, nil
}

// removalWindow is how long after a removal the panel will still acknowledge it.
//
// 🔴 IT IS THE DATABASE'S CLOCK, NOT THE APP'S AND CERTAINLY NOT THE CLIENT'S. The
// comparison is `at > now() - make_interval(...)` INSIDE the statement, so nothing a
// caller sends can widen it -- ADR 0006's rule about server time satisfied by
// structure rather than by discipline.
//
// 30 SECONDS IS THE GAP BETWEEN A 303 AND THE GET THAT FOLLOWS IT, generously. The
// window is not a security boundary: nothing is authorised on the strength of this
// read, it only decides whether a sentence is printed. What a longer window would buy
// is an acknowledgement surviving a slow reload; what it would cost is a manager
// seeing "removed" again after wandering back to a stale tab.
const removalWindow = 30 * time.Second

// RemovalReceipt is the trail's answer to "did this manager just remove this row?".
type RemovalReceipt struct {
	// Found is false when no such entry exists -- which is the ordinary answer for a
	// replayed URL, a stale bookmark, another business's id or a colleague's removal.
	Found bool
	// Name is what the row was called, read OUT OF THE TRAIL. It is the only surviving
	// copy: the row itself is gone, and the id in the address bar is a client value.
	Name string
}

// ConfirmRemoval reports whether THIS admin, in THIS business, removed THIS row inside
// the window.
//
// 🔴 IT DECIDES A SENTENCE, NOT A PERMISSION. Nothing is authorised by its answer, so
// a failure to read is not a refusal -- the caller renders the list without a banner.
// Reading it as authorisation would be the mistake this comment exists to prevent.
//
// ⚠️ WHAT IT RESTS ON, AND THE ONE HOLE IN IT. The acknowledgement is trustworthy
// because audit_log is append-only: tappa_app holds SELECT and INSERT only, and a
// trigger refuses UPDATE and DELETE even for tappa_owner (migration 00005). A security
// audit measured the gap in that: the trigger is BEFORE UPDATE OR DELETE, so it does
// NOT cover TRUNCATE, and tappa_owner can therefore empty the table wholesale. That is
// 00005's situation rather than this change's, and it is written here because THIS is
// the code that depends on the guarantee -- a wholesale truncate would not forge an
// acknowledgement, it would silence every one of them, which is the failure direction
// this function already treats as "say nothing".
func (v *Venues) ConfirmRemoval(ctx context.Context, tenantID, actorID uuid.UUID, action, target string) (RemovalReceipt, error) {
	if tenantID == uuid.Nil || actorID == uuid.Nil || action == "" || target == "" {
		return RemovalReceipt{}, nil
	}
	var out RemovalReceipt
	err := v.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		name, err := store.New(tx).ConfirmRecentRemoval(ctx, store.ConfirmRecentRemovalParams{
			TenantID:      tenantID,
			ActorID:       actorID,
			Action:        action,
			Target:        target,
			WindowSeconds: int32(removalWindow / time.Second),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// NOT AN ERROR. "No such removal" is the ordinary answer for every
				// address that was not produced by this manager a moment ago.
				return nil
			}
			return fmt.Errorf("confirm removal: %w", err)
		}
		out.Found = true
		out.Name = name
		return nil
	})
	if err != nil {
		return RemovalReceipt{}, wrapVenue("confirm removal", err)
	}
	return out, nil
}

// DeleteCommand removes one venue or one department.
type DeleteCommand struct {
	TenantID uuid.UUID
	ID       uuid.UUID
	// ActorID is the ADMIN performing the act, from the signed panel session.
	ActorID uuid.UUID
}

// deletedDetail is the audit row's payload for a removal.
//
// 🔴 IT CARRIES THE WHOLE ROW, WHICH IS THE OPPOSITE OF EVERY OTHER DETAIL HERE AND
// THE REASON IS THAT THERE IS NOTHING LEFT TO READ. An update's trail row can name an
// id and an investigator can go and look; after a delete the id resolves to nothing,
// forever. So the configuration that produced every verdict at this venue — its
// address ranges, its coordinate, its shift — is recorded at the moment it stops
// existing. §4.6 in its narrowest reading for this task: the row goes, the trace does
// not.
type deletedDetail struct {
	Name string `json:"name"`
	// 🔴 NO omitempty ON ANY EVIDENCE FIELD, AND THAT IS THE WHOLE DIFFERENCE FROM THE
	// TWO DETAILS ABOVE. A security audit measured what omitempty cost here: across 58
	// location.deleted rows, `static_ips` was present on 21 and ABSENT on 37, because
	// ipStrings returns an empty slice for a venue with no ranges and omitempty drops
	// it entirely. Five years on, an investigator asking "did this venue have IP
	// evidence?" cannot tell "no, it had none" from "that version did not record it" —
	// and after a deletion THIS ROW IS THE ONLY SURVIVING RECORD, so there is nothing
	// else to consult. §4.6 asks the opposite: an absence must be recorded as an
	// absence. `"static_ips": []` and `"gps": ""` now say so out loud.
	//
	// ⚠️ venueDetail AND departmentDetail KEEP THEIR omitempty, deliberately. Those
	// describe a row that still EXISTS: an investigator who wants its current state
	// can read the row, and the trail's job there is to record what CHANGED. Here the
	// row is gone and the trail's job is to record what it WAS. Different job, so a
	// different rule — stated rather than left as an inconsistency somebody harmonises
	// by accident.
	StaticIPs []string `json:"static_ips"`
	GPS       string   `json:"gps"`
	Shift     string   `json:"shift"`
	WiFiSSID  string   `json:"wifi_ssid"`
	// CreatedAt is when the row was first registered.
	//
	// 🔴 IT WAS MISSING ENTIRELY, AND THAT MEANT A DELETION DESTROYED IT PERMANENTLY.
	// `locations` has no updated_at, so created_at was the only timestamp the row ever
	// carried; once the row goes there is no other copy anywhere. An investigator
	// reconstructing "how long did this venue operate" needs both ends, and the trail
	// held only the second.
	CreatedAt time.Time `json:"created_at"`
	// LocationID is set for a department, so the trail says which venue it was in. It
	// keeps omitempty because it is not evidence about the removed row's
	// configuration — it is absent for a venue by definition, not by omission.
	LocationID string `json:"location_id,omitempty"`
}

// DeleteVenue removes a venue and records that it did, in ONE transaction.
//
// 🔴 THE FOREIGN KEYS ARE THE GUARD AND THE COUNT IS ONLY THE SCREEN'S. Six ON DELETE
// RESTRICT keys reference these tables. Between the panel counting zero references and
// this statement running, another manager can hire somebody into the venue — Postgres
// then answers 23503 and the row survives. That is the CORRECT outcome, and it is
// translated into ErrVenueInUse rather than escaping as a 500, because the manager can
// act on "somebody was just placed here" and cannot act on a driver error string.
//
// 🔴 THE TRAIL SHARES THE TRANSACTION, so a delete whose audit row fails does not
// happen. That matters more here than anywhere else in this file: an update that lost
// its trail leaves a row somebody can still inspect; a DELETE that lost its trail
// leaves nothing at all.
func (v *Venues) DeleteVenue(ctx context.Context, c DeleteCommand) error {
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return err
	}
	if c.ID == uuid.Nil {
		return ErrUnknownVenue
	}
	err := v.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.DeleteLocation(ctx, store.DeleteLocationParams{
			TenantID: c.TenantID, ID: c.ID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// No row matched the tenant + id pair: another business's, or gone.
				return ErrUnknownVenue
			}
			if isForeignKeyViolation(err) {
				return ErrVenueInUse
			}
			return fmt.Errorf("delete venue: %w", err)
		}
		detail := deletedDetail{
			Name:      row.Name,
			StaticIPs: ipStrings(row.StaticIps),
			GPS:       gpsText(gpsOf(row.GpsLat, row.GpsLng)),
			Shift:     shiftText(shiftOf(row.ShiftStart, row.ShiftEnd, row.Overnight)),
			WiFiSSID:  deref(row.WifiSsid),
			CreatedAt: row.CreatedAt,
		}
		if _, err := v.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionLocationDeleted,
			Target:   c.ID.String(),
			Detail:   detail,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return wrapVenue("delete venue", err)
	}
	v.log.Info("venue deleted", "venue_id", c.ID, "actor_id", c.ActorID)
	return nil
}

// DeleteDepartment removes a department. Same shape, same guarantees.
func (v *Venues) DeleteDepartment(ctx context.Context, c DeleteCommand) error {
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return err
	}
	if c.ID == uuid.Nil {
		return ErrUnknownDepartment
	}
	err := v.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.DeleteDepartment(ctx, store.DeleteDepartmentParams{
			TenantID: c.TenantID, ID: c.ID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownDepartment
			}
			if isForeignKeyViolation(err) {
				return ErrDepartmentInUse
			}
			return fmt.Errorf("delete department: %w", err)
		}
		detail := deletedDetail{
			Name:       row.Name,
			Shift:      shiftText(shiftOf(row.ShiftStart, row.ShiftEnd, row.Overnight)),
			LocationID: row.LocationID.String(),
			CreatedAt:  row.CreatedAt,
			// StaticIPs is written EXPLICITLY EMPTY rather than left nil: a department
			// has no address ranges by schema, and `null` in the trail would read as
			// "not recorded" where `[]` reads as "it had none".
			StaticIPs: []string{},
		}
		if _, err := v.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionDepartmentDeleted,
			Target:   c.ID.String(),
			Detail:   detail,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return wrapVenue("delete department", err)
	}
	v.log.Info("department deleted", "department_id", c.ID, "actor_id", c.ActorID)
	return nil
}

// isForeignKeyViolation reports whether Postgres refused the statement because
// something still references the row (SQLSTATE 23503).
//
// 🔴 IT MATCHES THE CODE, NOT THE MESSAGE. An error STRING is a locale- and
// version-dependent sentence; 23503 is the standard's own class for a foreign key
// violation and is what the six RESTRICT keys raise.
func isForeignKeyViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23503"
}

// VenueCommand is a create or an update. The zero ID means "create".
type VenueCommand struct {
	TenantID uuid.UUID
	// VenueID is the venue being rewritten, or uuid.Nil to add one.
	VenueID uuid.UUID
	// ActorID is the ADMIN performing the act, taken from the signed panel session
	// by the handler -- NEVER from the request. It is required for the reason
	// requireIDs gives: an administrative act with no actor is an unattributable
	// trail row.
	ActorID uuid.UUID

	Name string
	// StaticIPs may be empty, which is a configuration rather than a mistake -- and
	// is the single most consequential thing on this form (see the file header).
	StaticIPs []netip.Prefix
	GPS       *GPS
	Shift     *Shift
	WiFiSSID  string
}

// DepartmentCommand is a create or an update. The zero ID means "create".
type DepartmentCommand struct {
	TenantID     uuid.UUID
	DepartmentID uuid.UUID
	ActorID      uuid.UUID

	// LocationID is the venue the department sits in. It is read ONLY on create:
	// UpdateDepartment does not name location_id, for the reason
	// db/queries/departments.sql gives.
	LocationID uuid.UUID
	Name       string
	Shift      *Shift
}

// venueDetail is the audit row's jsonb payload.
//
// 🔴 IT IS A PURPOSE-BUILT STRUCT WITH KNOWN KEYS, which is the rule
// internal/audit states and the reason section 4.7 survives a jsonb column:
// nothing here is a dump of a request or a map of whatever was in scope, so no
// future edit can add a secret to it by accident.
//
// 🔴 IT RECORDS THE THREE FACTS THAT CHANGE A VERDICT, BEFORE AND AFTER, AND
// NOTHING ELSE OF THE KIND. The static IP RANGES themselves are recorded because
// they ARE the evidence -- an investigator asking "why did every tap at this
// venue start flagging on Tuesday" needs to see that the list went from one entry
// to none, and a bare count would not distinguish "narrowed" from "replaced".
// They are not secrets: section 4.7's list is keys, tokens, CMACs, invitation
// codes and precise GPS of PEOPLE. A venue's own address range is configuration
// the manager typed.
//
// ⚠️ THE VENUE'S COORDINATE IS RECORDED AND A PERSON'S IS NOT, and the difference
// is deliberate rather than an inconsistency. Section 4.2 and 4.7 are about
// tracking PEOPLE: a tap's GPS says where an employee was standing, which is why
// it is never logged. A venue's coordinate is the address of a building the
// business published to its own staff, and it is the value under review when a
// manager asks why proof of place stopped matching.
//
// 🔴 AND THIS RECONCILES A RED LINE WRITTEN IN A MIGRATION THAT CANNOT BE EDITED.
// db/migrations/00005_create_transactions_audit_reviews.sql:228 says, of this very
// column: "section 4.7: detail'a SIR (token/anahtar/tam GPS) YAZILMAZ -- uygulama
// sorumlu." Read as a flat ban on any six-decimal coordinate, this file breaks it: a
// security audit measured 128 of 508 location.created rows carrying a full "to_gps",
// plus 4 of 202 updates and 23 of 58 removals. The migration is APPLIED and CLAUDE.md
// section 3 forbids changing it, so the reconciliation belongs here, where the value
// is written:
//
//	"tam GPS" IN THAT SENTENCE MEANS A PERSON'S POSITION. Its neighbours in the list
//	are a token and a key -- three things whose exposure harms somebody. Section 4.2
//	scopes the whole GPS rule to the tap: "GPS yalnizca tap aninda okunur", with no
//	reading in the background and no perimeter watching. A tap's coordinate is
//	evidence about a HUMAN BEING and is never written anywhere but the transaction
//	row.
//
//	⚠️ THE PARAGRAPH ABOVE DELIBERATELY AVOIDS ONE WORD. scripts/redline-check.sh
//	greps for the English term for perimeter watching as an R2 violation, and it
//	cannot tell a prohibition being QUOTED from a feature being built -- an earlier
//	draft of this comment turned `make audit` red for that reason alone. The right
//	response is to reword the prose, not to teach the scanner an exemption: a
//	red-line net loosened for a comment's convenience is a net loosened.
//
//	A VENUE'S COORDINATE IS CONFIGURATION, AND IT IS ALREADY IN THIS TENANT'S DATA.
//	The same six decimals sit in locations.gps_lat/gps_lng under the same tenant and
//	the same RLS policy, so the trail adds NO new exposure -- it adds the ability to
//	answer "when did this venue's coordinate change, and to what". Process logs still
//	get a boolean (`has_gps`) and never the value; that half of 4.7 is untouched.
//
// If that reading is ever rejected, the fix is to store a coarser value or a hash
// HERE, not to edit 00005.
type venueDetail struct {
	// Name is recorded on both sides because a rename is a change an investigator
	// has to be able to follow -- audit_log rows key on the id, and without the
	// name the trail cannot be read by a person.
	FromName string `json:"from_name,omitempty"`
	ToName   string `json:"to_name"`

	FromStaticIPs []string `json:"from_static_ips,omitempty"`
	ToStaticIPs   []string `json:"to_static_ips"`

	FromGPS string `json:"from_gps,omitempty"`
	ToGPS   string `json:"to_gps"`

	FromShift string `json:"from_shift,omitempty"`
	ToShift   string `json:"to_shift"`

	FromWiFiSSID string `json:"from_wifi_ssid,omitempty"`
	ToWiFiSSID   string `json:"to_wifi_ssid"`

	// LostProofOfPlace is the one DERIVED field, and it earns its place: it is
	// true when the act left the venue with neither static IPs nor GPS, which is
	// the state in which every later tap there is recorded as `flag` (section 5
	// row 7). An investigator should not have to re-derive section 5 from two
	// other fields to find the acts that did this.
	LostProofOfPlace bool `json:"lost_proof_of_place"`
}

// departmentDetail is the department trail's payload. It carries no static IPs
// and no GPS because departments have neither column -- what a department can
// change about a verdict is its SHIFT, which beats its venue's (section 5, Q17).
type departmentDetail struct {
	LocationID string `json:"location_id"`

	FromName string `json:"from_name,omitempty"`
	ToName   string `json:"to_name"`

	FromShift string `json:"from_shift,omitempty"`
	ToShift   string `json:"to_shift"`
}

// SaveVenue adds a venue or rewrites one, and records that it did, in ONE
// transaction.
//
// 🔴 THE TRAIL IS WRITTEN INSIDE THE SAME TRANSACTION, which is the opposite of
// what most callers of internal/audit want and is the choice staff.go documents:
// audit.Record opens its own transaction because the caller who most needs a
// trail row is usually the one whose main transaction failed. Here the two writes
// are one act and either direction is a false statement -- a venue whose proof of
// place changed with no trail row, or a trail row saying it changed when it did
// not.
func (v *Venues) SaveVenue(ctx context.Context, c VenueCommand) (Venue, error) {
	name, err := venueName(c.Name, ErrVenueName)
	if err != nil {
		return Venue{}, err
	}
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return Venue{}, err
	}

	var out Venue
	err = v.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)

		lat, lng, err := gpsParams(c.GPS)
		if err != nil {
			return err
		}
		start, end, overnight := shiftParams(c.Shift)

		var (
			detail venueDetail
			action string
			row    venueRow
		)
		if c.VenueID == uuid.Nil {
			created, err := q.CreateLocation(ctx, store.CreateLocationParams{
				TenantID:   c.TenantID,
				Name:       name,
				StaticIps:  normaliseIPs(c.StaticIPs),
				GpsLat:     lat,
				GpsLng:     lng,
				ShiftStart: start,
				ShiftEnd:   end,
				Overnight:  overnight,
				WifiSsid:   strOrNil(c.WiFiSSID),
			})
			if err != nil {
				// 🔴 NO ROW MEANS THE CALLER'S OWN TENANT ROW WAS NOT VISIBLE, which is
				// not the same as "unknown venue" and must not be reported as one. The
				// statement selects from `tenants` (see db/queries/locations.sql for why),
				// so zero rows here means the resolved session named a tenant that RLS
				// cannot see -- a wiring or data failure, never something a manager did.
				// Collapsing it into a refusal would tell somebody their input was wrong
				// when the product was.
				if errors.Is(err, pgx.ErrNoRows) {
					return fmt.Errorf("create venue: the tenant row is not visible to this session")
				}
				return fmt.Errorf("create venue: %w", err)
			}
			action = ActionLocationCreated
			row = venueRowOfCreate(created)
		} else {
			// THE BEFORE STATE IS READ INSIDE THIS TRANSACTION so the trail describes
			// the row the UPDATE is about to change, not a snapshot taken a round trip
			// earlier -- staff.go's shape, and the reason is the same: audit_log is
			// append-only, so a row describing a state that never existed cannot be
			// corrected afterwards.
			before, err := q.GetPanelLocation(ctx, store.GetPanelLocationParams{
				TenantID: c.TenantID,
				ID:       c.VenueID,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrUnknownVenue
				}
				return fmt.Errorf("load venue: %w", err)
			}
			detail.FromName = before.Name
			detail.FromStaticIPs = ipStrings(before.StaticIps)
			detail.FromGPS = gpsText(gpsOf(before.GpsLat, before.GpsLng))
			detail.FromShift = shiftText(shiftOf(before.ShiftStart, before.ShiftEnd, before.Overnight))
			detail.FromWiFiSSID = deref(before.WifiSsid)

			updated, err := q.UpdateLocation(ctx, store.UpdateLocationParams{
				TenantID:   c.TenantID,
				ID:         c.VenueID,
				Name:       name,
				StaticIps:  normaliseIPs(c.StaticIPs),
				GpsLat:     lat,
				GpsLng:     lng,
				ShiftStart: start,
				ShiftEnd:   end,
				Overnight:  overnight,
				WifiSsid:   strOrNil(c.WiFiSSID),
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					// The row was read a moment ago inside this transaction, so zero rows
					// here means it went away underneath us rather than that it was never
					// this tenant's. Either way the manager is told the venue is gone.
					return ErrUnknownVenue
				}
				return fmt.Errorf("update venue: %w", err)
			}
			action = ActionLocationUpdated
			row = venueRowOfUpdate(updated)
		}

		out = venueOf(row)
		detail.ToName = out.Name
		detail.ToStaticIPs = ipStrings(out.StaticIPs)
		detail.ToGPS = gpsText(out.GPS)
		detail.ToShift = shiftText(out.Shift)
		detail.ToWiFiSSID = out.WiFiSSID
		detail.LostProofOfPlace = !out.HasProofOfPlace()

		if _, err := v.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   action,
			Target:   out.ID.String(),
			Detail:   detail,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Venue{}, wrapVenue("save venue", err)
	}
	// 🔴 THE STATIC IP RANGES ARE NOT LOGGED, only their COUNT. They are fine in
	// audit_log -- a tenant-scoped, access-controlled table -- and process logs have
	// no retention story and no tenant boundary (section 7). The count is what a
	// reader of the log needs anyway: it is the quantity that changes a verdict.
	v.log.Info("venue saved",
		"venue_id", out.ID, "actor_id", c.ActorID,
		"static_ip_ranges", len(out.StaticIPs),
		"has_gps", out.GPS != nil, "has_shift", out.Shift != nil,
		"proof_of_place", out.HasProofOfPlace())
	return out, nil
}

// SaveDepartment adds a department or rewrites one, and records that it did, in
// ONE transaction.
func (v *Venues) SaveDepartment(ctx context.Context, c DepartmentCommand) (Department, error) {
	name, err := venueName(c.Name, ErrDepartmentName)
	if err != nil {
		return Department{}, err
	}
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return Department{}, err
	}
	if c.DepartmentID == uuid.Nil && c.LocationID == uuid.Nil {
		// A department must sit in a venue: departments.location_id is NOT NULL, so
		// there is no "nowhere" to create one in.
		return Department{}, ErrUnknownVenue
	}

	var out Department
	err = v.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		start, end, overnight := shiftParams(c.Shift)

		var (
			detail departmentDetail
			action string
		)
		if c.DepartmentID == uuid.Nil {
			created, err := q.CreateDepartment(ctx, store.CreateDepartmentParams{
				TenantID:   c.TenantID,
				LocationID: c.LocationID,
				Name:       name,
				ShiftStart: start,
				ShiftEnd:   end,
				Overnight:  overnight,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					// The INSERT ... SELECT matched no location inside this tenant, so the
					// venue is foreign or invented. That is a refusal with a sentence, not
					// a 23503 the handler would have to read out of a driver error string.
					return ErrUnknownVenue
				}
				return fmt.Errorf("create department: %w", err)
			}
			action = ActionDepartmentCreated
			out = Department{
				ID:         created.ID,
				LocationID: created.LocationID,
				Name:       created.Name,
				Shift:      shiftOf(created.ShiftStart, created.ShiftEnd, created.Overnight),
				CreatedAt:  created.CreatedAt,
			}
			detail.LocationID = created.LocationID.String()
		} else {
			before, err := q.GetPanelDepartment(ctx, store.GetPanelDepartmentParams{
				TenantID: c.TenantID,
				ID:       c.DepartmentID,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrUnknownDepartment
				}
				return fmt.Errorf("load department: %w", err)
			}
			detail.FromName = before.Name
			detail.FromShift = shiftText(shiftOf(before.ShiftStart, before.ShiftEnd, before.Overnight))
			detail.LocationID = before.LocationID.String()

			updated, err := q.UpdateDepartment(ctx, store.UpdateDepartmentParams{
				TenantID:   c.TenantID,
				ID:         c.DepartmentID,
				Name:       name,
				ShiftStart: start,
				ShiftEnd:   end,
				Overnight:  overnight,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrUnknownDepartment
				}
				return fmt.Errorf("update department: %w", err)
			}
			action = ActionDepartmentUpdated
			out = Department{
				ID:           updated.ID,
				LocationID:   updated.LocationID,
				LocationName: before.LocationName,
				Name:         updated.Name,
				Shift:        shiftOf(updated.ShiftStart, updated.ShiftEnd, updated.Overnight),
				CreatedAt:    updated.CreatedAt,
			}
		}

		detail.ToName = out.Name
		detail.ToShift = shiftText(out.Shift)

		if _, err := v.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   action,
			Target:   out.ID.String(),
			Detail:   detail,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Department{}, wrapVenue("save department", err)
	}
	v.log.Info("department saved",
		"department_id", out.ID, "actor_id", c.ActorID,
		"location_id", out.LocationID, "has_shift", out.Shift != nil)
	return out, nil
}

// --- conversions -----------------------------------------------------------

// venueRow is the shape CreateLocation and UpdateLocation share. sqlc emits two
// distinct row structs for them because their RETURNING lists are separate
// declarations, so one small adapter here is cheaper than two copies of venueOf.
type venueRow struct {
	ID         uuid.UUID
	Name       string
	StaticIps  []netip.Prefix
	GpsLat     pgtype.Numeric
	GpsLng     pgtype.Numeric
	ShiftStart pgtype.Time
	ShiftEnd   pgtype.Time
	Overnight  bool
	WifiSsid   *string
	CreatedAt  time.Time
}

func venueRowOfCreate(r store.CreateLocationRow) venueRow {
	return venueRow{
		ID: r.ID, Name: r.Name, StaticIps: r.StaticIps,
		GpsLat: r.GpsLat, GpsLng: r.GpsLng,
		ShiftStart: r.ShiftStart, ShiftEnd: r.ShiftEnd, Overnight: r.Overnight,
		WifiSsid: r.WifiSsid, CreatedAt: r.CreatedAt,
	}
}

func venueRowOfUpdate(r store.UpdateLocationRow) venueRow {
	return venueRow{
		ID: r.ID, Name: r.Name, StaticIps: r.StaticIps,
		GpsLat: r.GpsLat, GpsLng: r.GpsLng,
		ShiftStart: r.ShiftStart, ShiftEnd: r.ShiftEnd, Overnight: r.Overnight,
		WifiSsid: r.WifiSsid, CreatedAt: r.CreatedAt,
	}
}

func venueOf(r venueRow) Venue {
	return Venue{
		ID:        r.ID,
		Name:      r.Name,
		StaticIPs: r.StaticIps,
		GPS:       gpsOf(r.GpsLat, r.GpsLng),
		Shift:     shiftOf(r.ShiftStart, r.ShiftEnd, r.Overnight),
		WiFiSSID:  deref(r.WifiSsid),
		CreatedAt: r.CreatedAt,
	}
}

// shiftOf turns a stored pair into the domain shape, or nil when there is none.
//
// A HALF-FILLED PAIR IS ALSO nil. locations_shift_pair and departments_shift_pair
// refuse that shape anyway, so this branch is unreachable from the database --
// but guessing an end for a start would invent a wall clock, which is exactly the
// class of silent fabrication the pointer exists to prevent.
func shiftOf(start, end pgtype.Time, overnight bool) *Shift {
	if !start.Valid || !end.Valid {
		return nil
	}
	return &Shift{
		Start:     time.Duration(start.Microseconds) * time.Microsecond,
		End:       time.Duration(end.Microseconds) * time.Microsecond,
		Overnight: overnight,
	}
}

// shiftParams renders a shift for the two `time` columns and the overnight flag.
//
// 🔴 nil PRODUCES INVALID pgtype.Time VALUES -- THAT IS, SQL NULL -- AND NOT
// ZERO. This is the single line the file header is about: a nil shift that
// arrived here as pgtype.Time{Microseconds: 0, Valid: true} would store 00:00 and
// silently make every arrival at that venue late, forever, with nothing on any
// screen to show for it.
func shiftParams(s *Shift) (start, end pgtype.Time, overnight bool) {
	if s == nil {
		return pgtype.Time{}, pgtype.Time{}, false
	}
	return pgtype.Time{Microseconds: int64(s.Start / time.Microsecond), Valid: true},
		pgtype.Time{Microseconds: int64(s.End / time.Microsecond), Valid: true},
		s.Overnight
}

// gpsOf turns a stored coordinate pair into the domain shape, or nil when either
// half is absent.
func gpsOf(lat, lng pgtype.Numeric) *GPS {
	if !lat.Valid || !lng.Valid {
		return nil
	}
	latF, err := lat.Float64Value()
	if err != nil || !latF.Valid {
		return nil
	}
	lngF, err := lng.Float64Value()
	if err != nil || !lngF.Valid {
		return nil
	}
	return &GPS{Lat: latF.Float64, Lng: lngF.Float64}
}

// gpsParams renders a coordinate for the two numeric(9,6) columns.
//
// 🔴 nil PRODUCES SQL NULL AND NOT 0,0, for the reason GPS's own comment gives:
// the zero pair is a real point in the Gulf of Guinea that passes the
// locations_gps_pair CHECK.
func gpsParams(g *GPS) (lat, lng pgtype.Numeric, err error) {
	if g == nil {
		return pgtype.Numeric{}, pgtype.Numeric{}, nil
	}
	if lat, err = numericFrom(g.Lat); err != nil {
		return pgtype.Numeric{}, pgtype.Numeric{}, err
	}
	if lng, err = numericFrom(g.Lng); err != nil {
		return pgtype.Numeric{}, pgtype.Numeric{}, err
	}
	return lat, lng, nil
}

// numericFrom renders one coordinate through a SIX-DECIMAL STRING rather than as
// a float parameter -- the argument internal/domain/checkin makes for the same
// columns: the column has six decimal places, and handing pgx a float64 leaves
// the rounding to two layers that disagree about it.
func numericFrom(v float64) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if err := n.Scan(strconv.FormatFloat(v, 'f', 6, 64)); err != nil {
		return pgtype.Numeric{}, fmt.Errorf("coordinate is not storable: %w", err)
	}
	return n, nil
}

// normaliseIPs guarantees a non-nil slice.
//
// THE COLUMN IS NOT NULL DEFAULT '{}' AND A nil SLICE WOULD BE A NULL PARAMETER,
// which the column refuses -- so "the manager cleared every range" would be a 500
// instead of the configuration change it is. An empty slice is the schema's own
// spelling of "no IP evidence" (migration 00002 argues why it is not NULL).
func normaliseIPs(in []netip.Prefix) []netip.Prefix {
	if in == nil {
		return []netip.Prefix{}
	}
	return in
}

// ipStrings renders the ranges for the audit row.
func ipStrings(in []netip.Prefix) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		out = append(out, p.String())
	}
	return out
}

// gpsText renders a coordinate for the audit row, or "" for none. It is TEXT
// rather than two numbers so that "no coordinate" is one unambiguous value in the
// trail rather than a pair of nulls a reader has to interpret.
func gpsText(g *GPS) string {
	if g == nil {
		return ""
	}
	return strconv.FormatFloat(g.Lat, 'f', 6, 64) + "," + strconv.FormatFloat(g.Lng, 'f', 6, 64)
}

// shiftText renders a shift for the audit row, or "" for none -- the same
// argument as gpsText, and the same reason it matters here: "" in the trail means
// LATENESS IS NOT COMPUTED, and it must not be confusable with "00:00-00:00",
// which is a real shift.
func shiftText(s *Shift) string {
	if s == nil {
		return ""
	}
	out := clockText(s.Start) + "-" + clockText(s.End)
	if s.Overnight {
		out += " (+1d)"
	}
	return out
}

// clockText renders an offset-from-midnight as a wall clock.
func clockText(d time.Duration) string {
	m := int(d / time.Minute)
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

// venueName validates a name that is about to be STORED (section 7: the boundary
// validates, but this is the last gate before the column and the column has no
// bound of its own).
//
// 🔴 IT REFUSES RATHER THAN TRUNCATES, WHICH IS THE OPPOSITE OF WHAT THE ROSTER'S
// NAME FILTER DOES, and the difference is the point. Cutting a SEARCH term only
// widens a search; cutting a name somebody is SAVING stores a name they did not
// type and tells them nothing. Section 4.6 asks for the refusal.
//
// NUL IS REMOVED FIRST. PostgreSQL text cannot hold a zero byte -- it answers
// "invalid byte sequence for encoding UTF8" and the request becomes a 500, which
// was measured on the roster's filter box and is the same driver here.
func venueName(raw string, sentinel error) (string, error) {
	s := strings.ReplaceAll(raw, "\x00", "")
	s = strings.ToValidUTF8(s, "")
	s = strings.TrimSpace(s)
	if s == "" {
		return "", sentinel
	}
	// COUNTED IN RUNES, NOT BYTES. Maltese names carry ċ ġ ħ ż and the brand ships
	// a latin-ext subset precisely because venue names are not ASCII, so a byte
	// bound would refuse ordinary names at four fifths of the stated length.
	if utf8.RuneCountInString(s) > MaxVenueNameRunes {
		return "", sentinel
	}
	return s, nil
}

// requireActor refuses the zero value for the two ids every command needs. THE
// ACTOR IS AS REQUIRED AS THE TENANT: a command with no actor would write an
// audit row with a NULL actor_id, which the schema permits (00005: the column is
// polymorphic, for system events) and which would be a lie here -- every act this
// file performs is performed BY somebody.
func requireActor(tenantID, actorID uuid.UUID) error {
	switch {
	case tenantID == uuid.Nil:
		return errors.New("tenant: tenant id is required")
	case actorID == uuid.Nil:
		return errors.New("tenant: actor id is required")
	}
	return nil
}

// strOrNil maps "" onto SQL NULL. "" IS NOT A VALID wifi_ssid: migration 00010's
// locations_wifi_ssid_len CHECK requires 1..32 bytes, so an empty box has to
// become NULL ("no network to show") rather than an empty string, which would be
// a 23514 the manager sees as a 500.
func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// wrapVenue keeps the sentinels unwrapped-by-name so a caller can switch on them,
// and wraps everything else so a real failure reaches the handler as a failure.
func wrapVenue(op string, err error) error {
	switch {
	case errors.Is(err, ErrUnknownVenue),
		errors.Is(err, ErrUnknownDepartment),
		errors.Is(err, ErrVenueName),
		errors.Is(err, ErrDepartmentName):
		return err
	default:
		return fmt.Errorf("tenant: %s: %w", op, err)
	}
}
