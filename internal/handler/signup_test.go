package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE WIZARD, DRIVEN OVER REAL HTTP BY A CLIENT THAT VALIDATES NOTHING.
//
// 🔴 THAT IS THE M7-02 CARD'S CRITERION AND IT IS THE SHAPE OF EVERY TEST HERE:
// "sunucu tarafı doğrulama TAM; istemci doğrulaması yalnızca kolaylık". A browser's
// `required`, `minlength` and `type="email"` are removed by anybody who wants to, so
// every case below posts what a hostile client would post — an empty field, a
// skipped step, a forged cookie — against the REAL router with the REAL budgets.
//
// THE PROVISIONER IS A FAKE AND THE HANDLER IS NOT. internal/domain/signup's own
// _db_test.go drives the WRITE against real Postgres; what this file measures is the
// boundary — what reaches the domain, what never does, and what a visitor is told.

func signupTestConfig() *config.Config {
	return &config.Config{
		Env:            config.EnvDev,
		BaseURL:        testBaseURL,
		SessionHMACKey: []byte("0123456789abcdef0123456789abcdef"),
	}
}

// fakeProvisioner records what the boundary handed the domain.
type fakeProvisioner struct {
	mu    sync.Mutex
	calls []signup.Draft
	err   error
	// result is what a successful call returns; the zero value is filled from the
	// draft so a test does not have to state the obvious.
	result *signup.Result
}

func (f *fakeProvisioner) Provision(_ context.Context, d signup.Draft) (signup.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, d)
	if f.err != nil {
		return signup.Result{}, f.err
	}
	if f.result != nil {
		return *f.result, nil
	}
	names := make([]string, 0, len(d.Venues))
	for _, v := range d.Venues {
		names = append(names, v.Name)
	}
	return signup.Result{
		TenantID:     uuid.New(),
		AdminUserID:  uuid.New(),
		BusinessName: d.Business.Name,
		VenueNames:   names,
		VAT:          d.Business.VAT,
	}, nil
}

func (f *fakeProvisioner) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeProvisioner) last(t *testing.T) signup.Draft {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		t.Fatal("the provisioner was never called")
	}
	return f.calls[len(f.calls)-1]
}

// fakeVAT stands in for the VIES checker. It counts calls so a test can assert that
// garbage costs no outbound request.
type fakeVAT struct {
	mu     sync.Mutex
	status signup.VATStatus
	calls  int
}

func (f *fakeVAT) Check(context.Context, string) signup.VATStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.status
}

func (f *fakeVAT) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// wizardDriver walks the wizard the way a browser does: one cookie jar, one 303 per
// step, and the synchronizer token read out of the rendered form.
type wizardDriver struct {
	t      *testing.T
	router http.Handler
	jar    map[string]string
	// clock is what the handler's signed state reads. Tests move it forward to get
	// past the minimum-fill-time gate without sleeping for it.
	clock *time.Time
}

func newSignupDriver(t *testing.T, p signupProvisioner, vat vatChecker) (*wizardDriver, *Signup) {
	t.Helper()
	wizard, err := NewSignup(p, vat, signupTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewSignup: %v", err)
	}
	// THE CLOCK IS INJECTED INTO THE SIGNED STATE, not into the handler. It is the
	// only piece of this flow that reads wall time, and driving it is how a test gets
	// past signupMinFillSeconds in microseconds instead of eight seconds x every case.
	now := time.Now()
	wizard.state.now = func() time.Time { return now }
	d := &wizardDriver{t: t, router: httpx.NewRouter(nil, wizard), jar: map[string]string{}, clock: &now}
	return d, wizard
}

// advance moves the driver's clock, which is what the wizard's state reads.
func (d *wizardDriver) advance(by time.Duration) { *d.clock = d.clock.Add(by) }

func (d *wizardDriver) do(method, path string, form url.Values, decorate func(*http.Request)) *httptest.ResponseRecorder {
	d.t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", testBaseURL)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	for name, value := range d.jar {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	if decorate != nil {
		decorate(req)
	}
	rec := httptest.NewRecorder()
	d.router.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 || c.Value == "" {
			delete(d.jar, c.Name)
			continue
		}
		d.jar[c.Name] = c.Value
	}
	return rec
}

func (d *wizardDriver) get(path string) *httptest.ResponseRecorder {
	return d.do(http.MethodGet, path, nil, nil)
}

func (d *wizardDriver) post(path string, form url.Values) *httptest.ResponseRecorder {
	return d.do(http.MethodPost, path, form, nil)
}

var csrfRE = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

// token reads the synchronizer token out of a rendered form, exactly as a browser
// would submit it.
func (d *wizardDriver) token(rec *httptest.ResponseRecorder) string {
	d.t.Helper()
	m := csrfRE.FindStringSubmatch(rec.Body.String())
	if m == nil {
		d.t.Fatalf("no csrf field in the rendered form (status %d)", rec.Code)
	}
	return m[1]
}

// businessForm is a complete, valid step one.
func businessForm(csrf string) url.Values {
	return url.Values{
		"csrf":          {csrf},
		"name":          {"Kebab Factory Ltd"},
		"vat_number":    {"MT12345678"},
		"business_type": {"restaurant"},
		"structure":     {"multi"},
	}
}

func placesForm(csrf string, venues ...string) url.Values {
	v := url.Values{"csrf": {csrf}}
	for _, n := range venues {
		v.Add("venue", n)
	}
	return v
}

func accountForm(csrf string) url.Values {
	return url.Values{
		"csrf":      {csrf},
		"full_name": {"Maria Borg"},
		"email":     {"maria@kebabfactory.com.mt"},
		"password":  {"correct horse battery staple"},
	}
}

// walkToAccount drives steps one and two and returns the account form's token.
func (d *wizardDriver) walkToAccount(t *testing.T) string {
	t.Helper()
	start := d.get(signupPath)
	if start.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", signupPath, start.Code)
	}
	if rec := d.post(signupPath, businessForm(d.token(start))); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d, want 303\n%s", signupPath, rec.Code, rec.Body.String())
	}
	places := d.get(signupPlacesPath)
	if places.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", signupPlacesPath, places.Code)
	}
	if rec := d.post(signupPlacesPath, placesForm(d.token(places), "St Julians", "Sliema")); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d, want 303\n%s", signupPlacesPath, rec.Code, rec.Body.String())
	}
	account := d.get(signupAccountPath)
	if account.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", signupAccountPath, account.Code)
	}
	return d.token(account)
}

// ------------------------------------------------------------- the happy path --

