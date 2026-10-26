package adminauth

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
)

// shellname_test.go -- M14-12: the business's name the panel shell shows comes from the statement
// that proves the session (TouchAdminSession's tenants.name, ADR 0031 §5.3), so naming the business
// on every section costs no read. The handler half -- the chrome draws Resolved.TenantName -- is
// internal/handler's shell tests.

// TestVerify_CarriesTheBusinessNameTheStatementReturned: Verify copies tenants.name from the one
// statement it runs inside the business's transaction, unchanged (a name to escape included), and
// runs no second statement for it; the statement it ran is the one that joins tenants and returns
// the name.
//
// MUTATION (measured): the TenantName line removed from Verify's Resolved -> red.
func TestVerify_CarriesTheBusinessNameTheStatementReturned(t *testing.T) {
	for _, name := range []string{"Kebab Factory", `Ħaġar <Qim> & "Co"`} {
		data := &scriptedDB{tenant: uuid.New(), admin: uuid.New(), session: uuid.New(), role: "owner", tenantName: name}
		m, err := New(data, &config.Config{SessionHMACKey: []byte("0123456789abcdef0123456789abcdef")})
		if err != nil {
			t.Fatal(err)
		}
		tok, err := newToken()
		if err != nil {
			t.Fatal(err)
		}
		res, err := m.Verify(context.Background(), tok)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if res.TenantName != name {
			t.Errorf("Verify carries the business name %q, the statement returned %q", res.TenantName, name)
		}
		if data.statements != 1 {
			t.Errorf("Verify ran %d statements in the business's transaction, want 1 (TouchAdminSession)", data.statements)
		}
		if sql := strings.Join(strings.Fields(data.sql), " "); !strings.Contains(sql, "t.name AS tenant_name") {
			t.Errorf("the statement Verify ran does not return tenants.name:\n%s", sql)
		}
	}
}
