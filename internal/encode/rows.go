package encode

import (
	"context"
	"errors"
	"fmt"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The two trail events an encode round writes — ADR 0017 §6 md. 8, decided in
// M8-05 FAZ B2c-2a.
//
// 🔴 THEY ARE DECLARED IN internal/domain/tenant AND ALIASED HERE, AND THE
// INDIRECTION IS THE MECHANISM. internal/handler derives the panel's word list from
// THAT package's source with go/ast, and its counted-limit table named exactly one
// shape it cannot see: an action written from another package. This round is that
// shape. Declaring the two constants where the scan already looks turns "the panel
// prints a raw database word at a manager" into a RED TEST — measured: adding them
// without a word failed TestPlaqueTrail_NamesEveryActionTheDOMAINCanWrite naming
// both, before either could reach a screen.
//
// 🔴 THE SPELLING IS THE TREE'S, NOT THE ADR'S, AND THAT IS A CORRECTION TO ADR
// 0017. §6 md. 8 asks for a name "consistent with the existing vocabulary — the
// `tag.retired` pattern". There is no `tag.*` action anywhere; the shipped
// constants are plaque.mounted, plaque.retired and plaque.unmounted. The ADR named
// a pattern that does not exist, so the tree's wins — `plaque.<past participle>`,
// one action per ROW TOUCHED.
//
// ⚠️ AND THE COMMAND PUBLISHED HERE WAS WRONG THE FIRST TIME, WHICH MATTERS MORE
// THAN THE CONCLUSION DID. It said `grep -rn '"tag\.' internal/ db/` "returns
// nothing at all". Re-run on this tree: SIX hits — internal/db/rls_test.go lines
// 1344, 1347, 1353, 1356, 1359 and 1376, every one of them a `t.Errorf("tag.UID =
// …")` format string, i.e. a Go field selector and not an event name. The
// conclusion survived; the evidence offered for it did not. The narrow command,
// whose output really is zero:
//
//	grep -rn '"tag\.[a-z]' internal/ db/     ->  0
//
// A published measurement has to be reproducible in THIS tree, or it is a sentence
// wearing a command's clothes.
//
// 🔴 ListPlaqueHistory PICKS THEM UP FOR FREE, and that is a consequence worth
// naming rather than discovering. db/queries/audit.sql filters on the PREFIX
// `plaque.%` precisely so "whatever the next plaque act is called" appears without a
// query change. So the panel's plaque card starts showing these two rows the day an
// endpoint writes them, with the actor column reading "by the system" — see the
// Event construction below for why that is the honest rendering today and what
// would change it.
//
// 🔴 TWO EVENTS AND NOT ONE, AND COLLAPSING THEM WAS MEASURED AS WRONG IN BOTH
// DIRECTIONS. ADR 0017 §5.2 writes the row BEFORE the chip's first irreversible
// command, so the two facts are genuinely different and a round can produce the
// first without the second:
//
//	only the END event   -> a plaque whose round died mid-flight has an EMPTY
//	                        trail. ListPlaqueHistory answers zero rows and the card
//	                        says nothing has been done to a plaque Tappa itself
//	                        created a row for. §5.2's rule is that a failed round
//	                        KEEPS its row ("sessiz temizlik yoktur"); a row with no
//	                        trail entry honours that on the table and drops it on
//	                        the trail.
//	only the START event -> a completed round and an abandoned one look the same in
//	                        the trail, which is the exact distinction migration
//	                        00022 exists to make.
//
// So: both. ⚠️ THE COST, COUNTED — AND THE FIRST COUNT WAS SHORT (audit,
// 2026-08-24): two rows per successful encode, PLUS ONE PER RETRIED MARKING. The
// marker is idempotent on the ROW (MarkTagEncoded's coalesce) and NOT on the trail,
// because audit_log is append-only: a second call writes a second, true entry. The
// package's own test drives that ("plaque.encoded entries = 2"), so the two
// sentences contradicted each other inside one change set. In a table with no
// retention job (backlog T6/T13) the honest bound is per CALL, not per encode.
//
// ⚠️ AND ActionPlaqueEncoded IS NOT A CERTIFICATE THAT THE PLAQUE IS SAFE TO MOUNT,
// which 00022's own comment says about the column too. ADR 0017 §5.1 step 8
// (application key 0) is NORMATIVE but BLOCKED on the schema decision §6 md. 5
// records, so an "encoded" plaque today still carries the PUBLIC factory master
// key. What may not happen is the wall (M8-06's pilot gate, item 7); building and
// testing is unaffected.
const (
	// ActionPlaqueLoaded: the inventory row was created. ADR 0017 §5.1 step 3.
	ActionPlaqueLoaded = tenant.ActionPlaqueLoaded
	// ActionPlaqueEncoded: the chip completed its round. ADR 0017 §5.1 step 9.
	ActionPlaqueEncoded = tenant.ActionPlaqueEncoded
)

// database is the narrow slice of *db.DB this file needs, declared at the CONSUMER
// (CLAUDE.md §7) — the same shape internal/audit declares for itself.
type database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// trail is the narrow slice of *audit.Recorder this file needs.
//
// 🔴 RecordTx AND NOT Record, AND THE CHOICE IS THE WHOLE POINT OF THE PORT.
// audit.Recorder documents both: Record opens its OWN transaction so a failed
// caller still leaves a trace, RecordTx joins the caller's so the entry "commits or
// rolls back WITH that caller's work … when the event is only true if the
// surrounding change is true". Both of these events are of the second kind. An
// entry written in its own transaction could survive a rolled-back INSERT and
// describe a plaque that does not exist — a trail row is supposed to be evidence,
// and evidence for something that did not happen is worse than silence.
type trail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
}

