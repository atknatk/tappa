package handler

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/internal/webhook"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE SECURITY PAGE'S PINS (LP-07). The page describes the red lines (CLAUDE.md §4), so
// its facts are derived from the code that keeps each one: internal/sun, internal/session,
// internal/netx, the policy engine, tap.Decide, the activation flow, the migrations that
// make records immutable and businesses separate, the operator's read path, the webhook
// signer and the deployment record. Like every derivation in this package, NONE OF THEM
// READS THE PAGE'S SENTENCE; TestSecurityFacts_SayNoWhenTheFactIsAbsent takes one piece
// of what each reads away and expects a no.
//
// The figures (every number the page prints) are part of the derivations: a derivation
// compares the number internal/handler hands the page with the number the code holds, so
// a change in the code turns this red rather than leaving the page wrong.
//
// The page's copy joins the shared closure through publicPagesCopy (publicpages_test.go),
// its derivations through pageFactDerivations (marketing_facts_test.go), and two of its
// facts delegate to the landing's Sources (factSourceDelegations).

// securityFactSources is everything the security page's derivations read, so the
// negative control can hand them a copy with one piece taken out. Go sources are held
// with `//` comments stripped: a scan a comment can satisfy pins nothing.
type securityFactSources struct {
	Figures pages.SecurityFigures
	Copy    pages.SecurityText

	SunParams  string // internal/sun/params.go
	SunKeys    string // internal/sun/keys.go
	Token      string // internal/session/token.go
	Activate   string // internal/handler/activate.go
	Checkin    string // internal/domain/checkin/checkin.go
	Decide     string // internal/domain/tap/decide.go
	Guardrails string // internal/policy/guardrails.go
	Prefix     string // internal/netx/prefix.go
	Venue      string // internal/domain/tenant/venue.go
	Config     string // internal/config/config.go
	Report     string // internal/domain/ledger/report.go
	Manual     string // internal/domain/manual/manual.go
	Review     string // internal/domain/review/review.go
	Pool       string // internal/db/pool.go
	Signing    string // internal/webhook/signing.go
	Delivery   string // internal/webhook/delivery/delivery.go
	Dashboard  string // internal/handler/dashboard.go

	RollTempl   string // web/templates/pages/reports.templ
	DocketTempl string // web/templates/components/docket.templ
	Deploy      string // deploy/README.md

	TxQueries string            // db/queries/transactions.sql
	Queries   map[string]string // every db/queries file, by name
	Migs      map[string]string // every migration, by file name

	// NonPolicyGo is every non-test Go file outside internal/policy, by path, comments
	// stripped: where an input to a rule would have to be set.
	NonPolicyGo map[string]string
	Scripts     map[string]string // the shipped scripts (web/static/js)

	Guards   []policy.Guardrail
	Baseline []policy.BaselineDoc
	Notice   []pages.CookieRow
	Policies []string // the public pages' Content-Security-Policy values
}

func loadSecurityFactSources(t *testing.T) securityFactSources {
	t.Helper()
	strip := func(s string) string { return goLineCommentRE.ReplaceAllString(s, "") }
	readDir := func(sub ...string) map[string]string {
		dir := filepath.Join(append([]string{"..", ".."}, sub...)...)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			out[e.Name()] = string(b)
		}
		return out
	}
	migs := readDir("db", "migrations")
	queries := readDir("db", "queries")
	if len(migs) < 60 || len(queries) < 20 {
		t.Fatalf("PREMISE: read %d migration(s) and %d query file(s); there are more", len(migs), len(queries))
	}
	nonPolicy := map[string]string{}
	for path, src := range nonTestGoSource(t, "internal", "cmd") {
		if strings.HasPrefix(path, filepath.Join("internal", "policy")+string(filepath.Separator)) {
			continue
		}
		nonPolicy[path] = strip(src)
	}
	figures := securityFigures()
	return securityFactSources{
		Figures:     figures,
		Copy:        pages.SecurityCopy(figures),
		SunParams:   strip(repoFile(t, "internal", "sun", "params.go")),
		SunKeys:     strip(repoFile(t, "internal", "sun", "keys.go")),
		Token:       strip(repoFile(t, "internal", "session", "token.go")),
		Activate:    strip(repoFile(t, "internal", "handler", "activate.go")),
		Checkin:     strip(repoFile(t, "internal", "domain", "checkin", "checkin.go")),
		Decide:      strip(repoFile(t, "internal", "domain", "tap", "decide.go")),
		Guardrails:  strip(repoFile(t, "internal", "policy", "guardrails.go")),
		Prefix:      strip(repoFile(t, "internal", "netx", "prefix.go")),
		Venue:       strip(repoFile(t, "internal", "domain", "tenant", "venue.go")),
		Config:      strip(repoFile(t, "internal", "config", "config.go")),
		Report:      strip(repoFile(t, "internal", "domain", "ledger", "report.go")),
		Manual:      strip(repoFile(t, "internal", "domain", "manual", "manual.go")),
		Review:      strip(repoFile(t, "internal", "domain", "review", "review.go")),
		Pool:        strip(repoFile(t, "internal", "db", "pool.go")),
		Signing:     strip(repoFile(t, "internal", "webhook", "signing.go")),
		Delivery:    strip(repoFile(t, "internal", "webhook", "delivery", "delivery.go")),
		Dashboard:   strip(repoFile(t, "internal", "handler", "dashboard.go")),
		RollTempl:   repoFile(t, "web", "templates", "pages", "reports.templ"),
		DocketTempl: repoFile(t, "web", "templates", "components", "docket.templ"),
		Deploy:      repoFile(t, "deploy", "README.md"),
		TxQueries:   queries["transactions.sql"],
		Queries:     queries,
		Migs:        migs,
		NonPolicyGo: nonPolicy,
		Scripts:     shippedScripts(t),
		Guards:      policy.Guardrails(policy.DefaultParams()),
		Baseline:    policy.Baseline(),
		Notice:      cookieNotice(),
		Policies:    []string{marketingCSP, landingCSPFor("", "")},
	}
}

// laterThan is every migration after name, by file name.
func laterThan(migs map[string]string, name string) map[string]string {
	out := map[string]string{}
	for k, v := range migs {
		if k > name {
			out[k] = v
		}
	}
	return out
}

