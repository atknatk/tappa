package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The panel's SUSPENSION GATE — ADR 0025 §3, OP-15 phase B.
//
// A business the operator suspended keeps its taps, its activations, its reads, its
// exports and its sign-in; what stops is its panel's WRITES, and the business is told
// why in one sentence. The gate is the LAST ring of ProtectWriting
// (floodGate -> sameOriginGate -> requireAdmin -> sessionGate -> suspensionGate), so
// every route mounted behind ProtectWriting is gated by construction and a new one
// joins the day it is written. The routes that are NOT gated are a closed list in
// mountWriting's sibling group (dashboard.go) plus the three sign-in routes that never
// were behind the chain; ADR 0025 §3 names its eight and gives each its reason, and its
// M11 note adds K-26's defences (revoking API keys, switching the webhook off -- the
// latter by internal/webhook/endpoints' OpenWhileSuspended).
//
// 🔴 WHY AFTER sessionGate. The gate WRITES a row on every refusal. Ahead of the
// session budget, every refused POST would be a free append to a table nobody can
// delete from — the "unbounded row is a write primitive" lesson adminlogin.go records.
// Behind it, flood and session budgets bound the refusals as they bound everything
// else.
//
// 🔴 WHERE THE SUSPENSION COMES FROM. TouchAdminSession reads the two tenants columns in
// the same statement that proves the session live (db/queries/admins.sql), Verify
// copies them into adminauth.Resolved, and httpx.AdminIdentity.Suspension hands them
// here. No second query, no cache: a suspension committed now is in force on the
// session's next request. A tenant row that statement cannot see is no session at all
// (requireAdmin's redirect), never "not suspended".
//
// WHAT IT IS NOT (ADR 0025 "Sayılı sınırlar" 1, 2): it is an application gate, not a
// database lock — tappa_app keeps every write privilege, so SQL injection or code
// execution in the process passes it — and a write that passed the gate a moment
// before the suspension committed may still commit after it.
//
// 🔴 THE TAP PATH NEVER READS IT (§4.6, ADR 0025 §4): internal/domain/tap,
// internal/policy and internal/domain/checkin do not name a suspension, and a
// suspended business's taps get the decision, the trust and the record an unsuspended
// one gets (TestSuspensionDB_TapsAreDecidedAsIfThereWereNoSuspension).

// ActionTenantWriteRefused is the audit action of one refused write. The row is the
// tenant's OWN (tappa_app, in the tenant's context, RLS) — not the operator's §4.5
// exception — and it says which route was refused, by whom and under which code.
const ActionTenantWriteRefused = "tenant.write_refused"

// suspensionRefusalDetail is the refusal row's whole detail: {"reason": <code>}. A
// purpose-built struct (internal/audit's rule) so no form value, address, name or
// cookie can ride into an append-only row by somebody widening a map.
type suspensionRefusalDetail struct {
	Reason string `json:"reason"`
}

// The sentences a suspended business reads, on the panel's strip and on the 403 page.
//
// 🔴 ONE NEUTRAL SENTENCE IS SHARED BY THREE INPUTS (ADR 0025 §3, "Nötr cümle
// PAYLAŞILIR"): security_hold, customer_request and a code this build does not know.
// The repository is public, so a sentence of security_hold's own would tell anybody
// who searched the source exactly which suspension they were looking at — and the
// likeliest reader of a security hold is the person who took over the account. Shared,
// the three are the same words on the screen. Only non_payment and terms_breach carry
// their own sentence, because knowing the reason is how a business removes it.
// TestSuspensionSentences_OnlyTwoReasonsHaveTheirOwnSentence pins the sharing.
//
// The code itself is never rendered (TestSuspensionScreens_NeverRenderTheReasonCode).
const (
	suspensionSentenceNonPayment  = "A payment to Taptime is outstanding. Once it is settled, Taptime switches changes back on."
	suspensionSentenceTermsBreach = "Taptime needs to resolve a question about the terms of service with this business. Contact Taptime to sort it out."
	suspensionSentenceNeutral     = "Taptime has paused changes to this business. Contact Taptime to find out more."
)

// suspensionSentences is the closed map from code to sentence -- the panel's copy of
// the reason set (ADR 0025 §1's fourth).
//
// 🔴 ITS KEYS ARE PLAIN STRINGS, NOT internal/db's SuspensionReason CONSTANTS, AND THAT
// IS ADR 0021 §3.6: internal/db declares those in its operator file, and the customer
// panel uses none of the operator's names (TestCustomerPanel_ImportsNoOperatorPackage).
// The copy is bound by a TEST instead: TestSuspensionSentences_CoverTheClosedReasonSet
// holds the keys equal to db.SuspensionReasons() (a test may read internal/db; this
// file may not use that name), and TestOperator00063_TheReasonSetIsOneSetInEveryCopy
// binds that set to the schema's CHECK and to op_suspend_tenant's refusal.
var suspensionSentences = map[string]string{
	"non_payment":      suspensionSentenceNonPayment,
	"terms_breach":     suspensionSentenceTermsBreach,
	"security_hold":    suspensionSentenceNeutral,
	"customer_request": suspensionSentenceNeutral,
}

