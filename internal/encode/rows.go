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
// 🔴 THREE EVENTS SINCE 2026-08-24, AND THE THIRD ANSWERS A DIFFERENT QUESTION FROM
// THE OTHER TWO. plaque.loaded and plaque.encoded describe a round that WORKED, at its
// two persistent moments; plaque.unmarked describes the one that did not, in the only
// way that matters — the chip is personalised and the row does not say so. Without it
// that state was byte-identical to a round that never touched the chip, and the two
// have opposite recovery instructions (security audit F3). See
// tenant.ActionPlaqueUnmarked, and note that it is written with Record rather than
// RecordTx for the reason the trail interface gives.
//
// 🔴 THE FIRST TWO ARE TWO AND NOT ONE, AND COLLAPSING THEM WAS MEASURED AS WRONG IN BOTH
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
	// ActionPlaqueUnmarked: the chip completed and the row could NOT be marked. See
	// tenant.ActionPlaqueUnmarked for the two states it separates and why one log line
	// was not enough to separate them.
	ActionPlaqueUnmarked = tenant.ActionPlaqueUnmarked
)

// database is the narrow slice of *db.DB this file needs, declared at the CONSUMER
// (CLAUDE.md §7) — the same shape internal/audit declares for itself.
type database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// trail is the narrow slice of *audit.Recorder this file needs.
//
// 🔴 BOTH ENTRY POINTS, AND WHICH ONE IS USED IS THE WHOLE POINT OF THE PORT.
// audit.Recorder documents the difference: Record opens its OWN transaction so a
// failed caller still leaves a trace, RecordTx joins the caller's so the entry
// "commits or rolls back WITH that caller's work … when the event is only true if the
// surrounding change is true".
//
//	plaque.loaded / plaque.encoded   RecordTx. They are only true if the statement
//	                                 beside them committed. An entry in its own
//	                                 transaction could survive a rolled-back INSERT and
//	                                 describe a plaque that does not exist — evidence
//	                                 for something that did not happen is worse than
//	                                 silence.
//	plaque.unmarked                  Record. It is true PRECISELY BECAUSE the
//	                                 surrounding statement did NOT commit, so joining
//	                                 that transaction would roll the evidence back with
//	                                 the thing it is evidence of.
//
// ⚠️ Record WAS ADDED IN THE THIRD ROUND AND ITS ABSENCE WAS THE DEFECT, not an
// omission: with only RecordTx there was no way to record a FAILED marking at all, so
// the round's worst outcome and its most harmless one left byte-identical rows. See
// tenant.ActionPlaqueUnmarked.
type trail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
	Record(ctx context.Context, e audit.Event) (uuid.UUID, error)
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

