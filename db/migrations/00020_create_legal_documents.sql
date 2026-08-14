-- +goose Up

-- Migration 0020 -- M7-06: the four LEGAL TEXTS, entered from a form.
--
-- WHAT WAS MISSING. M7-01 shipped /legal/privacy, /legal/terms, /legal/imprint and
-- /legal/cookies as SKELETONS: each page states what it will contain, says plainly
-- that nothing on it is in force, and prints the list of facts it is waiting for.
-- Three sessions later the facts had still not arrived, because the only way to
-- supply them was to edit Go source. This table is the other end of a form.
--
-- 🔴 IT CARRIES NO tenant_id, AND THAT IS THE POINT RATHER THAN AN OMISSION.
-- CLAUDE.md section 6 says every new table is born with the five, and that rule is
-- written for TENANT DATA -- rows that belong to one customer and must be invisible
-- to the next. These four documents belong to nobody's tenant: they are Tappa's own
-- published text, identical for every reader, served from a page that has no
-- identity at all. A tenant_id here would have to be filled with SOMETHING, and
-- every candidate is worse than none:
--
--   * the writing admin's own tenant -- then the privacy policy is owned by
--     whichever customer's account happened to publish it, and the public page
--     would have to name that customer's uuid to read it back;
--   * a dedicated "Tappa" tenant -- then the operator, whose admin account lives in
--     a CUSTOMER tenant, has to write ACROSS tenants, which means a handler
--     reachable by a customer identity that calls WithTenant() with a tenant that is
--     not the caller's. That capability is exactly what section 4.5 exists to prevent
--     and it must never be born. Measured and rejected; see docs/adr/0016.
--
-- So the exemption is declared out loud, in the syntax scripts/redline-check.sh
-- already understands, and it prints as a WARN on EVERY run -- an exemption cannot
-- be silent. This is the FIRST use of that waiver in this repository; the mechanism
-- has existed since the script was written and has had nothing to waive until now.
--
-- ⚠️ THE WAIVER SKIPS THE WHOLE FIVE-CHECK, so RLS below is written VOLUNTARILY and
-- is pinned by a test in the live catalog rather than by the script.
-- redline: no-tenant-scope(legal_documents) — Tappa'nin kendi yayimlanmis yasal
-- metni; hicbir tenant'a ait degil, herkese acik /legal/* yolundan okunur, ve bir
-- tenant_id koymak capraz-tenant yazma yetenegi dogururdu (ADR 0016).
--
-- RED LINES:
--   section 4.3  APPEND-ONLY. A published legal text that can be edited in place has
--                no history, and "what did the policy say on the day of the
--                complaint" is the only question anybody will ever ask of it. A
--                correction is a NEW ROW, which is the same remedy transactions,
--                audit_log, policy_versions and billing_periods use. Belt and
--                braces: REVOKE UPDATE, DELETE from tappa_app (privilege) AND the
--                BEFORE UPDATE OR DELETE trigger from 0005, which binds the table
--                owner too.
--   section 4.5  No tenant scope, declared above; and the absence of the column is
--                what makes a cross-tenant read structurally impossible here --
--                there is no column to compare and no tenant to name.
--   section 4.7  No secret is stored. The body is text somebody typed to be
--                PUBLISHED; there is nothing here that a log must not carry.


CREATE TABLE legal_documents (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- slug: KAPALI sozluk, ve kapali olmasi yapisal. Her deger web/templates/pages
    -- LegalPages'teki bir yola karsilik gelir; besinci bir deger yolu olmayan bir
    -- belge yaratirdi. Yeni belge = yeni migration + yeni sayfa, ikisi birlikte.
    slug         text NOT NULL
                      CHECK (slug IN ('privacy', 'terms', 'imprint', 'cookies')),
    -- body: metnin KENDISI, kullanicinin yazdigi gibi. Paragraf bolme render
    -- katmanindadir (internal/domain/legal.Paragraphs); burada saklanan sey girdi,
    -- turetilmis bicim degil -- iki temsil olmasin.
    --
    -- CHECK: bosluktan ibaret bir govde "yayimlandi" demek olurdu ve sayfa o zaman
    -- ne taslak uyarisini basar ne de metin -- gorunur sekilde BOS kalirdi, yani
    -- kirik bir sayfadan ayirt edilemezdi. Yayimlamak icin yazacak bir sey gerekir.
    body         text NOT NULL CHECK (btrim(body) <> ''),
    published_at timestamptz NOT NULL DEFAULT now(),
    -- published_by: metni yayimlayan admin_users.id. FK YOK, ve bu bilincli --
    -- audit_log.actor_id ile ayni gerekce (00005): admin_users tenant kapsamli ve
    -- RLS altinda; tenant kapsamsiz bir tablodan ona bir FK, FK denetiminin RLS'i
    -- gormemesi sayesinde "bu uuid var mi" sorusunu cevaplayan bir CAPRAZ-TENANT
    -- VARLIK KEHANETI olurdu (tahmin edilen id ile INSERT -> 23503 ya da basari).
    -- Satirin kendi provenance'i burada; tenant kapsamli idari iz audit_log'da.
    published_by uuid
);

