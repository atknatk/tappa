package tenant

// staff.go -- the WRITE side of the employee lifecycle: putting somebody on the
// roster (M6-13), ending their employment, and moving them between venues or
// departments (both M6-05 phase B).
//
// 🔴 THE FIRST OF THOSE THREE ARRIVED LAST, AND THE GAP WAS FOUND IN LIVE
// PRODUCTION. Until M6-13 this package could deactivate and move employees that
// nothing in the product could create: `CreateEmployee`/`AddEmployee` appeared ZERO
// times outside internal/store, and the only thing in the repository that made an
// employee row was test/fixtures/seed.sql. A customer signed up, reached the panel
// and asked how to add somebody. They could not. It is this repository's signature
// defect class wearing product clothes -- A SCREEN THAT MANAGES SOMETHING THAT
// CANNOT BE BROUGHT INTO EXISTENCE -- and neither of the two audit rounds over
// M6-05 and M6-06 saw it, because no card had asked for creation and a review
// checks what a card asked for.
//
// 🔴 WHY IT IS IN THIS PACKAGE. CLAUDE.md section 3 assigns "tenant / lokasyon /
// departman / calisan is kurallari" to internal/domain/tenant, and this file is the
// first thing that makes that sentence true for employees. The two alternatives
// were weighed rather than dismissed:
//
//	internal/domain/ledger   already holds the roster READ and already has the
//	                         section 4.5 scope net, so putting the writes there
//	                         would have cost nothing to build -- and would have
//	                         destroyed the one property that package advertises:
//	                         "no store call in ledger is not a SELECT", which is a
//	                         fact grep can check. roster.go says the write path does
//	                         not live there, and it is right.
//	a NEW package            starts with no scope net either way, so it buys
//	                         nothing this one does not.
//
// The cost of choosing here is a THIRD copy of the scope net (query_test.go), and
// that cost is real: this repository has already paid for the "second
// representation" failure class more than once. It is written down there rather
// than smuggled, together with what this copy does NOT carry.
//
// 🔴 WHAT THIS FILE DOES NOT DO, in the order somebody would expect it to:
//
//   - It does NOT revoke sessions when it deactivates somebody. That is a user
//     decision (2026-08-07) and db/queries/sessions.sql carries the argument at
//     RevokeSessionsForEmployee. Deactivate below states it again at the point a
//     future reader would add the call.
//   - It does NOT reactivate. There is no query for it and no method here; see
//     docs/adr/0010.
//   - It does NOT issue invitations. Minting a code is internal/invite's, and the
//     code leaves that package through exactly one seam (its Channel). A copy of
//     that flow here would be a second egress for a section 4.7 secret.
//   - It does NOT speak HTTP and does not render. It returns facts and typed
//     errors; internal/handler decides what a manager is told.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// Trail is the slice of internal/audit this file needs, declared HERE at the
// consumer (section 7).
//
// 🔴 IT IS RecordTx AND NOT Record, WHICH IS THE OPPOSITE OF WHAT MOST CALLERS OF
// THAT PACKAGE WANT, and internal/domain/review states the same reasoning for the
// same reason. audit.Record opens its OWN transaction because the caller who most
// needs a trail row is usually the one whose main transaction FAILED. Here the two
// writes are one act and either direction is a false statement:
//
//	employee changed, no audit row   an administrative act with no trail -- the
//	                                 half of section 4.6 this section exists to
//	                                 provide. The card's criterion is "every action
//	                                 is written to audit_log", and a row that can be
//	                                 lost independently does not satisfy it.
//	audit row, employee unchanged    the trail says somebody was deactivated while
//	                                 their taps still succeed. A trail that says
//	                                 something untrue is worse than no trail.
//
// Both directions are measured in staff_db_test.go by breaking each side in turn
// and counting rows.
type Trail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
}

// Audit actions this file writes. The vocabulary is free text by schema decision
// (00005), so these constants are the vocabulary.
//
// ONE ACTION PER TRANSITION, with the FROM and TO facts in `detail`. An
// investigator asking "what has been done to this person" gets every answer from
// `target = <employee id>`, and `action LIKE 'employee.%'` is the whole of what the
// panel can do to somebody.
//
// ⚠️ THE INVITE ACTION IS NOT HERE, and its absence is deliberate rather than an
// omission: issuing an invitation writes invite.ActionCodeShownToManager
// ("invite.code_shown_to_manager") from internal/invite's Channel, which is the
// moment worth recording -- ADR 0005 Y-D's detection signal is who READ a code, not
// who created a row. Adding a second "employee.invited" row for the same act would
// double-count it and give an investigator two half-answers instead of one.
const (
	ActionEmployeeAdded       = "employee.added"
	ActionEmployeeDeactivated = "employee.deactivated"
	ActionEmployeeMoved       = "employee.moved"
)

