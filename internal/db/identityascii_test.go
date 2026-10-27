package db

// identityascii_test.go -- M14-08a (migration 00076; ADR 0031 §5.1's address rule, the user's
// decision of 2026-10-26): every stored address that can birth or key an identity passes
// mail.ValidRecipient. citext folds U+0130 and U+212A onto ASCII, so a homoglyph registered first
// would squat the identity of the ASCII spelling -- whose owner could never get a reset, because mail
// goes to printable-ASCII recipients alone. identity_create_for_signup now decides the rule before
// it locks or writes (its 6b), and its e-mail-wide read no longer counts a row that is not printable
// ASCII. The worlds are written at the top level of the test's transaction, every call in a
// savepoint over them (identitysignup_test.go's S13 note).

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/mail"
)

const ia00076File = "00076_create_identities_only_for_sendable_addresses.sql"

// identityAddressRuleMarker opens 6b in the body; identityAddressRule reads the predicate after it.
const identityAddressRuleMarker = "-- 6b. THE ADDRESS RULE (M14-08a)"

// identityAddressPattern is the regex 00076 writes out, built here from its parts so a reader can
// check the parts rather than 2 700 characters. It is net/mail's addr-spec as ParseAddress hands it
// back unchanged, restricted to printable ASCII:
//
//	atext       RFC 5322 atext: printable ASCII but the specials ()<>[]:;@\,". and the dot
//	dot-atom    atext+ ("." atext+)*  -- no leading, trailing or double dot (consumeAtom)
//	address     dot-atom "@" ( dot-atom | "[" ip "]" )  -- a quoted local part never comes back as
//	            itself (net/mail unquotes it), so it is not in the rule
//	ip          what net.ParseIP accepts (netip.ParseAddr, no zone): four decimal octets 0-255 with
//	            no leading zero, or IPv6 -- up to eight 1-4 digit hex groups, "::" standing for at
//	            least one group, and an IPv4 tail counting as two groups
//
// The 254-octet bound and the "=?" refusal are the predicate's two other conjuncts.
func identityAddressPattern() string {
	const atext = "[-A-Za-z0-9!#$%&'*+/=?^_`{|}~]"
	dotAtom := atext + `+(\.` + atext + `+)*`
	octet := "(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])"
	v4 := strings.Join([]string{octet, octet, octet, octet}, `\.`)
	const h = "[0-9A-Fa-f]{1,4}"
	groups := func(n int) string { // exactly n colon-separated groups
		if n <= 2 {
			return strings.Repeat(h+":", n-1) + h
		}
		return fmt.Sprintf("(%s:){%d}%s", h, n-1, h)
	}
	upTo := func(n int) string { // 0..n colon-separated groups
		if n == 1 {
			return "(" + h + ")?"
		}
		return fmt.Sprintf("(%s(:%s){0,%d})?", h, h, n-1)
	}
	alts := []string{groups(8), "(" + h + ":){6}" + v4}
	for a := 0; a <= 7; a++ { // a groups left of "::", at most 7-a right of it
		left, right := "", ""
		if a > 0 {
			left = groups(a)
		}
		if a < 7 {
			right = upTo(7 - a)
		}
		alts = append(alts, left+"::"+right)
	}
	for a := 0; a <= 5; a++ { // a groups left of "::", at most 5-a groups, then the IPv4 tail
		left, right := "", ""
		if a > 0 {
			left = groups(a)
		}
		if a < 5 {
			right = fmt.Sprintf("(%s:){0,%d}", h, 5-a)
		}
		alts = append(alts, left+"::"+right+v4)
	}
	v6 := "(" + strings.Join(alts, "|") + ")"
	return "^" + dotAtom + "@(" + dotAtom + `|\[(` + v4 + "|" + v6 + `)\])$`
}

