package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
)

// THE PUNCH FEED (M11 API-7; ADR 0028 §2, §4, §5, §8, §10; K-4, K-5, K-13, K-31, K-32, K-40):
//
//	GET /v1/punches?scope=employer|location[&cursor=…][&limit=…]
//
// A page of the key's business's punch events in its feed order, for one scope (§2):
//
//	employer   the punches of the business's own employees -- at its own plaques and at a holding
//	           peer's (there the location fields name the PEER's location, K-32)
//	location   every punch at the business's plaques -- a holding peer's employees' included (there
//	           the employee fields name the PEER's employee)
//
// A single-tenant punch is in both scopes; a holding punch is in the employer's employer scope and
// the plaque's business's location scope (plan §2 items 27, 30, 31). The business is the KEY's,
// never one the request names, and the scope comes from the request every time: the cursor carries
// neither (tenant.PunchFeed's header).
//
// THE QUERY PARAMETERS are API-5's body rule applied to the query: scope, cursor, limit, from and to
// exactly, each at most once, scope required; any other name -- a misspelling, another case -- is
// 400, and so is a query string that does not parse. from and to together are the history query
// (API-7b, history.go); without them the request reads the feed. A misspelled `curser` silently ignored would restart a reader at the beginning of the feed;
// a misspelled `scop` defaulted would hand one scope's reader the other's punches.
//
// THE ANSWER is {data, nextCursor, hasMore}. nextCursor is ALWAYS a cursor -- also on an empty
// page, where it is the position the reader has caught up to (the request's own, or further when the
// feed had nothing of this scope in between) -- so a poller keeps one value per scope (the two scopes
// share one feed, so a cursor carried across skips) and never goes back. A
// page may hold fewer events than limit, even none, while hasMore is true (the window,
// tenant.PunchFeedWindow); hasMore false says the reader has every event the feed held when the page
// was read.
//
// Open while the business is suspended (§10): a read does not pass the suspension checkpoint.
//
// NOTHING BUT THE FEED'S OWN COLUMNS ANSWERS: no e-mail, coordinate, address, note, policy context,
// verdict or rule (K-1f, K-39) -- punch_events carries none, and the answer is built field by field.

// Punches is the feed's reader (*tenant.PunchFeed), declared at the consumer (§7).
type Punches interface {
	PunchPage(ctx context.Context, tenantID uuid.UUID, role tenant.FeedRole, after int64, limit int) (tenant.PunchPage, error)
	HistoryPage(ctx context.Context, tenantID uuid.UUID, role tenant.FeedRole, q tenant.HistoryQuery, limit int) (tenant.HistoryPage, error)
}

var _ Punches = (*tenant.PunchFeed)(nil)

const (
	punchesPath   = "/punches"
	punchIDPrefix = "pch_"
	tagIDPrefix   = "tag_"
	// cursorPrefix makes the cursor a value of its own shape: a bare number would invite arithmetic
	// on it, and a later cursor of another meaning (the history query's) can be told apart.
	cursorPrefix = "cur_"
	paramScope   = "scope"
	paramLimit   = "limit"
	paramFrom    = "from"
	paramTo      = "to"
)

var punchParameters = []string{paramScope, fieldCursor, paramLimit, paramFrom, paramTo}

var (
	problemPunchParameter = &Problem{Code: CodeInvalidRequest,
		Message: "This endpoint takes the query parameters scope, cursor, limit, from and to only, each at most once, in a well-formed query string.",
		Details: map[string]any{"allowed": punchParameters}}
	problemScope = &Problem{Code: CodeInvalidRequest,
		Message: "scope is required: employer (this business's employees' punches) or location (every punch at this business's locations).",
		Details: map[string]any{"parameter": paramScope, "allowed": []string{string(tenant.FeedEmployer), string(tenant.FeedLocation)}}}
	problemPunchCursor = &Problem{Code: CodeInvalidRequest,
		Message: "cursor must be the nextCursor of an earlier page of this business's punches.",
		Details: map[string]any{"parameter": fieldCursor}}
)

