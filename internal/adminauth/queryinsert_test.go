package adminauth

// queryinsert_test.go -- the INSERT ... SELECT arm of this package's section 4.5 belt (API-5 review,
// 2026-10-15). The same file stands in the five other packages whose query belts read their own calls
// (internal/adminauth, internal/domain/{billing,ledger,review,signup}); internal/domain/tenant's copy
// carries the richer version (it also knows which tables are tenant-scoped).
//
// WHAT IT CHECKS. A WHERE-walking belt checks the SOURCE of `INSERT INTO t (...) SELECT ... FROM src
// WHERE src.tenant_id = @tenant_id`, and nothing checked WHICH value the select list writes into the
// row's tenant_id: `SELECT @other_tenant, ...` behind a perfectly scoped WHERE passed. Here the item in
// tenant_id's position must be @tenant_id or a source row's own scope column (`<alias>.tenant_id`, or
// `<alias>.id` of tenants), with its cast and `AS name` removed. An INSERT ... SELECT with no column
// list cannot be read and is refused; a parenthesised `(SELECT ...)` is read like a bare one.
//
// ⚠️ ITS LIMITS, NAMED: an expression that merely EVALUATES to the right value (CASE, COALESCE) is
// refused rather than understood; a column list that does not name tenant_id is not judged here (this
// copy does not know which tables are tenant-scoped -- RLS's WITH CHECK is the last belt for the
// value).

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

var (
	isInsertSelectRE = regexp.MustCompile(`(?is)\bINSERT\s+INTO\s+(?:public\.)?([a-z_][a-z0-9_]*)(?:\s+AS\s+[a-z_][a-z0-9_]*)?\s*(\(([^)]*)\))?\s*(\(\s*)?SELECT\b`)
	isCastRE         = regexp.MustCompile(`::\s*[a-zA-Z_][a-zA-Z0-9_]*`)
	isAsNameRE       = regexp.MustCompile(`(?is)\s+AS\s+[a-z_][a-z0-9_]*$`)
	isSourceRE       = regexp.MustCompile(`(?is)^FROM\s+(?:ONLY\s+)?(?:public\.)?([a-z_][a-z0-9_]*)\s*(?:AS\s+)?([a-z_][a-z0-9_]*)?`)
)

// insertSelectWrites returns a finding for every INSERT ... SELECT in body whose written tenant_id is
// not the caller's.
func insertSelectWrites(body string) []string {
	var out []string
	for _, loc := range isInsertSelectRE.FindAllStringSubmatchIndex(body, -1) {
		table := strings.ToLower(body[loc[2]:loc[3]])
		if loc[4] < 0 {
			out = append(out, fmt.Sprintf("has an INSERT ... SELECT into %s with no column list; which value "+
				"lands in its tenant_id cannot be read off it", table))
			continue
		}
		cols := isSplitTopLevel(body[loc[6]:loc[7]])
		at := -1
		for i, c := range cols {
			if strings.EqualFold(strings.TrimSpace(c), "tenant_id") {
				at = i
			}
		}
		if at < 0 {
			continue
		}
		rest := body[loc[1]:]
		end, depth := -1, 0
		upper := strings.ToUpper(rest)
		for i := 0; i < len(rest) && end < 0; i++ {
			switch rest[i] {
			case '(':
				depth++
			case ')':
				depth--
			default:
				if depth == 0 && strings.HasPrefix(upper[i:], "FROM") &&
					(i == 0 || !isIdentByte(rest[i-1])) && (i+4 >= len(rest) || !isIdentByte(rest[i+4])) {
					end = i
				}
			}
		}
		if end < 0 {
			out = append(out, fmt.Sprintf("has an INSERT ... SELECT into %s whose source cannot be found", table))
			continue
		}
		items := isSplitTopLevel(rest[:end])
		if len(items) != len(cols) {
			out = append(out, fmt.Sprintf("has an INSERT ... SELECT into %s with %d column(s) and %d select item(s)",
				table, len(cols), len(items)))
			continue
		}
		val := strings.ToLower(strings.Join(strings.Fields(isAsNameRE.ReplaceAllString(
			isCastRE.ReplaceAllString(strings.TrimSpace(items[at]), ""), "")), " "))
		accepted := map[string]bool{"@tenant_id": true}
		if m := isSourceRE.FindStringSubmatch(rest[end:]); m != nil {
			src, alias := strings.ToLower(m[1]), strings.ToLower(m[2])
			switch alias {
			case "where", "join", "left", "inner", "cross", "on", "for", "limit", "order", "group":
				alias = ""
			}
			col := "tenant_id"
			if src == "tenants" {
				col = "id"
			}
			accepted[col], accepted[src+"."+col] = true, true
			if alias != "" {
				accepted[alias+"."+col] = true
			}
		}
		if !accepted[val] {
			out = append(out, fmt.Sprintf("has an INSERT ... SELECT into %s that writes %q into tenant_id: neither "+
				"@tenant_id nor the source row's own scope column. The source's WHERE can be scoped while the row "+
				"is written for another tenant.", table, strings.TrimSpace(items[at])))
		}
	}
	return out
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// isSplitTopLevel splits a comma-separated list outside parentheses and single quotes.
func isSplitTopLevel(s string) []string {
	var out []string
	depth, inQuote, last := 0, false, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'':
			inQuote = !inQuote
		case inQuote:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			out = append(out, s[last:i])
			last = i + 1
		}
	}
	return append(out, s[last:])
}

// TestInsertSelectWrites_TheArmFiresAndStaysQuiet is this arm's negative control.
func TestInsertSelectWrites_TheArmFiresAndStaysQuiet(t *testing.T) {
	for name, sql := range map[string]string{
		"another parameter": `INSERT INTO admin_sessions (tenant_id, admin_user_id) SELECT @other::uuid, a.id FROM admin_users a WHERE a.tenant_id = @tenant_id;`,
		"parenthesised":     `INSERT INTO admin_sessions (tenant_id, admin_user_id) (SELECT @other::uuid, a.id FROM admin_users a WHERE a.tenant_id = @tenant_id);`,
		"no column list":    `INSERT INTO admin_sessions SELECT a.tenant_id, a.id FROM admin_users a WHERE a.tenant_id = @tenant_id;`,
		"another column":    `INSERT INTO admin_sessions (admin_user_id, tenant_id) SELECT a.id, a.id FROM admin_users a WHERE a.tenant_id = @tenant_id;`,
	} {
		if len(insertSelectWrites(sql)) == 0 {
			t.Errorf("%s: the arm reported nothing.\nSQL: %s", name, sql)
		}
	}
	for name, sql := range map[string]string{
		"the source row's tenant": `INSERT INTO admin_sessions (tenant_id, admin_user_id) SELECT a.tenant_id, a.id FROM admin_users a WHERE a.tenant_id = @tenant_id;`,
		"the parameter":           `INSERT INTO admin_sessions (admin_user_id, tenant_id) SELECT a.id, @tenant_id::uuid FROM admin_users a WHERE a.tenant_id = @tenant_id;`,
		"the tenant row":          `INSERT INTO locations (tenant_id, name) SELECT t.id, @name FROM tenants t WHERE t.id = @tenant_id ON CONFLICT DO NOTHING;`,
	} {
		if f := insertSelectWrites(sql); len(f) > 0 {
			t.Errorf("%s: the arm flagged a correctly scoped statement: %v", name, f)
		}
	}
}
