package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// T137 (2026-10-19): the R5b rules read the TEXT of a top-level statement, so four things stood
// outside them -- dynamic SQL in a body, a privilege given to PUBLIC, a string form the lexer
// misread (`E'…'`) and, measured in this round, a GRANT that is not the first word of its statement
// (`CREATE SCHEMA … GRANT …`). These tests pin each closure with the red case the backlog measured
// (exit 0 before) and its green neighbours. The lexical gate is not the last one: internal/db's
// TestRLS_AppCannotMutateTransactions, TestRLS_AppCannotMutatePolicyVersions,
// TestAppendOnly_AppHasNoTruncatePrivilege and the privilege-matrix tests measure the effective
// privilege as tappa_app.

const t137File = "db/migrations/00099_zz_probe.sql"

func t137Migration(up string) string {
	return "-- +goose Up\n" + up + "\n-- +goose Down\nSELECT 1;\n"
}

type t137Case struct{ what, up, want string }

// t137Run runs a table's cases in two trees, not one tree per case (T139): every red case side by
// side, each its own migration file, and every green case side by side the same way. A red case must
// get an R5b FAIL naming ITS file and carrying want (so the tree's exit is non-zero); the green tree
// must exit 0 with no R5b FAIL at all -- each green case is then a file the whole scan passed, which
// is what a tree of its own proved. The R5b rules this file pins read one migration at a time (the
// only name-keyed ones are R5B_BODY_DDL's pins and the 00058 waiver, and no case here is named like
// them), so a neighbour can add a FAIL but cannot take one away. Why: one redline-check run costs
// about a second of CPU whatever the tree (gawk, the CI's awk: measured 0.87 s on a one-file tree),
// and these tables were ~95 runs -- with the other T137 tests, cmd/tappa went from 270 s to 1115 s
// in CI's `make check` (runs 39184166839 and 39204779143).
func t137Run(t *testing.T, cases []t137Case) {
	t.Helper()
	t.Parallel()
	name := func(i int) string { return fmt.Sprintf("db/migrations/00099_zz_probe_%02d.sql", i) }
	red, green := map[string]string{}, map[string]string{}
	for i, c := range cases {
		if c.want == "" {
			green[name(i)] = t137Migration(c.up)
		} else {
			red[name(i)] = t137Migration(c.up)
		}
	}
	var redOut, greenOut string
	var redCode, greenCode int
	if len(red) > 0 {
		redOut, redCode = redlineRun(t, red)
	}
	if len(green) > 0 {
		greenOut, greenCode = redlineRun(t, green)
	}
	redFail, greenFail := failSection(redOut, "R5b"), failSection(greenOut, "R5b")
	for i, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			if c.want == "" {
				if greenCode != 0 || greenFail != "" {
					t.Fatalf("the green cases' tree: want exit 0 and no R5b FAIL, got exit %d (this case is %s):\n%s", greenCode, name(i), greenOut)
				}
				return
			}
			if redCode == 0 || !strings.Contains(redFail, name(i)+": "+c.want) {
				t.Fatalf("want a non-zero exit and an R5b FAIL %q on %s, got exit %d, FAIL:\n%s", c.want, name(i), redCode, redFail)
			}
		})
	}
}

