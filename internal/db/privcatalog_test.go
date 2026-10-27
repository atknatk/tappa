package db

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// privcatalog_test.go -- T137's direction (d): the WHOLE privilege catalogue of the database's
// user schemas, held to one written allowlist.
//
// Why a catalogue test when redline already reads every migration: redline is a lexical gate, and
// T137's rounds measured it being read wrong three times over (an E-string continued after a line
// break, a quoted DO body continued the same way, a setting name built by concatenation) -- each
// time a `GRANT DELETE ON public.employees TO tappa_app` that PostgreSQL ran and redline never saw.
// A lexer can always be out-lexed. What the database HOLDS after the chain ran cannot: this test
// reads it, not the text that built it.
//
// How it splits the work with m11schema_test.go (ADR 0028 §1's matrix): that test reasons about
// NEW objects (born after 00035) -- each one's exact grantees by aclexplode, its owner, its definer
// flag, the `arwd` witness, the trigger binding. This one is the coarse net over EVERYTHING, old
// and new: one flat line per fact, compared as a set. The two cannot disagree while both are green,
// because both are compared with the same database; an M11 table therefore appears in both, and
// that is deliberate -- the matrix says why, this list says that nothing else exists.
//
// What it reads (T137 low round widened all four from public / the tappa_* roles / no grant options):
//   - every USER schema: everything but pg_catalog, information_schema and the pg_toast*/pg_temp*
//     schemas (a session's temporary objects are not the chain's, and another package's test may
//     hold some while this one reads). A name outside public is written schema-qualified;
//   - every role but the predefined pg_* ones and the superusers, plus PUBLIC (privCatalogRolesSQL).
//     A pg_* role cannot log in; what it confers (pg_write_all_data, a membership) shows in the
//     EFFECTIVE privilege of the role that holds it, which is read. A superuser is not read for
//     privileges -- has_*_privilege is true for it on everything, so its lines would say nothing --
//     but each one is a line of its own (`superuser`), so a new one is red;
//   - every privilege the server version has, PG17's MAINTAIN included, and each one's grant
//     option: a verb written `VERB*` is held WITH GRANT OPTION (the ACL's own notation). The
//     object's OWNER is not starred: it holds every grant option on what it owns, implicitly and
//     inalienably, and its ownership is already a line.
//
// What a line says (every role's privilege is EFFECTIVE: has_*_privilege, so role membership and
// PUBLIC count):
//
//	rel <name> <relkind> owner=<role> rls=<enable|no-enable>,<force|no-force>   every relation
//	table <name> <role> <verbs>             table-level SELECT…MAINTAIN, when the role holds any
//	column <name> <role> <verb>[*] <columns>  a column grant (or its grant option) the role holds
//	                                        WITHOUT the same at table level
//	sequence <name> owner=<role>            every sequence
//	seqpriv <name> <role> <privs>           USAGE/SELECT/UPDATE on it
//	function <signature> owner=<role> definer=<bool> execute=<roles|->   every non-extension function
//	namespace <schema> owner=<role>         every user schema
//	schema <schema> <role> <privs>          USAGE/CREATE on it
//	database <role> <privs>                 CONNECT/CREATE/TEMPORARY on the current database
//	extension <name> schema=<schema>        every installed extension
//	superuser <role>                        every superuser but the pg_* ones
//
// So a new table, column grant, function, sequence, schema, extension, role or superuser, a FORCE
// or an ENABLE taken away, an owner changed, a definer flag set, a grant option or a privilege
// given to any role (PUBLIC included, directly or through a membership) is a line the allowlist
// does not hold: red, until someone writes it here on purpose.

// privCatalogRolesSQL is the set of roles every line is read for -- ONE definition, used by the
// snapshot and by TestPrivCatalog_EveryRoleIsRead, so the two cannot drift.
const privCatalogRolesSQL = `SELECT 'public'::text AS r UNION ALL
                  SELECT rolname::text FROM pg_roles WHERE rolname !~ '^pg_' AND NOT rolsuper`

// privCatalogRoles is what privCatalogRolesSQL returns today: PUBLIC and every role 01-roles.sql
// and the migrations create but the owner (a superuser -- see above). The snapshot reads whatever
// roles exist, so an unexpected role is already red there (each role inherits PUBLIC's USAGE and
// CONNECT, so it has lines); TestPrivCatalog_EveryRoleIsRead names it outright.
var privCatalogRoles = []string{"public", "tappa_app", "tappa_feedwriter", "tappa_holdingdefiner", "tappa_identitydefiner", "tappa_opdefiner", "tappa_operator", "tappa_resolver"}

