package db

// identitybelt_test.go -- M14-04 (ADR 0031 §5.2, ADR 0021 §3.2): two text scans over the bodies of
// the identity functions, both FAIL CLOSED.
//
// 🔴 SCOPE. The belt is a guard against a BUILDER'S MISTAKE -- a statement in a BYPASSRLS body that
// forgot its tenant -- not a proof that a body is safe. The behaviour tests (identitysession_test.go:
// the refusal and isolation cases, the mutation proofs) remain the primary line. What the belt owes
// is to FAIL CLOSED: anything it does not model is a finding, never a pass.
//
//  1. THE BELT. tappa_identitydefiner and tappa_resolver are BYPASSRLS, so in their bodies the
//     explicit tenant predicate is the only barrier: every reference to a tenant relation must name
//     its tenant. The scanner models a small SQL subset -- the one these bodies are written in -- and
//     every construct outside it is ITSELF a finding unless identityBeltAllowedConstructs names it for
//     that function: in a WHERE/ON, a conjunct holding any bare word but IS / NOT / NULL / OPERATOR /
//     TRUE / FALSE (OR, CASE, DISTINCT FROM, IN, a function call ...), an IS other than IS [NOT] NULL,
//     a NOT other than IS NOT NULL; anywhere, a WITH, a CASE, a set operation, a double-quoted
//     identifier, a function, a subquery or a relation list in FROM, a relation reached other than as
//     `public.<name>`, a `public.<name>` that is neither a tenant relation (table, view or
//     materialised view with a tenant_id) nor a named non-tenant one, dynamic SQL, a dollar quote.
//     A value anchors only if it is a parameter, or a variable / FOR record every writer of which
//     (SELECT INTO, RETURNING INTO, FOR ... IN) itself passed the belt -- never one written by `:=`.
//     (The third eye's rounds, each measured: an anchor in a SELECT list, a join correlation, an OR,
//     a comment, a CTE alias, a quoted name, a region cut by DISTINCT FROM or CASE, `= v IS NOT TRUE`,
//     a variable of any source and a view each passed an earlier version that tried to recognise the
//     bad shapes; this one recognises only the good ones.)
//     THE IDENTITY KEY RULE (third eye, round 5): every statement that reads or writes one of the
//     five identity tables (identityPersonTables) through `public.<table> AS <alias>` carries, as a
//     top-level conjunct of its WHERE/ON, a KEY: `<alias>.id = X` (X a parameter, a proven variable or
//     a proven FOR record's column), `<alias>.token_hash = p_*`, or -- on the four tables below
//     admin_identities -- `<alias>.identity_id = X`. A statement without one is a finding (its
//     construct name is identityUnkeyedIdentityStatement; a sweep or the e-mail resolver names it in
//     identityBeltAllowedConstructs with its reason), and a variable it writes is never trusted --
//     whitelisted or not (`SELECT i.last_used_tenant INTO v_last ... LIMIT 1` is no tenant). An
//     INSERT INTO an identity table needs no key. M14-05's union runs this rule over 00072.
//  2. DELETE ON THE PERSON TABLES. ADR 0031 §5.1: no deletion on the five identity tables but the two
//     named sweeps (R17). Every function body in the database is read; a DELETE or TRUNCATE that
//     reaches an identity table may appear only in the function identityPersonTableDeleters names
//     for that table.
//
//  3. THE NAMED E-MAIL-WIDE READ (M14-08, ADR 0031 §5.2 named exception 2): an admin_users alias bound
//     by `<alias>.email OPERATOR(public.=) X` -- X a parameter or a variable every writer of which
//     passed -- as a top-level conjunct of its WHERE is neither anchored nor bounded by an identity:
//     it reads one ADDRESS across every tenant. It passes only where identityEmailWideReads names it,
//     per function, with its count; anywhere else it is the ordinary "anchored by no tenant" finding.
//
// M14-05 and later cards add their functions' counts to identityTenantWideReads /
// identitySessionKeyLookups (identitycatalog_test.go's skeleton rule) and, for a construct the scanner
// does not model, a named entry in identityBeltAllowedConstructs with its reason.

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
	// M14-05 (00072): the lock on every membership of the identity (the lock order: identity ->
	// memberships -> tokens / sessions) and, where the digest is written to every linked row, the loop
	// that drives those writes -- each write pinned to the loop record's tenant.
	"identity_set_password":      2,
	"identity_consume_row_reset": 2,
	"identity_consume_reset":     2,
	"identity_link_legacy":       1,
	"identity_unlink_backfilled": 1,
	"identity_relink_refused":    1,
	// M14-18 (00079): the window's list of the identity's own memberships (bounded by the identity; 49 list rows).
	"identity_confirm_window": 1,
	// M14-17b (00080): "no active membership" -- the identity's memberships, under the person's lock, an
	// existence test (it returns nothing and writes nothing).
	"identity_claim_signup": 1,
	// M14-15 (00077): the lock on every membership of the identity before the business's row and the
	// invitation are locked (the lock order).
	"identity_accept_invite": 1,
}

// identitySessionKeyLookups names, per function, how many statements find a session -- or (M14-05) a
// per-row reset link -- by its OWN key (`<alias>.token_hash = p_*`, globally UNIQUE on both tables --
// the md.7 shape: the statement produces the tenant, it cannot name one). A membership row joined to
// it is anchored only through BOTH halves of the composite key, `a.id = s.admin_user_id AND
// a.tenant_id = s.tenant_id` (`a.id = r.admin_user_id AND a.tenant_id = r.tenant_id`).
var identitySessionKeyLookups = map[string]int{
	"identity_memberships":     1,
	"identity_switch_session":  1,
	"identity_revoke_sessions": 1,
	// M14-05 (00072): the session behind the call; link_legacy reads two (the identity's and the
	// legacy row's), verify_email one, consume_row_reset its per-row link (password_resets.token_hash).
	"identity_password_for_session": 1,
	"identity_set_password":         1,
	"identity_link_legacy":          2,
	"identity_unlink_backfilled":    1,
	"identity_relink_refused":       1,
	"identity_request_verification": 1,
	"identity_verify_email":         1,
	"identity_consume_row_reset":    1,
	// M14-18 (00079): the session behind the window's read and behind "Keep all".
	"identity_confirm_window": 1,
	"identity_keep_all":       1,
	// M14-17 (00078): the existing identity's own live session -- the only way the claim reaches it.
	"identity_claim_signup": 1,
	// M14-15 (00077): the invitation by its own token (admin_invites.token_hash, UNIQUE) -- the
	// resolver's one statement, each acceptance's first read -- and identity_accept_invite's session.
	"resolve_admin_invite_by_token_hash": 1,
	"identity_accept_invite":             2,
	"identity_accept_invite_new":         1,
}

