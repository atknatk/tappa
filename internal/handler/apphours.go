package handler

// apphours.go -- the staff app's My hours (M16-31): POST /app/v1/hours reads the person's own week
// -- the panel report's totals for them, each record with its state in plain words, and how many
// of their records and claims wait for a manager -- inside the window the owner chose (M16-30).
//
// ORDER: signature -> the device's bucket -> the setting (inside the read's transaction) -> the
// queries. Off is 404 not_found: an unsigned caller cannot probe the setting (401 comes first),
// and a server that does not know the route answers the same.
//
// NOTHING ABOUT A TIME OR A VENUE IS LOGGED: opaque ids only (§7). Nothing is written (§4.3).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
)

// appHours is the consumer's view of ledger.Reader (CLAUDE.md §7).
type appHours interface {
	OwnHours(ctx context.Context, tenantID uuid.UUID, q ledger.OwnQuery) (ledger.OwnWeek, error)
}

// ServeHours turns My hours on in the app. cmd/tappa wires it with the panel's ledger.Reader.
func (a *App) ServeHours(h appHours) error {
	if isNil(h) {
		return errors.New("handler: nil hours reader")
	}
	a.hours = h
	return nil
}

type appHoursRequest struct {
	Week  string `json:"week,omitempty"`
	After string `json:"after,omitempty"`
}

type appHoursTotals struct {
	WorkedMinutes   int  `json:"workedMinutes"`
	AwaitingMinutes int  `json:"awaitingMinutes"`
	OpenEntries     int  `json:"openEntries"`
	CheckInMissing  int  `json:"checkInMissing"`
	Truncated       bool `json:"truncated"`
	Complete        bool `json:"complete"`
}

type appHoursDay struct {
	Date          string `json:"date"`
	WorkedMinutes int    `json:"workedMinutes"`
}

type appHoursRecord struct {
	Date      string  `json:"date"`
	At        string  `json:"at"`
	Venue     string  `json:"venue"`
	Direction string  `json:"direction"`
	Channel   string  `json:"channel"`
	State     string  `json:"state"`
	Claim     *string `json:"claim"`
}

type appHoursWaiting struct {
	Records int `json:"records"`
	Claims  int `json:"claims"`
}

type appHoursResponse struct {
	Week         string           `json:"week"`
	WeeksVisible int              `json:"weeksVisible"`
	HasPrev      bool             `json:"hasPrev"`
	HasNext      bool             `json:"hasNext"`
	Totals       appHoursTotals   `json:"totals"`
	Days         []appHoursDay    `json:"days"`
	Records      []appHoursRecord `json:"records"`
	NextCursor   *string          `json:"nextCursor"`
	Waiting      appHoursWaiting  `json:"waiting"`
}

// appHoursChannels is the closed channel set the app draws; any other value is "other".
var appHoursChannels = map[string]bool{"nfc": true, "qr": true, "qr_live": true, "manual": true}

// ownCursorPrefix marks a My hours cursor: own_<occurred_at unix nanos>_<record id>_<binding>.
//
// THE BINDING is a digest of the walk the cursor was issued for -- the business, the person and the
// week's Monday (M16-31's card; /v1/timesheets' tms_ rule): a cursor sent by another person or with
// another week is 400, never a page of a different walk. The week is the one the read RESOLVED (an
// absent week is this week), so the check runs after the read (ownCursorBound); the read itself is
// already the person's own records only. A cursor of the older two-part shape is refused like any
// malformed one. The apps carry the cursor opaquely (Android wire.Wire, iOS AppClient): they send
// back nextCursor with the week the answer named.
const ownCursorPrefix = "own_"

// ownCursorBinding is that digest: 8 bytes of SHA-256, 16 lower-case hex characters.
func ownCursorBinding(tenantID, employeeID uuid.UUID, week ledger.Date) string {
	sum := sha256.Sum256([]byte("tappa/app-own-hours\x00" + tenantID.String() + "\x00" + employeeID.String() + "\x00" + week.String()))
	return hex.EncodeToString(sum[:8])
}

func encodeOwnCursor(c *ledger.OwnCursor, bind string) *string {
	if c == nil {
		return nil
	}
	s := ownCursorPrefix + strconv.FormatInt(c.At.UnixNano(), 10) + "_" + c.ID.String() + "_" + bind
	return &s
}

// decodeOwnCursor reads the cursor's shape; ok false is a malformed cursor. The binding it returns is
// checked against the resolved walk by ownCursorBound.
func decodeOwnCursor(s string) (c *ledger.OwnCursor, bind string, ok bool) {
	if s == "" {
		return nil, "", true
	}
	rest, ok := strings.CutPrefix(s, ownCursorPrefix)
	if !ok {
		return nil, "", false
	}
	parts := strings.Split(rest, "_")
	if len(parts) != 3 {
		return nil, "", false
	}
	n, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, "", false
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, "", false
	}
	bind = parts[2]
	if len(bind) != 16 || strings.ToLower(bind) != bind {
		return nil, "", false
	}
	if _, err := hex.DecodeString(bind); err != nil {
		return nil, "", false
	}
	return &ledger.OwnCursor{At: time.Unix(0, n).UTC(), ID: id}, bind, true
}

