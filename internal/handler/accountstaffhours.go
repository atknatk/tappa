package handler

// accountstaffhours.go -- the owner's My hours setting on the account screen (M16-30). One select,
// one POST, owner-only on both sides: the handler refuses a manager before reading the form, and
// the domain reads the owner from admin_users inside the write's own transaction.

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
)

var accountStaffHoursHref = accountHref + "/staff-hours"

func (a *AdminAuth) accountStaffHoursSave(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !mayEditAccount(id) {
		a.log.Warn("panel My hours setting refused: this admin is not an owner",
			"actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)
		a.redirect(w, accountReturn("", "not-permitted"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAccountBody)
	if err := r.ParseForm(); err != nil {
		a.log.Warn("panel My hours setting refused: the form could not be read")
		a.redirect(w, accountReturn("", "unreadable"))
		return
	}
	// "0" is off; anything but 0, 1, 2, 4 or 13 is not a press of this form.
	var weeks *int
	n, err := strconv.Atoi(r.PostFormValue("staff_hours_weeks"))
	switch {
	case err != nil:
		a.redirect(w, accountReturn("", "unreadable"))
		return
	case n == 0:
	case tenant.ValidStaffHoursWeeks(n):
		weeks = &n
	default:
		a.redirect(w, accountReturn("", "unreadable"))
		return
	}
	err = a.accounts.SetStaffHoursWeeks(r.Context(), id.TenantID(), id.Admin.AdminUserID, weeks)
	switch {
	case err == nil:
		a.redirect(w, accountReturn("hours", ""))
	case errors.Is(err, tenant.ErrStaffHoursNotPermitted):
		a.log.Warn("panel My hours setting refused by the domain: not an active owner", "actor_id", id.Admin.AdminUserID)
		a.redirect(w, accountReturn("", "not-permitted"))
	default:
		a.log.Error("panel: writing the My hours setting failed", "actor_id", id.Admin.AdminUserID, "err", err)
		a.redirect(w, accountReturn("", "unavailable"))
	}
}