// identityEmailWideReads names, per function, how many statements read admin_users by ONE ADDRESS
// across every tenant (`<alias>.email OPERATOR(public.=) <trusted value>`, the belt's third class).
// Only a named exception of ADR 0031 §5.2 may: such a read is how an e-mail reaches a writer, which
// the rule "a writer finds the identity only through a secret" otherwise forbids.
var identityEmailWideReads = map[string]int{
	// M14-08 (00074): "the address is in no other admin_users row" -- an existence test, under the
	// address's namespaced lock; it returns nothing and writes nothing. M14-08a (00076): a FOR loop
	// over those rows, a row whose address is not printable ASCII not counting.
	"identity_create_for_signup": 1,
	// M14-18 (00079): the rows refused in THIS window -- identity_relink_refused's own predicate (the
	// identity's address, read from the identity row the session found; unlinked_at >= the window's
	// opening), LIMIT 50. It writes nothing (STABLE) and lists only what the person may take back.
	"identity_confirm_window": 1,
}

// identityClaimAddressKeys names, per function, how many statements key an admin_identities alias by
// THE CLAIM'S ADDRESS -- `<alias>.email OPERATOR(public.=) X`, X a variable every writer of which passed
// (there: the draft row the claim token found) -- the belt's fourth class (M14-17b, 00080). The identity
// key rule otherwise forbids an address as a key: "a writer finds the identity only through a secret",
// and here the secret is the registration token (ADR 0031 §5.2's "kayıt token'ı"), the address its draft's.
var identityClaimAddressKeys = map[string]int{
	// M14-17b (00080): the identity with no active membership, locked while its digest is the verified one.
	"identity_claim_signup": 1,
}

// identityTokenKeyed are the tenant tables whose own globally UNIQUE token_hash may anchor a lookup
// (counted in identitySessionKeyLookups): the panel session (00006) and, from M14-05, the per-row reset
// link (00006/00019, password_resets_token_hash_key); from M14-15 the administrator invitation
// (00077, admin_invites_token_hash_key) -- the mailed secret the acceptance is keyed by; the
// statement produces the invitation's tenant, it cannot name one (md.7's shape).
var identityTokenKeyed = map[string]bool{"admin_sessions": true, "password_resets": true, "admin_invites": true}

// identityUnkeyedIdentityStatement is the construct name of the identity key rule's finding.
const identityUnkeyedIdentityStatement = "an identity-table statement keyed by no id, identity_id or token hash"

// identityBeltAllowedConstructs: function -> the unmodelled constructs it may use, each with its
// reason.
var identityBeltAllowedConstructs = map[string]map[string]string{
	"resolve_identity_by_email": {
		identityUnkeyedIdentityStatement: "ADR 0002 md.7 / ADR 0031 §5.2: the one e-mail -> identity path; the address is the key " +
			"(admin_identities.email is globally UNIQUE, compared with OPERATOR(public.=)); STABLE sql, it writes nothing",
	},
	// M14-05 (00072).
	"identity_sweep_events": {
		identityUnkeyedIdentityStatement: "ADR 0031 §5.1, R17: the event log's 90-day sweep deletes by age alone, no argument, " +
			"no identity; identityPersonTableDeleters names it the table's one deleter",
	},
	// M14-08 (00074). The scanner reads every ON as a join predicate; `ON CONFLICT (email) DO NOTHING` is
	// none -- it carries no predicate, it names the UNIQUE (email) arbiter -- so its three words are
	// named here, for this body alone, rather than taught to the scanner for every body.
	"identity_create_for_signup": {
		`"CONFLICT" in a predicate`: "ADR 0031 §5.2 / S13: the identity INSERT's ON CONFLICT (email) DO NOTHING -- the second belt behind the " +
			"address's advisory lock; a concurrent birth of the same address ends in no row, never a 500 ...",
		`"DO" in a predicate`:      "... (ON CONFLICT's DO; the arbiter is admin_identities_email_key, an INSERT INTO an identity table needs no key) ...",
		`"NOTHING" in a predicate`: "... (DO NOTHING: no row is updated, so no SET and no WHERE follow it)",
	},
	// M14-17 (00078). The new address's identity INSERT carries 00074's ON CONFLICT (email) DO NOTHING
	// (S13: the second belt behind the address's advisory lock); its three words are named for this body
	// alone, as for identity_create_for_signup.
	"identity_claim_signup": {
		`"CONFLICT" in a predicate`: "ADR 0031 §5.2 / S13: the new identity's INSERT ... ON CONFLICT (email) DO NOTHING -- a concurrent birth of " +
			"the same address (another confirmation, an invitation, the old pod's identity_create_for_signup) ends in no row and a refusal, never a 500 ...",
		`"DO" in a predicate`:      "... (ON CONFLICT's DO; the arbiter is admin_identities_email_key, an INSERT INTO an identity table needs no key) ...",
		`"NOTHING" in a predicate`: "... (DO NOTHING: no row is updated, so no SET and no WHERE follow it)",
	},
	"identity_sweep_signup_claims": {
		identityUnkeyedIdentityStatement: "ADR 0031 §5.1, R17: the registration drafts' sweep deletes the expired ones by age and the used ones " +
			"by state, no argument, no identity (a draft is not a record); identityPersonTableDeleters names it the table's one deleter",
	},
	// M14-15 (00077): identity_accept_invite_new's identity INSERT, the same three words for the same
	// reason as identity_create_for_signup's -- the address's advisory lock first, ON CONFLICT (email)
	// DO NOTHING second (S13: an acceptance and a registration of one new address end in one identity).
	"identity_accept_invite_new": {
		`"CONFLICT" in a predicate`: "ADR 0031 §5.2 / S13: the identity INSERT's ON CONFLICT (email) DO NOTHING behind the address's advisory " +
			"lock -- an identity that already carries the address makes the acceptance 0 rows, never a 500 ...",
		`"DO" in a predicate`:      "... (ON CONFLICT's DO; the arbiter is admin_identities_email_key, an INSERT INTO an identity table needs no key) ...",
		`"NOTHING" in a predicate`: "... (DO NOTHING: no row is updated, so no SET and no WHERE follow it)",
	},
	"identity_unlink_backfilled": {
		"a call to public.crypt":            "ADR 0031 §5.8, S20: the sentinel digest is bcrypt (pgcrypto, a C function that reads no table) of ...",
		"a call to public.gen_random_bytes": "... 32 random bytes from pgcrypto's strong source, discarded in the same expression ...",
		"a call to public.gen_salt":         "... at cost 12 (adminauth.Cost); pgcrypto lives in public (scripts/db-init/01-roles.sql), 00072's precondition checks it",
	},
}

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
	// The TRUE clause boundaries a WHERE/ON region ends at, at its own depth (V1/V2: an inner keyword
	// -- DISTINCT FROM, CASE ... THEN -- is no boundary; it is inside the region and unmodelled).
	identityStop = map[string]bool{
		"WHERE": true, "ON": true, "ORDER": true, "GROUP": true, "HAVING": true, "WINDOW": true, "LIMIT": true,
		"OFFSET": true, "RETURNING": true, "FOR": true, "INTO": true, "JOIN": true, "LEFT": true, "RIGHT": true,
		"FULL": true, "INNER": true, "CROSS": true, "UNION": true, "EXCEPT": true, "INTERSECT": true,
	}
	// The bare words a conjunct may hold; any other bare word (OR, CASE, DISTINCT, IN, ANY, EXISTS, a
	// function name ...) is a construct the belt does not model.
	identityConjunctWords = map[string]bool{"IS": true, "NOT": true, "NULL": true, "OPERATOR": true, "TRUE": true, "FALSE": true}
	identityParamOrVar    = regexp.MustCompile(`^[pv]_\w+$`)
	identityWord          = regexp.MustCompile(`^[A-Za-z_]\w*$`)
)

