package invite

// peerplaque.go -- the activating tap on a plaque of a LIVE HOLDING PEER of the invitation's business
// (the user's decision of 2026-11-11; ADR 0026 and ADR 0027 notes of that day). An employee registered
// at one company of a group (KF) who works at another (KM) activates at a KM plaque.
//
// 🔴 BOTH CALLS RUN IN THE INVITATION'S TENANT CONTEXT and reach the peer's row only through ADR 0027
// §2's closed list (holding_tap_site, holding_activation_tap), which take the caller from app.tenant_id
// and answer only for a plaque of a peer -- strictly (left_at > now). There is no WithTenant(peer)
// here (ADR 0016 §2): a plaque of a business that is not a peer answers nothing, and nothing moves.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// PeerPlaque is a plaque of a live holding peer of an invitation's business, as the pre-check saw it.
// It carries what the activation judges before anything advances -- whose plaque, in service, on a
// wall -- and none of the venue's evidence (holding_tap_site's K-39 columns stay inside db.HoldingSite).
type PeerPlaque struct {
	// SiteTenantID is the plaque's business -- the peer.
	SiteTenantID uuid.UUID
	// TagStatus is the plaque's status as the read saw it.
	TagStatus string
	// LocationID is the plaque's wall; nil for a plaque on none.
	LocationID *uuid.UUID
}

// PeerAdvanceOutcome is holding_activation_tap's verdict on the counter -- the advance's four.
type PeerAdvanceOutcome = db.HoldingAdvanceOutcome

// The four outcomes, re-exported so the HTTP layer does not import internal/db.
const (
	PeerAdvanced = db.HoldingAdvanced
	PeerNotPeer  = db.HoldingNotPeer
	PeerReplay   = db.HoldingReplay
	PeerInactive = db.HoldingPeerInactive
)

const errPeerArgsShape = "invite: %s: tenant and plaque are required"

// PeerPlaque reads uid's plaque when its business is a live peer of tenantID's (the invitation's):
// (plaque, true, nil). Not a peer, an unknown uid and an own plaque are (zero, false, nil) -- one
// answer. It writes nothing and moves no counter: it is the judgement made BEFORE anything advances.
func (m *Manager) PeerPlaque(ctx context.Context, tenantID uuid.UUID, uid string) (PeerPlaque, bool, error) {
	if tenantID == uuid.Nil || uid == "" {
		return PeerPlaque{}, false, fmt.Errorf(errPeerArgsShape, "peer plaque")
	}
	var (
		out  PeerPlaque
		peer bool
	)
	err := m.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		site, ok, err := db.HoldingOn(tx).TapSite(ctx, uid)
		if err != nil || !ok {
			return err
		}
		out, peer = PeerPlaque{SiteTenantID: site.SiteTenantID, TagStatus: site.TagStatus, LocationID: site.LocationID}, true
		return nil
	})
	if err != nil {
		return PeerPlaque{}, false, fmt.Errorf("invite: peer plaque: %w", err)
	}
	return out, peer, nil
}

// AdvancePeerPlaque is §4.4's advance for an activating tap on a peer's plaque: holding_activation_tap
// in tenantID's (the invitation's) context -- holding_advance_tag_counter's one statement, its strict
// peer check inside it, and, only when it advanced, the peer's own audit_log row in the same
// transaction (migration 00100). The caller has verified the chip's CMAC; nothing here compares a
// counter. A replay, a plaque out of service and a business no longer a peer are OUTCOMES; an error
// means the database could not say whether the counter moved.
func (m *Manager) AdvancePeerPlaque(ctx context.Context, tenantID uuid.UUID, uid string, ctr uint32) (PeerAdvanceOutcome, error) {
	if tenantID == uuid.Nil || uid == "" {
		return "", fmt.Errorf(errPeerArgsShape, "advance a peer's plaque")
	}
	var out db.HoldingAdvance
	err := m.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		out, e = db.HoldingOn(tx).ActivationTap(ctx, uid, ctr)
		return e
	})
	if err != nil {
		return "", fmt.Errorf("invite: advance a peer's plaque: %w", err)
	}
	if out.Outcome == "" {
		return "", errors.New("invite: advance a peer's plaque: no outcome")
	}
	return out.Outcome, nil
}
