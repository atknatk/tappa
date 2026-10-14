package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/store"
)

// m11schema_test.go -- migrations 00037 and 00038 (M11 API-1) against real Postgres, and the
// general catalogue test ADR 0028 §1 (and CLAUDE.md §6, "`arwd` varsayılanına güvenilmez") asks
// for: from M11 on, every NEW table, column and function is held to a written matrix, on a
// database migrated under PRODUCTION's default ACL.

// ---------------------------------------------------------------- the matrix --

// preM11Relations, preM11Columns, preM11Functions and preM11Sequences are the schema's objects
// at migration 00035 -- the day the `arwd` rule became permanent (ADR 0028 §1: "kalıcıdır").
// FROZEN: nothing is ever added here. Anything the catalogue holds beyond them is new, and a new
// object must be in the matrix below or this test is red.
var preM11Relations = []string{
	"admin_sessions", "admin_users", "audit_log", "billing_periods", "departments", "employee_invites",
	"employees", "goose_db_version", "legal_documents", "locations", "operator_audit_log",
	"operator_read_tickets", "password_resets", "platform_admins", "platform_sessions", "policies",
	"policy_attachments", "policy_versions", "sessions", "tags", "tenant_branding", "tenants",
	"transaction_reviews", "transactions",
}

var preM11Columns = map[string]string{
	"admin_sessions":        "id,tenant_id,admin_user_id,token_hash,created_at,last_used_at,revoked_at",
	"admin_users":           "id,tenant_id,full_name,email,password_hash,role,status,created_at,last_login_at",
	"audit_log":             "id,tenant_id,actor_id,action,target,detail,at",
	"billing_periods":       "id,tenant_id,period_month,period_from,period_to,timezone,plan,free_period,employee_count,unstamped_employees,unit_price,currency,amount_due,closed_by,closed_at,created_at",
	"departments":           "id,tenant_id,location_id,name,shift_start,shift_end,overnight,created_at",
	"employee_invites":      "id,tenant_id,employee_id,code_hash,created_at,expires_at,used_at,cancelled_at,consented_at,consent_binding_hash",
	"employees":             "id,tenant_id,location_id,department_id,full_name,role,email,status,invited_at,activated_at,deactivated_at,created_at",
	"goose_db_version":      "id,version_id,is_applied,tstamp",
	"legal_documents":       "id,slug,body,published_at,published_by",
	"locations":             "id,tenant_id,name,static_ips,gps_lat,gps_lng,shift_start,shift_end,overnight,created_at,wifi_ssid",
	"operator_audit_log":    "id,at,kind,session_id,actor_admin_id,target_admin_id,target_tenant_id,target_scope,page_number,page_size,detail",
	"operator_read_tickets": "id,ticket_hash,session_id,kind,target_tenant_id,audit_id,created_at,created_xact,expires_at,consumed_at",
	"password_resets":       "id,tenant_id,admin_user_id,token_hash,created_at,expires_at,used_at,cancelled_at",
	"platform_admins":       "id,email,display_name,status,password_hash,totp_secret_sealed,totp_last_step,totp_failures,totp_locked_until,last_login_at,enroll_token_hash,enroll_issued_at,enroll_expires_at,enroll_used_at,created_at",
	"platform_sessions":     "id,admin_id,token_hash,created_at,mfa_verified_at,last_used_at,revoked_at",
	"policies":              "id,tenant_id,name,layer,enabled,created_at,created_by",
	"policy_attachments":    "id,tenant_id,policy_id,resource,created_at",
	"policy_versions":       "id,tenant_id,policy_id,version_no,document,created_at,created_by",
	"sessions":              "id,tenant_id,employee_id,token_hash,device_info,created_at,last_used_at,revoked_at",
	"tags":                  "uid,tenant_id,location_id,aes_key_ref,last_ctr,status,retired_at,replaced_by,created_at,encoded_at,app_key_ref",
	"tenant_branding":       "tenant_id,accent,logo,logo_sha256,logo_mime,logo_width,logo_height,created_at,updated_at,updated_by",
	"tenants":               "id,name,vat_number,business_type,structure,plan,timezone,created_at,price_per_employee_month,vat_verified,vat_checked_at",
	"transaction_reviews":   "id,tenant_id,transaction_id,reviewer_id,outcome,note,reviewed_at",
	"transactions":          "id,tenant_id,employee_id,location_id,department_id,tag_uid,ctr,type,occurred_at,source_ip,ip_match,gps_lat,gps_lng,gps_match,sun_valid,trust,verdict,note,channel,entered_by,practice,queued,created_at,policy_version_id,matched_sid,policy_layer,policy_context",
}

var preM11Functions = []string{
	"op_begin_read(text,text,jsonb)", "op_close_session(text)",
	"op_complete_enrollment(uuid,text,text,bytea,bigint,text)", "op_open_session(uuid,text,bigint)",
	"op_publish_legal(text,text,text)", "op_read_audit(text,text,text,integer,integer)",
	"op_read_legal_versions(text,text,integer,integer)", "op_read_tenant_billing(text,text,uuid,integer)",
	"op_read_tenant_detail(text,text,uuid)", "op_read_tenant_plaques(text,text,uuid)",
	"op_read_tenant_vat(text,text,uuid)", "op_read_tenants(text,text,text,integer,integer)",
	"op_record_auth_event(text,text,uuid)", "op_record_vat_check(text,uuid,text,boolean)",
	"op_touch_session(text)", "resolve_admin_by_email(citext)", "resolve_admin_session_by_token_hash(text)",
	"resolve_invite_by_code_hash(text)", "resolve_password_reset_by_token_hash(text)",
	"resolve_session_by_token_hash(text)", "resolve_tag_by_uid(character)",
	"tappa_audit_at_is_the_wall_clock()", "tappa_billing_amount_due(boolean,numeric,integer)",
	"tappa_check_transaction_review()",
	"tappa_employee_is_billable(text,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone)",
	"tappa_employee_lifecycle_status(timestamp with time zone,timestamp with time zone)",
	"tappa_first_chargeable_month(text,timestamp with time zone,text)", "tappa_forbid_app_key_ref_rewrite()",
	"tappa_forbid_counter_rewind()", "tappa_forbid_encoded_at_at_insert()", "tappa_forbid_encoded_at_rewrite()",
	"tappa_forbid_mutation()", "tappa_forbid_revocation_reset()", "tappa_forbid_unencoded_activation()",
	"tappa_local_month_start(date,text)",
}

var preM11Sequences = []string{"goose_db_version_id_seq"}

// m11Roles are the roles every matrix row is read for. A role absent from a row holds NOTHING
// on that object.
var m11Roles = []string{"tappa_app", "tappa_operator", "tappa_opdefiner", "tappa_resolver", "tappa_holdingdefiner"}

// m11TableACL is one role's whole reach on one table: the columns per verb (attnum order, ""
// for none) and the table-level verbs.
type m11TableACL struct {
	Select, Insert, Update                string
	Delete, Truncate, References, Trigger bool
}

