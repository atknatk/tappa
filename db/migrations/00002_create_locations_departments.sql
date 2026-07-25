-- +goose Up

-- locations: bir tenant'in fiziksel giris noktalari. "Proof of place"in temeli --
-- her lokasyon statik IP blok(lar)i ve/veya GPS koordinati tasir; tap karari
-- (CLAUDE.md section 5, satir 6) once IP eslesmesine, yoksa GPS'e (< 150 m) bakar.
-- Vardiya alanlari lokasyon tabanidir; departman vardiyasi (asagida) varsa ezer.
CREATE TABLE locations (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- tenant_id: her tabloda zorunlu kapsam anahtari (CLAUDE.md section 4.5).
    -- ON DELETE RESTRICT: altinda lokasyon varken tenant silinemez -- silme
    -- yerine durum alani tercih edilir, audit izi korunur.
    tenant_id   uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    name        text NOT NULL,
    -- static_ips: proof-of-place icin izinli IP araliklari (Q07 karari: cidr[]).
    -- Tek IP /32, ISS blogu /29 gibi; eslesme M1-08'de @src::inet <<= ANY(...).
    -- NOT NULL DEFAULT '{}': bos kume "IP kanidi yapilandirilmadi, GPS'e dus"
    -- demektir ve <<= ANY('{}') temiz sekilde FALSE doner. NULL yerine bos kume:
    -- NULL uc-degerli mantikta ANY(NULL) -> NULL uretirdi ve "kanit yok"un tek,
    -- belirsizsiz temsili bos kumedir.
    static_ips  cidr[] NOT NULL DEFAULT '{}',
    -- GPS: yedek proof-of-place. numeric, float DEGIL (CLAUDE.md section 6) --
    -- float haversine hesabinda kayma uretir. numeric(9,6): 3 tam + 6 ondalik,
    -- Malta enlem/boylami (35.9, 14.5) ile -90..90 / -180..180 sinir degerleri sigar.
    -- Nullable ve cift halinde: yalniz IP'ye dayanan lokasyon GPS tasimayabilir.
    gps_lat     numeric(9,6) CHECK (gps_lat BETWEEN -90 AND 90),
    gps_lng     numeric(9,6) CHECK (gps_lng BETWEEN -180 AND 180),
    -- Vardiya: duvar saati (time), timestamp DEGIL (CLAUDE.md section 6) -- tarih
    -- + zaman dilimi birlestirme render/domain katmanindadir. overnight bitisin
    -- ertesi gune sarktigini soyler (Rusty Bar 18:00-02:00). Vardiya NULLABLE:
    -- vardiya izlemeyen lokasyon (esnek saat) sahte saat uydurmaya zorlanmamali --
    -- M4-05 gec kalma cozumu null vardiyayi zarifce (gec kalma hesaplanmaz) ele alir.
    shift_start time,
    shift_end   time,
    overnight   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- GPS ve vardiya cift olarak gelir: biri varsa digeri de olmali.
    CONSTRAINT locations_gps_pair   CHECK ((gps_lat IS NULL) = (gps_lng IS NULL)),
    CONSTRAINT locations_shift_pair CHECK ((shift_start IS NULL) = (shift_end IS NULL)),
    -- Capraz-tenant FK korumasi icin bilesik benzersizlik: departments bu ciftle
    -- referans verir, boylece departmanin lokasyonu ayni tenant'ta olmak zorunda.
    CONSTRAINT locations_id_tenant_key UNIQUE (id, tenant_id)
);

-- tenant_id ONDE olan indeks (CLAUDE.md section 6). id PK'si benzersizlik saglar
-- ama kapsam anahtari tenant_id oldugundan RLS ve tenant kapsamli listeleme icin
-- ayri indeks ZORUNLU -- tenants'tan farki bu (orada kapsam anahtari PK'nin kendisi).
CREATE INDEX locations_tenant_idx ON locations (tenant_id);

ALTER TABLE locations ENABLE ROW LEVEL SECURITY;
-- FORCE: tablo sahibi bile politikaya tabi olsun (superuser haric -- M0-03).
ALTER TABLE locations FORCE ROW LEVEL SECURITY;

-- Politika ifadesi ADR 0002 madde 3 / Q27 uyarinca NORMATIF ve birebir: baglam
-- yokken GUC ya NULL'dur (hic yazilmamis) ya '' (bir kez yazilip tx bitmis);
-- NULLIF ikisini de NULL'a cevirir -> hicbir satir eslesmez (fail-closed). Ciplak
-- ::uuid cast YASAK: bos dize uzerinde hata firlatir, davranis baglantiya baglanir.
CREATE POLICY locations_tenant_isolation ON locations
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- tappa_app DML yetkisi. db-init'teki ALTER DEFAULT PRIVILEGES devrede olsa da
-- acik GRANT yazilir (CLAUDE.md section 6; redline R5 bunu tablo bazinda arar).
GRANT SELECT, INSERT, UPDATE, DELETE ON locations TO tappa_app;


-- departments: bir lokasyona ait organizasyon birimi (KM 5 departman). Vardiya
-- alanlari NULLABLE ve doluysa lokasyon vardiyasini EZER (CLAUDE.md section 5).
CREATE TABLE departments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    -- location_id NOT NULL: her departman bir lokasyona aittir. Duz FK tek basina
    -- capraz-tenant baglanmayi ENGELLEMEZ (A'nin departmani B'nin lokasyonuna
    -- baglanabilir); bu yuzden asagidaki bilesik FK (location_id, tenant_id) ->
    -- locations (id, tenant_id) kullanilir ve lokasyonun AYNI tenant'ta olmasini
    -- zorlar. ON DELETE RESTRICT: kullanilan lokasyon silinemez (audit izi).
    location_id uuid NOT NULL,
    name        text NOT NULL,
    -- Vardiya NULLABLE: doluysa lokasyon tabanini ezer, bossa lokasyondan cozulur.
    shift_start time,
    shift_end   time,
    overnight   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT departments_shift_pair CHECK ((shift_start IS NULL) = (shift_end IS NULL)),
    CONSTRAINT departments_location_fk
        FOREIGN KEY (location_id, tenant_id)
        REFERENCES locations (id, tenant_id) ON DELETE RESTRICT
);

-- tenant_id ONDE olan indeks (CLAUDE.md section 6). location_id ikinci sutun:
-- "bir lokasyonun departmanlari" listelemesini ve locations silmedeki bilesik
-- FK RESTRICT kontrolunu de karsilar.
CREATE INDEX departments_tenant_idx ON departments (tenant_id, location_id);

ALTER TABLE departments ENABLE ROW LEVEL SECURITY;
-- FORCE: tablo sahibi bile politikaya tabi olsun (superuser haric -- M0-03).
ALTER TABLE departments FORCE ROW LEVEL SECURITY;

CREATE POLICY departments_tenant_isolation ON departments
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON departments TO tappa_app;

-- +goose Down

-- Bagimlilik sirasi: departments once (locations'a bilesik FK ile bagli), sonra
-- locations. DROP TABLE tabloya bagli politika, RLS ayarlari ve GRANT'lari da duser.
DROP TABLE departments;
DROP TABLE locations;
