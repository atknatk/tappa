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

CREATE ROLE tappa_app LOGIN PASSWORD 'tappa' NOBYPASSRLS NOSUPERUSER NOCREATEDB NOCREATEROLE;

GRANT CONNECT ON DATABASE tappa TO tappa_app;
GRANT USAGE ON SCHEMA public TO tappa_app;

-- Migration'larin bundan sonra yaratacagi her tabloda tappa_app'e DML yetkisi.
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO tappa_app;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO tappa_app;

-- Kimlik dogrulama gerektiren uzantilar.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;
