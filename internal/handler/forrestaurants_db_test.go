package handler

// forrestaurants_db_test.go -- the restaurants page's cover claim, end to end (LP-09):
// "By default, a tap at another venue is judged on that venue's network and position,
// exactly as a tap at home would be." The page's derivation reads checkin's gather for
// the same thing; a source scan cannot see a later assignment that overwrites it (the
// LP-09 review measured exactly that: the gather writing the HOME venue's evidence after
// the tapped venue's kept every non-DB test green). This test runs the real tap path.

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/policy"
)

// TestCheckinDB_ATapAtAnotherVenueIsJudgedOnThatVenuesEvidence: a business with two
// venues -- the harness's St Julians (203.0.113.0/24, its GPS point) and KF Msida
// (198.51.100.0/24, a point about 2.5 km away). Two people whose home is KF Msida tap the
// St Julians plaque. The first taps from St Julians' network with a position at St
// Julians: both halves of the evidence match, the record is approved at St Julians with
// trust 100, and it carries the cross-location note. The second taps the same plaque from
// KF Msida's network with a position at KF Msida: neither half matches -- the home venue's
// evidence is not the tapped venue's -- and the record is flagged.
//
// MUTANT (the review's M4b): the gather writing the home venue's ranges and point over
// the tapped venue's -> the first tap matches nothing and is flagged, red.
func TestCheckinDB_ATapAtAnotherVenueIsJudgedOnThatVenuesEvidence(t *testing.T) {
	var sentence string
	for _, d := range policy.Baseline() {
		for _, st := range d.Document.Statements {
			if st.Sid == policy.SidCrossLocationNote {
				sentence = st.Reason
			}
		}
	}
	if sentence == "" {
		t.Fatal("PREMISE: the baseline has no cross-location note")
	}
	h := newTapHarness(t)
	var msida uuid.UUID
	first, second := uuid.New(), uuid.New()
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO locations (tenant_id, name, static_ips, gps_lat, gps_lng)
			 VALUES ($1, 'KF Msida', '{198.51.100.0/24}', 35.895, 14.489) RETURNING id`, h.tenantID).Scan(&msida); err != nil {
			return err
		}
		for _, id := range []uuid.UUID{first, second} {
			if _, err := tx.Exec(ctx,
				`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at, activated_at)
				 VALUES ($1, $2, $3, 'Sample Person', 'active', now(), now())`, id, h.tenantID, msida); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	h.postFrom(t, h.nfcContext(uint32(h.startCtr)+1), h.cookieForEmployee(t, first), "203.0.113.5:41234",
		url.Values{"lat": {"35.918"}, "lng": {"14.489"}})
	here := h.lastRecord(t, first)
	h.postFrom(t, h.nfcContext(uint32(h.startCtr)+2), h.cookieForEmployee(t, second), "198.51.100.7:41234",
		url.Values{"lat": {"35.895"}, "lng": {"14.489"}})
	home := h.lastRecord(t, second)

	trust := int16(-1)
	if here.Trust != nil {
		trust = *here.Trust
	}
	if here.Verdict != "ok" || here.IPMatch == nil || !*here.IPMatch || here.GPSMatch == nil || !*here.GPSMatch ||
		here.Trust == nil || *here.Trust != 100 {
		t.Errorf("a tap at St Julians from its network and its point, by somebody whose home is KF Msida: "+
			"verdict %q, IP %v, GPS %v, trust %v; want ok, both matched, 100 -- the tapped venue's evidence",
			here.Verdict, flagOf(here.IPMatch), flagOf(here.GPSMatch), trust)
	}
	if here.LocationID == nil || *here.LocationID != h.locationID {
		t.Errorf("the record is at %v, want the tapped venue %v", here.LocationID, h.locationID)
	}
	if !strings.Contains(deref(here.Note), sentence) {
		t.Errorf("the record of a tap away from home carries %q, not the cross-location note", deref(here.Note))
	}
	if home.Verdict != "flag" || home.IPMatch == nil || *home.IPMatch || home.GPSMatch == nil || *home.GPSMatch {
		t.Errorf("a tap at St Julians from KF Msida's network and point: verdict %q, IP %v, GPS %v; want flag "+
			"and neither matched -- the home venue's evidence is not the tapped venue's",
			home.Verdict, flagOf(home.IPMatch), flagOf(home.GPSMatch))
	}
}

// flagOf is a nullable evidence column as a word: "true", "false" or "null".
func flagOf(b *bool) string {
	if b == nil {
		return "null"
	}
	if *b {
		return "true"
	}
	return "false"
}
