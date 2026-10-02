package db_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// The compile-time half: *db.OperatorDB is internal/operatorauth's Store. It is asserted
// here because package db cannot import operatorauth (operatorauth imports db).
var _ operatorauth.Store = (*db.OperatorDB)(nil)

// TestOperatorDB_IsTheStoreAndNothingMore: the method set of *db.OperatorDB is
// operatorauth.Store's methods plus Close -- DERIVED from the Store interface, so a
// method added to the pool that the operator's sign-in does not need (a raw Exec, a
// WithTenant, a Ping a customer handler could take) is red here until it is argued for.
func TestOperatorDB_IsTheStoreAndNothingMore(t *testing.T) {
	store := reflect.TypeOf((*operatorauth.Store)(nil)).Elem()
	want := []string{"Close"}
	for i := 0; i < store.NumMethod(); i++ {
		want = append(want, store.Method(i).Name)
	}
	slices.Sort(want)
	rt := reflect.TypeOf((*db.OperatorDB)(nil))
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		got = append(got, rt.Method(i).Name)
	}
	slices.Sort(got)
	if store.NumMethod() < 7 {
		t.Fatalf("PREMISE: operatorauth.Store has %d methods; the derivation has gone blind", store.NumMethod())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("*db.OperatorDB's methods are %v, want operatorauth.Store's plus Close: %v", got, want)
	}
}
