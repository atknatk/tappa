package tenant

// staffapi.go -- the integration API's employee (M11 API-6; ADR 0028 §9, §12, K-14 ... K-18,
// K-21, K-23, K-24, K-25): PUT /v1/employees/{ref}, DELETE /v1/employees/{ref},
// POST /v1/employees/{ref}/resend-activation and the two reads. internal/handler/api is the one
// caller; the writes run inside the API's write runner (internal/apikey.Writes), whose
// transaction is in the KEY's tenant context -- the tenant a request acts for is never one the
// request names (ADR 0028 §2).
//
// THE SAME RULES AS THE PANEL, WHERE THE PANEL HAS ONE:
//
//	the address   deliverableEmail -- ChangeEmail's send rule (mail.ValidRecipient: ASCII, no
//	              display name, at most 254 bytes), not the add form's weaker one: an API
//	              address is one the invitation queue WILL send to (K-16). An administrator's
//	              address of this business is refused (ADR 0022 §7); another live employee's is
//	              employees_tenant_email_key's 23505, recognised by NAME (isEmailTaken) -> 409.
//	the change    ChangeEmail's order and reason: retire, lock FOR UPDATE, retire again, write --
//	              so an invitation being minted right now (the queue worker's CreateInvite) is
//	              either retired by this change or waits for it (K-15). Then the queue: a person
//	              who never activated gets a new invitation queued; an activated one none.
//	deactivation  DeactivateEmployee and its trail row, in the write's transaction (RecordTx) --
//	              with K-25's budget in FRONT of it (below).
//	the names     the panel's personName (whitespace collapsed, blank and bidi refused, at most
//	              120 runes) after CleanName, and the API's character rule
//	              (NameHasHiddenCharacters -- what API-5 applies to a venue name) on a new or
//	              changed name. The schema refuses a control character in first_name/last_name
//	              (00037's CHECK); this says so as a 422 instead of a 23514, and refuses the
//	              direction controls and invisible joiners the CHECK cannot see.
//
// K-25, THE DEACTIVATION BUDGET (ADR 0028 §12). An API deactivation skips ADR 0010's two-step
// confirmation; what bounds a leaked key instead is a per-KEY hourly budget, counted from the
// audit trail -- never from process memory, so a restart cannot reset it -- under an advisory lock
// on the key, in the SAME transaction as the deactivation and its trail row (RecordTx). If the
// trail row were written in a transaction of its own, a second request could take the lock after
// the first commit but before the row is visible and count one too few; N concurrent requests
// would exceed the budget by the degree of concurrency. TestDeactivationBudget_ConcurrentLastSlotExactlyOne
// measures that.
//
// K-18, THE FIRST BINDING. A ref this business does not hold binds, before anything is created,
// the live employee of THIS business with no ref whose address is the PUT's -- never across
// tenants (K-24). Otherwise a new row is opened -- with no home venue (K-14) -- and, when it has an
// address, an invitation is QUEUED (K-16, K-17): the code is minted by the worker at send time.
//
// WHAT IS NEVER WRITTEN TO A LOG OR A TRAIL ROW: an address, a name, a code (§7, ADR 0022 §10).
// The rows say THAT something changed and whether there is an address; the values live in
// `employees` alone.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// The trail actions this file adds. employee.added, employee.email_changed and
// employee.deactivated are the panel's own (staff.go, staffemail.go); the API's rows carry
// detail.via = "api" and actor_id = the key's id (K-23).
const (
	// ActionEmployeeLinked: K-18 bound an existing employee to the integrator's ref.
	ActionEmployeeLinked = "employee.linked"
	// ActionEmployeeUpdated: the integrator's names or code changed.
	ActionEmployeeUpdated = "employee.updated"
	// ActionInviteRequested: resend-activation put the person into the invitation queue.
	ActionInviteRequested = "invite.requested"
	// ActionAPIDeactivationBudgetExceeded: a key ran out of its hourly deactivation budget (K-25)
	// -- the owner's alert. Its target is the KEY's id.
	ActionAPIDeactivationBudgetExceeded = "api.deactivation_budget_exceeded"
)

// DefaultDeactivationBudget is K-25's per-key hourly budget (ADR 0028 §12: 20).
const DefaultDeactivationBudget = 20

// DeactivationBudgetWindow is the budget's window. CountAPIDeactivations spells the same hour.
const DeactivationBudgetWindow = time.Hour

// MaxEmployeeCodeRunes bounds employee_code (00037's CHECK: 1..64).
const MaxEmployeeCodeRunes = 64

// The API's refusals of its own. Each is a sentence the handler turns into a status; none names
// a value.
var (
	// ErrEmployeeEmailIsAdministrators: the address is an administrator's of this business (ADR
	// 0022 §7) -- 422.
	ErrEmployeeEmailIsAdministrators = errors.New("tenant: that address belongs to an administrator of this business")
	// ErrEmployeeCode: the employee code cannot be stored (blank, too long).
	ErrEmployeeCode = errors.New("tenant: that employee code cannot be stored")
	// ErrAlreadyActivated: resend-activation for somebody who has activated (409 -- a new device
	// is the panel's, ADR 0028 §9).
	ErrAlreadyActivated = errors.New("tenant: this employee has already activated")
	// ErrNoAddressToInvite: resend-activation for somebody with no address on file.
	ErrNoAddressToInvite = errors.New("tenant: this employee has no address to send an invitation to")
	// ErrEmployeeChangedConcurrently: between the lookup and the lock another request deactivated
	// or re-bound the row. Nothing was written; the request can be repeated.
	ErrEmployeeChangedConcurrently = errors.New("tenant: the employee changed while this request ran")
)

// EmployeeField names the field a refusal is about, in the domain's words; the handler maps it
// onto the API's.
type EmployeeField string

