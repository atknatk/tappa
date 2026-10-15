package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// THE ERROR CONTRACT (ADR 0028 §8): every refusal on /v1 is a JSON body {code, message, details}
// with one of §8's statuses -- 400, 401, 403, 404, 409, 422, 429, 503 -- and 429 carries
// Retry-After. The body is built from SERVER constants only: no message repeats the request (a key,
// a header, a body field), and none names what is inside -- no SQL, no tenant id, no stack. A
// failure this package does not recognise is 503 "unavailable" and the cause goes to the process
// log, never to the response.
//
// The code set is closed and is the openapi.yaml Error schema's enum, both ways
// (TestOpenAPI_PathsAreTheRoutersAndTheErrorIsSection8). API-5..8 add their codes HERE and THERE.

// Code is an error body's machine-readable code.
type Code string

// The codes, each with its one status (statusOf).
const (
	CodeInvalidRequest       Code = "invalid_request"        // 400: a parameter or header the contract refuses
	CodeBodyTooLarge         Code = "body_too_large"         // 400: a body over MaxBody bytes
	CodeUnauthorized         Code = "unauthorized"           // 401: no key, a key of another shape, unknown or revoked
	CodeAPIDisabled          Code = "api_disabled"           // 403: the operator has not opened this business's API (K-22)
	CodeTenantSuspended      Code = "tenant_suspended"       // 403: a write the suspension closes (ADR 0028 §10)
	CodeNotFound             Code = "not_found"              // 404: no such route or resource (and an unsupported method)
	CodeIdempotencyKeyReused Code = "idempotency_key_reused" // 422: the Idempotency-Key was used with another request
	CodeRateLimited          Code = "rate_limited"           // 429: a bucket is empty; Retry-After says when to come back
	CodeUnavailable          Code = "unavailable"            // 503: anything on our side; the cause is in our log only
)

// Codes is the closed set, in a stable order (the OpenAPI enum's).
func Codes() []Code {
	return []Code{CodeInvalidRequest, CodeBodyTooLarge, CodeUnauthorized, CodeAPIDisabled, CodeTenantSuspended,
		CodeNotFound, CodeIdempotencyKeyReused, CodeRateLimited, CodeUnavailable}
}

// Section8Statuses are the statuses ADR 0028 §8 allows an error body to carry.
func Section8Statuses() []int {
	return []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusServiceUnavailable}
}

// StatusOf is a code's status. A code outside the set is 503: the closed set is the contract.
func StatusOf(c Code) int {
	switch c {
	case CodeInvalidRequest, CodeBodyTooLarge:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeAPIDisabled, CodeTenantSuspended:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeIdempotencyKeyReused:
		return http.StatusUnprocessableEntity
	case CodeRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusServiceUnavailable
	}
}

// Problem is one refusal: a code, a fixed message and fixed details. It is an error, so a write's
// work (API-5, API-6) can return one and the surface answers with it.
type Problem struct {
	Code    Code
	Message string
	// Details is built by the server from constants (a parameter's name, a bound); never input.
	Details map[string]any
	// retryAfter is 429's Retry-After.
	retryAfter time.Duration
}

func (p *Problem) Error() string { return "api: " + string(p.Code) }

// The surface's own refusals.
var (
	problemUnauthorized = &Problem{Code: CodeUnauthorized,
		Message: "A valid integration key is required: Authorization: Bearer <key>."}
	problemAPIDisabled = &Problem{Code: CodeAPIDisabled,
		Message: "The integration API is not enabled for this business. Ask Taptime to enable it."}
	problemTenantSuspended = &Problem{Code: CodeTenantSuspended,
		Message: "This business is suspended; this change is not accepted until it is reinstated."}
	problemNotFound = &Problem{Code: CodeNotFound,
		Message: "There is no such resource."}
	problemBodyTooLarge = &Problem{Code: CodeBodyTooLarge,
		Message: "The request body is larger than the API accepts.", Details: map[string]any{"maxBytes": MaxBody}}
	problemEncoding = &Problem{Code: CodeInvalidRequest,
		Message: "A compressed or otherwise encoded request body is not accepted.", Details: map[string]any{"header": "Content-Encoding"}}
	problemIdempotencyKey = &Problem{Code: CodeInvalidRequest,
		Message: "Idempotency-Key must be one header of 1 to 255 visible ASCII characters.", Details: map[string]any{"header": "Idempotency-Key"}}
	problemIdempotencyReused = &Problem{Code: CodeIdempotencyKeyReused,
		Message: "This Idempotency-Key was used in the last 24 hours with a different request."}
	problemUnavailable = &Problem{Code: CodeUnavailable,
		Message: "The request could not be completed on our side. Try again shortly."}
	problemLimit = &Problem{Code: CodeInvalidRequest,
		Message: "limit must be a whole number from 1 to 1000.", Details: map[string]any{"parameter": "limit", "max": MaxLimit}}
)

// rateLimited is 429 with when to retry, in seconds (Retry-After and details agree).
func rateLimited(after time.Duration) *Problem {
	secs := max(int(after.Seconds()), 1)
	return &Problem{Code: CodeRateLimited, Message: "Too many requests. Retry after the time in Retry-After.",
		Details: map[string]any{"retryAfterSeconds": secs}, retryAfter: time.Duration(secs) * time.Second}
}

// errorBody is the wire shape. details is always an object, never null.
type errorBody struct {
	Code    Code           `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// writeProblem answers with p.
func writeProblem(w http.ResponseWriter, p *Problem) {
	status := StatusOf(p.Code)
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	if status == http.StatusUnauthorized {
		h.Set("WWW-Authenticate", `Bearer realm="taptime"`)
	}
	if status == http.StatusTooManyRequests {
		h.Set("Retry-After", strconv.Itoa(max(int(p.retryAfter.Seconds()), 1)))
	}
	details := p.Details
	if details == nil {
		details = map[string]any{}
	}
	b, err := json.Marshal(errorBody{Code: p.Code, Message: p.Message, Details: details})
	if err != nil {
		// Unreachable: every value above is a string, an int or a map of those.
		b = []byte(`{"code":"unavailable","message":"","details":{}}`)
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}
