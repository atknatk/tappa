package tenant

// plaque.go -- the WALL PLAQUE side of the locations section (M6-06 phase B): the
// list a manager reads, mounting a loaded plaque on a wall, and replacing the one
// that is already there.
//
// 🔴 THE PANEL NEVER CREATES A PLAQUE, AND THAT IS THE PRODUCT DECISION THIS FILE
// IS BUILT AROUND (user decision, 2026-08-08: the inventory model). Creating a
// plaque means holding its AES-128 key; the key is produced by the M8-05 runbook,
// wrapped under the KEK and LOADED into the database by an operator as
// tappa_owner. So this file's whole vocabulary is: LIST, READ, BIND, UN-BIND and
// RETIRE -- never CREATE and never DELETE, which are the two the key and the
// evidence forbid.
// db/queries/tags.sql ships no INSERT over `tags` for the same reason.
//
// 🔴 SECTION 4.7 IS ENFORCED BY THE TYPE, AND THE CLAIM IS EXACTLY AS WIDE AS THE
// TEST UNDER IT. An earlier version of this paragraph said a key "has nowhere to
// travel even if a future query selected it" and named a test that DOES NOT EXIST;
// an audit then added three fields the wall of the day let straight through
// (`[44]byte`, a neutrally-named `string`, a `map[string][]byte`). So this is what
// is actually checked, by TestPlaquesDB_NoTypeOnThisPathCanCarryAKey:
//
//	COVERED   the FIELD NAMES of every type the test NAMES AS A ROOT, enumerated
//	          exactly -- a new field of any kind fails until somebody adds it there,
//	          which is the moment to ask what it carries. The roots are listed in the
//	          test and deliberately NOT counted here: an earlier version of this line
//	          named three of them and the test already walked four.
//	COVERED   the field SHAPES, to any depth, against an ALLOW-LIST of kinds. An
//	          array (however long), a map, an interface, a channel or a []byte is
//	          refused on shape alone. uuid.UUID is permitted by IDENTITY -- it is
//	          itself a [16]byte -- which is what keeps [44]byte refused.
//	COVERED   the SQL: no query in db/queries/tags.sql selects aes_key_ref, asserted
//	          against the FILE rather than from memory -- and the assertion scans
//	          every statement in it, so it does not depend on anybody counting them.
//	NOT COVERED  the RENDER types. A domain package cannot import web/templates, so
//	          the twin walk lives in internal/handler
//	          (TestPlaqueViewModels_CannotCarryAKey). Two tests, named in both files.
//	NOT COVERED  internal/sun, which legitimately reads the key through
//	          resolve_tag_by_uid (00004) to verify a CMAC.
//	NOT COVERED  a caller reaching store.Queries directly. Nothing in the panel does.
//	NOT COVERED  a key ENCODED INTO A PERMITTED FIELD -- hex in a string, base64 in
//	          a name. No type system refuses that. What makes it unreachable is that
//	          nothing on this path READS the column at all.
//
// 🔴 WHAT "ENCODED" ON THE SCREEN IS ACTUALLY BACKED BY -- measured, because the
// M6-06 card asks for an "encoded/pending" state and a word that overclaims is
// worse than no word. The screen reads NOTHING about the key. It reads
// `location_id IS NULL`. What lets it say "encoded" at all is a SCHEMA fact:
// tags.aes_key_ref is `bytea NOT NULL` (00004), so a row cannot exist without
// one, and only Tappa's loader writes rows. What it is NOT backed by, and this is
// backlog T7 restated where a reader will meet it: nothing verifies the value is
// a well-formed 44-byte KEK envelope. A corrupt or truncated envelope shows as
// "encoded" here and fails at TAP time, as a 500 out of sun.Unwrap's length
// check. This package cannot close that -- it would have to SELECT the column,
// which is exactly what the wall above forbids. Closing it needs a schema CHECK
// (T7 lists three options).
//
// 🔴 A REPLACE IS TWO WRITES AND ONE TRANSACTION (M6-06 card: "the old one is
// retired, the new one is registered"). Retiring the old plaque and binding the
// new one are separate statements because the schema's CHECKs make them separate
// -- but a caller that ran them in two transactions could leave a wall with the
// old plaque retired and the new one still in a box, i.e. an entrance where every
// tap rejects and nothing says why. Replace runs both inside one WithTenant
// callback, so that state is not reachable.
//
// 🔴 REVERSIBILITY WAS MEASURED BEFORE DECIDING WHAT GATE THESE ACTS NEED, and
// the answer decided it. Phase A put an HMAC confirmation AND an owner-only gate
// on venue removal, argued from one property: a DELETE destroys the row forever
// and there is no archive. Neither half of that is true here, measured live as
// tappa_app inside a rolled-back transaction on 2026-08-09:
//
//	DELETE FROM tags ...                          -> permission denied for table tags
//	  (00004 revokes it; the row can NEVER be destroyed by this role)
//	UPDATE ... SET status='retired', retired_at=now()   -> UPDATE 1
//	UPDATE ... SET status='active', retired_at=NULL,
//	               replaced_by=NULL                     -> UPDATE 1   (the reverse)
//	UPDATE ... SET status='unassigned', location_id=NULL -> UPDATE 1  (un-mount)
//	UPDATE ... SET status='active', location_id=<same>   -> UPDATE 1  (re-mount)
//	  row afterwards: same uid, same wall, no stamp, aes_key_ref still 44 bytes
//
// So every column these acts write is schema-reversible and nothing is lost:
// `transactions` keep resolving through tag_uid whatever the status says.
//
// ⚠️ AND THAT IS NOT THE QUESTION PHASE A's GATE ANSWERED, which is why the panel
// CONFIRMS a replacement anyway (internal/handler/plaqueactions.go). The product
// ships no route that UN-RETIRES a plaque: db/queries has no such statement and this
// file does not add one, so a mis-pressed REPLACEMENT leaves an entrance where every
// tap is refused until somebody physically swaps the plaque. "The schema permits the
// reverse" and "a manager can undo it" are different sentences, and for a retirement
// only the first is true.
//
// ⚠️ THAT PARAGRAPH USED TO SAY "no un-retire AND NO UN-MOUNT" WHILE THIS FILE
// SHIPPED Unmount TWO HUNDRED LINES BELOW IT. The behaviour was fixed in the round
// that added it and the sentence was not -- with a "(measured)" label still on it,
// which is worse than an ordinary stale comment because it invites the next reader to
// stop checking. A MOUNT is now genuinely undoable from the panel (UnmountTagFromWall,
// one press, no spare spent); a RETIREMENT still is not, and that asymmetry is the
// whole argument for which acts carry a confirmation.
//
// WHAT THIS PACKAGE CONTRIBUTES TO THAT GATE IS THE SHAPE OF THE STATEMENTS, not the
// gate itself: a replacement must name a real STOCK plaque of the same tenant, and
// both halves carry their precondition in their own WHERE, so a replayed POST matches
// zero rows and writes nothing. The HMAC confirmation is the handler's, and the
// M6-06 card records the decision and the numbers.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// Audit actions this file writes.
//
// ONE ACTION PER ROW TOUCHED, WHICH IS WHY A REPLACE WRITES TWO OF THEM. `target`
// names ONE row, so a single "plaque.replaced" entry would leave the SUCCESSOR with
// no trail row of its own and `action LIKE 'plaque.%' AND target = <new uid>` would
// answer nothing about how it got onto a wall. Writing plaque.retired for the old one
// and plaque.mounted for the new one, in the same transaction, keeps "what has been
// done to THIS plaque" answerable from target alone -- the rule venue.go's header
// states for locations.
//
// ⚠️ THE ACTIONS ARE NOT COUNTED HERE. This paragraph opened "TWO ACTIONS, NOT
// THREE" and a third shipped two rounds later; the set is the const block below, and
// internal/handler derives it from there rather than from any sentence.
//
// 🔴 detail.name IS WHAT THE ACKNOWLEDGEMENT READS, AND IT HOLDS A DIFFERENT FACT
// PER ACTION, deliberately. The panel's C' receipt (audit.sql,
// ConfirmRecentRemoval) reads `detail->>'name'`, so each action puts there the one
// value its own sentence needs: the VENUE NAME for a mount ("mounted at St
// Julians"), the SUCCESSOR UID for a retirement ("replaced by 04AC7E55000601").
// A uid is not a secret (section 4.7 names only keys, tokens, codes and precise
// coordinates -- a plaque uid is printed on the plaque and sits in the address bar).
const (
	ActionPlaqueMounted = "plaque.mounted"
	ActionPlaqueRetired = "plaque.retired"
	// ActionPlaqueUnmounted: the plaque came OFF a wall and went back into stock.
	//
	// 🔴 IT IS A THIRD ACTION AND NOT A FLAVOUR OF RETIREMENT. A retirement is
	// permanent and stamps retired_at + replaced_by; an un-mount changes the plaque's
	// mind about which door it serves and leaves both stamps alone. An investigator
	// asking "when did this plaque leave the wall for good" must not find this row.
	//
	// ListPlaqueHistory picks it up with NO change, which is why its filter is the
	// PREFIX `plaque.%` rather than a hand-listed pair.
	ActionPlaqueUnmounted = "plaque.unmounted"
)

