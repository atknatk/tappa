-- 00115 -- M16-23 (ADR 0026 "Bedel 3"'s withdrawal; ADR 0045 §2.5): activation_requests -- a phone
-- that cannot tap a plaque asks a manager to approve it.
--
-- NORMATIVE SOURCES: docs/plan/m16-mobil.md M16-23 (the contract: a pending request with a pairing
-- code shown on the phone and in the panel; a manager approves or rejects it; the decision is in
-- audit_log; the phone collects its session or device on its next status read; the request is
-- time-bound; approval and rejection are one way); docs/adr/0026-aktivasyon-ilk-nfc-dokunusunda.md
-- (the activation's two steps: a consent bound to one browser or one app key, then the proof that
-- completes it); docs/adr/0028-dis-api-anahtar-akis-webhook.md §1 (the `arwd` rule: REVOKE ALL from
-- tappa_app, then the verbs and the columns it needs; UPDATE always by column; a trigger function
-- EXECUTE in its owner alone, asserting TG_RELID); 00111 (the one-way state trigger this file copies).
--
-- ONE ROW IS ONE REQUEST. The phone -- a browser holding a consented activation, or the app holding
-- the key its bind recorded -- opens a request on its invitation: the invitation, the employee, the
-- CONSENT BINDING the request belongs to (binding_hash: the invitation's consent_binding_hash at
-- the moment of the request, so only the browser or key that asked can collect the approval), a
-- six-digit pairing code and an expiry. The manager sees the code in the panel and the person shows
-- it on the phone; the manager approves only the phone in front of them whose code matches. The
-- code is NOT a secret that grants anything -- the approval is the manager's press -- it is the
-- manager's proof that the request in the list is the phone they are looking at. It is still kept
-- out of every log line and audit detail (§7, the card).
--   * The lifetime is bounded by the DATABASE clock: created_at is not writable by tappa_app and
--     expires_at is at most one hour after it (the ceiling; internal/invite picks the actual TTL,
--     shorter). A decision and a collection happen before the expiry (CHECKs on the row's own
--     values -- no now() in a CHECK, which would refuse a restore).
--   * Four states and nothing between: OPEN (no decision, not withdrawn), WITHDRAWN (a newer request
--     of the same invitation replaced it, undecided), APPROVED (and then COLLECTED once, when the
--     phone took its session or device), REJECTED. The one-way trigger below holds the history:
--     a decision is made once and never changes, a withdrawal and a collection are stamped once, and
--     every stamp is the moment it happens.
--   * One open request per invitation (the partial UNIQUE): opening a new one withdraws the old in
--     the same transaction (db/queries/activationrequests.sql).
--   * tappa_app never DELETEs a request: the trail of who approved whose phone stays (audit_log has
--     the decision too; this row is what the phone collects against).
--   * No device probe and nothing of the person's body is read (§4.1): the client is the closed pair browser / app.
--
-- WHY NO RESOLVER. Every request is reached from a tenant already known: the phone's invitation
-- resolves its tenant (resolve_invite_by_code_hash, 00009), the panel's session names its own. So
-- every statement runs under SET LOCAL app.tenant_id with an explicit tenant_id filter (§4.5).
--
-- 🔴 lock_timeout 1 s, the FIRST statement of the Up and of the Down (00054's rule): the new keys
-- to tenants, employees, employee_invites and admin_users take SHARE ROW EXCLUSIVE on each (the FK
-- triggers), which a write to them waits behind. Behind a long writer the file gives up with 55P03
-- instead of queueing it.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.activation_requests') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc p WHERE p.proname = 'activation_requests_state_is_one_way') THEN
        RAISE EXCEPTION 'migration 00115 (M16-23) creates activation_requests and its trigger function, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF pg_catalog.to_regclass('public.live_qr_displays') IS NULL THEN
        RAISE EXCEPTION 'migration 00115 (M16-23) follows the live QR screens (00111-00114): the manager approval is offered only where no screen runs. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

CREATE TABLE activation_requests (
    id           uuid        NOT NULL DEFAULT gen_random_uuid(),
    tenant_id    uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    employee_id  uuid        NOT NULL,
    invite_id    uuid        NOT NULL REFERENCES employee_invites (id) ON DELETE RESTRICT,
    client       text        NOT NULL,
    binding_hash text        NOT NULL,
    pairing_code text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    decision     text,
    decided_at   timestamptz,
    decided_by   uuid,
    withdrawn_at timestamptz,
    collected_at timestamptz,
    CONSTRAINT activation_requests_pkey PRIMARY KEY (id),
    -- The request's person is its own tenant's (a request cannot name another tenant's employee).
    CONSTRAINT activation_requests_employee_fk FOREIGN KEY (employee_id, tenant_id)
        REFERENCES employees (id, tenant_id) ON DELETE RESTRICT,
    -- The deciding panel account is its own tenant's (a manager of another business cannot decide).
    CONSTRAINT activation_requests_decided_by_fk FOREIGN KEY (decided_by, tenant_id)
        REFERENCES admin_users (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT activation_requests_client_known CHECK (client IN ('browser', 'app')),
    CONSTRAINT activation_requests_binding_hash_shape CHECK (binding_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT activation_requests_pairing_code_shape CHECK (pairing_code ~ '^[0-9]{6}$'),
    CONSTRAINT activation_requests_lifetime CHECK (
        expires_at > created_at AND expires_at <= created_at + interval '1 hour'),
    -- A decision is the three of them or none of them.
    CONSTRAINT activation_requests_decision_known CHECK (decision IN ('approved', 'rejected')),
    CONSTRAINT activation_requests_decision_whole CHECK (
        (decision IS NULL AND decided_at IS NULL AND decided_by IS NULL)
        OR (decision IS NOT NULL AND decided_at IS NOT NULL AND decided_by IS NOT NULL)),
    CONSTRAINT activation_requests_decided_while_open CHECK (
        decided_at IS NULL OR (decided_at >= created_at AND decided_at < expires_at)),
    -- Withdrawn means replaced before anybody decided.
    CONSTRAINT activation_requests_withdrawn_undecided CHECK (withdrawn_at IS NULL OR decision IS NULL),
    CONSTRAINT activation_requests_withdrawn_after_created CHECK (withdrawn_at IS NULL OR withdrawn_at >= created_at),
    -- Only an approval is collected, once, after it was given and before the request ran out.
    -- IS NOT DISTINCT FROM, not =: on an undecided row `decision = 'approved'` is NULL, and a CHECK
    -- that evaluates to NULL passes -- an open request could be stamped collected (measured,
    -- TestActivationRequests_Checks).
    CONSTRAINT activation_requests_collected_approved CHECK (
        collected_at IS NULL OR (decision IS NOT DISTINCT FROM 'approved' AND collected_at >= decided_at AND collected_at < expires_at))
);

-- tenant_id leads (CLAUDE.md §6): RLS, and the panel's list of open requests by expiry.
CREATE INDEX activation_requests_tenant_idx ON activation_requests (tenant_id, expires_at);
-- The phone's read of its own request, and the invitation key's RESTRICT check.
CREATE INDEX activation_requests_invite_idx ON activation_requests (tenant_id, invite_id, created_at);
-- One open request per invitation: a second one is opened only after the first was withdrawn (the
-- same transaction), and two racing opens leave one (23505 for the other).
CREATE UNIQUE INDEX activation_requests_one_open_key ON activation_requests (invite_id)
    WHERE decision IS NULL AND withdrawn_at IS NULL;

ALTER TABLE activation_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE activation_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY activation_requests_tenant_isolation ON activation_requests
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- --- the one-way state (00111's shape) --------------------------------------------------------------
-- A request is BORN OPEN, for an invitation of its own tenant and employee. A decision is made once and
-- never changes, only on an open request, and is stamped when it happens; a withdrawal is stamped once,
-- only on an undecided request; a collection is stamped once, only on an approval. The CHECKs hold
-- the shape of a row, this holds its history -- tappa_app holds UPDATE on exactly these five columns.
-- The time tests are here, not in a CHECK: a CHECK that read now() would refuse a restore.
-- +goose StatementBegin
CREATE FUNCTION public.activation_requests_state_is_one_way()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP NOT IN ('INSERT', 'UPDATE')
       OR TG_RELID <> 'public.activation_requests'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'activation_requests_state_is_one_way: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.decision IS NOT NULL OR NEW.decided_at IS NOT NULL OR NEW.decided_by IS NOT NULL
           OR NEW.withdrawn_at IS NOT NULL OR NEW.collected_at IS NOT NULL THEN
            RAISE EXCEPTION 'an activation request is born open: no decision, not withdrawn, not collected'
                USING ERRCODE = 'restrict_violation';
        END IF;
        -- The invitation is the request's own tenant's and employee's (the invitation key alone
        -- names a row, not its owner).
        IF NOT EXISTS (SELECT 1 FROM public.employee_invites i
                        WHERE i.id = NEW.invite_id AND i.tenant_id = NEW.tenant_id AND i.employee_id = NEW.employee_id) THEN
            RAISE EXCEPTION 'an activation request names an invitation of its own tenant and employee'
                USING ERRCODE = 'foreign_key_violation';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.decision IS DISTINCT FROM OLD.decision OR NEW.decided_at IS DISTINCT FROM OLD.decided_at
       OR NEW.decided_by IS DISTINCT FROM OLD.decided_by THEN
        IF OLD.decision IS NOT NULL THEN
            RAISE EXCEPTION 'an activation request is decided once: its approval or rejection never changes'
                USING ERRCODE = 'restrict_violation';
        END IF;
        IF OLD.withdrawn_at IS NOT NULL THEN
            RAISE EXCEPTION 'a withdrawn activation request is not decided; decide the request that replaced it'
                USING ERRCODE = 'restrict_violation';
        END IF;
        IF NEW.decided_at < pg_catalog.now() OR NEW.decided_at > pg_catalog.clock_timestamp() THEN
            RAISE EXCEPTION 'an activation request''s decision is stamped when it happens: decided_at may not be back-dated or set in the future'
                USING ERRCODE = 'restrict_violation';
        END IF;
    END IF;
    IF NEW.withdrawn_at IS DISTINCT FROM OLD.withdrawn_at THEN
        IF OLD.withdrawn_at IS NOT NULL THEN
            RAISE EXCEPTION 'an activation request is withdrawn once'
                USING ERRCODE = 'restrict_violation';
        END IF;
        IF NEW.withdrawn_at < pg_catalog.now() OR NEW.withdrawn_at > pg_catalog.clock_timestamp() THEN
            RAISE EXCEPTION 'an activation request''s withdrawal is stamped when it happens'
                USING ERRCODE = 'restrict_violation';
        END IF;
    END IF;
    IF NEW.collected_at IS DISTINCT FROM OLD.collected_at THEN
        IF OLD.collected_at IS NOT NULL THEN
            RAISE EXCEPTION 'an approved activation request is collected once'
                USING ERRCODE = 'restrict_violation';
        END IF;
        IF NEW.collected_at < pg_catalog.now() OR NEW.collected_at > pg_catalog.clock_timestamp() THEN
            RAISE EXCEPTION 'an activation request''s collection is stamped when it happens'
                USING ERRCODE = 'restrict_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.activation_requests_state_is_one_way() FROM PUBLIC;

CREATE TRIGGER activation_requests_born_open
    BEFORE INSERT ON public.activation_requests
    FOR EACH ROW
    EXECUTE FUNCTION public.activation_requests_state_is_one_way();

CREATE TRIGGER activation_requests_state_one_way
    BEFORE UPDATE ON public.activation_requests
    FOR EACH ROW
    WHEN (NEW.decision IS DISTINCT FROM OLD.decision
          OR NEW.decided_at IS DISTINCT FROM OLD.decided_at
          OR NEW.decided_by IS DISTINCT FROM OLD.decided_by
          OR NEW.withdrawn_at IS DISTINCT FROM OLD.withdrawn_at
          OR NEW.collected_at IS DISTINCT FROM OLD.collected_at)
    EXECUTE FUNCTION public.activation_requests_state_is_one_way();

-- --- tappa_app (ADR 0028 §1's `arwd` rule) --------------------------------------------------------
-- Reads every column: binding_hash because the phone's read and the collection's WHERE name it (no
-- query RETURNS it), pairing_code because the panel and the phone show it. Inserts a request by
-- naming its tenant, employee, invitation, client, binding, code and expiry (id and created_at are
-- the database's -- the lifetime CHECK is bounded by the database's clock). Updates the five state
-- columns alone (the trigger holds their history). Never deletes.
REVOKE ALL ON activation_requests FROM tappa_app;
GRANT SELECT (id, tenant_id, employee_id, invite_id, client, binding_hash, pairing_code, created_at, expires_at,
              decision, decided_at, decided_by, withdrawn_at, collected_at)
    ON activation_requests TO tappa_app;
GRANT INSERT (tenant_id, employee_id, invite_id, client, binding_hash, pairing_code, expires_at)
    ON activation_requests TO tappa_app;
GRANT UPDATE (decision, decided_at, decided_by, withdrawn_at, collected_at) ON activation_requests TO tappa_app;

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_table_privilege('tappa_app', 'public.activation_requests', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.activation_requests', 'TRUNCATE')
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.activation_requests'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                     AND a.attname NOT IN ('decision', 'decided_at', 'decided_by', 'withdrawn_at', 'collected_at')
                     AND pg_catalog.has_column_privilege('tappa_app', a.attrelid, a.attnum, 'UPDATE'))
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.activation_requests'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                     AND a.attname NOT IN ('tenant_id', 'employee_id', 'invite_id', 'client', 'binding_hash', 'pairing_code', 'expires_at')
                     AND pg_catalog.has_column_privilege('tappa_app', a.attrelid, a.attnum, 'INSERT'))
       OR pg_catalog.has_function_privilege('tappa_app', 'public.activation_requests_state_is_one_way()', 'EXECUTE')
       OR pg_catalog.has_table_privilege('tappa_resolver', 'public.activation_requests', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00115 (M16-23): tappa_app must not delete or truncate a request, update anything but the five state columns, write id, created_at or a state column at INSERT, or run the trigger function; tappa_resolver reads nothing of it -- through PUBLIC or a role membership one of these does not hold. Fix it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- 🔴 A DOWN THAT WOULD ERASE A REQUEST REFUSES (the 00068 / 00108 / 00111 precedent). A request row
-- is which phone asked, which manager approved or rejected it and when, and which approval the phone
-- collected -- what an activation without a plaque tap stands on -- so while the table holds a row
-- this Down stops and changes nothing; on a database with none it takes the table and its function
-- away. After the first request the way back is the binary, not the schema: the previous binary
-- neither reads nor writes this table.
--
-- The table is LOCKED BEFORE IT IS LOOKED AT: between an unlocked EXISTS and the DROP a request could
-- commit and the DROP would erase it. The DROP also takes ACCESS EXCLUSIVE on the four referenced
-- tables (the keys' triggers there), so all five are taken HERE, in one statement, child first: the
-- Down either holds every lock it will need before it reads or changes anything, or gives up with
-- 55P03 having done nothing.
SET LOCAL lock_timeout = '1s';

LOCK TABLE public.activation_requests, public.admin_users, public.employee_invites, public.employees, public.tenants
    IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.activation_requests) THEN
        RAISE EXCEPTION 'migration 00115 Down: activation_requests holds rows (a phone that asked a manager to approve it); dropping them would erase who approved whose phone. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

DROP TABLE public.activation_requests;
DROP FUNCTION public.activation_requests_state_is_one_way();