// TestSignup_TheWholeWizardReachesAnApprovedStamp.
//
// 🔴 THIS IS THE TEST THAT SAYS THE PRODUCT'S BIGGEST GAP IS CLOSED. Before M7-02
// nobody could register at all; this walks a stranger from / to a created business.
func TestSignup_TheWholeWizardReachesAnApprovedStamp(t *testing.T) {
	t.Parallel()
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, &fakeVAT{status: signup.VATValid})

	csrf := d.walkToAccount(t)
	d.advance(time.Minute) // a person filling a form in
	rec := d.post(signupAccountPath, accountForm(csrf))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != signupDonePath {
		t.Fatalf("POST %s = %d -> %q, want 303 -> %s\n%s",
			signupAccountPath, rec.Code, rec.Header().Get("Location"), signupDonePath, rec.Body.String())
	}
	if p.count() != 1 {
		t.Fatalf("the provisioner was called %d time(s), want 1", p.count())
	}

	got := p.last(t)
	if got.Business.Name != "Kebab Factory Ltd" || got.Business.VATNumber != "MT12345678" {
		t.Errorf("the business reached the domain as %+v", got.Business)
	}
	if got.Business.Structure != signup.StructureMulti || got.Business.BusinessType != "restaurant" {
		t.Errorf("structure/type reached the domain as %q/%q", got.Business.Structure, got.Business.BusinessType)
	}
	if len(got.Venues) != 2 || got.Venues[0].Name != "St Julians" || got.Venues[1].Name != "Sliema" {
		t.Errorf("the venues reached the domain as %+v, in the wrong order or count", got.Venues)
	}
	if got.Account.Email != "maria@kebabfactory.com.mt" || got.Account.Password == "" {
		t.Errorf("the account reached the domain without its email or its password")
	}

	done := d.get(signupDonePath)
	if done.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", signupDonePath, done.Code)
	}
	body := done.Body.String()
	if !strings.Contains(body, `class="stamp stamp--approved"`) || !strings.Contains(body, "APPROVED") {
		t.Error("the confirmation carries no APPROVED stamp; the card asks for one and it is " +
			"true only because the business now exists")
	}
	text := screenText(t, body)
	for _, want := range []string{"Kebab Factory Ltd", "MT12345678", "St Julians", "Sliema"} {
		if !strings.Contains(text, want) {
			t.Errorf("the confirmation does not name %q", want)
		}
	}
	// THE CONFIRMATION IS SPENT. A reload must not print a second APPROVED stamp over
	// a business that was created once.
	if again := d.get(signupDonePath); again.Code == http.StatusOK &&
		strings.Contains(again.Body.String(), "stamp--approved") {
		t.Error("the confirmation can be re-read after it was shown, so a shared browser keeps " +
			"a finished registration lying around")
	}
}

// TestSignup_SingleStructureGetsExactlyOnePlace — the card's "`structure`
// (`single|multi`) seçimi lokasyon/departman modelini belirliyor", at the boundary.
func TestSignup_SingleStructureGetsExactlyOnePlace(t *testing.T) {
	t.Parallel()
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)

	start := d.get(signupPath)
	form := businessForm(d.token(start))
	form.Set("structure", "single")
	if rec := d.post(signupPath, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d", signupPath, rec.Code)
	}
	places := d.get(signupPlacesPath)
	// The form itself offers one row and no way to add another.
	if strings.Count(places.Body.String(), `name="venue"`) != 1 {
		t.Errorf("a single-place business is offered %d venue rows, want 1",
			strings.Count(places.Body.String(), `name="venue"`))
	}
	// And the SERVER refuses two, whatever the form offered.
	rec := d.post(signupPlacesPath, placesForm(d.token(places), "Front door", "Back door"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST two venues for a single-place business = %d, want 422", rec.Code)
	}
	if p.count() != 0 {
		t.Error("the provisioner was reached")
	}
}

// -------------------------------------------------------- server-side rules --

// TestSignup_ServerRefusesWhatTheBrowserWouldHave.
//
// Every case posts a form a browser would have blocked. The server must refuse each
// one on its own, and the provisioner must never be reached.
func TestSignup_ServerRefusesWhatTheBrowserWouldHave(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		mut   func(url.Values)
		field string
	}{
		{"no business name", func(v url.Values) { v.Set("name", "") }, "Tell us the business name"},
		{"no VAT number", func(v url.Values) { v.Set("vat_number", "") }, "VAT number is required"},
		{"a VAT number from nowhere", func(v url.Values) { v.Set("vat_number", "ZZ1") }, "EU VAT number"},
		{"a business type the schema refuses", func(v url.Values) { v.Set("business_type", "nightclub") }, "Pick the closest match"},
		{"no structure", func(v url.Values) { v.Del("structure") }, "Choose one place or several"},
		{"a structure the schema refuses", func(v url.Values) { v.Set("structure", "franchise") }, "Choose one place or several"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &fakeProvisioner{}
			vat := &fakeVAT{status: signup.VATValid}
			d, _ := newSignupDriver(t, p, vat)
			start := d.get(signupPath)
			form := businessForm(d.token(start))
			tc.mut(form)
			rec := d.post(signupPath, form)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422", rec.Code)
			}
			if !strings.Contains(screenText(t, rec.Body.String()), tc.field) {
				t.Errorf("the re-rendered form does not carry the message about %q", tc.name)
			}
			if p.count() != 0 {
				t.Error("the provisioner was reached by an invalid form")
			}
			// GARBAGE COSTS NO OUTBOUND REQUEST. The VIES call happens only after the
			// FORMAT check passes, which is what stops POST /signup being a request
			// amplifier for anything a caller types.
			if strings.Contains(tc.name, "VAT") && vat.count() != 0 {
				t.Errorf("%d outbound VAT lookup(s) were made for a number that failed the "+
					"format check", vat.count())
			}
		})
	}
}

// TestSignup_ServerRefusesABadAccountAndKeepsThePasswordOffTheScreen.
func TestSignup_ServerRefusesABadAccountAndKeepsThePasswordOffTheScreen(t *testing.T) {
	t.Parallel()
	const secret = "zzqqxx"
	tests := []struct {
		name string
		mut  func(url.Values)
		says string
	}{
		{"no name", func(v url.Values) { v.Set("full_name", "") }, "Tell us your name"},
		{"no email", func(v url.Values) { v.Set("email", "") }, "We need an address"},
		{"an email with no @", func(v url.Values) { v.Set("email", "maria.example.mt") }, "does not look like an email"},
		{"a short password", func(v url.Values) { v.Set("password", secret) }, "at least 12 characters"},
		{"no password", func(v url.Values) { v.Set("password", "") }, "Choose a password"},
		{"a password past bcrypt's byte limit", func(v url.Values) {
			v.Set("password", strings.Repeat("q", 73))
		}, "under 72 bytes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &fakeProvisioner{}
			d, _ := newSignupDriver(t, p, nil)
			csrf := d.walkToAccount(t)
			d.advance(time.Minute)
			form := accountForm(csrf)
			tc.mut(form)
			rec := d.post(signupAccountPath, form)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422", rec.Code)
			}
			if !strings.Contains(screenText(t, rec.Body.String()), tc.says) {
				t.Errorf("the re-rendered form does not say %q", tc.says)
			}
			if p.count() != 0 {
				t.Error("the provisioner was reached by an invalid account form")
			}
			// 🔴 THE PASSWORD IS NEVER ECHOED. A refused submission comes back with the
			// field empty; a password in a body is a password in the back/forward cache
			// and in "save page".
			body := rec.Body.String()
			for _, pw := range []string{form.Get("password"), accountForm("").Get("password")} {
				if pw != "" && strings.Contains(body, pw) {
					t.Errorf("the refused form echoed the password back into the body")
				}
			}
			if strings.Contains(body, `name="password"`) && strings.Contains(body, `name="password" `) {
				// A `value=` on the password input would be the mechanism; assert the
				// input carries none.
				if regexp.MustCompile(`name="password"[^>]*value=`).MatchString(body) {
					t.Error("the password input carries a value attribute")
				}
			}
		})
	}
}

