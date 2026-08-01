package checkin

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/store"
)

// TestTransaction_CarriesThePracticeColumn pins the FEED of the engine-side
// practice guard, which an audit found was the half nothing held.
//
// 🔴 THE MEASUREMENT THAT PRODUCED THIS TEST. §5's "a practice record never holds
// the chain open" is guarded twice — gather withholds such a row, and
// tap.resolveDirection refuses one if it arrives anyway. Deleting EITHER guard
// alone left the suite green until gather's side got its own test; and then an
// audit found a THIRD equivalent mutant hiding underneath both:
//
//	transaction()'s `Practice: t.Practice` -> `Practice: false`
//
// left the suite green too. It is equivalent TODAY for a precise reason —
// tap.Transaction.Practice has exactly one consumer (resolveDirection), and with
// gather's filter in place that field is never true in production, so nothing can
// observe the mapper dropping it. What that really means is that the redundancy is
// not two live guards but ONE live guard plus a spare whose SUPPLY is unpinned:
// the day somebody removes gather's filter believing "the engine guards it
// anyway", the engine would be guarding a field that arrives false.
//
// So this is not a mapper's getter being tested for its own sake. It is the one
// assertion that makes the spare real, and it is deliberately a pure unit test —
// the property is "the column reaches the domain value", which needs no database
// to state and no database to break.
func TestTransaction_CarriesThePracticeColumn(t *testing.T) {
	t.Parallel()
	dir := "in"
	for _, practice := range []bool{false, true} {
		row := store.Transaction{
			ID:         uuid.New(),
			OccurredAt: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC),
			Type:       &dir,
			Practice:   practice,
		}
		got := transaction(row)
		if got.Practice != practice {
			t.Errorf("practice column %v arrived as %v: internal/domain/tap's stale-practice "+
				"guard reads this field, and a guard fed a constant is not a guard", practice, got.Practice)
		}
		if got.ID != row.ID || !got.OccurredAt.Equal(row.OccurredAt) || got.Direction != "in" {
			t.Errorf("the rest of the mapping drifted: %+v", got)
		}
	}
}
