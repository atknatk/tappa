package db_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// The compile-time half: *db.OperatorDB is internal/operatorauth's Store and (OP-10,
// phase B) internal/handler/operator's LegalStore. It is asserted here because package db
// can import neither (both import db).
var (
	_ operatorauth.Store  = (*db.OperatorDB)(nil)
	_ operator.LegalStore = (*db.OperatorDB)(nil)
)

// TestOperatorDB_IsTheStoreAndNothingMore: the method set of *db.OperatorDB is
// operatorauth.Store's methods, operator.LegalStore's methods and Close -- DERIVED from
// the two interfaces, so a method added to the pool that neither the operator's sign-in
// nor its legal screen declares (a raw Exec, a WithTenant, a Ping a customer handler
// could take) is red here until it is argued for. OP-10 phase B widened the derivation by
// LegalStore -- the consumer the A phase named (operator.go's OP-10 section) -- and by
// nothing else; the two interfaces share no method name (checked: a shared name would
// let one method stand for two).
func TestOperatorDB_IsTheStoreAndNothingMore(t *testing.T) {
	store := reflect.TypeOf((*operatorauth.Store)(nil)).Elem()
	legal := reflect.TypeOf((*operator.LegalStore)(nil)).Elem()
	want := []string{"Close"}
	for _, it := range []reflect.Type{store, legal} {
		for i := 0; i < it.NumMethod(); i++ {
			if slices.Contains(want, it.Method(i).Name) {
				t.Fatalf("PREMISE: %s is declared twice across operatorauth.Store and operator.LegalStore", it.Method(i).Name)
			}
			want = append(want, it.Method(i).Name)
		}
	}
	slices.Sort(want)
	rt := reflect.TypeOf((*db.OperatorDB)(nil))
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		got = append(got, rt.Method(i).Name)
	}
	slices.Sort(got)
	if store.NumMethod() < 7 || legal.NumMethod() < 2 {
		t.Fatalf("PREMISE: operatorauth.Store has %d methods and operator.LegalStore %d; the derivation has gone blind",
			store.NumMethod(), legal.NumMethod())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("*db.OperatorDB's methods are %v, want operatorauth.Store's and operator.LegalStore's plus Close: %v", got, want)
	}
}
