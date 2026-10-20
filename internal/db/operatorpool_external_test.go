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
// internal/handler/operator's LegalStore (OP-10, phase B), TenantStore (OP-11, phase B),
// PlaqueStore (OP-13, phase B), AuditStore (OP-14, phase B), BillingStore (OP-12, phase B),
// VATStore (OP-16, phase B), HoldingStore (M11 API-1a) and SuspensionStore (OP-15, phase C). It
// is asserted here because package db can import neither package (both import db).
var (
	_ operatorauth.Store       = (*db.OperatorDB)(nil)
	_ operator.LegalStore      = (*db.OperatorDB)(nil)
	_ operator.TenantStore     = (*db.OperatorDB)(nil)
	_ operator.PlaqueStore     = (*db.OperatorDB)(nil)
	_ operator.AuditStore      = (*db.OperatorDB)(nil)
	_ operator.BillingStore    = (*db.OperatorDB)(nil)
	_ operator.VATStore        = (*db.OperatorDB)(nil)
	_ operator.HoldingStore    = (*db.OperatorDB)(nil)
	_ operator.SuspensionStore = (*db.OperatorDB)(nil)
)

// TestOperatorDB_IsTheStoreAndNothingMore: the method set of *db.OperatorDB is
// operatorauth.Store's methods, operator.LegalStore's, operator.TenantStore's,
// operator.PlaqueStore's, operator.AuditStore's, operator.BillingStore's, operator.VATStore's,
// operator.HoldingStore's, operator.SuspensionStore's and Close -- DERIVED from the nine
// interfaces, so a method added to the pool that neither the operator's sign-in nor its legal,
// tenant, plaque, audit, billing, VAT or holdings screens nor its overview's suspension declare (a
// raw Exec, a WithTenant, a Ping a customer handler could take) is red here until it is argued
// for. OP-10 phase B widened the derivation by LegalStore, OP-11 phase B by TenantStore, OP-13
// phase B by PlaqueStore, OP-14 phase B by AuditStore, OP-12 phase B by BillingStore, OP-16 phase
// B by VATStore, M11 API-1a by HoldingStore and OP-15 phase C by SuspensionStore -- the consumers
// the phases named (operator.go's OP-10, OP-11, OP-13, OP-14, OP-12, OP-16, API-1a and OP-15
// sections) -- and by nothing else; the nine interfaces share no method name (checked: a shared
// name would let one method stand for two).
func TestOperatorDB_IsTheStoreAndNothingMore(t *testing.T) {
	store := reflect.TypeOf((*operatorauth.Store)(nil)).Elem()
	legal := reflect.TypeOf((*operator.LegalStore)(nil)).Elem()
	tenants := reflect.TypeOf((*operator.TenantStore)(nil)).Elem()
	plaques := reflect.TypeOf((*operator.PlaqueStore)(nil)).Elem()
	audit := reflect.TypeOf((*operator.AuditStore)(nil)).Elem()
	billing := reflect.TypeOf((*operator.BillingStore)(nil)).Elem()
	vat := reflect.TypeOf((*operator.VATStore)(nil)).Elem()
	holdings := reflect.TypeOf((*operator.HoldingStore)(nil)).Elem()
	suspension := reflect.TypeOf((*operator.SuspensionStore)(nil)).Elem()
	want := []string{"Close"}
	for _, it := range []reflect.Type{store, legal, tenants, plaques, audit, billing, vat, holdings, suspension} {
		for i := 0; i < it.NumMethod(); i++ {
			if slices.Contains(want, it.Method(i).Name) {
				t.Fatalf("PREMISE: %s is declared twice across operatorauth.Store, operator.LegalStore, operator.TenantStore, operator.PlaqueStore, operator.AuditStore, operator.BillingStore, operator.VATStore, operator.HoldingStore and operator.SuspensionStore", it.Method(i).Name)
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
	if store.NumMethod() < 7 || legal.NumMethod() < 2 || tenants.NumMethod() < 2 || plaques.NumMethod() < 1 || audit.NumMethod() < 1 ||
		billing.NumMethod() < 1 || vat.NumMethod() < 2 || holdings.NumMethod() < 4 || suspension.NumMethod() < 2 {
		t.Fatalf("PREMISE: operatorauth.Store has %d methods, operator.LegalStore %d, operator.TenantStore %d, operator.PlaqueStore %d, operator.AuditStore %d, operator.BillingStore %d, operator.VATStore %d, operator.HoldingStore %d and operator.SuspensionStore %d; the derivation has gone blind",
			store.NumMethod(), legal.NumMethod(), tenants.NumMethod(), plaques.NumMethod(), audit.NumMethod(), billing.NumMethod(), vat.NumMethod(),
			holdings.NumMethod(), suspension.NumMethod())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("*db.OperatorDB's methods are %v, want operatorauth.Store's, operator.LegalStore's, operator.TenantStore's, operator.PlaqueStore's, operator.AuditStore's, operator.BillingStore's, operator.VATStore's, operator.HoldingStore's and operator.SuspensionStore's plus Close: %v", got, want)
	}
}
