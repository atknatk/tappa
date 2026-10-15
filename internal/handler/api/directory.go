package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
)

// THE DIRECTORY (M11 API-8; ADR 0028 §2, §8, §9, §10; ADR 0027 §2; K-1f, K-1h, K-21, K-24, K-33;
// plan §2 items 19 and 29):
//
//	GET /v1/directory[?updatedSince=…][&cursor=dir_…][&limit=…]
//
// The employees the key's business may know of, in (updatedAt, id) order: its OWN (the ordinary RLS
// read) and, when it is a member of a holding, its PEERS' (holding_directory -- never its own). Each
// entry is {id, company, externalRef, employeeCode, firstName, lastName, fullName, active, updatedAt}: no
// e-mail (K-1f, K-24), no address, no venue; active is false for a deactivated person (K-1h). An
// outsider's key -- a business in no holding, or in another one -- reads its own staff and nothing
// else (K-33: holding_directory answers it no row).
//
// THE QUERY PARAMETERS are API-7's rule: updatedSince, cursor and limit exactly, each at most once;
// any other name, another case, a repeated one or a query string that does not parse is 400, the
// value never echoed. updatedSince is an RFC 3339 instant with Z or an offset, at most microsecond
// precision (the history range's parser -- parseInstant); the server reads from ten minutes BEFORE it
// (K-21: tenant.DirectoryOverlap). limit is 1..1000, 500 by default.
//
// THE ANSWER is {data, nextCursor, hasMore, asOf}. A walk is the pages from one request without a
// cursor to the page whose hasMore is false; nextCursor is set exactly when hasMore is true, and the
// next page of the walk is the same request with cursor=nextCursor -- the SAME updatedSince (or none),
// or 400. asOf is the walk's: the database's clock when its first page was read, the same on every
// page. After the last page the integrator's next walk is updatedSince=asOf: every change committed
// since is in it (tenant.EmployeeDirectory's header says exactly what that guarantees), and a person
// already read may come again -- overwrite by id (plan §2 item 19). A business that joins or leaves a
// holding changes no employee: after a membership change, a walk without updatedSince.
//
// THE CURSOR is dir_<asOf µs>_<position µs>_<id>_<binding>: transparent like the feed's, bound like
// the history query's -- the binding is a digest of the key's business and the updatedSince the walk
// was asked with, so a cursor sent by another business or with another updatedSince is 400. It is
// not a signature: a forged one chooses a position in this business's own directory and the asOf
// handed back to its sender -- an asOf past the database's clock is 400 (tenant.ErrDirectoryCursor).
//
// Open while the business is suspended (§10): a read does not pass the suspension checkpoint. Every
// statement runs under tenant.APIReadTimeout.

// Directory is the directory's reader (*tenant.EmployeeDirectory), declared at the consumer (§7).
type Directory interface {
	DirectoryPage(ctx context.Context, tenantID uuid.UUID, q tenant.DirectoryQuery, limit int) (tenant.DirectoryPage, error)
}

var _ Directory = (*tenant.EmployeeDirectory)(nil)

const (
	directoryPath         = "/directory"
	paramUpdatedSince     = "updatedSince"
	directoryCursorPrefix = "dir_"
)

var directoryParameters = []string{paramUpdatedSince, fieldCursor, paramLimit}

var (
	problemDirectoryParameter = &Problem{Code: CodeInvalidRequest,
		Message: "This endpoint takes the query parameters updatedSince, cursor and limit only, each at most once, in a well-formed query string.",
		Details: map[string]any{"allowed": directoryParameters}}
	problemUpdatedSince = &Problem{Code: CodeInvalidRequest,
		Message: "updatedSince must be an RFC 3339 instant (YYYY-MM-DDThh:mm:ss[.fraction] and Z or an offset ±hh:mm), at most microsecond precision.",
		Details: map[string]any{"parameter": paramUpdatedSince}}
	problemUpdatedSincePlus = &Problem{Code: CodeInvalidRequest,
		Message: "updatedSince carries a space: a '+' in an offset must be percent-encoded as %2B in a query string (unencoded it reads as a space).",
		Details: map[string]any{"parameter": paramUpdatedSince, "reason": "unencoded_plus"}}
	problemDirectoryCursor = &Problem{Code: CodeInvalidRequest,
		Message: "cursor must be the nextCursor of an earlier page of this business's directory, sent with the same updatedSince (or none).",
		Details: map[string]any{"parameter": fieldCursor}}
)

// directoryCursor is a decoded dir_ cursor.
type directoryCursor struct {
	asOf time.Time
	pos  tenant.DirectoryPosition
	bind string
}

// directoryRequest is a parsed query string.
type directoryRequest struct {
	since *time.Time
	limit int
	at    *directoryCursor
}

// directoryBinding is the digest a directory cursor carries of the walk it was issued for: the key's
// business and the updatedSince ("-" for none).
func directoryBinding(tenantID uuid.UUID, since *time.Time) string {
	s := "-"
	if since != nil {
		s = strconv.FormatInt(since.UnixMicro(), 10)
	}
	sum := sha256.Sum256([]byte("tappa/v1-directory\x00" + tenantID.String() + "\x00" + s))
	return hex.EncodeToString(sum[:8])
}

