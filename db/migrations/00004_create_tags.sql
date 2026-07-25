-- +goose Up

-- tags: duvara monte edilen NFC plaketinin (NTAG 424 DNA) kaydi ve replay
-- korumasinin DURUM tarafi. Her tap bir tag satirina cozulur; SUN dogrulamasi
-- (skill tappa-sun) bu satirdaki aes_key_ref ve last_ctr'e dayanir.
-- KIRMIZI CIZGILER: aes_key_ref KEK ile SARMALANMIS'tir, duz anahtar ASLA
-- (section 4.7); last_ctr guncellemesi M1-08'de tek atomik ifadedir (section 4.4);
-- tenant izolasyonu RLS beslisiyle zorlanir (section 4.5).
CREATE TABLE tags (
    -- uid: NTAG 424 DNA 7-byte UID'nin 14 haneli HEX metni; PRIMARY KEY.
    -- KARAR (char(14) hex metin, bytea DEGIL): tappa-sun UID'yi URL'de hex olarak
    -- tasir (?tag=91AC7E5500000A) ve cozumleme/loglar hex okunabilirligi ister --
    -- bytea'da her sorgu ve log satiri encode/decode gerektirirdi. Sabit 14
    -- uzunluk char(14)'e dogal oturur (padding yok cunku her uid TAM 14 hane).
    -- GLOBAL PK (tenant kapsamli degil): cozumleme yolu (ADR 0002 madde 7) bu
    -- tabloyu baglam OLMADAN, yalnizca uid ile sorgular -- tenant o aramanin
    -- SONUCUdur. Global tekil PK, cozumlemenin <=1 satir dondurmesini YAPISAL
    -- garanti eder. uid zaten PUBLIC'tir (plakette basili, NFC URL'sinde) -- gizli
    -- degil, bu yuzden global tekil olmasi sir sizdirmaz.
    -- CHECK: tam 14 hane HEX zorlanir. char(14) 14'ten kisa girdiyi bosluklarla
    -- 14'e tamamlar -> regex (bosluk hex degil) reddeder; 14'ten uzun girdi tip
    -- duzeyinde reddedilir. Buyuk/kucuk harf ikisi de kabul; kanonik hale getirme
    -- (varsa) uygulama katmaninin isidir (bu katman yalnizca BICIMI zorlar).
    uid          char(14) PRIMARY KEY
                     CHECK (uid ~ '^[0-9A-Fa-f]{14}$'),
    -- tenant_id: her tabloda zorunlu kapsam anahtari (section 4.5).
    -- ON DELETE RESTRICT: altinda tag varken tenant silinemez (audit izi).
    tenant_id    uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    -- location_id NOT NULL: bir plaket DAIMA bir lokasyon girisine monte edilir --
    -- "proof of place"in cip tarafi. Lokasyonsuz bir tag'in anlami yok. Duz FK
    -- capraz-tenant baglanmayi ENGELLEMEZ; asagidaki bilesik FK (location_id,
    -- tenant_id) -> locations (id, tenant_id) lokasyonun AYNI tenant'ta olmasini
    -- YAPISAL olarak zorlar (M1-03/M1-04 kalibi). Iki sutun da NOT NULL oldugundan
    -- kisit DAIMA denetlenir.
    location_id  uuid NOT NULL,
    -- aes_key_ref: cipin AES-128 anahtarinin KEK ile SARMALANMIS hali (envelope
    -- encryption, TAPPA_TAG_KEK). KIRMIZI CIZGI section 4.7: DUZ anahtar ne burada,
    -- ne log'da, ne repoda bulunur. Sarmali deger KEK olmadan ise yaramaz; cozumleme
    -- fonksiyonu bunu dondurur ama sarmali oldugu icin maruziyet kabul edilebilir
    -- (asagida cozumleme bloguna bakin). bytea: ikili sarmali ciktisi.
    aes_key_ref  bytea NOT NULL,
    -- last_ctr: cipin en son GORULEN okuma sayaci (24-bit, integer'a rahat sigar;
    -- ctr 16.777.215'te sarar). DEFAULT 0: yeni tag hic okunmamis baslar, ilk
    -- gecerli tap (ctr >= 1) ilerletir. NOT: ATOMIK ilerletme bu migration'in isi
    -- DEGIL -- M1-08'de tek ifade olur:
    --   UPDATE tags SET last_ctr = @ctr WHERE uid = @uid AND last_ctr < @ctr RETURNING uid
    -- Karsilastirma '<' (STRICT), '>=' DEGIL: '>=' ayni sayaci iki kez gecirir =
    -- replay'in ta kendisi (section 4.4, skill tappa-sun). Bu sutun yalnizca DURUMU
    -- tutar; oku-sonra-yaz ile karsilastirma YASAK (TOCTOU).
    last_ctr     integer NOT NULL DEFAULT 0,
    -- status: tag yasam dongusu (skill tappa-sun "Etiket yasam dongusu").
    --   active  -> normal; retired -> "Replace tag" sonrasi eski uid (tap reject);
    --   lost    -> bildirilmis kayip (tap reject + guvenlik uyarisi).
    -- CHECK ile sabit kume; DEFAULT 'active' (yeni tag aktif dogar).
    status       text NOT NULL DEFAULT 'active'
                     CHECK (status IN ('active', 'retired', 'lost')),
    -- retired_at: emeklilik damgasi (nullable). Silme YOK -- eski tag retired_at +
    -- status='retired' ile emekli edilir, gecmis transactions.tag_uid'den hala
    -- cozulur (section 4.6 ruhu, audit izi). NULL = emekli degil.
    retired_at   timestamptz,
    -- replaced_by: "Replace tag" akisinda ESKI tag'in isaret ettigi YENI uid
    -- (audit izi zinciri). Nullable (cogu tag degistirilmez). char(14) + ayni HEX
    -- CHECK bicim tutarliligi icin. Asagidaki bilesik self-FK ile ayni tenant'ta
    -- olmasi zorlanir.
    replaced_by  char(14)
                     CHECK (replaced_by IS NULL OR replaced_by ~ '^[0-9A-Fa-f]{14}$'),
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- Bilesik self-FK'nin (asagida) referans verecegi UNIQUE. uid zaten global PK
    -- oldugundan (uid, tenant_id) TRIVIAL tekildir; bu kisit yalnizca ayni-tenant
    -- self-FK'yi mumkun kilmak icin var (FK hedefi TAM o sutunlarda UNIQUE ister).
    CONSTRAINT tags_uid_tenant_key UNIQUE (uid, tenant_id),
    -- Capraz-tenant baglanmayi yapisal engelleyen bilesik FK'ler (M1-03/M1-04
    -- kalibi). location_id + tenant_id ikisi de NOT NULL -> DAIMA denetlenir.
    CONSTRAINT tags_location_fk
        FOREIGN KEY (location_id, tenant_id)
        REFERENCES locations (id, tenant_id) ON DELETE RESTRICT,
    -- replaced_by self-FK: kartta "replaced_by -> tags.uid" olarak gecti; burada
    -- AYNI-TENANT bilesik bicimine GUCLENDIRILDI (bilincli sapma, agent-brief md 8):
    -- duz "replaced_by -> tags(uid)" A tenant'inin tag'inin B tenant'inin uid'sine
    -- isaret etmesine izin verirdi (uid public oldugundan SIR sizmaz, ama sema
    -- genelindeki "capraz-tenant baglanma YAPISAL olarak imkansiz" kaliyla celisir).
    -- MATCH SIMPLE: replaced_by NULL iken kisit denetlenmez (dogru); doluyken
    -- (replaced_by, tenant_id) ayni tenant'ta bir tag'e isaret etmek ZORUNDA.
    -- ON DELETE RESTRICT: isaret edilen tag silinemez -- zaten silme yok (asagida
    -- REVOKE DELETE), bu RESTRICT kemere ek kusaktir ve audit zincirini korur.
    CONSTRAINT tags_replaced_by_fk
        FOREIGN KEY (replaced_by, tenant_id)
        REFERENCES tags (uid, tenant_id) ON DELETE RESTRICT
);