// m11Tables is every table born at or after migration 00037, role by role.
var m11Tables = map[string]map[string]m11TableACL{
	// 00037 (K-17's queue): the worker reads it, a request is queued by naming the employee
	// (the queue's order, the lease and the tries start at their defaults), the lease and the
	// try count move, and a sent request leaves (DELETE). No column for a code exists at all.
	"invite_requests": {
		"tappa_app": {
			Select: "tenant_id,employee_id,requested_at,lease_until,attempts",
			Insert: "tenant_id,employee_id",
			Update: "lease_until,attempts",
			Delete: true,
		},
	},
	// 00041/00042 (M11 API-1a, ADR 0027 §1, §3): the holding tables -- TENANT-SCOPE-LESS
	// (m11ScopelessTables). tappa_app holds nothing (ADR 0028 §1's row: "hiçbir fiil"); the op_*
	// of 00042 write and read them through tappa_opdefiner's column grants; tappa_holdingdefiner
	// holds ADR 0027 §3's holding_members row -- SELECT on the four membership columns, UPDATE on
	// lock_anchor alone -- and nothing on holdings.
	"holdings": {
		"tappa_opdefiner": {Select: "id,name,created_at", Insert: "name"},
	},
	"holding_members": {
		"tappa_opdefiner": {
			Select: "holding_id,member_tenant_id,joined_at,left_at",
			Insert: "holding_id,member_tenant_id",
			Update: "left_at",
		},
		"tappa_holdingdefiner": {
			Select: "holding_id,member_tenant_id,joined_at,left_at",
			Update: "lock_anchor",
		},
	},
}

// m11ScopelessTables are the matrix's tenant-scope-less tables (CLAUDE.md §6): ENABLE and FORCE
// row level security like every other, and NO policy at all -- no role that obeys RLS reads or
// writes them (a policy would be a door for somebody); the BYPASSRLS definers reach them through
// their column grants alone. Each carries its redline R5 waiver
// (TestHoldingSchema_AppRoleHasNoVerbOnHoldingTables reads the waivers).
var m11ScopelessTables = map[string]bool{"holdings": true, "holding_members": true}

// m11Column is one column added at or after 00037 to a table that existed before, with each
// role's verbs on it ("S", "I", "U" in that order; "" for nothing).
type m11Column struct {
	table, column string
	verbs         map[string]string
}

var m11Columns = []m11Column{
	// 00037. employees and locations hand tappa_app table-level SELECT/INSERT/UPDATE (00003,
	// 00002), which a new column falls under -- and the product writes these columns (the
	// panel's external ref; API-5/6). updated_at is writable but OWNED by its trigger:
	// TestEmployeeUpdatedAt_OnlyDirectoryColumnsOnEveryUpdatePath shows a written value is
	// overruled.
	{"employees", "external_ref", map[string]string{"tappa_app": "SIU"}},
	{"employees", "employee_code", map[string]string{"tappa_app": "SIU"}},
	{"employees", "first_name", map[string]string{"tappa_app": "SIU"}},
	{"employees", "last_name", map[string]string{"tappa_app": "SIU"}},
	{"employees", "updated_at", map[string]string{"tappa_app": "SIU"}},
	// 00043 (M11 API-1b): tappa_holdingdefiner reads a peer's venue ref (holding_tap_site).
	{"locations", "external_ref", map[string]string{"tappa_app": "SIU", "tappa_holdingdefiner": "S"}},
	{"locations", "active", map[string]string{"tappa_app": "SIU"}},
	// 00038/00039 (K-22): tappa_app READS the gate and cannot write it; the one writer is the
	// operator's definer.
	{"tenants", "api_enabled_at", map[string]string{"tappa_app": "S", "tappa_opdefiner": "SU"}},
	// 00042 (M11 API-1a): tappa_opdefiner READS the code too -- the code rule in both directions
	// (op_add_holding_member, op_enable_tenant_api) and op_read_holdings.
	// 00043 (M11 API-1b): and tappa_holdingdefiner reads the site's code (holding_tap_site).
	{"tenants", "api_code", map[string]string{"tappa_app": "S", "tappa_opdefiner": "SU", "tappa_holdingdefiner": "S"}},
	// 00043 (M11 API-1b, ADR 0027 §4): the site of a tap on a peer's plaque. transactions hands
	// tappa_app table-level SELECT and INSERT (00005) and nothing else, which the new columns
	// fall under -- the tap path writes them (checkin), never updates them (§4.3).
	// tappa_holdingdefiner holds nothing on transactions: the gate reads NEW, which needs no
	// privilege; the matrix's transactions row is API-1c's/API-2's to earn.
	{"transactions", "site_tenant_id", map[string]string{"tappa_app": "SI"}},
	{"transactions", "site_location_id", map[string]string{"tappa_app": "SI"}},
	{"transactions", "site_tag_uid", map[string]string{"tappa_app": "SI"}},
}

// m11FunctionACL is one function's owner, SECURITY DEFINER flag and EXACT EXECUTE grantees
// (sorted, "PUBLIC" for the pseudo-role).
type m11FunctionACL struct {
	owner   string
	definer bool
	execute string
}

var m11Functions = map[string]m11FunctionACL{
	// 00037 (K-21): a trigger function -- EXECUTE in the owner alone (nobody calls it; a
	// trigger fires without an EXECUTE check), and its body asserts its table
	// (TestM11Catalog_EveryNewTableMatchesTheMatrixUnderArwd's binding arm).
	"tappa_employees_touch_updated_at()": {owner: "tappa_owner", execute: "tappa_owner"},
	// 00039 (K-22): op_* of ADR 0021's class.
	"op_enable_tenant_api(text,uuid,text)": {owner: "tappa_opdefiner", definer: true, execute: "tappa_opdefiner,tappa_operator"},
	"op_disable_tenant_api(text,uuid)":     {owner: "tappa_opdefiner", definer: true, execute: "tappa_opdefiner,tappa_operator"},
	// 00041 (M11 API-1a, ADR 0027 §2): the closed list's two members born here. holding_peer is
	// SECURITY INVOKER and EXECUTE in its owner alone; the timezone lock is a trigger function,
	// EXECUTE in its owner alone (HoldingCatalog_TheFunctionsAndTheirExactSignatures pins them
	// further).
	"holding_peer(uuid,uuid,text)": {owner: "tappa_holdingdefiner", execute: "tappa_holdingdefiner"},
	"holding_timezone_lock()":      {owner: "tappa_holdingdefiner", definer: true, execute: "tappa_holdingdefiner"},
	// 00042 (M11 API-1a): op_* of ADR 0021's class.
	"op_create_holding(text,text)":                {owner: "tappa_opdefiner", definer: true, execute: "tappa_opdefiner,tappa_operator"},
	"op_add_holding_member(text,uuid,uuid)":       {owner: "tappa_opdefiner", definer: true, execute: "tappa_opdefiner,tappa_operator"},
	"op_remove_holding_member(text,uuid,uuid)":    {owner: "tappa_opdefiner", definer: true, execute: "tappa_opdefiner,tappa_operator"},
	"op_read_holdings(text,text,integer,integer)": {owner: "tappa_opdefiner", definer: true, execute: "tappa_opdefiner,tappa_operator"},
	// 00043 (M11 API-1b, ADR 0027 §2): the tap path's three callables -- EXECUTE to tappa_app
	// alone -- and the site gate, a trigger function, EXECUTE in its owner alone.
	"holding_tap_site(character)":                    {owner: "tappa_holdingdefiner", definer: true, execute: "tappa_app,tappa_holdingdefiner"},
	"holding_advance_tag_counter(character,integer)": {owner: "tappa_holdingdefiner", definer: true, execute: "tappa_app,tappa_holdingdefiner"},
	"holding_logo(character,text)":                   {owner: "tappa_holdingdefiner", definer: true, execute: "tappa_app,tappa_holdingdefiner"},
	"holding_site_gate()":                            {owner: "tappa_holdingdefiner", definer: true, execute: "tappa_holdingdefiner"},
}