// The fields.
const (
	FieldFirstName    EmployeeField = "first_name"
	FieldLastName     EmployeeField = "last_name"
	FieldEmployeeCode EmployeeField = "employee_code"
	FieldEmail        EmployeeField = "email"
	FieldExternalRef  EmployeeField = "external_ref"
)

// EmployeeFieldError is a refusal of one field: errors.Is sees the sentinel through it
// (ErrEmployeeName, ErrNameHiddenCharacter, ErrEmployeeEmail, ErrEmployeeEmailIsAdministrators,
// ErrEmployeeCode, ErrExternalRef).
type EmployeeFieldError struct {
	Field EmployeeField
	Err   error
}

func (e *EmployeeFieldError) Error() string {
	return "tenant: " + string(e.Field) + ": " + e.Err.Error()
}

// Unwrap exposes the sentinel.
func (e *EmployeeFieldError) Unwrap() error { return e.Err }

func fieldErr(f EmployeeField, err error) error { return &EmployeeFieldError{Field: f, Err: err} }

// DeactivationBudgetExceeded is K-25's refusal: this key deactivated Budget employees in the last
// hour. RetryAfter is when the oldest of them leaves the window.
type DeactivationBudgetExceeded struct {
	Budget     int
	RetryAfter time.Duration
}

func (e *DeactivationBudgetExceeded) Error() string {
	return "tenant: this key has used its deactivation budget for the hour"
}

// EmployeeWrite is what a PUT turned out to be, for the API's suspension checkpoint (ADR 0028
// §10): opening a NEW employee stays open while the business is suspended; changing one --
// active:false included -- does not.
type EmployeeWrite uint8

const (
	// EmployeeOpens: a new row (a new person, or a re-hire under a ref whose row is deactivated).
	EmployeeOpens EmployeeWrite = iota + 1
	// EmployeeChanges: an existing row -- changed, bound (K-18), deactivated, or matched as it is.
	EmployeeChanges
)

// Admit is the API's suspension checkpoint as this domain sees it (declared here, at the
// consumer): asked once a PUT knows which write it is, before it writes anything. Asking again
// with another class is allowed (a concurrent PUT can turn an opening into a change).
type Admit func(ctx context.Context, w EmployeeWrite) error

// EmployeePut is one PUT /v1/employees/{ref}, as the handler validated it at its boundary. The
// strings are RAW: the cleaning is this file's, so the panel's rules apply exactly once.
type EmployeePut struct {
	// TenantID is the API key's tenant -- the ONE authority (ADR 0028 §2).
	TenantID uuid.UUID
	// ActorID is the API key's id (K-23).
	ActorID uuid.UUID
	// Via is K-23's detail.via ("api"). Required.
	Via string

	ExternalRef  string
	FirstName    string
	LastName     string
	EmployeeCode *string // nil: none
	Email        *string // nil: none
	Active       bool

	// Budget is K-25's hourly budget per key; zero means DefaultDeactivationBudget.
	Budget int
}

// EmployeeDelete is one DELETE /v1/employees/{ref}.
type EmployeeDelete struct {
	TenantID    uuid.UUID
	ActorID     uuid.UUID
	Via         string
	ExternalRef string
	Budget      int
}

// EmployeeResend is one POST /v1/employees/{ref}/resend-activation.
type EmployeeResend struct {
	TenantID    uuid.UUID
	ActorID     uuid.UUID
	Via         string
	ExternalRef string
}

// InvitationState is the activation e-mail's state as the API reports it (ADR 0028 §8:
// "activationEmail durumu ve zamanı").
type InvitationState string

const (
	// InvitationQueued: a request waits in the queue (it is minted and sent at send time).
	InvitationQueued InvitationState = "queued"
	// InvitationSent: an invitation that can still be spent exists AND was delivered -- the trail
	// holds its invite.code_emailed row (or invite.code_shown_to_manager in the panel mode).
	InvitationSent InvitationState = "sent"
	// InvitationUndelivered: no request waits and no delivered link is live, and the person's
	// LAST delivery attempt did not reach them (invite.undelivered or invite.email_refused): the
	// queue gave up, or a press was refused. A resend asks again (API-6 review O-1).
	InvitationUndelivered InvitationState = "undelivered"
	// InvitationNone: none of those -- never asked, spent, expired, or retired.
	InvitationNone InvitationState = "none"
)

// APIEmployee is an employee as the API answers with it. NO ADDRESS (ADR 0028 §8): HasEmail
// says whether there is one.
type APIEmployee struct {
	ID           uuid.UUID
	ExternalRef  *string
	EmployeeCode *string
	FirstName    *string
	LastName     *string
	FullName     string
	Status       string
	HasEmail     bool
	ActivatedAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Invitation   InvitationState
	// InvitationAt is when the request was queued (queued), the delivered invitation minted (sent)
	// or the last attempt failed (undelivered); nil for none.
	InvitationAt *time.Time
}

// Active reports whether the employment is live (not deactivated).
func (e APIEmployee) Active() bool { return e.Status != statusDeactivated }

// EmployeePutResult says what a write did.
type EmployeePutResult struct {
	Employee APIEmployee
	// Created: a new row (201). Rehired says it is a re-hire under a ref whose earlier row is
	// deactivated.
	Created bool
	Rehired bool
	// Linked: K-18 bound an existing employee to the ref.
	Linked bool
	// Changed: an existing row was written (names, address, deactivation, binding).
	Changed bool
	// Deactivated: this write deactivated the employee.
	Deactivated bool
	// Queued: this write put an invitation request into the queue -- the caller tells the K-19
	// worker after the commit.
	Queued bool
}

// cleanAPIPerson is a PUT's text, cleaned and bounded (the character rule waits for the row on
// file: refuseHiddenName judges only what changes).
type cleanAPIPerson struct {
	first, last, full string
	code              *string
	email             *string
}

