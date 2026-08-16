-- Tappa -- rol ayrimi.
--
-- NEDEN: Row-Level Security uc durumda atlanir; hangisi ve neden onemli:
--   - SUPERUSER rolleri RLS'i KOSULSUZ atlar; FORCE ROW LEVEL SECURITY bile
--     onlara erisemez.
--   - Bir tablonun SAHIBI RLS'i varsayilan olarak atlar; AMA tablo FORCE ROW
--     LEVEL SECURITY ile isaretlenirse sahip de politikalara tabi olur. FORCE
--     yalnizca tablo sahipligini yener, superuser'i degil (M0-03 ile olculdu).
--   - BYPASSRLS yetkisi olan roller RLS'i atlar.
-- Uygulama bu rollerden biri ile baglanirsa RLS hicbir sey korumaz ve tenant
-- izolasyonu kagit uzerinde kalir (bkz. CLAUDE.md section 6, ADR 0002).
--
--   tappa_owner : semanin sahibi, SADECE migration calistirir. initdb'nin
--                 bootstrap SUPERUSER'i (POSTGRES_USER) oldugu icin RLS'i
--                 kosulsuz atlar -> RLS izolasyon testi tappa_app ile kosmali.
--   tappa_app   : uygulamanin baglandigi rol. Tablo sahibi DEGIL, NOSUPERUSER,
--                 NOBYPASSRLS. Her sorgusu RLS politikalarina tabi.
--   tappa_resolver : tenant cozumleme yolunun cevrelenmis (bounded) bypass'i
--                 (ADR 0002 madde 7). NOLOGIN -- kimse bu rol ile baglanmaz;
--                 yalnizca SECURITY DEFINER cozumleme fonksiyonlarinin SAHIBI
--                 olarak var (M1-04 sessions, M1-05 tags). BYPASSRLS oldugu icin
--                 o fonksiyonlarin govdesi bir satiri tenant baglami olmadan
--                 okuyabilir -- AMA patlama yaricapi YAPISAL olarak sinirlidir:
--                 (1) hicbir DEFAULT PRIVILEGE almaz; yalnizca sessions/tags'e
--                 ACIK GRANT SELECT verilir (baska hicbir tabloyu goremez),
--                 (2) tek yuzeyi, anahtar-parametreli ve <=1 satir donduren
--                 SECURITY DEFINER fonksiyonlardir (EXECUTE yalnizca tappa_app'e,
--                 PUBLIC'ten REVOKE). Superuser DEGIL. Neden GUC-anahtar saf-RLS
--                 alternatifi degil de bu secildi: ADR 0002 madde 7 (cevreleme
--                 disipline degil YAPIYA dayanmali).

-- 🔴 NOLOGIN VE PAROLASIZ YARATILIR -- ve bu, OLCULMUS BIR FAIL-OPEN'IN
-- DUZELTMESIDIR (2026-08-15, M8-02 guvenlik denetimi).
--
-- ESKI HALI: `LOGIN PASSWORD 'tappa'`. Uretimde bir sonraki init script'i
-- (deploy/k8s/postgres-init/02-app-password.sh) parolayi degistiriyordu. Ama o
-- script HERHANGI bir sebeple duserse (bos TAPPA_APP_PASSWORD, OOM kill, eviction,
-- node reboot) konteyner exit 1 verir -- PGDATA ise ARTIK DOLUDUR. Pod yeniden
-- baslar, postgres entrypoint'i init'i TAMAMEN atlar, sunucu normal servise girer
-- ve tappa_app REPODA YAZILI parolayla yasar. Kalici ve sessiz.
--
-- Manifestin birebir replikasiyla olculdu:
--   1. TAPPA_APP_PASSWORD="" -> "is empty" -> exit=1
--   2. docker start            (k8s'in yaptigi) -> running
--   3. AYRI BIR KONTEYNERDEN (agdan, yani uygulamanin bagladigi bicimde):
--      PGPASSWORD=tappa psql -U tappa_app -h <db> -Atc "SELECT current_user"
--      -> tappa_app            <-- BAGLANDI, repo parolasiyla
--
-- ⚠️ 3. ADIM ONCE KONTEYNER ICI 127.0.0.1 ILE OLCULMUSTU VE O OLCUM GECERSIZDI:
-- initdb'nin varsayilan pg_hba'si loopback'i `trust` eder, yani orada HER parola
-- calisir ve sonuc hicbir sey kanitlamaz. Sonuc dogruydu, komut onu kanitlamiyordu.
-- Bir dogrulama, tukettigi seyin GORDUGU bicimi gormek zorundadir -- bu repoda ayni
-- ders uc kez bedel odetti (internal/config/config.go, prefixes).
--
-- ⚠️ RLS bunu DURDURMAZ: politikalar tenant GUC'unu okur ve o GUC her rolun KENDI
-- atayabildigi bir session degeridir (ADR 0002). Kimlik bilgisini ele geciren onu
-- kendi oturumunda atar ve o tenant'in tum satirlarini okur. (§4.3 ayakta kalir:
-- transactions'ta UPDATE/DELETE REVOKE'lu ve trigger'li, yani mesai kaydi yine de
-- DEGISTIRILEMEZ.)
--
-- SIMDIKI HALI FAIL-CLOSED: parolayi veren script duserse tappa_app HIC giris
-- yapamaz. Uygulama acilista olur (gurultulu), bilinen bir parolayla yasamaz
-- (sessiz). Girisi acan iki yer var ve ikisi de bu dosyanin DISINDA:
--   uretim     deploy/k8s/postgres-init/02-app-password.sh   (Secret'tan)
--   gelistirme scripts/db-init/02-dev-only-password.sh       (sabit 'tappa')
-- Ikincisi, uretimde mount edilirse ACILISTA REDDEDER -- bkz. o dosyadaki koruma.
CREATE ROLE tappa_app NOLOGIN NOBYPASSRLS NOSUPERUSER NOCREATEDB NOCREATEROLE;

-- Cozumleme fonksiyonlarinin en-az-ayricalikli sahibi. LOGIN yok; blast radius
-- yalnizca kendisine ACIKCA GRANT'lanan tablolarla sinirli (ASAGIDA default
-- privilege VERILMEZ -- tappa_app'inkinden farki budur).
CREATE ROLE tappa_resolver NOLOGIN BYPASSRLS NOSUPERUSER NOCREATEDB NOCREATEROLE;

GRANT CONNECT ON DATABASE tappa TO tappa_app;
GRANT USAGE ON SCHEMA public TO tappa_app;
GRANT USAGE ON SCHEMA public TO tappa_resolver;

-- Migration'larin bundan sonra yaratacagi her tabloda tappa_app'e taban yetki.
--
-- 🔴 UPDATE VE DELETE BILEREK YOK, VE BU M8-02 FAZ E'DE OLCULMUS BIR GUVENLIK
-- DUZELTMESIDIR. Eskiden `GRANT SELECT, INSERT, UPDATE, DELETE` yaziyordu ve her
-- migration istemedigini ACIKCA REVOKE ediyordu (`REVOKE DELETE ON tags`,
-- `REVOKE UPDATE, DELETE ON transactions`, ...). Bu kalip migration'lar icin
-- calisiyor ama BIR GERI YUKLEMEDE COKUYOR:
--
--   Bir pg_dump geri yuklenirken her CREATE TABLE bu varsayilani otomatik uygular,
--   ve pg_dump ACL'i PostgreSQL'in YERLESIK varsayilanina (yalniz sahip) gore
--   hesapladigi icin fazlaligi geri alan REVOKE'lari HIC YAZMAZ. Sonuc olculdu
--   (taze pod, 2026-08-16): kaynak 45 tablo yetkisi, geri yukleme 76 -- yani
--   tappa_app'e 31 FAZLA yetki, ve icinde su ucu vardi:
--
--     tags UPDATE  = true   (kaynakta false)
--     tags DELETE  = true   (kaynakta false)
--     tags.aes_key_ref UPDATE = true (kaynakta false; tablo duzeyi UPDATE sutun
--                                     kisitini EZER -> sarmalanmis AES anahtarlari
--                                     UZERINE YAZILABILIR, section 4.7)
--
--   Ve bu, section 4.3'ten fazlasini dusuruyordu: `tags` uzerinde DELETE + INSERT
--   ile calinmis bir tappa_app kimligi bir plaketi silip last_ctr=0 ile yeniden
--   ekleyebiliyor -- yani section 4.4'un REPLAY korumasi SIFIRLANIYOR.
--   `tags_counter_monotonic` bunu durdurmuyor: yalniz BEFORE UPDATE ve yalniz
--   `new.last_ctr < old.last_ctr` icin ates ediyor, INSERT'te hic calismiyor.
--   (transactions_tag_fk ON DELETE RESTRICT bir sinir koyuyor: yalniz henuz tap
--   kaydi olmayan plaketler silinebilir. Yeni encode edilmis her plaket oyledir.)
--
-- OLCUM, TAHMIN DEGIL -- daraltmanin taze bir kurulumu bozup bozmadigi sinandi:
--   gercek goose imaji ile iki taze kurulum (genis varsayilan / dar varsayilan),
--   20 migration ikisinde de basarili, ve tablo yetkileri ARASINDAKI TEK FARK
--   `goose_db_version` uzerindeki UPDATE+DELETE (goose'un kendi defteri; uygulama
--   ona hic dokunmaz). UYGULAMA TABLOLARININ HEPSI BIREBIR AYNI: 45 = 45.
--   `go test -race -count=1 ./...` dar veritabanina karsi: exit 0, 22 paket ok,
--   0 FAIL, 0 atlanan DB testi.
--
-- ⚠️ SONRAKI MIGRATION'LARI YAZAN ICIN: yeni bir tablo UPDATE ya da DELETE
-- istiyorsa artik ACIKCA GRANT etmek zorunda.
--
-- 🔴 VE BU VAADIN KAPSAMI DARDIR -- BU CUMLE BIR TUR BOYUNCA KOSULSUZ YAZILDI VE
-- ONE FAZLASINI SOYLUYORDU. "Unutulursa gurultulu permission denied alinir
-- (fail-closed)" YALNIZ dar varsayilanin YURURLUKTE OLDUGU veritabanlarinda dogru,
-- ve iki yerde yururlukte DEGIL (ikisi de olculdu, 2026-08-16):
--
--   (a) HALIHAZIRDA CALISAN BIR VERITABANI. Bu bir init script'idir, migration
--       degil: yalniz BOS PGDATA uzerinde kosar. Uretimin ve gelistirme
--       veritabaninin pg_default_acl'i bugun hala `tappa_app=arwd/tappa_owner`.
--
--   (b) 🔴 HER GERI YUKLEMENIN SONU. pg_dump'in URETTIGI DUMP, kendi son
--       satirlarinda varsayilani YENIDEN GENISLETIYOR:
--         ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
--           GRANT SELECT,INSERT,DELETE,UPDATE ON TABLES TO tappa_app;
--       Olculdu: dar init -> `ar`, geri yukleme sonrasi -> `arwd`, ve o anda
--       yaratilan yeni bir tablo `tappa_app=arwd` aliyor. VAR OLAN tablolar
--       etkilenmiyor (geri yukleme sonunda tags UPDATE/DELETE hala false, 45 yetki).
--       Etkiledigi tek sey SONRAKI DDL'dir.
--
--   SOMUT SONUC VE NEDEN ONEMLI: dev/CI dar, uretim ve her geri yuklenmis
--   veritabani genis. Yani DOGRULAMA ORTAMI URETIMDEN KATI, ve ayrim kusuru
--   GIZLER yonde -- REVOKE satirini unutan bir migration yerelde hicbir fark
--   uretmez, uretimde sessizce `arwd` verir. `pg-restore-verify.sh` de bunu
--   goremez: referansi dump'in kendisi, dump ise genis varsayilani ILAN ediyor.
--
--   ⚠️ CARE VAR VE OLCULDU: geri yukleme prosedurunun SON adimi tek bir ifadeyle
--   varsayilani yeniden daraltiyor (`REVOKE UPDATE, DELETE ON TABLES`), var olan
--   tablolara dokunmadan (45 yetki, tags false/false, sekans varsayilani `rU`
--   degismeden). deploy/README.md'nin A YOLU 7. adimi ve B YOLU B3'u onu tasiyor.
--   CALISAN veritabani icin ayni ifade istege bagli bir sertlestirmedir ve
--   deploy/README.md'de sinir olarak sayiliyor -- burada dayatilmiyor, cunku canli
--   bir veritabanini migration disinda degistirmek bir karardir.
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  GRANT SELECT, INSERT ON TABLES TO tappa_app;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO tappa_app;

-- Kimlik dogrulama gerektiren uzantilar.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;
