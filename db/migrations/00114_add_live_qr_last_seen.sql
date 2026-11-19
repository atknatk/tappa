-- 00114 -- M16-20 (ADR 0045 §2.5; the security lens's ORTA-1): live_qr_displays.last_seen_at -- when
-- a paired screen last asked for a code, at most every ten minutes.
--
-- WHY. A screen's session lives in one HttpOnly cookie, and a copy of it works anywhere: before this
-- column nothing told a manager whether a screen was still standing at the venue, had been switched
-- off, or was polling from somewhere it should not. The panel now prints "last seen" beside each
-- screen (internal/handler/liveqrpanel.go); together with the server-side lifetime of a screen
-- session (internal/liveqr.ScreenSessionLifetime, 180 days from paired_at) it bounds and exposes a
-- copied cookie. It is NOT evidence: it is when the screen polled, not who scanned it.
--
-- WRITTEN SPARINGLY. The screen polls every ~17 s; the stamp is moved by ONE conditional UPDATE
-- (db/queries/liveqr.sql TouchLiveQRDisplay) only when it is NULL or older than ten minutes, so one
-- screen writes at most six rows an hour, never a write per poll. Only a paired, unrevoked screen is
-- stamped (the CHECK below holds the "paired" half; the query the other).
--
-- tappa_app reads it and updates it, by column (ADR 0028 §1's `arwd` rule), and nothing else of the
-- table's grants changes: tappa_resolver does not read it (no resolver returns it), and it is not one
-- of the four state columns the one-way trigger guards (00111) -- it carries no state.
--
-- 🔴 lock_timeout 1 s, the FIRST statement of the Up and of the Down (00054's rule): ADD COLUMN and
-- DROP COLUMN take ACCESS EXCLUSIVE on live_qr_displays; behind a long reader the file gives up with
-- 55P03 instead of queueing every screen's poll behind it.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.live_qr_displays') IS NULL THEN
        RAISE EXCEPTION 'migration 00114 (M16-20) adds a column to live_qr_displays (00111), which is not there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_attribute
                WHERE attrelid = 'public.live_qr_displays'::pg_catalog.regclass
                  AND attname = 'last_seen_at' AND NOT attisdropped) THEN
        RAISE EXCEPTION 'migration 00114 (M16-20): live_qr_displays.last_seen_at is already there. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE public.live_qr_displays ADD COLUMN last_seen_at timestamptz;
-- A screen is seen only once it is paired, and never before its pairing.
ALTER TABLE public.live_qr_displays ADD CONSTRAINT live_qr_displays_seen_after_paired
    CHECK (last_seen_at IS NULL OR (paired_at IS NOT NULL AND last_seen_at >= paired_at));

GRANT SELECT (last_seen_at), UPDATE (last_seen_at) ON public.live_qr_displays TO tappa_app;

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_column_privilege('tappa_resolver', 'public.live_qr_displays', 'last_seen_at', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.live_qr_displays', 'last_seen_at', 'INSERT')
       OR NOT pg_catalog.has_column_privilege('tappa_app', 'public.live_qr_displays', 'last_seen_at', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.live_qr_displays', 'session_hash', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00114 (M16-20): tappa_app must read and update last_seen_at and never insert it, never read session_hash, and tappa_resolver must not read last_seen_at -- through PUBLIC or a role membership. Fix it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- The column is a poll's timestamp, not evidence (no tap reads it), so the Down drops it with rows in
-- the table -- unlike 00111's, which refuses: what it would erase is which screen stood where.
SET LOCAL lock_timeout = '1s';
REVOKE SELECT (last_seen_at), UPDATE (last_seen_at) ON public.live_qr_displays FROM tappa_app;
ALTER TABLE public.live_qr_displays DROP CONSTRAINT live_qr_displays_seen_after_paired;
ALTER TABLE public.live_qr_displays DROP COLUMN last_seen_at;
