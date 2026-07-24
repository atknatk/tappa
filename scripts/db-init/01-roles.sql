-- Tappa — rol ayrimi.
--
-- NEDEN: Row-Level Security, tablonun SAHIBI ve BYPASSRLS yetkisi olan roller
-- icin sessizce atlanir. Uygulama bu roller ile baglanirsa RLS hicbir sey
-- korumaz ve tenant izolasyonu kagit uzerinde kalir (bkz. CLAUDE.md §6).
--
--   tappa_owner : semanin sahibi. SADECE migration calistirir.
--   tappa_app   : uygulamanin baglandigi rol. Tablo sahibi DEGIL,
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