// TestRedline_T137_TheLexerReadsWhatPostgresReads: every case hides a GRANT on an append-only
// table from the old sql_lex (measured exit 0 for each against the previous lexer) and each is
// what PostgreSQL 17 lexes as a real GRANT between two harmless statements.
func TestRedline_T137_TheLexerReadsWhatPostgresReads(t *testing.T) {
	const grant = "GRANT DELETE ON public.transactions TO tappa_app;"
	const appendOnly = "append-only tabloya UPDATE/DELETE yetkisi"
	t137Run(t, []t137Case{
		{"control: the bare grant", grant, appendOnly},
		{"the backlog's E'' (a backslash quote does not close it)", "SELECT E'\\'';\n" + grant + "\nSELECT 'x';", appendOnly},
		{"an E'' with a doubled quote then a backslash quote", "SELECT e'a''\\'';\n" + grant + "\nSELECT 'x';", appendOnly},
		{"a named dollar quote holding a block comment opener", "SELECT $x$/*$x$;\n" + grant + "\nSELECT $x$*/$x$;", appendOnly},
		{"a quoted identifier holding a block comment opener", "SELECT 1 AS \"/*\";\n" + grant + "\nSELECT 1 AS \"*/\";", appendOnly},
		{"a quoted identifier holding a line comment opener", "SELECT 1 AS \"a--\"; " + grant, appendOnly},
		{"a nested block comment with a quote in its tail", "/* a /* b */ it's */\n" + grant + "\nSELECT 'x';", appendOnly},
		{"a dollar sign inside an identifier opens no quote", "SELECT 1 AS a$$;\n" + grant + "\nSELECT 1 AS b$$;", appendOnly},
		// Green: harmless text inside an E'' string, a GRANT-shaped sentence included, stays data.
		{"an E'' holding harmless text", "COMMENT ON TABLE public.employees IS E'grant delete on public.transactions to tappa_app \\' ; still the string';", ""},
		{"an E'' with a newline escape", "SELECT E'line\\none';", ""},
	})
}

// TestRedline_T137_PublicIsEveryRole: a GRANT whose grantee list holds PUBLIC is an R5b FAIL
// whatever the privilege and the object (measured: SELECT to PUBLIC on employees gives
// tappa_operator SELECT, EXECUTE to PUBLIC on an op_* function gives tappa_app EXECUTE).
// REVOKE … FROM PUBLIC -- which every op_* and resolver function carries -- is not one.
func TestRedline_T137_PublicIsEveryRole(t *testing.T) {
	const public = "PUBLIC'e yetki"
	t137Run(t, []t137Case{
		{"the backlog's UPDATE on employees", "GRANT UPDATE ON public.employees TO PUBLIC;", public},
		{"SELECT to PUBLIC", "GRANT SELECT ON public.employees TO PUBLIC;", public},
		{"PUBLIC second in a list", "GRANT SELECT ON public.employees TO tappa_app, PUBLIC;", public},
		{"PUBLIC first in a list", "GRANT SELECT ON public.employees TO public , tappa_app;", public},
		{"a quoted \"public\"", "GRANT SELECT ON public.employees TO \"public\";", public},
		{"GROUP PUBLIC", "GRANT SELECT ON public.employees TO GROUP PUBLIC;", public},
		{"with grant option", "GRANT SELECT ON public.employees TO PUBLIC WITH GRANT OPTION;", public},
		{"granted by", "GRANT SELECT ON public.employees TO PUBLIC GRANTED BY tappa_owner;", public},
		{"EXECUTE on an operator function", "GRANT EXECUTE ON FUNCTION public.op_zz_probe(text) TO PUBLIC;", public},
		{"across lines", "GRANT INSERT\n  ON public.employees\n  TO\n  PUBLIC;", public},
		{"DELETE on transactions (the append-only rule fired here before)", "GRANT DELETE ON public.transactions TO PUBLIC;", public},
		{"REVOKE from PUBLIC", "REVOKE ALL ON FUNCTION public.op_zz_probe(text) FROM PUBLIC;", ""},
		{"a grantee named like public", "GRANT SELECT ON public.employees TO public_reader;", ""},
		{"an object named like public", "GRANT SELECT ON public.public_holidays TO tappa_app;", ""},
	})

	// The Down is read too: a rollback runs in production.
	body := "-- +goose Up\nSELECT 1;\n-- +goose Down\nGRANT SELECT ON public.employees TO PUBLIC;\n"
	out, code := redlineRun(t, map[string]string{t137File: body})
	if code == 0 || !strings.Contains(failSection(out, "R5b"), t137File+": "+public) {
		t.Fatalf("a GRANT to PUBLIC in the Down: exit %d:\n%s", code, out)
	}
}