// cleanEmployeePut cleans and bounds the PUT's text. The order is the body's.
func cleanEmployeePut(c EmployeePut) (cleanAPIPerson, error) {
	var out cleanAPIPerson
	var err error
	if out.first, err = apiPersonName(c.FirstName); err != nil {
		return out, fieldErr(FieldFirstName, err)
	}
	if out.last, err = apiPersonName(c.LastName); err != nil {
		return out, fieldErr(FieldLastName, err)
	}
	out.full = out.first + " " + out.last
	if utf8.RuneCountInString(out.full) > MaxEmployeeNameRunes {
		// The panel's bound on a person's name (MaxEmployeeNameRunes) applies to the name the
		// roster shows, which is the two joined.
		return out, fieldErr(FieldLastName, ErrEmployeeName)
	}
	if c.EmployeeCode != nil {
		code := CleanName(*c.EmployeeCode)
		if code == "" || utf8.RuneCountInString(code) > MaxEmployeeCodeRunes {
			return out, fieldErr(FieldEmployeeCode, ErrEmployeeCode)
		}
		out.code = &code
	}
	if c.Email != nil {
		e, err := deliverableEmail(*c.Email)
		if err != nil || e == nil {
			// "" is not "no address" here: the body says that with null, and an empty string
			// sent as an address is a mistake worth a sentence.
			return out, fieldErr(FieldEmail, ErrEmployeeEmail)
		}
		out.email = e
	}
	return out, nil
}

// apiPersonName is CleanName, then the panel's personName (whitespace collapsed, blank refused,
// at most MaxEmployeeNameRunes). Two classes are refused FIRST, as the API's character sentence
// (hidden_character) rather than personName's "cannot be stored", and whatever the row on file
// holds -- neither can be on file (00037's CHECK refuses a control character in first_name and
// last_name; only this path writes them):
//
//	a control character inside   a tab, a line break, BEL, ESC: personName would collapse the
//	                             first two into a space and keep the others (the panel's add form
//	                             does -- measured, see the API-6 note); the API refuses them, as it
//	                             does in a venue name
//	a direction control          U+202A-U+202E, U+2066-U+2069: personName refuses them too
//
// The rest of the API's rule (a word joiner, a joiner or tag character outside an emoji, a flag
// or a word) is judged against the name on file (checkHiddenNames).
func apiPersonName(raw string) (string, error) {
	s := CleanName(raw)
	if containsBidiControl(s) || strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Cc, r) }) {
		return "", hiddenName{sentinel: ErrEmployeeName}
	}
	return personName(s)
}

// checkHiddenNames applies the character rule to what this PUT introduces, against the row on
// file (nil for a new row): an unchanged name is never refused.
func checkHiddenNames(in cleanAPIPerson, first, last, code *string) error {
	if err := refuseHiddenName(in.first, deref(first), ErrEmployeeName); err != nil {
		return fieldErr(FieldFirstName, err)
	}
	if err := refuseHiddenName(in.last, deref(last), ErrEmployeeName); err != nil {
		return fieldErr(FieldLastName, err)
	}
	if in.code != nil {
		if err := refuseHiddenName(*in.code, deref(code), ErrEmployeeCode); err != nil {
			return fieldErr(FieldEmployeeCode, err)
		}
	}
	return nil
}

// apiEmployeeDetail is the trail row of an API act on an employee (K-23). A purpose-built struct
// with known keys: the ref, and booleans -- NO name, NO address (addDetail's rule: audit_log is
// append-only and a GDPR erasure is an UPDATE on employees that would never reach it).
type apiEmployeeDetail struct {
	Via         string `json:"via"`
	ExternalRef string `json:"external_ref"`
}

type apiAddedDetail struct {
	apiEmployeeDetail
	HasEmail         bool   `json:"has_email"`
	InvitationQueued bool   `json:"invitation_queued"`
	Status           string `json:"status"`
	// RehireOf is the deactivated row this re-hire follows under the same ref ("" for none).
	RehireOf string `json:"rehire_of"`
}

type apiUpdatedDetail struct {
	apiEmployeeDetail
	// Changed names the columns that changed -- never their values.
	Changed []string `json:"changed"`
}

type apiEmailChangedDetail struct {
	apiEmployeeDetail
	HadEmail         bool `json:"had_email"`
	HasEmail         bool `json:"has_email"`
	Retired          int  `json:"retired_invitations"`
	InvitationQueued bool `json:"invitation_queued"`
}

type apiDeactivatedDetail struct {
	apiEmployeeDetail
	PreviousStatus string `json:"previous_status"`
	// Budget is K-25's budget this deactivation was counted against, Used how many of it the key
	// had spent in the hour before it.
	Budget int `json:"budget"`
	Used   int `json:"used"`
}

type apiResendDetail struct {
	apiEmployeeDetail
	Cause string `json:"cause"`
}

type apiBudgetDetail struct {
	Via           string `json:"via"`
	Budget        int    `json:"budget"`
	WindowSeconds int    `json:"window_seconds"`
}

func budgetOf(n int) int {
	if n <= 0 {
		return DefaultDeactivationBudget
	}
	return n
}

// apiCommand checks what every API write needs.
func apiCommand(tenantID, actorID uuid.UUID, via string, tx pgx.Tx) error {
	if err := requireActor(tenantID, actorID); err != nil {
		return err
	}
	if via == "" {
		return errors.New("tenant: an API act needs its detail.via (K-23)")
	}
	if tx == nil {
		return errors.New("tenant: nil transaction")
	}
	return nil
}

