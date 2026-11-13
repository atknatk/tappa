-- 00103 -- T173 EC-1 (ADR 0044 §5.9; the user's decision K-E1 (iii) (a), 2026-11-12: one day): the address
-- change links are swept. They carry an address -- the new one in a 'change' row, the old one in a
-- 'revert' row (00101) -- and nothing deleted them.
--
-- identity_sweep_events() is redefined with the SAME signature (CREATE OR REPLACE; owner, SECURITY
-- DEFINER, search_path, REVOKE / GRANT restated -- ADR 0031 §5.2's T5 rule): 00072's body byte for byte,
-- plus one DECLARE line and the three DELETEs of the 'change' / 'revert' rows spent, cancelled or expired
-- more than a day ago. A new sweep function would change ADR 0031 §6.1's "two fixed-period sweeps"; the
-- widened one does not. Its answer stays the event log's count (the signature is unchanged and its caller
-- -- EC-2 wires it into cmd/tappa/main.go, T294 -- reads that). ADR 0031 §5.1's "no deletion" names this
-- table's second deleter in its dated note of 2026-11-12 (identitybelt_test.go's identityPersonTableDeleters).
--
-- THE GRANT: DELETE on admin_identity_verifications to tappa_identitydefiner, with the sweep that uses it
-- (the card's item 10). Its WHERE reads address, used_at, cancelled_at and expires_at -- the definer's
-- SELECT cells since 00072 / 00101. Nothing to tappa_app or tappa_resolver.
--
-- EXPAND-SAFE: the binary before this file never calls the sweep (T294).
--
-- THE DOWN REFUSES with 00101's block (ADR 0044 §6: goose runs this Down first; without its own block
-- `down-to 100` would commit it -- and 00102's -- before 00101 stops). Otherwise 00072's body back, byte
-- for byte, and the DELETE taken back.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.identity_revert_email_change(text)') IS NULL
       OR pg_catalog.to_regprocedure('public.identity_sweep_events()') IS NULL THEN
        RAISE EXCEPTION 'migration 00103 (T173 EC-1) needs 00102 (the e-mail change functions) and 00072''s identity_sweep_events. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

GRANT DELETE ON admin_identity_verifications TO tappa_identitydefiner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_sweep_events()
    RETURNS TABLE (deleted bigint)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_cutoff timestamptz;
    v_n      bigint;
    v_links  timestamptz; -- 00103 (T173): the address change links' one day
BEGIN
    v_cutoff := pg_catalog.clock_timestamp() - interval '90 days';
    DELETE FROM public.admin_identity_events AS e WHERE e.created_at < v_cutoff;
    GET DIAGNOSTICS v_n = ROW_COUNT;
    -- 00103 (T173, ADR 0044 §5.9, K-E1 (iii) (a)): the address change links -- 'change' (the new
    -- address) and 'revert' (the old one), the rows with an address (00101's CHECK) -- are personal data;
    -- each goes one day after it was spent, cancelled or expired. 'verify' / 'link' rows carry no address
    -- and are left alone. Three statements, one per way a link ends: the belt admits no OR.
    v_links := pg_catalog.clock_timestamp() - interval '1 day';
    DELETE FROM public.admin_identity_verifications AS v WHERE v.address IS NOT NULL AND v.used_at < v_links;
    DELETE FROM public.admin_identity_verifications AS v WHERE v.address IS NOT NULL AND v.cancelled_at < v_links;
    DELETE FROM public.admin_identity_verifications AS v WHERE v.address IS NOT NULL AND v.expires_at < v_links;
    RETURN QUERY SELECT v_n;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_sweep_events() OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_sweep_events() SECURITY DEFINER;
ALTER FUNCTION public.identity_sweep_events() SET search_path = pg_catalog, pg_temp;
REVOKE ALL ON FUNCTION public.identity_sweep_events() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_sweep_events() TO tappa_app;

-- +goose Down
SET LOCAL lock_timeout = '1s';
LOCK TABLE admin_identity_verifications, admin_identity_events IN SHARE MODE;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_identity_verifications AS v
                WHERE v.purpose = 'revert' AND v.used_at IS NULL AND v.cancelled_at IS NULL
                  AND v.expires_at > pg_catalog.clock_timestamp())
       OR EXISTS (SELECT 1 FROM public.admin_identity_events AS e
                   WHERE e.event IN ('email_change_requested', 'email_changed', 'email_reverted')) THEN
        RAISE EXCEPTION 'migration 00103 Down: an e-mail change can still be undone, or admin_identity_events holds an e-mail change event (it ages out after 90 days); the way back is the previous binary, which runs on this schema (ADR 0044 6). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- 00072's identity_sweep_events, byte for byte, its properties restated.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_sweep_events()
    RETURNS TABLE (deleted bigint)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_cutoff timestamptz;
    v_n      bigint;
BEGIN
    v_cutoff := pg_catalog.clock_timestamp() - interval '90 days';
    DELETE FROM public.admin_identity_events AS e WHERE e.created_at < v_cutoff;
    GET DIAGNOSTICS v_n = ROW_COUNT;
    RETURN QUERY SELECT v_n;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_sweep_events() OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_sweep_events() SECURITY DEFINER;
ALTER FUNCTION public.identity_sweep_events() SET search_path = pg_catalog, pg_temp;
REVOKE ALL ON FUNCTION public.identity_sweep_events() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_sweep_events() TO tappa_app;

REVOKE DELETE ON admin_identity_verifications FROM tappa_identitydefiner;