// TestRedline_T137_AGrantInsideAStatement: PostgreSQL accepts a GRANT as a schema element and
// applies it to a table of ANOTHER schema (measured: CREATE SCHEMA zz GRANT DELETE ON
// public.employees TO tappa_app -> has_table_privilege t); no `^grant` rule saw it.
func TestRedline_T137_AGrantInsideAStatement(t *testing.T) {
	const inside = "GRANT bir ifadenin ICINDE"
	t137Run(t, []t137Case{
		{"CREATE SCHEMA … GRANT", "CREATE SCHEMA zz GRANT DELETE ON public.transactions TO tappa_app;", inside},
		{"CREATE SCHEMA … CREATE TABLE … GRANT", "CREATE SCHEMA zz CREATE SEQUENCE zz_s GRANT SELECT ON public.employees TO tappa_app;", inside},
		{"with grant option", "GRANT SELECT ON public.employees TO tappa_resolver WITH GRANT OPTION;", ""},
		{"revoke grant option for", "REVOKE GRANT OPTION FOR SELECT ON public.employees FROM tappa_resolver;", ""},
		{"the word in a string", "COMMENT ON TABLE public.employees IS 'grant nothing here';", ""},
		{"the word in a quoted identifier", "SELECT 1 AS \"grant\";", ""},
	})
}

// TestRedline_T137_DynamicSQLAndDDLInABody: a DO or function body whose CODE (comments and strings
// out) holds EXECUTE, a DDL verb or a nested DO is an R5b FAIL in a migration R5B_BODY_DDL does not
// pin; a body whose code cannot be read (an E-string or U&-string body) is one in every migration.
func TestRedline_T137_DynamicSQLAndDDLInABody(t *testing.T) {
	const body = "GOVDEDE dinamik SQL / DDL kodu"
	const escaped = "KACISLI dize GOVDE"
	do := func(tag, code string) string {
		return "-- +goose StatementBegin\nDO " + tag + "\nBEGIN\n" + code + "\nEND\n" + tag + ";\n-- +goose StatementEnd"
	}
	t137Run(t, []t137Case{
		{"G1: EXECUTE builds a GRANT on transactions", do("$$", "EXECUTE 'GRANT DELETE ON public.transactions TO tappa_app';"), body},
		{"EXECUTE format() turns RLS off", do("$$", "EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', 'transactions');"), body},
		{"EXECUTE in a named dollar quote", do("$x$", "EXECUTE 'DROP POLICY transactions_tenant_isolation ON public.transactions';"), body},
		{"EXECUTE after a body comment with an apostrophe", do("$$", "-- it's a note\nEXECUTE 'GRANT DELETE ON public.transactions TO tappa_app';"), body},
		{"EXECUTE after an E'' string in the body", do("$$", "RAISE NOTICE E'\\'';\nEXECUTE 'SELECT 1';\nRAISE NOTICE 'x';"), body},
		{"EXECUTE of a dollar-quoted string", do("$$", "EXECUTE $q$GRANT DELETE ON public.transactions TO tappa_app$q$;"), body},
		{"a static GRANT in a DO", do("$$", "GRANT DELETE ON public.transactions TO tappa_app;"), body},
		{"a static RLS switch-off in a DO", do("$$", "ALTER TABLE public.transactions DISABLE ROW LEVEL SECURITY;"), body},
		{"a static TRUNCATE in a DO", do("$$", "TRUNCATE public.transactions;"), body},
		{"a DO inside a DO", do("$$", "DO $x$ BEGIN PERFORM 1; END $x$;"), body},
		{"a DO with a quoted body", "DO 'BEGIN EXECUTE ''GRANT DELETE ON public.transactions TO tappa_app''; END';", body},
		{"a DO with LANGUAGE first", "DO LANGUAGE plpgsql 'BEGIN GRANT DELETE ON public.transactions TO tappa_app; END';", body},
		{"RETURN QUERY EXECUTE in a function", "CREATE FUNCTION public.zz_probe() RETURNS SETOF int LANGUAGE plpgsql AS $$\nBEGIN RETURN QUERY EXECUTE 'SELECT 1'; END\n$$;", body},
		{"a SQL function with a quoted body", "CREATE OR REPLACE FUNCTION public.zz_probe() RETURNS void LANGUAGE sql AS 'GRANT DELETE ON public.transactions TO tappa_app';", body},
		// Fail-closed by design: every top-level dollar string is read as a body, whatever the
		// statement -- a string can be handed to something that runs it.
		{"a dollar string outside a routine", "SELECT $x$ GRANT DELETE ON public.transactions TO tappa_app $x$;", body},
		{"a procedure", "CREATE PROCEDURE public.zz_probe() LANGUAGE plpgsql AS $$ BEGIN EXECUTE 'SELECT 1'; END $$;", body},
		{"an E'' DO body", "DO E'BEGIN \\x45XECUTE ''SELECT 1''; END';", escaped},
		{"a U&'' DO body", "DO U&'BEGIN \\0045XECUTE ''SELECT 1''; END';", escaped},
		{"an E'' function body", "CREATE FUNCTION public.zz_probe() RETURNS void LANGUAGE plpgsql AS E'BEGIN NULL; END';", escaped},
		// Green: the words in a comment or a string are not code; `DO NOTHING`, `EXECUTE FUNCTION` and
		// `GRANT EXECUTE` at the top level are not a body.
		{"the words in a body comment and a privilege string", do("$$",
			"-- execute, grant, alter: none of it runs\nIF NOT pg_catalog.has_function_privilege('tappa_app', 'public.zz_probe()', 'EXECUTE') THEN\n  RAISE EXCEPTION 'grant missing; create it, then alter nothing';\nEND IF;"), ""},
		{"ON CONFLICT DO NOTHING in a function", "CREATE FUNCTION public.zz_probe() RETURNS void LANGUAGE sql AS $$\nINSERT INTO public.zz (a) VALUES (1) ON CONFLICT DO NOTHING\n$$;\nREVOKE ALL ON FUNCTION public.zz_probe() FROM PUBLIC;\nGRANT EXECUTE ON FUNCTION public.zz_probe() TO tappa_app;", ""},
		{"a trigger's EXECUTE FUNCTION", "CREATE TRIGGER zz BEFORE UPDATE ON public.employees FOR EACH ROW EXECUTE FUNCTION public.zz_probe();", ""},
		{"identifiers that hold the words", do("$$", "PERFORM created_at, dropped_by, \"grant\" FROM public.zz;"), ""},
		{"a plain quoted string in a function header", "CREATE FUNCTION public.zz_probe() RETURNS void LANGUAGE plpgsql SET search_path = '' AS $$ BEGIN NULL; END $$;", ""},
	})
}

