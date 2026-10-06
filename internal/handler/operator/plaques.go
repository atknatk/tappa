package operator

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// ONE TENANT'S PLAQUE INVENTORY (M10 OP-13, phase B; ADR 0020 §4, §9; ADR 0021 §2 v, OP-13
// note). GET /operator/tenants/{id}/plaques sits in the console's group (routes.go, mount):
// host gate, security headers, flood gate, same-origin gate (a GET a browser labels
// same-site or cross-site is the sign-in redirect), requireOperator, sessionGate. The
// overview links it; the console does not -- a plaque inventory belongs to one tenant.
//
// THE VIEW IS ONE TWO-PHASE READ through PlaqueStore: op_begin_read commits the read's
// audit row (target_scope tenant_plaques, the tenant named), then op_read_tenant_plaques
// consumes the ticket and returns the tenant's name, the fact that it exists and its
// plaques. The banner's name comes from that same read -- no overview read is made for it
// (that would be two reads, two 'read' rows and two units of the read budget). The ticket
// never reaches this package and this file writes no SQL.
//
// WHAT THE SCREEN CAN SHOW IS WHAT db.TenantPlaque CARRIES: the plaque's uid, its status
// as stored, its location and that location's name, the encode stamp, when it was added and
// retired, its successor and its counter. That type has no field for a plaque key or a
// key's presence (its field list, and the view's, are pinned:
// TestPlaqueScreen_NoKeyReachesThePage), and the definer that fills it holds no SELECT on
// aes_key_ref or app_key_ref (ADR 0021, OP-13 note, PART I). The screen writes nothing: no
// retire, unmount, delete or encode -- its only form is the bar's sign-out (measured:
// TestPlaqueScreen_EveryShapeHasItsSentenceAndAnUnknownStatusStaysVisible).

// PlaqueStore is the operator database's slice the plaque screen needs, declared at the
// consumer (CLAUDE.md §7). *db.OperatorDB implements it by delegating to internal/db's
// TenantPlaques (TestOperatorDB_EveryMethodDelegatesVerbatim), and that type's method set
// is derived from this interface, TenantStore, LegalStore and operatorauth.Store
// (TestOperatorDB_IsTheStoreAndNothingMore). sessionHash is operatorauth's SessionHash of
// the request's token -- on ADR 0020 §5's never-log list. The signature carries no read
// ticket: the method is both phases.
type PlaqueStore interface {
	// TenantPlaques is op_begin_read + op_read_tenant_plaques: the tenant's name, its first
	// db.MaxTenantPlaques plaques in its own list's order, and the total. An id no tenant
	// has is db.ErrNoSuchTenant -- after the 'read' row naming it committed; a dead session
	// is db.ErrOperatorRefused.
	TenantPlaques(ctx context.Context, sessionHash string, tenantID uuid.UUID) (db.TenantPlaqueInventory, error)
}

// errPlaquesOfAnotherTenant is the screen's refusal of an inventory that names another
// tenant than the path's. internal/db cannot return one (op_read_tenant_plaques filters on
// the id, and readTenantPlaques refuses a row of another tenant); this is the handler's copy
// of that rule, because the banner would otherwise name a tenant whose plaques were never
// asked for.
var errPlaquesOfAnotherTenant = errors.New("operator: the plaque inventory names another tenant than the path")

