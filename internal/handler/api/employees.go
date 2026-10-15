package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// THE EMPLOYEE ENDPOINTS (M11 API-6; ADR 0028 §8, §9, §10, §12; K-14 ... K-18, K-23 ... K-25):
//
//	GET    /v1/employees                              a page of the business's employees
//	GET    /v1/employees/{ref}                        one, by the integrator's ref
//	PUT    /v1/employees/{ref}                        create (201), bind (K-18), change, re-hire,
//	                                                  deactivate (active:false)
//	DELETE /v1/employees/{ref}                        deactivate
//	POST   /v1/employees/{ref}/resend-activation      queue an invitation (202)
//
// NO ADDRESS LEAVES THE API (ADR 0028 §8): the answer says whether there is one (hasEmail) and the
// activation e-mail's state and time (activationEmail) -- never the address. The work is
// internal/domain/tenant's staffapi.go, the panel's own employee domain, so the address, the names
// and the trail follow the panel's rules (and its file says where they differ).
//
// THE BODY OF A PUT (API-5's contract -- one UTF-8 JSON object, every key exactly once, no other
// key):
//
//	firstName     string, required
//	lastName      string, required
//	email         string or null, required: the address the invitation is sent to (null: none)
//	active        boolean, required: false deactivates (K-25's budget)
//	employeeCode  string or null, optional
//	externalRef   string, optional; when present it must be the path's ref
//
// THE CHECKPOINT (§10): a PUT that opens a NEW employee is open while the business is suspended;
// one that changes an existing employee -- active:false, a K-18 binding and an unchanged PUT
// included -- is 403, and so is DELETE; resend-activation is open. The PUT's class is known only
// after the lookup, so the domain asks the checkpoint (tenant.Admit) before it writes.
//
// K-25: a deactivation past the key's hourly budget is 429 deactivation_budget_exceeded with
// Retry-After; the refused request's transaction is rolled back, and the owner's alert
// (api.deactivation_budget_exceeded) is written in a transaction of its own after it.
//
// K-19: a write that queued an invitation tells the worker AFTER the commit -- the chain's notice
// came before the write, and a worker run between the two would find nothing.

// Employees is the employee domain's API half (*tenant.Staff), declared at the consumer (§7).
type Employees interface {
	PutEmployeeTx(ctx context.Context, tx pgx.Tx, c tenant.EmployeePut, admit tenant.Admit) (tenant.EmployeePutResult, error)
	DeleteEmployeeTx(ctx context.Context, tx pgx.Tx, c tenant.EmployeeDelete) (tenant.EmployeePutResult, error)
	ResendActivationTx(ctx context.Context, tx pgx.Tx, c tenant.EmployeeResend) (tenant.EmployeePutResult, error)
	APIEmployeeByRef(ctx context.Context, tenantID uuid.UUID, ref string) (tenant.APIEmployee, error)
	APIEmployeeByID(ctx context.Context, tenantID, id uuid.UUID) (tenant.APIEmployee, error)
	ListAPIEmployees(ctx context.Context, tenantID uuid.UUID, after *uuid.UUID, limit int) ([]tenant.APIEmployee, error)
	RecordDeactivationBudgetExceeded(ctx context.Context, tenantID, keyID uuid.UUID, via string, budget int) error
}

var _ Employees = (*tenant.Staff)(nil)

// The routes under Prefix.
const (
	employeesPath     = "/employees"
	employeePath      = "/employees/{ref}"
	employeeResend    = "/employees/{ref}/resend-activation"
	employeeIDPrefix  = "emp_"
	resendTail        = "/resend-activation"
	employeesSegment  = "/employees/"
	fieldFirstName    = "firstName"
	fieldLastName     = "lastName"
	fieldEmail        = "email"
	fieldEmployeeCode = "employeeCode"
	fieldCursor       = "cursor"
)

var employeeFields = []string{fieldFirstName, fieldLastName, fieldEmail, fieldActive, fieldEmployeeCode, fieldExternalRef}

// The employee refusals.
const (
	// reasonAdministratorAddress: the address is an administrator's of this business (ADR 0022 §7).
	reasonAdministratorAddress = "administrator_address"
)

