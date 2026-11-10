package manual

// duplicate_db_test.go -- backlog T258 (user decision 2026-11-09): the same manual record pressed
// twice is written once. Real Postgres, the race world's own tenant.

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestManualRecord_TheSameRecordPressedNTimesIsWrittenOnce: four presses of the same confirmed entry
// at once -- they all meet before the person's lock (beforeLock is the barrier) -- write exactly one
// row and one trail row; the others answer ErrAlreadyRecorded with that row's id. A record that
// differs in instant, direction or author is still written.
func TestManualRecord_TheSameRecordPressedNTimesIsWrittenOnce(t *testing.T) {
	// FOUR: pgxpool's default ceiling is max(4, NumCPU) connections and every press holds one while
	// it waits at the barrier, so more presses than connections wait for each other. With 8 the CI
	// run timed out at 30 min, its trace showing four presses at the barrier and four without a
	// connection. The barrier is also bounded.
	const presses = 4
	w := newRaceWorld(t)
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	entry := Entry{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: w.admin, Direction: Out, At: at, Note: "phone was flat"}

	// The barrier lets every press reach the lock together, and it is bounded so that a smaller
	// pool can never turn it into a hang.
	var mu sync.Mutex
	arrived, all := 0, make(chan struct{})
	w.rec.beforeLock = func(attempt int) {
		if attempt != 0 {
			return
		}
		mu.Lock()
		arrived++
		if arrived == presses {
			close(all)
		}
		mu.Unlock()
		select {
		case <-all:
		case <-time.After(5 * time.Second):
		}
	}
	type result struct {
		rec Recorded
		err error
	}
	results := make([]result, presses)
	var done sync.WaitGroup
	for i := range presses {
		done.Add(1)
		go func() {
			defer done.Done()
			rec, err := w.rec.Record(w.ctx, entry)
			results[i] = result{rec, err}
		}()
	}
	done.Wait()
	w.rec.beforeLock = nil

	var written uuid.UUID
	refused := 0
	for _, r := range results {
		switch {
		case r.err == nil:
			if written != uuid.Nil {
				t.Errorf("two presses both wrote a record (%v and %v)", written, r.rec.ID)
			}
			written = r.rec.ID
		case errors.Is(r.err, ErrAlreadyRecorded):
			refused++
		default:
			t.Errorf("a press answered %v", r.err)
		}
	}
	if written == uuid.Nil || refused != presses-1 {
		t.Fatalf("%d press(es) refused and written=%v, want one written and %d refused", refused, written, presses-1)
	}
	for _, r := range results {
		if errors.Is(r.err, ErrAlreadyRecorded) && r.rec.ID != written {
			t.Errorf("a refused press names record %v, want the written one %v", r.rec.ID, written)
		}
	}
	manualRows := func() int {
		return w.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1 AND channel = 'manual'`, w.tenant)
	}
	if n := manualRows(); n != 1 {
		t.Fatalf("%d manual records after %d presses of the same one, want 1", n, presses)
	}
	if n := w.count(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`, w.tenant, Action); n != 1 {
		t.Errorf("%d record.entered rows, want 1", n)
	}

	// A DIFFERENT RECORD IS STILL WRITTEN: another instant, another direction, another author.
	other := uuid.New()
	w.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Manager', $3, $4, 'manager')`,
		other, w.tenant, "m-"+other.String()[:12]+"@example.test", "$2a$12$"+strings.Repeat("h", 53))
	for i, e := range []Entry{
		{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: w.admin, Direction: Out, At: at.Add(time.Minute)},
		{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: w.admin, Direction: In, At: at},
		{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: other, Direction: Out, At: at},
	} {
		if _, err := w.rec.Record(w.ctx, e); err != nil {
			t.Fatalf("different record %d: %v", i, err)
		}
		if n := manualRows(); n != 2+i {
			t.Fatalf("after different record %d: %d manual records, want %d", i, n, 2+i)
		}
	}
	// and the first one pressed once more, later, is still refused
	if _, err := w.rec.Record(w.ctx, entry); !errors.Is(err, ErrAlreadyRecorded) {
		t.Errorf("the same record pressed again later: %v, want ErrAlreadyRecorded", err)
	}
	if n := manualRows(); n != 4 {
		t.Errorf("%d manual records, want 4", n)
	}
}

// TestManualRecord_AMinuteWithAnotherVenueOrNoteIsNotTheSameRecord is the user decision of
// 2026-11-10: the rule does not change (nothing is written), but the answer says whether the row
// already there is the same record (Same) or another venue or note for the same minute.
func TestManualRecord_AMinuteWithAnotherVenueOrNoteIsNotTheSameRecord(t *testing.T) {
	w := newRaceWorld(t)
	at := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	entry := Entry{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: w.admin, Direction: In, At: at}
	if _, err := w.rec.Record(w.ctx, entry); err != nil {
		t.Fatal(err)
	}
	existing := func(e Entry) Existing {
		t.Helper()
		_, err := w.rec.Record(w.ctx, e)
		var dup *AlreadyRecordedError
		if !errors.As(err, &dup) || !errors.Is(err, ErrAlreadyRecorded) {
			t.Fatalf("Record = %v, want an *AlreadyRecordedError", err)
		}
		return dup.Existing
	}

	if x := existing(entry); !x.Same() || !x.SameVenue || !x.SameNote || x.AnswersAClaim || x.LocationName != "m15-05 race venue" {
		t.Errorf("the same record pressed again: %+v, want Same at the race venue", x)
	}
	withNote := entry
	withNote.Note = "came in early"
	if x := existing(withNote); x.Same() || !x.SameVenue || x.SameNote {
		t.Errorf("another note: %+v, want the same venue and another note", x)
	}
	// ANOTHER VENUE: the person's home venue is now another one, so this press would file the
	// record there; the row is at the first.
	var second uuid.UUID
	if err := w.owner.QueryRow(w.ctx, `INSERT INTO locations (tenant_id, name, static_ips, shift_start, shift_end)
	                                   VALUES ($1, 'm15-05 second venue', '{198.51.100.0/24}', '09:00', '17:00') RETURNING id`, w.tenant).Scan(&second); err != nil {
		t.Fatal(err)
	}
	w.exec(`UPDATE employees SET location_id = $3 WHERE tenant_id = $1 AND id = $2`, w.tenant, w.who, second)
	if x := existing(entry); x.Same() || x.SameVenue || !x.SameNote || x.LocationName != "m15-05 race venue" {
		t.Errorf("another venue: %+v, want another venue and the row's own venue named", x)
	}
	if n := w.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1 AND channel = 'manual'`, w.tenant); n != 1 {
		t.Errorf("%d manual records, want 1: nothing is written for a taken minute", n)
	}
}