// ------------------------------------------------------------ the sequence --

// TestSignup_TheStepsCannotBeSkipped.
//
// 🔴 THE FINAL POST IS THE ONE THAT RUNS bcrypt AND WRITES ROWS. Reaching it without
// the two earlier steps would mean registering a business with no venues — and it is
// also a third of this flow's bot defence (signupratelimit.go).
func TestSignup_TheStepsCannotBeSkipped(t *testing.T) {
	t.Parallel()
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)

	// Cold, with no state at all.
	for _, path := range []string{signupPlacesPath, signupAccountPath, signupDonePath} {
		if rec := d.get(path); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s with no wizard state = %d, want 400", path, rec.Code)
		}
	}

	// With step one done, the account form is still out of reach.
	start := d.get(signupPath)
	if rec := d.post(signupPath, businessForm(d.token(start))); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d", signupPath, rec.Code)
	}
	if rec := d.get(signupAccountPath); rec.Code != http.StatusBadRequest {
		t.Errorf("GET %s after step one = %d, want 400", signupAccountPath, rec.Code)
	}
	// And so is POSTING it, which is the half that matters.
	places := d.get(signupPlacesPath)
	if rec := d.post(signupAccountPath, accountForm(d.token(places))); rec.Code != http.StatusBadRequest {
		t.Errorf("POST %s after step one = %d, want 400", signupAccountPath, rec.Code)
	}
	if p.count() != 0 {
		t.Fatal("a skipped step reached the provisioner")
	}
}

// TestSignup_AForgedStateIsRefused — the signature, and the browser binding.
func TestSignup_AForgedStateIsRefused(t *testing.T) {
	t.Parallel()
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)
	d.walkToAccount(t)

	good := d.jar[signupStateCookieName]
	if good == "" {
		t.Fatal("no wizard state cookie was set")
	}

	t.Run("a tampered payload", func(t *testing.T) {
		payload, mac, _ := strings.Cut(good, ".")
		// Flip one character of the base64 payload. The MAC no longer matches.
		mutated := "x" + payload[1:]
		d2, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
		d2.jar[signupCookieName] = d.jar[signupCookieName]
		d2.jar[signupStateCookieName] = mutated + "." + mac
		if rec := d2.get(signupAccountPath); rec.Code != http.StatusBadRequest {
			t.Errorf("a tampered state answered %d, want 400", rec.Code)
		}
	})

	t.Run("a state lifted into another browser", func(t *testing.T) {
		// 🔴 THE BINDING. The MAC covers the `bind` half of the synchronizer cookie,
		// which never enters a page. A state blob copied out of one browser does not
		// verify in another that has its own synchronizer cookie.
		other, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
		other.get(signupPath) // mints ITS OWN synchronizer pair
		other.jar[signupStateCookieName] = good
		if rec := other.get(signupAccountPath); rec.Code != http.StatusBadRequest {
			t.Errorf("a state blob replayed in another browser answered %d, want 400", rec.Code)
		}
	})

	t.Run("an expired state", func(t *testing.T) {
		d3, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
		d3.walkToAccount(t)
		d3.advance(signupTTL + time.Minute)
		if rec := d3.get(signupAccountPath); rec.Code != http.StatusBadRequest {
			t.Errorf("a state past its TTL answered %d, want 400", rec.Code)
		}
	})
}

// TestSignup_TheVIESVerdictCannotBeChosenByTheCaller.
//
// 🔴 THE WHOLE VALUE OF THE CHECK IS THAT THE ANSWER IS NOT THE CALLER'S. Without
// this, anybody could register with a VAT number stamped "confirmed by the European
// Commission" that the Commission never saw — and tenants.vat_verified is a fact the
// panel will show a customer.
func TestSignup_TheVIESVerdictCannotBeChosenByTheCaller(t *testing.T) {
	t.Parallel()
	p := &fakeProvisioner{}
	// The service says the number is NOT valid.
	d, _ := newSignupDriver(t, p, &fakeVAT{status: signup.VATInvalid})

	start := d.get(signupPath)
	form := businessForm(d.token(start))
	// A caller trying to overrule it by posting the field itself.
	form.Set("vat_check", "valid")
	form.Set("vc", "valid")
	form.Set("VATCheck", "valid")
	if rec := d.post(signupPath, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d", signupPath, rec.Code)
	}
	places := d.get(signupPlacesPath)
	if rec := d.post(signupPlacesPath, placesForm(d.token(places), "Front door")); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d", signupPlacesPath, rec.Code)
	}
	account := d.get(signupAccountPath)
	d.advance(time.Minute)
	if rec := d.post(signupAccountPath, accountForm(d.token(account))); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d\n%s", signupAccountPath, rec.Code, rec.Body.String())
	}
	if got := p.last(t).Business.VAT; got != signup.VATInvalid {
		t.Errorf("the VAT verdict reached the domain as %v; the caller's own fields must not "+
			"be able to move it off what VIES said", got)
	}
}

// TestSignup_AVIESOutageDoesNotStopTheRegistration — Q09's criterion, end to end.
func TestSignup_AVIESOutageDoesNotStopTheRegistration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		vat  vatChecker
		want signup.VATStatus
	}{
		{"no checker is wired at all", nil, signup.VATUnknown},
		{"the service answered nothing", &fakeVAT{status: signup.VATUnknown}, signup.VATUnknown},
		{"the service says the number is not valid", &fakeVAT{status: signup.VATInvalid}, signup.VATInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &fakeProvisioner{}
			d, _ := newSignupDriver(t, p, tc.vat)
			csrf := d.walkToAccount(t)
			d.advance(time.Minute)
			if rec := d.post(signupAccountPath, accountForm(csrf)); rec.Code != http.StatusSeeOther {
				t.Fatalf("the registration was stopped: %d\n%s", rec.Code, rec.Body.String())
			}
			if got := p.last(t).Business.VAT; got != tc.want {
				t.Errorf("the recorded verdict is %v, want %v", got, tc.want)
			}
			// And the confirmation says which of the three it was, in words.
			done := d.get(signupDonePath)
			text := screenText(t, done.Body.String())
			switch tc.want {
			case signup.VATInvalid:
				if !strings.Contains(text, "did not recognise this number") {
					t.Error("the confirmation does not say the register refused the number")
				}
			default:
				if !strings.Contains(text, "could not reach the EU VAT register") {
					t.Error("the confirmation does not say the check did not happen; silence " +
						"would make an unchecked number look like a confirmed one")
				}
			}
		})
	}
}

