package db

// identitybelt_test.go -- M14-04 (ADR 0031 §5.2, ADR 0021 §3.2): two text scans over the bodies of
// the identity functions, both FAIL CLOSED.
//
//  1. THE BELT. tappa_identitydefiner and tappa_resolver are BYPASSRLS, so in their bodies the
//     explicit tenant predicate is the only barrier: every reference to a tenant table must name its
//     tenant. The scanner models a small SQL subset -- the one these bodies are written in -- and
//     every construct outside it is ITSELF a finding unless identityBeltAllowedConstructs names it for
//     that function: a top-level OR or NOT in a predicate, a WITH, a double-quoted identifier, a
//     function or a subquery in FROM, a tenant table reached other than as `public.<name>`, dynamic
//     SQL, a dollar quote. (The third eye's rounds: an anchor counted in a SELECT list, a join
//     correlation, an OR, a comment, a CTE alias and a quoted name each passed an earlier version
//     that tried to recognise the bad shapes; this one recognises only the good ones.)
//  2. DELETE ON THE PERSON TABLES. ADR 0031 §5.1: no deletion on the five identity tables but the two
//     named sweeps (R17). Every function body in the database is read; a DELETE or TRUNCATE that
//     reaches an identity table may appear only in the function identityPersonTableDeleters names
//     for that table.
//
// M14-05 and later cards add their functions' counts to identityTenantWideReads /
// identitySessionKeyLookups (identitycatalog_test.go's skeleton rule) and, for a construct the scanner
// does not model, a named entry here with its reason.

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// identityTenantWideReads names, per function, how many statements read a tenant table
// DELIBERATELY across tenants (ADR 0021 §3.2: "bilerek bütün tenant'ları tarayan fonksiyonlar adıyla
// bellidir"; 00071's header names the four). Each is bounded by an identity instead of a tenant:
// `<alias>.identity_id = <parameter | variable | an admin_identities row's id>` at the top level of
// its WHERE/ON.
var identityTenantWideReads = map[string]int{
	"identity_open_session":     1, // the login's oldest-membership fallback (LIMIT 1)
	"identity_memberships":      1, // the membership list itself (LIMIT 50)
	"identity_revoke_sessions":  1, // "sign out everywhere"'s driver; every write it drives is pinned
	"resolve_identity_by_email": 1, // md.7's has_active_membership
}

// identitySessionKeyLookups names, per function, how many statements find a session by its OWN key
// (`<sessions alias>.token_hash = p_*`, globally UNIQUE -- the md.7 shape: the statement produces the
// tenant, it cannot name one). A membership row joined to it is anchored only through BOTH halves of
// the composite key, `a.id = s.admin_user_id AND a.tenant_id = s.tenant_id`.
var identitySessionKeyLookups = map[string]int{
	"identity_memberships":     1,
	"identity_switch_session":  1,
	"identity_revoke_sessions": 1,
}

// identityBeltAllowedConstructs: function -> the unmodelled constructs it may use, each with its
// reason. Empty for M14-04: none of its bodies needs one.
var identityBeltAllowedConstructs = map[string]map[string]string{}

// identityPersonTables are ADR 0031 §5.1's five tenant-scope-less identity tables.
var identityPersonTables = []string{
	"admin_identities", "admin_identity_resets", "admin_identity_verifications", "admin_signup_claims", "admin_identity_events",
}

// identityPersonTableDeleters: table -> the only functions whose bodies may DELETE from (or TRUNCATE)
// it (ADR 0031 §5.1, R17: the two named sweeps). A table missing here has no deleter at all.
var identityPersonTableDeleters = map[string][]string{
	"admin_identity_events": {"identity_sweep_events"},
	"admin_signup_claims":   {"identity_sweep_signup_claims"},
}

// identityLexed is a body with comments removed, string literals emptied, and what the lexer saw.
type identityLexed struct {
	code    string   // the body, `--` and (nested) `/* */` comments as spaces, every literal as ''
	strings []string // the literals' contents (dynamic SQL hides in them)
	quoted  bool     // a double-quoted identifier occurs
	dollar  bool     // a dollar quote occurs inside the body
}

