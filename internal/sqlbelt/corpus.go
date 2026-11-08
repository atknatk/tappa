package sqlbelt

// Case is one statement every belt is driven over.
type Case struct {
	Name string
	SQL  string
	// Closes names the part of this package that turns the case red, so a mutation run knows
	// which guard it is proving.
	Closes string
}

// Planted are statements that scope NOTHING to the caller's tenant and that every one of the six
// belts must flag. Each belt package runs its own per-statement check over all of them in a test
// named TestBelt_SharedLexicalCorpus, which is what keeps the six from drifting: one corpus, one
// verdict, six readers.
//
// MEASURED AGAINST THE SIX PRE-T164 COPIES (2026-11-08, each copy's own per-statement logic run
// over this list at 8bb8bcf6): the eight E-string / dollar-quote / block-comment / quoted-identifier
// cases and "a literal fakes the only WHERE" passed ALL SIX; "a paren inside a literal" and "a paren
// in a trailing line comment" passed five (tenant's paren masking caught them); "a literal paren pair"
// passed tenant only. The FILTER case is a control and was flagged everywhere. Each guard named in
// Closes was then removed one at a time and the case turned red (the quoted-identifier refusal in
// five belts of six: tenant's block walk flags that case without it). The tables are real tenant-scoped
// ones (employees) because the belts derive that list from CREATE POLICY.
var Planted = []Case{
	{
		Name:   "an E-string's backslash-quote hides a top-level OR",
		SQL:    `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id AND e.full_name <> E'x\'' OR true`,
		Closes: "the E-string or the unterminated-literal refusal (lex)",
	},
	{
		Name:   "two E-strings balance the quotes and hide a top-level OR",
		SQL:    `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id AND e.full_name <> E'\'' OR true AND e.full_name <> E'\''`,
		Closes: "the E-string refusal (lex)",
	},
	{
		Name:   "two dollar quotes holding a quote hide a top-level OR",
		SQL:    `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id AND e.full_name <> $$it's$$ OR true AND e.full_name <> $$it's$$`,
		Closes: "the dollar-quote refusal (lex)",
	},
	{
		Name: "a dollar quote fakes the only WHERE",
		SQL: `SELECT $q$ WHERE e.tenant_id = @tenant_id LIMIT 1 $q$ AS x, e.id
		      FROM employees e`,
		Closes: "the dollar-quote refusal, at the BODY: the clause it fakes holds no unreadable form, so only a belt that calls Prepare sees it",
	},
	{
		Name:   "a block comment holding a quote hides a top-level OR",
		SQL:    `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id AND e.id = @id /* it's */ OR true`,
		Closes: "the block-comment or the unterminated-literal refusal (lex)",
	},
	{
		Name:   "a block comment holding a paren hides a top-level OR",
		SQL:    `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id AND e.id = @id /* ( */ OR true`,
		Closes: "the block-comment refusal (lex)",
	},
	{
		Name:   "a quoted identifier holding a quote hides a top-level OR",
		SQL:    `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id AND e."it's" IS NULL OR true`,
		Closes: "the quoted-identifier refusal (lex), for the belts' own quote-toggling list splitters",
	},
	{
		Name:   "a quoted identifier holding a paren hides a top-level OR",
		SQL:    `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id AND e."a(" IS NULL OR true`,
		Closes: "the quoted-identifier refusal (lex)",
	},
	{
		Name:   "a paren inside a literal ends the clause before its OR",
		SQL:    `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id AND e.full_name <> ')' OR true`,
		Closes: "literal blanking (lex)",
	},
	{
		Name:   "a literal paren pair swallows an OR (tenant's maskParens)",
		SQL:    `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id AND e.full_name <> '(' OR true AND e.full_name <> ')'`,
		Closes: "literal blanking (lex) in MaskParens",
	},
	{
		Name: "a paren in a trailing line comment ends the clause before its OR",
		SQL: `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id AND e.id = @id -- )
		        OR true`,
		Closes: "line-comment blanking (lex)",
	},
	{
		Name:   "a literal fakes the only WHERE",
		SQL:    `SELECT 'x WHERE e.tenant_id = @tenant_id LIMIT 1' AS x, e.id FROM employees e`,
		Closes: "literal blanking (lex)",
	},
	{
		Name: "a WHERE-less EXISTS subquery inside an aggregate FILTER",
		SQL: `SELECT count(*) FILTER (WHERE EXISTS (SELECT 1 FROM employees x)) AS n
		      FROM employees e WHERE e.tenant_id = @tenant_id`,
		Closes: "WhereClauses reads a FILTER whose parenthesis holds a SELECT (third-eye audit of 86689718, YUKSEK 1)",
	},
	{
		Name: "a WHERE-less IN subquery inside an aggregate FILTER",
		SQL: `SELECT count(*) FILTER (WHERE e.id IN (SELECT x.id FROM employees x)) AS n
		      FROM employees e WHERE e.tenant_id = @tenant_id`,
		Closes: "WhereClauses reads a FILTER whose parenthesis holds a SELECT (third-eye audit of 86689718, YUKSEK 1)",
	},
	{
		Name: "an inner subquery's ORDER BY and LIMIT cut the outer clause before its OR",
		SQL: `SELECT e.id FROM employees e
		      WHERE e.tenant_id = @tenant_id
		        AND e.id IN (SELECT x.id FROM employees x WHERE x.tenant_id = @tenant_id ORDER BY x.id LIMIT 1) OR true`,
		Closes: "WhereClauses searches its stop words at the clause's own depth (third-eye audit of 86689718, ORTA 2)",
	},
	{
		Name: "an inner subquery's GROUP BY cuts the outer clause before its OR",
		SQL: `SELECT e.id FROM employees e
		      WHERE e.tenant_id = @tenant_id
		        AND e.location_id IN (SELECT x.location_id FROM employees x WHERE x.tenant_id = @tenant_id GROUP BY x.location_id) OR true`,
		Closes: "WhereClauses searches its stop words at the clause's own depth (third-eye audit of 86689718, ORTA 2)",
	},
	{
		Name: "a WHERE-less scalar subquery in the select list",
		SQL: `SELECT e.id, (SELECT count(*) FROM sessions) AS n
		      FROM employees e WHERE e.tenant_id = @tenant_id`,
		Closes: "WherelessSubqueries, now in all six belts (third-eye audit of 86689718: adminauth and review passed it before and after T164)",
	},
	{
		Name: "a WHERE-less EXISTS in the WHERE",
		SQL: `SELECT e.id FROM employees e
		      WHERE e.tenant_id = @tenant_id AND EXISTS (SELECT 1 FROM sessions s)`,
		Closes: "WherelessSubqueries, now in all six belts (third-eye audit of 86689718: adminauth and review passed it before and after T164)",
	},
	{
		Name: "the only tenant predicate sits inside an aggregate FILTER",
		SQL: `SELECT count(*) FILTER (WHERE e.tenant_id = @tenant_id) AS n
		      FROM employees e WHERE e.id = @id`,
		Closes: "nothing new: the FILTER narrowing must not turn into a pass (T24's other direction)",
	},
}