// ------------------------------------------------------------- bot defence --

// TestSignup_TheHoneypotRefusesAndTheScreenDoesNotSayWhy.
func TestSignup_TheHoneypotRefusesAndTheScreenDoesNotSayWhy(t *testing.T) {
	t.Parallel()
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)
	csrf := d.walkToAccount(t)
	d.advance(time.Minute)

	form := accountForm(csrf)
	form.Set(signupHoneypotField, "https://example.test")
	rec := d.post(signupAccountPath, form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a filled honeypot answered %d, want 400", rec.Code)
	}
	if p.count() != 0 {
		t.Error("a filled honeypot reached the provisioner")
	}
	// Telling an automated caller which mechanism caught it is telling whoever wrote
	// it what to change.
	text := strings.ToLower(screenText(t, rec.Body.String()))
	for _, word := range []string{"honeypot", "bot", "automated", "robot"} {
		if strings.Contains(text, word) {
			t.Errorf("the refusal screen names the mechanism (%q)", word)
		}
	}
}

// TestSignup_AFormFilledFasterThanAPersonCanTypeIsRefused.
func TestSignup_AFormFilledFasterThanAPersonCanTypeIsRefused(t *testing.T) {
	t.Parallel()
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)
	csrf := d.walkToAccount(t)
	// The clock has NOT moved: the whole wizard was walked instantly.
	if rec := d.post(signupAccountPath, accountForm(csrf)); rec.Code != http.StatusBadRequest {
		t.Fatalf("an instant submission answered %d, want 400", rec.Code)
	}
	if p.count() != 0 {
		t.Fatal("an instant submission reached the provisioner")
	}

	// One second past the gate it is accepted, so the gate is a MINIMUM and not a
	// blanket refusal.
	d2, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
	csrf2 := d2.walkToAccount(t)
	d2.advance(time.Duration(signupMinFillSeconds+1) * time.Second)
	if rec := d2.post(signupAccountPath, accountForm(csrf2)); rec.Code != http.StatusSeeOther {
		t.Errorf("a submission just past the minimum fill time answered %d, want 303\n%s",
			rec.Code, rec.Body.String())
	}
}

// TestSignup_TheHoneypotFieldIsOnTheFormAndHidden. A defence whose field is not
// rendered is inert, and one a person can see is a defence that refuses customers.
func TestSignup_TheHoneypotFieldIsOnTheFormAndHidden(t *testing.T) {
	t.Parallel()
	d, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
	d.walkToAccount(t)
	body := d.get(signupAccountPath).Body.String()
	if !strings.Contains(body, `name="`+signupHoneypotField+`"`) {
		t.Fatalf("the account form carries no %q input, so the check in the handler can "+
			"never fire", signupHoneypotField)
	}
	if !strings.Contains(body, `aria-hidden="true"`) || !strings.Contains(body, `tabindex="-1"`) {
		t.Error("the honeypot is not hidden from assistive technology and from the tab order, " +
			"so a real person can reach it")
	}
	// The name must not advertise itself.
	for _, giveaway := range []string{"leave_this_empty", "honeypot", "do_not_fill"} {
		if strings.Contains(body, giveaway) {
			t.Errorf("the honeypot is named %q, which anything worth catching skips", giveaway)
		}
	}

	// 🔴 AND IT MUST NOT LOOK LIKE A FIELD A PASSWORD MANAGER FILLS. The field was
	// called "company_website" for one round; several identity autofills carry a
	// WEBSITE / URL field, and an autofill that filled this input would make the
	// wizard refuse a GENUINE registration at its last step. A false positive here
	// costs a paying customer; a false negative costs one spam tenant.
	//
	// ⚠️ THIS IS A RISK REDUCTION AND NOT A MEASUREMENT. No password manager can be
	// driven from here, so nothing proves the negative. What IS asserted is the part
	// that can be: the field name and its label carry none of the tokens a published
	// autofill taxonomy keys on, and the two opt-out attributes that are actually
	// honoured are present.
	// 🔴 THE ASSERTION IS OVER THE RENDERED BODY, NOT THE CONSTANT, AND `company` IS IN
	// THE LIST. The first version walked signupHoneypotField alone and omitted
	// `company` — so it passed while the page carried "Company reference" and
	// id="signup-company-reference", which is exactly what Chromium's COMPANY_NAME
	// heuristics match on. A constant cannot tell you what the page says.
	//
	// The scan is restricted to the honeypot's own markup: `name` and `email` are
	// legitimate tokens elsewhere on this form (the operator's real fields), so a
	// whole-page scan would be permanently red.
	openTag := body
	if i := strings.Index(openTag, `for="signup-internal-ref"`); i >= 0 {
		openTag = openTag[i:]
		if j := strings.Index(openTag, "</div>"); j >= 0 {
			openTag = openTag[:j]
		}
	} else {
		t.Fatal("the honeypot's label/for pair is not signup-internal-ref, so this scan is " +
			"reading the wrong element")
	}
	for _, token := range []string{"website", "url", "company", "phone", "address"} {
		if strings.Contains(strings.ToLower(openTag), token) {
			t.Errorf("the honeypot's markup carries the autofill token %q:\n%s\n"+
				"Browser heuristics key on the label and the id as well as the name, and a "+
				"profile autofill that fills this input refuses a paying customer at the last "+
				"step with a screen that deliberately will not say why.", token, openTag)
		}
	}
	if strings.Contains(signupHoneypotField, "company") {
		t.Errorf("the honeypot field is named %q", signupHoneypotField)
	}
	for _, optOut := range []string{`data-1p-ignore`, `data-lpignore`, `autocomplete="off"`} {
		if !strings.Contains(body, optOut) {
			t.Errorf("the honeypot input carries no %s, so a password manager is not told to "+
				"leave it alone", optOut)
		}
	}
}

// ------------------------------------------------------- CSRF, Origin, budgets --