// parseCursor reads cur_<n>: the prefix, then a plain decimal -- no sign, no leading zero, no
// blank -- that fits a seq.
func parseCursor(v string) (int64, bool) {
	digits, ok := strings.CutPrefix(v, cursorPrefix)
	if !ok || digits == "" || len(digits) > 19 || (len(digits) > 1 && digits[0] == '0') {
		return 0, false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func formatCursor(seq int64) string { return cursorPrefix + strconv.FormatInt(seq, 10) }

// punchRequest is a parsed query string: the feed (history false) or the history query.
type punchRequest struct {
	role  tenant.FeedRole
	limit int
	// The feed: the position after which to read.
	after int64
	// The history query (from and to present): the range and, past the first page, the cursor.
	history bool
	from    time.Time
	to      time.Time
	at      *historyCursor
}

// punchQuery reads the query string under the rule above. The cursor's prefix says which read it
// belongs to: cur_ the feed (no from/to), hst_ the history query (with the from/to it was issued
// for); either given to the other is 400 -- never silently a position of the other kind.
func punchQuery(r *http.Request, tenantID uuid.UUID) (punchRequest, *Problem) {
	var req punchRequest
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return req, problemPunchParameter
	}
	for k, v := range q {
		known := false
		for _, p := range punchParameters {
			known = known || k == p
		}
		if !known || len(v) != 1 {
			return req, problemPunchParameter
		}
	}
	scope, present := q[paramScope]
	switch {
	case !present:
		return req, problemScope
	case scope[0] == string(tenant.FeedEmployer):
		req.role = tenant.FeedEmployer
	case scope[0] == string(tenant.FeedLocation):
		req.role = tenant.FeedLocation
	default:
		return req, problemScope
	}
	var prob *Problem
	if req.limit, prob = parseLimit(q); prob != nil {
		return req, prob
	}
	_, hasFrom := q[paramFrom]
	_, hasTo := q[paramTo]
	if hasFrom || hasTo {
		req.history = true
		if req.from, req.to, prob = historyRange(q); prob != nil {
			return req, prob
		}
	}
	c, present := q[fieldCursor]
	switch {
	case !present:
	case req.history:
		hc, ok := parseHistoryCursor(c[0])
		if !ok || hc.bind != historyBinding(tenantID, req.role, req.from, req.to) {
			return req, problemHistoryCursor
		}
		req.at = &hc
	default:
		var ok bool
		if req.after, ok = parseCursor(c[0]); !ok {
			return req, problemPunchCursor
		}
	}
	return req, nil
}

// punchBody is one punch on the wire.
type punchBody struct {
	ID         string        `json:"id"`
	Version    int32         `json:"version"`
	Status     string        `json:"status"`
	PunchTime  time.Time     `json:"punchTime"`
	RecordedAt time.Time     `json:"recordedAt"`
	Channel    string        `json:"channel"`
	Direction  string        `json:"direction"`
	Employee   punchEmployee `json:"employee"`
	Location   punchLocation `json:"location"`
	TagID      *string       `json:"tagId"`
}

type punchEmployee struct {
	ID           string  `json:"id"`
	ExternalRef  *string `json:"externalRef"`
	EmployeeCode *string `json:"employeeCode"`
	Company      *string `json:"company"`
}

type punchLocation struct {
	ID          *string `json:"id"`
	ExternalRef *string `json:"externalRef"`
	Name        *string `json:"name"`
	Company     *string `json:"company"`
}

type punchPage struct {
	Data       []punchBody `json:"data"`
	NextCursor string      `json:"nextCursor"`
	HasMore    bool        `json:"hasMore"`
}

func punchOf(e tenant.PunchEvent) punchBody {
	b := punchBody{
		ID: punchIDPrefix + e.PunchID.String(), Version: e.Version, Status: e.Status,
		PunchTime: e.PunchTime.UTC(), RecordedAt: e.RecordedAt.UTC(), Channel: e.Channel, Direction: e.Direction,
		Employee: punchEmployee{ID: employeeIDPrefix + e.EmployeeID.String(), ExternalRef: e.EmployeeRef,
			EmployeeCode: e.EmployeeCode, Company: e.EmployeeCompany},
		Location: punchLocation{ExternalRef: e.LocationRef, Name: e.LocationName, Company: e.LocationCompany},
	}
	if e.LocationID != nil {
		id := locationIDPrefix + e.LocationID.String()
		b.Location.ID = &id
	}
	if e.TagUID != nil {
		// tags.uid is character(14): a full UID fills it; the trim keeps a padded value off the wire.
		id := tagIDPrefix + strings.TrimRight(*e.TagUID, " ")
		b.TagID = &id
	}
	return b
}

func (s *Surface) listPunches(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalOf(r)
	req, prob := punchQuery(r, p.TenantID)
	if prob != nil {
		writeProblem(w, prob)
		return
	}
	if req.history {
		s.listPunchHistory(w, r, req)
		return
	}
	page, err := s.punches.PunchPage(r.Context(), p.TenantID, req.role, req.after, req.limit)
	switch {
	case errors.Is(err, tenant.ErrCursorAhead):
		writeProblem(w, problemPunchCursor)
		return
	case err != nil:
		s.unavailable(w, r, "reading the punch feed failed", err)
		return
	}
	out := punchPage{Data: make([]punchBody, 0, len(page.Events)), NextCursor: formatCursor(page.Next), HasMore: page.HasMore}
	for _, e := range page.Events {
		out.Data = append(out.Data, punchOf(e))
	}
	writeJSON(w, http.StatusOK, out)
}
