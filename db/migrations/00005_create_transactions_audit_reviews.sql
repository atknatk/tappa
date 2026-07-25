-- +goose Up

-- Migration 0005 -- Tappa'nin ASIL kaydi ve denetim izi. Uc tablo:
--   transactions        : her tap'in APPEND-ONLY, hukuki delil olabilecek kaydi.
--   audit_log           : yonetimsel/domain olaylarinin APPEND-ONLY izi.
--   transaction_reviews : FLAGGED kayitlarin insan onay/ret karari (APPEND-ONLY).
--
-- KIRMIZI CIZGILER:
--   section 4.3  transactions IMMUTABLE -- UPDATE/DELETE YOK. Kusak+kemer: hem
--                tappa_app'ten REVOKE (yetki) HEM BEFORE UPDATE OR DELETE trigger
--                (superuser tappa_owner yolunu da kapatir). Ayni kalip audit_log
--                ve transaction_reviews'de de.
--   section 4.6  KAYIT ASLA KAYBOLMAZ -- kanit yetersizse REJECT edip ATMA, yaz.
--                Bu yuzden employee_id/location_id/department_id NULLABLE (asagida
--                gerekcesiyle) ve (tag_uid, ctr) uzerine UNIQUE KOYULMAZ.
--   section 4.5  Tenant izolasyonu: her tabloda RLS beslisi + acik GRANT.


-- --- Ortak APPEND-ONLY mutasyon engeli (section 4.3 kusak+kemer) --------------
-- REVOKE UPDATE/DELETE tappa_app'i durdurur; ama superuser tappa_owner RLS'i ve
-- yetki kontrolunu kosulsuz atlar (M0-03). Bu trigger tappa_owner dahil HERKESI
-- durdurur -- kemere ek kusak. NOT (kart kabul ediyor): superuser trigger'i
-- DISABLE edebilir; bu bilincli defense-in-depth, mutlak degil.
-- Guvenli yazim: search_path sabit (pg_catalog, pg_temp) -- injection'a kapali.
-- SECURITY INVOKER (varsayilan): govde yalnizca RAISE eder, hicbir veri okumaz;
-- DEFINER yapmak gereksiz bir ayricalik yuzeyi olurdu.
-- +goose StatementBegin
CREATE FUNCTION tappa_forbid_mutation()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
    AS $$
    BEGIN
        RAISE EXCEPTION
            'append-only table %: % is forbidden (section 4.3, use a new row + audit_log)',
            TG_TABLE_NAME, TG_OP
            USING ERRCODE = 'restrict_violation';
    END;
    $$;
-- +goose StatementEnd


