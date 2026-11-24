package handler

// appaccount.go -- "Delete my account" in the staff app (M16-37; Apple 5.1.1(v)): POST
// /app/v1/account/delete, nonce-signed, body {} -> 204. The person is signed out everywhere and
// the request reaches the employer (the People card; the owner's e-mail comes with M16-35). Their
// clock-in records are NOT deleted (§4.3): the employer controls them. tenant.Staff.RequestDeletion
// says what is and is not touched.
//
// NOTHING BUT IDS IS LOGGED (§7).

import (
	"context"
	"errors"
	"net/http"

	"github.com/atknatk/tappa/internal/domain/tenant"
)

// appDeletion is the consumer's view of tenant.Staff (CLAUDE.md §7).
type appDeletion interface {
	RequestDeletion(ctx context.Context, r tenant.DeletionRequest) (tenant.DeletionRequested, error)
}

// ServeDeletion turns the app's "Delete my account" on.
func (a *App) ServeDeletion(d appDeletion) error {
	if isNil(d) {
		return errors.New("handler: nil deletion service")
	}
	a.deletion = d
	return nil
}

// AccountDelete serves POST /app/v1/account/delete.
func (a *App) AccountDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	if a.deletion == nil {
		writeAppError(w, appErrNotFound)
		return
	}
	body, ok := readAppBody(w, r)
	if !ok {
		return
	}
	dev, ok := a.authRequest(w, r, body, "account_delete", "")
	if !ok {
		return
	}
	if !decodeAppJSON(body, &struct{}{}) {
		writeAppError(w, appErrBadRequest)
		return
	}
	if _, err := a.deletion.RequestDeletion(ctx, tenant.DeletionRequest{
		TenantID: dev.TenantID, EmployeeID: dev.EmployeeID, AppDeviceID: dev.ID,
	}); err != nil {
		a.log.ErrorContext(ctx, "app: a deletion request failed", "employee_id", dev.EmployeeID, "err", err)
		writeAppError(w, appErrServer)
		return
	}
	setAppHeaders(w)
	w.WriteHeader(http.StatusNoContent)
}