-- "Her slug'in EN SON surumu" tek sorgudur ve indeksi budur. DESC: okuma yolu
-- daima en yeniyi ister; ekleme yolu sadece ekler.
CREATE INDEX legal_documents_slug_published_idx
    ON legal_documents (slug, published_at DESC);

-- RLS: MUAFIYETE RAGMEN acilir, ama NE YAPTIGI DAR YAZILIYOR. Politika herkese
-- okuma verir (metin zaten kamuya acik bir sayfada basiliyor) ve eklemeye izin
-- verir. Politikanin isi burada tenant ayirmak DEGIL -- ayiracak tenant yok --
-- yalnizca tablonun RLS'siz kalmamasi.
--
-- 🔴 YAZMA TARAFINDA VERITABANI DERINLIGI YOKTUR, VE BU MUAFIYETIN GERCEK FIYATI.
-- Bir onceki surumu burada "yazma yetkisi GRANT'ta" diyordu ve bu YANILTICIYDI:
-- GRANT kimseyi kimseden ayirmaz, cunku ayiracak bir tenant sutunu yok. Olculdu
-- (BEGIN … ROLLBACK, yabanci bir tenant baglaminda, tappa_app olarak):
-- `INSERT INTO legal_documents` BASARILI (50 -> 51), ayni baglamda `admin_users`
-- ve `tenants` 0 satir. Yani KIM YAZABILIR sorusunun TEK cevabi uygulama
-- katmanindaki izin listesidir (TAPPA_OPERATOR_ADMIN_IDS -> admin_users.id;
-- internal/handler.mayPublishLegal). Urunun her yerinde olan kusak+kemer
-- (§4.5: RLS + acik tenant filtresi) BURADA YOK ve olamaz -- tenant kapsamsiz bir
-- tablonun kacinilmaz sonucu budur. Karsiliginda alinan sey ADR 0016'da: capraz-
-- tenant YAZMA yetenegi hic dogmuyor.
--
-- NE HALA KORUYOR: append-only (asagidaki REVOKE + 0005 trigger'i), yani yanlis
-- yazilan bir satir GIZLENEMEZ; ve `slug` CHECK'i, yani yazilabilecek belge kumesi
-- kapali.
ALTER TABLE legal_documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_documents FORCE ROW LEVEL SECURITY;

CREATE POLICY legal_documents_public ON legal_documents
    USING      (true)
    WITH CHECK (true);

-- YETKILER SUTUN DUZEYINDE, ve bunun sebebi guvenlik denetiminde olculdu.
--
-- DIKKAT (M1-04 dersi): db-init'teki ALTER DEFAULT PRIVILEGES her yeni tabloya dort
-- yetkiyi birden verir; GRANT'tan cikarmak TEK BASINA YETMEZ. Once hepsi REVOKE
-- edilir, sonra yalnizca gerekenler verilir.
--
-- 🔴 `published_by` YAZILABILIR AMA OKUNAMAZ, VE BU BIR CAPRAZ-TENANT KEHANETINI
-- KAPATIR. Tablo tenant kapsamsiz ve politikasi `USING (true)`, yani her tenant'in
-- baglantisi her satiri gorur. Bir denetim olctu: yabanci bir tenant baglaminda
-- `SELECT published_by FROM legal_documents` 37 satir dondururken ayni baglamda
-- `admin_users` 0 satir donuyordu -- yani sutun, baska bir tenant'in admin uuid'sini
-- sizdiran zayif bir varlik kehanetiydi. Bu, ADR 0016 §4'un FK'yi reddettigi
-- gerekcenin ta kendisi; ham sutun onu daha zayif bir bicimde yine sagliyordu.
-- Cozum: uygulama rolu sutunu YAZAR (provenance korunur, adli bir okuma tappa_owner
-- ile yapilir) ama HIC OKUYAMAZ. db/queries/legal.sql da onu hicbir yerde secmez.
REVOKE ALL ON legal_documents FROM tappa_app;
GRANT SELECT (id, slug, body, published_at) ON legal_documents TO tappa_app;
GRANT INSERT (slug, body, published_by)     ON legal_documents TO tappa_app;

-- Ayricalik yetmez: tablo sahibi (tappa_owner) REVOKE'u gormez. 0005'in paylasilan
-- fonksiyonu, transactions/audit_log/policy_versions/billing_periods ile ayni.
CREATE TRIGGER legal_documents_append_only
    BEFORE UPDATE OR DELETE ON legal_documents
    FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation();


-- +goose Down

-- DROP TABLE tabloya bagli politika, RLS, GRANT, indeks ve TRIGGER'i birlikte
-- dusurur. tappa_forbid_mutation() PAYLASILAN bir fonksiyondur ve 0005'e aittir --
-- burada DROP EDILMEZ.
DROP TABLE IF EXISTS legal_documents;