// The plaque lifecycle vocabulary, read from the same CHECK constraint the policy
// engine reads (00004 + 00013). They are constants here rather than free strings
// so a typo is a compile error.
const (
	PlaqueActive     = "active"
	PlaqueUnassigned = "unassigned"
	PlaqueRetired    = "retired"
	PlaqueLost       = "lost"
)

// The refusals a caller must be able to tell apart, because each is a different
// sentence to a manager (section 4.6: no silent failure).
var (
	// ErrUnknownPlaque: no plaque with that uid in this tenant. It covers "does not
	// exist" and "belongs to somebody else" together, for the reason ErrUnknownVenue
	// gives: separating them would answer "does this uid exist elsewhere?" for
	// anybody who can post a form -- and a uid is readable off a wall in another
	// building.
	ErrUnknownPlaque = errors.New("tenant: no such plaque in this tenant")

	// ErrPlaqueNotInStock: the plaque named as the new one is not `unassigned`.
	// Somebody mounted it first, or it is already on a wall, or it is retired.
	ErrPlaqueNotInStock = errors.New("tenant: that plaque is not in stock")

	// ErrPlaqueNotOnAWall: the plaque named as the old one is not `active`. It is
	// stock, or already retired, or reported lost.
	ErrPlaqueNotOnAWall = errors.New("tenant: that plaque is not on a wall")

	// ErrSamePlaque: a plaque cannot replace itself. The statements would refuse it
	// anyway (the first one makes it `retired`, and the second demands
	// `unassigned`), but the refusal would arrive as "not in stock", which sends a
	// manager looking for the wrong thing.
	ErrSamePlaque = errors.New("tenant: a plaque cannot replace itself")

	// ErrPlaqueNoWall: the plaque named for an un-mount is not on a wall at all.
	// Separate from ErrPlaqueNotOnAWall only in the sentence it produces: that one is
	// about the plaque a REPLACEMENT would retire, this one about the plaque a
	// manager is trying to take down.
	ErrPlaqueNoWall = errors.New("tenant: that plaque is not on a wall to take down")

	// ErrPlaqueUID: the uid is not in the canonical form the column requires.
	// Section 7: the boundary validates, but the domain states its own bound so a
	// second caller cannot get in under it.
	ErrPlaqueUID = errors.New("tenant: that is not a plaque id")

	// ErrPlaqueFrozen: the row exists but the database refuses to change it.
	//
	// 🔴 IT IS A REAL, MEASURED STATE AND NOT A THEORETICAL ONE -- in DEVELOPMENT.
	// Migration 00013 added `tags_uid_canonical_hex` as NOT VALID because 18 010
	// pre-existing rows carry a LOWER-CASE uid and could not be normalised (12 437
	// of them are referenced by append-only `transactions`; T8 carries the whole
	// argument). A CHECK is evaluated on every UPDATE of a row, so those rows are
	// FROZEN: any write to them answers SQLSTATE 23514. ListTagsForTenant returns
	// them like any other row, so without this the development panel would answer
	// 500 on a plaque a manager can see. In production the count is zero -- a fresh
	// database has no violating row and none can be written -- so this maps a
	// development reality onto a sentence rather than a crash.
	ErrPlaqueFrozen = errors.New("tenant: that plaque's id was recorded in a spelling this database no longer accepts")
)

// plaqueUIDRE is the canonical uid: 14 upper-case hex characters. It is
// migration 00013's `tags_uid_canonical_hex` restated at this boundary, and it is
// the same form internal/sun.Parse produces from a chip URL
// (strings.ToUpper(hex.EncodeToString(...))).
//
// 🔴 IT IS HERE BECAUSE THE SQL CANNOT BE THE ONLY GUARD, and the measurement is
// in db/queries/tags.sql: `'AABBCCDDEEFF01ZZZZZZ'::char(14)` TRUNCATES to
// 'AABBCCDDEEFF01' with no error, so an over-long successor uid silently becomes a
// DIFFERENT, possibly real plaque and the self-FK accepts it. RetireTagForReplacement
// added a predicate on the UNCAST parameter for that; this is the section 7 half the
// data-layer round left owed.
var plaqueUIDRE = regexp.MustCompile(`^[0-9A-F]{14}$`)

// plaqueRefRE is the same shape with EITHER case, for looking a row UP.
var plaqueRefRE = regexp.MustCompile(`^[0-9A-Fa-f]{14}$`)

