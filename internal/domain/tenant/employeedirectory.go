package tenant

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// THE DIRECTORY'S READ END (M11 API-8; ADR 0028 §2, §9; ADR 0027 §2; K-1f, K-1h, K-21, K-24, K-33):
// one page of the employees a business's integration may know of -- its OWN, read through the
// ordinary RLS path (db/queries/directory.sql: WithTenant + the explicit tenant_id), and its holding
// PEERS', read through holding_directory (00055, ADR 0027's closed list), which never answers the
// business's own. Both in ONE transaction of the key's tenant, merged on (updated_at, id): ids are
// unique across tenants, so the two sources share one total order, and the page is its first rows.
//
// THE WALK AND asOf. A walk starts at updatedSince (none: the whole directory) and goes page by page,
// each page after the last row the previous one handed out. Its first page reads asOf -- the
// database clock at the start of that page's transaction, before any snapshot of it -- and every
// later page of the walk carries it on. The integrator's next walk starts at updatedSince = asOf, and
// the server reads from updatedSince - DirectoryOverlap (K-21's ten minutes) on.
//
// WHAT IS GUARANTEED (measured: TestEmployeeDirectory_WalksMissNothingUnderConcurrentWriters):
// a change to a directory column (00037's trigger sets updated_at to the WRITE's clock) is in some
// walk that starts after it commits -- unless its transaction stays open longer than the overlap
// between the write and the commit. Why: a row the walk did not see was invisible to the snapshot
// that read its position, so its transaction was still open then, which is after asOf; if the write
// was before asOf - overlap, that transaction was open longer than the overlap; otherwise the next
// walk (from asOf - overlap) reads it. Every writer of employees today is a request, bounded by
// httpx.RequestTimeout (30 s), far inside ten minutes. A row changed during a walk moves to the end of
// the order and is handed out again, later in the same walk or in the next: REPEATS ARE EXPECTED and
// are overwritten by id (plan §2 item 19).
//
// WHAT IT DOES NOT SAY. A business that JOINS or LEAVES the holding changes no employee row: its
// staff appear in (or vanish from) a full walk, never in an incremental one, and nothing marks the
// ones that vanished -- a membership change is the integrator's cue to read the whole directory
// again (the rehber note, plan §5 API-12).

// DirectoryOverlap is K-21's overlap window: a walk from updatedSince reads from updatedSince minus
// this.
const DirectoryOverlap = 10 * time.Minute

// DirectoryMaxPage is the largest page: holding_directory's own cap (db.HoldingDirectoryPage), so a
// merged page always holds the peers' first rows in full.
const DirectoryMaxPage = db.HoldingDirectoryPage

// maxUUID is the greatest uuid: "after (t - 1 µs, maxUUID)" is "at or after t".
var maxUUID = uuid.UUID{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

// directoryOrigin is the own read's lower bound for a walk with no updatedSince: before every
// updated_at there is (00037's trigger writes the database clock). A bare value, not NULL and a
// COALESCE: the bound must stay two plain parameters to be an index condition in a generic plan
// (directory.sql says what was measured).
var directoryOrigin = time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC)

// ErrDirectoryCursor: the cursor's asOf is past the database's clock -- not a value a walk handed out.
var ErrDirectoryCursor = errors.New("tenant: the directory cursor is not one this business's walk handed out")

// DirectoryEntry is one employee as the API answers with it. No e-mail, address or home venue: the
// signature of holding_directory is the shape, and the own rows take the same one. FullName is the
// panel's name -- the one a person the panel created has (the user's decision, 2026-10-15).
type DirectoryEntry struct {
	EmployeeID uuid.UUID
	// Company is the employer's company code (tenants.api_code; K-22, the spec's `company`).
	Company      *string
	ExternalRef  *string
	EmployeeCode *string
	FirstName    *string
	LastName     *string
	FullName     string
	Active       bool
	UpdatedAt    time.Time
}

// DirectoryPosition is a place in the directory's order: a row's (updated_at, id).
type DirectoryPosition struct {
	At time.Time
	ID uuid.UUID
}

// DirectoryQuery is one page's request.
type DirectoryQuery struct {
	// Since is the integrator's updatedSince (nil: the whole directory); the walk reads from
	// Since - DirectoryOverlap. It is read on the first page only: a later page is after After.
	Since *time.Time
	// After is the last row the previous page handed out; nil on a walk's first page.
	After *DirectoryPosition
	// AsOf is the walk's asOf, carried by its cursor; zero on the first page (which reads it).
	AsOf time.Time
}

// DirectoryPage is one page: the entries, the position to read after next (nil when the walk is
// over), whether there is more, and the walk's asOf.
type DirectoryPage struct {
	Entries []DirectoryEntry
	Next    *DirectoryPosition
	HasMore bool
	AsOf    time.Time
}

// EmployeeDirectory reads the directory.
type EmployeeDirectory struct {
	data    Database
	overlap time.Duration
	// bound is the statement timeout of a page's transaction: APIReadTimeout (a test of the walk
	// guarantee lifts it -- on a database of tens of thousands of tenants a member's page costs the
	// peer set, T124's member half, and the guarantee is not what that test measures).
	bound time.Duration
}