// identityRegions returns, for every WHERE and ON of a statement, the equalities that ANCHOR -- a
// top-level conjunct that is exactly `X = Y` (inside parentheses nothing anchors, and `X = Y IS NOT
// TRUE` is not `X = Y` -- V3) -- and the constructs the belt does not model: a conjunct with a bare
// word outside identityConjunctWords, an IS other than IS NULL / IS NOT NULL at the conjunct's end,
// a NOT other than in IS NOT NULL, and a FOR that is not a row lock.
func identityRegions(stmt string) (eqs, ops [][2]string, unmodelled []string) {
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
				if strings.ToUpper(t) == "FOR" && (j+1 >= len(toks) || !slices.Contains([]string{"UPDATE", "SHARE", "NO", "KEY"}, strings.ToUpper(toks[j+1]))) {
					unmodelled = append(unmodelled, "a FOR in a predicate that is not a row lock")
				}
				break
			}
			if depth == 0 {
				top = append(top, t)
			}
		}
		var conj []string
		flush := func() {
			defer func() { conj = nil }()
			for k, t := range conj {
				u := strings.ToUpper(t)
				if !identityWord.MatchString(t) || identityParamOrVar.MatchString(t) {
					continue
				}
				if !identityConjunctWords[u] {
					unmodelled = append(unmodelled, fmt.Sprintf("%q in a predicate", u))
					continue
				}
				switch u {
				case "IS":
					rest := strings.ToUpper(strings.Join(conj[k+1:], " "))
					if rest != "NULL" && rest != "NOT NULL" {
						unmodelled = append(unmodelled, "an IS other than IS NULL / IS NOT NULL in a predicate")
					}
				case "NOT":
					if k == 0 || strings.ToUpper(conj[k-1]) != "IS" {
						unmodelled = append(unmodelled, "a NOT in a predicate")
					}
				}
			}
			if len(conj) == 3 && conj[1] == "=" {
				eqs = append(eqs, [2]string{conj[0], conj[2]})
			}
			// `X OPERATOR(<op>) Y` at the top level: the parenthesised operator is depth 1 and gone
			// from conj, so the caller re-reads the operator from the statement (identityEmailBound).
			if len(conj) == 3 && strings.ToUpper(conj[1]) == "OPERATOR" {
				ops = append(ops, [2]string{conj[0], conj[2]})
			}
		}
		for _, t := range top {
			if strings.ToUpper(t) == "AND" {
				flush()
				continue
			}
			conj = append(conj, t)
		}
		flush()
	}
	return eqs, ops, unmodelled
}

type identityBody struct {
	Name, Src string
	Args      []string
}