// loadedDetail is ActionPlaqueLoaded's jsonb payload.
//
// 🔴 §4.7 IS KEPT BY THE TYPE, NOT BY CARE. internal/audit's package doc states the
// rule: the detail is "a typed, hand-written detail struct per event — never … a
// map of whatever was in scope". Neither field can hold key material: KeyBytes is an
// INT (a length, which §4.7 explicitly sanctions as the way to verify without
// showing) and ClaimedBy is the operator label Begin bounded. The wrapped envelope,
// the plain plaque key, the session keys, the session handle and the CMAC are not
// reachable from this struct — there is no field of any of their shapes.
//
// 🔴 THE FIELD IS "claimed_by" AND NOT "actor", DELIBERATELY. audit_log.actor_id is
// the column a manager's screen renders as "who did this", and it is left NULL by
// this flow — see the Event construction below. Naming the json key `actor` would
// invite a future reader to promote an UNVERIFIED string into that position. The
// name says what the value is: a claim by the caller, worth keeping for forensics
// and worth nothing as authority.
type loadedDetail struct {
	ClaimedBy string `json:"claimed_by"`
	// KeyBytes records that a full-size envelope was stored — 44, always, because
	// InsertUnassigned refuses anything else before it reaches SQL. It is here so
	// the trail can be read on its own the day migration 00021's
	// tags_aes_key_ref_is_kek_envelope is examined for a specific plaque.
	KeyBytes int `json:"key_bytes"`
}

// encodedDetail is ActionPlaqueEncoded's payload. It carries no length because
// nothing was stored: the marker is a server-side timestamp on a row that already
// existed.
type encodedDetail struct {
	ClaimedBy string `json:"claimed_by"`
}

// DBRows is the production Rows: Postgres through sqlc, as tappa_app, with an
// audit_log entry inside each statement's own transaction.
//
// 🔴 IT CONNECTS AS tappa_app AND HAS NO CHOICE, which ADR 0017 §3.1 established by
// measurement rather than preference: the encode flow is an HTTP endpoint, so it
// runs in the server process, and internal/db/pool.go refuses a DSN whose role is a
// superuser, holds BYPASSRLS, or owns an RLS table. ⚠️ §3.1's own qualifier belongs
// here too — that refusal is PRODUCTION-only (roleRefusal returns nil unless
// cfg.IsProd()), so a developer machine warns instead of refusing, and a round driven
// over an owner DSN in development does not demonstrate the shipped behaviour.
//
// 🔴 WHAT THIS TYPE DOES NOT DO, named because the gap is the round's largest:
// it does not decide whether the caller may write to tenantID. It writes where it is
// told. ADR 0017 §6 md. 10 is that gate and it is open. What the database still
// refuses is a MISMATCH — db.WithTenant sets app.tenant_id from the same value the
// statement names, and the tags policy's WITH CHECK plus tappa_app's NOBYPASSRLS mean
// a row cannot land in a different tenant than the transaction's. A caller who names
// somebody else's tenant outright is refused by nothing here.
type DBRows struct {
	data  database
	trail trail
}