// intConst reads `name = <digits>` out of a Go source, or -1.
func intConst(src, name string) int {
	m := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*=\s*(\d+)`).FindStringSubmatch(src)
	if m == nil {
		return -1
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return -1
	}
	return n
}

// laterUpMatches names the first migration after `after` whose Up section matches re,
// or "".
func laterUpMatches(migs map[string]string, after string, re *regexp.Regexp) string {
	names := slices.Sorted(maps.Keys(laterThan(migs, after)))
	for _, n := range names {
		if re.MatchString(gooseUp(migs[n])) {
			return n
		}
	}
	return ""
}

// guardBySid is the guardrail with sid in gs, or nil.
func guardBySid(gs []policy.Guardrail, sid string) *policy.Guardrail {
	for i := range gs {
		if gs[i].Sid == sid {
			return &gs[i]
		}
	}
	return nil
}

// baselineStatements is every statement of the baseline documents, by sid.
func baselineStatements(docs []policy.BaselineDoc) map[string]policy.Statement {
	out := map[string]policy.Statement{}
	for _, d := range docs {
		for _, st := range d.Document.Statements {
			out[st.Sid] = st
		}
	}
	return out
}

// baselineSet is the engine's Set for gs and docs, the baseline as Policies.
func baselineSet(gs []policy.Guardrail, docs []policy.BaselineDoc, tenant ...policy.Policy) policy.Set {
	var base []policy.Policy
	for _, d := range docs {
		base = append(base, policy.Policy{VersionID: uuid.Nil, Layer: policy.LayerBaseline, Document: d.Document})
	}
	return policy.Set{Guardrails: gs, Baseline: base, Tenant: tenant}
}

// tapSession is the session a sample tap carries: a known person, so sys:no-session
// does not answer first.
var tapSession = uuid.MustParse("00000000-0000-4000-8000-0000000000a1")

// tapAt is a tap:record request by a known person at one venue with the given evidence
// keys.
func tapAt(keys map[policy.ContextKey]any) policy.Context {
	return policy.Context{Action: policy.ActionTapRecord, Resources: []string{"location/venue-a"}, Keys: keys,
		SessionTenantID: &tapSession}
}

// --- the derivations -------------------------------------------------------------------

// signatureViolation: the signature's length and the plaque key's size are the code's.
func signatureViolation(s securityFactSources) string {
	if n := intConst(s.SunParams, "cmacHexLen"); n != s.Figures.SignatureHexChars {
		return "internal/sun's cmacHexLen is " + strconv.Itoa(n) + " and the page prints a " +
			strconv.Itoa(s.Figures.SignatureHexChars) + "-character signature"
	}
	if n := intConst(s.SunKeys, "tagKeyLen"); n*8 != s.Figures.PlaqueKeyBits {
		return "internal/sun's tagKeyLen is " + strconv.Itoa(n) + " bytes and the page prints an AES-" +
			strconv.Itoa(s.Figures.PlaqueKeyBits) + " key"
	}
	f := fieldNames(sun.Params{})
	for _, want := range []string{"UID", "Ctr", "CMAC"} {
		if _, ok := f[want]; !ok {
			return "sun.Params no longer carries " + want + ", so the link is not the plaque's id, a counter and a signature"
		}
	}
	return ""
}

// wrappedViolation: a plaque's key is stored only as an AES-256-GCM envelope, and the
// schema holds the stored column to the envelope.
func wrappedViolation(s securityFactSources) string {
	if intConst(s.SunKeys, "kekLen") != 32 {
		return "internal/sun's kekLen is no longer 32, so the envelope is not AES-256"
	}
	wrap := goFuncBody(s.SunKeys, "func Wrap(")
	if !strings.Contains(wrap, "aead(kek)") || !strings.Contains(wrap, ".Seal(") {
		return "internal/sun's Wrap no longer seals the plaque key under the deployment's key"
	}
	if gcm := goFuncBody(s.SunKeys, "func aead("); !strings.Contains(gcm, "aes.NewCipher(") || !strings.Contains(gcm, "cipher.NewGCM(") {
		return "internal/sun's envelope is no longer AES-GCM"
	}
	const envelope = "tags_aes_key_ref_is_kek_envelope"
	if !strings.Contains(gooseUp(s.Migs["00021_truncate_guard_key_shape_audit_index.sql"]), "ADD CONSTRAINT "+envelope) {
		return "migration 00021 no longer holds tags.aes_key_ref to the envelope's shape"
	}
	if m := laterUpMatches(s.Migs, "00021_truncate_guard_key_shape_audit_index.sql",
		regexp.MustCompile(`(?i)DROP\s+CONSTRAINT\s+(?:IF\s+EXISTS\s+)?`+envelope)); m != "" {
		return m + " drops the envelope check on tags.aes_key_ref"
	}
	return ""
}

// activationViolation: activation ends on a real NFC tap at an active plaque of the same
// business, checked by sun.Verify, and writes no attendance record.
func activationViolation(s securityFactSources) string {
	body := goFuncBody(s.Activate, "func (a *Activation) CompleteByTap(")
	if body == "" {
		return "internal/handler/activate.go no longer has Activation.CompleteByTap"
	}
	for _, want := range []string{"!p.HasSUN()", "a.sun.Verify(", "pv.TenantID != ictx.TenantID",
		`pv.TagStatus != "active"`, "!res.SUNValid", "a.finishActivation("} {
		if !strings.Contains(body, want) {
			return "Activation.CompleteByTap no longer carries " + want + ", so activation is not a verified tap " +
				"at an active plaque of the same business"
		}
	}
	finish := goFuncBody(s.Activate, "func (a *Activation) finishActivation(")
	for _, fn := range []string{body, finish} {
		if regexp.MustCompile(`(?i)transaction\b|InsertTransaction|checkin\.`).MatchString(fn) {
			return "the activation tap now reaches the attendance record; the page says it writes none"
		}
	}
	return ""
}

// tokenViolation: the session token is TokenBits of crypto/rand, and the database keeps
// only an HMAC of it.
func tokenViolation(s securityFactSources) string {
	if n := intConst(s.Token, "tokenBytes"); n*8 != s.Figures.TokenBits {
		return "internal/session's tokenBytes is " + strconv.Itoa(n) + " and the page prints a " +
			strconv.Itoa(s.Figures.TokenBits) + "-bit token"
	}
	if !strings.Contains(goFuncBody(s.Token, "func newToken("), "rand.Read(") {
		return "internal/session's newToken no longer draws from crypto/rand"
	}
	if !strings.Contains(s.Token, "hmac.New(sha256.New") {
		return "internal/session no longer keys the stored value with HMAC-SHA256"
	}
	block := sqlTableBlock(s.Migs["00003_create_employees_sessions.sql"], "sessions")
	if !regexp.MustCompile(`(?m)^\s*token_hash\s+text\s+NOT NULL`).MatchString(block) {
		return "migration 00003's sessions table no longer keeps token_hash"
	}
	if regexp.MustCompile(`(?m)^\s*token\s+`).MatchString(block) {
		return "migration 00003's sessions table keeps the token itself"
	}
	return ""
}

// sqlTableBlock is the body of `CREATE TABLE name (…);` in sql, or "".
func sqlTableBlock(sql, name string) string {
	m := regexp.MustCompile(`(?is)CREATE TABLE (?:IF NOT EXISTS )?` + regexp.QuoteMeta(name) + `\s*\((.*?)\n\s*\);`).FindStringSubmatch(sql)
	if m == nil {
		return ""
	}
	return m[1]
}

// deactivatedEventViolation: a switched-off account's tap raises an alert, and the
// check-in writes an audit row for it.
func deactivatedEventViolation(s securityFactSources) string {
	g := guardBySid(s.Guards, "sys:employee-deactivated")
	if g == nil || g.Alert == nil || g.Alert(policy.Context{}) == "" {
		return "sys:employee-deactivated no longer raises a security alert"
	}
	at := strings.Index(s.Checkin, "if dec.Security {")
	if at < 0 {
		return "internal/domain/checkin no longer acts on a security alert"
	}
	branch := s.Checkin[at:min(len(s.Checkin), at+2500)]
	if !regexp.MustCompile(`s\.record\(ctx, audit\.Event\{[\s\S]*?Action:\s+ActionTapSecurityAlert`).MatchString(branch) {
		return "internal/domain/checkin no longer writes an audit row for a security alert"
	}
	return ""
}

// networkNameRE is a network's name as an input: an SSID, by either spelling.
var networkNameRE = regexp.MustCompile(`(?i)ssid|wi-?fi`)

// addressViolation: the address that counts is the request's own source address against
// the tapped venue's ranges, and nothing the engine reads is a network's name.
func addressViolation(s securityFactSources) string {
	if !strings.Contains(s.Decide, "ipMatches(in.SourceIP, in.LocationIPs)") {
		return "tap.Decide no longer matches the request's source address against the venue's ranges"
	}
	for name := range fieldNames(tap.Input{}) {
		if networkNameRE.MatchString(name) {
			return "tap.Input carries " + name + ", a network name, as an input to the decision"
		}
	}
	for _, key := range contextKeyValues(s.Guardrails + "\n" + repoFileNoT("internal", "policy", "document.go")) {
		if networkNameRE.MatchString(key) {
			return "the engine's vocabulary carries " + key + ", a network name"
		}
	}
	if networkNameRE.MatchString(s.Decide) || networkNameRE.MatchString(s.Guardrails) {
		return "tap.Decide or the guardrails read a network name"
	}
	return ""
}

// repoFileNoT reads a repository file without a *testing.T, for the derivations that run
// inside a closure; "" when it cannot be read (and the caller's check then fails).
func repoFileNoT(parts ...string) string {
	b, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		return ""
	}
	return goLineCommentRE.ReplaceAllString(string(b), "")
}

// contextKeyRE is a key of the engine's closed vocabulary.
var contextKeyRE = regexp.MustCompile(`\bCtx\w+\s+ContextKey\s*=\s*"([^"]+)"`)

func contextKeyValues(src string) []string {
	var out []string
	for _, m := range contextKeyRE.FindAllStringSubmatch(src, -1) {
		out = append(out, m[1])
	}
	return out
}

// wideViolation: the limits are internal/netx's, applied by the same function when a
// venue is saved and when a tap is judged.
func wideViolation(s securityFactSources) string {
	if n := intConst(s.Prefix, "maxProofHostBitsV4"); 32-n != s.Figures.IPv4Prefix {
		return "internal/netx's IPv4 limit is " + strconv.Itoa(n) + " host bits, a /" + strconv.Itoa(32-n) +
			", and the page prints a /" + strconv.Itoa(s.Figures.IPv4Prefix)
	}
	if n := intConst(s.Prefix, "maxProofHostBitsV6"); 128-n != s.Figures.IPv6Prefix {
		return "internal/netx's IPv6 limit is " + strconv.Itoa(n) + " host bits, a /" + strconv.Itoa(128-n) +
			", and the page prints a /" + strconv.Itoa(s.Figures.IPv6Prefix)
	}
	if !strings.Contains(goFuncBody(s.Decide, "func ipMatches("), "netx.TooWideForProofOfPlace(prefixes)") {
		return "tap.Decide's ipMatches no longer refuses a list too wide to be proof (the judging side)"
	}
	if !regexp.MustCompile(`netx\.TooWideForProofOfPlace\(c\.StaticIPs\); tooWide \{\s*return Venue\{\}, ErrVenueRangeTooWide`).
		MatchString(s.Venue) {
		return "saving a venue no longer refuses a list too wide to be proof (the saving side)"
	}
	return ""
}

// farViolation: a position read outside the radius is a conflict, which the baseline
// sends to review, and review beats an address match at the same scope.
func farViolation(s securityFactSources) string {
	st, ok := baselineStatements(s.Baseline)["base:gps-conflict-review"]
	if !ok || st.Effect != policy.EffectReview {
		return "the baseline no longer reviews a position read outside the radius (base:gps-conflict-review)"
	}
	if v, ok := st.Condition[policy.OpBool][policy.CtxTapGPSConflict]; !ok || v != true {
		return "base:gps-conflict-review no longer fires on tap:gpsConflict"
	}
	if !strings.Contains(goFuncBody(s.Decide, "func gpsFacts("), "return match, !match, distanceM, true") {
		return "tap.Decide's gpsFacts no longer reports a read position outside the radius as a conflict"
	}
	d := policy.Evaluate(baselineSet(s.Guards, s.Baseline), tapAt(map[policy.ContextKey]any{
		policy.CtxTapChannel: "nfc", policy.CtxTapSunValid: true, policy.CtxTapIPMatch: true,
		policy.CtxTapGPSMatch: false, policy.CtxTapGPSConflict: true,
	}))
	if d.Effect != policy.EffectReview {
		return "a tap with an address match and a position outside the radius ends in " + string(d.Effect) +
			" (" + d.MatchedSid + "), not a review"
	}
	return ""
}

// orderViolation: guardrails first and terminal, then the baseline and the business's
// rules together (the most specific wins, then the strictest).
func orderViolation(s securityFactSources) string {
	set := baselineSet(s.Guards, s.Baseline)
	d := policy.Evaluate(set, tapAt(map[policy.ContextKey]any{
		policy.CtxTagStatus: "retired", policy.CtxTapChannel: "nfc", policy.CtxTapSunValid: true,
		policy.CtxTapIPMatch: true, policy.CtxTapGPSMatch: false,
	}))
	if d.Layer != policy.LayerGuardrail || d.Effect != policy.EffectDeny {
		return "a retired plaque with an address match ends in " + string(d.Effect) + " (" + d.MatchedSid +
			"), so a guardrail no longer runs ahead of the baseline"
	}
	tenant := policy.Policy{VersionID: uuid.New(), Layer: policy.LayerTenant, Document: policy.Document{
		Version: "x", Name: "x", Statements: []policy.Statement{{
			Sid: "t:venue-a", Effect: policy.EffectAllow, Action: []policy.Action{policy.ActionTapRecord},
			Resource: []string{"location/venue-a"}, Condition: policy.Condition{},
		}},
	}}
	none := map[policy.ContextKey]any{policy.CtxTapChannel: "nfc", policy.CtxTapSunValid: true,
		policy.CtxTapIPMatch: false, policy.CtxTapGPSMatch: false}
	if d := policy.Evaluate(baselineSet(s.Guards, s.Baseline, tenant), tapAt(none)); d.MatchedSid != "t:venue-a" {
		return "a business's rule for one venue does not win over the baseline's rule for every venue (" +
			d.MatchedSid + " decided)"
	}
	return ""
}

// undecidedViolation: a tap no rule decides is reviewed, and a review is a flag.
func undecidedViolation(s securityFactSources) string {
	d := policy.Evaluate(policy.Set{Guardrails: s.Guards}, tapAt(map[policy.ContextKey]any{}))
	if d.Effect != policy.EffectReview || d.MatchedSid != "default" {
		return "a tap no rule decides ends in " + string(d.Effect) + " (" + d.MatchedSid + "), not the default review"
	}
	if !regexp.MustCompile(`case policy\.EffectReview:\s*dec\.Verdict = VerdictFlag`).MatchString(s.Decide) {
		return "tap.Decide no longer turns a review into a flag"
	}
	return ""
}

// guardEffectWord is the ladder's word for a guardrail's effect: the verdict a tap ends
// in, or "" for the redirect that ends in none.
var guardEffectWord = map[policy.Effect]string{
	policy.EffectDeny:     "reject",
	policy.EffectIgnore:   "ignored",
	policy.EffectReview:   "flag",
	policy.EffectAllow:    "ok",
	policy.EffectRedirect: "",
}

// guardListViolation: the page names every guardrail exactly once -- the five rungs and
// the others in the note -- and prints the list's length.
func guardListViolation(s securityFactSources) string {
	if len(s.Guards) != s.Figures.Guardrails {
		return "the engine runs " + strconv.Itoa(len(s.Guards)) + " guardrails and the page says " +
			strconv.Itoa(s.Figures.Guardrails)
	}
	named := map[string]int{}
	for _, r := range s.Copy.Guardrails {
		for _, sid := range r.Sids {
			named[sid]++
		}
	}
	for _, o := range s.Copy.OtherGuards {
		named[o.Sid]++
	}
	for _, o := range s.Copy.UnnamedGuards {
		named[o.Sid]++
	}
	for _, g := range s.Guards {
		if named[g.Sid] != 1 {
			return "the page names the guardrail " + g.Sid + " " + strconv.Itoa(named[g.Sid]) + " time(s); every one is " +
				"named once"
		}
		delete(named, g.Sid)
	}
	for sid := range named {
		return "the page names " + sid + ", which is not a guardrail"
	}
	return ""
}

// baselineChangeViolation: the business changes its rules from the panel, and the
// baseline is a layer below the guardrails.
func baselineChangeViolation(s securityFactSources) string {
	if !regexp.MustCompile(`r\.Post\(\s*policyApplyHref\s*,`).MatchString(s.Dashboard) {
		return "the panel no longer applies a policy change (POST policyApplyHref)"
	}
	for sid := range baselineStatements(s.Baseline) {
		if strings.HasPrefix(sid, "sys:") {
			return "the baseline carries " + sid + ", a guardrail's sid"
		}
	}
	return ""
}

// trustViolation: the trust score is TrustBase plus the two weights, computed from the
// evidence, carried on the record and set as no rule's input.
func trustViolation(s securityFactSources) string {
	if n := intConst(s.Decide, "TrustBase"); n != s.Figures.TrustBase {
		return "tap.TrustBase is " + strconv.Itoa(n) + " and the page prints " + strconv.Itoa(s.Figures.TrustBase)
	}
	body := goFuncBody(s.Decide, "func trustScore(")
	ip := regexp.MustCompile(`if ipMatch \{\s*score \+= (\d+)`).FindStringSubmatch(body)
	gps := regexp.MustCompile(`if gpsMatch \{\s*score \+= (\d+)`).FindStringSubmatch(body)
	if ip == nil || gps == nil || ip[1] != strconv.Itoa(s.Figures.TrustIP) || gps[1] != strconv.Itoa(s.Figures.TrustGPS) {
		return "tap.trustScore no longer adds +" + strconv.Itoa(s.Figures.TrustIP) + " for an address match and +" +
			strconv.Itoa(s.Figures.TrustGPS) + " for a position match"
	}
	if !strings.Contains(s.Decide, "Trust: trustScore(ipMatch, gpsMatch)") {
		return "tap.Decide no longer computes the score from the evidence alone"
	}
	for path, src := range s.NonPolicyGo {
		if strings.Contains(src, "CtxTapTrust") {
			return path + " sets the trust score as an input to the rules, so it can decide something"
		}
	}
	if !strings.Contains(s.DocketTempl, "docketTrust(d)") {
		return "the panel's docket no longer shows the score"
	}
	return ""
}

// debounceViolation: the same person inside the window is ignored, the window is timed
// on the database's clock too, and its default is the page's.
func debounceViolation(s securityFactSources) string {
	g := guardBySid(s.Guards, "sys:person-debounce")
	if g == nil || g.Effect != policy.EffectIgnore {
		return "sys:person-debounce no longer ignores a repeat"
	}
	if !strings.Contains(s.Guardrails, "c.SecondsSincePersonLastTap") {
		return "sys:person-debounce no longer measures the PERSON's last tap"
	}
	q := queryBlock(s.TxQueries, "SecondsSinceLastRecordedTap")
	if !strings.Contains(q, "clock_timestamp() - created_at") {
		return "SecondsSinceLastRecordedTap no longer measures on the database's clock"
	}
	if !strings.Contains(goFuncBody(s.Decide, "func debounceGap("), "recorded < gap") {
		return "tap.Decide no longer takes the database's measure when it is the shorter"
	}
	m := regexp.MustCompile(`floatEnvRange\(\s*"TAPPA_DEBOUNCE_SECONDS"\s*,\s*(\d+)`).FindStringSubmatch(s.Config)
	if m == nil || m[1] != strconv.Itoa(s.Figures.DebounceSeconds) {
		return "internal/config's debounce default is not the page's " + strconv.Itoa(s.Figures.DebounceSeconds) + " seconds"
	}
	return ""
}

// heldViolation: a flagged record's hours wait outside the payroll figure, and The Roll
// prints them as waiting or refused.
func heldViolation(s securityFactSources) string {
	for _, re := range []string{
		`case HoursAwaiting:\s*ph\.Awaiting \+= worked`,
		`case HoursRefused:\s*ph\.Refused \+= worked`,
		`default:\s*ph\.Worked \+= worked`,
	} {
		if !regexp.MustCompile(re).MatchString(s.Report) {
			return "the ledger no longer keeps undecided and refused hours outside the payroll figure"
		}
	}
	if !strings.Contains(s.RollTempl, "{ p.Awaiting } waiting") || !strings.Contains(s.RollTempl, "{ p.Refused } refused") {
		return "The Roll no longer prints held hours as waiting and refused"
	}
	return ""
}

// reviewFinalViolation: a flagged record is decided once and the decision cannot be
// changed afterwards -- one row per record, never updated or deleted. It reads the
// constraint and the guards, not any sentence about who may decide: the page no longer
// says anything about that (see pages.FactReviewIsOnceAndFinal).
func reviewFinalViolation(s securityFactSources) string {
	const mig = "00005_create_transactions_audit_reviews.sql"
	up := gooseUp(s.Migs[mig])
	if !strings.Contains(sqlTableBlock(up, "transaction_reviews"), "CONSTRAINT transaction_reviews_txn_key UNIQUE (transaction_id)") {
		return "a flagged record can be decided more than once: transaction_reviews lost UNIQUE (transaction_id)"
	}
	if !strings.Contains(up, "REVOKE UPDATE, DELETE ON transaction_reviews FROM tappa_app;") {
		return "the application can change or delete a decision"
	}
	if !regexp.MustCompile(`CREATE TRIGGER transaction_reviews_no_mutation\s+BEFORE UPDATE OR DELETE ON transaction_reviews\s+FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation\(\)`).MatchString(up) {
		return "a decision can be changed: transaction_reviews lost its mutation guard"
	}
	for _, re := range []string{
		`(?i)DROP\s+TRIGGER\s+(?:IF\s+EXISTS\s+)?transaction_reviews_no_mutation\b`,
		`(?i)ALTER\s+TABLE\s+(?:public\.)?transaction_reviews\s+DISABLE\s+TRIGGER`,
		`(?i)DROP\s+CONSTRAINT\s+(?:IF\s+EXISTS\s+)?transaction_reviews_txn_key\b`,
		`(?is)GRANT\s[^;]*\b(?:UPDATE|DELETE|ALL)\b[^;]*\bON\s+(?:TABLE\s+)?(?:public\.)?transaction_reviews\b[^;]*\bTO\s+tappa_app\b`,
	} {
		if m := laterUpMatches(s.Migs, mig, regexp.MustCompile(re)); m != "" {
			return m + " lets a decision be made twice or changed"
		}
	}
	return ""
}

// immutableViolation: transactions cannot be changed or deleted by the application or
// by the owner.
func immutableViolation(s securityFactSources) string {
	const mig = "00005_create_transactions_audit_reviews.sql"
	up := gooseUp(s.Migs[mig])
	if !strings.Contains(up, "REVOKE UPDATE, DELETE ON transactions FROM tappa_app;") {
		return "migration 00005 no longer revokes UPDATE and DELETE on transactions from the application"
	}
	if !regexp.MustCompile(`CREATE TRIGGER transactions_no_mutation\s+BEFORE UPDATE OR DELETE ON transactions\s+FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation\(\)`).MatchString(up) {
		return "migration 00005 no longer refuses a change to transactions by trigger (the owner's guard)"
	}
	for _, re := range []string{
		`(?i)DROP\s+TRIGGER\s+(?:IF\s+EXISTS\s+)?transactions_no_mutation\b`,
		`(?i)ALTER\s+TABLE\s+(?:public\.)?transactions\s+DISABLE\s+TRIGGER`,
		`(?i)(?:CREATE\s+OR\s+REPLACE|DROP)\s+FUNCTION\s+(?:IF\s+EXISTS\s+)?(?:public\.)?tappa_forbid_mutation\b`,
		`(?is)GRANT\s[^;]*\b(?:UPDATE|DELETE|ALL)\b[^;]*\bON\s+(?:TABLE\s+)?(?:public\.)?transactions\b[^;]*\bTO\s+tappa_app\b`,
	} {
		if m := laterUpMatches(s.Migs, mig, regexp.MustCompile(re)); m != "" {
			return m + " gives back a way to change a stored record"
		}
	}
	return ""
}

// manualAuditViolation: a manual entry writes its audit row in the same transaction.
func manualAuditViolation(s securityFactSources) string {
	if !strings.Contains(s.Manual, `const Action = "record.entered"`) {
		return "internal/domain/manual no longer names its audit action"
	}
	if !strings.Contains(s.Manual, "r.trail.RecordTx(ctx, tx, audit.Event{") {
		return "a manual entry no longer writes its audit row in the record's transaction"
	}
	return ""
}

// cookiesViolation: every cookie the notice lists is first-party and HttpOnly with a
// stated job, no shipped script writes one, and the public pages load nothing from
// another site. THE LIMIT, STATED: that the notice lists every cookie the code sets is
// TestCookieNotice_ListsExactlyTheCookiesTheProductSets's job, not this one's.
func cookiesViolation(s securityFactSources) string {
	if len(s.Notice) < 3 {
		return "the cookie notice lists " + strconv.Itoa(len(s.Notice)) + " cookie(s); this is not reading it"
	}
	for _, c := range s.Notice {
		if !strings.Contains(c.Flags, "HttpOnly") || strings.TrimSpace(c.Purpose) == "" {
			return "the cookie " + c.Name + " is readable by a script or states no job"
		}
	}
	for path, src := range s.Scripts {
		if strings.Contains(jsLineCommentRE.ReplaceAllString(src, ""), "document.cookie") {
			return "web/static/" + path + " writes a cookie from a script"
		}
	}
	for _, p := range s.Policies {
		if !strings.Contains(p, "default-src 'none'") || strings.Contains(p, "http") {
			return "a public page's policy loads from another site: " + p
		}
	}
	return ""
}

// isolationTables are the business records the page speaks of, by their migration.
var isolationTables = map[string]string{
	"locations":           "00002_create_locations_departments.sql",
	"departments":         "00002_create_locations_departments.sql",
	"employees":           "00003_create_employees_sessions.sql",
	"sessions":            "00003_create_employees_sessions.sql",
	"tags":                "00004_create_tags.sql",
	"transactions":        "00005_create_transactions_audit_reviews.sql",
	"audit_log":           "00005_create_transactions_audit_reviews.sql",
	"transaction_reviews": "00005_create_transactions_audit_reviews.sql",
}

// untenantedQueries are the two queries that name no business, each for its reason:
// CreateTenant makes the business, and the legal documents are Taptime's own (a
// no-tenant-scope table, redline R5's list).
var untenantedQueries = map[string]bool{"CreateTenant": true, "ListPublishedLegalDocuments": true}

var queryNameRE = regexp.MustCompile(`(?m)^-- name: (\w+)`)

// isolationViolation: each business record carries its tenant_id under forced row-level
// security, production refuses a role that could bypass it, and every query names the
// business again.
func isolationViolation(s securityFactSources) string {
	tables := slices.Sorted(maps.Keys(isolationTables))
	for _, tbl := range tables {
		mig := s.Migs[isolationTables[tbl]]
		if !regexp.MustCompile(`(?m)^\s*tenant_id\s+uuid\s+NOT NULL`).MatchString(sqlTableBlock(mig, tbl)) {
			return tbl + " no longer carries tenant_id NOT NULL"
		}
		for _, want := range []string{"ALTER TABLE " + tbl + " ENABLE ROW LEVEL SECURITY;", "ALTER TABLE " + tbl + " FORCE ROW LEVEL SECURITY;"} {
			if !strings.Contains(mig, want) {
				return tbl + "'s migration no longer carries " + want
			}
		}
		if !regexp.MustCompile(`CREATE POLICY \w+ ON ` + tbl + `\s+USING\s+\(tenant_id = NULLIF\(current_setting\('app\.tenant_id', true\), ''\)::uuid\)`).MatchString(mig) {
			return tbl + " no longer filters by the connection's business"
		}
		re := regexp.MustCompile(`(?i)ALTER\s+TABLE\s+(?:public\.)?` + tbl + `\s+(?:NO\s+FORCE|DISABLE)\s+ROW\s+LEVEL\s+SECURITY|DROP\s+POLICY\s+(?:IF\s+EXISTS\s+)?` + tbl + `_tenant_isolation\b`)
		if m := laterUpMatches(s.Migs, "00005_create_transactions_audit_reviews.sql", re); m != "" {
			return m + " loosens " + tbl + "'s row-level security"
		}
	}
	if !strings.Contains(goFuncBody(s.Pool, "func roleRefusal("), "if !f.Privileged() || !isProd {") {
		return "internal/db no longer refuses a privileged role in production"
	}
	for _, f := range []db.RoleFacts{{Super: true}, {BypassRLS: true}, {OwnsScopedTable: true}} {
		if !f.Privileged() {
			return "internal/db no longer counts a superuser, a BYPASSRLS role and a table owner as privileged"
		}
	}
	for _, file := range slices.Sorted(maps.Keys(s.Queries)) {
		src := s.Queries[file]
		names := queryNameRE.FindAllStringSubmatchIndex(src, -1)
		for i, m := range names {
			end := len(src)
			if i+1 < len(names) {
				end = names[i+1][0]
			}
			name := src[m[2]:m[3]]
			var body []string
			for _, line := range strings.Split(src[m[1]:end], "\n") {
				if !strings.HasPrefix(strings.TrimSpace(line), "--") {
					body = append(body, line)
				}
			}
			if !strings.Contains(strings.Join(body, "\n"), "tenant_id") && !untenantedQueries[name] {
				return "db/queries/" + file + "'s " + name + " names no business"
			}
		}
	}
	return ""
}

// operatorViolation: Taptime's staff sign in with their own cookie, and a read into a
// business starts with an append-only audit row before the read's ticket.
func operatorViolation(s securityFactSources) string {
	if operatorauth.SessionCookieName == adminauth.CookieName || operatorauth.SessionCookieName == session.CookieName {
		return "the operator's sign-in shares a cookie with a business's"
	}
	re := regexp.MustCompile(`CREATE (?:OR REPLACE )?FUNCTION public\.op_begin_read\(p_session`)
	latest := ""
	for _, name := range slices.Sorted(maps.Keys(s.Migs)) {
		if re.MatchString(gooseUp(s.Migs[name])) {
			latest = name
		}
	}
	if latest == "" {
		return "no migration defines op_begin_read"
	}
	up := gooseUp(s.Migs[latest])
	body := up[re.FindStringIndex(up)[0]:]
	if end := strings.Index(body, "$$;"); end >= 0 {
		body = body[:end]
	}
	audit := strings.Index(body, "INSERT INTO public.operator_audit_log")
	ticket := strings.Index(body, "INSERT INTO public.operator_read_tickets")
	if audit < 0 || ticket < 0 || audit > ticket {
		return latest + "'s op_begin_read no longer writes the audit row before it issues the read's ticket"
	}
	mig26 := gooseUp(s.Migs["00026_create_platform_operator.sql"])
	if !strings.Contains(mig26, "REVOKE ALL ON operator_audit_log FROM tappa_app;") ||
		!regexp.MustCompile(`BEFORE UPDATE OR DELETE ON operator_audit_log`).MatchString(mig26) {
		return "operator_audit_log is no longer append-only and out of the application's reach"
	}
	return ""
}

// holdingViolation: outside a holding, a tap on another business's plaque redirects and
// writes nothing; inside one it is not that guardrail's, and the record names the site.
// Holdings are written by the operator alone.
func holdingViolation(s securityFactSources) string {
	g := guardBySid(s.Guards, "sys:tenant-mismatch")
	if g == nil || g.Effect != policy.EffectRedirect {
		return "sys:tenant-mismatch no longer redirects a tap on another business's plaque"
	}
	a, b := uuid.New(), uuid.New()
	if !g.Match(policy.Context{TagTenantID: &a, SessionTenantID: &b}) {
		return "sys:tenant-mismatch no longer fires on another business's plaque"
	}
	if g.Match(policy.Context{TagTenantID: &a, SessionTenantID: &b, SameHolding: true}) {
		return "sys:tenant-mismatch fires inside a holding too, so there is no crossing at all"
	}
	mig41 := gooseUp(s.Migs["00041_create_holdings.sql"])
	if !strings.Contains(mig41, "REVOKE ALL ON holdings FROM tappa_app;") || !strings.Contains(mig41, "REVOKE ALL ON holding_members FROM tappa_app;") {
		return "a business can write its own holding"
	}
	if !strings.Contains(gooseUp(s.Migs["00043_record_a_tap_on_a_peers_plaque.sql"]), "ADD COLUMN site_tenant_id") {
		return "a holding tap no longer names the site on the employer's record"
	}
	// What the venue sees: its projection carries the name, the time, the direction and
	// the outcome, and no column of location evidence.
	const mig46 = "00046_project_a_holding_tap_to_its_site.sql"
	site := sqlTableBlock(gooseUp(s.Migs[mig46]), "site_punches")
	for _, col := range []string{"employee_name", "occurred_at", "type ", "outcome "} {
		if !regexp.MustCompile(`(?m)^\s*` + col).MatchString(site) {
			return "the venue's projection (site_punches) no longer carries " + strings.TrimSpace(col)
		}
	}
	if m := regexp.MustCompile(`(?im)^\s*(\w*(?:ip|gps|lat|lng|distance|trust|note|reason)\w*)\s+(?:uuid|text|inet|cidr|numeric|double|real|integer|smallint|boolean|jsonb|point)`).FindStringSubmatch(site); m != nil {
		return "the venue's projection (site_punches) carries " + m[1] + ", location evidence"
	}
	if m := laterUpMatches(s.Migs, mig46, regexp.MustCompile(`(?i)ALTER\s+TABLE\s+(?:public\.)?site_punches\s+ADD`)); m != "" {
		return m + " adds to what the venue sees; re-read it"
	}
	return ""
}

// webhookViolation: a delivery carries the signature header, "sha256=" and an
// HMAC-SHA256 of its body.
func webhookViolation(s securityFactSources) string {
	if webhook.SignatureHeader != s.Figures.WebhookHeader {
		return "the webhook header is " + webhook.SignatureHeader + " and the page names " + s.Figures.WebhookHeader
	}
	if !strings.Contains(s.Signing, `SignaturePrefix = "sha256="`) ||
		!strings.Contains(goFuncBody(s.Signing, "func (k SigningKey) Sign("), "hmac.New(sha256.New,") {
		return "internal/webhook no longer signs with HMAC-SHA256"
	}
	if !strings.Contains(s.Delivery, "webhook.SignatureHeader: {key.Sign(body)}") {
		return "a webhook delivery no longer carries its signature"
	}
	return ""
}

// hostedRE is the deployment record's two statements: the one node is in fsn1, and fsn1
// is Falkenstein, Germany ("Almanya").
var (
	hostedNodeRE   = regexp.MustCompile("tek node\\s*\\(`k8s-1\\.fsn1\\.private`, Hetzner fsn1")
	hostedRegionRE = regexp.MustCompile(`(?s)fsn1 = Hetzner\s+Falkenstein, Almanya`)
)

// hostedViolation: production is one node, and the deployment record places it in
// Germany. The page publishes the country and the union only (K2).
func hostedViolation(s securityFactSources) string {
	if !hostedNodeRE.MatchString(s.Deploy) {
		return "deploy/README.md no longer records the one production node in fsn1"
	}
	if !hostedRegionRE.MatchString(s.Deploy) {
		return "deploy/README.md no longer records fsn1 as a German data centre"
	}
	return ""
}

// securityFactDerivations is the security page's derivation table (pageFactDerivations).
func securityFactDerivations(t *testing.T) map[pages.Fact]func() string {
	t.Helper()
	s := loadSecurityFactSources(t)
	on := func(f func(securityFactSources) string) func() string { return func() string { return f(s) } }
	return map[pages.Fact]func() string{
		pages.FactSignatureLengthAndKey:               on(signatureViolation),
		pages.FactPlaqueKeysAreWrapped:                on(wrappedViolation),
		pages.FactActivationEndsOnARealTap:            on(activationViolation),
		pages.FactSessionTokenIsStoredHashed:          on(tokenViolation),
		pages.FactDeactivatedTapIsASecurityEvent:      on(deactivatedEventViolation),
		pages.FactAddressIsTheRequestsOwn:             on(addressViolation),
		pages.FactWideAddressListIsNoEvidence:         on(wideViolation),
		pages.FactFarPositionIsReviewed:               on(farViolation),
		pages.FactRulesRunInOrder:                     on(orderViolation),
		pages.FactUndecidedTapIsFlagged:               on(undecidedViolation),
		pages.FactGuardrailsAreTheList:                on(guardListViolation),
		pages.FactBaselineCanBeChanged:                on(baselineChangeViolation),
		pages.FactTrustScoreDecidesNothing:            on(trustViolation),
		pages.FactDebounceIsPerPersonOnTheServerClock: on(debounceViolation),
		pages.FactFlaggedHoursWaitOutsideTheTotal:     on(heldViolation),
		pages.FactReviewIsOnceAndFinal:                on(reviewFinalViolation),
		pages.FactRecordsAreImmutable:                 on(immutableViolation),
		pages.FactManualEntryIsAudited:                on(manualAuditViolation),
		pages.FactNoAdOrAnalyticsCookies:              on(cookiesViolation),
		pages.FactBusinessesAreIsolatedByRLS:          on(isolationViolation),
		pages.FactOperatorReadsAreAudited:             on(operatorViolation),
		pages.FactHoldingIsTheOneCrossing:             on(holdingViolation),
		pages.FactWebhooksAreSigned:                   on(webhookViolation),
		pages.FactHostedInTheEU:                       on(hostedViolation),
	}
}

// TestSecurityFacts_SayNoWhenTheFactIsAbsent is the negative control for every derivation
// above: each holds over the real sources, and each says no once one piece of what it
// reads is taken away or changed -- the code's number, the code's guard, or the page's
// figure.
func TestSecurityFacts_SayNoWhenTheFactIsAbsent(t *testing.T) {
	t.Parallel()
	real := loadSecurityFactSources(t)
	within := func(src, whole, old, new string) string {
		return strings.Replace(src, whole, strings.ReplaceAll(whole, old, new), 1)
	}
	migs := func(s *securityFactSources, name, src string) {
		m := maps.Clone(s.Migs)
		m[name] = src
		s.Migs = m
	}
	later := func(sql string) func(*securityFactSources) {
		return func(s *securityFactSources) {
			migs(s, "00999_x.sql", "-- +goose Up\n"+sql+"\n-- +goose Down\nSELECT 1;\n")
		}
	}
	guards := func(edit func([]policy.Guardrail) []policy.Guardrail) func(*securityFactSources) {
		return func(s *securityFactSources) { s.Guards = edit(slices.Clone(s.Guards)) }
	}
	baseline := func(sid string, edit func(*policy.Statement)) func(*securityFactSources) {
		return func(s *securityFactSources) {
			out := make([]policy.BaselineDoc, len(s.Baseline))
			for i, d := range s.Baseline {
				d.Document.Statements = slices.Clone(d.Document.Statements)
				for j := range d.Document.Statements {
					if d.Document.Statements[j].Sid == sid {
						edit(&d.Document.Statements[j])
					}
				}
				out[i] = d
			}
			s.Baseline = out
		}
	}
	for _, tc := range []struct {
		name   string
		derive func(securityFactSources) string
		mutate func(*securityFactSources)
	}{
		{"a longer signature", signatureViolation, func(s *securityFactSources) {
			s.SunParams = strings.Replace(s.SunParams, "cmacHexLen = 16", "cmacHexLen = 32", 1)
		}},
		{"the page prints another signature length", signatureViolation, func(s *securityFactSources) { s.Figures.SignatureHexChars = 8 }},
		{"a bigger plaque key", signatureViolation, func(s *securityFactSources) {
			s.SunKeys = strings.Replace(s.SunKeys, "tagKeyLen = 16", "tagKeyLen = 32", 1)
		}},
		{"a weaker envelope", wrappedViolation, func(s *securityFactSources) {
			s.SunKeys = strings.Replace(s.SunKeys, "kekLen = 32", "kekLen = 16", 1)
		}},
		{"the envelope is not GCM", wrappedViolation, func(s *securityFactSources) {
			s.SunKeys = within(s.SunKeys, goFuncBody(s.SunKeys, "func aead("), "cipher.NewGCM(", "cipher.NewCBCEncrypter(")
		}},
		{"a later migration drops the envelope check", wrappedViolation, later("ALTER TABLE tags DROP CONSTRAINT tags_aes_key_ref_is_kek_envelope;")},
		{"activation without a signature check", activationViolation, func(s *securityFactSources) {
			s.Activate = within(s.Activate, goFuncBody(s.Activate, "func (a *Activation) CompleteByTap("), "a.sun.Verify(", "a.sun.Peek(")
		}},
		{"activation at another business's plaque", activationViolation, func(s *securityFactSources) {
			s.Activate = within(s.Activate, goFuncBody(s.Activate, "func (a *Activation) CompleteByTap("), "pv.TenantID != ictx.TenantID", "false")
		}},
		{"activation writes a record", activationViolation, func(s *securityFactSources) {
			s.Activate = within(s.Activate, goFuncBody(s.Activate, "func (a *Activation) finishActivation("), "{", "{\n\ta.checkin.Record(r.Context(), nil)")
		}},
		{"a shorter session value", tokenViolation, func(s *securityFactSources) {
			s.Token = strings.Replace(s.Token, "tokenBytes = 32", "tokenBytes = 16", 1)
		}},
		{"the stored value is not keyed", tokenViolation, func(s *securityFactSources) {
			s.Token = strings.ReplaceAll(s.Token, "hmac.New(sha256.New", "sha256.New(")
		}},
		{"the database keeps the value itself", tokenViolation, func(s *securityFactSources) {
			const m = "00003_create_employees_sessions.sql"
			migs(s, m, within(s.Migs[m], sqlTableBlock(s.Migs[m], "sessions"), "token_hash   text NOT NULL", "token   text NOT NULL"))
		}},
		{"a switched-off account raises nothing", deactivatedEventViolation, guards(func(g []policy.Guardrail) []policy.Guardrail {
			for i := range g {
				if g[i].Sid == "sys:employee-deactivated" {
					g[i].Alert = nil
				}
			}
			return g
		})},
		{"the alert leaves no audit row", deactivatedEventViolation, func(s *securityFactSources) {
			s.Checkin = strings.Replace(s.Checkin, "Action:   ActionTapSecurityAlert", "Action:   ActionTapRecorded", 1)
		}},
		{"the address is the phone's word", addressViolation, func(s *securityFactSources) {
			s.Decide = strings.Replace(s.Decide, "ipMatches(in.SourceIP, in.LocationIPs)", "ipMatches(in.DeclaredIP, in.LocationIPs)", 1)
		}},
		{"the network name becomes evidence", addressViolation, func(s *securityFactSources) {
			s.Decide = strings.Replace(s.Decide, "func ipMatches(", "func ssidMatches() bool { return true }\n\nfunc ipMatches(", 1)
		}},
		{"a wider IPv4 limit", wideViolation, func(s *securityFactSources) {
			s.Prefix = strings.Replace(s.Prefix, "maxProofHostBitsV4 = 25", "maxProofHostBitsV4 = 26", 1)
		}},
		{"a wider IPv6 limit", wideViolation, func(s *securityFactSources) {
			s.Prefix = strings.Replace(s.Prefix, "maxProofHostBitsV6 = 97", "maxProofHostBitsV6 = 98", 1)
		}},
		{"the judging side forgets the limit", wideViolation, func(s *securityFactSources) {
			s.Decide = within(s.Decide, goFuncBody(s.Decide, "func ipMatches("), "netx.TooWideForProofOfPlace(prefixes)", "netx.Nothing(prefixes)")
		}},
		{"the saving side forgets the limit", wideViolation, func(s *securityFactSources) {
			s.Venue = strings.Replace(s.Venue, "return Venue{}, ErrVenueRangeTooWide", "_ = 0", 1)
		}},
		{"a far position is allowed", farViolation, baseline("base:gps-conflict-review", func(st *policy.Statement) { st.Effect = policy.EffectAllow })},
		{"a far position is no conflict", farViolation, func(s *securityFactSources) {
			s.Decide = strings.Replace(s.Decide, "return match, !match, distanceM, true", "return match, false, distanceM, true", 1)
		}},
		{"no guardrail runs first", orderViolation, guards(func(g []policy.Guardrail) []policy.Guardrail {
			return slices.DeleteFunc(g, func(x policy.Guardrail) bool { return x.Sid == "sys:tag-not-active" })
		})},
		{"an undecided tap is approved", undecidedViolation, func(s *securityFactSources) {
			s.Guards = append(slices.Clone(s.Guards), policy.Guardrail{Sid: "sys:x", Effect: policy.EffectAllow,
				Match: func(policy.Context) bool { return true }})
		}},
		{"a review is not a flag", undecidedViolation, func(s *securityFactSources) {
			s.Decide = strings.Replace(s.Decide, "dec.Verdict = VerdictFlag", "dec.Verdict = VerdictOK", 1)
		}},
		{"a twelfth guardrail", guardListViolation, guards(func(g []policy.Guardrail) []policy.Guardrail {
			return append(g, policy.Guardrail{Sid: "sys:twelfth", Effect: policy.EffectDeny})
		})},
		{"the page counts another number", guardListViolation, func(s *securityFactSources) { s.Figures.Guardrails = 12 }},
		{"a named guardrail is gone", guardListViolation, func(s *securityFactSources) {
			s.Copy.OtherGuards = s.Copy.OtherGuards[1:]
		}},
		{"the panel cannot apply a change", baselineChangeViolation, func(s *securityFactSources) {
			s.Dashboard = strings.ReplaceAll(s.Dashboard, "r.Post(policyApplyHref,", "r.Get(policyApplyHref,")
		}},
		{"another trust weight", trustViolation, func(s *securityFactSources) {
			s.Decide = within(s.Decide, goFuncBody(s.Decide, "func trustScore("), "score += 50", "score += 60")
		}},
		{"the score feeds a rule", trustViolation, func(s *securityFactSources) {
			s.NonPolicyGo = maps.Clone(s.NonPolicyGo)
			s.NonPolicyGo["internal/domain/tap/x.go"] = "keys[policy.CtxTapTrust] = 70"
		}},
		{"the docket hides the score", trustViolation, func(s *securityFactSources) {
			s.DocketTempl = strings.ReplaceAll(s.DocketTempl, "docketTrust(d)", "")
		}},
		{"debounce is per plaque", debounceViolation, func(s *securityFactSources) {
			s.Guardrails = strings.ReplaceAll(s.Guardrails, "c.SecondsSincePersonLastTap", "c.SecondsSinceTagLastTap")
		}},
		{"debounce on the phone's clock", debounceViolation, func(s *securityFactSources) {
			s.TxQueries = within(s.TxQueries, queryBlock(s.TxQueries, "SecondsSinceLastRecordedTap"), "clock_timestamp() - created_at", "@now - occurred_at")
		}},
		{"another default window", debounceViolation, func(s *securityFactSources) {
			s.Config = strings.Replace(s.Config, `"TAPPA_DEBOUNCE_SECONDS", 60`, `"TAPPA_DEBOUNCE_SECONDS", 90`, 1)
		}},
		{"undecided hours are paid", heldViolation, func(s *securityFactSources) {
			s.Report = strings.Replace(s.Report, "ph.Awaiting += worked", "ph.Worked += worked", 1)
		}},
		{"The Roll hides waiting hours", heldViolation, func(s *securityFactSources) {
			s.RollTempl = strings.ReplaceAll(s.RollTempl, "{ p.Awaiting } waiting", "")
		}},
		{"a record decided twice", reviewFinalViolation, func(s *securityFactSources) {
			const m = "00005_create_transactions_audit_reviews.sql"
			migs(s, m, strings.Replace(s.Migs[m], "CONSTRAINT transaction_reviews_txn_key UNIQUE (transaction_id)", "CONSTRAINT x CHECK (true)", 1))
		}},
		{"a decision can be rewritten", reviewFinalViolation, later("GRANT UPDATE ON transaction_reviews TO tappa_app;")},
		{"the decision's guard is dropped", reviewFinalViolation, later("DROP TRIGGER transaction_reviews_no_mutation ON transaction_reviews;")},
		{"the decision's guard is switched off", reviewFinalViolation, later("ALTER TABLE transaction_reviews DISABLE TRIGGER ALL;")},
		{"the application may update records", immutableViolation, later("GRANT UPDATE ON transactions TO tappa_app;")},
		{"the owner's guard is dropped", immutableViolation, later("DROP TRIGGER IF EXISTS transactions_no_mutation ON transactions;")},
		{"the owner's guard is switched off", immutableViolation, later("ALTER TABLE transactions DISABLE TRIGGER transactions_no_mutation;")},
		{"a manual entry leaves no trail", manualAuditViolation, func(s *securityFactSources) {
			s.Manual = strings.Replace(s.Manual, "r.trail.RecordTx(ctx, tx, audit.Event{", "nothing(audit.Event{", 1)
		}},
		{"a cookie a script can read", cookiesViolation, func(s *securityFactSources) {
			s.Notice = slices.Clone(s.Notice)
			s.Notice[0].Flags = "SameSite=Lax"
		}},
		{"a script sets a cookie", cookiesViolation, func(s *securityFactSources) {
			s.Scripts = maps.Clone(s.Scripts)
			s.Scripts["js/x.js"] = "document.cookie = 'a=b';"
		}},
		{"a page loads another site", cookiesViolation, func(s *securityFactSources) {
			s.Policies = []string{"default-src 'none'; script-src https://analytics.example"}
		}},
		{"a table without forced security", isolationViolation, func(s *securityFactSources) {
			const m = "00005_create_transactions_audit_reviews.sql"
			migs(s, m, strings.Replace(s.Migs[m], "ALTER TABLE transactions FORCE ROW LEVEL SECURITY;", "", 1))
		}},
		{"a later migration lifts the security", isolationViolation, later("ALTER TABLE employees NO FORCE ROW LEVEL SECURITY;")},
		{"production starts under any role", isolationViolation, func(s *securityFactSources) {
			s.Pool = within(s.Pool, goFuncBody(s.Pool, "func roleRefusal("), "if !f.Privileged() || !isProd {", "if true {")
		}},
		{"a query names no business", isolationViolation, func(s *securityFactSources) {
			s.Queries = maps.Clone(s.Queries)
			s.Queries["x.sql"] = "-- name: EveryTap :many\nSELECT id FROM transactions;\n"
		}},
		{"the read's ticket comes before its audit row", operatorViolation, later(
			"CREATE OR REPLACE FUNCTION public.op_begin_read(p_session text, p_kind text, p_params jsonb) RETURNS text AS $$\nBEGIN\n" +
				"INSERT INTO public.operator_read_tickets (x) VALUES (1);\nINSERT INTO public.operator_audit_log (kind) VALUES ('x');\nEND\n$$;")},
		{"the audit log can be rewritten", operatorViolation, func(s *securityFactSources) {
			const m = "00026_create_platform_operator.sql"
			migs(s, m, strings.ReplaceAll(s.Migs[m], "BEFORE UPDATE OR DELETE ON operator_audit_log", "BEFORE TRUNCATE ON operator_audit_log"))
		}},
		{"no crossing is refused", holdingViolation, guards(func(g []policy.Guardrail) []policy.Guardrail {
			for i := range g {
				if g[i].Sid == "sys:tenant-mismatch" {
					g[i].Match = func(policy.Context) bool { return false }
				}
			}
			return g
		})},
		{"the venue sees the position", holdingViolation, func(s *securityFactSources) {
			const m = "00046_project_a_holding_tap_to_its_site.sql"
			migs(s, m, strings.Replace(s.Migs[m], "    ctr_gap            integer,", "    ctr_gap            integer,\n    gps_distance_m     numeric,", 1))
		}},
		{"a later migration shows the venue more", holdingViolation, later("ALTER TABLE site_punches ADD COLUMN source_ip inet;")},
		{"a business writes its own group", holdingViolation, func(s *securityFactSources) {
			const m = "00041_create_holdings.sql"
			migs(s, m, strings.Replace(s.Migs[m], "REVOKE ALL ON holdings FROM tappa_app;", "GRANT ALL ON holdings TO tappa_app;", 1))
		}},
		{"an unsigned delivery", webhookViolation, func(s *securityFactSources) {
			s.Delivery = strings.Replace(s.Delivery, "webhook.SignatureHeader: {key.Sign(body)}", "", 1)
		}},
		{"the page names another header", webhookViolation, func(s *securityFactSources) { s.Figures.WebhookHeader = "X-Signature" }},
		{"production moves", hostedViolation, func(s *securityFactSources) {
			s.Deploy = strings.Replace(s.Deploy, "fsn1 = Hetzner\nFalkenstein, Almanya", "ash = Hetzner\nAshburn, ABD", 1)
		}},
	} {
		if why := tc.derive(real); why != "" {
			t.Errorf("control %q: the derivation does not hold over the real sources: %s", tc.name, why)
			continue
		}
		m := real
		tc.mutate(&m)
		if sameSecuritySources(m, real) {
			t.Errorf("control %q: the mutation changed nothing in the sources", tc.name)
			continue
		}
		if tc.derive(m) == "" {
			t.Errorf("control %q: the derivation still holds over a source without it", tc.name)
		}
	}
}

// sameSecuritySources reports whether a and b read the same product. The guardrail lists
// carry funcs, which reflect.DeepEqual never calls equal, so they are compared by sid,
// effect and the code each func points at.
func sameSecuritySources(a, b securityFactSources) bool {
	sig := func(gs []policy.Guardrail) string {
		var out []string
		for _, g := range gs {
			var match, alert uintptr
			if g.Match != nil {
				match = reflect.ValueOf(g.Match).Pointer()
			}
			if g.Alert != nil {
				alert = reflect.ValueOf(g.Alert).Pointer()
			}
			out = append(out, g.Sid+"/"+string(g.Effect)+"/"+strconv.FormatUint(uint64(match), 16)+"/"+
				strconv.FormatUint(uint64(alert), 16))
		}
		return strings.Join(out, ",")
	}
	if sig(a.Guards) != sig(b.Guards) {
		return false
	}
	a.Guards, b.Guards = nil, nil
	return reflect.DeepEqual(a, b)
}

// --- the page's copy -----------------------------------------------------------------

// securityLines is every sentence the security page renders from a pages.Line: every
// Line its SecurityText carries, at any depth (a field, a rung's words and note, a Line
// in a slice), read by reflection so a Line added there is checked without a list to
// keep; the landing's values it repeats; and the sign-off's free months. A Line with no
// Text is a rung with no note and renders nothing.
func securityLines() map[string]pages.Line {
	out := map[string]pages.Line{
		"LandingSecurityLede":       pages.LandingSecurityLede,
		"LandingPrivacyNote":        pages.LandingPrivacyNote,
		"LandingPricingIncludes[4]": pages.LandingPricingIncludes[4],
		"LandingFreeMonthsLine":     pages.LandingFreeMonthsLine(foundingFreeMonths),
	}
	collectLines(reflect.ValueOf(pages.SecurityCopy(securityFigures())), "SecurityText", out)
	return out
}

var lineType = reflect.TypeOf(pages.Line{})

// collectLines adds every non-empty pages.Line inside v to out, by its path.
func collectLines(v reflect.Value, path string, out map[string]pages.Line) {
	switch {
	case v.Type() == lineType:
		if l := v.Interface().(pages.Line); l.Text != "" {
			out[path] = l
		}
	case v.Kind() == reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			collectLines(v.Field(i), path+"."+v.Type().Field(i).Name, out)
		}
	case v.Kind() == reflect.Slice || v.Kind() == reflect.Array:
		for i := 0; i < v.Len(); i++ {
			collectLines(v.Index(i), path+"["+strconv.Itoa(i)+"]", out)
		}
	}
}