// PlaqueUID validates a uid that is about to be STORED, in the canonical form and
// only in the canonical form.
//
// 🔴 IT NO LONGER UPPER-CASES, AND THE PREVIOUS VERSION WHICH DID REOPENED PHASE
// A's OWN DEFECT. Normalising here looked friendly and was measured doing this, on
// the working database (tenant 2f2bb0e2..., plaque b27188a6d32e49): the list
// rendered a link to the row's REAL uid, the lookup upper-cased it, `tags.uid` is
// char(14) and case-SENSITIVE, no row matched -- so the section answered "that
// plaque id is not one of this business's" about a row it had printed two lines
// higher. That is phase A's defect number 2 word for word, and it also made
// ErrPlaqueFrozen unreachable over HTTP, i.e. a whole refusal path that no test
// could fire.
//
// 🔴 SO THERE ARE TWO FUNCTIONS NOW, AND THEY ARE NOT TWO REPRESENTATIONS OF ONE
// RULE -- they answer two different questions:
//
//	PlaqueUID   "may this value be WRITTEN?"  -> canonical only. It is what
//	            db/queries/tags.sql's own predicate demands of @replaced_by
//	            (`~ '^[0-9A-F]{14}$'`, on the UNCAST parameter) and what
//	            00013's tags_uid_canonical_hex demands of every new row.
//	PlaqueRef   "may this value be LOOKED UP?" -> either case, PRESERVED. The
//	            table still holds 18 010 lower-case rows that predate 00013 and
//	            ListTagsForTenant returns them; a lookup that cannot express them
//	            cannot open them, and a screen that lists a row it cannot open is
//	            the defect above.
//
// internal/sun.Parse always produces the canonical form from a chip, so the TAP
// path is unaffected either way; this distinction exists entirely for the panel.
// CanonicalUID is a plaque id that HAS BEEN CHECKED against the canonical form.
//
// 🔴 IT IS A TYPE BECAUSE THE PREVIOUS ARRANGEMENT WAS DISCIPLINE, AND AN AUDIT
// MEASURED THAT. PlaqueUID and PlaqueRef both returned a plain string, so swapping
// one for the other at a call site compiled and left the WHOLE SUITE GREEN in the
// safe-looking direction; the only thing that actually refused a non-canonical
// successor was Replace calling PlaqueUID again inside the domain. That is a real
// mechanism, but the caller's own comment presented the CHOICE OF VALIDATOR as the
// mechanism — "a shape is not a mechanism" is this task's own lesson, and it had
// been re-earned.
//
// So the two functions now return two TYPES, and a value that has only been checked
// as a lookup key cannot be handed to the field that is written into `replaced_by`.
// The domain's re-validation stays: a type says where a value came from, not that it
// is still true, and CanonicalUID("") is constructible by anyone in this package.
type CanonicalUID string

func (c CanonicalUID) String() string { return string(c) }

func PlaqueUID(raw string) (CanonicalUID, error) {
	s := strings.TrimSpace(raw)
	if !plaqueUIDRE.MatchString(s) {
		return "", ErrPlaqueUID
	}
	return CanonicalUID(s), nil
}

// PlaqueRef validates a uid used as a LOOKUP KEY and returns it EXACTLY as typed.
//
// 🔴 CASE IS PRESERVED BECAUSE THE COLUMN IS CASE-SENSITIVE. `tags.uid` is
// char(14); 'b27188a6d32e49' and 'B27188A6D32E49' are two different keys, and only
// one of them can be the row a manager just saw. Folding either way here would make
// the panel unable to open a row it lists.
//
// IT IS STILL A BOUNDARY CHECK, not a pass-through: the shape is bounded so the
// truncating cast can never see an over-long value (db/queries/tags.sql measures
// `'AABBCCDDEEFF01ZZZZZZ'::char(14)` -> 'AABBCCDDEEFF01', no error), and a lookup
// key that is not a plaque id at all never reaches a statement.
func PlaqueRef(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if !plaqueRefRE.MatchString(s) {
		return "", ErrPlaqueUID
	}
	return s, nil
}

// Plaque is one wall plaque as the panel reads it.
//
// 🔴 THERE IS NO KEY FIELD AND THERE CANNOT BE ONE. See the file header for what
// that wall covers. Nothing here is a []byte.
type Plaque struct {
	// UID is the chip id printed on the plaque and carried in the tap URL. Public.
	UID string
	// Status is the lifecycle value: active | unassigned | retired | lost.
	Status string
	// LocationID is the wall it is mounted at, or nil for STOCK.
	//
	// 🔴 A POINTER, AND THAT IS THE WHOLE INVENTORY MODEL IN ONE FIELD. 00013 made
	// tags.location_id nullable so a plaque can exist before anybody has decided
	// which door it goes on. uuid.Nil would be a location id that resolves to
	// nothing, which is how "stock" would quietly become "a venue we cannot find".
	LocationID *uuid.UUID
	// LastCtr is the chip's read counter as the database last recorded it. It is
	// STATE, never compared read-then-write: the atomic advance is
	// store.AdvanceTagCounter (section 4.4).
	LastCtr int32
	// RetiredAt is when this plaque left the wall, or nil.
	RetiredAt *time.Time
	// ReplacedBy is the uid of the plaque that took its place, or "".
	ReplacedBy string
	// Replaces is the uid of the plaque THIS one took the place of, or "".
	//
	// 🔴 IT IS DERIVED IN GO FROM THE SAME LIST, WITH NO SECOND QUERY. The schema
	// stores the chain one way (replaced_by points forward), and the backward step is
	// a scan of the tenant's own plaques -- bounded by the doors a business has.
	//
	// ⚠️ NO FIGURE IS QUOTED HERE ANY MORE, AND THAT IS THE THIRD ATTEMPT. It said
	// "largest 11, mean 1.02 over 78 919 tenants" (true when written), then "63 …
	// 83 011" (true for about a day: 93 the next morning). The number moves because
	// THIS milestone's own journey tests seed plaques into the shared demo tenant and
	// `tags` cannot be deleted by this role — so any level written here is stale before
	// the next reader arrives, and the repo's own rule is a range with its conditions
	// or nothing. What does NOT move is the shape: a plaque is screwed to a door frame,
	// so a tenant's list is tens, and a reverse index would be a query plus an index
	// for a loop over a handful of items. Count it from the table if you need a number.
	Replaces string
	// LastSeen is when this plaque was last tapped, or NIL FOR NEVER.
	//
	// 🔴 A POINTER BECAUSE ABSENCE IS ABSENCE (the data-layer round's own lesson,
	// and the reason ListTagLastSeen is a separate query). A zero time.Time would be
	// orderable, formattable and WRONG -- the exact twin of reading a NULL location
	// as uuid.Nil. A plaque with no row in the last-seen result has never been
	// tapped, and that is what nil says.
	LastSeen *time.Time
	// CreatedAt is when Tappa loaded this plaque into the customer's inventory.
	CreatedAt time.Time
	// Canonical says the uid is in the spelling the schema now requires. False only
	// for development residue -- see ErrPlaqueFrozen. A non-canonical row can be
	// READ but never written, so the screen must not offer it an action.
	Canonical bool
}

// InStock reports that this plaque is loaded but not on any wall.
func (p Plaque) InStock() bool { return p.Status == PlaqueUnassigned }

// OnAWall reports that this plaque is in service.
func (p Plaque) OnAWall() bool { return p.Status == PlaqueActive }

// Changeable reports whether the database would accept a write to this row at all.
// It is false ONLY for the frozen development residue; see ErrPlaqueFrozen.
func (p Plaque) Changeable() bool { return p.Canonical }