// TestRedline_T137_StringAndEnvironmentSwitches: a migration that changes how `'…'` is lexed, or
// whose text comes from the environment at run time, is an R5b FAIL.
func TestRedline_T137_StringAndEnvironmentSwitches(t *testing.T) {
	t137Run(t, []t137Case{
		{"standard_conforming_strings off", "SET standard_conforming_strings = off;", "standard_conforming_strings/backslash_quote"},
		{"set_config of it", "SELECT pg_catalog.set_config('standard_conforming_strings', 'off', true);", "standard_conforming_strings/backslash_quote"},
		{"backslash_quote", "SET backslash_quote = on;", "standard_conforming_strings/backslash_quote"},
		{"goose ENVSUB", "-- +goose ENVSUB ON\nGRANT SELECT ON public.employees TO ${ROLE};", "goose ENVSUB"},
	})
}

// TestRedline_T137_TheBodyWhitelistIsPinned: the nine shipped migrations whose bodies hold static
// DDL are an R5b WARN, each by name; the whole shipped tree exits 0 with no R5b FAIL (so the 40-odd
// `REVOKE … FROM PUBLIC` and every shipped body stay green). A pinned file whose body code -- or,
// since fix1, a string constant inside the body -- changes, and a copy of one under a new name, are
// FAILs; a comment inside a pinned body is not code.
func TestRedline_T137_TheBodyWhitelistIsPinned(t *testing.T) {
	dir := filepath.Join(repoRoot, "db", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	tree := map[string]string{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		tree["db/migrations/"+e.Name()] = string(b)
	}
	const warned = "govdede statik DDL"
	out, code := redlineRun(t, tree)
	if code != 0 || failSection(out, "R5b") != "" {
		t.Fatalf("the shipped migrations: want exit 0 and no R5b FAIL, got exit %d:\n%s", code, out)
	}
	var got []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, warned) {
			got = append(got, strings.TrimSpace(l[:strings.Index(l, ":")]))
		}
	}
	sort.Strings(got)
	want := []string{
		"db/migrations/00027_move_legal_publishing_to_the_operator.sql",
		"db/migrations/00029_read_tenants_from_the_operator.sql",
		"db/migrations/00030_read_plaques_from_the_operator.sql",
		"db/migrations/00031_read_the_operator_audit_log.sql",
		"db/migrations/00032_read_billing_from_the_operator.sql",
		"db/migrations/00033_audit_opadmin_actions.sql",
		"db/migrations/00034_recheck_a_tenants_vat_from_the_operator.sql",
		"db/migrations/00039_open_the_api_from_the_operator.sql",
		"db/migrations/00042_put_holdings_on_the_operator_surface.sql",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the WARN names %v, want %v", got, want)
	}

	const name = "00027_move_legal_publishing_to_the_operator.sql"
	shipped := tree["db/migrations/"+name]
	const tail = "'logout'));\n    END IF;\nEND\n$$;\n" // the Down's DO, the pinned body that holds the DDL
	if strings.Count(shipped, tail) != 1 {
		t.Fatalf("PREMISE: %s carries %q %d times, want once", name, tail, strings.Count(shipped, tail))
	}
	for _, c := range []struct{ what, file, body, want string }{
		{"a comment inside the pinned body", name,
			strings.Replace(shipped, tail, "'logout'));\n    END IF;\n    -- it's a note: EXECUTE nothing\nEND\n$$;\n", 1), ""},
		{"an RLS switch-off added to the pinned body", name,
			strings.Replace(shipped, tail, "'logout'));\n    END IF;\n    ALTER TABLE public.transactions DISABLE ROW LEVEL SECURITY;\nEND\n$$;\n", 1), "pinle AYNI DEGIL"},
		{"a string literal changed inside the pinned body (fix1: the pin holds string contents)", name,
			strings.Replace(shipped, tail, "'logoff'));\n    END IF;\nEND\n$$;\n", 1), "pinle AYNI DEGIL"},
		{"an EXECUTE added to the pinned body", name,
			strings.Replace(shipped, tail, "'logout'));\n    END IF;\n    EXECUTE 'GRANT DELETE ON public.transactions TO tappa_app';\nEND\n$$;\n", 1), "pinle AYNI DEGIL"},
		{"a second body (a quoted DO) in the pinned file", name,
			shipped + "DO 'BEGIN ALTER TABLE public.operator_audit_log ADD CONSTRAINT zz CHECK (true); END';\n", "pinle AYNI DEGIL"},
		{"the pinned file under a new name", "00099_move_legal_publishing_to_the_operator.sql", shipped, "GOVDEDE dinamik SQL / DDL kodu"},
	} {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			out, code := redlineRun(t, map[string]string{"db/migrations/" + c.file: c.body})
			fail := failSection(out, "R5b")
			if c.want == "" {
				if code != 0 || fail != "" || !strings.Contains(out, "db/migrations/"+c.file+": "+warned) {
					t.Fatalf("want exit 0, no R5b FAIL and the WARN, got exit %d:\n%s", code, out)
				}
				return
			}
			if code == 0 || !strings.Contains(fail, "db/migrations/"+c.file+": GOVDEDE") || !strings.Contains(fail, c.want) {
				t.Fatalf("want a non-zero exit and an R5b FAIL %q, got exit %d, FAIL %q:\n%s", c.want, code, fail, out)
			}
		})
	}
}