func TestSignup_EveryPostNeedsTheSynchronizerTokenAndTheRightOrigin(t *testing.T) {
	t.Parallel()
	for _, path := range []string{signupPath, signupPlacesPath, signupAccountPath} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			p := &fakeProvisioner{}
			d, _ := newSignupDriver(t, p, nil)
			csrf := d.walkToAccount(t)
			d.advance(time.Minute)

			form := url.Values{"csrf": {csrf}, "name": {"X Ltd"}, "vat_number": {"MT12345678"},
				"business_type": {"bar"}, "structure": {"single"}, "venue": {"Door"},
				"full_name": {"M"}, "email": {"m@example.mt"}, "password": {"a passphrase here"}}

			t.Run("no token", func(t *testing.T) {
				bad := url.Values{}
				for k, v := range form {
					bad[k] = v
				}
				bad.Del("csrf")
				if rec := d.post(path, bad); rec.Code != http.StatusBadRequest {
					t.Errorf("a POST with no synchronizer token answered %d, want 400", rec.Code)
				}
			})
			t.Run("a wrong token", func(t *testing.T) {
				bad := url.Values{}
				for k, v := range form {
					bad[k] = v
				}
				bad.Set("csrf", "not-the-token")
				if rec := d.post(path, bad); rec.Code != http.StatusBadRequest {
					t.Errorf("a POST with a wrong token answered %d, want 400", rec.Code)
				}
			})
			t.Run("another origin", func(t *testing.T) {
				rec := d.do(http.MethodPost, path, form, func(r *http.Request) {
					r.Header.Set("Origin", "https://evil.example")
				})
				if rec.Code != http.StatusBadRequest {
					t.Errorf("a cross-origin POST answered %d, want 400", rec.Code)
				}
			})
			t.Run("no origin and no fetch metadata", func(t *testing.T) {
				rec := d.do(http.MethodPost, path, form, func(r *http.Request) {
					r.Header.Del("Origin")
				})
				if rec.Code != http.StatusBadRequest {
					t.Errorf("a POST with no Origin answered %d, want 400", rec.Code)
				}
			})
			if p.count() != 0 {
				t.Error("one of the refused POSTs reached the provisioner")
			}
		})
	}
}

// TestSignup_TheCreateBudgetBoundsHowManyBusinessesOneAddressMakes.
//
// 🔴 THIS IS THE BUDGET MIGRATION 00017 IS ABOUT. `tenants` rows are the fuel for
// the candidate-stuffing attack that file measures, and this is what makes assembling
// them cost time rather than nothing.
func TestSignup_TheCreateBudgetBoundsHowManyBusinessesOneAddressMakes(t *testing.T) {
	t.Parallel()
	p := &fakeProvisioner{}
	// ONE handler across every registration, so the budgets are the real shared ones
	// rather than a fresh limiter per attempt.
	wizard, err := NewSignup(p, nil, signupTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewSignup: %v", err)
	}
	now := time.Now()
	wizard.state.now = func() time.Time { return now }
	router := httpx.NewRouter(nil, wizard)

	register := func() int {
		d := &wizardDriver{t: t, router: router, jar: map[string]string{}, clock: &now}
		csrf := d.walkToAccount(t)
		now = now.Add(time.Minute)
		return d.post(signupAccountPath, accountForm(csrf)).Code
	}
	for i := 0; i < signupCreateLimit; i++ {
		if code := register(); code != http.StatusSeeOther {
			t.Fatalf("registration %d answered %d, want 303", i+1, code)
		}
	}
	if code := register(); code != http.StatusTooManyRequests {
		t.Errorf("registration %d answered %d, want 429 — the create budget is %d per %s",
			signupCreateLimit+1, code, signupCreateLimit, signupCreatePeriod)
	}
	if p.count() != signupCreateLimit {
		t.Errorf("the provisioner was called %d time(s); the budget must refuse BEFORE the "+
			"write, not after it", p.count())
	}
}

// TestSignup_TheAttemptBudgetBoundsTheExpensiveRequest. It meters the POST that runs
// bcrypt, so a caller cannot spend CPU by submitting a form that fails.
func TestSignup_TheAttemptBudgetBoundsTheExpensiveRequest(t *testing.T) {
	t.Parallel()
	p := &fakeProvisioner{}
	wizard, err := NewSignup(p, nil, signupTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewSignup: %v", err)
	}
	now := time.Now()
	wizard.state.now = func() time.Time { return now }
	router := httpx.NewRouter(nil, wizard)

	d := &wizardDriver{t: t, router: router, jar: map[string]string{}, clock: &now}
	csrf := d.walkToAccount(t)
	now = now.Add(time.Minute)

	// Each attempt is refused by the honeypot, which charges the attempt budget: a
	// refusal that cost nothing would be a cheaper way to reach what the budget
	// protects (ratelimit.go's rule).
	form := accountForm(csrf)
	form.Set(signupHoneypotField, "x")
	seen := map[int]int{}
	for i := 0; i < signupAttemptLimit+2; i++ {
		seen[d.post(signupAccountPath, form).Code]++
	}
	if seen[http.StatusTooManyRequests] == 0 {
		t.Errorf("no attempt was refused after %d submissions; codes seen: %v",
			signupAttemptLimit+2, seen)
	}
}

// ------------------------------------------------------------ headers, shape --

// TestSignup_EveryScreenIsUncacheableAndCarriesThePolicy.
//
// 🔴 no-store IS THE OPPOSITE OF THE MARKETING SURFACE NEXT DOOR AND IS THE POINT.
// handler.Marketing sends `public, max-age=300` because its bodies are identical for
// every visitor; these carry a synchronizer token, a company name and, on a
// re-rendered form, somebody's own email address.
func TestSignup_EveryScreenIsUncacheableAndCarriesThePolicy(t *testing.T) {
	t.Parallel()
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)

	var recs []*httptest.ResponseRecorder
	recs = append(recs, d.get(signupPath))
	start := recs[0]
	recs = append(recs, d.post(signupPath, businessForm(d.token(start))))
	recs = append(recs, d.get(signupPlacesPath))
	places := recs[len(recs)-1]
	recs = append(recs, d.post(signupPlacesPath, placesForm(d.token(places), "Front door")))
	recs = append(recs, d.get(signupAccountPath))
	account := recs[len(recs)-1]
	d.advance(time.Minute)
	recs = append(recs, d.post(signupAccountPath, accountForm(d.token(account))))
	recs = append(recs, d.get(signupDonePath))
	// A refusal screen too.
	recs = append(recs, d.get(signupAccountPath))

	if len(recs) < 8 {
		t.Fatalf("drove %d responses; this scan is not covering the flow", len(recs))
	}
	for i, rec := range recs {
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("response %d sends Cache-Control: %q, want no-store", i, got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("response %d sends X-Content-Type-Options: %q", i, got)
		}
		if rec.Code == http.StatusSeeOther {
			continue // a redirect carries no body and no policy
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != signupCSP {
			t.Errorf("response %d sends a different policy: %q", i, got)
		}
	}
}

// TestSignupCSP_NamesWhatThePageLoads. Directive by directive rather than by
// comparing the string with marketingCSP or adminCSP — the three are free to diverge
// and this must not have to be edited to permit it.
func TestSignupCSP_NamesWhatThePageLoads(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"default-src 'none'",
		"style-src 'self'",
		"font-src 'self'",
		// The one with a concrete threat behind it here: a public form carrying a
		// PASSWORD, and form-action does not fall back to default-src.
		"form-action 'self'",
		"base-uri 'none'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(signupCSP, want) {
			t.Errorf("the signup policy does not name %q", want)
		}
	}
	for _, absent := range []string{"script-src", "img-src", "unsafe-inline", "unsafe-eval"} {
		if strings.Contains(signupCSP, absent) {
			t.Errorf("the signup policy names %q; the wizard loads no script and no image, so "+
				"naming one makes adding it a silent inheritance", absent)
		}
	}
}

