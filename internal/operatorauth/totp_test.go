package operatorauth

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"go/ast"
	"go/parser"
	"go/token"
	"hash"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The RFC seeds below are the ASCII strings the RFCs themselves print. They are
// public test vectors, not credentials; the SHA-256 and SHA-512 seeds are the 32- and
// 64-byte ones RFC 6238's errata gives (the reference code's shorter seeds do not
// reproduce the published table).
const (
	rfcSeed20 = "12345678901234567890"
	rfcSeed32 = "12345678901234567890123456789012"
	rfcSeed64 = "1234567890123456789012345678901234567890123456789012345678901234"
)

// TestHOTP_RFC4226AppendixD is the six-digit HOTP table of RFC 4226 Appendix D,
// counters 0-9, HMAC-SHA1, the 20-byte ASCII seed. Six digits is the product's length,
// so this table pins the modulo and the zero-padding at the width that ships.
func TestHOTP_RFC4226AppendixD(t *testing.T) {
	want := []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"}
	for i, w := range want {
		var got [6]byte
		hotp(sha1New, []byte(rfcSeed20), uint64(i), got[:])
		if string(got[:]) != w {
			t.Errorf("HOTP(counter=%d) = %s, RFC 4226 Appendix D says %s", i, got, w)
		}
	}
}

// TestTOTP_RFC6238AppendixB is all eighteen rows of RFC 6238 Appendix B: six times,
// three hashes, eight digits -- and the step T the RFC prints for each time, so the
// time-to-step arithmetic is pinned on its own and not only through the code.
func TestTOTP_RFC6238AppendixB(t *testing.T) {
	type row struct {
		unix             int64
		stepHex          string
		sha1, s256, s512 string
	}
	rows := []row{
		{59, "0000000000000001", "94287082", "46119246", "90693936"},
		{1111111109, "00000000023523EC", "07081804", "68084774", "25091201"},
		{1111111111, "00000000023523ED", "14050471", "67062674", "99943326"},
		{1234567890, "000000000273EF07", "89005924", "91819424", "93441116"},
		{2000000000, "0000000003F940AA", "69279037", "90698825", "38618901"},
		{20000000000, "0000000027BC86AA", "65353130", "77737706", "47863826"},
	}
	for _, r := range rows {
		at := time.Unix(r.unix, 0).UTC()
		s := step(at)
		if got := strings.ToUpper(leftPad16(strconv.FormatUint(uint64(s), 16))); got != r.stepHex {
			t.Errorf("T(%d) = %s, RFC 6238 says %s", r.unix, got, r.stepHex)
		}
		for _, c := range []struct {
			name string
			h    func() hash.Hash
			seed string
			want string
		}{
			{"SHA1", sha1New, rfcSeed20, r.sha1},
			{"SHA256", sha256.New, rfcSeed32, r.s256},
			{"SHA512", sha512.New, rfcSeed64, r.s512},
		} {
			var got [8]byte
			hotp(c.h, []byte(c.seed), uint64(s), got[:])
			if string(got[:]) != c.want {
				t.Errorf("TOTP %s at %d = %s, RFC 6238 Appendix B says %s", c.name, r.unix, got, c.want)
			}
		}
	}
}

func leftPad16(s string) string { return strings.Repeat("0", 16-len(s)) + s }

// TestVerifyCode_TheWindowIsExactlyOneStepEachSide is ADR 0020 §3's ±1 step, with the
// clock injected: the code of cur-1, cur and cur+1 is accepted AND reported with ITS
// OWN step (that step is what the database stores); cur-2 and cur+2 are refused. The
// seed is RFC 4226's, so the codes are the published ones and not this function's own
// output.
func TestVerifyCode_TheWindowIsExactlyOneStepEachSide(t *testing.T) {
	// ADR 0020 §3's numbers, as LITERALS: an expectation computed from the constants
	// would move with them (a first version did exactly that, and a mutation widening
	// the window to ±2 stayed green).
	if Digits != 6 || PeriodSeconds != 30 || SkewSteps != 1 {
		t.Fatalf("TOTP parameters %d digits / %d s / ±%d steps; ADR 0020 §3 says 6 / 30 / ±1", Digits, PeriodSeconds, SkewSteps)
	}
	key := []byte(rfcSeed20)
	now := time.Unix(1111111111, 0) // mid-step 37037037 (RFC 6238's third row)
	cur := step(now)
	for d := int64(-3); d <= 3; d++ {
		var c [6]byte
		hotp(sha1New, key, uint64(cur+d), c[:])
		got, ok := verifyCode(key, string(c[:]), now)
		inside := d >= -1 && d <= 1
		switch {
		case inside && (!ok || got != cur+d):
			t.Errorf("the code of cur%+d: accepted=%v step=%d, want accepted with step %d", d, ok, got, cur+d)
		case !inside && ok:
			t.Errorf("the code of cur%+d was accepted (step %d); the window is ±1", d, got)
		}
	}
	// The same, one second either side of a step boundary: the window moves with the
	// clock, it is not "the step of the minute".
	boundary := time.Unix((cur+1)*PeriodSeconds, 0)
	for _, at := range []time.Time{boundary.Add(-time.Second), boundary} {
		var c [Digits]byte
		hotp(sha1New, key, uint64(step(at)), c[:])
		if s, ok := verifyCode(key, string(c[:]), at); !ok || s != step(at) {
			t.Errorf("at %d the current code: accepted=%v step=%d", at.Unix(), ok, s)
		}
	}
}