// PlaqueScreen is every plaque the section renders in one round trip.
type PlaqueScreen struct {
	Plaques []Plaque
	// Capped says the ceiling was reached and the list is therefore INCOMPLETE
	// (section 4.6: a truncated list that does not say so is a claim about the
	// business).
	Capped bool
	// Zone is the tenant's timezone. Every instant on a Plaque is UTC (section 6)
	// and is converted at RENDER; this is what the renderer converts to.
	//
	// 🔴 NOT THE SERVER'S ZONE AND NOT THE BROWSER'S -- db/queries/tenants.sql
	// carries the argument at GetTenantClock: the server's would make one business
	// see a different day depending on where the VPS is, and the browser's would let
	// a client decide what it is shown. It is the FIRST timestamp this section
	// renders, which is why the zone arrives with phase B rather than phase A.
	Zone *time.Location
}

// plaquePageLimit is the ceiling on the rendered list.
//
// ⚠️ IT CAPS THE RENDER, NOT THE READ, AND THAT IS A COUNTED LIMIT RATHER THAN A
// CLOSED ONE. ListTagsForTenant carries no LIMIT (it shipped in the data-layer
// round and changing its signature here would edit an audited query as a side
// effect of a screen), so the bytes are still read. What bounds them in practice is
// the same thing that bounds the venue list: a plaque is a physical object screwed
// to a door frame -- a tenant's list is tens, and the development database's own
// maximum drifts upward on every suite run because this milestone's journey tests
// seed into the shared demo tenant and `tags` cannot be deleted by this role. No
// level is quoted for that reason; 200 is the same ceiling venuePageLimit uses, for
// the same class of object.
const plaquePageLimit = 200

// Plaques performs the manager's acts on wall plaques. Dependencies are injected
// as struct fields (section 7): no package-level state, no singleton.
type Plaques struct {
	data  Database
	trail Trail
	log   *slog.Logger
}

// NewPlaques wires it. A nil database or a nil trail is REFUSED rather than
// tolerated, for the reason NewVenues gives: a Plaques that cannot write the trail
// would retire the plaque an entrance depends on with no record of who did it, and
// `tags` has no updated_at to fall back on.
func NewPlaques(data Database, trail Trail, log *slog.Logger) (*Plaques, error) {
	switch {
	case data == nil:
		return nil, errors.New("tenant: nil database")
	case trail == nil:
		return nil, errors.New("tenant: nil audit trail")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Plaques{data: data, trail: trail, log: log}, nil
}

// Screen reads the tenant's plaques and their last-seen stamps.
//
// 🔴 THE TWO READS SHARE ONE TRANSACTION so the stamps belong to the plaques
// listed beside them -- the reason Venues.Screen gives for its two lists.
//
// 🔴 THE JOIN IS IN GO, AND THAT IS A TYPE DECISION THE DATA LAYER MEASURED RATHER
// THAN A CONVENIENCE. "Last seen" cannot be a column on the list query: sqlc v1.28
// types the nullable maximum four different wrong ways, and the cast form compiles
// and then panics on exactly the rows this milestone exists for (a stock plaque has
// never been tapped). ListTagLastSeen therefore returns a row ONLY for a plaque
// that HAS been tapped, and the absence of a key here is what "never" means.
func (p *Plaques) Screen(ctx context.Context, tenantID uuid.UUID) (PlaqueScreen, error) {
	if tenantID == uuid.Nil {
		return PlaqueScreen{}, errors.New("tenant: tenant id is required")
	}
	var out PlaqueScreen
	err := p.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		rows, err := q.ListTagsForTenant(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("list plaques: %w", err)
		}
		seen, err := q.ListTagLastSeen(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("list plaque last seen: %w", err)
		}
		out = plaqueScreenOf(rows, seen)
		// THE ZONE IS READ IN THE SAME TRANSACTION as the rows it will render, so a
		// tenant renamed mid-request cannot produce stamps in one zone and a label in
		// another. GetTenantClock is an EXISTING query (M6-03's) rather than a new one.
		clock, err := q.GetTenantClock(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("read tenant clock: %w", err)
		}
		out.Zone = p.loadZone(clock.Timezone)
		return nil
	})
	if err != nil {
		return PlaqueScreen{}, wrapPlaque("screen", err)
	}
	return out, nil
}

// loadZone resolves the tenant's timezone name, falling back to UTC.
//
// IT IS internal/domain/ledger's loadZone, AND THE DUPLICATION IS NAMED RATHER
// THAN HIDDEN. Sharing it would mean either importing a READ package from a WRITE
// one -- the separation "no store call in ledger is not a SELECT" depends on -- or
// creating a third package for eight lines. What both copies must keep is the
// second paragraph, which is the part that is not obvious.
//
// 🔴 A ZONE THE RUNTIME CANNOT LOAD MUST NOT COST THE MANAGER THEIR PAGE, AND THE
// DEGRADATION IS NOT SILENT. The column is NOT NULL with a sane default (migration
// 0001), so an unloadable name is a DEPLOYMENT problem -- a container shipped
// without tzdata -- and falling back to UTC silently would make every stamp on the
// page quietly wrong by an hour or two with nothing anywhere saying so. Section 7
// forbids swallowing it; the fallback stays, the silence goes.
func (p *Plaques) loadZone(name string) *time.Location {
	if name == "" {
		p.log.Warn("tenant: this business has no timezone; showing plaque stamps in UTC",
			"fallback", "UTC")
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		p.log.Error("tenant: cannot load the business timezone; showing plaque stamps in UTC",
			"zone", name, "fallback", "UTC", "err", err)
		return time.UTC
	}
	return loc
}

// Plaque reads ONE plaque by uid, inside this tenant.
func (p *Plaques) Plaque(ctx context.Context, tenantID uuid.UUID, uid string) (Plaque, error) {
	if tenantID == uuid.Nil {
		return Plaque{}, errors.New("tenant: tenant id is required")
	}
	// A LOOKUP, so the spelling is preserved -- see PlaqueRef.
	canonical, err := PlaqueRef(uid)
	if err != nil {
		return Plaque{}, err
	}
	var out Plaque
	err = p.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetTagForTenant(ctx, store.GetTagForTenantParams{
			TenantID: tenantID, Uid: canonical,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownPlaque
			}
			return fmt.Errorf("read plaque: %w", err)
		}
		out = plaqueOf(plaqueRow{
			UID: row.Uid, LocationID: row.LocationID, LastCtr: row.LastCtr,
			Status: row.Status, RetiredAt: row.RetiredAt, ReplacedBy: row.ReplacedBy,
			CreatedAt: row.CreatedAt,
		})
		return nil
	})
	if err != nil {
		return Plaque{}, wrapPlaque("read plaque", err)
	}
	return out, nil
}

