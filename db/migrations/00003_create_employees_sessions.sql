-- +goose Up

-- On-kosul: employees.department_id AYNI-tenant bilesik FK ile departments'a
-- baglanacak: (department_id, tenant_id) -> departments (id, tenant_id). Bir
-- bilesik FK, referans verdigi sutunlar uzerinde bir UNIQUE (ya da PK) kisiti
-- ISTER. M1-03 departments'a yalnizca PRIMARY KEY (id) koydu; (id, tenant_id)
-- uzerinde UNIQUE yok. Bu yuzden burada eklenir -- locations'in M1-03'te aldigi
-- locations_id_tenant_key ile ayni kalip. id zaten global tekil oldugundan
-- (id, tenant_id) eklemek mevcut veriyi ihlal etmez; kisit yalnizca capraz-tenant
-- baglanmayi yapisal olarak imkansiz kilan bilesik FK'yi mumkun kilar.
-- (Uygulanmis 00002 DEGISTIRILMEZ; duzeltme yeni migration'dir -- CLAUDE.md section 6.)
ALTER TABLE departments
    ADD CONSTRAINT departments_id_tenant_key UNIQUE (id, tenant_id);


-- employees: "Proof of person"in veri temeli. Bir tenant'in davet ettigi ve
-- aktive olan calisanlari. Biyometrik veri YOK (CLAUDE.md section 4.1): ne parmak
-- izi, ne yuz, ne ses. full_name/role/email kimlik metnidir, biyometri degil;
-- cihaz bilgisi burada degil sessions'ta durur.
CREATE TABLE employees (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- tenant_id: her tabloda zorunlu kapsam anahtari (CLAUDE.md section 4.5).
    -- ON DELETE RESTRICT: altinda calisan varken tenant silinemez (audit izi).
    tenant_id      uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    -- location_id NOT NULL: her calisan bir "ev" lokasyonuna aittir. KF 9
    -- lokasyonluk bir tenant'ta calisan bir subeye atanir; tek lokasyonlu
    -- tenant'ta zaten tek lokasyon vardir. section 5 "tap edilen lokasyonun vardiyasi"
    -- kurali, calisan BASKA subede tap ettiginde tap lokasyonunun vardiyasini
    -- kullanir (profildeki lokasyon degil) -- yani ev lokasyonu profil capasidir,
    -- tap lokasyonu farkli olabilir. Bilesik FK (location_id, tenant_id) her
    -- iki sutun da NOT NULL oldugundan DAIMA denetlenir -> lokasyon ayni tenant'ta.
    location_id    uuid NOT NULL,
    -- department_id NULLABLE: departman istege bagli bir organizasyon birimidir.
    -- KM 5 departman kullanir; KF (bar/restoran) departman modellemeyebilir.
    -- NOT NULL yapmak departman kullanmayan tenant'lari kirardi. section 5 gec kalma
    -- cozumu: departman vardiyasi VARSA ezer, yoksa lokasyon vardiyasi -- yani
    -- null departman birinci sinif, ele alinmis bir durum. Bilesik FK MATCH SIMPLE
    -- (varsayilan): department_id NULL iken kisit denetlenmez (dogru davranis);
    -- dolu iken (department_id, tenant_id) departments'ta ayni tenant'ta olmali.
    department_id  uuid,
    full_name      text NOT NULL,
    -- role: calisanin is unvani (garson, asci, mudur ...). SERBEST METIN cunku her
    -- tenant kendi unvan sozlugunu kullanir ve HICBIR karar buna dallanmaz --
    -- policy motoru admin yetkisi icin admin_users.role'u okur (M1-11), calisanin
    -- role'unu degil. CHECK ile sabit bir kume dayatmak yanlis olurdu (unvanlar
    -- sayilamaz); nullable cunku davet aninda unvan henuz atanmamis olabilir.
    role           text,
    -- email citext: buyuk/kucuk harf farkiyla ikinci davet gitmesin (kart tuzagi).
    -- NULLABLE: her vardiya calisani e-posta vermez/kullanmaz; davet bir kod/link
    -- olarak fiziksel de verilebilir. Tekillik TENANT ICINDE ve yalnizca dolu
    -- e-postalarda -- asagidaki kismi UNIQUE indeks. Global degil tenant kapsamli:
    -- ayni kisi iki farkli isletmede calisan olabilir.
    email          citext,
    -- status: davet yasam dongusu. DEFAULT 'invited' -- yeni calisan davetli dogar
    -- (handoff akisi). section 5 satir 4 deactivated calisani reject eder; bu alani okur.
    status         text NOT NULL DEFAULT 'invited'
                        CHECK (status IN ('invited', 'active', 'deactivated')),
    -- Yasam dongusu damgalari: yalnizca ilgili olay olunca dolar (nullable).
    invited_at     timestamptz,
    activated_at   timestamptz,
    deactivated_at timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    -- sessions bu ciftle (employee_id, tenant_id) referans verir; bilesik FK'nin
    -- gerektirdigi UNIQUE. Ayni zamanda calisanin tek tenant'a aitligini pekistirir.
    CONSTRAINT employees_id_tenant_key UNIQUE (id, tenant_id),
    -- Capraz-tenant baglanmayi yapisal engelleyen bilesik FK'ler (M1-03 kalibi).
    CONSTRAINT employees_location_fk
        FOREIGN KEY (location_id, tenant_id)
        REFERENCES locations (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT employees_department_fk
        FOREIGN KEY (department_id, tenant_id)
        REFERENCES departments (id, tenant_id) ON DELETE RESTRICT
);

-- tenant_id ONDE olan indeks (CLAUDE.md section 6): RLS ve tenant kapsamli
-- listeleme icin. UNIQUE (id, tenant_id) id-once oldugundan bu unsuru KARSILAMAZ.
CREATE INDEX employees_tenant_idx ON employees (tenant_id);

-- Tenant icinde e-posta tekilligi, yalnizca dolu e-postalarda (kismi indeks).
-- citext + kismi UNIQUE: ayni e-postaya (harf farkiyla dahi) ikinci davet gitmez,
-- ama e-postasiz calisanlar (NULL) coklu olabilir. tenant_id once oldugundan bu
-- indeks R5 "tenant_id onde indeks" unsurunu da karsilar (yedek olarak).
CREATE UNIQUE INDEX employees_tenant_email_key
    ON employees (tenant_id, email) WHERE email IS NOT NULL;

ALTER TABLE employees ENABLE ROW LEVEL SECURITY;
-- FORCE: tablo sahibi bile politikaya tabi olsun (superuser haric -- M0-03).
ALTER TABLE employees FORCE ROW LEVEL SECURITY;

-- Politika ifadesi ADR 0002 madde 3 / Q27 uyarinca NORMATIF ve birebir: baglam
-- yokken GUC ya NULL (hic yazilmamis) ya '' (bir kez yazilip tx bitmis); NULLIF
-- ikisini de NULL'a cevirir -> hicbir satir eslesmez (fail-closed). Ciplak
-- ::uuid cast YASAK: bos dize uzerinde hata firlatir, davranis baglantiya baglanir.
CREATE POLICY employees_tenant_isolation ON employees
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- DELETE bilincli olarak VERILMEZ (tekduze grant kalibindan bilincli sapma):
-- calisan silinmez, deaktive edilir (status='deactivated' + deactivated_at). GDPR
-- silme (Q13) anonimlestirmedir (UPDATE), hard-delete degil. DELETE yetkisi bir
-- bug/ele gecirilmis kod yolunda audit izini sert silebilirdi.
-- DIKKAT: db-init'teki ALTER DEFAULT PRIVILEGES her yeni tabloda tappa_app'e
-- SELECT/INSERT/UPDATE/DELETE verir; DELETE'i GRANT'tan CIKARMAK TEK BASINA YETMEZ
-- -- acikca REVOKE edilmelidir (M1-06 transactions ile ayni kalip).
GRANT SELECT, INSERT, UPDATE ON employees TO tappa_app;
REVOKE DELETE ON employees FROM tappa_app;


-- sessions: kalici tarayici oturumu. "Proof of person"in dogrulama tarafi -- bir
-- tap geldiginde cerezdeki token'in hash'i buradan calisana cozulur.
-- KIRMIZI CIZGI (CLAUDE.md section 4.7): token ASLA saklanmaz, yalnizca hash'i.
-- Sutun adi (token_hash) bunu acikca soyler. device_info yalnizca KABA cihaz
-- etiketidir (model/tarayici); ASLA fingerprint/biyometri DEGIL (section 4.1).
CREATE TABLE sessions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    -- employee_id NOT NULL: oturum daima bir calisana aittir. Bilesik FK
    -- (employee_id, tenant_id) her iki sutun NOT NULL oldugundan DAIMA denetlenir
    -- -> oturumun calisani ayni tenant'ta olmak zorunda (capraz-tenant engeli).
    employee_id  uuid NOT NULL,
    -- token_hash GLOBAL UNIQUE (tenant kapsamli degil): tenant cozumleme yolu
    -- (ADR 0002 madde 7) bu tabloyu baglam OLMADAN, yalnizca token_hash ile
    -- sorgular -- cunku tenant tam da o aramanin SONUCUdur. Global UNIQUE, o
    -- cozumlemenin <=1 satir dondurmesini YAPISAL olarak garanti eder. token'in
    -- rastgeleligi capraz-tenant carpismayi kriptografik olarak imkansiz kilar.
    token_hash   text NOT NULL UNIQUE,
    -- device_info: yalnizca kaba cihaz etiketi (orn. "iPhone Safari"), NULLABLE.
    -- Tarayici fingerprint'ine ASLA donusturulmez; biyometri saklanmaz (section 4.1).
    device_info  text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- last_used_at: baglam kurulduktan sonra normal RLS-bagli UPDATE ile guncellenir.
    last_used_at timestamptz,
    -- Silme YOK: iptal revoked_at damgasidir (audit izi korunur). NULL = aktif.
    -- Cozumleme fonksiyonu bu alani DONDURUR ama FILTRELEMEZ: iptal karari
    -- domain katmanindadir (section 5), veri katmani yalnizca gercegi tasir.
    revoked_at   timestamptz,
    CONSTRAINT sessions_employee_fk
        FOREIGN KEY (employee_id, tenant_id)
        REFERENCES employees (id, tenant_id) ON DELETE RESTRICT
);

