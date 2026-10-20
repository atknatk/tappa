package handler

// peoplereads_test.go -- §4.7 asserted STRUCTURALLY on the People screen's new domain reads
// (M12-17 ..., D3): no field a coordinate, an address or a secret could travel in, and every leaf
// a count, a word, a flag, an id, an instant or a Reading. The view types lane D builds on them get
// their own walkers; this pins the layer underneath, so a field added here is refused before any
// template can print it.

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// peopleReadTypes are the domain types the People reads hand to the panel.
var peopleReadTypes = []reflect.Type{
	reflect.TypeOf(ledger.StatusTally{}),
	reflect.TypeOf(ledger.LastTap{}),
	reflect.TypeOf(ledger.RecentTaps{}),
	reflect.TypeOf(tenant.PersonSummary{}),
}

// peopleReadFields are the fields M12 added to an existing type, whose other fields predate this
// walker (ledger.Person's HasHome, from M11 API-1, holds "hash" as a substring; ledger.RosterPage
// carries a *time.Location). They are checked by name and by type here; ledger.personFields closes
// Person's set.
var peopleReadFields = []struct {
	typ   reflect.Type
	field string
}{
	{reflect.TypeOf(ledger.Person{}), "Role"},
	{reflect.TypeOf(ledger.Person{}), "LastTap"},
	{reflect.TypeOf(ledger.RosterPage{}), "LastTaps"},
	{reflect.TypeOf(ledger.RosterScreen{}), "Tally"},
}

func TestPeopleReads_TheFieldsAddedToOlderTypesAreClean(t *testing.T) {
	want := map[string]reflect.Type{
		"Role":     reflect.TypeOf(""),
		"LastTap":  reflect.TypeOf(&ledger.LastTap{}),
		"LastTaps": reflect.TypeOf(ledger.Reading(0)),
		"Tally":    reflect.TypeOf(ledger.StatusTally{}),
	}
	for _, f := range peopleReadFields {
		sf, ok := f.typ.FieldByName(f.field)
		if !ok {
			t.Errorf("%s has no field %s", f.typ, f.field)
			continue
		}
		if why := bannedFieldName(sf.Name); why != "" {
			t.Errorf("%s.%s is a field a coordinate or a secret could travel in (%s)", f.typ, sf.Name, why)
		}
		if sf.Type != want[f.field] {
			t.Errorf("%s.%s is a %s, want %s", f.typ, sf.Name, sf.Type, want[f.field])
		}
	}
}

func TestPeopleReads_CarryNothingACoordinateCouldTravelIn(t *testing.T) {
	allowedStruct := map[reflect.Type]bool{reflect.TypeOf(time.Time{}): true}
	allowedLeaf := map[reflect.Type]bool{
		reflect.TypeOf(uuid.UUID{}):       true,
		reflect.TypeOf(ledger.Reading(0)): true,
		reflect.TypeOf(tenant.Reading(0)): true,
	}
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
			for !allowedLeaf[ft] && (ft.Kind() == reflect.Ptr || ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array) {
				ft = ft.Elem()
			}
			switch {
			case allowedStruct[ft], allowedLeaf[ft]:
			case ft.Kind() == reflect.String, ft.Kind() == reflect.Bool, ft.Kind() == reflect.Int:
			case ft.Kind() == reflect.Struct:
				walk(ft, path+rt.Name()+".")
			default:
				t.Errorf("%s%s.%s is of type %s -- a People figure is a count, a word, a flag, an id, an instant or a Reading",
					path, rt.Name(), f.Name, f.Type)
			}
		}
	}
	for _, typ := range peopleReadTypes {
		walk(typ, "")
	}
	if fields < 3*len(peopleReadTypes) {
		t.Fatalf("the walk saw %d field(s); it is reading the wrong types", fields)
	}
}
