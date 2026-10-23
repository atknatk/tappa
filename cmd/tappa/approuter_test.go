package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/billing"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/legal"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/review"
	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/handler"
	"github.com/atknatk/tappa/internal/handler/api"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
)

// approuter_test.go -- ADR 0025 §3, "Hangi yönlendirici yürünür" (OP-15 phase B): the
// application's routes are walked on the router appRouter composes -- the function
// run() serves -- with every feature built the way run() builds it, over a database
// that answers nothing. Building a feature runs no query, so nothing here needs
// Postgres, and the walk sees exactly the routes the binary mounts.

// errSilent is every answer silentDB gives.
var errSilent = errors.New("silentDB: this walk builds routers and never queries")

// silentDB satisfies every Database the features' constructors take (session, invite,
// audit, tenant, checkin, adminauth and its resets, ledger, review, manual, billing,
// signup, legal, sun's tag resolver): the union of *db.DB's methods those name. It is
// never called while routes are walked.
type silentDB struct{}

func (silentDB) WithTenant(context.Context, uuid.UUID, db.TxFunc) error { return errSilent }
func (silentDB) GetTagByUID(context.Context, string) (db.ResolvedTag, error) {
	return db.ResolvedTag{}, errSilent
}
func (silentDB) GetEmployeeBySessionHash(context.Context, string) (db.ResolvedSession, error) {
	return db.ResolvedSession{}, errSilent
}
func (silentDB) GetInviteByCodeHash(context.Context, string) (db.ResolvedInvite, error) {
	return db.ResolvedInvite{}, errSilent
}
func (silentDB) GetAdminByEmail(context.Context, string) ([]db.ResolvedAdmin, error) {
	return nil, errSilent
}
func (silentDB) GetAdminSessionByTokenHash(context.Context, string) (db.ResolvedAdminSession, error) {
	return db.ResolvedAdminSession{}, errSilent
}
func (silentDB) GetPasswordResetByTokenHash(context.Context, string) (db.ResolvedPasswordReset, error) {
	return db.ResolvedPasswordReset{}, errSilent
}
func (silentDB) Ping(context.Context) error { return errSilent }
func (silentDB) GetAPIKeyByHash(context.Context, string) (db.ResolvedAPIKey, error) {
	return db.ResolvedAPIKey{}, errSilent
}

// appFeatures is every feature appRouter takes, built as run() builds it, plus the
// feature names the walk attributes routes to.
type appFeatures struct {
	activation *handler.Activation
	tap        *handler.Tap
	panel      *handler.AdminAuth
	logos      *handler.BrandLogos
	marketing  *handler.Marketing
	signupFlow *handler.Signup
	resetFlow  *handler.AdminReset
	ready      *handler.Health
	theme      *handler.BrandTheme
	operator   *operator.Surface
	// api and devTools are nil interfaces when off, exactly as run() hands them.
	api      httpx.Mounter
	devTools httpx.Mounter
}

func (f appFeatures) router(cfg *config.Config) http.Handler {
	return appRouter(cfg, nil, f.activation, f.tap, f.panel, f.logos, f.marketing, f.signupFlow, f.resetFlow, f.ready, f.theme, f.operator, f.api, f.devTools)
}

func (f appFeatures) named() []struct {
	name string
	m    httpx.Mounter
} {
	return []struct {
		name string
		m    httpx.Mounter
	}{
		{"activation", f.activation}, {"tap", f.tap}, {"panel", f.panel}, {"logos", f.logos},
		{"marketing", f.marketing}, {"signup", f.signupFlow}, {"reset", f.resetFlow},
		{"ready", f.ready}, {"theme", f.theme}, {"operator", f.operator},
		{"api", f.api}, {"devtools", f.devTools},
	}
}

