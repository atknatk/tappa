package db_test

import (
	"testing"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/billing"
)

// TestTenantBilling_FivePagesOfTwelveAreTheTenantsHistoryCap holds the operator's page bound
// (orchestrator decision K12-3) to the tenant's own history depth: MaxTenantBillingPage pages
// of TenantBillingMonthsPerPage months are billing.HistoryCap months -- the furthest back the
// tenant's own screen lists its frozen periods. It lives outside package db because package db
// cannot import internal/domain/billing (billing imports db). Migration 00032 writes the same
// two numbers into op_begin_read (page 1..5) and op_read_tenant_billing (twelve months a page,
// the page bounded to 1..5); TestOpBeginRead_TheBillingKindBindsTheTenantAndAPage and
// TestOpReadTenantBilling_PagesAreBoundedInTheBody hold the database to these constants.
func TestTenantBilling_FivePagesOfTwelveAreTheTenantsHistoryCap(t *testing.T) {
	if got := db.MaxTenantBillingPage * db.TenantBillingMonthsPerPage; got != billing.HistoryCap {
		t.Errorf("MaxTenantBillingPage x TenantBillingMonthsPerPage = %d x %d = %d, want billing.HistoryCap (%d)",
			db.MaxTenantBillingPage, db.TenantBillingMonthsPerPage, got, billing.HistoryCap)
	}
	if db.TenantBillingMonthsPerPage != 12 {
		t.Errorf("TenantBillingMonthsPerPage is %d; a page is a year of months (00032's generate_series(0, 11))", db.TenantBillingMonthsPerPage)
	}
}
