package handler

import (
	"reflect"
	"testing"

	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// docketSigns are the only field names on the read path's view types that the §4.7
// matcher refuses and the product needs: the two evidence SIGNS on DocketView. They pass
// only as words ("yes", "no", "n/a"); a sign that turned into a number would be a value
// travelling under a sign's name, and the matcher refuses it again.
var docketSigns = map[string]bool{"IPSign": true, "GPSSign": true}

// walkViewFields is §4.7 over a view type and every type it holds: no field whose name a
// coordinate, an address or a secret would arrive under (bannedFieldName), and every leaf a
// string, a count, a flag or a state. It returns how many fields it read and which types it
// reached, so a caller can refuse a walk that saw nothing or missed the type it is about.
//
// 🔴 DocketView IS WALKED HERE TOO, NOT SKIPPED. It used to be skipped on the claim that
// TestTypes_NothingOnTheReadPathCanCarryACoordinate walked it, which only read the start of
// each name: TapLat float64 on DocketView stayed green in both tests.
func walkViewFields(t *testing.T, roots ...reflect.Type) (int, map[reflect.Type]bool) {
	t.Helper()
	docket := reflect.TypeOf(components.DocketView{})
	seen := map[reflect.Type]bool{}
	fields := 0
	var walk func(reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Slice {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			fields++
			sign := rt == docket && docketSigns[f.Name] && f.Type.Kind() == reflect.String
			if why := bannedFieldName(f.Name); why != "" && !sign {
				t.Errorf("%s.%s: %s", rt.Name(), f.Name, why)
			}
			switch k := f.Type.Kind(); k {
			case reflect.String, reflect.Bool, reflect.Int, reflect.Uint8:
			case reflect.Struct, reflect.Slice:
				walk(f.Type)
			default:
				t.Errorf("%s.%s is a %s -- a view holds words, counts, flags and states", rt.Name(), f.Name, f.Type)
			}
		}
	}
	for _, rt := range roots {
		walk(rt)
	}
	return fields, seen
}

// TestSpikeViews_CarryNothingACoordinateCouldTravelIn is §4.7 on the review queue's view
// types (The Spike, M12-28): the queue, its items, the record in focus and its evidence tiles.
// A tile SAYS whether the network and the location placed the tap at the venue; a field that
// carried the address or the reading it was decided on would be one template edit from the
// screen.
//
// The positive control is the walk itself: it must reach the docket the items and the focus
// carry, and the tiles, or it is reading the wrong types.
func TestSpikeViews_CarryNothingACoordinateCouldTravelIn(t *testing.T) {
	roots := []reflect.Type{
		reflect.TypeOf(pages.ReviewView{}), reflect.TypeOf(pages.ReviewItem{}),
		reflect.TypeOf(components.SpikeFocusView{}), reflect.TypeOf(components.EvidenceTile{}),
	}
	fields, seen := walkViewFields(t, roots...)
	for _, must := range []reflect.Type{reflect.TypeOf(components.DocketView{}), reflect.TypeOf(components.EvidenceTile{})} {
		if !seen[must] {
			t.Errorf("the walk never reached %s; it is reading the wrong types", must)
		}
	}
	if want := reflect.TypeOf(components.DocketView{}).NumField() + reflect.TypeOf(components.EvidenceTile{}).NumField(); fields < want {
		t.Fatalf("the walk saw %d fields, fewer than the docket and a tile alone (%d)", fields, want)
	}
}