// identityAddressRule returns 6b's predicate as the live body of identity_create_for_signup writes
// it (the text between "IF (" and ") IS NOT TRUE THEN" after the marker), with v_email::text
// replaced by x -- an expression over one text column x.
func identityAddressRule(t *testing.T, ctx context.Context, q pgx.Tx) string {
	t.Helper()
	var def string
	if err := q.QueryRow(ctx, `SELECT pg_get_functiondef('public.identity_create_for_signup(uuid, uuid)'::regprocedure)`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	i := strings.Index(def, identityAddressRuleMarker)
	if i < 0 {
		t.Fatal("PREMISE: identity_create_for_signup carries no address rule (00076's 6b)")
	}
	rest := def[i:]
	a, b := strings.Index(rest, "IF ("), strings.Index(rest, ") IS NOT TRUE THEN")
	if a < 0 || b < a {
		t.Fatal("PREMISE: 6b is not an `IF (...) IS NOT TRUE THEN`")
	}
	pred := rest[a+len("IF (") : b]
	if strings.Count(pred, "v_email::text") != 3 {
		t.Fatalf("PREMISE: 6b reads v_email::text %d times, want 3 (length, \"=?\", pattern)", strings.Count(pred, "v_email::text"))
	}
	return strings.ReplaceAll(pred, "v_email::text", "x")
}

// identityAddressCorpus is the parity table: named inputs (the finding's two code points, the
// controls and every refusal ValidRecipient makes for its own reason) and a deterministic generated
// corpus of structured near-misses -- dot-atoms with specials and dots in the wrong places, and IP
// domain literals in every shape netip accepts or refuses.
func identityAddressCorpus() []string {
	named := []string{
		// The finding and its relatives.
		"ceo@cor\u0130p.com", "\u212aate@example.mt", "ceo@corip.com", "kate@example.mt",
		"jos\u00e9@example.mt", "maria@ex\u0430mple.mt", "a@b\u00a0c.mt", "a@b.c\u200b",
		// Punycode is ASCII and passes; its Unicode form does not.
		"ceo@xn--corp-ifa.com", "ceo@xn--80ak6aa92e.com", "ceo@\u043f\u0440\u0438\u043c\u0435\u0440.com",
		// Spaces and controls, inside and around.
		" a@b.c", "a@b.c ", "a @b.c", "a@ b.c", "a\t@b.c", "a@b.c\n", "a\r\n@b.c", "a\x7f@b.c", "a\x01@b.c", "\x1fa@b.c",
		// Shape.
		"", "@", "a@", "@b", "a@b", "a@b.c", "a.b.c@d.e.f", ".a@b", "a.@b", "a..b@c", "a@.b", "a@b.", "a@b..c",
		"a@b@c", `"a"@b.c`, `"a b"@c.d`, `a\b@c.d`, "a(b)@c.d", "a@b.c(d)", "a<b@c.d>", "<a@b.c>", "a,b@c.d", "a;b@c.d",
		"a:b@c.d", "Name <a@b.c>", "a@b.c, d@e.f", "a+tag@b.c", "o'neil@b.c", "a/b=c?d^e_f`g{h|i}j~k@l.m",
		"!#$%&'*+-/=?^_`{|}~@x.y", "a=?b@c.d", "=?utf-8?q?a?=@b.c", "a=@b.c", "a?@b.c", "a?=@b.c",
		// Domain literals.
		"a@[1.2.3.4]", "a@[255.255.255.255]", "a@[256.1.1.1]", "a@[01.2.3.4]", "a@[1.2.3]", "a@[1.2.3.4.5]",
		"a@[0.0.0.0]", "a@[::]", "a@[::1]", "a@[1::]", "a@[1:2:3:4:5:6:7:8]", "a@[1:2:3:4:5:6:7::]", "a@[::2:3:4:5:6:7:8]",
		"a@[1:2:3:4:5:6:7:8::]", "a@[::1:2:3:4:5:6:7:8]", "a@[1::2::3]", "a@[12345::1]", "a@[g::1]", "a@[::ffff:1.2.3.4]",
		"a@[1:2:3:4:5:6:1.2.3.4]", "a@[1:2:3:4:5::1.2.3.4]", "a@[1::2:3:4:5:6:1.2.3.4]", "a@[::1.2.3.4]", "a@[::a.2.3.4]",
		"a@[fe80::1%eth0]", "a@[FE80::ABCD]", "a@[1.2.3.4]x", "a@[]", "a@[", "a@]", "a@[a@b]", "a@[IPv6:::1]",
		// Length: 254 octets is the bound.
		strings.Repeat("a", 64) + "@" + strings.Repeat("b", 186) + ".mt",
		strings.Repeat("a", 64) + "@" + strings.Repeat("b", 187) + ".mt",
	}
	r := rand.New(rand.NewPCG(76, 8))
	pick := func(xs []string) string { return xs[r.IntN(len(xs))] }
	atomBits := []string{"a", "Z", "9", "-", "_", "+", "'", "=", "?", "~", "{", "}", "|", "/", "^", "`", "!", "#", "$", "%", "&", "*"}
	noise := []string{".", "..", "@", "\"", "(", ")", "[", "]", ":", ";", ",", "<", ">", "\\", " ", "\t", "\u0130", "\u212a", "\u00e9", "=?"}
	atom := func() string {
		var b strings.Builder
		for n := 1 + r.IntN(4); n > 0; n-- {
			b.WriteString(pick(atomBits))
		}
		return b.String()
	}
	dotAtom := func() string {
		parts := []string{atom()}
		for r.IntN(3) == 0 {
			parts = append(parts, atom())
		}
		s := strings.Join(parts, ".")
		if r.IntN(4) == 0 { // a near-miss: noise somewhere
			i := r.IntN(len(s) + 1)
			s = s[:i] + pick(noise) + s[i:]
		}
		return s
	}
	octets := []string{"0", "1", "9", "01", "00", "10", "99", "100", "199", "200", "249", "250", "255", "256", "300", "1000", "a", ""}
	hexes := []string{"0", "1", "a", "F", "ab", "fff", "FFFF", "12345", "g", "0000", "00000", ""}
	v4 := func() string {
		n := 4
		if r.IntN(6) == 0 {
			n = 3 + r.IntN(3)
		}
		f := make([]string, n)
		for i := range f {
			f[i] = pick(octets[:13])
			if r.IntN(10) == 0 {
				f[i] = pick(octets)
			}
		}
		return strings.Join(f, ".")
	}
	v6 := func() string {
		n := r.IntN(10)
		g := make([]string, n)
		for i := range g {
			g[i] = pick(hexes[:7])
			if r.IntN(12) == 0 {
				g[i] = pick(hexes)
			}
		}
		if n > 0 && r.IntN(3) == 0 {
			g[n-1] = v4()
		}
		s := strings.Join(g, ":")
		if r.IntN(4) != 0 { // a "::" somewhere, now and then two
			cuts := 1
			if r.IntN(8) == 0 {
				cuts = 2
			}
			for ; cuts > 0; cuts-- {
				i := strings.Index(s, ":")
				if i < 0 || r.IntN(3) == 0 {
					i = []int{0, len(s)}[r.IntN(2)]
				} else {
					// a random colon position
					idx := []int{}
					for j := 0; j < len(s); j++ {
						if s[j] == ':' {
							idx = append(idx, j)
						}
					}
					i = idx[r.IntN(len(idx))]
				}
				s = s[:i] + "::" + s[i:]
				s = strings.ReplaceAll(s, ":::", "::")
			}
		}
		if r.IntN(15) == 0 {
			s += "%eth0"
		}
		return s
	}
	out := append([]string(nil), named...)
	for range 6000 {
		out = append(out, dotAtom()+"@"+dotAtom())
	}
	for range 3000 {
		out = append(out, atom()+"@["+v4()+"]")
	}
	for range 9000 {
		out = append(out, atom()+"@["+v6()+"]")
	}
	return out
}

// TestIdentityAddressRule_SQLAgreesWithValidRecipient: 6b's predicate, read out of the LIVE body,
// answers every input of identityAddressCorpus exactly as mail.ValidRecipient does -- the rule the
// database applies is the rule Send applies, with no drift either way. The pattern in the body is
// identityAddressPattern byte for byte. A value PostgreSQL cannot hold as text (a NUL byte, invalid
// UTF-8) is ValidRecipient's refusal on the Go side alone. Anti-vacuity: both answers occur, and
// accepted IPv4 literals, IPv6 literals (with and without an IPv4 tail) and dot-atom addresses are
// each counted. MUTATION (measured): one IPv6 alternative dropped from the body's pattern, the
// leading-zero octet allowed, a special added to atext, or the "=?" conjunct removed -> red here.
func TestIdentityAddressRule_SQLAgreesWithValidRecipient(t *testing.T) {
	ctx, tx := opTx(t)
	pred := identityAddressRule(t, ctx, tx)
	wantLiteral := `x ~ '` + strings.ReplaceAll(identityAddressPattern(), "'", "''") + `'`
	if !strings.Contains(pred, wantLiteral) {
		t.Fatal("the body's pattern is not identityAddressPattern byte for byte")
	}
	if !strings.Contains(pred, "pg_catalog.octet_length(x) <= 254") || !strings.Contains(pred, "pg_catalog.strpos(x, '=?') = 0") {
		t.Fatalf("6b's other two conjuncts are not the length bound and the \"=?\" refusal: %q", pred[:min(len(pred), 200)])
	}
	iaCompare(t, ctx, tx, pred, append(identityAddressCorpus(), "a\x00@b.c", "a\xff@b.c"))
}

// iaCompare evaluates pred over every input in one statement and holds it to ValidRecipient.
func iaCompare(t *testing.T, ctx context.Context, q pgx.Tx, pred string, inputs []string) {
	t.Helper()
	var sendable []string
	goAnswer := map[string]bool{}
	for _, s := range inputs {
		ok := mail.ValidRecipient(s)
		if strings.ContainsRune(s, 0) || !utf8.ValidString(s) {
			if ok {
				t.Errorf("ValidRecipient accepts %q, which PostgreSQL cannot store", s)
			}
			continue
		}
		goAnswer[s] = ok
		sendable = append(sendable, s)
	}
	rows, err := q.Query(ctx, `SELECT x, (`+pred+`) IS TRUE FROM unnest($1::text[]) AS u(x)`, sendable)
	if err != nil {
		t.Fatalf("evaluate 6b: %v", err)
	}
	defer rows.Close()
	var (
		n, yes, no, mismatches int
		kinds                  = map[string]int{}
	)
	v6 := regexp.MustCompile(`@\[[^\]]*:`)
	tail := regexp.MustCompile(`@\[[^\]]*:[^\]]*\.[^\]]*\]$`)
	for rows.Next() {
		var (
			s  string
			db bool
		)
		if err := rows.Scan(&s, &db); err != nil {
			t.Fatal(err)
		}
		n++
		if db != goAnswer[s] {
			mismatches++
			if mismatches <= 20 {
				t.Errorf("%q: the database says %v, mail.ValidRecipient %v", s, db, goAnswer[s])
			}
			continue
		}
		if !db {
			no++
			continue
		}
		yes++
		switch {
		case tail.MatchString(s):
			kinds["IPv6 with an IPv4 tail"]++
		case v6.MatchString(s):
			kinds["IPv6"]++
		case strings.Contains(s, "@["):
			kinds["IPv4"]++
		default:
			kinds["dot-atom"]++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if mismatches > 0 {
		t.Fatalf("%d of %d inputs disagree", mismatches, n)
	}
	if n != len(sendable) || yes < 1000 || no < 1000 {
		t.Fatalf("anti-vacuity: %d rows of %d, %d accepted, %d refused", n, len(sendable), yes, no)
	}
	for _, k := range []string{"dot-atom", "IPv4", "IPv6", "IPv6 with an IPv4 tail"} {
		if kinds[k] < 20 {
			t.Errorf("anti-vacuity: only %d accepted %s inputs (%v)", kinds[k], k, kinds)
		}
	}
	t.Logf("%d inputs: %d accepted, %d refused, accepted by kind %v", n, yes, no, kinds)
}

// TestIdentityCreateForSignup_ANonASCIIAddressBirthsNothing is the finding, end to end through the
// function, AS tappa_app:
//
//	squat    a registration under U+0130 / U+212A -- each address nobody else holds, so 6b is the
//	         only thing in the way -- links nothing and births no identity.
//	victim   with such a homoglyph row standing in ANOTHER business (the old binary wrote it), the
//	         ASCII spelling's own registration births ITS identity, under its own spelling, byte for
//	         byte: the homoglyph row no longer counts in the e-mail-wide read.
//	control  an ASCII letter-case variant in another business still keeps the new row unlinked
//	         (identitysignup_test.go's case, kept by the same loop).
//
// MUTATIONS, in the test (savepoints): 6b removed -> the squats link (red); the loop's
// printable-ASCII test removed (every row counts again) -> the victim is refused (red).
func TestIdentityCreateForSignup_ANonASCIIAddressBirthsNothing(t *testing.T) {
	ctx, tx := opTx(t)
	tag := uuid.NewString()[:8]
	squatI := isNewWorld(t, ctx, tx, "", "")
	squatK := isNewWorld(t, ctx, tx, "", "")
	attacker := isNewWorld(t, ctx, tx, "", "")
	victim := isNewWorld(t, ctx, tx, "", "")
	caseVariant := isNewWorld(t, ctx, tx, "", "")
	casePeer := isNewWorld(t, ctx, tx, "", "")
	set := func(w *isWorld, email string) {
		w.email = email
		aiExec(t, ctx, tx, `UPDATE admin_users SET email = $2 WHERE id = $1`, w.admin, email)
	}
	set(&squatI, "ceo-"+tag+"@cor\u0130p-a.example")
	set(&squatK, "\u212aate-"+tag+"@corp-b.example")
	set(&attacker, "ceo-"+tag+"@cor\u0130p-c.example")
	set(&victim, "ceo-"+tag+"@corip-c.example")
	set(&caseVariant, "kate-"+tag+"@corp-d.example")
	set(&casePeer, "KATE-"+strings.ToUpper(tag)+"@CORP-D.EXAMPLE")
	if opInt(t, ctx, tx, `SELECT count(*) FROM admin_users WHERE email = $1::citext`, victim.email) != 2 {
		t.Fatal("PREMISE: citext does not fold the attacker's spelling onto the victim's")
	}

	identities := func(q pgx.Tx, email string) int64 {
		return opInt(t, ctx, q, `SELECT count(*) FROM admin_identities WHERE email = $1::citext`, email)
	}
	squat := func(q pgx.Tx, w isWorld) bool {
		linked := isCall(t, ctx, q, w.tenant, w.tenant, w.admin)
		if ident, _ := isLinked(t, ctx, q, w.admin); linked != (ident != uuid.Nil) {
			t.Fatalf("the answer (%v) and the row's link (%s) disagree", linked, ident)
		}
		return linked
	}
	var def string
	if err := tx.QueryRow(ctx, `SELECT pg_get_functiondef('public.identity_create_for_signup(uuid, uuid)'::regprocedure)`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	mutate := func(sp pgx.Tx, cut, put string) {
		if strings.Count(def, cut) != 1 {
			t.Fatalf("PREMISE: the body holds %q %d times", cut, strings.Count(def, cut))
		}
		aiExec(t, ctx, sp, strings.Replace(def, cut, put, 1))
	}
	const (
		ruleCut  = "    IF (pg_catalog.octet_length(v_email::text) <= 254\n"
		rulePut  = "    IF (true OR pg_catalog.octet_length(v_email::text) <= 254\n"
		asciiCut = "IF (r.email::text ~ '^[\\x21-\\x7e]+$') IS NOT FALSE THEN"
		asciiPut = "IF true THEN"
	)

	for _, w := range []struct {
		name string
		w    isWorld
	}{{"U+0130", squatI}, {"U+212A", squatK}} {
		t.Run("squat/"+w.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				if squat(sp, w.w) || identities(sp, w.w.email) != 0 {
					t.Fatal("a homoglyph registration linked its row or birthed an identity")
				}
			})
		})
		t.Run("mutation/6b removed/"+w.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				mutate(sp, ruleCut, rulePut)
				if !squat(sp, w.w) {
					t.Fatal("with 6b removed the squat is still refused: the case does not test 6b")
				}
			})
		})
	}

	t.Run("victim: the ASCII spelling births its own identity", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if squat(sp, attacker) {
				t.Fatal("PREMISE: the attacker's row linked")
			}
			if !squat(sp, victim) {
				t.Fatal("the victim's registration linked nothing: a homoglyph row in another business still blocks it")
			}
			var stored string
			if err := sp.QueryRow(ctx, `SELECT i.email::text FROM admin_identities i JOIN admin_users a ON a.identity_id = i.id WHERE a.id = $1`, victim.admin).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if stored != victim.email {
				t.Fatalf("the identity's address is %q, want the victim's spelling %q", stored, victim.email)
			}
			if ident, _ := isLinked(t, ctx, sp, attacker.admin); ident != uuid.Nil {
				t.Fatal("the attacker's row is linked")
			}
		})
	})
	t.Run("mutation/the e-mail-wide read counts every row/victim", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			mutate(sp, asciiCut, asciiPut)
			if squat(sp, victim) {
				t.Fatal("with the loop's ASCII test removed the victim still links: the case does not test it")
			}
		})
	})
	t.Run("control: an ASCII letter-case variant elsewhere still blocks", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if squat(sp, caseVariant) {
				t.Fatal("a known address (a case variant in another business) was linked")
			}
		})
	})
}