// The refusals a caller must be able to tell apart, because each one is a
// different sentence to a manager (section 4.6: no silent failure).
var (
	// ErrUnknownEmployee: no employee with that id in this tenant. It covers "does
	// not exist" and "belongs to somebody else" together, deliberately -- telling
	// them apart would answer "does this id exist elsewhere?" for anybody who can
	// post a form, which is the enumeration oracle the sign-in flow refuses to be.
	ErrUnknownEmployee = errors.New("tenant: no such employee in this tenant")

	// ErrAlreadyDeactivated: the person is already deactivated, so nothing was
	// written and no trail row was added. A double submission lands here.
	ErrAlreadyDeactivated = errors.New("tenant: this employee is already deactivated")

	// ErrUnknownPlacement: the requested placement cannot be written. THREE causes
	// collapse into it and the collapse is deliberate, for the reason
	// ErrUnknownEmployee gives: the venue is not this tenant's, the department is not
	// this tenant's, or -- the one a security audit found being accepted silently --
	// the department belongs to a DIFFERENT VENUE of this same tenant.
	//
	// 🔴 THE THIRD IS SECTION 5 RATHER THAN SECTION 4.5, and it is the one with teeth:
	// lateness is computed from the department's shift when there is one, and policies
	// can be scoped per department, so a mismatched pair judges somebody against
	// another branch's shift and another branch's rules. The screen names both
	// readings because the panel's dropdown offers every department in the business.
	ErrUnknownPlacement = errors.New("tenant: that venue and department cannot be paired in this tenant")

	// ErrEmployeeName: the name is empty once cleaned, or longer than the bound. It
	// is a REFUSAL rather than a truncation, for the reason ErrVenueName gives:
	// cutting a name somebody is SAVING stores a different name than the one they
	// typed and says nothing about it.
	ErrEmployeeName = errors.New("tenant: that name cannot be stored")

	// ErrEmployeeEmail: the address is unstorable or does not have the shape of one.
	// An EMPTY address is not this error -- it is the ordinary "no address" state.
	ErrEmployeeEmail = errors.New("tenant: that email address cannot be stored")

	// ErrEmployeeRole: the job title is unstorable or past the bound. Like the email
	// it is OPTIONAL, so empty is not this error.
	ErrEmployeeRole = errors.New("tenant: that role cannot be stored")

	// ErrEmailTaken: somebody in THIS business already holds that address.
	//
	// 🔴 IT IS A SENTENCE, NOT A 500, AND CASE IS WHY IT SURPRISES PEOPLE. The column
	// is `citext` and the index is UNIQUE (tenant_id, email) WHERE email IS NOT NULL,
	// so `A@x.mt` and `a@x.mt` are THE SAME ROW -- a manager who "changed the
	// capitalisation to make it different" gets this, and the screen has to say why
	// rather than blaming their submission. It is tenant-scoped: the same person may
	// work for two businesses on this platform, and this must never answer "that
	// address exists" about somebody else's payroll.
	ErrEmailTaken = errors.New("tenant: somebody in this business already has that email address")

	// ErrSamePlacement: the requested placement is the one the person already has.
	//
	// 🔴 IT IS A REFUSAL RATHER THAN A NO-OP SUCCESS, and the difference is the
	// trail. Writing the row anyway would put "moved from A to A" in an append-only
	// table on every double submission and every stray refresh, which is how a trail
	// stops being readable. Telling the manager "that is where they already work" is
	// also the true sentence, and section 4.6 asks for the true one.
	ErrSamePlacement = errors.New("tenant: that is already this employee's placement")
)

// Person is one employee as the panel's action card reads them.
//
// 🔴 THE FIELDS THAT ARE NOT HERE ARE THE POINT (section 4.7), exactly as on
// ledger.Person: there is no Email, no invitation code, no code hash, no session
// token and no device label. The query does not select them, this struct has no
// field for them, and the view model above it has none either -- three independent
// walls.
type Person struct {
	ID   uuid.UUID
	Name string
	// Status is one of the three values migration 00003's CHECK admits. It is never
	// defaulted here: an empty string would mean the query stopped selecting it.
	Status     string
	LocationID uuid.UUID
	// DepartmentID is nil for a business that does not model departments (00003) --
	// a first-class state rather than a gap.
	DepartmentID *uuid.UUID
	// LocationName and DepartmentName are "" when there is no such name.
	// location_id is NOT NULL, so an empty LocationName means a join missed rather
	// than "no venue", and the screen says so in words.
	LocationName   string
	DepartmentName string
}

// Deactivated reports whether this person's taps are already being refused.
func (p Person) Deactivated() bool { return p.Status == statusDeactivated }

// Invitable reports whether an activation link would be usable by this person.
//
// IT IS THE SAME SET db/queries/invites.sql's consuming statement tests
// ('invited' or 'active'), spelled once here so the button and the database agree.
// An invitation minted for a deactivated employee would be a code that can never be
// spent -- the consuming statement refuses it -- so offering the control would be
// offering something the server cannot do.
func (p Person) Invitable() bool {
	return p.Status == statusInvited || p.Status == statusActive
}

// The lifecycle vocabulary migration 00003's CHECK admits. internal/domain/ledger
// exports the same three for the panel's dropdown; they are spelled again here
// rather than imported because a domain package importing another domain package
// for three strings is a dependency the two do not otherwise have.
const (
	statusInvited     = "invited"
	statusActive      = "active"
	statusDeactivated = "deactivated"
)

