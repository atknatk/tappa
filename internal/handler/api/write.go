package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/apikey"
)

// THE REQUEST BODY (ADR 0028 §8: "İstek gövdesi ≤ 64 KiB"). The bound is on the bytes that arrive,
// before anything is decoded: a declared Content-Length over it is refused without reading, a body
// that streams past it (chunked, or a lying Content-Length) is cut by http.MaxBytesReader at
// MaxBody+1, and a Content-Encoding other than identity is refused outright -- the API never
// inflates a body, so a 64 KiB gzip cannot become megabytes here (TestBody_The64KiBBound).

// MaxBody is the largest request body /v1 accepts: 64 KiB.
const MaxBody = 64 << 10

func (s *Surface) bodyBound(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, enc := range r.Header.Values("Content-Encoding") {
			if !strings.EqualFold(strings.TrimSpace(enc), "identity") {
				writeProblem(w, problemEncoding)
				return
			}
		}
		if r.ContentLength > MaxBody {
			writeProblem(w, problemBodyTooLarge)
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
		}
		next.ServeHTTP(w, r)
	})
}

// readBody reads the whole (bounded) body. Over the bound is problemBodyTooLarge; any other read
// failure is the client's broken stream, answered 400.
func readBody(r *http.Request) ([]byte, *Problem) {
	if r.Body == nil {
		return nil, nil
	}
	b, err := io.ReadAll(r.Body)
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return nil, problemBodyTooLarge
	case err != nil:
		return nil, &Problem{Code: CodeInvalidRequest, Message: "The request body could not be read."}
	}
	return b, nil
}

// THE WRITE (ADR 0028 §8, §10). Every /v1 write -- API-5's and API-6's endpoints -- is one call of
// Surface.write: the Idempotency-Key header (optional; one header of 1-255 visible ASCII), the
// request's hash, the runner (apikey.Writes: one transaction in the key's tenant context, the claim,
// the lazy delete) and the suspension checkpoint, which the work must pass.

// Work is a write's own body, in the runner's transaction. It calls gate.Admit with its §4.7 class
// before it writes; a *Problem it returns is answered as it is (a 409, a 422 of its own), anything
// else is 503.
type Work func(ctx context.Context, tx pgx.Tx, gate *Gate) (apikey.Outcome, error)

// errNoCheckpoint: a write's work returned without passing the checkpoint. Its transaction is
// rolled back -- a write that never asked whether the business is suspended does not commit.
var errNoCheckpoint = errors.New("api: a write returned without passing the suspension checkpoint")

// write runs one write and reports its outcome, whether it was a replay, and ok=false when it has
// already answered the request (a refusal). The caller renders the outcome on ok.
func (s *Surface) write(w http.ResponseWriter, r *http.Request, body []byte, work Work) (out apikey.Outcome, replayed, ok bool) {
	p, authed := PrincipalOf(r)
	if !authed {
		s.unavailable(w, r, "a write reached the runner without a resolved key", errors.New("no principal"))
		return apikey.Outcome{}, false, false
	}
	idem, prob := idempotencyKey(r)
	if prob != nil {
		writeProblem(w, prob)
		return apikey.Outcome{}, false, false
	}
	req := apikey.WriteRequest{Principal: p, IdempotencyKey: idem}
	if idem != "" {
		req.RequestHash = requestHash(r, body)
	}
	out, replayed, err := s.writes.Run(r.Context(), req, func(ctx context.Context, tx pgx.Tx) (apikey.Outcome, error) {
		g := &Gate{suspension: s.suspension, tx: tx, tenant: p.TenantID}
		o, err := work(ctx, tx, g)
		if err != nil {
			return apikey.Outcome{}, err
		}
		if !g.admitted {
			return apikey.Outcome{}, errNoCheckpoint
		}
		return o, nil
	})
	var problem *Problem
	switch {
	case err == nil:
	case errors.As(err, &problem):
		writeProblem(w, problem)
		return apikey.Outcome{}, false, false
	case errors.Is(err, apikey.ErrIdempotencyMismatch):
		writeProblem(w, problemIdempotencyReused)
		return apikey.Outcome{}, false, false
	case errors.Is(err, apikey.ErrIdempotencyKeyShape):
		writeProblem(w, problemIdempotencyKey)
		return apikey.Outcome{}, false, false
	default:
		s.unavailable(w, r, "a write failed", err)
		return apikey.Outcome{}, false, false
	}
	if replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	return out, replayed, true
}