// walkConfig is a configuration every constructor accepts: keys of the right sizes,
// the decision engine's thresholds, and -- when on -- the three optional surfaces'
// settings: the operator surface's three, the integration API's host and key (M11
// API-4), and the dev-only tap simulator's opt-in on a loopback base URL (ADR 0026).
func walkConfig(t *testing.T, on bool) *config.Config {
	t.Helper()
	cfg := &config.Config{
		Env: config.EnvDev, BaseURL: "https://taptime.mt",
		SessionHMACKey: randBytes(t, 32), InviteHMACKey: randBytes(t, 32), TagKEK: randBytes(t, 32),
		APIKeyHMACKey:   randBytes(t, 32),
		GPSRadiusMeters: 150, Debounce: 60 * time.Second, Freshness: 180 * time.Second, RetentionYears: 2,
	}
	if on {
		cfg.OperatorTOTPKEK, cfg.OperatorTokenHMACKey, cfg.OperatorHost = randBytes(t, 32), randBytes(t, 32), "ops.taptime.mt"
		cfg.APIHost = "api.taptime.mt"
		cfg.DevTools, cfg.BaseURL = true, "http://localhost:8080"
	}
	return cfg
}

// buildAppFeatures mirrors run()'s construction, feature by feature, over silentDB.
// The encoder is nil (a deployment without an https base URL: the three relay routes
// are mounted either way), the reset channel is "none", and the panel gets the one option
// run() always passes (API keys -- NewAdminAuth refuses a configured key without it) and
// not the two it passes by deployment (e-mailed invitations, the webhook): mountWriting
// registers every route unconditionally and an option changes what a route DOES, not
// which routes exist.
// The integration API is built as openAPISurface builds it (that function takes the real
// pool, so its body is mirrored here -- the suspension reader included, tenant.WriteSuspension
// since API-4b) with no sandbox extension -- the production build's.
func buildAppFeatures(t *testing.T, cfg *config.Config, on bool) appFeatures {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	data := silentDB{}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	sessions, err := session.New(data, cfg)
	must(err)
	invites, err := invite.New(data, cfg)
	must(err)
	trail, err := audit.New(data)
	must(err)
	verifier := sun.NewVerifier(data, cfg.TagKEK, cfg.TagKEKPrevious)
	brandReader, err := tenant.NewBrandReader(data)
	must(err)
	activation, err := handler.NewActivation(invites, sessions, verifier, trail, brandReader, cfg, log)
	must(err)
	directory, err := tenant.NewDirectory(data)
	must(err)
	checkins, err := checkin.New(data, trail, cfg, log)
	must(err)
	tap, err := handler.NewTap(verifier, directory, sessions, checkins, activation, trail, cfg, log)
	must(err)
	admins, err := adminauth.New(data, cfg)
	must(err)
	records, err := ledger.NewReader(data, log)
	must(err)
	reviewer, err := review.NewReviewer(data, trail, log)
	must(err)
	staff, err := tenant.NewStaff(data, trail, log)
	must(err)
	venues, err := tenant.NewVenues(data, trail, log)
	must(err)
	plaques, err := tenant.NewPlaques(data, trail, log)
	must(err)
	entries, err := manual.NewRecorder(data, trail, log)
	must(err)
	rules, err := tenant.NewRulebook(data, checkins.Params(), checkins.GPSRadiusM(), log)
	must(err)
	ruleWriter, err := tenant.NewRuleWriter(data, trail, checkins, checkins.Params(), log)
	must(err)
	books, err := billing.NewBook(data, trail, log)
	must(err)
	accounts, err := tenant.NewAccounts(data, trail, log)
	must(err)
	brandWriter, err := tenant.NewBrands(data, trail, log)
	must(err)
	resets, err := adminauth.NewResets(data, cfg)
	must(err)
	resetFlow, err := handler.NewAdminReset(resets, nil, trail, cfg, log)
	must(err)
	apiKeys, err := apikey.NewKeys(apiKeyPrefix, cfg.APIKeyHMACKey, data, trail)
	must(err)
	panel, err := handler.NewAdminAuth(admins, trail, records, records, reviewer, staff, invites, venues, plaques, entries, rules, ruleWriter, books, accounts, brandReader, brandWriter, nil, resetFlow, cfg, log, handler.IntegrationKeys(apiKeys))
	must(err)
	logos, err := handler.NewBrandLogos(brandReader, panel, tap, log)
	must(err)
	texts, err := legal.NewStore(data)
	must(err)
	provisioner, err := signup.NewProvisioner(data, trail, log)
	must(err)
	signupFlow, err := handler.NewSignup(provisioner, noCheck{}, cfg, log)
	must(err)
	ready, err := handler.NewHealth(data.Ping, log)
	must(err)
	f := appFeatures{
		activation: activation, tap: tap, panel: panel, logos: logos,
		marketing: handler.NewMarketing(texts, log, cfg.APIHost, cfg.BaseURL), signupFlow: signupFlow, resetFlow: resetFlow,
		ready: ready, theme: handler.NewBrandTheme(), operator: operator.Off(),
	}
	if !on {
		return f
	}
	f.operator, err = configuredSurface(noStore{}, noTexts{}, noCheck{}, cfg, log)
	must(err)
	chain, err := apikey.NewAuthenticator(apiKeyPrefix, cfg.APIKeyHMACKey, data, data)
	must(err)
	writes, err := apikey.NewWrites(data)
	must(err)
	punches, err := tenant.NewPunchFeed(data)
	must(err)
	employees, err := tenant.NewEmployeeDirectory(data)
	must(err)
	surface, err := api.New(api.Config{
		Host: cfg.APIHost, Keys: chain, Writes: writes, Suspension: tenant.WriteSuspension{}, Work: noWork{},
		Locations: venues, Employees: staff, Punches: punches, Directory: employees, Log: log,
	})
	must(err)
	f.api = surface
	if !handler.DevToolsEnabled(cfg) {
		t.Fatal("the walk's configuration does not enable the dev tools")
	}
	minter, err := verifier.WithDevelopmentMinting(cfg)
	must(err)
	devTap, err := handler.NewDevTap(minter, plaques, invites, sessions, cfg, log)
	must(err)
	f.devTools = devTap
	return f
}