// PutEmployeeTx is PUT /v1/employees/{ref} inside tx (the API runner's transaction, in
// c.TenantID's context). admit is asked once the PUT knows whether it opens a new employee, before
// anything is written.
//
// The refusals: an *EmployeeFieldError, ErrEmailTaken (409 email_unavailable),
// ErrUnknownEmployee (active:false for a ref never held), *DeactivationBudgetExceeded,
// ErrEmployeeChangedConcurrently, and whatever admit returns.
func (s *Staff) PutEmployeeTx(ctx context.Context, tx pgx.Tx, c EmployeePut, admit Admit) (EmployeePutResult, error) {
	if err := apiCommand(c.TenantID, c.ActorID, c.Via, tx); err != nil {
		return EmployeePutResult{}, err
	}
	if admit == nil {
		return EmployeePutResult{}, errors.New("tenant: put employee: nil checkpoint")
	}
	ref, err := apiExternalRef(c.ExternalRef)
	if err != nil {
		return EmployeePutResult{}, fieldErr(FieldExternalRef, err)
	}
	in, err := cleanEmployeePut(c)
	if err != nil {
		return EmployeePutResult{}, err
	}
	q := store.New(tx)
	if in.email != nil {
		admin, err := q.IsAdministratorAddress(ctx, store.IsAdministratorAddressParams{TenantID: c.TenantID, Email: *in.email})
		if err != nil {
			return EmployeePutResult{}, fmt.Errorf("tenant: put employee: administrator address: %w", err)
		}
		if admin {
			return EmployeePutResult{}, fieldErr(FieldEmail, ErrEmployeeEmailIsAdministrators)
		}
	}

	live, err := q.GetLiveEmployeeByExternalRef(ctx, store.GetLiveEmployeeByExternalRefParams{
		Email: in.email, TenantID: c.TenantID, ExternalRef: &ref,
	})
	switch {
	case err == nil:
		if err := admit(ctx, EmployeeChanges); err != nil {
			return EmployeePutResult{}, err
		}
		return s.changeAPIEmployee(ctx, tx, q, c, ref, in, live.ID, live.SameEmail, false)
	case !errors.Is(err, pgx.ErrNoRows):
		return EmployeePutResult{}, fmt.Errorf("tenant: put employee: lookup: %w", err)
	}

	latest, err := q.GetLatestEmployeeByExternalRef(ctx, store.GetLatestEmployeeByExternalRefParams{TenantID: c.TenantID, ExternalRef: &ref})
	rehireOf := uuid.Nil
	switch {
	case err == nil:
		rehireOf = latest
	case !errors.Is(err, pgx.ErrNoRows):
		return EmployeePutResult{}, fmt.Errorf("tenant: put employee: earlier rows: %w", err)
	}
	if !c.Active {
		// Nothing live under the ref: a deactivation of somebody already deactivated is the row as
		// it is (no budget is spent on it); of a ref never held, "no such employee".
		if rehireOf == uuid.Nil {
			return EmployeePutResult{}, ErrUnknownEmployee
		}
		if err := admit(ctx, EmployeeChanges); err != nil {
			return EmployeePutResult{}, err
		}
		emp, err := s.apiEmployeeTx(ctx, q, c.TenantID, rehireOf)
		return EmployeePutResult{Employee: emp}, err
	}

	// K-18: the first binding, by address, inside this business only.
	if in.email != nil {
		cand, err := q.FindLinkableEmployeeByEmail(ctx, store.FindLinkableEmployeeByEmailParams{Email: *in.email, TenantID: c.TenantID})
		switch {
		case err == nil:
			if err := admit(ctx, EmployeeChanges); err != nil {
				return EmployeePutResult{}, err
			}
			return s.changeAPIEmployee(ctx, tx, q, c, ref, in, cand.ID, cand.SameEmail, true)
		case !errors.Is(err, pgx.ErrNoRows):
			return EmployeePutResult{}, fmt.Errorf("tenant: put employee: binding: %w", err)
		}
	}

	if err := admit(ctx, EmployeeOpens); err != nil {
		return EmployeePutResult{}, err
	}
	if err := checkHiddenNames(in, nil, nil, nil); err != nil {
		return EmployeePutResult{}, err
	}
	row, err := createAPIEmployee(ctx, tx, store.CreateEmployeeFromAPIParams{
		ExternalRef: &ref, EmployeeCode: in.code, FirstName: &in.first, LastName: &in.last,
		FullName: in.full, Email: in.email, TenantID: c.TenantID,
	})
	switch {
	case errors.Is(err, errRefTaken):
		// A concurrent PUT of the same ref committed the row while this one waited on its index
		// entry (the savepoint took this INSERT back). It is a change of THAT row now -- and the
		// checkpoint is asked again for it.
		live, err := q.GetLiveEmployeeByExternalRef(ctx, store.GetLiveEmployeeByExternalRefParams{
			Email: in.email, TenantID: c.TenantID, ExternalRef: &ref,
		})
		if err != nil {
			return EmployeePutResult{}, fmt.Errorf("tenant: put employee: the concurrent row: %w", err)
		}
		if err := admit(ctx, EmployeeChanges); err != nil {
			return EmployeePutResult{}, err
		}
		return s.changeAPIEmployee(ctx, tx, q, c, ref, in, live.ID, live.SameEmail, false)
	case err != nil:
		if isEmailTaken(err) {
			return EmployeePutResult{}, ErrEmailTaken
		}
		return EmployeePutResult{}, fmt.Errorf("tenant: put employee: create: %w", err)
	}

	queued := false
	if in.email != nil {
		if _, err := q.EnqueueInviteRequest(ctx, store.EnqueueInviteRequestParams{TenantID: c.TenantID, EmployeeID: row.ID}); err != nil {
			return EmployeePutResult{}, fmt.Errorf("tenant: put employee: queue the invitation: %w", err)
		}
		queued = true
	}
	d := apiAddedDetail{
		apiEmployeeDetail: apiEmployeeDetail{Via: c.Via, ExternalRef: ref},
		HasEmail:          row.HasEmail, InvitationQueued: queued, Status: row.Status,
	}
	if rehireOf != uuid.Nil {
		d.RehireOf = rehireOf.String()
	}
	if err := s.recordAPI(ctx, tx, c.TenantID, c.ActorID, ActionEmployeeAdded, row.ID, d); err != nil {
		return EmployeePutResult{}, err
	}
	emp, err := s.apiEmployeeTx(ctx, q, c.TenantID, row.ID)
	if err != nil {
		return EmployeePutResult{}, err
	}
	return EmployeePutResult{Employee: emp, Created: true, Rehired: rehireOf != uuid.Nil, Queued: queued}, nil
}

