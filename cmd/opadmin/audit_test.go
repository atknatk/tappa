package main

// audit_test.go -- M10 OP-14 D (migration 00033) without a database: where and how each
// subcommand's SQL writes its one operator_audit_log row. The database half is
// audit_db_test.go.

import (
	"bytes"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/db"
)

// auditInsertRE reads the one audit INSERT of a script: its column list, its kind and its id.
var auditInsertRE = regexp.MustCompile(`INSERT INTO public\.operator_audit_log \(([^)]*)\)\n\s*VALUES \('([a-z_]+)', '([0-9a-f-]{36})'::uuid\);\n`)

// auditRowFindings is everything TestSQL_EachActionWritesOneAuditRowInItsDoBlock refuses in
// one script: its audit row is not the one row, in the DO block's inner block right after the
// account's last write (after) and right before the constraint handler, naming exactly the
// kind and the account's id in the column list (kind, target_admin_id).
func auditRowFindings(t *testing.T, sql, kind, id, after string) []string {
	t.Helper()
	var out []string
	if n := strings.Count(sql, "operator_audit_log"); n != 1 {
		out = append(out, "the script names operator_audit_log "+strconv.Itoa(n)+" times (want once)")
	}
	stmts := stmtsOf(t, sql)
	if len(stmts) != 7 || !strings.HasPrefix(stmts[5], "DO "+dollarTag) {
		return append(out, "the script is not the seven statements around one DO block")
	}
	do := stmts[5]
	if !strings.Contains(do, "operator_audit_log") {
		out = append(out, "the audit row is not in the DO block")
	}
	m := auditInsertRE.FindAllStringSubmatch(do, -1)
	if len(m) != 1 {
		return append(out, "the DO block holds "+strconv.Itoa(len(m))+" audit INSERTs of the expected shape (want one)")
	}
	if m[0][1] != "kind, target_admin_id" {
		out = append(out, "the audit INSERT's column list is ("+m[0][1]+"), want (kind, target_admin_id)")
	}
	if m[0][2] != kind || m[0][3] != id {
		out = append(out, "the audit row names the kind "+m[0][2]+" and another id or kind than the script's")
	}
	block := after + "        " + m[0][0] + "    EXCEPTION WHEN integrity_constraint_violation THEN\n"
	if strings.Count(do, block) != 1 {
		out = append(out, "the audit row is not between the account's last write and the inner block's constraint handler")
	}
	return out
}

// TestSQL_EachActionWritesOneAuditRowInItsDoBlock (M10 OP-14 D): each subcommand's script
// names operator_audit_log exactly ONCE, inside its DO block, as
//
//	INSERT INTO public.operator_audit_log (kind, target_admin_id)
//	VALUES ('<the subcommand's kind>', '<the script's account id>'::uuid);
//
// placed right after the account's last write (create: the account's INSERT; reset-mfa and
// disable: the session revocation's row count) and right before the inner block's constraint
// handler -- so the change and its row are one statement, and a constraint that refuses the
// row is reported by name. The column list names neither `at` (the database's trigger stamps
// it; the owner may not choose it) nor detail (the address and the name never reach the log).
// The three kinds are exactly the kinds internal/db's OperatorAuditKind.ByOwner names, kind
// for kind (this binary cannot import internal/db; its test can). CONTROLS: the reader
// refuses a second audit INSERT in the DO block, one outside it, one with `at` in its column
// list, one before the account's write, and one of another kind.
//
// PART II -- red on each of those, on the three spellings drifting from internal/db, and on a
// script whose DO block lost its row. PART III -- These shapes only (no completeness claim).
func TestSQL_EachActionWritesOneAuditRowInItsDoBlock(t *testing.T) {
	for name, pair := range map[string][2]string{
		"create":    {auditKindCreate, string(db.OperatorAuditOperatorCreated)},
		"reset-mfa": {auditKindResetMFA, string(db.OperatorAuditOperatorMFAReset)},
		"disable":   {auditKindDisable, string(db.OperatorAuditOperatorDisabled)},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s writes the kind %q; internal/db names it %q", name, pair[0], pair[1])
		}
	}
	var owner []string
	for _, k := range db.OperatorAuditKinds() {
		if k.ByOwner() {
			owner = append(owner, string(k))
		}
	}
	if want := []string{auditKindCreate, auditKindResetMFA, auditKindDisable}; !slices.Equal(owner, want) {
		t.Errorf("internal/db's ByOwner names %v; opadmin writes %v", owner, want)
	}

	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	c := generateAt(t, now, "create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt")
	r := generateAt(t, now, "reset-mfa", "--id", c.id, "--email", "a@b.test", "--host", "ops.taptime.mt")
	var dOut, dErr bytes.Buffer
	if code := run([]string{"disable", "--id", c.id, "--email", "a@b.test"}, &dOut, &dErr, true, now); code != exitOK {
		t.Fatalf("disable: exit %d", code)
	}
	const afterCreate = "                v_hash, v_now, v_now + interval '30 minutes');\n"
	const afterRevoke = "        GET DIAGNOSTICS v_revoked = ROW_COUNT;\n"
	cases := []struct{ name, sql, kind, after string }{
		{"create", c.sql, auditKindCreate, afterCreate},
		{"reset-mfa", r.sql, auditKindResetMFA, afterRevoke},
		{"disable", dOut.String(), auditKindDisable, afterRevoke},
	}
	for _, tc := range cases {
		for _, f := range auditRowFindings(t, tc.sql, tc.kind, c.id, tc.after) {
			t.Errorf("%s: %s", tc.name, f)
		}
	}

	// CONTROLS: the reader sees each misplacement, on copies of the create script.
	row := "        INSERT INTO public.operator_audit_log (kind, target_admin_id)\n" +
		"        VALUES ('" + auditKindCreate + "', '" + c.id + "'::uuid);\n"
	if !strings.Contains(c.sql, row) {
		t.Fatal("PREMISE: the create script does not hold its row in the expected text")
	}
	for name, broken := range map[string]string{
		"a second row in the DO block": strings.Replace(c.sql, row, row+row, 1),
		"a row outside the DO block":   strings.Replace(strings.Replace(c.sql, row, "", 1), "\nCOMMIT;", "\nINSERT INTO public.operator_audit_log (kind, target_admin_id) VALUES ('"+auditKindCreate+"', '"+c.id+"'::uuid);\nCOMMIT;", 1),
		"at in the column list": strings.Replace(c.sql, "operator_audit_log (kind, target_admin_id)\n        VALUES ('",
			"operator_audit_log (kind, target_admin_id, at)\n        VALUES ('", 1),
		"the row before the account's write": strings.Replace(strings.Replace(c.sql, row, "", 1),
			"        INSERT INTO public.platform_admins\n", row+"        INSERT INTO public.platform_admins\n", 1),
		"another kind": strings.Replace(c.sql, "VALUES ('"+auditKindCreate+"'", "VALUES ('"+auditKindDisable+"'", 1),
	} {
		if broken == c.sql {
			t.Fatalf("CONTROL %q: the edit did not apply", name)
		}
		if len(auditRowFindings(t, broken, auditKindCreate, c.id, afterCreate)) == 0 {
			t.Errorf("CONTROL FAILED: the reader does not refuse %s", name)
		}
	}
}
