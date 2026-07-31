-- +goose Up

-- Migration 0009 -- employee_invites: the DATA half of M5-02 (davet ve aktivasyon
-- akisi). One row per invitation handed to an employee; consuming that row is what
-- turns an 'invited' employee into an 'active' one holding a session cookie (M5-01).
--
-- THIS MIGRATION IS PHASE A (data layer) ONLY. The HTTP endpoint (POST
-- /api/activate), the activation page, the GDPR Art. 13 notice and the WiFi step
-- are phase B and touch NO schema; if phase B needs a column it writes a NEW
-- migration (an applied migration is immutable -- CLAUDE.md section 6).
--
-- KIRMIZI CIZGILER:
--   section 4.7  The invite CODE is not stored or returned -- only its hash. The
--                column is named code_hash to say so, the CHECK below refuses
--                anything that is not hash-SHAPED, no query in db/queries returns
--                the column (greppable: it appears in no SELECT or RETURNING
--                list), and the resolution function below takes the hash as its
--                ONLY input. WHAT THE SCHEMA CANNOT SEE: provenance. A CHECK can
--                enforce the SHAPE of a value, never where it came from -- a
--                caller that used a 64-lowercase-hex string as the code ITSELF
--                would satisfy it. That the stored value is an HMAC of the code
--                under a server-side key is phase B's obligation
--                (internal/session's pattern), not something this table can
--                verify. "Not logged" likewise lives in Go, NOT here: section 7
--                forbids it and scripts/redline-check.sh R7 is a MECHANICAL net,
--                not a proof. That net was EXTENDED in M5-02 to cover `code_hash`
--                and a bare `code` argument -- before the change it matched only
--                `invite_?code`, so a structured log call passing the hash under
--                the key "code_hash" passed clean (measured; the literal example
--                lives in the test report, not here, because the scanner reads
--                this file too and would flag its own documentation). This
--                matters because in THIS design the
--                hash is a BEARER credential: hash -> resolver -> tenant ->
--                consumption. Logging the hash is as bad as logging the code.
--                Any other spelling ("hash", "c") still slips past the net.
--   section 4.5  RLS five + explicit GRANT, exactly like every other table.
--   section 4.6  No hard delete: a used or expired invite STAYS as a trail
--                ("this code was consumed at 14:02"). GRANT omits DELETE AND an
--                explicit REVOKE removes the ALTER DEFAULT PRIVILEGES grant
--                (M1-04 lesson: dropping it from the GRANT alone does NOTHING).
--                SCOPE OF THAT GUARANTEE, stated precisely: it binds tappa_app --
--                i.e. the application, i.e. every code path that serves a request.
--                It does NOT bind tappa_owner (the migration superuser), which
--                keeps DELETE and bypasses RLS by definition (measured). Closing
--                that too would need a BEFORE DELETE trigger, the belt 00005 adds
--                over transactions/audit_log; this table cannot reuse 00005's
--                tappa_forbid_mutation() as-is because that one also blocks UPDATE
--                and used_at MUST be updatable. A DELETE-only trigger is a
--                deliberate NON-goal here (00009 mirrors the sessions/employees
--                precedent, which is REVOKE-only for the same reason); if the trail
--                is ever required to survive the owner too, that trigger arrives in
--                a NEW migration.
--
-- CODE ENTROPY IS A SCHEMA ASSUMPTION, WRITTEN DOWN SO PHASE B CAN SEE IT.
-- This table carries NO brute-force lockout state (no attempt counter, no
-- locked_until, no per-IP column) and that is a DECISION, not an omission. It is
-- sound ONLY while the code is >= 128 bits of cryptographic randomness (M5-02
-- card: "ya >= 128 bit, ya da insan okuyacak kisa kod ise KATI KILITLEME"): a
-- 128-bit space cannot be searched online, so the guessing defence is the code
-- itself, not database state. If phase B ever switches to a SHORT human-typed
-- code (the card's 10^6 example), this assumption breaks and the card REQUIRES
-- strict lockout -- which needs a NEW migration adding that state here (attempt
-- count + lock stamp, keyed per invite and per source IP). Endpoint rate limiting
-- and writing failed attempts to audit_log belong to phase B either way; they are
-- not schema and are deliberately absent from this file.
--
-- NOTHING IN THIS SCHEMA CAN DETECT THAT SWITCH -- say it plainly so nobody trusts
-- a tripwire that does not exist. The code_hash CHECK below tests the HASH's shape,
-- and the hash of a 6-digit code is 64 hex characters exactly like the hash of a
-- 256-bit one (measured: encode(sha256('123456'), 'hex') matches the pattern). A
-- phase-B author who picks a short code, HMACs it correctly and writes it here gets
-- a green database. The obligation above is therefore carried by the M5-02 card's
-- acceptance criteria and by review -- not by any constraint in this file.
CREATE TABLE employee_invites (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- tenant_id: her tabloda zorunlu kapsam anahtari (section 4.5).
    -- ON DELETE RESTRICT: davet kaydi varken tenant silinemez (audit izi).
    tenant_id   uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    -- employee_id NOT NULL: bir davet DAIMA belirli bir calisan icindir -- the code
    -- names WHO it activates, which is why consuming it can activate exactly that
    -- employee and nobody else. Bilesik same-tenant FK (both columns NOT NULL ->
    -- DAIMA denetlenir) capraz-tenant baglanmayi YAPISAL olarak engeller: A
    -- tenant'inin daveti B tenant'inin calisanina baglanamaz (M1-04/M1-11 kalibi).
    employee_id uuid NOT NULL,
    -- code_hash: the HMAC of the invite code (phase B computes it with the
    -- internal/session HMAC pattern). KIRMIZI CIZGI section 4.7: the CODE itself
    -- is not written here and no query returns this column.
    --
    -- THE CHECK MAKES THAT CLAIM STRUCTURAL INSTEAD OF ASPIRATIONAL -- as far as a
    -- CHECK can reach. A bare `text` column accepts a raw code just as happily as
    -- a hash, so the sentence above would have been a comment guarding nothing.
    -- `^[0-9a-f]{64}$` is exactly what internal/session produces today (token.go:
    -- hex.EncodeToString of an HMAC-SHA256, 64 lowercase hex chars -- the same
    -- shape M5-01 asserts for sessions.token_hash), so every plausible invite code
    -- -- any human-typed string, any base64url token (the UNHASHED token shape),
    -- any uppercase hex, any wrong length -- is REJECTED by the database (measured:
    -- 23514, TestEmployeeInvites_CodeHashMustBeAHash).
    --
    -- THE LIMIT: this is a SHAPE test, not a provenance test. A 64-lowercase-hex
    -- string that happens to BE the code would pass, because no constraint can ask
    -- "is this the HMAC of something?". Deriving the value with the server key is
    -- phase B's obligation; the CHECK removes the accidental and the careless case,
    -- not a determined one.
    --
    -- DELIBERATE DEVIATION FROM PRECEDENT (agent-brief md 8): sessions.token_hash
    -- (00003) and password_resets.token_hash (00006) carry no such CHECK. This
    -- column takes one anyway because THIS file states the guarantee out loud; an
    -- absolute claim in a comment must be enforced by something. The deviation is
    -- additive (it constrains only this table) and costs a new migration if the
    -- KDF/encoding ever changes -- an acceptable price for a red-line claim that
    -- the schema can actually keep.
    --
    -- GLOBAL UNIQUE (tenant kapsamli DEGIL) -- deliberately the sessions.token_hash
    -- shape, for the SAME structural reason: the resolution path below runs WITHOUT
    -- a tenant context, because the tenant is the RESULT of the lookup, not an
    -- input (an activation link carries only a code). Global UNIQUE is what makes
    -- that context-less lookup return AT MOST ONE row a STRUCTURAL property of the
    -- index, not a property of the function body. The code's randomness (>= 128
    -- bit, see above) makes a cross-tenant collision cryptographically unreachable.
    code_hash   text NOT NULL UNIQUE
                    CONSTRAINT employee_invites_code_hash_shape
                    CHECK (code_hash ~ '^[0-9a-f]{64}$'),
    -- created_at: when the invite was ISSUED (distinct from expires_at, "when it
    -- dies", and used_at, "when it was consumed").
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- expires_at NOT NULL: an invite with no expiry is a security hole -- the link
    -- is photographed, forwarded or left in an inbox and stays usable forever
    -- (password_resets, 00006, took the same NOT NULL for the same reason).
    --
    -- TWO WAYS TO SPELL "NEVER EXPIRES", AND BOTH ARE BLOCKED -- measured, not
    -- assumed. NOT NULL blocks the omitted expiry. It does NOT block the LITERAL
    -- one: `expires_at = 'infinity'` was accepted by an earlier draft of this
    -- table and `now() < 'infinity'` is true forever, i.e. a genuinely immortal
    -- invite. The CHECK below closes it.
    --
    -- WHAT IS STILL REPRESENTABLE (the honest limit): a FINITE but absurd expiry,
    -- e.g. year 9999. That is a TTL policy question, not a "no expiry" one, and
    -- the TTL is deliberately a phase-B product decision -- so the schema does not
    -- pick a bound here. If phase B wants a maximum TTL enforced structurally it
    -- adds a bounded CHECK in a NEW migration. '-infinity' is left representable
    -- on purpose: it means ALREADY expired, which is fail-closed and harmless.
    expires_at  timestamptz NOT NULL
                    CONSTRAINT employee_invites_expiry_finite
                    CHECK (expires_at <> 'infinity'::timestamptz),
    -- used_at: TEK KULLANIMLIK damgasi. NULL = not yet consumed. No application
    -- code path can delete the row (section 4.6; REVOKE DELETE below, and see the
    -- scope note in the header -- the owner still can): a consumed invite is the
    -- audit answer to "when was this activation code used, and for whom".
    --
    -- FILTERED BY THE CONSUMING QUERY, NOT BY THE RESOLVER. The resolution
    -- function below RETURNS used_at and expires_at but does NOT filter on them --
    -- the same principle 00003 states for sessions.revoked_at: the data layer
    -- carries the truth and the DOMAIN decides. A resolver that hid a consumed
    -- invite would report "unknown code" for a code that really exists, and the
    -- caller could not tell a replay from a typo -- exactly the shape that loses a
    -- record (section 4.6). The ATOMIC single-use enforcement is the ConsumeInviteAndActivate
    -- UPDATE by CODE HASH (db/queries/invites.sql: ConsumeInviteAndActivate), which
    -- is the only place used_at is ever written.
    used_at     timestamptz,
    CONSTRAINT employee_invites_employee_fk
        FOREIGN KEY (employee_id, tenant_id)
        REFERENCES employees (id, tenant_id) ON DELETE RESTRICT
);

-- tenant_id ONDE olan indeks (section 6 / R5). employee_id ikinci sutun: "bir
-- calisanin bekleyen davetleri" listelemesini (ListPendingInvitesForEmployee) ve
-- employees silmedeki bilesik FK RESTRICT kontrolunu de karsilar. The code_hash
-- UNIQUE index is NOT tenant_id-first, so it does NOT satisfy the R5 element --
-- this separate index is ZORUNLU (same note as 00003 sessions / 00006).
CREATE INDEX employee_invites_tenant_idx ON employee_invites (tenant_id, employee_id);

ALTER TABLE employee_invites ENABLE ROW LEVEL SECURITY;
-- FORCE: tablo sahibi bile politikaya tabi olsun (superuser haric -- M0-03).
ALTER TABLE employee_invites FORCE ROW LEVEL SECURITY;

-- STANDART NULLIF politikasi -- cozumleme OR-dali YOK (00003 sessions / 00004 tags
-- ile birebir ayni). The GUC-keyed pure-RLS alternative was rejected in ADR 0002
-- madde 7 precisely because it adds an additive OR branch: a resolve-GUC set
-- without SET LOCAL stays on the pooled connection and leaks a cross-tenant row
-- into every legitimate tenant query on it. Resolution goes through the bounded
-- SECURITY DEFINER function below, never through the policy. Ifade ADR 0002 madde
-- 3 / Q27 uyarinca birebir: baglam yokken GUC ya NULL (hic yazilmamis) ya '' (bir
-- kez yazilip tx bitmis); NULLIF ikisini de NULL'a cevirir -> hicbir satir
-- eslesmez (fail-closed). Ciplak ::uuid cast YASAK: bos dize uzerinde hata
-- firlatir, davranis baglantiya baglanir.
CREATE POLICY employee_invites_tenant_isolation ON employee_invites
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- DELETE bilincli olarak VERILMEZ (section 4.6): bir davet silinmez -- it is
-- consumed (used_at) or it simply expires, and either way the row remains as the
-- audit answer to "which code activated this person, and when".
--
-- UPDATE IS COLUMN-LEVEL, ON used_at ALONE -- a DELIBERATE DEVIATION from the
-- table-wide UPDATE that 00003 (sessions) and 00004 (tags) grant (agent-brief md 8).
-- Gerekce: those tables have SEVERAL legitimately writable columns (last_used_at
-- and revoked_at; last_ctr, status, retired_at and replaced_by), so a column grant
-- there would only enumerate the whole table. THIS table has exactly ONE writable
-- column. A table-wide UPDATE would additionally let the application:
--   * RESURRECT an expired invite (SET expires_at = now() + '10 years') -- the
--     expiry guarantee this file spends a paragraph on, undone by one statement;
--   * REASSIGN a live invite to a different employee of the same tenant (SET
--     employee_id = ...) -- a code mailed to one person activating another;
--   * UN-CONSUME a used invite (SET used_at = NULL).
-- Only the third was named in db/queries/invites.sql; the first two were possible
-- and unmentioned, which is exactly how a guarantee rots. The column grant closes
-- all three writes at the PRIVILEGE level, i.e. structurally, instead of relying on
-- "no query in this file does it" (measured: consumption still works, the other
-- writes get permission denied).
--
-- THE LIMIT that remains: a column grant constrains WHICH column may be written,
-- never WHAT value. `UPDATE ... SET used_at = NULL` and `SET used_at = <some other
-- instant>` are still permitted by the privilege system; what forbids them is that
-- no query in db/queries does it (the file states this as its own discipline).
-- Making the VALUE transition (NULL -> now(), never back) structural needs a
-- trigger in a NEW migration.
--
-- DIKKAT (M1-04 dersi): db-init'teki ALTER DEFAULT PRIVILEGES her yeni tabloda
-- tappa_app'e SELECT/INSERT/UPDATE/DELETE verir; bir yetkiyi GRANT'tan CIKARMAK
-- TEK BASINA YETMEZ -- acikca REVOKE edilmelidir. Bu yuzden once table-wide UPDATE
-- ve DELETE geri alinir, sonra yalnizca gereken verilir (empirically verified with
-- has_table_privilege / has_column_privilege in internal/db/invites_test.go).
REVOKE UPDATE, DELETE ON employee_invites FROM tappa_app;
GRANT SELECT, INSERT ON employee_invites TO tappa_app;
GRANT UPDATE (used_at) ON employee_invites TO tappa_app;


-- --- Tenant cozumleme mekanizmasi (ADR 0002 madde 7 -- YAPISAL cevreleme) ------
-- An activation request arrives with ONLY the code from the link; tenant bilinmez
-- cunku tenant bu aramanin SONUCUdur -- the third lookup of that kind, after
-- resolve_tag_by_uid (00004) and resolve_session_by_token_hash (00003). Cozumleme,
-- RLS'e tabi tappa_app'in goremeyecegi (FORCE + baglamsiz = 0 satir) bir satiri
-- okumak zorundadir. Guvenlik RLS'ten degil ARAYUZDEN gelir: dar, anahtar-
-- parametreli, <=1 satir donduren, SELECT * yuzeyi olmayan bir SECURITY DEFINER
-- fonksiyon. Patlama yaricapi tappa_resolver'a verilen GRANT'larla bu tek tabloya
-- (hatta bu sutunlara) sinirli -- tum DB degil.

-- Sutun-duzeyi SELECT: tappa_resolver yalnizca cozumleme icin gereken sutunlari
-- gorur -- created_at is NOT among them. db-init'te tappa_resolver HICBIR default
-- privilege almadigindan baska hicbir tabloyu da goremez; blast radius yapisal
-- olarak bu GRANT'la sinirli.
GRANT SELECT (id, tenant_id, employee_id, code_hash, expires_at, used_at)
    ON employee_invites TO tappa_resolver;

-- +goose StatementBegin
CREATE FUNCTION resolve_invite_by_code_hash(p_code_hash text)
    RETURNS TABLE (
        id          uuid,
        tenant_id   uuid,
        employee_id uuid,
        expires_at  timestamptz,
        used_at     timestamptz
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    -- KRITIK (search_path injection): search_path sabitlenir. public YOLDA DEGIL,
    -- bu yuzden tablo public.employee_invites olarak ACIKCA nitelenir -- aksi
    -- halde ya fonksiyon tabloyu bulamaz ya da bir saldirgan kendi
    -- 'employee_invites'ini araya sokabilirdi. pg_temp daima sona yazilir
    -- (Postgres onerisi).
    SET search_path = pg_catalog, pg_temp
    AS $$
        -- <=1 satir: code_hash GLOBAL UNIQUE. Sabit sutun listesi, SELECT * YOK.
        -- code_hash itself is the INPUT and is deliberately NOT among the returned
        -- columns: nothing outside this lookup ever needs it back (section 4.7).
        -- created_at (ihtiyactan fazla) DONDURULMEZ. expires_at ve used_at
        -- dondurulur ama FILTRELENMEZ -- tuketim/sure karari domain katmanindadir
        -- ve ATOMIK tuketim ayri bir UPDATE ifadesidir (ConsumeInviteAndActivate); buradan
        -- okunan used_at bir oku-sonra-yaz karsilastirmasinda KULLANILMAZ
        -- (section 4.4 TOCTOU, tags.last_ctr ile ayni tuzak).
        SELECT i.id, i.tenant_id, i.employee_id, i.expires_at, i.used_at
        FROM public.employee_invites AS i
        WHERE i.code_hash = p_code_hash;
    $$;
-- +goose StatementEnd

-- EN-AZ-AYRICALIK: fonksiyon sahibi tappa_resolver (NOLOGIN, BYPASSRLS,
-- NOSUPERUSER). SECURITY DEFINER govdesi SAHIBININ yetkisiyle kosar; sahip
-- superuser (tappa_owner) OLSAYDI govde tum DB'de RLS'i atlar, patlama yaricapi
-- sinirsiz olurdu -- ADR 0002 madde 7'nin ACIKCA yasakladigi genel bypass.
ALTER FUNCTION resolve_invite_by_code_hash(text) OWNER TO tappa_resolver;

-- KRITIK: fonksiyonlar varsayilan olarak PUBLIC EXECUTE alir. Once PUBLIC'ten
-- tumuyle geri alinir, sonra yalnizca tek mesru cagirana (tappa_app) verilir.
REVOKE ALL ON FUNCTION resolve_invite_by_code_hash(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION resolve_invite_by_code_hash(text) TO tappa_app;

-- +goose Down

-- Bagimlilik sirasi: once fonksiyon (public.employee_invites'a atifta bulunur),
-- sonra tablo. DROP TABLE tabloya bagli politika, RLS, GRANT (tappa_app ve
-- tappa_resolver sutun grant'i dahil), UNIQUE, indeks ve bilesik FK'yi birlikte
-- duser. tappa_resolver ROLU db-init'e aittir ve BURADA DUSURULMEZ. employees
-- tablosuna dokunulmaz: bu migration ona hicbir sutun/kisit eklemedi.
DROP FUNCTION IF EXISTS resolve_invite_by_code_hash(text);
DROP TABLE employee_invites;
