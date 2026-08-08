-- 00012 -- employee_invites.cancelled_at: an invitation can be RETIRED without
-- being used.
--
-- 🔴 NEDEN (kullanici karari, 2026-08-08). Bir daveti harcamak KARDES daveti
-- emekliye ayirmiyordu. M6-05 B fazinda uctan uca HTTP ile olculdu:
--
--   iki kez "Send invite"        -> 2 ayni anda gecerli davet
--   en YENI kodla aktivasyon     -> calisan 'active', 1 davet HALA bekliyor
--   en ESKI kodla aktivasyon     -> HALA BASARILI  (ikinci-cihaz yolu)
--
-- Ikinci-cihaz yolu RevokeAllForEmployee cagirir: gercek calisanin telefonu
-- oturumdan duser ve eski linki tutan kisi onun yerine trust 100 ile mesai yazar
-- (ADR 0005 Y-D'nin hayalet-calisan sekli, ama YABANCI icin). Kotu niyetli mudur
-- gerekmiyor: "link gelmedi, tekrar bas" yeterli.
--
-- 🔴 NEDEN MIGRATION SART -- olculdu, varsayilmadi (2026-08-07):
--
--   has_column_privilege('tappa_app','employee_invites',<col>,'UPDATE')
--     -> yalnizca used_at = true
--   UPDATE employee_invites SET expires_at = now()  -> permission denied
--   DELETE FROM employee_invites                    -> permission denied
--
-- 00009 sutun-duzeyi UPDATE'i BILEREK used_at'e daraltti, yani uygulama rolunun
-- elinde bir daveti gecersizlestirecek HICBIR ifade yoktu. Yeni sutun + yeni
-- sutun GRANT'i olmadan bu kapatilamaz.
--
-- 🔴 used_at ILE cancelled_at IKI AYRI OLGUDUR VE ASLA BIRBIRININ YERINE
-- OKUNMAZ. 00009 ve db/queries/invites.sql used_at'i "bu kod KULLANILDI" diye
-- tanimliyor ve iptal icin kullanilmasini acikca yasakliyor -- overloading, bir
-- audit cevabini bozardi ("bu davet ne zaman harcandi?"). Bu yuzden her sorgu
-- IKISINI DE AYRI AYRI eler: `used_at IS NULL AND cancelled_at IS NULL`.
--
-- 🔴 BES ZORUNLU UNSUR (CLAUDE.md section 6) BOZULMADI. Bu migration YENI TABLO
-- yaratmiyor; var olan employee_invites'a bir sutun ekliyor. Tablonun tenant_id
-- NOT NULL'u, (tenant_id, employee_id) indeksi, ENABLE + FORCE RLS'i, USING +
-- WITH CHECK politikasi ve tappa_app GRANT'i 00009'da ve DEGISMIYOR -- asagida
-- yalnizca GRANT genisliyor, sutun duzeyinde.

-- +goose Up

-- cancelled_at: davet ARTIK KULLANILAMAZ ama KULLANILMADI. NULL = hala gecerli.
-- Nullable ve varsayilansiz: var olan satirlarin hepsi NULL dogar, yani bu
-- migration hicbir bekleyen daveti oldurmez (geri-doldurma YOK -- bir satirin
-- gecmiste iptal edildigini iddia etmek uydurma olurdu).
ALTER TABLE employee_invites ADD COLUMN cancelled_at timestamptz;

COMMENT ON COLUMN employee_invites.cancelled_at IS
    'Davet gecersizlestirildi (kullanilmadi). used_at ile ASLA karistirilmaz: '
    'used_at = "kod harcandi", cancelled_at = "kod artik kullanilamaz". '
    'Sorgular ikisini de ayri ayri eler.';

-- INDEKS EKLENMEDI, ve bu bir karar. Bu sutunu okuyan her sorgu zaten
-- employee_invites_tenant_idx (tenant_id, employee_id) ile ya da code_hash'in
-- GLOBAL UNIQUE indeksiyle giriyor; cancelled_at yalnizca o dar kumeyi eleyen
-- bir yuklem. Bir tenant'in bir calisaninin ayni anda bekleyen davet sayisi tek
-- haneli (00009: tek kullanimlik, kisa TTL), yani ayri bir indeks tarama
-- kazandirmaz ve her INSERT'e bedel yazardi.

-- 🔴 SUTUN DUZEYI GRANT -- tabloya toptan UPDATE VERILMEZ. 00009'un gerekcesi
-- burada birebir gecerli: table-wide UPDATE, expires_at'i uzatmayi (olu bir
-- daveti diriltmek), employee_id'yi degistirmeyi ve code_hash'i yeniden yazmayi
-- da acardi. Yeni GRANT eski GRANT'in YERINE gecer (Postgres sutun grant'leri
-- birikimlidir; ikisini de yazmak niyeti gorunur kilar).
GRANT UPDATE (used_at, cancelled_at) ON employee_invites TO tappa_app;

