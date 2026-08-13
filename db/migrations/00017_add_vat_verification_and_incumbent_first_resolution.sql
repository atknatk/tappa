-- +goose Up

-- Migration 0017 -- M7-02: the DATA layer the PUBLIC sign-up wizard needs.
--
-- It adds NO table, so CLAUDE.md section 6's five-fold rule has nothing to apply
-- to. What it does is two things that both exist because ONE fact changes today:
-- until this milestone no APPLICATION PATH created a tenant, and from M7-02 a
-- stranger can.
--
--   1. tenants.vat_verified / tenants.vat_checked_at -- Q09's decision recorded as
--      DATA rather than as a promise, plus the column-level INSERT grant that lets
--      the wizard write them.
--   2. resolve_admin_by_email is REPLACED so its ordering is INCUMBENT-FIRST. That
--      is migration 00011's PHASE B OBLIGATION 4 being paid a second time, against
--      the harm that only exists once sign-up is public.
--
-- ============================================================================
-- WHY (2) IS IN THIS MIGRATION AND NOT SOMEWHERE MORE COMFORTABLE
-- ============================================================================
-- 00011 wrote down an expiry date and named this task:
--
--   "NOT EXPLOITABLE TODAY, and the reason is worth naming because it expires: no
--    APPLICATION path creates a tenant ... M7-02 changes exactly that. Its sign-up
--    wizard is PUBLIC, and RLS does not stop a tenant from writing ANY email it
--    likes into its OWN admin_users -- so from M7-02 the ATTACKER controls the row
--    count, i.e. controls the bound."
--
-- internal/adminauth/manager.go then chose 00011's FIRST option (cap the candidates
-- one request will compare, MaxCandidates = 8) and named the price in the same
-- breath: "THE CAP CONVERTS A CPU DoS INTO AN ACCOUNT-LOCKOUT DoS ... an attacker
-- who registers nine tenants carrying a victim's address can push the victim's real
-- row past the cap."
--
-- THAT LOCKOUT IS NOT A PREDICTION. Measured on THIS database, before this
-- migration, five independent runs (one victim tenant created a year ago, twenty
-- attacker tenants created now, all carrying the same address, planted inside a
-- transaction and rolled back):
--
--   rows resolved       21  21  21  21  21
--   victim's position   18  16   4  17   4      <- ORDER BY tenant_id, i.e. random
--   compared at all?    NO  NO  YES NO  YES
--
-- Three of five runs put a PAYING CUSTOMER outside the compared window, at which
-- point their correct password is answered with "those credentials did not work"
-- and nothing tells them why (it must not -- OBLIGATION 1). The attacker spends
-- twenty sign-ups; the customer loses their panel.
--
-- ============================================================================
-- THE FIX IS AN ORDERING, AND THE REASON IT WORKS IS THAT THE ATTACK NEEDS TO
-- OVERTAKE SOMEBODY WHO WAS ALREADY THERE
-- ============================================================================
-- ORDER BY tenant_id is a random permutation with respect to WHEN a row was
-- written -- a tenant id is gen_random_uuid(), so an attacker registering N tenants
-- gets N/(N+1) of the ordering on average and the incumbent's place is a coin toss.
-- ORDER BY created_at, tenant_id makes the OLDEST row first, and the one thing an
-- attacker cannot do is register EARLIER than a customer who is already a customer.
--
-- WHAT IT CLOSES, EXACTLY: a person who ALREADY HAS an account cannot be pushed out
-- of the compared window by any number of later registrations. That is the whole of
-- the harm M7-02 creates for existing customers, and it is the harm 00011 handed
-- forward.
--
-- 🔴 WHAT IT DOES **NOT** CLOSE, AND THIS IS A LIMIT RATHER THAN A DETAIL. An
-- attacker who plants eight or more rows carrying an address BEFORE that address
-- ever signs up still owns the whole window, and the real customer -- arriving
-- later, sorting later -- cannot log in from the day they register. The ordering
-- makes the attack require FORESIGHT instead of merely requiring volume, which is a
-- large practical difference and not a proof. THE THING THAT ACTUALLY CLOSES IT is
-- the option 00011 listed third and assigned to this milestone: "require email
-- verification before a tenant's admin row is resolvable at all". That needs an
-- email transport, which this repository does not have (Q02 is open and M7-04 owns
-- it), so it is NOT built here and is NOT claimed.
--
-- 🔴🔴 AND IT OPENED TWO CHANNELS OF ITS OWN. BOTH ARE NOW CLOSED, IN
-- internal/adminauth AND NOT HERE, AND THIS FILE SPENT TWO ROUNDS CLAIMING THEY COULD
-- NOT BE. THIS IS THE MOST IMPORTANT PARAGRAPH IN IT.
--
-- The ordering makes the incumbent RELIABLY occupy the first slot of the compared
-- window -- and therefore reliably FAIL to verify against somebody else's password.
-- An attacker who plants exactly adminauth.MaxCandidates rows for an address and
-- signs in with their OWN password reads the tenant picker's row count as an answer
-- to "does this address already have a Tappa account". Measured through
-- adminauth.Authenticate on this stack, 3 of 3 runs:
--
--   address unknown     planted=8 resolved=8 compared=8 truncated=false  PICKER SHOWS 8
--   address REGISTERED  planted=8 resolved=9 compared=8 truncated=true   PICKER SHOWS 7
--
-- That is the question 00011's OBLIGATION 1 spends a dummy bcrypt refusing to answer.
--
-- ⚠️ THE ORDERING DID NOT CREATE THE CHANNEL, IT MADE IT DETERMINISTIC. Under
-- ORDER BY tenant_id the incumbent landed inside the window with probability 8/9, so
-- the same count leaked with noise. This migration turns a probabilistic leak into a
-- certain one, and that is stated plainly rather than left for the next audit.
--
-- THE **PICKER COUNT** LEAKS ONLY AT SATURATION (same probe):
--
--   planted=3 unknown -> shows 3   ·   planted=3 REGISTERED -> shows 3
--   planted=5 unknown -> shows 5   ·   planted=5 REGISTERED -> shows 5
--
-- 🔴 AND AN EARLIER VERSION OF THIS FILE READ THAT AS "BELOW THE CAP THERE IS NO
-- SIGNAL AT ALL", WHICH IS FALSE AND WAS THE WORST SENTENCE IN IT. The picker COUNT
-- does not move below saturation; the RESPONSE TIME does, because Authenticate
-- compared every candidate in the window and nothing padded it. Measured on the
-- shipped code with ONE planted row, nine interleaved rounds:
--
--   address UNKNOWN     median 216 ms  (208-226)
--   address REGISTERED  median 437 ms  (422-478)     the two ranges do NOT overlap
--
-- So the question OBLIGATION 1 refuses was answerable for the price of ONE
-- registration, not eight, and the "two hours per address" cost this file published
-- was eight times too high because it was priced against the wrong channel.
--
-- ✅ THAT CHANNEL IS CLOSED IN internal/adminauth: every login now performs exactly
-- MaxCandidates comparisons, the real ones padded with dummies (padComparisons), so
-- the number of candidates is no longer observable in time. Both readings of that
-- trade are recorded there.
--
-- 🔴 AND THE SENTENCE THAT USED TO END THAT PARAGRAPH WAS WRONG, IN THE PLACE IT
-- MATTERED MOST. It read "NO new DoS headroom -- the attempt budget was already sized
-- for eight comparisons per request", and a security audit measured that the attempt
-- budget (adminAttemptLimit) is charged ONLY BY FAILURES: failLogin charges it,
-- completeLogin charges nothing. Measured on the shipped stack, 18 consecutive
-- SUCCESSFUL sign-ins drew zero refusals while 13 failures were cut off at the 11th.
-- So the padding's cost on the SUCCESS path was bounded only by adminFloodLimit
-- (3000 per window), and at eight comparisons each that is ~15 cores from one address.
-- The trade was bought with a claim about a budget that does not measure that path --
-- the same defect class that blocked this task twice, this time underneath a security
-- decision.
--
-- ✅ internal/handler/adminratelimit.go's adminLoginWorkLimit CLOSES IT, and closes
-- MORE THAN M7-02 OPENED. It meters every login that reaches the password loop
-- whatever its outcome, at 120 per 10 minutes per address -- a number derived from
-- that file's own office model (10 admins x 2 devices x 2 headroom), not from a CPU
-- target. The resulting ceiling is ~365 CPU-s per window per address, about 0.61 of
-- one core.
--
-- 🔴 STATED PLAINLY BECAUSE IT IS EASY TO READ THIS AS MERELY UNDOING M7-02'S OWN
-- REGRESSION: THIS PATH WAS UNBUDGETED BEFORE M7-02 AS WELL. At one comparison per
-- login the same 3000-per-window ceiling allowed roughly 1.9 cores from a single
-- address, and nothing metered it; the padding made that 8x worse and therefore
-- visible. 0.61 cores is BELOW the pre-M7-02 figure, so what ships here is not a
-- restoration -- it closes a hole that predates this milestone.
--
-- WHY THAT REACHES §4.6: the tap surface shares this process and this connection pool
-- (httpx.TapLimiter says so from its side). CPU a panel flood has taken is CPU a tap
-- cannot have, and a tap that cannot be served is an attendance record that is not
-- written -- the one thing §4.6 forbids losing.
--
-- ✅ AND THE PICKER COUNT IS CLOSED TOO, by a fifth closure an audit found after the
-- paragraph below was written. adminauth.PickerCap caps what the picker DISPLAYS at
-- MaxCandidates-1, which absorbs exactly the slot the incumbent occupies: measured
-- over k = 0..40, with the cap no k leaks and without it every k >= 8 does.
--
-- FOUR CLOSURES WERE BUILT AND MEASURED. Every one is worse:
--
--  1. COMPARE EVERY RESOLVED CANDIDATE (apply the bound after the loop, not before).
--     It closes the channel perfectly. Measured: 200 candidates = 45.2 s of CPU in
--     ONE credential-less request, i.e. ~1 m 53 s at 500. That is the denial of
--     service the cap exists for, restored in full.
--  2. CAP HOW MANY BUSINESSES ONE EMAIL MAY SELF-REGISTER (say 3), so the attacker
--     can never reach the window. Measured: the identical oracle reappears at the new
--     bound -- 3 vs 2 -- for a THIRD of the price. Strictly worse.
--  3. COMPARE THE OLDEST ROW **IN ADDITION** TO MaxCandidates OTHERS. Measured: it
--     closes the 8-row probe (8 vs 8) and reopens at 9 (9 vs 8). It MOVES the
--     saturation point by one and costs one more bcrypt per login.
--  4. COMPARE A RANDOM SAMPLE instead of a prefix, to make the count noisy again.
--     Measured over 900 trials: the incumbent is left uncompared in 10.1% of
--     sign-ins, i.e. an INTERMITTENT lockout of a paying customer -- harder to
--     diagnose than the deterministic one this migration exists to fix.
--
-- 🔴 A "GENERAL STATEMENT" STOOD HERE AND IT IS **WITHDRAWN**. It read: "any bound the
-- caller can SATURATE reproduces the signal at the bound, and lowering the bound makes
-- the probe cheaper", with the only escapes being an unbounded window or email
-- verification. An audit refuted it by bounding a DIFFERENT thing -- not the window
-- that is COMPARED but the list that is DISPLAYED (adminauth.PickerCap). Four closures
-- failing the same way was not evidence that all closures must; it was evidence that
-- all four had been aimed at the same mechanism.
--
-- ✅ SO BOTH CHANNELS ARE CLOSED, by two mechanisms in one other package, and neither
-- of them is this ordering:
--   the picker COUNT   adminauth.PickerCap        display cap = compare cap - 1
--   the response TIME  adminauth.padComparisons   a constant number of comparisons
--
-- WHAT REMAINS OPEN is the audit_log write asymmetry internal/adminauth already
-- documents and bounds (~1-3% of a response). The probe is also made OBSERVABLE:
-- internal/handler.AdminAuth.recordCandidateProbe writes one audit_log row, into a
-- tenant whose digest actually verified, metered by the account budget so it cannot
-- become an unbounded write into a table nobody can delete from. ⚠️ That row's
-- signature is "this address is over-subscribed", NOT "somebody is probing" -- the
-- VICTIM's own correct sign-in produces it too (measured), which is stated at the
-- function rather than left to be inferred.
--
-- ⚠️ AND A SECOND ORDERING IS **WORSE** FOR THE NEW CUSTOMER THAN THE ONE BEING
-- REPLACED, WHICH IS WORTH STATING BECAUSE IT LOOKS LIKE A REGRESSION AND IS ONE.
-- Under ORDER BY tenant_id a brand-new registration lands at a random position and
-- has SOME chance of being compared even when rows already exist for its address.
-- Under created_at it lands LAST, deterministically.
--
-- 🔴 THE TRADE WAS DEFENDED HERE WITH A SENTENCE THE PRODUCT DID NOT KEEP, AND AN
-- AUDIT MEASURED IT END TO END. It said the new customer "finds out immediately, at a
-- moment when they are on our page and can be told something". Measured: eight rows
-- planted on an unused address, a real registration completed against it -> 303, a
-- confirmation stamped APPROVED saying "Sign in - use the email address and password
-- you just chose", and the very next request answering 401 with no explanation, on the
-- one screen that is forbidden to give one (OBLIGATION 1). The customer was told
-- nothing, at the only moment they could have acted.
--
-- ✅ THE HALF THAT WAS MISSING IS NOW BUILT, so the sentence is true rather than
-- softened. internal/domain/signup.Provisioner.signInBlocked resolves the address
-- AFTER the transaction commits and reports whether the row it just wrote falls inside
-- the window a login compares; the confirmation then says so instead of inviting a
-- sign-in that cannot work. It is not an enumeration oracle: it is reachable only by
-- COMPLETING a registration and it branches on ONE BIT ("is the row I just wrote in
-- the window").
--
-- 🔴 BUT THAT BIT IS ITSELF A CHANNEL, AND THIS FILE USED TO DENY IT — the sentence
-- "an address with one, two or seven existing accounts answers exactly as an unknown
-- one does" is TRUE ONLY WHILE THE ATTACKER PLANTS NOTHING, and planting is the
-- premise of every control on this page. Measured on real Postgres: plant
-- MaxCandidates-1 rows for an address, then complete ONE more registration. Unknown
-- address -> 8 identities -> false. One existing account -> 9 -> true.
--
-- ⚠️ AND IT IS NOT ONE BIT IF THE PLANT COUNT IS VARIED: registering at several plant
-- counts and observing where the flag flips reveals k, the number of identities
-- already present, EXACTLY.
--
-- IT IS THE SHAPE THIS FILE REJECTED AS CLOSURE #2 ("the identical oracle reappears at
-- the new bound"), and the shape the withdrawn general statement described — with one
-- difference that matters: the picker count was a DISPLAY cap, which a narrower
-- display cap could absorb (PickerCap). This is the comparison window's EDGE, reported
-- to the caller, and no narrower report exists that still tells a customer their
-- account will not work.
--
-- SO IT IS COUNTED RATHER THAN CLOSED, AND MADE OBSERVABLE:
--   PRICE       MaxCandidates COMPLETED registrations per address probed, each needing
--               its own globally UNIQUE vat_number and a unit of signupCreateLimit
--               (3/hour/address) -- about three hours from one address, i.e. roughly
--               EIGHT TIMES the price of the timing channel that WAS closed.
--   TRAIL       internal/domain/signup writes signup.sign_in_unreachable into the
--               tenant the registration just created, bounded by the create budget it
--               already runs under, carrying counts and never the address (§4.7).
--   ALTERNATIVE Removing the check restores the defect an audit measured end to end: a
--               real customer gets an APPROVED stamp and then a bare 401 on the one
--               screen that is forbidden to explain itself. A certain loss to a paying
--               customer is worse than an expensive, counted, observable signal.
--
-- So the trade stands as argued: a customer who cannot use a new account is told at
-- the moment they are on our page, and a customer who has been using a panel for a
-- year is not silently locked out of it when payroll is due.
--
-- ⚠️ IT IS NOT A CPU BOUND AND DOES NOT PRETEND TO BE ONE. What bounds the bcrypt
-- work per request is still adminauth.MaxCandidates; this migration does not change
-- how many rows are compared, only WHICH ones. The amplification arithmetic in
-- 00011 (its "~500x", corrected to ~190 s at the deployed cost 12 in
-- internal/adminauth/manager.go) describes an UNCAPPED loop and has been bounded
-- since M6-01 phase B. M7-02 re-measures it rather than inheriting the sentence --
-- see the task's report.
--
-- KIRMIZI CIZGILER touched:
--   section 4.5  The replaced function is the SAME contained bypass ADR 0002 madde
--                7 defines: definer tappa_resolver, pinned search_path,
--                schema-qualified body, OPERATOR(public.=), column-level SELECT,
--                EXECUTE to tappa_app alone, no PUBLIC EXECUTE, fixed column list.
--                Every one of those is restated below rather than inherited,
--                because DROP + CREATE does not carry an owner or an ACL forward.
--   section 4.6  vat_verified is NULLABLE ON PURPOSE. See its comment: a VIES
--                outage recorded as `false` would be an accusation, and a VIES
--                outage recorded as nothing at all would be the silent approval
--                4.6 forbids.


-- --- 1. VAT verification, as recorded fact ------------------------------------
--
-- Q09 (orchestrator decision, 2026-08-13): FORMAT validation is mandatory and
-- server-side; VIES is BEST EFFORT -- a short timeout, and a failure or timeout
-- does NOT stop the sign-up (the M7-02 card's own criterion). The outcome is STORED.
--
-- ⚠️ IT IS **NOT** SURFACED IN THE PANEL, AND THIS LINE SAID IT WAS. Measured: no
-- query in db/queries READS either column (the only mention is CreateTenant's
-- INSERT), no panel screen shows a VAT number, and the grant below deliberately
-- withholds UPDATE so a re-check is not possible either. The sign-up confirmation is
-- the one place a customer is told, at the moment they are told it. The panel surface
-- belongs to M7-05 ("firma bilgileri, fatura verileri"), and the two cheap-looking
-- homes were costed and rejected -- see web/templates/pages/signupview.go's VATNote,
-- whose wording is held to the product by
-- TestSignupDone_PromisesNoPanelSurfaceForTheVATCheck.
--
-- 🔴 THREE STATES, NOT TWO, AND THE THIRD IS THE WHOLE POINT. A boolean NOT NULL
-- would have to record "VIES did not answer" as `false`, i.e. as "this VAT number
-- is not valid" -- which is an accusation against a paying customer caused by an
-- outage at the European Commission. The honest vocabulary is:
--
--   vat_checked_at IS NULL                     we have never asked
--   vat_checked_at set, vat_verified IS NULL    we asked and got no answer
--   vat_checked_at set, vat_verified = true     VIES confirmed the number
--   vat_checked_at set, vat_verified = false    VIES answered: not a valid number
--
-- The fourth row is the only one that may be shown to a customer as a problem, and
-- the second is the only one that is OUR problem. Collapsing them is what a boolean
-- would have done.
--
-- NO DEFAULT AND NO NOT NULL on vat_verified: the absence of a value IS a value
-- here. vat_checked_at is likewise NULL until somebody asks.
ALTER TABLE tenants
    ADD COLUMN vat_verified   boolean,
    ADD COLUMN vat_checked_at timestamptz;

-- THE GRANT IS **INSERT ONLY**, AND THE OMISSION OF UPDATE IS DELIBERATE (the 00016
-- discipline: do not open what nobody needs yet).
--
-- 00016 narrowed tappa_app's INSERT on `tenants` to a fixed column list, so a column
-- added afterwards is NOT writable until it is named here -- measured, that is the
-- whole reason this statement exists rather than being implied by the ALTER above.
-- The wizard writes both columns once, at sign-up, from the VIES answer it has (or
-- has not) received.
--
-- RE-CHECKING A VAT NUMBER LATER IS **NOT POSSIBLE** as this migration stands, and
-- that is named rather than smuggled: a tenant whose check timed out keeps
-- vat_verified IS NULL until somebody grants UPDATE on these two columns. That is
-- M7-05's (account settings) decision to make, with its own argument about who may
-- trigger a re-check and how often -- an outbound HTTP call a customer can trigger
-- at will is a different threat model from one that happens once per registration.
--
-- The M1-04 lesson does NOT apply in reverse here: a column-level GRANT that ADDS
-- columns to an existing column-level grant is cumulative, so no REVOKE is needed
-- and issuing one would drop the five columns 00016 granted.
GRANT INSERT (vat_verified, vat_checked_at) ON tenants TO tappa_app;


-- --- 2. resolve_admin_by_email, incumbent-first --------------------------------
--
-- WHY DROP + CREATE RATHER THAN CREATE OR REPLACE. Replace preserves the owner and
-- the ACL, which sounds like the safer verb -- and that is exactly the problem: the
-- eight properties that make this function a CONTAINED bypass rather than a general
-- one would then be invisible in this file, inherited from a migration two
-- milestones back. ADR 0002 madde 7's constraints are restated below, in full, so
-- the function's security profile is readable in the file that last wrote it.
--
-- created_at IS NEEDED BY THE BODY AND IS NOT RETURNED. tappa_resolver's grant is
-- column-level (00011), so ordering by a column it cannot read fails outright --
-- measured, `permission denied for table admin_users`. It is granted for the same
-- reason `email` is: the body USES it. It stays out of the RETURNS list for the same
-- reason `role` does -- nothing before authentication reads it, and ADR 0002 madde 7
-- constraint (iii) forbids widening the pre-auth surface for a value nothing pre-auth
-- reads.
GRANT SELECT (created_at) ON admin_users TO tappa_resolver;

-- --- 3. admin_users: the ordering column stops being writable ---------------------
--
-- 🔴 EVERYTHING ABOVE RESTS ON created_at, AND UNTIL THIS STATEMENT ANY CODE PATH
-- RUNNING AS tappa_app COULD REWRITE IT. The whole argument for the new ordering is
-- "the one thing an attacker cannot do is register EARLIER than somebody who is
-- already a customer" — and 00006 grants tappa_app TABLE-WIDE UPDATE, so
-- `UPDATE admin_users SET created_at = ...` was privilege-legal (measured:
-- relacl `tappa_app=arw`, has_column_privilege on created_at true). The guarantee was
-- discipline, not structure, in the same migration that carefully kept 00016's
-- column-list discipline for `tenants` two statements up. A security audit named that
-- inconsistency.
--
-- 🔴 IT IS ALSO WHY 00011's REASON FOR LEAVING IT WIDE NO LONGER HOLDS. That file
-- argued a column grant "would merely enumerate the table", because nearly every
-- column is legitimately writable — password_hash on reset, status on disable,
-- full_name/email/role in panel management, last_login_at on login. That list is
-- exactly the grant below, and the three columns it OMITS are precisely the ones that
-- are never legitimately written: `id` and `tenant_id` (identity — re-pointing a row
-- has no valid use, and the RLS WITH CHECK already refuses a foreign tenant_id) and
-- `created_at` (the ordering this migration depends on). So it is not an enumeration
-- any more; it is three exclusions, one of which is load-bearing.
--
-- NOT EXPLOITABLE TODAY, and the reason is stated because that is this file's habit:
-- no query in db/queries writes created_at, and the single UPDATE on this table is
-- MarkAdminLoggedIn, which touches last_login_at alone (grepped). What changes is that
-- the protection stops being "no query in this repo does it".
--
-- DIKKAT (the M1-04 lesson, repeated in 0005/0013/0016): db-init's ALTER DEFAULT
-- PRIVILEGES hands tappa_app all four DMLs, so narrowing a GRANT does nothing on its
-- own — the table privilege must be REVOKEd first, which also clears any column-level
-- grants on it. INSERT is untouched (00006's), and DELETE stays revoked (00006, §4.6).
REVOKE UPDATE ON admin_users FROM tappa_app;
GRANT UPDATE (full_name, email, password_hash, role, status, last_login_at)
    ON admin_users TO tappa_app;

-- 🔴 AND THE SAME COLUMN ON THE **INSERT** SIDE, WHICH THE FIRST VERSION OF THIS
-- STATEMENT MISSED. Narrowing UPDATE alone left the identical capability one verb
-- away: a second security audit, running as tappa_app inside its own tenant context
-- and inside BEGIN…ROLLBACK, wrote an admin_users row backdated 3000 days and got
-- INSERT 0 1. Eight such rows put a row IN FRONT of an existing customer in the
-- created_at order and push them out of the compared window — the exact account
-- lockout this migration exists to close, reached by INSERT instead of UPDATE.
--
-- IT IS THE SAME MISTAKE 00016 RECORDS MAKING AND FIXING ("narrowing UPDATE alone
-- leaves the same power one statement away"), repeated one migration later on the
-- other table, which is why the reference is worth carrying.
--
-- 🔴 WHAT IS EXCLUDED AND WHY, since the list otherwise looks like an enumeration:
--   created_at    THE ONE THAT MATTERS. Nothing may choose a row's position in the
--                 order this migration's whole argument rests on. CreateAdminUser does
--                 not name it (the column DEFAULT stamps now()).
--   last_login_at nothing inserts it; it is stamped by MarkAdminLoggedIn.
--
-- ⚠️ `id` AND `status` ARE GRANTED, AND THE MEASUREMENT BEHIND THAT IS STATED RATHER
-- THAN THE CONCLUSION. The PRODUCT needs neither: CreateAdminUser names
-- (tenant_id, full_name, email, password_hash, role) and lets both take their column
-- DEFAULTs, and unlike `tenants` -- where 00016 needed `id` because that table's policy
-- scopes on it -- this table's policy scopes on `tenant_id`, so an INSERT here
-- satisfies WITH CHECK without naming `id` at all (measured: the end-to-end
-- registration below completes with `id` absent from the grant's justification).
-- Excluding them would therefore be defensible. It is NOT taken here, and the reason
-- is a measured cost with no security gain: ten test fixtures across five packages
-- name `id` and `status` explicitly, and neither column can carry the harm this
-- statement is about — `id` is bound to the caller's own tenant by the RLS WITH CHECK
-- on `tenant_id`, and `status` defaults to 'active' with its lifecycle enforced in
-- db/queries rather than by privilege. The exclusion that closes the demonstrated hole
-- is `created_at`, and that one is unconditional.
REVOKE INSERT ON admin_users FROM tappa_app;
GRANT INSERT (id, tenant_id, full_name, email, password_hash, role, status)
    ON admin_users TO tappa_app;


DROP FUNCTION IF EXISTS resolve_admin_by_email(citext);

-- +goose StatementBegin
CREATE FUNCTION resolve_admin_by_email(p_email citext)
    RETURNS TABLE (
        id            uuid,
        tenant_id     uuid,
        password_hash text,
        status        text
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    -- KRITIK (search_path injection): unchanged from 00011 and restated rather than
    -- inherited. public is NOT on the path, so the table is explicitly qualified
    -- public.admin_users and the citext equality is explicitly qualified
    -- OPERATOR(public.=). 00011 measured what happens without the operator
    -- qualification: the lookup silently becomes CASE SENSITIVE (text = text through
    -- citext's implicit cast), an operator typing their address in lower case cannot
    -- log in, and the function's equality stops being the SAME equality as the UNIQUE
    -- index that underwrites the at-most-one-row-per-tenant bound.
    SET search_path = pg_catalog, pg_temp
    AS $$
        -- N rows, bounded by the partial UNIQUE (tenant_id, email): at most one per
        -- tenant, never two of the same tenant. Fixed column list, no SELECT *.
        --
        -- ORDER BY created_at FIRST -- the whole change. The caller compares only the
        -- first adminauth.MaxCandidates rows, so this ordering decides WHOSE password
        -- is tested when several tenants hold one address. Oldest first means an
        -- existing account cannot be displaced by later registrations.
        --
        -- tenant_id SECOND, as the tie-break. created_at is a timestamptz with
        -- microsecond resolution and two rows CAN share one (a single transaction
        -- writing two admins takes one now(); the seed does exactly that), so it is not
        -- a total order on its own -- and an unordered set-returning function makes the
        -- candidate list depend on physical row order, which changes under VACUUM and
        -- under plan changes. 00011 wanted determinism and this keeps it.
        SELECT a.id, a.tenant_id, a.password_hash, a.status
        FROM public.admin_users AS a
        WHERE a.email OPERATOR(public.=) p_email
        ORDER BY a.created_at, a.tenant_id;
    $$;
-- +goose StatementEnd

-- EN-AZ-AYRICALIK: owner tappa_resolver (NOLOGIN, BYPASSRLS, NOSUPERUSER). A
-- SECURITY DEFINER body runs with the OWNER's rights; a superuser owner would bypass
-- RLS database-wide -- the general bypass ADR 0002 madde 7 forbids. DROP + CREATE
-- makes the new function owned by whoever ran the migration, so this is REQUIRED
-- rather than decorative.
ALTER FUNCTION resolve_admin_by_email(citext) OWNER TO tappa_resolver;

-- KRITIK: functions get PUBLIC EXECUTE by default, and a freshly created one has it
-- again. Revoke it all, then grant to the single legitimate caller.
REVOKE ALL ON FUNCTION resolve_admin_by_email(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION resolve_admin_by_email(citext) TO tappa_app;


-- NOTE (measured, deliberately NOT done here): internal/adminauth/password.go names
-- a fourth timing arm on the DIGEST side -- an admin row whose password_hash is not
-- a valid bcrypt string answers ~1.9 million times faster than an unregistered
-- address, because bcrypt errors out before building a key schedule. That file names
-- the structural closure ("a CHECK constraint on the column would make it structural
-- rather than disciplinary") and names M7-02 as one of the tasks that opens the write
-- path.
--
-- IT IS NOT ADDED, AND THE TWO BLOCKERS ARE DIFFERENT IN KIND -- which the first
-- version of this note ran together, in a way that read as a fact about the PRODUCT'S
-- data and was not one.
--
--   THE DATA IS CLEAN. Every admin_users row belonging to the two seed tenants passes
--   the pattern (measured by TENANT ID, not by name: `Kebab Factory Ltd` is a name
--   this repository's own tests have attached to tens of thousands of tenants, and
--   adminratelimit.go records that exact trap). On a freshly seeded database a PLAIN
--   CHECK could be added today.
--
--   THE DEVELOPMENT DATABASE IS NOT, and that is test residue rather than product
--   data: a majority of admin_users rows across thousands of tenants carry the
--   placeholders `x` and `not-a-real-hash`. No figure is written here, because this
--   table grows on every `make test` run -- run the count if you want today's digits.
--   That blocker is backlog T26/T22 (test pollution and a `db-reset` that does not
--   clear it), not this migration's.
--
--   THE REMAINING REAL BLOCKER IS THE SECOND ONE: a CHECK ... NOT VALID does not scan
--   existing rows but DOES enforce on new ones, and FIVE test files write those
--   placeholders (internal/db/admins_test.go, internal/db/rls_test.go,
--   internal/domain/tenant/rulebook_db_test.go, .../rulewriter_db_test.go,
--   internal/domain/billing/billing_db_test.go). Converting them is a mechanical edit
--   across three packages that belongs with whoever owns those fixtures.
--
-- WHAT M7-02 DOES INSTEAD, at the only write path it opens: internal/domain/signup
-- writes password_hash exclusively with adminauth.Hash, which refuses an empty
-- password and anything over MaxPasswordBytes, so this milestone cannot create such a
-- row. That is discipline plus a test, not structure, and it is written down as such.


-- +goose Down

-- Reverse order: the resolver back to 00011's body, then the grants, then the
-- columns.
DROP FUNCTION IF EXISTS resolve_admin_by_email(citext);

-- +goose StatementBegin
CREATE FUNCTION resolve_admin_by_email(p_email citext)
    RETURNS TABLE (
        id            uuid,
        tenant_id     uuid,
        password_hash text,
        status        text
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
        SELECT a.id, a.tenant_id, a.password_hash, a.status
        FROM public.admin_users AS a
        WHERE a.email OPERATOR(public.=) p_email
        ORDER BY a.tenant_id;
    $$;
-- +goose StatementEnd

ALTER FUNCTION resolve_admin_by_email(citext) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION resolve_admin_by_email(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION resolve_admin_by_email(citext) TO tappa_app;

-- Restore 00006's table-wide UPDATE exactly. Revoking the column grant first is the
-- same M1-04 discipline in reverse: a table-level GRANT supersedes column-level ones,
-- but leaving them behind would make the restored state differ from 00006's in the
-- catalog even though has_column_privilege agrees.
REVOKE UPDATE ON admin_users FROM tappa_app;
GRANT UPDATE ON admin_users TO tappa_app;
REVOKE INSERT ON admin_users FROM tappa_app;
GRANT INSERT ON admin_users TO tappa_app;

-- A column-level REVOKE removes exactly that column's privilege and leaves the rest
-- of the column-level grant intact (verified after running this Down:
-- has_column_privilege stays true for id/tenant_id/email/password_hash/status).
REVOKE SELECT (created_at) ON admin_users FROM tappa_resolver;
REVOKE INSERT (vat_verified, vat_checked_at) ON tenants FROM tappa_app;

ALTER TABLE tenants
    DROP COLUMN vat_checked_at,
    DROP COLUMN vat_verified;