// unmarkedDetail is ActionPlaqueUnmarked's payload.
//
// 🔴 IT CARRIES THE SAME ONE FIELD AND DELIBERATELY NOT THE ERROR. A database error's
// text is diagnostic and belongs in the log; what this row has to say is the FACT —
// this uid's chip is personalised and its row is not marked — and the fact is fully
// carried by the action name plus the target. A message field here would be a place
// for whatever a driver happened to print, on a row nobody can ever delete.
type unmarkedDetail struct {
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
// told. ⚠️ THAT GATE IS ADR 0017 §6 md. 10 AND IT CLOSED IN FAZ B2c-2b — one layer up,
// in internal/handler, where a request has an identity to answer it with. This
// sentence said "and it is open" for a round after it shut; internal/encode's package
// doc was corrected in the same change set and this one was missed. What the database still
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
func (r *DBRows) InsertUnassigned(ctx context.Context, tenantID, adminID uuid.UUID, uidHex string, wrappedKey []byte, actor string) error {
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
			// ✅ THE ADMIN'S REAL ID, SINCE FAZ B2c-2b's FOURTH AUDIT — and the
			// paragraph that used to stand here PREDICTED ITS OWN EXPIRY AND THEN
			// OUTLIVED IT BY A ROUND, which is why the retraction is kept.
			//
			// It said: NIL, because "the encode flow's actor is a caller-supplied
			// STRING that nothing has authenticated … a caller-supplied uuid in that
			// column is a name on a screen that nobody verified", and it closed with
			// "the day that gate lands, the admin's real id goes here and this
			// paragraph expires". ADR 0017 §6 md. 10's gate LANDED in this same
			// phase; the paragraph did not expire, and the plaque card kept saying
			// "by the system" for something a person did.
			//
			// 🔴 WHAT CHANGED IS THE AUTHORITY, NOT THE OPINION. The id no longer
			// comes from a label: internal/handler derives it from
			// httpx.AdminOf(r).Admin — the session row a cookie HASH matched — and it
			// travels here as its own typed argument. actorIDOf carries the rest,
			// including why `actor` is still never parsed for it and why uuid.Nil
			// still means the system.
			ActorID: actorIDOf(adminID),
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
//
// 🔴 AND IT IS THE ONE ARM THAT MAKES THIS FUNCTION WRITE A ROW THAT IS NOT TRUE —
// COUNTED, NOT CLOSED (eleventh audit, 2026-08-25). ErrNoRows leaves the write path
// perfectly intact, so by the property below the failure IS recorded: a plaque.unmarked
// row appears. But that event means "the chip WAS personalised and the row could not be
// marked — do NOT re-encode", and internal/handler renders it to a manager in exactly
// those words. Measured:
//
//	tenant B marks tenant A's uid    -> plaque.unmarked in B = 1, in A = 0
//	a uid that exists nowhere        -> plaque.unmarked = 1, with no tags row at all
//
// In both, the row asserts a chip was personalised when none was, in an append-only
// table no role can delete.
//
// ⚠️ THE LIMIT THIS EXPOSES IS IN THE PROPERTY ITSELF, and saying so is the point of
// counting it: the property binds WHAT CAN BE RECORDED. It says nothing about whether
// what is recorded is TRUE. This is the only arm where those two come apart.
//
// NOT REACHABLE FROM THE ENDPOINT, which is why it is counted rather than fixed:
// Store.markEncoded runs only after Progress.Done, with the session's own tenantID and
// uidHex, in a round whose InsertUnassigned already succeeded for that pair. A caller
// would have to use DBRows directly. Hand-over list, md. 28.
func (r *DBRows) MarkEncoded(ctx context.Context, tenantID, adminID uuid.UUID, uidHex string, actor string) error {
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
			ActorID:  actorIDOf(adminID),
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
		//
		// 🔴 AND THE FACT GETS A PERMANENT HOME BEFORE THE ERROR IS RETURNED. Until
		// the third round it did not: the marking and its trail entry share one
		// transaction, so a failure rolled BOTH back, and the database was left
		// byte-identical to a round that never touched the chip — same status, same
		// NULL location, same NULL encoded_at, same lone plaque.loaded. The two have
		// OPPOSITE recovery instructions and the only thing telling them apart was one
		// line in the process log, which log rotation removes. audit_log cannot be
		// deleted by any role (00005's trigger), so that is where the distinction
		// belongs.
		//
		// Record, not RecordTx: this event is true precisely because the transaction
		// above did not commit (see the trail interface).
		//
		// 🔴 AND ON A CONTEXT DETACHED FROM THE REQUEST, WHICH IS THE WHOLE REASON THIS
		// ENTRY EXISTS AT ALL (security audit, 2026-08-24, second pass). It used to
		// reuse ctx — the REQUEST's context, since plaqueEncodeStep calls Step with
		// r.Context() — so a cancelled request failed the marking AND the evidence of
		// the marking, for one reason, at once.
		//
		// 🔴 AND IN THE NINTH ROUND THE CALLER WAS DETACHED TOO, WHICH CHANGES WHAT
		// THIS EVENT MEANS. session.go's markEncoded now runs the whole marking on a
		// detached context, so the ORDINARY cancellation — the phone posting its last
		// R-APDU and hanging up — is REPAIRED rather than recorded: encoded_at is
		// stamped and no row lands here at all. Measured, cancelled ctx both ways:
		//
		//	request ctx   err=true   encoded_at=<nil>  unmarked=1
		//	detached      err=false  encoded_at=set    unmarked=0
		//
		// 🔴 SO WHY DOES THIS ENTRY STILL EXIST? Because the two writes answer
		// DIFFERENT failures, and collapsing them would lose the second:
		//
		//	REPAIRED by the detach   the request went away. Nothing is wrong with the
		//	                         database; only the caller stopped listening.
		//	RECORDED here            the marking failed for a reason that leaves this
		//	                         write's own path intact — a constraint refusing
		//	                         the row, a logical error, a bug. The chip is
		//	                         already personalised, so a durable record is the
		//	                         only thing standing between the operator and a
		//	                         plaque that looks untouched.
		//
		// 🔴 AND THE SECOND ROW USED TO CLAIM MORE THAN IT CAN DELIVER — it read "the
		// database is genuinely out of reach: pool exhausted, network gone, the budget
		// spent, a constraint refusing". Some of those defeat THIS write too, because
		// it goes through the same pool to the same database.
		//
		// ⚠️ AND "SOME" IS DELIBERATE, BECAUSE THE REPLACEMENT SENTENCE SAID "THREE OF
		// THOSE FOUR" AND THAT COUNT WAS ALSO WRONG (eleventh audit). A spent budget is
		// RECORDABLE — the compensation gets a fresh deadline. Counting is what keeps
		// failing here; the property below is what decides, and it is stated once.
		//
		// The detach made this entry RARE. It did not make it unnecessary — it made it
		// the record of a real fault instead of the record of a hang-up.
		//
		// context.WithoutCancel keeps the tenant GUC and the pool but drops the
		// cancellation AND the deadline; the timeout stops a hung write from outliving
		// the request by more than a moment. The shape is internal/handler/health.go's
		// — context.WithTimeout(context.WithoutCancel(ctx), <named constant>) — which
		// is the tree's existing precedent for exactly this, and is cited here after an
		// audit measured that the file this comment used to name, internal/db/tenant.go,
		// contains no WithoutCancel at all: it uses a bare context.Background() with no
		// timeout. Same REASONING, different shape; the shape belongs to health.go.
		trailCtx, cancelTrail := context.WithTimeout(context.WithoutCancel(ctx), DefaultRepairGrace)
		defer cancelTrail()
		if _, aerr := r.trail.Record(trailCtx, audit.Event{
			TenantID: tenantID,
			ActorID:  actorIDOf(adminID),
			Action:   ActionPlaqueUnmarked,
			Target:   uidHex,
			Detail:   unmarkedDetail{ClaimedBy: actor},
		}); aerr != nil {
			// Both errors matter and neither may hide the other: the marking failed AND
			// the only permanent record of that failure could not be written. The
			// caller is told about the first (it decides what the operator sees); the
			// second is joined to it so nothing is swallowed (CLAUDE.md §7).
			return fmt.Errorf("encode: mark plaque %s encoded: %w (and the %s trail entry "+
				"could not be written either: %v)", uidHex, err, ActionPlaqueUnmarked, aerr)
		}
		return fmt.Errorf("encode: mark plaque %s encoded: %w", uidHex, err)
	}
	return nil
}

// 🔴 WHAT THIS COMPENSATION CAN AND CANNOT RECORD — A PROPERTY, NOT A LIST, BECAUSE
// THE LIST WAS WRONG (tenth audit, 2026-08-25). The ninth round wrote the limit as
// "process death": SIGKILL, OOM, a pod past its grace period. That was true and it was
// far too narrow, and this file's own sentence explains why the shape fails — a
// promise whose limits are unwritten gets read as unlimited, and so does one whose
// limits are written as a short list.
//
//	THE COMPENSATION TRAVELS THE PATH IT REPORTS ON. It writes through the same pool,
//	to the same database, from the same process as the marking it compensates for. So
//	it can record only a fault that leaves that path INTACT. Any fault IN the path
//	takes the compensation with it.
//
// 🔴 NOW DERIVE FROM IT RATHER THAN ENUMERATING AGAIN. The first derivation written
// under this property said "of the four causes, only a constraint refusing is
// recordable", and that was ANOTHER LIST and it was MEASURED WRONG (eleventh audit,
// 2026-08-25). Eleven rounds produced eleven lists and all eleven were incomplete;
// the property does the work if it is actually applied.
//
// APPLY IT: which parts of the path does the compensation genuinely SHARE?
//
//	SHARED      the pool, the database, the process. A fault in any of these is
//	            reported by a write that must itself traverse them, so it cannot be
//	            recorded.
//	NOT SHARED  the DEADLINE. context.WithoutCancel drops the parent's deadline as
//	            well as its cancellation, so the compensation starts a FRESH budget.
//
// So a fault confined to the marking's own deadline IS recordable — the compensation
// is not standing in the failure it reports. This file's neighbour states the same
// thing from the other side (session.go, DefaultRepairGrace: "the second write still
// gets a FULL budget, not the remainder ... that is precisely the case it exists
// for"), and the first derivation contradicted it in the same diff. Measured:
//
//	healthy pool, the MARKING's own budget spent
//	  err = mark plaque F81892FD5B927E encoded: db: begin tx: context deadline exceeded
//	  -> plaque.unmarked = 1, plaque.encoded = 0        RECORDED
//
// ⚠️ AND THE COUNTER-CASE PRODUCES A BYTE-IDENTICAL ERROR STRING, WHICH IS WHY THE
// CONDITION IS PART OF THE EVIDENCE AND NOT A FOOTNOTE. Under pool_max_conns=1 with
// the single connection already held, pool.Begin waits for a connection it will never
// get and times out with the same words — and the compensation, needing that same
// connection, fails identically:
//
//	pool_max_conns=1, pool saturated
//	  err = mark plaque BA6449CA48DB40 encoded: db: begin tx: context deadline exceeded
//	        (and the plaque.unmarked trail entry could not be written either:
//	         audit: record "plaque.unmarked": db: begin tx: context deadline exceeded)
//	  -> encoded_at NULL, NO plaque.unmarked row              NOT RECORDED
//
// Same string, opposite outcome, process healthy in both. What separates them is not
// the message but whether the POOL could still hand out a connection — which is the
// property, restated by measurement. An error string quoted without its condition
// cannot distinguish the class it is offered as evidence for.
//
// ⚠️ SO WHAT IS IT FOR? It is worth keeping and it is not a guarantee. It converts
// SOME silent losses into recorded ones, at no cost, and it is the only in-process
// mechanism that can. What it cannot do is bound the failure, because no write into a
// resource can be the witness for that resource's own failure.
//
// 🔴 THE REAL FIX IS OUT OF PROCESS AND IS NOT WRITTEN YET: an idempotent
// reconciliation pass over rows with a wrapped key but no encoded_at, which needs
// neither the request nor the process that lost it. Hand-over list, md. 27 — whose
// scope the tenth audit widened from "ungraceful death" to "every fault that also
// defeats the compensation", the ordinary database outage being both the commonest
// case and the one nobody was counting.

// actorIDOf turns the round's admin id into audit_log.actor_id.
//
// 🔴 IT WRITES A REAL ID WHERE THE FIRST VERSION WROTE nil, AND THE nil WAS RIGHT AT
// THE TIME (security audit N3, 2026-08-24). The old comment argued the case exactly:
// audit_log.actor_id is what db/queries/audit.sql LEFT JOINs to admin_users to render
// a NAME, and the only actor this flow had was a caller-supplied STRING that nothing
// had authenticated. Putting that in the column would have been "a name on a screen
// that nobody verified".
//
// What changed is the AUTHORITY, not the opinion. ADR 0017 §6 md. 10's gate landed in
// FAZ B2c-2b: internal/handler derives the id from httpx.AdminOf(r).Admin, i.e. from
// the session row a cookie HASH matched. That is a resolved value, not a claim, and it
// is the one the plaque card should show. The old paragraph said so itself — "the day
// that gate lands, the admin's real id goes here and this paragraph expires" — and
// then the gate landed and the paragraph did not.
//
// 🔴 THE LABEL IS STILL NOT PARSED FOR IT, AND THAT IS THE WHOLE POINT. `actor` is
// caller-supplied and reaches detail.claimed_by; digging a uuid out of it would
// promote an unverified string into this column, which is precisely what the original
// refusal was about. The id travels as its own typed argument or not at all.
//
// uuid.Nil means SYSTEM and keeps the old behaviour for any caller without an admin —
// audit.RecordAuditEvent's own rule for a NULL actor.
func actorIDOf(adminID uuid.UUID) *uuid.UUID {
	if adminID == uuid.Nil {
		return nil
	}
	return &adminID
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