-- ⚠️ APPEND-ONLY TRIGGER YOK -- CARPISACAK BIR SEY YOK. 00009 bu tabloya
-- transactions/audit_log'un tappa_forbid_mutation trigger'ini BILEREK koymadi
-- (kendi yorumu: "bir DELETE-only trigger 00005'in ikili yasagini kopyalayamaz
-- ve used_at GUNCELLENEBILIR OLMALI"), koruma REVOKE ile kuruldu. Dolayisiyla
-- bu sutun mevcut hicbir kisitla catismaz. Kalan zayiflik da degismiyor ve
-- 00009'da yazili: sutun grant'i HANGI sutunun yazilabilecegini soyler, HANGI
-- DEGERI degil -- `SET cancelled_at = NULL` (iptali geri alma) yetki sistemince
-- serbesttir. Buna karsi koruma, db/queries icinde bunu yapan HICBIR IFADE
-- OLMAMASIDIR (CancelPendingInvitesForEmployee yalnizca NULL -> now() yonunde
-- yazar ve WHERE'i cancelled_at IS NULL ister). Yapisal hale getirmek bir
-- trigger ister ve o da used_at'in ayni sorunuyla birlikte ele alinmali -- YENI
-- bir migration, alinmadi.

-- Cozumleme fonksiyonu cancelled_at'i de dondurmeli: aksi halde iptal edilmis
-- bir kodla gelen istek "bilinmeyen kod"tan ayirt edilemez ve audit izi YANLIS
-- sebep yazar. RETURNS TABLE'in sutun kumesi degistigi icin CREATE OR REPLACE
-- YETMEZ (Postgres donus tipini degistirmeye izin vermez) -- DROP + CREATE.
DROP FUNCTION IF EXISTS resolve_invite_by_code_hash(text);

-- Sutun-duzeyi SELECT genisletilir: tappa_resolver artik cancelled_at'i de
-- gormeli. created_at yine LISTEDE DEGIL (ihtiyactan fazla sutun dondurulmez).
GRANT SELECT (cancelled_at) ON employee_invites TO tappa_resolver;

-- +goose StatementBegin
CREATE FUNCTION resolve_invite_by_code_hash(p_code_hash text)
    RETURNS TABLE (
        id           uuid,
        tenant_id    uuid,
        employee_id  uuid,
        expires_at   timestamptz,
        used_at      timestamptz,
        cancelled_at timestamptz
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    -- KRITIK (search_path injection): 00009'un kurallari birebir korunur --
    -- sabit search_path, public YOLDA DEGIL, tablo acikca nitelenmis, pg_temp
    -- sonda. Bu govde 00009'un govdesidir + bir sutun.
    SET search_path = pg_catalog, pg_temp
    AS $$
        -- <=1 satir: code_hash GLOBAL UNIQUE. Sabit sutun listesi, SELECT * YOK.
        -- Uc durum sutunu da DONDURULUR ama FILTRELENMEZ: karar domain
        -- katmanindadir ve atomik tuketim ayri bir UPDATE'tir. Buradan okunan
        -- used_at/cancelled_at bir oku-sonra-yaz karsilastirmasinda KULLANILMAZ
        -- (section 4.4 TOCTOU).
        SELECT i.id, i.tenant_id, i.employee_id, i.expires_at, i.used_at,
               i.cancelled_at
        FROM public.employee_invites AS i
        WHERE i.code_hash = p_code_hash;
    $$;
-- +goose StatementEnd

-- EN-AZ-AYRICALIK: 00009'daki uc satir birebir tekrarlanir -- DROP fonksiyonun
-- sahipligini ve GRANT'larini da dusurdugu icin bunlar YENIDEN kurulmalidir.
-- Atlanirsa fonksiyon tappa_owner'a (superuser) ait olur ve govdesi tum DB'de
-- RLS'i atlar: ADR 0002 madde 7'nin acikca yasakladigi genel bypass.
ALTER FUNCTION resolve_invite_by_code_hash(text) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION resolve_invite_by_code_hash(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION resolve_invite_by_code_hash(text) TO tappa_app;

-- +goose Down

-- Sira: once fonksiyonu 00009'daki hALINE geri koy (cancelled_at'a atif
-- birakmadan), sonra sutunu dusur. Ters sirada yapilirsa DROP COLUMN fonksiyonun
-- govdesini bozardi.
DROP FUNCTION IF EXISTS resolve_invite_by_code_hash(text);

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
    SET search_path = pg_catalog, pg_temp
    AS $$
        SELECT i.id, i.tenant_id, i.employee_id, i.expires_at, i.used_at
        FROM public.employee_invites AS i
        WHERE i.code_hash = p_code_hash;
    $$;
-- +goose StatementEnd

ALTER FUNCTION resolve_invite_by_code_hash(text) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION resolve_invite_by_code_hash(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION resolve_invite_by_code_hash(text) TO tappa_app;

-- Sutun grant'i sutunla birlikte duser ama ACIKCA geri alinir: bir sutun tekrar
-- eklenirse eski grant'in hayalet olarak donmeyecegini gormek isteriz.
REVOKE SELECT (cancelled_at) ON employee_invites FROM tappa_resolver;

-- 00009'un GRANT'ini geri yukle: used_at TEK BASINA.
REVOKE UPDATE ON employee_invites FROM tappa_app;
GRANT UPDATE (used_at) ON employee_invites TO tappa_app;

ALTER TABLE employee_invites DROP COLUMN cancelled_at;