// errRefTaken: the INSERT met another live row under the ref (employees_tenant_external_ref_key).
var errRefTaken = errors.New("tenant: a live employee holds the ref")

// createAPIEmployee runs CreateEmployeeFromAPI inside a SAVEPOINT of tx: a 23505 on the ref's
// unique index (a concurrent PUT of the same new ref committed first) is rolled back to the
// savepoint and reported as errRefTaken, so the PUT carries on in the same transaction as a
// change of the row the other request made. Any other failure -- the address index included --
// is returned as it came, after the savepoint is rolled back.
func createAPIEmployee(ctx context.Context, tx pgx.Tx, p store.CreateEmployeeFromAPIParams) (store.CreateEmployeeFromAPIRow, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return store.CreateEmployeeFromAPIRow{}, fmt.Errorf("tenant: put employee: savepoint: %w", err)
	}
	row, err := store.New(sp).CreateEmployeeFromAPI(ctx, p)
	if err != nil {
		if rerr := sp.Rollback(ctx); rerr != nil {
			return row, errors.Join(err, rerr)
		}
		if constraintOf(err) == "employees_tenant_external_ref_key" {
			return row, errRefTaken
		}
		return row, err
	}
	if err := sp.Commit(ctx); err != nil {
		return row, fmt.Errorf("tenant: put employee: release the savepoint: %w", err)
	}
	return row, nil
}

// changeAPIEmployee writes a PUT onto an existing live row (id): the ref (linking), the names and
// code, the address (K-15), and the deactivation (K-25). sameEmail is the UNLOCKED comparison --
// it only decides whether the first retirement runs before the lock, ChangeEmail's lock order;
// the decision itself is the LOCKED row's.
func (s *Staff) changeAPIEmployee(ctx context.Context, tx pgx.Tx, q *store.Queries, c EmployeePut, ref string,
	in cleanAPIPerson, id uuid.UUID, sameEmail, linking bool) (EmployeePutResult, error) {
	out := EmployeePutResult{Linked: linking}
	budget := budgetOf(c.Budget)
	used := 0
	if !c.Active {
		// K-25 FIRST, before any row lock: one lock order (the key, then the rows) for every
		// deactivation, and a refused request does no other work.
		n, err := s.chargeDeactivation(ctx, q, c.TenantID, c.ActorID, budget)
		if err != nil {
			return out, err
		}
		used = n
	}
	retire := store.CancelPendingInvitesForEmployeeParams{TenantID: c.TenantID, EmployeeID: id}
	retired := 0
	if !sameEmail {
		ids, err := q.CancelPendingInvitesForEmployee(ctx, retire)
		if err != nil {
			return out, fmt.Errorf("tenant: put employee: retire invitations: %w", err)
		}
		retired += len(ids)
	}
	locked, err := q.LockEmployeeForAPI(ctx, store.LockEmployeeForAPIParams{Email: in.email, TenantID: c.TenantID, ID: id})
	if err != nil {
		return out, fmt.Errorf("tenant: put employee: lock: %w", err)
	}
	switch {
	case locked.Status == statusDeactivated && !c.Active && !linking:
		// A concurrent request deactivated the row first: this one is the row as it is.
		if retired > 0 {
			// Nothing of this request may stand without its own write: roll the retirement back.
			return out, ErrEmployeeChangedConcurrently
		}
		emp, err := s.apiEmployeeTx(ctx, q, c.TenantID, id)
		out.Employee = emp
		return out, err
	case locked.Status == statusDeactivated:
		return out, ErrEmployeeChangedConcurrently
	case linking && locked.ExternalRef != nil:
		return out, ErrEmployeeChangedConcurrently
	case !linking && deref(locked.ExternalRef) != ref:
		return out, ErrEmployeeChangedConcurrently
	}
	if err := checkHiddenNames(in, locked.FirstName, locked.LastName, locked.EmployeeCode); err != nil {
		return out, err
	}
	detail := apiEmployeeDetail{Via: c.Via, ExternalRef: ref}

	if linking {
		if _, err := q.SetEmployeeExternalRef(ctx, store.SetEmployeeExternalRefParams{ExternalRef: &ref, TenantID: c.TenantID, ID: id}); err != nil {
			if constraintOf(err) == "employees_tenant_external_ref_key" {
				// A concurrent PUT opened a row under this ref after the lookup.
				return out, ErrEmployeeChangedConcurrently
			}
			return out, fmt.Errorf("tenant: put employee: bind the ref: %w", err)
		}
		if err := s.recordAPI(ctx, tx, c.TenantID, c.ActorID, ActionEmployeeLinked, id, detail); err != nil {
			return out, err
		}
		out.Changed = true
	}

	var changed []string
	if deref(locked.FirstName) != in.first {
		changed = append(changed, "first_name")
	}
	if deref(locked.LastName) != in.last {
		changed = append(changed, "last_name")
	}
	if locked.FullName != in.full {
		changed = append(changed, "full_name")
	}
	if deref(locked.EmployeeCode) != deref(in.code) || (locked.EmployeeCode == nil) != (in.code == nil) {
		changed = append(changed, "employee_code")
	}
	if len(changed) > 0 {
		if _, err := q.UpdateEmployeeNamesFromAPI(ctx, store.UpdateEmployeeNamesFromAPIParams{
			FirstName: &in.first, LastName: &in.last, FullName: in.full, EmployeeCode: in.code,
			TenantID: c.TenantID, ID: id,
		}); err != nil {
			return out, fmt.Errorf("tenant: put employee: names: %w", err)
		}
		if err := s.recordAPI(ctx, tx, c.TenantID, c.ActorID, ActionEmployeeUpdated, id,
			apiUpdatedDetail{apiEmployeeDetail: detail, Changed: changed}); err != nil {
			return out, err
		}
		out.Changed = true
	}

	if !locked.SameEmail {
		// K-15, ChangeEmail's third step: an invitation committed while the first retirement could
		// not see it (the queue worker's mint, which held FOR KEY SHARE on this row until the lock
		// above got it) is retired here.
		ids, err := q.CancelPendingInvitesForEmployee(ctx, retire)
		if err != nil {
			return out, fmt.Errorf("tenant: put employee: retire invitations: %w", err)
		}
		retired += len(ids)
		if _, err := q.SetEmployeeEmail(ctx, store.SetEmployeeEmailParams{Email: in.email, TenantID: c.TenantID, ID: id}); err != nil {
			if isEmailTaken(err) {
				return out, ErrEmailTaken
			}
			return out, fmt.Errorf("tenant: put employee: address: %w", err)
		}
		queued := false
		switch {
		case in.email == nil || !c.Active:
			if err := q.DeleteInviteRequest(ctx, store.DeleteInviteRequestParams{TenantID: c.TenantID, EmployeeID: id}); err != nil {
				return out, fmt.Errorf("tenant: put employee: unqueue: %w", err)
			}
		case locked.ActivatedAt == nil && locked.Status == statusInvited:
			// Never activated: the new address gets an invitation (K-15). RequeueInviteRequest also
			// RELEASES a request a worker holds, so a mint for the old address cannot take the
			// request out of the queue after this commits.
			if err := q.RequeueInviteRequest(ctx, store.RequeueInviteRequestParams{TenantID: c.TenantID, EmployeeID: id}); err != nil {
				return out, fmt.Errorf("tenant: put employee: queue the invitation: %w", err)
			}
			queued = true
		}
		if err := s.recordAPI(ctx, tx, c.TenantID, c.ActorID, ActionEmployeeEmailChanged, id, apiEmailChangedDetail{
			apiEmployeeDetail: detail, HadEmail: locked.HasEmail, HasEmail: in.email != nil, Retired: retired, InvitationQueued: queued,
		}); err != nil {
			return out, err
		}
		out.Changed, out.Queued = true, queued
	} else if retired > 0 {
		// The unlocked read said "a new address" and the locked row says "the same": another request
		// wrote this address in between. The first retirement must not stand on its own.
		return out, ErrEmployeeChangedConcurrently
	}

	if !c.Active {
		if err := s.deactivateAPIEmployee(ctx, tx, q, c.TenantID, c.ActorID, c.Via, ref, id, locked.Status, budget, used); err != nil {
			return out, err
		}
		out.Changed, out.Deactivated, out.Queued = true, true, false
	}
	emp, err := s.apiEmployeeTx(ctx, q, c.TenantID, id)
	if err != nil {
		return out, err
	}
	out.Employee = emp
	return out, nil
}