// The bounds Add enforces on the three free-text columns.
//
// 🔴 THEY EXIST BECAUSE THE COLUMNS DO NOT BOUND THEMSELVES. full_name, role and
// email are unbounded `text`/`citext` in migration 00003, and this repository has
// already MEASURED what that costs on this exact column: a security audit found a
// 605-rune name on a roster page boundary serving page one forever and making
// everybody behind it unreachable by browsing. The venue and department names got a
// CHECK constraint for the same reason (migration 00013).
//
// ⚠️ AND THE SCHEMA DID NOT GET ONE HERE, WHICH IS A COUNTED LIMIT RATHER THAN AN
// OVERSIGHT. employees.full_name has no CHECK, so rows longer than this bound already
// exist and a constraint would not validate without a decision about them -- a data
// question and a migration task, not this card's. What that means precisely, said
// plainly: THIS BOUND BINDS NEW ROWS ONLY, and a row that predates it stays exactly as
// long as it is. The boundary is the only guard, so it is the thing that has to be
// tested -- TestPersonName_TheBoundIsExactlyOneHundredAndTwentyRunes does that with a
// literal, because an assertion written as MaxEmployeeNameRunes+1 slides along with
// the constant and measures nothing about its value.
//
// 🔴 THE JUSTIFICATION IS A MECHANISM, NOT A ROW COUNT, AND THAT IS A CORRECTION. This
// comment used to carry four figures from the development database ("longest 616, 352
// rows past 120, longest role 17, mean 19.78"). An audit re-measured them the same day
// and every one had moved -- 1200, 404, 80, 19.88 -- because THE SUITE IS THE THING
// WRITING THOSE ROWS: employees cannot be deleted (00003 revokes DELETE), every run
// hires more, and the extremes are this task's own fixtures (1200 is round 1's mutated
// bound, 80 is MaxEmployeeRoleRunes). A number that a test run changes is not evidence
// for a constant; it is a second representation with a clock on it.
//
// WHAT DOES NOT MOVE, and is the actual argument:
//
//	the column is unbounded `text`, so without this boundary there is NO ceiling;
//	a roster pages at 50 and orders by full_name, so one absurd name occupies a page
//	  boundary and makes everybody behind it unreachable by browsing -- the defect a
//	  security audit measured on THIS column;
//	120 is the bound internal/domain/signup already puts on a person's name, so the
//	  two screens that take one agree;
//	and real names are an order of magnitude shorter than that in any script.
//
// Anybody wanting today's distribution should ask the database rather than this
// comment, and should expect the answer to differ from the last person's.
// COUNTED IN RUNES, NOT BYTES -- venueName's argument. Maltese names carry ċ ġ ħ ż
// and the brand ships a latin-ext subset precisely because staff names are not
// ASCII, so a byte bound would refuse ordinary names at four fifths of its length.
const (
	MaxEmployeeNameRunes  = 120
	MaxEmployeeEmailRunes = 254
	MaxEmployeeRoleRunes  = 80
)

// Staff performs the manager's actions on one employee. Dependencies are injected
// as struct fields (section 7): no package-level state, no singleton.
type Staff struct {
	data  Database
	trail Trail
	log   *slog.Logger
}

// NewStaff wires it. A nil database or a nil trail is REFUSED rather than
// tolerated: a Staff that cannot write the trail would deactivate people with no
// audit row, which is the state the card's criterion exists to prevent. The M5-04
// lesson is that a capability can be delivered, approved and DEAD in the wired
// product because two halves were never assembled, so the failure has to be
// impossible to construct rather than merely unlikely.
func NewStaff(data Database, trail Trail, log *slog.Logger) (*Staff, error) {
	switch {
	case data == nil:
		return nil, errors.New("tenant: nil database")
	case trail == nil:
		return nil, errors.New("tenant: nil audit trail")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Staff{data: data, trail: trail, log: log}, nil
}

// Person loads one employee for the action card.
//
// pgx.ErrNoRows becomes ErrUnknownEmployee: a foreign or invented id is "not on
// this roster", never a database failure and never another tenant's person.
func (s *Staff) Person(ctx context.Context, tenantID, employeeID uuid.UUID) (Person, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil {
		return Person{}, ErrUnknownEmployee
	}
	var out Person
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetPanelEmployeeForAction(ctx, store.GetPanelEmployeeForActionParams{
			TenantID: tenantID,
			ID:       employeeID,
		})
		if err != nil {
			return err
		}
		out = person(row)
		return nil
	})
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, pgx.ErrNoRows):
		return Person{}, ErrUnknownEmployee
	default:
		return Person{}, fmt.Errorf("tenant: load employee: %w", err)
	}
}

// AddCommand is a new person, as a manager typed them (M6-13).
//
// THE STRINGS ARE RAW. Cleaning happens in Add rather than at the handler, so every
// caller gets the same trimming, the same bounds and the same ""-means-absent rule --
// a second copy at the boundary is how a POST comes to accept what another path
// rejects.
type AddCommand struct {
	TenantID uuid.UUID
	// ActorID is the ADMIN doing the adding, from the signed panel session and NEVER
	// from the request (requireActor's argument).
	ActorID uuid.UUID

	// FullName is the only text that is REQUIRED, because full_name is the only one
	// of the three that is NOT NULL.
	FullName string
	// Role and Email are optional. EMPTY MEANS ABSENT for both, and for Email that
	// is not a convenience -- see optionalEmail, where it is the difference between
	// a legal row and a unique-index collision.
	Role  string
	Email string

	// LocationID is REQUIRED: employees.location_id is NOT NULL (00003), so there is
	// no "nowhere" to hire somebody into. A tenant with no venues at all therefore
	// cannot add anybody, and the SCREEN has to say that rather than offering an
	// empty picker -- Add refuses it here as ErrUnknownPlacement either way.
	LocationID uuid.UUID
	// DepartmentID is nil for "no department", which is a first-class placement
	// (00003: a bar does not model departments) rather than a missing argument.
	DepartmentID *uuid.UUID
}