// identityBodies reads the bodies the belt holds: every function tappa_identitydefiner owns, every
// resolve_identity_* and (M14-15) every resolve_admin_invite_* -- the md.7 resolvers of ADR 0031's
// closed list (BYPASSRLS, so their tenant-table reads are held to the belt too).
func identityBodies(ctx context.Context, q opQuerier) ([]identityBody, error) {
	rows, err := q.Query(ctx, `SELECT p.proname, p.prosrc, coalesce(p.proargnames, '{}')
	                             FROM pg_proc p
	                            WHERE p.proowner = 'tappa_identitydefiner'::regrole OR p.proname LIKE 'resolve\_identity\_%'
	                               OR p.proname LIKE 'resolve\_admin\_invite\_%'
	                            ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[identityBody])
}

// identityBeltNonTenantRelations are the relations in public a belt body may name that carry no
// tenant: ADR 0031 §5.1's five identity tables. Any other `public.<name>` that is not a tenant
// relation is a finding (V5) -- a view over a tenant table without a tenant_id column included.
var identityBeltNonTenantRelations = identityPersonTables

// identityTenantRelations: every table, view and materialised view in public with a tenant_id column
// (V5: a view over admin_sessions is a tenant relation), and tenants.
func identityTenantRelations(ctx context.Context, q opQuerier) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT c.relname FROM pg_class c WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p', 'v', 'm')
	                             AND (c.relname = 'tenants' OR EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid
	                                                                    AND a.attname = 'tenant_id' AND NOT a.attisdropped))
	                            ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// identityBeltScope is what one body's statements share: the function's parameters, the variables
// its DECLARE names, the FOR records it declares, and which of them are TRUSTED as tenant / identity
// values (V4): a parameter always; a variable or a FOR record only when every statement that writes
// it -- SELECT ... INTO, RETURNING ... INTO, FOR <record> IN -- itself passed the belt; never one
// written by `:=` (unless from a single parameter).
type identityBeltScope struct {
	params, declared, records map[string]bool
	trusted                   map[string]bool
	assigned                  map[string]bool // written by := from anything but a parameter
}

// identityStatementVerdict is one statement held to the belt under a scope.
type identityStatementVerdict struct {
	findings                    []string
	passed                      bool
	wide, keyed, email, address int
	writes                      []string // the variables (INTO) or the record (FOR ... IN) the statement writes
}

var (
	identityRef        *regexp.Regexp // set per run: public.<tenant relation>
	identityIntoList   = regexp.MustCompile(`(?i)\bINTO\s+(\w+(?:\s*,\s*\w+)*)`)
	identityForRecord  = regexp.MustCompile(`(?i)^\s*(?:\w+\s+)*?FOR\s+(\w+)\s+IN\b`)
	identityPublicName = regexp.MustCompile(`(?i)\bpublic\s*\.\s*(\w+)(\s*\()?`)
	// identityRelationKeyword: what makes `public.<name> (` a relation with a column list (INSERT INTO
	// public.audit_log (...)) and not a call.
	identityRelationKeyword = regexp.MustCompile(`(?i)\b(INTO|FROM|JOIN|UPDATE|TABLE|ONLY)\s*$`)
	identityRelation        = regexp.MustCompile(`(?i)\b(FROM|JOIN|INTO|UPDATE|TABLE)\s+([^\s(,;]+)`)
)

func identityStatement(fn, stmt string, tenantRel map[string]bool, sc identityBeltScope) identityStatementVerdict {
	var v identityStatementVerdict
	bad := func(format string, a ...any) { v.findings = append(v.findings, fn+": "+fmt.Sprintf(format, a...)) }
	unmodelled := func(what string) {
		if _, ok := identityBeltAllowedConstructs[fn][what]; !ok {
			bad("%s -- a construct the belt does not model (name it in identityBeltAllowedConstructs, with its reason, or rewrite it)", what)
		}
	}
	if m := identityForRecord.FindStringSubmatch(stmt); m != nil {
		v.writes = append(v.writes, m[1])
	}
	for _, m := range identityRelation.FindAllStringSubmatch(stmt, -1) {
		kw, target := strings.ToUpper(m[1]), m[2]
		switch {
		case kw == "INTO" && !regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+`+regexp.QuoteMeta(target)).MatchString(stmt):
			continue // a plpgsql INTO list
		case kw == "UPDATE" && strings.EqualFold(target, "OF"):
			continue // FOR UPDATE OF <alias>
		case kw == "TABLE":
			unmodelled("a TABLE command")
		case !strings.HasPrefix(strings.ToLower(target), "public."):
			unmodelled(fmt.Sprintf("a relation reached other than as public.<name> (%s %s)", kw, target))
		}
	}
	for _, ix := range identityPublicName.FindAllStringSubmatchIndex(stmt, -1) {
		name := strings.ToLower(stmt[ix[2]:ix[3]])
		called := ix[4] >= 0 && !identityRelationKeyword.MatchString(stmt[:ix[0]])
		if called {
			// M14-05: a call to a function in public (pgcrypto's, for the unlink's sentinel). A function
			// can read any table under the definer's BYPASSRLS, so each is a named construct per body.
			unmodelled("a call to public." + name)
			continue
		}
		if !tenantRel[name] && !slices.Contains(identityBeltNonTenantRelations, name) {
			bad("public.%s is neither a tenant relation nor a named non-tenant one (identityBeltNonTenantRelations)", name)
		}
	}

	tableOf := map[string]string{}
	refs := identityRef.FindAllStringSubmatch(stmt, -1)
	for _, m := range refs {
		table, alias := strings.ToLower(m[2]), m[4]
		switch {
		case m[1] != "":
			if !regexp.MustCompile(`\btenant_id\b`).MatchString(m[6]) {
				bad("INSERT INTO %s does not name tenant_id", table)
			}
		case alias == "":
			bad("a reference to %s without `AS <alias>`", table)
		default:
			tableOf[alias] = table
		}
	}
	eqs, ops, um := identityRegions(stmt)
	for _, u := range um {
		unmodelled(u)
	}
	for _, m := range identityIntoList.FindAllStringSubmatch(stmt, -1) {
		if regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+` + regexp.QuoteMeta(strings.TrimSpace(strings.Split(m[1], ",")[0]))).MatchString(stmt) {
			continue // INSERT INTO <relation>
		}
		for _, n := range strings.Split(m[1], ",") {
			v.writes = append(v.writes, strings.TrimSpace(n))
		}
	}
	identities := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?i)\bpublic\.admin_identities\s+AS\s+(\w+)`).FindAllStringSubmatch(stmt, -1) {
		identities[m[1]] = true
	}
	trustedValue := func(x string) bool {
		if sc.params[x] {
			return true
		}
		if identityParamOrVar.MatchString(x) {
			if sc.assigned[x] {
				unmodelled(fmt.Sprintf("%s, written by :=, used as an anchor", x))
				return false
			}
			return sc.declared[x] && sc.trusted[x]
		}
		return false
	}
	isAnchor := func(x string) bool {
		if trustedValue(x) {
			return true
		}
		rec, col, ok := strings.Cut(x, ".")
		return ok && sc.records[rec] && sc.trusted[rec] && (col == "tenant_id" || col == "id")
	}
	isIdentity := func(x string) bool {
		if trustedValue(x) {
			return true
		}
		rec, col, ok := strings.Cut(x, ".")
		return ok && col == "id" && identities[rec]
	}
	has := func(l, r string) bool {
		for _, p := range eqs {
			if (p[0] == l && p[1] == r) || (p[0] == r && p[1] == l) {
				return true
			}
		}
		return false
	}
	// THE IDENTITY KEY RULE (the file header).
	unkeyed := false
	for _, m := range regexp.MustCompile(`(?i)(INSERT\s+INTO\s+)?\bpublic\.(`+strings.Join(identityPersonTables, "|")+`)\b(\s+AS\s+(\w+))?`).FindAllStringSubmatch(stmt, -1) {
		table, alias := strings.ToLower(m[2]), m[4]
		switch {
		case m[1] != "":
			continue
		case alias == "":
			bad("a reference to %s without `AS <alias>`", table)
			unkeyed = true
			continue
		}
		keyed := false
		for _, p := range eqs {
			for _, side := range [][2]string{{p[0], p[1]}, {p[1], p[0]}} {
				switch side[0] {
				case alias + ".id":
					keyed = keyed || isAnchor(side[1])
				case alias + ".token_hash":
					keyed = keyed || sc.params[side[1]]
				case alias + ".identity_id":
					keyed = keyed || (table != "admin_identities" && isAnchor(side[1]))
				}
			}
		}
		// THE CLAIM'S ADDRESS (identityClaimAddressKeys): an admin_identities alias whose WHERE holds, at
		// the top level, `<alias>.email OPERATOR(public.=) X` with X a trusted VARIABLE -- the operator read
		// back from the statement -- keys it only in a function the map names.
		if !keyed && table == "admin_identities" && identityClaimAddressKeys[fn] > 0 {
			for _, p := range ops {
				for _, side := range [][2]string{{p[0], p[1]}, {p[1], p[0]}} {
					if keyed || side[0] != alias+".email" || !identityParamOrVar.MatchString(side[1]) || sc.params[side[1]] || !trustedValue(side[1]) {
						continue
					}
					if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(alias) + `\.email\s+OPERATOR\s*\(\s*public\s*\.\s*=\s*\)\s*` + regexp.QuoteMeta(side[1]) + `\b`).MatchString(stmt) {
						keyed = true
						v.address++
					}
				}
			}
		}
		if !keyed {
			unkeyed = true
			unmodelled(identityUnkeyedIdentityStatement)
		}
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
				if identityTokenKeyed[table] && side[0] == alias+".token_hash" && sc.params[side[1]] && !anchored[alias] {
					anchored[alias] = true
					v.keyed++
				}
				if table == "admin_users" && side[0] == alias+".identity_id" && isIdentity(side[1]) {
					bounded[alias], selfBounded[alias] = true, true
				}
			}
		}
	}
	// THE NAMED E-MAIL-WIDE READ (the file header's third class): an admin_users alias whose WHERE
	// holds, at the top level, `<alias>.email OPERATOR(public.=) X` with X trusted -- the operator read
	// back from the statement, so `OPERATOR(public.<>)` or any other is no such read.
	emailBounded := map[string]bool{}
	for alias, table := range tableOf {
		if table != "admin_users" {
			continue
		}
		for _, p := range ops {
			for _, side := range [][2]string{{p[0], p[1]}, {p[1], p[0]}} {
				if side[0] != alias+".email" || !trustedValue(side[1]) {
					continue
				}
				if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(alias) + `\.email\s+OPERATOR\s*\(\s*public\s*\.\s*=\s*\)\s*` + regexp.QuoteMeta(side[1]) + `\b`).MatchString(stmt) {
					emailBounded[alias] = true
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
		case tableOf[x] == "admin_users" && tableOf[y] == "password_resets":
			return has(x+".id", y+".admin_user_id")
		case tableOf[x] == "password_resets" && tableOf[y] == "admin_users":
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
				v.wide++
			}
		case emailBounded[alias] && identityEmailWideReads[fn] > 0:
			v.email++
		default:
			bad("%s AS %s is anchored by no tenant (parameter, trusted variable, trusted FOR record), no session key and no identity", table, alias)
		}
	}
	v.passed = len(v.findings) == 0 && !unkeyed
	return v
}