// privCatalogQuery builds the snapshot, one line per fact, in byte order.
const privCatalogQuery = `
WITH roles(r) AS (` + privCatalogRolesSQL + `),
nsps AS (SELECT n.oid, n.nspname, n.nspowner FROM pg_namespace n
          WHERE n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname !~ '^pg_(toast|temp)'),
rels AS (SELECT c.oid, CASE WHEN n.nspname = 'public' THEN '' ELSE n.nspname || '.' END || c.relname AS relname,
                c.relkind, c.relowner, c.relrowsecurity, c.relforcerowsecurity
           FROM pg_class c JOIN nsps n ON n.oid = c.relnamespace
          WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S')
            AND NOT EXISTS (SELECT 1 FROM pg_depend d
                             WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype = 'e')),
verbs(v, o) AS (VALUES ('SELECT', 1), ('INSERT', 2), ('UPDATE', 3), ('DELETE', 4), ('TRUNCATE', 5), ('REFERENCES', 6),
                       ('TRIGGER', 7), ('MAINTAIN', 8)),
colverbs(v, o) AS (VALUES ('SELECT', 1), ('INSERT', 2), ('UPDATE', 3), ('REFERENCES', 6)),
fns AS (SELECT p.oid, p.proowner, p.prosecdef FROM pg_proc p JOIN nsps n ON n.oid = p.pronamespace
         WHERE NOT EXISTS (SELECT 1 FROM pg_depend d
                            WHERE d.classid = 'pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e'))
SELECT line FROM (
  SELECT 'rel ' || relname || ' ' || relkind::text || ' owner=' || pg_get_userbyid(relowner)
         || ' rls=' || CASE WHEN relrowsecurity THEN 'enable' ELSE 'no-enable' END
         || ',' || CASE WHEN relforcerowsecurity THEN 'force' ELSE 'no-force' END AS line
    FROM rels WHERE relkind <> 'S'
  UNION ALL
  SELECT 'table ' || rels.relname || ' ' || roles.r || ' '
         || string_agg(verbs.v || CASE WHEN roles.r <> pg_get_userbyid(rels.relowner)
                                        AND has_table_privilege(roles.r, rels.oid, verbs.v || ' WITH GRANT OPTION')
                                       THEN '*' ELSE '' END, ',' ORDER BY verbs.o)
    FROM rels CROSS JOIN roles CROSS JOIN verbs
   WHERE rels.relkind <> 'S' AND has_table_privilege(roles.r, rels.oid, verbs.v)
   GROUP BY rels.relname, roles.r
  UNION ALL
  SELECT 'column ' || rels.relname || ' ' || roles.r || ' ' || colverbs.v || ' ' || string_agg(a.attname, ',' ORDER BY a.attnum)
    FROM rels CROSS JOIN roles CROSS JOIN colverbs
    JOIN pg_attribute a ON a.attrelid = rels.oid AND a.attnum > 0 AND NOT a.attisdropped
   WHERE rels.relkind <> 'S' AND NOT has_table_privilege(roles.r, rels.oid, colverbs.v)
     AND has_column_privilege(roles.r, rels.oid, a.attnum, colverbs.v)
   GROUP BY rels.relname, roles.r, colverbs.v
  UNION ALL
  SELECT 'column ' || rels.relname || ' ' || roles.r || ' ' || colverbs.v || '* ' || string_agg(a.attname, ',' ORDER BY a.attnum)
    FROM rels CROSS JOIN roles CROSS JOIN colverbs
    JOIN pg_attribute a ON a.attrelid = rels.oid AND a.attnum > 0 AND NOT a.attisdropped
   WHERE rels.relkind <> 'S' AND roles.r <> pg_get_userbyid(rels.relowner)
     AND NOT has_table_privilege(roles.r, rels.oid, colverbs.v || ' WITH GRANT OPTION')
     AND has_column_privilege(roles.r, rels.oid, a.attnum, colverbs.v || ' WITH GRANT OPTION')
   GROUP BY rels.relname, roles.r, colverbs.v
  UNION ALL
  SELECT 'sequence ' || relname || ' owner=' || pg_get_userbyid(relowner) FROM rels WHERE relkind = 'S'
  UNION ALL
  SELECT 'seqpriv ' || rels.relname || ' ' || roles.r || ' '
         || string_agg(v.v || CASE WHEN roles.r <> pg_get_userbyid(rels.relowner)
                                    AND has_sequence_privilege(roles.r, rels.oid, v.v || ' WITH GRANT OPTION')
                                   THEN '*' ELSE '' END, ',' ORDER BY v.o)
    FROM rels CROSS JOIN roles CROSS JOIN (VALUES ('USAGE', 1), ('SELECT', 2), ('UPDATE', 3)) AS v(v, o)
   WHERE rels.relkind = 'S' AND has_sequence_privilege(roles.r, rels.oid, v.v)
   GROUP BY rels.relname, roles.r
  UNION ALL
  SELECT 'function ' || fns.oid::regprocedure::text || ' owner=' || pg_get_userbyid(fns.proowner)
         || ' definer=' || fns.prosecdef::text || ' execute='
         || coalesce((SELECT string_agg(roles.r || CASE WHEN roles.r <> pg_get_userbyid(fns.proowner)
                                                          AND has_function_privilege(roles.r, fns.oid, 'EXECUTE WITH GRANT OPTION')
                                                         THEN '*' ELSE '' END, ',' ORDER BY roles.r)
                        FROM roles WHERE has_function_privilege(roles.r, fns.oid, 'EXECUTE')), '-')
    FROM fns
  UNION ALL
  SELECT 'namespace ' || nspname || ' owner=' || pg_get_userbyid(nspowner) FROM nsps
  UNION ALL
  SELECT 'schema ' || nsps.nspname || ' ' || roles.r || ' '
         || string_agg(v.v || CASE WHEN roles.r <> pg_get_userbyid(nsps.nspowner)
                                    AND has_schema_privilege(roles.r, nsps.oid, v.v || ' WITH GRANT OPTION')
                                   THEN '*' ELSE '' END, ',' ORDER BY v.v)
    FROM nsps CROSS JOIN roles CROSS JOIN (VALUES ('CREATE'), ('USAGE')) AS v(v)
   WHERE has_schema_privilege(roles.r, nsps.oid, v.v)
   GROUP BY nsps.nspname, roles.r
  UNION ALL
  SELECT 'database ' || roles.r || ' '
         || string_agg(v.v || CASE WHEN roles.r <> pg_get_userbyid(d.datdba)
                                    AND has_database_privilege(roles.r, d.oid, v.v || ' WITH GRANT OPTION')
                                   THEN '*' ELSE '' END, ',' ORDER BY v.o)
    FROM pg_database d CROSS JOIN roles CROSS JOIN (VALUES ('CONNECT', 1), ('CREATE', 2), ('TEMPORARY', 3)) AS v(v, o)
   WHERE d.datname = current_database() AND has_database_privilege(roles.r, d.oid, v.v)
   GROUP BY roles.r
  UNION ALL
  SELECT 'extension ' || e.extname || ' schema=' || n.nspname
    FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace
  UNION ALL
  SELECT 'superuser ' || rolname FROM pg_roles WHERE rolsuper AND rolname !~ '^pg_'
) s ORDER BY line COLLATE "C"`