// addDetail is the audit row's jsonb payload for a hire.
//
// 🔴 IT CARRIES NO NAME AND NO EMAIL, which is deactivateDetail's rule applied to
// the one act where the temptation is strongest. The row already names the employee
// as its `target`, and audit_log is APPEND-ONLY: a person's name or address written
// into a jsonb column is a second copy of personal data that nothing can ever
// correct, redact or erase -- and a GDPR erasure (Q13) is an UPDATE over `employees`
// that would not reach it. The booleans answer the only question an investigator
// actually has ("was this person created with an address, and could they therefore
// be emailed a link") without storing the address to answer it.
type addDetail struct {
	LocationID   string `json:"location_id"`
	DepartmentID string `json:"department_id"`
	HasEmail     bool   `json:"has_email"`
	HasRole      bool   `json:"has_role"`
	// Status is what the row was BORN with, read back from the database rather than
	// assumed. It is the whole point of recording this act: 'invited' means the
	// person still has to activate, and anything else here would be evidence that the
	// statement started naming a column it must not (see db/queries/employees.sql).
	Status string `json:"status"`
}

// Add puts somebody on the roster and records that it did, in ONE transaction.
//
// 🔴 THEY ARE BORN 'invited' AND THIS FUNCTION NEVER SAYS SO. The value comes from
// the column's DEFAULT, because CreateEmployee does not name `status` at all -- the
// argument is in db/queries/employees.sql and the short form is that a literal here
// or there is a second place the starting status is decided. What matters to §5 is
// what 'invited' MEANS: the person has no session and no way to tap. Activation is
// a separate, deliberate act that spends an invite (M5-02 binding decision 2), and
// the practice-tap and session chain hangs off it. Creating somebody 'active' would
// skip all of that and leave a person the decision engine believes is set up to tap
// with nothing to tap from.
//
// 🔴 AND THAT IS EXACTLY WHY THE ADD -> INVITE CHAIN CLOSES. Person.Invitable()
// admits 'invited', and db/queries/invites.sql's consuming statement admits the same
// set, so somebody added here can be sent an activation link IMMEDIATELY -- no
// second state to reach first, and no `not-invitable` refusal. That is asserted end
// to end rather than reasoned about: TestStaffDB_AddedPeopleAreImmediatelyInvitable.
//
// THE TRAIL SHARES THE TRANSACTION, Trail's rule: a hire with no audit row and an
// audit row with no hire are both false statements, and audit_log cannot be
// corrected afterwards.
func (s *Staff) Add(ctx context.Context, c AddCommand) (Person, error) {
	// 🔴 THE ORDER IS THE SCREEN'S ORDER, AND IT IS DELIBERATE. The manager reads
	// name, then role, then email, then placement; reporting the first fault in that
	// order means the message points at the field their eye is already on. Reporting
	// the LAST one instead is how a form tells somebody their venue is wrong when
	// what they actually mistyped was the name.
	name, err := personName(c.FullName)
	if err != nil {
		return Person{}, err
	}
	role, err := optionalRole(c.Role)
	if err != nil {
		return Person{}, err
	}
	email, err := optionalEmail(c.Email)
	if err != nil {
		return Person{}, err
	}
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return Person{}, err
	}
	// A ZERO VENUE IS REFUSED HERE RATHER THAN SENT TO THE DATABASE, where it would
	// match no location and come back as the same sentinel anyway. Doing it early
	// keeps "the manager picked nothing" and "the manager picked something that is
	// not ours" as ONE answer, which is what the screen says: the two are the same
	// instruction ("choose a venue that belongs to this business").
	if c.LocationID == uuid.Nil {
		return Person{}, ErrUnknownPlacement
	}
	if c.DepartmentID != nil && *c.DepartmentID == uuid.Nil {
		// A zero uuid is not "no department" -- nil is. MoveCommand.validate refuses
		// the same shape for the same reason: one absence, one representation.
		return Person{}, ErrUnknownPlacement
	}

	var out Person
	err = s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.CreateEmployee(ctx, store.CreateEmployeeParams{
			TenantID:     c.TenantID,
			LocationID:   c.LocationID,
			DepartmentID: c.DepartmentID,
			FullName:     name,
			Role:         role,
			Email:        email,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// The INSERT ... SELECT matched no location inside this tenant, so the
				// venue is foreign, invented or gone -- or the department did not resolve
				// to one inside the chosen venue. CreateDepartment's shape: a refusal with
				// a sentence rather than a 23503 the handler reads out of a driver string.
				return ErrUnknownPlacement
			}
			if isEmailTaken(err) {
				return ErrEmailTaken
			}
			if isForeignKeyViolation(err) {
				// 🔴 THE COMPOSITE KEYS ANSWERING RATHER THAN THE PREDICATE. Reaching here
				// means the SELECT admitted the pair and the KEY refused it, which is the
				// race (a venue removed between the two) or a defect in the predicate
				// above. Either way it is the manager's placement that cannot be written,
				// so they are told that -- not shown a 500 -- and §4.5 held either way.
				return ErrUnknownPlacement
			}
			return fmt.Errorf("create employee: %w", err)
		}
		out = Person{
			ID:           row.ID,
			Name:         row.FullName,
			Status:       row.Status,
			LocationID:   row.LocationID,
			DepartmentID: row.DepartmentID,
		}
		if _, err := s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionEmployeeAdded,
			Target:   row.ID.String(),
			Detail: addDetail{
				LocationID:   row.LocationID.String(),
				DepartmentID: idText(row.DepartmentID),
				HasEmail:     email != nil,
				HasRole:      role != nil,
				Status:       row.Status,
			},
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Person{}, wrap("add", err)
	}
	// 🔴 NEITHER THE NAME NOR THE ADDRESS IS LOGGED, only whether there was one.
	// Employee ids are opaque handles; a name or an email in a process log is
	// personal data in a place with no retention story and no tenant boundary
	// (§4.7, §7). The booleans are what a reader of the log needs anyway.
	s.log.Info("employee added",
		"employee_id", out.ID, "actor_id", c.ActorID,
		"location_id", out.LocationID,
		"has_email", email != nil, "status", out.Status)
	return out, nil
}