// identityBeltFindings holds every body to the belt. A reference to a tenant relation is either an
// INSERT whose column list names tenant_id, or `public.<relation> AS <alias>` whose alias is
//
//   - ANCHORED: a top-level conjunct `<alias>.tenant_id = X` (`<alias>.id` for tenants) of a WHERE/ON
//     with X a parameter, a TRUSTED variable or a column of a TRUSTED FOR record (identityBeltScope);
//     or a sessions alias found by its own key (identitySessionKeyLookups); or correlated by the
//     tenant key to an anchored alias (a membership and a session: both halves of the composite key);
//     or
//   - in a NAMED TENANT-WIDE READ: an admin_users alias bounded by `identity_id = <trusted value |
//     an admin_identities row's id>`, or correlated by the tenant key to one (identityTenantWideReads).
//
// Everything the scanner does not model is a finding of its own (fail closed). The counts of both
// named classes are pinned per function.
func identityBeltFindings(ctx context.Context, q opQuerier, wide, keyed map[string]int) ([]string, int, error) {
	tenantRelations, err := identityTenantRelations(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	bodies, err := identityBodies(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	tenantRel := map[string]bool{}
	for _, r := range tenantRelations {
		tenantRel[r] = true
	}
	names := strings.Join(tenantRelations, "|")
	identityRef = regexp.MustCompile(`(?i)(INSERT\s+INTO\s+)?\bpublic\.(` + names + `)\b(\s+AS\s+(\w+))?(\s*\(([^)]*)\))?`)
	anyMention := regexp.MustCompile(`(?i)\b(` + names + `)\b`)
	qualified := regexp.MustCompile(`(?i)\bpublic\.(` + names + `)\b`)
	bodyWide := []struct {
		what string
		re   *regexp.Regexp
	}{
		{"a WITH (common table expression)", regexp.MustCompile(`(?i)\bWITH\b`)},
		{"a function in FROM", regexp.MustCompile(`(?i)\b(FROM|JOIN)\s+[A-Za-z_][\w.]*\s*\(`)},
		{"a subquery in FROM", regexp.MustCompile(`(?i)\b(FROM|JOIN)\s*\(`)},
		{"a relation list in FROM", regexp.MustCompile(`(?i)\bFROM\s+[A-Za-z_][\w.]*(\s+AS\s+\w+)?\s*,`)},
		{"a set operation", regexp.MustCompile(`(?i)\b(UNION|EXCEPT|INTERSECT)\b`)},
		{"dynamic SQL", regexp.MustCompile(`(?i)\bEXECUTE\b`)},
		{"a CASE", regexp.MustCompile(`(?i)\bCASE\b`)},
		{"an IS [NOT] DISTINCT FROM", regexp.MustCompile(`(?i)\bDISTINCT\s+FROM\b`)},
	}
	assignRe := regexp.MustCompile(`(\w+)\s*:=\s*([^;]*)`)
	declaredRe := regexp.MustCompile(`\bv_\w+\b`)
	forDecl := regexp.MustCompile(`(?i)\bFOR\s+(\w+)\s+IN\b`)

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
		for _, u := range bodyWide {
			if u.re.MatchString(lx.code) {
				flag(u.what)
			}
		}
		if n, m := len(anyMention.FindAllString(lx.code, -1)), len(qualified.FindAllString(lx.code, -1)); n != m {
			flag(fmt.Sprintf("a tenant relation reached other than as public.<name> (%d mention(s), %d qualified)", n, m))
		}
		sc := identityBeltScope{params: map[string]bool{}, declared: map[string]bool{}, records: map[string]bool{}, trusted: map[string]bool{}, assigned: map[string]bool{}}
		for _, a := range b.Args {
			if strings.HasPrefix(a, "p_") {
				sc.params[a] = true
			}
		}
		code := lx.code
		if up := strings.ToUpper(code); strings.Contains(up, "DECLARE") {
			start := strings.Index(up, "DECLARE")
			if end := strings.Index(up[start:], "BEGIN"); end > 0 {
				for _, v := range declaredRe.FindAllString(code[start:start+end], -1) {
					sc.declared[v] = true
				}
				code = code[:start] + code[start+end:] // the DECLARE section is no statement
			}
		}
		for _, m := range forDecl.FindAllStringSubmatch(code, -1) {
			sc.records[m[1]] = true
		}
		for _, m := range assignRe.FindAllStringSubmatch(code, -1) {
			if rhs := strings.TrimSpace(m[2]); !sc.params[rhs] {
				sc.assigned[m[1]] = true
			}
		}
		stmts := identityStatementSplit.Split(code, -1)
		// V4: trust grows from the parameters, statement by statement, until nothing changes -- a
		// variable (or a FOR record) is trusted once EVERY statement that writes it passed.
		var verdicts []identityStatementVerdict
		for {
			verdicts = verdicts[:0]
			writers := map[string][]bool{}
			for _, s := range stmts {
				v := identityStatement(b.Name, s, tenantRel, sc)
				verdicts = append(verdicts, v)
				for _, w := range v.writes {
					writers[w] = append(writers[w], v.passed)
				}
			}
			grew := false
			for w, passes := range writers {
				if !sc.trusted[w] && !sc.assigned[w] && !slices.Contains(passes, false) {
					sc.trusted[w], grew = true, true
				}
			}
			if !grew {
				break
			}
		}
		wideUsed, keyedUsed, emailUsed, addressUsed := 0, 0, 0, 0
		for _, v := range verdicts {
			out = append(out, v.findings...)
			wideUsed += v.wide
			keyedUsed += v.keyed
			emailUsed += v.email
			addressUsed += v.address
		}
		for _, s := range stmts {
			refs += len(identityRef.FindAllString(s, -1))
		}
		if wideUsed != wide[b.Name] {
			out = append(out, fmt.Sprintf("%s: %d tenant-wide read(s), identityTenantWideReads names %d", b.Name, wideUsed, wide[b.Name]))
		}
		if emailUsed != identityEmailWideReads[b.Name] {
			out = append(out, fmt.Sprintf("%s: %d e-mail-wide read(s), identityEmailWideReads names %d", b.Name, emailUsed, identityEmailWideReads[b.Name]))
		}
		if addressUsed != identityClaimAddressKeys[b.Name] {
			out = append(out, fmt.Sprintf("%s: %d identity lookup(s) by the claim's address, identityClaimAddressKeys names %d", b.Name, addressUsed, identityClaimAddressKeys[b.Name]))
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
//   - E2: the anchor inside a block comment;
//   - E3: a CTE column as the "anchor";
//   - E4: a quoted name (`public."admin_sessions"`);
//   - an anchor inside parentheses with an OR, a spaced qualification, an unqualified tenant table,
//     a subquery in FROM;
//   - V1: a region an inner `IS DISTINCT FROM` used to cut before a later top-level OR;
//   - V2: the same with `CASE ... THEN`;
//   - V3: `o.tenant_id = v_tenant IS NOT TRUE` (an equality that is not the conjunct);
//   - V4: the anchor variable written by `:=`, and written by a statement that names no tenant;
//   - V5: the table behind a view (with a tenant_id column: a tenant relation unanchored; without
//     one: a relation the belt does not know).
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
		revoke      = "public.identity_revoke_sessions(text, boolean)"
		switchFn    = "public.identity_switch_session(text, uuid, text)"
		membership  = "public.identity_memberships(text)"
		openFn      = "public.identity_open_session(uuid, uuid, text, text)"
		signupFn    = "public.identity_create_for_signup(uuid, uuid)"
		acceptFn    = "public.identity_accept_invite(text, text)"
		acceptNewFn = "public.identity_accept_invite_new(text, text)"
		claimFn     = "public.identity_claim_signup(text, uuid, text, text)"
	)
	// M14-08a (00076): the e-mail-wide read is a FOR loop (its rows' addresses are tested in plpgsql).
	emailRead := "    FOR r IN\n        SELECT a.email\n          FROM public.admin_users AS a\n         WHERE a.email OPERATOR(public.=) v_email\n           AND a.id <> p_admin_user_id\n    LOOP"
	switchRevoke := "    UPDATE public.admin_sessions AS o\n       SET revoked_at = pg_catalog.now()\n     WHERE o.id = v_session\n       AND o.tenant_id = v_tenant\n       AND o.revoked_at IS NULL;"
	plainRevoke := "    UPDATE public.admin_sessions AS o\n       SET revoked_at = pg_catalog.now()\n     WHERE "
	viewRevoke := "    UPDATE public.zz_belt_sessions AS o\n       SET revoked_at = pg_catalog.now()\n     WHERE o.id = v_session\n       AND o.revoked_at IS NULL;"
	identityBeltRunControls(t, ctx, tx, []identityBeltControl{
		{name: "the revoke loop's tenant", fn: revoke, cut: "WHERE x.tenant_id = m.tenant_id\n               AND ", put: "WHERE "},
		{name: "the switch's target tenant", fn: switchFn, cut: "WHERE b.tenant_id = p_target_tenant\n       AND ", put: "WHERE "},
		{name: "M5: the membership list without its identity", fn: membership, cut: "WHERE a.identity_id = v_identity\n           AND ", put: "WHERE "},
		{name: "M9: the revoke correlated to itself", fn: revoke,
			cut: "WHERE x.tenant_id = m.tenant_id\n               AND x.admin_user_id = m.id\n",
			put: "FROM public.admin_users AS a\n             WHERE a.id = x.admin_user_id\n               AND a.tenant_id = x.tenant_id\n"},
		{name: "the session joined by half its key", fn: membership,
			cut: "JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id\n     WHERE s.token_hash",
			put: "JOIN public.admin_users AS a ON a.id = s.admin_user_id\n     WHERE s.token_hash"},
		{name: "E1: a top-level OR", fn: revoke, cut: "AND (NOT p_keep_current OR x.id <> v_session)", put: "AND NOT p_keep_current OR x.id <> v_session"},
		{name: "E2: the anchor in a block comment", fn: switchFn, cut: switchRevoke,
			put: plainRevoke + "/* o.tenant_id = v_tenant */ o.id IS NOT NULL\n       AND o.id = v_session\n       AND o.revoked_at IS NULL;"},
		{name: "E3: a CTE column as the anchor", fn: switchFn, cut: switchRevoke,
			put: "    WITH c AS (SELECT i.last_used_tenant AS tenant_id FROM public.admin_identities AS i WHERE i.id = v_identity)\n" +
				"    UPDATE public.admin_sessions AS o\n       SET revoked_at = pg_catalog.now()\n      FROM c\n     WHERE o.id = v_session\n       AND o.tenant_id = c.tenant_id\n       AND o.revoked_at IS NULL;"},
		{name: "E4: a quoted table name", fn: switchFn, cut: switchRevoke,
			put: "    UPDATE public.\"admin_sessions\" AS o\n       SET revoked_at = pg_catalog.now()\n     WHERE o.id = v_session\n       AND o.revoked_at IS NULL;"},
		{name: "the anchor inside parentheses with an OR", fn: switchFn, cut: "       AND o.tenant_id = v_tenant\n       AND o.revoked_at IS NULL;",
			put: "       AND (o.tenant_id = v_tenant OR true)\n       AND o.revoked_at IS NULL;"},
		{name: "a spaced qualification", fn: switchFn, cut: "    UPDATE public.admin_sessions AS o\n", put: "    UPDATE public . admin_sessions AS o\n"},
		{name: "a tenant table without its schema", fn: switchFn, cut: "    UPDATE public.admin_sessions AS o\n", put: "    UPDATE admin_sessions AS o\n"},
		{name: "a subquery in FROM", fn: switchFn, cut: switchRevoke,
			put: "    UPDATE public.admin_sessions AS o\n       SET revoked_at = pg_catalog.now()\n      FROM (SELECT v_tenant AS tenant_id) AS c\n     WHERE o.id = v_session\n       AND o.tenant_id = c.tenant_id\n       AND o.revoked_at IS NULL;"},
		{name: "V1: IS DISTINCT FROM before a top-level OR", fn: switchFn, cut: switchRevoke,
			put: plainRevoke + "o.id IS DISTINCT FROM v_session AND o.tenant_id = v_tenant OR o.id = v_session;"},
		{name: "V2: CASE before a top-level OR", fn: switchFn, cut: switchRevoke,
			put: plainRevoke + "CASE WHEN o.id = v_session THEN true ELSE false END AND o.tenant_id = v_tenant OR true;"},
		{name: "V3: an equality that is not the conjunct", fn: switchFn, cut: switchRevoke,
			put: plainRevoke + "o.id = v_session\n       AND o.tenant_id = v_tenant IS NOT TRUE\n       AND o.revoked_at IS NULL;"},
		{name: "V4: the anchor written by :=", fn: switchFn, cut: switchRevoke, put: "    v_tenant := v_target;\n" + switchRevoke},
		{name: "V4: the anchor written by a statement that names no tenant", fn: switchFn, cut: switchRevoke,
			put: "    SELECT b.tenant_id INTO v_tenant FROM public.admin_users AS b WHERE b.id = v_target;\n" + switchRevoke},
		{name: "V5: a view with a tenant_id", fn: switchFn, cut: switchRevoke, put: viewRevoke,
			pre: []string{`CREATE VIEW public.zz_belt_sessions AS SELECT * FROM public.admin_sessions`}},
		{name: "V5: a view without one", fn: switchFn, cut: switchRevoke, put: viewRevoke,
			pre: []string{`CREATE VIEW public.zz_belt_sessions AS SELECT id, revoked_at FROM public.admin_sessions`}},
		// The identity key rule (round 5): an identity-table UPDATE without its key, and P3 -- a tenant
		// read off an unkeyed identity row and then used as the landing's anchor.
		{name: "the login's identity stamp without its key", fn: openFn, cut: "WHERE i.id = p_identity_id;", put: "WHERE true;"},
		{name: "the switch's last-used hint without its key", fn: switchFn, cut: "WHERE i.id = v_identity;", put: "WHERE true;"},
		// M14-08 (00074): named exception 2's statements, each without what holds it.
		{name: "the signup's row read without its tenant", fn: signupFn,
			cut: "WHERE a.id = p_admin_user_id\n       AND a.tenant_id = p_tenant_id\n       AND a.identity_id IS NULL\n       AND a.unlinked_at IS NULL\n       AND a.status",
			put: "WHERE a.id = p_admin_user_id\n       AND a.identity_id IS NULL\n       AND a.unlinked_at IS NULL\n       AND a.status"},
		{name: "the signup's membership count without its tenant", fn: signupFn,
			cut: "WHERE a.tenant_id = p_tenant_id;", put: "WHERE a.identity_id IS NULL;"},
		{name: "the signup's link without its tenant", fn: signupFn,
			cut: "     WHERE a.id = p_admin_user_id\n       AND a.tenant_id = p_tenant_id\n       AND a.identity_id IS NULL;",
			put: "     WHERE a.id = p_admin_user_id\n       AND a.identity_id IS NULL;"},
		{name: "the e-mail read through another operator", fn: signupFn, cut: emailRead,
			put: strings.Replace(emailRead, "OPERATOR(public.=)", "OPERATOR(public.<>)", 1)},
		{name: "the e-mail read with a top-level OR", fn: signupFn, cut: emailRead,
			put: strings.Replace(emailRead, "        AND a.id <>", "        OR a.id <>", 1)},
		{name: "the e-mail read by a value written by :=", fn: signupFn, cut: emailRead, put: "    v_email := v_digest;\n" + emailRead},
		{name: "a second e-mail read the map does not count", fn: signupFn, cut: emailRead, put: emailRead + "\n        NULL;\n    END LOOP;\n" + emailRead},
		{name: "an e-mail read in a function the map does not name", fn: switchFn, cut: switchRevoke,
			put: "    PERFORM 1 FROM public.admin_users AS z WHERE z.email OPERATOR(public.=) p_new_hash;\n" + switchRevoke},
		// M14-17 (00078): identity_claim_signup's tenant statements, each without what holds it.
		{name: "the claim's membership test without its tenant", fn: claimFn,
			cut: "WHERE a.tenant_id = p_tenant_id;", put: "WHERE a.identity_id IS NULL;"},
		{name: "the claim's business read without its id", fn: claimFn,
			cut: "WHERE t.id = p_tenant_id;", put: "WHERE t.id IS NOT NULL;"},
		{name: "the claim's session joined by half its key", fn: claimFn,
			cut: "JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id",
			put: "JOIN public.admin_users AS a ON a.id = s.admin_user_id"},
		{name: "the claim's owner row without its tenant", fn: claimFn,
			cut: "INSERT INTO public.admin_users (tenant_id, full_name,", put: "INSERT INTO public.admin_users (full_name,"},
		{name: "the claim's draft consumed without its key", fn: claimFn,
			cut: "     WHERE c.id = v_claim\n       AND c.used_at IS NULL;", put: "     WHERE c.used_at IS NULL;"},
		// M14-15 (00077): the acceptances' and the resolver's statements, each without what holds it.
		{name: "the invitation's relink without its tenant", fn: acceptFn,
			cut: "         WHERE a.id = v_row\n           AND a.tenant_id = v_tenant\n", put: "         WHERE a.id = v_row\n"},
		{name: "the address's row in the business read without its tenant", fn: acceptFn,
			cut: "     WHERE a.tenant_id = v_tenant\n       AND a.email OPERATOR(public.=) v_email\n       FOR UPDATE;",
			put: "     WHERE a.email OPERATOR(public.=) v_email\n       FOR UPDATE;"},
		{name: "the invitation spent without its tenant", fn: acceptNewFn,
			cut: "     WHERE v.id = v_invite\n       AND v.tenant_id = v_tenant\n       AND v.used_at IS NULL;",
			put: "     WHERE v.id = v_invite\n       AND v.used_at IS NULL;"},
		{name: "the resolver without the invitation's token", fn: "public.resolve_admin_invite_by_token_hash(text)",
			cut: "WHERE v.token_hash = p_token_hash", put: "WHERE v.used_at IS NULL"},
		{name: "P3: a tenant read off an unkeyed identity row", fn: openFn,
			cut: "WHERE i.id = p_identity_id\n       AND i.password_hash = p_verified_digest\n       FOR UPDATE;",
			put: "WHERE i.password_hash = p_verified_digest\n     LIMIT 1;"},
	})
}

// identityBeltControl is one belt control: the body of fn with cut replaced by put (after the pre
// statements) must turn the belt red.
type identityBeltControl struct {
	name, fn, cut, put string
	pre                []string
}

// identityBeltRunControls runs each control in a savepoint of tx (rolled back with it).
func identityBeltRunControls(t *testing.T, ctx context.Context, tx pgx.Tx, controls []identityBeltControl) {
	t.Helper()
	for _, c := range controls {
		t.Run("control/"+c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				var def string
				if err := sp.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure)`, c.fn).Scan(&def); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(def, c.cut) {
					t.Fatalf("PREMISE: %s's body lacks %q", c.fn, c.cut)
				}
				for _, p := range c.pre {
					aiExec(t, ctx, sp, p)
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

// TestIdentityBodies_TheClaimsPasswordBranchIsHeldByTheBelt is M14-17b's (00080) part of the belt's
// controls, in a test of its own: each control re-reads every identity body, ~4 s apiece under -race on
// CI, and the controls above already use most of opTx's three minutes (measured: the run with these six
// inside it ended at 183 s, past the deadline). The password branch's identity lookup by the claim's
// address (the fourth class, identityClaimAddressKeys) and its "no active membership" test, each without
// what holds it, must turn the belt red.
func TestIdentityBodies_TheClaimsPasswordBranchIsHeldByTheBelt(t *testing.T) {
	ctx, tx := opTx(t)
	const (
		claimFn      = "public.identity_claim_signup(text, uuid, text, text)"
		switchFn     = "public.identity_switch_session(text, uuid, text)"
		switchRevoke = "    UPDATE public.admin_sessions AS o\n       SET revoked_at = pg_catalog.now()\n     WHERE o.id = v_session\n       AND o.tenant_id = v_tenant\n       AND o.revoked_at IS NULL;"
	)
	identityBeltRunControls(t, ctx, tx, []identityBeltControl{
		// M14-17b (00080): the password branch's identity lookup by the claim's address (the fourth class,
		// identityClaimAddressKeys) and its "no active membership" test, each without what holds it.
		{name: "the claim's address key through another operator", fn: claimFn,
			cut: "         WHERE i.email OPERATOR(public.=) v_email\n", put: "         WHERE i.email OPERATOR(public.<>) v_email\n"},
		{name: "the claim's address key by a parameter", fn: claimFn,
			cut: "         WHERE i.email OPERATOR(public.=) v_email\n", put: "         WHERE i.email OPERATOR(public.=) p_session_hash\n"},
		{name: "the claim's address key by a value written by :=", fn: claimFn,
			cut: "        SELECT i.id, i.email, i.password_hash, i.email_verified_at INTO",
			put: "        v_email := v_name;\n        SELECT i.id, i.email, i.password_hash, i.email_verified_at INTO"},
		{name: "a second claim's address key the map does not count", fn: claimFn,
			cut: "        IF NOT FOUND THEN\n            RETURN;\n        END IF;\n        -- Under that lock",
			put: "        PERFORM 1 FROM public.admin_identities AS z WHERE z.email OPERATOR(public.=) v_email;\n        -- Under that lock"},
		{name: "an address key in a function the map does not name", fn: switchFn, cut: switchRevoke,
			put: "    PERFORM 1 FROM public.admin_identities AS z WHERE z.email OPERATOR(public.=) v_identity;\n" + switchRevoke},
		{name: "the claim's membership test without its identity", fn: claimFn,
			cut: "          WHERE a.identity_id = v_identity\n            AND a.status = 'active'", put: "          WHERE a.status = 'active'"},
	})
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
		// (M14-05's real sweep exists from 00072 on; each control replaces it in its savepoint.)
		{"the named sweep", []string{`DROP FUNCTION IF EXISTS public.identity_sweep_events()`, plpgsql("public", "identity_sweep_events",
			"DELETE FROM public.admin_identity_events WHERE created_at < now() - interval '90 days';")}, false},
		{"the named sweep over another table", []string{`DROP FUNCTION IF EXISTS public.identity_sweep_events()`,
			plpgsql("public", "identity_sweep_events", "DELETE FROM public.admin_identity_resets;")}, true},
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
