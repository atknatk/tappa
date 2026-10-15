package handler

// webhooks_db_test.go -- the webhook part of the API keys section (M11 API-9) driven over HTTP against
// REAL Postgres, with the real internal/webhook/endpoints service and the real trail behind it. The
// sender is a fake (nothing reaches the network; internal/egress has the dial-level tests): it answers
// the class a test chooses and records what it was handed.

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/egress"
	"github.com/atknatk/tappa/internal/webhook/endpoints"
)

var webhookKeyRE = regexp.MustCompile(`tt_whsec_[A-Za-z0-9_-]{43}`)

// webhookCanaryHost is in every address these tests type: a log or a trail row that holds it leaked.
const webhookCanaryHost = "panel-canary.api9.example.com"

var webhookCanaryURL = "https://" + webhookCanaryHost + "/taptime/hook-token-path"

type panelSender struct {
	mu     sync.Mutex
	posts  int
	result egress.Result
}

func (p *panelSender) Post(context.Context, egress.Endpoint, []byte, http.Header) egress.Result {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.posts++
	if p.result == (egress.Result{}) {
		return egress.Result{Status: 204}
	}
	return p.result
}

func (p *panelSender) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.posts
}

type webhookPanelFixture struct {
	*apiKeysDBFixture
	svc    *endpoints.Service
	sender *panelSender
}

func newWebhookPanelFixture(t *testing.T) *webhookPanelFixture {
	t.Helper()
	f := &webhookPanelFixture{apiKeysDBFixture: newAPIKeysDBFixture(t), sender: &panelSender{}}
	trail, err := audit.New(f.data)
	if err != nil {
		t.Fatal(err)
	}
	if f.svc, err = endpoints.New([]byte(strings.Repeat("w", 32)), f.data, trail, f.sender); err != nil {
		t.Fatal(err)
	}
	return f
}

// browserAs is a signed-in panel browser with both the keys and the webhook service.
func (f *webhookPanelFixture) browserAs(t *testing.T, adminID uuid.UUID, role string, withWebhook bool) *browser {
	t.Helper()
	trail, err := audit.New(f.data)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := tenant.NewAccounts(f.data, trail, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: uuid.New(), TenantID: f.tenantID, AdminUserID: adminID, Role: role, FullName: "Hook Admin"}, nil
	}}
	records := newFakeLedger()
	log := slog.New(slog.NewTextHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	opts := []AdminAuthOption{IntegrationKeys(f.keys)}
	if withWebhook {
		opts = append(opts, IntegrationWebhook(f.svc))
	}
	h, err := NewAdminAuth(admins, trail, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{},
		&fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), accounts, newFakeBrands(),
		newFakeBrandWriter(), nil, &fakeNotices{}, adminTestConfig(), log, opts...)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// endpointRow is the business's endpoint as the database holds it ("" url when there is none).
func (f *webhookPanelFixture) endpointRow(t *testing.T) (url string, sealed string, active bool) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT url, encode(signing_key_sealed, 'hex'), active FROM webhook_endpoints WHERE tenant_id = $1`, f.tenantID).
			Scan(&url, &sealed, &active)
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return
}

// trailText is every trail row of the business, as text.
func (f *webhookPanelFixture) trailText(t *testing.T) string {
	t.Helper()
	var sb strings.Builder
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action || ' ' || coalesce(target, '') || ' ' || coalesce(detail::text, '') FROM audit_log WHERE tenant_id = $1`, f.tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			sb.WriteString(s + "\n")
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

func (f *webhookPanelFixture) create(t *testing.T, b *browser) string {
	t.Helper()
	rec := b.do(http.MethodPost, webhookHref, url.Values{"address": {webhookCanaryURL}})
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d", rec.Code)
	}
	return webhookKeyRE.FindString(rec.Body.String())
}