// noWork is K-19's set for a walk: nothing is noticed because nothing is served.
type noWork struct{}

func (noWork) Notice(uuid.UUID) {}

// walkOf is every "METHOD /pattern" a router serves.
func walkOf(t *testing.T, h http.Handler) []string {
	t.Helper()
	routes, ok := h.(chi.Routes)
	if !ok {
		t.Fatalf("%T is not a chi router", h)
	}
	var out []string
	if err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out = append(out, method+" "+route)
		return nil
	}); err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	sort.Strings(out)
	return out
}

// TestAppRoutes_EveryNonReadRouteHasOneOfFourClasses is ADR 0025 §3's application-wide
// classification, over the router appRouter composes (the one run() serves), with the
// three optional surfaces -- the operator's, the integration API's and the dev-only
// tap simulator -- all off, and all configured. Every (method, route) the walk reports
// for a method other than GET and HEAD -- every method, not only POST: chi reports a
// route registered without a method once per method it knows -- falls into exactly one
// class:
//
//	1 panel write       mounted by the panel (AdminAuth) and not an exception: behind
//	                    ProtectWriting, whose last ring is the suspension gate. That
//	                    each of these answers 403 for a suspended business is measured
//	                    by internal/handler's
//	                    TestSuspensionRoutes_EveryPanelWriteIsGatedOrANamedException.
//	2 panel exception   ADR 0025's exceptions, by name: its eight, and K-26's three
//	                    defences (ADR 0025's M11 note; plan §4.7).
//	3 outside the panel the tap, activation, password recovery and sign-up routes and
//	                    the dev-only simulator, by name; the configured operator surface's
//	                    routes (its own host, its own identity -- ADR 0020; classified
//	                    route by route by internal/handler/operator's own header classes);
//	                    and the integration API's /v1 routes (its own host, its own key
//	                    identity and its OWN suspension checkpoint -- ADR 0028 §10,
//	                    internal/handler/api's WriteClass, API-4b's to measure).
//	4 methodless, no    /static/* (the embedded read-only file server), and /operator
//	  writes            and /operator/* while the operator surface is off (503 to every
//	                    method) -- by name, and required to answer EVERY method.
//
// The walked routes must also be exactly the union of what each feature mounts on its
// own plus the router's own (/healthz, /static/*), so a route with no owner is red.
// A route in no class is red; a name in a class that the walk does not report is red.
//
// PART II -- what it catches: a POST (or PUT, DELETE...) added to any feature outside
// the panel's writing chain; a further panel exception; a methodless route added; the
// walk narrowed to one method; a feature added to run() without being added to appRouter
// (it would not compile here) or composed outside it (TestAppRouter_IsTheOnlyComposition).
// PART III: a form not on that list is code review's -- no completeness claim; the
// sandbox build's /v1 extensions are not walked (this file builds without the tag).
func TestAppRoutes_EveryNonReadRouteHasOneOfFourClasses(t *testing.T) {
	panelExceptions := map[string]bool{
		"POST /admin/login": true, "POST /admin/login/choose": true, "POST /admin/logout": true,
		"POST /admin/account/password": true, "POST /admin/employees/add": true,
		"POST /admin/employees/invite": true, "POST /admin/records/new": true, "POST /admin/records": true,
		"POST /admin/api-keys/revoke": true, "POST /admin/api-keys/revoke-all": true,
		"POST /admin/api-keys/webhook/disable": true,
	}
	outsidePanel := map[string]string{
		"POST /activate":        "activation",
		"POST /api/activate":    "activation",
		"POST /api/checkin":     "tap",
		"POST /admin/reset":     "reset",
		"POST /admin/reset/new": "reset",
		"POST /signup":          "signup",
		"POST /signup/places":   "signup",
		"POST /signup/account":  "signup",
	}
	// Named routes that exist only with the optional surfaces on.
	outsidePanelWhenOn := map[string]string{
		"POST " + handler.DevSimulateTapPath: "devtools",
	}
	methodless := map[string]string{"/static/*": "router", "/operator": "operator", "/operator/*": "operator"}
	// chi's own method list, less the two reads: what a methodless route is reported under.
	allWrites := []string{"CONNECT", "DELETE", "OPTIONS", "PATCH", "POST", "PUT", "QUERY", "TRACE"}

	for _, on := range []bool{false, true} {
		name := map[bool]string{false: "optional surfaces off", true: "optional surfaces configured"}[on]
		t.Run(name, func(t *testing.T) {
			cfg := walkConfig(t, on)
			f := buildAppFeatures(t, cfg, on)
			if f.operator.Configured() != on || (f.api != nil) != on || (f.devTools != nil) != on {
				t.Fatalf("the optional surfaces' state is operator %v, api %v, dev tools %v; want all %v",
					f.operator.Configured(), f.api != nil, f.devTools != nil, on)
			}
			named := map[string]string{}
			for mr, who := range outsidePanel {
				named[mr] = who
			}
			if on {
				for mr, who := range outsidePanelWhenOn {
					named[mr] = who
				}
			}

			owner := map[string]string{}
			for _, feat := range f.named() {
				if feat.m == nil {
					continue
				}
				r := chi.NewRouter()
				feat.m.Mount(r)
				for _, mr := range walkOf(t, r) {
					if prev, ok := owner[mr]; ok {
						t.Errorf("%s is mounted by both %s and %s", mr, prev, feat.name)
					}
					owner[mr] = feat.name
				}
			}
			for _, mr := range walkOf(t, httpx.NewRouter(cfg, nil)) {
				if _, ok := owner[mr]; !ok {
					owner[mr] = "router"
				}
			}

			app := walkOf(t, f.router(cfg))
			var unowned, missing []string
			seen := map[string]bool{}
			for _, mr := range app {
				seen[mr] = true
				if _, ok := owner[mr]; !ok {
					unowned = append(unowned, mr)
				}
			}
			for mr := range owner {
				if !seen[mr] {
					missing = append(missing, mr)
				}
			}
			if len(unowned) > 0 || len(missing) > 0 {
				t.Fatalf("the composed router and its features disagree: unowned %v, mounted by a feature "+
					"but not served %v", unowned, missing)
			}

			counts := map[int]int{}
			api := 0
			methodlessSeen := map[string][]string{}
			found := map[string]bool{}
			for _, mr := range app {
				if strings.HasPrefix(mr, "GET ") || strings.HasPrefix(mr, "HEAD ") {
					continue
				}
				method, route, _ := strings.Cut(mr, " ")
				who := owner[mr]
				switch {
				case who == "panel" && panelExceptions[mr]:
					counts[2]++
					found[mr] = true
				case who == "panel":
					counts[1]++
				case named[mr] != "" && named[mr] == who:
					counts[3]++
					found[mr] = true
				case who == "operator" && on && strings.HasPrefix(route, "/operator"):
					counts[3]++
				case who == "api" && on && strings.HasPrefix(route, "/v1"):
					counts[3]++
					api++
				case methodless[route] == who && (route == "/static/*" || !on):
					counts[4]++
					methodlessSeen[route] = append(methodlessSeen[route], method)
				default:
					t.Errorf("%s (mounted by %s) is in no class: a write route outside the panel's writing "+
						"chain that ADR 0025 §3 has not named", mr, who)
				}
			}
			for mr := range panelExceptions {
				if !found[mr] {
					t.Errorf("the panel exception %s is not served", mr)
				}
			}
			for mr := range named {
				if !found[mr] {
					t.Errorf("the outside-the-panel route %s is not served", mr)
				}
			}
			for route := range methodless {
				if route != "/static/*" && on {
					continue
				}
				got := methodlessSeen[route]
				sort.Strings(got)
				if !slices.Equal(got, allWrites) {
					t.Errorf("the methodless route %s is reported under %v, want every method %v -- "+
						"either it gained a method or this walk stopped looking at every method", route, got, allWrites)
				}
			}
			if counts[1] < 20 || counts[2] != len(panelExceptions) || (on && api == 0) {
				t.Fatalf("classes %v (/v1 %d): the panel's writes or the API's were not walked", counts, api)
			}
			t.Logf("%s: class 1 (panel, gated) %d · class 2 (named exceptions) %d · class 3 (outside the panel) %d, of them /v1 %d · class 4 (methodless) %d",
				name, counts[1], counts[2], counts[3], api, counts[4])
		})
	}
}