// m11DefaultACLStatement is production's pg_default_acl, which m11DefaultACLInit gives every
// fresh development and CI volume before the first migration -- so the catalogue below is the
// one production builds. The test holds the file to this text.
const m11DefaultACLStatement = `ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO tappa_app`

// m11DefaultACLInit is the init script that runs m11DefaultACLStatement on an empty PGDATA.
const m11DefaultACLInit = "scripts/db-init/03-dev-production-default-acl.sql"

// m11Witness is the catalogue test's premise, read by m11ArwdWitness.
type m11Witness struct {
	// wide: the default ACL tappa_owner's new tables in public get hands tappa_app UPDATE and
	// DELETE -- production's `arwd`, not 01-roles.sql's narrow `ar`.
	wide bool
	// beforeTheChain: that default was last written BEFORE goose recorded its first ledger row,
	// i.e. before the first migration ran -- so every table of the chain was born under it.
	beforeTheChain bool
	why            string
}

// m11ArwdWitness reads the witness of `arwd` from two facts a REVOKE on a table cannot move
// (migration 00040, M11 API-1a). Until 00040 the witness was tappa_app's UPDATE and DELETE on
// goose_db_version -- the table goose creates before the first migration, under whatever
// default the database had then; 00040 takes every privilege on the ledger away from tappa_app
// (INSERT alone let a stolen application credential mark a future migration as applied), so
// that table proves nothing any more. What it proved is read directly instead:
//
//   - pg_default_acl's row for tappa_owner's tables in public grants tappa_app UPDATE and DELETE
//     (the default is wide), and
//   - that row's last write is OLDER than goose_db_version's first row: age(xmin) of the default's
//     catalogue row is larger than age(xmin) of the ledger's version-0 row, which goose inserts in
//     the transaction that creates the table. A default widened AFTER the chain ran (by hand, on
//     an old narrow volume) is a newer write than that row, and the tables born before it did
//     not get it -- the case the old witness caught, caught again.
//
// age() counts transactions back from the current one modulo the 2^31 horizon, so the order is
// sound on any database younger than two billion transactions -- every development and CI volume
// this test meets. TestM11Catalog_TheWitnessReadsTheDefaultAndItsOrder drives both halves.
func m11ArwdWitness(t *testing.T, ctx context.Context, q opQuerier) m11Witness {
	t.Helper()
	var (
		w                  m11Witness
		found              bool
		update, del        bool
		defaultAge, ledger *int64
	)
	if err := q.QueryRow(ctx, `
		SELECT count(*) > 0,
		       coalesce(bool_or(a.privilege_type = 'UPDATE' AND a.grantee = 'tappa_app'::regrole), false),
		       coalesce(bool_or(a.privilege_type = 'DELETE' AND a.grantee = 'tappa_app'::regrole), false),
		       max(age(d.xmin))::bigint
		  FROM pg_default_acl d
		  LEFT JOIN LATERAL aclexplode(d.defaclacl) AS a ON true
		 WHERE d.defaclrole = 'tappa_owner'::regrole
		   AND d.defaclnamespace = 'public'::regnamespace
		   AND d.defaclobjtype = 'r'`).Scan(&found, &update, &del, &defaultAge); err != nil {
		t.Fatalf("read the default ACL: %v", err)
	}
	if err := q.QueryRow(ctx, `SELECT age(g.xmin)::bigint FROM public.goose_db_version g ORDER BY g.id LIMIT 1`).Scan(&ledger); err != nil {
		t.Fatalf("read goose's first ledger row: %v", err)
	}
	w.wide = found && update && del
	w.beforeTheChain = defaultAge != nil && ledger != nil && *defaultAge > *ledger
	switch {
	case !found:
		w.why = "pg_default_acl holds no default for tappa_owner's tables in public"
	case !w.wide:
		w.why = fmt.Sprintf("the default for tappa_owner's tables in public gives tappa_app UPDATE=%v DELETE=%v (narrow)", update, del)
	case !w.beforeTheChain:
		w.why = "the wide default for tappa_owner's tables in public was written AFTER goose recorded its first ledger row " +
			"(age of the default's row is not larger than the ledger's), so the tables the chain created before it did not get it"
	}
	return w
}

