package db_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// The compile-time half: *db.OperatorDB is internal/operatorauth's Store and
// internal/handler/operator's LegalStore (OP-10, phase B) and TenantStore (OP-11, phase
// B). It is asserted here because package db can import neither package (both import db).
var (
	_ operatorauth.Store   = (*db.OperatorDB)(nil)
	_ operator.LegalStore  = (*db.OperatorDB)(nil)
	_ operator.TenantStore = (*db.OperatorDB)(nil)
)

// TestOperatorDB_IsTheStoreAndNothingMore: the method set of *db.OperatorDB is
// operatorauth.Store's methods, operator.LegalStore's, operator.TenantStore's and Close --
// DERIVED from the three interfaces, so a method added to the pool that neither the
// operator's sign-in nor its legal or tenant screens declare (a raw Exec, a WithTenant, a
// Ping a customer handler could take) is red here until it is argued for. OP-10 phase B
// widened the derivation by LegalStore and OP-11 phase B by TenantStore -- the consumers
// the A phases named (operator.go's OP-10 and OP-11 sections) -- and by nothing else; the
// three interfaces share no method name (checked: a shared name would let one method
// stand for two).
func TestOperatorDB_IsTheStoreAndNothingMore(t *testing.T) {
	store := reflect.TypeOf((*operatorauth.Store)(nil)).Elem()
	legal := reflect.TypeOf((*operator.LegalStore)(nil)).Elem()
	tenants := reflect.TypeOf((*operator.TenantStore)(nil)).Elem()
	want := []string{"Close"}
	for _, it := range []reflect.Type{store, legal, tenants} {
		for i := 0; i < it.NumMethod(); i++ {
			if slices.Contains(want, it.Method(i).Name) {
				t.Fatalf("PREMISE: %s is declared twice across operatorauth.Store, operator.LegalStore and operator.TenantStore", it.Method(i).Name)
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
	if store.NumMethod() < 7 || legal.NumMethod() < 2 || tenants.NumMethod() < 2 {
		t.Fatalf("PREMISE: operatorauth.Store has %d methods, operator.LegalStore %d and operator.TenantStore %d; the derivation has gone blind",
			store.NumMethod(), legal.NumMethod(), tenants.NumMethod())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("*db.OperatorDB's methods are %v, want operatorauth.Store's, operator.LegalStore's and operator.TenantStore's plus Close: %v", got, want)
	}
}