// identityAdminIdentitiesNonWriters is the allow-list of TestIdentityAddressRule_EveryIdentityInsertDecidesIt:
// the functions whose owner may INSERT into admin_identities and whose body names it, that write no
// identity row -- each reads, locks or updates an existing identity by its key (re-derived 2026-10-27
// on the M14-08a database: the fifteen tappa_identitydefiner bodies naming the table, minus
// identity_create_for_signup; resolve_identity_by_email is tappa_resolver's, which holds no INSERT,
// so it is no candidate). An allow-listed body is still read: an INSERT or a MERGE into
// admin_identities, or an EXECUTE, in it is red.
var identityAdminIdentitiesNonWriters = map[string]bool{
	"identity_consume_reset(text,text)":                       true,
	"identity_consume_row_reset(text,text)":                   true,
	"identity_link_legacy(text,text)":                         true,
	"identity_open_reset(uuid,text,timestamp with time zone)": true,
	"identity_open_session(uuid,uuid,text,text)":              true,
	"identity_password_for_session(text)":                     true,
	"identity_record_failed_login(uuid,text)":                 true,
	"identity_relink_refused(text,uuid)":                      true,
	"identity_request_verification(text,text)":                true,
	"identity_revoke_sessions(text,boolean)":                  true,
	"identity_set_password(text,text)":                        true,
	"identity_switch_session(text,uuid,text)":                 true,
	"identity_unlink_backfilled(text,uuid)":                   true,
	"identity_verify_email(text,text)":                        true,
	// M14-15 (00077): the session acceptance births no identity -- it locks the session's identity and,
	// on the first mailbox proof, UPDATEs its email_verified_at and window; its two addresses are held
	// to printable ASCII before it links anything (TestAdminInvites00077_NoIdentityIsBornOrLinkedForANonASCIIAddress).
	"identity_accept_invite(text,text)": true,
}