// TestAppRouter_IsTheOnlyComposition holds the other half of ADR 0025 §3's decision:
// across cmd/tappa's shipped sources, httpx.NewRouter is called exactly once, inside
// appRouter, with cfg, log and then appRouter's every feature parameter in declaration
// order, each once; appRouter is called exactly once, from run(). So the router the
// walk classifies and the router the binary serves are composed by one body, and a
// feature cannot reach the serving router without passing through appRouter's
// signature -- which the walk's own call must then fill.
func TestAppRouter_IsTheOnlyComposition(t *testing.T) {
	dir := filepath.Join(repoRoot, "cmd", "tappa")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	type call struct {
		in   string
		call *ast.CallExpr
	}
	var routers, composes []call
	var decl *ast.FuncDecl
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if fd.Recv == nil && fd.Name.Name == "appRouter" {
				decl = fd
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch qualName(c.Fun) {
				case "httpx.NewRouter":
					routers = append(routers, call{fd.Name.Name, c})
				case "appRouter":
					composes = append(composes, call{fd.Name.Name, c})
				}
				return true
			})
		}
	}
	if decl == nil {
		t.Fatal("cmd/tappa declares no appRouter; this test reads it by name")
	}
	if len(routers) != 1 || routers[0].in != "appRouter" {
		var where []string
		for _, r := range routers {
			where = append(where, r.in)
		}
		t.Fatalf("httpx.NewRouter is called from %v; want exactly once, from appRouter -- a router "+
			"composed anywhere else is one the route walk never sees", where)
	}
	if len(composes) != 1 || composes[0].in != "run" {
		var where []string
		for _, c := range composes {
			where = append(where, c.in)
		}
		t.Fatalf("appRouter is called from %v; want exactly once, from run()", where)
	}
	var params []string
	for _, field := range decl.Type.Params.List {
		for _, n := range field.Names {
			params = append(params, n.Name)
		}
	}
	var args []string
	for _, a := range routers[0].call.Args {
		id, ok := a.(*ast.Ident)
		if !ok {
			t.Fatalf("appRouter hands httpx.NewRouter an expression (%T), not one of its parameters", a)
		}
		args = append(args, id.Name)
	}
	if !slices.Equal(args, params) {
		t.Errorf("appRouter passes %v to httpx.NewRouter, want its parameters in order %v", args, params)
	}
	if len(params) < 14 {
		t.Errorf("appRouter declares %d parameter(s); cfg, log and the twelve features the binary mounts "+
			"are at least fourteen", len(params))
	}
	if len(composes[0].call.Args) != len(params) {
		t.Errorf("run() passes appRouter %d argument(s) for %d parameter(s)", len(composes[0].call.Args), len(params))
	}
}