// TestSignup_IsNeverIndexed. The landing page asks to be found; a step of a form
// flow must not be.
func TestSignup_IsNeverIndexed(t *testing.T) {
	t.Parallel()
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)

	// The screens are collected AS THEY ARE SERVED, in order, so this reads the real
	// forms rather than the "start again" page a cold GET would produce. An earlier
	// version walked the paths afterwards and measured four copies of the problem
	// screen.
	screens := map[string]string{}
	start := d.get(signupPath)
	screens[signupPath] = start.Body.String()
	d.post(signupPath, businessForm(d.token(start)))
	places := d.get(signupPlacesPath)
	screens[signupPlacesPath] = places.Body.String()
	d.post(signupPlacesPath, placesForm(d.token(places), "Front door"))
	account := d.get(signupAccountPath)
	screens[signupAccountPath] = account.Body.String()
	d.advance(time.Minute)
	d.post(signupAccountPath, accountForm(d.token(account)))
	screens[signupDonePath] = d.get(signupDonePath).Body.String()

	for path, body := range screens {
		if !strings.Contains(body, `name="robots" content="noindex, nofollow"`) {
			t.Errorf("%s does not ask to be left out of search indexes", path)
		}
		// ANTI-VACUITY: the collected screen must be the real one. A problem page
		// carries no form, so this distinguishes them.
		if path != signupDonePath && !strings.Contains(body, `name="csrf"`) {
			t.Errorf("%s was collected as a screen with no form on it, so this test is "+
				"measuring the wrong page", path)
		}
	}
	if !strings.Contains(screens[signupDonePath], "stamp--approved") {
		t.Error("the confirmation screen was not collected")
	}
}

// TestSignup_MountsExactlyTheRoutesTheLandingPageOffers. The "Start free" button
// points at signupPath; a route table that drifted from it would be a 404 on the most
// public URL in the product.
func TestSignup_MountsExactlyTheRoutesTheLandingPageOffers(t *testing.T) {
	t.Parallel()
	d, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
	if rec := d.get(signupPath); rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d; this is where the landing page's button points", signupPath, rec.Code)
	}
	if NewMarketing(nil, nil).signupHref != signupPath {
		t.Errorf("handler.Marketing points at %q and the wizard is mounted at %q",
			NewMarketing(nil, nil).signupHref, signupPath)
	}
}

// TestSignup_HoldsNoSessionCodec — the shape of the security argument on the type.
//
// The wizard is the one unauthenticated surface in this product that WRITES, and its
// argument is partly that it cannot sign anybody in: there is no field through which
// it could reach a session manager, a token minter or the panel's cookie codec. A
// dependency here has to arrive with its own argument.
func TestSignup_HoldsNoSessionCodec(t *testing.T) {
	t.Parallel()
	wizard, err := NewSignup(&fakeProvisioner{}, nil, signupTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewSignup: %v", err)
	}
	// Driven rather than reflected: a completed registration must not set the panel
	// session cookie.
	d := &wizardDriver{t: t, router: httpx.NewRouter(nil, wizard), jar: map[string]string{}}
	now := time.Now()
	wizard.state.now = func() time.Time { return now }
	d.clock = &now
	csrf := d.walkToAccount(t)
	now = now.Add(time.Minute)
	rec := d.post(signupAccountPath, accountForm(csrf))
	for _, c := range rec.Result().Cookies() {
		if !strings.HasPrefix(c.Name, "tappa_signup") {
			t.Errorf("a completed registration set the cookie %q; the wizard ends at the "+
				"sign-in page and issues no session", c.Name)
		}
	}
	done := d.get(signupDonePath)
	if !strings.Contains(done.Body.String(), adminLoginPath) {
		t.Error("the confirmation does not link to the sign-in page, so a customer who just " +
			"registered has nowhere to go")
	}
}

// TestSignupDone_PromisesNoPanelSurfaceForTheVATCheck.
//
// 🔴 THIS IS M7-01's BLOCKING FINDING, TURNED INTO A TRIPWIRE, BECAUSE IT REPEATED
// ONE TASK LATER. The confirmation used to say "check the number on your dashboard"
// and "it will show as unverified on your dashboard" — under the APPROVED stamp, on
// the screen that closes the sale — while NO PANEL SCREEN READS EITHER VAT COLUMN.
//
// THE CONSTRAINT IS DERIVED, NOT A HAND LIST. It asks the GENERATED sqlc code
// whether any query READS tenants.vat_verified / vat_checked_at (an INSERT parameter
// is not a read). While none does, the confirmation may not point at a panel; the
// day M7-05 adds one, this test stops constraining the sentence on its own. That is
// the anchorDerivations discipline M7-01's card demanded of exactly this task.
func TestSignupDone_PromisesNoPanelSurfaceForTheVATCheck(t *testing.T) {
	t.Parallel()
	// Every generated *Row struct in internal/store, i.e. everything a query RETURNS.
	dir := filepath.Join("..", "..", "internal", "store")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading internal/store: %v", err)
	}
	rowType := regexp.MustCompile(`(?s)type \w+Row struct \{(.*?)\n\}`)
	reads := 0
	files := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql.go") {
			continue
		}
		files++
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		for _, m := range rowType.FindAllStringSubmatch(string(raw), -1) {
			if strings.Contains(m[1], "VatVerified") || strings.Contains(m[1], "VatCheckedAt") {
				reads++
			}
		}
	}
	// ANTI-VACUITY: a scan that read no file would report "nothing reads it" about a
	// directory it never opened.
	if files < 5 {
		t.Fatalf("scanned %d generated query file(s); internal/store has more, so this "+
			"derivation is reading the wrong directory", files)
	}

	var body strings.Builder
	for _, v := range []pages.SignupDoneView{
		{VATVerified: true}, {VATRefused: true}, {},
	} {
		body.WriteString(v.VATNote())
		body.WriteString("\n")
	}
	text := strings.ToLower(body.String())
	// CreateTenant's own RETURNING row carries the columns — that is a WRITE echoing
	// what it just wrote, not a screen reading them — so the floor is one.
	if reads <= 1 {
		for _, promise := range []string{"dashboard", "panel", "your account page", "settings"} {
			if strings.Contains(text, promise) {
				t.Errorf("the confirmation's VAT note points at %q, and no query in "+
					"internal/store READS tenants.vat_verified or vat_checked_at "+
					"(%d row type(s) carry them, and CreateTenant's RETURNING is one). "+
					"That is a screen promising a capability the product does not have — "+
					"M7-01's blocking finding, under the APPROVED stamp.", promise, reads)
			}
		}
		return
	}
	t.Logf("%d generated row type(s) now read a VAT column, so a panel surface exists "+
		"and the confirmation may name it; this test no longer constrains the wording.", reads)
}