// unreachableLines names every place in t a pages.Line could hide from collectLines:
// behind a pointer, a map, an interface, a func or a channel.
func unreachableLines(t reflect.Type, path string) []string {
	if t == lineType {
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		var out []string
		for i := 0; i < t.NumField(); i++ {
			out = append(out, unreachableLines(t.Field(i).Type, path+"."+t.Field(i).Name)...)
		}
		return out
	case reflect.Slice, reflect.Array:
		return unreachableLines(t.Elem(), path+"[]")
	case reflect.Pointer, reflect.Map, reflect.Interface, reflect.Func, reflect.Chan:
		return []string{path + " is a " + t.String() + ", which collectLines does not walk"}
	}
	return nil
}

// TestSecurityLines_EveryLineIsCollected: the walk above reaches every Line of the page
// -- the fields, the rungs' words and notes -- and no field's type could hide one from
// it. CONTROLS: a struct with a *Line and a map of Lines is refused twice.
func TestSecurityLines_EveryLineIsCollected(t *testing.T) {
	t.Parallel()
	for _, p := range unreachableLines(reflect.TypeOf(pages.SecurityText{}), "SecurityText") {
		t.Error(p)
	}
	lines := securityLines()
	if len(lines) < 70 {
		t.Fatalf("securityLines collects %d line(s); the page carries more", len(lines))
	}
	for _, want := range []string{"SecurityText.Guardrails[0].Text", "SecurityText.Guardrails[2].Note",
		"SecurityText.Default.Note", "SecurityText.FingerTitle", "SecurityText.ChipGPS"} {
		if _, ok := lines[want]; !ok {
			t.Errorf("securityLines does not reach %s", want)
		}
	}
	for name, l := range lines {
		if strings.TrimSpace(l.Text) == "" {
			t.Errorf("%s is empty", name)
		}
	}
	type hidden struct {
		A *pages.Line
		B map[string]pages.Line
	}
	if len(unreachableLines(reflect.TypeOf(hidden{}), "hidden")) != 2 {
		t.Error("CONTROL: a *Line and a map of Lines passed the reachability check")
	}
}

