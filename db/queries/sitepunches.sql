-- sitepunches.sql -- the SITE's reads of its projection (M11 API-1c, ADR 0027 §9): what a
-- holding member sees of a peer's employees' taps on its own plaques and at its own venues.
--
-- 🔴 ORDINARY, ROW-LEVEL-SECURED, TENANT-FILTERED READS (ADR 0027 §9: "KF'nin bütün okumaları
-- sıradan, RLS'li, açık tenant_id filtreli sorgulardır -- KF hiçbir holding_* fonksiyonunu izdüşüm
-- için çağırmaz"). The tenant is the panel session's; the join to locations re-states it.
--
-- 🔴 WHAT NONE OF THEM CAN SELECT: sun_valid (tappa_app holds no SELECT on it -- K-1h, migration
-- 00046), and nothing the projection does not carry (no coordinate, no address, no note, no
-- policy context, no verdict, no matched rule, no e-mail -- the table has no such column).
--
-- 🔴 NO HOURS ARE COMPUTED FROM THESE ROWS, ANYWHERE (ADR 0027 §9, 10th round: the projection
-- does not see the employer's chain between two taps, and pairing "the latest in with the
-- earliest out" counted 9 h and 32 h intervals nobody worked). The employer computes its
-- employees' hours; the site lists the taps.

-- name: ListSitePunchesForPeriod :many
-- The holding staff's taps at this business's venues in [from_at, until_at) by the DECLARED
-- time -- the window the reports section reads its own staff by -- one row per employer record,
-- its LAST outcome ("transaction_id başına son satır": an approval or a rejection supersedes the
-- pending_review it decides; a decision outranks a pending row written in the same transaction).
-- Ordered by time, the earliest first -- or, with newest_first (the Group section's "recent" list,
-- GRP-1), the latest first, so a capped read keeps the newest rows; the limit is a bound on the
-- request, one row more than the screen lists so it can say "showing N of M".
SELECT f.transaction_id, f.employer_api_code, f.employee_id, f.employee_ref, f.employee_code, f.employee_name,
       f.site_location_id, f.location_name, f.site_tag_uid, f.occurred_at, f.channel, f.type,
       f.outcome, f.created_at
FROM (
    SELECT DISTINCT ON (s.transaction_id)
           s.transaction_id, s.employer_api_code, s.employee_id, s.employee_ref, s.employee_code, s.employee_name,
           s.site_location_id, l.name AS location_name, s.site_tag_uid, s.occurred_at, s.channel, s.type,
           s.outcome, s.created_at
    FROM site_punches s
    LEFT JOIN locations l ON l.id = s.site_location_id AND l.tenant_id = s.tenant_id
    WHERE s.tenant_id = @tenant_id
      AND s.occurred_at >= @from_at
      AND s.occurred_at < @until_at
    ORDER BY s.transaction_id, s.created_at DESC,
             CASE s.outcome WHEN 'approved' THEN 0 WHEN 'rejected' THEN 0 ELSE 1 END
) AS f
ORDER BY CASE WHEN sqlc.arg('newest_first')::boolean THEN f.occurred_at END DESC,
         f.occurred_at, f.transaction_id
LIMIT sqlc.arg('row_limit')::int;

-- name: ListSiteLostPlaqueAlerts :many
-- This business's LOST or retired plaques a peer's employee touched, in [from_at, to_at) by the
-- SERVER's clock (created_at -- ADR 0027 §9, 11th round: one forged URL declaring 2099 must
-- neither move an alert nor hide one). One row per plaque: how many refusals reached this
-- business (the dedup already collapsed repeats of one URL within the hour) and the last time.
-- The panel's alert surface for a tap the EMPLOYER refused (ADR 0027 §5: no audit row crosses
-- tenants; this row is the site's only trace).
SELECT s.site_tag_uid::text                                   AS tag_uid,
       l.name                                                 AS location_name,
       count(*)                                               AS refusals,
       max(s.created_at)::timestamptz                         AS last_at
FROM site_punches s
LEFT JOIN locations l ON l.id = s.site_location_id AND l.tenant_id = s.tenant_id
WHERE s.tenant_id = @tenant_id
  AND s.outcome = 'refused_tag_not_active'
  AND s.site_tag_uid IS NOT NULL
  AND s.created_at >= @from_at
  AND s.created_at < @to_at
GROUP BY s.site_tag_uid, l.name
ORDER BY max(s.created_at) DESC, s.site_tag_uid
LIMIT @row_limit;

-- name: ListSiteTagLastSeen :many
-- "Last seen" of this business's plaques from the holding staff's taps -- by the SERVER's clock
-- (created_at), never the declared occurred_at (ADR 0027 §9). The caller keeps the later of this
-- and ListTagLastSeen's stamp. Like that query, a plaque with no row here was never touched by a
-- peer's employee; the GROUP BY makes max() NOT NULL.
SELECT s.site_tag_uid::text            AS tag_uid,
       max(s.created_at)::timestamptz  AS last_seen
FROM site_punches s
WHERE s.tenant_id = @tenant_id
  AND s.site_tag_uid IS NOT NULL
GROUP BY s.site_tag_uid;

-- name: ListSitePlaqueGaps :many
-- The counter gaps of this business's plaques that the holding staff's taps revealed (K-1i: the
-- employer's tap:ctrGap, carried by holding_feed), in [from_at, to_at) by the SERVER's clock.
-- Same shape as ListAnomalyPlaques; the caller adds the two per plaque. records counts the TAPS --
-- an approval or rejection is a later row about the same tap (the API-1c review's E-D4), so it is
-- not counted again.
SELECT s.site_tag_uid::text                                   AS tag_uid,
       l.name                                                 AS location_name,
       count(*) FILTER (WHERE s.outcome NOT IN ('approved', 'rejected')) AS records,
       count(*) FILTER (WHERE s.ctr_gap > 0)                  AS counter_gaps,
       coalesce(max(s.ctr_gap), 0)::int                       AS largest_gap
FROM site_punches s
LEFT JOIN locations l ON l.id = s.site_location_id AND l.tenant_id = s.tenant_id
WHERE s.tenant_id = @tenant_id
  AND s.site_tag_uid IS NOT NULL
  AND s.created_at >= @from_at
  AND s.created_at < @to_at
GROUP BY s.site_tag_uid, l.name
HAVING count(*) FILTER (WHERE s.ctr_gap > 0) > 0
ORDER BY largest_gap DESC, counter_gaps DESC, s.site_tag_uid
LIMIT @row_limit;