// ownCursorBound says whether a request's cursor (none: always) belongs to the walk the read resolved.
func ownCursorBound(after *ledger.OwnCursor, bind string, tenantID, employeeID uuid.UUID, week ledger.Date) bool {
	return after == nil || bind == ownCursorBinding(tenantID, employeeID, week)
}

func minutesOf(d time.Duration) int { return int(d / time.Minute) }

// Hours serves POST /app/v1/hours: a signed read (a POST: a signed read refuses a query string).
func (a *App) Hours(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	if a.hours == nil {
		writeAppError(w, appErrNotFound)
		return
	}
	body, ok := readAppBody(w, r)
	if !ok {
		return
	}
	dev, ok := a.authRead(w, r, body, "hours", "")
	if !ok {
		return
	}
	var req appHoursRequest
	if !decodeAppJSON(body, &req) {
		writeAppError(w, appErrBadRequest)
		return
	}
	q := ledger.OwnQuery{EmployeeID: dev.EmployeeID, Cap: ledger.ReportEventCap}
	if req.Week != "" {
		d, err := time.Parse("2006-01-02", req.Week)
		if err != nil {
			writeAppError(w, appErrBadRequest)
			return
		}
		q.Week = ledger.Date{Year: d.Year(), Month: d.Month(), Day: d.Day()}
	}
	after, bind, ok := decodeOwnCursor(req.After)
	if !ok {
		writeAppError(w, appErrBadRequest)
		return
	}
	q.After = after
	week, err := a.hours.OwnHours(ctx, dev.TenantID, q)
	switch {
	case errors.Is(err, ledger.ErrOwnHoursOff):
		writeAppError(w, appErrNotFound)
		return
	case errors.Is(err, ledger.ErrOwnWeek), errors.Is(err, ledger.ErrOwnCursor):
		writeAppError(w, appErrBadRequest)
		return
	case err != nil:
		a.log.ErrorContext(ctx, "app: reading my hours failed", "employee_id", dev.EmployeeID, "err", err)
		writeAppError(w, appErrServer)
		return
	}
	if !ownCursorBound(after, bind, dev.TenantID, dev.EmployeeID, week.Period.FirstDay) {
		writeAppError(w, appErrBadRequest)
		return
	}
	a.writeAppJSON(w, r, http.StatusOK, a.appHoursOf(ctx, week, ownCursorBinding(dev.TenantID, dev.EmployeeID, week.Period.FirstDay)))
}

// appHoursOf is the answer; bind is the binding its nextCursor carries.
func (a *App) appHoursOf(ctx context.Context, w ledger.OwnWeek, bind string) appHoursResponse {
	loc := w.Zone
	if loc == nil {
		loc = time.UTC
	}
	out := appHoursResponse{
		Week:         w.Period.FirstDay.String(),
		WeeksVisible: w.Weeks,
		HasPrev:      w.HasPrev,
		HasNext:      w.HasNext,
		Totals: appHoursTotals{
			WorkedMinutes:   minutesOf(w.Worked),
			AwaitingMinutes: minutesOf(w.Awaiting),
			OpenEntries:     w.Open,
			CheckInMissing:  w.InMissing,
			Truncated:       w.Truncated,
			Complete:        w.Complete(),
		},
		Days:       make([]appHoursDay, 0, len(w.Daily)),
		Records:    make([]appHoursRecord, 0, len(w.Records)),
		NextCursor: encodeOwnCursor(w.Next, bind),
		Waiting:    appHoursWaiting{Records: w.WaitingRecords, Claims: w.WaitingClaims},
	}
	for i, d := range w.Daily {
		out.Days = append(out.Days, appHoursDay{
			Date:          w.Period.From.In(loc).AddDate(0, 0, i).Format("2006-01-02"),
			WorkedMinutes: minutesOf(d),
		})
	}
	for _, rec := range w.Records {
		ch := rec.Channel
		if !appHoursChannels[ch] {
			a.log.ErrorContext(ctx, "app: my hours met a channel it does not name; shown as other", "channel", ch)
			ch = "other"
		}
		o := appHoursRecord{
			Date:      rec.At.In(loc).Format("2006-01-02"),
			At:        rec.At.In(loc).Format("15:04"),
			Venue:     rec.Venue,
			Direction: rec.Direction,
			Channel:   ch,
			State:     rec.State,
		}
		if rec.Claim != "" {
			c := rec.Claim
			o.Claim = &c
		}
		out.Records = append(out.Records, o)
	}
	return out
}
