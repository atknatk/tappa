package punchwire

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
)

// TestOf_DirectionSourceIsCopiedAndNullWhenUnknown (M15-13; ADR 0032 §B.6): the event's
// direction_source reaches the wire as directionSource, unchanged -- the trigger that wrote the event
// already applied the reader rule -- and an event written before the feed carried it (nil) is the
// contract's null, present like every other nullable field (tagId), directly after direction.
func TestOf_DirectionSourceIsCopiedAndNullWhenUnknown(t *testing.T) {
	str := func(s string) *string { return &s }
	for _, c := range []struct {
		name string
		in   *string
		want string
	}{
		{"an event before 00067", nil, `"direction":"in","directionSource":null,"employee"`},
		{"the engine's direction", str("auto"), `"direction":"in","directionSource":"auto","employee"`},
		{"the employee's choice", str("employee"), `"direction":"in","directionSource":"employee","employee"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := tenant.PunchEvent{PunchID: uuid.New(), Version: 1, Status: "valid", PunchTime: time.Now(), RecordedAt: time.Now(),
				Channel: "nfc", Direction: "in", DirectionSource: c.in, EmployeeID: uuid.New()}
			b, err := json.Marshal(Of(e))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), c.want) {
				t.Errorf("the wire is %s, want it to carry %s", b, c.want)
			}
		})
	}
}

// TestOf_ALiveQRPunchIsAQRPunchWithItsKind is Q38 (a), the user's answer of 2026-11-19 (M16-21):
// punch_events stores qr_live (00112), the wire says `channel: qr` + `qrKind: live` and `tagId:
// null` -- a client whose channel enum is [nfc, qr] keeps reading it. A static QR punch gains
// `qrKind: static` and keeps its tagId; an NFC punch carries no qrKind at all (the field is
// optional), so its bytes -- and every signature over them -- are exactly what they were.
func TestOf_ALiveQRPunchIsAQRPunchWithItsKind(t *testing.T) {
	uid := "04A1B2C3D4E5F6"
	for _, c := range []struct {
		name, stored string
		tag          *string
		want, absent []string
	}{
		{"a live QR punch", "qr_live", nil,
			[]string{`"channel":"qr","qrKind":"live","direction"`, `"tagId":null`}, []string{"qr_live"}},
		{"a static QR punch", "qr", &uid,
			[]string{`"channel":"qr","qrKind":"static","direction"`, `"tagId":"tag_04A1B2C3D4E5F6"`}, nil},
		{"an NFC punch", "nfc", &uid,
			[]string{`"channel":"nfc","direction"`, `"tagId":"tag_04A1B2C3D4E5F6"`}, []string{"qrKind"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := tenant.PunchEvent{PunchID: uuid.New(), Version: 1, Status: "valid", PunchTime: time.Now(), RecordedAt: time.Now(),
				Channel: c.stored, Direction: "in", EmployeeID: uuid.New(), TagUID: c.tag}
			b, err := json.Marshal(Of(e))
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range c.want {
				if !strings.Contains(string(b), w) {
					t.Errorf("the wire is %s, want it to carry %s", b, w)
				}
			}
			for _, a := range c.absent {
				if strings.Contains(string(b), a) {
					t.Errorf("the wire is %s, it must not carry %s", b, a)
				}
			}
		})
	}
}
