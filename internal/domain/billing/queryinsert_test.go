package billing

// queryinsert_test.go -- the INSERT ... SELECT arm of this package's section 4.5 belt (API-5 review,
// 2026-10-15). The same file stands in all six packages whose query belts read their own calls
// (internal/adminauth, internal/domain/{billing,ledger,review,signup,tenant}); internal/domain/tenant's
// belt reads its heads (insertSelectHeads) with a richer judgement (it also knows which tables are
// tenant-scoped and checks ON CONFLICT ... DO UPDATE).
//
// WHAT IT CHECKS. A WHERE-walking belt checks the SOURCE of `INSERT INTO t (...) SELECT ... FROM src
// WHERE src.tenant_id = @tenant_id`, and nothing checked WHICH value the select list writes into the
// row's tenant_id: `SELECT @other_tenant, ...` behind a perfectly scoped WHERE passed. Here the item in
// tenant_id's position must be @tenant_id or a source row's own scope column (`<alias>.tenant_id`, or
// `<alias>.id` of tenants), with its cast and `AS name` removed. An INSERT ... SELECT with no column
// list cannot be read and is refused; a parenthesised `(SELECT ...)` and a CTE between the column list
// and the SELECT are read like a bare one (fourth round).
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
	isInsertHeadRE = regexp.MustCompile(`(?is)\bINSERT\s+INTO\s+(?:public\.)?([a-z_][a-z0-9_]*)(?:\s+AS\s+[a-z_][a-z0-9_]*)?\s*`)
	isCastRE       = regexp.MustCompile(`::\s*[a-zA-Z_][a-zA-Z0-9_]*`)
	isAsNameRE     = regexp.MustCompile(`(?is)\s+AS\s+[a-z_][a-z0-9_]*$`)
	isSourceRE     = regexp.MustCompile(`(?is)^FROM\s+(?:ONLY\s+)?(?:public\.)?([a-z_][a-z0-9_]*)\s*(?:AS\s+)?([a-z_][a-z0-9_]*)?`)
	isQueryWordRE  = regexp.MustCompile(`(?is)^\s*(SELECT|WITH)\b`)
)

// insertSelectHead is one INSERT ... SELECT: the target, its column list (hasCols false when the
// statement names none) and the text right after the SELECT that feeds the row.
type insertSelectHead struct {
	table   string
	cols    []string
	hasCols bool
	rest    string
}

// insertSelectHeads finds every INSERT ... SELECT in body, in the three spellings the review found
// (fourth round): a bare `SELECT`, a parenthesised `(SELECT ...)`, and a CTE between the column list
// and the SELECT (`WITH z AS (...) SELECT ...`) -- the feeding SELECT is the first one at the
// statement's own depth, so a CTE's inner SELECT is skipped. An INSERT ... VALUES is not one.
func insertSelectHeads(body string) []insertSelectHead {
	var out []insertSelectHead
	for _, loc := range isInsertHeadRE.FindAllStringSubmatchIndex(body, -1) {
		h := insertSelectHead{table: strings.ToLower(body[loc[2]:loc[3]])}
		i := loc[1]
		// A parenthesised span right here is the column list -- unless it opens with SELECT or WITH,
		// which makes it the parenthesised query itself.
		if i < len(body) && body[i] == '(' {
			if isQueryWordRE.MatchString(body[i+1:]) {
				i++
			} else {
				end := strings.IndexByte(body[i:], ')')
				if end < 0 {
					continue
				}
				h.cols, h.hasCols = isSplitTopLevel(body[i+1:i+end]), true
				i += end + 1
				for i < len(body) && (body[i] == ' ' || body[i] == '\t' || body[i] == '\n' || body[i] == '\r') {
					i++
				}
				if i < len(body) && body[i] == '(' && isQueryWordRE.MatchString(body[i+1:]) {
					i++
				}
			}
		}
		// The feeding keyword: the first SELECT or VALUES at the statement's own depth.
		depth, sel := 0, -1
		upper := strings.ToUpper(body)
		for j := i; j < len(body) && sel < 0; j++ {
			switch body[j] {
			case '(':
				depth++
				continue
			case ')':
				depth--
				if depth < 0 {
					j = len(body)
				}
				continue
			case ';':
				j = len(body)
				continue
			}
			if depth != 0 || (j > 0 && isIdentByte(body[j-1])) {
				continue
			}
			if strings.HasPrefix(upper[j:], "VALUES") && (j+6 >= len(body) || !isIdentByte(body[j+6])) {
				break
			}
			if strings.HasPrefix(upper[j:], "SELECT") && (j+6 >= len(body) || !isIdentByte(body[j+6])) {
				sel = j + 6
			}
		}
		if sel < 0 {
			continue
		}
		h.rest = body[sel:]
		out = append(out, h)
	}
	return out
}