// assertNoLeak fails when the logs or the trail hold the address, its host, its path or a key.
func (f *webhookPanelFixture) assertNoLeak(t *testing.T, keys ...string) {
	t.Helper()
	for where, text := range map[string]string{"log": f.logs.String(), "trail": f.trailText(t)} {
		for _, leak := range append([]string{webhookCanaryHost, "hook-token-path", "api9.example", "tt_whsec_"}, keys...) {
			if leak != "" && strings.Contains(text, leak) {
				t.Fatalf("the %s holds %q", where, leak)
			}
		}
	}
}

// TestWebhookPanel_TheKeyIsShownOnceAndNeverAgain is K-12 over HTTP: the create answer is 200 and
// carries exactly ONE signing key, which signs the next ping; the section read afterwards carries none
// (the address and the switch, never the key); re-posting the form issues no second endpoint and shows
// no key; rotating without the tick changes nothing, with it answers 200 with a NEW key and the old one
// no longer signs; the read after shows none; no log line and no trail row holds a key or the address.
func TestWebhookPanel_TheKeyIsShownOnceAndNeverAgain(t *testing.T) {
	f := newWebhookPanelFixture(t)
	b := f.browserAs(t, f.owner, "owner", true)
	rec := b.do(http.MethodPost, webhookHref, url.Values{"address": {webhookCanaryURL}})
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	keys := webhookKeyRE.FindAllString(rec.Body.String(), -1)
	if len(keys) != 1 {
		t.Fatalf("the create answer carries %d keys, want 1", len(keys))
	}
	first := keys[0]
	page := b.do(http.MethodGet, apiKeysHref, nil).Body.String()
	if webhookKeyRE.MatchString(page) || !strings.Contains(page, webhookCanaryURL) || !strings.Contains(page, ">On<") {
		t.Fatal("the section after creation shows a key, or not the address and the switch")
	}
	again := b.do(http.MethodPost, webhookHref, url.Values{"address": {webhookCanaryURL}})
	if again.Code != http.StatusSeeOther || !strings.Contains(again.Header().Get("Location"), "problem=webhook-exists") ||
		webhookKeyRE.MatchString(again.Body.String()) {
		t.Fatalf("a re-posted create: %d %s", again.Code, again.Header().Get("Location"))
	}
	_, sealed0, _ := f.endpointRow(t)
	if r := b.do(http.MethodPost, webhookRotateHref, url.Values{}); !strings.Contains(r.Header().Get("Location"), "problem=webhook-not-confirmed") {
		t.Fatalf("rotate without the tick: %d %s", r.Code, r.Header().Get("Location"))
	}
	if _, sealed, _ := f.endpointRow(t); sealed != sealed0 {
		t.Fatal("an unconfirmed rotation changed the key")
	}
	rot := b.do(http.MethodPost, webhookRotateHref, url.Values{"confirm": {"yes"}})
	keys = webhookKeyRE.FindAllString(rot.Body.String(), -1)
	if rot.Code != http.StatusOK || len(keys) != 1 || keys[0] == first {
		t.Fatalf("rotate: %d, %d keys, new %v", rot.Code, len(keys), len(keys) == 1 && keys[0] != first)
	}
	if webhookKeyRE.MatchString(b.do(http.MethodGet, apiKeysHref, nil).Body.String()) {
		t.Fatal("the section after rotation shows a key")
	}
	f.assertNoLeak(t, first, keys[0])
}