// identityLex is a small lexer for PostgreSQL text: `'...'` with ” doubling and E'...' backslash
// escapes, `--` to the end of the line, nested `/* */`, and `"..."` identifiers (kept, flagged).
func identityLex(src string) identityLexed {
	var (
		out strings.Builder
		lx  identityLexed
	)
	isWord := func(b byte) bool {
		return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
	}
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '-' && i+1 < len(src) && src[i+1] == '-':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			out.WriteByte(' ')
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			depth := 0
			for i < len(src) {
				if src[i] == '/' && i+1 < len(src) && src[i+1] == '*' {
					depth++
					i += 2
					continue
				}
				if src[i] == '*' && i+1 < len(src) && src[i+1] == '/' {
					depth--
					i += 2
					if depth == 0 {
						break
					}
					continue
				}
				i++
			}
			out.WriteByte(' ')
		case c == '\'':
			escape := i > 0 && (src[i-1] == 'E' || src[i-1] == 'e') && (i < 2 || !isWord(src[i-2]))
			var lit strings.Builder
			i++
			for i < len(src) {
				if escape && src[i] == '\\' && i+1 < len(src) {
					lit.WriteByte(src[i+1])
					i += 2
					continue
				}
				if src[i] == '\'' {
					if i+1 < len(src) && src[i+1] == '\'' {
						lit.WriteByte('\'')
						i += 2
						continue
					}
					i++
					break
				}
				lit.WriteByte(src[i])
				i++
			}
			lx.strings = append(lx.strings, lit.String())
			out.WriteString("''")
		case c == '"':
			lx.quoted = true
			out.WriteByte(c)
			i++
		case c == '$':
			lx.dollar = true
			out.WriteByte(c)
			i++
		default:
			out.WriteByte(c)
			i++
		}
	}
	lx.code = out.String()
	return lx
}

var (
	identityStatementSplit = regexp.MustCompile(`(?i);|\bLOOP\b`)
	identityToken          = regexp.MustCompile(`[A-Za-z_][\w.]*|<>|<=|>=|!=|\S`)
	identityStop           = map[string]bool{
		"WHERE": true, "ON": true, "ORDER": true, "GROUP": true, "LIMIT": true, "RETURNING": true, "FOR": true,
		"JOIN": true, "LEFT": true, "RIGHT": true, "FULL": true, "INNER": true, "CROSS": true, "SET": true, "VALUES": true,
		"UNION": true, "EXCEPT": true, "INTERSECT": true, "SELECT": true, "FROM": true, "INTO": true, "THEN": true,
		"LOOP": true, "HAVING": true, "WINDOW": true, "USING": true,
	}
	identityParamOrVar = regexp.MustCompile(`^[pv]_\w+$`)
)

// identityRegions returns, for every WHERE and ON of a statement, its TOP-LEVEL equalities (inside
// parentheses nothing anchors: `(x.tenant_id = v OR true)` constrains nothing) and the top-level
// constructs the scanner does not model (OR, a NOT that is not IS NOT).
func identityRegions(stmt string) (eqs [][2]string, unmodelled []string) {
	toks := identityToken.FindAllString(stmt, -1)
	for i, tk := range toks {
		if u := strings.ToUpper(tk); u != "WHERE" && u != "ON" {
			continue
		}
		depth := 0
		var top []string
		for j := i + 1; j < len(toks); j++ {
			t := toks[j]
			if t == "(" {
				depth++
				continue
			}
			if t == ")" {
				depth--
				if depth < 0 {
					break
				}
				continue
			}
			if depth == 0 && identityStop[strings.ToUpper(t)] {
				break
			}
			if depth == 0 {
				top = append(top, t)
			}
		}
		for k, t := range top {
			switch strings.ToUpper(t) {
			case "OR":
				unmodelled = append(unmodelled, "a top-level OR in a predicate")
			case "NOT":
				if k == 0 || strings.ToUpper(top[k-1]) != "IS" {
					unmodelled = append(unmodelled, "a top-level NOT in a predicate")
				}
			case "=":
				if k > 0 && k+1 < len(top) {
					eqs = append(eqs, [2]string{top[k-1], top[k+1]})
				}
			}
		}
	}
	return eqs, unmodelled
}

type identityBody struct {
	Name, Src string
	Args      []string
}