// TestM11Catalog_TheWitnessReadsTheDefaultAndItsOrder drives m11ArwdWitness's two halves on
// this database, each inside a savepoint that is rolled back: a REVOKE of UPDATE and DELETE
// from the default turns `wide` false; the same default granted again (in this transaction,
// i.e. AFTER goose's first ledger row) keeps `wide` and turns `beforeTheChain` false -- the
// old volume widened by hand, which must not pass. The live answer is only reported (it is the
// catalogue test's to judge): a narrow volume must still be able to run this test.
func TestM11Catalog_TheWitnessReadsTheDefaultAndItsOrder(t *testing.T) {
	ctx, tx := opTx(t)
	live := m11ArwdWitness(t, ctx, tx)
	t.Logf("this database: wide=%v beforeTheChain=%v %s", live.wide, live.beforeTheChain, live.why)
	for _, c := range []struct {
		what       string
		stmts      []string
		wide, prev bool
	}{
		{"the default narrowed", []string{
			`ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public REVOKE UPDATE, DELETE ON TABLES FROM tappa_app`},
			false, false},
		{"the default widened after the chain", []string{
			`ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public REVOKE UPDATE, DELETE ON TABLES FROM tappa_app`,
			`ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO tappa_app`},
			true, false},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range c.stmts {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("%s: %v", c.what, err)
			}
		}
		if w := m11ArwdWitness(t, ctx, sp); w.wide != c.wide || w.beforeTheChain != c.prev || w.why == "" {
			t.Errorf("%s: witness wide=%v beforeTheChain=%v (%q), want wide=%v beforeTheChain=%v and a reason",
				c.what, w.wide, w.beforeTheChain, w.why, c.wide, c.prev)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// TestM11Catalog_EveryNewTableMatchesTheMatrixUnderArwd is ADR 0028 §1's general catalogue
// test:
//
//   - PREMISE, the witness of `arwd` (m11ArwdWitness, since 00040): the default ACL hands
//     tappa_app UPDATE and DELETE on tappa_owner's new tables, and it was set before goose
//     recorded its first ledger row -- so the WHOLE migration chain ran under production's wide
//     default, and a REVOKE a new migration forgot shows up below as an extra verb. Under the
//     narrow default (`ar`, 01-roles.sql alone) the missing REVOKE would hide and this test
//     would be green for the wrong reason, so it does not judge there: red in CI, where every
//     volume is fresh and a narrow one is a broken build; SKIPPED elsewhere -- not a pass --,
//     where it is a volume built before m11DefaultACLInit existed, and says how to rebuild it;
//   - every relation, column, function and sequence the catalogue holds is either in the frozen
//     pre-M11 baseline or in the matrix -- a new one in neither is red;
//   - every table, column and function of the matrix holds EXACTLY its row, for every role
//     (column verbs by has_column_privilege, table verbs by has_table_privilege, EXECUTE by
//     aclexplode -- so PUBLIC is seen), and a new table has ENABLE + FORCE row level security and
//     a policy;
//   - the trigger function refuses to run bound to any other table (TG_RELID), and tappa_app
//     cannot even bind it (no EXECUTE).
func TestM11Catalog_EveryNewTableMatchesTheMatrixUnderArwd(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", m11DefaultACLInit))
	if err != nil {
		t.Fatalf("read %s: %v", m11DefaultACLInit, err)
	}
	var statements []string
	for _, line := range strings.Split(string(script), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			statements = append(statements, line)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(strings.Join(statements, " ")), " "), m11DefaultACLStatement+";") {
		t.Fatalf("%s does not run %s; -- the database this test judges is not built the way it says", m11DefaultACLInit, m11DefaultACLStatement)
	}

	ctx, tx := opTx(t)

	if w := m11ArwdWitness(t, ctx, tx); !w.wide || !w.beforeTheChain {
		// CI (GitHub Actions sets CI) builds every database on a fresh volume, so a narrow one there
		// is a broken build. Anywhere else it is a volume older than m11DefaultACLInit -- init runs on
		// an empty PGDATA only -- and red would fail `make check` on the database, not on a defect.
		report := t.Skipf
		if os.Getenv("CI") != "" {
			report = t.Fatalf
		}
		report("this database was NOT migrated under production's default ACL: %s. Under the narrow default a "+
			"forgotten REVOKE ALL is invisible, so this test proves nothing here -- THIS IS NOT A PASS. %s gives a "+
			"FRESH volume production's default: rebuild the compose database with `make db-reset`, or build any other "+
			"from an empty PGDATA with scripts/db-init mounted (or run, as tappa_owner, BEFORE its first migrate\n  %s;)",
			w.why, m11DefaultACLInit, m11DefaultACLStatement)
	}

	// The relations and their columns.
	rows, err := tx.Query(ctx, `
		SELECT c.relname, string_agg(a.attname, ',' ORDER BY a.attnum)
		  FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		 WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
		 GROUP BY c.relname ORDER BY c.relname`)
	if err != nil {
		t.Fatal(err)
	}
	columns := map[string][]string{}
	for rows.Next() {
		var rel, cols string
		if err := rows.Scan(&rel, &cols); err != nil {
			t.Fatal(err)
		}
		columns[rel] = strings.Split(cols, ",")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(columns) < len(preM11Relations) {
		t.Fatalf("anti-vacuity: %d relations in public, the baseline alone is %d", len(columns), len(preM11Relations))
	}
	newColumn := map[string]bool{}
	for _, c := range m11Columns {
		newColumn[c.table+"."+c.column] = true
	}
	for rel, cols := range columns {
		base, isOld := preM11Columns[rel]
		_, isNew := m11Tables[rel]
		switch {
		case isOld && isNew:
			t.Errorf("%s is in the frozen baseline AND in the matrix", rel)
		case !isOld && !isNew:
			t.Errorf("%s is a table born after 00035 and is not in m11Tables: write its whole reach, role by role, "+
				"in the matrix (ADR 0028 §1) -- REVOKE ALL from tappa_app, then only the verbs and columns it needs", rel)
		case isOld:
			old := strings.Split(base, ",")
			for _, col := range cols {
				if !slices.Contains(old, col) && !newColumn[rel+"."+col] {
					t.Errorf("%s.%s is a column added after 00035 and is not in m11Columns: write each role's verbs on it", rel, col)
				}
			}
		}
	}
	for _, c := range m11Columns {
		if !slices.Contains(columns[c.table], c.column) {
			t.Errorf("m11Columns names %s.%s, which the database does not hold", c.table, c.column)
		}
	}

	// Each new table: every role's reach, and the RLS five.
	verbs := func(role, table string) m11TableACL {
		t.Helper()
		var acl m11TableACL
		acl.Select = opColumns(t, ctx, tx, role, table, "SELECT")
		acl.Insert = opColumns(t, ctx, tx, role, table, "INSERT")
		acl.Update = opColumns(t, ctx, tx, role, table, "UPDATE")
		if err := tx.QueryRow(ctx, `SELECT has_table_privilege($1, 'public.' || $2, 'DELETE'), has_table_privilege($1, 'public.' || $2, 'TRUNCATE'),
		                                   has_table_privilege($1, 'public.' || $2, 'REFERENCES'), has_table_privilege($1, 'public.' || $2, 'TRIGGER')`,
			role, table).Scan(&acl.Delete, &acl.Truncate, &acl.References, &acl.Trigger); err != nil {
			t.Fatalf("table privileges of %s on %s: %v", role, table, err)
		}
		return acl
	}
	for table, byRole := range m11Tables {
		for _, role := range m11Roles {
			if got, want := verbs(role, table), byRole[role]; got != want {
				t.Errorf("%s on %s holds %+v, want %+v", role, table, got, want)
			}
		}
		var enabled, forced bool
		if err := tx.QueryRow(ctx, `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = ('public.' || $1)::regclass`, table).
			Scan(&enabled, &forced); err != nil {
			t.Fatal(err)
		}
		policies := opInt(t, ctx, tx, `SELECT count(*) FROM pg_policy WHERE polrelid = ('public.' || $1)::regclass`, table)
		switch {
		case m11ScopelessTables[table] && (!enabled || !forced || policies != 0):
			t.Errorf("%s (tenant-scope-less): ENABLE=%v FORCE=%v and %d policies; want ENABLE, FORCE and NO policy (CLAUDE.md §6)", table, enabled, forced, policies)
		case !m11ScopelessTables[table] && (!enabled || !forced || policies == 0):
			t.Errorf("%s: ENABLE=%v FORCE=%v and a policy are all required (CLAUDE.md §6)", table, enabled, forced)
		}
	}

	// Each new column on an old table: every role's verbs.
	for _, c := range m11Columns {
		for _, role := range m11Roles {
			got := ""
			for _, v := range []struct{ letter, priv string }{{"S", "SELECT"}, {"I", "INSERT"}, {"U", "UPDATE"}} {
				var may bool
				if err := tx.QueryRow(ctx, `SELECT has_column_privilege($1, 'public.' || $2, $3, $4)`, role, c.table, c.column, v.priv).Scan(&may); err != nil {
					t.Fatalf("has_column_privilege(%s, %s.%s, %s): %v", role, c.table, c.column, v.priv, err)
				}
				if may {
					got += v.letter
				}
			}
			if want := c.verbs[role]; got != want {
				t.Errorf("%s on %s.%s holds %q, want %q", role, c.table, c.column, got, want)
			}
		}
	}

	// The functions: none outside the baseline and the matrix; each of the matrix exactly.
	fnRows, err := tx.Query(ctx, `
		SELECT p.oid::regprocedure::text, pg_get_userbyid(p.proowner), p.prosecdef,
		       coalesce((SELECT string_agg(CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END, ','
		                                    ORDER BY CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END)
		                   FROM aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) AS a
		                  WHERE a.privilege_type = 'EXECUTE'), '')
		  FROM pg_proc p
		 WHERE p.pronamespace = 'public'::regnamespace
		   AND NOT EXISTS (SELECT 1 FROM pg_depend d
		                    WHERE d.classid = 'pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e')`)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for fnRows.Next() {
		var sig, owner, execute string
		var definer bool
		if err := fnRows.Scan(&sig, &owner, &definer, &execute); err != nil {
			t.Fatal(err)
		}
		seen[sig] = true
		want, isNew := m11Functions[sig]
		switch {
		case slices.Contains(preM11Functions, sig) && isNew:
			t.Errorf("%s is in the frozen baseline AND in the matrix", sig)
		case slices.Contains(preM11Functions, sig):
		case !isNew:
			t.Errorf("%s is a function born after 00035 and is not in m11Functions: REVOKE ALL ON FUNCTION ... FROM PUBLIC "+
				"and write its owner and its EXACT EXECUTE grantees in the matrix (ADR 0028 §1)", sig)
		default:
			got := m11FunctionACL{owner: owner, definer: definer, execute: execute}
			if got != want {
				t.Errorf("%s is %+v, want %+v", sig, got, want)
			}
		}
	}
	if err := fnRows.Err(); err != nil {
		t.Fatal(err)
	}
	for sig := range m11Functions {
		if !seen[sig] {
			t.Errorf("m11Functions names %s, which the database does not hold", sig)
		}
	}

	// Sequences: none new (a sequence carries the default `rU` a table REVOKE does not reach).
	var seqs []string
	sqRows, err := tx.Query(ctx, `SELECT relname FROM pg_class WHERE relnamespace = 'public'::regnamespace AND relkind = 'S' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	seqs, err = pgx.CollectRows(sqRows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range seqs {
		if !slices.Contains(preM11Sequences, s) {
			t.Errorf("sequence %s is new; give it its own REVOKE ALL ON SEQUENCE and a row here", s)
		}
	}

	// The trigger function's binding: another table refuses at fire time (TG_RELID), and
	// tappa_app cannot bind it at all.
	for _, s := range []string{
		`CREATE TEMP TABLE zz_m11_touch (id int, updated_at timestamptz)`,
		`CREATE TRIGGER zz_m11_touch_bound BEFORE INSERT ON zz_m11_touch FOR EACH ROW EXECUTE FUNCTION public.tappa_employees_touch_updated_at()`,
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("bind the trigger function elsewhere: %v", err)
		}
	}
	opWant(t, opTry(t, ctx, tx, `INSERT INTO zz_m11_touch (id) VALUES (1)`), sqlstatePrerequisiteState,
		"the trigger function fired on a table that is not employees")
	opWant(t, opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `CREATE TEMP TABLE zz_m11_app (id int, updated_at timestamptz)`); err != nil {
			return err
		}
		_, err := sp.Exec(ctx, `CREATE TRIGGER zz_m11_app_bound BEFORE INSERT ON zz_m11_app FOR EACH ROW EXECUTE FUNCTION public.tappa_employees_touch_updated_at()`)
		return err
	}), sqlstateInsufficientPrivi, "tappa_app binding the trigger function to a table of its own")
}

// ----------------------------------------------------------- the fixtures --

// m11Tenant commits a tenant and one venue as tappa_app in its own context.
func m11Tenant(t *testing.T, d *DB) (tenantID, venue uuid.UUID) {
	t.Helper()
	tenantID, venue = uuid.New(), uuid.New()
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
		                          VALUES ($1, 'm11 fixture', $2, 'restaurant', 'multi')`, tenantID, "VAT-M11-"+tenantID.String()); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'm11 venue')`, venue, tenantID)
		return e
	})
	if err != nil {
		t.Fatalf("m11 tenant: %v", err)
	}
	return tenantID, venue
}

// m11Exec runs one statement as tappa_app in tenantID's context, committed.
func m11Exec(t *testing.T, d *DB, tenantID uuid.UUID, sql string, args ...any) error {
	t.Helper()
	return d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql, args...)
		return err
	})
}

func m11Constraint(err error) (code, name string) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code, pg.ConstraintName
	}
	return "", ""
}

// ------------------------------------------------------------------ K-24 --

// TestEmployeeEmail_UniqueAmongActiveOnly is K-24 (ADR 0028 §9): the index keeps its NAME
// (internal/domain/tenant recognises the violation by it -- a refusal with a sentence on the
// panel, never a 500) and now counts LIVE rows only. Two live rows cannot share an address
// (whatever its case); a deactivated row's address can be taken by a new live row; two
// deactivated rows may share one; and a second live row still collides.
func TestEmployeeEmail_UniqueAmongActiveOnly(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)
	var def string
	if err := d.pool.QueryRow(context.Background(),
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'employees_tenant_email_key'`).Scan(&def); err != nil {
		t.Fatalf("the index employees_tenant_email_key is gone: %v", err)
	}
	if want := `CREATE UNIQUE INDEX employees_tenant_email_key ON public.employees USING btree (tenant_id, email) WHERE ((email IS NOT NULL) AND (status <> 'deactivated'::text))`; def != want {
		t.Errorf("employees_tenant_email_key is\n %s\nwant\n %s", def, want)
	}

	tenantID, venue := m11Tenant(t, d)
	address := "Maria." + strings.ToUpper(uuid.NewString()[:8]) + "@k24.example.test"
	hire := func(status, email string) (uuid.UUID, error) {
		id := uuid.New()
		return id, m11Exec(t, d, tenantID, `INSERT INTO employees (id, tenant_id, location_id, full_name, status, email)
		                                    VALUES ($1, $2, $3, 'k24 person', $4, $5)`, id, tenantID, venue, status, email)
	}
	first, err := hire("active", address)
	if err != nil {
		t.Fatalf("the first live holder: %v", err)
	}
	_, err = hire("invited", strings.ToLower(address))
	if code, name := m11Constraint(err); code != "23505" || name != "employees_tenant_email_key" {
		t.Fatalf("a second LIVE row with the address (other case) answered %q on %q (%v), want 23505 on employees_tenant_email_key", code, name, err)
	}
	if err := m11Exec(t, d, tenantID, `UPDATE employees SET status = 'deactivated', deactivated_at = now() WHERE tenant_id = $1 AND id = $2`, tenantID, first); err != nil {
		t.Fatalf("deactivate the first holder: %v", err)
	}
	if _, err := hire("invited", address); err != nil {
		t.Fatalf("a re-hire with the address of a DEACTIVATED row was refused: %v", err)
	}
	if _, err := hire("deactivated", address); err != nil {
		t.Errorf("a second deactivated row with the address was refused: %v", err)
	}
	_, err = hire("active", address)
	if code, name := m11Constraint(err); code != "23505" || name != "employees_tenant_email_key" {
		t.Errorf("a second LIVE row beside the re-hire answered %q on %q (%v), want the named 23505", code, name, err)
	}
	// And through the panel's own address write: moving the re-hire's address onto a live
	// colleague is the named refusal.
	colleague, err := hire("active", "other."+strings.ToUpper(uuid.NewString()[:8])+"@k24.example.test")
	if err != nil {
		t.Fatal(err)
	}
	err = d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).SetEmployeeEmail(ctx, store.SetEmployeeEmailParams{TenantID: tenantID, ID: colleague, Email: &address})
		return e
	})
	if code, name := m11Constraint(err); code != "23505" || name != "employees_tenant_email_key" {
		t.Errorf("SetEmployeeEmail onto the live re-hire's address answered %q on %q (%v), want the named 23505", code, name, err)
	}
}