// privCatalogAllowed is the allowlist: generated from the catalogue at migration 00060 (T137 fix1,
// 2026-10-19; `TAPPA_PRIVCATALOG_DUMP=1 go test -run TestPrivCatalog_ ./internal/db -v` prints the
// live one; 00061 changed nine function lines, read again at 00061; 00063 -- M10 OP-15 -- added
// op_suspend_tenant and op_reinstate_tenant and the two suspension columns to tappa_opdefiner's
// two tenants lines, read again at 00063; 00064 -- M15-02 -- added day_cutoff to tappa_app's tenants
// UPDATE line and tappa_checkout_class, read again at 00064; 00069 -- M14-03, ADR 0031 §5.1 -- added
// tappa_identitydefiner (CONNECT/TEMPORARY and USAGE, nothing else: its column grants come with the
// functions that need them), admin_identities (ENABLE + FORCE, no policy, no grant to anyone), the
// INVOKER trigger function admin_users_guard_identity_credentials (execute=-), narrowed tappa_app's
// SELECT on admin_users from the table to ten columns (linked_via, linked_at, unlinked_at outside)
// and took email out of its UPDATE list, read again at 00070; 00071 -- M14-04, ADR 0031 §5.2 -- added
// the identity resolver and the four session functions (EXECUTE to tappa_app and the owner),
// tappa_identitydefiner's first column grants (admin_identities, admin_users, admin_sessions, tenants,
// audit_log -- identitycatalog_test.go holds them inside the ADR's matrix), tappa_resolver's SELECT on
// four admin_identities columns and on admin_users' identity_id and unlinked_at, and narrowed
// tappa_app's INSERT on admin_sessions from the table to (id, tenant_id, admin_user_id, token_hash),
// read again at 00071; 00072 -- M14-05, ADR 0031 §5.2 -- added the three tenant-scope-less tables
// admin_identity_resets / _verifications / _events (ENABLE + FORCE, no policy, nothing to tappa_app),
// the fifteen identity functions (thirteen owned by tappa_identitydefiner, two token resolvers by
// tappa_resolver; EXECUTE tappa_app and the owner) and widened tappa_identitydefiner's column grants
// to what those bodies read and write (no e-mail write anywhere -- K-15; DELETE on the event log
// alone, the 90-day sweep), read again at 00072; 00074 -- M14-08, ADR 0031 §5.2 named exception 2 --
// added identity_create_for_signup (EXECUTE tappa_app and the owner) and the three grants its body
// needs: admin_identities INSERT (email, password_hash), admin_users SELECT (password_hash) -- and
// the system column xmin, which this catalogue (attnum > 0) does not list; identitycatalog_test.go's
// identitySystemColumns pins it -- and tenants SELECT (created_at), read again at 00074; 00077 -- M14-15, ADR 0031 §5.1/§5.2 -- added the
// tenant-scoped admin_invites (the RLS five: ENABLE + FORCE and its tenant policy; tappa_app reads
// every column but token_hash, inserts an invitation and updates cancelled_at alone -- the owner lists,
// issues and cancels), its md.7 resolver (tappa_resolver reads the six columns it returns and the hash
// it filters on), the two acceptances identity_accept_invite / _new (EXECUTE tappa_app and the owner)
// and the grants their bodies need: admin_invites SELECT and UPDATE (used_at), admin_users INSERT (the
// matrix's eight columns -- the membership an acceptance births), read again at 00077;
// 00078 -- M14-17,
// ADR 0031 §5.2/§5.10 -- added admin_signup_claims (ENABLE + FORCE, no policy, nothing to tappa_app; the
// definer's SELECT/INSERT/UPDATE (used_at) and its one DELETE -- the sweep -- and the resolver's SELECT of
// the key and the six columns it returns), the four functions (EXECUTE tappa_app and the owner),
// RESTATED the owner row's admin_users INSERT for the definer (00077 owns that shared grant; 00078's Down
// keeps it while an invite function exists) and added tenants SELECT (vat_number) -- plus the system column
// xmin, outside this catalogue -- read again at 00078; 00079 -- M14-18,
// ADR 0031 §5.2's 2026-10-27 note -- added identity_confirm_window and identity_keep_all (EXECUTE
// tappa_app and the owner) and NO grant: both bodies run on the definer's 00071/00072 cells) and READ
// line by line before
// it was written here. Things the reading noted, all
// kept on purpose:
//   - nine pre-M11 trigger functions (tappa_forbid_*, tappa_check_transaction_review,
//     tappa_audit_at_is_the_wall_clock) carried PostgreSQL's default EXECUTE to PUBLIC until 00061
//     (T138, 2026-10-19) took it away: they read `execute=-` now, like every function from 00037 on
//     (ADR 0028 §1's matrix). The triggers keep firing -- EXECUTE is checked when a trigger is
//     created, not when it fires -- and triggerexecute_test.go measures that for each of the nine;
//     00061's Down does not give PUBLIC the grant back (its header says why), so these nine lines
//     hold below 00061 too;
//   - the low round's twelve new lines (same chain, 00060) are the server's and db-init's, none a
//     migration's: CONNECT and TEMPORARY on the database for every role come from PostgreSQL's
//     default PUBLIC grant on a new database (TEMPORARY lets a session make temporary tables of
//     its own, in its own pg_temp, which no other session sees); public is owned by
//     pg_database_owner (PG15+'s default); pgcrypto and citext are 01-roles.sql's, plpgsql the
//     server's; the one superuser is the bootstrap POSTGRES_USER. No MAINTAIN, no grant option
//     and no user schema but public exists at 00060;
//   - tappa_operator reads platform_admins' password_hash and totp_secret_sealed: that is the
//     login lookup itself (00026; ADR 0020 -- the password and the TOTP are verified in the
//     process, never in SQL).
//
// A line is added here only together with the migration that makes it true, and with the reason
// in that migration (or, for an M11 object, in m11schema_test.go's matrix).
const privCatalogAllowed = `
column admin_identities tappa_identitydefiner INSERT email,password_hash
column admin_identities tappa_identitydefiner SELECT id,email,password_hash,email_verified_at,confirm_opened_at,confirm_open_until,confirm_scope,last_used_tenant
column admin_identities tappa_identitydefiner UPDATE password_hash,email_verified_at,confirm_opened_at,confirm_open_until,confirm_scope,last_used_tenant,last_login_at
column admin_identities tappa_resolver SELECT id,email,password_hash,email_verified_at
column admin_identity_events tappa_identitydefiner INSERT identity_id,event,detail
column admin_identity_events tappa_identitydefiner SELECT created_at
column admin_identity_resets tappa_identitydefiner INSERT identity_id,token_hash,expires_at
column admin_identity_resets tappa_identitydefiner SELECT id,identity_id,token_hash,expires_at,used_at,cancelled_at
column admin_identity_resets tappa_identitydefiner UPDATE used_at,cancelled_at
column admin_identity_resets tappa_resolver SELECT id,identity_id,token_hash,expires_at,used_at,cancelled_at
column admin_identity_verifications tappa_identitydefiner INSERT identity_id,token_hash,expires_at
column admin_identity_verifications tappa_identitydefiner SELECT id,identity_id,token_hash,expires_at,used_at
column admin_identity_verifications tappa_identitydefiner UPDATE used_at
column admin_identity_verifications tappa_resolver SELECT id,identity_id,token_hash,expires_at,used_at
column admin_invites tappa_app INSERT tenant_id,email,full_name,role,token_hash,invited_by,expires_at
column admin_invites tappa_app SELECT id,tenant_id,email,full_name,role,invited_by,created_at,expires_at,used_at,cancelled_at
column admin_invites tappa_app UPDATE cancelled_at
column admin_invites tappa_identitydefiner SELECT id,tenant_id,email,full_name,role,token_hash,expires_at,used_at,cancelled_at
column admin_invites tappa_identitydefiner UPDATE used_at
column admin_invites tappa_resolver SELECT id,tenant_id,role,token_hash,expires_at,used_at,cancelled_at
column admin_signup_claims tappa_identitydefiner INSERT token_hash,email,draft,password_hash,expires_at
column admin_signup_claims tappa_identitydefiner SELECT id,token_hash,email,draft,password_hash,expires_at,used_at
column admin_signup_claims tappa_identitydefiner UPDATE used_at
column admin_signup_claims tappa_resolver SELECT id,token_hash,email,draft,password_hash,expires_at,used_at
column admin_sessions tappa_app INSERT id,tenant_id,admin_user_id,token_hash
column admin_sessions tappa_app UPDATE last_used_at,revoked_at
column admin_sessions tappa_identitydefiner INSERT tenant_id,admin_user_id,token_hash,authenticated_at
column admin_sessions tappa_identitydefiner SELECT id,tenant_id,admin_user_id,token_hash,revoked_at,authenticated_at
column admin_sessions tappa_identitydefiner UPDATE revoked_at
column admin_sessions tappa_resolver SELECT id,tenant_id,admin_user_id,token_hash,revoked_at
column admin_users tappa_app INSERT id,tenant_id,full_name,email,password_hash,role,status
column admin_users tappa_app SELECT id,tenant_id,full_name,email,password_hash,role,status,created_at,last_login_at,identity_id
column admin_users tappa_app UPDATE full_name,password_hash,role,status,last_login_at
column admin_users tappa_identitydefiner INSERT tenant_id,full_name,email,password_hash,role,identity_id,linked_via,linked_at
column admin_users tappa_identitydefiner SELECT id,tenant_id,email,password_hash,role,status,created_at,identity_id,linked_via,linked_at,unlinked_at
column admin_users tappa_identitydefiner UPDATE password_hash,status,last_login_at,identity_id,linked_via,linked_at,unlinked_at
column admin_users tappa_opdefiner SELECT tenant_id,email,status
column admin_users tappa_resolver SELECT id,tenant_id,email,password_hash,status,created_at,identity_id,unlinked_at
column api_idempotency tappa_app INSERT tenant_id,key_id,idem_key,request_hash
column api_idempotency tappa_app SELECT tenant_id,key_id,idem_key,request_hash,status,result_ref,created_at
column api_idempotency tappa_app UPDATE status,result_ref
column api_keys tappa_app INSERT tenant_id,name,token_hash,prefix_hint,created_by
column api_keys tappa_app SELECT tenant_id,id,name,prefix_hint,created_by,created_at,last_used_at,revoked_at
column api_keys tappa_app UPDATE last_used_at,revoked_at
column api_keys tappa_resolver SELECT tenant_id,id,token_hash,revoked_at
column audit_log tappa_app INSERT id,tenant_id,actor_id,action,target,detail
column audit_log tappa_identitydefiner INSERT tenant_id,actor_id,action,target,detail
column audit_log tappa_opdefiner INSERT tenant_id,actor_id,action,target,detail,at
column billing_periods tappa_opdefiner SELECT tenant_id,period_month,period_from,period_to,timezone,plan,free_period,employee_count,unstamped_employees,unit_price,currency,amount_due,closed_at
column employee_invites tappa_app UPDATE used_at,cancelled_at,consented_at,consent_binding_hash
column employee_invites tappa_resolver SELECT id,tenant_id,employee_id,code_hash,expires_at,used_at,cancelled_at
column employees tappa_feedwriter SELECT id,tenant_id,external_ref,employee_code
column employees tappa_holdingdefiner SELECT id,tenant_id,full_name,status,external_ref,employee_code,first_name,last_name,updated_at
column employees tappa_opdefiner SELECT tenant_id,status,activated_at,deactivated_at
column feed_counters tappa_app SELECT tenant_id,last_seq
column feed_counters tappa_feedwriter INSERT tenant_id,last_seq
column feed_counters tappa_feedwriter SELECT tenant_id,last_seq
column feed_counters tappa_feedwriter UPDATE last_seq
column feed_counters tappa_holdingdefiner INSERT tenant_id,last_seq
column feed_counters tappa_holdingdefiner SELECT tenant_id,last_seq
column feed_counters tappa_holdingdefiner UPDATE last_seq
column holding_members tappa_holdingdefiner SELECT holding_id,member_tenant_id,joined_at,left_at
column holding_members tappa_holdingdefiner UPDATE lock_anchor
column holding_members tappa_opdefiner INSERT holding_id,member_tenant_id
column holding_members tappa_opdefiner SELECT holding_id,member_tenant_id,joined_at,left_at
column holding_members tappa_opdefiner UPDATE left_at
column holdings tappa_holdingdefiner SELECT id,name
column holdings tappa_opdefiner INSERT name
column holdings tappa_opdefiner SELECT id,name,created_at
column invite_requests tappa_app INSERT tenant_id,employee_id
column invite_requests tappa_app UPDATE lease_until,attempts
column legal_documents tappa_app SELECT id,slug,body,published_at
column legal_documents tappa_opdefiner INSERT slug,body,published_by
column legal_documents tappa_opdefiner SELECT id,slug,body,published_at,published_by
column locations tappa_feedwriter SELECT id,tenant_id,name,external_ref
column locations tappa_holdingdefiner SELECT id,tenant_id,name,static_ips,gps_lat,gps_lng,shift_start,shift_end,overnight,external_ref
column locations tappa_opdefiner SELECT id,tenant_id,name
column operator_audit_log tappa_opdefiner INSERT kind,session_id,actor_admin_id,target_admin_id,target_tenant_id,target_scope,page_number,page_size,detail
column operator_audit_log tappa_opdefiner SELECT id,at,kind,session_id,actor_admin_id,target_admin_id,target_tenant_id,target_scope,page_number,page_size,detail
column operator_read_tickets tappa_opdefiner INSERT ticket_hash,session_id,kind,target_tenant_id,audit_id,expires_at
column operator_read_tickets tappa_opdefiner SELECT id,ticket_hash,session_id,kind,target_tenant_id,audit_id,created_xact,expires_at,consumed_at
column operator_read_tickets tappa_opdefiner UPDATE consumed_at
column password_resets tappa_app INSERT id,tenant_id,admin_user_id,token_hash,expires_at
column password_resets tappa_app UPDATE used_at,cancelled_at
column password_resets tappa_identitydefiner SELECT id,tenant_id,admin_user_id,token_hash,expires_at,used_at,cancelled_at
column password_resets tappa_identitydefiner UPDATE used_at,cancelled_at
column password_resets tappa_resolver SELECT id,tenant_id,admin_user_id,token_hash,expires_at,used_at,cancelled_at
column platform_admins tappa_opdefiner SELECT id,email,display_name,status,totp_last_step,totp_failures,totp_locked_until,enroll_token_hash,enroll_expires_at,enroll_used_at
column platform_admins tappa_opdefiner UPDATE status,password_hash,totp_secret_sealed,totp_last_step,totp_failures,totp_locked_until,last_login_at,enroll_used_at
column platform_admins tappa_operator SELECT id,email,display_name,status,password_hash,totp_secret_sealed,totp_locked_until
column platform_sessions tappa_opdefiner INSERT admin_id,token_hash,mfa_verified_at
column platform_sessions tappa_opdefiner SELECT id,admin_id,token_hash,created_at,mfa_verified_at,last_used_at,revoked_at
column platform_sessions tappa_opdefiner UPDATE last_used_at,revoked_at
column punch_events tappa_app SELECT tenant_id,seq,punch_id,punch_tenant_id,roles,version,status,punch_time,recorded_at,channel,direction,employee_id,employee_ref,employee_code,employee_company,location_id,location_ref,location_name,location_company,tag_uid,created_at
column punch_events tappa_feedwriter INSERT tenant_id,seq,punch_id,punch_tenant_id,roles,punch_time,recorded_at,channel,direction,employee_id,employee_ref,employee_code,employee_company,location_id,location_ref,location_name,location_company,tag_uid
column punch_events tappa_holdingdefiner INSERT tenant_id,seq,punch_id,punch_tenant_id,roles,punch_time,recorded_at,channel,direction,employee_id,employee_ref,employee_code,employee_company,location_id,location_ref,location_name,location_company,tag_uid
column sessions tappa_resolver SELECT id,tenant_id,employee_id,token_hash,revoked_at
column site_punches tappa_app SELECT tenant_id,id,transaction_id,employer_tenant_id,employer_api_code,employee_id,employee_ref,employee_code,employee_name,site_location_id,site_tag_uid,ctr,bucket_hour,occurred_at,channel,type,outcome,ctr_gap,created_at
column site_punches tappa_holdingdefiner INSERT tenant_id,transaction_id,employer_tenant_id,employer_api_code,employee_id,employee_ref,employee_code,employee_name,site_location_id,site_tag_uid,ctr,sun_valid,bucket_hour,occurred_at,channel,type,outcome,ctr_gap
column site_punches tappa_holdingdefiner SELECT tenant_id,employee_id,site_tag_uid,ctr,sun_valid,bucket_hour,outcome
column tags tappa_app SELECT uid,tenant_id,location_id,last_ctr,status,retired_at,replaced_by,created_at,encoded_at
column tags tappa_app UPDATE location_id,last_ctr,status,retired_at,replaced_by,encoded_at
column tags tappa_holdingdefiner SELECT uid,tenant_id,location_id,last_ctr,status
column tags tappa_holdingdefiner UPDATE last_ctr
column tags tappa_opdefiner SELECT uid,tenant_id,location_id,last_ctr,status,retired_at,replaced_by,created_at,encoded_at
column tags tappa_resolver SELECT uid,tenant_id,location_id,aes_key_ref,last_ctr,status
column tenant_branding tappa_app INSERT tenant_id,updated_by
column tenant_branding tappa_app UPDATE accent,logo,logo_sha256,logo_mime,logo_width,logo_height,updated_at,updated_by
column tenant_branding tappa_holdingdefiner SELECT tenant_id,accent,logo,logo_sha256,logo_mime,logo_width,logo_height
column tenants tappa_app INSERT id,name,vat_number,business_type,structure,timezone,vat_verified,vat_checked_at
column tenants tappa_app UPDATE name,business_type,timezone,day_cutoff
column tenants tappa_feedwriter SELECT id,api_enabled_at,api_code
column tenants tappa_holdingdefiner SELECT id,name,timezone,vat_verified,api_enabled_at,api_code
column tenants tappa_identitydefiner SELECT id,name,vat_number,created_at,suspended_at
column tenants tappa_opdefiner SELECT id,name,vat_number,business_type,plan,timezone,created_at,price_per_employee_month,vat_verified,vat_checked_at,api_enabled_at,api_code,suspended_at,suspension_reason
column tenants tappa_opdefiner UPDATE vat_verified,vat_checked_at,api_enabled_at,api_code,suspended_at,suspension_reason
column tenants tappa_resolver SELECT id,api_enabled_at
column transactions tappa_feedwriter SELECT id,tenant_id,employee_id,location_id,tag_uid,type,occurred_at,verdict,channel,practice,site_tenant_id
column transactions tappa_holdingdefiner SELECT id,tenant_id,employee_id,ctr,type,occurred_at,sun_valid,verdict,channel,practice,site_tenant_id,site_location_id,site_tag_uid
column webhook_deliveries tappa_app SELECT tenant_id,id,endpoint_id,punch_event_seq,attempt,next_attempt_at,lease_until,state,last_status,last_error_class,created_at,delivered_at,settled_at
column webhook_deliveries tappa_app UPDATE attempt,next_attempt_at,lease_until,state,last_status,last_error_class,delivered_at
column webhook_deliveries tappa_feedwriter INSERT tenant_id,endpoint_id,punch_event_seq
column webhook_deliveries tappa_holdingdefiner INSERT tenant_id,endpoint_id,punch_event_seq
column webhook_endpoints tappa_app INSERT tenant_id,id,url,signing_key_sealed,active
column webhook_endpoints tappa_app SELECT tenant_id,id,url,signing_key_sealed,active,created_at,rotated_at
column webhook_endpoints tappa_app UPDATE url,signing_key_sealed,active,rotated_at
column webhook_endpoints tappa_feedwriter SELECT tenant_id,id,active
column webhook_endpoints tappa_holdingdefiner SELECT tenant_id,id,active
database public CONNECT,TEMPORARY
database tappa_app CONNECT,TEMPORARY
database tappa_feedwriter CONNECT,TEMPORARY
database tappa_holdingdefiner CONNECT,TEMPORARY
database tappa_identitydefiner CONNECT,TEMPORARY
database tappa_opdefiner CONNECT,TEMPORARY
database tappa_operator CONNECT,TEMPORARY
database tappa_resolver CONNECT,TEMPORARY
extension citext schema=public
extension pgcrypto schema=public
extension plpgsql schema=pg_catalog
function admin_users_guard_identity_credentials() owner=tappa_owner definer=false execute=-
function api_keys_revocation_is_one_way() owner=tappa_owner definer=false execute=-
function feed_open_counter() owner=tappa_feedwriter definer=true execute=tappa_feedwriter
function holding_advance_tag_counter(character,integer) owner=tappa_holdingdefiner definer=true execute=tappa_app,tappa_holdingdefiner
function holding_directory(timestamp with time zone,uuid) owner=tappa_holdingdefiner definer=true execute=tappa_app,tappa_holdingdefiner
function holding_feed() owner=tappa_holdingdefiner definer=true execute=tappa_holdingdefiner
function holding_group() owner=tappa_holdingdefiner definer=true execute=tappa_app,tappa_holdingdefiner
function holding_logo(character,text) owner=tappa_holdingdefiner definer=true execute=tappa_app,tappa_holdingdefiner
function holding_peer(uuid) owner=tappa_holdingdefiner definer=false execute=tappa_holdingdefiner
function holding_peer(uuid,text) owner=tappa_holdingdefiner definer=false execute=tappa_holdingdefiner
function holding_peer(uuid,uuid,text) owner=tappa_holdingdefiner definer=false execute=tappa_holdingdefiner
function holding_site_gate() owner=tappa_holdingdefiner definer=true execute=tappa_holdingdefiner
function holding_site_locations(uuid) owner=tappa_holdingdefiner definer=true execute=tappa_app,tappa_holdingdefiner
function holding_tap_site(character) owner=tappa_holdingdefiner definer=true execute=tappa_app,tappa_holdingdefiner
function holding_timezone_lock() owner=tappa_holdingdefiner definer=true execute=tappa_holdingdefiner
function identity_accept_invite(text,text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_accept_invite_new(text,text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_claim_signup(text,uuid,text,text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_confirm_window(text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_consume_reset(text,text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_consume_row_reset(text,text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_create_for_signup(uuid,uuid) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_keep_all(text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_link_legacy(text,text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_memberships(text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_open_reset(uuid,text,timestamp with time zone) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_open_session(uuid,uuid,text,text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_open_signup_claim(text,citext,jsonb,text,timestamp with time zone) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_password_for_session(text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_record_failed_login(uuid,text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_relink_refused(text,uuid) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_request_verification(text,text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_revoke_sessions(text,boolean) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_set_password(text,text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_sweep_events() owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_sweep_signup_claims() owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_switch_session(text,uuid,text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_unlink_backfilled(text,uuid) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_verify_email(text,text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function identity_withdraw_reset(text) owner=tappa_identitydefiner definer=true execute=tappa_app,tappa_identitydefiner
function op_add_holding_member(text,uuid,uuid) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_begin_read(text,text,jsonb) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_close_session(text) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_complete_enrollment(uuid,text,text,bytea,bigint,text) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_create_holding(text,text) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_disable_tenant_api(text,uuid) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_enable_tenant_api(text,uuid,text) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_open_session(uuid,text,bigint) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_publish_legal(text,text,text) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_read_audit(text,text,text,integer,integer) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_read_holdings(text,text,integer,integer) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_read_legal_versions(text,text,integer,integer) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_read_tenant_billing(text,text,uuid,integer) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_read_tenant_detail(text,text,uuid) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_read_tenant_plaques(text,text,uuid) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_read_tenant_vat(text,text,uuid) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_read_tenants(text,text,text,integer,integer) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_record_auth_event(text,text,uuid) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_record_vat_check(text,uuid,text,boolean) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_reinstate_tenant(text,uuid) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_remove_holding_member(text,uuid,uuid) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_suspend_tenant(text,uuid,text) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function op_touch_session(text) owner=tappa_opdefiner definer=true execute=tappa_opdefiner,tappa_operator
function punch_feed() owner=tappa_feedwriter definer=true execute=tappa_feedwriter
function resolve_admin_by_email(citext) owner=tappa_resolver definer=true execute=tappa_app,tappa_resolver
function resolve_admin_invite_by_token_hash(text) owner=tappa_resolver definer=true execute=tappa_app,tappa_resolver
function resolve_admin_session_by_token_hash(text) owner=tappa_resolver definer=true execute=tappa_app,tappa_resolver
function resolve_api_key_by_hash(text) owner=tappa_resolver definer=true execute=tappa_app,tappa_resolver
function resolve_identity_by_email(citext) owner=tappa_resolver definer=true execute=tappa_app,tappa_resolver
function resolve_identity_reset_by_token_hash(text) owner=tappa_resolver definer=true execute=tappa_app,tappa_resolver
function resolve_identity_verification_by_token_hash(text) owner=tappa_resolver definer=true execute=tappa_app,tappa_resolver
function resolve_invite_by_code_hash(text) owner=tappa_resolver definer=true execute=tappa_app,tappa_resolver
function resolve_password_reset_by_token_hash(text) owner=tappa_resolver definer=true execute=tappa_app,tappa_resolver
function resolve_signup_claim_by_token_hash(text) owner=tappa_resolver definer=true execute=tappa_app,tappa_resolver
function resolve_session_by_token_hash(text) owner=tappa_resolver definer=true execute=tappa_app,tappa_resolver
function resolve_tag_by_uid(character) owner=tappa_resolver definer=true execute=tappa_app,tappa_resolver
function tappa_audit_at_is_the_wall_clock() owner=tappa_owner definer=false execute=-
function tappa_billing_amount_due(boolean,numeric,integer) owner=tappa_owner definer=false execute=tappa_app,tappa_opdefiner
function tappa_check_transaction_review() owner=tappa_owner definer=false execute=-
function tappa_checkout_class(timestamp with time zone,boolean,boolean,boolean,timestamp with time zone,timestamp with time zone) owner=tappa_owner definer=false execute=tappa_app
function tappa_employee_is_billable(text,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone) owner=tappa_owner definer=false execute=tappa_app,tappa_opdefiner
function tappa_employee_lifecycle_status(timestamp with time zone,timestamp with time zone) owner=tappa_owner definer=false execute=tappa_app,tappa_opdefiner
function tappa_employees_touch_updated_at() owner=tappa_owner definer=false execute=-
function tappa_first_chargeable_month(text,timestamp with time zone,text) owner=tappa_owner definer=false execute=tappa_app,tappa_opdefiner
function tappa_forbid_app_key_ref_rewrite() owner=tappa_owner definer=false execute=-
function tappa_forbid_counter_rewind() owner=tappa_owner definer=false execute=-
function tappa_forbid_encoded_at_at_insert() owner=tappa_owner definer=false execute=-
function tappa_forbid_encoded_at_rewrite() owner=tappa_owner definer=false execute=-
function tappa_forbid_mutation() owner=tappa_owner definer=false execute=-
function tappa_forbid_revocation_reset() owner=tappa_owner definer=false execute=-
function tappa_forbid_unencoded_activation() owner=tappa_owner definer=false execute=-
function tappa_local_month_start(date,text) owner=tappa_owner definer=false execute=tappa_app,tappa_opdefiner
function webhook_delivery_settle() owner=tappa_owner definer=false execute=-
namespace public owner=pg_database_owner
rel admin_identities r owner=tappa_owner rls=enable,force
rel admin_identity_events r owner=tappa_owner rls=enable,force
rel admin_identity_resets r owner=tappa_owner rls=enable,force
rel admin_identity_verifications r owner=tappa_owner rls=enable,force
rel admin_invites r owner=tappa_owner rls=enable,force
rel admin_signup_claims r owner=tappa_owner rls=enable,force
rel admin_sessions r owner=tappa_owner rls=enable,force
rel admin_users r owner=tappa_owner rls=enable,force
rel api_idempotency r owner=tappa_owner rls=enable,force
rel api_keys r owner=tappa_owner rls=enable,force
rel audit_log r owner=tappa_owner rls=enable,force
rel billing_periods r owner=tappa_owner rls=enable,force
rel departments r owner=tappa_owner rls=enable,force
rel employee_invites r owner=tappa_owner rls=enable,force
rel employees r owner=tappa_owner rls=enable,force
rel feed_counters r owner=tappa_owner rls=enable,force
rel goose_db_version r owner=tappa_owner rls=no-enable,no-force
rel holding_members r owner=tappa_owner rls=enable,force
rel holdings r owner=tappa_owner rls=enable,force
rel invite_requests r owner=tappa_owner rls=enable,force
rel legal_documents r owner=tappa_owner rls=enable,force
rel locations r owner=tappa_owner rls=enable,force
rel operator_audit_log r owner=tappa_owner rls=enable,force
rel operator_read_tickets r owner=tappa_owner rls=enable,force
rel password_resets r owner=tappa_owner rls=enable,force
rel platform_admins r owner=tappa_owner rls=enable,force
rel platform_sessions r owner=tappa_owner rls=enable,force
rel policies r owner=tappa_owner rls=enable,force
rel policy_attachments r owner=tappa_owner rls=enable,force
rel policy_versions r owner=tappa_owner rls=enable,force
rel punch_events r owner=tappa_owner rls=enable,force
rel sessions r owner=tappa_owner rls=enable,force
rel site_punches r owner=tappa_owner rls=enable,force
rel tags r owner=tappa_owner rls=enable,force
rel tenant_branding r owner=tappa_owner rls=enable,force
rel tenants r owner=tappa_owner rls=enable,force
rel transaction_reviews r owner=tappa_owner rls=enable,force
rel transactions r owner=tappa_owner rls=enable,force
rel webhook_deliveries r owner=tappa_owner rls=enable,force
rel webhook_endpoints r owner=tappa_owner rls=enable,force
schema public public USAGE
schema public tappa_app USAGE
schema public tappa_feedwriter USAGE
schema public tappa_holdingdefiner USAGE
schema public tappa_identitydefiner USAGE
schema public tappa_opdefiner USAGE
schema public tappa_operator USAGE
schema public tappa_resolver USAGE
sequence goose_db_version_id_seq owner=tappa_owner
superuser tappa_owner
table admin_identity_events tappa_identitydefiner DELETE
table admin_signup_claims tappa_identitydefiner DELETE
table admin_sessions tappa_app SELECT
table api_idempotency tappa_app DELETE
table audit_log tappa_app SELECT
table billing_periods tappa_app SELECT,INSERT
table departments tappa_app SELECT,INSERT,UPDATE,DELETE
table employee_invites tappa_app SELECT,INSERT
table employees tappa_app SELECT,INSERT,UPDATE
table invite_requests tappa_app SELECT,DELETE
table locations tappa_app SELECT,INSERT,UPDATE,DELETE
table password_resets tappa_app SELECT
table policies tappa_app SELECT,INSERT,UPDATE
table policy_attachments tappa_app SELECT,INSERT,UPDATE,DELETE
table policy_versions tappa_app SELECT,INSERT
table sessions tappa_app SELECT,INSERT,UPDATE
table tags tappa_app INSERT
table tenant_branding tappa_app SELECT
table tenants tappa_app SELECT
table transaction_reviews tappa_app SELECT,INSERT
table transactions tappa_app SELECT,INSERT
table webhook_deliveries tappa_app DELETE
`

