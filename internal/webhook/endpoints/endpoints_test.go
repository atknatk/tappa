package endpoints

import "testing"

// TestEndpoints_SuspensionClassesAreSection10 pins ADR 0028 §10 for the endpoint: switching off is
// the one act open while suspended (K-26); creating, rotating, re-addressing and the test ping are
// closed by name; switching on is closed because §10 does not name it ("varsayılan kapalı"), and so is
// the zero Act.
func TestEndpoints_SuspensionClassesAreSection10(t *testing.T) {
	for act, open := range map[Act]bool{
		actUnnamed: false, ActCreate: false, ActRotate: false, ActReaddress: false,
		ActDisable: true, ActEnable: false, ActPing: false, ActResend: false,
	} {
		if OpenWhileSuspended(act) != open {
			t.Errorf("act %d: open while suspended %v, want %v", act, OpenWhileSuspended(act), open)
		}
	}
	if len(openWhileSuspended) != 1 {
		t.Fatalf("%d acts open while suspended, want 1 (switching off)", len(openWhileSuspended))
	}
}

// TestNew_RefusesAMissingPart: a KEK of the wrong size, or no database, trail or sender, builds no
// service.
func TestNew_RefusesAMissingPart(t *testing.T) {
	if _, err := New(make([]byte, 16), nil, nil, nil); err == nil {
		t.Fatal("a 16-byte KEK")
	}
	if _, err := New(make([]byte, 32), nil, nil, nil); err == nil {
		t.Fatal("no database, trail or sender")
	}
}