// TestRedline_T137_AStringContinuesAfterALineBreak (fix1, ORTA 1 and 2): PostgreSQL continues a
// string constant when only whitespace -- `--` comments included -- with at least one line break
// stands between its closing quote and a new `'`, and the continued part keeps the FIRST part's
// mode: `E'a'` + line break + `'\”` is one E-string, which `\'` does not close (measured on 17:
// `SELECT E'a'` / `'\”` returns `a'`). Both lexers now continue it; an E/U&-string continuation is
// an R5b FAIL on top (the shipped tree continues plain strings 23 times, every one a COMMENT ON
// text, and an escaped one never), and a quoted DO body is read as the joined text PostgreSQL runs.
func TestRedline_T137_AStringContinuesAfterALineBreak(t *testing.T) {
	const cont = "KACISLI dizenin DEVAMI"
	const appendOnly = "append-only tabloya UPDATE/DELETE yetkisi"
	const body = "GOVDEDE dinamik SQL / DDL kodu"
	const employees = "GRANT DELETE ON public.employees TO tappa_app;"
	const grant = "GRANT DELETE ON public.transactions TO tappa_app;"
	t137Run(t, []t137Case{
		// The third eye's case, byte for byte: the GRANT on employees is no rule's subject when it is
		// visible, so the continuation itself is the FAIL.
		{"the audit's E'' continued (00126)", "SELECT E'a'\n'\\'';\n" + employees + "\nSELECT 'x';", cont},
		{"a line comment before the line break", "SELECT E'a' -- note\n'\\'';\n" + employees + "\nSELECT 'x';", cont},
		{"a comment line and a blank line between", "SELECT E'a'\n  -- note\n\n  '\\'';\n" + employees + "\nSELECT 'x';", cont},
		{"a carriage return is a line break", "SELECT E'a'\r'\\'';\n" + employees + "\nSELECT 'x';", cont},
		{"a U&'' continued", "SELECT U&'a'\n'b';", cont},
		// What the lexer now reads: the GRANT after the continued E'' is code.
		{"the GRANT after a continued E'' is seen", "SELECT E'a'\n'\\'';\n" + grant + "\nSELECT 'x';", appendOnly},
		// The audit's quoted body (00131) and its neighbours: the body is the joined text.
		{"the audit's DO body continued (00131)", "DO 'BEGIN EXEC'\n'UTE ''GRANT DELETE ON public.employees TO tappa_app''; END';", body},
		{"a DO body continued across a comment", "DO 'BEGIN GR' -- split\n'ANT DELETE ON public.transactions TO tappa_app; END';", body},
		{"a SQL function body continued", "CREATE FUNCTION public.zz_probe() RETURNS void LANGUAGE sql AS 'DR'\n'OP POLICY zz ON public.transactions';", body},
		// Green: a continued plain string is data (the shipped COMMENT ON shape) and a continued body
		// with no DDL is a body with no DDL.
		{"a continued COMMENT ON text", "COMMENT ON COLUMN public.employees.email IS 'grant, drop '\n  'and alter: none of it runs';", ""},
		{"a harmless continued DO body", "DO 'BEGIN PERFORM 1; '\n'END';", ""},
		{"two strings on one line are not a continuation", "SELECT E'a\\'b', 'c';", ""},
	})

	out, code := redlineRun(t, map[string]string{t137File: t137Migration("SELECT E'a'\n'\\'';\n" + grant + "\nSELECT 'x';")})
	if fail := failSection(out, "R5b"); code == 0 || !strings.Contains(fail, t137File+": "+cont) {
		t.Fatalf("the continued E'' before a GRANT on transactions: want the continuation FAIL too, got exit %d:\n%s", code, out)
	}
}