// ------------------------------------------------------------- K-14, K-24 --

// TestEmployeeHomeVenue_OptionalOnlyWithAnExternalRef is K-14: location_id may be NULL only
// for a row that carries an external_ref (employees_home_or_external_ref); a non-NULL one is
// still bound to a venue of the same tenant (the composite FK). And K-24's ref half: one LIVE
// row per ref in a tenant; a deactivated row keeps its ref and the re-hire opens a new row
// under it; another tenant may hold the same ref; the ref has the URL-safe shape.
func TestEmployeeHomeVenue_OptionalOnlyWithAnExternalRef(t *testing.T) {
	d := appDB(t)
	tenantID, venue := m11Tenant(t, d)
	otherTenant, otherVenue := m11Tenant(t, d)
	ref := "EMP-" + strings.ToUpper(uuid.NewString()[:8])
	insert := func(tenant uuid.UUID, location *uuid.UUID, externalRef *string, status string) (uuid.UUID, error) {
		id := uuid.New()
		return id, m11Exec(t, d, tenant, `INSERT INTO employees (id, tenant_id, location_id, full_name, status, external_ref)
		                                  VALUES ($1, $2, $3, 'k14 person', $4, $5)`, id, tenant, location, status, externalRef)
	}
	_, err := insert(tenantID, nil, nil, "invited")
	if code, name := m11Constraint(err); code != "23514" || name != "employees_home_or_external_ref" {
		t.Errorf("no venue and no ref answered %q on %q (%v), want 23514 on employees_home_or_external_ref", code, name, err)
	}
	first, err := insert(tenantID, nil, &ref, "active")
	if err != nil {
		t.Fatalf("a ref-anchored row without a venue: %v", err)
	}
	_, err = insert(tenantID, &venue, &ref, "invited")
	if code, name := m11Constraint(err); code != "23505" || name != "employees_tenant_external_ref_key" {
		t.Errorf("a second live row with the ref answered %q on %q (%v), want 23505 on employees_tenant_external_ref_key", code, name, err)
	}
	if _, err := insert(otherTenant, &otherVenue, &ref, "active"); err != nil {
		t.Errorf("another tenant cannot hold the same ref: %v", err)
	}
	_, err = insert(tenantID, &otherVenue, nil, "invited")
	if code, _ := m11Constraint(err); code != "23503" {
		t.Errorf("a venue of another tenant answered %q (%v), want the composite FK's 23503", code, err)
	}
	if err := m11Exec(t, d, tenantID, `UPDATE employees SET status = 'deactivated', deactivated_at = now() WHERE tenant_id = $1 AND id = $2`, tenantID, first); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := insert(tenantID, nil, &ref, "invited"); err != nil {
		t.Errorf("a re-hire under the deactivated row's ref was refused: %v", err)
	}
	for _, bad := range []string{"", ".hidden", "..", "a/b", "a b", "ä1", strings.Repeat("x", 65)} {
		b := bad
		_, err := insert(tenantID, &venue, &b, "invited")
		if code, name := m11Constraint(err); code != "23514" || name != "employees_external_ref_shape" {
			t.Errorf("the ref %q answered %q on %q, want 23514 on employees_external_ref_shape", bad, code, name)
		}
	}
	ok := strings.Repeat("x", 64)
	if _, err := insert(tenantID, &venue, &ok, "invited"); err != nil {
		t.Errorf("CONTROL: a 64-character ref was refused: %v", err)
	}
	// Locations: unique in the tenant (no live/deactivated split -- a venue is never
	// deactivated, K-20's active is a label), the same shape.
	vref := "LOC-" + strings.ToUpper(uuid.NewString()[:8])
	if err := m11Exec(t, d, tenantID, `UPDATE locations SET external_ref = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, venue, vref); err != nil {
		t.Fatalf("bind the venue: %v", err)
	}
	err = m11Exec(t, d, tenantID, `INSERT INTO locations (tenant_id, name, external_ref) VALUES ($1, 'second', $2)`, tenantID, vref)
	if code, name := m11Constraint(err); code != "23505" || name != "locations_tenant_external_ref_key" {
		t.Errorf("a second venue with the ref answered %q on %q, want 23505 on locations_tenant_external_ref_key", code, name)
	}
	if err := m11Exec(t, d, otherTenant, `INSERT INTO locations (tenant_id, name, external_ref) VALUES ($1, 'theirs', $2)`, otherTenant, vref); err != nil {
		t.Errorf("another tenant cannot hold the same venue ref: %v", err)
	}
}

// ------------------------------------------------------------------- K-21 --

// TestEmployeeUpdatedAt_OnlyDirectoryColumnsOnEveryUpdatePath is K-21: updated_at belongs to
// migration 00037's trigger. It moves on INSERT and on every UPDATE path that changes a column
// the integration directory shows -- DeactivateEmployee, a FIRST ConsumeInviteAndActivate,
// SetEmployeeExternalRef, a rename (full_name, first_name, last_name), a new employee_code --
// and on NO other: MoveEmployee, SetEmployeeEmail, a SECOND ConsumeInviteAndActivate (the
// status stays 'active'), a role change. A writer that names updated_at itself is overruled,
// on INSERT and on UPDATE.
func TestEmployeeUpdatedAt_OnlyDirectoryColumnsOnEveryUpdatePath(t *testing.T) {
	d := appDB(t)
	tenantID, venue := m11Tenant(t, d)
	var otherVenue uuid.UUID
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, 'm11 second') RETURNING id`, tenantID).Scan(&otherVenue)
	}); err != nil {
		t.Fatal(err)
	}
	employee := uuid.New()
	before := time.Now().Add(-time.Hour)
	if err := m11Exec(t, d, tenantID, `INSERT INTO employees (id, tenant_id, location_id, full_name, status, updated_at)
	                                    VALUES ($1, $2, $3, 'k21 person', 'invited', '2000-01-01T00:00:00Z')`, employee, tenantID, venue); err != nil {
		t.Fatal(err)
	}
	stamp := func() time.Time {
		t.Helper()
		var at time.Time
		if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT updated_at FROM employees WHERE tenant_id = $1 AND id = $2`, tenantID, employee).Scan(&at)
		}); err != nil {
			t.Fatal(err)
		}
		return at
	}
	last := stamp()
	if last.Before(before) {
		t.Fatalf("an INSERT naming updated_at = 2000-01-01 stored %v; the trigger must overrule it with the wall clock", last)
	}
	q := func(fn func(ctx context.Context, q *store.Queries) error) func() error {
		return func() error {
			return d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
				return fn(ctx, store.New(tx))
			})
		}
	}
	consume := func() error {
		code, binding := randCodeHash(t), randCodeHash(t)
		if err := m11Exec(t, d, tenantID, `INSERT INTO employee_invites (tenant_id, employee_id, code_hash, expires_at, consented_at, consent_binding_hash)
		                                    VALUES ($1, $2, $3, now() + interval '1 hour', now(), $4)`, tenantID, employee, code, binding); err != nil {
			return err
		}
		return q(func(ctx context.Context, s *store.Queries) error {
			_, err := s.ConsumeInviteAndActivate(ctx, store.ConsumeInviteAndActivateParams{TenantID: tenantID, CodeHash: code, ConsentBindingHash: binding})
			return err
		})()
	}
	ref := "K21-" + strings.ToUpper(uuid.NewString()[:8])
	email := "k21." + uuid.NewString()[:8] + "@k21.example.test"
	steps := []struct {
		name  string
		run   func() error
		moves bool
	}{
		{"ConsumeInviteAndActivate, first (invited -> active)", consume, true},
		{"ConsumeInviteAndActivate, second device (active stays active)", consume, false},
		{"MoveEmployee", q(func(ctx context.Context, s *store.Queries) error {
			_, err := s.MoveEmployee(ctx, store.MoveEmployeeParams{TenantID: tenantID, ID: employee, LocationID: otherVenue})
			return err
		}), false},
		{"SetEmployeeEmail", q(func(ctx context.Context, s *store.Queries) error {
			_, err := s.SetEmployeeEmail(ctx, store.SetEmployeeEmailParams{TenantID: tenantID, ID: employee, Email: &email})
			return err
		}), false},
		{"SetEmployeeExternalRef", q(func(ctx context.Context, s *store.Queries) error {
			_, err := s.SetEmployeeExternalRef(ctx, store.SetEmployeeExternalRefParams{TenantID: tenantID, ID: employee, ExternalRef: &ref})
			return err
		}), true},
		{"a role change", func() error {
			return m11Exec(t, d, tenantID, `UPDATE employees SET role = 'chef' WHERE tenant_id = $1 AND id = $2`, tenantID, employee)
		}, false},
		{"updated_at written by hand", func() error {
			return m11Exec(t, d, tenantID, `UPDATE employees SET updated_at = '2000-01-01T00:00:00Z' WHERE tenant_id = $1 AND id = $2`, tenantID, employee)
		}, false},
		{"a rename (full_name)", func() error {
			return m11Exec(t, d, tenantID, `UPDATE employees SET full_name = 'k21 renamed' WHERE tenant_id = $1 AND id = $2`, tenantID, employee)
		}, true},
		{"first_name", func() error {
			return m11Exec(t, d, tenantID, `UPDATE employees SET first_name = 'Maria' WHERE tenant_id = $1 AND id = $2`, tenantID, employee)
		}, true},
		{"last_name", func() error {
			return m11Exec(t, d, tenantID, `UPDATE employees SET last_name = 'Borg' WHERE tenant_id = $1 AND id = $2`, tenantID, employee)
		}, true},
		{"employee_code", func() error {
			return m11Exec(t, d, tenantID, `UPDATE employees SET employee_code = '1001' WHERE tenant_id = $1 AND id = $2`, tenantID, employee)
		}, true},
		{"the same employee_code again (no change)", func() error {
			return m11Exec(t, d, tenantID, `UPDATE employees SET employee_code = '1001' WHERE tenant_id = $1 AND id = $2`, tenantID, employee)
		}, false},
		{"DeactivateEmployee", q(func(ctx context.Context, s *store.Queries) error {
			_, err := s.DeactivateEmployee(ctx, store.DeactivateEmployeeParams{TenantID: tenantID, ID: employee})
			return err
		}), true},
	}
	for _, s := range steps {
		if err := s.run(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		now := stamp()
		switch {
		case s.moves && !now.After(last):
			t.Errorf("%s: updated_at stayed %v; a directory column changed", s.name, last)
		case !s.moves && !now.Equal(last):
			t.Errorf("%s: updated_at moved %v -> %v; no directory column changed", s.name, last, now)
		}
		last = now
	}
}

// ---------------------------------------------------------- K-14 readers --

// employeeLocationReaders is every db/queries statement that reads or returns
// employees.location_id, counted when K-14 made the column nullable (M11 API-1) and reviewed
// one by one (the API-1 report lists each with what a NULL means for it):
//
//	GetEmployeeActivationContext  the activation's Wi-Fi venue -> none (internal/invite)
//	GetEmployeeForTap             the home venue of the cross-location fact -> not cross-location
//	ListPanelEmployees            roster venue name and the venue filter -> "No home venue"
//	GetPanelEmployeeForAction     the action card's placement -> "No home venue"; Move gives one
//	MoveEmployee                  writes a venue (never NULL) and returns it
//	CreateEmployee                the panel always places somebody (never NULL)
//	ConsumeInviteAndActivate      returned to the activation -> none
//	InsertManualTransaction       the record's venue -> the manager's choice (K-14)
//	CountLocationReferences       a venue's references -> a NULL never counts
//	LockEmployeeForExternalRefChange  (new in API-1) no home venue -> the ref cannot be removed
var employeeLocationReaders = []string{
	"ConsumeInviteAndActivate", "CountLocationReferences", "CreateEmployee", "GetEmployeeActivationContext",
	"GetEmployeeForTap", "GetPanelEmployeeForAction", "InsertManualTransaction", "ListPanelEmployees",
	"LockEmployeeForExternalRefChange", "MoveEmployee",
}

// employeeLocationNonReaders are the statements the wide net below catches -- they name employees
// and a location_id or a wildcard -- that do NOT read employees.location_id, each with what its
// location_id is. A statement is in exactly one of the two lists.
var employeeLocationNonReaders = map[string]string{
	"ListFlaggedForReview":  "t.location_id, the record's venue; employees is joined for full_name",
	"ListOpenCheckIns":      "t.location_id, the record's venue; employees is joined for full_name",
	"ListPanelTransactions": "t.location_id and the venue filter on transactions; employees is matched and joined for full_name",
	"ListTapsTakenTogether": "transactions' location_id through its ordered/pairs CTEs; employees is joined for full_name",
	"ListWorkedShiftEvents": "t.location_id, the record's venue; employees is joined for full_name",
}

// TestEmployeeLocationReaders_Counted pins K-14's count: the statements of db/queries that
// touch employees.location_id are exactly employeeLocationReaders. A new reader is red until
// somebody decides what it does with an employee who has no home venue and adds it to the list.
// (The Go readers are pinned by the type system: sqlc hands every one of them a *uuid.UUID.)
//
// THE NET IS WIDE AND THE NARROWING IS WRITTEN DOWN. Its first version recognised a reader by
// its shape -- alias.location_id, or a bare location_id in a statement naming no other table
// with that column -- and two shapes walked past it: a bare location_id beside a JOIN to
// locations (which has no such column, so it IS employees'), and a read through a wildcard
// (SELECT * in a CTE, alias.*, RETURNING *, sqlc.embed). So every statement that names employees
// and a location_id or a wildcard is a candidate, and each candidate is classified by hand:
// employeeLocationReaders or employeeLocationNonReaders. The shape reading stays as a check on
// the second list only -- a statement it calls a reader cannot be filed as a non-reader.
func TestEmployeeLocationReaders_Counted(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "db", "queries", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("read db/queries: %v", err)
	}
	nameRE := regexp.MustCompile(`(?m)^-- name: (\w+)`)
	employeesRE := regexp.MustCompile(`(?i)\bemployees\b`)
	locationRE := regexp.MustCompile(`(?i)\blocation_id\b`)
	wildcardRE := regexp.MustCompile(`(?i)\bSELECT\s+(?:DISTINCT\s+)?\*|\w\.\*|\bRETURNING\s+\*|\bsqlc\.embed\s*\(`)
	candidates := map[string]string{} // name -> statement
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		idx := nameRE.FindAllStringSubmatchIndex(src, -1)
		for i, m := range idx {
			end := len(src)
			if i+1 < len(idx) {
				end = idx[i+1][0]
			}
			var body strings.Builder
			for _, line := range strings.Split(src[m[1]:end], "\n") {
				if !strings.HasPrefix(strings.TrimSpace(line), "--") {
					body.WriteString(line + "\n")
				}
			}
			stmt := body.String()
			if employeesRE.MatchString(stmt) && (locationRE.MatchString(stmt) || wildcardRE.MatchString(stmt)) {
				candidates[src[m[2]:m[3]]] = stmt
			}
		}
	}

	readers := map[string]bool{}
	for _, name := range employeeLocationReaders {
		readers[name] = true
	}
	var unclassified []string
	for name, stmt := range candidates {
		_, non := employeeLocationNonReaders[name]
		switch {
		case readers[name] && non:
			t.Errorf("%s is in employeeLocationReaders AND employeeLocationNonReaders", name)
		case !readers[name] && !non:
			unclassified = append(unclassified, name)
		case non && employeeLocationShapeReads(stmt):
			t.Errorf("%s is filed as a non-reader, but it reads employees.location_id by alias or bare name", name)
		}
	}
	sort.Strings(unclassified)
	if len(unclassified) > 0 {
		t.Errorf("statements naming employees and a location_id or a wildcard, in neither list: %v -- decide what each "+
			"does with an employee who has no home venue (K-14) and add it to employeeLocationReaders, or say what it "+
			"reads instead in employeeLocationNonReaders", unclassified)
	}
	for name := range readers {
		if _, ok := candidates[name]; !ok {
			t.Errorf("employeeLocationReaders names %s, which the net no longer catches", name)
		}
	}
	for name := range employeeLocationNonReaders {
		if _, ok := candidates[name]; !ok {
			t.Errorf("employeeLocationNonReaders names %s, which the net no longer catches", name)
		}
	}
}

// employeeLocationShapeReads is the first version's reading: alias.location_id for an alias of
// employees, a bare location_id in a statement over employees that names no other table carrying
// the column, or location_id in an INSERT INTO employees column list.
func employeeLocationShapeReads(stmt string) bool {
	aliasRE := regexp.MustCompile(`(?i)\b(?:FROM|JOIN|UPDATE)\s+(?:public\.)?employees(?:\s+(?:AS\s+)?(\w+))?`)
	keywords := map[string]bool{"where": true, "set": true, "on": true, "left": true, "join": true, "inner": true, "order": true, "group": true}
	for _, am := range aliasRE.FindAllStringSubmatch(stmt, -1) {
		alias := strings.ToLower(am[1])
		if alias == "" || keywords[alias] {
			if regexp.MustCompile(`(?i)(^|[^.\w])location_id\b`).MatchString(stmt) &&
				!regexp.MustCompile(`(?i)\b(FROM|JOIN|UPDATE|INTO)\s+(public\.)?(locations|departments|tags|transactions)\b`).MatchString(stmt) {
				return true
			}
			continue
		}
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(alias) + `\.location_id\b`).MatchString(stmt) {
			return true
		}
	}
	return regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+employees\s*\([^)]*\blocation_id\b`).MatchString(stmt)
}