// insertSelectWrites returns a finding for every INSERT ... SELECT in body whose written tenant_id is
// not the caller's.
func insertSelectWrites(body string) []string {
	var out []string
	for _, h := range insertSelectHeads(body) {
		table := h.table
		if !h.hasCols {
			out = append(out, fmt.Sprintf("has an INSERT ... SELECT into %s with no column list; which value "+
				"lands in its tenant_id cannot be read off it", table))
			continue
		}
		cols := h.cols
		at := -1
		for i, c := range cols {
			if strings.EqualFold(strings.Trim(strings.TrimSpace(c), `"`), "tenant_id") {
				at = i
			}
		}
		if at < 0 {
			continue
		}
		val, src, ok := isSelectItem(h.rest, at, len(cols))
		if !ok {
			out = append(out, fmt.Sprintf("has an INSERT ... SELECT into %s whose select list cannot be lined up "+
				"with its %d column(s)", table, len(cols)))
			continue
		}
		if !src[val] {
			out = append(out, fmt.Sprintf("has an INSERT ... SELECT into %s that writes %q into tenant_id: neither "+
				"@tenant_id nor the source row's own scope column. The source's WHERE can be scoped while the row "+
				"is written for another tenant.", table, val))
		}
	}
	return out
}

// isSelectItem reads the at-th item of the select list in rest (the text after SELECT) -- cast and
// `AS name` removed, lower-cased -- and the values that count as the caller's tenant for its source.
func isSelectItem(rest string, at, n int) (string, map[string]bool, bool) {
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
		return "", nil, false
	}
	items := isSplitTopLevel(rest[:end])
	if len(items) != n {
		return "", nil, false
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
	return val, accepted, true
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
		"behind a CTE":      `INSERT INTO admin_sessions (tenant_id, admin_user_id) WITH z AS (SELECT a.id FROM admin_users a WHERE a.tenant_id = @tenant_id) SELECT @other::uuid, z.id FROM z;`,
		"a quoted column":   `INSERT INTO admin_sessions ("tenant_id", admin_user_id) SELECT @other::uuid, a.id FROM admin_users a WHERE a.tenant_id = @tenant_id;`,
	} {
		if len(insertSelectWrites(sql)) == 0 {
			t.Errorf("%s: the arm reported nothing.\nSQL: %s", name, sql)
		}
	}
	for name, sql := range map[string]string{
		"the source row's tenant": `INSERT INTO admin_sessions (tenant_id, admin_user_id) SELECT a.tenant_id, a.id FROM admin_users a WHERE a.tenant_id = @tenant_id;`,
		"the parameter":           `INSERT INTO admin_sessions (admin_user_id, tenant_id) SELECT a.id, @tenant_id::uuid FROM admin_users a WHERE a.tenant_id = @tenant_id;`,
		"the tenant row":          `INSERT INTO locations (tenant_id, name) SELECT t.id, @name FROM tenants t WHERE t.id = @tenant_id ON CONFLICT DO NOTHING;`,
		"a VALUES insert":         `INSERT INTO admin_sessions (tenant_id, admin_user_id) VALUES (@tenant_id, (SELECT a.id FROM admin_users a WHERE a.tenant_id = @tenant_id));`,
	} {
		if f := insertSelectWrites(sql); len(f) > 0 {
			t.Errorf("%s: the arm flagged a correctly scoped statement: %v", name, f)
		}
	}
}
