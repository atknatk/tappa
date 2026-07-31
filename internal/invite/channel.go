package invite

import (
	"context"
	"errors"
	"fmt"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/google/uuid"
)

// Delivery is what a Channel receives: one invitation and the link that spends
// it.
//
// ⚠️ ActivationURL CONTAINS THE RAW CODE. It is the single egress of a §4.7
// secret from this package (code.go names it), and every implementation of
// Channel inherits three obligations that no type system here can enforce:
//
//  1. NEVER log it, and never log anything derived from it. Not at debug level,
//     not in an error string, not in a retry message. scripts/redline-check.sh
//     R7 is a mechanical net over `code_hash`/`"code",`-shaped log calls; a
//     variable called `link` sails straight past it.
//  2. Never persist it. The database stores the HASH (00009) and nothing else;
//     a delivery queue that keeps the URL for retries has re-created the plain
//     credential the schema went to some trouble to avoid.
//  3. Hand it to ONE recipient — the person the invitation names.
//
// The field is exported because implementations live outside this package (an
// email sender will). That is the trade-off: the redaction machinery on Code
// covers accidental rendering everywhere else, and this one struct field is the
// declared, greppable exception.
type Delivery struct {
	Invite        Invite
	ActivationURL string
}

// Channel delivers an activation link to whoever must use it.
//
// WHY AN INTERFACE FOR SOMETHING WITH ONE IMPLEMENTATION (docs/plan open
// question Q02 is unanswered — there is no mail provider yet). Because the
// SHAPE of the fix is already known and the interface is what makes it a
// one-file change: today the link reaches the employee through their manager's
// screen; when Q02 is decided it must reach the employee's OWN channel
// (personal email or SMS) and no call site should have to move.
//
// ⚠️ THE INTERIM IMPLEMENTATION IS AN ACCEPTED RISK, NOT A DESIGN. ADR 0005 Y-D
// ("müdürün kimlik basması"): the person who generates and READS the code is the
// same person with the strongest incentive to inflate the payroll. A manager
// opens a fake employee profile, reads the link off their own screen, activates
// it in a second browser profile, and that ghost employee taps in every day with
// trust 100 — indistinguishable from a real one, because every piece of evidence
// (SUN, session, IP, GPS) is genuine. Moving the code to the employee's own
// channel removes the PRECONDITION of that attack. It is mandatory, not optional.
//
// Declared at the consumer (§7): the producer of a delivery mechanism does not
// get to define what this package needs from it.
type Channel interface {
	DeliverInvite(ctx context.Context, d Delivery) error
}

// LinkSink is where ManagerVisibleChannel puts the link so the admin panel can
// show it. It exists so that this package does not import the panel and the
// panel does not import a raw-string function from here.
type LinkSink interface {
	ShowActivationLink(ctx context.Context, d Delivery) error
}

// auditRecorder is the slice of *audit.Recorder this file needs (§7, consumer
// side).
type auditRecorder interface {
	Record(ctx context.Context, e audit.Event) (uuid.UUID, error)
}

// ActionCodeShownToManager is the audit action every manager-visible delivery
// writes.
//
// THIS IS THE POINT OF THE TYPE, not a side effect. ADR 0005 says the Y-D risk is
// ACCEPTED until Q02, and that its detection signal is the M6-11 report "N
// employees activated from a single device/session". That report needs raw
// material, and the moment the manager sees a code is exactly the moment worth
// recording: the trail then answers "who read this person's activation link, and
// when", which is the question an investigation starts from. Delivering through
// a channel that leaves no trace would make the accepted risk invisible as well
// as unmitigated.
const ActionCodeShownToManager = "invite.code_shown_to_manager"

// ManagerVisibleChannel is TODAY'S ONLY CHANNEL: it shows the activation link on
// the admin panel and records that it did.
//
// The name is deliberately uncomfortable. A future reader looking for "how does
// the code reach the employee" should trip over the fact that it does not — it
// reaches their manager — and find ADR 0005 Y-D one comment away.
type ManagerVisibleChannel struct {
	sink  LinkSink
	audit auditRecorder
	// actorID is the admin who asked for the invitation, when the caller knows
	// it. audit_log.actor_id is for ADMIN identities (00005), so this is the one
	// activation-flow event that legitimately fills it.
	actorID *uuid.UUID
}

// NewManagerVisibleChannel wires the interim channel. Both dependencies are
// required: a delivery that cannot be shown is useless, and one that is not
// recorded defeats the reason this type exists.
func NewManagerVisibleChannel(sink LinkSink, rec auditRecorder, actorID *uuid.UUID) (*ManagerVisibleChannel, error) {
	if sink == nil {
		return nil, errors.New("invite: manager channel: nil sink")
	}
	if rec == nil {
		return nil, errors.New("invite: manager channel: nil audit recorder")
	}
	return &ManagerVisibleChannel{sink: sink, audit: rec, actorID: actorID}, nil
}

// codeShownDetail is the audit payload. It is a PURPOSE-BUILT STRUCT, not a map
// of whatever was in scope, because that is how internal/audit's §4.7 promise is
// kept: there is no field here that could carry the code or its hash, so no
// future edit can add one by accident.
type codeShownDetail struct {
	InviteID   uuid.UUID `json:"invite_id"`
	EmployeeID uuid.UUID `json:"employee_id"`
	ExpiresAt  string    `json:"expires_at"`
	// Channel names the delivery route so the M6-11 report can separate the
	// interim path from whatever Q02 brings.
	Channel string `json:"channel"`
	// AcceptedRisk points the reader at the reasoning rather than repeating it.
	AcceptedRisk string `json:"accepted_risk"`
}

// DeliverInvite records the disclosure and then shows the link.
//
// ORDER: audit FIRST. If the panel render fails after the row is written, the
// trail over-reports by one — harmless. If the link were shown first and the
// audit write failed, a manager would hold a code that the trail does not know
// was ever disclosed, which is the one outcome the ADR's detection signal cannot
// survive (§4.6).
func (c *ManagerVisibleChannel) DeliverInvite(ctx context.Context, d Delivery) error {
	if _, err := c.audit.Record(ctx, audit.Event{
		TenantID: d.Invite.TenantID,
		ActorID:  c.actorID,
		Action:   ActionCodeShownToManager,
		Target:   d.Invite.EmployeeID.String(),
		Detail: codeShownDetail{
			InviteID:     d.Invite.ID,
			EmployeeID:   d.Invite.EmployeeID,
			ExpiresAt:    d.Invite.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Channel:      "manager_panel",
			AcceptedRisk: "ADR-0005 Y-D: the manager sees the code until Q02 moves it to the employee's own channel",
		},
	}); err != nil {
		return fmt.Errorf("invite: record disclosure: %w", err)
	}
	return c.sink.ShowActivationLink(ctx, d)
}
