// Package punchwire is ONE punch on the wire (M11; ADR 0028 §4, §6, §8; K-8, K-32): the item of
// GET /v1/punches (internal/handler/api) and the body of a webhook delivery (internal/webhook/delivery)
// are the same struct, built by the same function, so the two answers an integrator receives about
// one punch cannot drift apart (ADR 0028 §6: the webhook's payload is spec §5's fields + channel + the
// snapshot -- the feed item's). It is a package of its own because neither of its two users may import
// the other: the API surface imports no worker, the worker no HTTP surface.
//
// NOTHING BUT THE FEED'S OWN COLUMNS: an event (tenant.PunchEvent) carries no e-mail, coordinate,
// address, note, policy context, verdict or rule (K-1f, K-39) -- punch_events has none -- and Of copies
// it field by field.
package punchwire

import (
	"strings"
	"time"

	"github.com/atknatk/tappa/internal/domain/tenant"
)

// The external id prefixes (ADR 0028 §8, K-8).
const (
	PunchIDPrefix    = "pch_"
	EmployeeIDPrefix = "emp_"
	LocationIDPrefix = "loc_"
	TagIDPrefix      = "tag_"
)

// Punch is one punch on the wire.
type Punch struct {
	ID         string    `json:"id"`
	Version    int32     `json:"version"`
	Status     string    `json:"status"`
	PunchTime  time.Time `json:"punchTime"`
	RecordedAt time.Time `json:"recordedAt"`
	// Channel is "nfc" or "qr" on the wire, never "qr_live" (Q38 (a), the user's answer 2026-11-19):
	// punch_events stores the REAL channel (00112), and a live QR punch goes out as "qr" with QRKind
	// "live", so a client whose enum is [nfc, qr] keeps working.
	Channel string `json:"channel"`
	// QRKind is "static" (the plaque's printed code) or "live" (a venue screen's rotating code) on a
	// qr punch, and ABSENT on an nfc one -- an addition to v1, optional (Q38 (a): "isteğe bağlı
	// eklemeli"). Absent rather than null ON PURPOSE (a SEÇİM, M16-21): an nfc punch's bytes -- and
	// so every signature an integrator has ever verified, the guide's test vector among them -- stay
	// exactly what they were; only a qr punch gains a field, which v1's "ignore a field you do not
	// know" already allows.
	QRKind    *string `json:"qrKind,omitempty"`
	Direction string  `json:"direction"`
	// DirectionSource is who chose Direction: "auto" or "employee" (M15-13; ADR 0032 §B.6). Always
	// on the wire, null for a punch the feed recorded before it carried this -- the contract's shape
	// for every other nullable field (tagId). An addition to v1: a client that does not know the
	// field reads every other field as before.
	DirectionSource *string  `json:"directionSource"`
	Employee        Employee `json:"employee"`
	Location        Location `json:"location"`
	TagID           *string  `json:"tagId"`
}

// Employee is the punch's employee snapshot (K-32).
type Employee struct {
	ID           string  `json:"id"`
	ExternalRef  *string `json:"externalRef"`
	EmployeeCode *string `json:"employeeCode"`
	Company      *string `json:"company"`
}

// Location is the punch's location snapshot (K-32); every field NULL on an approval written after
// the holding ended (ADR 0027 §8).
type Location struct {
	ID          *string `json:"id"`
	ExternalRef *string `json:"externalRef"`
	Name        *string `json:"name"`
	Company     *string `json:"company"`
}

// Of is the wire form of e.
func Of(e tenant.PunchEvent) Punch {
	b := Punch{
		ID: PunchIDPrefix + e.PunchID.String(), Version: e.Version, Status: e.Status,
		PunchTime: e.PunchTime.UTC(), RecordedAt: e.RecordedAt.UTC(), Channel: e.Channel, Direction: e.Direction,
		DirectionSource: e.DirectionSource,
		Employee: Employee{ID: EmployeeIDPrefix + e.EmployeeID.String(), ExternalRef: e.EmployeeRef,
			EmployeeCode: e.EmployeeCode, Company: e.EmployeeCompany},
		Location: Location{ExternalRef: e.LocationRef, Name: e.LocationName, Company: e.LocationCompany},
	}
	b.Channel, b.QRKind = wireChannel(e.Channel)
	if e.LocationID != nil {
		id := LocationIDPrefix + e.LocationID.String()
		b.Location.ID = &id
	}
	if e.TagUID != nil {
		// tags.uid is character(14): a full UID fills it; the trim keeps a padded value off the wire.
		id := TagIDPrefix + strings.TrimRight(*e.TagUID, " ")
		b.TagID = &id
	}
	return b
}

// The stored channels Of maps (punch_events_channel, 00112), and their wire forms.
const (
	storedNFC    = "nfc"
	storedQR     = "qr"
	storedQRLive = "qr_live"
	wireQR       = "qr"
	kindStatic   = "static"
	kindLive     = "live"
)

// wireChannel is the stored channel as v1 says it (Q38 (a)): qr_live -> qr + live, qr -> qr + static,
// nfc -> nfc and no kind. A value punch_events' CHECK does not admit cannot be read here; one that is
// unknown to this mapping (a future channel) goes out as stored and without a kind -- loud in the
// client rather than silently relabelled.
func wireChannel(stored string) (string, *string) {
	kind := func(k string) *string { return &k }
	switch stored {
	case storedQRLive:
		return wireQR, kind(kindLive)
	case storedQR:
		return wireQR, kind(kindStatic)
	case storedNFC:
		return storedNFC, nil
	}
	return stored, nil
}
