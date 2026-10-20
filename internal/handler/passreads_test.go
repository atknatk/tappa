package handler

// passreads_test.go -- §4.7 asserted STRUCTURALLY on the Pass's new domain reads (M12-13 ...,
// D3): no field a coordinate, an address or a secret could travel in, and every leaf a count, a
// name, a flag, an id, an instant or a Reading. The view types lane D builds on them get their own
// walkers; this pins the layer underneath, so a field added here is refused before any template
// can print it.

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
)

func TestPassReads_CarryNothingACoordinateCouldTravelIn(t *testing.T) {
	allowedStruct := map[reflect.Type]bool{reflect.TypeOf(time.Time{}): true}
	allowedArray := map[reflect.Type]bool{reflect.TypeOf(uuid.UUID{}): true}
	seen := map[reflect.Type]bool{}
	fields := 0
	var walk func(reflect.Type, string)
	walk = func(rt reflect.Type, path string) {
		for rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Slice {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] || allowedStruct[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			fields++
			if why := bannedFieldName(f.Name); why != "" {
				t.Errorf("%s%s.%s is a field a coordinate or a secret could travel in (%s)", path, rt.Name(), f.Name, why)
			}
			ft := f.Type
			for ft.Kind() == reflect.Slice {
				ft = ft.Elem()
			}
			switch {
			case allowedStruct[ft], allowedArray[ft], ft == reflect.TypeOf(ledger.Reading(0)):
			case ft.Kind() == reflect.String, ft.Kind() == reflect.Bool, ft.Kind() == reflect.Int:
			case ft.Kind() == reflect.Struct:
				walk(ft, path+rt.Name()+".")
			default:
				t.Errorf("%s%s.%s is of type %s -- a Pass figure is a count, a name, a flag, an id, an instant or a Reading",
					path, rt.Name(), f.Name, f.Type)
			}
		}
	}
	for _, typ := range []reflect.Type{
		reflect.TypeOf(ledger.Floor{}),
		reflect.TypeOf(ledger.Tape{}),
	} {
		walk(typ, "")
	}
	if fields < 8 {
		t.Fatalf("the walk saw %d field(s); it is reading the wrong types", fields)
	}
}