// personName cleans and bounds a stored employee name.
//
// IT IS venueName's SHAPE WITH ONE ADDITION -- interior whitespace is COLLAPSED, not
// merely trimmed. "Maria   Borg" and "Maria Borg" are the same person typed twice,
// and storing both makes a roster that looks duplicated and sorts oddly;
// internal/domain/signup does the same to the operator's own name. A name that is
// nothing BUT whitespace collapses to "" and is refused, which is the degenerate
// input this bound exists for.
func personName(raw string) (string, error) {
	s := strings.ReplaceAll(raw, "\x00", "")
	// ToValidUTF8 BEFORE counting: Postgres refuses invalid UTF-8 outright (SQLSTATE
	// 22021), so sending it would be a 500 on a path a manager can reach by pasting
	// from the wrong application. internal/domain/signup closed the same two byte
	// classes on its own write path after shipping it open.
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(dropFormattingArtefact, s)
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "", ErrEmployeeName
	}
	// 🔴 A NAME MADE ENTIRELY OF INVISIBLE RUNES IS NOT A NAME, AND strings.Fields
	// CANNOT SEE IT. Fields splits on unicode.IsSpace, which correctly refuses NBSP and
	// tab -- and says nothing about the much larger class of runes that occupy no
	// visible space and are NOT spaces. An audit measured eight of them being accepted
	// and stored; two more were found while fixing it. The consequence is specific:
	// the roster is ORDER BY full_name ASC, so a blank-looking name sorts to the very
	// top of the list, and two of them are indistinguishable from each other.
	if !hasVisibleRune(s) {
		return "", ErrEmployeeName
	}
	// 🔴 BIDI OVERRIDES ARE REFUSED RATHER THAN STRIPPED, because what they do is not
	// local to the name: U+202E reverses the visual order of everything after it, so a
	// single one in a roster cell can rewrite how the REST OF THE ROW reads. Removing
	// it silently would store a different string than the manager typed (the rule
	// optionalEmail states for case), so this is a refusal with a sentence.
	if containsBidiControl(s) {
		return "", ErrEmployeeName
	}
	if utf8.RuneCountInString(s) > MaxEmployeeNameRunes {
		return "", ErrEmployeeName
	}
	return s, nil
}

// dropFormattingArtefact removes the three runes that are pure typography artefacts
// in a name -- they carry no meaning, survive copy-paste from documents and mail
// clients, and make two identical-looking names into two different rows.
//
// ⚠️ IT DELIBERATELY DOES NOT TOUCH U+200C / U+200D (zero-width non-joiner and
// joiner). Those ARE semantically load-bearing: ZWJ builds emoji sequences and both
// control conjunct formation in Indic scripts, so stripping them would corrupt real
// names in exactly the scripts this product least wants to break. A name made ONLY of
// them still fails the visibility test above; one inside an otherwise visible name is
// left as typed.
func dropFormattingArtefact(r rune) rune {
	switch r {
	case '\u00ad', // SOFT HYPHEN
		'\u200b', // ZERO WIDTH SPACE
		'\ufeff': // ZERO WIDTH NO-BREAK SPACE / BOM
		return -1
	}
	return r
}