// NewDBRows builds the production Rows. Both dependencies are injected as struct
// fields (CLAUDE.md §7); neither is optional, because a Rows that silently skipped
// the trail would satisfy the interface and quietly drop ADR 0017 §5.2's obligation.
func NewDBRows(data database, t trail) (*DBRows, error) {
	if data == nil {
		return nil, errors.New("encode: nil database")
	}
	if t == nil {
		return nil, errors.New("encode: nil audit trail")
	}
	return &DBRows{data: data, trail: t}, nil
}

// InsertUnassigned implements Rows — ADR 0017 §5.1 step 3.
//
// THE ROW AND ITS TRAIL ENTRY SHARE ONE TRANSACTION. See the trail interface for
// why, and note the direction of the guarantee: if the INSERT fails there is no
// entry, and if the entry fails there is no row. The second half matters because
// the chip has not been touched yet — driver.go's own comment says failing here
// "costs nothing but the round" — so rolling back is free, and it is the only
// point in the round where that is true.
func (r *DBRows) InsertUnassigned(ctx context.Context, tenantID uuid.UUID, uidHex string, wrappedKey []byte, actor string) error {
	if err := checkPlaqueArgs(tenantID, uidHex, actor); err != nil {
		return err
	}
	// 🔴 THE ENVELOPE'S LENGTH IS CHECKED HERE, IN GO, AND THE REASON HAS BEEN
	// RE-MEASURED AND NARROWED (audit, 2026-08-24). It used to rest on 00021's
	// finding that a CHECK violation's DETAIL line is the WHOLE FAILING TUPLE —
	// aes_key_ref included — in a server log this repository does not control, so
	// refusing here kept the flow from ever asking the database to print anything.
	//
	// ⚠️ THAT PREMISE IS FALSE FOR THIS PATH, WHICH RUNS AS tappa_app. Measured on
	// two roles, with `employees` as a control (where tappa_app holds FULL table
	// SELECT): a role that is neither rolsuper nor rolbypassrls gets NO DETAIL LINE
	// AT ALL, because with RLS active PostgreSQL never builds the tuple description.
	// The channel this guard was justified by does not exist for this role.
	//
	// THE GUARD STAYS, ON THE REASONS THAT SURVIVE MEASUREMENT: it fails fast and
	// locally, without opening a transaction; its message names a LENGTH rather than
	// a constraint name; and the DETAIL channel IS real for tappa_owner, which is who
	// runs migrations and the rotatekek runbook. What it is not is the last line
	// between the envelope and a log file.
	//
	// The message reports a LENGTH and never a byte, which is exactly what §4.7
	// sanctions as verification without disclosure.
	//
	// ⚠️ THE MESSAGE NAMES NO COLUMN, AND THAT IS NOT AN AESTHETIC CHOICE. Spelling
	// the column here tripped scripts/redline-check.sh's R7 — a FALSE POSITIVE (both
	// arguments are ints) — and session.go's Close already records the right answer
	// to exactly this, in exactly these words: "the right answer was not an
	// exemption … a §4.7 scanner that has to be argued with is a scanner that gets
	// argued with again". Measured: with the column named, redline-check exits 1.
	if len(wrappedKey) != sun.WrappedKeyLen {
		return fmt.Errorf("encode: the wrapped plaque key is %d bytes, the envelope is fixed at %d (ADR 0003 md. 4)", len(wrappedKey), sun.WrappedKeyLen)
	}

	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.InsertUnassigned(ctx, store.InsertUnassignedParams{
			Uid:       uidHex,
			TenantID:  tenantID,
			AesKeyRef: wrappedKey,
		})
		if err != nil {
			// pgx returns *pgconn.PgError, whose Error() is severity + message +
			// SQLSTATE and does NOT include Detail (migration 00021 read that in
			// pgx/v5's pgconn/errors.go). So a unique-violation on uid wraps as
			// "duplicate key value violates unique constraint tags_pkey" and the
			// "Key (uid)=(…)" line stays behind — which is fine either way, since a
			// uid is public (ADR 0003 md. 1).
			return fmt.Errorf("insert the plaque row: %w", err)
		}
		_, err = r.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: tenantID,
			// 🔴 NIL, AND IT IS A DECISION WITH A MEASUREMENT BEHIND IT (ADR 0017
			// §6 md. 8's second question). audit_log.actor_id is a uuid that
			// db/queries/audit.sql LEFT JOINs to admin_users to render a NAME, and
			// RecordAuditEvent's own comment fixes the rule: NULL "when the actor
			// is the SYSTEM", and "an employee activating themselves is NOT an
			// admin actor". The encode flow's actor is a caller-supplied STRING
			// (see Begin) that nothing has authenticated and that is not an
			// admin_users.id at all. Putting it there would need it to be a uuid,
			// and a caller-supplied uuid in that column is a name on a screen that
			// nobody verified — the exact misattribution ListPlaqueHistory's
			// by_system column was added to prevent.
			//
			// ⚠️ THE COST, STATED RATHER THAN GLOSSED: the plaque-history card will
			// render these two rows as "by the system", which is not true — a human
			// ran the encode. It is the honest reading of the ROW, and the row is
			// what it is because md. 10's gate does not exist. The day that gate
			// lands, the admin's real id goes here and this paragraph expires.
			ActorID: nil,
			Action:  ActionPlaqueLoaded,
			Target:  row.Uid,
			Detail:  loadedDetail{ClaimedBy: actor, KeyBytes: len(wrappedKey)},
		})
		if err != nil {
			return fmt.Errorf("record the plaque.loaded event: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("encode: load plaque %s: %w", uidHex, err)
	}
	return nil
}