// TestVerifyCode_ALaterMatchingStepWins: if one code is valid for two steps of the
// window, the LATER step is reported, so the database stores the larger one and both
// are retired. Built by searching the RFC seed for a real collision rather than by
// asserting on an assumption.
func TestVerifyCode_ALaterMatchingStepWins(t *testing.T) {
	key := []byte(rfcSeed20)
	codeOf := func(s int64) string {
		var c [Digits]byte
		hotp(sha1New, key, uint64(s), c[:])
		return string(c[:])
	}
	// Find the first step whose code repeats one or two steps later: about two in a
	// million per step, so a sliding search (one HMAC per step) finds one within a few
	// million steps. The seed is fixed, so the step found is always the same one.
	prev2, prev1 := codeOf(0), codeOf(1)
	for s := int64(2); s < 8_000_000; s++ {
		c := codeOf(s)
		var first int64 = -1
		switch {
		case c == prev1:
			first = s - 1
		case c == prev2:
			first = s - 2
		}
		if first >= 0 {
			// A clock whose window [cur-1 .. cur+1] holds both steps.
			cur := s - 1
			if s-first == 1 {
				cur = s
			}
			got, ok := verifyCode(key, c, time.Unix(cur*PeriodSeconds+1, 0))
			if !ok || got != s {
				t.Fatalf("a code valid at steps %d and %d was reported as step %d (ok=%v); want the later one", first, s, got, ok)
			}
			return
		}
		prev2, prev1 = prev1, c
	}
	t.Skip("no collision found in the search range; the property is argued in verifyCode's comment")
}

// TestNormalizeCode: six ASCII digits, spaces allowed anywhere, nothing else.
func TestNormalizeCode(t *testing.T) {
	for in, want := range map[string]bool{
		"123456": true, "123 456": true, " 12 34 56 ": true,
		"12345": false, "1234567": false, "12345a": false, "１２３４５６": false,
		"": false, "123-456": false, "123456\n": false,
	} {
		if _, ok := normalizeCode(in); ok != want {
			t.Errorf("normalizeCode(%q) ok=%v, want %v", in, ok, want)
		}
	}
}

// TestVerifyCode_TheWindowIsWalkedWholeInConstantTime reads verifyCode's own source
// (constant time is not observable from a unit test): the window loop must contain
// exactly one subtle.ConstantTimeCompare and no statement that leaves it early --
// return, break, goto, or a labelled continue -- and every == or != in it must have that
// comparison's result as one operand.
//
// The last rule is the one that closes the shape a count cannot see (OP-6 verification,
// 2026-09-30, measured): `hit := want == in && subtle.ConstantTimeCompare(...) == 1`
// and `_ = subtle.ConstantTimeCompare(...); hit := want == in` both keep ONE
// constant-time call in the loop, and both stayed green under the earlier rules -- the
// decision then rests on an ordinary array comparison.
func TestVerifyCode_TheWindowIsWalkedWholeInConstantTime(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "totp.go", nil, 0)
	if err != nil {
		t.Fatalf("parse totp.go: %v", err)
	}
	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "verifyCode" {
			fn = fd
		}
	}
	if fn == nil {
		t.Fatal("verifyCode not found in totp.go")
	}
	loops, compares := 0, 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		loop, ok := n.(*ast.ForStmt)
		if !ok {
			return true
		}
		loops++
		ast.Inspect(loop.Body, func(m ast.Node) bool {
			switch x := m.(type) {
			case *ast.ReturnStmt:
				t.Errorf("%s: a return inside the window loop leaves it early", fset.Position(x.Pos()))
			case *ast.BranchStmt:
				if x.Tok == token.BREAK || x.Tok == token.GOTO || x.Label != nil {
					t.Errorf("%s: %s inside the window loop leaves it early", fset.Position(x.Pos()), x.Tok)
				}
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
					if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "subtle" && sel.Sel.Name == "ConstantTimeCompare" {
						compares++
					}
					if sel.Sel.Name == "Equal" || sel.Sel.Name == "Compare" {
						t.Errorf("%s: %s.%s in the window loop is not constant time", fset.Position(x.Pos()), sel.X, sel.Sel.Name)
					}
				}
			case *ast.BinaryExpr:
				if x.Op == token.EQL || x.Op == token.NEQ {
					if isStringish(x.X) || isStringish(x.Y) {
						t.Errorf("%s: a == / != on the code in the window loop is not constant time", fset.Position(x.Pos()))
					}
					if !isConstantTimeCompare(x.X) && !isConstantTimeCompare(x.Y) {
						t.Errorf("%s: a == / != in the window loop that does not test subtle.ConstantTimeCompare's result decides on an ordinary comparison", fset.Position(x.Pos()))
					}
				}
			}
			return true
		})
		return true
	})
	if loops != 1 || compares != 1 {
		t.Fatalf("verifyCode has %d loop(s) and %d constant-time comparison(s) in them, want 1 and 1", loops, compares)
	}
}