// tenantPlaques is GET /operator/tenants/{id}/plaques.
//
// The refusals, in order: the session (storeSession); an id that is not the 36-character
// hyphenated form (tenantID) -- 404 with no store call and no unit of the read budget; the
// read budget (spendRead) -- 429. Then the read: an id no tenant has is a 404 after the
// 'read' row naming it committed; a session the read refuses is the sign-in's 303; any
// other error -- an inventory of another tenant included -- is a 503 whose log line names
// the tenant's id and not the session's hash.
func (s *Surface) tenantPlaques(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	tenant, ok := tenantID(chi.URLParam(r, "id"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, problemTenantNotAnID)
		return
	}
	if !s.spendRead(w, r, id) {
		return
	}
	inv, err := s.plaqueStore.TenantPlaques(r.Context(), hash, tenant)
	if err == nil && inv.TenantID != tenant {
		err = errPlaquesOfAnotherTenant
	}
	switch {
	case errors.Is(err, db.ErrNoSuchTenant):
		s.problem(w, r, http.StatusNotFound, problemNoSuchTenant)
		return
	case errors.Is(err, db.ErrOperatorRefused):
		s.redirect(w, pathSignIn)
		return
	case err != nil:
		// internal/db's error is the call and a SQLSTATE (operatorErr); the hash is not an
		// argument of this line.
		s.log.ErrorContext(r.Context(), "operator: a tenant's plaques could not be read", "tenant_id", tenant.String(), "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemPlaquesUnreadable)
		return
	}
	name, err := tenantBanner(inv.TenantID, inv.TenantName)
	if err != nil {
		// Not reached: tenantBanner names a tenant without a visible name by its id. Were
		// it reached, the zero name makes TenantScreen refuse and render answers a plain
		// 500 -- this line says why first.
		s.log.ErrorContext(r.Context(), "operator: a tenant could not be named on its plaque screen", "tenant_id", tenant.String(), "err", err)
	}
	s.render(w, r, http.StatusOK, operatorpages.TenantPlaques(tenantPlaquesView(inv, name)))
}

// plaquesPath is a tenant's plaque screen, /operator/tenants/<id>/plaques -- the overview's
// link to it.
func plaquesPath(tenant uuid.UUID) string {
	return pathTenants + "/" + tenant.String() + "/plaques"
}

// plaqueWord is how the screen says one plaque shape: the label of the row's chip, the
// sentence beside it, and the chip's tone (the brand's fixed status mapping).
type plaqueWord struct {
	label, sentence string
	tone            operatorpages.PlaqueTone
}

// plaqueWords is the screen's dictionary: one entry for each value db.TenantPlaque.Shape
// returns. TestPlaqueWords_NameEveryShapeAndNothingElse derives those values from internal/db's
// constants of type PlaqueShape and holds this map's keys equal to them. A shape with no
// entry is said as db.PlaqueUnrecognised (plaqueWordFor) -- the row is not dropped and is
// not read as a neighbouring state.
//
// What each sentence says is the schema's or the product's own rule: a plaque mounts only
// with an encode stamp (00025, and AssignTagToLocation's WHERE); a tap on a retired or lost
// plaque is rejected (§5 row 1, sys:tag-not-active); a plaque that is active with no stamp
// is incident A-1's shape (backlog T75) and is named as such. A retired plaque need not
// have a successor (tags.replaced_by is nullable; on the development database about half
// of the retired plaques have none), so its sentence claims neither -- the row's
// "Replaced by" fact, present only when there is a successor, tells the two apart.
var plaqueWords = map[db.PlaqueShape]plaqueWord{
	db.PlaqueOnAWall: {"On a wall",
		"In service at its location; its encode was recorded.", operatorpages.PlaqueToneInService},
	db.PlaqueOnAWallNeverEncoded: {"On a wall, never encoded",
		"Mounted and in service, but no encode was ever recorded for it.", operatorpages.PlaqueToneAttention},
	db.PlaqueInStock: {"In stock",
		"Not on a wall; encoded and ready to mount.", operatorpages.PlaqueToneStock},
	db.PlaqueInStockNotEncoded: {"In stock, not encoded",
		"Not on a wall, and it cannot be mounted until its encode is recorded.", operatorpages.PlaqueToneAttention},
	db.PlaqueRetired: {"Retired",
		"Taken out of service, with or without a replacement; taps on it are rejected.", operatorpages.PlaqueToneOut},
	db.PlaqueLost: {"Lost",
		"Reported lost; taps on it are rejected.", operatorpages.PlaqueToneOut},
	db.PlaqueUnrecognised: {"Unrecognised",
		"Its stored status and location are not a state this screen knows; the status is shown as stored.",
		operatorpages.PlaqueToneUnknown},
}

// plaqueWordFor is shape's entry, or db.PlaqueUnrecognised's for a shape the dictionary
// does not name.
func plaqueWordFor(shape db.PlaqueShape) plaqueWord {
	if w, ok := plaqueWords[shape]; ok {
		return w
	}
	return plaqueWords[db.PlaqueUnrecognised]
}

// tenantPlaquesView builds the screen: the counts (Total is every plaque the tenant holds,
// Shown the rows the read returned -- db.MaxTenantPlaques at most), and a row per plaque in
// the read's order, every time in UTC.
func tenantPlaquesView(inv db.TenantPlaqueInventory, name operatorpages.TenantName) operatorpages.TenantPlaquesView {
	v := operatorpages.TenantPlaquesView{
		Name:         name,
		ID:           inv.TenantID.String(),
		OverviewPath: pathTenants + "/" + inv.TenantID.String(),
		Total:        strconv.FormatInt(inv.Total, 10),
		Shown:        strconv.Itoa(len(inv.Plaques)),
		Truncated:    inv.Truncated(),
		Noun:         "plaques",
	}
	if inv.Total == 1 {
		v.Noun = "plaque"
	}
	for _, p := range inv.Plaques {
		shape := p.Shape()
		word := plaqueWordFor(shape)
		row := operatorpages.PlaqueRow{
			UID:       p.UID,
			Label:     word.label,
			Sentence:  word.sentence,
			Tone:      word.tone,
			CreatedAt: utcStamp(p.CreatedAt),
			LastCtr:   strconv.FormatInt(int64(p.LastCtr), 10),
		}
		if word == plaqueWordFor(db.PlaqueUnrecognised) {
			// The status as stored, quoted, so an empty value, a space, a capital or a
			// control character shows as what it is.
			row.StoredStatus = strconv.Quote(p.Status)
		}
		if p.LocationID != nil {
			row.LocationID = p.LocationID.String()
			if p.LocationName != nil && namedVisibly(*p.LocationName) {
				row.Location = *p.LocationName
			}
		}
		if p.EncodedAt != nil {
			row.EncodedAt = utcStamp(*p.EncodedAt)
		}
		if p.RetiredAt != nil {
			row.RetiredAt = utcStamp(*p.RetiredAt)
		}
		if p.ReplacedBy != nil {
			row.ReplacedBy = *p.ReplacedBy
		}
		v.Rows = append(v.Rows, row)
	}
	return v
}