var (
	problemEmployeeRef = invalidField(fieldRef, reasonInvalid,
		"The employee reference must be 1 to 64 characters of A-Z, a-z, 0-9, '.', '_', '~' and '-', starting with a letter or a digit.")
	problemEmployeeUnknownField = &Problem{Code: CodeValidationFailed,
		Message: "The body carries a field this endpoint does not accept.",
		Details: map[string]any{"reason": reasonUnknownField, "allowed": employeeFields}}
	problemFirstNameRequired = invalidField(fieldFirstName, reasonRequired, "firstName is required.")
	problemFirstNameType     = invalidField(fieldFirstName, reasonType, "firstName must be a string.")
	problemLastNameRequired  = invalidField(fieldLastName, reasonRequired, "lastName is required.")
	problemLastNameType      = invalidField(fieldLastName, reasonType, "lastName must be a string.")
	problemEmailRequired     = invalidField(fieldEmail, reasonRequired, "email is required: an address, or null for none.")
	problemEmailType         = invalidField(fieldEmail, reasonType, "email must be a string or null.")
	problemEmailInvalid      = invalidField(fieldEmail, reasonInvalid,
		"email must be one plain ASCII address (no display name, no spaces, at most 254 characters); send null for none.")
	problemEmailAdministrator = invalidField(fieldEmail, reasonAdministratorAddress,
		"email is the address of an administrator of this business; an employee needs an address of their own.")
	problemCodeType    = invalidField(fieldEmployeeCode, reasonType, "employeeCode must be a string or null.")
	problemCodeInvalid = invalidField(fieldEmployeeCode, reasonInvalid,
		"employeeCode must be 1 to 64 characters once surrounding spaces are removed.")
	problemCodeHidden = invalidField(fieldEmployeeCode, reasonHiddenCharacter,
		"employeeCode cannot carry a control character, a line or paragraph separator, a direction override or isolate, a zero-width space, a word joiner or an inner U+FEFF, nor a joiner or tag character outside an emoji, a flag or a word.")
	problemNoAddressToInvite = invalidField(fieldEmail, reasonRequired,
		"This employee has no address on file; PUT the employee with an email first.")
	problemEmailUnavailable = &Problem{Code: CodeEmailUnavailable,
		Message: "Another active employee of this business already has that address."}
	problemAlreadyActivated = &Problem{Code: CodeAlreadyActivated,
		Message: "This employee has already activated. A new device is set up in the Taptime panel."}
	problemEmployeeNotFound = &Problem{Code: CodeNotFound,
		Message: "This business has no employee with that reference."}
	problemEmployeeGone = &Problem{Code: CodeNotFound,
		Message: "The employee this Idempotency-Key recorded no longer exists."}
	problemCursor = &Problem{Code: CodeInvalidRequest,
		Message: "cursor must be the nextCursor of a previous page.", Details: map[string]any{"parameter": fieldCursor}}
	problemQueryParameter = &Problem{Code: CodeInvalidRequest,
		Message: "This endpoint takes the query parameters limit and cursor only, each at most once.",
		Details: map[string]any{"allowed": []string{"limit", fieldCursor}}}
)

// nameProblem is the refusal of a name field.
func nameProblem(field string, hidden bool) *Problem {
	if hidden {
		return invalidField(field, reasonHiddenCharacter,
			field+" cannot carry a control character (a tab or a line break included), a line or paragraph separator, a direction override or isolate (U+202A-U+202E, U+2066-U+2069), a zero-width space, a word joiner or an inner U+FEFF, nor a joiner or tag character outside an emoji, a flag or a word.")
	}
	return invalidField(field, reasonInvalid,
		field+" must have a visible character, and firstName and lastName together at most 120 characters (spaces are collapsed).")
}

// budgetExceeded is K-25's 429.
func budgetExceeded(after time.Duration) *Problem {
	secs := max(int((after+time.Second-1)/time.Second), 1)
	return &Problem{Code: CodeDeactivationBudgetExceeded,
		Message: "This key has deactivated as many employees as it may in an hour. Retry after the time in Retry-After.",
		Details: map[string]any{"retryAfterSeconds": secs}, retryAfter: time.Duration(secs) * time.Second}
}

// employeePut is a parsed body.
type employeePut struct {
	first, last string
	email, code *string
	active      bool
}