-- --- transactions ------------------------------------------------------------
-- Urunun ASIL kaydi: her tap bir satirdir ve o satir bir daha DEGISMEZ. Duzeltme
-- = yeni satir + audit_log (section 4.3). FLAGGED onay/ret akisi bu tabloya HIC
-- dokunmaz -- yalnizca transaction_reviews + audit_log'a yazar (Q20), boylece
-- saat toplami sismez ve kaydin anlami net kalir.
CREATE TABLE transactions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- tenant_id: her tabloda zorunlu kapsam anahtari (section 4.5).
    tenant_id     uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    -- employee_id/location_id/department_id NULLABLE -- section 4.6 ZORUNLU KILAR.
    -- section 5 karar sirasinda satir 1 (etiket lost/retired) ve satir 2 (SUN
    -- gecersiz), satir 3'ten (oturum yok) ONCE gelir. Yani cerezsiz biri CALINMIS
    -- bir plakete dokundugunda employee_id OLMAYAN bir reject kaydi yazilmak
    -- ZORUNDA -- ve bu, kayit tutmayi EN COK istedigimiz senaryodur. NOT NULL
    -- varsayimi bu INSERT'i patlatir, hata yoluna dusurur, kayit kaybolur =
    -- section 4.6'nin var olus sebebi olan durumda section 4.6 ihlali. Hangi
    -- verdict'te hangi alanin null olabilecegi asagidaki CHECK'lerle sabitlenir.
    employee_id   uuid,
    location_id   uuid,
    department_id uuid,
    -- tag_uid NULLABLE: KARAR+GEREKCE -- manuel kayitta (channel='manual') fiziksel
    -- plaket YOKTUR, mudur elle giris yapar. NFC/QR tap'inde ise tag DAIMA cozulmus
    -- (var olan) bir tag'dir -- cozulemeyen bir uid tenant'i bilinmedigi icin (tenant
    -- aramanin sonucu) transaction'a hic donusmez. Yani tag_uid ya NULL ya var olan
    -- bir tag -> asagidaki FK section 4.6'yi ihlal etmez. char(14): tags.uid ile ayni.
    tag_uid       char(14),
    -- ctr NULLABLE: manuel/QR kayitta cip okuma sayaci olmayabilir. (tag_uid, ctr)
    -- uzerine UNIQUE ASLA KOYULMAZ (asagida ayrica aciklandi): reddedilen bir replay
    -- de ayni cifti tasir; UNIQUE olsa INSERT patlar = kayit kaybi (section 4.6).
    -- Replay korumasi YALNIZCA tags.last_ctr atomik update'indedir (section 4.4).
    ctr           integer,
    -- type: yon. reject/ignored'da yon YOK -> NULL. CHECK kapali sozluk (in|out).
    type          text CHECK (type IS NULL OR type IN ('in', 'out')),
    -- occurred_at: TAP ANI (cevrimdisi kuyrukta gecmis olabilir). created_at'ten
    -- FARKLIDIR (satirin yazildigi an). NOT NULL: her tap'in bir ani vardir; motor
    -- daima uretir (section 5 sys:occurred-at-bound guardrail'i ileride sinirlar).
    occurred_at   timestamptz NOT NULL,
    -- source_ip: proof-of-place'in IP tarafi (nullable -- GPS-yalniz veya manuel).
    -- inet tipi. Log'a tam GPS/IP yazilmaz ama DB'ye yazilir (kayit butunlugu).
    source_ip     inet,
    -- Kanit boolean'lari NULLABLE: NULL = "bu kanal icin degerlendirilmedi" (orn.
    -- manuel kayitta sun_valid anlamsizdir). NOT NULL yapmak, motor bir alani set
    -- etmeyi atlarsa kaydi dusururdu (section 4.6). Uc-durum (true/false/null)
    -- bilinmeyeni durusten ayirir.
    ip_match      boolean,
    -- GPS: numeric(9,6) -- float DEGIL (section 6). RANGE/PAIR CHECK YOK (BILINCLI):
    -- transactions section 4.6 "her seyi kaydet" tablosudur; bozuk/menzil-disi GPS
    -- gonderen bir istemci tap kaydini KAYBETTIRMEMELI. Dogrulama HANDLER sinirinda
    -- yapilir (section 7); handler bozuk GPS'i NULL'a indirir. Gecerli koordinat
    -- (-90..90 / -180..180) numeric(9,6)'ya TAM sigar (en cok 3 tam hane), yani
    -- gecerli veri asla tasmaz; menzil CHECK'i yalnizca gecersiz veriyi keserdi ki
    -- o zaten handler'da elenmistir -- burada CHECK sadece kayit kaybi riski katardi.
    gps_lat       numeric(9, 6),
    gps_lng       numeric(9, 6),
    gps_match     boolean,
    sun_valid     boolean,
    -- trust: guven puani (section 5: 20 taban + 50 IP + 30 GPS). smallint yeter.
    -- Menzil CHECK'i YOK -- ayni section 4.6 gerekcesi (motor beklenmedik deger
    -- uretse bile kayit dusmemeli); dogru araligi motor garanti eder, DB degil.
    trust         smallint,
    -- verdict: kaydin karari. KAPALI sozluk, uretici KENDI kodumuzdur (tap motoru
    -- section 5) -> CHECK guvenli (gecersiz verdict bir bug'dir, anlamsiz kayit
    -- yazmaktansa erken patlamasi dogru). NOT NULL: kararsiz kayit yok.
    verdict       text NOT NULL
                      CHECK (verdict IN ('ok', 'flag', 'reject', 'ignored')),
    -- note: serbest aciklama (orn. 'verified via GPS', 'different branch'). Nullable.
    note          text,
    -- channel: kaydin gelis yolu. KAPALI sozluk, handler set eder -> CHECK guvenli.
    -- NOT NULL: her kaydin bir kanali vardir. Default YOK -- ureticinin acikca
    -- set etmesi beklenir (sessiz 'nfc' default'u bir bug'i maskeleyebilirdi).
    channel       text NOT NULL
                      CHECK (channel IN ('nfc', 'qr', 'manual')),
    -- entered_by: manuel kayitta GIRISI YAPAN yonetici. Nullable (nfc/qr'da NULL).
    -- FK'siz: reviewer bir admin/yonetici -> admin_users (M1-11, HENUZ YOK). FK
    -- M1-11'de eklenir (sapma degil, siralama gercegi -- ADR/kart notu).
    entered_by    uuid,
    -- practice: aktivasyon sonrasi ilk kayit (TRAINING damgasi); calisilan saate
    -- ASLA sayilmaz (section 5). NOT NULL DEFAULT false.
    practice      boolean NOT NULL DEFAULT false,
    -- queued: kayit onay kuyrugunda mi (flag -> true). NOT NULL DEFAULT false.
    queued        boolean NOT NULL DEFAULT false,
    -- created_at: SATIRIN yazildigi an (occurred_at'ten farkli). DEFAULT now().
    created_at    timestamptz NOT NULL DEFAULT now(),

    -- CHECK 1 -- KARAR VERILMIS kayit KIMI tanir. 'ok' (section 5 satir 6) ve
    -- 'flag' (satir 7) yalnizca oturum cozuldukten (satir 3) SONRA uretilir, yani
    -- employee_id DAIMA bilinir. reject/ignored ise satir 3'ten ONCE uretilebilir
    -- -> employee_id NULL olabilir (section 4.6 calinmis-plaket rejecti). Bu CHECK
    -- ayni zamanda transaction_reviews'un 3. kisitinin (reviewer_id <>
    -- employee_id) TABANIDIR: flag kaydin employee_id'si non-null oldugundan
    -- kendini-onaylama kontrolu anlamlidir (NULL <> x -> unknown, sessizce gecerdi).
    CONSTRAINT transactions_decided_has_employee CHECK (
        verdict IN ('reject', 'ignored') OR employee_id IS NOT NULL
    ),
    -- CHECK 2 -- ONAYLANMIS varlik ('ok') bir YON tasir (in/out toggle, section 5).
    -- 'flag' bilincli olarak SERBEST birakildi: flag = section 4.6'nin cekirdek
    -- "yaz ve kuyruga dus" kaydidir, bir CHECK onu ASLA bloklamamali.
    CONSTRAINT transactions_ok_has_direction CHECK (
        verdict <> 'ok' OR type IS NOT NULL
    ),

    -- Bilesik same-tenant UNIQUE: transaction_reviews bu ciftle referans verir
    -- (asagida). id zaten global PK oldugundan (id, tenant_id) trivial tekildir;
    -- kisit yalnizca same-tenant FK'yi mumkun kilmak icin var.
    CONSTRAINT transactions_id_tenant_key UNIQUE (id, tenant_id),

    -- Capraz-tenant baglanmayi YAPISAL engelleyen bilesik FK'ler (M1-03/04/05
    -- kalibi). Hepsi MATCH SIMPLE: ilgili id NULL iken kisit DENETLENMEZ (section
    -- 4.6 -- null employee/location/department/tag'li kayit yazilabilir). Hepsi
    -- ON DELETE RESTRICT: mesai gecmisi silinmez, referanslari cozulebilir kalir.
    CONSTRAINT transactions_tag_fk
        FOREIGN KEY (tag_uid, tenant_id)
        REFERENCES tags (uid, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT transactions_employee_fk
        FOREIGN KEY (employee_id, tenant_id)
        REFERENCES employees (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT transactions_location_fk
        FOREIGN KEY (location_id, tenant_id)
        REFERENCES locations (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT transactions_department_fk
        FOREIGN KEY (department_id, tenant_id)
        REFERENCES departments (id, tenant_id) ON DELETE RESTRICT
);

-- Indeksler (section 6, ikisi de tenant_id ONDE -> R5 karsilanir):
--   1. (tenant_id, occurred_at DESC): tenant kapsamli kronolojik listeleme
--      (gunluk rapor, "son N tap").
--   2. (tenant_id, employee_id, occurred_at DESC): YON TAYININ temeli -- kisinin
--      SON ACIK girisini bulan sorgu (section 5 toggle). M1-08 GetLastOpenTransaction
--      ve debounce sorgulari bunu kullanir.
CREATE INDEX transactions_tenant_occurred_idx
    ON transactions (tenant_id, occurred_at DESC);
CREATE INDEX transactions_tenant_employee_occurred_idx
    ON transactions (tenant_id, employee_id, occurred_at DESC);

ALTER TABLE transactions ENABLE ROW LEVEL SECURITY;
-- FORCE: tablo sahibi bile politikaya tabi olsun (superuser haric -- M0-03).
ALTER TABLE transactions FORCE ROW LEVEL SECURITY;

-- Politika ifadesi ADR 0002 madde 3 / Q27 uyarinca NORMATIF ve birebir: baglam
-- yokken GUC ya NULL (hic yazilmamis) ya '' (bir kez yazilip tx bitmis); NULLIF
-- ikisini de NULL'a cevirir -> hicbir satir eslesmez (fail-closed). Ciplak ::uuid
-- cast YASAK: bos dize uzerinde hata firlatir.
CREATE POLICY transactions_tenant_isolation ON transactions
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- IMMUTABILITY (section 4.3) -- KUSAK 1: yetki. tappa_app YALNIZCA okuyup yazabilir.
-- DIKKAT (M1-04 dersi): db-init'teki ALTER DEFAULT PRIVILEGES her yeni tabloya
-- SELECT/INSERT/UPDATE/DELETE verir; UPDATE/DELETE yetkisini GRANT'tan CIKARMAK
-- TEK BASINA YETMEZ -- acikca REVOKE edilmelidir. has_table_privilege ile dogrulanir.
GRANT SELECT, INSERT ON transactions TO tappa_app;
REVOKE UPDATE, DELETE ON transactions FROM tappa_app;

-- IMMUTABILITY -- KUSAK 2: trigger. Yetki atlanan yolu (superuser tappa_owner)
-- da kapatir. tappa_owner bir satiri degistirmeye/silmeye kalkarsa trigger RAISE
-- eder -- permission degil, TRIGGER (kusak+kemer kaniti, section 4.3).
CREATE TRIGGER transactions_no_mutation
    BEFORE UPDATE OR DELETE ON transactions
    FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation();


-- --- audit_log ---------------------------------------------------------------
-- Yonetimsel ve domain olaylarinin APPEND-ONLY izi (tag emekli edildi, review
-- kaydedildi, calisan deaktive edildi ...). transactions ile ayni immutability
-- kalibi: REVOKE UPDATE/DELETE + trigger.
CREATE TABLE audit_log (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    -- actor_id NULLABLE, FK'siz: aktor ya bir admin (admin_users, M1-11) ya da
    -- SISTEM (arka plan isi -> aktor yok). Polimorfik (admin veya sistem)
    -- oldugundan tek bir FK dogru degil; kimlik metni detail'da tasinabilir.
    actor_id  uuid,
    -- action: ne oldu (orn. 'tag.retired', 'review.recorded'). NOT NULL -- olaysiz
    -- audit satiri anlamsiz. Serbest metin: olay sozlugu domain'de buyur, CHECK
    -- dayatmak her yeni olay tipinde migration gerektirirdi.
    action    text NOT NULL,
    -- target: etkilenen varligin opak referansi (orn. transaction id, tag uid).
    -- Nullable: bazi olaylarin tekil hedefi yoktur. Metin: heterojen id tipleri.
    target    text,
    -- detail: KARAR+GEREKCE -- jsonb (text DEGIL). Denetim ayrintisi dogal olarak
    -- yapisal anahtar-deger'dir (eski/yeni deger, sebep, baglam); jsonb sorgulanab
    -- ve iyi-bicimliligi dogrular. text ad-hoc ayristirma dayatirdi. NOT NULL
    -- DEFAULT '{}': her satir iyi-bicimli (bos da olsa) bir nesne tasir ve DEFAULT
    -- sayesinde bu alan ASLA bir INSERT'i bloklamaz (section 4.6). KIRMIZI CIZGI
    -- section 4.7: detail'a SIR (token/anahtar/tam GPS) YAZILMAZ -- uygulama sorumlu.
    detail    jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- at: olay ani. NOT NULL DEFAULT now().
    at        timestamptz NOT NULL DEFAULT now()
);

-- tenant_id ONDE indeks (section 6 / R5). at DESC: tenant kapsamli kronolojik
-- audit listelemesi.
CREATE INDEX audit_log_tenant_at_idx ON audit_log (tenant_id, at DESC);

ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
-- FORCE: tablo sahibi bile politikaya tabi olsun (superuser haric -- M0-03).
ALTER TABLE audit_log FORCE ROW LEVEL SECURITY;

CREATE POLICY audit_log_tenant_isolation ON audit_log
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- APPEND-ONLY (section 4.3): ayni kusak+kemer. REVOKE (yetki) + trigger (superuser).
GRANT SELECT, INSERT ON audit_log TO tappa_app;
REVOKE UPDATE, DELETE ON audit_log FROM tappa_app;

CREATE TRIGGER audit_log_no_mutation
    BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation();


-- --- transaction_reviews -----------------------------------------------------
-- FLAGGED kayitlarin insan karari. transactions'a HIC dokunmaz (Q20): onay/ret
-- BURAYA + audit_log'a yazilir, transactions salt tap kaydi kalir (saat toplami
-- sismez). APPEND-ONLY + UC KISIT -- yoksa Q20 transactions'i koruyup etkin durumu
-- sinirsiz degistirilebilir kilardi (section 4.3'un LAFZI korunur, RUHU yok olur).
CREATE TABLE transaction_reviews (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    transaction_id uuid NOT NULL,
    -- reviewer_id NOT NULL, FK'siz: reviewer bir admin/yonetici -> admin_users
    -- (M1-11, HENUZ YOK). FK M1-11'de eklenir -- bu SAPMA DEGIL, siralama gercegi
    -- (transactions.entered_by ile ayni durum). Kart/ADR bunu boyle ongoruyor.
    reviewer_id    uuid NOT NULL,
    -- outcome: karar. KAPALI sozluk (approved|rejected). NOT NULL.
    outcome        text NOT NULL CHECK (outcome IN ('approved', 'rejected')),
    note           text,
    reviewed_at    timestamptz NOT NULL DEFAULT now(),

    -- KISIT 1 -- bir kayit BIR KEZ karara baglanir. Aksi halde mudur
    -- approved/rejected/approved yazarak etkin sonucu istedigi kadar degistirir
    -- (transactions'a UPDATE yetkisi olmasa bile). UNIQUE(transaction_id) global:
    -- transaction_id zaten global tekildir, per-tenant'a gerek yok.
    CONSTRAINT transaction_reviews_txn_key UNIQUE (transaction_id),
    -- transaction_id -> transactions same-tenant bilesik FK, ON DELETE RESTRICT.
    -- (id, tenant_id) hedefi icin transactions_id_tenant_key UNIQUE kisitini (yukarida)
    -- kullanir. Capraz-tenant review baglamayi yapisal engeller.
    CONSTRAINT transaction_reviews_txn_fk
        FOREIGN KEY (transaction_id, tenant_id)
        REFERENCES transactions (id, tenant_id) ON DELETE RESTRICT
);

-- tenant_id ONDE indeks (section 6 / R5) + tenant kapsamli review listeleme.
-- transaction_id uzerindeki indeks KISIT 1'in UNIQUE kisiti tarafindan saglanir
-- (rapor son onayi JOIN ile transaction_id uzerinden okur).
CREATE INDEX transaction_reviews_tenant_idx
    ON transaction_reviews (tenant_id, transaction_id);

ALTER TABLE transaction_reviews ENABLE ROW LEVEL SECURITY;
-- FORCE: tablo sahibi bile politikaya tabi olsun (superuser haric -- M0-03).
ALTER TABLE transaction_reviews FORCE ROW LEVEL SECURITY;

CREATE POLICY transaction_reviews_tenant_isolation ON transaction_reviews
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- APPEND-ONLY (section 4.3): ayni kusak+kemer. REVOKE (yetki) + trigger (superuser).
GRANT SELECT, INSERT ON transaction_reviews TO tappa_app;
REVOKE UPDATE, DELETE ON transaction_reviews FROM tappa_app;

CREATE TRIGGER transaction_reviews_no_mutation
    BEFORE UPDATE OR DELETE ON transaction_reviews
    FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation();

-- KISIT 2 + 3 -- CHECK baska tabloya bakamaz, bu yuzden BEFORE INSERT trigger:
--   KISIT 2: transaction_id yalnizca verdict='flag' bir kayda isaret edebilir.
--            Aksi halde mudur trust=100, 'ok' bir kayda 'rejected' yazip SAATI
--            SILER -- transactions'a UPDATE yetkisi olmamasina ragmen. transactions
--            immutable oldugundan verdict INSERT'te sabittir, sonradan degismez ->
--            tek kontrol yeter.
--   KISIT 3: reviewer_id <> ilgili transaction.employee_id (kendini onaylama
--            yasak, sys:no-self-review). flag kayitlarin employee_id'si
--            transactions_decided_has_employee CHECK'i sayesinde non-null'dir ->
--            karsilastirma anlamli (aksi halde NULL <> x -> unknown, sessizce gecerdi).
-- SECURITY INVOKER (varsayilan): govde tappa_app olarak, RLS'e TABI kosar. Review
-- daima tenant baglaminda insert edilir (reviews WITH CHECK NEW.tenant_id=baglam'i
-- zorlar) -> SELECT ayni tenant'in transaction'ini gorur. DEFINER GEREKSIZ ve
-- fazladan ayricalik yuzeyi olurdu. FK (RLS'i atlar) referansin varligini ayrica
-- garantiler; trigger yalnizca verdict/self-review anlamsal kisitlarini ekler.
-- +goose StatementBegin
CREATE FUNCTION tappa_check_transaction_review()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
    AS $$
    DECLARE
        v_verdict     text;
        v_employee_id uuid;
    BEGIN
        -- public.-nitelenmis (search_path sabit; injection'a kapali).
        SELECT t.verdict, t.employee_id
          INTO v_verdict, v_employee_id
          FROM public.transactions AS t
         WHERE t.id = NEW.transaction_id
           AND t.tenant_id = NEW.tenant_id;

        IF NOT FOUND THEN
            RAISE EXCEPTION
                'review target transaction % not found in tenant %',
                NEW.transaction_id, NEW.tenant_id
                USING ERRCODE = 'foreign_key_violation';
        END IF;

        IF v_verdict <> 'flag' THEN
            RAISE EXCEPTION
                'only flag transactions are reviewable; % has verdict %',
                NEW.transaction_id, v_verdict
                USING ERRCODE = 'check_violation';
        END IF;

        IF NEW.reviewer_id = v_employee_id THEN
            RAISE EXCEPTION
                'self-review forbidden: reviewer equals the reviewed employee'
                USING ERRCODE = 'check_violation';
        END IF;

        RETURN NEW;
    END;
    $$;
-- +goose StatementEnd

CREATE TRIGGER transaction_reviews_validate
    BEFORE INSERT ON transaction_reviews
    FOR EACH ROW EXECUTE FUNCTION tappa_check_transaction_review();


-- +goose Down

-- Bagimlilik/FK sirasi: once transaction_reviews (transactions'a FK ile bagli),
-- sonra audit_log (bagimsiz), sonra transactions. DROP TABLE tabloya bagli
-- politika, RLS ayarlari, GRANT'lar, indeksler, kisitlar ve TRIGGER'lari birlikte
-- dusurur -- ama TRIGGER FONKSIYONLARI ayri nesnelerdir; tum trigger'lar (dolayisi
-- ile tablolar) dustukten SONRA fonksiyonlar dusurulur. tappa_resolver ROLU ve
-- db-init nesneleri BURADA DUSURULMEZ.
DROP TABLE transaction_reviews;
DROP TABLE audit_log;
DROP TABLE transactions;
DROP FUNCTION IF EXISTS tappa_check_transaction_review();
DROP FUNCTION IF EXISTS tappa_forbid_mutation();