// hasVisibleRune reports whether s contains at least one rune that actually puts ink
// on the screen.
//
// 🔴 IT TAKES THREE TESTS AND NOT ONE, BECAUSE NO SINGLE PREDICATE COVERS THE
// CLASS -- measured in both directions, which is the only reason to carry three:
//
//	unicode categories   format, control, separator and non-spacing-mark runes (ZWJ,
//	                     bidi marks, NBSP, combining accents).
//	!unicode.IsGraphic   the UNASSIGNED class, and nothing else here reaches it.
//	                     Measured: U+2065 and U+FFF0 are unassigned, render blank, and
//	                     were accepted and stored until round 6. IsGraphic is false for
//	                     both -- and for private-use and surrogate runes with them, so
//	                     one predicate closes an open-ended class rather than the two
//	                     runes an audit happened to find.
//	the enumerated set   the runes that are ASSIGNED, GRAPHIC and still render as
//	                     nothing. Measured: U+3164 HANGUL FILLER is a LETTER with
//	                     IsGraphic TRUE; U+2800 BRAILLE PATTERN BLANK is a SYMBOL with
//	                     IsGraphic TRUE. IsGraphic admits both, so deleting this list
//	                     re-opens the two cases that motivated the function.
//
// ⚠️ SO THE ENUMERATION IS A COUNTED LIMIT AND THE OTHER TWO ARE RULES. Only the
// enumeration needs extending when a new blank-rendering ASSIGNED rune appears;
// Unicode does not mark "renders blank" as a property, so that part cannot be derived.
// An earlier version of this comment justified the WHOLE function that way, which was
// true of the two runes it named and false of the unassigned class beside them.
//
// TestPersonName_RefusesNamesThatRenderBlank is the inventory, and
// TestPersonName_AcceptsRealNamesIncludingTheAwkwardOnes is what stops any of the
// three from growing teeth on legitimate names.
func hasVisibleRune(s string) bool {
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Cf, r), // format (ZWJ, ZWNJ, bidi marks, ...)
			unicode.Is(unicode.Cc, r), // control
			unicode.Is(unicode.Zs, r), unicode.Is(unicode.Zl, r), unicode.Is(unicode.Zp, r),
			unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r): // marks with no advance
			continue
		case !unicode.IsGraphic(r): // unassigned, private use, surrogate
			continue
		case r == '\u3164', r == '\u2800', r == '\u115f', r == '\u1160', r == '\uffa0':
			continue
		default:
			return true
		}
	}
	return false
}

// containsBidiControl reports whether s carries an explicit bidirectional override or
// embedding. These are the runes that reorder text around them.
func containsBidiControl(s string) bool {
	for _, r := range s {
		if (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069') {
			return true
		}
	}
	return false
}

// optionalRole cleans a job title. "" means absent and yields nil, which reaches the
// column as NULL.
//
// ⚠️ UNLIKE THE EMAIL, "" HERE IS HARMLESS EITHER WAY -- `role` carries no unique
// index, so an empty string would merely be an odd-looking value rather than a
// collision. It is still folded to NULL so that "no role" has ONE representation in
// the table, which is what makes `role IS NULL` a usable question later.
func optionalRole(raw string) (*string, error) {
	s := strings.ReplaceAll(raw, "\x00", "")
	s = strings.ToValidUTF8(s, "")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(s) > MaxEmployeeRoleRunes {
		return nil, ErrEmployeeRole
	}
	return &s, nil
}

// optionalEmail cleans an address. "" means absent and yields nil.
//
// 🔴 THE ""-TO-nil FOLD IS THE WHOLE FUNCTION AND IT IS NOT COSMETIC. The unique
// index is PARTIAL -- UNIQUE (tenant_id, email) WHERE email IS NOT NULL -- so NULL
// rows are skipped by it and empty-string rows are NOT. Measured directly against
// the database (2026-08-17), rolled back:
//
//	two employees with email NULL   both INSERTs accepted
//	two employees with email ''     23505 on employees_tenant_email_key
//
// So without this fold the SECOND person a manager added without an address would be
// refused as a duplicate of the first, naming an address neither of them has. That
// is the card's trap and it is pinned by TestStaffDB_TwoPeopleWithNoEmailAreLegal.
//
// 🔴 IT DOES NOT LOWER-CASE, AND THAT IS THE COLUMN'S JOB RATHER THAN AN OMISSION.
// `email` is citext, so `A@x.mt` and `a@x.mt` compare equal and collide in the index
// (measured the same way). Folding case here would store a different address than the
// manager typed -- and would suggest the uniqueness is OURS when it is the column's.
// What the product owes the manager is the SENTENCE when it happens (ErrEmailTaken),
// not a silent rewrite of what they entered.
func optionalEmail(raw string) (*string, error) {
	s := strings.ReplaceAll(raw, "\x00", "")
	s = strings.ToValidUTF8(s, "")
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(s) > MaxEmployeeEmailRunes {
		return nil, ErrEmployeeEmail
	}
	// THE SHAPE TEST IS DELIBERATELY WEAK, signup's argument verbatim: exactly one
	// '@' with something either side, and no interior whitespace. A pattern claiming
	// to implement RFC 5322 refuses genuine addresses, and the authority on what this
	// column accepts is the column. What this catches is the typo worth catching --
	// a name typed into the email box.
	if strings.ContainsFunc(s, unicode.IsSpace) {
		return nil, ErrEmployeeEmail
	}
	at := strings.Index(s, "@")
	if at <= 0 || at != strings.LastIndex(s, "@") || at == len(s)-1 {
		return nil, ErrEmployeeEmail
	}
	return &s, nil
}

// isEmailTaken reports whether Postgres refused the row because THIS tenant already
// holds that address.
//
// 🔴 IT NAMES THE INDEX RATHER THAN TRUSTING THE SQLSTATE ALONE. `employees` carries
// three unique indexes (employees_pkey, employees_id_tenant_key,
// employees_tenant_email_key), so a bare 23505 would let a primary-key collision --
// which would be a real fault worth a 500 and an alert -- be reported to a manager as
// "that address is taken", sending them to fix something that is not wrong. The
// constraint name is populated for a partial unique INDEX as well as for a named
// constraint; measured against this schema (2026-08-17), the violation reports
// CONSTRAINT = employees_tenant_email_key.
func isEmailTaken(err error) bool {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" {
		return false
	}
	return pg.ConstraintName == "employees_tenant_email_key"
}

