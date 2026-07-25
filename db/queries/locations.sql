-- locations.sql -- tenant-scoped location reads for proof-of-place. Both queries
-- carry an explicit tenant_id filter (CLAUDE.md section 4.5, belt + braces on RLS).

-- name: GetLocationByIP :one
-- Proof-of-place, IP side (CLAUDE.md section 5, row 6): the location whose
-- static_ips contains the request source IP. static_ips is cidr[]; the source is
-- matched with @src::inet <<= ANY(static_ips), which is TRUE when the address
-- falls inside any configured range (a single IP is a /32, an ISP block a /29,
-- etc.). An unconfigured location has static_ips = '{}', so <<= ANY('{}') is
-- cleanly FALSE and it never matches (proof falls back to GPS).
SELECT id, tenant_id, name, static_ips, gps_lat, gps_lng, shift_start, shift_end,
       overnight, created_at
FROM locations
WHERE tenant_id = @tenant_id
  AND @src::inet <<= ANY(static_ips);

-- name: ListLocationsForTenant :many
-- GPS fallback path (CLAUDE.md section 5, row 6): when no IP matches, the domain
-- computes the haversine distance from the tap's GPS to each location and checks
-- the < 150 m radius. Returns every location in the tenant so that check can run.
SELECT id, tenant_id, name, static_ips, gps_lat, gps_lng, shift_start, shift_end,
       overnight, created_at
FROM locations
WHERE tenant_id = @tenant_id
ORDER BY name;