func formatDirectoryCursor(asOf time.Time, pos tenant.DirectoryPosition, bind string) string {
	return directoryCursorPrefix + strconv.FormatInt(asOf.UnixMicro(), 10) + "_" + strconv.FormatInt(pos.At.UnixMicro(), 10) + "_" +
		pos.ID.String() + "_" + bind
}

// parseDirectoryCursor reads dir_<asOf µs>_<µs>_<canonical uuid>_<16 lower-case hex>.
func parseDirectoryCursor(v string) (directoryCursor, bool) {
	rest, ok := strings.CutPrefix(v, directoryCursorPrefix)
	if !ok {
		return directoryCursor{}, false
	}
	parts := strings.Split(rest, "_")
	if len(parts) != 4 || len(parts[3]) != 16 || strings.ToLower(parts[3]) != parts[3] {
		return directoryCursor{}, false
	}
	if _, err := hex.DecodeString(parts[3]); err != nil {
		return directoryCursor{}, false
	}
	asOf, ok1 := canonicalInt(parts[0], false)
	at, ok2 := canonicalInt(parts[1], true)
	id, err := uuid.Parse(parts[2])
	// The canonical text alone: uuid.Parse also takes braces, a urn: prefix, upper case and no hyphens.
	if !ok1 || !ok2 || err != nil || id.String() != parts[2] {
		return directoryCursor{}, false
	}
	return directoryCursor{asOf: time.UnixMicro(asOf).UTC(), pos: tenant.DirectoryPosition{At: time.UnixMicro(at).UTC(), ID: id},
		bind: parts[3]}, true
}

// directoryQuery reads the query string under the rule above.
func directoryQuery(r *http.Request, tenantID uuid.UUID) (directoryRequest, *Problem) {
	var req directoryRequest
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return req, problemDirectoryParameter
	}
	for k, v := range q {
		known := false
		for _, p := range directoryParameters {
			known = known || k == p
		}
		if !known || len(v) != 1 {
			return req, problemDirectoryParameter
		}
	}
	var prob *Problem
	if req.limit, prob = parseLimit(q); prob != nil {
		return req, prob
	}
	if v, present := q[paramUpdatedSince]; present {
		if unencodedPlusShape.MatchString(v[0]) {
			return req, problemUpdatedSincePlus
		}
		since, ok := parseInstant(v[0])
		if !ok {
			return req, problemUpdatedSince
		}
		req.since = &since
	}
	if c, present := q[fieldCursor]; present {
		dc, ok := parseDirectoryCursor(c[0])
		if !ok || dc.bind != directoryBinding(tenantID, req.since) {
			return req, problemDirectoryCursor
		}
		req.at = &dc
	}
	return req, nil
}

// directoryEntryBody is one employee on the wire.
type directoryEntryBody struct {
	ID           string    `json:"id"`
	Company      *string   `json:"company"`
	ExternalRef  *string   `json:"externalRef"`
	EmployeeCode *string   `json:"employeeCode"`
	FirstName    *string   `json:"firstName"`
	LastName     *string   `json:"lastName"`
	FullName     string    `json:"fullName"`
	Active       bool      `json:"active"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type directoryPage struct {
	Data       []directoryEntryBody `json:"data"`
	NextCursor *string              `json:"nextCursor"`
	HasMore    bool                 `json:"hasMore"`
	AsOf       time.Time            `json:"asOf"`
}

func (s *Surface) listDirectory(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalOf(r)
	req, prob := directoryQuery(r, p.TenantID)
	if prob != nil {
		writeProblem(w, prob)
		return
	}
	q := tenant.DirectoryQuery{Since: req.since}
	if req.at != nil {
		pos := req.at.pos
		q.After, q.AsOf = &pos, req.at.asOf
	}
	page, err := s.directory.DirectoryPage(r.Context(), p.TenantID, q, req.limit)
	switch {
	case errors.Is(err, tenant.ErrDirectoryCursor):
		writeProblem(w, problemDirectoryCursor)
		return
	case err != nil:
		s.unavailable(w, r, "reading the directory failed", err)
		return
	}
	out := directoryPage{Data: make([]directoryEntryBody, 0, len(page.Entries)), HasMore: page.HasMore, AsOf: page.AsOf.UTC()}
	for _, e := range page.Entries {
		out.Data = append(out.Data, directoryEntryBody{ID: employeeIDPrefix + e.EmployeeID.String(), Company: e.Company,
			ExternalRef: e.ExternalRef, EmployeeCode: e.EmployeeCode, FirstName: e.FirstName, LastName: e.LastName,
			FullName: e.FullName, Active: e.Active, UpdatedAt: e.UpdatedAt.UTC()})
	}
	if page.Next != nil {
		c := formatDirectoryCursor(page.AsOf, *page.Next, directoryBinding(p.TenantID, req.since))
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
}
