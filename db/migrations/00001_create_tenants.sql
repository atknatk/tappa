-- +goose Up

-- tenants: multi-tenant hiyerarsisinin koku. Diger her tablo buraya tenant_id
-- ile baglanir; bu tablonun kendi kapsam anahtari `tenant_id` degil `id`'dir
-- (ADR 0002 madde 5). Politikasiz bir tenants tablosu tum musteri isimlerini
-- ve VAT numaralarini sizdirir -- bu yuzden burasi da RLS ile korunur.
CREATE TABLE tenants (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name          text NOT NULL,
    -- VAT global UNIQUE: her firma tekil bir VAT ile kayit olur ve ayni firma
    -- iki kez kayit olamaz. NOT NULL cunku kayitta VAT zorunludur (handoff
    -- section 11 -- gercek isletme filtresi + fatura on-dolumu). UNIQUE kisiti
    -- RLS'in ALTINDA calisir (tum tenant'lar arasi tekillik), ki istenen budur.
    -- Format/VIES dogrulamasi UYGULAMA katmanindadir, DB'ye VIES bilgisi girmez.
    vat_number    text NOT NULL UNIQUE,
    -- Isletme tipi -- kayit sihirbazinin cip kumesi (handoff section 9).
    business_type text NOT NULL CHECK (business_type IN (
        'restaurant', 'cafe', 'bar', 'kiosk',
        'retail', 'hotel', 'production', 'other'
    )),
    -- single = tek lokasyon, multi = coklu lokasyon. Enum tipi yerine CHECK:
    -- goose Down'da enum tipini ayrica dusurmek fazladan istir, CHECK daha temiz.
    structure     text NOT NULL CHECK (structure IN ('single', 'multi')),
    -- Ticari durum: founding (ilk musteriler -- 3 ay ucretsiz + omurluk sabit
    -- fiyat) vs standard (handoff section 11). M6-12 founding offer'in 3. ay
    -- uyarisini bu alandan okur; serbest metin bir yazim hatasiyla o billing
    -- uyarisini sessizce bozardi, bu yuzden CHECK ile sinirlandi. Default
    -- founding: MVP'de var olan tenant'lar (iki design partner) founding'dir.
    plan          text NOT NULL DEFAULT 'founding'
                       CHECK (plan IN ('founding', 'standard')),
    -- IANA zaman dilimi adi (Q01, ornek: Europe/Malta). Vardiya cozumu ve
    -- render bunu kullanir; DB'de her sey timestamptz UTC kalir -- bu alan
    -- yalnizca yerel duvar saatini yorumlamak icindir.
    timezone      text NOT NULL DEFAULT 'Europe/Malta',
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- Kolon + indeks maddeleri: `id` PRIMARY KEY hem NOT NULL hem indeks saglar,
-- dolayisiyla ayri bir kapsam indeksi bu tabloda gereksizdir (kapsam anahtari
-- zaten PK). Bu, redline R5'in `tenants` istisnasinin dayandigi gerekcedir.

ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
-- FORCE: tablonun sahibi bile politikaya tabi olsun (superuser haric -- M0-03).
ALTER TABLE tenants FORCE ROW LEVEL SECURITY;

-- Politika ifadesi ADR 0002 madde 3 / Q27 uyarinca NORMATIF ve birebir:
-- baglam yokken GUC ya NULL'dur (hic yazilmamis) ya '' (bir kez yazilip
-- tx bitmis); NULLIF her ikisini de NULL'a cevirir -> hicbir satir eslesmez
-- (fail-closed). Ciplak ::uuid cast YASAK: bos dize uzerinde hata firlatir,
-- davranis baglantinin gecmisine baglanir. tenants'ta kapsam anahtari `id`.
CREATE POLICY tenants_tenant_isolation ON tenants
    USING      (id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- tappa_app DML yetkisi. db-init'teki ALTER DEFAULT PRIVILEGES devrede olsa da
-- acik GRANT yazilir (CLAUDE.md section 6; redline R5 bunu tablo bazinda arar).
GRANT SELECT, INSERT, UPDATE, DELETE ON tenants TO tappa_app;

-- +goose Down

-- DROP TABLE, tabloya bagimli tum nesneleri birlikte dusurur: politika, RLS
-- ayarlari ve tablo-duzeyi GRANT'lar. db-init'teki ALTER DEFAULT PRIVILEGES
-- bu migration'a ait degildir ve etkilenmez.
DROP TABLE tenants;