// identityAddressRuleWriters are the functions that DO insert into admin_identities, each carrying
// 00076's rule as a refusal: the registration's dual-write (00076) and, from M14-15 (00077), the
// invitation's acceptance with a new password.
var identityAddressRuleWriters = []string{"identity_create_for_signup(uuid,uuid)", "identity_accept_invite_new(text,text)"}

// iaStripComments removes SQL / PL/pgSQL comments (-- to the end of the line, /* ... */, nested)
// outside single-quoted literals and double-quoted identifiers, so a pattern written in a comment is
// not mistaken for a predicate. Literals and identifiers are kept as they are.
func iaStripComments(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\'' || s[i] == '"':
			q := s[i]
			j := i + 1
			for j < len(s) {
				if s[j] == q {
					if j+1 < len(s) && s[j+1] == q { // '' or "" -- an escaped quote
						j += 2
						continue
					}
					break
				}
				j++
			}
			end := min(j+1, len(s))
			b.WriteString(s[i:end])
			i = end
		case strings.HasPrefix(s[i:], "--"):
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				return b.String()
			}
			i += j
		case strings.HasPrefix(s[i:], "/*"):
			depth, j := 1, i+2
			for j < len(s) && depth > 0 {
				switch {
				case strings.HasPrefix(s[j:], "/*"):
					depth, j = depth+1, j+2
				case strings.HasPrefix(s[j:], "*/"):
					depth, j = depth-1, j+2
				default:
					j++
				}
			}
			b.WriteByte(' ')
			i = j
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