// privCatalogDiff compares a snapshot with the allowlist: the lines the database holds and the
// list does not (unexpected), and the lines the list holds and the database does not (missing).
func privCatalogDiff(got []string) (unexpected, missing []string) {
	want := map[string]bool{}
	for _, l := range strings.Split(privCatalogAllowed, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			want[l] = true
		}
	}
	have := map[string]bool{}
	for _, l := range got {
		have[l] = true
		if !want[l] {
			unexpected = append(unexpected, l)
		}
	}
	for l := range want {
		if !have[l] {
			missing = append(missing, l)
		}
	}
	slices.Sort(missing)
	return unexpected, missing
}

func privCatalogSnapshot(t *testing.T, ctx context.Context, q opQuerier) []string {
	t.Helper()
	rows, err := q.Query(ctx, privCatalogQuery)
	if err != nil {
		t.Fatalf("read the privilege catalogue: %v", err)
	}
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("read the privilege catalogue: %v", err)
	}
	return lines
}

// privCatalogPremise is m11schema_test.go's: the chain must have run under production's wide
// default ACL, or a forgotten REVOKE hides (and a privilege the default would hand out is missing
// here, which is red for the wrong reason). Red in CI, skipped -- not passed -- elsewhere.
func privCatalogPremise(t *testing.T, ctx context.Context, q opQuerier) {
	t.Helper()
	if w := m11ArwdWitness(t, ctx, q); !w.wide || !w.beforeTheChain {
		report := t.Skipf
		if os.Getenv("CI") != "" {
			report = t.Fatalf
		}
		report("this database was NOT migrated under production's default ACL: %s -- the catalogue it holds is not "+
			"the one production builds, so this test proves nothing here (THIS IS NOT A PASS); see "+
			"TestM11Catalog_EveryNewTableMatchesTheMatrixUnderArwd for how to rebuild it", w.why)
	}
}