// ------------------------------------------------------------------- RLS --

// TestRLS_InviteRequests_AnotherTenantsQueueIsInvisible is CLAUDE.md §8's mandatory RLS test
// for the K-17 queue: B's context reads none of A's requests through a probe WITHOUT a tenant
// predicate (a WHERE could explain a zero; only RLS can explain this one), cannot forge one
// stamped with A's tenant, and cannot lease, re-count or delete A's. Each negative has its
// positive control in A's own context.
func TestRLS_InviteRequests_AnotherTenantsQueueIsInvisible(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)
	a, aVenue := m11Tenant(t, d)
	b, _ := m11Tenant(t, d)
	aEmployee := uuid.New()
	if err := m11Exec(t, d, a, `INSERT INTO employees (id, tenant_id, location_id, full_name) VALUES ($1, $2, $3, 'queue person')`, aEmployee, a, aVenue); err != nil {
		t.Fatal(err)
	}
	if err := m11Exec(t, d, a, `INSERT INTO invite_requests (tenant_id, employee_id) VALUES ($1, $2)`, a, aEmployee); err != nil {
		t.Fatalf("A queues its own request: %v", err)
	}
	probe := `SELECT count(*) FROM invite_requests WHERE employee_id = $1`
	if n := rawCountCtx(t, d, a, probe, aEmployee); n != 1 {
		t.Fatalf("positive control: A's context sees %d of its own requests, want 1", n)
	}
	if n := rawCountCtx(t, d, b, probe, aEmployee); n != 0 {
		t.Errorf("RLS FAILED: B's context reads %d of A's requests", n)
	}
	// The whole table from a context that queued nothing: B sees none of anybody's.
	var all int
	if err := d.WithTenant(context.Background(), b, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM invite_requests`).Scan(&all)
	}); err != nil {
		t.Fatal(err)
	}
	if all != 0 {
		t.Errorf("B's context, which queued nothing, reads %d request(s)", all)
	}
	assertWriteBlocked(t, "invite_requests", blockRLS,
		m11Exec(t, d, b, `INSERT INTO invite_requests (tenant_id, employee_id) VALUES ($1, $2)`, a, aEmployee))
	for _, s := range []string{
		`UPDATE invite_requests SET lease_until = now() + interval '1 hour', attempts = attempts + 1 WHERE employee_id = $1`,
		`DELETE FROM invite_requests WHERE employee_id = $1`,
	} {
		if err := m11Exec(t, d, b, s, aEmployee); err != nil {
			t.Fatalf("%s from B: %v", s, err)
		}
	}
	var lease *time.Time
	var attempts int
	if err := d.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT lease_until, attempts FROM invite_requests WHERE employee_id = $1`, aEmployee).Scan(&lease, &attempts)
	}); err != nil {
		t.Fatalf("A's request after B's UPDATE and DELETE: %v (B deleted it?)", err)
	}
	if lease != nil || attempts != 0 {
		t.Errorf("B leased or counted A's request: lease %v, attempts %d", lease, attempts)
	}
	// The grants hold too: a request cannot be re-dated or moved, even in its own context.
	assertPermissionDenied(t, "A re-dating its own request",
		m11Exec(t, d, a, `UPDATE invite_requests SET requested_at = now() - interval '1 day' WHERE employee_id = $1`, aEmployee))
	assertPermissionDenied(t, "A queueing with a chosen place in the line",
		m11Exec(t, d, a, `INSERT INTO invite_requests (tenant_id, employee_id, requested_at) VALUES ($1, $2, now() - interval '1 day')`, a, uuid.New()))
}