// TestWebhookPanel_ManagerIs403EverywhereAndLeavesATrail: a manager gets 403 at the section and at the
// six writes; each refused write leaves one webhook_endpoint.refused row naming the act and the role;
// nothing is created, changed or sent.
func TestWebhookPanel_ManagerIs403EverywhereAndLeavesATrail(t *testing.T) {
	f := newWebhookPanelFixture(t)
	f.create(t, f.browserAs(t, f.owner, "owner", true))
	url0, sealed0, _ := f.endpointRow(t)
	m := f.browserAs(t, f.manager, "manager", true)
	if rec := m.do(http.MethodGet, apiKeysHref, nil); rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), webhookCanaryHost) {
		t.Fatalf("the manager's section: %d", rec.Code)
	}
	writes := map[string]url.Values{
		webhookHref: {"address": {"https://other.example.com/"}}, webhookAddressHref: {"address": {"https://other.example.com/"}},
		webhookRotateHref: {"confirm": {"yes"}}, webhookDisableHref: {}, webhookEnableHref: {}, webhookPingHref: {},
	}
	for path, form := range writes {
		if rec := m.do(http.MethodPost, path, form); rec.Code != http.StatusForbidden {
			t.Errorf("manager POST %s: %d", path, rec.Code)
		}
	}
	if n := len(f.trail(t, ActionWebhookRefused)); n != len(writes) {
		t.Fatalf("%d refusal rows, want %d", n, len(writes))
	}
	for _, d := range f.trail(t, ActionWebhookRefused) {
		if !strings.Contains(d, `"role": "manager"`) || !strings.Contains(d, `"required_role": "owner"`) {
			t.Fatalf("refusal row: %s", d)
		}
	}
	if url1, sealed1, active := f.endpointRow(t); url1 != url0 || sealed1 != sealed0 || !active || f.sender.count() != 0 {
		t.Fatal("a manager's write changed something or sent a ping")
	}
}

// TestWebhookPanel_CrossSiteWritesAreRefusedBeforeTheResolver is GATE-1 on the six writes: a POST whose
// Origin is another site changes nothing, sends nothing (the ping is the one panel write that makes an
// outbound request) and is answered before the role gate (no refusal row); the same ping from the
// panel's own origin is sent.
func TestWebhookPanel_CrossSiteWritesAreRefusedBeforeTheResolver(t *testing.T) {
	f := newWebhookPanelFixture(t)
	owner := f.browserAs(t, f.owner, "owner", true)
	f.create(t, owner)
	url0, sealed0, _ := f.endpointRow(t)
	evil := f.browserAs(t, f.owner, "owner", true)
	evil.origin = "https://evil.example"
	for path, form := range map[string]url.Values{
		webhookHref: {"address": {"https://other.example.com/"}}, webhookAddressHref: {"address": {"https://other.example.com/"}},
		webhookRotateHref: {"confirm": {"yes"}}, webhookDisableHref: {}, webhookEnableHref: {}, webhookPingHref: {},
	} {
		rec := evil.do(http.MethodPost, path, form)
		// The write chain answers a cross-site POST with a redirect away from the section (never to
		// it with a done/ping/problem word, which only a handler writes).
		loc := rec.Header().Get("Location")
		if rec.Code == http.StatusOK || strings.Contains(loc, apiKeysHref) || webhookKeyRE.MatchString(rec.Body.String()) {
			t.Errorf("cross-site POST %s: %d %s", path, rec.Code, loc)
		}
	}
	if url1, sealed1, active := f.endpointRow(t); url1 != url0 || sealed1 != sealed0 || !active || f.sender.count() != 0 {
		t.Fatal("a cross-site POST changed something or sent a ping")
	}
	if n := len(f.trail(t, ActionWebhookRefused)); n != 0 {
		t.Fatalf("%d refusal rows: a cross-site POST reached the role gate", n)
	}
	if rec := owner.do(http.MethodPost, webhookPingHref, url.Values{}); rec.Code != http.StatusSeeOther || f.sender.count() != 1 {
		t.Fatalf("CONTROL ping from the panel's origin: %d, %d sent", rec.Code, f.sender.count())
	}
}