// DeactivateCommand is what a manager submitted.
type DeactivateCommand struct {
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	// ActorID is the ADMIN performing the action, taken from the signed panel
	// session by the handler -- NEVER from the request. It is required: an
	// administrative act with no actor is an unattributable trail row, and
	// audit_log.actor_id is exactly the column that exists to answer "who".
	ActorID uuid.UUID
}

// deactivateDetail is the audit row's jsonb payload.
//
// 🔴 IT IS A PURPOSE-BUILT STRUCT WITH KNOWN KEYS, which is the rule internal/audit
// states and the reason section 4.7 survives a jsonb column: nothing here is a dump
// of a request or a map of whatever was in scope, so no future edit can add a
// secret to it by accident.
//
// THE NAME IS NOT COPIED IN. The row already carries the employee id as its target
// and audit_log is append-only; a person's name in a jsonb column is a second copy
// of personal data that nothing can ever correct or delete.
type deactivateDetail struct {
	// PreviousStatus is what the person was before this act -- 'invited' or
	// 'active'. It is the FROM half of the transition, read inside the same
	// transaction as the write, so it describes the row that actually changed.
	PreviousStatus string `json:"previous_status"`
}

// Deactivate ends somebody's employment and records that it did, in ONE
// transaction.
//
// 🔴 THE NEXT TAP BECOMES A REJECT AND THIS IS THE ONLY THING THAT MAKES IT SO.
// Section 5 row 4 wants a deactivated employee's tap REFUSED, RECORDED and alerted
// on; what produces all three is sys:employee-deactivated reading employees.status
// out of the policy context, so writing the column IS the mechanism.
//
// 🔴 AND THIS FUNCTION DELIBERATELY DOES NOT CALL RevokeSessionsForEmployee (user
// decision, 2026-08-07). It is written here because this is where the next reader
// will want to add it. Revoking would change nothing about the refusal -- the
// guardrail already denies on status alone -- and would move every later tap by
// that person off the branch where the outcome is CERTAIN (reject + RECORD + alert)
// onto the "revoked session" branch, whose correctness depends on each caller
// remembering to write the record anyway. That is a section 4.6 risk taken for no
// gain. revoked_at keeps its two jobs: a lost or stolen phone, and the second
// activation. tenant/staff_db_test.go pins the OUTCOME (a deactivated person's tap
// is rejected, alerted and RECORDED) rather than the absence alone, because a bare
// negative assertion is the shape that quietly empties out.
func (s *Staff) Deactivate(ctx context.Context, c DeactivateCommand) (Person, error) {
	if err := c.validate(); err != nil {
		return Person{}, err
	}
	var out Person
	err := s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		// THE READ IS INSIDE THE WRITE'S TRANSACTION so the trail's "previous status"
		// is the status of the row the UPDATE is about to change, not of a snapshot
		// taken a round trip earlier.
		before, err := q.GetPanelEmployeeForAction(ctx, store.GetPanelEmployeeForActionParams{
			TenantID: c.TenantID,
			ID:       c.EmployeeID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownEmployee
			}
			return fmt.Errorf("load employee: %w", err)
		}
		out = person(before)

		row, err := q.DeactivateEmployee(ctx, store.DeactivateEmployeeParams{
			TenantID: c.TenantID,
			ID:       c.EmployeeID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// The statement's own `status <> 'deactivated'` predicate refused it.
				// Nothing was written, so nothing is trailed: a second press is not a
				// second act.
				return ErrAlreadyDeactivated
			}
			return fmt.Errorf("deactivate: %w", err)
		}
		out.Status = row.Status

		// THE TRAIL IS WRITTEN INSIDE THE SAME TRANSACTION. If it fails, the status
		// change above rolls back with it -- see Trail for why that is right here and
		// wrong almost everywhere else in this codebase.
		if _, err := s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionEmployeeDeactivated,
			Target:   c.EmployeeID.String(),
			Detail:   deactivateDetail{PreviousStatus: before.Status},
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return out, wrap("deactivate", err)
	}
	// 🔴 THE NAME IS NOT LOGGED. Employee ids are opaque handles; a person's name in
	// a process log is personal data in a place with no retention story (section 4.7,
	// section 7).
	s.log.Info("employee deactivated",
		"employee_id", c.EmployeeID, "actor_id", c.ActorID)
	return out, nil
}

// MoveCommand is a re-placement.
type MoveCommand struct {
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	ActorID    uuid.UUID
	// LocationID is the destination venue. It is REQUIRED because
	// employees.location_id is NOT NULL (00003): everybody has a home venue, so
	// there is no "no venue" to move to.
	LocationID uuid.UUID
	// DepartmentID is the destination department, or nil for "no department" --
	// which is a legitimate destination rather than a missing argument (00003: a bar
	// does not model departments).
	DepartmentID *uuid.UUID
}