// PlaqueEvent is one entry of a plaque's trail: what was done, when, and by whom.
//
// 🔴 IT IS THE HALF THE `tags` ROW CANNOT CARRY. The row answers WHAT and WHEN — its
// created_at, its retired_at, its replaced_by chain — and has no actor column at all.
// "Who took this plaque off the wall?" is the question a manager arrives with when a
// door has stopped working, and only audit_log can answer it.
type PlaqueEvent struct {
	// Action is the trail's own vocabulary (plaque.mounted, plaque.retired). The
	// render layer turns it into a sentence; carrying the word rather than the
	// sentence keeps the domain out of the wording business.
	Action string
	At     time.Time
	// ActorName is the admin's name, or "" when there is none to show.
	//
	// ⚠️ "" HAS TWO CAUSES AND ONLY ONE OF THEM IS THE SYSTEM. admin_users.full_name
	// is `text NOT NULL` with no non-blank CHECK, so an admin can be stored nameless;
	// audit_log.actor_id is separately nullable and holds NULL for SYSTEM events.
	// Collapsing the two would attribute a nameless admin's act to the product, in
	// the one place a manager goes to ask who changed the plaque on their door.
	ActorName string
	// BySystem says the trail row carried NO actor at all.
	//
	// Today no plaque.* row can have one — requireActor refuses a command with no
	// actor, so the arm is unreachable from the product and is driven only in tests.
	// It is carried rather than assumed away because audit_log is shared and
	// append-only: a future writer (a retention job, a runbook) will produce one, and
	// this reader must not misattribute it on the day it does.
	BySystem bool
}

// plaqueHistoryLimit bounds one plaque's trail.
//
// ⚠️ IT BOUNDS THE OUTPUT, NOT THE SCAN, and db/queries/audit.sql carries the
// measurement: the tenant predicate is indexed, `target` and `action` are not, so the
// statement reads every audit row the TENANT has and discards them. Measured
// 2026-08-10 on the demo tenant, whose audit_log holds 2 566 rows of 63 624 in a
// 19 MB relation: 1 064 buffers, 4.2 ms,
// zero rows returned. Linear in the business's age, not in the plaque's history. The
// index that would fix it is a MIGRATION and this phase's slot is spent; it is in the
// backlog. 20 is far above any real chain and small enough that the card stays a card.
const plaqueHistoryLimit = 20

// History reads WHO did WHAT to this plaque, most recent first.
//
// 🔴 IT IS THE SECOND HALF OF THE CARD'S HISTORY AND NOT A REPLACEMENT FOR THE
// FIRST. The `tags` row is still what says "loaded on the 3rd, took over from X,
// retired on the 9th" — it is complete for the plaque's whole life and needs no
// join. This adds the actor, which nothing else can. Two sources, two jobs; the
// M6-06 card records both readings.
//
// A FAILED READ IS AN ERROR, NOT AN EMPTY LIST (§4.6): "nothing has been done to this
// plaque" is a claim, and a timeout is not evidence for it.
func (p *Plaques) History(ctx context.Context, tenantID uuid.UUID, uid string) ([]PlaqueEvent, error) {
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant: tenant id is required")
	}
	ref, err := PlaqueRef(uid)
	if err != nil {
		return nil, err
	}
	var out []PlaqueEvent
	err = p.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := store.New(tx).ListPlaqueHistory(ctx, store.ListPlaqueHistoryParams{
			TenantID: tenantID, Target: ref, RowLimit: plaqueHistoryLimit,
		})
		if err != nil {
			return fmt.Errorf("list plaque history: %w", err)
		}
		out = make([]PlaqueEvent, 0, len(rows))
		for _, r := range rows {
			out = append(out, PlaqueEvent{
				Action: r.Action, At: r.At, ActorName: r.ActorName, BySystem: r.BySystem,
			})
		}
		return nil
	})
	if err != nil {
		return nil, wrapPlaque("plaque history", err)
	}
	return out, nil
}

// MountCommand binds ONE loaded plaque to ONE wall.
type MountCommand struct {
	TenantID uuid.UUID
	// ActorID is the ADMIN performing the act, from the signed panel session --
	// NEVER from the request.
	ActorID uuid.UUID
	// UID is the plaque being mounted. It must currently be in stock.
	UID string
	// LocationID is the wall. It is NOT validated here and does not need to be: the
	// composite FK (location_id, tenant_id) -> locations (id, tenant_id) refuses an
	// id from another tenant with 23503, which becomes ErrUnknownVenue below.
	LocationID uuid.UUID
	// VenueName is what the wall is called, for the trail's own copy. It is read by
	// the caller from the venue list it already has; the trail keeps it because the
	// acknowledgement sentence is built from the trail row rather than from the URL.
	VenueName string
}

// mountedDetail is the audit row's payload for a mount.
//
// 🔴 NO omitempty ON ANY FACT -- the lesson deletedDetail records. A key that is
// absent cannot be told apart from a value that was empty, and this row is how an
// investigator answers "when did this door get this plaque".
//
// 🔴 SECTION 4.7: THERE IS NO FIELD HERE A SECRET COULD TRAVEL IN. No key, no
// counter secret, no coordinate, no session id. A uid and a venue name are values
// the tenant already owns and the plaque already prints.
type mountedDetail struct {
	Name       string `json:"name"`
	LocationID string `json:"location_id"`
	// Replaces is the uid this plaque took over from, or "" for a first mount. It
	// is what tells a reader whether this mount was half of a replacement.
	Replaces string `json:"replaces"`
	// FromStatus is what the row was before. It is always "unassigned" today,
	// because the statement's own WHERE demands it; recording it means a future
	// transition cannot silently reuse this action name.
	FromStatus string `json:"from_status"`
}

// retiredDetail is the audit row's payload for a retirement.
type retiredDetail struct {
	// Name holds the SUCCESSOR's uid -- see the ActionPlaqueRetired comment for why
	// `name` carries a different fact per action.
	Name       string `json:"name"`
	LocationID string `json:"location_id"`
	// LastCtr is the chip's read counter at the moment it left the wall. It is the
	// one number that cannot be reconstructed later: the row keeps it, but a future
	// mount of another plaque at the same door does not.
	LastCtr string `json:"last_ctr"`
}