// chargeDeactivation takes the key's budget lock and counts its deactivations in the window
// (K-25). It returns how many were spent, or *DeactivationBudgetExceeded when none is left.
func (s *Staff) chargeDeactivation(ctx context.Context, q *store.Queries, tenantID, keyID uuid.UUID, budget int) (int, error) {
	if s.beforeBudget != nil {
		s.beforeBudget(ctx)
	}
	if err := lockBudget(ctx, q, tenantID, keyID); err != nil {
		return 0, err
	}
	n, err := q.CountAPIDeactivations(ctx, store.CountAPIDeactivationsParams{TenantID: tenantID, KeyID: keyID})
	if err != nil {
		return 0, fmt.Errorf("tenant: deactivation budget: count: %w", err)
	}
	if int(n.Deactivations) >= budget {
		after := time.Until(n.Oldest.Add(DeactivationBudgetWindow))
		if after < time.Second {
			after = time.Second
		}
		return int(n.Deactivations), &DeactivationBudgetExceeded{Budget: budget, RetryAfter: after}
	}
	return int(n.Deactivations), nil
}

// lockBudget takes K-25's lock on the key, in the caller's transaction. No row means the
// business's own row was not visible in this context, so no lock was taken: a failure, never
// "locked".
func lockBudget(ctx context.Context, q *store.Queries, tenantID, keyID uuid.UUID) error {
	if _, err := q.LockAPIDeactivationBudget(ctx, store.LockAPIDeactivationBudgetParams{KeyID: keyID, TenantID: tenantID}); err != nil {
		return fmt.Errorf("tenant: deactivation budget: lock: %w", err)
	}
	return nil
}

// deactivateAPIEmployee is DeactivateEmployee + the queue + the trail row, in tx, AFTER
// chargeDeactivation took the key's lock in the same transaction.
func (s *Staff) deactivateAPIEmployee(ctx context.Context, tx pgx.Tx, q *store.Queries, tenantID, keyID uuid.UUID, via, ref string,
	id uuid.UUID, previous string, budget, used int) error {
	if _, err := q.DeactivateEmployee(ctx, store.DeactivateEmployeeParams{TenantID: tenantID, ID: id}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEmployeeChangedConcurrently
		}
		return fmt.Errorf("tenant: deactivate: %w", err)
	}
	// Nothing is left to send to somebody whose taps are refused from now on.
	if err := q.DeleteInviteRequest(ctx, store.DeleteInviteRequestParams{TenantID: tenantID, EmployeeID: id}); err != nil {
		return fmt.Errorf("tenant: deactivate: unqueue: %w", err)
	}
	return s.recordAPI(ctx, tx, tenantID, keyID, ActionEmployeeDeactivated, id, apiDeactivatedDetail{
		apiEmployeeDetail: apiEmployeeDetail{Via: via, ExternalRef: ref}, PreviousStatus: previous, Budget: budget, Used: used,
	})
}

