package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/atknatk/tappa/internal/domain/tenant"
)

// THE HISTORY QUERY (M11 API-7b; ADR 0028 §5, plan §2 item 18 and §5 API-7b):
//
//	GET /v1/punches?scope=…&from=…&to=…[&cursor=hst_…][&limit=…]
//
// The scope's punches whose punchTime is in [from, to) -- from before to, at most 31 days apart --
// in (punchTime, feed position) order. from and to are RFC 3339 instants with an offset, at most
// microsecond precision (punch_time's): a finer instant is refused, not rounded.
//
// THE WALK IS A SNAPSHOT (tenant.PunchFeed.HistoryPage): its first page fixes the feed position S0
// and every page answers the punches at or below it. A punch that enters the feed later -- an
// approved flag, a phone's offline queue, both carrying an older punchTime -- is not in this walk,
// even inside its range; it is in the feed (cursor=cur_…), which is the read that misses nothing.
// The same range asked again later can answer more.
//
// THE HISTORY CURSOR is hst_<S0>_<punchTime µs>_<position>_<binding>: transparent like the feed's,
// and bound to the request it was issued for -- the binding is a digest of the scope, from and to, so
// a cursor sent with another scope or range is 400 instead of silently answering a different set (the
// feed cursor's cross-scope trap, which the binding closes here). It is not a secret and not a
// signature: it names a position in the key's own feed, and a forged one can only pick another
// position there. An S0 past the business's counter is 400 (tenant.ErrCursorAhead).

const historyCursorPrefix = "hst_"

var (
	problemHistoryRange = &Problem{Code: CodeInvalidRequest,
		Message: "from and to go together: two RFC 3339 instants with an offset, at most microsecond precision, from before to and at most 31 days apart.",
		Details: map[string]any{"parameters": []string{paramFrom, paramTo}, "maxDays": 31}}
	problemHistoryCursor = &Problem{Code: CodeInvalidRequest,
		Message: "With from and to, cursor must be the nextCursor of an earlier page of the same scope, from and to.",
		Details: map[string]any{"parameter": fieldCursor}}
)

// historyCursor is a decoded hst_ cursor.
type historyCursor struct {
	pos  tenant.HistoryPosition
	bind string
}

// historyRange reads from and to: both present, RFC 3339 with an offset, whole microseconds, from
// before to, at most tenant.MaxHistoryRange apart.
func historyRange(q url.Values) (from, to time.Time, prob *Problem) {
	f, okF := q[paramFrom]
	t, okT := q[paramTo]
	if !okF || !okT {
		return time.Time{}, time.Time{}, problemHistoryRange
	}
	parse := func(v string) (time.Time, bool) {
		at, err := time.Parse(time.RFC3339Nano, v)
		if err != nil || at.Nanosecond()%1000 != 0 {
			return time.Time{}, false
		}
		return at.UTC(), true
	}
	from, okF = parse(f[0])
	to, okT = parse(t[0])
	if !okF || !okT || !from.Before(to) || to.Sub(from) > tenant.MaxHistoryRange {
		return time.Time{}, time.Time{}, problemHistoryRange
	}
	return from, to, nil
}

// historyBinding is the digest a history cursor carries of the request it was issued for.
func historyBinding(role tenant.FeedRole, from, to time.Time) string {
	sum := sha256.Sum256([]byte("tappa/v1-history\x00" + string(role) + "\x00" +
		strconv.FormatInt(from.UnixMicro(), 10) + "\x00" + strconv.FormatInt(to.UnixMicro(), 10)))
	return hex.EncodeToString(sum[:8])
}

func formatHistoryCursor(pos tenant.HistoryPosition, bind string) string {
	return historyCursorPrefix + strconv.FormatInt(pos.S0, 10) + "_" + strconv.FormatInt(pos.Time.UnixMicro(), 10) + "_" +
		strconv.FormatInt(pos.Seq, 10) + "_" + bind
}

// canonicalInt reads a plain decimal -- an optional leading '-' where signed, no '+', no leading
// zero, no blank -- that fits an int64.
func canonicalInt(v string, signed bool) (int64, bool) {
	digits := v
	if signed {
		digits = strings.TrimPrefix(v, "-")
		if digits == "0" && v != "0" {
			return 0, false
		}
	}
	if digits == "" || len(digits) > 19 || (len(digits) > 1 && digits[0] == '0') {
		return 0, false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	return n, err == nil
}

// parseHistoryCursor reads hst_<S0>_<µs>_<seq>_<16 hex>.
func parseHistoryCursor(v string) (historyCursor, bool) {
	rest, ok := strings.CutPrefix(v, historyCursorPrefix)
	if !ok {
		return historyCursor{}, false
	}
	parts := strings.Split(rest, "_")
	if len(parts) != 4 || len(parts[3]) != 16 || strings.ToLower(parts[3]) != parts[3] {
		return historyCursor{}, false
	}
	if _, err := hex.DecodeString(parts[3]); err != nil {
		return historyCursor{}, false
	}
	s0, ok1 := canonicalInt(parts[0], false)
	micros, ok2 := canonicalInt(parts[1], true)
	seq, ok3 := canonicalInt(parts[2], false)
	if !ok1 || !ok2 || !ok3 {
		return historyCursor{}, false
	}
	return historyCursor{pos: tenant.HistoryPosition{S0: s0, Time: time.UnixMicro(micros).UTC(), Seq: seq}, bind: parts[3]}, true
}

// listPunchHistory answers one page of the history query.
func (s *Surface) listPunchHistory(w http.ResponseWriter, r *http.Request, req punchRequest) {
	q := tenant.HistoryQuery{From: req.from, To: req.to, First: req.at == nil}
	if req.at != nil {
		q.At = req.at.pos
	}
	p, _ := PrincipalOf(r)
	page, err := s.punches.HistoryPage(r.Context(), p.TenantID, req.role, q, req.limit)
	switch {
	case errors.Is(err, tenant.ErrCursorAhead), errors.Is(err, tenant.ErrHistoryRange):
		writeProblem(w, problemHistoryCursor)
		return
	case err != nil:
		s.unavailable(w, r, "reading the punch history failed", err)
		return
	}
	out := punchPage{Data: make([]punchBody, 0, len(page.Events)),
		NextCursor: formatHistoryCursor(page.Next, historyBinding(req.role, req.from, req.to)), HasMore: page.HasMore}
	for _, e := range page.Events {
		out.Data = append(out.Data, punchOf(e))
	}
	writeJSON(w, http.StatusOK, out)
}
