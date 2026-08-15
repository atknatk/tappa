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

-- Migration'larin bundan sonra yaratacagi her tabloda tappa_app'e DML yetkisi.
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO tappa_app;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO tappa_app;

-- Kimlik dogrulama gerektiren uzantilar.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;
