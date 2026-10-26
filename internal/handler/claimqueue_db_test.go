package handler

// claimqueue_db_test.go -- the Spike's "Time claims" end to end (M15-18): the real panel, the real
// claim service and the real manual writer over Postgres, signed in over HTTP. The fake-backed
// tests in claimqueue_test.go hold the words and the binding; these hold the rows.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/claim"
	"github.com/atknatk/tappa/internal/domain/tap"
)

// claimScene is one business's person at a 09:00–17:00 venue in Malta, with the business default
// 05:00 cutoff.
type claimScene struct {
	tenant, venue, person uuid.UUID
	plaque                string
	ctr                   int
	malta                 *time.Location
	monday                time.Time // 00:00 Malta of a Monday two weeks back
}

func newClaimScene(t *testing.T, p *panelHarness, tenantID uuid.UUID) *claimScene {
	t.Helper()
	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(malta)
	monday := time.Date(now.Year(), now.Month(), now.Day()-14, 0, 0, 0, 0, malta)
	for monday.Weekday() != time.Monday {
		monday = time.Date(monday.Year(), monday.Month(), monday.Day()-1, 0, 0, 0, 0, malta)
	}
	s := &claimScene{tenant: tenantID, venue: uuid.New(), person: uuid.New(), malta: malta, monday: monday,
		plaque: strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))[:14]}
	if err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name, static_ips, shift_start, shift_end) VALUES ($1, $2, 'Claims Venue', '{203.0.113.0/24}', '09:00', '17:00')`,
			s.venue, tenantID); e != nil {
			return fmt.Errorf("location: %w", e)
		}
		if _, e := tx.Exec(ctx, `INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'Ahmet Demir', 'active')`,
			s.person, tenantID, s.venue); e != nil {
			return fmt.Errorf("employee: %w", e)
		}
		_, e := tx.Exec(ctx, `INSERT INTO tags (uid, tenant_id, location_id, status, last_ctr, aes_key_ref) VALUES ($1, $2, $3, 'active', 0, decode(repeat('dead', 22), 'hex'))`,
			s.plaque, tenantID, s.venue)
		return e
	}); err != nil {
		t.Fatalf("seed the claim scene: %v", err)
	}
	return s
}

// at is day d of the scene's week at hh:mm Malta.
func (s *claimScene) at(d, hh, mm int) time.Time {
	return time.Date(s.monday.Year(), s.monday.Month(), s.monday.Day()+d, hh, mm, 0, 0, s.malta)
}

// tap writes a directed nfc record with its frozen business day, as the engine would.
func (s *claimScene) tap(t *testing.T, p *panelHarness, dir string, at time.Time) uuid.UUID {
	t.Helper()
	day, ok := tap.BusinessDayStart(at, tap.DayCutoff{Cutoff: 5 * time.Hour, Timezone: "Europe/Malta"})
	if !ok {
		t.Fatal("no business day")
	}
	s.ctr++
	var id uuid.UUID
	if err := p.data.WithTenant(context.Background(), s.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, created_at, verdict, channel, type, sun_valid, ctr, business_day_start)
		                         VALUES ($1, $2, $3, $4, $5, $5, 'ok', 'nfc', $6, true, $7, $8) RETURNING id`,
			s.tenant, s.person, s.venue, s.plaque, at, dir, s.ctr, day).Scan(&id)
	}); err != nil {
		t.Fatalf("write a %s: %v", dir, err)
	}
	return id
}

func (s *claimScene) submit(t *testing.T, p *panelHarness, anchor uuid.UUID, kind claim.Kind, at time.Time) uuid.UUID {
	t.Helper()
	got, err := p.claims.Submit(context.Background(), claim.Claim{TenantID: s.tenant, EmployeeID: s.person, AnchorID: anchor, Kind: kind, ClaimedAt: at})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	return got.ID
}

func (s *claimScene) count(t *testing.T, p *panelHarness, sql string) int {
	t.Helper()
	return countRows(t, p, s.tenant, sql)
}

func (s *claimScene) manualRows(t *testing.T, p *panelHarness) int {
	return s.count(t, p, `SELECT count(*) FROM transactions WHERE tenant_id = $1 AND channel = 'manual'`)
}

func (s *claimScene) decisions(t *testing.T, p *panelHarness) int {
	return s.count(t, p, `SELECT count(*) FROM punch_claim_decisions WHERE tenant_id = $1`)
}