// Mount binds a loaded plaque to a wall and records that it did, in ONE
// transaction.
//
// 🔴 THE PRECONDITION IS IN THE STATEMENT, NOT HERE. AssignTagToLocation carries
// `status = 'unassigned'` in its own WHERE, so two managers mounting the same
// plaque produce ONE winner and one pgx.ErrNoRows with no read-then-write window
// between them (the section 4.4 shape applied to a different column). This function
// does NOT read the row first to check -- that would be the TOCTOU the shape exists
// to forbid.
func (p *Plaques) Mount(ctx context.Context, c MountCommand) (Plaque, error) {
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return Plaque{}, err
	}
	// 🔴 THE SUBJECT IS A LOOKUP KEY, NOT A VALUE BEING WRITTEN. Mounting does not
	// create a uid -- it finds a row Tappa already loaded and sets its location, so
	// the spelling has to be the one the row carries. A frozen (pre-00013) row
	// therefore REACHES the statement and is refused by the CHECK as 23514, which
	// classifyMount turns into ErrPlaqueFrozen. That is the only way that refusal is
	// reachable at all.
	uid, err := PlaqueRef(c.UID)
	if err != nil {
		return Plaque{}, err
	}
	if c.LocationID == uuid.Nil {
		return Plaque{}, ErrUnknownVenue
	}
	var out Plaque
	err = p.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.AssignTagToLocation(ctx, store.AssignTagToLocationParams{
			LocationID: c.LocationID, TenantID: c.TenantID, Uid: uid,
		})
		if err != nil {
			return classifyMount(ctx, q, c.TenantID, uid, err)
		}
		out = plaqueOf(plaqueRow{
			UID: row.Uid, LocationID: row.LocationID, LastCtr: row.LastCtr,
			Status: row.Status, RetiredAt: row.RetiredAt, ReplacedBy: row.ReplacedBy,
			CreatedAt: row.CreatedAt,
		})
		_, err = p.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionPlaqueMounted,
			Target:   uid,
			Detail: mountedDetail{
				Name:       c.VenueName,
				LocationID: c.LocationID.String(),
				Replaces:   "",
				FromStatus: PlaqueUnassigned,
			},
		})
		return err
	})
	if err != nil {
		return Plaque{}, wrapPlaque("mount plaque", err)
	}
	p.log.Info("plaque mounted", "tag_uid", uid, "location_id", c.LocationID,
		"actor_id", c.ActorID)
	return out, nil
}

// UnmountCommand takes ONE plaque off its wall and puts it back in stock.
type UnmountCommand struct {
	TenantID uuid.UUID
	ActorID  uuid.UUID
	// UID is the plaque coming down. It must currently be `active`.
	UID string
	// VenueName is the wall it is leaving, for the trail's own copy — the
	// acknowledgement is built from the audit row rather than from the URL.
	VenueName string
}

// unmountedDetail is the audit row's payload for an un-mount.
type unmountedDetail struct {
	// Name holds the venue it CAME OFF, so the trail reads the way the screen did.
	Name string `json:"name"`
	// LocationID is that wall's id. It is the only surviving pointer: the row's own
	// location_id is NULL from this moment, which is exactly what makes an absent key
	// here indistinguishable from a fact nobody recorded (§4.6, the omitempty lesson).
	LocationID string `json:"location_id"`
	FromStatus string `json:"from_status"`
}

// Unmount takes a plaque off its wall and records that it did, in ONE transaction.
//
// 🔴 IT EXISTS BECAUSE MOUNTING WAS OTHERWISE IRREVERSIBLE, AND THE PRODUCT SAID
// OTHERWISE IN TWO PLACES WITH A "MEASURED" LABEL ON THEM. An audit drove it end to
// end: a plaque mounted at the wrong entrance could not be moved, could not go back
// to stock, and — with no spare in the box — its card offered no action at all,
// while every tap at that door was judged against the WRONG venue's IP and
// coordinate (§5 row 7, the approval queue). The schema always permitted the
// reverse; there was no statement for it.
//
// ⚠️ AND THIS PATH IS ITSELF SERVICE-AFFECTING, WHICH IS WHY IT IS GATED. A plaque
// off the wall is `unassigned`, so §5 row 1 refuses every tap at that door until
// something is mounted there. The handler puts the SAME HMAC confirmation on it that
// a replacement carries — a fifth action on the shipped primitive, not a second
// primitive. Mounting stays ungated, and NOW that sentence is true: a mount can be
// undone from the panel, in one press, with no spare spent.
//
// THE PRECONDITION IS IN THE STATEMENT, not here: UnmountTagFromWall carries
// `status = 'active'` in its own WHERE, so two managers pressing at once produce one
// winner and one pgx.ErrNoRows with no read-then-write window.
func (p *Plaques) Unmount(ctx context.Context, c UnmountCommand) (Plaque, error) {
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return Plaque{}, err
	}
	// A LOOKUP KEY: the row exists and may carry a pre-00013 spelling.
	uid, err := PlaqueRef(c.UID)
	if err != nil {
		return Plaque{}, err
	}
	var out Plaque
	err = p.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		// The wall is read BEFORE the write because after it there is none: the row's
		// location_id is NULL and the trail row is the only place the door survives.
		before, err := q.GetTagForTenant(ctx, store.GetTagForTenantParams{
			TenantID: c.TenantID, Uid: uid,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownPlaque
			}
			return fmt.Errorf("read plaque: %w", err)
		}
		if before.Status != PlaqueActive || before.LocationID == nil {
			return ErrPlaqueNoWall
		}

		row, err := q.UnmountTagFromWall(ctx, store.UnmountTagFromWallParams{
			TenantID: c.TenantID, Uid: uid,
		})
		if err != nil {
			return classifyUnmount(ctx, q, c.TenantID, uid, err)
		}
		out = plaqueOf(plaqueRow{
			UID: row.Uid, LocationID: row.LocationID, LastCtr: row.LastCtr,
			Status: row.Status, RetiredAt: row.RetiredAt, ReplacedBy: row.ReplacedBy,
			CreatedAt: row.CreatedAt,
		})
		_, err = p.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionPlaqueUnmounted,
			Target:   uid,
			Detail: unmountedDetail{
				Name:       c.VenueName,
				LocationID: before.LocationID.String(),
				FromStatus: PlaqueActive,
			},
		})
		return err
	})
	if err != nil {
		return Plaque{}, wrapPlaque("unmount plaque", err)
	}
	p.log.Info("plaque unmounted", "tag_uid", uid, "actor_id", c.ActorID)
	return out, nil
}

// classifyUnmount turns a failed un-bind into the sentence a manager can act on.
func classifyUnmount(ctx context.Context, q *store.Queries, tenantID uuid.UUID, uid string, cause error) error {
	if errors.Is(cause, pgx.ErrNoRows) {
		// The read above already established the row is active, so zero rows here means
		// somebody else took it down between the two statements — a race, not a state.
		return ErrPlaqueNoWall
	}
	if isCheckViolation(cause) {
		return ErrPlaqueFrozen
	}
	_ = ctx
	_ = q
	return fmt.Errorf("unmount plaque: %w", cause)
}

// ReplaceCommand swaps the plaque on one wall.
type ReplaceCommand struct {
	TenantID uuid.UUID
	ActorID  uuid.UUID
	// RetiringUID is the plaque coming off the wall. It must be `active`.
	RetiringUID string
	// SuccessorUID is the plaque going on. It must be in stock.
	//
	// 🔴 ITS TYPE IS THE GUARD. It is written into `replaced_by`, whose own SQL
	// predicate accepts the canonical form only, so the field accepts only a value
	// PlaqueUID produced — a lookup key checked by PlaqueRef will not compile here.
	SuccessorUID CanonicalUID
	// VenueName is what the wall is called, for the trail. The WALL ITSELF is not an
	// input: it is read from the retiring plaque inside the transaction, because a
	// replacement is by definition "the same door, a different plaque" and asking a
	// manager to re-pick the venue is a second chance to get it wrong.
	VenueName string
}

// Replacement is what a completed replace did, for the caller's logging and its
// redirect.
type Replacement struct {
	Retired Plaque
	Mounted Plaque
}