-- tenant_id ONDE olan indeks (section 6). location_id ikinci sutun: "bir
-- lokasyonun plaketleri" listelemesini ve locations silmedeki bilesik FK RESTRICT
-- kontrolunu de karsilar. uid PK ve UNIQUE(uid, tenant_id) indeksleri tenant_id
-- ONDE olmadigindan R5 "tenant_id onde indeks" unsurunu KARSILAMAZ; bu indeks
-- ZORUNLU.
CREATE INDEX tags_tenant_idx ON tags (tenant_id, location_id);

ALTER TABLE tags ENABLE ROW LEVEL SECURITY;
-- FORCE: tablo sahibi bile politikaya tabi olsun (superuser haric -- M0-03).
ALTER TABLE tags FORCE ROW LEVEL SECURITY;

-- STANDART NULLIF politikasi -- cozumleme OR-dali YOK (M1-04 sessions ile ayni).
-- GUC-anahtar saf-RLS alternatifi tam da toplamsal bir OR-dali ekledigi icin
-- reddedildi (ADR 0002 madde 7, "Degerlendirilen alternatifler"): SET LOCAL'siz
-- set edilen bir resolve-GUC havuz baglantisinda kalir ve her mesru tenant
-- sorgusuna capraz-tenant satir sizdirirdi. Cozumleme politikadan degil, asagidaki
-- cevrelenmis SECURITY DEFINER fonksiyonundan gecer. Ifade ADR 0002 madde 3 / Q27
-- uyarinca birebir: baglam yokken GUC ya NULL (hic yazilmamis) ya '' (bir kez
-- yazilip tx bitmis); NULLIF ikisini de NULL'a cevirir -> hicbir satir eslesmez
-- (fail-closed). Ciplak ::uuid cast YASAK: bos dize uzerinde hata firlatir.
CREATE POLICY tags_tenant_isolation ON tags
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- DELETE bilincli olarak VERILMEZ (tekduze grant kalibindan bilincli sapma):
-- tag silinmez, emekli edilir (status='retired' + retired_at) -- gecmis
-- transactions.tag_uid hala cozulebilsin (section 4.6, audit izi). UPDATE korunur:
-- emeklilik ve last_ctr ilerletmesi UPDATE'tir.
-- DIKKAT: db-init'teki ALTER DEFAULT PRIVILEGES her yeni tabloda tappa_app'e
-- SELECT/INSERT/UPDATE/DELETE verir; DELETE'i GRANT'tan CIKARMAK TEK BASINA YETMEZ
-- -- acikca REVOKE edilmelidir (M1-04 employees/sessions ile ayni kalip; M1-06
-- transactions immutability'sinin de temeli).
GRANT SELECT, INSERT, UPDATE ON tags TO tappa_app;
REVOKE DELETE ON tags FROM tappa_app;


-- --- Tenant cozumleme mekanizmasi (ADR 0002 madde 7 -- YAPISAL cevreleme) ------
-- Bir tap geldiginde elde yalnizca URL'deki uid var; tenant bilinmez cunku tenant
-- bu aramanin SONUCUdur. Cozumleme, RLS'e tabi tappa_app'in goremeyecegi (FORCE +
-- baglamsiz = 0 satir) bir satiri okumak zorundadir. Guvenlik RLS'ten degil
-- ARAYUZDEN gelir: dar, anahtar-parametreli, <=1 satir donduren, SELECT * yuzeyi
-- olmayan bir SECURITY DEFINER fonksiyon. Patlama yaricapi tappa_resolver'a verilen
-- GRANT'larla bu tek tabloya (hatta bu sutunlara) sinirli -- tum DB degil.

-- Sutun-duzeyi SELECT: tappa_resolver yalnizca cozumleme icin gereken sutunlari
-- gorur (created_at, retired_at, replaced_by gibi digerlerini DEGIL). db-init'te
-- tappa_resolver HICBIR default privilege almadigindan baska hicbir tabloyu da
-- goremez; blast radius yapisal olarak bu GRANT'la sinirli.
GRANT SELECT (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
    ON tags TO tappa_resolver;

-- +goose StatementBegin
CREATE FUNCTION resolve_tag_by_uid(p_uid char(14))
    RETURNS TABLE (
        uid         char(14),
        tenant_id   uuid,
        location_id uuid,
        aes_key_ref bytea,
        last_ctr    integer,
        status      text
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    -- KRITIK (search_path injection): search_path sabitlenir. public YOLDA DEGIL,
    -- bu yuzden tablo public.tags olarak ACIKCA nitelenir -- aksi halde ya fonksiyon
    -- tabloyu bulamaz ya da bir saldirgan kendi 'tags'ini araya sokabilirdi. pg_temp
    -- daima sona yazilir (Postgres onerisi).
    SET search_path = pg_catalog, pg_temp
    AS $$
        -- <=1 satir: uid PRIMARY KEY. Sabit sutun listesi, SELECT * YOK. aes_key_ref
        -- (SARMALI) SUN/CMAC dogrulamasi icin baglam kurulmadan gerekir; sarmali
        -- oldugundan KEK olmadan ise yaramaz ve uid zaten public -> maruziyet kabul
        -- edilebilir. last_ctr DURUM olarak dondurulur; ATOMIK ilerletme M1-08'in
        -- ayri UPDATE...RETURNING ifadesidir (bu deger oku-sonra-yaz karsilastirmada
        -- KULLANILMAZ -- section 4.4 TOCTOU). Ihtiyactan fazla sutun (created_at,
        -- retired_at, replaced_by) DONDURULMEZ.
        SELECT t.uid, t.tenant_id, t.location_id, t.aes_key_ref, t.last_ctr, t.status
        FROM public.tags AS t
        WHERE t.uid = p_uid;
    $$;
-- +goose StatementEnd

-- EN-AZ-AYRICALIK: fonksiyon sahibi tappa_resolver (NOLOGIN, BYPASSRLS,
-- NOSUPERUSER). SECURITY DEFINER govdesi SAHIBININ yetkisiyle kosar; sahip
-- superuser (tappa_owner) OLSAYDI govde tum DB'de RLS'i atlar, patlama yaricapi
-- sinirsiz olurdu -- ADR 0002 madde 7'nin ACIKCA yasakladigi genel bypass.
ALTER FUNCTION resolve_tag_by_uid(char(14)) OWNER TO tappa_resolver;

-- KRITIK: fonksiyonlar varsayilan olarak PUBLIC EXECUTE alir. Once PUBLIC'ten
-- tumuyle geri alinir, sonra yalnizca tek mesru cagirana (tappa_app) verilir.
REVOKE ALL ON FUNCTION resolve_tag_by_uid(char(14)) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION resolve_tag_by_uid(char(14)) TO tappa_app;

-- +goose Down

-- Bagimlilik sirasi: once fonksiyon (public.tags'e atifta bulunur), sonra tablo.
-- DROP TABLE tabloya bagli politika, RLS, GRANT (tappa_app ve tappa_resolver
-- sutun grant'i dahil), UNIQUE, indeks ve self-FK dahil FK'leri birlikte duser.
-- tappa_resolver ROLU db-init'e aittir ve BURADA DUSURULMEZ.
DROP FUNCTION IF EXISTS resolve_tag_by_uid(char(14));
DROP TABLE tags;