// --- the ladder ------------------------------------------------------------------------

// ladderProblems says how the page's ladder departs from the engine: every rung's
// statements exist and end where the rung says, every guardrail and every baseline rule
// about a tap is named once (shown or in the note), and the last rung is the engine's
// default. Package level so the control can hand it an engine the page does not describe.
func ladderProblems(c pages.SecurityText, gs []policy.Guardrail, docs []policy.BaselineDoc) []string {
	var out []string
	for _, r := range c.Guardrails {
		for _, sid := range r.Sids {
			g := guardBySid(gs, sid)
			if g == nil {
				out = append(out, "the rung "+strconv.Quote(r.Text.Text)+" names "+sid+", which is not a guardrail")
				continue
			}
			if guardEffectWord[g.Effect] != r.Verdict {
				out = append(out, "the rung "+strconv.Quote(r.Text.Text)+" ends in "+strconv.Quote(r.Verdict)+" and "+sid+
					" is "+string(g.Effect))
			}
			if g.Effect == policy.EffectRedirect && !strings.Contains(r.Note.Text, "nothing recorded") {
				out = append(out, "the rung "+strconv.Quote(r.Text.Text)+" redirects and does not say nothing is recorded")
			}
			if g.Alert != nil && g.Alert(policy.Context{}) != "" && !strings.Contains(r.Note.Text, "security event") {
				out = append(out, "the rung "+strconv.Quote(r.Text.Text)+" raises an alert and does not say so")
			}
		}
	}
	others := map[string]policy.Effect{
		"sys:tap-freshness": policy.EffectDeny, "sys:occurred-at-bound": policy.EffectDeny,
		"sys:tenant-mismatch": policy.EffectRedirect, "sys:cross-tenant-qr-site-ip": policy.EffectReview,
		"sys:policy-edit-owner-only": policy.EffectDeny,
	}
	for _, o := range c.OtherGuards {
		g := guardBySid(gs, o.Sid)
		want, described := others[o.Sid]
		switch {
		case g == nil:
			out = append(out, "the note names "+o.Sid+", which is not a guardrail")
		case !described:
			out = append(out, "the note names "+o.Sid+" and this test does not know what its phrase claims")
		case g.Effect != want:
			out = append(out, "the note's phrase for "+o.Sid+" assumes "+string(want)+" and it is "+string(g.Effect))
		}
	}
	named := map[string]int{}
	for _, r := range c.Guardrails {
		for _, sid := range r.Sids {
			named[sid]++
		}
	}
	for _, o := range c.OtherGuards {
		named[o.Sid]++
	}
	// A guardrail the page deliberately does not describe is named here with its reason,
	// so a new one is still red until somebody decides what the page says about it.
	for _, o := range c.UnnamedGuards {
		named[o.Sid]++
		if guardBySid(gs, o.Sid) == nil {
			out = append(out, "the page leaves out "+o.Sid+", which is not a guardrail")
		}
		if strings.Contains(c.LadderNote.Text, o.Phrase) {
			out = append(out, "the note describes "+o.Sid+", which the page says it leaves out")
		}
	}
	for _, g := range gs {
		if named[g.Sid] != 1 {
			out = append(out, "the guardrail "+g.Sid+" is named "+strconv.Itoa(named[g.Sid])+" time(s)")
		}
	}
	// The baseline: every statement that decides a tap is a rung or in the note.
	sts := baselineStatements(docs)
	baseNamed := map[string]int{}
	for _, r := range c.Baseline {
		for _, sid := range r.Sids {
			baseNamed[sid]++
			st, ok := sts[sid]
			if !ok {
				out = append(out, "the rung "+strconv.Quote(r.Text.Text)+" names "+sid+", which is not in the baseline")
				continue
			}
			if guardEffectWord[st.Effect] != r.Verdict {
				out = append(out, "the rung "+strconv.Quote(r.Text.Text)+" ends in "+strconv.Quote(r.Verdict)+" and "+sid+
					" is "+string(st.Effect))
			}
		}
	}
	for _, o := range c.OtherBaseline {
		baseNamed[o.Sid]++
		if st, ok := sts[o.Sid]; !ok || st.Effect != policy.EffectReview {
			out = append(out, "the note says "+o.Sid+" flags a tap, and it does not")
		}
	}
	baseNamed[c.NotedBaseline.Sid]++
	if st, ok := sts[c.NotedBaseline.Sid]; !ok || st.Effect != policy.EffectNote {
		out = append(out, "the note says "+c.NotedBaseline.Sid+" only writes a note, and it does not")
	}
	for sid, st := range sts {
		if !slices.Contains(st.Action, policy.ActionTapRecord) {
			continue
		}
		if baseNamed[sid] != 1 {
			out = append(out, "the baseline's tap rule "+sid+" is named "+strconv.Itoa(baseNamed[sid])+" time(s)")
		}
	}
	if d := policy.Evaluate(policy.Set{Guardrails: gs}, tapAt(map[policy.ContextKey]any{})); guardEffectWord[d.Effect] != c.Default.Verdict {
		out = append(out, "the last rung ends in "+strconv.Quote(c.Default.Verdict)+" and the engine's default is "+string(d.Effect))
	}
	sort.Strings(out)
	return out
}