// MarkEncoded implements Rows — ADR 0017 §5.1 step 9.
//
// IDEMPOTENT, and the idempotence is the STATEMENT's (db/queries/tags.sql:
// `SET encoded_at = coalesce(encoded_at, now())`), not a check-then-write here. A
// read-then-write would be the TOCTOU shape CLAUDE.md §4.4 forbids for the counter,
// applied to a different column, and it would race two retries into two timestamps.
//
// 🔴 pgx.ErrNoRows MEANS "NO SUCH PLAQUE IN THIS TENANT" AND NOTHING ELSE. It cannot
// mean "already encoded", because the statement has no `encoded_at IS NULL`
// predicate — see the query's own comment for why that predicate was refused. So the
// error below can say what it says without qualification.
func (r *DBRows) MarkEncoded(ctx context.Context, tenantID uuid.UUID, uidHex string, actor string) error {
	if err := checkPlaqueArgs(tenantID, uidHex, actor); err != nil {
		return err
	}
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.MarkTagEncoded(ctx, store.MarkTagEncodedParams{Uid: uidHex, TenantID: tenantID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errors.New("no such plaque in this tenant")
			}
			return fmt.Errorf("stamp the encoded marker: %w", err)
		}
		_, err = r.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: tenantID,
			ActorID:  nil, // See InsertUnassigned for why.
			Action:   ActionPlaqueEncoded,
			Target:   row.Uid,
			Detail:   encodedDetail{ClaimedBy: actor},
		})
		if err != nil {
			return fmt.Errorf("record the plaque.encoded event: %w", err)
		}
		return nil
	})
	if err != nil {
		// ⚠️ THE CALLER TREATS THIS AS BOOKKEEPING, NOT AS A ROUND FAILURE, and the
		// wording has to survive that. Store.markEncoded returns Progress.Done TRUE
		// alongside this error and tells the operator NOT to re-run: the chip is
		// personalised whatever the database says.
		return fmt.Errorf("encode: mark plaque %s encoded: %w", uidHex, err)
	}
	return nil
}

// checkPlaqueArgs is the ONE place both methods validate their shared arguments, so
// "every entry point checks the tenant" is a property of a single function rather
// than a sentence about two.
//
// ⚠️ WHAT IT DOES NOT CHECK, counted: that uidHex is canonical upper-case hex. That
// is migration 00013's tags_uid_canonical_hex, it is VALIDATED as of 00021 Part 3,
// and it fires for every role. Re-implementing the regex here would put the
// canonical form in a second place — which is the precise defect 00013 Part 1
// describes for uid spellings. The value this flow passes comes from
// sun.ParseGetVersion, which produces the canonical form by construction.
func checkPlaqueArgs(tenantID uuid.UUID, uidHex, actor string) error {
	if tenantID == uuid.Nil {
		return errors.New("encode: a plaque row needs a tenant")
	}
	if uidHex == "" {
		return errors.New("encode: a plaque row needs a uid")
	}
	if actor == "" {
		return errors.New("encode: a plaque row needs an actor label")
	}
	if len(actor) > MaxActorLen {
		return fmt.Errorf("encode: the actor label is %d bytes, at most %d are accepted", len(actor), MaxActorLen)
	}
	return nil
}
