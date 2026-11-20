package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE PANEL'S "PHONES WAITING FOR APPROVAL" (M16-23): at the top of the people section, the
// business's open manager-approval requests -- each with the person, their venue, the pairing code
// their phone shows and when they asked -- and Approve / Reject. internal/domain/tenant
// staffapproval.go decides (one way, one transaction with its audit row, the self-approval rule);
// this file reads the press and says what happened.
//
// WHO MAY: any admin of the business, owner or manager (staffapproval.go's SEÇİM).
//
// ON THE MAIN WRITE CHAIN (mountWriting's first group): a suspended business can approve no phone.
// ADR 0025 §3's list of writes a suspension does not stop is closed and names no approval;
// widening it is that decision's edit, not this card's (M16-24 carries it as an open item).

// employeeApprovalHref is the one write route, under the section's own URL.
var employeeApprovalHref = employeesHref + "/approval"

// approvalWords is the closed vocabulary of the ?approval= notice: what one press did. Every word
// is produced by a branch of employeeApproval.
var approvalWords = []string{"approved", "rejected", "self", "closed", "unknown", "unreadable"}

// approvalViews maps the open requests onto the card's rows: the code grouped as the phone shows it
// ("482 913"), the time in the business's zone.
func approvalViews(rows []tenant.PendingApproval, zone *time.Location, now time.Time) []pages.ApprovalRowView {
	out := make([]pages.ApprovalRowView, 0, len(rows))
	for _, p := range rows {
		code := groupedPairingCode(p.PairingCode)
		client := "Browser"
		if p.Client == "app" {
			client = "App"
		}
		out = append(out, pages.ApprovalRowView{
			ID:       p.ID.String(),
			Name:     p.EmployeeName,
			Venue:    p.LocationName,
			Client:   client,
			Code:     code,
			Asked:    askedWords(p.CreatedAt, now, zone),
			Deadline: p.ExpiresAt.In(zone).Format("15:04"),
			Replaces: p.ReplacesPhone,
		})
	}
	return out
}

// askedWords is "just now" / "N min ago" / the clock time for an older request.
func askedWords(at, now time.Time, zone *time.Location) string {
	d := now.Sub(at)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		n := int(d / time.Minute)
		if n == 1 {
			return "1 min ago"
		}
		return strconv.Itoa(n) + " min ago"
	default:
		return at.In(zone).Format("15:04")
	}
}

// maxApprovalBody bounds the press: a uuid, one word and the form binding.
const maxApprovalBody = 2 << 10

// employeeApproval is one press of Approve or Reject.
func (a *AdminAuth) employeeApproval(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxApprovalBody)
	if err := r.ParseForm(); err != nil {
		a.log.Warn("panel activation approval refused: the form could not be read")
		a.redirect(w, approvalReturn("unreadable"))
		return
	}
	requestID, err := uuid.Parse(strings.TrimSpace(r.PostFormValue("id")))
	decision := r.PostFormValue("decision")
	if err != nil || (decision != "approve" && decision != "reject") {
		a.log.Warn("panel activation approval refused: the posted request or decision is not one we know")
		a.redirect(w, approvalReturn("unreadable"))
		return
	}
	_, err = a.staff.DecideApproval(r.Context(), tenant.DecideApprovalCommand{
		TenantID: id.TenantID(), RequestID: requestID, ActorID: id.Admin.AdminUserID, Approve: decision == "approve",
	})
	switch {
	case err == nil && decision == "approve":
		a.redirect(w, approvalReturn("approved"))
	case err == nil:
		a.redirect(w, approvalReturn("rejected"))
	case errors.Is(err, tenant.ErrSelfApproval):
		a.redirect(w, approvalReturn("self"))
	case errors.Is(err, tenant.ErrApprovalClosed):
		a.redirect(w, approvalReturn("closed"))
	case errors.Is(err, tenant.ErrUnknownApproval):
		a.redirect(w, approvalReturn("unknown"))
	default:
		a.log.Error("panel: could not decide an activation request", "err", err, "request_id", requestID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	}
}

// approvalReturn is the 303 target: the people section with the word (a member of approvalWords).
func approvalReturn(word string) string {
	return employeesHref + "?approval=" + word
}