// TestSecurityLadder_EveryRungIsTheEngines: the ladder, the note and the last rung are
// the engine's, read live. CONTROLS: a twelfth guardrail, a rung whose stamp is not the
// statement's effect, and a baseline rule nobody names are each red.
func TestSecurityLadder_EveryRungIsTheEngines(t *testing.T) {
	t.Parallel()
	c := pages.SecurityCopy(securityFigures())
	gs, docs := policy.Guardrails(policy.DefaultParams()), policy.Baseline()
	for _, p := range ladderProblems(c, gs, docs) {
		t.Error(p)
	}
	if len(c.Guardrails) != 5 {
		t.Errorf("the guardrail card shows %d rung(s); the note says five are shown", len(c.Guardrails))
	}
	if !strings.Contains(c.LadderNote.Text, "Five of the "+pagesCountWord(len(gs))+" guardrails are shown.") {
		t.Errorf("the note does not count the engine's %d guardrails: %q", len(gs), c.LadderNote.Text)
	}
	if len(ladderProblems(c, append(slices.Clone(gs), policy.Guardrail{Sid: "sys:x", Effect: policy.EffectDeny}), docs)) == 0 {
		t.Error("CONTROL: a twelfth guardrail passed")
	}
	flipped := c
	flipped.Guardrails = slices.Clone(c.Guardrails)
	flipped.Guardrails[4].Verdict = "reject"
	if len(ladderProblems(flipped, gs, docs)) == 0 {
		t.Error("CONTROL: the debounce rung stamped REJECTED passed")
	}
	extra := append(slices.Clone(docs), policy.BaselineDoc{Document: policy.Document{Statements: []policy.Statement{{
		Sid: "base:x", Effect: policy.EffectReview, Action: []policy.Action{policy.ActionTapRecord},
	}}}})
	if len(ladderProblems(c, gs, extra)) == 0 {
		t.Error("CONTROL: a baseline rule the page does not name passed")
	}
}