// TestWebhookPanel_AClosedAPIOffersNothingButTheSwitchOff: with the operator's gate closed the part
// says so and draws no address form, no rotation and no ping -- only "switch off"; a posted create,
// change, rotation, switch-on and ping are refused (api-closed) and change nothing; switching off works
// (K-26). A closed business with no endpoint gets no create form.
func TestWebhookPanel_AClosedAPIOffersNothingButTheSwitchOff(t *testing.T) {
	f := newWebhookPanelFixture(t)
	b := f.browserAs(t, f.owner, "owner", true)
	f.create(t, b)
	f.setAPI(t, false)
	page := b.do(http.MethodGet, apiKeysHref, nil).Body.String()
	if !strings.Contains(page, "cannot be created, changed or tested") {
		t.Fatal("the closed note is missing")
	}
	for _, href := range []string{webhookAddressHref, webhookRotateHref, webhookPingHref, webhookEnableHref} {
		if strings.Contains(page, `action="`+href+`"`) {
			t.Errorf("a closed API draws the form for %s", href)
		}
	}
	if !strings.Contains(page, `action="`+webhookDisableHref+`"`) {
		t.Fatal("a closed API does not offer the switch-off")
	}
	_, sealed0, _ := f.endpointRow(t)
	for path, form := range map[string]url.Values{
		webhookAddressHref: {"address": {"https://other.example.com/"}}, webhookRotateHref: {"confirm": {"yes"}}, webhookPingHref: {},
	} {
		if rec := b.do(http.MethodPost, path, form); !strings.Contains(rec.Header().Get("Location"), "problem=webhook-api-closed") {
			t.Errorf("POST %s on a closed API: %d %s", path, rec.Code, rec.Header().Get("Location"))
		}
	}
	if url1, sealed1, _ := f.endpointRow(t); url1 != webhookCanaryURL || sealed1 != sealed0 || f.sender.count() != 0 {
		t.Fatal("a refused write on a closed API changed something")
	}
	if rec := b.do(http.MethodPost, webhookDisableHref, url.Values{}); !strings.Contains(rec.Header().Get("Location"), "done=webhook-disabled") {
		t.Fatalf("switch off on a closed API: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := b.do(http.MethodPost, webhookEnableHref, url.Values{}); !strings.Contains(rec.Header().Get("Location"), "problem=webhook-api-closed") {
		t.Fatalf("switch on on a closed API: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if _, _, active := f.endpointRow(t); active {
		t.Fatal("the endpoint is on")
	}

	fresh := newWebhookPanelFixture(t)
	fresh.setAPI(t, false)
	fb := fresh.browserAs(t, fresh.owner, "owner", true)
	if strings.Contains(fb.do(http.MethodGet, apiKeysHref, nil).Body.String(), `action="`+webhookHref+`"`) {
		t.Fatal("a closed API without an endpoint draws the create form")
	}
	if rec := fb.do(http.MethodPost, webhookHref, url.Values{"address": {webhookCanaryURL}}); !strings.Contains(rec.Header().Get("Location"), "problem=webhook-api-closed") {
		t.Fatalf("create on a closed API: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	f.assertNoLeak(t)
}

// TestWebhookPanel_PingResultsAndTheBucket: each class the sender can report comes back to the owner
// as its sentence after a redirect (a reload shows it and sends nothing); the eleventh ping of the
// hour is refused with the honest sentence and the time the next slot frees, and nothing is sent; no
// log line or trail row holds the address or a key, for any class.
func TestWebhookPanel_PingResultsAndTheBucket(t *testing.T) {
	f := newWebhookPanelFixture(t)
	b := f.browserAs(t, f.owner, "owner", true)
	key := f.create(t, b)
	cases := []struct {
		res  egress.Result
		says string
	}{
		{egress.Result{Status: 200}, "Your system answered 200. Taptime"},
		{egress.Result{Class: egress.ClassTimeout}, "did not answer within 10 seconds"},
		{egress.Result{Class: egress.ClassTLS}, "secure connection failed"},
		{egress.Result{Class: egress.ClassRefused}, "could not be reached"},
		{egress.Result{Class: egress.ClassHTTP4xx, Status: 404}, "answered 404. Check the address"},
		{egress.Result{Class: egress.ClassHTTP4xx, Status: 302}, "Taptime does not follow redirects"},
		{egress.Result{Class: egress.ClassHTTP5xx, Status: 502}, "answered 502: an error on its side"},
		{egress.Result{Class: egress.ClassBlocked}, "a network Taptime does not send to"},
	}
	for _, c := range cases {
		f.sender.result = c.res
		rec := b.do(http.MethodPost, webhookPingHref, url.Values{})
		loc := rec.Header().Get("Location")
		if rec.Code != http.StatusSeeOther || !strings.Contains(loc, "ping=") {
			t.Fatalf("%+v: %d %s", c.res, rec.Code, loc)
		}
		sent := f.sender.count()
		page := b.do(http.MethodGet, loc, nil).Body.String()
		if !strings.Contains(page, c.says) {
			t.Errorf("%+v: the page does not say %q", c.res, c.says)
		}
		if f.sender.count() != sent {
			t.Fatal("reading the result sent another ping")
		}
	}
	f.sender.result = egress.Result{}
	for f.sender.count() < endpoints.BucketLimit {
		b.do(http.MethodPost, webhookPingHref, url.Values{})
	}
	rec := b.do(http.MethodPost, webhookPingHref, url.Values{})
	if !strings.Contains(rec.Header().Get("Location"), "problem=webhook-bucket-full") || f.sender.count() != endpoints.BucketLimit {
		t.Fatalf("the eleventh ping: %d %s, %d sent", rec.Code, rec.Header().Get("Location"), f.sender.count())
	}
	page := b.do(http.MethodGet, rec.Header().Get("Location"), nil).Body.String()
	if !strings.Contains(page, "used its 10 test messages for this hour") || !strings.Contains(page, "The next one is free at") ||
		strings.Contains(page, `action="`+webhookPingHref+`"`) {
		t.Fatal("a full bucket: the sentence, the next slot or the absent ping button")
	}
	// A forged result query names nothing outside the closed set.
	if page := b.do(http.MethodGet, apiKeysHref+"?ping=%3Cscript%3E&status=200", nil).Body.String(); strings.Contains(page, "<script>") ||
		strings.Contains(page, "The test message") {
		t.Fatal("an unknown class in the query was rendered")
	}
	f.assertNoLeak(t, key)
}

// TestWebhookPanel_AnAddressTheRuleRefusesIsSaidAndNothingIsWritten: an address the rule refuses comes
// back 422 with the owner's own text in the field and the rule's sentence; no endpoint is created and
// no log line or trail row holds the typed address.
func TestWebhookPanel_AnAddressTheRuleRefusesIsSaidAndNothingIsWritten(t *testing.T) {
	f := newWebhookPanelFixture(t)
	b := f.browserAs(t, f.owner, "owner", true)
	for _, c := range []struct{ typed, says string }{
		{"http://" + webhookCanaryHost + "/x", "must start with https://"},
		{"https://" + webhookCanaryHost + ":8443/x", "standard HTTPS port"},
		{"https://10.0.0.1/x", "not an IP address"},
		{"https://" + webhookCanaryHost + "/x?token=1", "query string"},
		{"https://" + webhookCanaryHost + "/x#frag", "fragment"},
		{"https://localhost/x", "public DNS name"},
	} {
		rec := b.do(http.MethodPost, webhookHref, url.Values{"address": {c.typed}})
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), c.says) {
			t.Errorf("%q: %d, says %q: %v", c.typed, rec.Code, c.says, strings.Contains(rec.Body.String(), c.says))
		}
	}
	if u, _, _ := f.endpointRow(t); u != "" {
		t.Fatal("a refused address created an endpoint")
	}
	f.assertNoLeak(t)
}

// TestWebhookPanel_NotConfiguredSaysSo: a panel built without the webhook service (a dev deployment
// without TAPPA_WEBHOOK_KEK) says the webhook is not configured, draws no form, and answers 503 to a
// write; nothing is written.
func TestWebhookPanel_NotConfiguredSaysSo(t *testing.T) {
	f := newWebhookPanelFixture(t)
	b := f.browserAs(t, f.owner, "owner", false)
	page := b.do(http.MethodGet, apiKeysHref, nil).Body.String()
	if !strings.Contains(page, "The webhook is not configured on this deployment.") || strings.Contains(page, `action="`+webhookHref+`"`) {
		t.Fatal("the not-configured part")
	}
	if rec := b.do(http.MethodPost, webhookHref, url.Values{"address": {webhookCanaryURL}}); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("a write without the service: %d", rec.Code)
	}
	if u, _, _ := f.endpointRow(t); u != "" {
		t.Fatal("an endpoint was created")
	}
}
