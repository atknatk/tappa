package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The GROUP SECTION -- GRP-1 (the user's decision of 2026-10-19; ADR 0027's GRP-1 note): the holding
// this business is a live member of, read only.
//
// 🔴 THE TENANT IS NEVER AN INPUT (§4.5). It is httpx.AdminOf(r)'s, and the request carries nothing
// else: no query parameter, no form. The holding is read through holding_group -- the closed list's
// function, which takes no argument and answers the session tenant's own holding alone -- and the
// two record figures are this business's own rows under its own RLS.
//
// 🔴 BOTH ROLES, AND NOTHING WRITES. An owner and a manager see the same page: the partner staff's
// records are already a manager's in Reports, and the holding itself is run from the operator's side,
// so there is no form here, no mountWriting entry and no audit row.
//
// 🔴 A BUSINESS IN NO GROUP GETS THE PANEL'S EMPTY STATE, NOT A 404. The route exists for every
// business (every row of pages.PanelSections is mounted -- "a tab whose link 404s" stays impossible),
// the TAB is drawn only for a member (PanelSection.GroupOnly, a.chrome), and a business that is not
// one -- never was, or has just left and follows a stale bookmark -- reads one sentence saying so.
// That sentence is about its own business, so it tells it nothing about anybody else.
//
// 🔴 §4.6 -- A FAILED READ IS A PROBLEM PAGE, never "not in a group".

// groupHref is the section's own URL, read from the section table.
var groupHref = mustSectionHref(pages.TabGroup)

func (a *AdminAuth) groupSection(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	screen, err := a.ledger.Group(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not read the business's group", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	// The section's read already answered the membership: the chrome takes it, not a second call.
	v := pages.GroupView{PanelChrome: a.chromeInGroup(r, pages.TabGroup, screen.Member)}
	fillGroupView(&v, screen)
	a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminGroup(v))
}

// fillGroupView maps the domain screen onto the view; every instant is converted here, in the
// business's zone (§6).
func fillGroupView(v *pages.GroupView, s ledger.GroupScreen) {
	v.Queried = s.Queried
	v.Member = s.Member
	if !s.Member {
		return
	}
	zone := s.Zone
	if zone == nil {
		zone = time.UTC
	}
	day := func(t time.Time) string { return t.In(zone).Format("2 Jan 2006") }
	for _, g := range s.Groups {
		h := pages.GroupHoldingView{Name: g.Name, Since: day(g.Since)}
		for _, m := range g.Members {
			h.Members = append(h.Members, pages.GroupMemberView{Name: memberName(m.Name), Code: m.Code, Joined: day(m.Joined), Self: m.Self})
		}
		v.Groups = append(v.Groups, h)
	}
	v.WindowLabel = "Last " + strconv.Itoa(ledger.GroupWindowDays) + " days · " + day(s.Window.From) + " – " + day(s.Window.To) + " · " + zone.String()
	v.ReportsHref = reportsHref
	v.PartnersMore = s.Partners.More
	for _, t := range s.Partners.Taps {
		v.Partners = append(v.Partners, partnerView(t, zone))
	}
	for _, t := range s.Partners.Manual {
		v.PartnerManual = append(v.PartnerManual, partnerView(t, zone))
	}
	v.AtPartnerVenues = strconv.FormatInt(s.AtPartnerVenues, 10)
}

// memberName is a member's name as a word: an absent one is said, never left blank (personName's rule).
func memberName(s string) string {
	if s == "" {
		return "A business with no name on file"
	}
	return s
}