// TestPrivCatalog_TheWholeCatalogueIsTheAllowlist: every relation, table and column privilege,
// sequence, function and schema privilege of public, for every role of privCatalogRoles, is
// exactly privCatalogAllowed.
func TestPrivCatalog_TheWholeCatalogueIsTheAllowlist(t *testing.T) {
	ctx, tx := opTx(t)
	privCatalogPremise(t, ctx, tx)
	got := privCatalogSnapshot(t, ctx, tx)
	if os.Getenv("TAPPA_PRIVCATALOG_DUMP") != "" {
		t.Logf("the live catalogue (%d lines):\n%s", len(got), strings.Join(got, "\n"))
	}
	if n := strings.Count(privCatalogAllowed, "\nrel "); n < 30 {
		t.Fatalf("anti-vacuity: the allowlist names %d relations", n)
	}
	unexpected, missing := privCatalogDiff(got)
	for _, l := range unexpected {
		t.Errorf("the database holds a line the allowlist does not -- a privilege, an object or an RLS switch "+
			"nobody wrote down (add it to privCatalogAllowed only with the migration that means it):\n  %s", l)
	}
	for _, l := range missing {
		t.Errorf("the allowlist holds a line the database does not (a grant, a FORCE or an object went away -- "+
			"or the list is stale):\n  %s", l)
	}
}

