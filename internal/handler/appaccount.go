package handler

// appaccount.go -- "Delete my account" in the staff app (M16-37; Apple 5.1.1(v)): POST
// /app/v1/account/delete, nonce-signed, body {} -> 204. The person is signed out everywhere and
// the request reaches the employer: the People card, and every owner's e-mail -- handed to the
// owners' notices queue (ownernotice.go) after the request committed, so the 204 never waits for a
// relay. Their
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

// ownerNotifier is what the request needs from the owners' notices (*OwnerNotices): a hand-over that
// never blocks and never fails.
type ownerNotifier interface {
	Submit(ctx context.Context, n OwnerNotice)
}

// ServeDeletion turns the app's "Delete my account" on, with the owners' notice: the two come
// together, so no deployment can take the request without telling the owners.
func (a *App) ServeDeletion(d appDeletion, owners ownerNotifier) error {
	if isNil(d) {
		return errors.New("handler: nil deletion service")
	}
	if isNil(owners) {
		return errors.New("handler: nil owner notices")
	}
	a.deletion, a.owners = d, owners
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
	done, err := a.deletion.RequestDeletion(ctx, tenant.DeletionRequest{
		TenantID: dev.TenantID, EmployeeID: dev.EmployeeID, AppDeviceID: dev.ID,
	})
	if err != nil {
		a.log.ErrorContext(ctx, "app: a deletion request failed", "employee_id", dev.EmployeeID, "err", err)
		writeAppError(w, appErrServer)
		return
	}
	// Active and deactivated alike: the request is the employer's to answer either way. Submit only
	// queues (or, when the queue will not take it, writes the one undelivered row) and returns. A
	// deactivated person's repeat inside tenant.DeletionRepeatWindow wrote nothing, and the owners
	// were told the first time: no notice, the same 204.
	if !done.Repeat {
		a.owners.Submit(ctx, OwnerNotice{
			Kind: ownerNoticeDeletionRequest, TenantID: dev.TenantID, EmployeeID: dev.EmployeeID,
			EmployeeName: done.EmployeeName, At: done.At, Zone: ownerZone(done.Timezone),
			Deactivated: done.Deactivated,
		})
	}
	setAppHeaders(w)
	w.WriteHeader(http.StatusNoContent)
}
