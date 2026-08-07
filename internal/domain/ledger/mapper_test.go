package ledger

// mapper_test.go -- the net over the fact that this package has TWO mappers onto
// ONE Record (M6-04).
//
// 🔴 THE FAILURE IS SILENT AND THE SHAPE IS FAMILIAR. sqlc generates a distinct row
// type per query, so record() and queueRecord() are two hand-written copies of the
// same mapping. A column added to one query and mapped in one place leaves the
// other screen rendering a blank field -- no error, no test, just a docket that is
// missing its counter or its venue on one of the two pages. This is the "second
// representation" class this repository has paid for repeatedly, and the cheap
// version of the check (read both functions and hope) is the one that rots.
//
// SO THE PROPERTY IS ASSERTED OVER THE TYPE rather than over a list of field names:
// fill every column of both row types with a distinguishable non-zero value, run
// both mappers, and require the resulting Records to differ in EXACTLY the fields
// named below -- a CLOSED SET, so a newly-unmapped field fails and a newly-mapped
// one fails too until somebody says which it is.

import (
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/store"
)

// queueOnlyDifferences is the closed set of Record fields the two mappers are
// ALLOWED to disagree about, with the reason.
//
// Review: ListFlaggedForReview returns records that have NO decision yet -- that is
// the query's defining predicate -- so there is no outcome column to map and the
// field is empty by construction rather than by omission.
var queueOnlyDifferences = map[string]string{
	"Review":     "the queue is the set with no decision yet, so there is nothing to map",
	"ReviewNote": "same: a pending record has no review row, so it has no note either",
}

// fillStruct sets every field of a struct to a distinguishable non-zero value.
//
// IT IS REFLECTION RATHER THAN A LITERAL because a literal is a list: sqlc adds a
// field, the literal still compiles (Go zero-fills), and the test goes on passing
// over a column nobody mapped.
func fillStruct(t *testing.T, v any, seed int64) {
	t.Helper()
	rv := reflect.ValueOf(v).Elem()
	rng := rand.New(rand.NewSource(seed))
	for i := 0; i < rv.NumField(); i++ {
		f := rv.Field(i)
		name := rv.Type().Field(i).Name
		// Channel must be a value the mapper DERIVES from, so both sides agree on
		// Manual. Anything else would make Manual false on both and hide a mapping
		// that was never wired.
		if name == "Channel" {
			f.SetString("manual")
			continue
		}
		if name == "Type" {
			s := "in"
			f.Set(reflect.ValueOf(&s))
			continue
		}
		setNonZero(t, f, rng, name)
	}
}

func setNonZero(t *testing.T, f reflect.Value, rng *rand.Rand, name string) {
	t.Helper()
	switch f.Kind() {
	case reflect.String:
		f.SetString("value-" + name)
	case reflect.Bool:
		f.SetBool(true)
	case reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
		f.SetInt(int64(rng.Intn(90) + 10))
	case reflect.Ptr:
		p := reflect.New(f.Type().Elem())
		setNonZero(t, p.Elem(), rng, name)
		f.Set(p)
	case reflect.Struct:
		switch f.Type() {
		case reflect.TypeOf(time.Time{}):
			f.Set(reflect.ValueOf(time.Date(2026, 8, 6, 9, 30, 0, 0, time.UTC)))
		default:
			t.Fatalf("fillStruct does not know how to fill %s (%s); teach it rather than "+
				"skipping the field, or this test starts passing over unfilled columns",
				name, f.Type())
		}
	case reflect.Array:
		// uuid.UUID is [16]byte.
		if f.Type() == reflect.TypeOf(uuid.UUID{}) {
			f.Set(reflect.ValueOf(uuid.New()))
			return
		}
		t.Fatalf("fillStruct does not know how to fill %s (%s)", name, f.Type())
	default:
		t.Fatalf("fillStruct does not know how to fill %s (%s)", name, f.Type())
	}
}

