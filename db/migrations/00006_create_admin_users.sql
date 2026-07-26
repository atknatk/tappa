-- +goose Up

-- Migration 0006 -- Panel identity. Three tables for the person who logs into the
-- admin dashboard (M6/M7), kept STRICTLY separate from the employee identity graph
-- (employees/sessions, 00003):
--   admin_users     : an owner/manager who signs into the panel with email+password.
--   admin_sessions  : that person's panel cookie (token_hash stored, token never).
--   password_resets : single-use password-reset tokens (token_hash stored, never).
--
-- WHY A SEPARATE IDENTITY (M1-11 tuzagi): an employee NEVER sees a password -- the
-- product promise is device-less, punchless. Bolting an admin flag onto employees
-- would fuse two kinds of identity with two session lifetimes and two threat models
-- (M1-11 card). So admin auth lives in its own tables; an employee cookie can never
-- reach the panel and an admin cookie can never reach the tap flow -- they resolve
-- from DIFFERENT tables (M1-11: "admin_sessions calisan sessions'tan AYRI").
--
-- WHY NO RESOLVER (differs from 00003 sessions / 00004 tags): a tap arrives with
-- only a tag UID and a cookie -- NEITHER carries a tenant, so tenant is the RESULT
-- of an unbounded lookup and needs the SECURITY DEFINER bounded-bypass of ADR 0002
-- madde 7. Admin login is different: the panel is reached with the tenant ALREADY
-- known (subdomain/URL carries it), so the tenant context is established at login
-- time and admin_sessions is read INSIDE that context. Email is unique PER TENANT
-- (not global), so an admin lookup is always tenant-scoped. Therefore all three
-- tables take STANDARD NULLIF RLS -- no context-less resolver function is created.
-- (If M6-01 ever needs a context-less admin-session resolution it is added THERE,
-- as a bounded SECURITY DEFINER surface per ADR 0002 madde 7 -- NOT in M1-11.)
--
-- KDF-AGNOSTIC (Q03): password_hash is `text NOT NULL`; it holds a bcrypt `$2a$...`
-- string (Q03 decision, 2026-07-26). No x/crypto dependency is added by this
-- migration -- the column is a plain text blob that fits any KDF output. The hashing
-- code (and the x/crypto dep) arrives with M6-01 (auth) and M7-04 (reset).
--
-- KIRMIZI CIZGILER:
--   section 4.5  Tenant izolasyonu: every table gets the RLS five + explicit GRANT.
--   section 4.7  A cookie/reset TOKEN is never stored -- only its hash. The column
--                name (token_hash) says so; UNIQUE lets a lookup be a single query.
--   section 4.6  No hard delete on any of the three (audit trail): admin_users
--                soft-disables (status), admin_sessions revoke (revoked_at),
--                password_resets mark-used (used_at). GRANT omits DELETE AND an
--                explicit REVOKE DELETE removes the ALTER DEFAULT PRIVILEGES grant.