// NewEmployeeDirectory wires the reader.
func NewEmployeeDirectory(data Database) (*EmployeeDirectory, error) {
	if data == nil {
		return nil, errors.New("tenant: nil database")
	}
	return &EmployeeDirectory{data: data, overlap: DirectoryOverlap, bound: APIReadTimeout}, nil
}

// DirectoryPage reads one page of tenantID's directory, at most limit entries.
func (d *EmployeeDirectory) DirectoryPage(ctx context.Context, tenantID uuid.UUID, q DirectoryQuery, limit int) (DirectoryPage, error) {
	switch {
	case tenantID == uuid.Nil:
		return DirectoryPage{}, errors.New("tenant: tenant id is required")
	case limit < 1 || limit > DirectoryMaxPage:
		return DirectoryPage{}, errors.New("tenant: a directory page holds 1 to 1000 entries")
	case q.After != nil && q.AsOf.IsZero():
		return DirectoryPage{}, errors.New("tenant: a later page carries its walk's asOf")
	}
	// The two sources' lower bounds. The own read is strictly after a position; the function takes
	// "at or after since" on a first page (cursor nil) and "after (since, cursor)" on a later one.
	var (
		ownAt   = directoryOrigin
		ownID   = maxUUID
		peerAt  *time.Time
		peerCur *uuid.UUID
	)
	switch {
	case q.After != nil:
		at, id := q.After.At, q.After.ID
		ownAt, ownID, peerAt, peerCur = at, id, &at, &id
	case q.Since != nil:
		from := q.Since.Add(-d.overlap)
		ownAt, peerAt = from.Add(-time.Microsecond), &from
	}
	var page DirectoryPage
	err := d.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := db.BoundStatements(ctx, tx, d.bound); err != nil {
			return err
		}
		qs := store.New(tx)
		head, err := qs.GetDirectoryHead(ctx, tenantID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("the key's business has no tenants row")
		}
		if err != nil {
			return err
		}
		page.AsOf = head.AsOf
		if q.After != nil {
			if q.AsOf.After(head.AsOf) {
				return ErrDirectoryCursor
			}
			page.AsOf = q.AsOf
		}
		own, err := qs.ListDirectoryEmployees(ctx, store.ListDirectoryEmployeesParams{
			TenantID: tenantID, AfterAt: ownAt, AfterID: ownID, PageSize: int32(limit + 1)}) //nolint:gosec // limit is 1..1000 (checked above)
		if err != nil {
			return err
		}
		peers, err := db.HoldingOn(tx).Directory(ctx, peerAt, peerCur)
		if err != nil {
			return err
		}
		page.Entries, page.HasMore = mergeDirectory(own, head.ApiCode, peers, limit)
		if page.HasMore {
			last := page.Entries[len(page.Entries)-1]
			page.Next = &DirectoryPosition{At: last.UpdatedAt, ID: last.EmployeeID}
		}
		return nil
	})
	if err != nil {
		return DirectoryPage{}, fmt.Errorf("tenant: read the directory: %w", err)
	}
	return page, nil
}

// directoryBefore orders two rows of the directory: (updated_at, id) -- the order both sources are
// read in and the index walks.
func directoryBefore(at time.Time, id uuid.UUID, bAt time.Time, bID uuid.UUID) bool {
	if !at.Equal(bAt) {
		return at.Before(bAt)
	}
	// PostgreSQL compares uuids byte by byte (memcmp), and so does this.
	return bytes.Compare(id[:], bID[:]) < 0
}

// mergeDirectory merges the own rows (at most limit+1, sorted) and the peers' (at most
// HoldingDirectoryPage, sorted) into the page's first limit entries. There is more when either
// source had a row past the page, or when the function answered a full page (it may hold more
// behind it -- the next page then asks again, and may be the empty end).
func mergeDirectory(own []store.ListDirectoryEmployeesRow, company *string, peers []db.HoldingDirectoryEntry, limit int) ([]DirectoryEntry, bool) {
	out := make([]DirectoryEntry, 0, min(limit, len(own)+len(peers)))
	i, j := 0, 0
	for len(out) < limit && (i < len(own) || j < len(peers)) {
		if j >= len(peers) || (i < len(own) && directoryBefore(own[i].UpdatedAt, own[i].ID, peers[j].UpdatedAt, peers[j].EmployeeID)) {
			o := own[i]
			out = append(out, DirectoryEntry{EmployeeID: o.ID, Company: company, ExternalRef: o.ExternalRef, EmployeeCode: o.EmployeeCode,
				FirstName: o.FirstName, LastName: o.LastName, FullName: o.FullName, Active: o.Active, UpdatedAt: o.UpdatedAt})
			i++
			continue
		}
		p := peers[j]
		out = append(out, DirectoryEntry{EmployeeID: p.EmployeeID, Company: p.APICode, ExternalRef: p.ExternalRef, EmployeeCode: p.EmployeeCode,
			FirstName: p.FirstName, LastName: p.LastName, FullName: p.FullName, Active: p.Active, UpdatedAt: p.UpdatedAt})
		j++
	}
	more := i < len(own) || j < len(peers) || len(peers) >= db.HoldingDirectoryPage
	return out, more
}
