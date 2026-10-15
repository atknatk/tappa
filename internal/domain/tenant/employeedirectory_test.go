package tenant

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// TestMergeDirectory_TheTwoSourcesInOneOrder: mergeDirectory interleaves the own rows and the peers' in
// (updated_at, id) order -- a tie of updated_at across the two sources broken by the id's bytes, as
// PostgreSQL's uuid order breaks it -- stops at the limit, and says there is more exactly when a
// source holds a row past the page or the peers' source answered a full page (it may hold more behind
// it). Each entry keeps its source's company: the business's own code for its own people, the peer's
// for the peer's.
func TestMergeDirectory_TheTwoSourcesInOneOrder(t *testing.T) {
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	id := func(b byte) uuid.UUID { return uuid.UUID{b} }
	own := func(b byte, d time.Duration) store.ListDirectoryEmployeesRow {
		return store.ListDirectoryEmployeesRow{ID: id(b), UpdatedAt: at.Add(d), Active: true}
	}
	km, kf := "KM", "KF"
	peer := func(b byte, d time.Duration) db.HoldingDirectoryEntry {
		return db.HoldingDirectoryEntry{EmployeeID: id(b), APICode: &kf, UpdatedAt: at.Add(d)}
	}
	ids := func(es []DirectoryEntry) []byte {
		var out []byte
		for _, e := range es {
			out = append(out, e.EmployeeID[0])
		}
		return out
	}
	ownRows := []store.ListDirectoryEmployeesRow{own(1, 0), own(5, time.Second), own(9, 3*time.Second)}
	peerRows := []db.HoldingDirectoryEntry{peer(2, 0), peer(4, time.Second), peer(6, time.Second), peer(7, 2*time.Second)}
	for _, c := range []struct {
		limit int
		want  []byte
		more  bool
	}{
		{10, []byte{1, 2, 4, 5, 6, 7, 9}, false},
		{7, []byte{1, 2, 4, 5, 6, 7, 9}, false},
		{6, []byte{1, 2, 4, 5, 6, 7}, true},
		{3, []byte{1, 2, 4}, true},
		{1, []byte{1}, true},
	} {
		got, more := mergeDirectory(ownRows, &km, peerRows, c.limit)
		if !slices.Equal(ids(got), c.want) || more != c.more {
			t.Errorf("limit %d: %v more=%v, want %v more=%v", c.limit, ids(got), more, c.want, c.more)
		}
		for _, e := range got {
			wantCompany := kf
			if e.EmployeeID[0]%4 == 1 {
				wantCompany = km
			}
			if e.Company == nil || *e.Company != wantCompany {
				t.Errorf("limit %d: %v carries company %v, want %s", c.limit, e.EmployeeID[0], e.Company, wantCompany)
			}
		}
	}
	// A full page of the peers' source says "more" even when the page takes all of it.
	full := make([]db.HoldingDirectoryEntry, db.HoldingDirectoryPage)
	for i := range full {
		full[i] = db.HoldingDirectoryEntry{EmployeeID: uuid.UUID{0, byte(i >> 8), byte(i)}, UpdatedAt: at, APICode: &kf}
	}
	if got, more := mergeDirectory(nil, &km, full, DirectoryMaxPage); len(got) != DirectoryMaxPage || !more {
		t.Errorf("a full peers' page: %d entries, more=%v; want 1000 and more", len(got), more)
	}
	if got, more := mergeDirectory(nil, &km, nil, 5); len(got) != 0 || more {
		t.Errorf("nothing: %d entries, more=%v", len(got), more)
	}
}