// ============================================================================
// THE CLAIM SCANNER — the ROOT CAUSE, addressed once rather than three times.
// ============================================================================
//
// 🔴 THE SAME DEFECT HAS NOW BEEN A BLOCKING FINDING THREE TIMES IN TWO TASKS: a
// SENTENCE DECLARING A GUARANTEE THE PRODUCT DOES NOT PROVIDE. M7-01 shipped
// "Reports and the monthly headcount split by department" for a capability that did
// not exist; M7-02 round 2 shipped "check the number on your dashboard" for a panel
// screen that does not read the column; M7-02 round 4 shipped a migration and an ADR
// defending an ordering trade with "the customer finds out immediately, on our page"
// while the product told them nothing at all.
//
// M7-01's answer was pages.Anchor + anchorDerivations — every sentence in one block
// had to NAME a fact derived from the schema. Its own card records the limit: an
// anchor is "a ratchet against drift, not a proof of truth", because it cannot see a
// sentence that was never true or one pinned to an unrelated anchor.
//
// THIS GOES AT THE PROBLEM FROM THE OTHER END. Instead of asking each sentence to
// name an anchor, it takes a VOCABULARY OF CAPABILITY WORDS — the words with which a
// screen promises the reader that something exists — and requires that any of them
// appearing anywhere on this surface be listed below WITH A DERIVATION THAT CURRENTLY
// HOLDS. A new promise is then a compile-free, silent edit that turns this red.
//
// THE COMMAND, so this is reproducible outside the suite:
//
//	go test ./internal/handler/ -run 'TestSignupSurface_' -v
//
// ⚠️ WHAT IT STILL CANNOT DO, stated because M7-01's version had to state it too: it
// checks CAPABILITY words, not truth. A false sentence built out of words not in the
// vocabulary passes, and so does a claim about the outside world. It narrows the class
// that has actually bitten three times; it does not make the surface honest by itself.

// signupClaim is one capability word this surface is allowed to use, and the fact
// that makes it true.
type signupClaim struct {
	// word is matched case-insensitively against the rendered text.
	word string
	// why is what the reader is entitled to conclude from seeing it.
	why string
	// derive returns "" when the product provides it, or the reason it does not.
	derive func(t *testing.T) string
}

func signupClaims() []signupClaim {
	return []signupClaim{
		{
			word: "sign in",
			why:  "the account just created can be used on the panel's sign-in page",
			derive: func(t *testing.T) string {
				src := repoFile(t, "internal", "handler", "adminlogin.go")
				if !strings.Contains(src, "r.Post(adminLoginPath, a.Login)") {
					return "no POST is registered on adminLoginPath, so there is nothing to sign in to"
				}
				// AND the screen must not say it to somebody who cannot. This is the
				// round-4 blocking finding, pinned.
				var blocked strings.Builder
				if err := pages.SignupDone(pages.SignupDoneView{
					BusinessName: "X", SignInBlocked: true, SignInHref: adminLoginPath,
				}).Render(t.Context(), &blocked); err != nil {
					return "the blocked confirmation does not render: " + err.Error()
				}
				if strings.Contains(strings.ToLower(screenText(t, blocked.String())), "sign in to your dashboard") {
					return "the confirmation still invites a sign-in it has MEASURED to be " +
						"impossible; that is the round-4 finding"
				}
				return ""
			},
		},
		{
			word: "invite",
			why:  "a manager can invite employees from the panel",
			derive: func(t *testing.T) string {
				src := repoFile(t, "internal", "handler", "employeeactions.go")
				if !strings.Contains(src, "Invite") {
					return "internal/handler/employeeactions.go no longer offers an invitation"
				}
				return ""
			},
		},
		{
			word: "plaque",
			why:  "Tappa encodes and ships a physical plaque; the panel binds it to a venue",
			derive: func(t *testing.T) string {
				src := repoFile(t, "db", "queries", "tags.sql")
				if !strings.Contains(src, "AssignTagToLocation") {
					return "no query mounts a plaque on a venue, so the promise of one arriving " +
						"leads nowhere"
				}
				return ""
			},
		},
		{
			word: "dashboard",
			why:  "the reader is being pointed at a panel screen that shows what is claimed",
			derive: func(t *testing.T) string {
				// It is allowed ONLY where a panel screen genuinely backs it. Today the
				// wizard's only dashboard sentences are about signing in and about adding
				// venues, both of which are mounted; the VAT sentences were the ones that
				// were not, and TestSignupDone_PromisesNoPanelSurfaceForTheVATCheck holds
				// that separately and derives it from the generated queries.
				src := repoFile(t, "internal", "handler", "dashboard.go")
				if !strings.Contains(src, "mountSections") {
					return "the panel mounts no sections, so nothing can be 'on your dashboard'"
				}
				return ""
			},
		},
		{
			word: "vat register",
			why:  "an EU VAT lookup really happens and its outcome is recorded",
			derive: func(t *testing.T) string {
				if !strings.Contains(repoFile(t, "internal", "domain", "signup", "vies.go"),
					"viesBaseURL = \"https://ec.europa.eu") {
					return "no VIES endpoint is called, so the page may not say a register was consulted"
				}
				if !strings.Contains(repoFile(t, "db", "queries", "tenants.sql"), "vat_verified") {
					return "the outcome is not stored, so 'we checked' leaves no record"
				}
				return ""
			},
		},
		{
			word: "free",
			why:  "the founding offer really gives free months, enforced by the schema",
			derive: func(t *testing.T) string {
				if !strings.Contains(repoFile(t, "db", "migrations", "00016_add_billing_price_and_periods.sql"),
					"tappa_first_chargeable_month") {
					return "nothing in the schema grants a free period"
				}
				return ""
			},
		},
	}
}

// TestSignupSurface_EveryCapabilityClaimHoldsToday runs each derivation.
func TestSignupSurface_EveryCapabilityClaimHoldsToday(t *testing.T) {
	t.Parallel()
	claims := signupClaims()
	if len(claims) < 5 {
		t.Fatalf("%d claim(s) in the table; this scan would pass over almost anything", len(claims))
	}
	for _, c := range claims {
		if reason := c.derive(t); reason != "" {
			t.Errorf("the sign-up surface may say %q — %s — and it no longer holds: %s",
				c.word, c.why, reason)
		}
	}
}

