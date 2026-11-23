package handler

// appclaims_db_test.go -- M16-43 end to end: the app lists a missing clock-out, /me counts it, the
// send writes the browser's one row (claim + trail, no transaction), a second send is already_sent,
// and a context is usable only by the device it was minted for -- never by another phone and never
// across the browser/app boundary.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/claim"
	"github.com/atknatk/tappa/internal/domain/tap"
)

func TestAppClaimsDB_ListSendAndTheContextsHolder(t *testing.T) {
	h := newAppHarness(t)
	ownerDSN := os.Getenv("DATABASE_MIGRATE_URL")
	if ownerDSN == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the fixture's check-in is written as the owner")
	}
	if err := h.app.ServeClaims(h.claims, h.cfg); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })

	// A check-in 30 hours ago with no clock-out: stale, so it takes a claim. Its business day is the
	// person's 05:00 cutoff in the business's zone.
	if _, err := owner.Exec(ctx, `UPDATE employees SET day_cutoff = '05:00' WHERE tenant_id = $1 AND id = $2`, h.tenantID, h.employeeID); err != nil {
		t.Fatal(err)
	}
	var zone string
	if err := owner.QueryRow(ctx, `SELECT timezone FROM tenants WHERE id = $1`, h.tenantID).Scan(&zone); err != nil {
		t.Fatal(err)
	}
	in := time.Now().Add(-30 * time.Hour).UTC().Truncate(time.Minute)
	day, ok := tap.BusinessDayStart(in, tap.DayCutoff{Cutoff: 5 * time.Hour, Timezone: zone})
	if !ok {
		t.Fatal("no business day")
	}
	var anchor uuid.UUID
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The ok tap's punch_feed trigger reads the transaction's tenant context.
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, h.tenantID.String()); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, created_at, verdict, channel, type, practice, business_day_start)
	                               VALUES ($1, $2, $3, $4, $4, 'ok', 'nfc', 'in', false, $5) RETURNING id`,
		h.tenantID, h.employeeID, h.locationID, in, day).Scan(&anchor); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	phone := newAppPhone(t)
	h.register(t, phone, h.employeeID)

	w := h.read(t, phone, http.MethodGet, AppMePath, "")
	if w.Code != http.StatusOK {
		t.Fatalf("/me = %d %s", w.Code, w.Body.String())
	}
	var me appMeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.MissingTimes != 1 || me.Presence != nil {
		t.Fatalf("/me missingTimes %d presence %+v, want 1 and none (the check-in is stale)", me.MissingTimes, me.Presence)
	}

	w = h.read(t, phone, http.MethodPost, AppClaimsPath, "{}")
	if w.Code != http.StatusOK {
		t.Fatalf("claims = %d %s", w.Code, w.Body.String())
	}
	var list appClaimsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Open) != 1 || list.Open[0].Kind != "out" || len(list.Sent) != 0 {
		t.Fatalf("list %+v, want one clock-out and nothing sent", list)
	}
	loc, _ := time.LoadLocation(zone)
	at := in.Add(8 * time.Hour).In(loc).Format("15:04")
	if list.Open[0].AnchorAt != in.In(loc).Format("15:04") {
		t.Fatalf("anchorAt %q, want %q", list.Open[0].AnchorAt, in.In(loc).Format("15:04"))
	}

	// Another person's phone cannot use the context (the holder is the device).
	otherEmployee, _ := h.invitedEmployee(t, "active")
	other := newAppPhone(t)
	h.register(t, other, otherEmployee)
	send := func(p *appPhone, ctxValue string) (int, string) {
		t.Helper()
		w := h.post(t, p, AppClaimsSendPath, jsonBody(t, map[string]string{"context": ctxValue, "at": at}))
		if w.Code != http.StatusOK {
			var e map[string]string
			_ = json.Unmarshal(w.Body.Bytes(), &e)
			return w.Code, e["error"]
		}
		var r appClaimSendResponse
		if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		return w.Code, r.Result
	}
	if code, word := send(other, list.Open[0].Context); word != string(appErrStaleContext) {
		t.Fatalf("another phone: %d %q, want stale_context", code, word)
	}
	// A browser context (bound to a session) is not an app context.
	browser, err := h.tapHarness.tap.claimContexts.mint(claimContext{Kind: claim.KindOut, AnchorID: anchor, AnchorAt: in,
		Window: claim.Window{From: claim.Bound{At: in}, To: claim.Bound{At: in.Add(18 * time.Hour), Inclusive: true}},
		TapAt:  time.Now(), Zone: zone}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if code, word := send(phone, browser); word != string(appErrStaleContext) {
		t.Fatalf("a browser context: %d %q, want stale_context", code, word)
	}
	if n := h.countWhere(t, `SELECT count(*) FROM punch_claims WHERE tenant_id = $1 AND employee_id = $2`, h.employeeID); n != 0 {
		t.Fatalf("%d claims after two refused contexts, want 0", n)
	}

	before := h.transactionCount(t)
	if _, word := send(phone, list.Open[0].Context); word != appClaimSent {
		t.Fatalf("send: %q, want sent", word)
	}
	if _, word := send(phone, list.Open[0].Context); word != appClaimAlreadySent {
		t.Fatalf("second send: %q, want already_sent", word)
	}
	if n := h.countWhere(t, `SELECT count(*) FROM punch_claims WHERE tenant_id = $1 AND employee_id = $2`, h.employeeID); n != 1 {
		t.Fatalf("%d claims, want 1", n)
	}
	if n := h.auditCount(t, claim.ActionSubmitted); n != 1 {
		t.Fatalf("%d claim.submitted rows, want 1", n)
	}
	if after := h.transactionCount(t); after != before {
		t.Fatalf("transactions %d -> %d: a claim is not a record", before, after)
	}

	w = h.read(t, phone, http.MethodPost, AppClaimsPath, "{}")
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Open) != 0 || len(list.Sent) != 1 || list.Sent[0].State != "pending" || list.Sent[0].ClaimedAt != at || list.Sent[0].ApprovedAt != nil {
		t.Fatalf("after the send: %+v", list)
	}
}