-- --- admin_users -------------------------------------------------------------
-- The panel operator. Seeded owner per tenant (M1-10 seed); managers created by an
-- owner in M6. Authorization decisions read `role` via the policy engine
-- (actor:role context key, sys:policy-edit-owner-only guardrail -- M3), not here.
CREATE TABLE admin_users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- tenant_id: her tabloda zorunlu kapsam anahtari (section 4.5).
    -- ON DELETE RESTRICT: admin varken tenant silinemez (audit izi korunur).
    tenant_id     uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    full_name     text NOT NULL,
    -- email citext: buyuk/kucuk harf farkiyla cift kayit olmasin. NULLABLE +
    -- kismi UNIQUE (employees kalibi): tekillik TENANT ICINDE ve yalnizca dolu
    -- e-postalarda. Pratikte bir admin DAIMA e-postayla yaratilir (panel girisi ve
    -- sifirlama ona baglidir) -- nullable, employees ile ayni bicimde, "e-posta
    -- zorunlulugu" kurali uygulama katmanina (M6-01) birakilir (repo ethos'u:
    -- format/varlik dogrulamasi app katmaninda, DB'de degil).
    email         citext,
    -- password_hash: Q03 -> bcrypt `$2a$...`. KDF-AGNOSTIK text; x/crypto BURADA
    -- EKLENMEZ (migration KDF'e bakmaz). NOT NULL: parolasiz admin girisi anlamsiz.
    password_hash text NOT NULL,
    -- role: KAPALI sozluk. Yetki karari policy motorundadir (actor:role); bu CHECK
    -- yalnizca gecerli bir rol degeri garanti eder. owner = tenant sahibi (tam
    -- yetki), manager = kisitli. NOT NULL: rolsuz admin anlamsiz.
    role          text NOT NULL CHECK (role IN ('owner', 'manager')),
    -- status: admin yasam dongusu. KARAR+GEREKCE (active|disabled): silme YOK
    -- (section 4.6 audit izi) -> devre disi birakma bir DURUM alanidir, hard-delete
    -- degil. employees'in 'invited' durumu BURADA YOK: owner seed'le, manager
    -- owner tarafindan dogrudan yaratilir; davet yasam dongusu (varsa) M6'da bir
    -- durum degil, ayri bir akistir. DEFAULT 'active'.
    status        text NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active', 'disabled')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    -- last_login_at: basarili girisde guncellenir (RLS-bagli UPDATE). Nullable
    -- (henuz giris yapmamis seed owner icin NULL).
    last_login_at timestamptz,

    -- Bilesik same-tenant FK'ler icin ZORUNLU UNIQUE: admin_sessions ve
    -- password_resets (admin_user_id, tenant_id) -> admin_users (id, tenant_id)
    -- referansi verir; bilesik FK, hedef sutunlar uzerinde UNIQUE/PK ISTER. id
    -- zaten global PK oldugundan (id, tenant_id) trivial tekildir; kisit yalnizca
    -- capraz-tenant baglanmayi YAPISAL engelleyen bilesik FK'yi mumkun kilar
    -- (M1-03/04/05 kalibi).
    CONSTRAINT admin_users_id_tenant_key UNIQUE (id, tenant_id)
);

-- tenant_id ONDE indeks (section 6 / R5): RLS ve tenant kapsamli listeleme.
CREATE INDEX admin_users_tenant_idx ON admin_users (tenant_id);

-- Tenant icinde e-posta tekilligi, yalnizca dolu e-postalarda (kismi indeks).
-- citext + kismi UNIQUE: harf farkiyla dahi ikinci kayit olusmaz; e-postasiz
-- (NULL) kayitlar coklu olabilir. tenant_id ONDE oldugundan bu indeks R5 "tenant_id
-- onde indeks" unsurunu da (yedek olarak) karsilar. Global degil tenant kapsamli:
-- ayni e-posta iki farkli tenant'ta admin olabilir (bu yuzden login tenant-kapsamli).
CREATE UNIQUE INDEX admin_users_tenant_email_key
    ON admin_users (tenant_id, email) WHERE email IS NOT NULL;

ALTER TABLE admin_users ENABLE ROW LEVEL SECURITY;
-- FORCE: tablo sahibi bile politikaya tabi olsun (superuser haric -- M0-03).
ALTER TABLE admin_users FORCE ROW LEVEL SECURITY;

-- Politika ifadesi ADR 0002 madde 3 / Q27 uyarinca NORMATIF ve birebir: baglam
-- yokken GUC ya NULL (hic yazilmamis) ya '' (bir kez yazilip tx bitmis); NULLIF
-- ikisini de NULL'a cevirir -> hicbir satir eslesmez (fail-closed). Ciplak ::uuid
-- cast YASAK: bos dize uzerinde hata firlatir, davranis baglantiya baglanir.
CREATE POLICY admin_users_tenant_isolation ON admin_users
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- DELETE bilincli olarak VERILMEZ (section 4.6): admin silinmez, devre disi
-- birakilir (status='disabled'). DIKKAT (M1-04 dersi): db-init'teki ALTER DEFAULT
-- PRIVILEGES her yeni tabloya tappa_app'e DELETE dahil dort yetki verir; DELETE'i
-- GRANT'tan CIKARMAK TEK BASINA YETMEZ -- acikca REVOKE edilmelidir.
GRANT SELECT, INSERT, UPDATE ON admin_users TO tappa_app;
REVOKE DELETE ON admin_users FROM tappa_app;


