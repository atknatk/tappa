// Package sqlbelt is the LEXICAL LAYER of CLAUDE.md §4.5's static belt: the test-side scanners
// that read every production query in db/queries and require an explicit tenant predicate beside
// RLS. Six packages carry such a belt over the queries THEY call (internal/adminauth and
// internal/domain/{billing,ledger,review,signup,tenant}); until backlog T164 each carried its own
// copy of this layer, and the copies had drifted (T24: the aggregate FILTER narrowing lived in
// three of six).
//
// IT IS A TEST HELPER AND NOTHING ELSE. Only _test.go files import it; TestNoProductionImport
// walks the repository and goes red the day a non-test file does. It imports only the standard
// library, so no belt can form an import cycle through it.
//
// WHAT IT READS, AND WHAT IT REFUSES TO READ (T164). The belt's structure scans (top-level OR,
// top-level AND, where a WHERE clause ends, which parentheses nest) are only as good as the
// lexer underneath them. This one reads exactly four forms:
//
//	'...'      a plain string literal, '' as the quote inside it (B'', X'', N'' and U&'' lex the
//	           same way); its contents are BLANKED, so a paren, an OR or a WHERE inside it is no
//	           longer structure
//	-- ...     a line comment, blanked to the end of its line
//	"name"     a quoted identifier made of letters, digits, _ and $ only, left as it is
//	$1         a positional parameter
//
// Everything else that can hide structure is UNREADABLE and the statement is counted as
// UNSCOPED (fail closed): an E'...' escape string (\' ends nothing), a $$...$$ or $tag$...$tag$
// dollar quote, a /* */ block comment (they nest), a quoted identifier holding anything else, and
// an unterminated literal or identifier. Measured before this package existed: in each of those
// forms a depth-0 OR passed all six belts (Planted in corpus.go). Reading them properly would mean
// a second SQL lexer that can be wrong; db/queries uses none of them today, so refusing them costs
// nothing and closes the channel. standard_conforming_strings is assumed on (PostgreSQL's default
// since 9.1), which is what makes a backslash in a plain literal an ordinary character.
package sqlbelt

import (
	"fmt"
	"regexp"
	"strings"
)

// Prepare is the belt's entry point for one statement body: the body with its literals and line
// comments blanked (same length, same line breaks, so offsets and printed clauses still line up),
// or an error naming the first form it cannot read. A belt that gets the error must count the
// statement as unscoped -- that is the fail-closed half of T164.
func Prepare(body string) (string, error) {
	m, bad := lex(body)
	if bad != "" {
		return "", fmt.Errorf("uses %s, a lexical form the section 4.5 belt cannot read, so the "+
			"statement is counted as UNSCOPED (fail closed) rather than read wrongly; write it "+
			"with plain '...' literals and -- comments (internal/sqlbelt)", bad)
	}
	return m, nil
}

// Unreadable names the first lexical form in s that this layer cannot read, or "" when there is
// none.
func Unreadable(s string) string {
	_, bad := lex(s)
	return bad
}

// Mask blanks the contents of every plain string literal and every line comment in s, keeping
// its length and line breaks. A form Unreadable would name is left as it is.
func Mask(s string) string {
	m, _ := lex(s)
	return m
}

func lex(s string) (string, string) {
	out := []byte(s)
	bad := ""
	note := func(form string) {
		if bad == "" {
			bad = form
		}
	}
	blank := func(from, to int) {
		for k := from; k < to; k++ {
			if out[k] != '\n' {
				out[k] = ' '
			}
		}
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			end := len(s)
			if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
				end = i + j
			}
			blank(i, end)
			i = end - 1
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			note("a /* */ block comment")
			i++
		case c == '\'':
			if i > 0 && (s[i-1] == 'E' || s[i-1] == 'e') && (i < 2 || !IsWordByte(s[i-2])) {
				note(`an E'...' escape string`)
			}
			j := i + 1
			for ; j < len(s); j++ {
				if s[j] != '\'' {
					continue
				}
				if j+1 < len(s) && s[j+1] == '\'' {
					j++
					continue
				}
				break
			}
			if j >= len(s) {
				note("an unterminated string literal")
				blank(i+1, len(s))
				return string(out), bad
			}
			blank(i+1, j)
			i = j
		case c == '"':
			j := strings.IndexByte(s[i+1:], '"')
			if j < 0 {
				note("an unterminated quoted identifier")
				return string(out), bad
			}
			end := i + 1 + j
			if j == 0 || !identOnly(s[i+1:end]) || (end+1 < len(s) && s[end+1] == '"') {
				note("a quoted identifier holding something other than letters, digits, _ and $")
			}
			i = end
		case c == '$':
			// Inside an identifier (a$b) a dollar is a letter; before a digit it is a positional
			// parameter. Anywhere else it opens a dollar quote, which this layer does not read.
			if i > 0 && IsWordByte(s[i-1]) {
				continue
			}
			if i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
				continue
			}
			note("a $$ dollar-quoted string")
		}
	}
	return string(out), bad
}

func identOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if !IsWordByte(s[i]) && s[i] != '$' {
			return false
		}
	}
	return true
}

// IsWordByte reports whether b can be part of an unquoted SQL word.
func IsWordByte(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// WordBoundary reports whether s[i:i+n] stands alone as a word.
func WordBoundary(s string, i, n int) bool {
	before := i == 0 || !IsWordByte(s[i-1])
	after := i+n >= len(s) || !IsWordByte(s[i+n])
	return before && after
}

// HasTopLevelOr reports whether s has an OR outside every parenthesis, string literal and line
// comment (M15-16 review, security ORTA, measured): `x.tenant_id = @tenant_id AND a OR b` is
// (scope AND a) OR b, every row whenever b holds, so a WHERE with a depth-zero OR scopes nothing
// whatever its conjuncts look like. A parenthesised OR (`AND (a OR b)`) is depth one and stays a
// filter.
//
// 🔴 A CLAUSE THIS LAYER CANNOT READ ANSWERS TRUE (T164). The OR it cannot see may be there --
// an E-string whose backslash-quote a plain reader takes as the end of the literal hides one
// (sqlbelt.Planted) -- so "no OR found" would be a guess, and the guess would be the unsafe one.
func HasTopLevelOr(s string) bool {
	m, bad := lex(s)
	if bad != "" {
		return true
	}
	depth := 0
	upper := strings.ToUpper(m)
	for i := 0; i < len(m); i++ {
		switch m[i] {
		case '(':
			depth++
			continue
		case ')':
			depth--
			continue
		}
		if depth <= 0 && strings.HasPrefix(upper[i:], "OR") && WordBoundary(upper, i, 2) {
			return true
		}
	}
	return false
}

// SplitTopLevelAnd splits s on AND outside parentheses, literals and line comments. The pieces
// come from the masked text, so a literal's contents can never satisfy a scope matcher.
func SplitTopLevelAnd(s string) []string {
	m := Mask(s)
	var parts []string
	depth, last := 0, 0
	upper := strings.ToUpper(m)
	for i := 0; i < len(m); i++ {
		switch m[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth != 0 {
			continue
		}
		if strings.HasPrefix(upper[i:], "AND") && WordBoundary(upper, i, 3) {
			parts = append(parts, m[last:i])
			last = i + 3
		}
	}
	return append(parts, m[last:])
}

// MaskParens blanks every parenthesised span of s (the parentheses too), keeping length and line
// breaks, so a caller can search the top level of a statement.
//
// 🔴 A PAREN INSIDE A LITERAL IS NOT ONE (T164). internal/domain/tenant's copy counted it, so
// `x <> '(' OR true AND x <> ')'` masked everything between the two quotes -- the OR with it --
// and the clause looked scoped. Literals and line comments are blanked first.
func MaskParens(s string) string {
	out := []byte(Mask(s))
	depth := 0
	for i := 0; i < len(out); i++ {
		switch out[i] {
		case '(':
			depth++
			out[i] = ' '
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			out[i] = ' '
			continue
		}
		if depth > 0 && out[i] != '\n' {
			out[i] = ' '
		}
	}
	return string(out)
}

// MaskNested blanks the contents of every NESTED parenthesis in s, leaving s's own text (and the
// nested parentheses themselves) at its own offsets. Spaces rather than deletion: removing text
// can join two tokens and invent a keyword that is not there.
func MaskNested(s string) string {
	out := []byte(Mask(s))
	depth := 0
	for i := 0; i < len(out); i++ {
		switch out[i] {
		case '(':
			depth++
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth > 0 {
			out[i] = ' '
		}
	}
	return string(out)
}

// ParenSpans returns every balanced parenthesised span of s as [open, close] offsets, innermost
// first, ignoring parentheses inside literals and line comments.
func ParenSpans(s string) [][2]int {
	m := Mask(s)
	var out [][2]int
	var stack []int
	for i := 0; i < len(m); i++ {
		switch m[i] {
		case '(':
			stack = append(stack, i)
		case ')':
			if n := len(stack); n > 0 {
				out = append(out, [2]int{stack[n-1], i})
				stack = stack[:n-1]
			}
		}
	}
	return out
}

// Clause is one WHERE clause: its text (literals blanked) and the offset of its WHERE keyword in
// the body it was read from, so a caller can look at what precedes it (which table it filters).
type Clause struct {
	Text string
	At   int
}

var (
	whereRE = regexp.MustCompile(`(?is)\bWHERE\b`)
	// The lookbehind for an aggregate FILTER; Go's regexp has none, so the text before the WHERE
	// is matched with its own anchored pattern.
	filterRE = regexp.MustCompile(`(?is)\bFILTER\s*\(\s*$`)
	// The clauses that may follow a WHERE. FOR UPDATE / FOR SHARE (M6-06 phase B: a locked read's
	// last conjunct read "p.tenant_id = @tenant_id FOR UPDATE" and no longer matched), ON CONFLICT
	// (M11 API-5: CreateLocationFromAPI read "t.id = @tenant_id ON CONFLICT ...") and RETURNING (a
	// write's RETURNING list is not its WHERE; ledger's copy lacked it, measured inert -- with the
	// stop a RETURNING list can no longer raise a false alarm, and a tenant_id named only there
	// still scopes nothing).
	stopRE = regexp.MustCompile(`(?is)\b(ORDER\s+BY|GROUP\s+BY|LIMIT|HAVING|RETURNING|FOR\s+NO\s+KEY\s+UPDATE|FOR\s+UPDATE|FOR\s+SHARE|ON\s+CONFLICT)\b`)
)

// WhereClauses returns every WHERE clause in body, each ending at its enclosing block's closing
// paren or at a clause that may follow WHERE.
//
// 🔴 AN AGGREGATE'S `FILTER (WHERE ...)` IS NOT A WHERE CLAUSE (M6-11, measured 2026-08-12; T24
// carried it to every belt). It chooses which rows of an ALREADY-SCOPED result an aggregate sees
// -- `count(*) FILTER (WHERE practice)` -- and can scope nothing, so demanding a tenant predicate
// in it is demanding one in the wrong place: the anomalies section is ten such counts in one
// statement. It is NARROWED, not weakened: only a WHERE right after `FILTER (` is skipped; the
// statement's own WHERE, every CTE's and every subquery's are still read, so a query whose only
// tenant predicate sits inside a FILTER is still refused.
//
// Literals and line comments are blanked first, so a WHERE, a paren or a stop word inside one is
// not structure (T164: `<> ')' OR true` ended the clause at the quoted paren and dropped the OR).
func WhereClauses(body string) []Clause {
	m := Mask(body)
	var out []Clause
	for _, loc := range whereRE.FindAllStringIndex(m, -1) {
		if filterRE.MatchString(m[:loc[0]]) {
			continue
		}
		rest := m[loc[1]:]
		depth, end := 0, len(rest)
		for i := 0; i < len(rest); i++ {
			if rest[i] == '(' {
				depth++
				continue
			}
			if rest[i] == ')' {
				if depth == 0 {
					end = i
					break
				}
				depth--
			}
		}
		clause := rest[:end]
		if k := stopRE.FindStringIndex(clause); k != nil {
			clause = clause[:k[0]]
		}
		clause = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(clause), ";"))
		if clause == "" {
			continue
		}
		out = append(out, Clause{Text: clause, At: loc[0]})
	}
	return out
}