// isConstantTimeCompare reports whether e is a call of subtle.ConstantTimeCompare.
func isConstantTimeCompare(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "subtle" && sel.Sel.Name == "ConstantTimeCompare"
}

// isStringish flags the conversions a hand-rolled equality would use.
func isStringish(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := c.Fun.(*ast.Ident)
	return ok && id.Name == "string"
}

// TestSecret_Is160BitsAndItsDisplayForms: 20 random bytes; base32 without padding (32
// characters) that decodes back to them; an otpauth:// URI an authenticator app
// parses with the same secret, SHA1, 6 digits, 30 seconds and the issuer; two secrets
// differ.
func TestSecret_Is160BitsAndItsDisplayForms(t *testing.T) {
	s1, err := NewSecret()
	if err != nil {
		t.Fatalf("NewSecret: %v", err)
	}
	s2, err := NewSecret()
	if err != nil {
		t.Fatalf("NewSecret: %v", err)
	}
	if len(s1.reveal()) != SecretBytes || SecretBytes*8 != 160 {
		t.Fatalf("a secret is %d bytes, want %d (160 bits)", len(s1.reveal()), SecretBytes)
	}
	if s1.Base32() == s2.Base32() {
		t.Fatal("two NewSecret calls produced the same secret")
	}
	b := s1.Base32()
	if len(b) != 32 || strings.Contains(b, "=") {
		t.Fatalf("base32 form %d characters, padding=%v; want 32 and none", len(b), strings.Contains(b, "="))
	}
	back, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(b)
	if err != nil || string(back) != string(s1.reveal()) {
		t.Fatalf("the base32 form does not decode back to the secret: %v", err)
	}
	u, err := url.Parse(s1.URI("Taptime Operator", "ops@example.test"))
	if err != nil {
		t.Fatalf("the URI does not parse: %v", err)
	}
	q := u.Query()
	if u.Scheme != "otpauth" || u.Host != "totp" || u.Path != "/Taptime Operator:ops@example.test" ||
		q.Get("secret") != b || q.Get("issuer") != "Taptime Operator" || q.Get("algorithm") != "SHA1" ||
		q.Get("digits") != "6" || q.Get("period") != "30" {
		t.Fatalf("URI fields: scheme=%s host=%s path=%q issuer=%q algorithm=%s digits=%s period=%s secret-matches=%v",
			u.Scheme, u.Host, u.Path, q.Get("issuer"), q.Get("algorithm"), q.Get("digits"), q.Get("period"), q.Get("secret") == b)
	}
	if strings.Contains(u.RawQuery, "+") {
		t.Fatalf("a space was encoded as '+', which not every authenticator decodes")
	}
	cp := s1
	s1.Zero()
	if s1.reveal() != nil || cp.reveal() != nil || s1.Base32() != "" {
		t.Fatal("Zero left the secret revealable (through the value or a copy of it)")
	}
}

// TestStep_FloorsBeforeTheEpoch: T is a floor, not a truncation.
func TestStep_FloorsBeforeTheEpoch(t *testing.T) {
	for unix, want := range map[int64]int64{0: 0, 29: 0, 30: 1, -1: -1, -30: -1, -31: -2} {
		if got := step(time.Unix(unix, 0)); got != want {
			t.Errorf("step(%d) = %d, want %d", unix, got, want)
		}
	}
}