// TestRedline_T137_ASettingIsNamedByALiteral (fix1, ORTA 3): `set_config('standard_' ||
// 'conforming_strings', 'off', true)` turns the setting off without its name appearing anywhere
// (measured: the next `'x\”` then closes in PostgreSQL and not in the lexer, and the GRANT behind it
// ran -- exit 0). A set_config's first argument must be one plain literal on R5B_SET_CONFIG's list;
// pg_settings, SET NAMES and a U&"…" identifier (which spells a name with escapes) are FAILs too.
func TestRedline_T137_ASettingIsNamedByALiteral(t *testing.T) {
	const notLiteral = "set_config — ayar adi TEK bir duz dize sabiti degil"
	const switches = "standard_conforming_strings/backslash_quote/client_encoding"
	const other = "pg_settings / SET NAMES kodda"
	const uident = "U&\"…\" tanimlayicisi"
	notListed := func(name string) string { return "set_config — '" + name + "' R5B_SET_CONFIG listesinde degil" }
	do := func(code string) string { return "DO $$\nBEGIN\n" + code + "\nEND\n$$;" }
	t137Run(t, []t137Case{
		{"the audit's concatenated name (00133)", "SELECT pg_catalog.set_config('standard_' || 'conforming_strings', 'off', true);\nSELECT 'x\\'';\n" +
			"GRANT DELETE ON public.employees TO tappa_app;\nSELECT 'y';", notLiteral},
		{"a name in a variable, in a body", do("PERFORM pg_catalog.set_config(v_name, 'off', true);"), notLiteral},
		{"a name as an E-string", "SELECT set_config(E'standard_conforming_strings', 'off', true);", notLiteral},
		{"a name as a dollar string", "SELECT set_config($n$lock_timeout$n$, '1s', true);", notLiteral},
		{"a name cast to text", "SELECT set_config('lock_timeout'::text, '1s', true);", notLiteral},
		{"a quoted \"set_config\"", "SELECT \"set_config\"('lock_timeout', '1s', true);", notLiteral},
		{"a name continued across lines", "SELECT set_config('standard_'\n'conforming_strings', 'off', true);", notListed("standard_conforming_strings")},
		{"a literal name off the list", "SELECT set_config('search_path', 'zz, public', false);", notListed("search_path")},
		{"the name in mixed case", "SELECT set_config('Standard_Conforming_Strings', 'off', true);", switches},
		{"client_encoding", "SET client_encoding = 'SJIS';", switches},
		{"SET NAMES", "SET LOCAL NAMES 'SJIS';", other},
		{"pg_settings", "UPDATE pg_catalog.pg_settings SET setting = 'off' WHERE name = 'standard_' || 'conforming_strings';", other},
		{"pg_settings in a body", do("UPDATE pg_settings SET setting = 'off' WHERE name = v_name;"), other},
		{"a quoted \"pg_settings\"", "UPDATE pg_catalog.\"pg_settings\" SET setting = 'off' WHERE name = 'standard_' || 'conforming_strings';", other},
		{"a U&\"…\" setting name", "SET LOCAL U&\"standard\\005fconforming_strings\" = off;", uident},
		// Green: the shipped shapes.
		{"a listed literal at the top", "SELECT pg_catalog.set_config('lock_timeout', '1s', true);", ""},
		{"listed literals in a body", do("PERFORM pg_catalog.set_config('app.tenant_id', NEW.id::text, true);\n" +
			"PERFORM set_config ( 'jit' , 'off' , true);"), ""},
		{"the word in a comment and a string", do("-- set_config(v, 'off') is not called here\nRAISE NOTICE 'set_config(%)', 1;"), ""},
		{"a column named like it", "SELECT my_set_config FROM public.zz;", ""},
	})
}

// TestRedline_T137_ACommentIsNotABody (fix1, DÜŞÜK 7): `COMMENT ON … IS $$…$$` is a comment's
// text -- nothing runs it -- so its words are not a body's DDL; a real DO beside it still is.
func TestRedline_T137_ACommentIsNotABody(t *testing.T) {
	const body = "GOVDEDE dinamik SQL / DDL kodu"
	const comment = "COMMENT ON FUNCTION public.zz_f() IS $$Do not drop or alter this; create a new one instead.$$;"
	t137Run(t, []t137Case{
		{"the audit's COMMENT ON (00105)", comment, ""},
		{"a named dollar quote", "COMMENT ON TABLE public.employees IS $c$grant nothing; execute nothing$c$;", ""},
		{"a DO after the comment", comment + "\nDO $$ BEGIN ALTER TABLE public.transactions DISABLE ROW LEVEL SECURITY; END $$;", body},
		{"a SELECT of a dollar string is still a body", "SELECT $c$ drop $c$;", body},
	})
}