// TestQueueRecord_CarriesEverythingTheDayRecordDoes.
func TestQueueRecord_CarriesEverythingTheDayRecordDoes(t *testing.T) {
	var dayRow store.ListPanelTransactionsRow
	var queueRow store.ListFlaggedForReviewRow
	fillStruct(t, &dayRow, 1)
	fillStruct(t, &queueRow, 1)

	day := record(dayRow)
	queue := queueRecord(queueRow)

	rt := reflect.TypeOf(Record{})
	// ANTI-VACUITY: a Record with no fields, or two mappers that both return the
	// zero value, would satisfy every comparison below.
	if rt.NumField() < 10 {
		t.Fatalf("Record has %d field(s); this comparison is not measuring a docket", rt.NumField())
	}
	filled := 0
	for i := 0; i < rt.NumField(); i++ {
		if !reflect.ValueOf(day).Field(i).IsZero() {
			filled++
		}
	}
	if filled < rt.NumField()-len(queueOnlyDifferences) {
		t.Fatalf("the DAY mapper left %d of %d Record fields zero from a fully-populated "+
			"row; the comparison below would pass over unmapped columns",
			rt.NumField()-filled, rt.NumField())
	}

	dv, qv := reflect.ValueOf(day), reflect.ValueOf(queue)
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		bothMapped := !dv.Field(i).IsZero() && !qv.Field(i).IsZero()
		reason, exempt := queueOnlyDifferences[name]

		if exempt {
			if !qv.Field(i).IsZero() {
				t.Errorf("Record.%s is listed as queue-only-empty (%q) and the queue mapper "+
					"filled it. Either the exemption is stale or the mapper is wrong; the "+
					"list is closed on purpose so this cannot drift.", name, reason)
			}
			// 🔴 AND THE DAY MAPPER MUST STILL FILL IT. Without this half the exemption
			// excuses a field NEITHER mapper fills, which is precisely what happens when
			// a column is dropped from the query — measured: removing rv.note from
			// ListPanelTransactions and from record() left this test green while the
			// panel stopped rendering the note entirely. The behavioural net caught that
			// one; this makes the type-level net catch it too.
			if dv.Field(i).IsZero() {
				t.Errorf("Record.%s is exempt for the QUEUE (%q), but the DAY mapper does "+
					"not fill it either. An exemption for one side is not an excuse for "+
					"both: either the column was dropped from the query, or the field is "+
					"dead and the exemption should go with it.", name, reason)
			}
			continue
		}
		if !bothMapped {
			t.Errorf("Record.%s is mapped by one of the two mappers and not the other "+
				"(day: %v, queue: %v).\n"+
				"sqlc gives each query its own row type, so record() and queueRecord() "+
				"are two copies of one mapping -- a column added to one and not the "+
				"other renders a blank field on one page with no error anywhere.\n"+
				"If the difference is deliberate, add %s to queueOnlyDifferences WITH "+
				"its reason.", name, !dv.Field(i).IsZero(), !qv.Field(i).IsZero(), name)
		}
	}
}

// TestQueueRecord_TheExemptionListIsNotAWildcard is the negative control for the
// mechanism above: an exemption that named a field the queue mapper DOES fill would
// silently excuse a real difference, so the check runs in both directions.
func TestQueueRecord_TheExemptionListIsNotAWildcard(t *testing.T) {
	rt := reflect.TypeOf(Record{})
	known := map[string]bool{}
	for i := 0; i < rt.NumField(); i++ {
		known[rt.Field(i).Name] = true
	}
	for name := range queueOnlyDifferences {
		if !known[name] {
			t.Errorf("queueOnlyDifferences names %q, which is not a field of Record. A "+
				"stale exemption excuses nothing and hides that fact.", name)
		}
	}
	if len(queueOnlyDifferences) >= rt.NumField() {
		t.Fatalf("every field of Record is exempt (%d of %d); the comparison checks nothing",
			len(queueOnlyDifferences), rt.NumField())
	}
}