// DeleteEmployeeTx is DELETE /v1/employees/{ref} inside tx: the live employee under the ref is
// deactivated (K-25's budget first), or -- when the ref's row is deactivated already -- answered as
// it is. The caller asked the checkpoint before (the class is fixed).
func (s *Staff) DeleteEmployeeTx(ctx context.Context, tx pgx.Tx, c EmployeeDelete) (EmployeePutResult, error) {
	if err := apiCommand(c.TenantID, c.ActorID, c.Via, tx); err != nil {
		return EmployeePutResult{}, err
	}
	ref, err := apiExternalRef(c.ExternalRef)
	if err != nil {
		return EmployeePutResult{}, fieldErr(FieldExternalRef, err)
	}
	q := store.New(tx)
	live, err := q.GetLiveEmployeeByExternalRef(ctx, store.GetLiveEmployeeByExternalRefParams{TenantID: c.TenantID, ExternalRef: &ref})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		latest, err := q.GetLatestEmployeeByExternalRef(ctx, store.GetLatestEmployeeByExternalRefParams{TenantID: c.TenantID, ExternalRef: &ref})
		if errors.Is(err, pgx.ErrNoRows) {
			return EmployeePutResult{}, ErrUnknownEmployee
		}
		if err != nil {
			return EmployeePutResult{}, fmt.Errorf("tenant: delete employee: earlier rows: %w", err)
		}
		emp, err := s.apiEmployeeTx(ctx, q, c.TenantID, latest)
		return EmployeePutResult{Employee: emp}, err
	case err != nil:
		return EmployeePutResult{}, fmt.Errorf("tenant: delete employee: lookup: %w", err)
	}
	budget := budgetOf(c.Budget)
	used, err := s.chargeDeactivation(ctx, q, c.TenantID, c.ActorID, budget)
	if err != nil {
		return EmployeePutResult{}, err
	}
	if err := s.deactivateAPIEmployee(ctx, tx, q, c.TenantID, c.ActorID, c.Via, ref, live.ID, live.Status, budget, used); err != nil {
		if errors.Is(err, ErrEmployeeChangedConcurrently) {
			// Deactivated by a concurrent request between the lookup and the UPDATE: this one is the
			// row as it is -- and it wrote nothing.
			emp, rerr := s.apiEmployeeTx(ctx, q, c.TenantID, live.ID)
			return EmployeePutResult{Employee: emp}, rerr
		}
		return EmployeePutResult{}, err
	}
	emp, err := s.apiEmployeeTx(ctx, q, c.TenantID, live.ID)
	if err != nil {
		return EmployeePutResult{}, err
	}
	return EmployeePutResult{Employee: emp, Changed: true, Deactivated: true}, nil
}

// ResendActivationTx is POST /v1/employees/{ref}/resend-activation inside tx (ADR 0028 §9, K-17):
// it NEVER retires an invitation. With a request already waiting in the queue, or an invitation
// minted in the last hour that can still be spent, it does nothing (the 202 is the same); otherwise
// it queues a request, and the worker mints at send time (retiring the older ones then, in its own
// transaction). An activated person is ErrAlreadyActivated (409); nobody live under the ref is
// ErrUnknownEmployee; nobody with an address ErrNoAddressToInvite.
func (s *Staff) ResendActivationTx(ctx context.Context, tx pgx.Tx, c EmployeeResend) (EmployeePutResult, error) {
	if err := apiCommand(c.TenantID, c.ActorID, c.Via, tx); err != nil {
		return EmployeePutResult{}, err
	}
	ref, err := apiExternalRef(c.ExternalRef)
	if err != nil {
		return EmployeePutResult{}, fieldErr(FieldExternalRef, err)
	}
	q := store.New(tx)
	live, err := q.GetLiveEmployeeByExternalRef(ctx, store.GetLiveEmployeeByExternalRefParams{TenantID: c.TenantID, ExternalRef: &ref})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return EmployeePutResult{}, ErrUnknownEmployee
	case err != nil:
		return EmployeePutResult{}, fmt.Errorf("tenant: resend: lookup: %w", err)
	}
	emp, err := s.apiEmployeeTx(ctx, q, c.TenantID, live.ID)
	if err != nil {
		return EmployeePutResult{}, err
	}
	switch {
	case emp.Status == statusActive || emp.ActivatedAt != nil:
		return EmployeePutResult{}, ErrAlreadyActivated
	case !emp.HasEmail:
		return EmployeePutResult{}, ErrNoAddressToInvite
	case emp.Invitation == InvitationQueued:
		return EmployeePutResult{Employee: emp}, nil
	}
	recent, err := q.CountRecentSentInvites(ctx, store.CountRecentSentInvitesParams{TenantID: c.TenantID, EmployeeID: live.ID})
	if err != nil {
		return EmployeePutResult{}, fmt.Errorf("tenant: resend: recent invitations: %w", err)
	}
	if recent > 0 {
		return EmployeePutResult{Employee: emp}, nil
	}
	if _, err := q.EnqueueInviteRequest(ctx, store.EnqueueInviteRequestParams{TenantID: c.TenantID, EmployeeID: live.ID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// ON CONFLICT DO NOTHING: a concurrent resend queued it first. The same no-op.
			return EmployeePutResult{Employee: emp}, nil
		}
		return EmployeePutResult{}, fmt.Errorf("tenant: resend: queue: %w", err)
	}
	if err := s.recordAPI(ctx, tx, c.TenantID, c.ActorID, ActionInviteRequested, live.ID, apiResendDetail{
		apiEmployeeDetail: apiEmployeeDetail{Via: c.Via, ExternalRef: ref}, Cause: "resend",
	}); err != nil {
		return EmployeePutResult{}, err
	}
	if emp, err = s.apiEmployeeTx(ctx, q, c.TenantID, live.ID); err != nil {
		return EmployeePutResult{}, err
	}
	return EmployeePutResult{Employee: emp, Changed: true, Queued: true}, nil
}