// signupSurfaceScreens renders EVERY screen this feature serves, by name.
//
// IT IS A HELPER RATHER THAN A LOOP INSIDE ONE TEST because two different scans now
// read the same surface: the capability-word scan below, and the delivery-claim scan
// (TestSignupSurface_MakesNoDeliveryClaim). A second copy of this walk would be a
// second list of screens, and the one that is forgotten is the one the next bad
// sentence lands on — which is exactly how "posts them to you" survived on the done
// screen while a tripwire for those very words ran next door on the panel.
func signupSurfaceScreens(t *testing.T) map[string]string {
	t.Helper()
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)
	screens := map[string]string{}
	start := d.get(signupPath)
	screens["business"] = start.Body.String()
	d.post(signupPath, businessForm(d.token(start)))
	places := d.get(signupPlacesPath)
	screens["places"] = places.Body.String()
	d.post(signupPlacesPath, placesForm(d.token(places), "Front door"))
	account := d.get(signupAccountPath)
	screens["account"] = account.Body.String()
	d.advance(time.Minute)
	d.post(signupAccountPath, accountForm(d.token(account)))
	screens["done"] = d.get(signupDonePath).Body.String()
	// The screens a handler can reach that a walk does not: the problem family and the
	// blocked confirmation.
	for name, v := range map[string]pages.SignupProblemView{
		"problem-restart": problemSignupRestart, "problem-nocookie": problemSignupNoCookie,
		"problem-toomany": problemSignupTooMany, "problem-server": problemSignupServer,
	} {
		var sb strings.Builder
		if err := pages.SignupProblem(v).Render(t.Context(), &sb); err != nil {
			t.Fatalf("rendering %s: %v", name, err)
		}
		screens[name] = sb.String()
	}
	var blocked strings.Builder
	if err := pages.SignupDone(pages.SignupDoneView{
		BusinessName: "Probe Ltd", Venues: []string{"Front door"}, VATNumber: "MT12345678",
		SignInBlocked: true, SignInHref: adminLoginPath,
	}).Render(t.Context(), &blocked); err != nil {
		t.Fatalf("rendering the blocked confirmation: %v", err)
	}
	screens["done-blocked"] = blocked.String()

	if len(screens) < 9 {
		t.Fatalf("collected %d screen(s); a scan over these is not covering the surface",
			len(screens))
	}
	return screens
}

// TestSignupSurface_MakesNoDeliveryClaim points the panel's delivery tripwire at the
// sign-up surface, WHICH IS WHERE THE CLAIM ACTUALLY WAS.
//
// 🔴 THE PRODUCT WAS FORBIDDING A PROMISE ON ONE SCREEN AND SELLING IT ON ANOTHER,
// ONE CLICK APART. The done screen said "Tappa encodes them and posts them to you"
// while /admin's landing notice was under a test refusing exactly those words —
// which the panel's literal list could not see anyway, because it held "posted to
// you" and the wizard used "posts". pages/locations.templ has said "loads it here"
// since 2026-08-08 for precisely this reason (a user decision), and the done screen
// now says the same.
//
// ⚠️ WHAT IT DOES NOT COVER: the rest of the panel. /admin/policies legitimately
// says "Baseline shipped with this release" — software, not plaques — so pointing
// these patterns at every panel screen would need a second, weaker vocabulary. The
// two surfaces that talk about PLAQUES ARRIVING are the two scanned.
func TestSignupSurface_MakesNoDeliveryClaim(t *testing.T) {
	t.Parallel()
	for name, body := range signupSurfaceScreens(t) {
		assertNoDeliveryClaim(t, "the sign-up "+name+" screen", screenText(t, body))
	}
	assertDeliveryScannerWorks(t)
}

// TestSignupSurface_UsesNoUnanchoredCapabilityWord renders every screen this feature
// serves and refuses a capability word that is not in the table above.
func TestSignupSurface_UsesNoUnanchoredCapabilityWord(t *testing.T) {
	t.Parallel()
	// The vocabulary. Each entry is a way a screen tells the reader that something
	// EXISTS; they are the words that carried all three findings.
	vocabulary := []string{
		"sign in", "dashboard", "invite", "plaque", "vat register", "free",
		"we will email", "we email", "report", "export", "notification",
	}
	anchored := map[string]bool{}
	for _, c := range signupClaims() {
		anchored[c.word] = true
	}

	screens := signupSurfaceScreens(t)
	hits := 0
	for name, body := range screens {
		text := strings.ToLower(screenText(t, body))
		for _, word := range vocabulary {
			if !strings.Contains(text, word) {
				continue
			}
			hits++
			if !anchored[word] {
				t.Errorf("the %s screen says %q, which promises the reader a capability, and "+
					"nothing derives that the product provides it.\n"+
					"Add it to signupClaims with a derivation, or take the sentence out. "+
					"This is the defect that blocked M7-01 and blocked M7-02 twice.",
					name, word)
			}
		}
	}
	// ANTI-VACUITY: a vocabulary that matched nothing would approve any page.
	if hits < 5 {
		t.Fatalf("the vocabulary matched %d time(s) across %d screens; it is not reading the "+
			"pages", hits, len(screens))
	}
	t.Logf("scanned %d screens, %d capability-word occurrence(s), all anchored", len(screens), hits)
}

// TestSignup_AnUnstorableByteIsRefusedNot500 — E2, over real HTTP.
//
// 🔴 BEFORE THIS, A NUL BYTE IN A VENUE NAME ANSWERED 500. It passed validation,
// reached the driver, and came back as SQLSTATE 22021 on the one UNAUTHENTICATED
// surface in this product that WRITES — while charging the attempt budget, so the
// probe was not even free to us. internal/adminauth closed the same two byte classes
// for the login's email and manager.go gives the reasoning this follows.
//
// IT DRIVES THE BOUNDARY, not the domain: the domain's own table test proves the
// rule, and this proves the STATUS a stranger gets and that the provisioner is never
// reached.
func TestSignup_AnUnstorableByteIsRefusedNot500(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, bad string }{
		{"a NUL byte", "Front\x00door"},
		{"invalid UTF-8", "Front" + string([]byte{0xff}) + "door"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &fakeProvisioner{}
			d, _ := newSignupDriver(t, p, nil)

			start := d.get(signupPath)
			form := businessForm(d.token(start))
			form.Set("structure", "single")
			if rec := d.post(signupPath, form); rec.Code != http.StatusSeeOther {
				t.Fatalf("POST %s = %d", signupPath, rec.Code)
			}
			places := d.get(signupPlacesPath)
			rec := d.post(signupPlacesPath, placesForm(d.token(places), tc.bad))
			if rec.Code == http.StatusInternalServerError {
				t.Fatalf("a venue name carrying %s answered 500 — an unauthenticated caller "+
					"reached the database driver", tc.name)
			}
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("answered %d, want 422", rec.Code)
			}
			if p.count() != 0 {
				t.Error("the provisioner was reached")
			}
		})
	}
}

// TestSignup_ARefusedValueIsNotEchoedRaw — F6.
//
// The wizard echoes a refused value back so the visitor does not lose their typing.
// internal/domain/signup refuses NUL and invalid UTF-8 because the DATABASE cannot
// store them — so echoing them raw means declaring a byte unstorable and then writing
// it onto the wire, where every byte-oriented thing in the path (a WAF, a log shipper,
// a proxy that treats NUL as a terminator) has to deal with it.
func TestSignup_ARefusedValueIsNotEchoedRaw(t *testing.T) {
	t.Parallel()
	d, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
	start := d.get(signupPath)
	form := businessForm(d.token(start))
	form.Set("name", "Kebab\x00Factory")
	rec := d.post(signupPath, form)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}
	if strings.ContainsRune(rec.Body.String(), 0) {
		t.Error("the 422 body carries a raw NUL byte — the product refused it as unstorable " +
			"and then wrote it to the wire")
	}
	if !utf8.ValidString(rec.Body.String()) {
		t.Error("the 422 body is not valid UTF-8")
	}
}
