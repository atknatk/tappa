package operatorauth

import "sync/atomic"

// CountWork wraps a's comparer and digest maker -- the PRODUCTION functions, not
// stand-ins -- with counters and returns their readers: bcrypt comparisons and bcrypt
// digests made. It exists for surface_external_test.go (M10 OP-8), which drives
// internal/handler/operator's real handlers and must count what each request paid;
// compareFn and digestFn are unexported, and this is a _test.go file, which the go tool
// compiles into this package's test build and not into the product.
//
// Call it before the Authenticator serves a request: the swap is not synchronised with
// a request in flight.
func CountWork(a *Authenticator) (comparisons, digests func() int64) {
	var c, d atomic.Int64
	cmp, dig := a.compareFn, a.digestFn
	a.compareFn = func(h, p []byte) error { c.Add(1); return cmp(h, p) }
	a.digestFn = func(p string) (string, error) { d.Add(1); return dig(p) }
	return c.Load, d.Load
}

// HoldComparisons runs before ahead of each of a's comparisons; the production comparer
// still compares. It exists for surface_external_test.go's aborted-request test (OP-14
// phase C, 2nd round): the client hangs up WHILE the comparison runs, and before is how
// that test learns the comparison has started and holds it until net/http has cancelled
// the request's context. Call it before the Authenticator serves a request.
func HoldComparisons(a *Authenticator, before func()) {
	cmp := a.compareFn
	a.compareFn = func(h, p []byte) error { before(); return cmp(h, p) }
}
