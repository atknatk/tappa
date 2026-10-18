package sandbox

import (
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
)

// TestIsVirtualTagUID_IsTheRangeAndNothingElse: the allow-list, and the derived UIDs are in it.
func TestIsVirtualTagUID_IsTheRangeAndNothingElse(t *testing.T) {
	for uid, want := range map[string]bool{
		"0053AABBCCDDEE":   true,
		"04968CAA5C5E80":   false, // a real NXP NTAG 424 DNA
		"0853AABBCCDDEE":   false, // another manufacturer byte
		"0052AABBCCDDEE":   false,
		"0053aabbccddee":   false, // another spelling: the column has one
		"0053AABBCCDD":     false,
		"0053AABBCCDDEEFF": false,
		"0053AABBCCDDEG":   false,
		"":                 false,
	} {
		if got := db.IsVirtualTagUID(uid); got != want {
			t.Errorf("IsVirtualTagUID(%q) = %v, want %v", uid, got, want)
		}
	}
	for range 100 {
		if v := uuid.New(); !db.IsVirtualTagUID(VirtualUID(v)) {
			t.Fatalf("the derived UID %s of %s is outside the range", VirtualUID(v), v)
		}
	}
}