// suspensionSentence is the sentence for a code; a code outside the map (a newer
// binary's code reaching an older one) gets the neutral sentence — never an empty one,
// never the code.
func suspensionSentence(code string) string {
	if s, ok := suspensionSentences[code]; ok {
		return s
	}
	return suspensionSentenceNeutral
}

// suspensionHeading heads both the strip and the refusal page.
const suspensionHeading = "Changes are paused"

// suspensionStillWorks is what a suspended business can still do, said on the strip so
// nobody concludes their records stopped (§4.6). The four record paths are ADR 0025's
// exception list (K-B1 = (i)); reads and exports are §4.
const suspensionStillWorks = "Taps are still recorded. You can still read and export your records, add and invite staff, and enter records by hand."

// suspensionRefusedHint is the refusal page's "what now" line.
const suspensionRefusedHint = "Nothing was changed. Taps are still recorded, and reading and exporting still work."

// suspensionProblem is the 403 page for a refused write.
func suspensionProblem(code string) pages.ProblemView {
	return pages.ProblemView{
		Title:   suspensionHeading,
		Message: suspensionSentence(code),
		Hint:    suspensionRefusedHint,
	}
}

// suspensionGate refuses every request behind it from a suspended business: first the
// tenant's own audit row, then 403.
//
// 🔴 THE ROW COMES FIRST AND THE ANSWER DOES NOT DEPEND ON IT (ADR 0025 §3, refusal 1
// and 3). A row that could not be written is an ERROR line naming exactly three things
// — the tenant, the SQLSTATE, the code from the closed set — and the request is refused
// all the same: a gate that opened because its trail failed would be a gate an outage
// could lift. No form value, address, name, cookie or session value reaches the line;
// the error's own text is not printed either, because a database error's text is SQL
// and arguments (TestSuspensionGate_AFailedRefusalRowStillRefusesAndLogsThreeFields).
//
// The row's target is the ROUTE PATTERN the router matched — a fixed set, with no id
// in any path — never the request's path. The two formats are chosen by that same
// pattern: the relay's three routes get its JSON word, everything else the problem page.
func (a *AdminAuth) suspensionGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := httpx.AdminOf(r)
		s, suspended := id.Suspension()
		if !suspended {
			next.ServeHTTP(w, r)
			return
		}
		route := matchedRoute(r)
		if _, err := a.audit.Record(r.Context(), audit.Event{
			TenantID: id.TenantID(),
			ActorID:  ptr(id.AdminUserID()),
			Action:   ActionTenantWriteRefused,
			Target:   route,
			Detail:   suspensionRefusalDetail{Reason: s.Reason},
		}); err != nil {
			a.log.Error("panel: a suspended business's refused write could not be recorded; it is refused anyway",
				"tenant_id", id.TenantID(), "sqlstate", sqlstateOf(err), "reason", reasonWord(s.Reason))
		}
		if encodeRelayRoutes[route] {
			refuseSuspendedRelay(w)
			return
		}
		a.renderProblem(w, r, http.StatusForbidden, suspensionProblem(s.Reason))
	})
}

// matchedRoute is the pattern the router matched for r. It is read from chi's own
// routing context, so it can only be a pattern somebody registered; "" when there is
// none (a handler driven outside a router), which no registered route produces.
func matchedRoute(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		return rc.RoutePattern()
	}
	return ""
}

// sqlstateOf is a database error's SQLSTATE, and "none" for an error that carries
// none (a cancelled context, a closed pool). Only the code: never the message.
func sqlstateOf(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return "none"
}

// reasonWord is the code for a log line, from the closed set only: a code this build
// does not know is logged as "unrecognised" rather than as whatever the row held.
func reasonWord(code string) string {
	if _, ok := suspensionSentences[code]; ok {
		return code
	}
	return "unrecognised"
}

// suspensionStrip builds the chrome's strip for a suspended business: since when, in
// the business's own timezone (§6: UTC in the database, local only at render), and the
// reason's sentence.
//
// 🔴 IT COSTS ONE READ, AND ONLY FOR A SUSPENDED BUSINESS. The suspension itself rides
// on the session statement; the TIMEZONE does not, and is read here from the business's
// settings. A failed read does not fail the page and does not drop the strip: the date
// is printed in UTC and says so.
func (a *AdminAuth) suspensionStrip(ctx context.Context, tenantID uuid.UUID, s adminauth.Suspension) pages.PanelSuspension {
	zone := time.UTC
	if st, err := a.accounts.Settings(ctx, tenantID); err != nil {
		a.log.ErrorContext(ctx, "panel: could not read the business's timezone for the suspension strip; its date is shown in UTC",
			"tenant_id", tenantID, "err", err)
	} else {
		zone = a.accountZone(st.Timezone)
	}
	// A date in UTC says so, whether UTC is the business's own zone or the fallback
	// (accountZone answers time.UTC for a name it cannot load, and logs it).
	label := ""
	if zone == time.UTC {
		label = " (UTC)"
	}
	return pages.PanelSuspension{
		Heading:    suspensionHeading,
		Since:      s.Since.In(zone).Format("2 Jan 2006") + label,
		Sentence:   suspensionSentence(s.Reason),
		StillWorks: suspensionStillWorks,
	}
}
