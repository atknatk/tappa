package checkin

import (
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// noticeRecorder is K-19's set as the tap path sees it.
type noticeRecorder struct {
	mu      sync.Mutex
	tenants []uuid.UUID
}

func (n *noticeRecorder) Notice(t uuid.UUID) {
	n.mu.Lock()
	n.tenants = append(n.tenants, t)
	n.mu.Unlock()
}

func (n *noticeRecorder) take() []uuid.UUID {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := n.tenants
	n.tenants = nil
	return out
}

// TestK19_ARecordedTapNoticesItsTenantsAndARefusalNobody (ADR 0028 §6, K-19 -- the set is built by
// events, never by a scan): a recorded tap on the tenant's own plaque notices that tenant; a
// recorded HOLDING tap (KM's employee on KF's plaque) notices BOTH -- the employer and the site, the
// two tenants whose feed it wrote; a tap that wrote nothing (C's session on KF's plaque, the
// foreign-tenant refusal) notices nobody. Every notice comes after the commit (Record returned).
//
// MUTANT (measured, API-4 report): the site's Notice removed -> the holding row notices KM alone.
func TestK19_ARecordedTapNoticesItsTenantsAndARefusalNobody(t *testing.T) {
	w := newHoldingWorld(t)
	rec := &noticeRecorder{}
	w.svc.NoticeWorkTo(rec)
	for _, c := range []struct {
		name string
		req  Request
		want []uuid.UUID
	}{
		{"own plaque", w.nfc(w.kf, w.kfEmp, w.kfTag, w.kf, 101, w.kfOnNet), []uuid.UUID{w.kf}},
		{"a peer's plaque", w.nfc(w.km, w.kmEmp, w.kfTag, w.kf, 102, w.kfOnNet), []uuid.UUID{w.km, w.kf}},
		{"an outsider's session, refused", w.nfc(w.c, w.cEmp, w.kfTag, w.kf, 150, w.kfOnNet), nil},
	} {
		res, err := w.svc.Record(w.ctx, c.req)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got := rec.take()
		if (res.TransactionID != uuid.Nil) != (len(c.want) > 0) {
			t.Fatalf("%s: record %s, outcome %s -- the fixture is not the case it names", c.name, res.TransactionID, res.Outcome)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: noticed %v, want %v", c.name, got, c.want)
		}
	}
}