// pagesCountWord spells a count as the page does (pages.countWord is unexported).
func pagesCountWord(n int) string {
	return []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
		"eleven", "twelve"}[n]
}

// --- the page as rendered ------------------------------------------------------------

// TestSecurityFigures_AreTheProductsAndThePagePrintsThem: the figures the handler hands
// the page are the product's, and the page prints each from the figures, never from a
// literal. CONTROL: the page rendered with other figures prints the other numbers and
// not the real ones.
func TestSecurityFigures_AreTheProductsAndThePagePrintsThem(t *testing.T) {
	t.Parallel()
	f := securityFigures()
	if want := len(policy.Guardrails(policy.DefaultParams())); f.Guardrails != want {
		t.Errorf("the page counts %d guardrails; the engine runs %d", f.Guardrails, want)
	}
	if want := int(policy.DefaultParams().DebounceWindow / time.Second); f.DebounceSeconds != want {
		t.Errorf("the page's window is %d s; policy.DefaultParams is %d s", f.DebounceSeconds, want)
	}
	m := gpsRadiusDefaultRE.FindStringSubmatch(repoFile(t, "internal", "config", "config.go"))
	if m == nil || m[1] != strconv.Itoa(f.GPSRadiusM) {
		t.Errorf("the page's radius is %d m and internal/config's default is %v", f.GPSRadiusM, m)
	}
	text := renderedPublicText(t, pages.SecurityPath)
	for _, want := range []string{
		strconv.Itoa(f.SignatureHexChars) + "-character signature (CMAC)",
		"AES-" + strconv.Itoa(f.PlaqueKeyBits) + " key",
		strconv.Itoa(f.TokenBits) + "-bit token",
		"a /" + strconv.Itoa(f.IPv4Prefix) + " holds in IPv4, or a /" + strconv.Itoa(f.IPv6Prefix) + " in IPv6",
		strconv.Itoa(f.GPSRadiusM) + " m by default",
		"within " + strconv.Itoa(f.DebounceSeconds) + " seconds",
		"of the " + pagesCountWord(f.Guardrails) + " guardrails",
		"Every tap " + strconv.Itoa(f.TrustBase),
		"+ " + strconv.Itoa(f.TrustIP),
		"+ " + strconv.Itoa(f.TrustGPS),
		f.WebhookHeader + " header",
	} {
		if !strings.Contains(text, asRendered(want)) {
			t.Errorf("/security does not print %q", want)
		}
	}
	other := pages.SecurityFigures{SignatureHexChars: 24, PlaqueKeyBits: 192, TokenBits: 384, IPv4Prefix: 9,
		IPv6Prefix: 33, GPSRadiusM: 175, DebounceSeconds: 45, Guardrails: 12, TrustBase: 15, TrustIP: 55,
		TrustGPS: 35, WebhookHeader: "X-Other-Signature"}
	var b strings.Builder
	if err := pages.Security(pages.SecurityView{SignupHref: signupPath, SignInHref: adminLoginPath,
		FreeMonths: foundingFreeMonths, Figures: other, Copy: pages.SecurityCopy(other)}).Render(t.Context(), &b); err != nil {
		t.Fatal(err)
	}
	otherText := screenText(t, b.String())
	for _, want := range []string{"24-character", "AES-192", "384-bit", "a /9 holds", "a /33 in IPv6",
		"175 m by default", "within 45 seconds", "of the twelve guardrails", "Every tap 15", "+ 55", "+ 35",
		"X-Other-Signature header", "limit /9", "24 hex", "A random 384-bit token"} {
		if !strings.Contains(otherText, asRendered(want)) {
			t.Errorf("CONTROL: the page rendered with other figures does not print %q, so it prints a literal", want)
		}
	}
	for _, real := range []string{"16-character", "256-bit", "a /7 holds", "150 m by default", "within 60 seconds",
		"of the eleven guardrails", "limit /7", "16 hex"} {
		if strings.Contains(otherText, asRendered(real)) {
			t.Errorf("CONTROL: the page rendered with other figures still prints %q", real)
		}
	}
}