// parseEmployeeBody reads exactly one JSON object with employeeFields' keys and types.
func parseEmployeeBody(body []byte, ref string) (employeePut, *Problem) {
	if !utf8.Valid(body) {
		return employeePut{}, problemNotUTF8
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	raw, duplicate, ok := readObject(dec)
	if !ok {
		return employeePut{}, problemLocationBody
	}
	if _, err := dec.Token(); err != io.EOF {
		return employeePut{}, problemLocationBody
	}
	if duplicate {
		return employeePut{}, problemDuplicateField
	}
	for k := range raw {
		known := false
		for _, f := range employeeFields {
			known = known || k == f
		}
		if !known {
			return employeePut{}, problemEmployeeUnknownField
		}
	}
	isNull := func(v json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(v), []byte("null")) }
	str := func(key string, nullable bool, required, typ *Problem) (*string, bool, *Problem) {
		v, ok := raw[key]
		if !ok {
			return nil, false, required
		}
		if isNull(v) {
			if nullable {
				return nil, true, nil
			}
			return nil, true, typ
		}
		var s string
		if json.Unmarshal(v, &s) != nil {
			return nil, true, typ
		}
		if brokenEscape(v, s) {
			return nil, true, problemNotUTF8
		}
		return &s, true, nil
	}
	var out employeePut
	first, _, prob := str(fieldFirstName, false, problemFirstNameRequired, problemFirstNameType)
	if prob != nil {
		return employeePut{}, prob
	}
	last, _, prob := str(fieldLastName, false, problemLastNameRequired, problemLastNameType)
	if prob != nil {
		return employeePut{}, prob
	}
	out.first, out.last = *first, *last
	if out.email, _, prob = str(fieldEmail, true, problemEmailRequired, problemEmailType); prob != nil {
		return employeePut{}, prob
	}
	act, ok := raw[fieldActive]
	if !ok {
		return employeePut{}, problemActiveRequired
	}
	if isNull(act) || json.Unmarshal(act, &out.active) != nil {
		return employeePut{}, problemActiveType
	}
	if out.code, _, prob = str(fieldEmployeeCode, true, nil, problemCodeType); prob != nil {
		return employeePut{}, prob
	}
	bodyRef, present, prob := str(fieldExternalRef, false, nil, problemRefType)
	switch {
	case prob != nil:
		return employeePut{}, prob
	case present && *bodyRef != ref:
		return employeePut{}, problemRefMismatch
	}
	return out, nil
}

// employeeRef is the path's ref after /employees/, with tail ("" or "/resend-activation") after
// it: decoded exactly once off the ESCAPED path, then held to the strict pattern (locationRef's
// rule and reason).
func employeeRef(r *http.Request, tail string) (string, bool) {
	rest, found := strings.CutPrefix(r.URL.EscapedPath(), Prefix+employeesSegment)
	if !found {
		return "", false
	}
	seg, found := strings.CutSuffix(rest, tail)
	if !found || strings.Contains(seg, "/") {
		return "", false
	}
	ref, err := url.PathUnescape(seg)
	if err != nil || !tenant.ValidAPIExternalRef(ref) {
		return "", false
	}
	return ref, true
}

// employeeBody is the answer -- no address.
type employeeBody struct {
	ID              string              `json:"id"`
	ExternalRef     *string             `json:"externalRef"`
	EmployeeCode    *string             `json:"employeeCode"`
	FirstName       *string             `json:"firstName"`
	LastName        *string             `json:"lastName"`
	FullName        string              `json:"fullName"`
	Active          bool                `json:"active"`
	Activated       bool                `json:"activated"`
	ActivatedAt     *time.Time          `json:"activatedAt"`
	HasEmail        bool                `json:"hasEmail"`
	ActivationEmail activationEmailBody `json:"activationEmail"`
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
}