// Clean are correctly scoped statements written in the forms this package DOES read; every belt
// must pass them. A belt that flags correct SQL is one the next author switches off. Measured on
// the pre-T164 copies: the trailing-comment case was flagged by all six (a paren in the comment
// hid the AND), and the FILTER case by adminauth and review (T24: their readers took the FILTER's
// OR for the WHERE's).
var Clean = []Case{
	{
		Name: "plain",
		SQL:  `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id AND e.id = @id`,
	},
	{
		Name: "literals carrying parens, a doubled quote, OR and AND",
		SQL:  `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id AND e.full_name <> 'it''s (a) OR b AND c' AND e.status <> ')'`,
	},
	{
		Name: "a trailing line comment carrying a paren and an OR",
		SQL: `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id -- or anything ( here
		        AND e.id = @id`,
	},
	{
		Name: "plain quoted identifiers",
		SQL:  `SELECT e."full_name" FROM employees e WHERE e.tenant_id = @tenant_id AND e."status" <> 'x'`,
	},
	{
		Name: "a positional parameter is not a dollar quote",
		SQL:  `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id AND e.id = $1`,
	},
	{
		Name: "a parenthesised OR is a filter",
		SQL:  `SELECT e.id FROM employees e WHERE e.tenant_id = @tenant_id AND (e.id = @id OR e.status = 'x')`,
	},
	{
		Name: "an aggregate FILTER with an OR beside a scoped WHERE (T24)",
		SQL: `SELECT count(*) FILTER (WHERE e.status = 'active' OR e.location_id IS NULL) AS n
		      FROM employees e WHERE e.tenant_id = @tenant_id`,
		Closes: "the FILTER narrowing: without it this is a false alarm",
	},
}