// moveDetail is the audit row's payload: where they were, where they are.
//
// THE IDS ARE TEXT AND "" MEANS "no department". A uuid.UUID cannot express
// "absent" in JSON without marshalling as a zero uuid, which reads as a real id
// that happens to be all zeroes -- the trail must not be ambiguous about the state
// migration 00003 calls first class.
//
// NO NAMES, same rule as deactivateDetail: an id is a handle, a venue name is a
// second copy of a fact that lives in `locations`.
type moveDetail struct {
	FromLocationID   string `json:"from_location_id"`
	ToLocationID     string `json:"to_location_id"`
	FromDepartmentID string `json:"from_department_id"`
	ToDepartmentID   string `json:"to_department_id"`
}

// Move re-places somebody and records it, in ONE transaction.
//
// A MOVE IS NOT A LIFECYCLE CHANGE. The statement does not name status, so this
// cannot activate or deactivate anybody, and a DEACTIVATED person can be moved --
// deliberately: correcting where somebody worked is exactly the repair section 4.6
// wants to stay possible months later.
func (s *Staff) Move(ctx context.Context, c MoveCommand) (Person, error) {
	if err := c.validate(); err != nil {
		return Person{}, err
	}
	var out Person
	err := s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		before, err := q.GetPanelEmployeeForAction(ctx, store.GetPanelEmployeeForActionParams{
			TenantID: c.TenantID,
			ID:       c.EmployeeID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownEmployee
			}
			return fmt.Errorf("load employee: %w", err)
		}
		out = person(before)
		if before.LocationID == c.LocationID && sameDepartment(before.DepartmentID, c.DepartmentID) {
			return ErrSamePlacement
		}

		row, err := q.MoveEmployee(ctx, store.MoveEmployeeParams{
			TenantID:     c.TenantID,
			ID:           c.EmployeeID,
			LocationID:   c.LocationID,
			DepartmentID: c.DepartmentID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// The employee was found a moment ago inside this transaction, so zero
				// rows here means the PLACEMENT did not resolve: the statement's join
				// and its department guard are what refused it.
				return ErrUnknownPlacement
			}
			return fmt.Errorf("move: %w", err)
		}
		out.LocationID = row.LocationID
		out.DepartmentID = row.DepartmentID

		if _, err := s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionEmployeeMoved,
			Target:   c.EmployeeID.String(),
			Detail: moveDetail{
				FromLocationID:   before.LocationID.String(),
				ToLocationID:     row.LocationID.String(),
				FromDepartmentID: idText(before.DepartmentID),
				ToDepartmentID:   idText(row.DepartmentID),
			},
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return out, wrap("move", err)
	}
	s.log.Info("employee moved",
		"employee_id", c.EmployeeID, "actor_id", c.ActorID,
		"location_id", c.LocationID)
	return out, nil
}

func (c DeactivateCommand) validate() error {
	return requireIDs(c.TenantID, c.EmployeeID, c.ActorID)
}

func (c MoveCommand) validate() error {
	if err := requireIDs(c.TenantID, c.EmployeeID, c.ActorID); err != nil {
		return err
	}
	if c.LocationID == uuid.Nil {
		return ErrUnknownPlacement
	}
	if c.DepartmentID != nil && *c.DepartmentID == uuid.Nil {
		// A zero uuid is not "no department" -- nil is. Accepting it would let a
		// caller express absence in two ways and only one of them is the one the
		// database understands.
		return ErrUnknownPlacement
	}
	return nil
}

// requireIDs refuses the zero value for the three ids every command needs.
//
// THE ACTOR IS AS REQUIRED AS THE SUBJECT. A command with no actor would write an
// audit row with a NULL actor_id, which the schema permits (00005: the column is
// polymorphic and nullable, for system events) and which would be a lie here --
// every action this file performs is performed BY somebody.
func requireIDs(tenantID, employeeID, actorID uuid.UUID) error {
	switch {
	case tenantID == uuid.Nil:
		return errors.New("tenant: tenant id is required")
	case actorID == uuid.Nil:
		return errors.New("tenant: actor id is required")
	case employeeID == uuid.Nil:
		return ErrUnknownEmployee
	}
	return nil
}

// wrap keeps the sentinels unwrapped-by-name so a caller can switch on them, and
// wraps everything else so a real failure reaches the handler as a failure. Guessing
// at an unknown error is how an outage gets rendered as "already deactivated".
func wrap(op string, err error) error {
	switch {
	case errors.Is(err, ErrUnknownEmployee),
		errors.Is(err, ErrAlreadyDeactivated),
		errors.Is(err, ErrUnknownPlacement),
		errors.Is(err, ErrSamePlacement),
		// The four M6-13 refusals. Each is a different sentence to a manager and
		// each must survive the wrap, or the handler's errors.Is chain falls through
		// to "the panel is unavailable" and a mistyped address becomes a 500.
		errors.Is(err, ErrEmployeeName),
		errors.Is(err, ErrEmployeeEmail),
		errors.Is(err, ErrEmployeeRole),
		errors.Is(err, ErrEmailTaken):
		return err
	default:
		return fmt.Errorf("tenant: %s: %w", op, err)
	}
}

func person(row store.GetPanelEmployeeForActionRow) Person {
	return Person{
		ID:             row.ID,
		Name:           row.FullName,
		Status:         row.Status,
		LocationID:     row.LocationID,
		DepartmentID:   row.DepartmentID,
		LocationName:   deref(row.LocationName),
		DepartmentName: deref(row.DepartmentName),
	}
}

func sameDepartment(a, b *uuid.UUID) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}

func idText(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
