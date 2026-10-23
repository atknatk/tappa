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