// TestPrivCatalog_EveryRoleIsRead: the roles the snapshot reads (privCatalogRolesSQL -- every role
// but the pg_* ones and the superusers, plus PUBLIC) are exactly privCatalogRoles. A role outside
// the list is already red in the catalogue test (it inherits PUBLIC's lines); this one names it,
// and a listed role that went away is red here too.
func TestPrivCatalog_EveryRoleIsRead(t *testing.T) {
	ctx, tx := opTx(t)
	rows, err := tx.Query(ctx, `SELECT r FROM (`+privCatalogRolesSQL+`) s ORDER BY r COLLATE "C"`)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) < len(privCatalogRoles) {
		t.Errorf("anti-vacuity: the snapshot reads %d roles %q, want at least %d", len(roles), roles, len(privCatalogRoles))
	}
	for _, r := range roles {
		if !slices.Contains(privCatalogRoles, r) {
			t.Errorf("role %s exists and privCatalogRoles does not name it: add it, regenerate the allowlist, read it", r)
		}
	}
	for _, r := range privCatalogRoles {
		if !slices.Contains(roles, r) {
			t.Errorf("privCatalogRoles names %s and the snapshot does not read it (gone, or a superuser now)", r)
		}
	}
}

// TestPrivCatalog_TheDiffSeesEachKind drives the comparison with one change of each kind, each in
// a savepoint that is rolled back (its locks with it). Each must surface as an unexpected line --
// so the test above is not green by construction. The tables that take an ACCESS EXCLUSIVE lock
// here (owner, RLS switches) are operator tables: opTx holds the operator-tables test lock, so no
// other package's test is using them meanwhile.
func TestPrivCatalog_TheDiffSeesEachKind(t *testing.T) {
	ctx, tx := opTx(t)
	privCatalogPremise(t, ctx, tx)
	if u, m := privCatalogDiff(privCatalogSnapshot(t, ctx, tx)); len(u)+len(m) != 0 {
		t.Skipf("the live catalogue already differs from the allowlist (%d/%d lines); "+
			"TestPrivCatalog_TheWholeCatalogueIsTheAllowlist names them", len(u), len(m))
	}
	var dbName string
	if err := tx.QueryRow(ctx, `SELECT current_database()`).Scan(&dbName); err != nil {
		t.Fatal(err)
	}
	database := pgx.Identifier{dbName}.Sanitize()
	for _, c := range []struct{ what, stmt, want string }{
		// fix1: T137's three ORTA grants, a FORCE taken away, PUBLIC, a column grant, a new table.
		{"DELETE on employees to tappa_app (the three ORTA cases)",
			`GRANT DELETE ON public.employees TO tappa_app`, "table employees tappa_app SELECT,INSERT,UPDATE,DELETE"},
		{"FORCE taken away", `ALTER TABLE public.platform_sessions NO FORCE ROW LEVEL SECURITY`,
			"rel platform_sessions r owner=tappa_owner rls=enable,no-force"},
		{"EXECUTE to PUBLIC", `GRANT EXECUTE ON FUNCTION public.resolve_tag_by_uid(character) TO PUBLIC`,
			"function resolve_tag_by_uid(character) owner=tappa_resolver definer=true execute=public,tappa_app,tappa_feedwriter,tappa_holdingdefiner,tappa_identitydefiner,tappa_opdefiner,tappa_operator,tappa_resolver"},
		{"a column grant", `GRANT SELECT (token_hash) ON public.api_keys TO tappa_operator`,
			"column api_keys tappa_operator SELECT token_hash"},
		{"a new table", `CREATE TABLE public.zz_privcatalog_probe (id int)`,
			"rel zz_privcatalog_probe r owner=tappa_owner rls=no-enable,no-force"},

		// low round (1): a schema other than public.
		{"a SECURITY DEFINER function in another schema, EXECUTE to tappa_app",
			`CREATE SCHEMA zz_privcatalog_probe;
			 CREATE FUNCTION zz_privcatalog_probe.f() RETURNS int LANGUAGE sql SECURITY DEFINER AS 'SELECT 1';
			 REVOKE EXECUTE ON FUNCTION zz_privcatalog_probe.f() FROM PUBLIC;
			 GRANT EXECUTE ON FUNCTION zz_privcatalog_probe.f() TO tappa_app`,
			"function zz_privcatalog_probe.f() owner=tappa_owner definer=true execute=tappa_app"},
		// (2): a role whose name is not tappa_*.
		{"a role outside tappa_* with SELECT on a table",
			`CREATE ROLE zz_privcatalog_probe NOLOGIN; GRANT SELECT ON public.employees TO zz_privcatalog_probe`,
			"table employees zz_privcatalog_probe SELECT"},
		// (3): PG17's MAINTAIN (VACUUM, ANALYZE, CLUSTER, REFRESH, REINDEX, LOCK TABLE).
		{"MAINTAIN on transactions", `GRANT MAINTAIN ON public.transactions TO tappa_app`,
			"table transactions tappa_app SELECT,INSERT,MAINTAIN"},
		// (4): a grant option, one per kind of object.
		{"a table grant option", `GRANT SELECT ON public.transactions TO tappa_app WITH GRANT OPTION`,
			"table transactions tappa_app SELECT*,INSERT"},
		{"a column grant option", `GRANT SELECT (token_hash) ON public.api_keys TO tappa_resolver WITH GRANT OPTION`,
			"column api_keys tappa_resolver SELECT* token_hash"},
		{"a function grant option",
			`GRANT EXECUTE ON FUNCTION public.resolve_tag_by_uid(character) TO tappa_app WITH GRANT OPTION`,
			"function resolve_tag_by_uid(character) owner=tappa_resolver definer=true execute=tappa_app*,tappa_resolver"},
		{"a sequence grant option", `GRANT SELECT ON SEQUENCE public.goose_db_version_id_seq TO tappa_app WITH GRANT OPTION`,
			"seqpriv goose_db_version_id_seq tappa_app SELECT*"},
		{"a schema grant option", `GRANT USAGE ON SCHEMA public TO tappa_app WITH GRANT OPTION`,
			"schema public tappa_app USAGE*"},
		{"a database grant option", `GRANT CONNECT ON DATABASE ` + database + ` TO tappa_app WITH GRANT OPTION`,
			"database tappa_app CONNECT*,TEMPORARY"},
		// (5): the database.
		{"CREATE on the database", `GRANT CREATE ON DATABASE ` + database + ` TO tappa_app`,
			"database tappa_app CONNECT,CREATE,TEMPORARY"},
		// (6): an extension (pg_trgm ships with the image and needs no network).
		{"a new extension", `CREATE EXTENSION pg_trgm`, "extension pg_trgm schema=public"},

		// (7): kinds the catalogue test always caught, pinned here.
		{"an owner changed", `ALTER TABLE public.operator_read_tickets OWNER TO tappa_app`,
			"rel operator_read_tickets r owner=tappa_app rls=enable,force"},
		{"a definer flag set", `ALTER FUNCTION public.tappa_local_month_start(date, text) SECURITY DEFINER`,
			"function tappa_local_month_start(date,text) owner=tappa_owner definer=true execute=tappa_app,tappa_opdefiner"},
		{"RLS disabled", `ALTER TABLE public.platform_admins DISABLE ROW LEVEL SECURITY`,
			"rel platform_admins r owner=tappa_owner rls=no-enable,force"},
		{"a role membership", `GRANT tappa_operator TO tappa_app`,
			"function op_close_session(text) owner=tappa_opdefiner definer=true execute=tappa_app,tappa_opdefiner,tappa_operator"},
		{"CREATE on a schema", `GRANT CREATE ON SCHEMA public TO tappa_app`,
			"schema public tappa_app CREATE,USAGE"},
		{"USAGE on a sequence", `GRANT USAGE ON SEQUENCE public.goose_db_version_id_seq TO tappa_app`,
			"seqpriv goose_db_version_id_seq tappa_app USAGE"},
		{"a new view", `CREATE VIEW public.zz_privcatalog_view AS SELECT 1 AS x`,
			"rel zz_privcatalog_view v owner=tappa_owner rls=no-enable,no-force"},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Exec(ctx, c.stmt); err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		unexpected, _ := privCatalogDiff(privCatalogSnapshot(t, ctx, sp))
		if !slices.Contains(unexpected, c.want) {
			t.Errorf("%s: the diff does not name %q; it names %q", c.what, c.want, unexpected)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}