var (
	// iaRefusalBlock is a refusal in the shape 6b is written in: IF (<predicate>) IS NOT TRUE THEN
	// RETURN | CONTINUE -- the predicate without a statement end inside it, so the match cannot run
	// from one block into the next.
	iaRefusalBlock = regexp.MustCompile(`(?is)\bIF\s*\(([^;]*?)\)\s*IS\s+NOT\s+TRUE\s+THEN\s+(RETURN|CONTINUE)\b`)
	// iaIdentityWrite is an INSERT or a MERGE whose TARGET is admin_identities, bare or quoted, with
	// or without its schema (comments are stripped before it runs; a U&-escaped name is iaEscapedName).
	iaIdentityWrite = regexp.MustCompile(`(?is)\b(insert|merge)\s+into\s+("?public"?\s*\.\s*)?"?admin_identities"?([^_a-z0-9]|$)`)
	iaEscapedName   = regexp.MustCompile(`(?i)U&"`)
	iaExecute       = regexp.MustCompile(`(?i)\bexecute\b`)
)

// iaCarriesTheRule reports whether body (comments stripped) holds 00076's rule as a refusal: an
// iaRefusalBlock whose predicate has the length bound, the "=?" refusal and the pattern.
func iaCarriesTheRule(body string) bool {
	literal := `~ '` + strings.ReplaceAll(identityAddressPattern(), "'", "''") + `'`
	for _, m := range iaRefusalBlock.FindAllStringSubmatch(body, -1) {
		p := m[1]
		if strings.Contains(p, literal) && strings.Contains(p, "octet_length(") && strings.Contains(p, "<= 254") &&
			strings.Contains(p, "strpos(") && strings.Contains(p, "'=?') = 0") {
			return true
		}
	}
	return false
}