-- --- admin_sessions ----------------------------------------------------------
-- The panel cookie. AYRI tablo (employee sessions degil) -- bu tablonun token'i
-- yalnizca panele acilir, tap akisina asla. KIRMIZI CIZGI (section 4.7): token
-- ASLA saklanmaz, yalnizca hash'i.
CREATE TABLE admin_sessions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    -- admin_user_id NOT NULL: oturum daima bir admin'e aittir. Bilesik FK
    -- (admin_user_id, tenant_id) her iki sutun NOT NULL oldugundan DAIMA denetlenir
    -- -> oturumun admin'i ayni tenant'ta olmak zorunda (capraz-tenant engeli).
    admin_user_id uuid NOT NULL,
    -- token_hash: cerezdeki opak token'in hash'i. UNIQUE -> dogrulama tek sorguda.
    -- Employee sessions'taki gibi resolver YOK: admin girisi tenant baglami KURULU
    -- iken (login tenant'i biliyor) yapilir, arama daima RLS altinda kosar. Bu
    -- yuzden token_hash tenant kapsamli olsa da (RLS satiri gizler) UNIQUE global:
    -- rastgele token'lar bu tablo ile employee sessions'ta ASLA carpismaz -- iki
    -- farkli tablo, iki farkli arama yolu (M1-11 "aynı token_hash iki tabloda
    -- cakismaz": ayrim YAPISAL, iki ayri tablodur).
    token_hash    text NOT NULL UNIQUE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    -- last_used_at: her panel isteginde guncellenebilir (RLS-bagli UPDATE).
    last_used_at  timestamptz,
    -- Silme YOK: iptal revoked_at damgasidir (section 4.6; audit izi). NULL = aktif.
    revoked_at    timestamptz,
    CONSTRAINT admin_sessions_admin_user_fk
        FOREIGN KEY (admin_user_id, tenant_id)
        REFERENCES admin_users (id, tenant_id) ON DELETE RESTRICT
);

-- tenant_id ONDE indeks (section 6 / R5). admin_user_id ikinci sutun: "bir admin'in
-- oturumlari" listelemesini ve admin_users silmedeki bilesik FK RESTRICT kontrolunu
-- de karsilar. token_hash UNIQUE indeksi tenant_id-once OLMADIGINDAN R5 unsurunu
-- karsilamaz; bu ayri indeks ZORUNLU.
CREATE INDEX admin_sessions_tenant_idx ON admin_sessions (tenant_id, admin_user_id);

ALTER TABLE admin_sessions ENABLE ROW LEVEL SECURITY;
-- FORCE: tablo sahibi bile politikaya tabi olsun (superuser haric -- M0-03).
ALTER TABLE admin_sessions FORCE ROW LEVEL SECURITY;

-- STANDART NULLIF politikasi -- resolve OR-dali YOK (admin'de resolver gerekmez,
-- yukaridaki tablo basi notuna bak). ADR 0002 madde 3 / Q27 birebir.
CREATE POLICY admin_sessions_tenant_isolation ON admin_sessions
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- DELETE VERILMEZ (section 4.6): oturum silinmez, iptal revoked_at damgasidir.
-- Acik REVOKE (M1-04 dersi -- GRANT'tan cikarmak yetmez).
GRANT SELECT, INSERT, UPDATE ON admin_sessions TO tappa_app;
REVOKE DELETE ON admin_sessions FROM tappa_app;


-- --- password_resets ---------------------------------------------------------
-- Single-use password-reset tokens (M7-04). KIRMIZI CIZGI (section 4.7): reset
-- token ASLA saklanmaz, yalnizca hash'i.
CREATE TABLE password_resets (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    -- admin_user_id NOT NULL: sifirlama daima bir admin icindir. Bilesik same-tenant
    -- FK (her iki sutun NOT NULL -> DAIMA denetlenir) capraz-tenant engeli.
    admin_user_id uuid NOT NULL,
    -- token_hash: e-posta ile gonderilen opak token'in hash'i. UNIQUE -> dogrulama
    -- tek sorguda ve iki reset ayni hash'i tasiyamaz.
    token_hash    text NOT NULL UNIQUE,
    -- created_at: sifirlama TALEP ani (skeleton/section 6 -- her tabloda created_at;
    -- kart alan listesinde yoktu, audit tutarliligi icin eklendi). expires_at'ten
    -- farki: created_at "ne zaman istendi", expires_at "ne zaman gecersizlesir".
    created_at    timestamptz NOT NULL DEFAULT now(),
    -- expires_at: bu andan sonra token gecersiz. NOT NULL -- suresiz reset token'i
    -- bir guvenlik acigidir (link fotoğraflanip sonra kullanilabilir).
    expires_at    timestamptz NOT NULL,
    -- used_at: TEK KULLANIMLIK damgasi. Doldugunda token tuketilmistir; satir
    -- SILINMEZ (section 4.6 audit izi -- "bu token ne zaman kullanildi"). NULL =
    -- henuz kullanilmamis. Kullanim kontrolu (used_at IS NULL AND now() < expires_at)
    -- uygulama/sorgu katmanindadir (M7-04).
    used_at       timestamptz,
    CONSTRAINT password_resets_admin_user_fk
        FOREIGN KEY (admin_user_id, tenant_id)
        REFERENCES admin_users (id, tenant_id) ON DELETE RESTRICT
);

-- tenant_id ONDE indeks (section 6 / R5) + "bir admin'in reset'leri" ve admin_users
-- silmedeki bilesik FK RESTRICT kontrolu. token_hash UNIQUE indeksi tenant_id-once
-- OLMADIGINDAN R5 unsurunu karsilamaz; bu ayri indeks ZORUNLU.
CREATE INDEX password_resets_tenant_idx ON password_resets (tenant_id, admin_user_id);

ALTER TABLE password_resets ENABLE ROW LEVEL SECURITY;
-- FORCE: tablo sahibi bile politikaya tabi olsun (superuser haric -- M0-03).
ALTER TABLE password_resets FORCE ROW LEVEL SECURITY;

CREATE POLICY password_resets_tenant_isolation ON password_resets
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- DELETE VERILMEZ (section 4.6): reset kaydi silinmez, used_at ile tuketilir.
-- UPDATE korunur (used_at damgasi bir UPDATE'tir). Acik REVOKE (M1-04 dersi).
GRANT SELECT, INSERT, UPDATE ON password_resets TO tappa_app;
REVOKE DELETE ON password_resets FROM tappa_app;

-- NOT (kapsam disi, bilincli): 00005 yorumlari transactions.entered_by,
-- transaction_reviews.reviewer_id ve audit_log.actor_id icin admin_users'a bir FK'yi
-- "M1-11'de eklenir" diye ongormustu. M1-11 KARTININ kabul kriterleri bu FK'leri
-- ISTEMEZ ve bu gorevin brief'i yalnizca uc tablonun yaratilmasini kapsar; ayrica
-- reviewer_id NOT NULL FK'si mevcut rls_test fixture'ini (rastgele reviewer_id)
-- kirardi. Bu yuzden back-FK'ler BURADA eklenmedi -- ayri, odakli bir degisiklige
-- (M6 auth/review wiring) birakildi. Rapor bunu acikca bir bosluk olarak isaretler.

-- +goose Down

-- FK sirasi: once admin_sessions ve password_resets (admin_users'a bilesik FK ile
-- bagli), sonra admin_users. DROP TABLE tabloya bagli politika, RLS, GRANT, indeks,
-- UNIQUE ve FK'leri birlikte duser. Rol yok, fonksiyon yok, resolver yok.
DROP TABLE password_resets;
DROP TABLE admin_sessions;
DROP TABLE admin_users;