// k2Forbidden is what the hosting sentence may not publish (user decision K2): the town,
// the data centre and the provider.
var k2Forbidden = []string{"falkenstein", "fsn1", "hetzner"}

// hostingProblems says how a page body departs from K2.
func hostingProblems(body string) []string {
	var out []string
	lower := strings.ToLower(body)
	for _, word := range k2Forbidden {
		if strings.Contains(lower, word) {
			out = append(out, "the page publishes "+strconv.Quote(word)+" (K2: the country and the union only)")
		}
	}
	if !strings.Contains(body, "Hosted in the EU (Germany)") {
		out = append(out, "the page does not say \"Hosted in the EU (Germany)\"")
	}
	return out
}

// TestSecurityPage_SaysWhereItIsHostedAndNoMore holds K2 on the page's bytes and on every
// other page of the public site. CONTROL: a body naming the town is red, and so is one
// that lost the sentence.
func TestSecurityPage_SaysWhereItIsHostedAndNoMore(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	body := mustFetchMarketing(t, r, pages.SecurityPath)
	for _, p := range hostingProblems(body) {
		t.Error(p)
	}
	for _, url := range marketingURLs() {
		lower := strings.ToLower(mustFetchMarketing(t, r, url))
		for _, word := range k2Forbidden {
			if strings.Contains(lower, word) {
				t.Errorf("%s publishes %q (K2)", url, word)
			}
		}
	}
	if len(hostingProblems(strings.Replace(body, "runs in Germany.", "runs in Falkenstein, Germany.", 1))) == 0 {
		t.Error("CONTROL: a page naming the town passed")
	}
	if len(hostingProblems(strings.ReplaceAll(body, "Hosted in the EU (Germany)", "Hosted in Germany"))) == 0 {
		t.Error("CONTROL: a page without the agreed sentence passed")
	}
}