// identityBodies reads the bodies the belt holds: every function tappa_identitydefiner owns and every
// resolve_identity_*.
func identityBodies(ctx context.Context, q opQuerier) ([]identityBody, error) {
	rows, err := q.Query(ctx, `SELECT p.proname, p.prosrc, coalesce(p.proargnames, '{}')
	                             FROM pg_proc p
	                            WHERE p.proowner = 'tappa_identitydefiner'::regrole OR p.proname LIKE 'resolve\_identity\_%'
	                            ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[identityBody])
}

// identityTenantTables: every table in public with a tenant_id column, and tenants.
func identityTenantTables(ctx context.Context, q opQuerier) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT c.relname FROM pg_class c WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
	                             AND (c.relname = 'tenants' OR EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid
	                                                                    AND a.attname = 'tenant_id' AND NOT a.attisdropped))
	                            ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// identityBeltFindings holds every body to the belt. A reference to a tenant table is either an
// INSERT whose column list names tenant_id, or `public.<table> AS <alias>` whose alias is
//
//   - ANCHORED: at the top level of a WHERE/ON, `<alias>.tenant_id` (`<alias>.id` for tenants) equals
//     a parameter of the function (p_*), a variable its DECLARE names (v_*), or a column of a record
//     the body declares with `FOR <name> IN`; or it is a sessions alias found by its own key
//     (identitySessionKeyLookups); or it is correlated by the tenant key to an anchored alias (a
//     membership and a session: both halves of the composite key); or
//   - in a NAMED TENANT-WIDE READ: an admin_users alias bounded by `identity_id = ...`, or correlated
//     by the tenant key to one (identityTenantWideReads).
//
// Nothing else counts -- another alias's tenant_id, a CTE's or a subquery's column, a SELECT-list
// expression -- and the counts of both named classes are pinned per function.
func identityBeltFindings(ctx context.Context, q opQuerier, wide, keyed map[string]int) ([]string, int, error) {
	tenantTables, err := identityTenantTables(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	bodies, err := identityBodies(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	names := strings.Join(tenantTables, "|")
	ref := regexp.MustCompile(`(?i)(INSERT\s+INTO\s+)?\bpublic\.(` + names + `)\b(\s+AS\s+(\w+))?(\s*\(([^)]*)\))?`)
	anyMention := regexp.MustCompile(`(?i)\b(` + names + `)\b`)
	qualified := regexp.MustCompile(`(?i)\bpublic\.(` + names + `)\b`)
	identityRef := regexp.MustCompile(`(?i)\bpublic\.admin_identities\s+AS\s+(\w+)`)
	forRecord := regexp.MustCompile(`(?i)\bFOR\s+(\w+)\s+IN\b`)
	declared := regexp.MustCompile(`\bv_\w+\b`)
	unmodelledRes := []struct {
		what string
		re   *regexp.Regexp
	}{
		{"a WITH (common table expression)", regexp.MustCompile(`(?i)\bWITH\b`)},
		{"a function in FROM", regexp.MustCompile(`(?i)\b(FROM|JOIN)\s+[A-Za-z_][\w.]*\s*\(`)},
		{"a subquery in FROM", regexp.MustCompile(`(?i)\b(FROM|JOIN)\s*\(`)},
		{"a relation list in FROM", regexp.MustCompile(`(?i)\bFROM\s+[A-Za-z_][\w.]*(\s+AS\s+\w+)?\s*,`)},
		{"dynamic SQL", regexp.MustCompile(`(?i)\bEXECUTE\b`)},
	}

	var out []string
	refs := 0
	for _, b := range bodies {
		lx := identityLex(b.Src)
		flag := func(what string) {
			if _, ok := identityBeltAllowedConstructs[b.Name][what]; !ok {
				out = append(out, fmt.Sprintf("%s: %s -- a construct the belt does not model (name it in identityBeltAllowedConstructs, with its reason, or rewrite it)", b.Name, what))
			}
		}
		if lx.quoted {
			flag("a double-quoted identifier")
		}
		if lx.dollar {
			flag("a dollar quote or a positional parameter")
		}
		for _, u := range unmodelledRes {
			if u.re.MatchString(lx.code) {
				flag(u.what)
			}
		}
		if n, m := len(anyMention.FindAllString(lx.code, -1)), len(qualified.FindAllString(lx.code, -1)); n != m {
			flag(fmt.Sprintf("a tenant table reached other than as public.<name> (%d mention(s), %d qualified)", n, m))
		}
		params := map[string]bool{}
		for _, a := range b.Args {
			if strings.HasPrefix(a, "p_") {
				params[a] = true
			}
		}
		vars := map[string]bool{}
		if up := strings.ToUpper(lx.code); strings.Contains(up, "DECLARE") {
			start := strings.Index(up, "DECLARE")
			if end := strings.Index(up[start:], "BEGIN"); end > 0 {
				for _, v := range declared.FindAllString(lx.code[start:start+end], -1) {
					vars[v] = true
				}
			}
		}
		records := map[string]bool{}
		for _, m := range forRecord.FindAllStringSubmatch(lx.code, -1) {
			records[m[1]] = true
		}
		isVar := func(x string) bool {
			return identityParamOrVar.MatchString(x) && (params[x] || vars[x])
		}

		wideUsed, keyedUsed := 0, 0
		for _, stmt := range identityStatementSplit.Split(lx.code, -1) {
			tableOf := map[string]string{}
			for _, m := range ref.FindAllStringSubmatch(stmt, -1) {
				refs++
				table, alias := m[2], m[4]
				switch {
				case m[1] != "":
					if !regexp.MustCompile(`\btenant_id\b`).MatchString(m[6]) {
						out = append(out, fmt.Sprintf("%s: INSERT INTO %s does not name tenant_id", b.Name, table))
					}
				case alias == "":
					out = append(out, fmt.Sprintf("%s: a reference to %s without `AS <alias>`", b.Name, table))
				default:
					tableOf[alias] = table
				}
			}
			eqs, unmodelled := identityRegions(stmt)
			for _, u := range unmodelled {
				flag(u)
			}
			if len(tableOf) == 0 {
				continue
			}
			identities := map[string]bool{}
			for _, m := range identityRef.FindAllStringSubmatch(stmt, -1) {
				identities[m[1]] = true
			}
			has := func(l, r string) bool {
				for _, p := range eqs {
					if (p[0] == l && p[1] == r) || (p[0] == r && p[1] == l) {
						return true
					}
				}
				return false
			}
			isAnchor := func(x string) bool {
				if isVar(x) {
					return true
				}
				rec, col, ok := strings.Cut(x, ".")
				return ok && records[rec] && (col == "tenant_id" || col == "id")
			}
			isIdentity := func(x string) bool {
				if isVar(x) {
					return true
				}
				rec, col, ok := strings.Cut(x, ".")
				return ok && col == "id" && identities[rec]
			}
			key := func(alias string) string {
				if tableOf[alias] == "tenants" {
					return alias + ".id"
				}
				return alias + ".tenant_id"
			}
			anchored, bounded, selfBounded := map[string]bool{}, map[string]bool{}, map[string]bool{}
			for alias, table := range tableOf {
				for _, p := range eqs {
					for _, side := range [][2]string{{p[0], p[1]}, {p[1], p[0]}} {
						if side[0] == key(alias) && isAnchor(side[1]) {
							anchored[alias] = true
						}
						if table == "admin_sessions" && side[0] == alias+".token_hash" && params[side[1]] && !anchored[alias] {
							anchored[alias] = true
							keyedUsed++
						}
						if table == "admin_users" && side[0] == alias+".identity_id" && isIdentity(side[1]) {
							bounded[alias], selfBounded[alias] = true, true
						}
					}
				}
			}
			correlated := func(x, y string) bool {
				if !has(key(x), key(y)) {
					return false
				}
				switch {
				case tableOf[x] == "admin_users" && tableOf[y] == "admin_sessions":
					return has(x+".id", y+".admin_user_id")
				case tableOf[x] == "admin_sessions" && tableOf[y] == "admin_users":
					return has(y+".id", x+".admin_user_id")
				}
				return true
			}
			for changed := true; changed; {
				changed = false
				for x := range tableOf {
					for y := range tableOf {
						if x == y || !correlated(x, y) {
							continue
						}
						if anchored[y] && !anchored[x] {
							anchored[x], changed = true, true
						}
						if bounded[y] && !bounded[x] {
							bounded[x], changed = true, true
						}
					}
				}
			}
			for alias, table := range tableOf {
				switch {
				case anchored[alias]:
				case bounded[alias]:
					if selfBounded[alias] {
						wideUsed++
					}
				default:
					out = append(out, fmt.Sprintf("%s: %s AS %s is anchored by no tenant (parameter, variable, FOR record), no session key and no identity", b.Name, table, alias))
				}
			}
		}
		if wideUsed != wide[b.Name] {
			out = append(out, fmt.Sprintf("%s: %d tenant-wide read(s), identityTenantWideReads names %d", b.Name, wideUsed, wide[b.Name]))
		}
		if keyedUsed != keyed[b.Name] {
			out = append(out, fmt.Sprintf("%s: %d session lookup(s) by its own key, identitySessionKeyLookups names %d", b.Name, keyedUsed, keyed[b.Name]))
		}
	}
	sort.Strings(out)
	return slices.Compact(out), refs, nil
}

// TestIdentityBodies_EveryTenantTableReferenceNamesItsTenant is the belt (ADR 0031 §5.2: both owners
// are BYPASSRLS, so the explicit predicate is the only barrier). CONTROLS: the real body rewritten in
// a savepoint -- each must turn the belt red:
//   - the revoke loop's tenant, and the switch target's tenant, removed;
//   - M5: the membership list without `a.identity_id = v_identity` (left with a SELECT-list
//     `a.tenant_id = v_tenant` and the join `t.id = a.tenant_id`);
//   - M9: the revoke's UPDATE correlated only to itself (`FROM admin_users a WHERE a.id =
//     x.admin_user_id AND a.tenant_id = x.tenant_id` -- every session of every business);
//   - the session lookup joined by half its composite key;
//   - E1: a top-level OR (`AND NOT p_keep_current OR x.id <> v_session` -- the parentheses gone);
//   - E2: the anchor inside a block comment (`WHERE /* o.tenant_id = v_tenant */ o.id IS NOT NULL`);
//   - E3: a CTE column as the "anchor" (`WITH c AS (SELECT i.last_used_tenant AS tenant_id ...) ...
//     WHERE o.tenant_id = c.tenant_id`);
//   - E4: a quoted name (`public."admin_sessions"`), invisible to `public.<name>`;
//   - a block comment hiding the whole anchor clause at the end, an anchor inside parentheses with
//     an OR, and an unqualified tenant table.
func TestIdentityBodies_EveryTenantTableReferenceNamesItsTenant(t *testing.T) {
	ctx, tx := opTx(t)
	findings, refs, err := identityBeltFindings(ctx, tx, identityTenantWideReads, identitySessionKeyLookups)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Error(f)
	}
	if refs < 25 {
		t.Fatalf("anti-vacuity: the scan saw %d references to a tenant table", refs)
	}
	const (
		revoke     = "public.identity_revoke_sessions(text, boolean)"
		switchFn   = "public.identity_switch_session(text, uuid, text)"
		membership = "public.identity_memberships(text)"
	)
	switchRevoke := "    UPDATE public.admin_sessions AS o\n       SET revoked_at = pg_catalog.now()\n     WHERE o.id = v_session\n       AND o.tenant_id = v_tenant\n       AND o.revoked_at IS NULL;"
	for _, c := range []struct{ name, fn, cut, put string }{
		{"the revoke loop's tenant", revoke, "WHERE x.tenant_id = m.tenant_id\n               AND ", "WHERE "},
		{"the switch's target tenant", switchFn, "WHERE b.tenant_id = p_target_tenant\n       AND ", "WHERE "},
		{"M5: the membership list without its identity", membership, "WHERE a.identity_id = v_identity\n           AND ", "WHERE "},
		{"M9: the revoke correlated to itself", revoke,
			"WHERE x.tenant_id = m.tenant_id\n               AND x.admin_user_id = m.id\n",
			"FROM public.admin_users AS a\n             WHERE a.id = x.admin_user_id\n               AND a.tenant_id = x.tenant_id\n"},
		{"the session joined by half its key", membership,
			"JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id\n     WHERE s.token_hash",
			"JOIN public.admin_users AS a ON a.id = s.admin_user_id\n     WHERE s.token_hash"},
		{"E1: a top-level OR", revoke, "AND (NOT p_keep_current OR x.id <> v_session)", "AND NOT p_keep_current OR x.id <> v_session"},
		{"E2: the anchor in a block comment", switchFn, switchRevoke,
			"    UPDATE public.admin_sessions AS o\n       SET revoked_at = pg_catalog.now()\n     WHERE /* o.tenant_id = v_tenant */ o.id IS NOT NULL\n       AND o.id = v_session\n       AND o.revoked_at IS NULL;"},
		{"E3: a CTE column as the anchor", switchFn, switchRevoke,
			"    WITH c AS (SELECT i.last_used_tenant AS tenant_id FROM public.admin_identities AS i WHERE i.id = v_identity)\n" +
				"    UPDATE public.admin_sessions AS o\n       SET revoked_at = pg_catalog.now()\n      FROM c\n     WHERE o.id = v_session\n       AND o.tenant_id = c.tenant_id\n       AND o.revoked_at IS NULL;"},
		{"E4: a quoted table name", switchFn, switchRevoke,
			"    UPDATE public.\"admin_sessions\" AS o\n       SET revoked_at = pg_catalog.now()\n     WHERE o.id = v_session\n       AND o.revoked_at IS NULL;"},
		{"the anchor inside parentheses with an OR", switchFn, "       AND o.tenant_id = v_tenant\n       AND o.revoked_at IS NULL;",
			"       AND (o.tenant_id = v_tenant OR true)\n       AND o.revoked_at IS NULL;"},
		{"a spaced qualification", switchFn, "    UPDATE public.admin_sessions AS o\n", "    UPDATE public . admin_sessions AS o\n"},
		{"a tenant table without its schema", switchFn, "    UPDATE public.admin_sessions AS o\n", "    UPDATE admin_sessions AS o\n"},
		{"a subquery in FROM", switchFn, switchRevoke,
			"    UPDATE public.admin_sessions AS o\n       SET revoked_at = pg_catalog.now()\n      FROM (SELECT v_tenant AS tenant_id) AS c\n     WHERE o.id = v_session\n       AND o.tenant_id = c.tenant_id\n       AND o.revoked_at IS NULL;"},
	} {
		t.Run("control/"+c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				var def string
				if err := sp.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure)`, c.fn).Scan(&def); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(def, c.cut) {
					t.Fatalf("PREMISE: %s's body lacks %q", c.fn, c.cut)
				}
				aiExec(t, ctx, sp, strings.Replace(def, c.cut, c.put, 1))
				got, _, err := identityBeltFindings(ctx, sp, identityTenantWideReads, identitySessionKeyLookups)
				if err != nil {
					t.Fatal(err)
				}
				if len(got) == 0 {
					t.Fatalf("the rewritten body passed the belt")
				}
				t.Logf("flagged: %v", got)
			})
		})
	}
}

// TestIdentityLex_CommentsLiteralsAndQuotes pins the lexer the belt stands on: line comments, nested
// block comments, ” doubling, E” escapes, quoted identifiers and dollar quotes.
func TestIdentityLex_CommentsLiteralsAndQuotes(t *testing.T) {
	lx := identityLex("a -- x.tenant_id = v\n/* b /* nested */ x.tenant_id = v */ 'it''s' E'a\\'b' \"q\" c")
	if strings.Contains(lx.code, "tenant_id") || !lx.quoted || lx.dollar {
		t.Fatalf("code %q quoted=%v dollar=%v", lx.code, lx.quoted, lx.dollar)
	}
	if !slices.Equal(lx.strings, []string{"it's", "a'b"}) {
		t.Fatalf("literals %q", lx.strings)
	}
	if !identityLex("SELECT $x$y$x$").dollar {
		t.Fatal("a dollar quote was not seen")
	}
}

// identityDeleteFindings reads EVERY function body in every user schema: a body that reaches one of
// the identity tables (any spelling -- qualified or not, quoted or not, in code or inside a string
// literal, i.e. dynamic SQL) AND carries a DELETE or TRUNCATE (in code or in a literal) must belong
// to a function identityPersonTableDeleters names for that table.
func identityDeleteFindings(ctx context.Context, q opQuerier, deleters map[string][]string) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT n.nspname || '.' || p.proname, p.proname, coalesce(p.prosrc, '')
	                             FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
	                            WHERE n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname !~ '^pg_(toast|temp)'
	                              AND p.prolang <> (SELECT oid FROM pg_language WHERE lanname = 'c')
	                              AND p.prolang <> (SELECT oid FROM pg_language WHERE lanname = 'internal')`)
	if err != nil {
		return nil, err
	}
	type fn struct{ Qualified, Name, Src string }
	fns, err := pgx.CollectRows(rows, pgx.RowToStructByPos[fn])
	if err != nil {
		return nil, err
	}
	verb := regexp.MustCompile(`(?i)\b(DELETE|TRUNCATE)\b`)
	var out []string
	for _, f := range fns {
		lx := identityLex(f.Src)
		text := lx.code + " " + strings.Join(lx.strings, " ")
		if !verb.MatchString(text) {
			continue
		}
		for _, table := range identityPersonTables {
			if !regexp.MustCompile(`(?i)\b` + table + `\b`).MatchString(text) {
				continue
			}
			if !slices.Contains(deleters[table], f.Name) || f.Qualified != "public."+f.Name {
				out = append(out, fmt.Sprintf("%s: a DELETE or TRUNCATE in a body that reaches %s (its deleters: %v -- ADR 0031 §5.1, R17)", f.Qualified, table, deleters[table]))
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// TestIdentityTables_OnlyTheNamedSweepsDelete (M14-01's security audit; ADR 0031 §5.1, R17): no
// function body deletes from an identity table but the sweep the map names for it. The rule is
// data-driven: M14-05's identity_sweep_events (and M14-17's identity_sweep_signup_claims) pass by
// their name. CONTROLS (savepoints): a DELETE on admin_identities, the same quoted and qualified, the
// same as dynamic SQL, and a TRUNCATE are flagged; a sweep of the right name and table passes; the
// right name over another identity table, and the right name in another schema, are flagged.
func TestIdentityTables_OnlyTheNamedSweepsDelete(t *testing.T) {
	ctx, tx := opTx(t)
	got, err := identityDeleteFindings(ctx, tx, identityPersonTableDeleters)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range got {
		t.Error(f)
	}
	plpgsql := func(schema, name, body string) string {
		return fmt.Sprintf("CREATE FUNCTION %s.%s() RETURNS void LANGUAGE plpgsql AS $f$ BEGIN %s END $f$", schema, name, body)
	}
	for _, c := range []struct {
		name string
		sql  []string
		want bool // a finding expected
	}{
		{"a DELETE on admin_identities", []string{plpgsql("public", "zz_identity_probe", "DELETE FROM public.admin_identities;")}, true},
		{"quoted and qualified", []string{plpgsql("public", "zz_identity_probe", `DELETE FROM "public"."admin_identities";`)}, true},
		{"dynamic SQL", []string{plpgsql("public", "zz_identity_probe", `EXECUTE 'DELETE FROM admin_identities';`)}, true},
		{"a TRUNCATE", []string{plpgsql("public", "zz_identity_probe", "TRUNCATE public.admin_identities;")}, true},
		{"the named sweep", []string{plpgsql("public", "identity_sweep_events",
			"DELETE FROM public.admin_identity_events WHERE created_at < now() - interval '90 days';")}, false},
		{"the named sweep over another table", []string{plpgsql("public", "identity_sweep_events", "DELETE FROM public.admin_identity_resets;")}, true},
		{"the named sweep in another schema", []string{`CREATE SCHEMA zz_identity_probe`,
			plpgsql("zz_identity_probe", "identity_sweep_events", "DELETE FROM public.admin_identity_events;")}, true},
	} {
		t.Run("control/"+c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				for _, s := range c.sql {
					aiExec(t, ctx, sp, s)
				}
				got, err := identityDeleteFindings(ctx, sp, identityPersonTableDeleters)
				if err != nil {
					t.Fatal(err)
				}
				if c.want != (len(got) > 0) {
					t.Fatalf("findings %v, want a finding = %v", got, c.want)
				}
			})
		})
	}
}
