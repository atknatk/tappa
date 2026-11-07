package operator_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/httpx"
)

type readCtxKey struct{}

// readCtxProbe records, for each read the screens make, the context it was handed.
type readCtxProbe struct {
	*fakeStore
	seen map[string]context.Context
}

func (p *readCtxProbe) LegalVersions(ctx context.Context, h string, q db.LegalVersionsPage) ([]db.LegalVersion, error) {
	p.seen["LegalVersions"] = ctx
	return p.fakeStore.LegalVersions(ctx, h, q)
}

func (p *readCtxProbe) TenantList(ctx context.Context, h string, q db.TenantListQuery) ([]db.TenantSummary, error) {
	p.seen["TenantList"] = ctx
	return p.fakeStore.TenantList(ctx, h, q)
}

func (p *readCtxProbe) TenantDetail(ctx context.Context, h string, id uuid.UUID) (db.TenantOverview, error) {
	p.seen["TenantDetail"] = ctx
	return p.fakeStore.TenantDetail(ctx, h, id)
}

func (p *readCtxProbe) TenantPlaques(ctx context.Context, h string, id uuid.UUID) (db.TenantPlaqueInventory, error) {
	p.seen["TenantPlaques"] = ctx
	return p.fakeStore.TenantPlaques(ctx, h, id)
}

func (p *readCtxProbe) OperatorAudit(ctx context.Context, h string, q db.OperatorAuditQuery) ([]db.OperatorAuditEntry, error) {
	p.seen["OperatorAudit"] = ctx
	return p.fakeStore.OperatorAudit(ctx, h, q)
}

func (p *readCtxProbe) TenantBilling(ctx context.Context, h string, id uuid.UUID, page int32) (db.TenantBillingTimeline, error) {
	p.seen["TenantBilling"] = ctx
	return p.fakeStore.TenantBilling(ctx, h, id, page)
}

func (p *readCtxProbe) TenantVAT(ctx context.Context, h string, id uuid.UUID) (db.TenantVATStatus, error) {
	p.seen["TenantVAT"] = ctx
	return p.fakeStore.TenantVAT(ctx, h, id)
}

func (p *readCtxProbe) Holdings(ctx context.Context, h string, page int32) ([]db.HoldingRow, error) {
	p.seen["Holdings"] = ctx
	return p.fakeStore.Holdings(ctx, h, page)
}

// TestScreens_EveryReadRunsOnTheRequestsContext (T98) drives each read screen with a request whose
// context carries a value: the store is handed a context with that value and the router's deadline,
// no further away than httpx.RequestTimeout -- so a client that leaves and a request past its
// deadline both reach the database. internal/db's TestOperatorReads_TheCallersContextEndsABlockedRead
// takes it from there. PART II -- these eight reads.
func TestScreens_EveryReadRunsOnTheRequestsContext(t *testing.T) {
	g := newRig(t)
	probe := &readCtxProbe{fakeStore: g.store, seen: map[string]context.Context{}}
	s, err := operator.New(g.auth, probe, probe, probe, probe, probe, probe, probe, probe, g.vies, g.texts, opHost, opBase, captureAt(g.logs, 0))
	if err != nil {
		t.Fatal(err)
	}
	g.h = httpx.NewRouter(&config.Config{OperatorHost: opHost, BaseURL: opBase}, captureAt(g.logs, 0), customer{}, s)
	c := g.signIn(g.active())
	overview := g.seedOverview(db.TenantOverview{Name: "FAKE overview", Plan: "founding"})
	plaques := g.seedPlaques("FAKE plaques")
	billing := g.seedBilling("FAKE billing", billingBeforeSignupMonths(2026, time.September, 12)...)
	vat := g.seedVAT(uuid.Nil, "FAKE vat", vatNumberOK, nil, nil)
	ctx := context.WithValue(context.Background(), readCtxKey{}, "this request")
	for method, path := range map[string]string{
		"LegalVersions": "/operator/legal",
		"TenantList":    "/operator/tenants",
		"TenantDetail":  "/operator/tenants/" + overview.String(),
		"TenantPlaques": plaquePath(plaques.String()),
		"OperatorAudit": "/operator/audit",
		"TenantBilling": "/operator/tenants/" + billing.String() + "/billing",
		"TenantVAT":     vatPathOf(vat.String()),
		"Holdings":      "/operator/holdings",
	} {
		before := time.Now()
		w := g.do(req{method: http.MethodGet, host: opHost, path: path, cookies: []*http.Cookie{c}, ctx: ctx})
		if w.Code != http.StatusOK {
			t.Errorf("%s: GET %s = %d", method, path, w.Code)
			continue
		}
		got := probe.seen[method]
		if got == nil {
			t.Errorf("%s: GET %s did not reach the store's read", method, path)
			continue
		}
		if got.Value(readCtxKey{}) != "this request" {
			t.Errorf("%s: the store's context does not derive from the request's", method)
		}
		if d, ok := got.Deadline(); !ok || d.Sub(before) > httpx.RequestTimeout+time.Second {
			t.Errorf("%s: the store's context has no deadline within the request's (%v, %v)", method, d, ok)
		}
	}
}