// Replace retires the plaque on a wall and binds its successor to the SAME wall,
// in ONE transaction.
//
// 🔴 ONE TRANSACTION IS THE WHOLE POINT AND IT IS A SECTION 4.6 ARGUMENT RATHER
// THAN A TIDINESS ONE. Split across two, a failure between them leaves the old
// plaque retired and the new one still in stock: an entrance where every tap is
// rejected (section 5 row 1), no plaque to mount without a second visit, and
// nothing on any screen saying so. Inside one, the failure rolls the retirement
// back and the manager is told which half refused.
//
// ORDER: RETIRE, THEN BIND. The self-FK (replaced_by, tenant_id) -> tags (uid,
// tenant_id) requires the successor row to exist before the old one can point at
// it -- it does, because Tappa loaded it -- and retiring first means the wall is
// never held by two active plaques even for the width of a statement.
//
// THE LOCATION COMES FROM THE RETIRING ROW, NOT FROM THE CALLER. That is why the
// read below is not a TOCTOU: it does not decide whether the write may happen (the
// statements' own WHERE clauses do that), it only answers WHICH WALL, and the
// retire in the next statement pins the row it read.
func (p *Plaques) Replace(ctx context.Context, c ReplaceCommand) (Replacement, error) {
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return Replacement{}, err
	}
	// The plaque coming OFF is a lookup key; the plaque going ON is written into
	// `replaced_by`, whose own SQL predicate accepts the canonical form only.
	retiring, err := PlaqueRef(c.RetiringUID)
	if err != nil {
		return Replacement{}, err
	}
	// 🔴 RE-VALIDATED EVEN THOUGH THE TYPE SAYS IT WAS CHECKED, AND THIS IS THE REAL
	// MECHANISM RATHER THAN THE TYPE. CanonicalUID records where a value came from,
	// not that it is still true — anything in this package can write
	// CanonicalUID("nonsense"). The type stops a lookup key being handed to this
	// field at COMPILE time; this line is what refuses a bad value at RUN time, and
	// an audit measured that removing it (with the types collapsed) left the entire
	// suite green.
	successor, err := PlaqueUID(string(c.SuccessorUID))
	if err != nil {
		return Replacement{}, err
	}
	if retiring == string(successor) {
		return Replacement{}, ErrSamePlaque
	}

	var out Replacement
	err = p.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		old, err := q.RetireTagForReplacement(ctx, store.RetireTagForReplacementParams{
			ReplacedBy: string(successor), TenantID: c.TenantID, Uid: retiring,
		})
		if err != nil {
			return classifyRetire(ctx, q, c.TenantID, retiring, err)
		}
		// An `active` plaque always has a wall -- tags_active_requires_location, and
		// RetireTagForReplacement demands `status = 'active'`. The check is here because
		// a nil here would mean the schema changed under us, and mounting the successor
		// "nowhere" is not a state this function may invent.
		if old.LocationID == nil {
			return fmt.Errorf("replace plaque: %s was active with no wall", retiring)
		}
		wall := *old.LocationID

		fresh, err := q.AssignTagToLocation(ctx, store.AssignTagToLocationParams{
			LocationID: wall, TenantID: c.TenantID, Uid: string(successor),
		})
		if err != nil {
			return classifyMount(ctx, q, c.TenantID, string(successor), err)
		}

		out.Retired = plaqueOf(plaqueRow{
			UID: old.Uid, LocationID: old.LocationID, LastCtr: old.LastCtr,
			Status: old.Status, RetiredAt: old.RetiredAt, ReplacedBy: old.ReplacedBy,
			CreatedAt: old.CreatedAt,
		})
		out.Mounted = plaqueOf(plaqueRow{
			UID: fresh.Uid, LocationID: fresh.LocationID, LastCtr: fresh.LastCtr,
			Status: fresh.Status, RetiredAt: fresh.RetiredAt, ReplacedBy: fresh.ReplacedBy,
			CreatedAt: fresh.CreatedAt,
		})

		if _, err := p.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionPlaqueRetired,
			Target:   retiring,
			Detail: retiredDetail{
				Name:       string(successor),
				LocationID: wall.String(),
				LastCtr:    strconv.FormatInt(int64(old.LastCtr), 10),
			},
		}); err != nil {
			return err
		}
		_, err = p.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionPlaqueMounted,
			Target:   string(successor),
			Detail: mountedDetail{
				Name:       c.VenueName,
				LocationID: wall.String(),
				Replaces:   retiring,
				FromStatus: PlaqueUnassigned,
			},
		})
		return err
	})
	if err != nil {
		return Replacement{}, wrapPlaque("replace plaque", err)
	}
	p.log.Info("plaque replaced", "retired_tag_uid", retiring,
		"mounted_tag_uid", successor, "actor_id", c.ActorID)
	return out, nil
}

// ConfirmPlaqueAct answers "did THIS admin, in THIS business, do THIS to THIS
// plaque just now?" from the append-only trail.
//
// 🔴 IT DECIDES A SENTENCE, NOT A PERMISSION -- Venues.ConfirmRemoval's rule, and
// the same query behind it. That query is named ConfirmRecentRemoval because a
// removal is what first needed it, and its shape is entirely general: tenant +
// actor + action + target + a server-clock window. A second copy under a nicer
// name would be a second representation of one rule, which is the shape this
// repository has paid for repeatedly -- so it is reused, and this comment is the
// note that the name is narrower than the query.
//
// ⚠️ IT IS BOUND TO THE ACTION, WHICH IS WHAT KEEPS THE WORDS APART. A plain
// mount and a replace BOTH write plaque.mounted for the new uid, so a
// "replaced" acknowledgement verified against plaque.mounted would print after a
// mere mount -- the M6-04 defect (a URL claiming something that did not happen) in
// a new costume. "plaque-replaced" is therefore verified against plaque.RETIRED
// for the OLD uid, which only a replacement writes.
func (p *Plaques) ConfirmPlaqueAct(ctx context.Context, tenantID, actorID uuid.UUID, action, target string) (RemovalReceipt, error) {
	if tenantID == uuid.Nil || actorID == uuid.Nil || action == "" || target == "" {
		return RemovalReceipt{}, nil
	}
	var out RemovalReceipt
	err := p.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		name, err := store.New(tx).ConfirmRecentRemoval(ctx, store.ConfirmRecentRemovalParams{
			TenantID:      tenantID,
			ActorID:       actorID,
			Action:        action,
			Target:        target,
			WindowSeconds: int32(removalWindow / time.Second),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// NOT AN ERROR. "No such act" is the ordinary answer for every address
				// that was not produced by this manager a moment ago.
				return nil
			}
			return fmt.Errorf("confirm plaque act: %w", err)
		}
		out.Found = true
		out.Name = name
		return nil
	})
	if err != nil {
		return RemovalReceipt{}, wrapPlaque("confirm plaque act", err)
	}
	return out, nil
}

// --- refusal classification ----------------------------------------------------