// TestSecurityPage_TheLandingLinksToTheProofs: the landing's "How each proof works"
// lands on the security page's four proofs, through the link check every public page
// passes. CONTROL: the page without the section's id is red.
func TestSecurityPage_TheLandingLinksToTheProofs(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	href := pages.SecurityPath + "#" + pages.SecurityProofsID
	if href != "/security#proofs" {
		t.Errorf("the landing links to %s; the boards draw /security#proofs", href)
	}
	landing := mustFetchMarketing(t, r, "/")
	if !regexp.MustCompile(`<a class="more[^"]*" href="` + regexp.QuoteMeta(href) + `">\s*How each proof works`).MatchString(landing) {
		t.Error("the landing's Security section does not end with \"How each proof works\" to " + href)
	}
	if why := internalLinkProblem(t, r, href); why != "" {
		t.Error(why)
	}
	body := mustFetchMarketing(t, r, pages.SecurityPath)
	without := strings.Replace(body, `id="`+pages.SecurityProofsID+`"`, `id="gone"`, 1)
	if without == body {
		t.Fatal("PREMISE: the page renders no id=\"proofs\"")
	}
	for _, m := range idAttrRE.FindAllStringSubmatch(without, -1) {
		if m[1] == pages.SecurityProofsID {
			t.Error("CONTROL: the page without the section's id still carries it")
		}
	}
}

// TestSecurityPage_SaysTheDecisionRulesAsTheFAQDoes: §5's QR sentence and the
// disjunction of row six are said here as the landing's FAQ says them, and the FAQ's
// refused phrasings are refused here too; the drawn samples are what their captions say.
func TestSecurityPage_SaysTheDecisionRulesAsTheFAQDoes(t *testing.T) {
	t.Parallel()
	text := renderedPublicText(t, pages.SecurityPath)
	for _, banned := range []string{"have to agree", "IP or GPS proof", "or GPS proof instead",
		"position on its own is enough", "position alone is enough", "guarantee", "more secure than"} {
		if strings.Contains(text, banned) {
			t.Errorf("/security says %q, which mis-states CLAUDE.md §5 or claims what nobody measured", banned)
		}
	}
	for _, must := range []string{
		"a position alone is not enough for it: it needs the venue's network",
		"Venue network matched, or GPS inside the radius",
	} {
		if !strings.Contains(text, must) {
			t.Errorf("/security no longer says %q", must)
		}
	}
	// The hero's sample is a network match with no position (its chips say so), and the
	// flow's is a tap with neither (its first step says so).
	hero, flow := pages.SecurityHeroDocket, pages.LandingPanelPassDockets[2]
	chips := pages.SecurityCopy(securityFigures())
	if hero.Channel != "nfc" || hero.IP != "yes" || hero.GPS != "no" || chips.ChipGPS.Text != "Asked · none given" {
		t.Errorf("the hero's sample record is %+v with the chip %q; its chips say a signed tap, an address match "+
			"and no position given", hero, chips.ChipGPS.Text)
	}
	for _, p := range panelDocketProblems(hero, "") {
		t.Errorf("the hero's sample record: %s", p)
	}
	for _, p := range samplePeopleProblems([]string{hero.Who}, seedPeople(t)) {
		t.Errorf("the hero's sample record: %s", p)
	}
	if flow.Channel != "nfc" || flow.IP != "no" || flow.GPS != "no" || flow.Verdict != "flag" {
		t.Errorf("the flow's sample record is %+v; its first step says a real NFC tap with neither", flow)
	}
	for _, d := range []pages.PanelDocket{hero, flow} {
		want := strings.Join([]string{d.Venue, stampWord(d.Verdict), d.Who, d.Direction, d.At,
			"Trust " + d.Trust, "IP " + d.IP + " · GPS " + d.GPS}, " ")
		if !strings.Contains(text, want) {
			t.Errorf("/security does not draw the sample record as %q", want)
		}
	}
	// Every proof card the landing carries is on this page too.
	for _, e := range pages.LandingEvidence {
		if !strings.Contains(text, asRendered(e.Body)) {
			t.Errorf("/security does not carry the proof card %q", e.Label)
		}
	}
}

// securityRefusedPhrasings are the sentences the security review (LP-07, 2026-10-22) took
// off the page because the product does not keep them, each with its reason. None may
// come back, on this page or any other page of the public site.
var securityRefusedPhrasings = map[string]string{
	"own record":                       "nothing stops a manager who is also an employee deciding their own flag",
	"describe one place":               "a /7's worth of addresses is a provider's block, not one place",
	"database owner's own account too": "the owner's guard is a trigger a deliberate change can switch off",
	"When Taptime's own staff":         "staff with the database owner's account read outside the support console",
	"counter already past it":          "the strict comparison refuses an equal counter too",
	"Backup · not needed":              "the tap page asks for a position on every press",
	"never silently approved":          "an owner can write a rule that allows without evidence",
	"Four proofs on every single tap":  "a QR check-in carries no signature and a phone may give no position",
	"four independent signals":         "a QR check-in carries no signature and a phone may give no position",
}

// TestSecurityPage_SaysNoneOfTheRefusedPhrasings: the review's refused sentences are on
// no public page, and /security opens with "Four checks behind every tap." rather than
// the landing's "Four proofs on every single tap." (a QR check-in carries no
// signature). CONTROL: each refused phrase planted in the page's text is red.
func TestSecurityPage_SaysNoneOfTheRefusedPhrasings(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	refused := func(text string) []string {
		var out []string
		for phrase, why := range securityRefusedPhrasings {
			if strings.Contains(text, phrase) {
				out = append(out, strconv.Quote(phrase)+": "+why)
			}
		}
		sort.Strings(out)
		return out
	}
	for _, url := range marketingURLs() {
		for _, p := range refused(screenText(t, mustFetchMarketing(t, r, url))) {
			t.Errorf("%s says %s", url, p)
		}
	}
	text := renderedPublicText(t, pages.SecurityPath)
	if !strings.Contains(text, "Four checks behind every tap.") || strings.Contains(text, "Four proofs on every single tap.") {
		t.Error("/security does not open with \"Four checks behind every tap.\"")
	}
	for _, must := range []string{
		"and the decision cannot be changed afterwards",
		"switching that guard off would take a deliberate change to the database itself",
		"Taptime's support console has its own sign-in, separate from yours",
		"too wide to stand for a single network",
		"keep the list to your venue's own connection",
		"by default nothing is approved without evidence",
		"never the location evidence",
		"already at or past it",
		"Asked · none given",
	} {
		if !strings.Contains(text, must) {
			t.Errorf("/security no longer says %q", must)
		}
	}
	for phrase := range securityRefusedPhrasings {
		if len(refused(text+" "+phrase)) == 0 {
			t.Errorf("CONTROL: %q planted in the page passed", phrase)
		}
	}
}