// TestClaimQueueDB_OnePressInTheBandTwoOutsideAndEveryRefusalWritesNothing walks the card's
// acceptance over real rows: focus + Approve is ONE POST that redirects and writes one manual record
// and one decision; outside the band the first press writes nothing and draws the step, whose own
// press writes one of each; a second decision is "already decided"; another business's claim id is
// "not found" -- each refusal writes nothing.
func TestClaimQueueDB_OnePressInTheBandTwoOutsideAndEveryRefusalWritesNothing(t *testing.T) {
	p := newPanelHarnessWith(t, panelHarnessOptions{claims: true})
	p.signIn(t)
	s := newClaimScene(t, p, p.tenantID)

	inBand := s.submit(t, p, s.tap(t, p, "in", s.at(0, 9, 5)), claim.KindOut, s.at(0, 17, 0))
	outOfBand := s.submit(t, p, s.tap(t, p, "in", s.at(1, 9, 5)), claim.KindOut, s.at(2, 1, 0))

	_, page := p.get(t, claimsHref+"?focus="+inBand.String())
	if !strings.Contains(page, "Ahmet Demir") || !strings.Contains(page, `name="id" value="`+inBand.String()+`"`) {
		t.Fatal("the queue does not offer the in-band claim in focus")
	}
	res, _ := p.post(t, claimsHref, decisionForm(inBand, "approved"))
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("Approve answered %d, want 303", res.StatusCode)
	}
	if m, d := s.manualRows(t, p), s.decisions(t, p); m != 1 || d != 1 {
		t.Fatalf("after one press: %d manual record(s), %d decision(s); want 1 and 1", m, d)
	}
	if _, done := p.get(t, res.Header.Get("Location")); !strings.Contains(done, "Claim approved") {
		t.Error("the redirect does not confirm the approval read back from the database")
	}

	first, step := p.post(t, claimsHref, decisionForm(outOfBand, "approved"))
	if first.StatusCode != http.StatusOK || !strings.Contains(step, "Confirm this interval") {
		t.Fatalf("the first press outside the band answered %d without the step", first.StatusCode)
	}
	if m, d := s.manualRows(t, p), s.decisions(t, p); m != 1 || d != 1 {
		t.Fatalf("the first press outside the band wrote: %d manual record(s), %d decision(s); want 1 and 1 (unchanged)", m, d)
	}
	if !strings.Contains(step, htmlText("= 15 h 55 m")) || !strings.Contains(step, "8 h 00 m after the shift ends") {
		t.Error("the step does not repeat the interval's duration and the claimed end's deviation")
	}
	shown, token := stepFields(t, step)
	second, _ := p.post(t, claimsHref, decisionForm(outOfBand, "approved", "shown", shown, "confirm_token", token))
	if second.StatusCode != http.StatusSeeOther {
		t.Fatalf("the step's press answered %d, want 303", second.StatusCode)
	}
	if m, d := s.manualRows(t, p), s.decisions(t, p); m != 2 || d != 2 {
		t.Fatalf("after the step's press: %d manual record(s), %d decision(s); want 2 and 2", m, d)
	}

	again, _ := p.post(t, claimsHref, decisionForm(outOfBand, "rejected"))
	if loc := again.Header.Get("Location"); !strings.Contains(loc, "problem=decided") {
		t.Errorf("a second decision went to %q, want problem=decided", loc)
	}

	otherTenant := uuid.New()
	if err := p.data.WithTenant(context.Background(), otherTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'Other Claims Ltd', $2, 'bar', 'single')`,
			otherTenant, "VAT-"+otherTenant.String())
		return e
	}); err != nil {
		t.Fatal(err)
	}
	o := newClaimScene(t, p, otherTenant)
	foreign := o.submit(t, p, o.tap(t, p, "in", o.at(0, 9, 5)), claim.KindOut, o.at(0, 17, 0))
	res, _ = p.post(t, claimsHref, decisionForm(foreign, "approved"))
	if loc := res.Header.Get("Location"); !strings.Contains(loc, "problem=unknown") {
		t.Errorf("another business's claim id went to %q, want problem=unknown", loc)
	}
	if _, body := p.get(t, res.Header.Get("Location")); !strings.Contains(body, "That claim was not found") {
		t.Error("the page does not say the claim was not found")
	}
	if m, d := o.manualRows(t, p), o.decisions(t, p); m != 0 || d != 0 {
		t.Errorf("deciding another business's claim wrote %d manual record(s) and %d decision(s) there", m, d)
	}
	if m, d := s.manualRows(t, p), s.decisions(t, p); m != 2 || d != 2 {
		t.Errorf("the refusals wrote: %d manual record(s), %d decision(s); want 2 and 2 (unchanged)", m, d)
	}
}