-- tenant_id ONDE olan indeks (CLAUDE.md section 6). employee_id ikinci sutun:
-- "bir calisanin oturumlari" listelemesini ve employees silmedeki bilesik FK
-- RESTRICT kontrolunu de karsilar. token_hash UNIQUE indeksi tenant_id-once
-- OLMADIGINDAN R5 unsurunu karsilamaz; bu ayri indeks ZORUNLU.
CREATE INDEX sessions_tenant_idx ON sessions (tenant_id, employee_id);

ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
-- FORCE: tablo sahibi bile politikaya tabi olsun (superuser haric -- M0-03).
ALTER TABLE sessions FORCE ROW LEVEL SECURITY;

-- STANDART NULLIF politikasi -- cozumleme OR-dali YOK. GUC-anahtar saf-RLS
-- alternatifi tam da toplamsal bir OR-dali ("baglam yokken token_hash aramasina
-- izin ver") ekledigi icin reddedildi (ADR 0002 madde 7, "Degerlendirilen
-- alternatifler"): SET LOCAL'siz set edilen bir resolve-GUC havuz baglantisinda
-- kalir ve her mesru tenant sorgusuna bir capraz-tenant satir sizdirirdi.
-- Cozumleme politikadan degil, asagidaki cevrelenmis fonksiyondan gecer.
CREATE POLICY sessions_tenant_isolation ON sessions
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- DELETE bilincli olarak VERILMEZ (tekduze grant kalibindan bilincli sapma):
-- oturum silinmez, iptal revoked_at damgasidir (section 4.6 ruhu; audit izi
-- korunur). revoked_at UPDATE ile iptal hala mumkun. DELETE yetkisi bir
-- bug/ele gecirilmis kod yolunda iptal izini sert silebilirdi.
-- DIKKAT: db-init'teki ALTER DEFAULT PRIVILEGES her yeni tabloda tappa_app'e
-- SELECT/INSERT/UPDATE/DELETE verir; DELETE'i GRANT'tan CIKARMAK TEK BASINA YETMEZ
-- -- acikca REVOKE edilmelidir (M1-06 transactions ile ayni kalip).
GRANT SELECT, INSERT, UPDATE ON sessions TO tappa_app;
REVOKE DELETE ON sessions FROM tappa_app;


-- --- Tenant cozumleme mekanizmasi (ADR 0002 madde 7 -- YAPISAL cevreleme) -----
-- Bir tap geldiginde elde yalnizca cerez token'i var; tenant bilinmez cunku
-- tenant bu aramanin SONUCUdur. Cozumleme, RLS'e tabi tappa_app'in goremeyecegi
-- (FORCE + baglamsiz = 0 satir) bir satiri okumak zorundadir. Guvenlik RLS'ten
-- degil ARAYUZDEN gelir: dar, anahtar-parametreli, <=1 satir donduren, SELECT *
-- yuzeyi olmayan bir SECURITY DEFINER fonksiyon. Patlama yaricapi tappa_resolver'a
-- verilen GRANT'larla bu tek tabloya (hatta bu sutunlara) sinirli -- tum DB degil.

-- Sutun-duzeyi SELECT: tappa_resolver yalnizca cozumleme icin gereken sutunlari
-- gorur (device_info, created_at gibi digerlerini DEGIL). db-init'te tappa_resolver
-- HICBIR default privilege almadigindan baska hicbir tabloyu da goremez; blast
-- radius yapisal olarak bu GRANT'la sinirli.
GRANT SELECT (id, tenant_id, employee_id, token_hash, revoked_at)
    ON sessions TO tappa_resolver;

-- +goose StatementBegin
CREATE FUNCTION resolve_session_by_token_hash(p_token_hash text)
    RETURNS TABLE (id uuid, tenant_id uuid, employee_id uuid, revoked_at timestamptz)
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    -- KRITIK (search_path injection): search_path sabitlenir. public YOLDA DEGIL,
    -- bu yuzden tablo public.sessions olarak ACIKCA nitelenir -- aksi halde ya
    -- fonksiyon tabloyu bulamaz ya da bir saldirgan kendi 'sessions'ini araya
    -- sokabilirdi. pg_temp daima sona yazilir (Postgres onerisi).
    SET search_path = pg_catalog, pg_temp
    AS $$
        -- <=1 satir: token_hash GLOBAL UNIQUE. Sabit sutun listesi, SELECT * YOK.
        -- revoked_at dondurulur ama filtrelenmez (iptal karari domain'de, section 5).
        SELECT s.id, s.tenant_id, s.employee_id, s.revoked_at
        FROM public.sessions AS s
        WHERE s.token_hash = p_token_hash;
    $$;
-- +goose StatementEnd

-- EN-AZ-AYRICALIK: fonksiyon sahibi tappa_resolver (NOLOGIN, BYPASSRLS,
-- NOSUPERUSER). SECURITY DEFINER govdesi SAHIBININ yetkisiyle kosar; sahip
-- superuser (tappa_owner) OLSAYDI govde tum DB'de RLS'i atlar, patlama yaricapi
-- sinirsiz olurdu -- ADR 0002 madde 7'nin ACIKCA yasakladigi genel bypass.
ALTER FUNCTION resolve_session_by_token_hash(text) OWNER TO tappa_resolver;

-- KRITIK: fonksiyonlar varsayilan olarak PUBLIC EXECUTE alir. Once PUBLIC'ten
-- tumuyle geri alinir, sonra yalnizca tek mesru cagirana (tappa_app) verilir.
REVOKE ALL ON FUNCTION resolve_session_by_token_hash(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION resolve_session_by_token_hash(text) TO tappa_app;

-- +goose Down

-- Bagimlilik/FK sirasi: once fonksiyon (public.sessions'a atifta bulunur), sonra
-- sessions (employees'a bilesik FK), sonra employees (departments/locations'a
-- bilesik FK), en son 00003'un departments'a ekledigi UNIQUE kisiti. DROP TABLE
-- tabloya bagli politika, RLS, GRANT (tappa_app ve tappa_resolver sutun grant'i
-- dahil), UNIQUE ve FK'leri birlikte duser. tappa_resolver ROLU db-init'e aittir
-- ve BURADA DUSURULMEZ.
DROP FUNCTION IF EXISTS resolve_session_by_token_hash(text);
DROP TABLE sessions;
DROP TABLE employees;
ALTER TABLE departments DROP CONSTRAINT departments_id_tenant_key;