// iaIdentityWriterFindings is the guard. Candidates: every function (outside pg_catalog and
// information_schema) whose OWNER may INSERT into admin_identities -- a column privilege is enough --
// and whose definition (pg_get_functiondef: it covers a BEGIN ATOMIC body, whose prosrc is empty)
// names admin_identities in any spelling the scan sees (bare, quoted, U&-escaped) or holds an
// EXECUTE (dynamic SQL can spell the name any way at all). A candidate passes only if it carries the
// rule as a refusal, or sits on the allow-list and neither writes the table nor executes dynamic SQL.
// Anything else is a finding. And no rewrite rule may target admin_identities (a rule can turn any
// statement into an INSERT there).
func iaIdentityWriterFindings(t *testing.T, ctx context.Context, q pgx.Tx) (findings, candidates []string) {
	t.Helper()
	rows, err := q.Query(ctx, `SELECT p.oid::regprocedure::text, pg_get_functiondef(p.oid)
	                             FROM pg_proc p
	                            WHERE p.pronamespace NOT IN ('pg_catalog'::regnamespace, 'information_schema'::regnamespace)
	                              AND p.prokind IN ('f', 'p')
	                              AND has_any_column_privilege(p.proowner, 'public.admin_identities'::regclass, 'INSERT')
	                              AND (pg_get_functiondef(p.oid) ~* 'admin_identities|\mexecute\M|U&"')
	                            ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, name)
		body := iaStripComments(def)
		switch {
		case iaCarriesTheRule(body):
		case identityAdminIdentitiesNonWriters[name]:
			if iaIdentityWrite.MatchString(body) || iaExecute.MatchString(body) || iaEscapedName.MatchString(body) {
				findings = append(findings, name+": allow-listed as a non-writer, yet it writes admin_identities, executes dynamic SQL or escapes a name")
			}
		default:
			findings = append(findings, name+": may write admin_identities and carries no refusal on 00076's address rule")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var rules int64
	if err := q.QueryRow(ctx, `SELECT count(*) FROM pg_rewrite WHERE ev_class = 'public.admin_identities'::regclass`).Scan(&rules); err != nil {
		t.Fatal(err)
	}
	if rules != 0 {
		findings = append(findings, fmt.Sprintf("%d rewrite rule(s) target admin_identities", rules))
	}
	return findings, candidates
}

// TestIdentityAddressRule_EveryIdentityInsertDecidesIt: no function can write admin_identities
// without deciding 00076's address rule first. There is no CHECK behind the predicates (00076's
// header: a violation's DETAIL prints the address and the digest into the server log), so a writer
// that forgot the rule would birth squattable identities silently -- this is where that turns red.
// The guard (iaIdentityWriterFindings) FAILS CLOSED: a function whose owner may insert there and
// whose body names the table or runs dynamic SQL is a finding unless it carries the rule as an
// `IF (...) IS NOT TRUE THEN RETURN | CONTINUE` refusal (comments stripped first) or is a listed
// non-writer that indeed writes nothing there. It is a STRUCTURAL guard: that the refusal is on the
// value inserted is each writer's behaviour test (this file's for identity_create_for_signup;
// M14-06b's for identity_reconcile). Today the one writer is identity_create_for_signup;
// identity_reconcile must carry the refusal before it merges; 00073's backfill is a DO block, guarded
// by 00076's own assertion and deploy/README.md's precheck.
//
// CONTROLS, in savepoints, each red: the six shapes the M14-08a third eye planted past the first
// version of this test -- a BEGIN ATOMIC body, INSERT /* c */ INTO, a quoted "public"."admin_identities",
// MERGE ... WHEN NOT MATCHED THEN INSERT, EXECUTE of a concatenated name, EXECUTE format(%I.%I) -- the
// pattern in a comment only, an allow-listed body that starts inserting, and a rewrite rule on the
// table. And the positive control: a body with the rule as a real refusal is not named.
func TestIdentityAddressRule_EveryIdentityInsertDecidesIt(t *testing.T) {
	ctx, tx := opTx(t)
	findings, candidates := iaIdentityWriterFindings(t, ctx, tx)
	for _, f := range findings {
		t.Error(f)
	}
	if len(candidates) != len(identityAdminIdentitiesNonWriters)+len(identityAddressRuleWriters) {
		t.Fatalf("anti-vacuity: the candidates are %v -- want %v and the %d listed non-writers (a new one is reviewed and listed, or carries the rule)",
			candidates, identityAddressRuleWriters, len(identityAdminIdentitiesNonWriters))
	}
	for _, w := range identityAddressRuleWriters {
		if !slices.Contains(candidates, w) {
			t.Errorf("anti-vacuity: the writer %s is no candidate", w)
		}
	}
	for name := range identityAdminIdentitiesNonWriters {
		if !slices.Contains(candidates, name) {
			t.Errorf("the allow-list names %s, which is no candidate any more -- take it off", name)
		}
	}

	literal := strings.ReplaceAll(identityAddressPattern(), "'", "''")
	rule := "IF (pg_catalog.octet_length(p_email) <= 254 AND pg_catalog.strpos(p_email, '=?') = 0 AND p_email ~ '" + literal + "') IS NOT TRUE THEN RETURN; END IF;"
	plpgsql := func(name, body string) string {
		return "CREATE FUNCTION public." + name + "(p_email text) RETURNS void LANGUAGE plpgsql AS $f$ BEGIN " + body + " END $f$"
	}
	const ins = "(email, password_hash) VALUES (p_email, 'h');"
	for _, c := range []struct{ name, sql string }{
		{"zz_m1408a_atomic", "CREATE FUNCTION public.zz_m1408a_atomic(p_email text) RETURNS void LANGUAGE sql BEGIN ATOMIC INSERT INTO public.admin_identities (email, password_hash) VALUES (p_email, 'h'); END"},
		{"zz_m1408a_comment_in_insert", plpgsql("zz_m1408a_comment_in_insert", "INSERT /* c */ INTO public.admin_identities "+ins)},
		{"zz_m1408a_quoted", plpgsql("zz_m1408a_quoted", `INSERT INTO "public"."admin_identities" `+ins)},
		{"zz_m1408a_merge", plpgsql("zz_m1408a_merge", "MERGE INTO public.admin_identities AS i USING (SELECT p_email AS e) AS s ON false WHEN NOT MATCHED THEN INSERT (email, password_hash) VALUES (s.e, 'h');")},
		{"zz_m1408a_dynamic", plpgsql("zz_m1408a_dynamic", "EXECUTE 'INSERT INTO public.admin_' || 'identities (email, password_hash) VALUES ($1, ''h'')' USING p_email;")},
		{"zz_m1408a_format", plpgsql("zz_m1408a_format", "EXECUTE format('INSERT INTO %I.%I (email, password_hash) VALUES ($1, ''h'')', 'public', 'admin_identities') USING p_email;")},
		{"zz_m1408a_rule_in_a_comment", plpgsql("zz_m1408a_rule_in_a_comment", "/* "+rule+" */ -- "+rule+"\n INSERT INTO public.admin_identities "+ins)},
	} {
		t.Run("control/"+c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				aiExec(t, ctx, sp, c.sql)
				if f, _ := iaIdentityWriterFindings(t, ctx, sp); !slices.ContainsFunc(f, func(s string) bool { return strings.HasPrefix(s, c.name+"(") }) {
					t.Fatalf("CONTROL: %s is not named (%v)", c.name, f)
				}
			})
		})
	}
	t.Run("control/an allow-listed body that starts inserting", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			var def string
			if err := sp.QueryRow(ctx, `SELECT pg_get_functiondef('public.identity_record_failed_login(uuid,text)'::regprocedure)`).Scan(&def); err != nil {
				t.Fatal(err)
			}
			const anchor = "\nBEGIN\n"
			if !strings.Contains(def, anchor) {
				t.Fatal("PREMISE: no BEGIN line")
			}
			aiExec(t, ctx, sp, strings.Replace(def, anchor, anchor+"    INSERT INTO public.admin_identities (email, password_hash) VALUES ('x@y.z', 'h');\n", 1))
			if f, _ := iaIdentityWriterFindings(t, ctx, sp); !slices.ContainsFunc(f, func(s string) bool { return strings.HasPrefix(s, "identity_record_failed_login(") }) {
				t.Fatalf("CONTROL: the allow-listed writer is not named (%v)", f)
			}
		})
	})
	t.Run("control/a rewrite rule on the table", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `CREATE RULE zz_m1408a_rule AS ON UPDATE TO public.admin_identities DO ALSO NOTHING`)
			if f, _ := iaIdentityWriterFindings(t, ctx, sp); !slices.ContainsFunc(f, func(s string) bool { return strings.Contains(s, "rewrite rule") }) {
				t.Fatalf("CONTROL: the rule is not named (%v)", f)
			}
		})
	})
	t.Run("control/a writer that carries the rule as a refusal is not named", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			aiExec(t, ctx, sp, plpgsql("zz_m1408a_good", rule+" INSERT INTO public.admin_identities "+ins))
			if f, _ := iaIdentityWriterFindings(t, ctx, sp); slices.ContainsFunc(f, func(s string) bool { return strings.HasPrefix(s, "zz_m1408a_good(") }) {
				t.Fatalf("CONTROL: a writer with the refusal is named (%v)", f)
			}
		})
	})
}

// TestIdentity00076_DownGivesBack00074ByteForByte: in the test's transaction (rolled back), 00076's
// Down gives identity_create_for_signup back exactly as 00074 created it -- the body (prosrc) equal
// to 00074's text between the dollar quotes, and the owner, SECURITY DEFINER, the pinned search_path
// and the ACL equal to HEAD's -- and the Up again gives HEAD's definition and ACL back byte for byte.
// The Down locks no table. The database holds identities the way it does after 00073 (two
// backfill-shaped ASCII ones, written here), so the Up's own assertion runs over real rows.
func TestIdentity00076_DownGivesBack00074ByteForByte(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, ia00076File)
	for range 2 {
		aiExec(t, ctx, tx, `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2)`, aiEmail(), opFakeDigest("b"))
	}
	if tables := downTables(down); len(tables) > 0 {
		t.Fatalf("PREMISE: 00076's Down locks %v", tables)
	}
	type def struct{ src, owner, config, acl, full string }
	read := func(q pgx.Tx) def {
		t.Helper()
		var d def
		if err := q.QueryRow(ctx, `SELECT p.prosrc, p.proowner::regrole::text,
		                                  coalesce(array_to_string(p.proconfig, ','), '') || '/' || p.prosecdef::text,
		                                  coalesce(p.proacl::text, ''), pg_get_functiondef(p.oid)
		                             FROM pg_proc p WHERE p.oid = 'public.identity_create_for_signup(uuid,uuid)'::regprocedure`).
			Scan(&d.src, &d.owner, &d.config, &d.acl, &d.full); err != nil {
			t.Fatal(err)
		}
		return d
	}
	head := read(tx)
	if !strings.Contains(head.src, identityAddressRuleMarker) {
		t.Fatal("PREMISE at HEAD: the body has no address rule")
	}
	u74, _ := opMigrationSections(t, "00074_create_identities_for_new_signups.sql")
	i := strings.Index(u74, "CREATE FUNCTION public.identity_create_for_signup(")
	if i < 0 {
		t.Fatal("00074 no longer creates identity_create_for_signup")
	}
	rest := u74[i:]
	a, b := strings.Index(rest, "AS $$\n"), strings.Index(rest, "\n$$;")
	if a < 0 || b < a {
		t.Fatal("00074: no dollar-quoted body")
	}
	want := rest[a+len("AS $$") : b+1]
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down, "00076 Down")
		got := read(sp)
		if got.src != want {
			t.Error("after the Down the body is not 00074's byte for byte")
		}
		if got.owner != "tappa_identitydefiner" || got.config != "search_path=pg_catalog, pg_temp/true" || got.acl != head.acl {
			t.Errorf("after the Down: owner %s, config %q, acl %q; want tappa_identitydefiner, the pinned definer, HEAD's acl %q",
				got.owner, got.config, got.acl, head.acl)
		}
		opRunSection(t, ctx, sp, up, "00076 Up again")
		if again := read(sp); again != head {
			t.Error("the Up again does not give HEAD's definition and ACL back byte for byte")
		}
	})
}

// TestIdentity00076_UpRefusesANonASCIIIdentityWithItsCountAlone closes the precheck's window: an
// identity 00073 birthed from a homoglyph row the manual count did not see (the old binary took it
// in between) stops 00076's Up in the same goose run, and the error names the COUNT and no address
// (CLAUDE.md §7). MUTATION, in the test: the assertion disabled -> the Up passes (red).
func TestIdentity00076_UpRefusesANonASCIIIdentityWithItsCountAlone(t *testing.T) {
	ctx, tx := opTx(t)
	up, _ := opMigrationSections(t, ia00076File)
	tag := uuid.NewString()[:8]
	addresses := []string{"ceo-" + tag + "@cor\u0130p.example", "\u212aate-" + tag + "@corp.example"}
	run := func(sp pgx.Tx, sql string) error {
		_, err := sp.Conn().PgConn().Exec(ctx, sql).ReadAll()
		return err
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		for _, a := range addresses {
			aiExec(t, ctx, sp, `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2)`, a, opFakeDigest("h"))
		}
		aiSavepoint(t, ctx, sp, func(inner pgx.Tx) {
			err := run(inner, up)
			if err == nil {
				t.Fatal("00076's Up passed over two identities whose addresses are not printable ASCII")
			}
			msg := err.Error()
			var pe *pgconn.PgError
			if errors.As(err, &pe) {
				msg += " " + pe.Detail + " " + pe.Hint + " " + pe.Where
			}
			if !strings.Contains(msg, "2 identity row(s)") {
				t.Errorf("the error does not give the count: %s", msg)
			}
			for _, a := range addresses {
				if strings.Contains(msg, a) || strings.Contains(msg, tag) {
					t.Errorf("the error names an address")
				}
			}
		})
		aiSavepoint(t, ctx, sp, func(inner pgx.Tx) {
			const cut = "    IF n > 0 THEN\n"
			if strings.Count(up, cut) != 1 {
				t.Fatal("PREMISE: the Up's assertion is not where the test cuts it")
			}
			if err := run(inner, strings.Replace(up, cut, "    IF false THEN\n", 1)); err != nil {
				t.Fatalf("MUTATION: with the assertion disabled the Up still fails: %v", err)
			}
		})
	})
}

// TestIdentityAddressRule_TheRunbookCountsWithThePattern: deploy/README.md's precheck (before Deploy 1)
// and its post-migrate count apply 00076's rule with the very pattern, byte for byte -- a hand copy
// of 2 700 characters would drift silently otherwise.
func TestIdentityAddressRule_TheRunbookCountsWithThePattern(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "deploy", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "AND email::text ~ '" + strings.ReplaceAll(identityAddressPattern(), "'", "''") + "'))"
	if n := strings.Count(string(b), want); n != 2 {
		t.Fatalf("deploy/README.md applies 00076's pattern %d times, want 2 (the precheck and the post-migrate count)", n)
	}
}
