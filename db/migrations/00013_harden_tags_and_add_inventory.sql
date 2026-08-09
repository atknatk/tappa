-- 00013 -- tags: canonical uid, column-scoped writes, a monotonic read counter,
-- and the INVENTORY state a plaque needs before it is mounted on a wall.
--
-- It adds NO table. 00004 created `tags` and an applied migration is immutable
-- (CLAUDE.md section 6), so everything here is additive or a re-shaped
-- constraint. The FIVE elements of section 6 are 00004's and are NOT touched:
-- tenant_id uuid NOT NULL, tags_tenant_idx (tenant_id, location_id),
-- ENABLE + FORCE ROW LEVEL SECURITY, the NULLIF tenant policy with USING and
-- WITH CHECK, and the tappa_app GRANT. Verified against pg_catalog after this
-- migration, not assumed.
--
-- Three debts close here (backlog T8, T9) plus one screen-shaping index (T11),
-- and one product decision lands (the inventory model, user decision
-- 2026-08-08: Tappa encodes a plaque and LOADS it into the database; the panel
-- only BINDS it to a location).
--
-- ============================================================================
-- PART 1 (T8) -- ONE SPELLING PER PLAQUE, AND WHY IT CANNOT BE VALIDATED TODAY
-- ============================================================================
-- 00004's CHECK is `uid ~ '^[0-9A-Fa-f]{14}$'` -- BOTH cases. That is not a
-- cosmetic looseness. hex.DecodeString is case-INSENSITIVE and the GCM AAD of
-- the wrapped key is the RAW 7 BYTES (internal/sun/keys.go), so
-- '04AC7E55000601' and '04ac7e55000601' are TWO DIFFERENT PRIMARY KEY ROWS WITH
-- THE SAME AAD: one plaque's envelope opens on the other's row, and the two
-- rows carry INDEPENDENT last_ctr values -- i.e. two independent replay windows
-- for one physical chip. The latent failure is M8-05: an operator types the uid
-- in the spelling printed on the plaque, the row is accepted, and the plaque is
-- DEAD -- it never takes a tap and nothing reports an error.
--
-- The fix is to delete the second representation: one canonical spelling,
-- UPPER case, matching what internal/sun.Parse already produces
-- (strings.ToUpper(hex.EncodeToString(...)), params.go).
--
-- 🔴 NOT VALID, AND THE BACKLOG'S PREMISE FOR THIS ITEM IS FALSE. T8 says
-- "the existing 12 rows are already upper case -> no data migration". Measured
-- on the working database (2026-08-09), that is not this database:
--
--   tags total ......................... 75 438
--   already upper case ................. 57 428
--   LOWER case ......................... 18 010
--
-- so a VALIDATED constraint cannot be added -- measured, not assumed:
--   ALTER TABLE tags ADD CONSTRAINT ... CHECK (uid ~ '^[0-9A-F]{14}$');
--   -> ERROR: check constraint is violated by some row
--
-- 🔴 AND THE ROWS CANNOT BE NORMALISED, for a reason that is a RED LINE and not
-- a preference. 12 437 of those 18 010 rows are referenced by 24 874 rows of
-- `transactions` through transactions_tag_fk (tag_uid, tenant_id) -> tags
-- (uid, tenant_id), which is MATCH SIMPLE / NO ACTION: rewriting the parent key
-- requires rewriting the child column, and that table is append-only at the
-- DATABASE level (00005's trigger binds tappa_owner too). Section 4.3 says a
-- recorded tap is legal evidence and is never edited. So "just upper-case them"
-- would mean disabling the append-only trigger as a superuser and rewriting
-- 24 874 evidence rows. It is not done, and it is not a close call.
--
-- WHERE THE 18 010 CAME FROM, because it changes what this constraint costs: all
-- of them are TEST RESIDUE, not product data. They carry a 2-byte '\xDEAD'
-- placeholder in aes_key_ref, which no seed and no product path ever writes.
-- (That population is COUNTED WITH A DATE because it moves: 18 038 two-byte rows on
-- 2026-08-09 at the time this migration was written, 19 580 of 78 803 a few hours
-- later, because every suite run adds fixtures. The 18 010 LOWER-CASE rows are the
-- number that has STOPPED moving -- re-counted after two full suite runs and still
-- 18 010 -- which is the point: the source was closed, so that figure is now a
-- historical fact rather than a live reading.) Their source
-- is two test helpers that emitted hex.EncodeToString without the ToUpper the
-- handler-side helpers already had -- internal/db/store_test.go and
-- internal/sun/advance_test.go. Both are fixed in this change set, so the
-- residue is bounded and stops growing. Backlog T6 already carries "the dev
-- database accumulates test residue" as hygiene debt.
--
-- WHAT NOT VALID DOES AND DOES NOT BUY -- measured on this database, both ways:
--   * INSERT of a lower-case uid  -> REFUSED (this is the whole point).
--   * UPDATE of ANY column on a row whose uid is lower case -> REFUSED. A CHECK
--     is evaluated on every INSERT and UPDATE of the row; NOT VALID only skips
--     the initial table scan. So the residue rows are FROZEN, which is
--     fail-closed and harmless here (nothing reads or writes them: every test
--     creates its own fixture) but must be said out loud rather than discovered.
--   * The 18 010 rows keep existing. This constraint does not pretend otherwise.
--
-- THE ONE-LINER THAT FINISHES IT, for whoever resets the development database
-- (make db-reset) or ships to a fresh one, where zero rows violate it:
--   ALTER TABLE tags VALIDATE CONSTRAINT tags_uid_canonical_hex;
-- On a production database this constraint is born effectively validated: there
-- is no data yet, and from this migration on no lower-case uid can be written.
--
-- 00004's looser tags_uid_check is deliberately LEFT IN PLACE. Both constraints
-- apply, so the strict one is the effective one; dropping the loose one would
-- buy nothing and would make Down a two-step restore of an applied migration's
-- own DDL.

-- +goose Up

ALTER TABLE tags
    ADD CONSTRAINT tags_uid_canonical_hex CHECK (uid ~ '^[0-9A-F]{14}$') NOT VALID;

-- replaced_by holds a uid and therefore takes the same canonical form. This one
-- IS validated: measured, ZERO rows carry a lower-case replaced_by (the self-FK
-- (replaced_by, tenant_id) -> tags (uid, tenant_id) already forces it to match
-- an existing row byte for byte, and char(14) comparison is case-SENSITIVE). The
-- constraint is belt over that brace: it states the rule where a reader of the
-- schema can see it, instead of leaving it as a consequence of another key.
ALTER TABLE tags
    ADD CONSTRAINT tags_replaced_by_canonical_hex
    CHECK (replaced_by IS NULL OR replaced_by ~ '^[0-9A-F]{14}$');

-- ============================================================================
-- PART 2 (T9) -- tappa_app MAY WRITE FIVE COLUMNS, AND MAY NOT REWIND THE
-- COUNTER
-- ============================================================================
-- Measured before this migration (has_column_privilege as tappa_app): tappa_app
-- held UPDATE on ALL NINE columns of `tags`. Three of them are capabilities
-- nobody ever intended to hand it:
--   * aes_key_ref -- overwrite a plaque's wrapped key: the plaque stops
--     verifying and every tap on it answers 500. Denial of service, silent.
--   * uid         -- rename a plaque: the URL printed on the wall resolves to
--     nothing. Denial of service, silent.
--   * last_ctr    -- move the read counter BACKWARDS, which re-opens the replay
--     window section 4.4 exists to close. The heaviest of the three.
-- Not reachable today (the only sqlc query over `tags` was AdvanceTagCounter and
-- this repo has no dynamic SQL), but this migration is the one that opens the
-- panel's write path, so the privilege is narrowed in the same change.
--
-- 🔴 THE COLUMN LIST DEVIATES FROM THE BACKLOG WORDING, DELIBERATELY, BECAUSE
-- TWO USER DECISIONS OF THE SAME DAY WOULD OTHERWISE CONTRADICT. T9 names
-- (last_ctr, status, retired_at, replaced_by). The inventory model (part 3,
-- user decision 2026-08-08) says the panel BINDS a loaded plaque to a location
-- -- and that bind IS an UPDATE of location_id. With T9's list as written, the
-- product decision would be unimplementable. location_id is therefore granted,
-- and the risk it carries is bounded by a key that was already there: the
-- composite FK (location_id, tenant_id) -> locations (id, tenant_id) means a
-- plaque can only ever be moved to a location of its OWN tenant.
--
-- The four columns NOT granted are exactly the four that should never move:
-- uid, tenant_id, aes_key_ref, created_at.
--
-- tenant_id gains a SECOND barrier from this. Before, an attempt to move a tag
-- to another tenant was refused by the RLS WITH CHECK ("new row violates
-- row-level security policy"); now it is refused earlier, by privilege
-- ("permission denied for table tags"). Both measured.
--
-- DIKKAT (the 00009/00011 lesson): db-init's ALTER DEFAULT PRIVILEGES grants the
-- four DML privileges on every new table and 00004 granted table-wide UPDATE on
-- top. NARROWING a grant does nothing -- the table-wide privilege must be
-- REVOKEd first. DELETE stays revoked (00004, section 4.6) and is not touched.
--
-- INSERT is deliberately NOT revoked even though the panel does not insert
-- plaques (Tappa loads them; part 3). SEVEN test files insert as tappa_app
-- (measured: grep -rl 'INSERT INTO tags' over internal/**/*_test.go) and revoking
-- it would trade a real, measured test surface for a privilege nothing currently
-- misuses. Named here rather than left as an oversight -- and it is the honest
-- weak point of this part: the day a loader writes plaques through the
-- application role, this line is what has to be revisited.
REVOKE UPDATE ON tags FROM tappa_app;
GRANT UPDATE (location_id, last_ctr, status, retired_at, replaced_by) ON tags TO tappa_app;

-- --- Monotonic read counter (the structural half) ------------------------------
-- A column grant constrains WHICH column may be written, never WHAT VALUE it may
-- take, and the most dangerous value for last_ctr is a SMALLER one. T9 says this
-- explicitly: "a column-level grant alone is not enough -- last_ctr has to stay
-- on the list and the heaviest capability is precisely rewinding it". So the
-- grant and this trigger are one mechanism in two halves.
--
-- 🔴 THE CONDITION IS "NEW < OLD", NOT "NEW > OLD", AND THE DIFFERENCE IS THE
-- REPLACE-TAG FLOW. T9's own sentence proposes `NEW.last_ctr > OLD.last_ctr`.
-- That predicate is wrong as a REQUIREMENT: retiring a plaque writes status,
-- retired_at and replaced_by and does NOT touch last_ctr, so a strict-increase
-- requirement would REFUSE the central write of the M6-06 "Replace tag" flow.
-- What must be forbidden is going BACKWARDS, not standing still. Both directions
-- are pinned by tests (a retire that leaves last_ctr alone must pass; a rewind
-- must be refused).
--
-- WHAT IT DOES NOT CHANGE -- measured, because a guard that alters section 4.4's
-- behaviour would be a regression however well-intentioned. store.AdvanceTagCounter
-- already demands a STRICT increase in its own WHERE (prev.old_ctr < @ctr), so
-- for that statement the trigger is unreachable: a replayed or smaller counter
-- matches zero rows and no UPDATE is attempted, which is why the caller sees
-- pgx.ErrNoRows rather than an exception. Verified after this migration by
-- running the shipped statement verbatim as tappa_app: it still advances, and
-- SELECT ... FOR UPDATE (the row lock that serialises concurrent taps) still
-- works under a column-only grant -- Postgres accepts a column-level UPDATE
-- privilege for row locking, which was NOT obvious and was probed rather than
-- assumed.
--
-- COUNTER WRAP, WRITTEN DOWN BECAUSE IT IS THE ONE CASE THIS TRIGGER LOOKS LIKE
-- IT WORSENS AND DOES NOT. The NTAG 424 DNA read counter is 24-bit and wraps at
-- 16 777 215 -> 0 (skill tappa-sun; ~2000 years at 20 taps/day). At the wrap the
-- presented ctr is SMALLER than last_ctr, so section 4.4's strict guard already
-- matches zero rows and the plaque is bricked: every subsequent tap is rejected
-- as a replay. That is TODAY's behaviour, before this trigger, and the trigger
-- does not touch it -- the guard's WHERE excludes the row, so no UPDATE is
-- attempted and the trigger never fires. What the trigger does add is that the
-- MANUAL remedy (an operator resetting the counter) is now refused as well: a
-- wrapped plaque must be retired and replaced, which is the same flow as a lost
-- one. That is a deliberate consequence, not an accident.
--
-- SECURITY INVOKER (default): the body reads no data, so DEFINER would be a
-- privilege surface for nothing. search_path pinned (injection defence).
-- The trigger binds tappa_owner too, which no REVOKE can -- the same belt 00005
-- puts over transactions/audit_log and 00011 over admin_sessions. A superuser can
-- still DISABLE it; this is defence in depth, not an absolute.
--
-- The function is NOT table-agnostic, unlike 00011's tappa_forbid_revocation_reset:
-- its message names the counter, and there is exactly one counter in this schema.
-- If a second monotonic integer ever appears, generalise it then, with that
-- table's concurrency story measured -- 00011's BOUNDARY 2 is the reason that
-- sentence exists.
-- +goose StatementBegin
CREATE FUNCTION tappa_forbid_counter_rewind()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
    AS $$
    BEGIN
        RAISE EXCEPTION
            'read counter is monotonic on %: last_ctr may only move forward (% -> % refused)',
            TG_TABLE_NAME, OLD.last_ctr, NEW.last_ctr
            USING ERRCODE = 'restrict_violation';
    END;
    $$;
-- +goose StatementEnd

-- WHEN carries the entire condition, so an ordinary retire, a bind or a status
-- change never even enters the function. Plain `<` (not IS DISTINCT FROM): both
-- columns are NOT NULL, so there is no NULL case to catch, and re-writing the
-- SAME value is allowed on purpose -- an idempotent write is not a rewind, and
-- refusing it would turn a harmless retry into a failed transaction (00011's
-- BOUNDARY 2: a monotonicity guard that fires on a concurrent duplicate makes
-- the caller report failure for work that actually succeeded).
CREATE TRIGGER tags_counter_monotonic
    BEFORE UPDATE ON tags
    FOR EACH ROW
    WHEN (NEW.last_ctr < OLD.last_ctr)
    EXECUTE FUNCTION tappa_forbid_counter_rewind();

-- ============================================================================
-- PART 3 -- THE INVENTORY MODEL: A PLAQUE MAY EXIST BEFORE IT IS ON A WALL
-- ============================================================================
-- USER DECISION (2026-08-08): the AES key never passes through the panel. Tappa
-- encodes a plaque and LOADS the row -- uid, tenant, wrapped key -- and the
-- panel only BINDS it: retire the old one, assign the new one to a location.
-- Q06 already says the key is produced by the M8-05 runbook and Q10 (supplier +
-- key delivery) is open.
--
-- The schema could not represent that. 00004 makes location_id NOT NULL and its
-- comment argues "a plaque is ALWAYS mounted at an entrance; a tag without a
-- location is meaningless" -- true of a MOUNTED plaque, and exactly what a
-- SHIPPED, NOT YET MOUNTED plaque is not. The card's "encoded/pending" criterion
-- is unrepresentable for the same reason (state.md, M6-06 phase B item 3).
--
-- WHAT IS AND IS NOT NULLABLE, and why tenant_id is NOT the nullable one:
-- section 4.5 requires tenant_id on every table and RLS keyed on it; making it
-- nullable would put rows outside every policy. It also is not necessary: a
-- plaque is SHIPPED TO A CUSTOMER, so the tenant is known at load time. The
-- unknown is the LOCATION -- which wall, decided by the manager later.
--
-- So: pending = location_id IS NULL. encoded = the row exists (aes_key_ref is
-- NOT NULL and Tappa wrapped it before loading), which is what makes the card's
-- criterion representable at all.
--
-- THE FK NEEDS NO CHANGE -- measured rather than reasoned: tags_location_fk is
-- MATCH SIMPLE, so with location_id NULL the constraint is not checked and the
-- insert succeeds; tenant_id stays NOT NULL, so a MOUNTED plaque is still
-- structurally forced into a location of its own tenant.
--
-- THE INDEX NEEDS NO CHANGE: tags_tenant_idx is (tenant_id, location_id) and
-- btree indexes NULLs, so "this tenant's unbound stock" is an index range like
-- any other.
--
-- A FOURTH STATUS, AND THE TWO CHECKS THAT KEEP IT HONEST. 'unassigned' joins
-- active/retired/lost (the vocabulary of skill tappa-sun's lifecycle section; it
-- reads as an adjective like the other three and names exactly what is missing).
-- Two representations of one fact is how they drift apart, so they are bound:
--   * an ACTIVE plaque must have a location (it is in service, on a wall);
--   * an UNASSIGNED plaque must not (it is stock).
-- retired and lost are deliberately unconstrained: a mounted plaque keeps its
-- location after retirement (the audit trail needs it) and a stock plaque can be
-- lost before it is ever mounted.
-- The pair also makes un-mounting ATOMIC: clearing location_id on an active row
-- fails, so a plaque can only go back to stock in one statement that sets both.
--
-- 🔴 THE FOURTH STATUS ESCAPED SECTION 5 ROW 1, AND THAT IS NOW CLOSED IN THE
-- GUARDRAIL (2026-08-09, same task). sys:tag-not-active
-- (internal/policy/guardrails.go) was a DENY-LIST -- it fired on 'retired' OR
-- 'lost' and never asked for 'active' -- so a tap on an 'unassigned' plaque
-- matched NO guardrail and fell through to the evidence rules. It now asks for
-- `active`, so every status this schema adds is refused on the day it is added.
--
-- WHAT THE ESCAPE ACTUALLY COST, measured end to end rather than assumed, because
-- an earlier version of this paragraph said "can end as `ok`" and that was WRONG in
-- the direction that flatters the schema:
--
--   `ok` was NEVER reachable. An unmounted plaque has location_id NULL ->
--   uuid.Nil -> GetLocationForTap returns no row -> the tap has NO IP range and NO
--   coordinate to be judged against, so base:no-evidence-review catches it. The
--   worst outcome was `flag`, and section 4.6 held throughout: the row was written,
--   nothing was silently dropped.
--
--   NFC was closed already, but NOT by the rule anyone would name.
--   internal/sun/preview.go short-circuits on `tag.Status != active` (an
--   allow-list, already) and reports cmacVerified=false, so the tap died at
--   sys:sun-invalid -- guardrail #3, whose REASON TEXT says the signature could not
--   be verified. That message is wrong for this case (nothing is wrong with the
--   signature; the plaque is in a box) and fixing the wording belongs to the
--   application round, not here.
--
--   QR WAS THE OPEN PATH, and it is the concrete one. sys:sun-invalid is gated on
--   channel = 'nfc', so a QR-shaped tap skipped it entirely: an employee who read
--   the uid off a plaque still in its box could produce, from anywhere, `flag`
--   rows that a manager is invited to approve.
--
-- STILL NOT REACHABLE FROM THE APPLICATION, and the reason is unchanged: db/queries
-- has NO INSERT over `tags` -- the panel does not create plaques, Tappa loads them
-- -- so only tappa_owner can produce an unassigned row. That protection does NOT
-- expire with the next round, because phase B deliberately ships no insert path;
-- it expires the day a loader or runbook writes one. The guardrail is inverted
-- anyway, because "no production path exists yet" has quietly stopped being true
-- twice in this repository.

ALTER TABLE tags ALTER COLUMN location_id DROP NOT NULL;

COMMENT ON COLUMN tags.location_id IS
    'The entrance this plaque is mounted at. NULL = loaded but not yet bound to '
    'a wall (inventory/stock; user decision 2026-08-08). An active plaque always '
    'has one -- see tags_active_requires_location.';

ALTER TABLE tags DROP CONSTRAINT tags_status_check;
ALTER TABLE tags ADD CONSTRAINT tags_status_check
    CHECK (status IN ('active', 'retired', 'lost', 'unassigned'));

COMMENT ON COLUMN tags.status IS
    'Lifecycle (skill tappa-sun): unassigned = loaded, not yet mounted; '
    'active = in service; retired = replaced, taps reject; lost = reported '
    'lost, taps reject plus a security alert.';

ALTER TABLE tags ADD CONSTRAINT tags_active_requires_location
    CHECK (status <> 'active' OR location_id IS NOT NULL);

ALTER TABLE tags ADD CONSTRAINT tags_unassigned_has_no_location
    CHECK (status <> 'unassigned' OR location_id IS NULL);

-- 🔴 THE AUDIT CLAIM, MADE STRUCTURAL. The paragraph above says "a mounted plaque
-- keeps its location after retirement (the audit trail needs it)" -- and until this
-- constraint that sentence was held up by NOTHING but the shipped statement's good
-- manners. Measured as tappa_app inside its own tenant:
--
--   UPDATE tags SET status='retired', retired_at=now(), location_id=NULL ...
--     -> UPDATE 1, and the row's location is gone for good.
--
-- Which venue a retired plaque served at is exactly the question a replacement
-- history exists to answer, and `transactions` rows carry their own location, so
-- losing it here loses the CONFIGURATION side of the trail with no way back.
--
-- WHY IT IS SAFE TO ADD, measured rather than assumed: ZERO existing rows violate it
-- (no retired plaque currently lacks a location), so it validates immediately; and
-- NO shipped flow is blocked, because RetireTagForReplacement demands
-- `status = 'active'` and tags_active_requires_location already demands that an
-- active plaque have a location. A plaque can therefore only reach 'retired' from a
-- state that HAS a wall.
--
-- WHAT IT DELIBERATELY DOES NOT COVER: 'lost'. A plaque can be lost while still in
-- stock -- measured on 2026-08-09: 8 such rows when this constraint was written,
-- 26 a few hours later, because the suite creates them; the number is a DIRECTION,
-- not a level, and what matters is that it is never zero -- so requiring a location
-- there would forbid a real state. Losing and retiring are different facts and only one of them
-- implies the plaque was ever mounted.
ALTER TABLE tags ADD CONSTRAINT tags_retired_keeps_its_location
    CHECK (status <> 'retired' OR location_id IS NOT NULL);

-- ============================================================================
-- PART 3b -- THE NAME BOUND PHASE A HANDED TO "the next time this schema is
-- touched". THIS IS THAT TIME.
-- ============================================================================
-- locations.name and departments.name are unbounded `text` (00002), with no UNIQUE
-- and no CHECK. M6-06 phase A bounded them at the BOUNDARY instead
-- (tenant.MaxVenueNameRunes = 80, plus a trim-and-reject-empty rule in venueName)
-- and its own comment names the debt: "a CHECK constraint would be the right home
-- for it the next time this schema is touched." The card repeats it. Both point
-- here, and nothing else was going to touch this schema.
--
-- WHY IT MATTERS BEYOND TIDINESS, in phase A's words: these two names are rendered
-- into the location and department dropdowns of three other sections' filter bars,
-- so one absurd name degrades screens its own section does not own. The precedent
-- is measured -- a 605-rune employees.full_name once served page one forever and
-- made everybody behind it unreachable by browsing.
--
-- MEASURED BEFORE ADDING, on 2026-08-09, so this validates instead of failing:
--   locations   140 331 rows, 0 over 80 runes (longest exactly 80), 0 blank
--   departments  25 372 rows, 0 over 80 runes (longest 23),         0 blank
-- The longest location name being EXACTLY 80 is our own test data -- phase A's
-- positive control stores a name at the limit -- which is also why that number is
-- not evidence of anything about real names.
--
-- char_length COUNTS RUNES, NOT BYTES, and that was verified rather than assumed:
-- char_length('ċġħż') = 4 while octet_length = 8. Maltese venue names carry ċ ġ ħ ż,
-- so a byte bound would refuse ordinary names at four fifths of the stated length --
-- the same reasoning venueName gives for using utf8.RuneCountInString.
--
-- THE CONSTRAINT MIRRORS venueName EXACTLY, both halves: a name is trimmed-non-empty
-- AND at most 80 runes. Writing only the length half would leave '   ' storable,
-- which the boundary already refuses -- and a schema rule that is WEAKER than the
-- code rule teaches the next reader the wrong bound.
--
-- 80 IS DUPLICATED HERE, and that is a real cost, named rather than hidden: the
-- number now lives in tenant.MaxVenueNameRunes and in these two constraints. It is
-- accepted because the alternative -- no schema bound at all -- is what this debt
-- was about; if the two ever disagree the boundary is the stricter of them by
-- construction (it trims first), so the failure mode is a rejected write, not a
-- stored row the code thinks is impossible.
ALTER TABLE locations ADD CONSTRAINT locations_name_bounded
    CHECK (btrim(name) <> '' AND char_length(name) <= 80);

ALTER TABLE departments ADD CONSTRAINT departments_name_bounded
    CHECK (btrim(name) <> '' AND char_length(name) <= 80);

-- ============================================================================
-- PART 4 (T11) -- THE INDEX THAT DECIDED A SCREEN LAYOUT
-- ============================================================================
-- M6-06 phase A moved the "can this venue be deleted" reference count off the
-- venue LIST and onto the venue's EDIT CARD, because the four counts cost ~30 ms
-- per venue and the whole time was the `transactions` predicate falling back to
-- a bitmap heap scan for want of this index. The card's own note says the index
-- "would drop the 288 ms and fits naturally into phase B's 00013".
--
-- MEASURED HERE, 2026-08-09, and written with its population as this repo's rule
-- now requires -- tenant 10000000-...-0001, 9 locations, 29 087 transactions of
-- 238 922 in the table (228 MB total relation), the counted location holding 0:
--
--   the four counts, WITHOUT this index ....... 74.8 ms cold / 30.3 / 29.8 ms warm
--     of which the transactions leg alone ..... 68.8 / 20.5 / 18.3 ms
--     (Bitmap Heap Scan, 5 149 buffers)
--   the four counts, WITH this index .................... 8.9 / 6.9 ms
--     of which the transactions leg alone ....... 0.069 / 0.045 ms
--     (Index Only Scan)
--
-- Cost: 4 472 kB (4 579 328 bytes) against a 228 MB relation, 269 ms to build.
-- Write cost, three paired samples of 20 000 INSERTs each (without / with):
--   5 237 / 5 280 ms . 5 130 / 5 134 ms . 5 206 / 5 561 ms
-- i.e. +0.8%, +0.1%, +6.8% -- the spread is run-to-run noise on this machine and
-- the honest claim is "not measurably slower at this volume", not a single
-- number. The read side is unambiguous: the transactions leg goes from ~20 ms to
-- ~0.05 ms.
--
-- ⚠️ WHAT IT DOES NOT FIX, so nobody reads the 30 -> 7 ms as "solved": the
-- remaining ~7 ms is now the EMPLOYEES leg (1 560 buffers), which has no
-- (tenant_id, location_id) index either. That is a separate item and is
-- deliberately not bundled -- T11 names transactions and only transactions.
--
-- ⚠️ THIS IS THE ONE PART OF 00013 THAT IS A PERFORMANCE PROPERTY, NOT A
-- CORRECTNESS ONE: no test in the suite fails without it, so a mutation that
-- removes it is invisible to CI (the same limit 00011 states for
-- admin_users_email_idx). It is also the one part that can be removed from this
-- file cleanly -- it is a self-contained CREATE/DROP pair -- if the orchestrator
-- decides the screen shape of phase A should stay as it is.
CREATE INDEX transactions_tenant_location_idx ON transactions (tenant_id, location_id);

-- --- "Last seen": AN INDEX THAT WAS MEASURED AND DELIBERATELY *NOT* ADDED --------
-- The card asks the plaque list for a fifth column, LAST SEEN, and phase B ships it
-- (db/queries/tags.sql -> ListTagLastSeen). `transactions` has no (tenant_id,
-- tag_uid) index, so a candidate for this slot was
-- (tenant_id, tag_uid, occurred_at DESC). It is not here, and the measurement is
-- recorded because a future reader will have the same idea:
--
--   THE QUERY SHAPE MATTERED MORE THAN THE INDEX. Measured 2026-08-09, tenant
--   10000000-...-0001, 11 plaques, 24 585 tapped rows of 242 642:
--
--     per-plaque correlated subquery, no index .. 203.3 / 195.1 / 196.4 ms (Seq Scan)
--     ONE GROUP BY pass, no index ................ 30.9 / 26.6 / 23.2 ms  (Bitmap Heap)
--     ONE GROUP BY pass, WITH the index ............ 7.7 / 7.6 ms        (Index Only)
--
--   Rewriting the query bought 195 -> 26 ms for free. The index then buys 26 -> 7.7
--   ms for 12 MB (12 304 384 bytes) on a 228 MB relation and 334 ms of build.
--
-- 🔴 THE DECISION RULE WAS SET BEFORE THE NUMBERS: add it if its profile is close to
-- T11's above (4 472 kB, no measurable write cost, and a gain of two orders of
-- magnitude). It is not close -- 2.7x the size for 3-4x the speed, against T11's
-- ~400x -- so it is NOT added. Both are linear in the tenant's history either way;
-- the index is a constant factor, not a change of shape, which is what makes 12 MB
-- the wrong trade TODAY.
--
-- WHEN TO REVISIT, so the next person does not have to re-measure: if the plaque
-- list becomes slow for a tenant with a long history, this is a one-line migration
-- and the numbers above are the before/after. Nothing in the shipped code depends
-- on it existing.

-- +goose Down

-- Reverse order: index, trigger, its function, then the constraints, then the
-- column, then the grants.
--
-- 🔴 READ THIS FIRST: A SUCCESSFUL Down IS A SECURITY REGRESSION, NOT A NEUTRAL
-- REWIND. Most of this block explains when Down REFUSES to run; that is the easy
-- half. What it does when it DOES run, named plainly because `make migrate-down` is
-- a documented target somebody will type:
--
--   * tappa_app gets TABLE-WIDE UPDATE on `tags` back -- all nine columns. The
--     HTTP-facing role can once again overwrite aes_key_ref (a plaque that answers
--     500 forever, silently) and rename uid (the URL printed on the wall resolves
--     to nothing).
--   * tags_counter_monotonic is DROPPED, so last_ctr can be moved BACKWARDS again.
--     That re-opens the section 4.4 replay window: rewind the counter and every
--     previously-used (uid, ctr) URL is accepted a second time.
--   * the canonical-uid, retired-keeps-its-location and venue-name constraints go
--     with it, and rows that violate them become insertable again.
--
-- None of that is a reason to leave Down empty -- a migration that cannot be rolled
-- back is worse -- but "rolled back cleanly" and "safe" are different words, and
-- this file's own standard is to name consequences rather than leave them to be
-- rediscovered. After a Down, the protection over `tags` is exactly what it was
-- before this migration: discipline.
--
-- 🔴 THIS Down IS HONEST ABOUT WHEN IT CANNOT RUN, which is the only useful kind.
-- Restoring 00004's three-value status CHECK and location_id's NOT NULL will
-- FAIL -- correctly -- if any plaque is currently 'unassigned' or unbound: you
-- cannot roll back a schema whose new states are in use, and silently rewriting
-- those rows to make the rollback succeed would be fabricating inventory.
--
-- ⚠️ THE RECIPE HAS BEEN WRONG TWICE, IN OPPOSITE WAYS, SO IT IS NOW WRITTEN AS
-- MEASURED OUTPUT. Every line below was reproduced on a fresh v13 clone on
-- 2026-08-09; nothing here is inferred.
--
-- THERE ARE TWO DIFFERENT OBSTACLES and they fail at different statements, which is
-- why one instruction could never cover both:
--
--   a plaque with status='unassigned'
--     -> Down fails restoring 00004's three-value CHECK:
--        ERROR: check constraint "tags_status_check" of relation "tags"
--               is violated by some row                        (SQLSTATE 23514)
--   a plaque with NO location whose status is already legal in v12 -- in practice
--   a plaque reported LOST while still in stock
--     -> Down gets past the CHECK and fails at the column:
--        ERROR: column "location_id" of relation "tags"
--               contains null values                           (SQLSTATE 23502)
--
-- 🔴 AND "RETIRE IT" IS NOT A REMEDY AT ALL ANY MORE -- this migration's own
-- tags_retired_keeps_its_location forbids it, so the operator never even reaches
-- Down:
--
--   UPDATE tags SET status='retired' ... on a plaque with no location
--     -> ERROR: new row for relation "tags" violates check constraint
--               "tags_retired_keeps_its_location"              (SQLSTATE 23514)
--
-- An earlier version of this note prescribed exactly that UPDATE and printed the
-- 23502 it would have produced under an earlier version of THIS FILE. The
-- constraint that invalidated it was added in the same task, a few hundred lines
-- above, and this paragraph was not re-measured. The lesson, stated for the next
-- person editing this file: ADDING A CONSTRAINT INVALIDATES EVERY SENTENCE IN THE
-- FILE THAT COUNTS CONSTRAINTS OR DESCRIBES WHAT THEY ALLOW. Re-measure them all.
--
-- WHAT ACTUALLY WORKS, both driven end to end to v12:
--   * an UNASSIGNED plaque -> BIND it:
--       UPDATE tags SET status='active', location_id=<a venue of its tenant> ...
--   * a LOST plaque in stock -> just GIVE IT A LOCATION; the status may stay 'lost'
--     (v12 accepts 'lost', and tags_retired_keeps_its_location constrains only
--     'retired'):
--       UPDATE tags SET location_id=<a venue of its tenant> ...
--   * or DELETE the rows, if they are fixtures rather than inventory.
-- After either, `goose down` exits 0 and the resulting v12 schema compares field by
-- field against a freshly built v12.
--
-- A FAILED Down IS ATOMIC, which is what makes the refusal safe rather than
-- destructive: goose wraps the migration in a transaction, so the DROPs below do
-- not survive the error. Re-measured after a refused Down on a fresh v13 clone:
-- `tags` still carried EIGHT CHECK constraints (count it from pg_constraint rather
-- than trusting this number -- it was written as 7 and went stale within the same
-- task when tags_retired_keeps_its_location was added), the monotonic trigger, the
-- one index this migration creates, and goose still reported version 13.
DROP INDEX IF EXISTS transactions_tenant_location_idx;

ALTER TABLE departments DROP CONSTRAINT IF EXISTS departments_name_bounded;
ALTER TABLE locations DROP CONSTRAINT IF EXISTS locations_name_bounded;

DROP TRIGGER IF EXISTS tags_counter_monotonic ON tags;
DROP FUNCTION IF EXISTS tappa_forbid_counter_rewind();

ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_retired_keeps_its_location;
ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_unassigned_has_no_location;
ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_active_requires_location;

ALTER TABLE tags DROP CONSTRAINT tags_status_check;
ALTER TABLE tags ADD CONSTRAINT tags_status_check
    CHECK (status IN ('active', 'retired', 'lost'));

COMMENT ON COLUMN tags.status IS NULL;
COMMENT ON COLUMN tags.location_id IS NULL;

ALTER TABLE tags ALTER COLUMN location_id SET NOT NULL;

ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_replaced_by_canonical_hex;
ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_uid_canonical_hex;

-- Restore 00004's grant exactly: table-wide UPDATE back, DELETE still revoked.
-- Revoking the table privilege also drops the column-level grants on it.
REVOKE UPDATE ON tags FROM tappa_app;
GRANT SELECT, INSERT, UPDATE ON tags TO tappa_app;
REVOKE DELETE ON tags FROM tappa_app;
