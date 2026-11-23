package handler

// accountstaffshift.go -- the owner's "these are my staff's real shifts" switch on the account
// screen (M16-45, ADR 0050 (iv)). One checkbox, one POST, owner-only on both sides: the handler
// refuses a manager before reading the form, and the domain reads the owner from admin_users
// inside the write's own transaction.

import (
	"errors"
	"net/http"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
)

var accountStaffShiftHref = accountHref + "/staff-shifts"

func (a *AdminAuth) accountStaffShiftSave(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !mayEditAccount(id) {
		a.log.Warn("panel staff shift switch refused: this admin is not an owner",
			"actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)
		a.redirect(w, accountReturn("", "not-permitted"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAccountBody)
	if err := r.ParseForm(); err != nil {
		a.log.Warn("panel staff shift switch refused: the form could not be read")
		a.redirect(w, accountReturn("", "unreadable"))
		return
	}
	// A checkbox: present and "on" means on; absent means off. Anything else is not a press of
	// this form.
	var on bool
	switch r.PostFormValue("staff_shift_visible") {
	case "on":
		on = true
	case "":
	default:
		a.redirect(w, accountReturn("", "unreadable"))
		return
	}
	err := a.accounts.SetStaffShiftVisible(r.Context(), id.TenantID(), id.Admin.AdminUserID, on)
	switch {
	case err == nil:
		a.redirect(w, accountReturn("shifts", ""))
	case errors.Is(err, tenant.ErrStaffShiftNotPermitted):
		a.log.Warn("panel staff shift switch refused by the domain: not an active owner", "actor_id", id.Admin.AdminUserID)
		a.redirect(w, accountReturn("", "not-permitted"))
	default:
		a.log.Error("panel: writing the staff shift switch failed", "actor_id", id.Admin.AdminUserID, "err", err)
		a.redirect(w, accountReturn("", "unavailable"))
	}
}
