-- +goose Up

-- Migration 0019 -- M7-04 phase A: password_resets stops being a table nobody uses
-- and becomes one the application cannot abuse.
--
-- DECISION RECORD: docs/adr/0015-sifirlama-tokeni-tek-gecislik-yetkidir.md. It carries
-- the seven-capability table below, the two things this migration does NOT close, the
-- rejected alternatives and the accepted risks. This file is the SQL; that file is why.
--
-- IT CREATES NO TABLE, so CLAUDE.md section 6's five-fold rule has nothing to apply
-- to -- and that is the FIRST thing the M7-04 card gets wrong. The card is written
-- as if the reset flow needs new storage; migration 00006 created password_resets
-- (id, tenant_id, admin_user_id, token_hash, created_at, expires_at, used_at) with
-- the RLS five and the DELETE revoke already in place, two milestones before
-- anything could use it. What was missing was never the table. It was:
--
--   * ANY query at all -- `password_resets` appears in NO file under db/queries
--     (grepped), so the generated store has no way to touch it;
--   * a way to REACH a row from a reset LINK, which carries only a token (the
--     tenant is the RESULT of that lookup, not an input -- ADR 0002 madde 7);
--   * privileges narrow enough that an EXISTING reset token cannot be turned into a
--     credential for somebody it was not issued to.
--
-- 🔴 THAT THIRD LINE SAID SOMETHING STRONGER AND AN AUDIT MEASURED IT FALSE. It read
-- "privileges narrow enough that possessing the write path is not the same as
-- possessing every account in the tenant". It is not, and cannot be: the INSERT grant
-- below necessarily carries `admin_user_id` AND `token_hash`, because ISSUING a reset
-- for an administrator under a hash the application computed is the entire job of this
-- table. Measured AFTER this migration, as tappa_app, in its own tenant context,
-- inside BEGIN..ROLLBACK:
--
--   N1  INSERT ... (tenant_id, admin_user_id = <THE OWNER>, token_hash = <mine>)  -> INSERT 0 1
--   N2  <the exact shape of ConsumePasswordResetAndSetPassword>                   -> UPDATE 1
--       RETURNING  <owner id> | owner-probe...@probe.example | owner
--
-- SO THE CORRECT VERB IS re-pointing, NOT minting. What 00019 closes is taking a token
-- that was issued to A and making it work for B (A1), plus resurrecting, replaying and
-- grafting (A2/A3/A4). MINTING a fresh token for any administrator of one's own tenant
-- is inherent to the flow and is closed nowhere in SQL -- what stands between it and an
-- account takeover is that the raw token is delivered to the ADDRESS ON THE
-- ADMINISTRATOR'S OWN ROW and never returned to the caller, which is phase B's
-- obligation and not this file's.
--
-- ⚠️ THE COMPARISON IS WORTH KEEPING because it points the other way from what one
-- would guess: `employee_invites` still holds `tappa_app=ar`, i.e. TABLE-WIDE INSERT,
-- so password_resets is the NARROWER of the two after this migration.
--
-- ============================================================================
-- WHAT WAS ACTUALLY OPEN -- SEVEN CAPABILITIES, MEASURED BEFORE THIS MIGRATION
-- ============================================================================
-- 00006 grants tappa_app TABLE-WIDE UPDATE and TABLE-WIDE INSERT on this table.
-- Measured on this database as tappa_app, inside its own tenant context, inside
-- BEGIN..ROLLBACK, against a fixture holding one live reset for a MANAGER plus an
-- OWNER row belonging to the same business. Taken 2026-08-14, load average 7.71:
--
-- (⚠️ THE PHRASING OF THE LINE ABOVE IS NOT FREE. It read "... in the same tenant
-- (2026-08-14, ...)" first, and internal/handler's roster-size tripwire
-- TestComments_DoNotQuoteTheDriftingRosterSize FAILED the tree on it: its
-- `(tenant|roster|kadro|payroll)\W{0,3}\(\s*[1-9]\d{3}` arm cannot tell a YEAR from
-- a headcount. It is reworded rather than exempted, which is the direction this
-- repository has taken every time -- a tripwire that fires on correct prose is the
-- one the next person deletes.)
--
--   A1  UPDATE ... SET admin_user_id = <the owner>       -> UPDATE 1
--   A2  UPDATE ... SET expires_at = now() + '10 years'   -> UPDATE 1
--   A3  UPDATE ... SET used_at = now(); SET used_at = NULL -> UPDATE 1, UPDATE 1
--   A4  UPDATE ... SET token_hash = <a value I chose>    -> UPDATE 1
--   A5  INSERT ... expires_at = 'infinity'               -> INSERT 0 1
--   A6  INSERT ... created_at = now() + '10 years'       -> INSERT 0 1
--   A7  INSERT ... token_hash = 'plaintext-reset-token'  -> INSERT 0 1
--
-- 🔴 A1 IS AN ACCOUNT TAKEOVER PRIMITIVE AND IT IS THE REASON THIS MIGRATION
-- EXISTS. A reset token is a one-shot bearer credential for ONE identity. With
-- table-wide UPDATE, one statement re-points a live token at a DIFFERENT admin of
-- the same tenant, so a manager who requested a reset for THEMSELVES holds a link
-- that sets the OWNER'S password. RLS does not help: it is the same tenant, which
-- is exactly the case RLS is not about. A2 makes a link immortal, A3 replays a
-- spent one, A4 lets a caller who knows a hash graft it onto somebody else's row.
--
-- 🔴 THIS IS THE SAME CLASS 00009 AND 00012 CLOSED FOR employee_invites, and the
-- fact that it survived here for thirteen migrations is the finding. 00009 wrote
-- the argument out in full for its own table ("a table-wide UPDATE would let the
-- application RESURRECT an expired invite, REASSIGN a live invite to a different
-- employee, UN-CONSUME a used one") and narrowed to `GRANT UPDATE (used_at)`.
-- password_resets is the SAME shape with a HIGHER prize -- an invite activates an
-- employee, a reset sets a panel administrator's password -- and it kept the wide
-- grant because no code path used it yet. "Nothing calls it" is not a control; it
-- is a countdown, and M7-04 is what runs it out.
--
-- ============================================================================
-- WHAT IS **NOT** HERE, AND WHY EACH ABSENCE IS A DECISION
-- ============================================================================
-- NO MAGIC-LINK TABLE, NO SESSION-GRANTING TOKEN. The card says "magic link
-- and/or password"; the RIGHT branch of that disjunction shipped in M6-01
-- (adminauth.Authenticate) and the left one is a SECOND authentication path, i.e.
-- a second thing that can be wrong. The decisive difference is at the data layer
-- and it is why it belongs in this file rather than in a handler: a row in THIS
-- table permits exactly ONE state transition -- write admin_users.password_hash,
-- once, and die -- and it grants NO session. A stolen reset link therefore costs
-- the victim a password they can see was changed; a stolen magic link would be a
-- silent sign-in with no trace the owner can notice, which is the shape section
-- 4.6 exists to refuse. If magic-link login is ever wanted it needs its OWN table
-- and its own ADR: putting it on password_resets would upgrade every reset token
-- already issued into a session token.
--
-- NO DELIVERY-STATUS COLUMN (`delivered_at`, `delivery_failed_at`, `provider_id`),
-- and this is the constraint Q02 imposes rather than an oversight. Q02 (which mail
-- provider) is an unmade PURCHASING decision, and nothing in this phase may depend
-- on it. A status column would: a synchronous SMTP send reports failure by
-- RETURNING AN ERROR to the request that is still open, while a queued provider
-- reports it minutes later through a webhook -- two different columns, two
-- different lifecycles, and choosing one here would pick the provider by
-- implication. The repository already answers "how does a failed delivery get
-- reported" without a column: internal/invite.Channel returns an error to the
-- caller and writes an audit_log row for the disclosure (M5-02), and
-- employee_invites carries no delivery column either. M7-04 phase B follows that
-- precedent. What phase B may NOT do is swallow the error -- section 7 forbids it
-- and the card's own criterion is "dürüstçe bildiriliyor".
--
-- NO ATTEMPT COUNTER / LOCKOUT STATE, for the reason 00009 states for invites and
-- with the same expiry condition attached: it is sound ONLY while the token is
-- >= 128 bits of cryptographic randomness. internal/adminauth's ResetToken draws
-- 32 bytes from crypto/rand and says so in a comment tied to this sentence. A
-- short human-typed reset code would need lockout state, and that needs a NEW
-- migration. NOTHING HERE DETECTS THAT SWITCH: the shape CHECK below tests the
-- HASH, and the hash of a six-digit code is 64 hex characters exactly like the
-- hash of a 256-bit one.
--
-- NO NEW INDEX. Every query in db/queries/passwordresets.sql enters either by
-- token_hash (the GLOBAL UNIQUE index 00006 created) or by (tenant_id,
-- admin_user_id) (password_resets_tenant_idx, 00006). cancelled_at is a predicate
-- that eliminates rows inside an already-narrow set -- a single admin's resets --
-- so a separate index would buy no scan and cost every INSERT. Same decision, same
-- reason, as 00012 for employee_invites.cancelled_at.
--
-- KIRMIZI CIZGILER touched:
--   section 4.5  No new table, so the five-fold rule does not apply; what DOES
--                apply is that the new resolver below is a CONTAINED bypass and
--                not a general one. Every ADR 0002 madde 7 constraint is restated
--                and granted explicitly here rather than inherited.
--   section 4.6  Nothing is deleted. A consumed reset keeps its used_at, a
--                superseded one gets cancelled_at, and the two are NEVER read as
--                each other (00012's rule, restated at the column).
--   section 4.7  The reset TOKEN is never stored -- only its hash, and the CHECK
--                below refuses anything that is not hash-SHAPED. THE LIMIT IS THE
--                ONE 00009 NAMES: a CHECK can enforce a value's SHAPE, never its
--                PROVENANCE. A caller that used a 64-lowercase-hex string as the
--                TOKEN would satisfy it. Deriving the stored value as an HMAC
--                under a server-side key is internal/adminauth's obligation.


-- --- 1. cancelled_at: a reset can be RETIRED without being spent ---------------
--
-- 🔴 IT EXISTS BECAUSE ISSUING A SECOND RESET MUST KILL THE FIRST, and this repo
-- has already paid once for the opposite. Migration 00012 was written after M6-05
-- measured, end to end over HTTP, that two presses of "Send invite" produced two
-- simultaneously valid links and that the OLDER one still worked after the newer
-- one had been spent -- "link gelmedi, tekrar bas" leaving a live account-takeover
-- credential in a chat history. A password reset is the same button with a bigger
-- prize, and the correct time to learn this lesson is before the first incident
-- rather than after it. CreatePasswordReset retires the live siblings in the same
-- statement that issues the new token.
--
-- 🔴 used_at AND cancelled_at ARE TWO FACTS AND ARE NEVER SWAPPED (00012's rule).
-- used_at = "this token was spent"; cancelled_at = "this token may no longer be
-- spent". Overloading used_at would corrupt the audit answer to "when was this
-- reset link used, and by whom" -- the question an account-takeover investigation
-- starts from. Every query eliminates both separately: `used_at IS NULL AND
-- cancelled_at IS NULL`.
--
-- Nullable, no default, no back-fill: existing rows are born NULL, so this
-- migration retires nothing. Claiming a row was cancelled in the past would be an
-- invention.
ALTER TABLE password_resets ADD COLUMN cancelled_at timestamptz;

COMMENT ON COLUMN password_resets.cancelled_at IS
    'Sifirlama token''i gecersizlestirildi (kullanilmadi). used_at ile ASLA '
    'karistirilmaz: used_at = "token harcandi", cancelled_at = "token artik '
    'kullanilamaz". Sorgular ikisini de ayri ayri eler.';


-- --- 2. the TTL ceiling: "süreli" stops being a promise ------------------------
--
-- The card's criterion is "sifirlama token'i tek kullanimlik, SURELI, hash'i
-- saklaniyor". 00006 made the expiry NOT NULL, which blocks the OMITTED expiry and
-- nothing else. Measured above: `expires_at = 'infinity'` was accepted (A5), and
-- `now() < 'infinity'` is true forever -- a genuinely immortal reset link. A6 is
-- the same hole through the other column: with created_at writable, a row stamped
-- ten years in the future carries a 24-hour TTL that starts in 2036 and is live
-- the whole way there.
--
-- ONE CHECK CLOSES BOTH, because both are the same statement about a SPAN rather
-- than about an instant. 'infinity' fails it; '-infinity' passes and that is
-- deliberate (already expired = fail-closed = harmless, 00009's reasoning).
--
-- WHY 24 HOURS AND WHY IT IS A CEILING RATHER THAN THE TTL. The product TTL is
-- internal/adminauth.ResetTTL (one hour) and this constraint does not set it: it
-- refuses the ABSURD, so that a future edit cannot quietly turn a reset link into
-- a permanent credential sitting in an email archive. 24 h is the longest span for
-- which "the owner read the mail the next morning" is still a real story. Changing
-- it costs a migration -- correct, because a longer-lived account-takeover
-- credential is a decision somebody should have to write down.
--
-- ⚠️ IT IS ADDED **VALIDATED**, and that is measured rather than hoped: all 6 502
-- existing rows carry exactly `expires_at - created_at = 01:00:00`
-- (min = max = 1 hour, 0 rows over 24 h -- counted 2026-08-14). Contrast 00018,
-- which had to use NOT VALID because 20 232 rows violated it.
--
-- ⚠️ THE CLOCK-SKEW SLACK, stated because the value comes from Go and the column
-- default comes from Postgres. phase B passes expires_at = <Go now> + ResetTTL
-- while created_at defaults to the transaction's now(), so the two clocks must
-- agree to within 24 h - 1 h = 23 hours for a legitimate issue to pass. A Go clock
-- running AHEAD by more than that is refused (fail-closed, no token issued); one
-- running behind produces a token that is already dead (fail-closed again).
ALTER TABLE password_resets
    ADD CONSTRAINT password_resets_ttl_ceiling
    CHECK (expires_at <= created_at + interval '24 hours');


-- --- 3. token_hash is hash-SHAPED ---------------------------------------------
--
-- A bare `text` column accepts a raw token as happily as a hash, so 00006's
-- sentence "the reset token is never stored, only its hash" was a comment guarding
-- nothing -- A7 above wrote the literal string 'plaintext-reset-token' into it and
-- got INSERT 0 1. `^[0-9a-f]{64}$` is exactly what internal/adminauth produces
-- (hex of an HMAC-SHA256), and it is the SAME constraint 00009 added to
-- employee_invites.code_hash after making the same argument. 00009 called that a
-- "deliberate deviation from precedent" because sessions.token_hash and
-- password_resets.token_hash carried no such CHECK; this statement removes half of
-- the deviation by bringing the precedent up to the rule. (sessions.token_hash and
-- admin_sessions.token_hash are still unconstrained. They are not in this task's
-- scope and are named so the gap is visible rather than implied.)
--
-- ⚠️ NOT VALID -- MEASURED, NOT ASSUMED, and the population is TEST RESIDUE. Counted
-- on this development database 2026-08-14: 6 502 rows, 0 matching the pattern,
-- 6 502 violating. They are not product data -- test/fixtures/seed.sql writes NO
-- password_resets row at all (its own header says so) and every existing row was
-- written by internal/db/rls_test.go's fixture, which needed a unique NOT NULL
-- string and had no reason to care what was in it ('reset-hash-<uuid>' 6 304 rows,
-- 'own-<uuid>'/'forge-<uuid>' 198). Those fixtures are corrected in this change set
-- (they now use randTokenHash), so the population is bounded and stops growing.
--
-- ⚠️ THE RESIDUE ROWS ARE NOW FROZEN, the same trade 00013 and 00018 record: a CHECK
-- runs on every INSERT and UPDATE, NOT VALID skips only the initial scan, so an
-- UPDATE of ANY column of a row whose token_hash is 'reset-hash-...' is refused from
-- here on.
--
-- 🔴 "HARMLESS -- NOTHING READS THOSE ROWS, NO QUERY UPDATES THEM" IS WHAT THIS
-- PARAGRAPH SAID, AND AN AUDIT REFUTED THE SECOND HALF. CreatePasswordReset's
-- `retired` CTE matches SIBLINGS by (tenant_id, admin_user_id) -- not by hash -- so a
-- frozen row that is still LIVE (unspent, unretired, unexpired) is inside that UPDATE's
-- target set. The audit planted such a row at v18 on a fresh database, migrated to 19,
-- and ran the production statement: `23514 password_resets_token_hash_shape`. The
-- consequence is not cosmetic -- that administrator could never be issued a reset link
-- again, and Consume's own `retired` CTE carries the same shape, so consumption would
-- die too. A frozen row is therefore an ACCOUNT LOCK, not a dead row.
--
-- ✅ TODAY'S EXPOSURE IS ZERO AND NO APPLICATION PATH CAN REOPEN IT, both counted rather
-- than argued. LIVE violating rows on this database: 0. Distinct administrators
-- affected: 0. And no frozen row can BECOME live, because `expires_at` is not in
-- tappa_app's UPDATE grant (measured false) -- every one of the 6 502 residue rows was
-- written with a one-hour TTL and has expired. On a fresh database the class cannot
-- arise at all, because the CHECK is in force from the first row.
--
-- ⚠️ "PERMANENTLY SHUT" IS WHAT THIS LINE SAID AND IT IS ONE WORD TOO STRONG. The Down
-- below DROPS the constraint, so a Down -> traffic -> Up cycle can write violating rows
-- in between and bring the class back. The accurate claim is narrower and still the one
-- that matters: no APPLICATION path can create such a row, because
-- internal/adminauth.ResetToken.hash always produces 64 lower-case hex -- the Down/Up
-- cycle excepted.
--
-- ⚠️ AND THE REMEDY IS NOT THE ONE 00018 NAMES. That migration can say "a statement
-- that fixes the digest unfreezes the row" because password_hash IS in tappa_app's
-- UPDATE grant. Here `token_hash` is NOT (measured false), so the only role that can
-- unfreeze a password_resets row is tappa_owner -- i.e. an operator with the migration
-- credentials, not the application. If this ever happens in production the fix is a
-- one-off owner statement, and it should be preceded by asking how a non-hash value
-- got in.
--
-- 🔴 AND THAT ONE-OFF STATEMENT CAN LEAK THE HASH, WHICH IS A BEARER CREDENTIAL IN THIS
-- DESIGN (this file says so at the resolver). Measured, the SAME rejected INSERT under
-- two roles:
--
--   as tappa_app     ERROR 23514 ... violates check constraint "..."   -- NO DETAIL
--   as tappa_owner   ERROR ... DETAIL: Failing row contains (..., <token_hash>, ...)
--
-- ⚠️ WHAT SUPPRESSES IT FOR THE APPLICATION ROLE IS **NOT THIS MIGRATION** -- it is a
-- side effect of FORCE RLS: Postgres withholds the row description from a role that
-- cannot read the whole row unconditionally. Nobody had written that down, which is how
-- it comes back: the first code path that runs under an RLS-exempt role gets the DETAIL
-- line again, and 00018 records the identical exposure for password_hash.
--
-- SO THE REMEDY HAS A PROCEDURE: run the owner statement with `\set VERBOSITY terse`,
-- in a session that is not writing to a collected log (the development compose file
-- runs log_statement=all -- measured: a token_hash appears in it twice per statement),
-- and never paste the failing row into a ticket.
--
-- THE ONE-LINER THAT FINISHES IT on a database where zero rows violate (a fresh
-- deployment, or after `make db-reset`):
--   ALTER TABLE password_resets VALIDATE CONSTRAINT password_resets_token_hash_shape;
-- Nothing runs it automatically, for the reason 00018 gives: on THIS database it
-- would fail and take the migration with it.
ALTER TABLE password_resets
    ADD CONSTRAINT password_resets_token_hash_shape
    CHECK (token_hash ~ '^[0-9a-f]{64}$')
    NOT VALID;


-- --- 4. the grants: which columns may be written, at all ----------------------
--
-- DIKKAT (the M1-04 lesson, recorded and re-recorded in 0005/0013/0016/0017):
-- db-init's ALTER DEFAULT PRIVILEGES hands tappa_app all four DMLs on every new
-- table, so NARROWING A GRANT DOES NOTHING ON ITS OWN -- the table-level privilege
-- must be REVOKEd first, which also clears any column grants on it. 00016 and 00017
-- both record making this mistake once each. DELETE stays revoked (00006).
--
-- UPDATE: TWO COLUMNS. used_at is the single-use stamp; cancelled_at is the
-- supersede stamp. Everything else becomes `permission denied` at the PRIVILEGE
-- level, i.e. structurally -- A1 (re-point at another admin), A2 (resurrect), A4
-- (graft a chosen hash) and any rewrite of tenant_id, id or created_at.
REVOKE UPDATE ON password_resets FROM tappa_app;
GRANT UPDATE (used_at, cancelled_at) ON password_resets TO tappa_app;

-- INSERT: FIVE COLUMNS, and the two exclusions are the point rather than tidiness.
--
--   created_at   🔴 THE LOAD-BEARING ONE. It is half of the TTL ceiling above:
--                with it writable, A6 issues a token whose 24-hour window opens in
--                ten years, so the constraint would be satisfiable and useless.
--                This is 00017's created_at argument reappearing on a different
--                table for a DIFFERENT reason -- there it fixed an ordering, here
--                it fixes a duration -- which is why it is spelled out instead of
--                cross-referenced.
--   used_at,     A reset that is born spent or born cancelled has no use, and
--   cancelled_at excluding them makes "NULL -> now(), never back" the only
--                transition the privilege system permits to originate anywhere.
--
-- id IS granted for the reason 00017 gives on admin_users: test fixtures name it,
-- it cannot carry harm (RLS's WITH CHECK on tenant_id binds the row to the caller's
-- own tenant regardless), and excluding it would cost an edit across fixtures for
-- no security.
REVOKE INSERT ON password_resets FROM tappa_app;
GRANT INSERT (id, tenant_id, admin_user_id, token_hash, expires_at)
    ON password_resets TO tappa_app;

-- ⚠️ THE LIMIT A COLUMN GRANT CANNOT EXPRESS, restated because 00009 and 00012 both
-- had to: a column grant says WHICH column may be written, never WHAT VALUE.
-- `SET used_at = NULL` (un-consume, A3) and `SET cancelled_at = NULL` are still
-- permitted by the privilege system. What forbids them is that NO STATEMENT in
-- db/queries does it -- ConsumePasswordResetAndSetPassword only ever moves used_at
-- from NULL to now(), and both cancel paths require `cancelled_at IS NULL` in their
-- WHERE. That is this repository's discipline, not the database's. Making the
-- TRANSITION structural needs a BEFORE UPDATE trigger, which would have to answer
-- for employee_invites at the same time; deliberately not done here.


-- --- 5. resolve_password_reset_by_token_hash -- the SIXTH contained bypass -----
--
-- A reset link carries ONLY a token. The tenant is therefore the RESULT of the
-- lookup and not an input, which is exactly the situation ADR 0002 madde 7 exists
-- for -- the sixth after resolve_tag_by_uid (00004), resolve_session_by_token_hash
-- (00003), resolve_invite_by_code_hash (00009) and 00011's two admin lookups.
-- Measured, as tappa_app with no context: a direct SELECT on password_resets by
-- token_hash returns 0 rows (FORCE RLS, fail-closed), so without this function the
-- flow cannot exist at all.
--
-- THE ALTERNATIVE THAT WAS REJECTED, because it needs no new database object and is
-- therefore the tempting one: put the tenant id in the reset LINK next to the token
-- and let the handler open the context from it. It is refused for the reason
-- db/queries/resolve.sql already gives about the admin session cookie -- "tenant is
-- the OUTPUT of resolution, never an input; a middleware that accepts one lets the
-- caller name its own tenant". A URL parameter that decides which tenant's RLS
-- context a request runs in is a parameter an attacker chooses, and the fact that
-- the token would still have to match does not make the CONTEXT safe: every other
-- statement in that transaction would run in a tenant the caller named.
--
-- THE SECOND ALTERNATIVE, also rejected, and this one is subtler: ask the user to
-- re-enter their email on the reset page and reuse resolve_admin_by_email. It adds
-- no bypass surface and it FAILS FOR A PRODUCT REASON -- the reset would then
-- inherit adminauth.MaxCandidates, so a customer whose admin row sorts past the
-- comparison window (00017's remaining, documented lockout) could not reset the
-- password they cannot use either. A reset flow that is dead exactly for the people
-- who most need it is worse than a sixth contained resolver.
--
-- "THERE ARE ALREADY FIVE" IS NOT A JUSTIFICATION (ADR 0002 madde 7's own rule).
-- The five constraints are re-earned here and measured in
-- internal/db/passwordresets_test.go, not asserted:
--   (i)   key-only input                p_token_hash text, one parameter
--   (ii)  bounded result                <=1 row, from the GLOBAL UNIQUE index on
--                                       token_hash (00006) -- a property of the
--                                       index, not of the body
--   (iii) fixed column list, no SELECT * token_hash (the input) and created_at
--                                       (more than the caller needs) are NOT
--                                       returned
--   (iv)  EXECUTE to tappa_app alone, no cross-tenant table SELECT for tappa_app
--   (v)   no naive "context is NULL -> show rows" RLS branch: the table keeps
--         00006's standard NULLIF policy, untouched by this migration.

-- Column-level SELECT for the definer role. created_at is NOT in the list, for the
-- same reason it is not in the RETURNS list: nothing in resolution reads it. In
-- db-init tappa_resolver holds NO default privileges, so its blast radius stays
-- structurally limited to the columns named here.
GRANT SELECT (id, tenant_id, admin_user_id, token_hash, expires_at, used_at, cancelled_at)
    ON password_resets TO tappa_resolver;

-- +goose StatementBegin
CREATE FUNCTION resolve_password_reset_by_token_hash(p_token_hash text)
    RETURNS TABLE (
        id            uuid,
        tenant_id     uuid,
        admin_user_id uuid,
        expires_at    timestamptz,
        used_at       timestamptz,
        cancelled_at  timestamptz
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    -- KRITIK (search_path injection): the path is pinned and public is NOT on it,
    -- so the table is explicitly qualified public.password_resets -- otherwise a
    -- caller could interpose their own `password_resets`. pg_temp goes last
    -- (Postgres' own recommendation).
    SET search_path = pg_catalog, pg_temp
    AS $$
        -- <=1 row: token_hash is GLOBALLY UNIQUE (00006). Fixed column list, no
        -- SELECT *. token_hash is the INPUT and is deliberately NOT returned --
        -- nothing outside this lookup needs it back, and in this design the hash
        -- is itself a BEARER credential (hash -> resolver -> tenant -> consumption),
        -- so handing it back would put it one %+v away from a log line (section 4.7).
        --
        -- ⚠️ THAT SENTENCE IS ABOUT THE **OUTPUT** SIDE ONLY, AND THE INPUT SIDE IS NOT
        -- PROTECTED. The raw token carries five redaction interfaces
        -- (internal/adminauth.ResetToken); its HASH is a bare Go `string` in
        -- store.CreatePasswordResetParams.TokenHash and
        -- store.ConsumePasswordResetAndSetPasswordParams.TokenHash/.PasswordHash, so a
        -- `%+v` of a Params value prints it. This is the REPOSITORY-WIDE pattern rather
        -- than a deviation introduced here -- CreateAdminUserParams.PasswordHash and
        -- CreateInviteParams.CodeHash are bare too -- and sqlc generates those structs,
        -- so changing it means changing every generated params type. NOT done here; the
        -- obligation it hands phase B is exact and greppable: NEVER print a store
        -- *Params value.
        --
        -- THE THREE STATE COLUMNS ARE RETURNED BUT NOT FILTERED, which is the
        -- principle 00003 states for sessions.revoked_at and 00009 for invites: the
        -- data layer carries the truth and the DOMAIN decides. A resolver that hid
        -- a spent or expired reset would answer "unknown token" for a token that
        -- really exists, and the caller could not tell a REPLAY from a typo -- the
        -- shape that loses a record (section 4.6) at exactly the moment an
        -- account-takeover attempt would be visible.
        --
        -- 🔴 AND THEY MUST NOT BE COMPARED THEN WRITTEN (section 4.4 TOCTOU, the
        -- tags.last_ctr trap in another costume). The authority on whether a token
        -- may still be spent is the single atomic statement
        -- ConsumePasswordResetAndSetPassword, whose WHERE carries the state test.
        SELECT r.id, r.tenant_id, r.admin_user_id, r.expires_at, r.used_at,
               r.cancelled_at
        FROM public.password_resets AS r
        WHERE r.token_hash = p_token_hash;
    $$;
-- +goose StatementEnd

-- EN-AZ-AYRICALIK: the definer is tappa_resolver (NOLOGIN, BYPASSRLS, NOSUPERUSER).
-- A SECURITY DEFINER body runs with its OWNER's rights, so a superuser owner would
-- bypass RLS database-wide -- the general bypass ADR 0002 madde 7 forbids. This is
-- REQUIRED, not decorative: CREATE makes the function owned by whoever ran the
-- migration (tappa_owner, a superuser).
ALTER FUNCTION resolve_password_reset_by_token_hash(text) OWNER TO tappa_resolver;

-- KRITIK: functions get PUBLIC EXECUTE by default. Revoke it all, then grant to the
-- single legitimate caller.
REVOKE ALL ON FUNCTION resolve_password_reset_by_token_hash(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION resolve_password_reset_by_token_hash(text) TO tappa_app;


-- +goose Down

-- 🔴 THIS DOWN IS A SECURITY REGRESSION, NOT A NEUTRAL UNDO -- the 00018 wording,
-- for a wider set of holes. Running it restores every capability measured at the
-- top of this file: A1 (re-point a live reset token at another administrator of the
-- same tenant) and A2/A3/A4 become legal for tappa_app again, and A5/A6/A7 become
-- writable again. It is written, and it runs, because CLAUDE.md section 6 requires
-- reversibility and because a Down that is never exercised is a Down that does not
-- work. Running it outside a rollback of this whole change set is a decision
-- somebody has to own.
--
-- ORDER: the function first (its body references public.password_resets and its
-- grant names cancelled_at), then the grants, then the constraints, then the
-- column. The reverse order would leave the function referring to a column that no
-- longer exists.
DROP FUNCTION IF EXISTS resolve_password_reset_by_token_hash(text);

-- The column grant would fall with the column anyway; it is revoked EXPLICITLY so
-- that re-adding the column later cannot resurrect a ghost privilege (00012's
-- reasoning, verbatim).
REVOKE SELECT (id, tenant_id, admin_user_id, token_hash, expires_at, used_at, cancelled_at)
    ON password_resets FROM tappa_resolver;

-- Restore 00006's table-wide grants EXACTLY. The column-level grants are revoked
-- first for the same M1-04 discipline in reverse: a table-level GRANT supersedes
-- column-level ones in has_column_privilege terms, but leaving them behind would
-- make the restored catalog differ from 00006's.
REVOKE INSERT ON password_resets FROM tappa_app;
GRANT INSERT ON password_resets TO tappa_app;
REVOKE UPDATE ON password_resets FROM tappa_app;
GRANT UPDATE ON password_resets TO tappa_app;

ALTER TABLE password_resets DROP CONSTRAINT password_resets_token_hash_shape;
ALTER TABLE password_resets DROP CONSTRAINT password_resets_ttl_ceiling;
ALTER TABLE password_resets DROP COLUMN cancelled_at;