// RecordDeactivationBudgetExceeded writes the owner's alert (ADR 0028 §12): one
// api.deactivation_budget_exceeded row per key per budget window, in a transaction of its own --
// the refused request's transaction was rolled back, and a refusal is when a trail row is most
// needed. Its target is the key's id; the detail says the budget and the window and nothing of the
// request.
func (s *Staff) RecordDeactivationBudgetExceeded(ctx context.Context, tenantID, keyID uuid.UUID, via string, budget int) error {
	if err := requireActor(tenantID, keyID); err != nil {
		return err
	}
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		// The key's budget lock: two refused requests at once write one row.
		if err := lockBudget(ctx, q, tenantID, keyID); err != nil {
			return err
		}
		n, err := q.CountAPIBudgetAlerts(ctx, store.CountAPIBudgetAlertsParams{TenantID: tenantID, KeyID: keyID.String()})
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		actor := keyID
		_, err = s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: tenantID, ActorID: &actor, Action: ActionAPIDeactivationBudgetExceeded, Target: keyID.String(),
			Detail: apiBudgetDetail{Via: via, Budget: budgetOf(budget), WindowSeconds: int(DeactivationBudgetWindow / time.Second)},
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("tenant: record the deactivation budget alert: %w", err)
	}
	return nil
}

func (s *Staff) recordAPI(ctx context.Context, tx pgx.Tx, tenantID, keyID uuid.UUID, action string, target uuid.UUID, detail any) error {
	actor := keyID
	if _, err := s.trail.RecordTx(ctx, tx, audit.Event{
		TenantID: tenantID, ActorID: &actor, Action: action, Target: target.String(), Detail: detail,
	}); err != nil {
		return fmt.Errorf("tenant: %s: trail: %w", action, err)
	}
	return nil
}

// apiEmployeeTx reads one employee as the API answers with it, inside tx.
func (s *Staff) apiEmployeeTx(ctx context.Context, q *store.Queries, tenantID, id uuid.UUID) (APIEmployee, error) {
	r, err := q.GetAPIEmployee(ctx, store.GetAPIEmployeeParams{TenantID: tenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return APIEmployee{}, ErrUnknownEmployee
	}
	if err != nil {
		return APIEmployee{}, fmt.Errorf("tenant: read the employee: %w", err)
	}
	return apiEmployeeOf(store.ListAPIEmployeesRow(r)), nil
}

func apiEmployeeOf(r store.ListAPIEmployeesRow) APIEmployee {
	e := APIEmployee{
		ID: r.ID, ExternalRef: r.ExternalRef, EmployeeCode: r.EmployeeCode, FirstName: r.FirstName, LastName: r.LastName,
		FullName: r.FullName, Status: r.Status, HasEmail: r.HasEmail, ActivatedAt: r.ActivatedAt,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Invitation: InvitationNone,
	}
	switch {
	case r.QueuedAt != nil:
		at := *r.QueuedAt
		e.Invitation, e.InvitationAt = InvitationQueued, &at
	case r.HasLiveInvite:
		at := r.LiveInviteAt
		e.Invitation, e.InvitationAt = InvitationSent, &at
	case r.LastUndelivered:
		at := r.LastAttemptAt
		e.Invitation, e.InvitationAt = InvitationUndelivered, &at
	}
	return e
}

// APIEmployeeByRef reads the employee under ref: the live row, or -- when none is live -- the most
// recent deactivated one. ErrUnknownEmployee when the business never held the ref.
func (s *Staff) APIEmployeeByRef(ctx context.Context, tenantID uuid.UUID, ref string) (APIEmployee, error) {
	if tenantID == uuid.Nil {
		return APIEmployee{}, errors.New("tenant: tenant id is required")
	}
	r, err := apiExternalRef(ref)
	if err != nil {
		return APIEmployee{}, fieldErr(FieldExternalRef, err)
	}
	var out APIEmployee
	err = s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		id, err := q.GetLatestEmployeeByExternalRef(ctx, store.GetLatestEmployeeByExternalRefParams{TenantID: tenantID, ExternalRef: &r})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownEmployee
		}
		if err != nil {
			return err
		}
		out, err = s.apiEmployeeTx(ctx, q, tenantID, id)
		return err
	})
	if err != nil {
		return APIEmployee{}, wrap("api employee", err)
	}
	return out, nil
}

// APIEmployeeByID reads one employee of the business by id -- an Idempotency-Key replay's answer
// (the ledger keeps the id, never a body), so the employee as it is NOW.
func (s *Staff) APIEmployeeByID(ctx context.Context, tenantID, id uuid.UUID) (APIEmployee, error) {
	if tenantID == uuid.Nil {
		return APIEmployee{}, errors.New("tenant: tenant id is required")
	}
	var out APIEmployee
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.apiEmployeeTx(ctx, store.New(tx), tenantID, id)
		return err
	})
	if err != nil {
		return APIEmployee{}, wrap("api employee", err)
	}
	return out, nil
}

// ListAPIEmployees reads one page of the business's employees in id order after `after` (nil: the
// first page). limit is the handler's bounded page size.
func (s *Staff) ListAPIEmployees(ctx context.Context, tenantID uuid.UUID, after *uuid.UUID, limit int) ([]APIEmployee, error) {
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant: tenant id is required")
	}
	if limit < 1 {
		return nil, errors.New("tenant: a page needs a size")
	}
	var out []APIEmployee
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := store.New(tx).ListAPIEmployees(ctx, store.ListAPIEmployeesParams{TenantID: tenantID, After: after, PageSize: int32(limit)})
		if err != nil {
			return err
		}
		out = make([]APIEmployee, 0, len(rows))
		for _, r := range rows {
			out = append(out, apiEmployeeOf(r))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("tenant: list api employees: %w", err)
	}
	return out, nil
}