// idempotencyKeyHeader is the request header (ADR 0028 §8).
const idempotencyKeyHeader = "Idempotency-Key"

// idempotencyKey reads the header: absent is "", present it is exactly one value of the shape.
func idempotencyKey(r *http.Request) (string, *Problem) {
	vals := r.Header.Values(idempotencyKeyHeader)
	switch {
	case len(vals) == 0:
		return "", nil
	case len(vals) > 1 || !apikey.ValidIdempotencyKey(vals[0]):
		return "", problemIdempotencyKey
	}
	return vals[0], nil
}

// requestHash identifies a request for the Idempotency-Key ledger: the SHA-256 of a domain label,
// the method, the escaped path, the query and the body, separated by bytes none of the first four
// can hold. The same key with ANY of them different is 422. It is not secret (the tenant sent all of
// it) and it is kept 24 hours.
func requestHash(r *http.Request, body []byte) string {
	h := sha256.New()
	h.Write([]byte("tappa/v1-request|"))
	h.Write([]byte(r.Method))
	h.Write([]byte{0})
	h.Write([]byte(r.URL.EscapedPath()))
	h.Write([]byte{0})
	h.Write([]byte(r.URL.RawQuery))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// THE QUERY STRING of a read (API-7's rule, which applies API-5's body rule to the query): every
// GET under /v1 -- the three lists, the single employee and the three documents -- reads its query
// through readQuery. A query string that does not parse (`%zz`, a `;` separator, a broken escape in
// a name), a name the endpoint does not take and a name given twice are each `refusal`: never a
// parameter silently dropped. r.URL.Query() is not used, because it drops what does not parse and
// answers the rest -- `?limit=1;x=1` would read as no limit at all, and `?curs%zzor=` as no cursor,
// restarting a reader at the beginning (API-12 fix, security finding 1).
func readQuery(r *http.Request, allowed []string, refusal *Problem) (url.Values, *Problem) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, refusal
	}
	for k, v := range q {
		if !slices.Contains(allowed, k) || len(v) != 1 {
			return nil, refusal
		}
	}
	return q, nil
}

// problemNoQuery refuses any query parameter on a GET that takes none (one employee, the documents).
var problemNoQuery = &Problem{Code: CodeInvalidRequest,
	Message: "This endpoint takes no query parameters.", Details: map[string]any{"allowed": []string{}}}

// THE PAGE SIZE (ADR 0028 §8: "limit ≤ 1000, varsayılan 500; 1001 → 400"). API-7 and API-8 read it.
const (
	DefaultLimit = 500
	MaxLimit     = 1000
)

// parseLimit reads ?limit=: absent is DefaultLimit; present it is ONE plain decimal from 1 to
// MaxLimit -- no sign, no blank, no second value -- or problemLimit.
func parseLimit(q url.Values) (int, *Problem) {
	vals, present := q["limit"]
	if !present {
		return DefaultLimit, nil
	}
	if len(vals) != 1 || len(vals[0]) == 0 || len(vals[0]) > 4 {
		return 0, problemLimit
	}
	for i := 0; i < len(vals[0]); i++ {
		if vals[0][i] < '0' || vals[0][i] > '9' {
			return 0, problemLimit
		}
	}
	n, err := strconv.Atoi(vals[0])
	if err != nil || n < 1 || n > MaxLimit {
		return 0, problemLimit
	}
	return n, nil
}