type activationEmailBody struct {
	Status string     `json:"status"`
	At     *time.Time `json:"at"`
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func employeeOf(e tenant.APIEmployee) employeeBody {
	return employeeBody{
		ID: employeeIDPrefix + e.ID.String(), ExternalRef: e.ExternalRef, EmployeeCode: e.EmployeeCode,
		FirstName: e.FirstName, LastName: e.LastName, FullName: e.FullName,
		Active: e.Active(), Activated: e.ActivatedAt != nil, ActivatedAt: utc(e.ActivatedAt), HasEmail: e.HasEmail,
		ActivationEmail: activationEmailBody{Status: string(e.Invitation), At: utc(e.InvitationAt)},
		CreatedAt:       e.CreatedAt.UTC(), UpdatedAt: e.UpdatedAt.UTC(),
	}
}

type employeePage struct {
	Data       []employeeBody `json:"data"`
	NextCursor *string        `json:"nextCursor"`
}

// employeeProblem turns a domain refusal into the API's answer; ok=false for an error that is
// not a refusal (an outage).
func employeeProblem(err error) (*Problem, bool) {
	var fe *tenant.EmployeeFieldError
	var over *tenant.DeactivationBudgetExceeded
	switch {
	case errors.As(err, &over):
		return budgetExceeded(over.RetryAfter), true
	case errors.Is(err, tenant.ErrEmailTaken):
		return problemEmailUnavailable, true
	case errors.Is(err, tenant.ErrAlreadyActivated):
		return problemAlreadyActivated, true
	case errors.Is(err, tenant.ErrNoAddressToInvite):
		return problemNoAddressToInvite, true
	case errors.Is(err, tenant.ErrUnknownEmployee):
		return problemEmployeeNotFound, true
	case errors.As(err, &fe):
		hidden := errors.Is(err, tenant.ErrNameHiddenCharacter)
		switch fe.Field {
		case tenant.FieldFirstName:
			return nameProblem(fieldFirstName, hidden), true
		case tenant.FieldLastName:
			return nameProblem(fieldLastName, hidden), true
		case tenant.FieldEmployeeCode:
			if hidden {
				return problemCodeHidden, true
			}
			return problemCodeInvalid, true
		case tenant.FieldEmail:
			if errors.Is(err, tenant.ErrEmployeeEmailIsAdministrators) {
				return problemEmailAdministrator, true
			}
			return problemEmailInvalid, true
		case tenant.FieldExternalRef:
			return problemEmployeeRef, true
		}
	}
	return nil, false
}

// classOf maps the domain's answer onto §10's class.
func classOf(w tenant.EmployeeWrite) WriteClass {
	if w == tenant.EmployeeOpens {
		return WriteEmployeeCreate
	}
	return WriteEmployeeChange
}

// employeeWrite runs one employee write through the runner, maps its refusals and answers.
func (s *Surface) employeeWrite(w http.ResponseWriter, r *http.Request, body []byte, status func(tenant.EmployeePutResult) int,
	run func(ctx context.Context, tx pgx.Tx, g *Gate, p apikey.Principal) (tenant.EmployeePutResult, error)) {
	p, _ := PrincipalOf(r)
	var done tenant.EmployeePutResult
	var over *tenant.DeactivationBudgetExceeded
	out, replayed, ok := s.write(w, r, body, func(ctx context.Context, tx pgx.Tx, g *Gate) (apikey.Outcome, error) {
		res, err := run(ctx, tx, g, p)
		if err != nil {
			if prob, refusal := employeeProblem(err); refusal {
				errors.As(err, &over)
				return apikey.Outcome{}, prob
			}
			return apikey.Outcome{}, err
		}
		done = res
		id := res.Employee.ID
		return apikey.Outcome{Status: status(res), Ref: &id}, nil
	})
	if !ok {
		if over != nil {
			// The owner's alert, in a transaction of its own: the request's was rolled back.
			if err := s.employees.RecordDeactivationBudgetExceeded(context.WithoutCancel(r.Context()), p.TenantID, p.KeyID, p.Attribution().Via, s.budget); err != nil {
				s.log.ErrorContext(r.Context(), "api: the deactivation budget alert was not written", "tenant_id", p.TenantID, "key_id", p.KeyID, "err", err)
			}
		}
		return
	}
	emp := done.Employee
	if replayed {
		if out.Ref == nil {
			s.unavailable(w, r, "a replayed employee write carries no employee id", errors.New("nil result_ref"))
			return
		}
		e, err := s.employees.APIEmployeeByID(r.Context(), p.TenantID, *out.Ref)
		switch {
		case errors.Is(err, tenant.ErrUnknownEmployee):
			writeProblem(w, problemEmployeeGone)
			return
		case err != nil:
			s.unavailable(w, r, "reading a replayed employee failed", err)
			return
		}
		emp = e
	} else if done.Queued {
		s.work.Notice(p.TenantID)
	}
	writeJSON(w, out.Status, employeeOf(emp))
}

func (s *Surface) putEmployee(w http.ResponseWriter, r *http.Request) {
	ref, ok := employeeRef(r, "")
	if !ok {
		writeProblem(w, problemEmployeeRef)
		return
	}
	body, prob := readBody(r)
	if prob != nil {
		writeProblem(w, prob)
		return
	}
	in, prob := parseEmployeeBody(body, ref)
	if prob != nil {
		writeProblem(w, prob)
		return
	}
	s.employeeWrite(w, r, body, func(res tenant.EmployeePutResult) int {
		if res.Created {
			return http.StatusCreated
		}
		return http.StatusOK
	}, func(ctx context.Context, tx pgx.Tx, g *Gate, p apikey.Principal) (tenant.EmployeePutResult, error) {
		return s.employees.PutEmployeeTx(ctx, tx, tenant.EmployeePut{
			TenantID: p.TenantID, ActorID: *p.Actor(), Via: p.Attribution().Via, ExternalRef: ref,
			FirstName: in.first, LastName: in.last, EmployeeCode: in.code, Email: in.email, Active: in.active,
			Budget: s.budget,
		}, func(ctx context.Context, wr tenant.EmployeeWrite) error { return g.Admit(ctx, classOf(wr)) })
	})
}

func (s *Surface) deleteEmployee(w http.ResponseWriter, r *http.Request) {
	ref, ok := employeeRef(r, "")
	if !ok {
		writeProblem(w, problemEmployeeRef)
		return
	}
	body, prob := readBody(r)
	if prob != nil {
		writeProblem(w, prob)
		return
	}
	s.employeeWrite(w, r, body, func(tenant.EmployeePutResult) int { return http.StatusOK },
		func(ctx context.Context, tx pgx.Tx, g *Gate, p apikey.Principal) (tenant.EmployeePutResult, error) {
			if err := g.Admit(ctx, WriteEmployeeDelete); err != nil {
				return tenant.EmployeePutResult{}, err
			}
			return s.employees.DeleteEmployeeTx(ctx, tx, tenant.EmployeeDelete{
				TenantID: p.TenantID, ActorID: *p.Actor(), Via: p.Attribution().Via, ExternalRef: ref, Budget: s.budget,
			})
		})
}

func (s *Surface) resendActivation(w http.ResponseWriter, r *http.Request) {
	ref, ok := employeeRef(r, resendTail)
	if !ok {
		writeProblem(w, problemEmployeeRef)
		return
	}
	body, prob := readBody(r)
	if prob != nil {
		writeProblem(w, prob)
		return
	}
	s.employeeWrite(w, r, body, func(tenant.EmployeePutResult) int { return http.StatusAccepted },
		func(ctx context.Context, tx pgx.Tx, g *Gate, p apikey.Principal) (tenant.EmployeePutResult, error) {
			if err := g.Admit(ctx, WriteResendActivation); err != nil {
				return tenant.EmployeePutResult{}, err
			}
			return s.employees.ResendActivationTx(ctx, tx, tenant.EmployeeResend{
				TenantID: p.TenantID, ActorID: *p.Actor(), Via: p.Attribution().Via, ExternalRef: ref,
			})
		})
}

func (s *Surface) getEmployee(w http.ResponseWriter, r *http.Request) {
	ref, ok := employeeRef(r, "")
	if !ok {
		writeProblem(w, problemEmployeeRef)
		return
	}
	p, _ := PrincipalOf(r)
	e, err := s.employees.APIEmployeeByRef(r.Context(), p.TenantID, ref)
	if err != nil {
		if prob, refusal := employeeProblem(err); refusal {
			writeProblem(w, prob)
			return
		}
		s.unavailable(w, r, "reading an employee failed", err)
		return
	}
	writeJSON(w, http.StatusOK, employeeOf(e))
}

func (s *Surface) listEmployees(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for k, v := range q {
		if (k != "limit" && k != fieldCursor) || len(v) != 1 {
			writeProblem(w, problemQueryParameter)
			return
		}
	}
	limit, prob := parseLimit(q)
	if prob != nil {
		writeProblem(w, prob)
		return
	}
	var after *uuid.UUID
	if c, present := q[fieldCursor]; present {
		id, err := uuid.Parse(strings.TrimPrefix(c[0], employeeIDPrefix))
		if err != nil || !strings.HasPrefix(c[0], employeeIDPrefix) {
			writeProblem(w, problemCursor)
			return
		}
		after = &id
	}
	p, _ := PrincipalOf(r)
	rows, err := s.employees.ListAPIEmployees(r.Context(), p.TenantID, after, limit)
	if err != nil {
		s.unavailable(w, r, "listing employees failed", err)
		return
	}
	page := employeePage{Data: make([]employeeBody, 0, len(rows))}
	for _, e := range rows {
		page.Data = append(page.Data, employeeOf(e))
	}
	if len(rows) == limit {
		next := employeeIDPrefix + rows[len(rows)-1].ID.String()
		page.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, page)
}
