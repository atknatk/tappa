-- locations.sql -- tenant-scoped location reads for proof-of-place, plus the one
-- read the activation page needs. EVERY query here carries an explicit tenant_id
-- filter (CLAUDE.md section 4.5, belt + braces on RLS).
--
-- WHY wifi_ssid IS IN THE TWO PROOF-OF-PLACE QUERIES BELOW, since it is NOT
-- proof of anything (00010): because leaving it out is the change, not putting it
-- in. Both column lists mirror the whole table, which is what makes sqlc map them
-- onto the shared `store.Location` model. Measured on 00010 before this edit: with
-- wifi_ssid missing from the lists, sqlc v1.28 stopped using Location and emitted
-- two near-identical ten-field structs instead (GetLocationByIPRow,
-- ListLocationsForTenantRow), changing both function signatures. Naming the column
-- keeps the canonical mapping and costs one text field per row. NOTHING ON THE TAP
-- PATH READS IT -- the decision engine takes static_ips and gps_lat/gps_lng
-- (section 5, row 6); wifi_ssid is display data, and 00010 explains at length why
-- it must never become a decision input.

-- name: GetLocationByIP :one
-- Proof-of-place, IP side (CLAUDE.md section 5, row 6): the location whose
-- static_ips contains the request source IP. static_ips is cidr[]; the source is
-- matched with @src::inet <<= ANY(static_ips), which is TRUE when the address
-- falls inside any configured range (a single IP is a /32, an ISP block a /29,
-- etc.). An unconfigured location has static_ips = '{}', so <<= ANY('{}') is
-- cleanly FALSE and it never matches (proof falls back to GPS).
SELECT id, tenant_id, name, static_ips, gps_lat, gps_lng, shift_start, shift_end,
       overnight, created_at, wifi_ssid
FROM locations
WHERE tenant_id = @tenant_id
  AND @src::inet <<= ANY(static_ips);

-- name: ListLocationsForTenant :many
-- GPS fallback path (CLAUDE.md section 5, row 6): when no IP matches, the domain
-- computes the haversine distance from the tap's GPS to each location and checks
-- the < 150 m radius. Returns every location in the tenant so that check can run.
SELECT id, tenant_id, name, static_ips, gps_lat, gps_lng, shift_start, shift_end,
       overnight, created_at, wifi_ssid
FROM locations
WHERE tenant_id = @tenant_id
ORDER BY name;

-- name: GetLocationWiFi :one
-- THE ACTIVATION READ (M5-02 phase B / Q14): the network name the page asks the
-- employee to join, for ONE location. Separate from the two queries above on
-- purpose -- neither of them is the activation path. GetLocationByIP is keyed by
-- the source IP, which is exactly what the employee does not have yet at the
-- moment we ask them to join the network, and ListLocationsForTenant returns the
-- whole tenant when the page needs one row.
--
-- KEYED BY LOCATION ID, AND THAT ID IS SERVER-SIDE. The activation flow already
-- holds it: ConsumeInviteAndActivate (db/queries/invites.sql) RETURNS
-- e.location_id for the employee it just activated. So phase B passes a value the
-- database produced, not one the client chose. Stated as intent for the caller,
-- NOT as a guarantee this query can enforce -- it cannot see where its argument
-- came from. What it does enforce is the tenant boundary: the explicit tenant_id
-- predicate plus the RLS policy mean an id from another tenant returns no row
-- (measured in internal/db/locations_test.go), so the worst a wrong id can do is
-- name another location OF THE SAME TENANT.
--
-- NARROW ON PURPOSE: id, tenant_id, name, wifi_ssid. static_ips, GPS and the
-- shift columns are not returned because the activation page has no use for them
-- -- the same "no more columns than needed" rule the resolver functions follow
-- (00003/00004/00009). name comes along so the page can say WHICH venue's network
-- it means; a venue with several locations would otherwise show a bare SSID.
--
-- wifi_ssid IS NULL means "this location has no network to show" (00010) and the
-- page skips the step. That is not an error and must not be rendered as one: a
-- skipped WiFi step costs the IP half of proof-of-place on later taps (section 5,
-- row 6), it does not cost the activation.
--
-- SECOND CALLER (M5-04): the TAP PAGE names the venue an employee just tapped
-- (internal/domain/tenant.Directory.TapPage). It reads `name` and ignores
-- wifi_ssid. The "server-side id" intent above still holds and is worth
-- restating for that path: the id comes from resolving the TAG (resolve_tag_by_uid
-- maps a plaque uid to its location), so the client supplies a uid and the
-- DATABASE supplies the location. The tenant is the SESSION's, not the tag's, so
-- a plaque belonging to another tenant returns NO ROW -- which is how the tap
-- page avoids rendering one tenant's venue name to another tenant's employee.
-- That is a DISCLOSURE choice, not the isolation decision: whether such a tap is
-- allowed is sys:tenant-mismatch's answer at POST time (hand-off N5).
SELECT id, tenant_id, name, wifi_ssid
FROM locations
WHERE tenant_id = @tenant_id
  AND id = @id;

-- name: GetLocationForTap :one
-- PROOF OF PLACE FOR ONE LOCATION -- the TAPPED one (M5-05). This is the third
-- proof-of-place query and the three do not overlap: GetLocationByIP asks "which
-- location does this address belong to" (a search), ListLocationsForTenant asks
-- "where are all of them" (a scan), and this asks "what is the evidence AT the
-- plaque that was actually touched".
--
-- WHY THE TAPPED LOCATION AND NOT A SEARCH. §5 matches a tap against the venue
-- whose plaque is in front of the person, which the TAG resolves to -- never
-- against whichever venue happens to own the source address, and never against
-- the employee's profile location (a chain moves people between branches). So the
-- id comes from resolving the tag, the DATABASE supplies it, and the caller
-- supplies only a uid.
--
--   * static_ips  -> the IP half of proof of place (50 of 100 trust points). An
--                    unconfigured location carries '{}' and simply never matches,
--                    which drops the tap to the GPS path rather than failing it.
--   * gps_lat/lng -> the backup half. numeric, never float (section 6).
--   * shift_*     -> the LOCATION shift, used for lateness when the employee has
--                    no department shift (§5, M4-05). Nullable: a location with
--                    no shift means lateness is not computed, not that it is zero.
--
-- NO ROW is returned for an id belonging to another tenant -- the explicit
-- tenant_id predicate plus RLS (section 4.5, belt and braces). That is NOT how a
-- cross-tenant tap is refused: refusing it is sys:tenant-mismatch's decision and
-- it must be RECORDED (hand-off N5). All this does is make sure the decision is
-- never made on another tenant's evidence.
SELECT id, tenant_id, name, static_ips, gps_lat, gps_lng,
       shift_start, shift_end, overnight
FROM locations
WHERE tenant_id = @tenant_id
  AND id = @id;