// classifyMount turns a failed bind into the sentence a manager can act on.
//
// 🔴 pgx.ErrNoRows IS AMBIGUOUS BY CONSTRUCTION AND THE SECOND READ IS WHAT
// RESOLVES IT. AssignTagToLocation matches on (tenant, uid, status='unassigned'),
// so zero rows means EITHER "no such plaque here" OR "it is not in stock" -- two
// different things to do about it. The follow-up read runs only on the failure
// path, inside the same transaction, and costs nothing on the ordinary one.
func classifyMount(ctx context.Context, q *store.Queries, tenantID uuid.UUID, uid string, cause error) error {
	if errors.Is(cause, pgx.ErrNoRows) {
		row, err := q.GetTagForTenant(ctx, store.GetTagForTenantParams{TenantID: tenantID, Uid: uid})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrUnknownPlaque
		case err != nil:
			return fmt.Errorf("classify mount: %w", err)
		case row.Status != PlaqueUnassigned:
			return ErrPlaqueNotInStock
		default:
			// The row IS in stock and the statement still matched nothing. There is no
			// third reading, so it is reported as itself rather than guessed at.
			return fmt.Errorf("mount plaque: %s is in stock but the bind matched no row", uid)
		}
	}
	if isCheckViolation(cause) {
		return ErrPlaqueFrozen
	}
	if isForeignKeyViolation(cause) {
		// The composite FK refused the wall: it is not a location of this tenant.
		return ErrUnknownVenue
	}
	return fmt.Errorf("mount plaque: %w", cause)
}

// classifyRetire is the same for the retirement half.
//
// ⚠️ ITS ErrNoRows HAS A THIRD CAUSE THE MOUNT'S DOES NOT: RetireTagForReplacement
// also demands that the SUCCESSOR uid match the canonical form, on the uncast
// parameter (db/queries/tags.sql explains why). PlaqueUID has already refused a
// non-canonical successor at this package's own boundary, so reaching that arm
// would mean the two forms disagree -- which is why the fallback below reports
// itself instead of inventing a sentence.
func classifyRetire(ctx context.Context, q *store.Queries, tenantID uuid.UUID, uid string, cause error) error {
	if errors.Is(cause, pgx.ErrNoRows) {
		row, err := q.GetTagForTenant(ctx, store.GetTagForTenantParams{TenantID: tenantID, Uid: uid})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrUnknownPlaque
		case err != nil:
			return fmt.Errorf("classify retire: %w", err)
		case row.Status != PlaqueActive:
			return ErrPlaqueNotOnAWall
		default:
			return fmt.Errorf("retire plaque: %s is active but the retire matched no row", uid)
		}
	}
	if isCheckViolation(cause) {
		return ErrPlaqueFrozen
	}
	if isForeignKeyViolation(cause) {
		// The self-FK refused the successor: no such plaque in this tenant.
		return ErrUnknownPlaque
	}
	return fmt.Errorf("retire plaque: %w", cause)
}

// isCheckViolation reports whether Postgres refused the statement because a CHECK
// constraint said no (SQLSTATE 23514).
//
// 🔴 IT MATCHES THE CODE, NOT THE MESSAGE, for the reason isForeignKeyViolation
// gives: an error STRING is a locale- and version-dependent sentence. What raises
// it here is `tags_uid_canonical_hex` on a frozen development row -- see
// ErrPlaqueFrozen for why those rows exist and why the panel must answer a
// sentence rather than a 500.
func isCheckViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23514"
}

// --- row adapters ---------------------------------------------------------------

// plaqueRow is the shape the tag queries return, named once so the
// call sites do not each re-list the column set.
//
// 🔴 IT HAS NO aes_key_ref FIELD AND THAT IS THE TYPE WALL'S INNER RING. Even a
// future query that selected the column could not be assigned into this struct
// without a visible edit here.
type plaqueRow struct {
	UID        string
	LocationID *uuid.UUID
	LastCtr    int32
	Status     string
	RetiredAt  *time.Time
	ReplacedBy *string
	CreatedAt  time.Time
}

func plaqueOf(r plaqueRow) Plaque {
	return Plaque{
		UID:        r.UID,
		Status:     r.Status,
		LocationID: r.LocationID,
		LastCtr:    r.LastCtr,
		RetiredAt:  r.RetiredAt,
		ReplacedBy: deref(r.ReplacedBy),
		LastSeen:   nil,
		CreatedAt:  r.CreatedAt,
		Canonical:  plaqueUIDRE.MatchString(r.UID),
	}
}

// plaqueScreenOf merges the list with the last-seen stamps and derives the
// backward half of the replacement chain.
//
// 🔴 THE MERGE IS BY KEY PRESENCE, NEVER BY ZERO VALUE. A plaque with no entry in
// the stamp map has never been tapped and keeps LastSeen nil.
//
// ⚠️ A nil TagUid IS SKIPPED RATHER THAN KEYED ON "". ListTagLastSeen filters
// `tag_uid IS NOT NULL` in SQL, so this cannot happen today -- but sqlc types the
// column as *string because the COLUMN is nullable, and a nil dereferenced into a
// map key would silently create an entry no plaque can match while looking like it
// worked.
func plaqueScreenOf(rows []store.ListTagsForTenantRow, seen []store.ListTagLastSeenRow) PlaqueScreen {
	stamps := make(map[string]time.Time, len(seen))
	for _, s := range seen {
		if s.TagUid == nil {
			continue
		}
		stamps[*s.TagUid] = s.LastSeen
	}
	// predecessor maps a uid onto the plaque it took over from, derived from the
	// forward pointers the schema stores.
	predecessor := make(map[string]string, len(rows))
	for _, r := range rows {
		if r.ReplacedBy != nil && *r.ReplacedBy != "" {
			predecessor[*r.ReplacedBy] = r.Uid
		}
	}

	out := PlaqueScreen{Plaques: make([]Plaque, 0, len(rows))}
	for _, r := range rows {
		if len(out.Plaques) >= plaquePageLimit {
			out.Capped = true
			break
		}
		p := plaqueOf(plaqueRow{
			UID: r.Uid, LocationID: r.LocationID, LastCtr: r.LastCtr,
			Status: r.Status, RetiredAt: r.RetiredAt, ReplacedBy: r.ReplacedBy,
			CreatedAt: r.CreatedAt,
		})
		if t, ok := stamps[r.Uid]; ok {
			stamp := t
			p.LastSeen = &stamp
		}
		p.Replaces = predecessor[r.Uid]
		out.Plaques = append(out.Plaques, p)
	}
	return out
}

// wrapPlaque adds the operation to an unexpected failure and leaves the typed
// refusals alone, so callers can still errors.Is them -- wrapVenue's rule.
func wrapPlaque(op string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrUnknownPlaque),
		errors.Is(err, ErrPlaqueNotInStock),
		errors.Is(err, ErrPlaqueNotOnAWall),
		errors.Is(err, ErrPlaqueNoWall),
		errors.Is(err, ErrSamePlaque),
		errors.Is(err, ErrPlaqueUID),
		errors.Is(err, ErrPlaqueFrozen),
		errors.Is(err, ErrUnknownVenue):
		return err
	default:
		return fmt.Errorf("tenant: %s: %w", op, err)
	}
}
